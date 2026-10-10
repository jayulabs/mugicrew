package bd

import (
	"os"
	"os/exec"
	"slices"
	"testing"

	"jayulabs.org/mugicrew/contract"
)

// The premise behind the frontier re-admit (Spec D6): bd ready hides an
// epic with an open blocker AND its child, so loop.Readmit has to put them back.
// If bd ever stops hiding the child, re-examine the re-admit rule. Also pins the
// reads the rule and the land guard take (OpenList's parent and blocks edges,
// OpenBlockers) and the release (FR 17): the epic's close readies a Task it blocks.
// Real bd in a scratch repo, skipped without it (the nix sandbox checkPhase).
func TestBdReadyHidesBlockedEpicChild(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not on PATH")
	}
	t.Setenv("BEADS_DIR", "")
	os.Unsetenv("BEADS_DIR")
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	t.Chdir(repo)
	if _, err := run("init", "--stealth", "-q"); err != nil {
		t.Fatal(err)
	}
	create := func(args ...string) string {
		t.Helper()
		id, err := run(append([]string{"create", "--silent", "--no-inherit-labels"}, args...)...)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	task := create("--type", "task", "--labels", "kind:task", "--title", "before land")
	cp := create("--type", "epic", "--labels", "kind:checkpoint", "--title", "checkpoint")
	build := create("--type", "task", "--labels", "kind:build", "--parent", cp, "--title", "build")
	after := create("--type", "task", "--labels", "kind:task", "--title", "after land")
	for _, e := range [][2]string{{cp, task}, {after, cp}} {
		if _, err := run("dep", "add", e[0], e[1]); err != nil {
			t.Fatal(err)
		}
	}

	ready, err := Ready()
	if err != nil {
		t.Fatal(err)
	}
	if ids := IDs(ready); !slices.Contains(ids, task) || slices.Contains(ids, cp) || slices.Contains(ids, build) {
		t.Errorf("bd ready = %v; want %s, without the held epic %s or its child %s", ids, task, cp, build)
	}
	open, err := OpenList()
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(open, func(b Bead) bool { return b.ID == cp })
	j := slices.IndexFunc(open, func(b Bead) bool { return b.ID == build })
	if i < 0 || j < 0 || !slices.Equal(open[i].BlockedBy, []string{task}) || open[j].Parent != cp {
		t.Errorf("OpenList = %+v; want %s blocked by %s and %s its child", open, cp, task, build)
	}
	held, err := OpenBlockers(cp)
	if err != nil || len(held) != 1 || held[0].ID != task || held[0].Kind != contract.KindTask || held[0].Title != "before land" {
		t.Errorf("OpenBlockers(%s) = %+v, %v; want the open Task %s", cp, held, err, task)
	}

	if err := Close(task, ""); err != nil {
		t.Fatal(err)
	}
	if held, err := OpenBlockers(cp); err != nil || len(held) != 0 {
		t.Errorf("OpenBlockers after the Task closed = %+v, %v; want none", held, err)
	}
	for _, id := range []string{build, cp} {
		if err := Close(id, ""); err != nil {
			t.Fatal(err)
		}
	}
	if ready, err = Ready(); err != nil || !slices.Contains(IDs(ready), after) {
		t.Errorf("bd ready after the land's close = %v, %v; want the released Task %s", IDs(ready), err, after)
	}
}
