package jinbe

import (
	"cmp"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
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
	lg.disc = &discussionsRead{}
	mr, err := forgeMR(lg.proj, iid)
	if err == nil {
		var waiting bool
		if cp.Unclosed() && mr.State == forge.StateClosed {
			if _, waiting, err = waitingApprovalOn(cp, mr.IID); err != nil {
				return holdRateLimited(w, "mrwatch.rate_limited", err, lg.attrs)
			}
		}
		switch {
		case waiting:
			err = holdClosedDraft(cp, lg, mr)
		case !cp.Unclosed() || mr.State == forge.StateMerged || mr.State == forge.StateClosed:
			if mr.State == forge.StateMerged { // a merge implies an un-draft: end the Approval before the late pass
				err = undraftArm(cp, mr)
			}
			if err == nil {
				err = lateMRWatch(w, cp, landings, lg, mr)
			}
		case mr.State == forge.StateOpened:
			if mr.WebURL != "" {
				if err := setLine(&w, git.MRLinkKey, git.MRLinkLine(mr.WebURL)); err != nil {
					slog.Warn("mrwatch.mr_link_write_failed", append(eventlog.BeadAttrs(w), "err", err)...)
				}
			}
			if err = forgeReviewArm(cp, nil, lg, mr); err == nil {
				err = closeOutArm(cp, lg, mr, git.BranchName(string(cp.Kind), cp.ID), closeOutTarget{head: mr.SHA})
			}
			if err == nil {
				err = undraftArm(cp, mr)
			}
			recordMRFacts(lg, &w, mr) // after the arms, so it reuses the discussions read they made
		}
	}
	return holdRateLimited(w, "mrwatch.rate_limited", err, lg.attrs)
}

// undraftedLine ends the Approval History of an Approval the human approved by un-drafting
// its Merge Request (Spec FR 12).
const undraftedLine = "approved (un-drafted on GitLab)"

// undraftArm is the MR Watch's approval arm (Spec FR 11, 13): the container's open Approval
// waiting on mr (its MR: line names the iid) is closed approved once the MR's draft flag, read
// with the MR the Watch already holds, is false. An open pivot holds the close to a later
// tick, so an un-draft cannot approve a Checkpoint the pivot has frozen (ADR 0043 §1). A merged
// MR is the exception: the merge is done and the pivot cannot hold it, so the Approval closes
// anyway, its History recording that it was merged past the pivot, and never stays stranded.
func undraftArm(cp bd.Bead, mr *forge.MR) error {
	merged := mr.State == forge.StateMerged
	if mr.Draft && !merged {
		return nil
	}
	a, ok, err := waitingApprovalOn(cp, mr.IID)
	if err != nil || !ok {
		return err
	}
	pivots, err := openPivots(cp.ID)
	if err != nil || len(pivots) > 0 && !merged {
		return err
	}
	line := undraftedLine
	if len(pivots) > 0 {
		line = mergedPastPivotLine
	}
	return closeApprovalApproved(a, line)
}

// mergedPastPivotLine ends the History of an Approval whose MR was merged while a pivot was open.
const mergedPastPivotLine = "approved (un-drafted on GitLab; merged past an open pivot)"

// landedPastLine ends the History of a waiting Approval its Checkpoint landed over by trailer
// before the MR Watch read the un-draft.
const landedPastLine = "approved (un-drafted on GitLab; landed before the Watch read it)"

// closeApprovalApproved ends a's Approval History with line and closes it approved.
func closeApprovalApproved(a bd.Bead, line string) error {
	if err := bd.EditBody(showBead, updateBody, a.ID, func(body string) string {
		// An auto-approved Approval whose close failed after its MR was opened ready is not
		// the human's un-draft to claim.
		if last, _ := bd.ApprovalHistoryLast(body); strings.HasPrefix(last, bd.AutoApprovedPrefix) {
			return body
		}
		return bd.EndApprovalHistory(body, line)
	}); err != nil {
		return err
	}
	return closeIfOpen(a, bd.ApprovedReason)
}

// closeWaitingApprovals closes every Approval of a landed container still waiting on its
// un-draft, before the container closes, so none is stranded open under a closed Checkpoint.
func closeWaitingApprovals(containerID string) error {
	approvals, err := childrenOfKind(containerID, contract.KindApproval)
	if err != nil {
		return err
	}
	for _, a := range approvals {
		if bd.AwaitingUndraft(a) {
			if err := closeApprovalApproved(a, landedPastLine); err != nil {
				return err
			}
		}
	}
	return nil
}

// waitingApprovalOn is cp's open Approval waiting on MR iid, if it has one.
func waitingApprovalOn(cp bd.Bead, iid int) (bd.Bead, bool, error) {
	approvals, err := childrenOfKind(cp.ID, contract.KindApproval)
	if err != nil {
		return bd.Bead{}, false, err
	}
	i := slices.IndexFunc(approvals, func(a bd.Bead) bool { return bd.AwaitingUndraft(a) && git.RecordedMR(a.Description) == iid })
	if i < 0 {
		return bd.Bead{}, false, nil
	}
	return approvals[i], true, nil
}

