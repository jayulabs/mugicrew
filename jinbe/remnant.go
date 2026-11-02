package jinbe

import (
	"fmt"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/remnant"
)

// Seams of the Remnant leg (fileRemnant).
var (
	remnantOf     = bd.RemnantOf
	spawnRemnant  = bd.SpawnRemnantIfAbsent
	appendLines   = bd.AppendLines
	openChildren  = bd.OpenChildBeads
	forceCloseAll = bd.ForceCloseAll
	blockOn       = bd.Blocks
	keptLine      = remnant.KeptLiner(func(ref string) (string, error) { return branchHead(ref) }) // late-bound, so tests can stub branchHead
)

// RemnantTitle is the title of the Remnant filed for the landed container cp.
func RemnantTitle(cp bd.Bead) string { return "Remnant of " + cp.Title }

// fileRemnant files the one Remnant of a gitlab Checkpoint that landed with work
// stranded. Stranded is the branch head past the landed squash's Tip:, an open pivot,
// or an open child other than a Landing, MR Watch or AwaitingUndraft Approval; with
// neither (and no Remnant yet) it files nothing, so an on-time land is untouched. Each leg is idempotent, in an order a crash converges
// from: spawn the Remnant, list then force-close each unstarted child, hold the
// Remnant on each running child and open pivot, record a before-land Errand.
func fileRemnant(cp bd.Bead, branch, sha string, landing bd.Bead, hasLanding bool, lg legs) error {
	if lg.repo != "" { // a nested container keeps its per-child Signals (Out of Scope: nested respins)
		return nil
	}
	rem, have, err := remnantOf(cp.ID)
	if err != nil {
		return err
	}
	rng, err := strandedRange(branch, sha, lg)
	if err != nil {
		return err
	}
	kids, err := strandedChildren(cp)
	if err != nil {
		return err
	}
	pivots, err := openPivots(cp.ID)
	if err != nil {
		return err
	}
	held, err := openBlockers(cp.ID)
	if err != nil {
		return err
	}
	var errands []bd.Bead // before-land Errands; Hold Errands keep clearing through ClearHolds
	for _, e := range held {
		if _, hold := holdKeyLine(e.Description); e.Kind == contract.KindErrand && !hold {
			errands = append(errands, e)
		}
	}
	if !have && rng.line == "" && len(kids) == 0 && len(pivots) == 0 && len(errands) == 0 { // an open pivot or before-land Errand is stranded work too
		return nil
	}
	if !have {
		raiser := ""
		if hasLanding {
			raiser = landing.ID
		}
		if rem, _, err = spawnRemnant(cp, RemnantTitle(cp), remnantHeader(cp, sha, rng), raiser); err != nil {
			return err
		}
	}
	for _, k := range kids {
		if remnant.Running(k) { // running: it holds the Remnant until it closes
			if err := blockOn(rem.ID, k.ID); err != nil {
				return err
			}
		}
	}
	if _, err := remnant.SweepUnstarted(appendLines, forceCloseAll, keptLine, rem.ID, kids); err != nil {
		return err
	}
	for _, p := range pivots {
		if err := blockOn(rem.ID, p.ID); err != nil {
			return err
		}
	}
	for _, e := range errands {
		if err := appendLines(rem.ID, fmt.Sprintf("Errand: %s %s (a before-land hold: an exit carries or closes it)", e.ID, e.Title)); err != nil {
			return err
		}
	}
	return nil
}

// strandedRangeInfo is the landed Tip: and the body line naming what the branch holds
// past it ("" when nothing, or the branch is gone).
type strandedRangeInfo struct{ tip, line string }

// strandedRange reads the landed squash's Tip: against the container branch's head.
// No Tip: records the range unknown (only once something else stranded: it files
// nothing alone); a Tip: that is not an ancestor of the head means the branch was
// rewritten after the pin, so the body points at git range-diff.
func strandedRange(branch, sha string, lg legs) (strandedRangeInfo, error) {
	tip, ok := landedTip(lg.repo, sha)
	if !ok {
		return strandedRangeInfo{}, nil
	}
	h, err := lg.head("refs/heads/" + branch)
	if git.MissingRef(err) || (err == nil && h == tip) {
		return strandedRangeInfo{tip: tip}, nil
	}
	if err != nil {
		return strandedRangeInfo{}, err
	}
	if contexts.AncestorIn(lg.repo, isAncestor)(tip, h) {
		return strandedRangeInfo{tip, fmt.Sprintf("Stranded: %s..%s", tip, h)}, nil
	}
	return strandedRangeInfo{tip, fmt.Sprintf("Stranded: branch rewritten after the landed pin; see git range-diff %s...%s", tip, h)}, nil
}

// strandedChildren is cp's open children bar the Landings, MR Watches and Approvals
// awaiting undraft (closeLanded closes those itself).
func strandedChildren(cp bd.Bead) ([]bd.Bead, error) {
	return remnant.StrandedChildren(openChildren, cp.ID)
}

// remnantHeader is the Remnant's first lines: which Checkpoint landed where and at
// what Tip:, then the stranded range (unknown when the squash carries no Tip:).
func remnantHeader(cp bd.Bead, sha string, rng strandedRangeInfo) string {
	tip, line := rng.tip, rng.line
	if tip == "" {
		tip, line = "unknown", "Stranded: unknown (the landed squash carries no Tip:)"
	}
	return bd.FormatRemnantHeader(cp.ID, sha, tip, line)
}
