// Package session is the shared launch-cwd wiring both HITL entry points consume:
// given a bead, where does the human's spawned session run and how is
// that tree torn down. It turns the pure route.Route decision into a
// concrete cwd + teardown by touching git — the git-bearing counterpart to the
// pure, git-free route package, kept out of route so route stays checkable with
// fake beads and no git subprocess.
//
// An in-tree kind (charting/grilling → chart/<...>) runs in a fresh per-session
// worktree of its container branch; every bead-ops leaf (signal triage, spec
// slicing) runs from the repo root. The CLI signal triage verb (runInteractive)
// wraps a foreground run in Placement and tears down unconditionally on exit. The
// TUI launches through Start instead (tmux.go): a detached tmux session whose
// in-pane hitl-run wrapper tears down only on a clean exit (ADR 0037, ADR 0039 §4–5).
package session

import (
	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/route"
)

// Placement resolves bead b's launch cwd and its teardown, per route.Route: a
// Worktree kind gets a fresh worktree of the routed container branch, a Detached
// land step one detached at its tip (teardown =
// `git worktree remove --force`, unconditional so a dirty/crashed tree still
// drops); a repo-root leaf runs from the repo root with a no-op teardown. The
// teardown is always non-nil, so a caller can defer it without a guard.
func Placement(b bd.Bead) (cwd string, teardown func(), err error) {
	cwd, _, teardown, err = placeTree(b)
	return cwd, teardown, err
}

// placeTree is Placement plus the worktree path ("" for a repo-root leaf), which the
// tmux launch hands hitl-run so a clean exit can tear exactly that tree down.
func placeTree(b bd.Bead) (cwd, worktree string, teardown func(), err error) {
	d, err := route.Route(b)
	if err != nil {
		return "", "", func() {}, err
	}
	if d.Placement != route.RepoRoot {
		wt, td, err := CutTree(b.ID, d)
		return wt, wt, td, err
	}
	root, err := git.RepoRoot()
	return root, "", func() {}, err
}

// CutTree cuts bead id's tree at its routed d, the one cut both launch paths
// share: a route.Worktree effort/<id>, cut at its container's tip or reused to
// roll forward (Spec D2), or a route.Detached land step's tree at its
// container, which it never checks out (ADR 0102 §8).
func CutTree(id string, d route.Destination) (path string, teardown func(), err error) {
	if d.Placement == route.Detached {
		return git.NewDetachedWorktree(d.Branch)
	}
	return git.NewEffortWorktree(id, d.Container, d.Tip)
}
