package loop

import (
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/liveness"
)

// Orphans hands liveness the active AFK efforts only, each with the tree of its
// effort branch from one listing, and a repo-root bead with none.
func TestOrphansReading(t *testing.T) {
	oT, oR, oL := effortTrees, readOrphans, ownLineage
	t.Cleanup(func() { effortTrees, readOrphans, ownLineage = oT, oR, oL })
	listings := 0
	effortTrees = func() (map[string]string, bool) {
		listings++
		return map[string]string{"effort/mc-b": "/wt/mc-b"}, true
	}
	ownLineage = func() []int { return []int{7} }
	var gotEfforts []liveness.Effort
	var gotExclude []int
	readOrphans = func(e []liveness.Effort, ex []int) map[string]bool {
		gotEfforts, gotExclude = e, ex
		return map[string]bool{"mc-b": true}
	}
	got := Orphans([]bd.Bead{
		{ID: "mc-b", Kind: contract.KindBuild, Status: "in_progress", Parent: "ws-cp"},
		{ID: "mc-s", Kind: contract.KindSpec, Status: "in_progress"},            // repo-root
		{ID: "ws-sig", Kind: contract.KindSignal, Status: "in_progress"},        // HITL
		{ID: "mc-o", Kind: contract.KindBuild, Status: "open", Parent: "ws-cp"}, // not in progress
	})
	if !got["mc-b"] {
		t.Errorf("reading = %v", got)
	}
	want := []liveness.Effort{{ID: "mc-b", Tree: "/wt/mc-b"}, {ID: "mc-s"}}
	if len(gotEfforts) != 2 || gotEfforts[0] != want[0] || gotEfforts[1] != want[1] {
		t.Errorf("efforts = %v, want %v", gotEfforts, want)
	}
	if len(gotExclude) != 1 || gotExclude[0] != 7 {
		t.Errorf("exclude = %v", gotExclude)
	}
	if listings != 1 {
		t.Errorf("worktree listings = %d, want 1", listings)
	}
}

func TestHostTree(t *testing.T) {
	for _, c := range []struct{ path, dir, want string }{
		{"/w/host", "", "/w/host"},
		{"/w/host/child", "child", "/w/host"},
		{"/w/host/libs/child", "libs/child", "/w/host"},
		{"/w/other", "child", "/w/other"},
	} {
		if got := hostTree(c.path, c.dir); got != c.want {
			t.Errorf("hostTree(%q,%q)=%q want %q", c.path, c.dir, got, c.want)
		}
	}
}

// A failed worktree listing is not "no tree": a tree-route bead it left without
// a tree reads not orphaned whatever liveness says, while one with a tree and a
// repo-root bead keep liveness's reading.
func TestOrphansListingErrorReadsNotOrphaned(t *testing.T) {
	oT, oR, oL := effortTrees, readOrphans, ownLineage
	t.Cleanup(func() { effortTrees, readOrphans, ownLineage = oT, oR, oL })
	effortTrees = func() (map[string]string, bool) {
		return map[string]string{"effort/mc-b": "/wt/mc-b"}, false
	}
	ownLineage = func() []int { return nil }
	readOrphans = func(e []liveness.Effort, _ []int) map[string]bool {
		return map[string]bool{"mc-b": true, "mc-c": true, "mc-s": true}
	}
	got := Orphans([]bd.Bead{
		{ID: "mc-b", Kind: contract.KindBuild, Status: "in_progress", Parent: "ws-cp"},
		{ID: "mc-c", Kind: contract.KindBuild, Status: "in_progress", Parent: "ws-cp"}, // no tree found
		{ID: "mc-s", Kind: contract.KindSpec, Status: "in_progress"},
	})
	if !got["mc-b"] || got["mc-c"] || !got["mc-s"] {
		t.Errorf("reading = %v, want mc-b and mc-s orphaned, mc-c not", got)
	}
}

// A declared repo whose root can't be resolved can't be listed, so the listing is
// incomplete rather than silently reading its trees as absent.
func TestTreesIncompleteWhenRepoRootFails(t *testing.T) {
	old := repoDirs
	t.Cleanup(func() { repoDirs = old })
	repoDirs = func() map[string]string { return map[string]string{"ctx": "sub"} }
	t.Chdir(t.TempDir()) // not a git repo: RepoRoot errors
	if _, complete := trees(); complete {
		t.Error("trees() complete = true with an unresolvable repo root")
	}
}
