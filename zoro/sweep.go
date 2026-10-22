package zoro

import (
	"fmt"
	"slices"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/git"
)

// Seams over the sweep's bd/git legs (the zoro test swaps them, like reconcileSpec's).
var (
	spawnDiscovered = bd.SpawnDiscoveredIfAbsent
	supersede       = bd.Supersede
	openChildBeads  = bd.OpenChildBeads
	forceClose      = bd.ForceClose
	openPivots      = bd.OpenPivots
	spawnSurvey     = bd.SpawnSurveyIfAbsent
	carryEdge       = bd.DiscoveredFrom
	filedChildren   = func(cp string) ([]bd.Bead, error) {
		return bd.ChildrenOfKind(cp, contract.FiledKind(contract.KindCheckpoint))
	}
)

// Confirm is the confirm sweep (`mugicrew pivot confirm`, FR39–42, ADR 0043 §8,
// ADR 0046 §5): the triage session has written the corrected Spec body with the
// human; Go files it and retires the old Checkpoint. In order, each leg guarded so
// a re-run after a partial failure converges (FR48, ADR 0003):
//
//  1. spawn the corrected Spec caused-by the old Spec's predecessor (its Chart,
//     else its Signal) and discovered-from the pivot — the re-run key — as a
//     Draft by the pivot, which the triage session's hitl-run publishes;
//  2. a fast-laned (Chart-less) Spec: pin its fresh Anchor: <sha> on the Spec bead
//     through bd.Anchor, as Luffy's fast lane does, under every land strategy (no
//     commit lands on main), so its Checkpoint cuts from that tip;
//  3. Supersede(old Spec, new Spec), then record each --carries Critique as new Spec
//     discovered-from Critique (carryCritiques), then carry the Checkpoint's Ordering
//     edges to the new Spec (ADR 0085), so its dependents wait for the corrected work;
//  4. force-close every open member, then the frozen Checkpoint, obsolete;
//  5. close each open sibling pivot 'superseded: <pivot>';
//  6. close the pivot 'confirmed' — last, so a closed pivot means a finished sweep.
//
// A closed pivot is therefore a no-op. title defaults to the old Spec's. carries are
// the open filed Critiques whose ask the session folded into body (FR 19–21); a bad
// id refuses before any write.
func Confirm(pivotID, title, body string, carries []string) error {
	cp, oldSpec, open, err := openSweep("confirm", "corrected Spec body", pivotID, body)
	if err != nil || !open {
		return err
	}
	if err := checkCarries("confirm", cp, pivotID, carries); err != nil {
		return err
	}
	newSpec, err := spawnCorrectedSpec(pivotID, cp, oldSpec, title, body)
	if err != nil {
		return err
	}
	if err := supersede(oldSpec, newSpec); err != nil {
		return err
	}
	if err := carryCritiques(newSpec, carries); err != nil {
		return err
	}
	if err := carryCheckpoint(cp, newSpec); err != nil {
		return err
	}
	if err := closeOut(cp, pivotID, bd.ObsoleteSuperseded(oldSpec, newSpec, pivotID)); err != nil {
		return err
	}
	return closeBead(pivotID, "confirmed")
}

