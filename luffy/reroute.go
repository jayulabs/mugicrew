package luffy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
)

// RerouteItem is one --pivot or --critique body of a reroute, in the order Luffy gave
// them: the first Critique after a pivot is that pivot's Ask Critique.
type RerouteItem struct {
	Pivot bool
	// Path is the file the CLI reads Body from; SeedReroute reads Body only.
	Path string
	Body string
}

// RaiseMerge is one --merge-into: an open, published pivot the host blocks, and the
// evidence body the publish posts on it.
//
// After is its argv position: how many --pivot and --critique items Luffy gave before
// it. The first --critique at or after that position is its Ask Critique. It is not
// recorded on the Signal; the Ask edge is.
type RaiseMerge struct {
	Pivot    string `json:"pivot"`
	Evidence string `json:"evidence"`
	After    int    `json:"-"`
}

// RaiseCheckpointMeta is the metadata key a pivot Draft carries naming its host; the
// publish gates every pivot Draft that has it, whoever drafted it (ADR 0326 §4).
const RaiseCheckpointMeta = "raise_checkpoint"

// RaiseMergesMeta is the creator's own metadata key holding a raise's merges until publish.
const RaiseMergesMeta = "raise_merges"

// SeedReroute is the whole job of `reconcile <signal> --checkpoint <cp> …` (Spec
// decision 3, ADR 0322): file the Signal's reroute onto its one host Checkpoint as
// Drafts by the Signal — a kind:pivot per --pivot (caused-by the Signal, the host in its
// metadata, no gate edge yet), a Critique per --critique (parented to the host,
// caused-by the Signal), each pivot's Ask Critique discovered-from it — and record the
// --merge-into targets on the Signal's metadata, writing nothing to the targets. Every
// refusal runs before any write. It never closes the Signal or gates the host: the
// clean-exit publish does. It carries the Signal's dependents (carryReroute).
func SeedReroute(sig bd.Bead, checkpoint string, items []RerouteItem, merges []RaiseMerge) error {
	const leg = "reconcile --checkpoint"
	if sig.Kind != contract.KindSignal {
		return fmt.Errorf("%s: %s is kind %q, not a signal", leg, sig.ID, sig.Kind)
	}
	if !sig.Open() {
		return fmt.Errorf("%s: signal %s is not open; refusing to reroute it", leg, sig.ID)
	}
	if ini, err := initiativeOf(sig); err != nil {
		return err
	} else if ini != "" {
		return fmt.Errorf("%s: signal %s is a member of Initiative %s; refusing to reroute it: triage it ACCEPT, FAST LANE, REJECT or DUPLICATE", leg, sig.ID, ini)
	}
	if isJiraRef(sig.ExternalRef) {
		return fmt.Errorf("%s: signal %s mirrors Jira work item %s; refusing to reroute it: triage it INITIATIVE, REJECT or DUPLICATE", leg, sig.ID, sig.ExternalRef)
	}
	if err := refuseBeside(sig.ID, leg, "reroute it", contract.KindChart, contract.KindSpec, contract.KindInitiative); err != nil {
		return err
	}
	if existing, err := rerouteDrafts(sig.ID); err != nil {
		return err
	} else if len(existing) > 0 {
		return fmt.Errorf("%s: signal %s already has reroute Drafts; drop and re-triage to change the reroute", leg, sig.ID)
	}
	if len(items) == 0 {
		return fmt.Errorf("%s: nothing to file for %s: every reroute files at least one --pivot or --critique; a --merge-into needs a --critique carrying the Ask", leg, sig.ID)
	}
	if err := checkRerouteBodies(items, merges); err != nil {
		return fmt.Errorf("%s: %w", leg, err)
	}
	asks, mergeAsks, err := pairAsks(items, merges)
	if err != nil {
		return fmt.Errorf("%s: %w", leg, err)
	}
	host, _, err := CheckHost(checkpoint, liveHostReads())
	if err != nil {
		return fmt.Errorf("%s: %w", leg, err)
	}
	if err := checkMerges(host.ID, merges); err != nil {
		return fmt.Errorf("%s: %w", leg, err)
	}
	notifyHost(host)

	meta, err := json.Marshal(map[string]string{RaiseCheckpointMeta: host.ID})
	if err != nil {
		return err
	}
	// before any Draft, so a crash cannot lose them; always written, so a re-run with
	// no --merge-into replaces any leftover merges instead of publishing them
	if len(merges) > 0 {
		err = setMetadata(sig.ID, RaiseMergesMeta, merges)
	} else {
		err = clearMetadata(sig.ID, RaiseMergesMeta)
	}
	if err != nil {
		return eventlog.Record("triage.reroute", "triage.failed", err, eventlog.BeadAttrs(sig)...)
	}
	var pivots, critiques []string
	pivotOf := map[int]string{} // item index → the pivot id it filed
	for i, it := range items {
		switch {
		case it.Pivot:
			id, err := createDraft(sig.ID, bd.Successor{Kind: contract.KindPivot, Title: draftTitle(it.Body, sig.Title), Description: it.Body, DraftedBy: sig.ID, Metadata: string(meta)})
			if err != nil {
				return eventlog.Record("triage.reroute", "triage.failed", err, eventlog.BeadAttrs(sig)...)
			}
			pivotOf[i] = id
			pivots = append(pivots, id)
		default:
			var deps []string
			for p, a := range asks {
				if a == i {
					deps = []string{"discovered-from:" + pivotOf[p]}
				}
			}
			for m, a := range mergeAsks { // the edge sits on this Draft; the target is written nothing
				if a == i {
					deps = append(deps, "discovered-from:"+merges[m].Pivot)
				}
			}
			id, err := createDraft(sig.ID, bd.Successor{Kind: contract.KindCritique, Title: draftTitle(it.Body, sig.Title), Description: it.Body, DraftedBy: sig.ID, Parent: host.ID}, deps...)
			if err != nil {
				return eventlog.Record("triage.reroute", "triage.failed", err, eventlog.BeadAttrs(sig)...)
			}
			critiques = append(critiques, id)
		}
	}
	mergeIDs := mergeTargets(merges)
	if err := carryReroute(sig, host.ID, firstOf(pivots, critiques)); err != nil {
		return err
	}
	return recordReroute(sig, host.ID, pivots, critiques, mergeIDs)
}

