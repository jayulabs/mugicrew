package withdraw

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contexts/contextstest"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/route"
	"jayulabs.org/mugicrew/session"
)

// world is a Workspace repo with a nested child repo kid, temp trees under a private
// TMPDIR, no sessions and no Drafts; the container beads it shows are unlabelled
// (Workspace) unless a test labels one context:kid.
type world struct {
	ws, kid  string
	sessions map[string]session.Info
	labels   map[string][]string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tmp)
	w := &world{sessions: map[string]session.Info{}, labels: map[string][]string{}}
	w.ws, w.kid = contextstest.Workspace(t)
	if w.ws, err = filepath.EvalSymlinks(w.ws); err != nil {
		t.Fatal(err)
	}
	w.kid = filepath.Join(w.ws, "kid")
	os, osh, od := readSessions, contexts.ShowBead, draftsOf
	t.Cleanup(func() { readSessions, contexts.ShowBead, draftsOf = os, osh, od })
	readSessions = func() (map[string]session.Info, error) { return w.sessions, nil }
	contexts.ShowBead = func(id string) (bd.Bead, error) { return bd.Bead{ID: id, Labels: w.labels[id]}, nil }
	draftsOf = func(string) ([]string, error) { return nil, nil }
	t.Chdir(w.ws)
	return w
}

func (w *world) git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return contextstest.Git(t, dir, args...)
}

// tree adds a temp-shaped worktree for key on branch in repo dir.
func (w *world) tree(t *testing.T, dir, key, branch string) string {
	t.Helper()
	p := git.TempWorktreePath(key)
	w.git(t, dir, "worktree", "add", "-q", p, branch)
	return p
}

func (w *world) commits(t *testing.T, dir string, n int) {
	t.Helper()
	for range n {
		w.git(t, dir, "commit", "-q", "--allow-empty", "-m", "c")
	}
}

func build(id string) bd.Bead {
	return bd.Bead{ID: id, Kind: contract.KindBuild, Parent: "cp", Status: "open"}
}

func take(t *testing.T, o Opts, set ...bd.Bead) Inventory {
	t.Helper()
	inv, err := TakeInventory(set, o)
	if err != nil {
		t.Fatal(err)
	}
	return inv
}

func only(t *testing.T, inv Inventory) (BeadLeftovers, TreeLeftover) {
	t.Helper()
	if len(inv.Beads) != 1 || len(inv.Beads[0].Trees) != 1 {
		t.Fatalf("inventory = %+v, want one bead with one tree", inv)
	}
	return inv.Beads[0], inv.Beads[0].Trees[0]
}

// A dirty orphan tree is counted, removable, and loses work.
func TestInventoryCountsADirtyTree(t *testing.T) {
	w := newWorld(t)
	w.git(t, w.ws, "branch", "checkpoint/cp")
	w.git(t, w.ws, "branch", "effort/b1", "checkpoint/cp")
	p := w.tree(t, w.ws, "b1", "effort/b1")
	os.WriteFile(filepath.Join(p, "a"), nil, 0o644)
	os.WriteFile(filepath.Join(p, "b"), nil, 0o644)
	inv := take(t, Opts{}, build("b1"))
	left, tr := only(t, inv)
	if left.Branch != "effort/b1" || tr.Host != p || tr.Class != Removable || tr.Dirty != 2 || !inv.Loses() {
		t.Errorf("left %+v tree %+v loses %v", left, tr, inv.Loses())
	}
}

// A prunable tree (its dir gone) is not counted dirty and loses nothing.
func TestInventoryPrunableIsNotCounted(t *testing.T) {
	w := newWorld(t)
	w.git(t, w.ws, "branch", "checkpoint/cp")
	w.git(t, w.ws, "branch", "effort/b1", "checkpoint/cp")
	p := w.tree(t, w.ws, "b1", "effort/b1")
	os.RemoveAll(filepath.Dir(p))
	inv := take(t, Opts{}, build("b1"))
	_, tr := only(t, inv)
	if !tr.Prunable || tr.Class != Removable || tr.Dirty != 0 || inv.Loses() {
		t.Errorf("tree %+v loses %v", tr, inv.Loses())
	}
}

