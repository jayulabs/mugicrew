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
	"path/filepath"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/contexts"
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
// container, which it never checks out (ADR 0102 §8). Under a repo = container the
// effort tree is a Workspace host with only the child materialized in it (Spec D4).
func CutTree(id string, d route.Destination) (path string, teardown func(), err error) {
	// the declared-repo gate comes first: a no-context launch reads one config pair
	// and no bead (ADR 0157)
	repos := declaredRepos(".")
	if d.Placement == route.Detached {
		if len(repos) > 0 {
			_, cid := git.ParseBranchName(d.Branch)
			c, err := showBead(cid)
			if err != nil {
				return "", nil, err
			}
			if r, dir := contexts.RepoDirOf(c); r != "" {
				return cutNestedLand(d.Branch, r, dir)
			}
		}
		return git.NewDetachedWorktree(d.Branch)
	}
	if len(repos) > 0 && d.Container != "" {
		_, cid := git.ParseBranchName(d.Container)
		c, err := showBead(cid)
		if err != nil {
			return "", nil, err
		}
		if r, dir := contexts.RepoDirOf(c); r != "" {
			return cutNested(id, d, r, dir)
		}
	}
	return git.NewEffortWorktree(id, d.Container, d.Tip)
}

// declaredRepos is the repo = table, a seam so a test counts who reads config.
var declaredRepos = cfglayer.Repos

// cutNestedLand is the land tree of a repo = container (Spec D4): the host detached at
// Workspace mainline, plus the child detached at branch's tip at <host>/<dir>, where
// Compose runs. Teardown is child first, host second.
// It skips RequireIgnored: the child was already cut and ignored when the effort began.
func cutNestedLand(branch string, r git.Repo, dir string) (string, func(), error) {
	host, hostDown, err := git.NewDetachedWorktree(git.Mainline())
	if err != nil {
		return "", nil, err
	}
	childDown, err := r.MaterializeDetached(filepath.Join(host, dir), branch)
	if err != nil {
		hostDown()
		return "", nil, err
	}
	return host, func() { childDown(); hostDown() }, nil
}

// cutNested is CutTree under a repo = container: the host detached at Workspace
// mainline, then the child materialized at <host>/<dir>. Teardown is child first,
// host second; a failed materialize brings the host down.
func cutNested(id string, d route.Destination, r git.Repo, dir string) (string, func(), error) {
	if err := git.RequireIgnored(dir); err != nil {
		return "", nil, err
	}
	host, hostDown, err := git.NewDetachedWorktree(git.Mainline())
	if err != nil {
		return "", nil, err
	}
	childDown, err := r.Materialize(filepath.Join(host, dir), id, d.Container, d.Tip)
	if err != nil {
		hostDown()
		return "", nil, err
	}
	return host, func() { childDown(); hostDown() }, nil
}
