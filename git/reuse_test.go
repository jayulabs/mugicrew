package git

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"jayulabs.org/mugicrew/eventlog/eventlogtest"
)

// ---- reuse of a kept effort branch (Spec mc-emoa): re-cut when it has no own commits ----

// keptEffort is a Workspace whose effort/b1 was cut at the container tip and then
// torn down, leaving the branch and its cut ref behind; the cwd is the main tree.
func keptEffort(t *testing.T) (cut string) {
	t.Helper()
	newRepo(t)
	mustGit(t, "branch", integCP)
	siblingMoves(t, "a.txt", "old\n")
	cut = mustGit(t, "rev-parse", integCP)
	_, teardown, err := NewEffortWorktree("b1", integCP, "")
	if err != nil {
		t.Fatal(err)
	}
	teardown()
	return cut
}

// reuse re-opens effort/b1 and returns the worktree's HEAD; the tree is torn down at cleanup.
func reuse(t *testing.T, container, pin string) string {
	t.Helper()
	wt, teardown, err := NewEffortWorktree("b1", container, pin)
	if err != nil {
		t.Fatalf("reuse: %v", err)
	}
	t.Cleanup(teardown)
	return mustGit(t, "-C", wt, "rev-parse", "HEAD")
}

// wantStaleBase fails unless evs is exactly one Warn effort.stale-base carrying want.
func wantStaleBase(t *testing.T, evs []eventlogtest.Event, want map[string]any) {
	t.Helper()
	if len(evs) != 1 || evs[0].Name != "effort.stale-base" || evs[0].Level != slog.LevelWarn {
		t.Fatalf("events %+v, want one Warn effort.stale-base", evs)
	}
	for k, v := range want {
		if got := evs[0].Attrs[k]; got != v {
			t.Errorf("attr %s = %v, want %v", k, got, v)
		}
	}
}

func TestReuseRecutsACommitlessBranchAfterTheContainerAdvances(t *testing.T) {
	keptEffort(t)
	siblingMoves(t, "s.txt", "s\n")
	tip := mustGit(t, "rev-parse", integCP)
	if got := reuse(t, integCP, ""); got != tip {
		t.Errorf("worktree HEAD %s, want the new container tip %s", got, tip)
	}
	if got := mustGit(t, "rev-parse", EffortBranch("b1")); got != tip {
		t.Errorf("effort tip %s, want %s", got, tip)
	}
	if got := mustGit(t, "rev-parse", cutRefPrefix+"b1"); got != tip {
		t.Errorf("cut ref %s, want %s", got, tip)
	}
}

func TestReuseRecutsACommitlessBranchAfterTheContainerIsRewritten(t *testing.T) {
	keptEffort(t)
	main := mustGit(t, "rev-parse", "main")
	// a rewrite: the container becomes a commit off main, not descending from the old tip
	tree := mustGit(t, "rev-parse", "main^{tree}")
	rewrite := mustGit(t, "commit-tree", tree, "-p", main, "-m", "rewritten")
	mustGit(t, "update-ref", "refs/heads/"+integCP, rewrite)
	if got := reuse(t, integCP, ""); got != rewrite {
		t.Errorf("worktree HEAD %s, want the rewritten tip %s", got, rewrite)
	}
	if got := mustGit(t, "rev-parse", cutRefPrefix+"b1"); got != rewrite {
		t.Errorf("cut ref %s, want %s", got, rewrite)
	}
}

func TestReuseRecutsAPinnedLeafToThePin(t *testing.T) {
	pin := keptEffort(t)
	// move the kept branch (and its cut ref) off the pin, still without own commits
	moved := mustGit(t, "commit-tree", pin+"^{tree}", "-p", pin, "-m", "moved")
	mustGit(t, "update-ref", cutRefPrefix+"b1", moved)
	mustGit(t, "update-ref", "refs/heads/"+EffortBranch("b1"), moved)
	siblingMoves(t, "s.txt", "s\n")
	if got := reuse(t, integCP, pin); got != pin {
		t.Errorf("worktree HEAD %s, want the pin %s", got, pin)
	}
}

func TestReuseFastForwardsALegacyBranchWithoutACutRef(t *testing.T) {
	keptEffort(t)
	mustGit(t, "update-ref", "-d", cutRefPrefix+"b1")
	siblingMoves(t, "s.txt", "s\n")
	tip := mustGit(t, "rev-parse", integCP)
	if got := reuse(t, integCP, ""); got != tip {
		t.Errorf("worktree HEAD %s, want %s", got, tip)
	}
}