// A tree stopped mid-rebase has its conflicted files uncounted but is flagged, and
// that is a loss.
func TestInventoryRebasingIsAFlaggedLoss(t *testing.T) {
	w := newWorld(t)
	w.git(t, w.ws, "branch", "checkpoint/cp")
	w.git(t, w.ws, "branch", "effort/b1", "checkpoint/cp")
	p := w.tree(t, w.ws, "b1", "effort/b1")
	os.WriteFile(filepath.Join(p, "f"), []byte("effort\n"), 0o644)
	w.git(t, p, "add", "f")
	w.git(t, p, "commit", "-q", "-m", "effort")
	os.WriteFile(filepath.Join(w.ws, "f"), []byte("main\n"), 0o644)
	w.git(t, w.ws, "add", "f")
	w.git(t, w.ws, "commit", "-q", "-m", "main")
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	// the rebase conflicts and stops; its error is the point
	if out, err := execGit(p, "rebase", "main"); err == nil {
		t.Fatalf("rebase did not stop: %s", out)
	}
	inv := take(t, Opts{}, build("b1"))
	_, tr := only(t, inv)
	if !tr.Rebasing || tr.Dirty != 0 || !inv.Loses() {
		t.Errorf("tree %+v loses %v", tr, inv.Loses())
	}
}

// Unintegrated counts from the cut ref; without one it is an AtMost bound from the
// container; a replace-mode leaf is counted against its pinned Tip.
func TestInventoryUnintegrated(t *testing.T) {
	w := newWorld(t)
	w.git(t, w.ws, "branch", "checkpoint/cp")
	w.git(t, w.ws, "branch", "effort/b1", "checkpoint/cp")
	cut := w.git(t, w.ws, "rev-parse", "checkpoint/cp")
	w.git(t, w.ws, "checkout", "-q", "effort/b1")
	w.commits(t, w.ws, 3)
	w.git(t, w.ws, "checkout", "-q", "main")

	left := take(t, Opts{}, build("b1")).Beads[0]
	if left.Unintegrated != 3 || !left.AtMost {
		t.Errorf("no cut ref: %+v, want AtMost 3", left)
	}
	w.git(t, w.ws, "update-ref", "refs/mugicrew/cut/b1", cut)
	w.git(t, w.ws, "checkout", "-q", "checkpoint/cp")
	w.commits(t, w.ws, 2) // the container moves on; the cut ref still counts only the effort's own
	w.git(t, w.ws, "checkout", "-q", "main")
	left = take(t, Opts{}, build("b1")).Beads[0]
	if left.Unintegrated != 3 || left.AtMost || left.Replace {
		t.Errorf("cut ref: %+v, want 3 exactly", left)
	}

	tip := w.git(t, w.ws, "rev-parse", "effort/b1~1")
	rb := bd.Bead{ID: "b1", Kind: contract.KindConflict, Parent: "cp", Description: "Tip: " + tip}
	left = take(t, Opts{}, rb).Beads[0]
	if left.Unintegrated != 1 || !left.Replace || left.AtMost {
		t.Errorf("replace: %+v, want 1 against Tip", left)
	}
	if d, _, _ := route.OnEffort(rb); d.Mode != route.Replace {
		t.Fatalf("fixture is not replace-mode: %+v", d)
	}
}

