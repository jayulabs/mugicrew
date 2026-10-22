package jinbe

import (
	"cmp"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
)

// spawnMRWatch gives a container its MR Watch (Spec D4) when recordMR first records an MR
// for it, copying the Landing's MR: and MR link: lines so the Watch knows the iid between Landings.
// SpawnChildIfAbsent keeps it to one open per container.
func spawnMRWatch(cp bd.Bead, lg legs, iid int, webURL string) error {
	body := git.MRLine(lg.mrPath, iid) + "\n"
	if webURL != "" {
		body += git.MRLinkLine(webURL) + "\n"
	}
	_, _, err := spawnChild(cp.ID, bd.Successor{
		Kind:        contract.KindMRWatch,
		Title:       fmt.Sprintf("Watch %s", git.MRRef(lg.mrPath, iid)),
		Description: body,
	})
	return err
}

// closeMRWatches closes every open MR Watch of a landed container, before the container
// itself closes.
func closeMRWatches(containerID, reason string) error {
	watches, err := childrenOfKind(containerID, contract.KindMRWatch)
	if err != nil {
		return err
	}
	for _, w := range watches {
		if err := closeIfOpen(w, reason); err != nil {
			return err
		}
	}
	return nil
}

// reconcileMRWatch is the MR Watch's one read of its container's MR (Spec D4). It skips
// while an open container has a pending Landing (mirrorMR reads the MR then, once per
// period). Over a still-open MR it runs the Forge Review arm then the close-out arm; once the
// container is closed or the MR is merged or closed it runs only the late pass, and closes
// once nothing is owed (ADR 0343 (an-mr-watch-closes-once-nothing-is-owed…)). A Forge 429 is held and logged once.
func reconcileMRWatch(w bd.Bead) error {
	if !w.Open() {
		return nil
	}
	cp, err := showBead(w.Parent)
	if err != nil {
		return err
	}
	if name, _, err := landStrategy(); err != nil || name != git.LandStrategyGitlab {
		if err == nil && !cp.Unclosed() {
			return closeIfOpen(w, "container closed")
		}
		return err
	}
	landings, err := landingsOf(cp.ID)
	if err != nil {
		return err
	}
	if cp.Unclosed() && slices.ContainsFunc(landings, bd.Bead.Open) {
		return nil
	}
	lg, ok, err := legsFor(cp, true)
	if err != nil || !ok {
		return err
	}
	iid := git.RecordedMR(w.Description)
	if iid == 0 {
		if !cp.Unclosed() {
			return closeIfOpen(w, "container closed")
		}
		return nil
	}
	mr, err := forgeMR(lg.proj, iid)
	if err == nil {
		switch {
		case !cp.Unclosed() || mr.State == forge.StateMerged || mr.State == forge.StateClosed:
			err = lateMRWatch(w, cp, landings, lg, mr)
		case mr.State == forge.StateOpened:
			if mr.WebURL != "" {
				if err := setLine(&w, git.MRLinkKey, git.MRLinkLine(mr.WebURL)); err != nil {
					slog.Warn("mrwatch.mr_link_write_failed", append(eventlog.BeadAttrs(w), "err", err)...)
				}
			}
			if err = forgeReviewArm(cp, nil, lg, mr); err == nil {
				err = closeOutArm(cp, lg, mr, git.BranchName(string(cp.Kind), cp.ID), closeOutTarget{head: mr.SHA})
			}
		}
	}
	return holdRateLimited(w, "mrwatch.rate_limited", err, lg.attrs)
}

// abandonAfter is how long a closed container's Watch keeps closing out before a human is told.
const abandonAfter = 7 * 24 * time.Hour

// fileAbandoned files the Signal an abandoned Watch is carried by: unparented, discovered-from
// the Watch, and added only if no Signal of that title is (a retried abandon files it once).
var fileAbandoned = bd.SpawnOffshootSignalTitledIfAbsent