// holdClosedDraft is the MR Watch's closed-MR arm (Spec FR 15, 16): a human closed the Draft MR
// a waiting Approval names, so the container takes the ADR 0185 §4 path, a Brook Task to
// reopen or abandon. The Approval and the Watch stay open, so a reopen is read on the next tick.
func holdClosedDraft(cp bd.Bead, lg legs, mr *forge.MR) error {
	ref := git.MRRef(lg.mrPath, mr.IID)
	noun := "Checkpoint"
	if cp.Kind == contract.KindChart {
		noun = "Chart"
	}
	lead := fmt.Sprintf("MR %s for %s was closed on the Forge while its Approval waited for the un-draft: the waiting Approval resumes on a reopened MR, or the %s is abandoned.", ref, cp.ID, noun)
	do := fmt.Sprintf("reopen MR %s on the Forge at the MR link, or run `mugicrew withdraw %s`. The waiting Approval resumes on a reopened MR.", ref, cp.ID)
	return holdClosedMR(cp, lg, mr, lead, do)
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

// recordMRFactsOnWatches is mirrorMR's route to the facts: the container's open MR Watches on
// iid get them, as backfillMRLink finds them for MR link:. A failed lookup only warns.
func recordMRFactsOnWatches(l, cp bd.Bead, iid int, lg legs, mr *forge.MR) {
	watches, err := childrenOfKind(cp.ID, contract.KindMRWatch)
	if err != nil {
		slog.Warn("land.mr_facts_write_failed", append(eventlog.BeadAttrs(l), "err", err)...)
		return
	}
	for _, w := range watches {
		if w.Open() && git.RecordedMR(w.Description) == iid {
			recordMRFacts(lg, &w, mr)
		}
	}
}

// recordMRFacts sets the Watch's display-only "MR head:", "MR pipeline:" and "MR merge
// status:" lines from the open MR just read (ADR 0385), in one write of the lines whose value
// changed. A failed write only warns: the next tick sees the body unchanged and retries, and
// a failed reconcile would read as retrying.
func recordMRFacts(lg legs, w *bd.Bead, mr *forge.MR) {
	pipeline, err := mrPipelineValue(lg.proj, w.Description, mr.HeadPipeline)
	if err != nil {
		slog.Warn("mrwatch.mr_ran_on_lookup_failed", append(eventlog.BeadAttrs(*w), "err", err)...)
	}
	want := []struct{ key, value string }{
		{git.MRHeadKey, mr.SHA},
		{git.MRPipelineKey, pipeline}, // "" on a failed lookup: the line stays as it is
		{git.MRMergeStatusKey, mr.MergeStatus},
	}
	if threads := openThreads(lg, mr, pipeline); threads != "" {
		want = append(want, struct{ key, value string }{git.MRThreadsKey, threads})
	}
	body := w.Description
	for _, f := range want {
		if old, _ := git.ParseLine(body, f.key); f.value != "" && old != f.value {
			body = setBodyLine(body, f.key+" ", git.MRFactLine(f.key, f.value))
		}
	}
	if body == w.Description {
		return
	}
	if err := updateBody(w.ID, []string{"--description", body}); err != nil {
		slog.Warn("mrwatch.mr_facts_write_failed", append(eventlog.BeadAttrs(*w), "err", err)...)
		return
	}
	w.Description = body
}

// mrPipelineValue is "none" without a head pipeline, else "<status> <id> <ran-on>": a branch
// pipeline ran on its sha, a merged-results one on its merge commit's second parent (Spec D5),
// and "-" stands for a merge-train pipeline, which never counts as on head. The parent is
// looked up only when body's MR pipeline: line names another pipeline id, since a commit's
// parents never change and a retry keeps both id and sha; a failed lookup returns "".
func mrPipelineValue(p forge.Project, body string, pl *forge.Pipeline) (string, error) {
	if pl == nil {
		return "none", nil
	}
	ranOn := pl.SHA
	switch {
	case strings.HasSuffix(pl.Ref, "/train"):
		ranOn = "-"
	case pl.MergedResults():
		ranOn = "-"
		old, _ := git.ParseLine(body, git.MRPipelineKey)
		if f := strings.Fields(old); len(f) == 3 && f[1] == strconv.Itoa(pl.ID) && f[2] != "-" {
			ranOn = f[2]
		} else if src, err := forgeSource(p, pl.SHA); err != nil {
			return "", err
		} else if src != "" {
			ranOn = src
		}
	case ranOn == "":
		ranOn = "-"
	}
	return fmt.Sprintf("%s %d %s", pl.Status, pl.ID, ranOn), nil
}

// openThreads is the "MR threads:" value "<open>/<resolvable>", counted from the tick's
// discussions read; "" leaves the line as it is. The read is whatever the Forge Review or
// close-out arm already made this tick (the user_notes_count gate opened), else one made
// only when the tick is candidate green: head pipeline success and on head, and a settled
// can_be_merged. Any other tick makes no read (Spec D4).
func openThreads(lg legs, mr *forge.MR, pipeline string) string {
	if lg.disc == nil || (!lg.disc.done && !candidateGreen(mr, pipeline)) {
		return ""
	}
	ds, err := lg.discussions(mr.IID)
	if err != nil {
		return ""
	}
	open, resolvable := 0, 0
	for _, d := range ds {
		if d.Resolvable {
			resolvable++
			if !d.Resolved {
				open++
			}
		}
	}
	return fmt.Sprintf("%d/%d", open, resolvable)
}

// candidateGreen is whether the MR's other facts are already green: pipeline is the
// "<status> <id> <ran-on>" value just computed.
func candidateGreen(mr *forge.MR, pipeline string) bool {
	f := strings.Fields(pipeline)
	return len(f) == 3 && f[0] == "success" && f[2] == mr.SHA && mr.MergeStatus == "can_be_merged"
}