// A nested effort's branch and trees are found in the child repo, host and child
// together, with the files changed in either counted.
func TestInventoryNestedChild(t *testing.T) {
	w := newWorld(t)
	w.labels["cp"] = []string{"kind:checkpoint", contextstest.Label}
	w.git(t, w.kid, "branch", "checkpoint/cp")
	// session.CutTree's nested cut: a detached Workspace host, the child materialized in it
	host, _, err := git.NewDetachedWorktree(git.Mainline()) // teardown is the cleanup's job
	if err != nil {
		t.Fatal(err)
	}
	if _, err := git.Repo(w.kid).Materialize(filepath.Join(host, "kid"), "b1", "checkpoint/cp", ""); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(host, "kid", "x"), nil, 0o644)
	os.WriteFile(filepath.Join(host, "y"), nil, 0o644)
	inv := take(t, Opts{}, build("b1"))
	left, tr := only(t, inv)
	if left.Repo != git.Repo(w.kid) || left.Branch != "effort/b1" {
		t.Errorf("left %+v", left)
	}
	if tr.Host != host || tr.Child != filepath.Join(host, "kid") || tr.Class != Removable || tr.Dirty != 2 {
		t.Errorf("tree %+v, want host %s", tr, host)
	}
}

// A tree that is not mugicrew's, and the main worktree on the branch, are foreign
// with their holder, and losing nothing of the inventory's.
func TestInventoryForeign(t *testing.T) {
	w := newWorld(t)
	w.git(t, w.ws, "branch", "checkpoint/cp")
	w.git(t, w.ws, "branch", "effort/b1", "checkpoint/cp")
	other := filepath.Join(t.TempDir(), "mine")
	w.git(t, w.ws, "worktree", "add", "-q", other, "effort/b1")
	os.WriteFile(filepath.Join(other, "z"), nil, 0o644)
	inv := take(t, Opts{}, build("b1"))
	_, tr := only(t, inv)
	if tr.Class != Foreign || tr.Holder != other || inv.Loses() {
		t.Errorf("linked: %+v loses %v", tr, inv.Loses())
	}
	w.git(t, w.ws, "worktree", "remove", "--force", other)
	w.git(t, w.ws, "checkout", "-q", "effort/b1")
	_, tr = only(t, take(t, Opts{}, build("b1")))
	if tr.Class != Foreign || tr.Holder != w.ws {
		t.Errorf("main worktree: %+v", tr)
	}
}

// An alive session's tree is alive and its dirt is a loss; an exited session of a
// set bead is removable and carries the session; an exited session of a bead outside
// the set is foreign.
func TestInventorySessions(t *testing.T) {
	w := newWorld(t)
	w.git(t, w.ws, "branch", "checkpoint/cp")
	w.git(t, w.ws, "branch", "effort/b1", "checkpoint/cp")
	p := w.tree(t, w.ws, "b1", "effort/b1")
	os.WriteFile(filepath.Join(p, "a"), nil, 0o644)

	w.sessions["b1"] = session.Info{Session: "s1", Worktree: p, State: session.Alive}
	inv := take(t, Opts{}, build("b1"))
	if _, tr := only(t, inv); tr.Class != Alive || !inv.Loses() {
		t.Errorf("alive: %+v", tr)
	}
	w.sessions["b1"] = session.Info{Session: "s1", Worktree: p, State: session.Exited}
	if _, tr := only(t, take(t, Opts{}, build("b1"))); tr.Class != Removable || tr.Session != "s1" {
		t.Errorf("exited: %+v", tr)
	}
	w.sessions = map[string]session.Info{"other": {Session: "s2", Worktree: p, State: session.Exited}}
	if _, tr := only(t, take(t, Opts{}, build("b1"))); tr.Class != Foreign || tr.Holder != "other" {
		t.Errorf("exited, other bead: %+v", tr)
	}
	w.sessions = nil
	live := Opts{InFlight: func(b bd.Bead) bool { return b.ID == "b1" }}
	if _, tr := only(t, take(t, live, build("b1"))); tr.Class != Alive {
		t.Errorf("live chopper: %+v", tr)
	}
}