// Escalate is the escalate sweep (`mugicrew pivot escalate`, FR43–46, ADR 0043
// §8, ADR 0046 §5–6): the Spec's problem is bigger than the Spec, so charting
// re-opens. In order, each leg guarded like Confirm's (FR48):
//
//  1. spawn a fresh container Chart caused-by the original Signal (which stays
//     closed) and discovered-from the pivot — the re-run key — its body the
//     evidence the session gathered, seeded as a wayfinder map (escalatedChartBody)
//     — a Draft by the pivot, like Confirm's Spec — and seed its Survey under it,
//     also a Draft by the pivot (Spec mc-r832 D5);
//  2. cut chart/<new> from main, as reconcileSignal does for a first Chart;
//  3. a charted Spec: Supersede(old Chart, new Chart). A fast-laned Spec skips
//     this — caused-by the Signal is the new Chart's only link (the recorded
//     decision: ADR 0046 §6 asks nothing more, and no Spec→Chart supersede edge
//     exists anywhere else in the graph);
//     then record each --carries Critique as new Chart discovered-from Critique,
//     then carry the Checkpoint's Ordering edges to the new Chart (ADR 0085);
//     4–5. the same Checkpoint, member and sibling-pivot close-out as Confirm;
//  6. close the pivot 'escalated', last.
//
// The Survey's charting session then maps the Chart with a human.
func Escalate(pivotID, evidence string, carries []string) error {
	cp, oldSpec, open, err := openSweep("escalate", "evidence", pivotID, evidence)
	if err != nil || !open {
		return err
	}
	if err := checkCarries("escalate", cp, pivotID, carries); err != nil {
		return err
	}
	oldChart, charted, err := causedBy(oldSpec, contract.KindChart)
	if err != nil {
		return err
	}
	from := oldSpec
	if charted {
		from = oldChart
	}
	sigID, ok, err := causedBy(from, contract.KindSignal)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("pivot escalate: %s has no caused-by Signal to hang the fresh Chart on", from)
	}
	sig, err := showBead(sigID)
	if err != nil {
		return err
	}
	// The fresh Chart joins the chain's Initiative, read off its Checkpoint (the
	// unclosed head adoption moves), never the closed original Signal (Spec D2).
	head, err := showBead(cp)
	if err != nil {
		return err
	}
	parent, err := initiativeOf(head)
	if err != nil {
		return err
	}
	newChart, _, err := spawnDiscovered(pivotID, sigID, bd.Successor{
		Kind: contract.KindChart, Container: true, Title: sig.Title,
		Description: escalatedChartBody(oldSpec, pivotID, evidence),
		DraftedBy:   pivotID, // published by the triage session's hitl-run, after chart/<new> is cut (Spec D11 row 14)
		Parent:      parent,
		Context:     bd.ContextOf(head),
		Repo:        bd.RepoStampOf(head),
	})
	if err != nil {
		return err
	}
	if _, _, err := spawnSurvey(newChart, sig.Title, pivotID); err != nil {
		return err
	}
	chartBranch := git.BranchName(string(contract.KindChart), newChart)
	if r := repoOf(head); r != "" {
		if err = stampCut(head, newChart, r, chartBranch); err == nil {
			_, err = repoCut(r, chartBranch, r.Mainline())
		}
	} else {
		_, err = cutBranch(chartBranch, git.Mainline())
	}
	if err != nil {
		return err
	}
	if charted {
		if err := supersede(oldChart, newChart); err != nil {
			return err
		}
	}
	if err := carryCritiques(newChart, carries); err != nil {
		return err
	}
	if err := carryCheckpoint(cp, newChart); err != nil {
		return err
	}
	if err := closeOut(cp, pivotID, bd.ObsoleteEscalated(oldSpec, newChart, pivotID)); err != nil {
		return err
	}
	return closeBead(pivotID, "escalated")
}

// escalatedChartBody seeds the fresh Chart in the wayfinder-map shape the charting
// session writes (contract.WayfinderMapFields; the test pins the headings), so it is off ChartPlaceholderBody (reconcileChart's un-charted gate)
// and promote-or-stamp promotes its Not-yet-specified fog into defog beads.
// ponytail: the evidence is one fog bullet; promote-or-stamp splits it if it must.
func escalatedChartBody(oldSpec, pivotID, evidence string) string {
	return fmt.Sprintf(`## Destination

A corrected Spec replacing %[1]s, which pivot %[2]s escalated: the Spec was wrong beyond one FR.

## Notes

Escalation evidence (pivot %[2]s):

%[3]s

## Decisions-so-far

## Not-yet-specified

- What the escalation evidence in Notes shows wrong, and what the Spec should say instead.

## Out-of-scope
`, oldSpec, pivotID, strings.TrimSpace(evidence))
}

// openSweep is both sweeps' preamble: the pivot must be a pivot and text (what)
// non-empty; a closed pivot is a finished sweep (open=false, no error). It resolves
// the pivot's Checkpoint and Spec.
func openSweep(verb, what, pivotID, text string) (cp, spec string, open bool, err error) {
	pivot, err := showBead(pivotID)
	if err != nil {
		return "", "", false, err
	}
	if pivot.Kind != contract.KindPivot {
		return "", "", false, fmt.Errorf("pivot %s: %s is kind %q, not a pivot", verb, pivotID, pivot.Kind)
	}
	if !pivot.Open() {
		return "", "", false, nil
	}
	if strings.TrimSpace(text) == "" {
		return "", "", false, fmt.Errorf("pivot %s: empty %s for %s", verb, what, pivotID)
	}
	cp, spec, err = pivotTargets(pivotID)
	return cp, spec, err == nil, err
}

// pivotTargets is the pivot's Checkpoint (reverse blocks) and Spec (related) —
// the same pair Launch resolves.
func pivotTargets(pivotID string) (cp, spec string, err error) {
	cp, ok, err := pivotCheckpoint(pivotID)
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "", "", fmt.Errorf("pivot %s blocks no Checkpoint", pivotID)
	}
	spec, ok, err = pivotSpec(pivotID)
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "", "", fmt.Errorf("pivot %s is related to no Spec", pivotID)
	}
	return cp, spec, nil
}

