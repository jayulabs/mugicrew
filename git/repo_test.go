package git

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A child repo whose origin/HEAD names a non-main trunk resolves that trunk, from a
// Workspace whose own trunk is main (Spec FR6).
func TestRepoMainlineResolvesTheChildsTrunk(t *testing.T) {
	newRepo(t) // the Workspace, trunk main
	child := t.TempDir()
	t.Chdir(child)
	mustGit(t, "init", "-b", "trunk")
	mustGit(t, "config", "user.email", "test@example.com")
	mustGit(t, "config", "user.name", "test")
	mustGit(t, "commit", "--allow-empty", "-m", "root")
	mustGit(t, "update-ref", "refs/remotes/origin/trunk", "HEAD")
	mustGit(t, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
	sha := mustGit(t, "rev-parse", "HEAD")
	t.Chdir(t.TempDir()) // the cwd is no repo at all: every op addresses the child
	r := Repo(child)
	if got := r.Mainline(); got != "trunk" {
		t.Errorf("Repo.Mainline = %q, want trunk", got)
	}
	if got, err := r.Head("trunk"); err != nil || got != sha {
		t.Errorf("Repo.Head = (%q, %v), want %q", got, err, sha)
	}
}

// A child repo inside a Workspace: branch cuts and trailer reads act on the child.
func TestRepoBranchesInTheChild(t *testing.T) {
	newRepo(t)
	ws, _ := os.Getwd()
	child := filepath.Join(ws, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	r := Repo(child)
	for _, a := range [][]string{{"init", "-b", "main"}, {"config", "user.email", "t@e.com"}, {"config", "user.name", "t"}, {"commit", "--allow-empty", "-m", "root"}} {
		if _, err := r.run(a...); err != nil {
			t.Fatal(err)
		}
	}
	wsMain := mustGit(t, "rev-parse", "main")
	base, _ := r.Head("main")

	if created, err := r.CutBranch("chart/x", base); !created || err != nil {
		t.Fatalf("CutBranch = %v, %v", created, err)
	}
	if created, _ := r.CutBranch("chart/x", base); created {
		t.Error("second CutBranch created again")
	}
	if _, err := r.LandedBase("Spec: mc-1"); !errors.Is(err, ErrNotLanded) {
		t.Fatalf("LandedBase = %v, want ErrNotLanded", err)
	}
	if got := mustGit(t, "rev-parse", "main"); got != wsMain {
		t.Error("Workspace main moved")
	}

	if err := r.DeleteBranch("chart/x"); err != nil {
		t.Error(err)
	}
}

// newChild makes a git repo at <workspace>/child with a root commit and a checkpoint
// branch one commit ahead of main, and returns its Repo.
func newChild(t *testing.T) Repo {
	t.Helper()
	newRepo(t)
	ws, _ := os.Getwd()
	child := Repo(filepath.Join(ws, "child"))
	if err := os.Mkdir(string(child), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, a := range [][]string{{"init", "-b", "main"}, {"config", "user.email", "t@e.com"}, {"config", "user.name", "t"}, {"commit", "--allow-empty", "-m", "root"}, {"branch", "checkpoint/cp"}} {
		if _, err := child.run(a...); err != nil {
			t.Fatal(err)
		}
	}
	return child
}

// Materialize cuts effort/<id> at the container as a linked worktree of the child,
// rolls forward an existing one, and honours a Tip pin; a child-first teardown leaves
// nothing prunable, an outer-first one leaves a prunable entry the next cut's prune
// heals (Spec FR19-23).
func TestRepoMaterializeCutsRollsForwardAndTearsDown(t *testing.T) {
	child := newChild(t)
	host := t.TempDir()
	path := filepath.Join(host, "child")

	down, err := child.Materialize(path, "b1", "checkpoint/cp", "")
	if err != nil {
		t.Fatal(err)
	}
	if id, err := Repo(path).CurrentEffort(); err != nil || id != "b1" {
		t.Fatalf("CurrentEffort in the materialized tree = (%q, %v), want b1", id, err)
	}
	if _, err := Repo(path).run("commit", "--allow-empty", "-m", "work"); err != nil {
		t.Fatal(err)
	}
	down()
	if _, err := os.Stat(path); err == nil {
		t.Error("teardown left the tree")
	}
	if h, ok, _ := child.WorktreeOn("effort/b1"); ok {
		t.Errorf("effort/b1 still checked out at %s", h.Path)
	}
	if n, _ := child.run("rev-list", "--count", "checkpoint/cp..effort/b1"); n != "1" {
		t.Errorf("effort/b1 ahead = %s, want 1 (rolled-forward commit kept)", n)
	}

	// roll forward, then an outer-first teardown strands a prunable entry
	if _, err := child.Materialize(path, "b1", "checkpoint/cp", ""); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(path)
	if h, ok, _ := child.WorktreeOn("effort/b1"); !ok || !h.Prunable {
		t.Fatalf("outer-first teardown holder = %+v, %v; want prunable", h, ok)
	}
	down, err = child.Materialize(path, "b1", "checkpoint/cp", "")
	if err != nil {
		t.Fatalf("re-cut over a prunable entry: %v", err)
	}
	if n, _ := Repo(path).run("rev-list", "--count", "checkpoint/cp..HEAD"); n != "1" {
		t.Errorf("re-cut did not roll forward: ahead = %s", n)
	}
	down()

	// a Tip-pinned effort replays at the pin, not the moved container
	pin, _ := child.Head("checkpoint/cp")
	moved, _ := child.run("commit-tree", "-p", pin, "-m", "moved", pin+"^{tree}")
	child.run("update-ref", "refs/heads/checkpoint/cp", moved)
	down, err = child.Materialize(path, "b2", "checkpoint/cp", pin)
	if err != nil {
		t.Fatal(err)
	}
	defer down()
	if head, _ := Repo(path).Head("HEAD"); head != pin {
		t.Errorf("pinned effort cut at %s, want pin %s", head, pin)
	}
}

// Integrate and IntegrateReplace run in the child worktree: the squash lands on the
// child's container, the Workspace's refs never move.
func TestRepoIntegrateSquashesInTheChildTree(t *testing.T) {
	child := newChild(t)
	wsMain := mustGit(t, "rev-parse", "main")
	path := filepath.Join(t.TempDir(), "child")
	down, err := child.Materialize(path, "b1", "checkpoint/cp", "")
	if err != nil {
		t.Fatal(err)
	}
	defer down()
	tree := Repo(path)
	if _, err := tree.run("commit", "--allow-empty", "-m", "work"); err != nil {
		t.Fatal(err)
	}
	sha, _, err := tree.Integrate("checkpoint/cp", "b1", "chore: x [b1]", TrailerFor("task", "b1"))
	if err != nil || sha == "" {
		t.Fatalf("Integrate = (%q, %v), want a squash", sha, err)
	}
	if got, _ := child.Head("checkpoint/cp"); got != sha {
		t.Errorf("child container at %s, want the squash %s", got, sha)
	}
	if again, _, err := tree.Integrate("checkpoint/cp", "b1", "chore: x [b1]", TrailerFor("task", "b1")); err != nil || again != "" {
		t.Errorf("second Integrate = (%q, %v), want nothing", again, err)
	}
	if mustGit(t, "rev-parse", "main") != wsMain {
		t.Error("Workspace main moved")
	}
}

// commitIn writes file (relative to dir's tree) with content and commits it through r.
func commitIn(t *testing.T, r Repo, rel, content string) {
	t.Helper()
	p := filepath.Join(string(r), rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, a := range [][]string{{"add", "-A"}, {"commit", "-m", "add " + rel}} {
		if _, err := r.run(a...); err != nil {
			t.Fatal(err)
		}
	}
}

// landNested is Jinbe's nested land: a child tree detached at the branch tip, Compose
// there, then Repo.Land with the same-base write leg.
func landNested(t *testing.T, child Repo, branch, trailer string) (string, error) {
	t.Helper()
	tree := filepath.Join(t.TempDir(), "child")
	down, err := child.MaterializeDetached(tree, branch)
	if err != nil {
		t.Fatal(err)
	}
	defer down()
	tr := Repo(tree)
	composed, base, pin, err := tr.Compose(branch, trailer, "", "")
	if err != nil {
		return "", err
	}
	return child.Land(branch, trailer, tr.LandSameBase(composed, base, pin))
}

// Repo.Land runs the full Compose path in the child: the squash with its trailer
// lands on the child's main (not the Workspace's), a re-land is idempotent, an ADR
// number the child already holds is *NumberClash, a dirty child main is *MainDirty, and
// no land.lock appears in the child (Spec FR 31-32, 43).
func TestRepoLandComposesAndLandsOnTheChildsMain(t *testing.T) {
	child := newChild(t)
	wsMain := mustGit(t, "rev-parse", "main")
	commitIn(t, child, "docs/adr/0001-a.md", "# a\n")
	if _, err := child.run("checkout", "-q", "checkpoint/cp"); err != nil {
		t.Fatal(err)
	}
	commitIn(t, child, "feature.txt", "x\n")
	if _, err := child.run("checkout", "-q", "main"); err != nil {
		t.Fatal(err)
	}
	trailer := TrailerFor("checkpoint", "cp")

	sha, err := landNested(t, child, "checkpoint/cp", trailer)
	if err != nil || sha == "" {
		t.Fatalf("nested land = (%q, %v)", sha, err)
	}
	if got, _ := child.Head("main"); got != sha {
		t.Errorf("child main = %s, want the squash %s", got, sha)
	}
	if got, _ := child.LandedBase(trailer); got != sha {
		t.Errorf("LandedBase = %s, want %s", got, sha)
	}
	if _, err := os.Stat(filepath.Join(string(child), "feature.txt")); err != nil {
		t.Errorf("child main checkout did not move with the ff: %v", err)
	}
	if again, err := landNested(t, child, "checkpoint/cp", trailer); err != nil || again != sha {
		t.Errorf("re-land = (%q, %v), want idempotent %s", again, err, sha)
	}
	if mustGit(t, "rev-parse", "main") != wsMain {
		t.Error("Workspace main moved")
	}
	if _, err := os.Stat(filepath.Join(string(child), ".git", "mugicrew", "land.lock")); err == nil {
		t.Error("a land.lock appeared in the child")
	}

	// the child's own ADR number space: 0002 on both sides under other names clashes
	commitIn(t, child, "docs/adr/0002-b.md", "# b\n")
	child.run("branch", "chart/c", "main~1")
	child.run("checkout", "-q", "chart/c")
	commitIn(t, child, "docs/adr/0002-other.md", "# other\n")
	child.run("checkout", "-q", "main")
	var clash *NumberClash
	if _, err := landNested(t, child, "chart/c", TrailerFor("chart", "c")); !errors.As(err, &clash) {
		t.Errorf("clashing child ADR: err = %v, want *NumberClash", err)
	}
	// the same number in the Workspace is not a clash
	writeFile(t, "w.txt", "w\n")
	os.MkdirAll("docs/adr", 0o755)
	writeFile(t, "docs/adr/0002-ws.md", "# ws\n")
	mustGit(t, "add", "-A")
	mustGit(t, "commit", "-m", "ws adr")
	child.run("branch", "chart/d", "main")
	child.run("checkout", "-q", "chart/d")
	commitIn(t, child, "docs/adr/0003-c.md", "# c\n")
	child.run("checkout", "-q", "main")
	if _, err := landNested(t, child, "chart/d", TrailerFor("chart", "d")); err != nil {
		t.Errorf("Workspace ADR number refused a child land: %v", err)
	}

	// a dirty child main checkout refuses the land
	child.run("branch", "chart/e", "main")
	child.run("checkout", "-q", "chart/e")
	commitIn(t, child, "e.txt", "e\n")
	child.run("checkout", "-q", "main")
	writeFile(t, filepath.Join(string(child), "dirty.txt"), "d\n")
	var dirty *MainDirty
	if _, err := landNested(t, child, "chart/e", TrailerFor("chart", "e")); !errors.As(err, &dirty) {
		t.Errorf("dirty child main: err = %v, want *MainDirty", err)
	}
}

// LandStrategyFor with no repo is LandStrategyByName; local and gitlab both act per repo.
func TestLandStrategyForRepo(t *testing.T) {
	if _, err := LandStrategyFor(LandStrategyLocal, "", ""); err != nil {
		t.Error(err)
	}
	if _, err := LandStrategyFor("nope", "/x", ""); err == nil {
		t.Error("unknown strategy accepted")
	}
	if _, err := LandStrategyFor(LandStrategyGitlab, "/x", ""); err != nil {
		t.Errorf("gitlab for a repo: %v", err)
	}
	child := newChild(t)
	child.run("checkout", "-q", "checkpoint/cp")
	commitIn(t, child, "f.txt", "f\n")
	child.run("checkout", "-q", "main")
	st, err := LandStrategyFor(LandStrategyLocal, child, "")
	if err != nil {
		t.Fatal(err)
	}
	if sha, err := st("checkpoint/cp", TrailerFor("checkpoint", "cp")); err != nil || sha == "" {
		t.Errorf("local strategy for the repo = (%q, %v)", sha, err)
	}
	if got, _ := child.LandedBase(TrailerFor("checkpoint", "cp")); got == "" {
		t.Error("strategy did not land in the child")
	}
}

// HolderAcross finds effort/<id> in the cwd repo first, then in a declared child:
// checked out in a worktree, or stopped mid-rebase; the host is the child path with
// /<dir> stripped (Spec FR 51).
func TestHolderAcrossFindsAChildHolder(t *testing.T) {
	newRepo(t)
	ws, _ := os.Getwd()
	child := Repo(filepath.Join(ws, "sub"))
	if err := os.Mkdir(string(child), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, a := range [][]string{{"init", "-b", "main"}, {"config", "user.email", "t@e.com"}, {"config", "user.name", "t"}, {"commit", "--allow-empty", "-m", "root"}} {
		if _, err := child.run(a...); err != nil {
			t.Fatal(err)
		}
	}
	host := t.TempDir()
	repos := map[string]string{"s": "sub"}
	if h, r, err := HolderAcross("effort/x", repos); err != nil || h.Path != "" || r != "" {
		t.Fatalf("no holder = (%+v, %q, %v)", h, r, err)
	}
	tree := filepath.Join(host, "sub")
	if _, err := child.Materialize(tree, "x", "main", ""); err != nil {
		t.Fatal(err)
	}
	h, r, err := HolderAcross("effort/x", repos)
	if err != nil || r != child || h.Path != tree || h.Host != host || h.Dir != "sub" || h.Rebasing {
		t.Fatalf("worktree holder = (%+v, %q, %v)", h, r, err)
	}
	if h, r, _ := HolderAcross("effort/x", nil); h.Path != "" || r != "" {
		t.Errorf("no repos must look at the cwd repo alone, got (%+v, %q)", h, r)
	}

	// stopped mid-rebase: the child's HEAD is detached, so only RebasingOn sees it
	for _, a := range [][]string{{"-C", tree, "commit", "--allow-empty", "-m", "a"}, {"-C", string(child), "commit", "--allow-empty", "-m", "b"}} {
		if _, err := child.run(a...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := child.run("-C", tree, "checkout", "--detach"); err != nil {
		t.Fatal(err)
	}
	head := filepath.Join(string(child), ".git", "worktrees", "sub", "rebase-merge")
	if err := os.MkdirAll(head, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(head, "head-name"), []byte("refs/heads/effort/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h, r, err = HolderAcross("effort/x", repos)
	if err != nil || r != child || h.Path != tree || !h.Rebasing || h.Host != host {
		t.Fatalf("rebasing holder = (%+v, %q, %v)", h, r, err)
	}
}

// CheckResolved and RestoreResolved act on the child while the cwd is no repo: an
// ADR-number clash in the child is caught, and a restore resets the child's branch
// (Spec FR 30).
func TestRepoCheckAndRestoreResolvedActInTheChild(t *testing.T) {
	child := t.TempDir()
	r := Repo(child)
	g := func(args ...string) string {
		t.Helper()
		out, err := r.run(append([]string{"-c", "user.email=t@e.com", "-c", "user.name=t"}, args...)...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	commit := func(file, msg string) {
		t.Helper()
		p := filepath.Join(child, file)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(msg), 0o644)
		g("add", ".")
		g("commit", "-m", msg)
	}
	g("init", "-b", "main")
	commit("README", "root")
	g("checkout", "-b", "chart/x")
	commit("docs/adr/0001-chart.md", "chart adr")
	g("checkout", "main")
	commit("docs/adr/0001-main.md", "main adr")
	base := g("rev-parse", "main")
	g("checkout", "chart/x")
	g("rebase", "main") // clean: the two ADRs differ in name, so only the number clashes
	t.Chdir(t.TempDir())

	var failed *ResolveCheckFailed
	if err := r.CheckResolved(base, ""); !errors.As(err, &failed) || failed.Assertion != 2 {
		t.Fatalf("CheckResolved = %v, want assertion 2 (ADR clash)", err)
	}
	start := g("rev-parse", "HEAD~1")
	if err := r.RestoreResolved(start); err != nil {
		t.Fatal(err)
	}
	if got := g("rev-parse", "HEAD"); got != start {
		t.Errorf("child HEAD = %s after restore, want %s", got, start)
	}
}

// Revert and Reapply act on the child's mainline and branches: the revert is one
// trailer-idempotent forward commit on the child's trunk, the reapply seeds a branch
// cut at it, the Workspace's main never moves and the lock stays the Workspace's
// (Spec FR 13, 17, 49).
func TestRepoRevertAndReapplyActOnTheChildsMainline(t *testing.T) {
	child := newChild(t)
	wsMain := mustGit(t, "rev-parse", "main")
	if _, err := child.run("checkout", "-q", "checkpoint/cp"); err != nil {
		t.Fatal(err)
	}
	commitIn(t, child, "feature.txt", "x\n")
	if _, err := child.run("checkout", "-q", "main"); err != nil {
		t.Fatal(err)
	}
	squash, err := landNested(t, child, "checkpoint/cp", TrailerFor("checkpoint", "cp"))
	if err != nil {
		t.Fatal(err)
	}
	trailer := RevertsTrailer("checkpoint", "cp")

	rev, err := child.Revert(squash, trailer)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := child.Head("main"); got != rev {
		t.Errorf("child main = %s, want the revert %s", got, rev)
	}
	if _, err := os.Stat(filepath.Join(string(child), "feature.txt")); err == nil {
		t.Error("child main checkout still has the reverted file")
	}
	if again, err := child.Revert(squash, trailer); err != nil || again != rev {
		t.Errorf("second Revert = (%q, %v), want idempotent %s", again, err, rev)
	}

	if _, err := child.CutBranch("checkpoint/succ", rev); err != nil {
		t.Fatal(err)
	}
	seed, err := child.Reapply("checkpoint/succ", rev)
	if err != nil || seed == rev {
		t.Fatalf("Reapply = (%q, %v), want a new tip", seed, err)
	}
	if got, _ := child.Head("checkpoint/succ"); got != seed {
		t.Errorf("successor tip = %s, want %s", got, seed)
	}
	if got, _ := child.Head("main"); got != rev {
		t.Error("Reapply moved the child's main")
	}
	if mustGit(t, "rev-parse", "main") != wsMain {
		t.Error("Workspace main moved")
	}
	if _, err := os.Stat(filepath.Join(string(child), ".git", "mugicrew", "land.lock")); err == nil {
		t.Error("a land.lock appeared in the child")
	}
}

// MainlineLogAcross merges the Workspace's and each declared repo's mainline log
// newest first, tags the child's rows with their context, and caps the result; with
// no repo declared it is MainlineLog alone (Spec FR47).
func TestMainlineLogAcrossMergesNewestFirst(t *testing.T) {
	child := newChild(t)
	land := func(r Repo, date, trailer string) {
		t.Helper()
		t.Setenv("GIT_COMMITTER_DATE", date)
		if _, err := r.run("commit", "--allow-empty", "-m", "land", "-m", trailer); err != nil {
			t.Fatal(err)
		}
	}
	land("", "2026-01-01T00:00:00Z", "Checkpoint: ws-old")
	land(child, "2026-01-02T00:00:00Z", "Checkpoint: kid")
	land(child, "2026-01-02T12:00:00Z", "Chart: kidchart")
	land("", "2026-01-03T00:00:00Z", "Checkpoint: ws-new")

	cs, err := MainlineLogAcross(200, map[string]string{"nested": "child"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range cs {
		if c.Lands != "" {
			got = append(got, c.Context+":"+c.Lands)
		}
	}
	if want := []string{":ws-new", "nested:kidchart", "nested:kid", ":ws-old"}; !slices.Equal(got, want) {
		t.Errorf("merged lands = %v, want %v", got, want)
	}
	if capped, _ := MainlineLogAcross(2, map[string]string{"nested": "child"}); len(capped) != 2 {
		t.Errorf("capped to %d rows, want 2", len(capped))
	}
	plain, _ := MainlineLogAcross(200, nil)
	if want, _ := MainlineLog(200); !slices.Equal(plain, want) {
		t.Errorf("no repos = %v, want MainlineLog's %v", plain, want)
	}
}

// A child repo refuses revert under gitlab before writing.
func TestChildRevertRefusesUnderGitlab(t *testing.T) {
	child := newChild(t)
	defer func(f func() (string, error)) { LandStrategyName = f }(LandStrategyName)
	mainBefore, _ := child.run("rev-parse", "main")
	objs := func() string { o, _ := child.run("count-objects", "-v"); return o }
	objsBefore := objs()

	LandStrategyName = func() (string, error) { return LandStrategyGitlab, nil }
	if _, err := child.revert(mainBefore, "Reverts-Checkpoint: x"); !errors.Is(err, ErrNestedGitlab) {
		t.Errorf("revert = %v, want ErrNestedGitlab", err)
	}
	if got, _ := child.run("rev-parse", "main"); got != mainBefore || objs() != objsBefore {
		t.Error("child main or objects changed")
	}
}
