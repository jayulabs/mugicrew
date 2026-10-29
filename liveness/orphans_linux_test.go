package liveness

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// orphanFixture lays a fake /proc, a temp lock dir and a worktree-shaped tree.
func orphanFixture(t *testing.T, argvs ...[]string) (tree, repoRoot string) {
	t.Helper()
	fakeProc(t, argvs...)
	fakeLocks(t)
	t.Setenv("BEADS_DIR", t.TempDir())
	root := t.TempDir()
	tree = filepath.Join(root, "mugicrew-wt-1", "wt")
	if err := os.MkdirAll(tree, 0o755); err != nil {
		t.Fatal(err)
	}
	return tree, root
}

func TestOrphans(t *testing.T) {
	for _, c := range []struct {
		name  string
		argvs [][]string
		cwd   string // "" = no cwd process; TREE, ROOT and LINK expand to the fixture paths
		hold  bool
		tree  string
		want  bool // orphaned
	}{
		{"none of the proofs", [][]string{{"bash"}}, "", false, "TREE", true},
		{"argv token", [][]string{{"mugicrew", "reconcile", "b1"}}, "", false, "TREE", false},
		{"held flock", nil, "", true, "TREE", false},
		{"cwd in tree", nil, "TREE/sub", false, "TREE", false},
		{"deleted cwd", nil, "TREE (deleted)", false, "TREE", false},
		{"symlinked tree", nil, "TREE", false, "LINK/wt", false},
		{"cwd outside the tree", nil, "ROOT", false, "TREE", true},
		{"repo-root skips the cwd check", nil, "ROOT", false, "", true},
		{"repo-root still has argv", [][]string{{"claude", "--name", "b1"}}, "ROOT", false, "", false},
		{"repo-root still has the flock", nil, "ROOT", true, "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			tree, root := orphanFixture(t, c.argvs...)
			link := filepath.Join(root, "link")
			if err := os.Symlink(filepath.Join(root, "mugicrew-wt-1"), link); err != nil {
				t.Fatal(err)
			}
			at := strings.NewReplacer("TREE", tree, "ROOT", root, "LINK", link).Replace
			if c.cwd != "" {
				fakeCwd(t, 50, at(c.cwd))
			}
			if c.hold {
				h, err := Acquire("b1")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(h.Release)
			}
			got := Orphans([]Effort{{ID: "b1", Tree: at(c.tree)}}, nil)
			if got["b1"] != c.want {
				t.Errorf("orphaned = %v, want %v", got["b1"], c.want)
			}
		})
	}
}

// The reading never counts the caller's own lineage, argv or cwd.
func TestOrphansExcludesLineage(t *testing.T) {
	tree, _ := orphanFixture(t, []string{"mugicrew", "reconcile", "b1"})
	fakeCwd(t, 2, tree) // a second pid whose cwd is the tree
	if got := Orphans([]Effort{{ID: "b1", Tree: tree}}, []int{1, 2}); !got["b1"] {
		t.Error("excluded argv and cwd pids must not keep the bead alive")
	}
	if got := Orphans([]Effort{{ID: "b1", Tree: tree}}, []int{1}); got["b1"] {
		t.Error("a non-excluded cwd pid must keep the bead alive")
	}
}

// A platform that can't read cwds reads a tree bead as not orphan; a bead
// with no cwd check, or already proven alive, is unaffected.
func TestOrphansCwdReadError(t *testing.T) {
	tree, _ := orphanFixture(t)
	orig := procTable
	procTable = func(bool) ([]procRow, error) {
		return []procRow{{pid: 7, argv: []string{"bash"}}}, errors.New("no lsof")
	}
	t.Cleanup(func() { procTable = orig })
	got := Orphans([]Effort{{ID: "b1", Tree: tree}, {ID: "b2"}}, nil)
	if got["b1"] {
		t.Error("a cwd read error must read not orphan")
	}
	if !got["b2"] {
		t.Error("a repo-root bead with no argv and no flock reads orphan")
	}
}

// A cwd read error keeps the argv rows: a repo-root bead whose only proof of life
// is an argv token stays alive.
func TestOrphansCwdReadErrorKeepsArgvRows(t *testing.T) {
	tree, _ := orphanFixture(t)
	orig := procTable
	procTable = func(bool) ([]procRow, error) {
		return []procRow{{pid: 7, argv: []string{"claude", "--name", "b2"}}}, errors.New("no lsof")
	}
	t.Cleanup(func() { procTable = orig })
	got := Orphans([]Effort{{ID: "b1", Tree: tree}, {ID: "b2"}}, nil)
	if got["b2"] {
		t.Error("a repo-root bead with a live argv token must not read orphan on a cwd read error")
	}
}

// One reading answers several ids, each on its own proofs.
func TestOrphansManyIDs(t *testing.T) {
	tree, root := orphanFixture(t, []string{"mugicrew", "reconcile", "b1"})
	tree2 := filepath.Join(root, "mugicrew-wt-2", "wt")
	tree3 := filepath.Join(root, "mugicrew-wt-3", "wt")
	for _, d := range []string{tree2, tree3} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fakeCwd(t, 50, tree2)
	h, err := Acquire("b4")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Release)
	got := Orphans([]Effort{{ID: "b1", Tree: tree}, {ID: "b2", Tree: tree2}, {ID: "b3", Tree: tree3}, {ID: "b4"}, {ID: "b5"}}, nil)
	want := map[string]bool{"b1": false, "b2": false, "b3": true, "b4": false, "b5": true}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s orphaned = %v, want %v", id, got[id], w)
		}
	}
}

// The reading walks the process table once: one glob of procDir per call, argv
// and cwd read in that same pass.
func TestOrphansOneProcWalk(t *testing.T) {
	tree, _ := orphanFixture(t)
	orig := procTable
	walks := 0
	procTable = func(wantCwd bool) ([]procRow, error) { walks++; return orig(wantCwd) }
	t.Cleanup(func() { procTable = orig })
	Orphans([]Effort{{ID: "b1", Tree: tree}, {ID: "b2", Tree: tree}}, nil)
	if walks != 1 {
		t.Errorf("walks = %d, want 1", walks)
	}
}