// spawnCorrectedSpec is legs 1–2: the new Spec caused-by the old one's
// predecessor, pinned (bd.Anchor) when that predecessor is a Signal (fast-laned).
// The Spec is a Draft by the pivot, so the loop can't slice it before its anchor
// pin is written; the triage session's hitl-run publishes it (Spec D11 rows 12–13).
func spawnCorrectedSpec(pivotID, cp, oldSpecID, title, body string) (string, error) {
	pred, charted, err := causedBy(oldSpecID, contract.KindChart)
	if err != nil {
		return "", err
	}
	if !charted {
		var ok bool
		if pred, ok, err = causedBy(oldSpecID, contract.KindSignal); err != nil {
			return "", err
		} else if !ok {
			return "", fmt.Errorf("pivot confirm: Spec %s has no caused-by Chart or Signal to hang the corrected Spec on", oldSpecID)
		}
	}
	if title == "" {
		old, err := showBead(oldSpecID)
		if err != nil {
			return "", err
		}
		title = old.Title
	}
	// Like Escalate's Chart, the Spec joins the Initiative of the chain's unclosed
	// head (the Checkpoint), never the closed Signal (Spec D2).
	head, err := showBead(cp)
	if err != nil {
		return "", err
	}
	parent, err := initiativeOf(head)
	if err != nil {
		return "", err
	}
	id, _, err := spawnDiscovered(pivotID, pred, bd.Successor{Kind: contract.KindSpec, Title: title, Description: body, DraftedBy: pivotID, Parent: parent, Context: bd.ContextOf(head), Repo: bd.RepoStampOf(head)})
	if err != nil || charted {
		return id, err
	}
	trailer := baseTrailer(contract.KindSpec, id)
	_, err = bd.Anchor(bd.AnchorSeams{Strategy: landStrategy, Show: showBead, Update: updateBody, Fetch: fetchForge, RepoFetch: repoFetch, Landed: repoLanded, Tip: repoTip}, repoOf(head), id, trailer)
	return id, err
}

// checkCarries refuses, before any write, a --carries id that is not an open filed
// child of the swept Checkpoint: its container's filed kind, with a Source: line.
// One this very pivot's close-out already closed is still accepted, so a re-run
// after a partial sweep converges.
func checkCarries(verb, cp, pivotID string, carries []string) error {
	if len(carries) == 0 {
		return nil
	}
	kids, err := filedChildren(cp)
	if err != nil {
		return err
	}
	for _, id := range carries {
		i := slices.IndexFunc(kids, func(k bd.Bead) bool { return k.ID == id })
		if i < 0 {
			return fmt.Errorf("pivot %s: --carries %s is not a filed %s child of %s", verb, id, contract.FiledKind(contract.KindCheckpoint), cp)
		}
		k := kids[i]
		if !k.Unclosed() && !strings.Contains(k.CloseReason, fmt.Sprintf("(pivot %s ", pivotID)) {
			return fmt.Errorf("pivot %s: --carries %s is already closed", verb, id)
		}
		if len(contract.SourcesOf(k.Description+"\n"+k.Notes)) == 0 {
			return fmt.Errorf("pivot %s: --carries %s has no %q line, so it is not a filed Critique", verb, id, strings.TrimSpace(contract.SourcePrefix))
		}
	}
	return nil
}

// carryCritiques records each --carries Critique as succ discovered-from Critique
// (add-if-absent, so a re-run adds no duplicate), before carryCheckpoint and closeOut:
// any Watch pass sees the mark before the Critique closes (FR 21).
func carryCritiques(succ string, carries []string) error {
	for _, id := range carries {
		if err := carryEdge(succ, id); err != nil {
			return err
		}
	}
	return nil
}

// carryCheckpoint carries the obsoleted Checkpoint's Ordering edges to its
// successor (a Draft by the pivot) before closeOut, and records the carry (ADR
// 0085). An error fails the sweep before the obsolete close; a re-run re-carries
// (add-only, so a no-op) until the pivot's close makes the sweep a no-op.
func carryCheckpoint(cp, succ string) error {
	moved, skipped, err := carry(cp, succ)
	return eventlog.RecordCarry(bd.Bead{ID: cp, Kind: contract.KindCheckpoint}, succ, moved, skipped, err)
}

// closeOut retires the Checkpoint for the sweep (ADR 0043 §8, ADR 0045 §5): every
// open member but an MR Watch, then the Checkpoint — both forced, since bd refuses to close a bead
// with an open blocker (a sibling member; the pivots themselves, and closing those
// first would unfreeze the Checkpoint mid-sweep) — then the open sibling pivots. The lead pivot is the caller's to close.
func closeOut(cp, pivotID, reason string) error {
	members, err := openChildBeads(cp)
	if err != nil {
		return err
	}
	for _, m := range members {
		if m.Kind == contract.KindMRWatch { // outlives the sweep to answer the MR's reviewers
			continue
		}
		if err := forceClose(m.ID, reason); err != nil { // a member blocked by an open sibling refuses a plain close
			return err
		}
	}
	c, err := showBead(cp)
	if err != nil {
		return err
	}
	if c.Open() {
		if err := forceClose(cp, reason); err != nil {
			return err
		}
	}
	siblings, err := openPivots(cp)
	if err != nil {
		return err
	}
	for _, s := range siblings {
		if s.ID != pivotID {
			if err := closeBead(s.ID, "superseded: "+pivotID); err != nil {
				return err
			}
		}
	}
	return nil
}
