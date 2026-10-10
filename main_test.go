package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/brook"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/jinbe"
	"jayulabs.org/mugicrew/loop"
	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/nami"
)

// The reconcile verb resolves a kind's owner through the registry + crew table
// and dispatches via Crewmate.Run. research must reach Nami (whose Run takes the
// agentic runResearch path) instead of erroring as the old hardcoded
// nami.Reconcile did on a kind with no reconcile handler (FR4).
func TestCrewForRoutesKindToOwner(t *testing.T) {
	c, err := crewFor(contract.KindResearch)
	if err != nil {
		t.Fatalf("crewFor(research): %v", err)
	}
	if _, ok := c.(nami.Nami); !ok {
		t.Errorf("research owner = %T, want nami.Nami", c)
	}
	// The Checkpoint container resolves to Jinbe's crew() entry: the
	// registry row jinbe→checkpoint now has a wired crewmate, so the loop dispatches it.
	cp, err := crewFor(contract.KindCheckpoint)
	if err != nil {
		t.Fatalf("crewFor(checkpoint): %v", err)
	}
	if _, ok := cp.(jinbe.Jinbe); !ok {
		t.Errorf("checkpoint owner = %T, want jinbe.Jinbe", cp)
	}
	// A Task resolves to Brook's crew() entry (Spec D2), so l launches it.
	tk, err := crewFor(contract.KindTask)
	if err != nil {
		t.Fatalf("crewFor(task): %v", err)
	}
	if _, ok := tk.(brook.Brook); !ok {
		t.Errorf("task owner = %T, want brook.Brook", tk)
	}
	// An unregistered kind is a loud error, not a nil crewmate.
	if _, err := crewFor(contract.Kind("nonesuch")); err == nil {
		t.Error("unknown kind must error")
	}
}

// A leading -C <dir> (Spec FR2) is stripped before verb dispatch, bare TUI
// included, and makes the Workspace root this process's cwd, so every child it
// spawns (detached loop, pinned/restart exec, tmux panes, reconcile) starts there.
// Without -C the argv and cwd are untouched (self-hosting parity).
func TestWorkspaceArgsResolvesDashC(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"commit", "-q", "--allow-empty", "-m", "root"}} {
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	elsewhere := t.TempDir()

	for _, tc := range []struct {
		argv, want []string
	}{
		{[]string{"-C", sub, "loop", "status"}, []string{"loop", "status"}},
		{[]string{"-C", root}, []string{}}, // the bare TUI path
	} {
		t.Chdir(elsewhere)
		got, err := workspaceArgs(tc.argv)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("workspaceArgs(%v) = %v, %v; want %v", tc.argv, got, err, tc.want)
		}
		pwd, err := exec.Command("pwd").Output()
		if err != nil || strings.TrimSpace(string(pwd)) != root {
			t.Errorf("after %v a spawned child's cwd = %q, %v; want the Workspace root %q", tc.argv, pwd, err, root)
		}
	}

	t.Chdir(elsewhere)
	if got, err := workspaceArgs([]string{"ps"}); err != nil || !slices.Equal(got, []string{"ps"}) {
		t.Errorf("no -C: workspaceArgs = %v, %v; want argv untouched", got, err)
	}
	if cwd, _ := os.Getwd(); cwd != elsewhere {
		t.Errorf("no -C: cwd = %q, want it untouched %q", cwd, elsewhere)
	}
	for _, argv := range [][]string{{"-C"}, {"-C", elsewhere, "ps"}} {
		if _, err := workspaceArgs(argv); err == nil {
			t.Errorf("workspaceArgs(%v): want an error (no dir / not a git repo)", argv)
		}
	}
}

