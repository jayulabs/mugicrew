// Package remnant runs a Remnant's exits (ADR D4 of the Remnant Spec): drop and
// offshoot. Both refuse unless the bead is an open Remnant with no open blocker,
// first sweep the landed Checkpoint's still-unstarted children onto the Remnant, and
// close the Remnant with the exit's outcome.
package remnant

import (
	"fmt"
	"slices"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// Seams are the graph and git reads and writes of an exit, a field set so tests fake them.
type Seams struct {
	Show          func(id string) (bd.Bead, error)
	Landed        func(remnant string) (string, bool, error)
	OpenBlockers  func(id string) ([]bd.Bead, error)
	OpenChildren  func(id string) ([]bd.Bead, error)
	OpenPivots    func(checkpoint string) ([]bd.Bead, error)
	AppendLines   func(id string, lines ...string) error
	ForceCloseAll func(ids []string, reason string) error
	Close         func(id, reason string) error
	Block         func(remnant, child string) error
	Renumbers     func(landed string) ([]bd.Bead, error)
	Moves         func(sha string) ([]string, error)
	NewOffshoot   func(title, body, remnant string) (string, error)
	FindOffshoot  func(remnant, title string) (string, bool, error)
	HasBranch     func(name string) (bool, error)
	DeleteBranch  func(name string) error
	Kept          func(b bd.Bead) (string, error)
	HasRespin     func(remnant string) (bool, error)
}

// Real is the Seams over bd and the cwd's repo.
var Real = Seams{
	Show:          bd.Show,
	Landed:        bd.RemnantLanded,
	OpenBlockers:  bd.OpenBlockers,
	OpenChildren:  bd.OpenChildBeads,
	OpenPivots:    bd.OpenPivots,
	AppendLines:   bd.AppendLines,
	ForceCloseAll: bd.ForceCloseAll,
	Close:         bd.Close,
	Block:         bd.Blocks,
	Renumbers:     bd.RenumberCheckpointsOf,
	Moves:         git.Repo("").Moves,
	NewOffshoot:   bd.NewRemnantOffshoot,
	FindOffshoot:  bd.RemnantOffshootTitled,
	HasBranch: func(name string) (bool, error) {
		_, err := git.Head("refs/heads/" + name)
		if git.MissingRef(err) {
			return false, nil
		}
		return err == nil, err
	},
	DeleteBranch: git.DeleteBranch,
	Kept:         KeptLiner(git.Head),
	HasRespin: func(r string) (bool, error) {
		_, ok, err := bd.SuccessorOf(r, contract.KindCheckpoint)
		return ok, err
	},
}

// Result is what an exit did, for the caller to print.
type Result struct {
	Swept   []string // children closed onto the Remnant just before the exit
	Deleted []string // drop: refs deleted
	Errands []string // before-land Errands closed
	Signal  string   // offshoot: the Signal's id
}

// StrandedChildren is the landed container's open children bar the Landings, MR
// Watches and Approvals awaiting undraft (closeLanded closes those itself).
func StrandedChildren(open func(string) ([]bd.Bead, error), cpID string) ([]bd.Bead, error) {
	kids, err := open(cpID)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(kids, func(k bd.Bead) bool {
		return k.Kind == contract.KindLanding || k.Kind == contract.KindMRWatch || (k.Kind == contract.KindApproval && bd.AwaitingUndraft(k))
	}), nil
}

// draftMark ends a Child: line whose child was an unpublished Draft (status deferred)
// when it was swept; respin skips such a child (FR 45).
const draftMark = " [draft]"

// ChildLine is the Remnant body's one line for a stranded child.
func ChildLine(k bd.Bead, reason string) string {
	l := bd.FormatChildLine(fmt.Sprintf("%s %s %s — %s", k.ID, k.Kind, k.Title, reason))
	if k.Status == "deferred" {
		l += draftMark
	}
	return l
}

// Running reports whether a child is being worked, so it is the Remnant's blocker, not
// its to sweep. A paused child counts: dispatch unclaims it when its effort ends, but
// its effort/<id> is kept and must be listed and adopted, not swept as unstarted.
func Running(k bd.Bead) bool {
	return k.Status == "in_progress" || k.Assignee != "" || bd.Paused(k)
}

// take refuses unless id is an open Remnant with no open blocker, then sweeps the
// landed Checkpoint's still-unstarted children onto it. A child running (or paused)
// since the Remnant was filed gains the hold fileRemnant gives its early starters, as does a
// pivot opened on the landed Checkpoint since, and either refuses the exit until it
// closes. It returns the Remnant as
// re-read after the sweep, and the landed Checkpoint.
func take(s Seams, id string) (rem, landed bd.Bead, swept []string, err error) {
	if rem, err = s.Show(id); err != nil {
		return
	}
	if rem.Kind != contract.KindRemnant {
		return rem, landed, nil, fmt.Errorf("%s is not a Remnant", rem.Title)
	}
	if !rem.Open() {
		return rem, landed, nil, fmt.Errorf("Remnant %s is not open", rem.Title)
	}
	blockers, err := s.OpenBlockers(rem.ID)
	if err != nil {
		return
	}
	if len(blockers) > 0 {
		return rem, landed, nil, fmt.Errorf("Remnant %s is blocked by %s", rem.Title, blockers[0].Title)
	}
	cpID, ok, err := s.Landed(rem.ID)
	if err != nil {
		return
	}
	if !ok {
		return rem, landed, nil, fmt.Errorf("Remnant %s is related to no Checkpoint", rem.Title)
	}
	if landed, err = s.Show(cpID); err != nil {
		return
	}
	kids, err := StrandedChildren(s.OpenChildren, cpID)
	if err != nil {
		return
	}
	var held *bd.Bead
	for _, k := range kids {
		if !Running(k) {
			continue
		}
		if err = s.Block(rem.ID, k.ID); err != nil {
			return
		}
		if held == nil {
			held = &k
		}
	}
	pivots, err := s.OpenPivots(cpID)
	if err != nil {
		return
	}
	for _, p := range pivots {
		if err = s.Block(rem.ID, p.ID); err != nil {
			return
		}
		if held == nil {
			held = &p
		}
	}
	swept, err = SweepUnstarted(s.AppendLines, s.ForceCloseAll, s.Kept, rem.ID, kids)
	if err != nil {
		return
	}
	if held != nil {
		return rem, landed, swept, fmt.Errorf("Remnant %s is blocked by %s", rem.Title, held.Title)
	}
	rem, err = s.Show(id)
	return
}

// closeErrands closes each carried before-land Errand still unclosed.
func closeErrands(s Seams, rem bd.Bead, why string) ([]string, error) {
	var done []string
	for _, id := range bd.ParseRemnantBody(rem.Description).ErrandIDs() {
		e, err := s.Show(id)
		if err != nil {
			return done, err
		}
		if !e.Unclosed() {
			continue
		}
		if err := s.Close(id, bd.ObsoleteReason+" "+why+" by "+rem.ID); err != nil {
			return done, err
		}
		done = append(done, id)
	}
	return done, nil
}

// refuseRespin refuses an exit other than respin while the Remnant holds a respin
// Draft a crashed respin left: the exit would close the Remnant and strand the Draft,
// which respin can no longer reach. Run before take, so a refusal sweeps nothing.
func refuseRespin(s Seams, id string) error {
	ok, err := s.HasRespin(id)
	if err != nil {
		return err
	}
	if ok {
		return fmt.Errorf("Remnant %s holds a respin Checkpoint a crashed respin left; run mugicrew remnant %s respin to finish it", id, id)
	}
	return nil
}

// Drop deletes checkpoint/<landed> and each Kept: effort branch with its cut ref,
// closes a carried before-land Errand obsolete: dropped by the Remnant, re-files
// nothing, and closes the Remnant dropped. Exactly the listed refs go.
func Drop(s Seams, id string) (Result, error) {
	if err := refuseRespin(s, id); err != nil {
		return Result{}, err
	}
	rem, landed, swept, err := take(s, id)
	res := Result{Swept: swept}
	if err != nil {
		return res, err
	}
	refs := append([]string{git.BranchName(string(contract.KindCheckpoint), landed.ID)}, bd.ParseRemnantBody(rem.Description).KeptBranches()...)
	for _, ref := range refs {
		ok, err := s.HasBranch(ref)
		if err != nil {
			return res, err
		}
		if !ok {
			continue
		}
		if err := s.DeleteBranch(ref); err != nil {
			return res, fmt.Errorf("delete %s: %w", ref, err)
		}
		res.Deleted = append(res.Deleted, ref)
	}
	if res.Errands, err = closeErrands(s, rem, "dropped"); err != nil {
		return res, err
	}
	return res, s.Close(rem.ID, "dropped")
}

// Offshoot files one offshoot Signal listing every Child: with its kept ref, the
// Stranded: range and any Renumber Checkpoint's title and moves; closes a carried
// before-land Errand obsolete: offshoot by the Remnant; keeps every ref; closes the
// Remnant offshoot: <signal id>. A re-run after a crash that filed the Signal but did
// not close the Remnant reuses the Signal it finds: the close is the commit point.
func Offshoot(s Seams, id string) (Result, error) {
	if err := refuseRespin(s, id); err != nil {
		return Result{}, err
	}
	rem, landed, swept, err := take(s, id)
	res := Result{Swept: swept}
	if err != nil {
		return res, err
	}
	renumbers, err := s.Renumbers(landed.ID)
	if err != nil {
		return res, err
	}
	body, err := offshootBody(s, rem, landed, renumbers)
	if err != nil {
		return res, err
	}
	title := landed.Title + ": work stranded when it landed early"
	found, ok, err := s.FindOffshoot(rem.ID, title)
	if err != nil {
		return res, err
	}
	if ok {
		res.Signal = found
	} else if res.Signal, err = s.NewOffshoot(title, body, rem.ID); err != nil {
		return res, err
	}
	if res.Errands, err = closeErrands(s, rem, "offshoot"); err != nil {
		return res, err
	}
	return res, s.Close(rem.ID, "offshoot: "+res.Signal)
}

// offshootBody lists the stranded work in the Remnant's own words, one fact a line.
func offshootBody(s Seams, rem, landed bd.Bead, renumbers []bd.Bead) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s was merged on the Forge while work on it was in flight; the work below is not on mainline. Refs are kept.\n", landed.Title)
	rb := bd.ParseRemnantBody(rem.Description)
	if tip := rb.LandedTip(); tip != "" {
		b.WriteString(strings.Join(bd.FormatTipLines(tip, rb.StrandedLine()), "\n") + "\n")
	} else if l := rb.StrandedLine(); l != "" {
		b.WriteString(l + "\n")
	}
	for _, c := range rb.Children() {
		b.WriteString(bd.FormatChildLine(c.Value) + "\n")
		if k, ok := rb.KeptFor(git.EffortBranch(c.ID)); ok {
			b.WriteString(bd.FormatKept(k.Value) + "\n")
		}
	}
	for _, c := range renumbers {
		fmt.Fprintf(&b, "Renumber Checkpoint: %s\n", c.Title)
		// A landed Renumber's moves are read off its squash; one still open has only
		// planned clashes, its moves not decided yet.
		moves, isLanded, err := landedMoves(s.Moves, c)
		if err != nil {
			return "", err
		}
		if isLanded {
			for _, m := range moves {
				fmt.Fprintf(&b, "Move: %s\n", m)
			}
			continue
		}
		for _, l := range strings.Split(c.Description, "\n") {
			if strings.HasPrefix(l, "- ") {
				fmt.Fprintf(&b, "Planned: %s\n", strings.TrimPrefix(l, "- "))
			}
		}
	}
	if len(renumbers) > 0 {
		b.WriteString("The stranded range cites pre-move numbers.\n")
	}
	return b.String(), nil
}

