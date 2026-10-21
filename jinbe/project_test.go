package jinbe

import (
	"errors"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
)

// legsFor resolves the bead's project once, under gitlab only: the Workspace's for a
// Workspace bead, the child's (with its path for MR lines) for a nested one.
func TestLegsForResolvesTheBeadsProjectUnderGitlab(t *testing.T) {
	stubJinbeSeams(t, &jinbeSeamRec{})
	t.Chdir(t.TempDir()) // legsFor reads git.TopLevelOf("."): root it in a scratch repo, not the checkout
	if out, err := exec.Command("git", "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	lg, ok, err := legsFor(openCheckpoint(), true)
	if err != nil || !ok || lg.proj != workspaceProject || lg.mrPath != "" {
		t.Errorf("workspace legs = %+v ok %v err %v, want the Workspace's project and no MR path", lg.proj, ok, err)
	}
	if lg, _, _ := legsFor(openCheckpoint(), false); lg.proj != (forge.Project{}) {
		t.Errorf("local legs carry project %v, want none (no Forge read under local)", lg.proj)
	}

	t.Cleanup(func(o func(bd.Bead) (git.Repo, string)) func() { return func() { repoDirOf = o } }(repoDirOf))
	repoDirOf = func(bd.Bead) (git.Repo, string) { return git.Repo("/ws/child"), "child" }
	lg, ok, err = legsFor(openCheckpoint(), true)
	if err != nil || !ok || lg.proj != childProject || lg.mrPath != "ns/child" {
		t.Errorf("nested legs = %+v path %q ok %v err %v, want the child's project and path", lg.proj, lg.mrPath, ok, err)
	}
}

// Every Forge arm of a pending tick acts on the legs' project, never the cwd's.
func TestMirrorMRArmsUseTheLegsProject(t *testing.T) {
	r := &jinbeSeamRec{}
	stubJinbeSeams(t, r)
	lg := legs{proj: childProject, repo: "/ws/child",
		compose: func(string, string, string, string) (string, string, string, error) { return "", "", "", nil }}
	l := openLanding("cafef00d")
	l.Description += "\nMR: ns/child!5\nChecked: forgesha\n"
	if _, err := mirrorMR(l, openCheckpoint(), "checkpoint/ws-cp", "Checkpoint: ws-cp", lg, "forgesha"); err != nil {
		t.Fatal(err)
	}
	if len(r.projects) < 2 || slices.ContainsFunc(r.projects, func(p forge.Project) bool { return p != childProject }) {
		t.Errorf("projects = %v, want only the child's (MR read, discussions)", r.projects)
	}
}

// A Checkpoint cut in the Workspace repo and then relabelled to a repo = context has no
// branch in the child: its Landing errors on the missing ref and does nothing
// irreversible (no land, no spawn, no close), with or without the loop's hold.
func TestLandingOfRelabelledUnstampedCheckpointErrorsOnMissingRef(t *testing.T) {
	r := &jinbeSeamRec{gated: true}
	stubJinbeSeams(t, r)
	root := t.TempDir()
	t.Chdir(root)
	child := filepath.Join(root, "child")
	for _, args := range [][]string{{"init", "-q", root}, {"init", "-q", "-b", "main", child}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if out, err := exec.Command("git", "-C", child, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init").CombinedOutput(); err != nil {
		t.Fatalf("commit: %v: %s", err, out)
	}
	t.Cleanup(func(o func(bd.Bead) (git.Repo, string)) func() { return func() { repoDirOf = o } }(repoDirOf))
	repoDirOf = func(bd.Bead) (git.Repo, string) { return git.Repo(child), "child" }
	cp := openCheckpoint()
	cp.Labels = []string{"context:x"} // relabelled, no repo: stamp
	showBead = func(string) (bd.Bead, error) { return cp, nil }
	err := reconcileLanding(openLanding("cafef00d"))
	if !errors.Is(err, git.ErrMissingRef) {
		t.Fatalf("err = %v, want ErrMissingRef", err)
	}
	if len(r.landed) != 0 || len(r.spawned) != 0 || len(r.closed) != 0 || len(r.reasons) != 0 {
		t.Errorf("land %v spawn %v close %v %v, want nothing irreversible", r.landed, r.spawned, r.closed, r.reasons)
	}
}