// Every user verb (Spec FR6) resolves the Bead Graph first and refuses
// when none resolves; internal verbs (reconcile, pivot, abort, ps) only inherit.
// A resolved graph is exported as BEADS_DIR, so every child mugicrew spawns
// carries it (FR8, FR9).
func TestRequireGraphRefusesUserVerbsAndExportsBeadsDir(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("BEADS_DIR", "")
	orig, origRoot := graphDir, git.RepoRoot
	t.Cleanup(func() { graphDir, git.RepoRoot = orig, origRoot })
	git.RepoRoot = func() (string, error) { return ws, nil }
	var asked []string
	graphDir = func(dir string) (string, error) {
		asked = append(asked, dir)
		return "", &bd.NoGraphError{Dir: dir}
	}

	for _, argv := range [][]string{{}, {"signal", "new"}, {"signal", "triage", "mc-1"}, {"surgery", "new"}, {"surgery", "end", "mc-1"}, {"loop"}, {"loop", "--watch"}, {"loop", "start"}, {"loop", "start", "--watch"}, {"loop", "status"}, {"loop", "stop"}, {"loop", "--rev=abc", "--lock-fd=3"}, {"chart", "append", "mc-1", "Notes", "- x"}} {
		err := requireGraph(argv)
		if err == nil || !strings.Contains(err.Error(), "bd init") {
			t.Errorf("requireGraph(%v) = %v, want the no-graph refusal", argv, err)
		}
	}
	for _, argv := range [][]string{{"reconcile", "mc-1"}, {"pivot", "confirm", "mc-1"}, {"abort", "mc-1"}, {"ps"}} {
		asked = nil
		if err := requireGraph(argv); err != nil || asked != nil {
			t.Errorf("requireGraph(%v) = %v, asked %v; want an internal verb to only inherit", argv, err, asked)
		}
	}
	// The loop image inherits its launcher's BEADS_DIR; one with none was exec'd by
	// a pre-ADR-0089 loop's restart and resolves the graph itself.
	t.Setenv("BEADS_DIR", "/g/.beads")
	asked = nil
	if err := requireGraph([]string{"loop", "--detached", "--rev=abc"}); err != nil || asked != nil {
		t.Errorf("loop image with BEADS_DIR: requireGraph = %v, asked %v; want it to only inherit", err, asked)
	}
	t.Setenv("BEADS_DIR", "")

	graphDir = func(dir string) (string, error) { asked = append(asked, dir); return "/g/.beads", nil }
	asked = nil
	if err := requireGraph([]string{"loop", "start"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(asked, []string{ws}) {
		t.Errorf("graph resolved from %v, want the Workspace root %q", asked, ws)
	}
	out, err := exec.Command("sh", "-c", "echo $BEADS_DIR").Output()
	if got := strings.TrimSpace(string(out)); err != nil || got != "/g/.beads" {
		t.Errorf("a spawned child's BEADS_DIR = %q, %v; want /g/.beads", got, err)
	}
}

// runInteractive runs the composed triage command in the cwd its bead routed to:
// it sets the exec'd command's Dir to that cwd (repo root for a leaf
// Signal) and hands it the terminal. Asserted via the argv + cwd seam
// (interactiveExec) with no real process.
func TestRunInteractiveSetsCwdAndArgv(t *testing.T) {
	orig := interactiveExec
	defer func() { interactiveExec = orig }()
	noDrafts(t)

	var got *exec.Cmd
	interactiveExec = func(cmd *exec.Cmd) error { got = cmd; return nil }

	ac := contract.AgentCommand{Argv: []string{"mugicrew", "signal", "triage", "sig1"}}
	if err := runInteractive(ac, "/repo/root", bd.Bead{ID: "sig1"}); err != nil {
		t.Fatalf("runInteractive: %v", err)
	}
	if got == nil {
		t.Fatal("interactiveExec must receive the composed command")
	}
	if got.Dir != "/repo/root" {
		t.Errorf("triage cwd = %q, want the repo root", got.Dir)
	}
	if strings.Join(got.Args, " ") != "mugicrew signal triage sig1" {
		t.Errorf("triage argv = %v, want the triage verb", got.Args)
	}

	// An empty argv is a loud error, not a spawn.
	if err := runInteractive(contract.AgentCommand{}, "/repo/root", bd.Bead{}); err == nil {
		t.Error("empty argv must error")
	}
}

// The triage verb hands a claude session to the TTY just like the TUI, so it
// records hitl.launched with the pre-assigned session id and hitl.exited with the
// session's exit (Spec FR29).
func TestRunInteractiveEmitsHitlEvents(t *testing.T) {
	origExec := interactiveExec
	defer func() { interactiveExec = origExec }()
	noDrafts(t)

	// The session writes its transcript's cost-state, which hitl.exited carries
	// top-level like hitl-run's (Spec FR11).
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	interactiveExec = func(*exec.Cmd) error {
		os.MkdirAll(cfg+"/projects/-repo", 0o755)
		os.WriteFile(cfg+"/projects/-repo/sess-1.jsonl", []byte(`{"type":"cost-state","totalCostUSD":0.75,"totalAPIDuration":900,"modelUsage":{},"hasUnknownModelCost":false}`+"\n"), 0o644)
		return exec.Command("sh", "-c", "exit 3").Run()
	}
	capt := eventlogtest.Capture(t)

	ac := contract.AgentCommand{Argv: []string{"claude", "--session-id", "sess-1"}, SessionID: "sess-1"}
	if err := runInteractive(ac, "/repo", bd.Bead{ID: "sig1", Kind: contract.KindSignal}); err == nil {
		t.Fatal("a non-zero session exit must surface as an error")
	}
	evs := *capt
	if len(evs) != 2 || evs[0].Name != "hitl.launched" || evs[1].Name != "hitl.exited" {
		t.Fatalf("events = %+v, want hitl.launched then hitl.exited", evs)
	}
	if a := evs[0].Attrs; a["session"] != "sess-1" || a["bead"] != "sig1" || a["crewmate"] != "nami" {
		t.Errorf("hitl.launched attrs = %v, want session sess-1, bead sig1, crewmate nami", a)
	}
	if a := evs[1].Attrs; a["session"] != "sess-1" || a["exit"] != 3 {
		t.Errorf("hitl.exited attrs = %v, want session sess-1, exit 3", a)
	}
	if a := evs[1].Attrs; a["cost_usd"] != 0.75 || a["api_ms"] != 900 {
		t.Errorf("hitl.exited attrs = %v, want the transcript's cost", a)
	}
}

// noDrafts stubs the triage verb's Draft delete/publish to no-ops, so a test of
// its exec seam touches no live bd.
func noDrafts(t *testing.T) {
	t.Helper()
	origDrop, origPub := dropDrafts, publishDrafts
	t.Cleanup(func() { dropDrafts, publishDrafts = origDrop, origPub })
	dropDrafts = func(bd.Bead) error { return nil }
	publishDrafts = func(string, []any) error { return nil }
}

// The triage verb ends a session like hitl-run (ADR 0059 §4, §6): it
// deletes a dead run's leftover Drafts before launching, publishes the session's
// Drafts on a clean exit, and keeps them on a failed one.
func TestRunInteractiveDraftLifecycle(t *testing.T) {
	origExec, origDrop, origPub := interactiveExec, dropDrafts, publishDrafts
	defer func() { interactiveExec, dropDrafts, publishDrafts = origExec, origDrop, origPub }()

	var calls []string
	dropDrafts = func(b bd.Bead) error { calls = append(calls, "drop "+b.ID); return nil }
	publishDrafts = func(b string, _ []any) error { calls = append(calls, "publish "+b); return nil }
	ac := contract.AgentCommand{Argv: []string{"claude"}}

	interactiveExec = func(*exec.Cmd) error { calls = append(calls, "run"); return nil }
	if err := runInteractive(ac, "/repo", bd.Bead{ID: "sig1"}); err != nil {
		t.Fatalf("runInteractive: %v", err)
	}
	if got := strings.Join(calls, ","); got != "drop sig1,run,publish sig1" {
		t.Errorf("clean exit calls = %s, want drop, run, publish", got)
	}

	calls = nil
	interactiveExec = func(*exec.Cmd) error { calls = append(calls, "run"); return errors.New("rc=1") }
	if err := runInteractive(ac, "/repo", bd.Bead{ID: "sig1"}); err == nil {
		t.Fatal("a failed session must error")
	}
	if got := strings.Join(calls, ","); got != "drop sig1,run" {
		t.Errorf("failed exit calls = %s, want drop, run (Drafts kept)", got)
	}

	// A failed publish fails the verb; a failed leftover delete launches nothing.
	interactiveExec = func(*exec.Cmd) error { return nil }
	publishDrafts = func(string, []any) error { return errors.New("bd down") }
	if err := runInteractive(ac, "/repo", bd.Bead{ID: "sig1"}); err == nil {
		t.Error("a failed publish must error")
	}
	calls = nil
	dropDrafts = func(bd.Bead) error { return errors.New("bd down") }
	interactiveExec = func(*exec.Cmd) error { calls = append(calls, "run"); return nil }
	if err := runInteractive(ac, "/repo", bd.Bead{ID: "sig1"}); err == nil || len(calls) != 0 {
		t.Errorf("failed drop: err=%v calls=%v, want an error and no launch", err, calls)
	}
}

// fakeBd puts a stub `bd` first on PATH (bd.run shells out by name): `show`
// prints showJSON, `dep list` prints depsJSON, anything else fails. Every argv is
// appended to the returned log, so a test can assert what was (not) written.
func fakeBd(t *testing.T, showJSON, depsJSON string) (logPath string) {
	t.Helper()
	dir := t.TempDir()
	logPath = filepath.Join(dir, "bd.log")
	script := `#!/bin/sh
echo "$*" >> "$FAKE_BD_LOG"
[ -n "$FAKE_BD_PATH" ] && echo "$PATH" >> "$FAKE_BD_PATH"
case "$1" in
show) printf '%s' "$FAKE_BD_SHOW" ;;
dep) printf '%s' "$FAKE_BD_DEPS" ;;
*) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_BD_LOG", logPath)
	t.Setenv("FAKE_BD_SHOW", showJSON)
	t.Setenv("FAKE_BD_DEPS", depsJSON)
	return logPath
}

func bdLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

// The fast-lane flags of `mugicrew reconcile` (ADR 0035 §10): a bad
// flag set is a usage error before bd is touched, and every refusal — a non-Signal
// kind, a missing or empty body file, a Signal already charted — errors with no
// `bd create` (nothing spawned).
func TestRunReconcileFastLaneRefusals(t *testing.T) {
	signal := `[{"id":"mc-s","title":"sig","status":"open","labels":["kind:signal"]}]`
	chart := `[{"id":"mc-c","title":"ch","status":"open","labels":["kind:chart"]}]`
	body := filepath.Join(t.TempDir(), "spec.md")
	if err := os.WriteFile(body, []byte("the spec body"), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(t.TempDir(), "empty.md")
	if err := os.WriteFile(empty, []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, argv := range [][]string{
		{},
		{"mc-s", "--title", "t"},          // --title without --spec
		{"mc-s", "--spec"},                // --spec missing its value
		{"mc-s", "--spec", body, "extra"}, // stray positional
		{"mc-s", "--bogus"},
	} {
		log := fakeBd(t, signal, "[]")
		if err := runReconcile(argv); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Errorf("runReconcile(%q) = %v, want a usage error", argv, err)
		}
		if l := bdLog(t, log); l != "" {
			t.Errorf("runReconcile(%q) touched bd before rejecting its flags: %q", argv, l)
		}
	}

	// A non-Signal kind is a usage error (D2), checked before the body file
	// is read — so even a missing file reports the kind, not the read failure.
	fakeBd(t, chart, "[]")
	if err := runReconcile([]string{"mc-c", "--spec", filepath.Join(t.TempDir(), "nope.md")}); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
		t.Errorf("--spec on a Chart = %v, want a usage error", err)
	}

	for name, tc := range map[string]struct {
		show, deps, file string
	}{
		"non-signal kind": {chart, "[]", body},
		"missing body":    {signal, "[]", filepath.Join(t.TempDir(), "nope.md")},
		"empty body":      {signal, "[]", empty},
		"charted signal":  {signal, `[{"id":"mc-c","labels":["kind:chart"]}]`, body},
	} {
		log := fakeBd(t, tc.show, tc.deps)
		if err := runReconcile([]string{"mc-s", "--spec", tc.file}); err == nil {
			t.Errorf("%s: runReconcile must error", name)
		}
		if l := bdLog(t, log); strings.Contains(l, "create") {
			t.Errorf("%s: spawned a successor despite refusing: %q", name, l)
		}
	}
}

// ADR 0069: `mugicrew reconcile` whose move of main hits a dirty main (here the
// Signal fast lane's anchor) exits non-zero with "clean main, then re-run" and
// writes no bead. Re-execs this binary as mugicrew over a real repo whose main
// worktree has an untracked file.
func TestReconcileMainDirtyExitsWithCleanMain(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "root"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "stray"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	log := fakeBd(t, `[{"id":"mc-s","title":"sig","status":"open","labels":["kind:signal"]}]`,
		`[{"id":"ws-sp","labels":["kind:spec"]}]`) // a seeded Spec successor: the fast lane
	self, _ := os.Executable()
	cmd := exec.Command(self, "reconcile", "mc-s")
	cmd.Env = append(os.Environ(), "MUGICREW_TEST_MAIN=1")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() == 0 {
		t.Fatalf("reconcile over a dirty main = %v, want a non-zero exit; output %s", err, out)
	}
	if !strings.Contains(string(out), "clean main, then re-run") {
		t.Errorf("output %q, want it to say clean main, then re-run", out)
	}
	if l := bdLog(t, log); strings.Contains(l, "create") || strings.Contains(l, "close") {
		t.Errorf("a dirty main must write no bead, bd saw %q", l)
	}
}

// `mugicrew pivot confirm` rejects a bad flag set as a usage error before bd is
// touched (the sweep itself is tested in zoro).
func TestRunPivotConfirmUsage(t *testing.T) {
	for _, argv := range [][]string{
		{},
		{"ws-pv"},                        // --spec is required
		{"ws-pv", "--title", "t"},        // still no --spec
		{"ws-pv", "--spec", "f", "more"}, // stray positional
	} {
		log := fakeBd(t, "[]", "[]")
		if err := runPivotConfirm(argv); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Errorf("runPivotConfirm(%q) = %v, want a usage error", argv, err)
		}
		if l := bdLog(t, log); l != "" {
			t.Errorf("runPivotConfirm(%q) touched bd: %q", argv, l)
		}
	}
}

// `mugicrew surgery new` refuses a missing or empty --title, as signal new does,
// before bd is touched.
func TestRunSurgeryNewUsage(t *testing.T) {
	for _, argv := range [][]string{
		{},
		{"--problem", "p"},          // --title is required
		{"--title", ""},             // and non-empty
		{"--title", "t", "--bogus"}, // unknown flag
		{"--title", "t", "more"},    // stray positional
	} {
		log := fakeBd(t, "[]", "[]")
		if err := runSurgeryNew(argv); err == nil {
			t.Errorf("runSurgeryNew(%q) = nil, want a usage error", argv)
		}
		if l := bdLog(t, log); l != "" {
			t.Errorf("runSurgeryNew(%q) touched bd: %q", argv, l)
		}
	}
}

// `mugicrew surgery end` refuses a missing id or stray args before bd is touched.
func TestRunSurgeryEndUsage(t *testing.T) {
	for _, argv := range [][]string{{}, {""}, {"ws-sg", "more"}} {
		log := fakeBd(t, "[]", "[]")
		if err := runSurgeryEnd(argv); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Errorf("runSurgeryEnd(%q) = %v, want a usage error", argv, err)
		}
		if l := bdLog(t, log); l != "" {
			t.Errorf("runSurgeryEnd(%q) touched bd: %q", argv, l)
		}
	}
}

// `mugicrew land-lineage` takes exactly one branch; anything else is a usage error
// (main exits non-zero on it) before the land lock or git is touched.
func TestRunLandLineageUsage(t *testing.T) {
	for _, argv := range [][]string{{}, {"lineage/stock/x-1", "more"}} {
		if err := runLandLineage(argv, io.Discard); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Errorf("runLandLineage(%q) = %v, want a usage error", argv, err)
		}
	}
}

// `mugicrew pivot escalate` likewise refuses a bad flag set before bd is touched.
func TestRunPivotEscalateUsage(t *testing.T) {
	for _, argv := range [][]string{
		{},
		{"ws-pv"},                            // --evidence is required
		{"ws-pv", "--spec", "f"},             // not a confirm flag
		{"ws-pv", "--evidence", "f", "more"}, // stray positional
	} {
		log := fakeBd(t, "[]", "[]")
		if err := runPivotEscalate(argv); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Errorf("runPivotEscalate(%q) = %v, want a usage error", argv, err)
		}
		if l := bdLog(t, log); l != "" {
			t.Errorf("runPivotEscalate(%q) touched bd: %q", argv, l)
		}
	}
}

// serveLoop holds the single-loop lock around the loop's run and is the loop's
// lifecycle writer: started once held, stopped on a clean exit, failed on a fatal
// error. A second loop is refused with the holder's pid and logs nothing.
func TestServeLoopLockAndLifecycleEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mugicrew", "loop.lock")
	evs := eventlogtest.Capture(t)
	var refusal error
	err := serveLoop(path, loopMarkers{lockFd: -1}, "", func(func(loop.State)) (*loop.Restart, error) {
		refusal = serveLoop(path, loopMarkers{lockFd: -1}, "", func(func(loop.State)) (*loop.Restart, error) { t.Fatal("second loop ran"); return nil, nil })
		var out strings.Builder
		runLoopStatus(path, &out)
		if want := fmt.Sprintf("on (pid %d)\n", os.Getpid()); out.String() != want {
			t.Errorf("status while held = %q; want %q", out.String(), want)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if refusal == nil || !strings.Contains(refusal.Error(), fmt.Sprintf("loop is already on (pid %d)", os.Getpid())) {
		t.Fatalf("second loop = %v; want a 'loop is already on' refusal", refusal)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"loop.started", "loop.stopped"}) {
		t.Fatalf("events = %v; want started, stopped (refusal unlogged)", got)
	}
	if (*evs)[0].Attrs["detached"] != false {
		t.Fatalf("loop.started attrs = %v; want detached=false", (*evs)[0].Attrs)
	}
	if _, ok := (*evs)[1].Attrs["dur_ms"]; !ok {
		t.Fatalf("loop.stopped attrs = %v; want dur_ms", (*evs)[1].Attrs)
	}
	var out strings.Builder
	runLoopStatus(path, &out)
	if out.String() != "off\n" {
		t.Fatalf("status after exit = %q; want off", out.String())
	}

	*evs = nil
	boom := errors.New("boom")
	if err := serveLoop(path, loopMarkers{lockFd: -1}, "", func(func(loop.State)) (*loop.Restart, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Fatalf("fatal run = %v; want boom", err)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"loop.started", "loop.failed"}) {
		t.Fatalf("events = %v; want started, failed", got)
	}

	*evs = nil
	blocker := filepath.Join(t.TempDir(), "file")
	os.WriteFile(blocker, nil, 0o644)
	if err := serveLoop(filepath.Join(blocker, "loop.lock"), loopMarkers{lockFd: -1}, "", func(func(loop.State)) (*loop.Restart, error) { return nil, nil }); err == nil {
		t.Fatal("unopenable lock: want an error")
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"loop.failed"}) {
		t.Fatalf("events = %v; want failed", got)
	}
}

// serveLoop maps each loop.State the run reports onto the lock body, and loop
// status --json passes every field through, restart_to and failed_rev included.
func TestServeLoopMapsStateOntoBody(t *testing.T) {
	eventlogtest.Capture(t)
	path := filepath.Join(t.TempDir(), "mugicrew", "loop.lock")
	pid := os.Getpid()
	err := serveLoop(path, loopMarkers{rev: "0123456789abcdef", lockFd: -1}, "/nix/store/x/bin/mugicrew", func(onState func(loop.State)) (*loop.Restart, error) {
		for _, c := range []struct {
			s    loop.State
			want string
		}{
			{loop.State{State: "on", Rev: "0123456789abcdef"},
				fmt.Sprintf(`{"state":"on","pid":%d,"rev":"0123456789abcdef","store_path":"/nix/store/x/bin/mugicrew"}`, pid)},
			{loop.State{State: "on", Rev: "0123456789abcdef", FailedRev: "fedcba9876543210"},
				fmt.Sprintf(`{"state":"on","pid":%d,"rev":"0123456789abcdef","store_path":"/nix/store/x/bin/mugicrew","failed_rev":"fedcba9876543210"}`, pid)},
			{loop.State{State: "draining", Rev: "0123456789abcdef", RestartTo: "fedcba9876543210", Inflight: []bd.Bead{{ID: "mc-a", Title: "Build: a"}}},
				fmt.Sprintf(`{"state":"draining","pid":%d,"rev":"0123456789abcdef","store_path":"/nix/store/x/bin/mugicrew","inflight":["Build: a"],"restart_to":"fedcba9876543210"}`, pid)},
		} {
			onState(c.s)
			var out strings.Builder
			if err := runLoopStatusJSON(path, &out); err != nil || out.String() != c.want+"\n" {
				t.Errorf("status --json after %+v = %q, %v; want %s", c.s, out.String(), err, c.want)
			}
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if runLoopStatusJSON(path, &out); out.String() != `{"state":"off","pid":0}`+"\n" {
		t.Fatalf("status --json after exit = %q; want off", out.String())
	}
}

// With an inherited lock fd (a restart's exec, ADR 0062 §5) serveLoop adopts the
// lock instead of acquiring it, rewrites the old image's body to its own (on, the
// new rev and store path, no restart_to or failed_rev) and logs loop.started
// restarted=true.
func TestServeLoopAdoptsInheritedLock(t *testing.T) {
	evs := eventlogtest.Capture(t)
	path := filepath.Join(t.TempDir(), "mugicrew", "loop.lock")
	old, err := looplock.Acquire(path, "0123456789abcdef", "/nix/store/old/bin/mugicrew", "")
	if err != nil {
		t.Fatal(err)
	}
	old.Write(looplock.Status{State: looplock.Draining, Rev: "0123456789abcdef", RestartTo: "fedcba9876543210", FailedRev: "0000"})
	fd, err := old.InheritFd()
	if err != nil {
		t.Fatal(err)
	}
	fd, err = syscall.Dup(fd) // the exec'd image's fd: same open file, so the flock stays held
	if err != nil {
		t.Fatal(err)
	}
	old.Close()
	err = serveLoop(path, loopMarkers{detached: true, rev: "fedcba9876543210", lockFd: fd}, "/nix/store/new/bin/mugicrew", func(func(loop.State)) (*loop.Restart, error) {
		var out strings.Builder
		want := fmt.Sprintf(`{"state":"on","pid":%d,"rev":"fedcba9876543210","store_path":"/nix/store/new/bin/mugicrew"}`, os.Getpid())
		if runLoopStatusJSON(path, &out); out.String() != want+"\n" {
			t.Errorf("status --json after adopt = %q; want %s", out.String(), want)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"loop.started", "loop.stopped"}) {
		t.Fatalf("events = %v; want started, stopped", got)
	}
	if a := (*evs)[0].Attrs; a["restarted"] != true || a["detached"] != true {
		t.Fatalf("loop.started attrs = %v; want restarted=true detached=true", a)
	}
	if st, _ := looplock.Read(path); st.State != looplock.Off {
		t.Fatalf("after exit: %+v; want off", st)
	}

	*evs = nil // an fd that does not hold the lock is refused, and that is a failed loop
	if err := serveLoop(path, loopMarkers{rev: "fedcba9876543210", lockFd: 0}, "", func(func(loop.State)) (*loop.Restart, error) { t.Fatal("ran"); return nil, nil }); err == nil {
		t.Fatal("adopt of stdin: want a refusal")
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"loop.failed"}) {
		t.Fatalf("events = %v; want failed", got)
	}
}

// A run that returns a *loop.Restart execs its build in place (ADR 0062 §5): each
// loop kind keeps its argv shape and source (ADR 0090 §3) and adds --lock-fd=N, the held lock's fd with
// close-on-exec cleared, after loop.restarting {rev, store_path, dur_ms}. An exec
// that returns failed: loop.failed.
func TestServeLoopRestartExecs(t *testing.T) {
	orig := execLoop
	t.Cleanup(func() { execLoop = orig })
	const sha, bin = "fedcba9876543210fedcba9876543210fedcba98", "/nix/store/new-mugicrew/bin/mugicrew"
	for _, detached := range []bool{false, true} {
		evs := eventlogtest.Capture(t)
		path := filepath.Join(t.TempDir(), "mugicrew", "loop.lock")
		var execd []string
		execLoop = func(b string, argv, env []string) error {
			execd = append([]string{b}, argv...)
			fd, _ := strconv.Atoi(strings.TrimPrefix(argv[len(argv)-1], lockFdArg))
			if flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_GETFD, 0); errno != 0 || flags&syscall.FD_CLOEXEC != 0 {
				t.Errorf("lock fd %d flags %d (%v); want open without close-on-exec", fd, flags, errno)
			}
			if st, _ := looplock.Read(path); st.State != looplock.Draining || st.Pid != os.Getpid() {
				t.Errorf("lock at exec = %+v; want still held, draining", st)
			}
			if !slices.Equal(env, os.Environ()) {
				t.Errorf("exec env differs from ours")
			}
			return errors.New("no exec")
		}
		err := serveLoop(path, loopMarkers{detached: detached, rev: "0123456789abcdef", source: "/src/.git", watch: true, lockFd: -1}, "/nix/store/old-mugicrew/bin/mugicrew", func(onState func(loop.State)) (*loop.Restart, error) {
			if st, _ := looplock.Read(path); !st.Watch {
				t.Errorf("lock body = %+v; want watch: true", st)
			}
			onState(loop.State{State: looplock.Draining, Rev: "0123456789abcdef", RestartTo: sha})
			return &loop.Restart{Rev: sha, Bin: bin}, nil
		})
		if err == nil {
			t.Fatal("failed exec: want an error")
		}
		want := []string{bin, bin, "loop", revArg + sha, sourceArg + "/src/.git", watchArg}
		if detached {
			want = []string{bin, bin, "loop", detachedArg, revArg + sha, sourceArg + "/src/.git", watchArg}
		}
		if len(execd) != len(want)+1 || !slices.Equal(execd[:len(want)], want) || !strings.HasPrefix(execd[len(want)], lockFdArg) {
			t.Fatalf("detached=%v: exec'd %v; want %v %sN", detached, execd, want, lockFdArg)
		}
		if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"loop.started", "loop.restarting", "loop.failed"}) {
			t.Fatalf("events = %v; want started, restarting, failed", got)
		}
		a := (*evs)[1].Attrs
		if _, ok := a["dur_ms"]; !ok || a["rev"] != sha || a["store_path"] != "/nix/store/new-mugicrew" {
			t.Fatalf("loop.restarting = %v; want rev, store_path, dur_ms", a)
		}
	}
}

// A force stop has already logged its own effort.abandoned/loop.forced: serveLoop
// adds neither loop.stopped nor loop.failed, and the process exits cleanly.
func TestServeLoopForcedLogsNoStop(t *testing.T) {
	evs := eventlogtest.Capture(t)
	path := filepath.Join(t.TempDir(), "mugicrew", "loop.lock")
	if err := serveLoop(path, loopMarkers{lockFd: -1}, "", func(func(loop.State)) (*loop.Restart, error) { return nil, loop.ErrForced }); err != nil {
		t.Fatalf("forced = %v; want nil", err)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"loop.started"}) {
		t.Fatalf("events = %v; want only loop.started", got)
	}
}

// loop stop signals the lock holder's pid once; --force waits for the loop to read
// draining, then signals again (never SIGKILL). Against a draining loop the one
// signal already forces, so nothing more is sent; against an off loop it reports
// off, succeeds, and sends nothing (FR12). Status shows a draining loop's in-flight titles.
func TestRunLoopStop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mugicrew", "loop.lock")
	orig := signalLoop
	t.Cleanup(func() { signalLoop = orig })
	var sent []int
	signalLoop = func(pid int) error { sent = append(sent, pid); return nil }

	var off strings.Builder
	if err := runLoopStop(path, false, &off); err != nil || off.String() != "loop is off\n" {
		t.Fatalf("stop off = %v, %q; want 'loop is off', no error", err, off.String())
	}
	if len(sent) != 0 {
		t.Fatalf("stop off signalled %v", sent)
	}

	lk, err := looplock.Acquire(path, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer lk.Close()
	pid := os.Getpid()
	origWait := forceWait
	t.Cleanup(func() { forceWait = origWait })
	forceWait = 50 * time.Millisecond
	// A loop that never reads draining: the plain stop says so rather than return
	// early, so a stop after it can't mistake the window for "on" and force.
	if err := runLoopStop(path, false, io.Discard); err == nil || !strings.Contains(err.Error(), "will force") || !slices.Equal(sent, []int{pid}) {
		t.Fatalf("stop, never draining = %v, sent %v; want one signal to %d and a 'will force' error", err, sent, pid)
	}

	sent = nil
	signalLoop = func(p int) error { // the loop acts on the first signal by going draining
		sent = append(sent, p)
		return lk.Write(looplock.Status{State: looplock.Draining, Inflight: []string{"Build: a", "Build: b"}})
	}
	if err := runLoopStop(path, false, io.Discard); err != nil || !slices.Equal(sent, []int{pid}) {
		t.Fatalf("stop = %v, sent %v; want one signal to %d, returning once draining", err, sent, pid)
	}
	lk.Write(looplock.Status{State: looplock.On})

	sent = nil
	if err := runLoopStop(path, true, io.Discard); err != nil || !slices.Equal(sent, []int{pid, pid}) {
		t.Fatalf("stop --force = %v, sent %v; want two signals to %d", err, sent, pid)
	}
	var out strings.Builder
	runLoopStatus(path, &out)
	if want := fmt.Sprintf("draining (pid %d, 2 in flight: Build: a, Build: b)\n", pid); out.String() != want {
		t.Fatalf("status = %q; want %q", out.String(), want)
	}

	sent = nil
	if err := runLoopStop(path, true, io.Discard); err != nil || len(sent) != 1 {
		t.Fatalf("stop --force while draining = %v, sent %v; want exactly one signal", err, sent)
	}

	// A restart drain is stopped like on: the first signal converts it (restart_to
	// cleared), and only --force sends the second (ADR 0063 §5).
	restartDrain := looplock.Status{State: looplock.Draining, RestartTo: "fedcba9876543210", Inflight: []string{"Build: a"}}
	signalLoop = func(p int) error {
		sent = append(sent, p)
		return lk.Write(looplock.Status{State: looplock.Draining, Inflight: restartDrain.Inflight})
	}
	for _, force := range []bool{false, true} {
		lk.Write(restartDrain)
		sent = nil
		want := []int{pid}
		if force {
			want = []int{pid, pid}
		}
		if err := runLoopStop(path, force, io.Discard); err != nil || !slices.Equal(sent, want) {
			t.Fatalf("stop force=%v on a restart drain = %v, sent %v; want %v", force, err, sent, want)
		}
	}
}

// TestMain lets a test re-exec this binary as mugicrew itself: with
// MUGICREW_TEST_MAIN set, the process runs main on its args instead of the tests.
func TestMain(m *testing.M) {
	if os.Getenv("MUGICREW_TEST_MAIN") != "" {
		main()
		os.Exit(0)
	}
	if bin := os.Getenv(spawnBin); bin != "" { // a TUI pane's `loop start`: spawn, then linger
		if _, _, err := spawnDetached(os.Getenv(spawnOut), bin, []string{"loop", detachedArg}); err != nil {
			os.Exit(1)
		}
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// spawnBin, set, makes this test binary spawn spawnBin detached, logging to
// spawnOut, as `loop start` does, then linger like the TUI pane it runs in.
const (
	spawnBin = "MUGICREW_TEST_SPAWN_BIN"
	spawnOut = "MUGICREW_TEST_SPAWN_OUT"
)

// A loop started from the TUI (o) while the TUI runs in a tmux pane survives that
// pane being killed (ADR 0065 §7, D10): the setsid spawn leaves the pane's
// session, so the pane's hangup never reaches it. Real tmux on a throwaway
// -f /dev/null server; skipped without tmux.
func TestDetachedLoopSurvivesKillPane(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	dir := t.TempDir()
	sock, pidFile := filepath.Join(dir, "s.sock"), filepath.Join(dir, "loop.pid")
	loopBin := filepath.Join(dir, "loop")
	// The pid lands by rename, so a poll never reads a partial one.
	if err := os.WriteFile(loopBin, []byte("#!/bin/sh\necho $$ > "+pidFile+".tmp && mv "+pidFile+".tmp "+pidFile+"\nexec sleep 300\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	tm := func(args ...string) {
		t.Helper()
		cmd := exec.Command("tmux", append([]string{"-S", sock, "-f", "/dev/null"}, args...)...)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tmux %v: %v: %s", args, err, b)
		}
	}
	t.Cleanup(func() { exec.Command("tmux", "-S", sock, "kill-server").Run() })
	tm("new-session", "-d", "-s", "keep")
	tm("new-session", "-d", "-s", "tui", "env", spawnBin+"="+loopBin, spawnOut+"="+filepath.Join(dir, "loop.out"), os.Args[0])
	var pid int
	for deadline := time.Now().Add(10 * time.Second); pid == 0; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the loop never started")
		}
		b, _ := os.ReadFile(pidFile)
		fmt.Sscan(string(b), &pid)
	}
	t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })
	tm("kill-pane", "-t", "=tui:")
	// The pane's hangup has landed once its own process (the TUI stand-in) is gone.
	for deadline := time.Now().Add(10 * time.Second); exec.Command("tmux", "-S", sock, "has-session", "-t", "=tui").Run() == nil; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the TUI pane never died")
		}
	}
	time.Sleep(100 * time.Millisecond) // a loop the hangup reached dies with it
	// Not Kill(pid, 0), which a zombie passes: the loop's state must not be Z.
	if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err != nil || strings.Contains(string(b), ") Z ") {
		t.Fatalf("loop (pid %d) died with the TUI pane: %v", pid, err)
	}
}

// stubBuild stamps this binary as built from source (sourceRoot: a scratch repo,
// whose common dir it returns) and swaps the mainline build resolver for one
// returning rev and bin (or err), recording each "source state" dir pair it was
// asked to build.
func stubBuild(t *testing.T, rev, bin string, err error) (source string, calls *[]string) {
	orig, origRoot := resolveBuild, sourceRoot
	t.Cleanup(func() { resolveBuild, sourceRoot = orig, origRoot })
	sourceRoot = t.TempDir()
	if out, err := exec.Command("git", "init", "-q", sourceRoot).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	source = filepath.Join(sourceRoot, ".git")
	calls = new([]string)
	resolveBuild = func(sourceDir, stateDir string) (string, string, error) {
		*calls = append(*calls, sourceDir+" "+stateDir)
		return rev, bin, err
	}
	return source, calls
}

// loop start refuses a held lock with the holder's state, unlogged, without
// building or spawning (FR8). Off, it builds main and execs the store path as
// `loop --detached --rev=<sha>` in its own session (Setsid), stdin /dev/null
// (nil), stdout+stderr on a freshly truncated loop.out; a spawn that fails is
// loop.failed. A failed build is returned verbatim, logs loop.failed and spawns
// nothing (FR6/FR7).
func TestRunLoopStart(t *testing.T) {
	common := t.TempDir()
	path := filepath.Join(t.TempDir(), "mugicrew-loop.lock") // in the Bead Graph, not the Workspace
	evs := eventlogtest.Capture(t)
	orig := spawnLoop
	t.Cleanup(func() { spawnLoop = orig })
	var spawned *exec.Cmd
	spawnLoop = func(cmd *exec.Cmd) error { spawned = cmd; return errors.New("no exec") }
	const sha, bin = "0123456789abcdef0123456789abcdef01234567", "/nix/store/x-mugicrew/bin/mugicrew"
	_, builds := stubBuild(t, sha, bin, nil)

	lk, err := looplock.Acquire(path, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := runLoopStart(path, common, false, io.Discard); err == nil || !strings.Contains(err.Error(), "loop is already on") {
		t.Fatalf("start while on = %v; want 'loop is already on'", err)
	}
	lk.Write(looplock.Status{State: looplock.Draining})
	if err := runLoopStart(path, common, false, io.Discard); err == nil || !strings.Contains(err.Error(), "loop is draining") {
		t.Fatalf("start while draining = %v; want 'loop is draining'", err)
	}
	lk.Close()
	if spawned != nil || len(*evs) != 0 || len(*builds) != 0 {
		t.Fatalf("refused start spawned %v, logged %v, built %v", spawned, eventlogtest.Names(*evs), *builds)
	}

	nixErr := errors.New("error: hash mismatch in fixed-output derivation")
	stubBuild(t, "", "", nixErr)
	if err := runLoopStart(path, common, false, io.Discard); err != nixErr {
		t.Fatalf("failed build = %v; want nix's error verbatim", err)
	}
	if got := eventlogtest.Names(*evs); spawned != nil || !slices.Equal(got, []string{"loop.failed"}) {
		t.Fatalf("failed build spawned %v, events %v; want no spawn, loop.failed", spawned, got)
	}
	*evs = nil
	source, builds := stubBuild(t, sha, bin, nil)

	out := filepath.Join(common, "mugicrew", "loop.out")
	os.WriteFile(out, []byte("last run's panic"), 0o644)
	if err := runLoopStart(path, common, false, io.Discard); err == nil {
		t.Fatal("failed spawn: want an error")
	}
	if !slices.Equal(*builds, []string{source + " " + common}) {
		t.Fatalf("built %v; want one build of %s into %s", *builds, source, common)
	}
	if want := []string{"loop", detachedArg, revArg + sha, sourceArg + source}; spawned == nil || spawned.Path != bin || !slices.Equal(spawned.Args[1:], want) {
		t.Fatalf("spawned %v; want %s %v", spawned, bin, want)
	}
	if spawned.SysProcAttr == nil || !spawned.SysProcAttr.Setsid || spawned.Stdin != nil {
		t.Fatalf("SysProcAttr %+v, stdin %v; want Setsid, stdin /dev/null", spawned.SysProcAttr, spawned.Stdin)
	}
	f, ok := spawned.Stdout.(*os.File)
	if !ok || spawned.Stderr != spawned.Stdout || f.Name() != out {
		t.Fatalf("stdout %v, stderr %v; want both on %s", spawned.Stdout, spawned.Stderr, out)
	}
	if b, _ := os.ReadFile(out); len(b) != 0 {
		t.Fatalf("loop.out = %q; want truncated", b)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"loop.failed"}) {
		t.Fatalf("events = %v; want loop.failed", got)
	}
}

// Bare loop is the foreground launcher (ADR 0060 §1): a held lock refuses,
// unlogged, without building or exec'ing; a failed build is returned verbatim,
// logs loop.failed and execs nothing; off, it execs the store path in place as
// `loop --rev=<sha>` with this process's env.
func TestRunLoopForeground(t *testing.T) {
	common := t.TempDir()
	path := filepath.Join(t.TempDir(), "mugicrew-loop.lock") // in the Bead Graph, not the Workspace
	evs := eventlogtest.Capture(t)
	orig := execLoop
	t.Cleanup(func() { execLoop = orig })
	var execd []string
	execLoop = func(bin string, argv, env []string) error {
		execd = append([]string{bin}, argv...)
		if !slices.Equal(env, os.Environ()) {
			t.Errorf("exec env differs from ours")
		}
		return errors.New("no exec")
	}
	const sha, bin = "0123456789abcdef0123456789abcdef01234567", "/nix/store/x-mugicrew/bin/mugicrew"
	_, builds := stubBuild(t, sha, bin, nil)

	lk, err := looplock.Acquire(path, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := runLoopForeground(path, common, false); err == nil || !strings.Contains(err.Error(), "loop is already on") {
		t.Fatalf("loop while on = %v; want 'loop is already on'", err)
	}
	lk.Close()
	if execd != nil || len(*evs) != 0 || len(*builds) != 0 {
		t.Fatalf("refused loop exec'd %v, logged %v, built %v", execd, eventlogtest.Names(*evs), *builds)
	}

	nixErr := errors.New("error: hash mismatch in fixed-output derivation")
	stubBuild(t, "", "", nixErr)
	if err := runLoopForeground(path, common, false); err != nixErr {
		t.Fatalf("failed build = %v; want nix's error verbatim", err)
	}
	if got := eventlogtest.Names(*evs); execd != nil || !slices.Equal(got, []string{"loop.failed"}) {
		t.Fatalf("failed build exec'd %v, events %v; want no exec, loop.failed", execd, got)
	}
	*evs = nil
	source, builds := stubBuild(t, sha, bin, nil)

	if err := runLoopForeground(path, common, false); err == nil {
		t.Fatal("failed exec: want an error")
	}
	if !slices.Equal(*builds, []string{source + " " + common}) {
		t.Fatalf("built %v; want one build of %s into %s", *builds, source, common)
	}
	if want := []string{bin, bin, "loop", revArg + sha, sourceArg + source}; !slices.Equal(execd, want) {
		t.Fatalf("exec'd %v; want %v", execd, want)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"loop.failed"}) {
		t.Fatalf("events = %v; want loop.failed", got)
	}
}

// loop status has three shapes (ADR 0090 §7), text and --json: an installed loop
// names its executable and no rev; a pinned unwatched loop names main's short rev
// and its store path, and is never stale; a watched loop adds a line naming the
// source repo and its mainline, and alone shows stale and restarting (ADR 0063 §3).
func TestRunLoopStatusShapes(t *testing.T) {
	pid := os.Getpid()
	status := func(path string) (text, js string) {
		var out, j strings.Builder
		runLoopStatus(path, &out)
		runLoopStatusJSON(path, &j)
		return out.String(), j.String()
	}
	held := func(rev, storePath, source string) (string, *looplock.Lock) {
		path := filepath.Join(t.TempDir(), "mugicrew", "loop.lock")
		lk, err := looplock.Acquire(path, rev, storePath, source)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { lk.Close() })
		return path, lk
	}

	path, _ := held("", "/usr/local/bin/mugicrew", "")
	text, js := status(path)
	if want := fmt.Sprintf("on (pid %d, /usr/local/bin/mugicrew)\n", pid); text != want {
		t.Errorf("installed status = %q; want %q", text, want)
	}
	if want := fmt.Sprintf(`{"state":"on","pid":%d,"store_path":"/usr/local/bin/mugicrew"}`+"\n", pid); js != want {
		t.Errorf("installed --json = %q; want %q", js, want)
	}

	path, lk := held("0123456789abcdef", "/nix/store/x/bin/mugicrew", "")
	text, js = status(path)
	if want := fmt.Sprintf("on (pid %d, main 0123456, /nix/store/x/bin/mugicrew)\n", pid); text != want {
		t.Errorf("pinned status = %q; want %q", text, want)
	}
	if want := fmt.Sprintf(`{"state":"on","pid":%d,"rev":"0123456789abcdef","store_path":"/nix/store/x/bin/mugicrew"}`+"\n", pid); js != want {
		t.Errorf("pinned --json = %q; want %q", js, want)
	}
	lk.Write(looplock.Status{State: looplock.Draining, Rev: "0123456789abcdef", Inflight: []string{"Build: a", "Build: b"}})
	if text, _ = status(path); text != fmt.Sprintf("draining (pid %d, main 0123456, /nix/store/x/bin/mugicrew, 2 in flight: Build: a, Build: b)\n", pid) {
		t.Errorf("pinned drain status = %q", text)
	}
	lk.Write(looplock.Status{State: looplock.On, Rev: "0123456789abcdef", FailedRev: "fedcba9876543210"})
	if text, _ = status(path); text != fmt.Sprintf("on (pid %d, main 0123456, /nix/store/x/bin/mugicrew)\n", pid) {
		t.Errorf("unwatched pinned status = %q; want never stale", text)
	}

	src := filepath.Join(t.TempDir(), "oe")
	if out, err := exec.Command("git", "init", "-q", src).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", src, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk").CombinedOutput(); err != nil {
		t.Fatalf("set origin/HEAD: %v: %s", err, out)
	}
	source := filepath.Join(src, ".git")
	watching := fmt.Sprintf("watching %s trunk\n", src)
	path, lk = held("0123456789abcdef", "/nix/store/x/bin/mugicrew", source)
	text, js = status(path)
	if want := fmt.Sprintf("on (pid %d, main 0123456, /nix/store/x/bin/mugicrew)\n", pid) + watching; text != want {
		t.Errorf("watched status = %q; want %q", text, want)
	}
	if want := fmt.Sprintf(`{"state":"on","pid":%d,"rev":"0123456789abcdef","store_path":"/nix/store/x/bin/mugicrew","watch":true,"source":%q}`+"\n", pid, source); js != want {
		t.Errorf("watched --json = %q; want %q", js, want)
	}
	lk.Write(looplock.Status{State: looplock.On, Rev: "0123456789abcdef", FailedRev: "fedcba9876543210"})
	if text, _ = status(path); text != fmt.Sprintf("on (pid %d, main 0123456, /nix/store/x/bin/mugicrew)\nstale: build of main fedcba9 failed (see build.failed in events)\n", pid)+watching {
		t.Errorf("stale status = %q; want %q", text, fmt.Sprintf("on (pid %d, main 0123456, /nix/store/x/bin/mugicrew)\nstale: build of main fedcba9 failed (see build.failed in events)\n", pid)+watching)
	}
	lk.Write(looplock.Status{State: looplock.Draining, Rev: "0123456789abcdef", RestartTo: "fedcba9876543210", Inflight: []string{"Build: a", "Build: b"}})
	if text, _ = status(path); text != fmt.Sprintf("draining (pid %d, main 0123456, /nix/store/x/bin/mugicrew) → restarting onto main fedcba9\n2 in flight: Build: a, Build: b\n", pid)+watching {
		t.Errorf("restart drain status = %q; want %q", text, fmt.Sprintf("draining (pid %d, main 0123456, /nix/store/x/bin/mugicrew) → restarting onto main fedcba9\n2 in flight: Build: a, Build: b\n", pid)+watching)
	}
	lk.Write(looplock.Status{State: looplock.Draining, Rev: "0123456789abcdef", Inflight: []string{"Build: a"}}) // a stop converted it
	if text, _ = status(path); text != fmt.Sprintf("draining (pid %d, main 0123456, /nix/store/x/bin/mugicrew, 1 in flight: Build: a)\n", pid)+watching {
		t.Errorf("converted drain status = %q; want %q", text, fmt.Sprintf("draining (pid %d, main 0123456, /nix/store/x/bin/mugicrew, 1 in flight: Build: a)\n", pid)+watching)
	}
}

// An installed (unstamped) binary is a fixed tool (ADR 0090 §2): loop start builds
// nothing and spawns itself as `loop --detached`, with no rev or source; bare loop
// builds and execs nothing but runs the loop in place (here failing to resolve a
// Workspace, outside any repo).
func TestLoopUnstampedRunsAsItself(t *testing.T) {
	_, builds := stubBuild(t, "r", "/nix/store/x-mugicrew/bin/mugicrew", nil) // records any build
	sourceRoot = ""                                                           // then unstamp
	origSpawn, origExec := spawnLoop, execLoop
	t.Cleanup(func() { spawnLoop, execLoop = origSpawn, origExec })
	var spawned *exec.Cmd
	spawnLoop = func(cmd *exec.Cmd) error { spawned = cmd; return errors.New("no exec") }
	execLoop = func(string, []string, []string) error { t.Fatal("exec'd an unstamped loop"); return nil }
	eventlogtest.Capture(t)
	path, common := filepath.Join(t.TempDir(), "loop.lock"), t.TempDir()

	runLoopStart(path, common, false, io.Discard)
	self, _ := os.Executable()
	if spawned == nil || spawned.Path != self || !slices.Equal(spawned.Args[1:], []string{"loop", detachedArg}) {
		t.Fatalf("spawned %v; want %s loop %s", spawned, self, detachedArg)
	}
	t.Chdir(t.TempDir())
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(t.TempDir()))
	if err := runLoopForeground(path, common, false); err == nil {
		t.Fatal("in-place loop outside a repo: want its resolve error")
	}
	if len(*builds) != 0 {
		t.Fatalf("unstamped loop built %v", *builds)
	}
}

// The hidden markers round-trip (ADR 0090 §3): what a launcher or restart execs
// parses back to the same markers, --source and --watch included.
func TestLoopMarkersRoundTrip(t *testing.T) {
	for _, m := range []loopMarkers{
		{detached: true, lockFd: -1},
		{rev: "abc", source: "/src/.git", lockFd: -1},
		{detached: true, rev: "abc", source: "/src/.git", lockFd: 3},
		{rev: "abc", lockFd: 0}, // a pre-ADR-0090 image's restart: no --source
		{rev: "abc", source: "/src/.git", watch: true, lockFd: -1},
		{detached: true, rev: "abc", source: "/src/.git", watch: true, lockFd: 3},
	} {
		if got, ok := parseLoopMarkers(m.args()[1:]); !ok || got != m {
			t.Errorf("parse(%v) = %+v, %v; want %+v", m.args(), got, ok, m)
		}
	}
}

// --watch needs the source stamp (Spec FR19): unstamped, loop --watch,
// loop start --watch and the TUI's --watch all fail at parse time, before any
// lock is taken or build runs.
func TestWatchWithoutStampErrorsBeforeLock(t *testing.T) {
	_, builds := stubBuild(t, "r", "/nix/store/x-mugicrew/bin/mugicrew", nil)
	sourceRoot = ""
	graph := t.TempDir()
	t.Setenv("BEADS_DIR", graph)
	for _, argv := range [][]string{{watchArg}, {"start", watchArg}} {
		if err := runLoopVerb(argv); err == nil || !strings.Contains(err.Error(), "--watch") {
			t.Errorf("loop %v unstamped = %v; want the --watch refusal", argv, err)
		}
	}
	if _, _, err := cutWatch([]string{watchArg}); err == nil {
		t.Error("mugicrew --watch unstamped: want the --watch refusal")
	}
	if ents, _ := os.ReadDir(graph); len(ents) != 0 || len(*builds) != 0 {
		t.Fatalf("refused --watch left %v in the graph, built %v", ents, *builds)
	}
}

// A stamped loop start --watch spawns its build watched: --watch rides the
// markers after --source (ADR 0090 §4).
func TestLoopStartWatchSpawnsWatched(t *testing.T) {
	orig := spawnLoop
	t.Cleanup(func() { spawnLoop = orig })
	var spawned *exec.Cmd
	spawnLoop = func(cmd *exec.Cmd) error { spawned = cmd; return errors.New("no exec") }
	const sha, bin = "0123456789abcdef0123456789abcdef01234567", "/nix/store/x-mugicrew/bin/mugicrew"
	source, _ := stubBuild(t, sha, bin, nil)
	t.Chdir(filepath.Dir(source)) // the Workspace: a scratch repo, so loop.out lands there
	eventlogtest.Capture(t)
	t.Setenv("BEADS_DIR", t.TempDir())
	runLoopVerb([]string{"start", watchArg})
	if want := []string{"loop", detachedArg, revArg + sha, sourceArg + source, watchArg}; spawned == nil || !slices.Equal(spawned.Args[1:], want) {
		t.Fatalf("spawned %v; want %v", spawned, want)
	}
}

// Anything but the four verbs (and the hidden markers) is a usage error, before
// any lock is touched, and the usage lists no marker (ADR 0060 §3). Marker forms
// route straight to the loop with no build: outside a repo they fail resolving
// the lock, not with usage.
func TestRunLoopVerbUsage(t *testing.T) {
	_, builds := stubBuild(t, "", "", errors.New("built"))
	for _, argv := range [][]string{{"bogus"}, {"start", "now"}, {"stop", "--force", "x"}, {"--force"}, {"status", "stop"}, {""}, {"stop --force"}, {"status", watchArg}, {watchArg, "start"}, {"stop", watchArg},
		{"--rev="}, {revArg + "abc", detachedArg}, {detachedArg, revArg + "abc", "x"}, {detachedArg, detachedArg}, {sourceArg + "/s"},
		{revArg + "abc", sourceArg}, {revArg + "abc", lockFdArg + "3", sourceArg + "/s"}, {detachedArg, sourceArg + "/s", revArg + "abc"},
		{lockFdArg + "3"}, {revArg + "abc", lockFdArg}, {revArg + "abc", lockFdArg + "x"}, {revArg + "abc", lockFdArg + "-1"}, {lockFdArg + "3", revArg + "abc"}} {
		err := runLoopVerb(argv)
		if err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Errorf("loop %v = %v; want usage", argv, err)
		} else if strings.Contains(err.Error(), detachedArg) || strings.Contains(err.Error(), revArg) || strings.Contains(err.Error(), sourceArg) || strings.Contains(err.Error(), lockFdArg) {
			t.Errorf("usage %q lists a marker", err)
		}
	}
	for _, doc := range []string{"pinned", "clone", "warm", "cold", watchArg, "-C <scratch Workspace>"} { // ADR 0060 §2, D9; ADR 0090 §8
		if !strings.Contains(loopUsage, doc) {
			t.Errorf("usage %q never mentions %s", loopUsage, doc)
		}
	}
	t.Chdir(t.TempDir())
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(t.TempDir()))
	eventlogtest.Capture(t)
	for _, argv := range [][]string{{revArg + "abc"}, {detachedArg, revArg + "abc"}, {detachedArg, revArg + "abc", lockFdArg + "3"},
		{detachedArg}, {revArg + "abc", sourceArg + "/s"}, {detachedArg, revArg + "abc", sourceArg + "/s", lockFdArg + "3"}} {
		if err := runLoopVerb(argv); err == nil || strings.HasPrefix(err.Error(), "usage:") || err.Error() == "built" {
			t.Errorf("loop %v = %v; want routed to the loop, failing on the lock", argv, err)
		}
	}
	if len(*builds) != 0 {
		t.Fatalf("marker forms built %v", *builds)
	}
}

// A start that can't open the lock leaves a trace (FR31): loop.failed, no spawn.
func TestRunLoopStartLockUnreadable(t *testing.T) {
	notDir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notDir, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	evs := eventlogtest.Capture(t)
	orig := spawnLoop
	t.Cleanup(func() { spawnLoop = orig })
	spawnLoop = func(*exec.Cmd) error { t.Fatal("spawned despite an unreadable lock"); return nil }
	if err := runLoopStart(filepath.Join(notDir, "loop.lock"), t.TempDir(), false, io.Discard); err == nil {
		t.Fatal("unreadable lock: want an error")
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"loop.failed"}) {
		t.Fatalf("events = %v; want loop.failed", got)
	}
}

// End to end: loop start returns once a real detached child (this test binary,
// stubbed in as main's pinned build, re-exec'd as mugicrew) holds the lock; status
// reads on with the child's pid and build, the child's PATH leads with its own
// dir, and it logs loop.started detached=true; a second start is refused; loop
// stop drains it to off, leaving its stderr in loop.out.
func TestLoopStartDetachedEndToEnd(t *testing.T) {
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	t.Chdir(repo)
	fakeBd(t, "", "") // every bd call fails: the child loop only logs tick failures
	pathLog := filepath.Join(t.TempDir(), "path.log")
	t.Setenv("FAKE_BD_PATH", pathLog) // the child's bd records the PATH it inherited
	self, _ := os.Executable()
	stubBuild(t, "fedcba9876543210fedcba9876543210fedcba98", self, nil) // the "store path" is this test binary
	t.Setenv("MUGICREW_TEST_MAIN", "1")
	t.Setenv("MUGICREW_POLL_INTERVAL", "bogus") // the child's stderr warning must land in loop.out
	t.Setenv("BEADS_DIR", t.TempDir())          // as requireGraph exports it; the child inherits it
	path, common, err := resolveLoop()
	if err != nil {
		t.Fatal(err)
	}

	var started strings.Builder
	if err := runLoopStart(path, common, false, &started); err != nil {
		t.Fatal(err)
	}
	st, _ := looplock.Read(path)
	if st.State != looplock.On || st.Pid == 0 || st.Pid == os.Getpid() {
		t.Fatalf("after start: %+v; want on with the child's pid", st)
	}
	child := st.Pid
	t.Cleanup(func() { // a failed test must not strand the child; a passed one reaped it (its pid may be reused)
		if t.Failed() {
			syscall.Kill(child, syscall.SIGKILL)
		}
	})
	if want := fmt.Sprintf("loop started (pid %d)\n", st.Pid); started.String() != want {
		t.Fatalf("start printed %q; want %q", started.String(), want)
	}
	var status strings.Builder
	runLoopStatus(path, &status)
	if want := fmt.Sprintf("on (pid %d, main fedcba9, %s)\n", child, self); status.String() != want {
		t.Fatalf("status = %q; want %q", status.String(), want)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		b, _ := os.ReadFile(pathLog)
		if first, _, _ := strings.Cut(string(b), string(os.PathListSeparator)); len(b) > 0 {
			if first != filepath.Dir(self) {
				t.Fatalf("child PATH starts %q; want the pinned build's dir %q", first, filepath.Dir(self))
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the child loop never ran bd")
		}
	}
	if err := runLoopStart(path, common, false, io.Discard); err == nil || !strings.Contains(err.Error(), "loop is already on") {
		t.Fatalf("second start = %v; want 'loop is already on'", err)
	}
	if err := runLoopStop(path, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); st.State != looplock.Off; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("loop never stopped: %+v", st)
		}
		st, _ = looplock.Read(path)
	}
	dir := filepath.Join(common, "mugicrew") // the Workspace's state, not the lock's dir
	if out, _ := os.ReadFile(filepath.Join(dir, "loop.out")); !strings.Contains(string(out), "ignoring invalid MUGICREW_POLL_INTERVAL") {
		t.Fatalf("loop.out = %q; want the child's stderr", out)
	}
	evs, _ := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	if !strings.Contains(string(evs), `"msg":"loop.started"`) || !strings.Contains(string(evs), `"detached":true`) {
		t.Fatalf("events.jsonl = %s; want loop.started detached=true", evs)
	}
}

// fakeNix puts a stub `nix` first on PATH standing in for main@rev's
// packages.mugicrew (ADR 0064 §4): its outPath is <store>/<blob of src at rev>, so a
// commit leaving src alone (docs) keeps the outPath, and `build --out-link L`
// copies bin (this test binary) into that store dir's bin/mugicrew, points L at
// it and prints it. It parses looplock's flakeRef shape. Every argv is appended to
// the returned log.
func fakeNix(t *testing.T, bin string) (logPath string) {
	t.Helper()
	dir := t.TempDir()
	logPath = filepath.Join(dir, "nix.log")
	script := `#!/bin/sh
echo "$*" >> "$FAKE_NIX_LOG"
verb=$1; shift
ref=
link=
while [ $# -gt 0 ]; do
	case "$1" in
	--out-link) link=$2; shift ;;
	git+file://*) ref=$1 ;;
	esac
	shift
done
common=${ref#git+file://}; common=${common%%\?*}
rev=${ref##*rev=}; rev=${rev%%#*}
blob=$(git --git-dir "$common" rev-parse "$rev:src") || exit 1
out=$FAKE_NIX_STORE/$blob-mugicrew
case "$verb" in
eval) printf '%s' "$out" ;;
build)
	[ -x "$out/bin/mugicrew" ] || { mkdir -p "$out/bin" && cp "$FAKE_NIX_BIN" "$out/bin/mugicrew"; } || exit 1
	ln -sfn "$out" "$link" && echo "$out" ;;
*) echo "fake nix: unknown $verb" >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "nix"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_NIX_LOG", logPath)
	t.Setenv("FAKE_NIX_STORE", t.TempDir())
	t.Setenv("FAKE_NIX_BIN", bin)
	return logPath
}

// A restart hands the new build a PATH without the old image's own bin dir:
// only a leading entry equal to the dir goes; the rest of PATH, in order,
// and every other env var stay; prepend-then-strip N times returns the original PATH.
func TestStripPathHead(t *testing.T) {
	sep := string(os.PathListSeparator)
	self := "/nix/store/aaa-mugicrew/bin"
	cases := []struct {
		name string
		env  []string
		want []string
	}{
		{"leads", []string{"HOME=/h", "PATH=" + self + sep + "/usr/bin" + sep + "/bin", "X=1"},
			[]string{"HOME=/h", "PATH=/usr/bin" + sep + "/bin", "X=1"}},
		{"later", []string{"PATH=/usr/bin" + sep + self + sep + "/bin"},
			[]string{"PATH=/usr/bin" + sep + self + sep + "/bin"}},
		{"absent", []string{"PATH=/usr/bin" + sep + "/bin", "Y=" + self},
			[]string{"PATH=/usr/bin" + sep + "/bin", "Y=" + self}},
		{"prefix only", []string{"PATH=" + self + "x" + sep + "/bin"},
			[]string{"PATH=" + self + "x" + sep + "/bin"}},
		{"sole entry", []string{"PATH=" + self}, []string{"PATH="}},
		{"no PATH", []string{"HOME=/h"}, []string{"HOME=/h"}},
	}
	for _, c := range cases {
		if got := stripPathHead(c.env, self); !slices.Equal(got, c.want) {
			t.Errorf("%s: stripPathHead = %q, want %q", c.name, got, c.want)
		}
	}

	orig := "/usr/bin" + sep + "/bin"
	env := []string{"PATH=" + orig}
	for i := range 7 {
		dir := fmt.Sprintf("/nix/store/%d-mugicrew/bin", i)
		env = []string{"PATH=" + dir + sep + strings.TrimPrefix(env[0], "PATH=")} // runLoop's prepend
		env = stripPathHead(env, dir)                                             // execRestart's strip
	}
	if env[0] != "PATH="+orig {
		t.Errorf("after 7 restarts PATH = %q, want %q", env[0], "PATH="+orig)
	}
}

// End to end, a restart (ADR 0064 §4): a detached watched pinned
// loop, self-hosted (Spec FR44), launched from a linked worktree through the real build path (a fake nix) notices a commit to main
// that changes the outPath, builds it, drains and execs onto it in place:
// build.started → build.done → loop.restarting → loop.started restarted=true, all
// on the one pid, and the lock reads on with the new rev and store path, still
// watched: --watch rode the restart's argv (ADR 0090 §4). A
// following docs commit (outPath unchanged) only rewrites rev: no build.
func TestLoopRestartDetachedEndToEnd(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	commit := func(file, body string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, file), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", file)
		git("commit", "-qm", file)
		return git("rev-parse", "HEAD")
	}
	git("init", "-q", "-b", "main")
	first := commit("src", "v1")
	wt := filepath.Join(t.TempDir(), "wt")
	git("worktree", "add", "-q", "-b", "side", wt) // started from a linked worktree: the lock and builds use the common dir
	t.Chdir(wt)
	fakeBd(t, "", "") // every bd call fails: the loop only logs tick failures
	self, _ := os.Executable()
	nixLog := fakeNix(t, self)
	origRoot := sourceRoot
	t.Cleanup(func() { sourceRoot = origRoot })
	sourceRoot = repo // a dev binary of this repo: source and Workspace are one (self-hosting)
	t.Setenv("MUGICREW_TEST_MAIN", "1")
	t.Setenv("MUGICREW_POLL_INTERVAL", "50ms")
	t.Setenv("BEADS_DIR", t.TempDir()) // as requireGraph exports it; the child inherits it
	path, common, err := resolveLoop()
	if err != nil {
		t.Fatal(err)
	}
	waitLock := func(what string, ok func(looplock.Status) bool) looplock.Status {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
			st, _ := looplock.Read(path)
			if ok(st) {
				return st
			}
			if time.Now().After(deadline) {
				t.Fatalf("lock never %s: %+v", what, st)
			}
		}
	}

	if err := runLoopStart(path, common, true, io.Discard); err != nil {
		t.Fatal(err)
	}
	st, _ := looplock.Read(path)
	child, oldBin := st.Pid, st.StorePath
	if st.State != looplock.On || child == 0 || child == os.Getpid() || st.Rev != first || !strings.HasSuffix(oldBin, "-mugicrew/bin/mugicrew") {
		t.Fatalf("after start: %+v; want on with the child's pid, rev %s, a fake store bin", st, first)
	}
	t.Cleanup(func() { // a failed test must not strand the child; a passed one reaped it
		if t.Failed() {
			syscall.Kill(child, syscall.SIGKILL)
		}
	})

	second := commit("src", "v2")
	st = waitLock("restarted onto "+second, func(s looplock.Status) bool { return s.Rev == second })
	if st.State != looplock.On || st.Pid != child || st.StorePath == oldBin || !strings.HasSuffix(st.StorePath, "-mugicrew/bin/mugicrew") || st.RestartTo != "" || st.FailedRev != "" || !st.Watch {
		t.Fatalf("after restart: %+v; want on, watched, pid %d, a new store bin (was %s), no restart_to/failed_rev", st, child, oldBin)
	}
	newBin := st.StorePath
	dir := filepath.Join(common, "mugicrew") // the Workspace's state, not the lock's dir
	if link, _ := os.Readlink(filepath.Join(dir, "loop-build")); link != filepath.Dir(filepath.Dir(newBin)) {
		t.Fatalf("loop-build -> %q; want the new store dir of %s", link, newBin)
	}

	docs := commit("README", "docs")
	st = waitLock("rewritten to "+docs, func(s looplock.Status) bool { return s.Rev == docs })
	if st.State != looplock.On || st.Pid != child || st.StorePath != newBin {
		t.Fatalf("after docs commit: %+v; want on, pid %d, still %s", st, child, newBin)
	}
	if err := runLoopStop(path, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	waitLock("off", func(s looplock.Status) bool { return s.State == looplock.Off })

	if b, _ := os.ReadFile(nixLog); strings.Count("\n"+string(b), "\nbuild ") != 2 { // the launch, the restart
		t.Fatalf("nix calls:\n%s\nwant two builds (launch, restart), none for the docs commit", b)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	var seq []string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var ev struct {
			Msg       string `json:"msg"`
			Pid       int    `json:"pid"`
			Rev       string `json:"rev"`
			Restarted bool   `json:"restarted"`
		}
		if json.Unmarshal([]byte(line), &ev) != nil || ev.Pid != child {
			continue
		}
		switch ev.Msg {
		case "build.started", "build.done", "build.failed", "loop.restarting", "loop.started", "loop.stopped":
			seq = append(seq, fmt.Sprintf("%s %s %v", ev.Msg, ev.Rev, ev.Restarted))
		}
	}
	want := []string{"loop.started  false", "build.started " + second + " false", "build.done " + second + " false",
		"loop.restarting " + second + " false", "loop.started  true", "loop.stopped  false"}
	if !slices.Equal(seq, want) {
		t.Fatalf("pid %d events:\n%s\nwant:\n%s", child, strings.Join(seq, "\n"), strings.Join(want, "\n"))
	}
}