func TestReuseKeepsOwnCommitsAndWarnsOnAStaleBase(t *testing.T) {
	cut := keptEffort(t)
	wt, teardown, err := NewEffortWorktree("b1", integCP, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wt+"/w.txt", []byte("w"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, "-C", wt, "add", ".")
	mustGit(t, "-C", wt, "commit", "-m", "own")
	own := mustGit(t, "-C", wt, "rev-parse", "HEAD")
	teardown()
	siblingMoves(t, "s.txt", "s\n")
	tip := mustGit(t, "rev-parse", integCP)

	evs := eventlogtest.Capture(t)
	if got := reuse(t, integCP, ""); got != own {
		t.Errorf("worktree HEAD %s, want the kept commit %s", got, own)
	}
	if got := mustGit(t, "rev-parse", cutRefPrefix+"b1"); got != cut {
		t.Errorf("cut ref %s, want it kept at %s", got, cut)
	}
	wantStaleBase(t, *evs, map[string]any{"bead": "b1", "branch": "effort/b1", "cut": cut, "container_tip": tip})
}

func TestReuseAtTheContainerTipIsUntouchedAndSilent(t *testing.T) {
	cut := keptEffort(t)
	evs := eventlogtest.Capture(t)
	if got := reuse(t, integCP, ""); got != cut {
		t.Errorf("worktree HEAD %s, want %s", got, cut)
	}
	if len(*evs) != 0 {
		t.Errorf("unexpected events %+v", *evs)
	}
}

func TestReuseOfABranchHeldElsewhereIsStillBusy(t *testing.T) {
	keptEffort(t)
	holder := t.TempDir() + "/held"
	mustGit(t, "worktree", "add", holder, EffortBranch("b1"))
	siblingMoves(t, "s.txt", "s\n")
	before := mustGit(t, "rev-parse", EffortBranch("b1"))
	_, _, err := NewEffortWorktree("b1", integCP, "")
	if _, ok := Busy(err, EffortBranch("b1")); !ok {
		t.Fatalf("err = %v, want Busy", err)
	}
	if got := mustGit(t, "rev-parse", EffortBranch("b1")); got != before {
		t.Errorf("held branch moved %s → %s", before, got)
	}
}

// A squash integrate after a re-cut lands only the effort's own work.
func TestIntegrateAfterARecutLandsOnlyTheEffortsWork(t *testing.T) {
	keptEffort(t)
	siblingMoves(t, "s.txt", "s\n")
	wt, teardown, err := NewEffortWorktree("b1", integCP, "")
	if err != nil {
		t.Fatal(err)
	}
	defer teardown()
	t.Chdir(wt)
	commitFile(t, "w.txt", "w\n")
	tip := mustGit(t, "rev-parse", integCP)
	if sha, _, err := integrateB1(); err != nil || sha == "" {
		t.Fatalf("Integrate = (%q, %v)", sha, err)
	}
	if got := mustGit(t, "diff", "--name-only", tip, integCP); got != "w.txt" {
		t.Errorf("integrate landed %q, want only w.txt", got)
	}
}

// ---- the same rule through Repo.Materialize in a nested repo ----

// keptChild is a child whose effort/b1 was materialized at checkpoint/cp and torn down.
func keptChild(t *testing.T) (child Repo, cut string) {
	t.Helper()
	child = newChild(t)
	cut, _ = child.Head("checkpoint/cp")
	down, err := child.Materialize(filepath.Join(t.TempDir(), "c"), "b1", "checkpoint/cp", "")
	if err != nil {
		t.Fatal(err)
	}
	down()
	return child, cut
}

// advanceChild moves checkpoint/cp to a new commit; parent is its parent ("" = old tip).
func advanceChild(t *testing.T, child Repo, parent string) string {
	t.Helper()
	if parent == "" {
		parent, _ = child.Head("checkpoint/cp")
	}
	n, err := child.run("commit-tree", "-p", parent, "-m", "moved", parent+"^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	child.run("update-ref", "refs/heads/checkpoint/cp", n)
	return n
}

// rematerialize returns the worktree HEAD of effort/b1, torn down at cleanup.
func rematerialize(t *testing.T, child Repo, pin string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "c")
	down, err := child.Materialize(path, "b1", "checkpoint/cp", pin)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(down)
	head, _ := Repo(path).Head("HEAD")
	return head
}

func TestMaterializeRecutsACommitlessChildBranch(t *testing.T) {
	child, _ := keptChild(t)
	tip := advanceChild(t, child, "")
	if got := rematerialize(t, child, ""); got != tip {
		t.Errorf("worktree HEAD %s, want the advanced tip %s", got, tip)
	}
	if got, _ := child.Head(cutRefPrefix + "b1"); got != tip {
		t.Errorf("cut ref %s, want %s", got, tip)
	}
	if got, _ := child.Head("effort/b1"); got != tip {
		t.Errorf("effort tip %s, want %s", got, tip)
	}
}

func TestMaterializeRecutsAfterAChildContainerRewrite(t *testing.T) {
	child := newChild(t)
	root, _ := child.Head("main")
	advanceChild(t, child, "") // cp = A, a child of root
	down, err := child.Materialize(filepath.Join(t.TempDir(), "c"), "b1", "checkpoint/cp", "")
	if err != nil {
		t.Fatal(err)
	}
	down()
	// rewrite: cp becomes a different child of root, not descending from A
	n, _ := child.run("commit-tree", "-p", root, "-m", "rewritten", root+"^{tree}")
	child.run("update-ref", "refs/heads/checkpoint/cp", n)
	if got := rematerialize(t, child, ""); got != n {
		t.Errorf("worktree HEAD %s, want %s", got, n)
	}
	if got, _ := child.Head(cutRefPrefix + "b1"); got != n {
		t.Errorf("cut ref %s, want %s", got, n)
	}
}

func TestMaterializeRecutsAPinnedChildLeafToThePin(t *testing.T) {
	child, cut := keptChild(t)
	moved, _ := child.run("commit-tree", "-p", cut, "-m", "moved", cut+"^{tree}")
	child.run("update-ref", cutRefPrefix+"b1", moved)
	child.run("update-ref", "refs/heads/effort/b1", moved)
	advanceChild(t, child, "")
	if got := rematerialize(t, child, cut); got != cut {
		t.Errorf("worktree HEAD %s, want the pin %s", got, cut)
	}
}

func TestMaterializeFastForwardsALegacyChildBranch(t *testing.T) {
	child, _ := keptChild(t)
	child.run("update-ref", "-d", cutRefPrefix+"b1")
	tip := advanceChild(t, child, "")
	if got := rematerialize(t, child, ""); got != tip {
		t.Errorf("worktree HEAD %s, want %s", got, tip)
	}
}

func TestMaterializeKeepsChildOwnCommitsAndWarns(t *testing.T) {
	child, cut := keptChild(t)
	path := filepath.Join(t.TempDir(), "c")
	down, err := child.Materialize(path, "b1", "checkpoint/cp", "")
	if err != nil {
		t.Fatal(err)
	}
	Repo(path).run("commit", "--allow-empty", "-m", "own")
	own, _ := Repo(path).Head("HEAD")
	down()
	advanceChild(t, child, "")
	evs := eventlogtest.Capture(t)
	if got := rematerialize(t, child, ""); got != own {
		t.Errorf("worktree HEAD %s, want the kept commit %s", got, own)
	}
	if got, _ := child.Head(cutRefPrefix + "b1"); got != cut {
		t.Errorf("cut ref %s, want %s", got, cut)
	}
	tip, _ := child.Head("checkpoint/cp")
	wantStaleBase(t, *evs, map[string]any{"bead": "b1", "branch": "effort/b1", "cut": cut, "container_tip": tip})
}

func TestMaterializeAtTheChildTipIsSilent(t *testing.T) {
	child, cut := keptChild(t)
	evs := eventlogtest.Capture(t)
	if got := rematerialize(t, child, ""); got != cut {
		t.Errorf("worktree HEAD %s, want %s", got, cut)
	}
	if len(*evs) != 0 {
		t.Errorf("unexpected events %+v", *evs)
	}
}

func TestReuseKeepsBranchAndCutConsistentWhenTheCutCannotBeRecorded(t *testing.T) {
	old := keptEffort(t)
	// legacy branch (no cut ref), and a nested ref that makes writing cut/b1 fail
	mustGit(t, "update-ref", "-d", cutRefPrefix+"b1")
	mustGit(t, "update-ref", cutRefPrefix+"b1/blocker", old)
	siblingMoves(t, "s.txt", "s\n")
	evs := eventlogtest.Capture(t)
	if got := reuse(t, integCP, ""); got != old {
		t.Errorf("worktree HEAD %s, want the old tip %s", got, old)
	}
	if got := mustGit(t, "rev-parse", EffortBranch("b1")); got != old {
		t.Errorf("effort tip %s, want it restored to %s", got, old)
	}
	if len(*evs) != 0 {
		t.Errorf("unexpected events: %v", eventlogtest.Names(*evs))
	}
}