// draftTitle titles a reroute Draft from its body: the first heading, else the first
// non-blank line (a pivot's is its "FR:" line), else fallback.
func draftTitle(body, fallback string) string {
	first := ""
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if h := strings.TrimLeft(l, "#"); h != l {
			return clip(strings.TrimSpace(h))
		}
		if first == "" {
			first = l
		}
	}
	if first == "" {
		return fallback
	}
	return clip(first)
}

func clip(s string) string {
	if r := []rune(s); len(r) > 100 {
		return string(r[:100])
	}
	return s
}

// firstOf is the first pivot, else the first Critique: the Signal's caused-by
// successor an open Latch moves onto.
func firstOf(pivots, critiques []string) string {
	if len(pivots) > 0 {
		return pivots[0]
	}
	if len(critiques) > 0 {
		return critiques[0]
	}
	return ""
}

// carryReroute carries the open Signal's ordering dependents onto the host and its
// open Latches onto succ (Spec decision 7, ADR 0322 §4), and records the carry. A
// carry error fails the reconcile, leaving the Signal open for the next to retry.
func carryReroute(sig bd.Bead, host, succ string) error {
	if !sig.Open() || succ == "" {
		return nil
	}
	moved, skipped, err := carryTargets(sig.ID, host, succ)
	return eventlog.RecordCarry(sig, host, moved, skipped, err)
}

// mergeTargets is the pivot id of each merge.
func mergeTargets(merges []RaiseMerge) []string {
	var ids []string
	for _, m := range merges {
		ids = append(ids, m.Pivot)
	}
	return ids
}

// recordReroute logs the one triage.reroute (Spec FR 30).
func recordReroute(sig bd.Bead, host string, pivots, critiques, merges []string) error {
	return eventlog.Record("triage.reroute", "triage.failed", nil,
		append(eventlog.BeadAttrs(sig), "host", host, "pivots", pivots, "critiques", critiques, "merges", merges)...)
}

// checkRerouteBodies refuses an empty pivot, Critique or evidence body and a merge
// with no target.
func checkRerouteBodies(items []RerouteItem, merges []RaiseMerge) error {
	for _, it := range items {
		if strings.TrimSpace(it.Body) == "" {
			return errors.New("empty pivot or Critique body")
		}
	}
	for _, m := range merges {
		if m.Pivot == "" || strings.TrimSpace(m.Evidence) == "" {
			return errors.New("empty --merge-into target or evidence body")
		}
	}
	return nil
}