// `mugicrew abort` takes exactly one bead id (a usage error otherwise, before
// anything runs), runs the shared abort by cli, prints the pids it signalled or
// "no live process; paused only", and fails on a refusal (ADR 0083).
func TestRunAbort(t *testing.T) {
	orig := abortEffort
	t.Cleanup(func() { abortEffort = orig })
	var called []string
	abortEffort = func(id, by string) ([]int, error) {
		called = append(called, id+" "+by)
		switch id {
		case "ws-live":
			return []int{41, 42}, nil
		case "ws-open":
			return nil, errors.New("ws-open: nothing in flight")
		}
		return nil, nil
	}
	for _, argv := range [][]string{nil, {"mc-a", "mc-b"}} {
		if err := runAbort(argv, io.Discard); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Errorf("runAbort(%q) = %v, want a usage error", argv, err)
		}
	}
	if len(called) != 0 {
		t.Fatalf("a usage error must abort nothing, got %v", called)
	}
	for id, want := range map[string]string{"ws-live": "signalled 41 42\n", "ws-dead": "no live process; paused only\n"} {
		var out bytes.Buffer
		if err := runAbort([]string{id}, &out); err != nil || out.String() != want {
			t.Errorf("abort %s = %q, %v, want %q", id, out.String(), err, want)
		}
	}
	if err := runAbort([]string{"ws-open"}, io.Discard); err == nil {
		t.Error("a refusal must exit non-zero")
	}
	if !slices.Contains(called, "ws-live cli") {
		t.Errorf("abort must run by cli, got %v", called)
	}
}

