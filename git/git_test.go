package git

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Busy is exit 128 AND a listed holder (ADR 0056), keyed on the exit code, never
// the fatal: string — asserted against a real *exec.ExitError with the holder
// lookup scripted, not a live worktree race. Exit 128 with no holder,
// or a failed lookup, is a failed cut, not busy.
func TestBusyNeedsAListedHolder(t *testing.T) {
	orig := WorktreeOn
	defer func() { WorktreeOn = orig }()
	held := Holder{Path: "/tmp/mugicrew-wt-1/wt", Prunable: true}
	var asked string
	listed, lerr := true, error(nil)
	WorktreeOn = func(branch string) (Holder, bool, error) {
		asked = branch
		if !listed {
			return Holder{}, false, lerr
		}
		return held, true, nil
	}
	e128 := exec.Command("sh", "-c", "exit 128").Run()
	// run() wraps the ExitError with %w; the wrap must not hide it.
	if h, busy := Busy(fmt.Errorf("git worktree add: %w: fatal: already used", e128), "chart/mc-x"); !busy || h != held || asked != "chart/mc-x" {
		t.Errorf("exit 128 + holder: busy=%v holder=%+v asked=%q, want busy naming %+v on chart/mc-x", busy, h, asked, held)
	}
	listed = false
	if _, busy := Busy(e128, "chart/mc-x"); busy {
		t.Error("exit 128 with no listed holder is a failed cut, not busy")
	}
	lerr = errors.New("list failed")
	if _, busy := Busy(e128, "chart/mc-x"); busy {
		t.Error("exit 128 with a failed holder lookup is a failed cut, not busy")
	}
	listed = true
	if _, busy := Busy(exec.Command("sh", "-c", "exit 1").Run(), "chart/mc-x"); busy {
		t.Error("exit 1 is a different fatal, not the interlock")
	}
	if _, busy := Busy(nil, "b"); busy {
		t.Error("nil is not busy")
	}
	if _, busy := Busy(errors.New("plain"), "b"); busy {
		t.Error("a non-exit error is not busy")
	}
}

// Real git: a branch held by a linked worktree is busy naming that tree; a cut on
// an invalid ref (also exit 128) names no holder, so it is not busy (ADR 0056).
func TestBusyRealGit(t *testing.T) {
	repo := t.TempDir()
	t.Chdir(repo)
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "i"}, {"branch", "chart/mc-x"}} {
		if _, err := run(args...); err != nil {
			t.Fatal(err)
		}
	}
	holder := filepath.Join(t.TempDir(), "h")
	if _, err := run(worktreeAddArgs(holder, "chart/mc-x")...); err != nil {
		t.Fatal(err)
	}
	_, err := run(worktreeAddArgs(filepath.Join(t.TempDir(), "x"), "chart/mc-x")...)
	if h, busy := Busy(err, "chart/mc-x"); !busy || filepath.Base(h.Path) != "h" || h.Prunable {
		t.Errorf("held branch: busy=%v holder=%+v (err %v), want busy naming the linked tree", busy, h, err)
	}
	_, err = run(worktreeAddArgs(filepath.Join(t.TempDir(), "y"), "chart/nope")...)
	if _, busy := Busy(err, "chart/nope"); err == nil || busy {
		t.Errorf("invalid ref: busy=%v err=%v, want a non-busy failure", busy, err)
	}
}

// ---- Seam A: pure helpers, no repo (mirrors bd/succession_test.go's *Args) ----

func TestBranchNameReconstructsFromID(t *testing.T) {
	if got := BranchName("chart", "mc-x"); got != "chart/mc-x" {
		t.Errorf("branchName chart = %q, want chart/mc-x", got)
	}
	if got := BranchName("checkpoint", "mc-x"); got != "checkpoint/mc-x" {
		t.Errorf("branchName checkpoint = %q, want checkpoint/mc-x", got)
	}
}

// TrailerFor takes the kind constant (lowercase) and title-cases it, so the
// trailer can't drift from the branch name BranchName builds from the same kind.
func TestTrailerForBuildsLandTrailer(t *testing.T) {
	if got := TrailerFor("chart", "mc-x"); got != "Chart: mc-x" {
		t.Errorf("TrailerFor chart = %q, want %q", got, "Chart: mc-x")
	}
	if got := TrailerFor("checkpoint", "mc-x"); got != "Checkpoint: mc-x" {
		t.Errorf("TrailerFor checkpoint = %q, want %q", got, "Checkpoint: mc-x")
	}
}

// The body pins round-trip: CommitPin writes "Commit: <sha>" and ParseCommit
// greps it back out of a multi-line bead body; ParseBase reads the conflict
// bead's "Base: <sha>"; an absent pin is (,false).
func TestBodyPinsRoundTrip(t *testing.T) {
	if got := CommitPin("deadbeef"); got != "Commit: deadbeef" {
		t.Errorf("CommitPin = %q, want %q", got, "Commit: deadbeef")
	}
	body := "Approved by hand.\n\nCommit: deadbeef\n"
	if sha, ok := ParseCommit(body); !ok || sha != "deadbeef" {
		t.Errorf("ParseCommit = (%q, %v), want (deadbeef, true)", sha, ok)
	}
	if got := BasePin("cafef00d"); got != "Base: cafef00d" {
		t.Errorf("BasePin = %q, want %q", got, "Base: cafef00d")
	}
	if sha, ok := ParseBase(BasePin("cafef00d")); !ok || sha != "cafef00d" {
		t.Errorf("BasePin→ParseBase round-trip = (%q, %v), want (cafef00d, true)", sha, ok)
	}
	if sha, ok := ParseBase("Conflict on tail.\nBase: cafef00d\n"); !ok || sha != "cafef00d" {
		t.Errorf("ParseBase = (%q, %v), want (cafef00d, true)", sha, ok)
	}
	if sha, ok := ParseTip("Chart-conflict.\n" + BasePin("cafef00d") + "\n" + TipPin("t1p")); !ok || sha != "t1p" {
		t.Errorf("TipPin→ParseTip round-trip = (%q, %v), want (t1p, true)", sha, ok)
	}
	if _, ok := ParseCommit("no pin here\n"); ok {
		t.Error("ParseCommit of a pinless body must be (,false)")
	}
	want := &RevertConflict{Base: "cafef00d", Paths: []string{"a.go", "b.go"}}
	if c, ok := ParseConflict("Successor.\n" + ConflictPin(want) + "\nSpec: mc-s"); !ok || c.Base != want.Base || !slices.Equal(c.Paths, want.Paths) {
		t.Errorf("ConflictPin→ParseConflict round-trip = (%+v, %v), want %+v", c, ok, want)
	}
	if _, ok := ParseConflict("Successor.\n" + BasePin("cafef00d")); ok {
		t.Error("ParseConflict must key on its own Fix-forward: pin, never a bare Base: line")
	}
}

// ApprovedAtHead is true only while the approval's Commit: pin equals the live
// tip; a moved HEAD, a missing pin, or a Head error unpins it. The Head var seam
// stands in for the repo.
func TestApprovedAtHead(t *testing.T) {
	orig := Head
	defer func() { Head = orig }()

	Head = func(branch string) (string, error) { return "deadbeef", nil }
	if !ApprovedAtHead("Commit: deadbeef", "checkpoint/mc-x") {
		t.Error("a pin equal to the live tip must be at head")
	}
	if ApprovedAtHead("Commit: stale", "checkpoint/mc-x") {
		t.Error("a pin behind the live tip must not be at head")
	}
	if ApprovedAtHead("no pin", "checkpoint/mc-x") {
		t.Error("a pinless body must not be at head")
	}

	Head = func(branch string) (string, error) { return "", errors.New("no such branch") }
	if ApprovedAtHead("Commit: deadbeef", "checkpoint/mc-x") {
		t.Error("a Head error must not be at head")
	}
}

func TestSquashArgs(t *testing.T) {
	if got := softResetArgs("deadbeef"); len(got) != 3 || got[0] != "reset" || got[1] != "--soft" || got[2] != "deadbeef" {
		t.Errorf("softResetArgs = %v, want [reset --soft deadbeef]", got)
	}
	commit := squashCommitArgs("chart/mc-x", "", "abc", "Chart: mc-x", "g1", "")
	if commit[len(commit)-1] != "Tip: abc\nChart: mc-x\nGraph: g1" {
		t.Errorf("squashCommitArgs must carry the landed tip, then the trailer, last: %v", commit)
	}
	// --allow-empty so a fog-only Chart still stamps its anchor (ADR 0014 §2).
	if !slices.Contains(commit, "--allow-empty") {
		t.Errorf("squashCommitArgs must allow an empty (artifact-less) land: %v", commit)
	}
	// The ff moves the mainline ref via update-ref, never `branch -f` (which
	// refuses a branch checked out in another worktree). It is a
	// compare-and-swap: the expected old base is the 4th positional, so a
	// concurrent land that moved mainline makes the swap fail.
	ff := ffMainlineArgs(Mainline(), "chart/mc-x", "oldbase")
	want := []string{"update-ref", "refs/heads/" + Mainline(), "chart/mc-x", "oldbase"}
	if len(ff) != len(want) {
		t.Fatalf("ffMainlineArgs = %v, want %v", ff, want)
	}
	for i := range want {
		if ff[i] != want[i] {
			t.Errorf("ffMainlineArgs = %v, want %v", ff, want)
		}
	}
	for _, a := range ff {
		if a == "branch" {
			t.Errorf("ff must not use `branch -f` (fails under worktree topology): %v", ff)
		}
	}
}

// holderOf picks the worktree whose checked-out branch is the one asked for, and
// is none when it is checked out nowhere (for Mainline, the per-effort topology,
// where Land falls back to the update-ref CAS). A prunable or locked stanza still
// holds its branch (ADR 0041); a detached (mid-rebase) or bare one holds none
// (ADR 0056). Pure, so the branch is checkable without a repo.
func TestHolderOfParse(t *testing.T) {
	const (
		main     = "worktree /repo\nHEAD aaa\nbranch refs/heads/" + "main" + "\n"
		linked   = "worktree /repo/wt\nHEAD bbb\nbranch refs/heads/chart/mc-x\n"
		locked   = "worktree /repo/lk\nHEAD bbb\nbranch refs/heads/chart/mc-x\nlocked reason\n"
		prunable = "worktree /tmp/mugicrew-wt-1/wt\nHEAD bbb\nbranch refs/heads/chart/mc-x\nprunable gitdir file points to non-existent location\n"
		detached = "worktree /repo/rb\nHEAD ccc\ndetached\n"
		bare     = "worktree /repo.git\nbare\n"
	)
	for _, c := range []struct {
		name, porcelain, branch string
		want                    Holder
		ok                      bool
	}{
		{"main is the first record", main + "\n" + linked, "main", Holder{Path: "/repo"}, true},
		{"linked", main + "\n" + linked, "chart/mc-x", Holder{Path: "/repo/wt"}, true},
		{"checked out nowhere", linked, "main", Holder{}, false},
		{"locked still holds", main + "\n" + locked, "chart/mc-x", Holder{Path: "/repo/lk"}, true},
		{"prunable still holds", prunable, "chart/mc-x", Holder{Path: "/tmp/mugicrew-wt-1/wt", Prunable: true}, true},
		{"prunable elsewhere doesn't leak", prunable + "\n" + main, "main", Holder{Path: "/repo"}, true},
		{"detached holds nothing", detached, "chart/mc-x", Holder{}, false},
		{"bare holds nothing", bare + "\n" + linked, "main", Holder{}, false},
	} {
		if h, ok := holderOf(c.porcelain, c.branch); h != c.want || ok != c.ok {
			t.Errorf("%s: holderOf = %+v,%v want %+v,%v", c.name, h, ok, c.want, c.ok)
		}
		if got := worktreeOn(c.porcelain, c.branch); got != c.want.Path {
			t.Errorf("%s: worktreeOn = %q, want %q", c.name, got, c.want.Path)
		}
	}
}

// The through-worktree ff is `git -C <dir> merge --ff-only <branch>`: it moves the
// checkout's index/tree with the ref (not just the ref, which stranded it), and
// --ff-only preserves the mainline CAS by refusing a non-fast-forward.
func TestMergeFFOnlyArgs(t *testing.T) {
	got := mergeFFOnlyArgs("/repo", "chart/mc-x")
	want := []string{"-C", "/repo", "merge", "--ff-only", "chart/mc-x"}
	if !slices.Equal(got, want) {
		t.Fatalf("mergeFFOnlyArgs = %v, want %v", got, want)
	}
}

// The footgun: cutting a branch must NOT switch HEAD, so it is `git branch`,
// never `checkout -b`.
func TestCutBranchArgsDoNotSwitchHead(t *testing.T) {
	args := cutBranchArgs("chart/mc-x", "main")
	if len(args) != 3 || args[0] != "branch" || args[1] != "chart/mc-x" || args[2] != "main" {
		t.Fatalf("cutBranchArgs = %v, want [branch chart/mc-x main]", args)
	}
	for _, a := range args {
		if a == "checkout" || a == "-b" {
			t.Errorf("CutBranch must not switch HEAD (found %q)", a)
		}
	}
}

func TestLandRebaseArgsRebaseOntoBase(t *testing.T) {
	args := landRebaseArgs("deadbeef")
	if len(args) != 2 || args[0] != "rebase" || args[1] != "deadbeef" {
		t.Errorf("landRebaseArgs = %v, want [rebase deadbeef]", args)
	}
}

// The invariant: the landed base is grepped on the mainline, not HEAD,
// against the whole trailer line, with the id escaped so it can't be
// read as a regex.
func TestLandedBaseArgsGrepMainlineConst(t *testing.T) {
	args := trailerLogArgs(Mainline(), "Chart: mc-x.1")
	has := func(want string) bool {
		for _, a := range args {
			if a == want {
				return true
			}
		}
		return false
	}
	if !has(Mainline()) {
		t.Errorf("trailerLogArgs must scope to mainline %q: %v", Mainline(), args)
	}
	if !has("--extended-regexp") {
		t.Errorf("trailerLogArgs must grep an ERE: %v", args)
	}
	if !has(`--grep=^Chart: mc-x\.1$`) {
		t.Errorf("trailerLogArgs must grep the escaped whole trailer line: %v", args)
	}
}

func TestWorktreeAddArgsCheckOutBranch(t *testing.T) {
	args := worktreeAddArgs("/tmp/wt", "chart/mc-x")
	want := []string{"worktree", "add", "/tmp/wt", "chart/mc-x"}
	if len(args) != len(want) {
		t.Fatalf("worktreeAddArgs = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("worktreeAddArgs = %v, want %v", args, want)
		}
	}
}

func TestWorktreeRemoveArgsForce(t *testing.T) {
	args := worktreeRemoveArgs("/tmp/wt")
	want := []string{"worktree", "remove", "--force", "/tmp/wt"}
	if len(args) != len(want) {
		t.Fatalf("worktreeRemoveArgs = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("worktreeRemoveArgs = %v, want %v", args, want)
		}
	}
}

// ---- Seam B: real git (t.TempDir() + git init) ----