// pairAsks maps each pivot's item index, and each merge's index, to its Ask Critique's
// item index: the first Critique after it in argv order that no earlier pivot or merge
// took. A pivot or merge with none is an error before any write.
func pairAsks(items []RerouteItem, merges []RaiseMerge) (pivotAsks, mergeAsks map[int]int, err error) {
	pivotAsks, mergeAsks = map[int]int{}, map[int]int{}
	taken := map[int]bool{}
	ask := func(from int) (int, bool) {
		for j := from; j < len(items); j++ {
			if !items[j].Pivot && !taken[j] {
				taken[j] = true
				return j, true
			}
		}
		return 0, false
	}
	mi := 0
	claimMerges := func(upTo int) error { // merges given before item upTo
		for ; mi < len(merges) && merges[mi].After <= upTo; mi++ {
			j, ok := ask(merges[mi].After)
			if !ok {
				return errors.New("every --merge-into needs an Ask Critique: pass a --critique after it")
			}
			mergeAsks[mi] = j
		}
		return nil
	}
	for i, it := range items {
		if err := claimMerges(i); err != nil {
			return nil, nil, err
		}
		if !it.Pivot {
			continue
		}
		j, ok := ask(i + 1)
		if !ok {
			return nil, nil, errors.New("every --pivot needs an Ask Critique: pass a --critique after it")
		}
		pivotAsks[i] = j
	}
	if err := claimMerges(len(items)); err != nil {
		return nil, nil, err
	}
	return pivotAsks, mergeAsks, nil
}

// HostReads are the reads CheckHost makes, so the publish recheck (package session)
// shares the refusals and fakes only its own seams.
type HostReads struct {
	Show     func(id string) (bd.Bead, error)
	SpecOf   func(id string) (string, bool, error)
	Landings func(host string) ([]bd.Bead, error)
	// Landed reports whether host's Checkpoint: trailer is on its repo's mainline.
	Landed func(host bd.Bead, trailer string) (bool, error)
}

// NewHostReads is HostReads over the given seams, so the reconcile and the publish
// recheck build the same reads and differ only in what they fake.
func NewHostReads(show func(string) (bd.Bead, error), specOf func(string) (string, bool, error),
	children func(parent string, kind contract.Kind) ([]bd.Bead, error),
	landed func(host bd.Bead, trailer string) (bool, error)) HostReads {
	return HostReads{
		Show:   show,
		SpecOf: specOf,
		Landings: func(host string) ([]bd.Bead, error) {
			return children(host, contract.KindLanding)
		},
		Landed: landed,
	}
}

// HostLanded reports whether trailer is on the mainline of host's repo; only
// git.ErrNotLanded is a miss.
func HostLanded(host bd.Bead, trailer string) (bool, error) {
	_, err := repoLanded(repoOf(host), trailer)
	if errors.Is(err, git.ErrNotLanded) {
		return false, nil
	}
	return err == nil, err
}

// liveHostReads are HostReads over this package's seams.
func liveHostReads() HostReads {
	return NewHostReads(show, specOf, childrenOfKind, HostLanded)
}

// CheckHost is the host's refusals (Spec FR 17–19), run at reconcile and again at
// publish: an unclosed kind:checkpoint that SpecOf resolves, with no unclosed Landing
// (local or GitLab-pending) and no land trailer on its repo's mainline. It returns
// the host and its Spec.
func CheckHost(id string, r HostReads) (host bd.Bead, spec string, err error) {
	host, err = r.Show(id)
	if err != nil {
		return bd.Bead{}, "", err
	}
	if host.Kind != contract.KindCheckpoint || !host.Unclosed() {
		return bd.Bead{}, "", fmt.Errorf("%s is not an open checkpoint (kind %q, status %q)", id, host.Kind, host.Status)
	}
	spec, ok, err := r.SpecOf(id)
	if err != nil {
		return bd.Bead{}, "", err
	}
	if !ok {
		return bd.Bead{}, "", fmt.Errorf("checkpoint %s has no Spec to relate a pivot to", id)
	}
	landings, err := r.Landings(id)
	if err != nil {
		return bd.Bead{}, "", err
	}
	if slices.ContainsFunc(landings, bd.Bead.Unclosed) {
		return bd.Bead{}, "", fmt.Errorf("checkpoint %s is landing; triage the Signal again after the land", id)
	}
	landed, err := r.Landed(host, git.TrailerFor(string(contract.KindCheckpoint), id))
	if err != nil {
		return bd.Bead{}, "", err
	}
	if landed {
		return bd.Bead{}, "", fmt.Errorf("checkpoint %s has already landed (its trailer is on mainline); triage the Signal again as a new defect", id)
	}
	return host, spec, nil
}

