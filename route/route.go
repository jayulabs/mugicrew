// Package route is the pure per-kind session→tree-destination decision:
// given a bead's kind and its membership edge, where does its spawned
// session run — inside a worktree of a container branch, or from the repo root.
// The discriminator is operate-inside-the-tree, not touch-the-branch (FR7).
//
// It is the shared seam the AFK-loop reshape and the HITL wiring
// both consume — both launch paths share this one decision. Pure and
// shells out to no git — it reads only the bead's kind, id, and parent — so the
// routing is unit-testable with fake beads and no git subprocess.
package route

import (
	"fmt"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// Placement is where a session's cwd lands (the Spec per-kind table).
type Placement int

const (
	// RepoRoot: bead-ops-only leaves that never enter a container tree — signal
	// triage and spec slicing — run from the repo root, no worktree.
	RepoRoot Placement = iota
	// Worktree: the session operates inside a working tree; Branch names the
	// checkout, its own effort/<id> (Spec D1).
	Worktree
	// Detached: a land step (Chart land, Checkpoint land) runs in a tree with HEAD
	// detached at the tip of Branch, its own container, which no tree checks out:
	// Land never writes the container (Spec D1/D2, ADR 0102 §8).
	Detached
)

// Mode is how an effort integrates onto its container (Spec D1, ADR 0102).
type Mode int

const (
	// Squash: one squash commit on the container tip, the default.
	Squash Mode = iota
	// Replace: a history-rewriting leaf (chart-conflict, conflict) publishes its
	// rewritten container with a replace-CAS, no squash.
	Replace
)

// Destination is the pure routing decision. For Worktree, Branch is the effort/<id>
// to check out (for Detached, the container whose tip the tree is cut at), and Ref
// (prototype only) the side ref its throwaway lands on; for RepoRoot both are
// empty. Container is the branch a committing session integrates onto and Mode how
// (mugicrew integrate); "" for a land step or RepoRoot. Tip (replace mode only) is
// the container's pinned Tip the failed land reported: the effort is cut there
// (while the container still descends from it) and the replace-CAS expects it (""
// when the bead carries no Tip: pin).
type Destination struct {
	Placement Placement
	Branch    string
	Ref       string
	Container string
	Mode      Mode
	Tip       string
}

// Route decides where bead b's session runs, per the authoritative per-kind table
// in the Spec. Pure: it reads only b.Kind, b.ID, b.Parent (the membership
// edge, FR6) and a replace leaf's Tip: pin off b.Description — no git, no bd — so
// both launch paths share one decision and it is checkable with fake beads.
//
// ponytail: only the wired kinds are enumerated.
func Route(b bd.Bead) (Destination, error) {
	switch b.Kind {
	case contract.KindChart:
		// The Chart's own committing session works on effort/<chart-id> and integrates
		// onto its OWN Chart branch, which no tree checks out (Spec D1, FR 4). A
		// defogged Chart's effort is its land (then the bd-only speccing session): a land step.
		c := git.BranchName(string(contract.KindChart), b.ID)
		if b.HasLabel(contract.LabelDefogged) {
			return Destination{Placement: Detached, Branch: c}, nil
		}
		return Destination{Placement: Worktree, Branch: git.EffortBranch(b.ID), Container: c}, nil
	case contract.KindCheckpoint:
		// Jinbe lands the Checkpoint from a tree detached at its OWN checkpoint tip,
		// mirroring the Chart-land arm (ADR 0102 §8).
		return Destination{Placement: Detached, Branch: git.BranchName(string(contract.KindCheckpoint), b.ID)}, nil
	case contract.KindBuild, contract.KindCritique, contract.KindConflict, contract.KindSurgery:
		// Franky's leaves (a Surgery included, ADR 0092) integrate onto their PARENT
		// Checkpoint, checkpoint/<parent-id> (route arm), each from its own
		// effort/<id> (Spec D1, ADR 0102). Same missing-edge guard as the
		// defog leaves: a missing membership edge is malformed input, NOT contention,
		// so fail loud rather than derive branch "checkpoint/".
		if b.Parent == "" {
			return Destination{}, errNoParent(b)
		}
		return leaf(b, git.BranchName(string(contract.KindCheckpoint), b.Parent)), nil
	case contract.KindGrilling, contract.KindResearch, contract.KindPrototype, contract.KindChartConflict:
		// Defog leaves (and the chart-conflict that fixes the Chart branch for its
		// re-land, ADR 0071) integrate onto their PARENT Chart, so their ADR/CONTEXT/docs
		// commits reach chart/<parent-id> (FR2/FR3/FR6); each commits on its own
		// effort/<id> (Spec D1). The Chart-body edit is chart append|drop's
		// lock, not a branch hold. A prototype's throwaway
		// is built in a detached tree and lands on Ref, never on chart/<id> (FR 36).
		// A missing membership edge is malformed input, NOT contention: fail loud here
		// so it never derives branch "chart/", whose worktree-add git rejects
		// (exit 128) as an invalid ref, failing every tick (ADR 0056).
		if b.Parent == "" {
			return Destination{}, errNoParent(b)
		}
		d := leaf(b, git.BranchName(string(contract.KindChart), b.Parent))
		if b.Kind == contract.KindPrototype {
			d.Ref = prototypeRef(b.Parent)
		}
		return d, nil
	default:
		// signal triage, spec slicing — bead ops plus non-checkout git only — run
		// from the repo root (FR5).
		return Destination{Placement: RepoRoot}, nil
	}
}

// OnEffort routes b and reports whether its session works on its own effort/<id>
// (a Worktree placement), so callers skip a repo-root or detached (land step)
// route. A route error is never on an effort.
func OnEffort(b bd.Bead) (Destination, bool, error) {
	d, err := Route(b)
	return d, err == nil && d.Placement == Worktree, err
}

// leaf is a leaf's effort/<id> worktree integrating onto container c: squash, or
// for the history-rewriting leaves (chart-conflict, conflict) replace, cut at the
// pinned Tip their body carries (Spec D1, ADR 0102 §4).
func leaf(b bd.Bead, c string) Destination {
	d := Destination{Placement: Worktree, Branch: git.EffortBranch(b.ID), Container: c}
	if b.Kind == contract.KindChartConflict || b.Kind == contract.KindConflict {
		d.Mode = Replace
		d.Tip, _ = git.ParseTip(b.Description)
	}
	return d
}

// errNoParent is the hard error for a defog leaf missing its parent membership
// edge (FR6): it cannot resolve a container Chart, so routing fails loud instead
// of deriving an empty-parent branch that git rejects (exit 128) on every tick.
func errNoParent(b bd.Bead) error {
	return fmt.Errorf("route: %s bead %q has no parent membership edge; cannot resolve container chart", b.Kind, b.ID)
}

// prototypeRef is the side ref a prototype commits to, keyed on its parent Chart id
// (ADR 0009: refs/mugicrew/prototypes/<chart-id>) — reachable for lineage, never merged.
func prototypeRef(chartID string) string { return "refs/mugicrew/prototypes/" + chartID }