// lateMRWatch is the Watch's late pass: the close-out arm alone, never the Forge Review arm,
// so no filed child is spawned under a closed container or a dead MR. The Watch closes once
// nothing is owed, with the reason that says what ended; a close-out still owed 7 days after
// the container closed is abandoned to a Signal (ADR 0345 (an-outliving-mr-watch-carries-its-close-out-lines…)).
func lateMRWatch(w, cp bd.Bead, landings []bd.Bead, lg legs, mr *forge.MR) error {
	branch := git.BranchName(string(cp.Kind), cp.ID)
	target := closeOutTarget{head: mr.SHA, final: true}
	landed := false
	if l, ok := bd.NewestLanding(landings); ok && !l.Open() && strings.HasPrefix(l.CloseReason, landedReason) {
		// landed: the container branch is gone, so the Tip (else the merge) carries the trailers
		sha := landedHead(lg.repo, strings.TrimSpace(strings.TrimPrefix(l.CloseReason, landedReason)))
		branch, target, landed = sha, closeOutTarget{head: sha, merged: true, landing: l}, true
	}
	if !cp.Unclosed() {
		target.watch = w
		clearCloseOutLines(cp)
	}
	res, err := closeOutPass(cp, lg, mr, branch, target)
	if err != nil {
		return err
	}
	owed, err := mrWatchOwes(cp, mr.IID, res)
	if err != nil {
		return err
	}
	if len(owed) > 0 {
		if cp.Unclosed() || cp.Closed.IsZero() || clock().Sub(cp.Closed) < abandonAfter {
			return nil
		}
		return abandonMRWatch(w, cp, lg, mr, landed, owed)
	}
	switch {
	case !cp.Unclosed():
		return closeIfOpen(w, "container closed")
	case mr.State == forge.StateMerged:
		return closeIfOpen(w, "obsolete: merge request merged")
	}
	return closeIfOpen(w, "obsolete: merge request closed")
}

// clearCloseOutLines removes the close-out lines a closed container still carries from before
// it closed, in one write.
func clearCloseOutLines(cp bd.Bead) {
	has := func(prefix string) bool {
		v, _ := git.ParseLine(cp.Description, strings.TrimSpace(prefix))
		return v != ""
	}
	if !has(closeOutsDuePrefix) && !has(closeOutsFailingPrefix) {
		return
	}
	writeMarker(cp, func(body string) string {
		return bd.RemoveBodyLine(bd.RemoveBodyLine(body, closeOutsDuePrefix), closeOutsFailingPrefix)
	})
}

// abandonMRWatch hands the owed close-outs of the closed container cp to a human: it files the
// Signal, warns, then closes w carried by it. A filing failure is returned before the close, so
// the Watch retries and never closes without its Signal. The Watch keeps its lines.
func abandonMRWatch(w, cp bd.Bead, lg legs, mr *forge.MR, landed bool, owed []string) error {
	how := "swept"
	if landed {
		how = "landed"
	}
	ref := git.MRRef(lg.mrPath, mr.IID)
	link := cmp.Or(mr.WebURL, git.RecordedMRLink(w.Description))
	var body strings.Builder
	fmt.Fprintf(&body, "%s (%s) closed %s, and its close-outs on %s are still owed after 7 days.\nMR: %s\n", cp.Title, cp.ID, how, ref, ref)
	if link != "" {
		body.WriteString(git.MRLinkLine(link) + "\n")
	}
	body.WriteString("Owed:\n")
	for _, l := range owed {
		body.WriteString("- " + l + "\n")
	}
	body.WriteString("Fix: reply to and resolve those threads by hand, or clear the lock, then close this Signal.\n")
	title := fmt.Sprintf("%s: close-outs abandoned on !%d", cp.Title, mr.IID)
	id, _, err := fileAbandoned(w.ID, title, body.String())
	if err != nil {
		return err
	}
	slog.Warn("mrwatch.closeout_abandoned", append(eventlog.BeadAttrs(w), "mr", ref, "signal", id)...)
	return closeIfOpen(w, "obsolete: close-out abandoned; carried by "+id)
}

// mrWatchOwes lists, one line each, what cp's filed children for MR iid still owe: a child
// that is unclosed, or closed with a discussion not yet closed out, read afresh after the pass
// (res holds the last error of each failing one).
func mrWatchOwes(cp bd.Bead, iid int, res closeOutResult) ([]string, error) {
	kids, err := childrenOfKind(cp.ID, contract.FiledKind(cp.Kind))
	if err != nil {
		return nil, err
	}
	var owed []string
	for _, k := range kids {
		for _, d := range sourcesOf(k, iid) {
			if k.Unclosed() {
				owed = append(owed, fmt.Sprintf("%s, discussion %s: not closed yet", k.ID, d))
			} else if !closedOut(k, d) {
				owed = append(owed, fmt.Sprintf("%s, discussion %s: %s", k.ID, d, cmp.Or(res.lastErr[k.ID], "not closed out")))
			}
		}
	}
	return owed, nil
}