func newRepo(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	mustGit(t, "init", "-b", "main")
	mustGit(t, "config", "user.email", "test@example.com")
	mustGit(t, "config", "user.name", "test")
	writeFile(t, "README", "root\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "root")
}

// The Workspace root is one dir from anywhere inside the repo (Spec FR3):
// the root itself, a subdirectory and a linked worktree all resolve to it.
func TestRepoRootIsTheWorkspaceFromAnywhereInside(t *testing.T) {
	newRepo(t)
	root, _ := os.Getwd()
	mustGit(t, "branch", "chart/mc-x")
	wt := filepath.Join(t.TempDir(), "wt")
	mustGit(t, worktreeAddArgs(wt, "chart/mc-x")...)
	if err := os.Mkdir("sub", 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{root, filepath.Join(root, "sub"), wt} {
		t.Chdir(dir)
		got, err := RepoRoot()
		if err != nil || got != root {
			t.Errorf("RepoRoot from %s = %q, %v; want %q", dir, got, err, root)
		}
	}
}

func mustGit(t *testing.T, args ...string) string {
	t.Helper()
	out, err := run(args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

func writeFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// Round-trip + idempotency + landed-base read + the Zoro checkpoint cut, on one
// landed repo: cut chart/x → accrue an artifact commit → Land → the landed base
// is an exact single commit whose content survives; Land is an idempotent no-op
// that still returns the SHA; LandedBase re-derives it; a checkpoint/x branch
// cuts cleanly from the landed SHA.
func TestLandRoundTrip(t *testing.T) {
	newRepo(t)

	created, err := CutBranch("chart/x", Mainline())
	if err != nil || !created {
		t.Fatalf("CutBranch chart/x = (%v, %v), want (true, nil)", created, err)
	}

	mustGit(t, "checkout", "chart/x")
	writeFile(t, "artifact.txt", "defog output\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "an artifact")

	sha, err := Land("chart/x", "Chart: x", LandLocal)
	if err != nil {
		t.Fatalf("Land: %v", err)
	}
	if sha == "" {
		t.Fatal("Land returned an empty SHA")
	}

	// The artifact content survives the squash onto mainline.
	if got := mustGit(t, "show", Mainline()+":artifact.txt"); got != "defog output" {
		t.Errorf("artifact content on mainline = %q, want the artifact body", got)
	}
	// Exactly one squash commit landed (root + squash == 2).
	if n := mustGit(t, "rev-list", "--count", Mainline()); n != "2" {
		t.Errorf("mainline commit count = %s, want 2 (root + one squash)", n)
	}
	// The landed base is mainline HEAD immediately after the land.
	if head := mustGit(t, "rev-parse", Mainline()); head != sha {
		t.Errorf("Land sha %s != mainline HEAD %s", sha, head)
	}
	// LandedBase re-derives the same SHA by trailer (Zoro's read path).
	if lb, err := LandedBase("Chart: x"); err != nil || lb != sha {
		t.Errorf("LandedBase = (%s, %v), want (%s, nil)", lb, err, sha)
	}

	// Idempotent no-op: a second Land still returns the SHA and adds no commit.
	sha2, err := Land("chart/x", "Chart: x", LandLocal)
	if err != nil || sha2 != sha {
		t.Errorf("idempotent Land = (%s, %v), want (%s, nil)", sha2, err, sha)
	}
	if n := mustGit(t, "rev-list", "--count", Mainline()); n != "2" {
		t.Errorf("idempotent Land added a commit: count = %s, want 2", n)
	}

	// A re-cut of an existing branch is a no-op (branch-if-absent).
	if created, err := CutBranch("chart/x", Mainline()); err != nil || created {
		t.Errorf("re-CutBranch chart/x = (%v, %v), want (false, nil)", created, err)
	}

	// The Zoro seam: checkpoint/x cuts cleanly from the landed SHA.
	created, err = CutBranch("checkpoint/x", sha)
	if err != nil || !created {
		t.Fatalf("CutBranch checkpoint/x from landed SHA = (%v, %v), want (true, nil)", created, err)
	}
	if tip := mustGit(t, "rev-parse", "checkpoint/x"); tip != sha {
		t.Errorf("checkpoint/x tip = %s, want the landed SHA %s", tip, sha)
	}
}

// A fog-only Chart (branch cut, zero artifact commits) still lands: it stamps
// its "Chart: <id>" anchor via an empty squash commit, so LandedBase resolves
// and Zoro has a base to cut from. Regression guard for the two ways this used
// to break — the squash "nothing to commit", and branch-ancestry idempotency
// mistaking the un-landed empty branch (sitting at base) for already-landed.
func TestLandEmptyChartStampsAnchor(t *testing.T) {
	newRepo(t)

	if _, err := CutBranch("chart/empty", Mainline()); err != nil {
		t.Fatalf("CutBranch chart/empty: %v", err)
	}
	// No checkout, no artifact commit — the branch sits exactly at base.

	sha, err := Land("chart/empty", "Chart: empty", LandLocal)
	if err != nil {
		t.Fatalf("Land empty chart: %v", err)
	}
	if sha == "" {
		t.Fatal("Land returned an empty SHA")
	}
	// The anchor exists on mainline (root + empty squash == 2) and is derivable.
	if n := mustGit(t, "rev-list", "--count", Mainline()); n != "2" {
		t.Errorf("mainline commit count = %s, want 2 (root + empty anchor)", n)
	}
	if lb, err := LandedBase("Chart: empty"); err != nil || lb != sha {
		t.Errorf("LandedBase = (%s, %v), want (%s, nil)", lb, err, sha)
	}
	// Idempotent: a second Land no-ops on the trailer and adds no commit.
	sha2, err := Land("chart/empty", "Chart: empty", LandLocal)
	if err != nil || sha2 != sha {
		t.Errorf("idempotent Land = (%s, %v), want (%s, nil)", sha2, err, sha)
	}
	if n := mustGit(t, "rev-list", "--count", Mainline()); n != "2" {
		t.Errorf("idempotent Land added a commit: count = %s, want 2", n)
	}
}

// Regression for the phantom-staged-revert bug: when Mainline is checked
// out in a worktree, Land must fast-forward THROUGH that worktree so its index and
// working tree move with the ref — not leave it stranded at the pre-land commit,
// which showed up as the land's reverse-diff staged in the main checkout.
func TestLandUpdatesMainlineWorktree(t *testing.T) {
	newRepo(t)
	mainWT := mustGit(t, "rev-parse", "--show-toplevel")

	if _, err := CutBranch("chart/x", Mainline()); err != nil {
		t.Fatalf("CutBranch: %v", err)
	}

	// Accrue the artifact on chart/x in its OWN linked worktree, leaving Mainline
	// checked out and clean in the root worktree — the real land topology.
	effortWT := t.TempDir()
	mustGit(t, "worktree", "add", effortWT, "chart/x")
	t.Chdir(effortWT)
	writeFile(t, "artifact.txt", "defog output\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "an artifact")

	if _, err := Land("chart/x", "Chart: x", LandLocal); err != nil {
		t.Fatalf("Land: %v", err)
	}

	// The Mainline worktree moved WITH the ref: the landed file is present in its
	// working tree, and its status is clean — no phantom staged reverse-diff.
	if _, err := os.Stat(filepath.Join(mainWT, "artifact.txt")); err != nil {
		t.Errorf("mainline worktree not fast-forwarded (artifact.txt missing): %v", err)
	}
	if st := mustGit(t, "-C", mainWT, "status", "--porcelain"); st != "" {
		t.Errorf("mainline worktree left dirty (stranded revert): %q", st)
	}
}

// Per-effort isolation: two efforts each get their own worktree, so their
// concurrent checkouts/commits land on their own branch without colliding on the
// shared working tree or its index.lock (AC1/AC2). Each worktree has an
// independent HEAD and index; the main working tree is untouched.
func TestWorktreesIsolateConcurrentEfforts(t *testing.T) {
	newRepo(t)
	mainHead := mustGit(t, "rev-parse", "HEAD")

	for _, b := range []string{"chart/a", "chart/b"} {
		if _, err := CutBranch(b, Mainline()); err != nil {
			t.Fatalf("CutBranch %s: %v", b, err)
		}
	}

	base := t.TempDir()
	wtA := filepath.Join(base, "a")
	wtB := filepath.Join(base, "b")
	if err := AddWorktree(wtA, "chart/a"); err != nil {
		t.Fatalf("AddWorktree a: %v", err)
	}
	if err := AddWorktree(wtB, "chart/b"); err != nil {
		t.Fatalf("AddWorktree b: %v", err)
	}

	// Concurrent commits, one per worktree: independent indexes ⇒ no index.lock
	// race, no blocked checkout. -C targets each worktree without touching cwd.
	errs := make(chan error, 2)
	for wt, name := range map[string]string{wtA: "a", wtB: "b"} {
		wt, name := wt, name
		go func() {
			if err := os.WriteFile(filepath.Join(wt, "art-"+name+".txt"), []byte(name), 0o644); err != nil {
				errs <- err
				return
			}
			if _, err := run("-C", wt, "add", "."); err != nil {
				errs <- err
				return
			}
			_, err := run("-C", wt, "commit", "-m", "artifact "+name)
			errs <- err
		}()
	}
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent worktree commit: %v", err)
		}
	}

	// Each branch advanced independently; the shared (main) working tree never moved.
	if head := mustGit(t, "rev-parse", "HEAD"); head != mainHead {
		t.Errorf("main working tree HEAD moved: %s != %s", head, mainHead)
	}
	for _, br := range []string{"chart/a", "chart/b"} {
		if tip := mustGit(t, "rev-parse", br); tip == mainHead {
			t.Errorf("%s did not advance past base", br)
		}
	}

	if err := RemoveWorktree(wtA); err != nil {
		t.Errorf("RemoveWorktree a: %v", err)
	}
	if _, err := os.Stat(wtA); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("worktree a not removed: %v", err)
	}
}

// The production topology: the effort's Land runs from a LINKED
// worktree while `main` stays checked out in the primary tree. `git branch -f
// main` refuses that ("cannot force update the branch main used by worktree");
// update-ref moves it. This is the case TestLandRoundTrip couldn't prove because
// it freed main by checking out chart/x in the single primary tree.
func TestLandFromLinkedWorktree(t *testing.T) {
	newRepo(t) // main checked out here, in the primary tree
	primaryHead := mustGit(t, "rev-parse", "HEAD")

	if _, err := CutBranch("chart/x", Mainline()); err != nil {
		t.Fatalf("CutBranch: %v", err)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	if err := AddWorktree(wt, "chart/x"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	t.Chdir(wt) // the effort's cwd IS the linked worktree (ADR 0010 seam)
	writeFile(t, "artifact.txt", "defog output\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "an artifact")

	sha, err := Land("chart/x", "Chart: x", LandLocal)
	if err != nil {
		t.Fatalf("Land from linked worktree: %v", err) // `branch -f main` would fail here
	}
	if head := mustGit(t, "rev-parse", Mainline()); head != sha {
		t.Errorf("mainline not fast-forwarded: %s != %s", head, sha)
	}
	if sha == primaryHead {
		t.Error("mainline did not advance past its pre-land tip")
	}
}

// Land's mainline ff is a compare-and-swap against the base it
// captured. Two efforts off the same base race; the first moves mainline, so the
// second's swap (expecting the old base) must FAIL rather than clobber the
// winner. Tested at the ff step directly, since a serialized Land re-captures
// base and the clobber only exists under genuine concurrency.
func TestFfMainlineIsCompareAndSwap(t *testing.T) {
	newRepo(t)
	base0 := mustGit(t, "rev-parse", Mainline())

	// Two branches cut from the same base, each with its own squash commit.
	for _, b := range []string{"winner", "loser"} {
		mustGit(t, "checkout", "-b", b, base0)
		writeFile(t, b+".txt", b)
		mustGit(t, "add", ".")
		mustGit(t, "commit", "-m", b)
	}
	mustGit(t, "checkout", Mainline())

	// Winner lands: swap succeeds because mainline is still base0.
	if _, err := run(ffMainlineArgs(Mainline(), "winner", base0)...); err != nil {
		t.Fatalf("winner ff: %v", err)
	}
	winnerTip := mustGit(t, "rev-parse", "winner")
	if head := mustGit(t, "rev-parse", Mainline()); head != winnerTip {
		t.Fatalf("winner did not land: mainline %s != %s", head, winnerTip)
	}

	// Loser still expects base0 — the swap must fail, not overwrite the winner.
	if _, err := run(ffMainlineArgs(Mainline(), "loser", base0)...); err == nil {
		t.Fatal("stale ff must fail once mainline moved off the expected base")
	}
	if head := mustGit(t, "rev-parse", Mainline()); head != winnerTip {
		t.Errorf("stale ff clobbered mainline: %s != winner %s", head, winnerTip)
	}
}

// A rebase conflict returns the typed error carrying the base SHA, aborts the
// rebase (no in-progress state), and leaves mainline untouched.
func TestLandRebaseConflict(t *testing.T) {
	newRepo(t)

	mustGit(t, "checkout", "-b", "chart/y")
	writeFile(t, "clash.txt", "from the branch\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "branch edit")

	mustGit(t, "checkout", Mainline())
	writeFile(t, "clash.txt", "from mainline\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "mainline edit")
	baseBefore := mustGit(t, "rev-parse", Mainline())

	_, err := Land("chart/y", "Chart: y", LandLocal)
	if err == nil {
		t.Fatal("Land onto a conflicting mainline must fail")
	}
	var conflict *RebaseConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("Land error = %v, want *RebaseConflict", err)
	}
	if conflict.Base != baseBefore {
		t.Errorf("conflict.Base = %s, want the mainline tip %s", conflict.Base, baseBefore)
	}
	// No in-progress rebase left behind.
	for _, d := range []string{".git/rebase-merge", ".git/rebase-apply"} {
		if _, err := os.Stat(filepath.FromSlash(d)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("rebase not aborted: %s still present", d)
		}
	}
	// Mainline is untouched by a conflicting land.
	if after := mustGit(t, "rev-parse", Mainline()); after != baseBefore {
		t.Errorf("mainline moved on a conflict: %s != %s", after, baseBefore)
	}
}

// A linked tree stopped mid-rebase on effort/<id> has a detached HEAD, so
// WorktreeOn misses it; RebasingOn finds it by its rebase state's head-name
// (either backend), and AbortRebase on its dir puts it back on the branch.
func TestRebasingOnFindsAStoppedRebase(t *testing.T) {
	for _, backend := range []string{"--merge", "--apply"} {
		t.Run(backend, func(t *testing.T) {
			newRepo(t)
			mustGit(t, "branch", "effort/mc-a")
			wt := filepath.Join(t.TempDir(), "wt")
			mustGit(t, worktreeAddArgs(wt, "effort/mc-a")...)
			writeFile(t, "clash.txt", "from main\n")
			mustGit(t, "add", ".")
			mustGit(t, "commit", "-m", "main edit")
			writeFile(t, filepath.Join(wt, "clash.txt"), "from the effort\n")
			mustGit(t, "-C", wt, "add", ".")
			mustGit(t, "-C", wt, "commit", "-m", "effort edit")
			if _, err := run("-C", wt, "rebase", backend, "main"); err == nil {
				t.Fatal("rebase must stop on the conflict")
			}

			if _, ok, err := WorktreeOn("effort/mc-a"); ok || err != nil {
				t.Fatalf("WorktreeOn found the detached tree (%v), the premise is gone", err)
			}
			h, ok, err := RebasingOn("effort/mc-a")
			if err != nil || !ok || h.Path != wt {
				t.Fatalf("RebasingOn = %+v, %v, %v; want %s", h, ok, err, wt)
			}
			if _, ok, _ := RebasingOn("effort/mc-b"); ok {
				t.Error("RebasingOn matched another branch")
			}
			if r, err := Rebasing(); err != nil || len(r) != 1 || r[wt] != "effort/mc-a" {
				t.Errorf("Rebasing = %v, %v; want %s → effort/mc-a", r, err, wt)
			}
			if ws, err := Worktrees(); err != nil || len(ws) != 2 || ws[0].Branch != Mainline() || ws[1] != (Worktree{Path: wt}) {
				t.Errorf("Worktrees = %+v, %v; want the main checkout on %s, then %s detached", ws, err, Mainline(), wt)
			}

			if err := AbortRebase(h.Path); err != nil {
				t.Fatal(err)
			}
			if _, ok, _ := RebasingOn("effort/mc-a"); ok {
				t.Error("rebase still found after the abort")
			}
			if h, ok, _ := WorktreeOn("effort/mc-a"); !ok || h.Path != wt {
				t.Errorf("after the abort WorktreeOn = %+v, %v; want %s back on the branch", h, ok, wt)
			}
		})
	}
}

// A rebase that REFUSES TO START (here, a dirty working tree) is not a
// merge conflict — it leaves no in-progress rebase — so Land surfaces it raw
// instead of mislabeling it *RebaseConflict.
func TestLandCleanRebaseFailureNotConflict(t *testing.T) {
	newRepo(t)

	// chart/z diverges by adding a new file only, so checking it out doesn't clash
	// with a dirty README.
	mustGit(t, "checkout", "-b", "chart/z")
	writeFile(t, "z.txt", "z\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "z work")

	// Mainline advances so chart/z isn't already an ancestor (Land reaches rebase).
	mustGit(t, "checkout", Mainline())
	writeFile(t, "main.txt", "m\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "mainline work")

	// A dirty tracked file in chart/z's checkout makes `git rebase` refuse to start —
	// no conflict state. main is checked out nowhere, so this isn't *MainDirty.
	mustGit(t, "checkout", "chart/z")
	writeFile(t, "README", "uncommitted edit\n")

	_, err := Land("chart/z", "Chart: z", LandLocal)
	if err == nil {
		t.Fatal("Land must fail when rebase can't start")
	}
	var conflict *RebaseConflict
	var dirty *MainDirty
	if errors.As(err, &conflict) || errors.As(err, &dirty) {
		t.Errorf("clean rebase failure mislabeled as typed: %v", err)
	}
	for _, d := range []string{".git/rebase-merge", ".git/rebase-apply"} {
		if _, err := os.Stat(filepath.FromSlash(d)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("rebase state left behind: %s", d)
		}
	}
}

// addADR commits docs/adr/<name> on the current branch; a name with a "/" is
// the whole path, for ADRs in another docs/adr/.
func addADR(t *testing.T, name string) {
	t.Helper()
	if !strings.Contains(name, "/") {
		name = "docs/adr/" + name
	}
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, name, "# "+name+"\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "adr "+name)
}

// Land's in-lock clash check (ADR 0030 §1, ADR 0069) flags only numbers the branch
// ADDS that the locked base already holds under another filename: *NumberClash, for
// a Chart and a Checkpoint branch alike, with main and the branch untouched.
func TestLandADRNumberClashes(t *testing.T) {
	for _, kind := range []string{"chart", "checkpoint"} {
		branch, trailer := BranchName(kind, "x"), TrailerFor(kind, "x")
		t.Run(kind+": same number, different slugs clashes", func(t *testing.T) {
			newRepo(t)
			addADR(t, "0001-base.md")
			mustGit(t, "checkout", "-b", branch)
			addADR(t, "0002-branch-side.md")
			tip := mustGit(t, "rev-parse", branch)
			mustGit(t, "checkout", Mainline())
			addADR(t, "0002-main-side.md")
			head := mustGit(t, "rev-parse", Mainline())

			_, err := Land(branch, trailer, LandLocal)
			var clash *NumberClash
			if !errors.As(err, &clash) {
				t.Fatalf("Land = %v, want *NumberClash", err)
			}
			if len(clash.Clashes) != 1 || !strings.Contains(clash.Clashes[0], "docs/adr/0002-branch-side.md") || !strings.Contains(clash.Clashes[0], "docs/adr/0002-main-side.md") {
				t.Errorf("clashes = %v, want the one 0002 pair", clash.Clashes)
			}
			if clash.Base != head {
				t.Errorf("base = %s, want mainline %s", clash.Base, head)
			}
			if after := mustGit(t, "rev-parse", Mainline()); after != head {
				t.Errorf("main moved on a clash: %s != %s", after, head)
			}
			if after := mustGit(t, "rev-parse", branch); after != tip {
				t.Errorf("branch moved on a clash: %s != %s", after, tip)
			}
		})
	}
	t.Run("distinct numbers do not clash and land idempotently", func(t *testing.T) {
		newRepo(t)
		mustGit(t, "checkout", "-b", "chart/x")
		addADR(t, "0003-chart-side.md")
		mustGit(t, "checkout", Mainline())
		addADR(t, "0002-main-side.md")

		sha, err := Land("chart/x", "Chart: x", LandLocal)
		if err != nil {
			t.Fatalf("Land: %v", err)
		}
		// Re-run (crash roll-forward) sees the Chart's own file on main, not a clash.
		if again, err := Land("chart/x", "Chart: x", LandLocal); err != nil || again != sha {
			t.Errorf("re-Land = (%s, %v), want (%s, nil)", again, err, sha)
		}
	})
	t.Run("pre-existing mainline duplicates never block", func(t *testing.T) {
		newRepo(t)
		addADR(t, "0002-a.md")
		addADR(t, "0002-b.md")
		mustGit(t, "checkout", "-b", "chart/x")
		addADR(t, "0003-fresh.md")
		mustGit(t, "checkout", Mainline())

		if _, err := Land("chart/x", "Chart: x", LandLocal); err != nil {
			t.Errorf("Land: %v", err)
		}
	})
	t.Run("each docs/adr/ is its own number space", func(t *testing.T) {
		newRepo(t)
		addADR(t, "0085-root.md")
		addADR(t, "docs/adr/0029-mugicrew-tombstone.md")
		addADR(t, "apps/mugicrew/docs/adr/0030-base.md")
		mustGit(t, "checkout", "-b", "chart/x")
		addADR(t, "apps/mugicrew/docs/adr/0085-app.md")        // root holds 0085: no clash
		addADR(t, "apps/other/docs/adr/0001-fresh.md")         // unregistered directory
		addADR(t, "apps/mugicrew/docs/adr/0030-clash.md")      // same directory: clash
		addADR(t, "0029-root.md")                              // root Tombstone holds 0029
		addADR(t, "apps/mugicrew/docs/adr/sub/0030-nested.md") // nested: not an ADR
		mustGit(t, "checkout", Mainline())
		addADR(t, "apps/other/docs/adr/0001-main.md")
		addADR(t, "apps/mugicrew/docs/adr/sub/0030-other.md")

		var clash *NumberClash
		if _, err := Land("chart/x", "Chart: x", LandLocal); !errors.As(err, &clash) {
			t.Fatalf("Land = %v, want *NumberClash", err)
		}
		want := []string{
			"apps/mugicrew/docs/adr/0030-clash.md (rule **/docs/adr/{n:4}-*.md) (vs mainline apps/mugicrew/docs/adr/0030-base.md)",
			"apps/other/docs/adr/0001-fresh.md (rule **/docs/adr/{n:4}-*.md) (vs mainline apps/other/docs/adr/0001-main.md)",
			"docs/adr/0029-root.md (rule **/docs/adr/{n:4}-*.md) (vs mainline docs/adr/0029-mugicrew-tombstone.md)",
		}
		if !slices.Equal(clash.Clashes, want) {
			t.Errorf("clashes = %q, want %q", clash.Clashes, want)
		}
	})
	t.Run("renaming onto a taken number clashes", func(t *testing.T) {
		newRepo(t)
		addADR(t, "0001-base.md")
		mustGit(t, "checkout", "-b", "chart/x")
		mustGit(t, "mv", "docs/adr/0001-base.md", "docs/adr/0002-base.md")
		mustGit(t, "commit", "-m", "renumber")
		mustGit(t, "checkout", Mainline())
		addADR(t, "0002-main-side.md")

		var clash *NumberClash
		if _, err := Land("chart/x", "Chart: x", LandLocal); !errors.As(err, &clash) || len(clash.Clashes) != 1 {
			t.Errorf("Land = %v, want *NumberClash on the renamed 0002", err)
		}
	})
}

// landRepo is a scratch repo where checkpoint/x has one commit and main has moved
// on, so a land really rebases; the cwd is a tree detached at the container's tip,
// as route.Detached places a land step (ADR 0102 §8). It returns the container tip.
func landRepo(t *testing.T) (pre string) {
	t.Helper()
	newRepo(t)
	mustGit(t, "checkout", "-b", "checkpoint/x")
	writeFile(t, "x.txt", "x\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "x work")
	mustGit(t, "checkout", Mainline())
	writeFile(t, "main.txt", "m\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "mainline work")
	wt, teardown, err := NewDetachedWorktree("checkpoint/x")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(teardown)
	t.Chdir(wt)
	return mustGit(t, "rev-parse", "checkpoint/x")
}

// preCommitHook installs a pre-commit hook that runs sh in place of Land's squash commit.
func preCommitHook(t *testing.T, sh string) {
	t.Helper()
	hooks := t.TempDir()
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\n"+sh+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, "config", "core.hooksPath", hooks)
}

// Land rebases and squashes in a detached tree and never writes the container
// (Spec D5, ADR 0102 §8): main gets the squash, the container ref stays
// at its pre-land tip, so an Approval's Commit: pin still matches it, and no tree
// ever checks the container out.
func TestLandRunsDetachedAndNeverWritesContainer(t *testing.T) {
	pre := landRepo(t)
	sha, err := Land("checkpoint/x", "Checkpoint: x", LandLocal)
	if err != nil {
		t.Fatalf("Land: %v", err)
	}
	if head := mustGit(t, "rev-parse", Mainline()); head != sha {
		t.Errorf("main = %s, want the land %s", head, sha)
	}
	if got := mustGit(t, "show", Mainline()+":x.txt"); got != "x" {
		t.Errorf("main:x.txt = %q, want the container's work", got)
	}
	if tip := mustGit(t, "rev-parse", "refs/heads/checkpoint/x"); tip != pre {
		t.Errorf("container = %s, want untouched at %s", tip, pre)
	}
	if !ApprovedAtHead(CommitPin(pre), "checkpoint/x") {
		t.Error("an Approval pinned at the pre-land tip must still be at head")
	}
	if _, ok, _ := WorktreeOn("checkpoint/x"); ok {
		t.Error("no tree may hold the container")
	}
	if _, err := run("symbolic-ref", "--quiet", "HEAD"); err == nil {
		t.Error("the land tree must stay detached")
	}
}

// A container moved off its pin mid-land (a human committing in a checkout of it,
// ADR 0106 §3) is refused under the lock: main does not move, the moved commit
// stays on the container, and the next Land lands it.
func TestLandRefusesWhenContainerMovesOffPin(t *testing.T) {
	pre := landRepo(t)
	head := mustGit(t, "rev-parse", Mainline())
	moved := mustGit(t, "commit-tree", pre+"^{tree}", "-p", pre, "-m", "a human commit mid-land")
	preCommitHook(t, "git update-ref refs/heads/checkpoint/x "+moved)

	if _, err := Land("checkpoint/x", "Checkpoint: x", LandLocal); err == nil || !strings.Contains(err.Error(), "moved off") {
		t.Fatalf("Land = %v, want the moved-off-pin refusal", err)
	}
	if after := mustGit(t, "rev-parse", Mainline()); after != head {
		t.Errorf("main moved on a refused land: %s != %s", after, head)
	}
	if tip := mustGit(t, "rev-parse", "checkpoint/x"); tip != moved {
		t.Errorf("container = %s, want the mid-land commit %s kept", tip, moved)
	}

	mustGit(t, "config", "--unset", "core.hooksPath")
	if _, err := Land("checkpoint/x", "Checkpoint: x", LandLocal); err != nil {
		t.Fatalf("re-Land: %v", err)
	}
	if tip := mustGit(t, "rev-parse", "checkpoint/x"); tip != moved {
		t.Errorf("container = %s, want untouched at %s", tip, moved)
	}
}

// A Land that fails after the rebase (here a pre-commit hook refuses the squash)
// leaves the container at its pre-land tip, so the Approval's Commit: pin is still
// at head; main doesn't move, and once the cause is gone the same Land succeeds.
func TestLandFailureAfterRebaseLeavesContainer(t *testing.T) {
	pre := landRepo(t)
	head := mustGit(t, "rev-parse", Mainline())
	preCommitHook(t, "exit 1")

	if _, err := Land("checkpoint/x", "Checkpoint: x", LandLocal); err == nil || !strings.Contains(err.Error(), "git commit") {
		t.Fatalf("Land = %v, want the refused squash commit's error", err)
	}
	if !ApprovedAtHead(CommitPin(pre), "checkpoint/x") {
		t.Error("ApprovedAtHead must hold after a failed land")
	}
	if after := mustGit(t, "rev-parse", Mainline()); after != head {
		t.Errorf("main moved on a failed land: %s != %s", after, head)
	}

	mustGit(t, "config", "--unset", "core.hooksPath")
	if _, err := Land("checkpoint/x", "Checkpoint: x", LandLocal); err != nil {
		t.Fatalf("re-Land after the cause is gone: %v", err)
	}
}

// Compose produces the squash LandLocal would land and moves neither main nor
// the container.
func TestComposeMovesNothingAndMatchesLandLocal(t *testing.T) {
	pre := landRepo(t)
	base := mustGit(t, "rev-parse", Mainline())

	composed, gotBase, gotPin, err := Compose("checkpoint/x", "Checkpoint: x", "", "")
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if gotBase != base || gotPin != pre {
		t.Errorf("Compose (base, pin) = (%s, %s), want (%s, %s)", gotBase, gotPin, base, pre)
	}
	if main := mustGit(t, "rev-parse", Mainline()); main != base {
		t.Errorf("Compose moved main to %s", main)
	}
	if tip := mustGit(t, "rev-parse", "checkpoint/x"); tip != pre {
		t.Errorf("Compose moved the container to %s", tip)
	}
	wantTree := mustGit(t, "rev-parse", composed+"^{tree}")

	local, err := Land("checkpoint/x", "Checkpoint: x", LandLocal)
	if err != nil {
		t.Fatalf("LandLocal: %v", err)
	}
	if tree := mustGit(t, "rev-parse", local+"^{tree}"); tree != wantTree {
		t.Errorf("LandLocal tree %s != composed tree %s", tree, wantTree)
	}
}

func TestSameBaseLandFastForwardsToComposed(t *testing.T) {
	pre := landRepo(t)
	composed, base, _, err := Compose("checkpoint/x", "Checkpoint: x", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sha, err := Land("checkpoint/x", "Checkpoint: x", LandSameBase(composed, base, pre))
	if err != nil {
		t.Fatalf("Land same-base: %v", err)
	}
	if sha != composed {
		t.Errorf("landed %s, want the composed %s", sha, composed)
	}
	if main := mustGit(t, "rev-parse", Mainline()); main != composed {
		t.Errorf("main = %s, want the composed %s", main, composed)
	}
}

func TestSameBaseLandRefusesMovedBase(t *testing.T) {
	pre := landRepo(t)
	composed, base, _, err := Compose("checkpoint/x", "Checkpoint: x", "", "")
	if err != nil {
		t.Fatal(err)
	}
	moved := mustGit(t, "commit-tree", base+"^{tree}", "-p", base, "-m", "main moved")
	mustGit(t, "update-ref", "refs/heads/"+Mainline(), moved)

	_, err = Land("checkpoint/x", "Checkpoint: x", LandSameBase(composed, base, pre))
	var bm *BaseMoved
	if !errors.As(err, &bm) {
		t.Fatalf("Land = %v, want *BaseMoved", err)
	}
	if main := mustGit(t, "rev-parse", Mainline()); main != moved {
		t.Errorf("main = %s, want untouched at %s", main, moved)
	}
}

func TestSameBaseLandRefusesMovedContainer(t *testing.T) {
	pre := landRepo(t)
	composed, base, _, err := Compose("checkpoint/x", "Checkpoint: x", "", "")
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, "update-ref", "refs/heads/checkpoint/x", mustGit(t, "commit-tree", pre+"^{tree}", "-p", pre, "-m", "moved"))

	_, err = Land("checkpoint/x", "Checkpoint: x", LandSameBase(composed, base, pre))
	var pm *PinMoved
	if !errors.As(err, &pm) {
		t.Fatalf("Land = %v, want *PinMoved", err)
	}
	if main := mustGit(t, "rev-parse", Mainline()); main != base {
		t.Errorf("main moved on a refused land: %s", main)
	}
}

// LandedBase matches the WHOLE trailer line, not a substring: a short id
// must not resolve to a longer or dotted id's land under the same trailer key, or
// Land's trailer-first early return would skip a real land and report success.
func TestLandedBaseMatchesWholeTrailerLine(t *testing.T) {
	newRepo(t)

	for _, id := range []string{"xy", "p.2"} {
		if _, err := CutBranch("chart/"+id, Mainline()); err != nil {
			t.Fatalf("CutBranch chart/%s: %v", id, err)
		}
		if _, err := Land("chart/"+id, "Chart: "+id, LandLocal); err != nil {
			t.Fatalf("Land chart/%s: %v", id, err)
		}
	}
	for _, short := range []string{"Chart: x", "Chart: p", "Chart: p."} {
		if lb, err := LandedBase(short); err == nil {
			t.Errorf("LandedBase(%q) = %s, want not-landed (prefix of a longer id)", short, lb)
		}
	}

	// Land of the short id really lands instead of no-oping on xy's trailer.
	if _, err := CutBranch("chart/x", Mainline()); err != nil {
		t.Fatalf("CutBranch chart/x: %v", err)
	}
	sha, err := Land("chart/x", "Chart: x", LandLocal)
	if err != nil {
		t.Fatalf("Land chart/x: %v", err)
	}
	if n := mustGit(t, "rev-list", "--count", Mainline()); n != "4" {
		t.Errorf("mainline commit count = %s, want 4 (root + xy + p.2 + x); Land no-oped", n)
	}
	if lb, err := LandedBase("Chart: x"); err != nil || lb != sha {
		t.Errorf("LandedBase(Chart: x) = (%s, %v), want (%s, nil)", lb, err, sha)
	}
}

// The revert trailer is the land trailer prefixed "Reverts-", so the anchored
// LandedBase grep of one can never match the other (ADR 0047 §5).
func TestRevertsTrailer(t *testing.T) {
	if got := RevertsTrailer("checkpoint", "mc-x"); got != "Reverts-Checkpoint: mc-x" {
		t.Errorf("RevertsTrailer = %q", got)
	}
}

// Revert's tree is a merge-tree of squash^ onto mainline with squash as the merge
// base (undo exactly squash's diff), and its commit is a plumbing commit-tree with
// mainline as sole parent — no index, worktree or HEAD touched, like Anchor.
func TestRevertArgs(t *testing.T) {
	if got, want := revertTreeArgs("base", "sq"), []string{"merge-tree", "--write-tree", "--name-only", "--merge-base=sq", "base", "sq^"}; !slices.Equal(got, want) {
		t.Errorf("revertTreeArgs = %v, want %v", got, want)
	}
	if got, want := revertCommitArgs("tree", "base", "feat(app): add x (KEY-1)", "Reverts-Checkpoint: x", "g1"), []string{"commit-tree", "tree", "-p", "base", "-m", "revert: feat(app): add x (KEY-1)", "-m", "Reverts-Checkpoint: x\nGraph: g1"}; !slices.Equal(got, want) {
		t.Errorf("revertCommitArgs = %v, want %v", got, want)
	}
}

// landCheckpoint lands a one-file checkpoint/<id> onto mainline for the revert tests.
func landCheckpoint(t *testing.T, id, file string) string {
	t.Helper()
	mustGit(t, "branch", "checkpoint/"+id, Mainline())
	mustGit(t, "checkout", "checkpoint/"+id)
	writeFile(t, file, id+"\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", id)
	mustGit(t, "checkout", Mainline())
	sha, err := Land("checkpoint/"+id, "Checkpoint: "+id, LandLocal)
	if err != nil {
		t.Fatalf("Land %s: %v", id, err)
	}
	mustGit(t, "checkout", Mainline()) // Land leaves HEAD on the branch; Revert then ffs through main's worktree
	return sha
}

// A Revert of a squash NOT at HEAD puts exactly one forward commit on mainline that
// undoes only that squash, carries the trailer, leaves earlier history and the main
// worktree untouched, and is idempotent on the trailer. LandedBase of the target
// still reads the original squash (the anchored grep ignores Reverts-Checkpoint:).
func TestRevertForwardAndIdempotent(t *testing.T) {
	newRepo(t)
	squash := landCheckpoint(t, "a", "a.txt")
	later := landCheckpoint(t, "b", "b.txt")

	sha, err := Revert(squash, "Reverts-Checkpoint: a")
	if err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if head := mustGit(t, "rev-parse", Mainline()); head != sha {
		t.Errorf("Revert sha %s != mainline HEAD %s", sha, head)
	}
	if parent := mustGit(t, "rev-parse", sha+"^"); parent != later {
		t.Errorf("revert parent = %s, want prior mainline %s (forward-only)", parent, later)
	}
	if d := mustGit(t, "diff", "--name-status", later, sha); d != "D\ta.txt" {
		t.Errorf("revert diff = %q, want only a.txt deleted", d)
	}
	if lb, err := LandedBase("Reverts-Checkpoint: a"); err != nil || lb != sha {
		t.Errorf("LandedBase(revert trailer) = (%s, %v), want (%s, nil)", lb, err, sha)
	}
	if lb, err := LandedBase("Checkpoint: a"); err != nil || lb != squash {
		t.Errorf("LandedBase(land trailer) = (%s, %v), want the original squash %s", lb, err, squash)
	}
	if st := mustGit(t, "status", "--porcelain"); st != "" {
		t.Errorf("main worktree left dirty after Revert:\n%s", st)
	}

	again, err := Revert(squash, "Reverts-Checkpoint: a")
	if err != nil || again != sha {
		t.Errorf("idempotent Revert = (%s, %v), want (%s, nil)", again, err, sha)
	}
	if n := mustGit(t, "rev-list", "--count", Mainline()); n != "4" {
		t.Errorf("mainline count = %s, want 4 (root + 2 lands + 1 revert)", n)
	}
}

// MainlineLog reads first-parent mainline newest first with each commit's land and
// revert trailer ids: a land, a legacy Spec anchor, a plain commit, a revert, and a merge
// whose second parent's trailer must not surface.
func TestMainlineLog(t *testing.T) {
	newRepo(t)
	land := landCheckpoint(t, "a", "a.txt")
	mustGit(t, "commit", "--allow-empty", "-m", "anchor Spec: s", "-m", "Spec: s\nGraph: g")
	anchor := mustGit(t, "rev-parse", "HEAD")
	commitFile(t, "p.txt", "plain\n")
	plain := mustGit(t, "rev-parse", "HEAD")
	mustGit(t, "commit", "--allow-empty", "-m", "land chart", "-m", "Chart: c")
	chart := mustGit(t, "rev-parse", "HEAD")
	mustGit(t, "commit", "--allow-empty", "-m", "stray", "-m", "Spec: x\n\nTip: t\nCheckpoint: z\nGraph: g")
	stray := mustGit(t, "rev-parse", "HEAD")
	rev, err := Revert(land, RevertsTrailer("checkpoint", "a"))
	if err != nil {
		t.Fatalf("Revert: %v", err)
	}
	mustGit(t, "checkout", "-b", "side")
	writeFile(t, "s.txt", "side\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "side", "-m", "Checkpoint: side")
	mustGit(t, "checkout", Mainline())
	mustGit(t, "merge", "--no-ff", "-m", "merge side", "side")
	merge := mustGit(t, "rev-parse", "HEAD")

	got, err := MainlineLog(200)
	if err != nil {
		t.Fatalf("MainlineLog: %v", err)
	}
	want := []MainlineCommit{
		{SHA: merge, Subject: "merge side"},
		{SHA: rev, Reverts: "a"},
		{SHA: stray, Kind: "checkpoint", Lands: "z", Subject: "stray"},
		{SHA: chart, Kind: "chart", Lands: "c", Subject: "land chart"},
		{SHA: plain, Subject: "edit p.txt"},
		{SHA: anchor, Kind: "spec", Lands: "s"},
		{SHA: land, Kind: "checkpoint", Lands: "a"},
		{SHA: mustGit(t, "rev-parse", land+"^")},
	}
	if len(got) != len(want) {
		t.Fatalf("MainlineLog = %d commits, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.SHA != w.SHA || g.Kind != w.Kind || g.Lands != w.Lands || g.Reverts != w.Reverts || g.Time.IsZero() || (w.Subject != "" && g.Subject != w.Subject) {
			t.Errorf("commit %d = %+v, want %+v (with a time)", i, g, w)
		}
	}
	if got, _ := MainlineLog(2); len(got) != 2 || got[0].SHA != merge {
		t.Errorf("MainlineLog(n=2) = %+v, want the newest 2", got)
	}
}

// ADR 0049: a revert that later lands built on fails as *RevertConflict carrying
// mainline HEAD and the conflicted paths; mainline, HEAD and the worktree are left
// byte-for-byte alone with no in-progress revert. A bad squash is a raw error.
func TestRevertConflictReportsPathsAndWritesNothing(t *testing.T) {
	newRepo(t)
	squash := landCheckpoint(t, "a", "a.txt")
	mustGit(t, "branch", "checkpoint/b", Mainline())
	mustGit(t, "checkout", "checkpoint/b")
	writeFile(t, "a.txt", "b built on a\n") // a later land on the same lines
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "b")
	mustGit(t, "checkout", Mainline())
	if _, err := Land("checkpoint/b", "Checkpoint: b", LandLocal); err != nil {
		t.Fatalf("Land b: %v", err)
	}
	mustGit(t, "checkout", Mainline())
	before := mustGit(t, "rev-parse", Mainline())

	_, err := Revert(squash, "Reverts-Checkpoint: a")
	var conflict *RevertConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("Revert err = %v, want *RevertConflict", err)
	}
	if conflict.Base != before || !slices.Equal(conflict.Paths, []string{"a.txt"}) {
		t.Errorf("conflict = %+v, want Base %s Paths [a.txt]", conflict, before)
	}
	if after := mustGit(t, "rev-parse", Mainline()); after != before {
		t.Errorf("mainline moved %s → %s on a conflicting revert", before, after)
	}
	if st := mustGit(t, "status", "--porcelain"); st != "" {
		t.Errorf("worktree dirty after a conflicting revert:\n%s", st)
	}
	if _, err := os.Stat(filepath.Join(mustGit(t, "rev-parse", "--git-dir"), "REVERT_HEAD")); err == nil {
		t.Error("a revert was left in progress")
	}

	if _, err := Revert("deadbeef", "Reverts-Checkpoint: x"); err == nil || errors.As(err, &conflict) {
		t.Errorf("bad squash err = %v, want a raw error, never *RevertConflict", err)
	}
}

// merge-tree --name-only's conflicted-file section is the lines between the tree OID
// and the first blank line; a path conflicted in several stages is reported once.
func TestConflictPaths(t *testing.T) {
	out := "tree\nb.txt\na.txt\nb.txt\n\nAuto-merging a.txt\nCONFLICT (content): a.txt\n"
	if got, want := conflictPaths(out), []string{"a.txt", "b.txt"}; !slices.Equal(got, want) {
		t.Errorf("conflictPaths = %v, want %v", got, want)
	}
}

// The reapply is a plumbing commit of the revert's parent tree on top of the revert,
// and the seed moves the branch with a CAS from the revert — no HEAD switch.
func TestReapplyArgs(t *testing.T) {
	if got, want := reapplyCommitArgs("rv", "revert: feat(app): add x (KEY-1)"), []string{"commit-tree", "rv~1^{tree}", "-p", "rv", "-m", "feat(app): add x (KEY-1)"}; !slices.Equal(got, want) {
		t.Errorf("reapplyCommitArgs = %v, want %v", got, want)
	}
	// A revert written before conventional subjects has no "revert: " prefix.
	if got, want := reapplyCommitArgs("rv", "revert abc123"), []string{"commit-tree", "rv~1^{tree}", "-p", "rv", "-m", "revert: revert abc123"}; !slices.Equal(got, want) {
		t.Errorf("legacy reapplyCommitArgs = %v, want %v", got, want)
	}
	if got, want := reapplyRefArgs("checkpoint/n", "s", "rv"), []string{"update-ref", "refs/heads/checkpoint/n", "s", "rv"}; !slices.Equal(got, want) {
		t.Errorf("reapplyRefArgs = %v, want %v", got, want)
	}
}

// ADR 0048 §4: with an unrelated land between the target and its revert, the
// successor branch cut at the revert takes one reapply commit that restores exactly
// the target's diff, leaves HEAD and the main worktree alone, and is skip-if-done.
func TestReapplySeedsSuccessorAtRevert(t *testing.T) {
	newRepo(t)
	squash := landCheckpoint(t, "a", "a.txt")
	landCheckpoint(t, "b", "b.txt") // unrelated land between target and revert
	revert, err := Revert(squash, "Reverts-Checkpoint: a")
	if err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if _, err := CutBranch("checkpoint/n", revert); err != nil {
		t.Fatalf("CutBranch: %v", err)
	}
	head := mustGit(t, "symbolic-ref", "HEAD")

	landSubject := mustGit(t, "log", "-1", "--format=%s", squash)
	if got, want := mustGit(t, "log", "-1", "--format=%s", revert), "revert: "+landSubject; got != want {
		t.Errorf("revert subject = %q, want %q", got, want)
	}
	sha, err := Reapply("checkpoint/n", revert)
	if err != nil {
		t.Fatalf("Reapply: %v", err)
	}
	if got := mustGit(t, "log", "-1", "--format=%s", sha); got != landSubject {
		t.Errorf("reapply subject = %q, want the land subject %q", got, landSubject)
	}
	if tip := mustGit(t, "rev-parse", "checkpoint/n"); tip != sha {
		t.Errorf("branch tip %s != reapply %s", tip, sha)
	}
	if parent := mustGit(t, "rev-parse", sha+"^"); parent != revert {
		t.Errorf("reapply parent = %s, want the revert %s", parent, revert)
	}
	if got, want := mustGit(t, "diff", revert, sha), mustGit(t, "diff", squash+"^", squash); got != want {
		t.Errorf("reapply diff %q != target diff %q", got, want)
	}
	if h := mustGit(t, "symbolic-ref", "HEAD"); h != head {
		t.Errorf("Reapply switched HEAD %s → %s", head, h)
	}
	if st := mustGit(t, "status", "--porcelain"); st != "" {
		t.Errorf("main worktree dirty after Reapply:\n%s", st)
	}

	again, err := Reapply("checkpoint/n", revert)
	if err != nil || again != sha {
		t.Errorf("re-run Reapply = (%s, %v), want the existing seed (%s, nil)", again, err, sha)
	}
	if n := mustGit(t, "rev-list", "--count", revert+"..checkpoint/n"); n != "1" {
		t.Errorf("seed commits = %s, want 1", n)
	}
}

// At HEAD the revert is still a forward commit — mainline never rewinds (ADR 0047 §4).
func TestRevertAtHeadIsForward(t *testing.T) {
	newRepo(t)
	squash := landCheckpoint(t, "a", "a.txt")

	sha, err := Revert(squash, "Reverts-Checkpoint: a")
	if err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if parent := mustGit(t, "rev-parse", sha+"^"); parent != squash {
		t.Errorf("at-HEAD revert parent = %s, want the squash %s", parent, squash)
	}
	if d := mustGit(t, "diff", "--name-status", squash, sha); d != "D\ta.txt" {
		t.Errorf("revert diff = %q, want a.txt deleted", d)
	}
}

// A conflicting revert (a later land edited the same lines) is an error that leaves
// mainline untouched — conflict handling itself is the fix-forward Build's.
func TestRevertConflictLeavesMainlineUntouched(t *testing.T) {
	newRepo(t)
	squash := landCheckpoint(t, "a", "a.txt")
	writeFile(t, "a.txt", "later edit\n")
	mustGit(t, "commit", "-am", "later")
	tip := mustGit(t, "rev-parse", Mainline())

	if _, err := Revert(squash, "Reverts-Checkpoint: a"); err == nil {
		t.Fatal("a conflicting revert must error")
	}
	if head := mustGit(t, "rev-parse", Mainline()); head != tip {
		t.Errorf("conflicting revert moved mainline: %s != %s", head, tip)
	}
}

// ---- the land lock and the dirty-main guard (ADR 0069) ----

// The 48/300 regression: many concurrent Lands, anchors and Reverts on one repo with
// main checked out all land, and main's index and tree are clean afterwards — the
// land lock serializes them, so none loses the CAS race or strands a reverse diff.
func TestConcurrentMovesOfMainAllLandClean(t *testing.T) {
	newRepo(t)
	mainWT := mustGit(t, "rev-parse", "--show-toplevel")
	effortWT := filepath.Join(t.TempDir(), "wt")
	mustGit(t, "worktree", "add", "--detach", effortWT)
	t.Chdir(effortWT) // Land detaches here; main stays checked out in mainWT

	commitOn := func(branch, file string) {
		mustGit(t, "checkout", "-b", branch, Mainline())
		writeFile(t, file, branch+"\n")
		mustGit(t, "add", ".")
		mustGit(t, "commit", "-m", branch)
	}
	const n = 4
	var squashes []string
	for i := range n {
		commitOn(fmt.Sprintf("checkpoint/r%d", i), fmt.Sprintf("r%d.txt", i))
		sha, err := Land(fmt.Sprintf("checkpoint/r%d", i), fmt.Sprintf("Checkpoint: r%d", i), LandLocal)
		if err != nil {
			t.Fatalf("pre-land r%d: %v", i, err)
		}
		squashes = append(squashes, sha)
	}
	for i := range n {
		commitOn(fmt.Sprintf("chart/c%d", i), fmt.Sprintf("c%d.txt", i))
	}
	mustGit(t, "checkout", "--detach")
	before, _ := strconv.Atoi(mustGit(t, "rev-list", "--count", Mainline()))

	var moves []func() (string, error)
	var trailers []string
	for i := range n {
		landTr, rev := fmt.Sprintf("Chart: c%d", i), fmt.Sprintf("Reverts-Checkpoint: r%d", i)
		branch, squash := fmt.Sprintf("chart/c%d", i), squashes[i]
		moves = append(moves,
			func() (string, error) { return Land(branch, landTr, LandLocal) },
			func() (string, error) { return Revert(squash, rev) })
		trailers = append(trailers, landTr, rev)
	}
	errs := make(chan error, len(moves))
	for _, m := range moves {
		go func() { _, err := m(); errs <- err }()
	}
	for range moves {
		if err := <-errs; err != nil {
			t.Errorf("concurrent move of main: %v", err)
		}
	}

	for _, tr := range trailers {
		if _, err := LandedBase(tr); err != nil {
			t.Errorf("%q did not land: %v", tr, err)
		}
	}
	if got := mustGit(t, "rev-list", "--count", Mainline()); got != strconv.Itoa(before+len(moves)) {
		t.Errorf("mainline count = %s, want %d (one commit per move)", got, before+len(moves))
	}
	if st := mustGit(t, "-C", mainWT, "status", "--porcelain"); st != "" {
		t.Errorf("main worktree dirty after concurrent moves:\n%s", st)
	}
	if head := mustGit(t, "-C", mainWT, "rev-parse", "HEAD"); head != mustGit(t, "rev-parse", Mainline()) {
		t.Errorf("main worktree HEAD %s stranded behind mainline", head)
	}
}

// A held land lock gives up after the (injected) timeout with an untyped error —
// transient, retried next tick — and moves nothing.
func TestLandLockTimeoutIsUntyped(t *testing.T) {
	newRepo(t)
	old := landLockTimeout
	landLockTimeout = 100 * time.Millisecond
	t.Cleanup(func() { landLockTimeout = old })
	held, release := make(chan struct{}), make(chan struct{})
	go withLandLock(func() (string, error) { close(held); <-release; return "", nil })
	<-held
	defer close(release)
	before := mustGit(t, "rev-parse", Mainline())

	_, err := Revert("deadbeef", "Reverts-Checkpoint: x")
	var dirty *MainDirty
	var rebase *RebaseConflict
	var revert *RevertConflict
	if err == nil || !strings.Contains(err.Error(), "still held") || errors.As(err, &dirty) || errors.As(err, &rebase) || errors.As(err, &revert) {
		t.Errorf("held lock err = %v, want an untyped timeout", err)
	}
	if after := mustGit(t, "rev-parse", Mainline()); after != before {
		t.Errorf("a timed-out move moved mainline %s → %s", before, after)
	}
}

// A dirty main worktree (a tracked change, or a non-ignored untracked file) refuses
// Land and Revert with *MainDirty and touches neither the branch nor
// main. Already-landed trailers still return their SHA over the dirt (write leg
// only), and Reapply, which never moves main, ignores it.
func TestDirtyMainRefusesEveryMoveOfMain(t *testing.T) {
	for name, c := range map[string]struct{ path string }{
		"tracked":   {"README"},
		"untracked": {"stray.txt"},
	} {
		t.Run(name, func(t *testing.T) {
			newRepo(t)
			mainWT := mustGit(t, "rev-parse", "--show-toplevel")
			squashA := landCheckpoint(t, "a", "a.txt")
			squashB := landCheckpoint(t, "b", "b.txt")
			rv, err := Revert(squashA, "Reverts-Checkpoint: a")
			if err != nil {
				t.Fatalf("Revert a: %v", err)
			}
			if _, err := CutBranch("checkpoint/n", rv); err != nil {
				t.Fatalf("CutBranch: %v", err)
			}
			mustGit(t, "branch", "chart/x", Mainline())
			effortWT := filepath.Join(t.TempDir(), "wt")
			mustGit(t, "worktree", "add", effortWT, "chart/x")
			t.Chdir(effortWT)
			writeFile(t, "x.txt", "x\n")
			mustGit(t, "add", ".")
			mustGit(t, "commit", "-m", "x")

			writeFile(t, filepath.Join(mainWT, c.path), "edit\n")
			mainTip, tip := mustGit(t, "rev-parse", Mainline()), mustGit(t, "rev-parse", "chart/x")

			for op, move := range map[string]func() (string, error){
				"Land":   func() (string, error) { return Land("chart/x", "Chart: x", LandLocal) },
				"Revert": func() (string, error) { return Revert(squashB, "Reverts-Checkpoint: b") },
			} {
				var md *MainDirty
				if _, err := move(); !errors.As(err, &md) || md.Path != mainWT {
					t.Errorf("%s over a dirty main = %v, want *MainDirty for %s", op, err, mainWT)
				} else if !slices.Contains(md.Files, c.path) || !strings.Contains(err.Error(), c.path) {
					t.Errorf("%s over a dirty main: Files = %v, err = %q, want %s named", op, md.Files, err, c.path)
				}
			}
			if got := mustGit(t, "rev-parse", Mainline()); got != mainTip {
				t.Errorf("mainline moved %s → %s over a dirty main", mainTip, got)
			}
			if got := mustGit(t, "rev-parse", "chart/x"); got != tip {
				t.Errorf("chart/x moved %s → %s over a dirty main", tip, got)
			}

			if sha, err := Land("checkpoint/a", "Checkpoint: a", LandLocal); err != nil || sha != squashA {
				t.Errorf("landed Land over dirt = (%s, %v), want (%s, nil)", sha, err, squashA)
			}
			if sha, err := Revert(squashA, "Reverts-Checkpoint: a"); err != nil || sha != rv {
				t.Errorf("landed Revert over dirt = (%s, %v), want (%s, nil)", sha, err, rv)
			}
			if sha, err := Reapply("checkpoint/n", rv); err != nil || sha == rv {
				t.Errorf("Reapply over dirt = (%s, %v), want a seed commit", sha, err)
			}
		})
	}
}

// More than 5 dirty paths: the message names the first 5, then " +N more".
func TestMainDirtyCapsListedFiles(t *testing.T) {
	newRepo(t)
	mainWT := mustGit(t, "rev-parse", "--show-toplevel")
	for i := 1; i <= 7; i++ {
		writeFile(t, filepath.Join(mainWT, fmt.Sprintf("f%d.txt", i)), "x\n")
	}
	err := mainClean()
	var md *MainDirty
	if !errors.As(err, &md) || len(md.Files) != 7 {
		t.Fatalf("mainClean = %v, want *MainDirty with 7 files", err)
	}
	if want := "(f1.txt, f2.txt, f3.txt, f4.txt, f5.txt, … +2 more)"; !strings.Contains(err.Error(), want) {
		t.Errorf("msg %q lacks %q", err, want)
	}
}

// commitFile writes name and commits it on the current branch.
func commitFile(t *testing.T, name, content string) {
	t.Helper()
	if dir := filepath.Dir(name); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, name, content)
	mustGit(t, "add", "-A")
	mustGit(t, "commit", "-m", "edit "+name)
}

// resolveRepo builds the post-resolve check's scene (ADR 0080 §3): a Chart
// branch whose land conflicted (Tip, pinned at filing) and a mainline that moved
// to base, then rebases the Chart onto base cleanly, as a correct resolve does.
// It leaves chart/x checked out and returns (base, tip).
func resolveRepo(t *testing.T) (base, tip string) {
	t.Helper()
	newRepo(t)
	commitFile(t, "shared.txt", "root\n")
	mustGit(t, "checkout", "-b", "chart/x")
	commitFile(t, "chart.txt", "chart\n")
	commitFile(t, "shared.txt", "chart side\n")
	tip = mustGit(t, "rev-parse", "HEAD")
	mustGit(t, "checkout", Mainline())
	commitFile(t, "main.txt", "mainline\n")
	base = mustGit(t, "rev-parse", "HEAD")
	mustGit(t, "checkout", "chart/x")
	mustGit(t, "rebase", base)
	return base, tip
}

const glossary = "# Glossary\n\nterm one\nterm two\nterm three\nterm four\n"

// renameRepo is resolveRepo's scene when mainline renames ctx.md to glossary.md
// after the fork: the Chart edits chartEdit, and the rebase onto base carries an
// edit of ctx.md onto glossary.md. It leaves chart/x checked out.
func renameRepo(t *testing.T, chartEdit string) (base, tip string) {
	t.Helper()
	newRepo(t)
	commitFile(t, "ctx.md", glossary)
	commitFile(t, "main.txt", "mainline\n")
	mustGit(t, "checkout", "-b", "chart/x")
	commitFile(t, chartEdit, glossary+"chart term\n")
	tip = mustGit(t, "rev-parse", "HEAD")
	mustGit(t, "checkout", Mainline())
	mustGit(t, "mv", "ctx.md", "glossary.md")
	mustGit(t, "commit", "-m", "rename")
	base = mustGit(t, "rev-parse", "HEAD")
	mustGit(t, "checkout", "chart/x")
	mustGit(t, "rebase", base)
	return base, tip
}

func wantCheckFailure(t *testing.T, err error, assertion int, path string) {
	t.Helper()
	var f *ResolveCheckFailed
	if !errors.As(err, &f) {
		t.Fatalf("CheckResolved = %v, want *ResolveCheckFailed", err)
	}
	if f.Assertion != assertion {
		t.Errorf("assertion = %d (%v), want %d", f.Assertion, f, assertion)
	}
	if path != "" && !slices.Contains(f.Paths, path) {
		t.Errorf("paths = %v, want one naming %s", f.Paths, path)
	}
	if !strings.Contains(err.Error(), fmt.Sprint(assertion)) {
		t.Errorf("error %q does not name assertion %d", err, assertion)
	}
}

// The post-resolve check (ADR 0080 §3): one case per assertion, on real git.
func TestCheckResolved(t *testing.T) {
	t.Run("a correct resolve passes", func(t *testing.T) {
		base, tip := resolveRepo(t)
		if err := CheckResolved(base, tip); err != nil {
			t.Fatalf("CheckResolved = %v, want nil", err)
		}
	})
	t.Run("dirty tree fails assertion 1", func(t *testing.T) {
		base, tip := resolveRepo(t)
		writeFile(t, "chart.txt", "uncommitted\n")
		wantCheckFailure(t, CheckResolved(base, tip), 1, "chart.txt")
	})
	t.Run("rebase in progress fails assertion 1", func(t *testing.T) {
		base, tip := resolveRepo(t)
		mustGit(t, "reset", "--hard", tip)
		mustGit(t, "checkout", Mainline())
		commitFile(t, "shared.txt", "mainline side\n")
		base = mustGit(t, "rev-parse", "HEAD")
		mustGit(t, "checkout", "chart/x")
		if _, err := run("rebase", base); err == nil {
			t.Fatal("rebase should conflict")
		}
		wantCheckFailure(t, CheckResolved(base, tip), 1, "")
	})
	t.Run("base not an ancestor fails assertion 1", func(t *testing.T) {
		base, tip := resolveRepo(t)
		mustGit(t, "reset", "--hard", tip) // the un-rebased Chart
		wantCheckFailure(t, CheckResolved(base, tip), 1, "")
	})
	t.Run("a remaining ADR clash fails assertion 2", func(t *testing.T) {
		base, tip := resolveRepo(t)
		addADR(t, "0001-chart.md")
		mustGit(t, "checkout", Mainline())
		addADR(t, "0001-main.md")
		base = mustGit(t, "rev-parse", "HEAD")
		mustGit(t, "checkout", "chart/x")
		mustGit(t, "rebase", base)
		wantCheckFailure(t, CheckResolved(base, tip), 2, "docs/adr/0001-chart.md (rule **/docs/adr/{n:4}-*.md) (vs mainline docs/adr/0001-main.md)")
	})
	t.Run("a mainline-only path modified fails assertion 3", func(t *testing.T) {
		base, tip := resolveRepo(t)
		commitFile(t, "main.txt", "clobbered\n")
		wantCheckFailure(t, CheckResolved(base, tip), 3, "main.txt")
	})
	t.Run("a mainline-only path deleted fails assertion 3", func(t *testing.T) {
		base, tip := resolveRepo(t)
		mustGit(t, "rm", "-q", "main.txt")
		mustGit(t, "commit", "-m", "drop mainline file")
		wantCheckFailure(t, CheckResolved(base, tip), 3, "main.txt")
	})
	t.Run("a reference sweep into a mainline-only file fails assertion 3", func(t *testing.T) {
		base, tip := resolveRepo(t)
		commitFile(t, "README", "see ADR 0002\n") // README predates the Chart: mainline's
		wantCheckFailure(t, CheckResolved(base, tip), 3, "README")
	})
	t.Run("a stray new file outside the Chart's diff fails assertion 3", func(t *testing.T) {
		base, tip := resolveRepo(t)
		commitFile(t, "scratch notes.txt", "stray\n")
		wantCheckFailure(t, CheckResolved(base, tip), 3, "scratch notes.txt")
	})
	t.Run("a renumbered Chart ADR passes", func(t *testing.T) {
		newRepo(t)
		mustGit(t, "checkout", "-b", "chart/x")
		addADR(t, "0001-chart.md")
		commitFile(t, "notes.txt", "see ADR 0001\n")
		tip := mustGit(t, "rev-parse", "HEAD")
		mustGit(t, "checkout", Mainline())
		addADR(t, "0001-main.md")
		base := mustGit(t, "rev-parse", "HEAD")
		mustGit(t, "checkout", "chart/x")
		mustGit(t, "rebase", base)
		mustGit(t, "mv", "docs/adr/0001-chart.md", "docs/adr/0002-chart.md")
		commitFile(t, "notes.txt", "see ADR 0002\n")
		if err := CheckResolved(base, tip); err != nil {
			t.Fatalf("CheckResolved = %v, want a renumber to pass", err)
		}
	})
	t.Run("a renumber within another docs/adr/ passes", func(t *testing.T) {
		newRepo(t)
		mustGit(t, "checkout", "-b", "chart/x")
		addADR(t, "apps/mugicrew/docs/adr/0001-chart.md")
		tip := mustGit(t, "rev-parse", "HEAD")
		mustGit(t, "checkout", Mainline())
		addADR(t, "apps/mugicrew/docs/adr/0001-main.md")
		base := mustGit(t, "rev-parse", "HEAD")
		mustGit(t, "checkout", "chart/x")
		mustGit(t, "rebase", base)
		mustGit(t, "mv", "apps/mugicrew/docs/adr/0001-chart.md", "apps/mugicrew/docs/adr/0002-chart.md")
		mustGit(t, "commit", "-m", "renumber")
		if err := CheckResolved(base, tip); err != nil {
			t.Fatalf("CheckResolved = %v, want a same-directory renumber to pass", err)
		}
	})
	t.Run("a same-slug add in another docs/adr/ fails assertion 3", func(t *testing.T) {
		newRepo(t)
		mustGit(t, "checkout", "-b", "chart/x")
		addADR(t, "apps/mugicrew/docs/adr/0001-chart.md")
		tip := mustGit(t, "rev-parse", "HEAD")
		mustGit(t, "checkout", Mainline())
		addADR(t, "apps/mugicrew/docs/adr/0001-main.md")
		base := mustGit(t, "rev-parse", "HEAD")
		mustGit(t, "checkout", "chart/x")
		mustGit(t, "rebase", base)
		if err := os.MkdirAll("docs/adr", 0o755); err != nil {
			t.Fatal(err)
		}
		mustGit(t, "mv", "apps/mugicrew/docs/adr/0001-chart.md", "docs/adr/0001-chart.md")
		mustGit(t, "commit", "-m", "move to root")
		wantCheckFailure(t, CheckResolved(base, tip), 3, "docs/adr/0001-chart.md")
	})
	t.Run("an added ADR with no same-slug Chart ADR fails assertion 3", func(t *testing.T) {
		base, tip := resolveRepo(t)
		addADR(t, "0005-stray.md")
		wantCheckFailure(t, CheckResolved(base, tip), 3, "docs/adr/0005-stray.md")
	})
	t.Run("a prior run's bad commit before session start is still caught", func(t *testing.T) {
		base, tip := resolveRepo(t)
		commitFile(t, "main.txt", "prior run clobbered\n") // an earlier escalated run
		commitFile(t, "chart.txt", "this session\n")       // session start is past the bad commit
		wantCheckFailure(t, CheckResolved(base, tip), 3, "main.txt")
	})
	t.Run("a Chart edit carried onto a mainline rename target passes", func(t *testing.T) {
		base, tip := renameRepo(t, "ctx.md")
		if err := CheckResolved(base, tip); err != nil {
			t.Fatalf("CheckResolved = %v, want a carried rename to pass", err)
		}
	})
	t.Run("a rename target does not excuse another mainline-only edit", func(t *testing.T) {
		base, tip := renameRepo(t, "ctx.md")
		commitFile(t, "main.txt", "clobbered\n")
		wantCheckFailure(t, CheckResolved(base, tip), 3, "main.txt")
	})
	t.Run("a mainline rename of a path the Chart never touched fails assertion 3", func(t *testing.T) {
		base, tip := renameRepo(t, "chart.txt")
		commitFile(t, "glossary.md", glossary+"resolver edit\n")
		wantCheckFailure(t, CheckResolved(base, tip), 3, "glossary.md")
	})
	t.Run("no tip skips assertion 3, still runs 1 and 2", func(t *testing.T) {
		base, _ := resolveRepo(t)
		commitFile(t, "main.txt", "clobbered\n")
		if err := CheckResolved(base, ""); err != nil {
			t.Fatalf("CheckResolved without tip = %v, want assertion 3 skipped", err)
		}
		writeFile(t, "chart.txt", "uncommitted\n")
		wantCheckFailure(t, CheckResolved(base, ""), 1, "chart.txt")
	})
}

// A failed check restores the session-start tip (ADR 0080 §4): this session's
// commits go, earlier ones stay, and an in-progress rebase is aborted first.
func TestRestoreResolved(t *testing.T) {
	base, tip := resolveRepo(t)
	commitFile(t, "chart.txt", "earlier escalated run\n")
	start := mustGit(t, "rev-parse", "HEAD")
	commitFile(t, "main.txt", "clobbered\n")
	if err := CheckResolved(base, tip); err == nil {
		t.Fatal("check should fail")
	}
	if err := RestoreResolved(start); err != nil {
		t.Fatalf("RestoreResolved: %v", err)
	}
	if got := mustGit(t, "rev-parse", "chart/x"); got != start {
		t.Errorf("branch = %s, want session-start tip %s", got, start)
	}
	if got := mustGit(t, "show", "chart/x:chart.txt"); got != "earlier escalated run" {
		t.Errorf("earlier commit lost: chart.txt = %q", got)
	}
	if err := CheckResolved(base, tip); err != nil {
		t.Errorf("CheckResolved after restore = %v, want nil", err)
	}

	t.Run("aborts a rebase in progress", func(t *testing.T) {
		mustGit(t, "checkout", Mainline())
		commitFile(t, "chart.txt", "mainline side\n")
		mustGit(t, "checkout", "chart/x")
		if _, err := run("rebase", Mainline()); err == nil {
			t.Fatal("rebase should conflict")
		}
		if err := RestoreResolved(start); err != nil {
			t.Fatalf("RestoreResolved: %v", err)
		}
		if rebaseInProgress() {
			t.Error("rebase still in progress")
		}
		if got := mustGit(t, "rev-parse", "chart/x"); got != start {
			t.Errorf("branch = %s, want %s", got, start)
		}
		if got := mustGit(t, "symbolic-ref", "--short", "HEAD"); got != "chart/x" {
			t.Errorf("HEAD = %s, want chart/x checked out", got)
		}
	})
}

// The mainline is detected from origin/HEAD, falling back to main (Spec
// FR11–13): a repo whose origin/HEAD names trunk cuts, lands, greps and logs on
// trunk, never main, and its Land's ADR-clash check is a no-op with no docs/adr/.
func TestMainlineDetectedFromOriginHEAD(t *testing.T) {
	newRepo(t)
	if got := Mainline(); got != "main" {
		t.Fatalf("Mainline() with no origin/HEAD = %q, want main", got)
	}

	t.Chdir(t.TempDir())
	mustGit(t, "init", "-b", "trunk")
	mustGit(t, "config", "user.email", "test@example.com")
	mustGit(t, "config", "user.name", "test")
	mustGit(t, "commit", "--allow-empty", "-m", "root")
	mustGit(t, "update-ref", "refs/remotes/origin/trunk", "HEAD")
	mustGit(t, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
	if got := Mainline(); got != "trunk" {
		t.Fatalf("Mainline() with origin/HEAD -> origin/trunk = %q, want trunk", got)
	}
	gitDir := mustGit(t, "rev-parse", "--absolute-git-dir")
	if got := MainlineOf(gitDir); got != "trunk" {
		t.Errorf("MainlineOf(%s) = %q, want trunk", gitDir, got)
	}

	if _, err := CutBranch("checkpoint/x", Mainline()); err != nil {
		t.Fatalf("CutBranch: %v", err)
	}
	mustGit(t, "checkout", "checkpoint/x")
	writeFile(t, "a.txt", "a\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "a")
	mustGit(t, "checkout", "trunk") // land through the mainline worktree
	sha, err := Land("checkpoint/x", "Checkpoint: x", LandLocal)
	if err != nil {
		t.Fatalf("Land onto trunk: %v", err)
	}
	if head := mustGit(t, "rev-parse", "trunk"); head != sha {
		t.Errorf("trunk = %s, want the land %s", head, sha)
	}
	if got := mustGit(t, "show", "HEAD:a.txt"); got != "a" {
		t.Errorf("trunk worktree HEAD:a.txt = %q, want a (ff through the checkout)", got)
	}
	if lb, err := LandedBase("Checkpoint: x"); err != nil || lb != sha {
		t.Errorf("LandedBase = (%s, %v), want (%s, nil)", lb, err, sha)
	}
	if at, err := MainlineAt(gitDir); err != nil || at != sha {
		t.Errorf("MainlineAt = (%s, %v), want (%s, nil)", at, err, sha)
	}
	if log, err := MainlineLog(5); err != nil || len(log) != 2 || log[0].Lands != "x" {
		t.Errorf("MainlineLog = (%+v, %v), want trunk's land then root", log, err)
	}
}

// CommitsAhead counts the branch's own commits since its merge-base with base
// (Spec D5): zero on a fresh cut, and mainline moving on doesn't count.
func TestCommitsAheadCountsSinceMergeBase(t *testing.T) {
	newRepo(t)
	mustGit(t, "branch", "checkpoint/ws-cp")
	if n, err := CommitsAhead("main", "checkpoint/ws-cp"); err != nil || n != 0 {
		t.Fatalf("fresh cut: CommitsAhead = %d, %v; want 0", n, err)
	}
	commitFile(t, "main-only", "x\n") // mainline moves on
	mustGit(t, "checkout", "-q", "checkpoint/ws-cp")
	commitFile(t, "a", "1\n")
	commitFile(t, "b", "2\n")
	if n, err := CommitsAhead("main", "checkpoint/ws-cp"); err != nil || n != 2 {
		t.Fatalf("CommitsAhead = %d, %v; want 2", n, err)
	}
	if _, err := CommitsAhead("main", "checkpoint/ws-none"); err == nil {
		t.Error("a missing branch must error, not read as zero commits")
	}
}

// CommitsByTrailer counts one trailer's squashes, whole-line: s1 doesn't match
// s1.1 (Spec D8).
func TestCommitsByTrailerIsWholeLine(t *testing.T) {
	newRepo(t)
	mustGit(t, "branch", "checkpoint/cp")
	mustGit(t, "checkout", "-q", "checkpoint/cp")
	for _, tr := range []string{"Build: b1", "Surgery: s1", "Surgery: s1.1", "Surgery: s1"} {
		writeFile(t, "f", tr)
		mustGit(t, "add", "-A")
		mustGit(t, "commit", "-m", "squash\n\n"+tr)
	}
	for tr, want := range map[string]int{"Surgery: s1": 2, "Surgery: s1.1": 1, "Surgery: s2": 0} {
		if n, err := CommitsByTrailer("main", "checkpoint/cp", tr); err != nil || n != want {
			t.Errorf("%s = %d, %v; want %d", tr, n, err, want)
		}
	}
}

func TestNewestByTrailerIsWholeLineAndNewest(t *testing.T) {
	newRepo(t)
	mustGit(t, "checkout", "-q", "-b", "checkpoint/cp")
	var shas []string
	for _, tr := range []string{"Critique: c1", "Critique: c1.1", "Critique: c1", "Build: b1"} {
		writeFile(t, "f", tr+fmt.Sprint(len(shas)))
		mustGit(t, "add", "-A")
		mustGit(t, "commit", "-m", "squash\n\n"+tr)
		shas = append(shas, mustGit(t, "rev-parse", "HEAD"))
	}
	for tr, want := range map[string]string{"Critique: c1": shas[2], "Critique: c1.1": shas[1], "Critique: c2": ""} {
		if got, err := NewestByTrailer("checkpoint/cp", tr); err != nil || got != want {
			t.Errorf("%s = %q, %v; want %q", tr, got, err, want)
		}
	}
}

// lineageCommit writes a hand-made lineage merge commit on parents (commit-tree,
// no lineage binary: ADR 0119) with its first parent's tree and body as its message tail,
// and points refs/heads/<branch> at it.
func lineageCommit(t *testing.T, branch, body string, parents ...string) string {
	t.Helper()
	args := []string{"commit-tree", parents[0] + "^{tree}"}
	for _, p := range parents {
		args = append(args, "-p", p)
	}
	sha := mustGit(t, append(args, "-m", "lineage: sync stock/x", "-m", body)...)
	mustGit(t, "branch", "-f", branch, sha)
	return sha
}

func branchExists(branch string) bool {
	_, err := run("rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// A one-commit child of main carrying a Lineage: trailer lands by fast-forward to
// exactly that commit (no squash, trailer byte-for-byte), main's checked-out tree
// follows the ref, and the spent branch is deleted (Spec D1, FR 1–2, 8–9).
func TestLandLineageFastForwardsAndDeletesBranch(t *testing.T) {
	newRepo(t)
	mainWT := mustGit(t, "rev-parse", "--show-toplevel")
	base := mustGit(t, "rev-parse", Mainline())
	mustGit(t, "branch", "effort")
	effortWT := t.TempDir()
	mustGit(t, "worktree", "add", effortWT, "effort")
	t.Chdir(effortWT) // run from a linked worktree, main checked out in the primary tree
	trailer := "Lineage: stock/x " + base
	want := lineageCommit(t, "lineage/stock/x-1", trailer, base)

	sha, err := LandLineage("lineage/stock/x-1")
	if err != nil || sha != want {
		t.Fatalf("LandLineage = (%s, %v), want (%s, nil)", sha, err, want)
	}
	if head := mustGit(t, "rev-parse", Mainline()); head != want {
		t.Errorf("main = %s, want the lineage commit %s", head, want)
	}
	if tr := mustGit(t, "log", "-1", "--format=%(trailers:key=Lineage)", Mainline()); tr != trailer {
		t.Errorf("trailer on main = %q, want %q", tr, trailer)
	}
	if branchExists("lineage/stock/x-1") {
		t.Error("spent lineage branch not deleted")
	}
	if head := mustGit(t, "-C", mainWT, "rev-parse", "HEAD"); head != want {
		t.Errorf("main worktree HEAD = %s, want %s", head, want)
	}
	if st := mustGit(t, "-C", mainWT, "status", "--porcelain"); st != "" {
		t.Errorf("main worktree left dirty: %q", st)
	}
}

// Main checked out nowhere: the ff is the CAS update-ref.
func TestLandLineageWithMainCheckedOutNowhere(t *testing.T) {
	newRepo(t)
	base := mustGit(t, "rev-parse", Mainline())
	mustGit(t, "checkout", "--detach")
	want := lineageCommit(t, "lineage/stock/x-1", "Lineage: stock/x "+base, base)
	if sha, err := LandLineage("lineage/stock/x-1"); err != nil || sha != want {
		t.Fatalf("LandLineage = (%s, %v), want (%s, nil)", sha, err, want)
	}
	if head := mustGit(t, "rev-parse", Mainline()); head != want {
		t.Errorf("main = %s, want %s", head, want)
	}
}

// Every refusal is typed and leaves main and the branch untouched (FR 4–7).
func TestLandLineageRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		scene func(t *testing.T, base string)
		want  func(err error) bool
	}{
		"main moved": {
			func(t *testing.T, base string) {
				lineageCommit(t, "lin", "Lineage: stock/x "+base, base)
				commitFile(t, "later.txt", "x\n")
			},
			func(err error) bool {
				var e *MainMoved
				return errors.As(err, &e) && strings.Contains(err.Error(), "lineage sync")
			},
		},
		"two commits": {
			func(t *testing.T, base string) {
				one := lineageCommit(t, "lin", "Lineage: stock/x "+base, base)
				lineageCommit(t, "lin", "Lineage: stock/x "+base, one)
			},
			func(err error) bool { var e *LineageShape; return errors.As(err, &e) },
		},
		"merge commit": {
			func(t *testing.T, base string) {
				commitFile(t, "later.txt", "x\n") // first parent main's tip, second already on main: one commit ahead
				lineageCommit(t, "lin", "Lineage: stock/x "+base, mustGit(t, "rev-parse", Mainline()), base)
			},
			func(err error) bool { var e *LineageShape; return errors.As(err, &e) },
		},
		"no trailer": {
			func(t *testing.T, base string) { lineageCommit(t, "lin", "Stock: stock/x "+base, base) },
			func(err error) bool { var e *NoLineageTrailer; return errors.As(err, &e) },
		},
		"dirty main": {
			func(t *testing.T, base string) {
				lineageCommit(t, "lin", "Lineage: stock/x "+base, base)
				writeFile(t, "stray.txt", "x\n")
			},
			func(err error) bool { var e *MainDirty; return errors.As(err, &e) },
		},
	} {
		t.Run(name, func(t *testing.T) {
			newRepo(t)
			tc.scene(t, mustGit(t, "rev-parse", Mainline()))
			main, tip := mustGit(t, "rev-parse", Mainline()), mustGit(t, "rev-parse", "lin")

			if _, err := LandLineage("lin"); !tc.want(err) {
				t.Errorf("LandLineage err = %v (%T), want the %s refusal", err, err, name)
			}
			if now := mustGit(t, "rev-parse", Mainline()); now != main {
				t.Errorf("refusal moved main %s → %s", main, now)
			}
			if !branchExists("lin") || mustGit(t, "rev-parse", "lin") != tip {
				t.Error("refusal touched the branch")
			}
		})
	}
}

// A missing branch names itself in the refusal, not git's bare rev-parse error.
func TestLandLineageMissingBranch(t *testing.T) {
	newRepo(t)
	if _, err := LandLineage("lineage/nope"); err == nil || !strings.HasPrefix(err.Error(), "git: no lineage branch lineage/nope") {
		t.Errorf("LandLineage err = %v, want git: no lineage branch lineage/nope", err)
	}
}

// A held land lock makes LandLineage wait, then time out untyped, moving nothing (FR 3).
func TestLandLineageWaitsOnTheLandLock(t *testing.T) {
	newRepo(t)
	old := landLockTimeout
	landLockTimeout = 100 * time.Millisecond
	t.Cleanup(func() { landLockTimeout = old })
	base := mustGit(t, "rev-parse", Mainline())
	lineageCommit(t, "lin", "Lineage: stock/x "+base, base)
	held, release := make(chan struct{}), make(chan struct{})
	go withLandLock(func() (string, error) { close(held); <-release; return "", nil })
	<-held
	defer close(release)

	if _, err := LandLineage("lin"); err == nil || !strings.Contains(err.Error(), "still held") {
		t.Errorf("held lock err = %v, want the lock timeout", err)
	}
	if now := mustGit(t, "rev-parse", Mainline()); now != base || !branchExists("lin") {
		t.Errorf("a timed-out land-lineage moved main or dropped the branch")
	}
}

// A lineage commit on main is invisible to the trailer readers: a prior
// Checkpoint land still resolves through LandedBase and MainlineLog (FR 12).
func TestLandedTrailersReadThroughALineageCommit(t *testing.T) {
	newRepo(t)
	squash := landCheckpoint(t, "a", "a.txt")
	lineageCommit(t, "lin", "Lineage: stock/x "+squash, squash)
	lin, err := LandLineage("lin")
	if err != nil {
		t.Fatalf("LandLineage: %v", err)
	}
	if lb, err := LandedBase("Checkpoint: a"); err != nil || lb != squash {
		t.Errorf("LandedBase = (%s, %v), want (%s, nil)", lb, err, squash)
	}
	log, err := MainlineLog(5)
	if err != nil || len(log) < 2 || log[0].SHA != lin || log[0].Lands != "" || log[1].Lands != "a" {
		t.Errorf("MainlineLog = (%+v, %v), want the lineage commit (no land) then a's land", log, err)
	}
}

// Land never runs the strategy when the trailer is already on mainline.
func TestLandSkipsStrategyWhenLanded(t *testing.T) {
	newRepo(t)
	CutBranch("chart/x", Mainline())
	mustGit(t, "checkout", "chart/x")
	writeFile(t, "a.txt", "a\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "a")
	sha, err := Land("chart/x", "Chart: x", LandLocal)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Land("chart/x", "Chart: x", func(string, string) (string, error) {
		t.Fatal("strategy ran for an already-landed trailer")
		return "", nil
	})
	if err != nil || again != sha {
		t.Fatalf("Land again = (%q, %v), want (%q, nil)", again, err, sha)
	}
}

// The strategy runs under the land lock, and its result and ErrLandPending pass through.
func TestLandStrategyRunsUnderLock(t *testing.T) {
	newRepo(t)
	old := landLockTimeout
	landLockTimeout = 100 * time.Millisecond
	t.Cleanup(func() { landLockTimeout = old })
	var branch, trailer string
	_, err := Land("chart/p", "Chart: p", func(b, tr string) (string, error) {
		branch, trailer = b, tr
		if _, lerr := withLandLock(func() (string, error) { return "", nil }); lerr == nil {
			t.Error("land lock was free inside the strategy")
		}
		return "", ErrLandPending
	})
	if !errors.Is(err, ErrLandPending) || branch != "chart/p" || trailer != "Chart: p" {
		t.Fatalf("err = %v, strategy saw (%q, %q)", err, branch, trailer)
	}
}

func TestLandStrategyByName(t *testing.T) {
	for _, name := range []string{"local", "gitlab"} {
		if s, err := LandStrategyByName(name); err != nil || s == nil {
			t.Errorf("LandStrategyByName(%q) = (%v, %v)", name, s != nil, err)
		}
	}
	if _, err := LandStrategyByName("svn"); err == nil || !strings.Contains(err.Error(), `"svn"`) {
		t.Errorf("unknown name err = %v, want one naming svn", err)
	}
}

// ChangedPaths is cut from the merge-base with base, so mainline's later commits
// stay out of it, including after the branch is rebased onto a newer mainline.
func TestChangedPathsFromMergeBase(t *testing.T) {
	newRepo(t)
	mustGit(t, "checkout", "-b", "feat")
	writeFile(t, "a.txt", "a\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "a")
	mustGit(t, "checkout", "main")
	writeFile(t, "m.txt", "m\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "m")

	got, err := ChangedPaths("main", "feat")
	if err != nil || !slices.Equal(got, []string{"a.txt"}) {
		t.Fatalf("ChangedPaths = %v, %v; want [a.txt]", got, err)
	}
	mustGit(t, "checkout", "feat")
	mustGit(t, "rebase", "main")
	writeFile(t, "m2.txt", "m2\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "m2")
	mustGit(t, "checkout", "main")
	if got, err = ChangedPaths("main", "feat"); err != nil || !slices.Equal(got, []string{"a.txt", "m2.txt"}) {
		t.Fatalf("after rebase ChangedPaths = %v, %v; want [a.txt m2.txt]", got, err)
	}
	if got, err = ChangedPaths("main", "main"); err != nil || len(got) != 0 {
		t.Fatalf("no change = %v, %v; want empty", got, err)
	}
}

// An empty packet is today's squash argv; a packet is the middle paragraph, the
// Tip:/trailer paragraph stays last, and the cleanup keeps markdown headings.
func TestSquashCommitArgsDecisionPacket(t *testing.T) {
	want := []string{"commit", "--allow-empty", "-m", "land chart/c", "-m", "Tip: abc\nChart: c\nGraph: g1"}
	if got := squashCommitArgs("chart/c", "", "abc", "Chart: c", "g1", ""); !slices.Equal(got, want) {
		t.Errorf("empty packet argv = %v, want %v", got, want)
	}
	want = []string{"commit", "--allow-empty", "--cleanup=whitespace", "-m", "land checkpoint/c", "-m", "## Background\nwhy", "-m", "Tip: abc\nCheckpoint: c\nGraph: g1"}
	if got := squashCommitArgs("checkpoint/c", "", "abc", "Checkpoint: c", "g1", "## Background\nwhy"); !slices.Equal(got, want) {
		t.Errorf("packet argv = %v, want %v", got, want)
	}
}

func TestDecisionPacket(t *testing.T) {
	for body, want := range map[string]string{
		"Commit: abc":                           "",
		"Commit: abc\n\n":                       "",
		"Commit: abc\n\n## Background\n\nwhy\n": "## Background\n\nwhy",
		// a pre-packet Approval: the spawn text, pin on the second line
		"Clean review pass over the built Checkpoint slice at the pinned commit.\n" + CommitPin("abc"): "",
		"Clean pass.\nCommit: cafef00d": "",
	} {
		if got := DecisionPacket(body); got != want {
			t.Errorf("DecisionPacket(%q) = %q, want %q", body, got, want)
		}
	}
}

// The squash commit's -m subject is the passed land subject, with and without a
// Decision Packet; the trailer block is unchanged, and "" keeps "land <branch>".
func TestSquashCommitArgsLandSubject(t *testing.T) {
	const subject = "feat(mugicrew): Add thing (MC-1)"
	want := []string{"commit", "--allow-empty", "-m", subject, "-m", "Tip: abc\nChart: c\nGraph: g1"}
	if got := squashCommitArgs("chart/c", subject, "abc", "Chart: c", "g1", ""); !slices.Equal(got, want) {
		t.Errorf("no packet argv = %v, want %v", got, want)
	}
	want = []string{"commit", "--allow-empty", "--cleanup=whitespace", "-m", subject, "-m", "why", "-m", "Tip: abc\nCheckpoint: c\nGraph: g1"}
	if got := squashCommitArgs("checkpoint/c", subject, "abc", "Checkpoint: c", "g1", "why"); !slices.Equal(got, want) {
		t.Errorf("packet argv = %v, want %v", got, want)
	}
}

// A local land writes the conventional subject onto mainline, trailers unchanged.
func TestLandLocalWritesConventionalSubject(t *testing.T) {
	pre := landRepo(t)
	const subject = "feat(mugicrew): Add thing (MC-1)"
	repoStrategy, err := LandStrategyFor(LandStrategyLocal, "", subject)
	if err != nil {
		t.Fatal(err)
	}
	sha, err := Land("checkpoint/x", "Checkpoint: x", repoStrategy)
	if err != nil {
		t.Fatalf("Land: %v", err)
	}
	if got := mustGit(t, "log", "-1", "--format=%s", sha); got != subject {
		t.Errorf("subject = %q, want %q", got, subject)
	}
	if got := mustGit(t, "log", "-1", "--format=%B", sha); !strings.Contains(got, "\n\nTip: "+pre+"\nCheckpoint: x") {
		t.Errorf("trailers changed:\n%s", got)
	}
	if got, err := LandedBase("Checkpoint: x"); err != nil || got != sha {
		t.Errorf("LandedBase = (%q, %v), want %q", got, err, sha)
	}
}

// A multi-section markdown packet survives a real land: headings and table in git
// log, and LandedBase / LandedTip still read the trailer and tip.
func TestLandCarriesDecisionPacket(t *testing.T) {
	pre := landRepo(t)
	packet := "## Background\n\nWhy it shipped.\n\nTip: not-the-pin prose\n\n## Summary\n\n| change | where |\n|---|---|\n| a | b.go |\n\n# Top heading"
	sha, err := Land("checkpoint/x", "Checkpoint: x", func(branch, trailer string) (string, error) {
		composed, base, pin, err := Compose(branch, trailer, packet, "")
		if err != nil {
			return "", err
		}
		return LandSameBase(composed, base, pin)(branch, trailer)
	})
	if err != nil {
		t.Fatalf("Land: %v", err)
	}
	msg := mustGit(t, "log", "-1", "--format=%B", sha)
	for _, w := range []string{"land checkpoint/x\n\n## Background", "| change | where |\n|---|---|", "\n# Top heading\n\nTip: " + pre + "\nCheckpoint: x"} {
		if !strings.Contains(msg, w) {
			t.Errorf("message missing %q:\n%s", w, msg)
		}
	}
	if tip, ok := LandedTip(sha); !ok || tip != pre {
		t.Errorf("LandedTip = (%q, %v), want %q", tip, ok, pre)
	}
	if got, err := LandedBase("Checkpoint: x"); err != nil || got != sha {
		t.Errorf("LandedBase = (%q, %v), want %q", got, err, sha)
	}
}

func TestTrailerBlock(t *testing.T) {
	for _, c := range []struct{ tip, trailer, want string }{
		{"abc", "Chart: c", "Tip: abc\nChart: c\nGraph: g1"},
		{"", "Spec: s", "Spec: s\nGraph: g1"},
	} {
		if got := TrailerBlock(c.tip, c.trailer, "g1"); got != c.want {
			t.Errorf("TrailerBlock(%q, %q) = %q, want %q", c.tip, c.trailer, got, c.want)
		}
	}
}

func TestParseGraph(t *testing.T) {
	for _, c := range []struct {
		msg, want string
		ok        bool
	}{
		{"land x\n\nTip: a\nChart: c\nGraph: g1", "g1", true},
		{"anchor Spec: s\n\nSpec: s\nGraph: g1\n", "g1", true},
		{"land x\n\nChart: c", "", false},
		{"land x\n\nChart: c\r\nGraph: g1\r\n", "", false},
		{"prose Graph: g1 inline", "", false},
	} {
		if got, ok := ParseGraph(c.msg); got != c.want || ok != c.ok {
			t.Errorf("ParseGraph(%q) = %q, %v; want %q, %v", c.msg, got, ok, c.want, c.ok)
		}
	}
}

func TestGraphIDCachesAndErrorsLoudly(t *testing.T) {
	defer SetGraphID("")()
	old := readGraphRoot
	defer func() { readGraphRoot = old }()
	calls := 0
	readGraphRoot = func() (string, error) { calls++; return "", errors.New("bd down") }
	if _, err := GraphID(); err == nil {
		t.Fatal("GraphID with bd down = nil error, want loud failure")
	}
	readGraphRoot = func() (string, error) { calls++; return "root1", nil }
	for range 2 {
		if id, err := GraphID(); id != "root1" || err != nil {
			t.Fatalf("GraphID = %q, %v", id, err)
		}
	}
	if calls != 2 {
		t.Errorf("readGraphRoot calls = %d, want 2 (one failed, one cached)", calls)
	}
}

// TestMain pins the Graph id so the real-repo tests never ask a live bd.
func TestMain(m *testing.M) {
	SetGraphID("testgraphroot")
	os.Exit(m.Run())
}

// Under local, a land squash and a Rollback revert each carry the
// block's Graph: line, and LandedBase still re-derives each by its trailer.
func TestEveryLandWritesGraphLine(t *testing.T) {
	newRepo(t)
	squash := landCheckpoint(t, "a", "a.txt")
	revert, err := Revert(squash, "Reverts-Checkpoint: a")
	if err != nil {
		t.Fatalf("Revert: %v", err)
	}
	for name, sha := range map[string]string{"squash": squash, "revert": revert} {
		if g, ok := ParseGraph(mustGit(t, "log", "-1", "--format=%B", sha)); !ok || g != "testgraphroot" {
			t.Errorf("%s Graph = %q, %v; want testgraphroot", name, g, ok)
		}
	}
	if lb, err := LandedBase("Checkpoint: a"); err != nil || lb != squash {
		t.Errorf("LandedBase = (%s, %v), want %s", lb, err, squash)
	}
	if tip, ok := LandedTip(squash); !ok || tip == "" {
		t.Errorf("LandedTip(squash) = %q, %v; want the pinned tip", tip, ok)
	}
}

// forgeFixture is a Workspace (cwd) cloned from a bare Forge, plus a second clone
// that pushes to the Forge. Returns the Forge's path and a push-a-commit helper.
func forgeFixture(t *testing.T) (forge string, push func() string) {
	t.Helper()
	forge = filepath.Join(t.TempDir(), "forge.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", "-b", "main", forge).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	newRepo(t)
	mustGit(t, "remote", "add", "origin", forge)
	mustGit(t, "push", "-q", "origin", "main")
	other := t.TempDir()
	for _, a := range [][]string{{"clone", "-q", forge, other}, {"-C", other, "config", "user.email", "o@e.com"}, {"-C", other, "config", "user.name", "o"}} {
		if out, err := exec.Command("git", a...).CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	return forge, func() string {
		for _, a := range [][]string{{"commit", "--allow-empty", "-q", "-m", "forge"}, {"push", "-q", "origin", "main"}, {"rev-parse", "HEAD"}} {
			out, err := exec.Command("git", append([]string{"-C", other}, a...)...).Output()
			if err != nil {
				t.Fatal(err)
			}
			if a[0] == "rev-parse" {
				return strings.TrimSpace(string(out))
			}
		}
		return ""
	}
}

// FetchMainline fast-forwards local main to the Forge's, with main checked out (its
// tree moves too) and not; equal is a no-op.
func TestFetchMainlineFastForwards(t *testing.T) {
	for _, checkedOut := range []bool{true, false} {
		_, push := forgeFixture(t)
		want := push()
		if !checkedOut {
			mustGit(t, "checkout", "-q", "--detach")
		}
		got, err := FetchMainline(ForgeRemote())
		if err != nil || got != want {
			t.Fatalf("checkedOut=%v: FetchMainline = %q, %v; want %q", checkedOut, got, err, want)
		}
		if h := mustGit(t, "rev-parse", "main"); h != want {
			t.Errorf("checkedOut=%v: main = %s, want %s", checkedOut, h, want)
		}
		if got, err := FetchMainline("origin"); err != nil || got != want {
			t.Errorf("equal fetch = %q, %v; want no-op at %q", got, err, want)
		}
	}
}

// A local main with a commit the Forge lacks (and the Forge one local lacks) is *MainDiverged.
func TestFetchMainlineDiverged(t *testing.T) {
	_, push := forgeFixture(t)
	forge := push()
	mustGit(t, "commit", "--allow-empty", "-q", "-m", "local")
	local := mustGit(t, "rev-parse", "main")
	_, err := FetchMainline("origin")
	var d *MainDiverged
	if !errors.As(err, &d) || d.Local != local || d.Forge != forge {
		t.Fatalf("err = %v, want *MainDiverged{%s, %s}", err, local, forge)
	}
	if h := mustGit(t, "rev-parse", "main"); h != local {
		t.Error("diverged fetch moved main")
	}
}

// A local main strictly ahead of the Forge is *MainAhead (not diverged), listing the
// unpushed subjects and naming both fixes; main is unmoved.
func TestFetchMainlineAhead(t *testing.T) {
	forgeFixture(t)
	mustGit(t, "commit", "--allow-empty", "-q", "-m", "unpushed work")
	local := mustGit(t, "rev-parse", "main")
	_, err := FetchMainline("origin")
	var a *MainAhead
	if !errors.As(err, &a) || !slices.Equal(a.Commits, []string{"unpushed work"}) {
		t.Fatalf("err = %v, want *MainAhead{unpushed work}", err)
	}
	var d *MainDiverged
	if errors.As(err, &d) {
		t.Error("ahead is also *MainDiverged")
	}
	for _, want := range []string{"1 commit(s)", "unpushed work", "push", "reset --hard origin/main"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q lacks %q", err, want)
		}
	}
	if h := mustGit(t, "rev-parse", "main"); h != local {
		t.Error("ahead fetch moved main")
	}
}

// branch.main.remote wins over origin; absent, origin.
func TestForgeRemote(t *testing.T) {
	newRepo(t)
	if got := ForgeRemote(); got != "origin" {
		t.Errorf("default = %q", got)
	}
	mustGit(t, "config", "branch.main.remote", "forge")
	if got := ForgeRemote(); got != "forge" {
		t.Errorf("configured = %q", got)
	}
}

// commitTrailer lands an empty mainline commit carrying msg, standing in for
// another graph's (or a legacy) land.
func commitTrailer(t *testing.T, msg string) string {
	t.Helper()
	mustGit(t, "commit", "--allow-empty", "-m", "land x", "-m", msg)
	return mustGit(t, "rev-parse", "HEAD")
}

// Under gitlab LandedBase counts a trailer with our Graph: or none as landed (the
// newest ours wins over a newer foreign one) and refuses one carrying only another
// graph's id as *ForeignLanding, never ErrNotLanded (Spec FR 40, ADR 0187 §2).
func TestLandedBaseGitlabGraph(t *testing.T) {
	newRepo(t)
	defer SetGraphID("me")()
	defer func(f func() (string, error)) { LandStrategyName = f }(LandStrategyName)
	LandStrategyName = func() (string, error) { return LandStrategyGitlab, nil }

	ours := commitTrailer(t, "Checkpoint: a\nGraph: me")
	legacy := commitTrailer(t, "Checkpoint: b")
	foreign := commitTrailer(t, "Checkpoint: c\nGraph: them")
	commitTrailer(t, "Checkpoint: d\nGraph: them")
	d2 := commitTrailer(t, "Checkpoint: d\nGraph: them2")
	mixedOurs := commitTrailer(t, "Checkpoint: e\nGraph: me")
	commitTrailer(t, "Checkpoint: e\nGraph: them")
	// a squash reachable only as a merge's second parent
	mustGit(t, "checkout", "-b", "side", Mainline())
	side := commitTrailer(t, "Checkpoint: f\nGraph: me")
	mustGit(t, "checkout", Mainline())
	mustGit(t, "merge", "--no-ff", "-m", "merge side", "side")

	for trailer, want := range map[string]string{"Checkpoint: a": ours, "Checkpoint: b": legacy, "Checkpoint: e": mixedOurs, "Checkpoint: f": side} {
		if got, err := LandedBase(trailer); err != nil || got != want {
			t.Errorf("LandedBase(%q) = (%s, %v), want (%s, nil)", trailer, got, err, want)
		}
	}
	_, err := LandedBase("Checkpoint: c")
	var fe *ForeignLanding
	if !errors.As(err, &fe) || errors.Is(err, ErrNotLanded) {
		t.Fatalf("LandedBase(c) err = %v, want *ForeignLanding", err)
	}
	if want := (ForeignLanding{Trailer: "Checkpoint: c", Ours: "me", Theirs: "them", Sha: foreign}); *fe != want {
		t.Errorf("ForeignLanding = %+v, want %+v", *fe, want)
	}
	if _, err := LandedBase("Checkpoint: d"); !errors.As(err, &fe) || fe.Sha != d2 {
		t.Errorf("LandedBase(d) err = %v, want the newest foreign match %s", err, d2)
	}
	if _, err := LandedBase("Checkpoint: zzz"); !errors.Is(err, ErrNotLanded) {
		t.Errorf("LandedBase(absent) err = %v, want ErrNotLanded", err)
	}
}

// Under local Graph: is never read: a foreign Graph: trailer still reads as landed (FR 2).
func TestLandedBaseLocalIgnoresGraph(t *testing.T) {
	newRepo(t)
	defer SetGraphID("me")()
	sha := commitTrailer(t, "Checkpoint: c\nGraph: them")
	if got, err := LandedBase("Checkpoint: c"); err != nil || got != sha {
		t.Errorf("LandedBase = (%s, %v), want (%s, nil)", got, err, sha)
	}
}

// The conflict probe (Spec FR 3-5, 12) reports what a Landing would hit — *NumberClash
// first, then *RebaseConflict, else nil — and leaves the branch ref, mainline ref and
// worktree byte-for-byte as they were.
func TestProbeConflict(t *testing.T) {
	snapshot := func(t *testing.T, branch string) string {
		return mustGit(t, "rev-parse", branch, Mainline()) + mustGit(t, "status", "--porcelain")
	}
	probe := func(t *testing.T, branch string) (string, error) {
		before := snapshot(t, branch)
		base, err := ResolveProbeBase()
		if err != nil {
			t.Fatal(err)
		}
		err = ProbeConflict(branch, base)
		if after := snapshot(t, branch); after != before {
			t.Errorf("probe moved state:\n%s\n!=\n%s", after, before)
		}
		return base, err
	}
	t.Run("clean branch is nil", func(t *testing.T) {
		newRepo(t)
		mustGit(t, "checkout", "-b", "checkpoint/x")
		commitFile(t, "a.txt", "a\n")
		mustGit(t, "checkout", "main")
		commitFile(t, "b.txt", "b\n")
		if _, err := probe(t, "checkpoint/x"); err != nil {
			t.Errorf("probe = %v, want nil", err)
		}
	})
	t.Run("up to date with base is nil", func(t *testing.T) {
		newRepo(t)
		mustGit(t, "checkout", "-b", "checkpoint/x")
		commitFile(t, "a.txt", "a\n")
		mustGit(t, "checkout", "main")
		if _, err := probe(t, "checkpoint/x"); err != nil {
			t.Errorf("probe = %v, want nil", err)
		}
	})
	t.Run("textual conflict is *RebaseConflict with the base", func(t *testing.T) {
		newRepo(t)
		commitFile(t, "shared.txt", "root\n")
		mustGit(t, "checkout", "-b", "checkpoint/x")
		commitFile(t, "shared.txt", "branch\n")
		mustGit(t, "checkout", "main")
		commitFile(t, "shared.txt", "main\n")
		base, err := probe(t, "checkpoint/x")
		var rc *RebaseConflict
		if !errors.As(err, &rc) || rc.Base != base || base != mustGit(t, "rev-parse", "main") {
			t.Errorf("probe = %v (base %s), want *RebaseConflict on main's tip", err, base)
		}
	})
	t.Run("ADR-number clash is *NumberClash with the base", func(t *testing.T) {
		newRepo(t)
		addADR(t, "0001-base.md")
		mustGit(t, "checkout", "-b", "checkpoint/x")
		addADR(t, "0002-branch-side.md")
		mustGit(t, "checkout", "main")
		addADR(t, "0002-main-side.md")
		base, err := probe(t, "checkpoint/x")
		var clash *NumberClash
		if !errors.As(err, &clash) || clash.Base != base || len(clash.Clashes) != 1 {
			t.Errorf("probe = %v, want *NumberClash with one clash on %s", err, base)
		}
	})
	t.Run("a git failure is a plain error, never a collision", func(t *testing.T) {
		newRepo(t)
		err := ProbeConflict("checkpoint/missing", mustGit(t, "rev-parse", "main"))
		var rc *RebaseConflict
		var clash *NumberClash
		if err == nil || errors.As(err, &rc) || errors.As(err, &clash) {
			t.Errorf("probe = %v, want a plain error", err)
		}
	})
}

func TestCommonDirFollowsCwd(t *testing.T) {
	newRepo(t)
	a, err := CommonDir()
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := CommonDir(); again != a {
		t.Fatalf("second call = %q, want %q", again, a)
	}
	newRepo(t) // another repo, another cwd: the memo must not carry over
	b, err := CommonDir()
	if err != nil || b == a {
		t.Fatalf("CommonDir in a second repo = %q, %v; want a path other than %q", b, err, a)
	}
}
