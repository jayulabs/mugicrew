package jinbe

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/looplock"
)

type cmdRun struct {
	dirs, cmds []string
	err        error
}

// cmdGateRun lands a Workspace Checkpoint with the top-level scope resolved to spec
// and a stub command runner returning err.
func cmdGateRun(t *testing.T, r *jinbeSeamRec, spec cfglayer.GateSpec, err error) (*cmdRun, error, []eventlogtest.Event) {
	t.Helper()
	t.Chdir(t.TempDir()) // the command runs at git.TopLevelOf("."): root it in a scratch repo, not the sandbox's repo-less cwd
	if out, err := exec.Command("git", "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	r.gated, r.head = true, "cafef00d"
	evs := eventlogtest.Capture(t)
	stubJinbeSeams(t, r)
	gateSpec = func() cfglayer.GateSpec { return spec }
	run := &cmdRun{}
	old := gateCommand
	t.Cleanup(func() { gateCommand = old })
	gateCommand = func(dir, cmd string) error {
		run.dirs, run.cmds = append(run.dirs, dir), append(run.cmds, cmd)
		return err
	}
	lerr := reconcileLanding(openLanding("cafef00d"))
	return run, lerr, *evs
}

var cmdSpec = cfglayer.GateSpec{Kind: cfglayer.GateCommand, Value: "make test"}

func TestCommandGatePassLandsAtTheSameBase(t *testing.T) {
	r := &jinbeSeamRec{}
	run, err, evs := cmdGateRun(t, r, cmdSpec, nil)
	if err != nil || !slices.Equal(run.cmds, []string{"make test"}) || r.gates != 0 {
		t.Fatalf("err %v cmds %v nix builds %d", err, run.cmds, r.gates)
	}
	if got := eventlogtest.Names(evs); !slices.Equal(got, []string{"gate.started", "gate.passed", "land.done"}) {
		t.Fatalf("events = %v", got)
	}
	if s := evs[0]; s.Attrs["cmd"] != "make test" || s.Attrs["ref"] != nil {
		t.Errorf("gate.started = %v", s.Attrs)
	}
	if p := evs[1]; p.Attrs["out"] != nil || p.Attrs["dur_ms"] == nil {
		t.Errorf("gate.passed = %v", p.Attrs)
	}
}

func TestCommandGateBuilderFailureFilesACritique(t *testing.T) {
	r := &jinbeSeamRec{}
	_, err, evs := cmdGateRun(t, r, cmdSpec, &looplock.BuilderError{Exit: 2, Log: "FAIL one\n"})
	if err != nil || len(r.spawned) != 1 {
		t.Fatalf("err %v spawned %v", err, r.spawned)
	}
	c := r.spawned[0]
	if c.Kind != contract.KindCritique || c.Title != "Checkpoint ws-cp fails the pinned gate command" {
		t.Errorf("critique = %+v", c)
	}
	for _, want := range []string{"`make test` exited 2", "Base: ", "Tip: ", "Exit: 2\n```\nFAIL one\n```", "sh -c 'make test'` at the repo root, in the loop's environment."} {
		if !strings.Contains(c.Description, want) {
			t.Errorf("body missing %q:\n%s", want, c.Description)
		}
	}
	if !slices.Equal(r.reasons, []string{"obsolete: build failed"}) {
		t.Errorf("reasons %v", r.reasons)
	}
	e := evs[1]
	if e.Name != "gate.failed" || e.Attrs["exit"] != 2 || e.Attrs["critique"] != "ws-conf" || e.Attrs["drv"] != nil || e.Attrs["dur_ms"] == nil {
		t.Errorf("gate.failed = %+v", e)
	}
}

func TestCommandGateInfraLeavesTheLandingOpen(t *testing.T) {
	r := &jinbeSeamRec{}
	_, err, evs := cmdGateRun(t, r, cmdSpec, &looplock.InfraError{Err: errors.New("x"), Tail: "tail", Timeout: true})
	if err == nil || len(r.closed) != 0 || len(r.spawned) != 0 {
		t.Fatalf("err %v closed %v spawned %v", err, r.closed, r.spawned)
	}
	if e := evs[1]; e.Name != "gate.infra" || e.Attrs["err"] != "tail" || e.Attrs["timeout"] != true || e.Attrs["dur_ms"] == nil {
		t.Errorf("gate.infra = %+v", e)
	}
}

func TestCommandGateDedupeRunsNothing(t *testing.T) {
	body := git.BasePin("base1") + "\n" + git.TipPin("cafef00d")
	r := &jinbeSeamRec{kids: []bd.Bead{{ID: "ws-crit", Kind: contract.KindCritique, Status: "open", Description: body}}}
	run, err, evs := cmdGateRun(t, r, cmdSpec, nil)
	if err != nil || len(run.cmds) != 0 || len(evs) != 1 {
		t.Fatalf("err %v cmds %v events %v", err, run.cmds, eventlogtest.Names(evs))
	}
	a := evs[0].Attrs
	if a["critique"] != "deduped" || fmt.Sprint(a["dur_ms"]) != "0" || a["exit"] != nil || a["drv"] != nil {
		t.Errorf("gate.failed = %v", a)
	}
}

func TestBothGateKeysIsInfraNamingTheScope(t *testing.T) {
	r := &jinbeSeamRec{}
	spec := cfglayer.GateSpec{Err: errors.New("the top level sets both gate and gate_command; set only one")}
	run, err, evs := cmdGateRun(t, r, spec, nil)
	if err == nil || len(run.cmds) != 0 || len(r.closed) != 0 || r.gates != 0 {
		t.Fatalf("err %v cmds %v closed %v", err, run.cmds, r.closed)
	}
	if e := evs[0]; e.Name != "gate.infra" || !strings.Contains(e.Attrs["err"].(string), "the top level") {
		t.Errorf("event = %+v", e)
	}
}

func TestNoGateKeyStillSkips(t *testing.T) {
	r := &jinbeSeamRec{}
	run, err, evs := cmdGateRun(t, r, cfglayer.GateSpec{}, nil)
	if err != nil || len(run.cmds) != 0 || eventlogtest.Names(evs)[0] != "gate.skipped" {
		t.Errorf("err %v cmds %v events %v", err, run.cmds, eventlogtest.Names(evs))
	}
}

func TestChartLandNeverRunsTheCommand(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", gated: true}
	stubJinbeSeams(t, r)
	showBead = func(string) (bd.Bead, error) {
		return bd.Bead{ID: "ws-cp", Kind: contract.KindChart, Status: "open"}, nil
	}
	gateSpec = func() cfglayer.GateSpec { return cmdSpec }
	old := gateCommand
	t.Cleanup(func() { gateCommand = old })
	gateCommand = func(string, string) error { t.Error("command ran for a Chart land"); return nil }
	landBranch = func(string, string, git.LandStrategy) (string, error) { return "sha", nil }
	if err := reconcileLanding(openLanding("cafef00d")); err != nil {
		t.Fatal(err)
	}
}

// A non-local strategy (gitlab) is gated by its MR pipeline, so the command never runs.
func TestNonLocalStrategyNeverRunsTheCommand(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d"} // gated unset: land_strategy resolves to gitlab
	stubJinbeSeams(t, r)
	gateSpec = func() cfglayer.GateSpec { return cmdSpec }
	old := gateCommand
	t.Cleanup(func() { gateCommand = old })
	gateCommand = func(string, string) error { t.Error("command ran under a non-local strategy"); return nil }
	_ = reconcileLanding(openLanding("cafef00d"))
}

// A nested repo = Checkpoint runs its own context's command in the child root.
func TestNestedCommandGateRunsInTheChildRoot(t *testing.T) {
	child, g := nestedWS(t)
	r := &jinbeSeamRec{}
	nestedSeams(t, r, "repo:child")
	tip := g(string(child), "rev-parse", "checkpoint/ws-cp")
	var ctxName string
	contextGateSpec = func(_, name string) cfglayer.GateSpec {
		ctxName = name
		return cfglayer.GateSpec{Kind: cfglayer.GateCommand, Value: "go test ./..."}
	}
	gateSpec = func() cfglayer.GateSpec {
		t.Error("top-level gate read for a repo = Checkpoint")
		return cfglayer.GateSpec{}
	}
	var dir, cmd string
	old := gateCommand
	gateCommand = func(d, c string) error { dir, cmd = d, c; return &looplock.BuilderError{Exit: 1, Log: "bad"} }
	t.Cleanup(func() { contextGateSpec = cfglayer.ResolveContextGate; gateCommand = old })
	if err := reconcileLanding(openLanding(tip)); err != nil {
		t.Fatal(err)
	}
	if ctxName != "child" || cmd != "go test ./..." || filepath.Base(dir) != "child" {
		t.Errorf("ctx %q cmd %q dir %q", ctxName, cmd, dir)
	}
	if len(r.spawned) != 1 || !strings.Contains(r.spawned[0].Description, "at the root of the nested repo `child` (git -C child), in the loop's environment.") {
		t.Errorf("spawned %v", r.spawned)
	}
}

// stubReset replaces the tree reset with a recorder of (dir, composes so far).
func stubReset(t *testing.T, r *jinbeSeamRec, err error) *[]string {
	t.Helper()
	old := resetLandTree
	t.Cleanup(func() { resetLandTree = old })
	var got []string
	resetLandTree = func(dir string) error {
		got = append(got, fmt.Sprintf("%s@%d", filepath.Base(dir), r.composes))
		return err
	}
	return &got
}

func TestCommandGateResetsTheTreeBeforeTheSecondCompose(t *testing.T) {
	moved := &git.BaseMoved{Base: "b", Now: "n"}
	r := &jinbeSeamRec{landErrs: []error{moved}}
	resets := stubReset(t, r, nil)
	run, err, evs := cmdGateRun(t, r, cmdSpec, nil)
	if err != nil || len(run.cmds) != 2 || r.composes != 2 {
		t.Fatalf("err %v cmds %v composes %d", err, run.cmds, r.composes)
	}
	if len(*resets) != 1 || !strings.HasSuffix((*resets)[0], "@1") {
		t.Errorf("resets = %v, want one after the first compose and before the second", *resets)
	}
	if got := eventlogtest.Names(evs); got[len(got)-1] != "land.done" {
		t.Errorf("events = %v", got)
	}
}

func TestFailingResetIsInfraWithTheLandingOpen(t *testing.T) {
	moved := &git.BaseMoved{Base: "b", Now: "n"}
	r := &jinbeSeamRec{landErrs: []error{moved}}
	stubReset(t, r, errors.New("clean: permission denied"))
	run, err, evs := cmdGateRun(t, r, cmdSpec, nil)
	var infra *looplock.InfraError
	if !errors.As(err, &infra) || len(run.cmds) != 1 || r.composes != 1 || len(r.closed) != 0 {
		t.Fatalf("err %v cmds %v composes %d closed %v", err, run.cmds, r.composes, r.closed)
	}
	e := evs[len(evs)-1]
	if e.Name != "gate.infra" || !strings.Contains(e.Attrs["err"].(string), "permission denied") {
		t.Errorf("last event = %+v", e)
	}
}

func TestNixGateRetryDoesNoResetAndThreeMovesArePending(t *testing.T) {
	moved := &git.BaseMoved{Base: "b", Now: "n"}
	r := &jinbeSeamRec{landErrs: []error{moved, moved, moved}}
	resets := stubReset(t, r, nil)
	err, evs := gateRun(t, r)
	if err != nil || len(*resets) != 0 || r.composes != 3 {
		t.Fatalf("err %v resets %v composes %d", err, *resets, r.composes)
	}
	if got := eventlogtest.Names(evs); got[len(got)-1] != "gate.pending" {
		t.Errorf("events = %v", got)
	}
}

func TestNestedCommandGateResetsOnlyTheChildRoot(t *testing.T) {
	child, g := nestedWS(t)
	r := &jinbeSeamRec{}
	nestedSeams(t, r, "repo:child")
	tip := g(string(child), "rev-parse", "checkpoint/ws-cp")
	contextGateSpec = func(_, _ string) cfglayer.GateSpec { return cmdSpec }
	var cmdDirs []string
	old := gateCommand
	gateCommand = func(d, _ string) error { // the first run sees mainline move under it
		if cmdDirs = append(cmdDirs, d); len(cmdDirs) == 1 {
			g(string(child), "commit", "--allow-empty", "-qm", "mainline moves")
		}
		return nil
	}
	t.Cleanup(func() { contextGateSpec = cfglayer.ResolveContextGate; gateCommand = old })
	resets := stubReset(t, r, nil)
	if err := reconcileLanding(openLanding(tip)); err != nil {
		t.Fatal(err)
	}
	if len(*resets) != 1 || !strings.HasPrefix((*resets)[0], "child@") || len(cmdDirs) != 2 {
		t.Errorf("resets %v command dirs %v", *resets, cmdDirs)
	}
}
