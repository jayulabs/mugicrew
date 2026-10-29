package main

import (
	"bytes"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
)

var relabelArgv = []string{"/bin/mugicrew", "errand", "relabel"} // doctorWS stubs doctorExe

func TestDoctorLegacyTasksOKAtZero(t *testing.T) {
	doctorWS(t)
	out, _ := doctorRun(t)
	if !strings.Contains(out, "ok  task beads") {
		t.Errorf("out %q", out)
	}
}

func TestDoctorLegacyTasksWarnsWithCountAndSafeFix(t *testing.T) {
	doctorWS(t)
	doctorLegacyTasks = func(string) ([]bd.Bead, error) { return []bd.Bead{{ID: "a"}, {ID: "b"}}, nil }
	out, code := doctorRun(t)
	if code != 0 {
		t.Errorf("a warn exited %d", code)
	}
	for _, w := range []string{"warn  task beads: 2 bead(s) still labelled kind:task", "every walk-through bead carries kind:errand", "Safe — doctor fix runs:", "mugicrew errand relabel"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in %q", w, out)
		}
	}
}

func TestDoctorFixRelabelsOnceThenOK(t *testing.T) {
	ran, _ := fixWS(t, "acme.atlassian.net")
	left := 3
	doctorLegacyTasks = func(string) ([]bd.Bead, error) { return make([]bd.Bead, left), nil }
	doctorExec = func(argv []string) (string, error) {
		*ran = append(*ran, argv)
		left = 0
		return "relabelled 3", nil
	}
	out, _ := fixRun(t)
	if !reflect.DeepEqual(*ran, [][]string{relabelArgv}) {
		t.Errorf("ran %v", *ran)
	}
	if !strings.Contains(out, "applied  task beads: /bin/mugicrew errand relabel") || !strings.Contains(out, "ok  task beads") {
		t.Errorf("out %q", out)
	}
	out, _ = fixRun(t) // idempotent: nothing left, nothing run
	if len(*ran) != 1 || !strings.Contains(out, "nothing safe to apply") {
		t.Errorf("second run ran %v, out %q", *ran, out)
	}
}

func TestRunErrandRelabel(t *testing.T) {
	old := relabelLegacyTasks
	t.Cleanup(func() { relabelLegacyTasks = old })
	relabelLegacyTasks = func(string) (int, error) { return 2, nil }
	var b bytes.Buffer
	if err := runErrandRelabel(nil, &b); err != nil || !strings.Contains(b.String(), "relabelled 2") {
		t.Errorf("err %v out %q", err, b.String())
	}
	if err := runErrandRelabel([]string{"x"}, &b); err == nil {
		t.Error("extra argument accepted")
	}
}

// Against real bd on a scratch graph: the relabel keeps closed_at, the claim and the
// comments, in every status.
func TestRelabelLegacyTasksRealBd(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("no bd")
	}
	dir := t.TempDir()
	t.Setenv("BEADS_DIR", dir+"/.beads")
	bdRun := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("bd", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("bd %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	bdRun("init", "--prefix", "sc", "--non-interactive")
	if got := bdRun("config", "get", "issue_prefix"); got != "sc" {
		t.Fatalf("prefix %q: not the scratch graph", got)
	}
	open := bdRun("create", "t open", "-l", "kind:task", "-d", "x", "--silent")
	prog := bdRun("create", "t prog", "-l", "kind:task", "-d", "x", "--silent")
	closed := bdRun("create", "t closed", "-l", "kind:task", "-d", "x", "--silent")
	both := bdRun("create", "t both", "-l", "kind:task,kind:errand", "-d", "x", "--silent")
	bdRun("update", prog, "--claim")
	bdRun("close", closed, "--reason", "done")
	closedAt := func() string {
		bs, err := bd.ListByID([]string{closed})
		if err != nil || len(bs) != 1 {
			t.Fatalf("read closed: %v %v", bs, err)
		}
		return bdRun("show", closed, "--json")
	}
	before := closedAt()
	time.Sleep(1100 * time.Millisecond) // closed_at has second resolution
	n, err := bd.RelabelLegacyTasks(dir)
	if err != nil || n != 4 {
		t.Fatalf("relabel = %d, %v", n, err)
	}
	for _, id := range []string{open, prog, closed, both} {
		bs, err := bd.ListByID([]string{id})
		if err != nil || len(bs) != 1 || !bs[0].HasLabel("kind:errand") || bs[0].HasLabel("kind:task") {
			t.Errorf("%s: %v %v", id, bs, err)
		}
		if cs := bdRun("comments", id); !strings.Contains(cs, "No comments") {
			t.Errorf("%s gained a comment: %s", id, cs)
		}
	}
	if bs, _ := bd.ListByID([]string{prog}); bs[0].Assignee == "" {
		t.Error("in-progress bead lost its assignee")
	}
	if after := closedAt(); !strings.Contains(after, closedAtOf(before)) {
		t.Errorf("closed_at moved: %s -> %s", closedAtOf(before), after)
	}
	if n, err := bd.RelabelLegacyTasks(dir); err != nil || n != 0 {
		t.Errorf("second run = %d, %v", n, err)
	}
}

// closedAtOf pulls the closed_at line's value out of bd show --json.
func closedAtOf(js string) string {
	i := strings.Index(js, `"closed_at": "`)
	if i < 0 {
		return "MISSING"
	}
	rest := js[i+len(`"closed_at": "`):]
	return rest[:strings.Index(rest, `"`)]
}
