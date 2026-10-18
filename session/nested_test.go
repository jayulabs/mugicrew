package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contexts/contextstest"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/route"
)

// A launch with no repo declared reads the repo table once and shows no bead: the
// cut is today's (Spec FR 46, ADR 0157).
func TestCutTreeWithNoReposReadsOneConfigAndNoBead(t *testing.T) {
	origRepos, origShow, origNE := declaredRepos, showBead, git.NewEffortWorktree
	t.Cleanup(func() { declaredRepos, showBead, git.NewEffortWorktree = origRepos, origShow, origNE })
	reads, cuts := 0, 0
	declaredRepos = func(string) map[string]string { reads++; return nil }
	showBead = func(id string) (bd.Bead, error) {
		t.Errorf("bd show %s with no repo declared", id)
		return bd.Bead{}, nil
	}
	git.NewEffortWorktree = func(string, string, string) (string, func(), error) { cuts++; return "/wt", func() {}, nil }
	if _, _, err := CutTree("b1", route.Destination{Placement: route.Worktree, Container: "checkpoint/cp"}); err != nil {
		t.Fatal(err)
	}
	if reads != 1 || cuts != 1 {
		t.Errorf("repo reads = %d, cuts = %d; want 1 and 1", reads, cuts)
	}
}

// A nested container's leaf gets a Workspace host with only the child materialized,
// torn down child first; a child dir the Workspace doesn't ignore refuses the cut
// naming the dir (Spec FR 5, 19, 22).
func TestCutTreeMaterializesTheChildAndRefusesAnUnignoredDir(t *testing.T) {
	origShow := showBead
	t.Cleanup(func() { showBead = origShow })
	showBead = func(id string) (bd.Bead, error) {
		return bd.Bead{ID: id, Labels: []string{"kind:checkpoint", "context:child"}}, nil
	}
	g := func(dir string, args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@e.com", "-c", "user.name=t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	ws, err := filepath.EvalSymlinks(t.TempDir()) // git lists the resolved spelling
	if err != nil {
		t.Fatal(err)
	}
	g(ws, "init", "-q", "-b", "main")
	os.MkdirAll(filepath.Join(ws, ".mugicrew"), 0o755)
	os.WriteFile(filepath.Join(ws, ".mugicrew", "config.toml"), []byte("[context.child]\nrepo = \"child\"\n"), 0o644)
	g(ws, "add", ".")
	g(ws, "commit", "-q", "-m", "root")
	child := filepath.Join(ws, "child")
	os.Mkdir(child, 0o755)
	g(child, "init", "-q", "-b", "main")
	g(child, "commit", "-q", "--allow-empty", "-m", "root")
	g(child, "branch", "checkpoint/cp")
	t.Chdir(ws)
	d := route.Destination{Placement: route.Worktree, Branch: "effort/b1", Container: "checkpoint/cp"}

	if _, _, err := CutTree("b1", d); err == nil || !strings.Contains(err.Error(), `"child"`) || !strings.Contains(err.Error(), ".gitignore") {
		t.Fatalf("un-ignored child dir: err = %v, want it to name the dir and .gitignore", err)
	}

	os.WriteFile(filepath.Join(ws, ".gitignore"), []byte("child/\n"), 0o644)
	host, teardown, err := CutTree("b1", d)
	if err != nil {
		t.Fatal(err)
	}
	if id, err := git.Repo(filepath.Join(host, "child")).CurrentEffort(); err != nil || id != "b1" {
		t.Errorf("child tree effort = (%q, %v), want b1", id, err)
	}
	if _, err := os.Stat(filepath.Join(host, ".mugicrew", "config.toml")); err != nil {
		t.Errorf("host is not a Workspace checkout: %v", err)
	}
	teardown()
	if _, err := os.Stat(host); err == nil {
		t.Error("teardown left the host")
	}
	if g(child, "worktree", "list", "--porcelain") != "worktree "+child+"\nHEAD "+g(child, "rev-parse", "HEAD")+"\nbranch refs/heads/main" {
		t.Errorf("child still lists a linked worktree: %s", g(child, "worktree", "list"))
	}
}

// A nested effort with nothing ahead of its container is dropped from the child, and
// one with work is kept there (Spec FR 25).
func TestDropEmptyEffortActsInTheChild(t *testing.T) {
	_, kid := contextstest.Workspace(t)
	contextstest.Git(t, kid, "branch", "checkpoint/cp")
	contextstest.Git(t, kid, "branch", "effort/b1", "checkpoint/cp")
	contextstest.Git(t, kid, "branch", "effort/b2", "checkpoint/cp")
	contextstest.Git(t, kid, "checkout", "-q", "effort/b2")
	contextstest.Git(t, kid, "commit", "-q", "--allow-empty", "-m", "work")
	contextstest.Git(t, kid, "checkout", "-q", "main")
	// leaves carry no context: (ADR 0166); the container does
	old := contexts.ShowBead
	contexts.ShowBead = func(id string) (bd.Bead, error) {
		return bd.Bead{ID: id, Kind: contract.KindCheckpoint, Labels: []string{contextstest.Label}}, nil
	}
	t.Cleanup(func() { contexts.ShowBead = old })
	for _, id := range []string{"b1", "b2"} {
		b := bd.Bead{ID: id, Kind: contract.KindBuild, Parent: "cp"}
		if err := dropEmptyEffort(b); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	if contextstest.Git(t, kid, "branch", "--list", "effort/b1") != "" || contextstest.Git(t, kid, "branch", "--list", "effort/b2") == "" {
		t.Error("want the empty effort/b1 dropped from the child and effort/b2 kept")
	}
}

// Dropping an orphaned nested host takes the child's tree down first, so the child
// does not keep effort/<id> checked out in a vanished tree (Spec FR 22).
func TestDropOrphanRemovesTheChildTreeBeforeTheHost(t *testing.T) {
	ws, kid := contextstest.Workspace(t)
	host, down, err := git.NewDetachedWorktree("main")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(down)
	contextstest.Git(t, kid, "worktree", "add", "-q", "-b", "effort/b1", filepath.Join(host, "kid"))
	if err := DropOrphan(host); err != nil {
		t.Fatal(err)
	}
	if got := contextstest.Git(t, kid, "worktree", "list"); strings.Contains(got, "effort/b1") {
		t.Errorf("child still lists its tree: %s", got)
	}
	if _, err := os.Stat(host); err == nil {
		t.Errorf("host %s survives the drop (ws %s)", host, ws)
	}
}

// ProbeChildren resolves no Workspace root when no repo = is declared (Spec FR 4,
// 56, ADR 0168).
func TestProbeChildrenWithNoReposResolvesNoRoot(t *testing.T) {
	old, oldRoot := declaredRepos, git.RepoRoot
	t.Cleanup(func() { declaredRepos, git.RepoRoot = old, oldRoot })
	declaredRepos = func(string) map[string]string { return nil }
	git.RepoRoot = func() (string, error) { t.Error("RepoRoot called"); return "", nil }
	if kids := ProbeChildren(t.TempDir()); len(kids) != 0 {
		t.Errorf("children = %v", kids)
	}
}

// A nested effort cut by Materialize integrates through IntegrateIn from its recorded
// cut point: a container commit the child rewrote under it no longer conflicts, and
// the cut ref goes with the branch (ADR 0264).
func TestIntegrateInNestedReplaysFromTheCutRef(t *testing.T) {
	// the integrate's own rebase commits, so it needs an identity the sandbox lacks
	for _, k := range []string{"GIT_AUTHOR", "GIT_COMMITTER"} {
		t.Setenv(k+"_NAME", "t")
		t.Setenv(k+"_EMAIL", "t@e.com")
	}
	_, kid := contextstest.Workspace(t)
	g := func(dir string, args ...string) string { return contextstest.Git(t, dir, args...) }
	g(kid, "branch", "checkpoint/ws-cp")
	child := git.Repo(kid)
	os.WriteFile(filepath.Join(kid, "a.txt"), []byte("old\n"), 0o644)
	// container commit a.txt=old, then the effort is cut after it
	g(kid, "checkout", "-q", "checkpoint/ws-cp")
	g(kid, "add", "a.txt")
	g(kid, "commit", "-q", "-m", "a")
	oldTip := g(kid, "rev-parse", "HEAD")
	g(kid, "checkout", "-q", "main")
	path := filepath.Join(t.TempDir(), "eff")
	teardown, err := child.Materialize(path, "mc-b", "checkpoint/ws-cp", "")
	if err != nil {
		t.Fatal(err)
	}
	defer teardown()
	os.WriteFile(filepath.Join(path, "w.txt"), []byte("w\n"), 0o644)
	g(path, "add", "w.txt")
	g(path, "commit", "-q", "-m", "w")
	// the child rewrites the container's commit under the effort
	g(kid, "update-ref", "refs/heads/checkpoint/ws-cp", g(kid, "rev-parse", oldTip+"^"), oldTip)
	g(kid, "checkout", "-q", "--detach", "checkpoint/ws-cp")
	os.WriteFile(filepath.Join(kid, "a.txt"), []byte("new\n"), 0o644)
	g(kid, "add", "a.txt")
	g(kid, "commit", "-q", "-m", "a rewritten")
	g(kid, "update-ref", "refs/heads/checkpoint/ws-cp", "HEAD")
	g(kid, "checkout", "-q", "main")

	var out strings.Builder
	if code := IntegrateIn(Child{Repo: git.Repo(path), Main: child}, bd.Bead{ID: "mc-b", Kind: contract.KindBuild, Parent: "ws-cp"}, "agent", &out); code != ExitIntegrated {
		t.Fatalf("exit = %d, want 0 (output %q)", code, out.String())
	}
	if got := g(kid, "show", "checkpoint/ws-cp:a.txt"); got != "new" {
		t.Errorf("a.txt = %q, want the rewritten container's", got)
	}
	if got := g(kid, "show", "checkpoint/ws-cp:w.txt"); got != "w" {
		t.Errorf("w.txt = %q, want the effort's work", got)
	}
	teardown()
	if err := child.DeleteBranch("effort/mc-b"); err != nil {
		t.Fatal(err)
	}
	if g(kid, "for-each-ref", "refs/mugicrew/cut/") != "" {
		t.Error("cut ref outlived effort/mc-b in the child")
	}
}