// checkMerges requires each merge target to be an open, published kind:pivot the host
// blocks; a Draft never qualifies. Nothing is written to a target.
func checkMerges(host string, merges []RaiseMerge) error {
	if len(merges) == 0 {
		return nil
	}
	gated, err := hostPivots(host)
	if err != nil {
		return err
	}
	for _, m := range merges {
		t, err := show(m.Pivot)
		if err != nil {
			return err
		}
		if t.Kind != contract.KindPivot || t.Draft() || !t.Open() || !slices.ContainsFunc(gated, func(b bd.Bead) bool { return b.ID == m.Pivot }) {
			return fmt.Errorf("--merge-into %s: not an open, published pivot that %s blocks", m.Pivot, host)
		}
	}
	return nil
}

// notifyHost prints the warnings and notes that follow passing checks: the MR's URL
// when the host has an open MR Watch but no open Landing (the Landing refusal already
// ran), and any open Surgery on the host (Spec FR 20, 21). Reads are best-effort.
func notifyHost(host bd.Bead) {
	for _, n := range hostNotes(host) {
		notify("reconcile", "%s", n)
	}
}

// hostNotes is notifyHost's lines: "warning: …" for an open MR, "note: …" for each
// open Surgery.
func hostNotes(host bd.Bead) (notes []string) {
	if watches, err := childrenOfKind(host.ID, contract.KindMRWatch); err == nil {
		for _, w := range watches {
			if w.Open() {
				notes = append(notes, fmt.Sprintf("warning: %s has an open MR %s; it stays mergeable at its old pin until the next Landing", host.ID, mrURL(w)))
				break
			}
		}
	}
	if surgeries, err := surgeriesOf(host.ID); err == nil {
		for _, s := range surgeries {
			if s.Open() {
				notes = append(notes, fmt.Sprintf("note: %s has an open Surgery %s (%s); you may fix the defect by hand there", host.ID, s.ID, s.Title))
			}
		}
	}
	return notes
}

// rerouteDrafts is the Signal's reroute Drafts: its pivot and Critique Drafts that are
// caused-by it.
func rerouteDrafts(sigID string) ([]bd.Bead, error) {
	drafts, err := draftsOf(sigID)
	if err != nil {
		return nil, err
	}
	pivots, critiques := RerouteDrafts(sigID, drafts)
	return append(pivots, critiques...), nil
}

// RerouteDrafts splits a Signal's Drafts into its reroute pivots and Critiques: those
// caused-by it.
func RerouteDrafts(sigID string, drafts []bd.Bead) (pivots, critiques []bd.Bead) {
	for _, d := range drafts {
		switch {
		case d.CausedBy != sigID:
		case d.Kind == contract.KindPivot:
			pivots = append(pivots, d)
		case d.Kind == contract.KindCritique:
			critiques = append(critiques, d)
		}
	}
	return pivots, critiques
}

// RaiseMergesOf is the --merge-into targets recorded on the creator's metadata.
func RaiseMergesOf(sig bd.Bead) ([]RaiseMerge, error) {
	raw, ok := sig.Metadata[RaiseMergesMeta]
	if !ok {
		return nil, nil
	}
	var merges []RaiseMerge
	if err := json.Unmarshal(raw, &merges); err != nil {
		return nil, fmt.Errorf("%s has unreadable %s metadata: %w", sig.ID, RaiseMergesMeta, err)
	}
	return merges, nil
}

// PivotHost is the host a reroute pivot Draft names in its metadata, or "".
func PivotHost(p bd.Bead) string {
	var h string
	if raw, ok := p.Metadata[RaiseCheckpointMeta]; ok {
		_ = json.Unmarshal(raw, &h)
	}
	return h
}

// RerouteHost is the one host a reroute's pivot and Critique Drafts name: each pivot's
// raise_checkpoint metadata and each Critique's parent. A pivot naming none, or two
// Drafts naming different hosts, is an error.
func RerouteHost(pivots, critiques []bd.Bead) (string, error) {
	hosts, err := RaiseHosts(pivots, critiques)
	switch {
	case err != nil:
		return "", err
	case len(hosts) > 1:
		return "", fmt.Errorf("Drafts name more than one host (%s)", strings.Join(hosts, ", "))
	case len(hosts) == 0:
		return "", nil
	}
	return hosts[0], nil
}

// RaiseDrafts splits a creator's Drafts into the pivots and Critiques of its raise. A
// Signal's are those caused-by it (RerouteDrafts); a Task's, which no edge marks, are its
// pivot Drafts carrying the raise_checkpoint metadata and its Critique Drafts.
func RaiseDrafts(creator bd.Bead, drafts []bd.Bead) (pivots, critiques []bd.Bead) {
	if creator.Kind == contract.KindSignal {
		return RerouteDrafts(creator.ID, drafts)
	}
	for _, d := range drafts {
		switch {
		case d.Kind == contract.KindPivot && PivotHost(d) != "":
			pivots = append(pivots, d)
		case d.Kind == contract.KindCritique:
			critiques = append(critiques, d)
		}
	}
	return pivots, critiques
}