// A closing Chart's or Checkpoint's branch is found with its commits ahead of mainline.
func TestInventoryContainers(t *testing.T) {
	w := newWorld(t)
	w.git(t, w.ws, "branch", "chart/ch")
	w.git(t, w.ws, "branch", "checkpoint/cp")
	w.git(t, w.ws, "checkout", "-q", "chart/ch")
	w.commits(t, w.ws, 2)
	w.git(t, w.ws, "checkout", "-q", "main")
	ch := bd.Bead{ID: "ch", Kind: contract.KindChart}
	cp := bd.Bead{ID: "cp", Kind: contract.KindCheckpoint}
	inv := take(t, Opts{}, ch, cp)
	if len(inv.Containers) != 2 || inv.Containers[0].Branch != "chart/ch" || inv.Containers[0].Ahead != 2 ||
		inv.Containers[1].Branch != "checkpoint/cp" || inv.Containers[1].Ahead != 0 || !inv.Loses() {
		t.Errorf("containers = %+v", inv.Containers)
	}
	if got := take(t, Opts{}, cp); got.Loses() {
		t.Errorf("an unchanged checkpoint loses work: %+v", got)
	}
	if got := inv.String(); !strings.Contains(got, "chart/ch (2 commits never landed)") {
		t.Errorf("String = %q", got)
	}
}

// A failed session-table read fails the inventory.
func TestInventorySessionReadFails(t *testing.T) {
	newWorld(t)
	boom := errors.New("tmux")
	readSessions = func() (map[string]session.Info, error) { return nil, boom }
	if _, err := TakeInventory([]bd.Bead{build("b1")}, Opts{}); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
}

// Leftover Drafts are listed, and a bead with nothing left is left out.
func TestInventoryDraftsAndEmpty(t *testing.T) {
	newWorld(t)
	draftsOf = func(id string) ([]string, error) {
		if id == "b1" {
			return []string{"d1", "d2"}, nil
		}
		return nil, nil
	}
	inv := take(t, Opts{}, build("b1"), build("b2"))
	if len(inv.Beads) != 1 || !slices.Equal(inv.Beads[0].Drafts, []string{"d1", "d2"}) || inv.Loses() {
		t.Errorf("inventory = %+v", inv)
	}
}

func execGit(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@e.com", "-c", "user.name=t"}, args...)...).CombinedOutput()
	return string(out), err
}

// A bead that can't route still has its Drafts inventoried, so cleanup deletes them.
func TestInventoryDraftsOfUnroutableBead(t *testing.T) {
	newWorld(t)
	draftsOf = func(string) ([]string, error) { return []string{"d1"}, nil }
	orphan := bd.Bead{ID: "x", Kind: contract.KindBuild, Status: "open"} // no parent: no route
	inv := take(t, Opts{}, orphan)
	if len(inv.Beads) != 1 || !slices.Equal(inv.Beads[0].Drafts, []string{"d1"}) {
		t.Errorf("inventory = %+v", inv)
	}
}

// An alive tree is attributed to its session, not a generic holder.
func TestInventoryAliveTreeNamesSession(t *testing.T) {
	w := newWorld(t)
	w.git(t, w.ws, "branch", "checkpoint/cp")
	w.git(t, w.ws, "branch", "effort/b1", "checkpoint/cp")
	p := w.tree(t, w.ws, "b1", "effort/b1")
	w.sessions["b1"] = session.Info{Session: "s1", Worktree: p, State: session.Alive}
	if _, tr := only(t, take(t, Opts{}, build("b1"))); tr.Class != Alive || tr.Session != "s1" {
		t.Errorf("alive: %+v", tr)
	}
}

// A count of one is singular.
func TestInventoryStringSingular(t *testing.T) {
	inv := Inventory{
		Beads:      []BeadLeftovers{{ID: "b1", Branch: "effort/b1", Unintegrated: 1}},
		Containers: []ContainerBranch{{ID: "ch", Branch: "chart/ch", Ahead: 1}},
	}
	got := inv.String()
	if !strings.Contains(got, "(1 unintegrated commit)") || !strings.Contains(got, "(1 commit never landed)") {
		t.Errorf("String = %q", got)
	}
}