// `mugicrew chart append|drop <chart> <section> <line>` (Spec D9) maps
// its sub-verb onto one locked Chart-body edit; anything else is a usage error
// that edits nothing.
func TestRunChart(t *testing.T) {
	orig := editChart
	t.Cleanup(func() { editChart = orig })
	var called []string
	editChart = func(id, section, line string, drop bool) error {
		called = append(called, fmt.Sprint(id, "|", section, "|", line, "|", drop))
		return nil
	}
	for _, argv := range [][]string{nil, {"append", "mc-c", "Notes"}, {"move", "mc-c", "Notes", "- x"}, {"drop", "mc-c", "Notes", "- x", "extra"}} {
		if err := runChart(argv); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Errorf("runChart(%q) = %v, want a usage error", argv, err)
		}
	}
	if err := runChart([]string{"append", "mc-c", "Decisions-so-far", "- mc-c.1: yes"}); err != nil {
		t.Fatal(err)
	}
	if err := runChart([]string{"drop", "mc-c", "Not-yet-specified", "- fog"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"mc-c|Decisions-so-far|- mc-c.1: yes|false", "mc-c|Not-yet-specified|- fog|true"}
	if !slices.Equal(called, want) {
		t.Errorf("edits = %q, want %q", called, want)
	}
}

// `mugicrew hitl-hook` never blocks or talks to Claude (Spec FR29): it
// exits 0 with empty stdout on garbage stdin, with the pane var missing, and with
// a pane var naming no server, even outside a repo.
func TestHitlHookAlwaysSilent(t *testing.T) {
	t.Chdir(t.TempDir())
	self, _ := os.Executable()
	for _, tc := range []struct{ stdin, pane string }{
		{"not json", ""},
		{`{"hook_event_name":"Stop"}`, ""},
		{`{"hook_event_name":"Stop"}`, filepath.Join(t.TempDir(), "none.sock") + ",%1"},
	} {
		cmd := exec.Command(self, "hitl-hook")
		cmd.Env = append(os.Environ(), "MUGICREW_TEST_MAIN=1", "MUGICREW_HITL_PANE="+tc.pane)
		cmd.Stdin = strings.NewReader(tc.stdin)
		out, err := cmd.Output()
		if err != nil || len(out) != 0 {
			t.Errorf("hitl-hook stdin %q pane %q = %v, stdout %q; want exit 0 and no stdout", tc.stdin, tc.pane, err, out)
		}
	}
}
