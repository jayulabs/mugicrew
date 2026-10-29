package loop

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/liveness"
	"jayulabs.org/mugicrew/route"
)

// The orphan reading's seams, vars so tests script the worktree listing and the
// process table without a repo or /proc.
var (
	effortTrees = trees // the branch → tree map, and false when a listing errored
	readOrphans = liveness.Orphans
	repoDirs    = contexts.Repos
	ownLineage  = func() []int { return liveness.Lineage(os.Getpid()) }
)

// Orphans is the shared reading of an Orphaned claim (ADR 0391) over beads: the
// id of every active AFK effort (IsActiveAFKEffort; HITL claims and other beads
// are left out) mapped to true when it is orphaned. Trees come from one git
// worktree listing per repo (the cwd's and each declared repo dir's), not one
// lookup per bead; a repo-root bead has no tree and skips the cwd check. The
// caller's own lineage is excluded. Observe-only: nothing is written.
func Orphans(beads []bd.Bead) map[string]bool {
	var efforts []liveness.Effort
	var forTrees []string
	unknown := map[string]bool{} // tree-route beads whose tree a failed listing hid
	for _, b := range beads {
		if !IsActiveAFKEffort(b) {
			continue
		}
		efforts = append(efforts, liveness.Effort{ID: b.ID})
		if route.HasTree(b) {
			forTrees = append(forTrees, b.ID)
		}
	}
	if len(forTrees) > 0 {
		byBranch, complete := effortTrees()
		for i, e := range efforts {
			if slices.Contains(forTrees, e.ID) {
				efforts[i].Tree = byBranch[git.EffortBranch(e.ID)]
				if efforts[i].Tree == "" && !complete {
					unknown[e.ID] = true
				}
			}
		}
	}
	out := map[string]bool{}
	maps.Copy(out, readOrphans(efforts, ownLineage()))
	for id := range unknown {
		out[id] = false
	}
	return out
}

// trees maps each branch to the tree holding it, from one worktree listing (plus
// the trees stopped mid-rebase, whose HEAD is detached) of the cwd repo and each
// declared repo dir, as git.HolderAcross searches them. A listing that errors
// contributes nothing and reports false: a read error is never taken to mean no
// holder, so Orphans reads a tree-route bead it found no tree for as not orphaned.
func trees() (map[string]string, bool) {
	out := map[string]string{}
	complete := true
	// note records any listing error: a read error is never taken to mean no holder.
	note := func(err error) {
		if err != nil {
			complete = false
		}
	}
	add := func(r git.Repo, dir string) {
		ws, err := r.Worktrees()
		note(err)
		for _, w := range ws {
			if w.Branch != "" {
				if _, ok := out[w.Branch]; !ok {
					out[w.Branch] = hostTree(w.Path, dir)
				}
			}
		}
		rb, err := r.Rebasing()
		note(err)
		for path, branch := range rb {
			if _, ok := out[branch]; !ok {
				out[branch] = hostTree(path, dir)
			}
		}
	}
	add("", "")
	if repos := repoDirs(); len(repos) > 0 {
		root, err := git.RepoRoot()
		note(err)
		if err == nil {
			for _, dir := range slices.Compact(slices.Sorted(maps.Values(repos))) {
				if r := filepath.Join(root, dir); git.IsRepoRoot(r) {
					add(git.Repo(r), dir)
				}
			}
		}
	}
	return out, complete
}

// hostTree is the tree to scan for a worktree path found in the declared repo
// dir: the host, the path with the trailing /dir stripped, as git.HolderAcross
// hands back (ADR 0169), so the scan matches what recover's Take scans. A path
// from the cwd repo (dir "") is its own tree.
func hostTree(path, dir string) string {
	if dir == "" {
		return path
	}
	return strings.TrimSuffix(path, string(filepath.Separator)+filepath.Clean(dir))
}

// StubTrees swaps the worktree listing Orphans reads for one that reports a
// complete answer, so tests outside this package don't depend on the ambient
// repo. The returned func restores it.
func StubTrees(byBranch map[string]string) (restore func()) {
	old := effortTrees
	effortTrees = func() (map[string]string, bool) { return byBranch, true }
	return func() { effortTrees = old }
}