// landedMoves is the moves a landed Renumber Checkpoint's squash made, and whether it
// landed at all: an unlanded one has no moves to read.
func landedMoves(moves func(sha string) ([]string, error), renumber bd.Bead) ([]string, bool, error) {
	sha, ok := strings.CutPrefix(renumber.CloseReason, "landed: ")
	if !ok {
		return nil, false, nil
	}
	m, err := moves(bd.FirstWord(sha))
	return m, true, err
}

// KeptLine is b's "Kept: effort/<id> <cut>..<head>" Remnant line, from its cut ref so
// it matches what Integrate would replay. An effort cut before the cut ref existed
// still lists its branch as "Kept: effort/<id> <head> (no cut ref)", so drop and offshoot
// see it and respin falls back to a cherry-pick note; "" only when the branch is gone
// too. head reads a ref's sha.
func KeptLine(head func(ref string) (string, error), b bd.Bead) (string, error) {
	branch := git.EffortBranch(b.ID)
	cut, err := head(git.CutRef(b.ID))
	noCut := git.MissingRef(err)
	if err != nil && !noCut {
		return "", err
	}
	tip, err := head("refs/heads/" + branch)
	if noCut && git.MissingRef(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if noCut {
		return bd.FormatKeptNoCutLine(branch, tip), nil
	}
	return bd.FormatKeptLine(branch, cut, tip), nil
}

// KeptLiner is KeptLine bound to a head reader, for the seams that each carry one.
func KeptLiner(head func(ref string) (string, error)) func(bd.Bead) (string, error) {
	return func(b bd.Bead) (string, error) { return KeptLine(head, b) }
}

// ChildLines is the lines to append for a stranded child: its Kept: line first when kept
// yields one, then its Child: line.
func ChildLines(k bd.Bead, reason string, kept func(bd.Bead) (string, error)) ([]string, error) {
	lines := []string{ChildLine(k, reason)}
	kl, err := kept(k)
	if err != nil {
		return nil, err
	}
	if kl != "" {
		lines = append([]string{kl}, lines...)
	}
	return lines, nil
}

// SweepUnstarted lists each not-running kid on the Remnant, its Kept: line first when
// it holds a kept effort branch (kept reads it), and then force-closes them all
// obsolete: container landed; carried by <remnant>, returning their ids. Listing
// comes first, so a crash re-lists nothing new.
func SweepUnstarted(appendLines func(id string, lines ...string) error, forceCloseAll func(ids []string, reason string) error, kept func(bd.Bead) (string, error), remnantID string, kids []bd.Bead) ([]string, error) {
	reason := bd.CarriedByReason(remnantID)
	var swept []string
	for _, k := range kids {
		if Running(k) {
			continue
		}
		lines, err := ChildLines(k, reason, kept)
		if err != nil {
			return swept, err
		}
		if err := appendLines(remnantID, lines...); err != nil {
			return swept, err
		}
		swept = append(swept, k.ID)
	}
	if len(swept) == 0 {
		return nil, nil
	}
	return swept, forceCloseAll(swept, reason)
}