// RaiseHosts is each Checkpoint a raise's Drafts name, in first-seen order: a pivot's
// raise_checkpoint metadata, a Critique's parent. A pivot naming none is an error.
func RaiseHosts(pivots, critiques []bd.Bead) ([]string, error) {
	var hosts []string
	add := func(h string) {
		if h != "" && !slices.Contains(hosts, h) {
			hosts = append(hosts, h)
		}
	}
	for _, p := range pivots {
		h := PivotHost(p)
		if h == "" {
			return nil, fmt.Errorf("pivot Draft %s names no host (%s metadata)", p.ID, RaiseCheckpointMeta)
		}
		add(h)
	}
	for _, c := range critiques {
		add(c.Parent)
	}
	return hosts, nil
}

// refuseBesideReroute refuses leg beside the Signal's reroute Drafts, before any write.
func refuseBesideReroute(sigID, leg, what string) error {
	drafts, err := rerouteDrafts(sigID)
	if err != nil || len(drafts) == 0 {
		return err
	}
	return fmt.Errorf("%s: signal %s already has reroute Draft %s; refusing to %s", leg, sigID, drafts[0].ID, what)
}

// reconcileReroute is the reroute leg of a flagless reconcile (ADR 0322): the Drafts
// are already filed, so it checks they are whole — every pivot has its Ask Critique,
// all on one host — and records triage.reroute. A gap it cannot fill (the bodies are
// gone) is an error pointing at drop and re-triage. It never closes the Signal.
func reconcileReroute(sig bd.Bead, drafts []bd.Bead) error {
	pivotDrafts, critiqueDrafts := RerouteDrafts(sig.ID, drafts)
	host, err := RerouteHost(pivotDrafts, critiqueDrafts)
	if err != nil {
		return fmt.Errorf("reconcile: signal %s: %w; drop and re-triage", sig.ID, err)
	}
	pivots, critiques := bd.IDs(pivotDrafts), bd.IDs(critiqueDrafts)
	for _, d := range pivotDrafts {
		if !slices.ContainsFunc(drafts, func(c bd.Bead) bool {
			return c.Kind == contract.KindCritique && slices.Contains(c.DiscoveredFrom, d.ID)
		}) {
			return fmt.Errorf("reconcile: reroute pivot %s of signal %s has no Ask Critique; drop and re-triage to change the reroute", d.ID, sig.ID)
		}
	}
	merges, err := RaiseMergesOf(sig)
	if err != nil {
		return fmt.Errorf("reconcile: %w", err)
	}
	for _, m := range merges {
		if !slices.ContainsFunc(drafts, func(c bd.Bead) bool {
			return c.Kind == contract.KindCritique && slices.Contains(c.DiscoveredFrom, m.Pivot)
		}) {
			return fmt.Errorf("reconcile: --merge-into %s of signal %s has no Ask Critique; drop and re-triage to change the reroute", m.Pivot, sig.ID)
		}
	}
	if err := carryReroute(sig, host, firstOf(pivots, critiques)); err != nil {
		return err
	}
	return recordReroute(sig, host, pivots, critiques, mergeTargets(merges))
}

// mrURL is the web URL of the open MR a host's MR Watch tracks, or the MR's ref
// when the Forge can't be read: the warning is advisory, so no read failure refuses.
var mrURL = func(w bd.Bead) string {
	path, iid := git.RecordedMRPath(w.Description)
	ref := git.MRRef(path, iid)
	fp, err := git.ForgeProject()
	if err != nil {
		return ref
	}
	proj, n, err := forge.ParseMRRef(ref, fp)
	if err != nil {
		return ref
	}
	mr, err := forge.GetMR(proj, n)
	if err != nil || mr == nil {
		return ref
	}
	return mr.WebURL
}

// notify writes a warning or note under leg's prefix to stderr, where the agent and the human
// both read it.
var notify = func(leg, format string, a ...any) { fmt.Fprintf(os.Stderr, leg+": "+format+"\n", a...) }

// Seams over the reads and writes the reroute leg adds to luffy.go's.
var (
	childrenOfKind   = bd.ChildrenOfKind
	hostPivots       = bd.OpenPivots
	draftsOf         = bd.Drafts
	createDraft      = bd.CreateDraft
	createRaiseDraft = bd.CreateRaiseDraft
	setMetadata      = bd.SetMetadata
	clearMetadata    = bd.ClearMetadata
)
