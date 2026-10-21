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
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/brook"
	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/chopper"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/jinbe"
	"jayulabs.org/mugicrew/jira"
	"jayulabs.org/mugicrew/loop"
	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/luffy"
	"jayulabs.org/mugicrew/nami"
	"jayulabs.org/mugicrew/rebase"
	"jayulabs.org/mugicrew/session"
	"jayulabs.org/mugicrew/withdraw"
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
	// ADR 0129: a Signal's owner is Luffy, so `reconcile <signal>` and `signal
	// triage` reach the triager the TUI launches.
	if sig, err := crewFor(contract.KindSignal); err != nil {
		t.Fatalf("crewFor(signal): %v", err)
	} else if _, ok := sig.(luffy.Luffy); !ok {
		t.Errorf("signal owner = %T, want luffy.Luffy", sig)
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

	for _, argv := range [][]string{{}, {"signal", "new"}, {"signal", "triage", "mc-1"}, {"signal", "offshoot"}, {"intake", "jira", "ABC-1"}, {"latch", "jira", "ABC-1"}, {"surgery", "new"}, {"surgery", "end", "mc-1"}, {"loop"}, {"loop", "--watch"}, {"loop", "start"}, {"loop", "start", "--watch"}, {"loop", "status"}, {"loop", "stop"}, {"loop", "--rev=abc", "--lock-fd=3"}, {"chart", "append", "mc-1", "Notes", "- x"}} {
		err := requireGraph(argv)
		if err == nil || !strings.Contains(err.Error(), "mugicrew init") {
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
	if err := runInteractive(ac, "/repo/root", bd.Bead{ID: "sig1"}, ""); err != nil {
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
	if err := runInteractive(contract.AgentCommand{}, "/repo/root", bd.Bead{}, ""); err == nil {
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
	if err := runInteractive(ac, "/repo", bd.Bead{ID: "sig1", Kind: contract.KindSignal}, ""); err == nil {
		t.Fatal("a non-zero session exit must surface as an error")
	}
	evs := *capt
	if len(evs) != 2 || evs[0].Name != "hitl.launched" || evs[1].Name != "hitl.exited" {
		t.Fatalf("events = %+v, want hitl.launched then hitl.exited", evs)
	}
	if a := evs[0].Attrs; a["session"] != "sess-1" || a["bead"] != "sig1" || a["crewmate"] != "luffy" {
		t.Errorf("hitl.launched attrs = %v, want session sess-1, bead sig1, crewmate luffy", a)
	}
	if a := evs[1].Attrs; a["session"] != "sess-1" || a["exit"] != 3 {
		t.Errorf("hitl.exited attrs = %v, want session sess-1, exit 3", a)
	}
	if a := evs[1].Attrs; a["cost_usd"] != 0.75 || a["api_ms"] != 900 {
		t.Errorf("hitl.exited attrs = %v, want the transcript's cost", a)
	}
}

// A triage session whose transcript ends in a continued-in record puts
// continued_in on hitl.exited and spawns the follower before it (Spec mc-11b8 FR21).
func TestRunInteractiveSpawnsFollowerOnHandoff(t *testing.T) {
	origExec, origSpawn := interactiveExec, spawnFollower
	defer func() { interactiveExec, spawnFollower = origExec, origSpawn }()
	noDrafts(t)
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	cs := `{"type":"cost-state","totalCostUSD":1.5,"totalAPIDuration":3000}` + "\n"
	ci := `{"type":"continued-in","sessionId":"sess-1","continuedInSessionId":"dest-9"}` + "\n"
	interactiveExec = func(*exec.Cmd) error {
		os.MkdirAll(cfg+"/projects/-repo", 0o755)
		return os.WriteFile(cfg+"/projects/-repo/sess-1.jsonl", []byte(cs+cs+ci), 0o644)
	}
	capt := eventlogtest.Capture(t)
	var dest string
	var exitedBefore bool
	spawnFollower = func(_ []any, m contract.Metered, _ string) error {
		dest = m.ContinuedIn
		for _, e := range *capt {
			exitedBefore = exitedBefore || e.Name == "hitl.exited"
		}
		return nil
	}
	ac := contract.AgentCommand{Argv: []string{"claude", "--session-id", "sess-1"}, SessionID: "sess-1"}
	if err := runInteractive(ac, "/repo", bd.Bead{ID: "sig1", Kind: contract.KindSignal}, ""); err != nil {
		t.Fatal(err)
	}
	if dest != "dest-9" || exitedBefore {
		t.Errorf("spawn dest=%q, hitl.exited already written=%v; want dest-9, false", dest, exitedBefore)
	}
	evs := *capt
	if a := evs[len(evs)-1].Attrs; a["continued_in"] != "dest-9" || a["cost_usd"] != 1.5 {
		t.Errorf("hitl.exited attrs = %v, want continued_in dest-9 and the source's own 1.5", a)
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
	if err := runInteractive(ac, "/repo", bd.Bead{ID: "sig1"}, ""); err != nil {
		t.Fatalf("runInteractive: %v", err)
	}
	if got := strings.Join(calls, ","); got != "drop sig1,run,publish sig1" {
		t.Errorf("clean exit calls = %s, want drop, run, publish", got)
	}

	calls = nil
	interactiveExec = func(*exec.Cmd) error { calls = append(calls, "run"); return errors.New("rc=1") }
	if err := runInteractive(ac, "/repo", bd.Bead{ID: "sig1"}, ""); err == nil {
		t.Fatal("a failed session must error")
	}
	if got := strings.Join(calls, ","); got != "drop sig1,run" {
		t.Errorf("failed exit calls = %s, want drop, run (Drafts kept)", got)
	}

	// A failed publish fails the verb; a failed leftover delete launches nothing.
	interactiveExec = func(*exec.Cmd) error { return nil }
	publishDrafts = func(string, []any) error { return errors.New("bd down") }
	if err := runInteractive(ac, "/repo", bd.Bead{ID: "sig1"}, ""); err == nil {
		t.Error("a failed publish must error")
	}
	calls = nil
	dropDrafts = func(bd.Bead) error { return errors.New("bd down") }
	interactiveExec = func(*exec.Cmd) error { calls = append(calls, "run"); return nil }
	if err := runInteractive(ac, "/repo", bd.Bead{ID: "sig1"}, ""); err == nil || len(calls) != 0 {
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
		{"mc-s", "--ref", "PROJ-1"}, // --ref without --initiative
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

// The reroute flags of `mugicrew reconcile` (Spec decision 3, ADR 0322): a bad flag
// set is a usage error before bd is touched; a non-Signal, a missing body file or an
// unpaired pivot errors with nothing created.
func TestRunReconcileRerouteFlagRefusals(t *testing.T) {
	signal := `[{"id":"mc-s","title":"sig","status":"open","labels":["kind:signal"]}]`
	chart := `[{"id":"mc-c","title":"ch","status":"open","labels":["kind:chart"]}]`
	body := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(body, []byte("a body"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, argv := range [][]string{
		{"mc-s", "--pivot", body},                        // --checkpoint is required
		{"mc-s", "--critique", body},                     // likewise
		{"mc-s", "--merge-into", "mc-p", body},           // likewise
		{"mc-s", "--checkpoint", "mc-a", "--spec", body}, // not beside the other legs
		{"mc-s", "--checkpoint", "mc-a", "--initiative", body},
		{"mc-s", "--checkpoint", "mc-a", "--critique", body, "--context", "x"},
		{"mc-s", "--checkpoint", "mc-a", "--critique", body, "--title", "t"},
		{"mc-s", "--checkpoint", "mc-a", "--critique", body, "--ref", "R-1"},
		{"mc-s", "--checkpoint"},                                 // missing its value
		{"mc-s", "--checkpoint", "mc-a", "--pivot"},              // missing its value
		{"mc-s", "--checkpoint", "mc-a", "--merge-into", "mc-p"}, // missing the evidence file
		{"mc-s", "--checkpoint", "mc-a", "--critique", body, "extra"},
	} {
		log := fakeBd(t, signal, "[]")
		if err := runReconcile(argv); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Errorf("runReconcile(%q) = %v, want a usage error", argv, err)
		}
		if l := bdLog(t, log); l != "" {
			t.Errorf("runReconcile(%q) touched bd before rejecting its flags: %q", argv, l)
		}
	}

	log := fakeBd(t, signal, "[]")
	if err := runReconcile([]string{"mc-s", "--checkpoint", "mc-a", "--checkpoint", "mc-b", "--pivot", body}); err == nil || !strings.Contains(err.Error(), "second --checkpoint") {
		t.Errorf("a second --checkpoint = %v, want it refused by name", err)
	}
	if l := bdLog(t, log); l != "" {
		t.Errorf("a second --checkpoint touched bd: %q", l)
	}

	fakeBd(t, chart, "[]")
	if err := runReconcile([]string{"mc-c", "--checkpoint", "mc-a", "--critique", body}); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
		t.Errorf("reroute flags on a Chart = %v, want a usage error", err)
	}

	for name, argv := range map[string][]string{
		"missing body":  {"mc-s", "--checkpoint", "mc-a", "--critique", filepath.Join(t.TempDir(), "nope.md")},
		"missing merge": {"mc-s", "--checkpoint", "mc-a", "--merge-into", "mc-p", filepath.Join(t.TempDir(), "nope.md")},
		"unpaired":      {"mc-s", "--checkpoint", "mc-a", "--pivot", body},
	} {
		log := fakeBd(t, signal, "[]")
		if err := runReconcile(argv); err == nil {
			t.Errorf("%s: runReconcile must error", name)
		}
		if l := bdLog(t, log); strings.Contains(l, "create") || strings.Contains(l, "update") {
			t.Errorf("%s: wrote despite refusing: %q", name, l)
		}
	}
}

// Spec D3: `mugicrew reconcile --initiative` shares the fast lane's flag rules —
// one body flag at a time, a usage error before bd is touched — and a refusal (a
// non-Signal, a Signal already charted) spawns nothing.
func TestRunReconcileInitiativeRefusals(t *testing.T) {
	signal := `[{"id":"mc-s","title":"sig","status":"open","labels":["kind:signal"]}]`
	chart := `[{"id":"mc-c","title":"ch","status":"open","labels":["kind:chart"]}]`
	body := filepath.Join(t.TempDir(), "initiative.md")
	if err := os.WriteFile(body, []byte("## Done when\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, argv := range [][]string{
		{"mc-s", "--initiative"},
		{"mc-s", "--initiative", body, "--spec", body},
	} {
		log := fakeBd(t, signal, "[]")
		if err := runReconcile(argv); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Errorf("runReconcile(%q) = %v, want a usage error", argv, err)
		}
		if l := bdLog(t, log); l != "" {
			t.Errorf("runReconcile(%q) touched bd before rejecting its flags: %q", argv, l)
		}
	}
	fakeBd(t, chart, "[]")
	if err := runReconcile([]string{"mc-c", "--initiative", body}); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
		t.Errorf("--initiative on a Chart = %v, want a usage error", err)
	}
	log := fakeBd(t, signal, `[{"id":"mc-c","labels":["kind:chart"]}]`)
	if err := runReconcile([]string{"mc-s", "--initiative", body, "--title", "effort"}); err == nil || !strings.Contains(err.Error(), "already has chart mc-c") {
		t.Errorf("--initiative on a charted Signal = %v, want the Chart refusal", err)
	}
	if l := bdLog(t, log); strings.Contains(l, "create") {
		t.Errorf("spawned an Initiative despite refusing: %q", l)
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

// `mugicrew pivot raise` takes exactly one task id; anything else is a usage error
// before bd is touched.
func TestRunPivotRaiseUsage(t *testing.T) {
	for _, argv := range [][]string{
		{},
		{""},
		{"ws-t", "more"},                         // stray positional
		{"--pivot", "f"},                         // a flag where the task goes
		{"ws-t", "--critique"},                   // a flag with no file
		{"ws-t", "--merge-into", "p"},            // a merge with no evidence file
		{"ws-t", "--merge-into", "--pivot", "f"}, // a flag where a value goes
		{"ws-t", "--bogus", "f"},
	} {
		log := fakeBd(t, "[]", "[]")
		if err := runPivotRaise(argv, io.Discard); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Errorf("runPivotRaise(%q) = %v, want a usage error", argv, err)
		}
		if l := bdLog(t, log); l != "" {
			t.Errorf("runPivotRaise(%q) touched bd: %q", argv, l)
		}
	}
}

// A flagged raise reads every body file before it touches bd: a missing one writes
// nothing, and a bad shape is refused by the verb with only reads run.
func TestRunPivotRaiseFlags(t *testing.T) {
	dir := t.TempDir()
	body := filepath.Join(dir, "b.md")
	if err := os.WriteFile(body, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	log := fakeBd(t, "[]", "[]")
	if err := runPivotRaise([]string{"ws-t", "--critique", filepath.Join(dir, "missing")}, io.Discard); err == nil {
		t.Error("a missing body file was accepted")
	}
	if l := bdLog(t, log); l != "" {
		t.Errorf("a missing body file touched bd: %q", l)
	}
	for _, argv := range [][]string{
		{"ws-t", "--pivot", body},                                      // pivot without Critique
		{"ws-t", "--pivot", body, "--pivot", body, "--critique", body}, // two pivots
		{"ws-t", "--merge-into", "ws-p", body},                         // merge without Critique
	} {
		log := fakeBd(t, `[{"id":"ws-t","status":"open","issue_type":"task","labels":["kind:task"]}]`, "[]")
		err := runPivotRaise(argv, io.Discard)
		if err == nil || strings.HasPrefix(err.Error(), "usage:") {
			t.Errorf("runPivotRaise(%q) = %v, want the verb's shape refusal", argv, err)
		}
		for _, l := range strings.Split(bdLog(t, log), "\n") {
			if l != "" && !strings.HasPrefix(l, "show ") && !strings.HasPrefix(l, "dep ") && !strings.HasPrefix(l, "list ") {
				t.Errorf("runPivotRaise(%q): bd write %q", argv, l)
			}
		}
	}
}

// A probe on a bead that is not an open Task refuses, names the offshoot-Signal
// fallback and writes nothing (only show is run).
func TestRunPivotRaiseRefusesNonTask(t *testing.T) {
	log := fakeBd(t, `[{"id":"ws-t","status":"open","issue_type":"task","labels":["kind:signal"]}]`, "[]")
	err := runPivotRaise([]string{"ws-t"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "offshoot Signal") {
		t.Errorf("runPivotRaise = %v, want a refusal naming the offshoot Signal", err)
	}
	for _, l := range strings.Split(bdLog(t, log), "\n") {
		if l != "" && !strings.HasPrefix(l, "show ") && !strings.HasPrefix(l, "dep ") {
			t.Errorf("bd write %q", l)
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
		{"--title", "t", "--initiative", "x", "--checkpoint", "y"}, // a joined Checkpoint decides membership
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
		if want := fmt.Sprintf("state: on\npid: %d\n\nin flight: 0\n", os.Getpid()); out.String() != want {
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
	if out.String() != "state: off\n" {
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
	if want := fmt.Sprintf("state: draining\npid: %d\n\nin flight: 2\n", pid); out.String() != want {
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

// --force sends its second signal no sooner than loop.StopWindow after the first,
// counted from the loop's receipt: a late first delivery must not shorten it.
func TestRunLoopStopForceWaitsWindow(t *testing.T) {
	for name, lag := range map[string]time.Duration{"prompt": 0, "first delivery lags": 100 * time.Millisecond} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "mugicrew", "loop.lock")
			lk, err := looplock.Acquire(path, "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			defer lk.Close()
			defer func(w time.Duration) { loop.StopWindow = w }(loop.StopWindow)
			loop.StopWindow = 200 * time.Millisecond
			orig := signalLoop
			t.Cleanup(func() { signalLoop = orig })
			var at []time.Time
			signalLoop = func(int) error {
				if len(at) == 0 {
					time.Sleep(lag)
				}
				at = append(at, time.Now()) // the loop's receipt
				return lk.Write(looplock.Status{State: looplock.Draining})
			}
			if err := runLoopStop(path, true, io.Discard); err != nil || len(at) != 2 {
				t.Fatalf("stop --force = %v, %d signals; want 2", err, len(at))
			}
			if d := at[1].Sub(at[0]); d < loop.StopWindow {
				t.Fatalf("second signal %v after first; want >= %v", d, loop.StopWindow)
			}
		})
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
	// The nix sandbox has no git identity; integrate's own git commit-tree needs one.
	for _, k := range []string{"GIT_AUTHOR", "GIT_COMMITTER"} {
		os.Setenv(k+"_NAME", "t")
		os.Setenv(k+"_EMAIL", "t@t")
	}
	// loop start's gitlab preflight would otherwise read the real repo (none in the nix sandbox).
	git.LandStrategyName = func() (string, error) { return git.LandStrategyLocal, nil }
	loop.StopWindow = 0 // tests send stop and force back to back; window tests set their own
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
	if runtime.GOOS != "linux" {
		t.Skip("reads /proc/<pid>/stat")
	}
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
	lk.Write(looplock.Status{State: looplock.Draining, RestartTo: "fedcba9876543210"})
	if err := runLoopStart(path, common, false, io.Discard); err == nil || !strings.Contains(err.Error(), "loop is draining") {
		t.Fatalf("start while restart-draining = %v; want 'loop is draining'", err)
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

// loop start on a stop drain cancels it (ADR 0236): SIGUSR1 via cancelLoop, no
// build or spawn, no event, and "loop resumed (pid N)" once the lock leaves the
// drain. A lock gone off mid-wait, or a signal that finds no process, ends in an
// ordinary fresh start; a loop that never leaves the drain is an error naming
// the older-build cause.
func TestRunLoopStartCancelsStopDrain(t *testing.T) {
	common := t.TempDir()
	path := filepath.Join(t.TempDir(), "mugicrew-loop.lock")
	evs := eventlogtest.Capture(t)
	origSpawn, origCancel, origWait := spawnLoop, cancelLoop, forceWait
	t.Cleanup(func() { spawnLoop, cancelLoop, forceWait = origSpawn, origCancel, origWait })
	var spawned *exec.Cmd
	spawnLoop = func(cmd *exec.Cmd) error { spawned = cmd; return errors.New("no exec") }
	_, builds := stubBuild(t, "0123456789abcdef0123456789abcdef01234567", "/nix/store/x-mugicrew/bin/mugicrew", nil)
	lk, err := looplock.Acquire(path, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lk.Close() })
	drain := looplock.Status{State: looplock.Draining, Inflight: []string{"Build: a"}}

	t.Run("resumes", func(t *testing.T) {
		lk.Write(drain)
		var sent []int
		cancelLoop = func(pid int) error {
			sent = append(sent, pid)
			lk.Write(looplock.Status{State: looplock.On})
			return nil
		}
		var out strings.Builder
		if err := runLoopStart(path, common, true, &out); err != nil {
			t.Fatal(err)
		}
		if want := fmt.Sprintf("loop resumed (pid %d)\n", os.Getpid()); out.String() != want || !slices.Equal(sent, []int{os.Getpid()}) {
			t.Fatalf("printed %q, signalled %v; want %q once", out.String(), sent, want)
		}
		if spawned != nil || len(*builds) != 0 || len(*evs) != 0 {
			t.Fatalf("cancel spawned %v, built %v, logged %v", spawned, *builds, eventlogtest.Names(*evs))
		}
	})
	t.Run("lock off mid-wait starts fresh", func(t *testing.T) {
		lk.Write(drain)
		cancelLoop = func(int) error { lk.Close(); return nil }
		runLoopStart(path, common, false, io.Discard)
		if spawned == nil || len(*builds) != 1 {
			t.Fatalf("spawned %v, built %v; want a fresh start", spawned, *builds)
		}
	})
	t.Run("ESRCH re-reads to a fresh start", func(t *testing.T) {
		lk.Close()
		lk, err = looplock.Acquire(path, "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		lk.Write(drain)
		spawned, *builds = nil, nil
		cancelLoop = func(int) error { lk.Close(); return syscall.ESRCH }
		runLoopStart(path, common, false, io.Discard)
		if spawned == nil || len(*builds) != 1 {
			t.Fatalf("spawned %v, built %v; want a fresh start", spawned, *builds)
		}
	})
	t.Run("never leaves the drain", func(t *testing.T) {
		lk, err = looplock.Acquire(path, "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		lk.Write(drain)
		spawned, *builds = nil, nil
		cancelLoop = func(int) error { return nil }
		forceWait = 50 * time.Millisecond
		err := runLoopStart(path, common, false, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "did not leave its drain") || spawned != nil || len(*builds) != 0 {
			t.Fatalf("err %v, spawned %v, built %v; want the older-build error and no start", err, spawned, *builds)
		}
	})
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

	bad := errors.New("land_strategy unusable")
	defer func(f func() (string, error)) { git.LandStrategyName = f }(git.LandStrategyName)
	git.LandStrategyName = func() (string, error) { return "", bad }
	if err := runLoopForeground(path, common, false); !errors.Is(err, bad) {
		t.Fatalf("foreground loop with a failing preflight = %v; want %v", err, bad)
	}
	if execd != nil || len(*builds) != 0 {
		t.Fatalf("failed preflight exec'd %v, built %v", execd, *builds)
	}
	*evs = nil
	git.LandStrategyName = func() (string, error) { return git.LandStrategyLocal, nil }

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

// A limited loop's status is its hold (ADR 0295 §3): the limit section names the
// local reset time and where the reset came from; --json is the Status as is.
func TestRunLoopStatusLimited(t *testing.T) {
	pid := os.Getpid()
	oLocal := time.Local
	t.Cleanup(func() { time.Local = oLocal })
	time.Local = time.UTC
	path := filepath.Join(t.TempDir(), "mugicrew", "loop.lock")
	lk, err := looplock.Acquire(path, "0123456789abcdef", "/nix/store/x/bin/mugicrew", "")
	if err != nil {
		t.Fatal(err)
	}
	defer lk.Close()
	now := time.Now()
	reset := time.Date(now.Year(), now.Month(), now.Day(), 15, 44, 0, 0, time.UTC)
	for _, c := range []struct{ source, line string }{
		{"message", "reset time from: message"},
		{"usage", "reset time from: usage"},
		{"fallback", "reset time from: fallback (1h, no reset time found)"},
	} {
		lk.Write(looplock.Status{State: looplock.Limited, Rev: "0123456789abcdef", Inflight: []string{"Build: a", "Build: b"}, ResetsAt: reset, LimitSource: c.source})
		var out, j strings.Builder
		runLoopStatus(path, &out)
		runLoopStatusJSON(path, &j)
		if want := fmt.Sprintf("state: limited\npid: %d\n\nmain: 0123456\nstore: /nix/store/x/bin/mugicrew\n\nin flight: 2\n\nheld by usage limit until: 15:44\n%s\n", pid, c.line); out.String() != want {
			t.Errorf("%s status = %q; want %q", c.source, out.String(), want)
		}
		if want := fmt.Sprintf(`{"state":"limited","pid":%d,"rev":"0123456789abcdef","store_path":"/nix/store/x/bin/mugicrew","inflight":["Build: a","Build: b"],"resets_at":%q,"limit_source":%q}`+"\n", pid, reset.Format(time.RFC3339), c.source); j.String() != want {
			t.Errorf("%s --json = %q; want %q", c.source, j.String(), want)
		}
	}
}

// loop status has three shapes (ADR 0295), text and --json: an installed loop
// names its executable and no rev; a pinned unwatched loop names main's short rev
// and its store path, and shows stale and restarting when asked; a watched loop adds a section naming the
// source repo and its mainline, and alone shows the watching line; stale and restarting show on any from-source loop.
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
	if want := fmt.Sprintf("state: on\npid: %d\n\nstore: /usr/local/bin/mugicrew\n\nin flight: 0\n", pid); text != want {
		t.Errorf("installed status = %q; want %q", text, want)
	}
	if want := fmt.Sprintf(`{"state":"on","pid":%d,"store_path":"/usr/local/bin/mugicrew"}`+"\n", pid); js != want {
		t.Errorf("installed --json = %q; want %q", js, want)
	}

	path, lk := held("0123456789abcdef", "/nix/store/x/bin/mugicrew", "")
	text, js = status(path)
	if want := fmt.Sprintf("state: on\npid: %d\n\nmain: 0123456\nstore: /nix/store/x/bin/mugicrew\n\nin flight: 0\n", pid); text != want {
		t.Errorf("pinned status = %q; want %q", text, want)
	}
	if want := fmt.Sprintf(`{"state":"on","pid":%d,"rev":"0123456789abcdef","store_path":"/nix/store/x/bin/mugicrew"}`+"\n", pid); js != want {
		t.Errorf("pinned --json = %q; want %q", js, want)
	}
	lk.Write(looplock.Status{State: looplock.Draining, Rev: "0123456789abcdef", Inflight: []string{"Build: a", "Build: b"}})
	if text, _ = status(path); text != fmt.Sprintf("state: draining\npid: %d\n\nmain: 0123456\nstore: /nix/store/x/bin/mugicrew\n\nin flight: 2\n", pid) {
		t.Errorf("pinned drain status = %q", text)
	}
	lk.Write(looplock.Status{State: looplock.On, Rev: "0123456789abcdef", FailedRev: "fedcba9876543210"})
	if text, _ = status(path); text != fmt.Sprintf("state: on (stale)\npid: %d\n\nmain: 0123456\nstore: /nix/store/x/bin/mugicrew\n\nin flight: 0\n\nstale: build of main fedcba9 failed (see build.failed in events)\n", pid) {
		t.Errorf("unwatched pinned failed status = %q; want stale line, no watching line", text)
	}
	lk.Write(looplock.Status{State: looplock.Draining, Rev: "0123456789abcdef", RestartTo: "fedcba9876543210", Inflight: []string{"Build: a"}})
	if text, _ = status(path); text != fmt.Sprintf("state: draining\nrestarting onto: main fedcba9\npid: %d\n\nmain: 0123456\nstore: /nix/store/x/bin/mugicrew\n\nin flight: 1\n", pid) {
		t.Errorf("unwatched restart drain status = %q", text)
	}
	lk.Write(looplock.Status{State: looplock.Limited, Rev: "0123456789abcdef", FailedRev: "fedcba9876543210", LimitSource: "usage", ResetsAt: time.Date(2030, 1, 1, 15, 44, 0, 0, time.Local)})
	if text, _ = status(path); text != fmt.Sprintf("state: limited (stale)\npid: %d\n\nmain: 0123456\nstore: /nix/store/x/bin/mugicrew\n\nin flight: 0\n\nheld by usage limit until: Jan 1 15:44\nreset time from: usage\n\nstale: build of main fedcba9 failed (see build.failed in events)\n", pid) {
		t.Errorf("limited failed status = %q", text)
	}

	src := filepath.Join(t.TempDir(), "oe")
	if out, err := exec.Command("git", "init", "-q", src).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", src, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk").CombinedOutput(); err != nil {
		t.Fatalf("set origin/HEAD: %v: %s", err, out)
	}
	source := filepath.Join(src, ".git")
	watching := fmt.Sprintf("\nwatching: %s trunk\n", src)
	path, lk = held("0123456789abcdef", "/nix/store/x/bin/mugicrew", source)
	text, js = status(path)
	if want := fmt.Sprintf("state: on\npid: %d\n\nmain: 0123456\nstore: /nix/store/x/bin/mugicrew\n\nin flight: 0\n", pid) + watching; text != want {
		t.Errorf("watched status = %q; want %q", text, want)
	}
	if want := fmt.Sprintf(`{"state":"on","pid":%d,"rev":"0123456789abcdef","store_path":"/nix/store/x/bin/mugicrew","watch":true,"source":%q}`+"\n", pid, source); js != want {
		t.Errorf("watched --json = %q; want %q", js, want)
	}
	lk.Write(looplock.Status{State: looplock.On, Rev: "0123456789abcdef", FailedRev: "fedcba9876543210"})
	if text, _ = status(path); text != fmt.Sprintf("state: on (stale)\npid: %d\n\nmain: 0123456\nstore: /nix/store/x/bin/mugicrew\n\nin flight: 0\n\nstale: build of main fedcba9 failed (see build.failed in events)\n", pid)+watching {
		t.Errorf("stale status = %q; want %q", text, fmt.Sprintf("state: on (stale)\npid: %d\n\nmain: 0123456\nstore: /nix/store/x/bin/mugicrew\n\nin flight: 0\n\nstale: build of main fedcba9 failed (see build.failed in events)\n", pid)+watching)
	}
	lk.Write(looplock.Status{State: looplock.Draining, Rev: "0123456789abcdef", RestartTo: "fedcba9876543210", Inflight: []string{"Build: a", "Build: b"}})
	if text, _ = status(path); text != fmt.Sprintf("state: draining\nrestarting onto: main fedcba9\npid: %d\n\nmain: 0123456\nstore: /nix/store/x/bin/mugicrew\n\nin flight: 2\n", pid)+watching {
		t.Errorf("restart drain status = %q; want %q", text, fmt.Sprintf("state: draining\nrestarting onto: main fedcba9\npid: %d\n\nmain: 0123456\nstore: /nix/store/x/bin/mugicrew\n\nin flight: 2\n", pid)+watching)
	}
	lk.Write(looplock.Status{State: looplock.Draining, Rev: "0123456789abcdef", Inflight: []string{"Build: a"}}) // a stop converted it
	if text, _ = status(path); text != fmt.Sprintf("state: draining\npid: %d\n\nmain: 0123456\nstore: /nix/store/x/bin/mugicrew\n\nin flight: 1\n", pid)+watching {
		t.Errorf("converted drain status = %q; want %q", text, fmt.Sprintf("state: draining\npid: %d\n\nmain: 0123456\nstore: /nix/store/x/bin/mugicrew\n\nin flight: 1\n", pid)+watching)
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
	if want := fmt.Sprintf("state: on\npid: %d\n\nmain: fedcba9\nstore: %s\n\nin flight: 0\n", child, self); status.String() != want {
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

// An unstamped loop's children inherit a PATH leading with the binary's own dir
// (Spec FR11, FR15): loop start spawns this test binary, re-exec'd as an unstamped
// mugicrew, through main(); the bd it runs records the PATH it inherited.
func TestUnstampedLoopChildPathLeadsWithSelf(t *testing.T) {
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	t.Chdir(repo)
	fakeBd(t, "", "") // PATH now leads with the stub dir, not the binary's
	pathLog := filepath.Join(t.TempDir(), "path.log")
	t.Setenv("FAKE_BD_PATH", pathLog)
	t.Setenv("MUGICREW_TEST_MAIN", "1")
	t.Setenv("BEADS_DIR", t.TempDir())
	origRoot := sourceRoot
	sourceRoot = "" // unstamped: loop start spawns os.Executable()
	t.Cleanup(func() { sourceRoot = origRoot })
	path, common, err := resolveLoop()
	if err != nil {
		t.Fatal(err)
	}
	if err := runLoopStart(path, common, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	st, _ := looplock.Read(path)
	t.Cleanup(func() { syscall.Kill(st.Pid, syscall.SIGKILL) })
	self, _ := os.Executable()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		b, _ := os.ReadFile(pathLog)
		if first, _, _ := strings.Cut(string(b), string(os.PathListSeparator)); len(b) > 0 {
			if first != filepath.Dir(self) {
				t.Fatalf("child PATH starts %q; want the binary's dir %q", first, filepath.Dir(self))
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the child loop never ran bd")
		}
	}
	if err := runLoopStop(path, true, io.Discard); err != nil {
		t.Fatal(err)
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
		env = []string{"PATH=" + dir + sep + strings.TrimPrefix(env[0], "PATH=")} // leadPathWithSelf's prepend
		env = stripPathHead(env, dir)                                             // execRestart's strip
	}
	if env[0] != "PATH="+orig {
		t.Errorf("after 7 restarts PATH = %q, want %q", env[0], "PATH="+orig)
	}
}

// An unstamped binary leads PATH with its own dir so bare `mugicrew` in a
// crewmate resolves to it (Spec FR11–15); a stamped one leaves PATH alone, and a
// PATH already leading with the dir isn't doubled. stripPathHead undoes the prefix.
func TestLeadPathWithSelf(t *testing.T) {
	sep := string(os.PathListSeparator)
	exe, _ := os.Executable()
	self := filepath.Dir(exe)
	orig := "/usr/bin" + sep + "/bin"
	origRoot := sourceRoot
	t.Cleanup(func() { sourceRoot = origRoot })
	for _, c := range []struct{ name, root, path, want string }{
		{"unstamped prepends", "", orig, self + sep + orig},
		{"stamped leaves", "/src", orig, orig},
		{"already leading", "", self + sep + orig, self + sep + orig},
		{"later entry still prepends", "", "/usr/bin" + sep + self, self + sep + "/usr/bin" + sep + self},
	} {
		sourceRoot = c.root
		t.Setenv("PATH", c.path)
		leadPathWithSelf()
		if got := os.Getenv("PATH"); got != c.want {
			t.Errorf("%s: PATH = %q, want %q", c.name, got, c.want)
		}
	}
	sourceRoot = ""
	t.Setenv("PATH", orig)
	leadPathWithSelf()
	leadPathWithSelf() // idempotent
	if got := stripPathHead(os.Environ(), self); !slices.Contains(got, "PATH="+orig) {
		t.Errorf("strip after prefix: PATH not restored to %q", orig)
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
	abortEffort = func(id, by string) ([]int, bool, error) {
		called = append(called, id+" "+by)
		switch id {
		case "ws-live":
			return []int{41, 42}, false, nil
		case "ws-held":
			return nil, true, nil
		case "ws-open":
			return nil, false, errors.New("ws-open: nothing in flight")
		}
		return nil, false, nil
	}
	for _, argv := range [][]string{nil, {"mc-a", "mc-b"}} {
		if err := runAbort(argv, io.Discard); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Errorf("runAbort(%q) = %v, want a usage error", argv, err)
		}
	}
	if len(called) != 0 {
		t.Fatalf("a usage error must abort nothing, got %v", called)
	}
	for id, want := range map[string]string{"ws-live": "signalled 41 42\n", "ws-dead": "no live process; paused only\n", "ws-held": "dispatcher holds it with no child; paused, it stops or finishes at its next step\n"} {
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

// `mugicrew --help`, -h and help print the usage on stdout and exit 0 from a
// repo with no Bead Graph and from outside any repo (the gate is bypassed),
// also after -C, ignoring trailing args and logging nothing (Spec FR1–5, FR10).
func TestHelpPrintsUsage(t *testing.T) {
	repo, bare := t.TempDir(), t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	t.Setenv("BEADS_DIR", "")
	self, _ := os.Executable()
	for _, tc := range []struct {
		dir  string
		args []string
	}{
		{repo, []string{"--help"}}, {repo, []string{"-h"}}, {repo, []string{"help"}},
		{bare, []string{"--help"}}, {bare, []string{"help", "foo", "bar"}},
		{bare, []string{"-C", repo, "--help"}}, {bare, []string{"-C", repo, "-h", "x"}},
	} {
		cmd := exec.Command(self, tc.args...)
		cmd.Dir = tc.dir
		cmd.Env = append(os.Environ(), "MUGICREW_TEST_MAIN=1")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil || string(out) != usage+"\n" || stderr.Len() != 0 {
			t.Errorf("%v in %s = %v, stdout %q, stderr %q; want exit 0 and the usage", tc.args, tc.dir, err, out, stderr.String())
		}
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "mugicrew", "events.jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("help left an event log: %v", err)
	}
}

// An unknown verb still exits 1 and points at --help, without the full usage (FR9).
func TestUnknownCommandPointsAtHelp(t *testing.T) {
	t.Chdir(t.TempDir())
	self, _ := os.Executable()
	cmd := exec.Command(self, "bogus")
	cmd.Env = append(os.Environ(), "MUGICREW_TEST_MAIN=1")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("bogus = %v, want exit 1; output %s", err, out)
	}
	if !strings.Contains(string(out), "mugicrew --help") || strings.Contains(string(out), "land-lineage") {
		t.Errorf("output %q, want the --help hint and not the usage", out)
	}
}

// The graph noun is gone: any subcommand is an unknown command and usage has no graph line.
func TestGraphVerbIsUnknown(t *testing.T) {
	t.Chdir(t.TempDir())
	self, _ := os.Executable()
	for _, sub := range []string{"push", "fetch"} {
		cmd := exec.Command(self, "graph", sub)
		cmd.Env = append(os.Environ(), "MUGICREW_TEST_MAIN=1")
		out, err := cmd.CombinedOutput()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(out), "unknown command") {
			t.Errorf("graph %s = %v, %s; want exit 1 and the unknown-command error", sub, err, out)
		}
	}
	if strings.Contains(usage, "mugicrew graph") {
		t.Error("usage still lists a graph verb")
	}
}

// Drift guard: the usage names every user-facing verb main dispatches and none
// of the hidden surface (FR7, FR8; ADR 0060 §3, ADR 0037, ADR 0099).
func TestUsageListsVerbsNotHidden(t *testing.T) {
	for _, verb := range []string{"signal new", "signal triage", "signal offshoot", "surgery new", "surgery end", "pivot confirm",
		"pivot escalate", "pivot raise", "reconcile", "loop", "abort", "ps", "stats", "doctor", "transcript", "integrate", "chart append|drop", "land-lineage", "--watch", "-C <dir>"} {
		if !strings.Contains(usage, "\n  mugicrew "+verb) {
			t.Errorf("usage lacks %q", verb)
		}
	}
	for _, hidden := range []string{"hitl-run", "hitl-follow", "doc-pane", "hitl-hook", "bead-detail", detachedArg, revArg, sourceArg, lockFdArg} {
		if strings.Contains(usage, hidden) {
			t.Errorf("usage leaks hidden %q", hidden)
		}
	}
}

// rootCtx is appCtx's ForKind pinned to a Workspace root, so the triage verb's
// profile resolution runs against a test catalog without a git repo.
type rootCtx struct{ root string }

func (c rootCtx) ForKind(k contract.Kind) (contract.Ctx, string, error) { return ctxForKind(c.root, k) }

// A HITL Signal triage resolves the signal kind's Agent Profile like the TUI
// launch (Spec FR25, FR29, FR33; ADR 0125): a mapped profile's --model and env
// reach the exec'd command (env, never argv) and hitl.launched names it; the
// built-in has no profile attr; an unrunnable mapping is refused with
// profile.unusable {launch: hitl} and nothing exec'd.
func TestTriageResolvesSignalProfile(t *testing.T) {
	// Luffy's best-effort adoption lookup warns when bd can't find the fake Signal.
	first := func(capt *[]eventlogtest.Event) eventlogtest.Event {
		for _, e := range *capt {
			if e.Name != "adoption.skipped" {
				return e
			}
		}
		return eventlogtest.Event{}
	}
	t.Setenv("DEEPSEEK_API_KEY", "sk-123")
	t.Setenv("BEADS_DIR", t.TempDir()+"/.beads") // the adoption lookup must not read an inherited graph
	noDrafts(t)
	origExec, origCwd := interactiveExec, resolveCwd
	t.Cleanup(func() { interactiveExec, resolveCwd = origExec, origCwd })
	resolveCwd = func(bd.Bead) (string, func(), error) { return "/repo", func() {}, nil }
	sig := bd.Bead{ID: "sig1", Kind: contract.KindSignal}

	root := profileWorkspace(t, deepseekCatalog, "[kind_profiles]\nsignal = \"deepseek\"\n")
	capt := eventlogtest.Capture(t)
	var got *exec.Cmd
	interactiveExec = func(cmd *exec.Cmd) error { got = cmd; return nil }
	if err := triage(rootCtx{root}, sig); err != nil {
		t.Fatalf("triage: %v", err)
	}
	if got == nil || !slices.Contains(got.Args, "deepseek-chat") || !slices.Contains(got.Env, "ANTHROPIC_AUTH_TOKEN=sk-123") {
		t.Fatalf("exec'd %+v; want --model deepseek-chat and the profile env", got)
	}
	if !strings.Contains(strings.Join(got.Args, " "), "You are Luffy, triaging") {
		t.Errorf("signal triage exec'd %v; want Luffy's triage session (ADR 0129)", got.Args)
	}
	if strings.Contains(strings.Join(got.Args, " "), "sk-123") {
		t.Errorf("profile env leaked into argv: %v", got.Args)
	}
	if a := first(capt).Attrs; first(capt).Name != "hitl.launched" || a["profile"] != "deepseek" {
		t.Errorf("first event = %+v, want hitl.launched profile=deepseek", first(capt))
	}

	root = profileWorkspace(t, deepseekCatalog, "")
	capt = eventlogtest.Capture(t)
	got = nil
	if err := triage(rootCtx{root}, sig); err != nil || got == nil || got.Env != nil {
		t.Fatalf("built-in triage = %v, cmd %+v; want an exec with the inherited env", err, got)
	}
	if _, ok := first(capt).Attrs["profile"]; ok {
		t.Errorf("built-in hitl.launched carries a profile attr: %v", first(capt).Attrs)
	}

	root = profileWorkspace(t, deepseekCatalog, "[kind_profiles]\nsignal = \"opus\"\n")
	capt = eventlogtest.Capture(t)
	got = nil
	err := triage(rootCtx{root}, sig)
	if err == nil || !strings.HasPrefix(err.Error(), "profile opus: ") || got != nil {
		t.Fatalf("unrunnable triage = %v, exec'd %v; want a profile opus refusal, nothing exec'd", err, got)
	}
	if e := first(capt); e.Name != "profile.unusable" || e.Attrs["profile"] != "opus" || e.Attrs["launch"] != "hitl" || e.Attrs["kind"] != "signal" || e.Attrs["bead"] != "sig1" {
		t.Errorf("events = %+v, want profile.unusable {opus, hitl}", *capt)
	} else if _, ok := e.Attrs["path"]; ok || e.Attrs["reason"] != "not declared" {
		t.Errorf("undeclared refusal attrs = %v, want no path and reason not declared", e.Attrs)
	}

	root = profileWorkspace(t, deepseekCatalog, "[profiles.bad]\nharness = \"claudecode\"\nmodle = \"x\"\n[kind_profiles]\nsignal = \"bad\"\n")
	capt = eventlogtest.Capture(t)
	if err := triage(rootCtx{root}, sig); err == nil {
		t.Fatal("bad-table triage launched")
	}
	var a map[string]any
	for _, e := range *capt {
		if e.Name == "profile.unusable" {
			a = e.Attrs
		}
	}
	if a["path"] != cfglayer.SharedPath(root) || !strings.HasSuffix(fmt.Sprint(a["reason"]), " (from .mugicrew/config.toml)") {
		t.Errorf("bad-table refusal attrs = %v, want path %s and reason ending (from .mugicrew/config.toml)", a, cfglayer.SharedPath(root))
	}
}

// `mugicrew recover <bead> [--discard]` maps Recover's outcome to its exit code
// (ADR 0127 §9): 0 repaired or nothing to recover, printing one line per step
// then any pause left; 1 refused or a usage error, with the way out; 2 a failed
// step, saying re-run.
func TestRunRecover(t *testing.T) {
	orig := recoverBead
	t.Cleanup(func() { recoverBead = orig })
	var called []string
	recoverBead = func(id string, o chopper.Opts) (chopper.Report, error) {
		called = append(called, fmt.Sprint(id, " discard=", o.Discard))
		switch id {
		case "mc-dead":
			return chopper.Report{Verdict: chopper.Dead, Lines: []string{"dropped tree /t/wt", "unclaimed"}, Remaining: []string{"manual"}}, nil
		case "mc-live":
			return chopper.Report{}, &chopper.Refused{Reason: "mc-live: alive (pids 7); stop it with mugicrew abort mc-live"}
		case "mc-fail":
			return chopper.Report{Lines: []string{"fenced with paused:recovering"}}, &chopper.Failed{Bead: "mc-fail", Step: "unclaim", Err: errors.New("bd down")}
		}
		return chopper.Report{}, nil
	}
	for _, argv := range [][]string{{"mc-a", "mc-b"}, {"--nope", "mc-a"}, {"--force"}} {
		var errw bytes.Buffer
		if code := runRecover(argv, io.Discard, &errw); code != 1 || !strings.Contains(errw.String(), "usage:") {
			t.Errorf("runRecover(%q) = %d, %q, want exit 1 and usage", argv, code, errw.String())
		}
	}
	if len(called) != 0 {
		t.Fatalf("a usage error must recover nothing, got %v", called)
	}
	for _, c := range []struct {
		argv      []string
		code      int
		out, errw string
	}{
		{[]string{"mc-dead", "--discard"}, 0, "dropped tree /t/wt\nunclaimed\nstill paused: manual\n", ""},
		{[]string{"mc-clean"}, 0, "mc-clean: nothing to recover\n", ""},
		{[]string{"mc-live"}, 1, "", "mc-live: alive (pids 7); stop it with mugicrew abort mc-live\n"},
		{[]string{"mc-fail"}, 2, "fenced with paused:recovering\n", "mc-fail: unclaim failed: bd down; paused:recovering stays, re-run mugicrew recover mc-fail\n"},
	} {
		var out, errw bytes.Buffer
		if code := runRecover(c.argv, &out, &errw); code != c.code || out.String() != c.out || errw.String() != c.errw {
			t.Errorf("recover %q = %d, out %q, err %q; want %d, %q, %q", c.argv, code, out.String(), errw.String(), c.code, c.out, c.errw)
		}
	}
	if want := []string{"mc-dead discard=true", "mc-clean discard=false", "mc-live discard=false", "mc-fail discard=false"}; !slices.Equal(called, want) {
		t.Errorf("calls = %q, want %q", called, want)
	}
}

// `mugicrew recover` with no argument prints Chopper's listing and exits 0, or 1
// if the listing fails; it never repairs.
func TestRunRecoverLists(t *testing.T) {
	ob, ol := recoverBead, listStranded
	t.Cleanup(func() { recoverBead, listStranded = ob, ol })
	recoverBead = func(id string, _ chopper.Opts) (chopper.Report, error) {
		t.Errorf("the listing recovered %s", id)
		return chopper.Report{}, nil
	}
	listStranded = func(w io.Writer) error { _, err := fmt.Fprintln(w, "nothing stranded"); return err }
	var out bytes.Buffer
	if code := runRecover(nil, &out, io.Discard); code != 0 || out.String() != "nothing stranded\n" {
		t.Errorf("recover = %d, %q; want 0 and the listing", code, out.String())
	}
	listStranded = func(io.Writer) error { return errors.New("bd down") }
	var errw bytes.Buffer
	if code := runRecover(nil, io.Discard, &errw); code != 1 || errw.String() != "bd down\n" {
		t.Errorf("recover with bd down = %d, %q; want 1 naming it", code, errw.String())
	}
}

// --force passes through to Recover, and the real Recover refuses it on a real
// flock while the loop is on, before it reads the bead (ADR 0127 §2).
func TestRunRecoverForceRefusedWhileLoopOn(t *testing.T) {
	orig := recoverBead
	t.Cleanup(func() { recoverBead = orig })
	var got chopper.Opts
	recoverBead = func(_ string, o chopper.Opts) (chopper.Report, error) { got = o; return chopper.Report{}, nil }
	if code := runRecover([]string{"--force", "mc-a"}, io.Discard, io.Discard); code != 0 || !got.Force || got.Discard {
		t.Fatalf("recover --force mc-a = %d, opts %+v; want Force passed", code, got)
	}

	recoverBead = orig
	dir := t.TempDir()
	t.Setenv("BEADS_DIR", dir)
	origCommon := git.CommonDir
	t.Cleanup(func() { git.CommonDir = origCommon })
	git.CommonDir = func() (string, error) { return t.TempDir(), nil }
	lk, err := looplock.Acquire(filepath.Join(dir, "mugicrew-loop.lock"), "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer lk.Close()
	evs := eventlogtest.Capture(t)
	var errw bytes.Buffer
	if code := runRecover([]string{"mc-a", "--force"}, io.Discard, &errw); code != 1 || !strings.Contains(errw.String(), "--force needs the loop off") {
		t.Fatalf("recover --force with the loop on = %d, %q; want exit 1 naming the loop", code, errw.String())
	}
	if names := eventlogtest.Names(*evs); !slices.Equal(names, []string{"recover.refused"}) {
		t.Errorf("events = %v, want one recover.refused", names)
	}
}

// `mugicrew withdraw <head>`, its alias `initiative withdraw <head>` and
// `initiative cancel <initiative>` (Spec mc-5rttm D8) map onto the verbs with one
// in-flight test: an active AFK effort or a live session. Anything else is a
// usage error that acts on nothing.
func TestRunInitiative(t *testing.T) {
	oc, ow, osn := cancelInitiative, withdrawChain, initiativeSessions
	t.Cleanup(func() { cancelInitiative, withdrawChain, initiativeSessions = oc, ow, osn })
	initiativeSessions = func() (map[string]session.Info, error) {
		return map[string]session.Info{"luffy": {State: session.Alive}, "gone": {State: session.Exited}}, nil
	}
	var called []string
	var flight func(bd.Bead) bool
	var released []string
	cancelInitiative = func(id string, o withdraw.Opts) (withdraw.Result, error) {
		called, flight, released = append(called, "cancel "+id), o.InFlight, o.Release
		return withdraw.Result{}, nil
	}
	withdrawChain = func(id string, o withdraw.Opts) (withdraw.Result, error) {
		called = append(called, "withdraw "+id)
		return withdraw.Result{}, nil
	}
	for _, argv := range [][]string{nil, {"cancel"}, {"adopt", "x"}, {"withdraw", "a", "b"}} {
		if err := runInitiative(argv); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Errorf("runInitiative(%q) = %v, want a usage error", argv, err)
		}
	}
	for _, argv := range [][]string{nil, {}, {"a", "b"}, {""}} {
		if err := runWithdraw(argv); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Errorf("runWithdraw(%q) = %v, want a usage error", argv, err)
		}
	}
	if err := runInitiative([]string{"cancel", "in"}); err != nil {
		t.Fatal(err)
	}
	if err := runInitiative([]string{"cancel", "in", "--release", "a", "--release", "b"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(released, []string{"a", "b"}) {
		t.Errorf("released = %q", released)
	}
	called = called[:1]
	for _, argv := range [][]string{{"cancel", "in", "stray"}, {"cancel", "--release", "a"}, {"cancel", "in", "--release"}} {
		if err := runInitiative(argv); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Errorf("runInitiative(%q) = %v, want a usage error", argv, err)
		}
	}
	if err := runInitiative([]string{"withdraw", "cp"}); err != nil {
		t.Fatal(err)
	}
	if err := runWithdraw([]string{"cp2"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(called, []string{"cancel in", "withdraw cp", "withdraw cp2"}) {
		t.Errorf("called = %q", called)
	}
	for _, tc := range []struct {
		b    bd.Bead
		want bool
	}{
		{bd.Bead{ID: "b", Kind: contract.KindBuild, Status: "in_progress"}, true},
		{bd.Bead{ID: "b", Kind: contract.KindBuild, Status: "open"}, false},
		{bd.Bead{ID: "luffy", Kind: contract.KindSignal, Status: "in_progress"}, true},
		{bd.Bead{ID: "gone", Kind: contract.KindSignal, Status: "in_progress"}, false},
	} {
		if got := flight(tc.b); got != tc.want {
			t.Errorf("inFlight(%s %s) = %v, want %v", tc.b.ID, tc.b.Status, got, tc.want)
		}
	}
}

// `mugicrew initiative adopt <head> <initiative>` / `release <head>` (Spec D5)
// map onto bd.AdoptChain / bd.ReleaseChain, reading no session table.
func TestRunInitiativeAdoptRelease(t *testing.T) {
	oa, or, osn := adoptChain, releaseChain, initiativeSessions
	t.Cleanup(func() { adoptChain, releaseChain, initiativeSessions = oa, or, osn })
	initiativeSessions = func() (map[string]session.Info, error) { return nil, errors.New("tmux down") }
	var called []string
	adoptChain = func(head, ini string) error { called = append(called, "adopt "+head+" "+ini); return nil }
	releaseChain = func(head string) error { called = append(called, "release "+head); return nil }
	for _, argv := range [][]string{{"adopt", "x"}, {"adopt", "x", "y", "z"}, {"release"}, {"release", "a", "b"}} {
		if err := runInitiative(argv); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Errorf("runInitiative(%q) = %v, want a usage error", argv, err)
		}
	}
	if err := runInitiative([]string{"adopt", "cp", "in"}); err != nil {
		t.Fatal(err)
	}
	if err := runInitiative([]string{"release", "cp"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(called, []string{"adopt cp in", "release cp"}) {
		t.Errorf("called = %q", called)
	}
}

// Bare mugicrew opens the TUI with no flag: outside the private server the
// self-launch re-passes only --watch (Spec FR1, FR3, FR5); --v2 is a usage error
// like any unknown argument (FR9).
func TestBareLaunchesTUI(t *testing.T) {
	orig := launchTUI
	t.Cleanup(func() { launchTUI = orig })
	var got [][]string
	launchTUI = func(args ...string) error { got = append(got, args); return nil }
	t.Setenv("TMUX", "")
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	t.Chdir(repo)
	for _, watch := range []bool{false, true} {
		if err := runBare(watch); err != nil {
			t.Fatalf("runBare(watch=%v): %v", watch, err)
		}
	}
	if len(got) != 2 || len(got[0]) != 0 || !slices.Equal(got[1], []string{watchArg}) {
		t.Errorf("launches = %q; want bare then [--watch]", got)
	}
	self, _ := os.Executable()
	cmd := exec.Command(self, "--v2")
	cmd.Env = append(os.Environ(), "MUGICREW_TEST_MAIN=1")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(out), "unknown command") {
		t.Errorf("--v2 = %v, %s; want exit 1 and the unknown-command error", err, out)
	}
}

// `mugicrew surgery new --checkpoint` joins the Checkpoint (Spec D5): success
// prints the Surgery id; a refusal is prefixed "surgery new: " and files nothing.
func TestRunSurgeryNewCheckpoint(t *testing.T) {
	var got [3]string
	old := newSurgeryOn
	t.Cleanup(func() { newSurgeryOn = old })
	newSurgeryOn = func(cp, title, problem string) (string, error) {
		got = [3]string{cp, title, problem}
		if cp == "ws-busy" {
			return "", bd.JoinRefusal("ws-busy is landing — no surgery while a Landing is live")
		}
		return "ws-sg", nil
	}
	capture := func(argv []string) (string, error) {
		r, w, _ := os.Pipe()
		stdout := os.Stdout
		os.Stdout = w
		err := runSurgeryNew(argv)
		os.Stdout = stdout
		w.Close()
		b, _ := io.ReadAll(r)
		return string(b), err
	}

	out, err := capture([]string{"--title", "Fix", "--problem", "p", "--checkpoint", "ws-cp"})
	if err != nil || out != "ws-sg\n" || got != [3]string{"ws-cp", "Fix", "p"} {
		t.Errorf("success: out %q err %v got %v", out, err, got)
	}
	out, err = capture([]string{"--title", "Fix", "--checkpoint", "ws-busy"})
	if want := "surgery new: ws-busy is landing — no surgery while a Landing is live"; err == nil || err.Error() != want || out != "" {
		t.Errorf("refusal: out %q err %v, want %q", out, err, want)
	}
}

// nestedHost builds a Workspace declaring [context.child] repo = "child" (ignored by
// the Workspace repo), a detached host of its mainline, and the child materialized in
// it on effort/<id> for each id, one commit ahead of checkpoint/cp. It returns the
// host path and chdirs into it (Spec FR 26).
func nestedHost(t *testing.T, dirs ...string) (host string) {
	t.Helper()
	g := func(dir string, args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@e.com", "-c", "user.name=t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	ws := t.TempDir()
	g(ws, "init", "-q", "-b", "main")
	cfg := "land_strategy = \"local\"\n"
	for _, d := range dirs {
		cfg += "\n[context." + d + "]\nrepo = \"" + d + "\"\n"
	}
	os.MkdirAll(filepath.Join(ws, ".mugicrew"), 0o755)
	os.WriteFile(filepath.Join(ws, ".mugicrew", "config.toml"), []byte(cfg), 0o644)
	os.WriteFile(filepath.Join(ws, ".gitignore"), []byte(strings.Join(dirs, "/\n")+"/\n"), 0o644)
	g(ws, "add", ".")
	g(ws, "commit", "-q", "-m", "root")
	host = filepath.Join(t.TempDir(), "host")
	g(ws, "worktree", "add", "-q", "--detach", host, "main")
	for _, d := range dirs {
		main := filepath.Join(ws, d)
		os.Mkdir(main, 0o755)
		g(main, "init", "-q", "-b", "main")
		g(main, "commit", "-q", "--allow-empty", "-m", "root")
		g(main, "branch", "checkpoint/cp")
		if _, err := git.Repo(main).Materialize(filepath.Join(host, d), "b1", "checkpoint/cp", ""); err != nil {
			t.Fatal(err)
		}
		g(filepath.Join(host, d), "commit", "-q", "--allow-empty", "-m", "work")
	}
	t.Chdir(host)
	return host
}

const nestedBuild = `[{"id":"b1","title":"leaf","status":"in_progress","labels":["kind:build"],"parent":"cp"}]`

// A detached host with one child on effort/<id> integrates it onto the child's
// container; the Workspace's refs never move (Spec FR 26).
func TestIntegrateFromTheHostIntegratesTheOneChild(t *testing.T) {
	host := nestedHost(t, "child")
	fakeBd(t, nestedBuild, "[]")
	if rc := runIntegrate(nil); rc != 0 {
		t.Fatalf("runIntegrate = %d, want 0", rc)
	}
	out, _ := exec.Command("git", "-C", filepath.Join(host, "child"), "log", "-1", "--format=%s", "checkpoint/cp").Output()
	if got := strings.TrimSpace(string(out)); got != "feat: leaf [b1]" {
		t.Errorf("child checkpoint/cp tip = %q, want the squash", got)
	}
}

// Two children on an effort branch are ambiguous: exit 1, nothing integrated.
func TestIntegrateFromTheHostRefusesTwoChildren(t *testing.T) {
	host := nestedHost(t, "alpha", "beta")
	fakeBd(t, nestedBuild, "[]")
	if rc := runIntegrate(nil); rc != 1 {
		t.Fatalf("runIntegrate = %d, want 1", rc)
	}
	out, _ := exec.Command("git", "-C", filepath.Join(host, "alpha"), "rev-list", "--count", "checkpoint/cp").Output()
	if got := strings.TrimSpace(string(out)); got != "1" {
		t.Errorf("alpha checkpoint/cp has %s commits, want the untouched 1", got)
	}
}

// Run from inside the child, integrate refuses with the Workspace-tree hint.
func TestIntegrateFromInsideTheChildRefuses(t *testing.T) {
	host := nestedHost(t, "child")
	// ADR 0168 §2: only a bead carrying context: is checked
	fakeBd(t, strings.Replace(nestedBuild, `"kind:build"`, `"kind:build","context:child"`, 1), "[]")
	t.Chdir(filepath.Join(host, "child"))
	r, w, _ := os.Pipe()
	orig := os.Stderr
	os.Stderr = w
	rc := runIntegrate(nil)
	os.Stderr = orig
	w.Close()
	msg, _ := io.ReadAll(r)
	if rc != 1 || !strings.Contains(string(msg), "run mugicrew integrate from the Workspace tree") {
		t.Fatalf("runIntegrate in the child = %d, %q; want 1 and the Workspace-tree hint", rc, msg)
	}
}

// A leaf never carries context:; the guard reads it off the routed container.
func TestIntegrateFromInsideTheChildRefusesAnUnlabelledLeaf(t *testing.T) {
	host := nestedHost(t, "child")
	fakeBd(t, nestedBuild, "[]")
	orig := contexts.ShowBead
	contexts.ShowBead = func(string) (bd.Bead, error) {
		return bd.Bead{ID: "cp", Labels: []string{"context:child"}}, nil
	}
	defer func() { contexts.ShowBead = orig }()
	t.Chdir(filepath.Join(host, "child"))
	r, w, _ := os.Pipe()
	origErr := os.Stderr
	os.Stderr = w
	rc := runIntegrate(nil)
	os.Stderr = origErr
	w.Close()
	msg, _ := io.ReadAll(r)
	if rc != 1 || !strings.Contains(string(msg), "run mugicrew integrate from the Workspace tree") {
		t.Fatalf("runIntegrate in the child = %d, %q; want 1 and the Workspace-tree hint", rc, msg)
	}
}

// With no contexts declared a detached tree keeps today's refusal.
func TestIntegrateDetachedWithNoContextsKeepsTodaysRefusal(t *testing.T) {
	host := nestedHost(t)
	t.Chdir(host)
	_, want := git.CurrentEffort()
	if rc := runIntegrate(nil); rc != 1 || want == nil {
		t.Errorf("runIntegrate = %d (%v), want 1", rc, want)
	}
}

// gitShim wraps git on PATH so every spawn is logged; it returns the log path.
func gitShim(t *testing.T) string {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "git.log")
	script := "#!/bin/sh\necho \"$*\" >> " + log + "\nexec " + real + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// assertNoAncestorGit fails if any logged git call names a dir above the Workspace.
func assertNoAncestorGit(t *testing.T, log, ws string) {
	t.Helper()
	b, _ := os.ReadFile(log)
	for p := filepath.Dir(ws); ; p = filepath.Dir(p) {
		for _, l := range strings.Split(string(b), "\n") {
			if strings.Contains(l, " "+p+" ") || strings.HasSuffix(l, " "+p) {
				t.Errorf("git call walked up to %s: %q", p, l)
			}
		}
		if p == filepath.Dir(p) {
			return
		}
	}
}

// FR 4: with no repo = declared, integrate runs no git per ancestor of the cwd.
func TestIntegrateNoContextsRunsNoAncestorGit(t *testing.T) {
	ws := t.TempDir()
	g := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", ws, "-c", "user.email=t@e.com", "-c", "user.name=t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	g("init", "-q", "-b", "main")
	os.MkdirAll(filepath.Join(ws, ".mugicrew"), 0o755)
	os.WriteFile(filepath.Join(ws, ".mugicrew", "config.toml"), []byte("land_strategy = \"local\"\n"), 0o644)
	g("add", ".")
	g("commit", "-q", "-m", "root")
	g("branch", "checkpoint/cp")
	eff := filepath.Join(t.TempDir(), "eff")
	g("worktree", "add", "-q", "-b", "effort/b1", eff, "checkpoint/cp")
	if out, err := exec.Command("git", "-C", eff, "-c", "user.email=t@e.com", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "work").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	fakeBd(t, nestedBuild, "[]")
	log := gitShim(t)

	t.Chdir(eff)
	if rc := runIntegrate(nil); rc != 0 {
		t.Fatalf("runIntegrate = %d, want 0", rc)
	}
	assertNoAncestorGit(t, log, eff)

	// the detached refusal: today's text, and no Detached probe
	g("worktree", "add", "-q", "--detach", filepath.Join(filepath.Dir(eff), "det"), "main")
	det := filepath.Join(filepath.Dir(eff), "det")
	os.Truncate(log, 0)
	t.Chdir(det)
	_, want := git.CurrentEffort()
	os.Truncate(log, 0)
	r, w, _ := os.Pipe()
	orig := os.Stderr
	os.Stderr = w
	rc := runIntegrate(nil)
	os.Stderr = orig
	w.Close()
	msg, _ := io.ReadAll(r)
	if rc != 1 || strings.TrimSpace(string(msg)) != want.Error() {
		t.Errorf("refusal = %d %q, want 1 %q", rc, msg, want)
	}
	assertNoAncestorGit(t, log, det)

}

func TestVersionString(t *testing.T) {
	old := version
	t.Cleanup(func() { version = old })
	version = ""
	if got := versionString(); got != "unknown" {
		t.Fatalf("unset: got %q", got)
	}
	version = "1.2.3"
	if got := versionString(); got != "1.2.3" {
		t.Fatalf("set: got %q", got)
	}
}

// `mugicrew approval rework <approval> --critique <file>...` dispatches to the
// rework act with every repeated --critique, and prints the ids it returns.
func TestRunApproval(t *testing.T) {
	orig := reworkApproval
	t.Cleanup(func() { reworkApproval = orig })
	var gotID string
	var gotFiles []string
	reworkApproval = func(id string, files []string) ([]string, error) {
		gotID, gotFiles = id, files
		return []string{"c1", "c2"}, nil
	}
	for _, argv := range [][]string{nil, {"rework"}, {"nope", "ap", "--critique", "a"}, {"rework", "ap", "x", "--critique", "a"}} {
		if err := runApproval(argv, io.Discard); err == nil || !strings.HasPrefix(err.Error(), "usage:") {
			t.Errorf("runApproval(%q) = %v, want a usage error", argv, err)
		}
	}
	var out strings.Builder
	if err := runApproval([]string{"rework", "ap", "--critique", "a.md", "--critique", "b.md"}, &out); err != nil {
		t.Fatal(err)
	}
	if gotID != "ap" || !slices.Equal(gotFiles, []string{"a.md", "b.md"}) || out.String() != "c1\nc2\n" {
		t.Errorf("got %q %q, out %q", gotID, gotFiles, out.String())
	}
}

// intakeWorkspace is a Workspace root whose shared config is cfg, with a fake acli
// on PATH printing out; it returns the acli argv log and the Signals filed.
func intakeWorkspace(t *testing.T, cfg, out string) (argvLog string, filed *[][3]string) {
	t.Helper()
	ws := t.TempDir()
	if err := exec.Command("git", "init", "-q", ws).Run(); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(ws, ".mugicrew"), 0o755)
	os.WriteFile(filepath.Join(ws, ".mugicrew", "config.toml"), []byte(cfg), 0o644)
	origRoot, origNew, origCarrier := git.RepoRoot, jira.NewSignalWithRef, jira.CarrierOf
	t.Cleanup(func() { git.RepoRoot, jira.NewSignalWithRef, jira.CarrierOf = origRoot, origNew, origCarrier })
	jira.CarrierOf = func(string) (bd.Bead, bool, error) { return bd.Bead{}, false, nil }
	git.RepoRoot = func() (string, error) { return ws, nil }
	filed = &[][3]string{}
	jira.NewSignalWithRef = func(title, body, ref string) (string, error) {
		*filed = append(*filed, [3]string{title, body, ref})
		return "mc-new", nil
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "out"), []byte(out), 0o644)
	os.WriteFile(filepath.Join(dir, "acli"), []byte("#!/bin/sh\necho \"$*\" >> "+dir+"/argv.log\ncat "+dir+"/out\n"), 0o755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return filepath.Join(dir, "argv.log"), filed
}

func TestIntakeJiraFilesARefCarryingSignal(t *testing.T) {
	log, filed := intakeWorkspace(t, "[jira]\nsite = \"acme.atlassian.net\"\n", `{"key":"ABC-1","fields":{"summary":"Fix it","status":{"name":"Open","statusCategory":{"key":"new"}}}}`)
	var out strings.Builder
	if err := runIntakeJira([]string{"ABC-1"}, &out); err != nil {
		t.Fatal(err)
	}
	wantBody := "Jira work item ABC-1: https://acme.atlassian.net/browse/ABC-1\n\nTriage reads this work item from Jira; this body only points at it:\n\n    acli jira workitem view ABC-1\n"
	if want := [][3]string{{"ABC-1: Fix it", wantBody, "jira-ABC-1"}}; !slices.Equal(*filed, want) {
		t.Errorf("filed %q, want %q", *filed, want)
	}
	if out.String() != "mc-new\n" {
		t.Errorf("printed %q, want the Signal id", out.String())
	}
	if b, _ := os.ReadFile(log); string(b) != "jira workitem view ABC-1 --fields summary,status --json\n" {
		t.Errorf("acli argv log %q: want exactly one view call", b)
	}
}

func TestIntakeJiraMovedKeyWins(t *testing.T) {
	_, filed := intakeWorkspace(t, "[jira]\nsite = \"acme.atlassian.net\"\n", `{"key":"NEW-9","fields":{"summary":"Moved","status":{"name":"Open","statusCategory":{"key":"new"}}}}`)
	var out strings.Builder
	if err := runIntakeJira([]string{"OLD-1"}, &out); err != nil {
		t.Fatal(err)
	}
	f := (*filed)[0]
	if f[0] != "NEW-9: Moved" || f[2] != "jira-NEW-9" || !strings.Contains(f[1], "https://acme.atlassian.net/browse/NEW-9") {
		t.Errorf("filed %q, want the returned key throughout", f)
	}
	if out.String() != "OLD-1 is now NEW-9\nmc-new\n" {
		t.Errorf("printed %q", out.String())
	}
}

func TestIntakeJiraRenameLineSurvivesBdFailure(t *testing.T) {
	moved := `{"key":"NEW-9","fields":{"summary":"Moved","status":{"name":"Open","statusCategory":{"key":"new"}}}}`
	for name, fail := range map[string]func(){
		"carrier": func() {
			jira.CarrierOf = func(string) (bd.Bead, bool, error) { return bd.Bead{}, false, errors.New("boom") }
		},
		"filing": func() { jira.NewSignalWithRef = func(_, _, _ string) (string, error) { return "", errors.New("boom") } },
	} {
		t.Run(name, func(t *testing.T) {
			intakeWorkspace(t, "[jira]\nsite = \"acme.atlassian.net\"\n", moved)
			fail()
			var out strings.Builder
			err := runIntakeJira([]string{"OLD-1"}, &out)
			if err == nil || err.Error() != "intake jira OLD-1: boom" {
				t.Errorf("err = %v", err)
			}
			if out.String() != "OLD-1 is now NEW-9\n" {
				t.Errorf("printed %q", out.String())
			}
		})
	}
}

func TestIntakeJiraWantsExactlyOneKey(t *testing.T) {
	for _, argv := range [][]string{nil, {"A-1", "B-2"}} {
		if err := runIntakeJira(argv, io.Discard); err == nil || !strings.Contains(err.Error(), "usage") {
			t.Errorf("runIntakeJira(%v) = %v, want a usage error", argv, err)
		}
	}
}

func TestIntakeJiraReintakeSupersedesOntoCarrier(t *testing.T) {
	_, filed := intakeWorkspace(t, "[jira]\nsite = \"acme.atlassian.net\"\n", `{"key":"ABC-1","fields":{"summary":"Fix it","status":{"name":"Open","statusCategory":{"key":"new"}}}}`)
	oc, osup := jira.CarrierOf, jira.Supersede
	t.Cleanup(func() { jira.CarrierOf, jira.Supersede = oc, osup })
	var asked string
	var edge [2]string
	jira.CarrierOf = func(ref string) (bd.Bead, bool, error) { asked = ref; return bd.Bead{ID: "mc-old"}, true, nil }
	jira.Supersede = func(old, n string) error { edge = [2]string{old, n}; return nil }
	var out strings.Builder
	if err := runIntakeJira([]string{"ABC-1"}, &out); err != nil {
		t.Fatal(err)
	}
	if asked != "jira-ABC-1" || len(*filed) != 1 || edge != [2]string{"mc-new", "mc-old"} {
		t.Errorf("asked %q, filed %d, edge %v", asked, len(*filed), edge)
	}
	if !strings.HasPrefix(out.String(), "mc-new\n") || !strings.Contains(out.String(), "mc-old") {
		t.Errorf("printed %q, want the id then a line naming the carrier", out.String())
	}
}

const jiraNew = `{"key":"ABC-1","fields":{"summary":"Fix it","status":{"name":"Open","statusCategory":{"key":"new"}}}}`

// latchWorkspace is an intakeWorkspace whose bd legs are faked: showBead knows
// targets, latchOf finds existing, newLatch records filings, blocksEdge records
// edges not already in have.
type latchFake struct {
	filed       [][4]string // title, body, ref, draftedBy
	edges       [][2]string // blocked, blocker
	existing    *bd.Bead
	have        map[string]bool // blocked ids already blocked by the existing latch
	missingBead string
}

func latchWorkspace(t *testing.T, cfg, out string) (*latchFake, string) {
	t.Helper()
	log, _ := intakeWorkspace(t, cfg, out)
	f := &latchFake{have: map[string]bool{}}
	oS, oL, oN, oE := showBead, latchOf, newLatch, blocksEdge
	t.Cleanup(func() { showBead, latchOf, newLatch, blocksEdge = oS, oL, oN, oE })
	showBead = func(id string) (bd.Bead, error) {
		if id == f.missingBead {
			return bd.Bead{}, errors.New("not found")
		}
		return bd.Bead{ID: id}, nil
	}
	latchOf = func(string) (bd.Bead, bool, error) {
		if f.existing == nil {
			return bd.Bead{}, false, nil
		}
		return *f.existing, true, nil
	}
	newLatch = func(title, body, ref, by string) (string, error) {
		f.filed = append(f.filed, [4]string{title, body, ref, by})
		return "mc-latch", nil
	}
	blocksEdge = func(blocked, blocker string) error {
		if !f.have[blocked] { // the real bd.Blocks adds the edge only if absent
			f.edges = append(f.edges, [2]string{blocked, blocker})
		}
		return nil
	}
	return f, log
}

const siteCfg = "[jira]\nsite = \"acme.atlassian.net\"\n"

func TestLatchJiraFirstFilingBlocksEachTarget(t *testing.T) {
	f, _ := latchWorkspace(t, siteCfg, jiraNew)
	var out strings.Builder
	if err := runLatchJira([]string{"ABC-1", "--blocks", "mc-a", "--blocks", "mc-b"}, &out); err != nil {
		t.Fatal(err)
	}
	wantBody := "Watches: jira ABC-1\nhttps://acme.atlassian.net/browse/ABC-1\n\nFix it"
	if len(f.filed) != 1 || f.filed[0] != [4]string{"Jira ABC-1 is done", wantBody, "jira-latch-ABC-1", ""} {
		t.Errorf("filed = %q", f.filed)
	}
	if !slices.Equal(f.edges[0][:], []string{"mc-a", "mc-latch"}) || len(f.edges) != 2 || out.String() != "mc-latch\n" {
		t.Errorf("edges %v, out %q", f.edges, out.String())
	}
}

func TestLatchJiraUsageErrorsBeforeAcli(t *testing.T) {
	_, log := latchWorkspace(t, siteCfg, jiraNew)
	for _, argv := range [][]string{{}, {"ABC-1"}, {"--blocks", "mc-a"}, {"ABC-1", "--blocks"}} {
		if err := runLatchJira(argv, io.Discard); err == nil || !strings.Contains(err.Error(), "usage") {
			t.Errorf("runLatchJira(%v) = %v, want a usage error", argv, err)
		}
	}
	if _, err := os.Stat(log); err == nil {
		t.Error("acli ran on a usage error")
	}
}

func TestLatchJiraMissingTargetFilesNothing(t *testing.T) {
	f, _ := latchWorkspace(t, siteCfg, jiraNew)
	f.missingBead = "mc-b"
	err := runLatchJira([]string{"ABC-1", "--blocks", "mc-a", "--blocks", "mc-b"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "mc-b") || len(f.filed) != 0 || len(f.edges) != 0 {
		t.Errorf("err %v, filed %v, edges %v", err, f.filed, f.edges)
	}
}

func TestLatchJiraBadSiteAndFailedReadAreDiagnosedAndFileNothing(t *testing.T) {
	f, log := latchWorkspace(t, "[jira]\nsite = \"https://x\"\n", jiraNew)
	err := runLatchJira([]string{"ABC-1", "--blocks", "mc-a"}, io.Discard)
	if err == nil || !strings.HasPrefix(err.Error(), "latch jira ABC-1: [jira] site must be a bare host") {
		t.Errorf("bad site err = %v", err)
	}
	if _, serr := os.Stat(log); serr == nil {
		t.Error("acli ran before the site check")
	}
	f, _ = latchWorkspace(t, siteCfg, "usage: acli")
	err = runLatchJira([]string{"ABC-1", "--blocks", "mc-a"}, io.Discard)
	if want := "latch jira ABC-1: acli gave unexpected output for ABC-1; it may be too old (check 'acli --version' and update)"; err == nil || err.Error() != want || len(f.filed) != 0 {
		t.Errorf("err = %v, filed %v", err, f.filed)
	}
}

func TestLatchJiraMovedKeyWinsAndDoneFilesNothing(t *testing.T) {
	f, _ := latchWorkspace(t, siteCfg, `{"key":"NEW-9","fields":{"summary":"Moved","status":{"name":"Closed","statusCategory":{"key":"done"}}}}`)
	var out strings.Builder
	if err := runLatchJira([]string{"OLD-1", "--blocks", "mc-a"}, &out); err != nil {
		t.Fatal(err)
	}
	if want := "OLD-1 is now NEW-9\nNEW-9 is already done; no latch filed\n"; out.String() != want || len(f.filed) != 0 || len(f.edges) != 0 {
		t.Errorf("out %q filed %v edges %v", out.String(), f.filed, f.edges)
	}
	f, _ = latchWorkspace(t, siteCfg, `{"key":"NEW-9","fields":{"summary":"Moved","status":{"name":"Open","statusCategory":{"key":"new"}}}}`)
	out.Reset()
	runLatchJira([]string{"OLD-1", "--blocks", "mc-a"}, &out)
	if !strings.HasPrefix(out.String(), "OLD-1 is now NEW-9\n") || f.filed[0][2] != "jira-latch-NEW-9" {
		t.Errorf("out %q filed %q", out.String(), f.filed)
	}
}

func TestLatchJiraRerunReusesLatchAndAddsOnlyMissingEdges(t *testing.T) {
	f, _ := latchWorkspace(t, siteCfg, jiraNew)
	f.existing = &bd.Bead{ID: "mc-old", Labels: []string{"kind:latch", "paused:failing"}}
	f.have["mc-a"] = true
	var out strings.Builder
	if err := runLatchJira([]string{"ABC-1", "--blocks", "mc-a", "--blocks", "mc-b"}, &out); err != nil {
		t.Fatal(err)
	}
	if len(f.filed) != 0 || len(f.edges) != 1 || f.edges[0] != [2]string{"mc-b", "mc-old"} || out.String() != "mc-old\n" {
		t.Errorf("filed %v edges %v out %q", f.filed, f.edges, out.String())
	}
}

// A closed latch is invisible to latchOf, so the re-run files a fresh one that blocks the target.
func TestLatchJiraClosedLatchIsIgnored(t *testing.T) {
	f, _ := latchWorkspace(t, siteCfg, jiraNew)
	var out strings.Builder
	if err := runLatchJira([]string{"ABC-1", "--blocks", "mc-a"}, &out); err != nil {
		t.Fatal(err)
	}
	if len(f.filed) != 1 || len(f.edges) != 1 || f.edges[0] != [2]string{"mc-a", "mc-latch"} {
		t.Errorf("filed %v edges %v", f.filed, f.edges)
	}
}

func TestLatchJiraDraftedByFilesADraft(t *testing.T) {
	f, _ := latchWorkspace(t, siteCfg, jiraNew)
	if err := runLatchJira([]string{"ABC-1", "--blocks", "mc-a", "--drafted-by", "mc-me"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(f.filed) != 1 || f.filed[0][3] != "mc-me" {
		t.Errorf("filed = %q", f.filed)
	}
}

// mrWorkspace fakes the bd legs (latchWorkspace), the land strategy, the Forge
// project and the MR read.
func mrWorkspace(t *testing.T, strategy string, mr *forge.MR, readErr error) (*latchFake, *[]string) {
	t.Helper()
	f, _ := latchWorkspace(t, siteCfg, jiraNew)
	oS, oP, oG := git.LandStrategyName, forgeProject, getMR
	t.Cleanup(func() { git.LandStrategyName, forgeProject, getMR = oS, oP, oG })
	git.LandStrategyName = func() (string, error) { return strategy, nil }
	forgeProject = func() (forge.Project, error) { return forge.Project{Host: "gl.example.com", ID: "ns%2Fp"}, nil }
	var reads []string
	getMR = func(p forge.Project, iid int) (*forge.MR, error) {
		reads = append(reads, fmt.Sprintf("%s/%s!%d", p.Host, p.ID, iid))
		return mr, readErr
	}
	return f, &reads
}

var openMR = &forge.MR{State: forge.StateOpened, Title: "Add thing", WebURL: "https://gl.example.com/a/b/-/merge_requests/7"}

func TestLatchMRFilesTitleBodyAndRefExactly(t *testing.T) {
	f, reads := mrWorkspace(t, "gitlab", openMR, nil)
	var out strings.Builder
	if err := runLatchMR([]string{"a/b!7", "--blocks", "mc-a", "--blocks", "mc-b"}, &out); err != nil {
		t.Fatal(err)
	}
	wantBody := "Watches: mr gl.example.com/a/b!7\nhttps://gl.example.com/a/b/-/merge_requests/7\n\nAdd thing"
	if len(f.filed) != 1 || f.filed[0] != [4]string{"MR a/b!7 is merged", wantBody, "gitlab-mr-latch-gl.example.com/a/b!7", ""} {
		t.Errorf("filed = %q", f.filed)
	}
	if len(f.edges) != 2 || out.String() != "mc-latch\n" || len(*reads) != 1 || (*reads)[0] != "gl.example.com/a%2Fb!7" {
		t.Errorf("edges %v out %q reads %v", f.edges, out.String(), *reads)
	}
}

func TestLatchMRBareIIDUsesTheForgeProject(t *testing.T) {
	f, _ := mrWorkspace(t, "gitlab", openMR, nil)
	if err := runLatchMR([]string{"!3", "--blocks", "mc-a"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if f.filed[0][0] != "MR ns/p!3 is merged" {
		t.Errorf("title = %q", f.filed[0][0])
	}
}

func TestLatchMRRefusalsFileNothing(t *testing.T) {
	cases := []struct {
		name, strategy, ref, want string
		mr                        *forge.MR
		readErr                   error
	}{
		{"local strategy", "local", "!1", "needs land_strategy=gitlab", openMR, nil},
		{"closed", "gitlab", "!1", "ns/p!1 is closed", &forge.MR{State: forge.StateClosed}, nil},
		{"read failure", "gitlab", "!1", "latch mr !1: forge: not found; ", nil, forge.ErrNotFound},
		{"wrong host", "gitlab", "https://x.example.com/a/-/merge_requests/1", "x.example.com", openMR, nil},
		{"malformed", "gitlab", "nope", "not an MR URL", openMR, nil},
	}
	for _, c := range cases {
		f, _ := mrWorkspace(t, c.strategy, c.mr, c.readErr)
		err := runLatchMR([]string{c.ref, "--blocks", "mc-a"}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), c.want) || len(f.filed) != 0 || len(f.edges) != 0 {
			t.Errorf("%s: err %v filed %v edges %v", c.name, err, f.filed, f.edges)
		}
	}
	f, _ := mrWorkspace(t, "gitlab", openMR, nil)
	f.missingBead = "mc-b"
	if err := runLatchMR([]string{"!1", "--blocks", "mc-a", "--blocks", "mc-b"}, io.Discard); err == nil || len(f.filed) != 0 {
		t.Errorf("missing bead: err %v filed %v", err, f.filed)
	}
	for _, argv := range [][]string{{}, {"!1"}, {"--blocks", "mc-a"}} {
		if err := runLatchMR(argv, io.Discard); err == nil || !strings.Contains(err.Error(), "usage") {
			t.Errorf("runLatchMR(%v) = %v, want usage", argv, err)
		}
	}
}

func TestLatchMRAlreadyMergedFilesNothingAndSucceeds(t *testing.T) {
	f, _ := mrWorkspace(t, "gitlab", &forge.MR{State: forge.StateMerged}, nil)
	var out strings.Builder
	if err := runLatchMR([]string{"a/b!7", "--blocks", "mc-a"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "a/b!7 is already merged; no latch filed\n" || len(f.filed) != 0 || len(f.edges) != 0 {
		t.Errorf("out %q filed %v edges %v", out.String(), f.filed, f.edges)
	}
}

func TestLatchMRRerunAddsOnlyMissingEdgesAndDraftedByIsParentless(t *testing.T) {
	f, _ := mrWorkspace(t, "gitlab", openMR, nil)
	f.existing = &bd.Bead{ID: "mc-old", Labels: []string{"kind:latch", "paused:failing"}}
	f.have["mc-a"] = true
	var out strings.Builder
	if err := runLatchMR([]string{"!7", "--blocks", "mc-a", "--blocks", "mc-b"}, &out); err != nil {
		t.Fatal(err)
	}
	if len(f.filed) != 0 || len(f.edges) != 1 || f.edges[0] != [2]string{"mc-b", "mc-old"} || out.String() != "mc-old\n" {
		t.Errorf("filed %v edges %v out %q", f.filed, f.edges, out.String())
	}
	f, _ = mrWorkspace(t, "gitlab", openMR, nil)
	if err := runLatchMR([]string{"!7", "--blocks", "mc-a", "--drafted-by", "mc-me"}, io.Discard); err != nil || f.filed[0][3] != "mc-me" {
		t.Errorf("err %v filed %q", err, f.filed)
	}
}

// bead-detail's failures go to stderr with a non-zero exit and nothing on stdout.
func TestBeadDetailBadWidthFailsOnStderr(t *testing.T) {
	for _, w := range []string{"wide", "0"} {
		var out, errw strings.Builder
		if code := beadDetail("mc-x", w, &out, &errw); code == 0 || out.Len() != 0 || !strings.Contains(errw.String(), "bead-detail:") {
			t.Errorf("width %q: code %d, stdout %q, stderr %q", w, code, out.String(), errw.String())
		}
	}
}

// `mugicrew rebase` exit codes and stdout: refusals exit 1 and print to stderr; the
// idempotent outcomes exit 0.
func TestRunRebaseExitCodes(t *testing.T) {
	old := rebaseSeams
	t.Cleanup(func() { rebaseSeams = old })
	rebaseSeams = rebase.Seams{
		Show: func(id string) (bd.Bead, error) {
			if id == "mc-gone" {
				return bd.Bead{}, errors.New("issue mc-gone not found")
			}
			if id == "mc-down" {
				return bd.Bead{}, errors.New("dolt unreachable")
			}
			return bd.Bead{ID: id, Kind: contract.KindChart, Status: "open"}, nil
		},
		Repo:       func(bd.Bead) (git.Repo, string) { return "", "" },
		Strategy:   func() (string, error) { return git.LandStrategyLocal, nil },
		HasBranch:  func(git.Repo, string) bool { return true },
		Landed:     func(git.Repo, string) (bool, error) { return false, nil },
		Base:       func(git.Repo) (string, error) { return "b4se", nil },
		Head:       func(git.Repo, string) (string, error) { return "t1p", nil },
		IsAncestor: func(_ git.Repo, a, _ string) bool { return false },
		Spawn:      func(string, bd.Successor) (string, bool, error) { return "mc-new", true, nil },
	}
	for _, tc := range []struct {
		argv     []string
		code     int
		out, err string
	}{
		{[]string{"mc-c"}, 0, "filed: ", ""},
		{[]string{"mc-gone"}, 1, "", "refused"},
		{[]string{"mc-down"}, 1, "", "rebase:"},
		{nil, 1, "", "usage"},
	} {
		var o, e strings.Builder
		if code := runRebase(tc.argv, &o, &e); code != tc.code || !strings.Contains(o.String(), tc.out) || !strings.Contains(e.String(), tc.err) {
			t.Errorf("%v: code %d out %q err %q", tc.argv, code, o.String(), e.String())
		}
		if tc.argv != nil && tc.argv[0] == "mc-down" && strings.Contains(e.String(), "refused") {
			t.Errorf("infra error reported as refusal: %q", e.String())
		}
	}
	rebaseSeams.IsAncestor = func(git.Repo, string, string) bool { return true }
	var o, e strings.Builder
	if code := runRebase([]string{"mc-c"}, &o, &e); code != 0 || o.String() != "already on mainline: b4se\n" {
		t.Errorf("already on mainline: code %d out %q", code, o.String())
	}
}

// loop restart (ADR 0289 §5, ADR 0288 §5): the lock pre-states refuse or report
// without a signal; otherwise SIGUSR2 goes through restartLoop and the holder's
// events after it, scripted here, decide the outcome.
func TestRunLoopRestart(t *testing.T) {
	const rev, next = "abc1234000000000000000000000000000000000", "def5678000000000000000000000000000000000"
	origSig, origWait := restartLoop, forceWait
	t.Cleanup(func() { restartLoop, forceWait = origSig, origWait })
	pid := os.Getpid()
	setup := func(t *testing.T, st looplock.Status) (path, common string, lk *looplock.Lock) {
		common, path = t.TempDir(), filepath.Join(t.TempDir(), "mugicrew-loop.lock")
		if err := os.MkdirAll(filepath.Join(common, "mugicrew"), 0o755); err != nil {
			t.Fatal(err)
		}
		lk, err := looplock.Acquire(path, rev, "/nix/store/x-mugicrew/bin/mugicrew", "")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { lk.Close() })
		lk.Write(st)
		return path, common, lk
	}
	emit := func(common string, evs ...map[string]any) {
		f, err := os.OpenFile(filepath.Join(common, "mugicrew", "events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		for _, ev := range evs {
			b, _ := json.Marshal(ev)
			f.Write(append(b, '\n'))
		}
	}
	ev := func(pid int, msg string, kv ...any) map[string]any {
		m := map[string]any{"time": time.Now().Format(time.RFC3339Nano), "level": "INFO", "msg": msg, "pid": pid}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}

	pre := []struct {
		name string
		st   looplock.Status
		off  bool
		out  string // stdout when it succeeds
		err  string
	}{
		{name: "off", off: true, err: "loop is off; loop start runs main's latest build"},
		{name: "installed", st: looplock.Status{State: looplock.On}, err: fmt.Sprintf("loop (pid %d) is not from source; stop and start it to change builds", pid)},
		{name: "stop drain", st: looplock.Status{State: looplock.Draining, Rev: rev}, err: fmt.Sprintf("loop (pid %d) is stopping; loop start cancels the stop drain", pid)},
		{name: "restart drain", st: looplock.Status{State: looplock.Draining, Rev: rev, RestartTo: next, Inflight: []string{"a", "b"}},
			out: fmt.Sprintf("loop is already restarting onto main def5678 (pid %d, 2 in flight)\n", pid)},
	}
	for _, c := range pre {
		t.Run("pre "+c.name, func(t *testing.T) {
			st := c.st
			path, common, lk := setup(t, st)
			if c.name == "installed" { // an installed loop writes no rev
				lk.Close()
				lk, _ = looplock.Acquire(path, "", "/bin/mugicrew", "")
				t.Cleanup(func() { lk.Close() })
				lk.Write(st)
			}
			if c.off {
				lk.Close()
			}
			sent := false
			restartLoop = func(int) error { sent = true; return nil }
			var out strings.Builder
			err := runLoopRestart(path, common, &out)
			if sent || out.String() != c.out || (err == nil) != (c.err == "") || err != nil && err.Error() != c.err {
				t.Fatalf("out %q err %v signalled %v; want out %q err %q, no signal", out.String(), err, sent, c.out, c.err)
			}
		})
	}

	outcomes := []struct {
		name   string
		script func(common string) []map[string]any
		out    string
		err    string
	}{
		{"current", func(string) []map[string]any {
			return []map[string]any{ev(pid, "restart.requested", "outcome", "current")}
		}, "loop is already on main abc1234\n", ""},
		{"restart.current", func(string) []map[string]any {
			return []map[string]any{ev(pid, "restart.requested", "outcome", "building"), ev(pid, "restart.current", "rev", "def5678abcd")}
		}, "loop is already on main def5678\n", ""},
		{"stopping", func(string) []map[string]any {
			return []map[string]any{ev(pid, "restart.requested", "outcome", "stopping")}
		}, "", fmt.Sprintf("loop (pid %d) is stopping; loop start cancels the stop drain", pid)},
		{"unsourced", func(string) []map[string]any {
			return []map[string]any{ev(pid, "restart.requested", "outcome", "unsourced")}
		}, "", fmt.Sprintf("loop (pid %d) is not from source; stop and start it to change builds", pid)},
		{"build failed", func(string) []map[string]any {
			return []map[string]any{ev(pid, "restart.requested", "outcome", "building"), ev(pid, "build.started"), ev(pid, "build.failed", "rev", next, "err", "boom")}
		}, "", "build of main def5678 failed (see build.failed in events)"},
		{"drain started; other pids ignored", func(string) []map[string]any {
			return []map[string]any{ev(pid+1, "build.failed", "rev", "bad"), ev(pid, "restart.requested", "outcome", "building"), ev(pid+1, "loop.draining", "restart_to", "zzz", "inflight", 9),
				ev(pid, "build.done"), ev(pid, "loop.draining", "restart_to", next, "inflight", 2)}
		}, fmt.Sprintf("loop restarting onto main def5678 (pid %d, 2 in flight)\n", pid), ""},
		{"stop took over", func(string) []map[string]any {
			return []map[string]any{ev(pid, "restart.requested", "outcome", "building"), ev(pid, "loop.draining", "inflight", 1)}
		}, "", fmt.Sprintf("loop (pid %d) is stopping; loop start cancels the stop drain", pid)},
	}
	for _, c := range outcomes {
		t.Run(c.name, func(t *testing.T) {
			path, common, _ := setup(t, looplock.Status{State: looplock.On, Rev: rev})
			emit(common, ev(pid, "restart.requested", "outcome", "stale before the mark"))
			restartLoop = func(p int) error {
				if p != pid {
					t.Errorf("signalled %d, want %d", p, pid)
				}
				emit(common, c.script(common)...)
				return nil
			}
			var out strings.Builder
			err := runLoopRestart(path, common, &out)
			if out.String() != c.out || (err == nil) != (c.err == "") || err != nil && err.Error() != c.err {
				t.Fatalf("out %q err %v; want out %q err %q", out.String(), err, c.out, c.err)
			}
		})
	}

	t.Run("no ack is an old build", func(t *testing.T) {
		path, common, _ := setup(t, looplock.Status{State: looplock.On, Rev: rev})
		restartLoop = func(int) error { return nil }
		forceWait = 100 * time.Millisecond
		err := runLoopRestart(path, common, io.Discard)
		if want := fmt.Sprintf("loop (pid %d) did not answer the restart request (an old build); stop and start it", pid); err == nil || err.Error() != want {
			t.Fatalf("err %v; want %q", err, want)
		}
	})
	t.Run("lock released mid-wait", func(t *testing.T) {
		path, common, lk := setup(t, looplock.Status{State: looplock.On, Rev: rev})
		restartLoop = func(int) error {
			emit(common, ev(pid, "restart.requested", "outcome", "building"))
			lk.Close()
			return nil
		}
		forceWait = 50 * time.Millisecond // acked: the build wait is unbounded, only the lock ends it
		err := runLoopRestart(path, common, io.Discard)
		if err == nil || err.Error() != "loop is off; loop start runs main's latest build" {
			t.Fatalf("err %v; want the off line", err)
		}
	})
}

// End to end, loop restart (ADR 0289): an unwatched detached loop stays put on a
// commit to main until `loop restart` signals it; the verb returns with the
// drain-started line, and the loop builds, drains and execs onto the new build on
// the same pid, still unwatched: restart.requested → build.started → build.done →
// loop.draining{restart_to} → loop.restarting → loop.started restarted=true.
func TestLoopRestartVerbEndToEnd(t *testing.T) {
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
	git("worktree", "add", "-q", "-b", "side", wt)
	t.Chdir(wt)
	fakeBd(t, "", "")
	self, _ := os.Executable()
	fakeNix(t, self)
	origRoot := sourceRoot
	t.Cleanup(func() { sourceRoot = origRoot })
	sourceRoot = repo
	t.Setenv("MUGICREW_TEST_MAIN", "1")
	t.Setenv("MUGICREW_POLL_INTERVAL", "50ms")
	t.Setenv("BEADS_DIR", t.TempDir())
	path, common, err := resolveLoop()
	if err != nil {
		t.Fatal(err)
	}
	if err := runLoopStart(path, common, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	st, _ := looplock.Read(path)
	child, oldBin := st.Pid, st.StorePath
	t.Cleanup(func() {
		if t.Failed() {
			syscall.Kill(child, syscall.SIGKILL)
		}
	})
	if st.Rev != first || st.Watch {
		t.Fatalf("after start: %+v; want unwatched on rev %s", st, first)
	}

	second := commit("src", "v2")
	time.Sleep(300 * time.Millisecond) // unwatched: several ticks pass and nothing restarts
	if st, _ = looplock.Read(path); st.Rev != first {
		t.Fatalf("unwatched loop moved on its own: %+v", st)
	}
	var out strings.Builder
	if err := runLoopRestart(path, common, &out); err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("loop restarting onto main %s (pid %d, 0 in flight)\n", second[:7], child); out.String() != want {
		t.Fatalf("printed %q; want %q", out.String(), want)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if st, _ = looplock.Read(path); st.Rev == second && st.State == looplock.On {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("lock never reached rev %s: %+v", second, st)
		}
	}
	if st.Pid != child || st.StorePath == oldBin || st.Watch {
		t.Fatalf("after restart: %+v; want pid %d, a new store bin (was %s), unwatched", st, child, oldBin)
	}
	if err := runLoopStop(path, false, io.Discard); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(filepath.Join(common, "mugicrew", "events.jsonl"))
	var seq []string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var ev struct {
			Msg       string `json:"msg"`
			Pid       int    `json:"pid"`
			Restarted bool   `json:"restarted"`
		}
		if json.Unmarshal([]byte(line), &ev) != nil || ev.Pid != child {
			continue
		}
		switch ev.Msg {
		case "restart.requested", "build.started", "build.done", "loop.draining", "loop.restarting", "loop.started":
			seq = append(seq, fmt.Sprintf("%s %v", ev.Msg, ev.Restarted))
		}
	}
	want := []string{"loop.started false", "restart.requested false", "build.started false", "build.done false", "loop.draining false", "loop.restarting false", "loop.started true"}
	if i := slices.Index(seq, "loop.started true"); i >= 0 { // the test's own stop drains after it
		seq = seq[:i+1]
	}
	if !slices.Equal(seq, want) {
		t.Fatalf("events:\n%s\nwant:\n%s", strings.Join(seq, "\n"), strings.Join(want, "\n"))
	}
}

// --discard on `withdraw`, `initiative withdraw` and `initiative cancel` sets
// Opts.Discard; without it a verb that stops for a discard returns its inventory as
// the error and the CLI says nothing closed (Spec mc-5rttm D8).
func TestRunWithdrawDiscardFlag(t *testing.T) {
	oc, ow, osn := cancelInitiative, withdrawChain, initiativeSessions
	t.Cleanup(func() { cancelInitiative, withdrawChain, initiativeSessions = oc, ow, osn })
	initiativeSessions = func() (map[string]session.Info, error) { return nil, nil }
	var got []bool
	record := func(id string, o withdraw.Opts) (withdraw.Result, error) {
		got = append(got, o.Discard)
		return withdraw.Result{}, nil
	}
	cancelInitiative, withdrawChain = record, record
	for _, argv := range [][]string{{"cp"}, {"cp", "--discard"}, {"--discard", "cp"}} {
		if err := runWithdraw(argv); err != nil {
			t.Fatal(err)
		}
	}
	for _, argv := range [][]string{{"withdraw", "cp", "--discard"}, {"cancel", "in"}, {"cancel", "in", "--discard"}} {
		if err := runInitiative(argv); err != nil {
			t.Fatal(err)
		}
	}
	if want := []bool{false, true, true, true, false, true}; !slices.Equal(got, want) {
		t.Errorf("Discard = %v, want %v", got, want)
	}
	inv := withdraw.Inventory{Beads: []withdraw.BeadLeftovers{{ID: "b1", Branch: "effort/b1", Unintegrated: 2}}}
	withdrawChain = func(id string, o withdraw.Opts) (withdraw.Result, error) {
		return withdraw.Result{}, &withdraw.DiscardRequiredError{Verb: "withdraw", Target: id, Inventory: inv}
	}
	err := runWithdraw([]string{"cp"})
	if err == nil || !strings.Contains(err.Error(), "effort/b1 (2 unintegrated commits)") || !strings.Contains(err.Error(), "--discard") {
		t.Errorf("err = %v, want the inventory and the flag", err)
	}
}

// The verbs print each kept cleanup item.
func TestPrintKeptItems(t *testing.T) {
	var sb strings.Builder
	printKept(&sb, withdraw.Result{Closed: []string{"h"}, Kept: []withdraw.Item{{Kind: "tree", Bead: "b1", Path: "/tmp/x", Holder: "/home/me/wt"}}})
	if want := "kept tree /tmp/x (b1), held by /home/me/wt\n"; sb.String() != want {
		t.Errorf("output = %q, want %q", sb.String(), want)
	}
}

// A --merge-into keeps its argv position among --pivot and --critique, so "the first
// --critique after it" pairs as it does for a --pivot.
func TestSplitMergeIntoRecordsPosition(t *testing.T) {
	rest, merges, ok := splitMergeInto([]string{"mc-s", "--critique", "a", "--merge-into", "mc-p", "e.md", "--critique", "b"})
	if want := []mergeArg{{pivot: "mc-p", file: "e.md", after: 1}}; !ok || !slices.Equal(merges, want) {
		t.Fatalf("merges = %v ok=%v, want %v", merges, ok, want)
	}
	want := []string{"mc-s", "--critique", "a", "--critique", "b"}
	if !slices.Equal(rest, want) {
		t.Errorf("rest = %q, want %q", rest, want)
	}
}

// The internal position marker is gone: a hand-passed --merge-at is an unknown flag.
func TestReconcileRejectsMergeAtFlag(t *testing.T) {
	err := runReconcile([]string{"mc-s", "--checkpoint", "mc-cp", "--merge-at=1"})
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("err = %v, want the usage error", err)
	}
}
