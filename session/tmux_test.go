package session

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/claudecode"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/git"
)

// hitl-run's exit → teardown/event mapping (ADR 0037 §2–3, ADR 0029 amendment):
// rc=0 removes the worktree and records hitl.exited; a non-zero exit or a signal
// keeps it, records hitl.failed at Error, and passes the status through.
func TestHitlRunExitMapping(t *testing.T) {
	origRun, origRemove := runAgent, removeWorktree
	defer func() { runAgent, removeWorktree = origRun, origRemove }()
	stubDrafts(t, nil)
	publish = func(string, []bd.Edge, *bd.Closing) ([]string, error) { return nil, nil } // no Drafts: nothing recorded
	signalled := exec.Command("sh", "-c", "kill -HUP $$").Run()

	for _, tc := range []struct {
		name     string
		err      error
		worktree string
		rc       int
		removed  bool
		ev       string
		rmErr    error
	}{
		{"clean exit tears down", nil, "/tmp/mugicrew-wt-x/wt", 0, true, "hitl.exited", nil},
		{"clean leaf keeps repo root", nil, "", 0, false, "hitl.exited", nil},
		{"non-zero keeps worktree", exec.Command("sh", "-c", "exit 3").Run(), "/tmp/mugicrew-wt-x/wt", 3, false, "hitl.failed", nil},
		{"signal keeps worktree", signalled, "/tmp/mugicrew-wt-x/wt", 129, false, "hitl.failed", nil},
		{"failed teardown keeps the pane", nil, "/tmp/mugicrew-wt-x/wt", 1, true, "hitl.failed", errors.New("git worktree remove: busy")},
		{"never ran keeps worktree", errors.New("no such file"), "/tmp/mugicrew-wt-x/wt", 1, false, "hitl.failed", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capt := eventlogtest.Capture(t)
			var gotArgv []string
			runAgent = func(argv []string, _ <-chan struct{}) error { gotArgv = argv; return tc.err }
			removed := ""
			removeWorktree = func(wt string) error { removed = wt; return tc.rmErr }

			rc := HitlRun([]string{"--bead=grl.1", "--parent=chart1", "--crewmate=nami", "--session=sess-1",
				"--worktree=" + tc.worktree, "--", "claude", "--session-id", "sess-1"}, meter(t))
			if rc != tc.rc {
				t.Errorf("rc = %d, want %d", rc, tc.rc)
			}
			if !slices.Equal(gotArgv, []string{"claude", "--session-id", "sess-1"}) {
				t.Errorf("agent argv = %v", gotArgv)
			}
			if (removed != "") != tc.removed || (tc.removed && removed != tc.worktree) {
				t.Errorf("removed %q, want removed=%v", removed, tc.removed)
			}
			evs := *capt
			if got := eventlogtest.Names(evs); !slices.Equal(got, []string{"hitl.launched", tc.ev}) {
				t.Fatalf("events = %v, want [hitl.launched %s]", got, tc.ev)
			}
			a := evs[1].Attrs
			if a["bead"] != "grl.1" || a["parent"] != "chart1" || a["crewmate"] != "nami" || a["session"] != "sess-1" {
				t.Errorf("attrs = %v", a)
			}
			if _, ok := a["dur_ms"]; !ok {
				t.Errorf("attrs = %v, want dur_ms", a)
			}
			if tc.err == signalled && a["signal"] != "hangup" {
				t.Errorf("attrs = %v, want signal=hangup", a)
			}
			if tc.ev == "hitl.failed" && (evs[1].Level.String() != "ERROR" || a["err"] == nil) {
				t.Errorf("hitl.failed = %+v, want Error with err", evs[1])
			}
		})
	}
}

// meter is the claudecode Session cost meter over an empty temp CLAUDE_CONFIG_DIR,
// so a run finds no transcript unless the test's agent writes one there.
func meter(t *testing.T) func(string) func() contract.Cost {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	return claudecode.MeterCost
}

// hitl.exited / hitl.failed carry the Session cost of the transcript's cost-state
// the agent wrote, top-level like the headless end events (Spec FR10);
// with no transcript they carry none, and the exit mapping is unchanged.
func TestHitlRunRecordsCost(t *testing.T) {
	origRun, origRemove := runAgent, removeWorktree
	defer func() { runAgent, removeWorktree = origRun, origRemove }()
	stubDrafts(t, nil)
	removeWorktree = func(string) error { return nil }
	for _, tc := range []struct {
		name  string
		write bool
		exit  string
		ev    string
	}{
		{"clean exit", true, "exit 0", "hitl.exited"},
		{"failed exit", true, "exit 3", "hitl.failed"},
		{"no transcript", false, "exit 0", "hitl.exited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capt := eventlogtest.Capture(t)
			m := meter(t)
			dir := os.Getenv("CLAUDE_CONFIG_DIR") + "/projects/-wt"
			runAgent = func([]string, <-chan struct{}) error {
				if tc.write {
					os.MkdirAll(dir, 0o755)
					os.WriteFile(dir+"/sess-1.jsonl", []byte(`{"type":"cost-state","totalCostUSD":1.5,"totalAPIDuration":3000,"modelUsage":{"m1":{"inputTokens":1,"outputTokens":2,"cacheReadInputTokens":3,"cacheCreationInputTokens":4,"costUSD":1.5}},"hasUnknownModelCost":true}`+"\n"), 0o644)
				}
				return exec.Command("sh", "-c", tc.exit).Run()
			}
			HitlRun([]string{"--bead=g", "--session=sess-1", "--worktree=/tmp/mugicrew-wt-x/wt", "--", "claude"}, m)
			evs := *capt
			end := evs[len(evs)-1]
			if end.Name != tc.ev {
				t.Fatalf("events = %v, want %s last", eventlogtest.Names(evs), tc.ev)
			}
			a := end.Attrs
			if !tc.write {
				for _, k := range []string{"cost_usd", "api_ms", "models", "cost_unpriced"} {
					if _, ok := a[k]; ok {
						t.Errorf("%s = %v, want absent without a transcript", k, a[k])
					}
				}
				return
			}
			if a["cost_usd"] != 1.5 || a["api_ms"] != 3000 || a["cost_unpriced"] != true || a["models"] == nil {
				t.Errorf("attrs = %v, want the transcript's cost top-level", a)
			}
		})
	}
}

// stubDrafts fakes bd's Publish / DeleteDrafts, each returning the Draft d1 and
// err, and records the calls in order ("publish <creator>", "delete <creator>";
// a handed close as "publish <creator> close <id>"). The session's bead reads as
// a grilling bead (no close). It shortens hitl-run's hangup grace and records the
// Closing mark ("closing <socket> <pane>") and the viewer return ("return <socket>
// <pane>") instead of touching tmux.
func stubDrafts(t *testing.T, err error) *[]string {
	origPub, origDel, origGrace, origRet := publish, deleteDrafts, hupGrace, returnViewers
	origShow, origSucc, origMark := showBead, successorOf, markClosing
	t.Cleanup(func() {
		publish, deleteDrafts, hupGrace, returnViewers = origPub, origDel, origGrace, origRet
		showBead, successorOf, markClosing = origShow, origSucc, origMark
	})
	hupGrace = 50 * time.Millisecond
	calls := &[]string{}
	markClosing = func(tmuxEnv, pane string) { *calls = append(*calls, "closing "+tmuxEnv+" "+pane) }
	returnViewers = func(tmuxEnv, pane string) { *calls = append(*calls, "return "+tmuxEnv+" "+pane) }
	showBead = func(id string) (bd.Bead, error) { return bd.Bead{ID: id, Kind: contract.KindGrilling}, nil }
	successorOf = func(string, contract.Kind) (string, bool, error) { return "", false, nil }
	publish = func(creator string, edges []bd.Edge, closing *bd.Closing) ([]string, error) {
		call := "publish " + creator
		if closing != nil {
			call += " close " + closing.ID
		}
		*calls = append(*calls, call)
		if edges != nil {
			t.Errorf("publish(%s) handed edges %v; the carry stays outside the batch", creator, edges)
		}
		return []string{"d1"}, err
	}
	deleteDrafts = func(creator string) ([]string, []bd.BranchOutcome, error) {
		*calls = append(*calls, "delete "+creator)
		return []string{"d1"}, nil, err
	}
	return calls
}

// A clean hitl-run exit marks its session Closing, returns its viewers to their
// TUIs on the pane's server (ADR 0108 §1–2), then publishes the bead's Drafts and
// removes the tree (ADR 0059 §4); a non-zero exit or a drop's hangup marks,
// publishes and returns nothing; a failed publish fails the session, keeping the
// tree and Drafts for resume or drop, and its @closing (never unset) tells that
// dead pane from a crash.
func TestHitlRunPublishesDrafts(t *testing.T) {
	origRun, origRemove := runAgent, removeWorktree
	defer func() { runAgent, removeWorktree = origRun, origRemove }()
	for _, tc := range []struct {
		name   string
		agent  func([]string, <-chan struct{}) error
		pubErr error
		rc     int
		steps  []string
		events []string
	}{
		{"clean exit publishes then removes", func([]string, <-chan struct{}) error { return nil }, nil, 0,
			[]string{"closing /s,1,2 %7", "return /s,1,2 %7", "publish grl.1", "remove"}, []string{"hitl.launched", "draft.published", "hitl.exited"}},
		{"non-zero keeps Drafts", func([]string, <-chan struct{}) error { return exec.Command("sh", "-c", "exit 3").Run() }, nil, 3,
			nil, []string{"hitl.launched", "hitl.failed"}},
		{"failed publish keeps the tree", func([]string, <-chan struct{}) error { return nil }, errors.New("bd batch: locked"), 1,
			[]string{"closing /s,1,2 %7", "return /s,1,2 %7", "publish grl.1"}, []string{"hitl.launched", "draft.failed", "hitl.failed"}},
		{"hangup (drop) publishes nothing", func([]string, <-chan struct{}) error { return syscall.Kill(os.Getpid(), syscall.SIGHUP) }, nil, 0,
			[]string{"remove"}, []string{"hitl.launched", "hitl.exited"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capt := eventlogtest.Capture(t)
			steps := stubDrafts(t, tc.pubErr)
			t.Setenv("TMUX", "/s,1,2")
			t.Setenv("TMUX_PANE", "%7")
			runAgent = tc.agent
			removeWorktree = func(string) error { *steps = append(*steps, "remove"); return nil }
			if rc := HitlRun([]string{"--bead=grl.1", "--session=s", "--worktree=/tmp/mugicrew-wt-x/wt", "--", "claude"}, meter(t)); rc != tc.rc {
				t.Errorf("rc = %d, want %d", rc, tc.rc)
			}
			if !slices.Equal(*steps, tc.steps) {
				t.Errorf("steps = %q, want %q", *steps, tc.steps)
			}
			evs := *capt
			if got := eventlogtest.Names(evs); !slices.Equal(got, tc.events) {
				t.Fatalf("events = %v, want %v", got, tc.events)
			}
			if last := evs[len(evs)-1]; (last.Attrs["dropped"] == true) != strings.HasPrefix(tc.name, "hangup") {
				t.Errorf("%s attrs = %v: only a drop carries dropped=true", last.Name, last.Attrs)
			}
			if a := evs[1].Attrs; len(evs) == 3 && (a["creator"] != "grl.1" || a["bead"] != "grl.1" || a["session"] != "s") {
				t.Errorf("%s attrs = %v, want creator and the session's attrs", evs[1].Name, a)
			}
		})
	}
}

// A clean exit closes the session's Signal in its publish batch only when the
// graph shows a caused-by Chart or Spec successor (ADR 0096; Spec D2).
// Any other bead, or a Signal with none, gets no close; a failed graph read fails
// the publish, keeping the session for a resume or drop.
func TestHitlRunClosesChartedSignalOnPublish(t *testing.T) {
	origRun, origRemove := runAgent, removeWorktree
	defer func() { runAgent, removeWorktree = origRun, origRemove }()
	for _, tc := range []struct {
		name    string
		kind    contract.Kind
		succ    contract.Kind // the caused-by successor's kind; "" for none
		readErr error
		rc      int
		steps   []string
		events  []string
	}{
		{"Signal with a Chart closes", contract.KindSignal, contract.KindChart, nil, 0,
			[]string{"closing /s,1,2 %7", "return /s,1,2 %7", "publish sig close sig", "remove"}, []string{"hitl.launched", "draft.published", "hitl.exited"}},
		{"Signal with a Spec closes", contract.KindSignal, contract.KindSpec, nil, 0,
			[]string{"closing /s,1,2 %7", "return /s,1,2 %7", "publish sig close sig", "remove"}, []string{"hitl.launched", "draft.published", "hitl.exited"}},
		{"undecided Signal closes nothing", contract.KindSignal, "", nil, 0,
			[]string{"closing /s,1,2 %7", "return /s,1,2 %7", "publish sig", "remove"}, []string{"hitl.launched", "draft.published", "hitl.exited"}},
		{"grilling bead closes nothing", contract.KindGrilling, contract.KindChart, nil, 0,
			[]string{"closing /s,1,2 %7", "return /s,1,2 %7", "publish sig", "remove"}, []string{"hitl.launched", "draft.published", "hitl.exited"}},
		{"failed lookup keeps the session", contract.KindSignal, contract.KindChart, errors.New("bd: locked"), 1,
			[]string{"closing /s,1,2 %7", "return /s,1,2 %7"}, []string{"hitl.launched", "draft.failed", "hitl.failed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capt := eventlogtest.Capture(t)
			steps := stubDrafts(t, nil)
			t.Setenv("TMUX", "/s,1,2")
			t.Setenv("TMUX_PANE", "%7")
			showBead = func(id string) (bd.Bead, error) { return bd.Bead{ID: id, Kind: tc.kind}, nil }
			successorOf = func(id string, want contract.Kind) (string, bool, error) {
				if tc.readErr != nil {
					return "", false, tc.readErr
				}
				return id + ".succ", want == tc.succ, nil
			}
			runAgent = func([]string, <-chan struct{}) error { return nil }
			removeWorktree = func(string) error { *steps = append(*steps, "remove"); return nil }
			if rc := HitlRun([]string{"--bead=sig", "--session=s", "--worktree=/tmp/mugicrew-wt-x/wt", "--", "claude"}, meter(t)); rc != tc.rc {
				t.Errorf("rc = %d, want %d", rc, tc.rc)
			}
			if !slices.Equal(*steps, tc.steps) {
				t.Errorf("steps = %q, want %q", *steps, tc.steps)
			}
			if got := eventlogtest.Names(*capt); !slices.Equal(got, tc.events) {
				t.Errorf("events = %v, want %v", got, tc.events)
			}
		})
	}
}

// hitl-run over a real stub agent (sh -c 'exit N'), not a faked runAgent: the
// exec path runs the argv after -- and the wrapper's own exit code is the agent's.
func TestHitlRunRealAgent(t *testing.T) {
	origRemove := removeWorktree
	defer func() { removeWorktree = origRemove }()
	stubDrafts(t, nil)
	for _, tc := range []struct {
		script  string
		rc      int
		removed bool
	}{
		{"exit 0", 0, true},
		{"exit 3", 3, false},
		{"kill -TERM $$", 128 + 15, false},
	} {
		t.Run(tc.script, func(t *testing.T) {
			eventlogtest.Capture(t)
			removed := false
			removeWorktree = func(string) error { removed = true; return nil }
			if rc := HitlRun([]string{"--bead=g", "--worktree=/tmp/mugicrew-wt-x/wt", "--", "sh", "-c", tc.script}, meter(t)); rc != tc.rc {
				t.Errorf("rc = %d, want %d", rc, tc.rc)
			}
			if removed != tc.removed {
				t.Errorf("removed = %v, want %v", removed, tc.removed)
			}
		})
	}
}

// The tmux argv the session package builds (ADR 0036–0038): server setup before
// new-session so a fast-dying pane is still kept, the exact-match target, the
// three @ tags, the HITL bar and key table, and the wrapper by its
// absolute path. A trailing ; in an argv word is escaped so tmux doesn't split.
func TestStartArgs(t *testing.T) {
	t1 := sessionTags{bd.Bead{ID: "mc-a.2", Parent: "mc-a", Kind: contract.KindGrilling, Title: "Grill #1"}, "s1", "/wt"}
	args := launchArgs(tmuxName(t1.bead.ID), "/wt", "/run/env;", paneArgs("/abs/mugicrew", t1, []string{"claude", "end;"}), t1)
	joined := strings.Join(args, " ")
	assertEnvArg(t, args)
	// The tags follow new-session in the same tmux command.
	if want := strings.Join(preamble(), " ") + " ; new-session -d -s mc-a_2 -c /wt -e MUGICREW_ENV_FILE=/run/env\\; -- /abs/mugicrew hitl-run --bead=mc-a.2 --parent=mc-a --crewmate=nami --session=s1 --worktree=/wt -- claude end\\; ; set-option -t =mc-a_2: @bead mc-a.2"; !strings.HasPrefix(joined, want) {
		t.Errorf("launch argv =\n%s\nwant prefix\n%s", joined, want)
	}

	for _, want := range []string{
		"set-option -t =mc-a_2: @bead mc-a.2",
		"set-option -t =mc-a_2: @session s1",
		"set-option -t =mc-a_2: @worktree /wt",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q:\n%s", want, joined)
		}
	}
	assertBar(t, args, "mc-a.2 · Grill ##1")
	assertUnmarks(t, args)
	// -c is a tmux format too: a # in the cwd is escaped, in launch and respawn alike.
	for _, a := range [][]string{launchArgs("s", "/w#t", "/e", nil, t1), respawnArgs("s", "/w#t", "/e", nil, t1.bead)} {
		if i := slices.Index(a, "-c"); i < 0 || a[i+1] != "/w##t" {
			t.Errorf("-c not escaped in %v", a)
		}
	}
}

// Both a HITL Start and the TUI self-launch open with the one server preamble,
// since either may start the server (ADR 0065 §4). A TUI session gets status off
// and its window's remain-on-exit off, and no @bead or key-table (§3); its pane
// runs mugicrew with stderr on tui.out and the env handoff for that process only.
func TestTUIArgs(t *testing.T) {
	pre := strings.Join(preamble(), " ")
	if !strings.HasPrefix(pre, "start-server ; set-option -gw remain-on-exit failed ; ") {
		t.Errorf("preamble = %q, want server start and remain-on-exit failed", pre)
	}
	// F12 is bound only in the HITL key table, never in root (ADR 0065 §5).
	if strings.Contains(pre, "-n F12") || strings.Contains(pre, "-T root") || !strings.Contains(pre, "bind-key -T mchitl F12 ") {
		t.Errorf("preamble binds F12 outside the HITL table: %q", pre)
	}
	// F11 cancels an Auto-exit tmux-side (Spec D5): it unsets the pressing
	// client's session's @autofinish, in the HITL table only.
	if !strings.Contains(pre, " ; bind-key -T mchitl F11 set-option -u @autofinish") || strings.Contains(pre, "-n F11") {
		t.Errorf("preamble lacks the HITL table's F11 cancel: %q", pre)
	}
	t1 := sessionTags{bd.Bead{ID: "g"}, "s", ""}
	tui := strings.Join(tuiArgs("tui-7", "/r#", tuiPane("/abs/mugicrew", "/c/tui.out", "/run/env")), " ")
	for _, a := range []string{strings.Join(launchArgs("g", "/r", "/e", nil, t1), " "), tui} {
		if !strings.HasPrefix(a, pre+" ; new-session -d -s ") {
			t.Errorf("argv does not open with the preamble:\n%s", a)
		}
	}
	want := pre + ` ; new-session -d -s tui-7 -c /r## -- sh -c o=$1 f=$2; shift 2; MUGICREW_ENV_FILE="$f" exec "$0" "$@" 2>>"$o" /abs/mugicrew /c/tui.out /run/env` +
		" ; set-option -t =tui-7: status off ; set-option -w -t =tui-7: remain-on-exit off"
	if tui != want {
		t.Errorf("tui argv =\n%s\nwant\n%s", tui, want)
	}
}

// The pane's env handoff runs the pane's program through sh exactly as tuiPane
// says: stderr appended to out, MUGICREW_ENV_FILE set for it alone, and the
// launcher's args (a TUI's --watch) passed on.
func TestTUIPaneRuns(t *testing.T) {
	dir := t.TempDir()
	exe, out := filepath.Join(dir, "exe"), filepath.Join(dir, "tui.out")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\necho \"env=$MUGICREW_ENV_FILE args=$*\" >&2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(out, []byte("kept\n"), 0o644)
	p := tuiPane(exe, out, "/run/env", "--watch")
	if err := exec.Command(p[0], p[1:]...).Run(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out); string(b) != "kept\nenv=/run/env args=--watch\n" {
		t.Errorf("tui.out = %q", b)
	}
}

// Already inside is $TMUX's socket field equal to the private socket exactly
// (ADR 0065 §1): the user's own tmux, a longer path, or no $TMUX is outside.
func TestInsideOf(t *testing.T) {
	sock := "/r/.git/mugicrew/tmux.sock"
	for env, want := range map[string]bool{
		sock + ",4242,0":                true,
		"/tmp/tmux-1000/default,4242,0": false,
		sock + "2,4242,0":               false,
		"":                              false,
	} {
		if got := insideOf(env, sock); got != want {
			t.Errorf("insideOf(%q) = %v, want %v", env, got, want)
		}
	}
}

// The TUI pane keeps its own TMUX/TMUX_PANE/TERM over the launcher's env.
func TestApplyTUIEnv(t *testing.T) {
	saved := os.Environ()
	t.Cleanup(func() {
		os.Clearenv()
		for _, kv := range saved {
			k, v, _ := strings.Cut(kv, "=")
			os.Setenv(k, v)
		}
	})
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("FOO", "fresh")
	os.Unsetenv("TMUX")
	path, err := writeEnvFile()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FOO", "stale")
	t.Setenv("TMUX", "/s,1,0")
	t.Setenv("TMUX_PANE", "%1")
	t.Setenv(envFileVar, path)
	if err := ApplyTUIEnv(); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"FOO": "fresh", "TMUX": "/s,1,0", "TMUX_PANE": "%1", envFileVar: ""} {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

// assertEnvArg: a launch or resume argv passes exactly one -e, the env file
// (FR5), and no TMUX/TMUX_PANE anywhere.
func assertEnvArg(t *testing.T, args []string) {
	t.Helper()
	i := slices.Index(args, "-e")
	if i < 0 || slices.Index(args[i+1:], "-e") >= 0 || !strings.HasPrefix(args[i+1], envFileVar+"=") {
		t.Errorf("want exactly one -e %s=..., got %v", envFileVar, args)
	}
	if j := strings.Join(args, " "); strings.Contains(j, "TMUX") {
		t.Errorf("TMUX leaked into %s", j)
	}
}

// writeEnvFile hands the launcher's env (minus TMUX/TMUX_PANE) to the pane's
// hitl-run through a 0600 file under $XDG_RUNTIME_DIR; hitl-run applies it over
// the stale server env (keeping tmux's TERM) and deletes it, and a missing file
// fails the session instead of falling through (FR5).
func TestEnvFileHandoff(t *testing.T) {
	saved := os.Environ()
	t.Cleanup(func() {
		os.Clearenv()
		for _, kv := range saved {
			k, v, _ := strings.Cut(kv, "=")
			os.Setenv(k, v)
		}
	})
	origRun := runAgent
	defer func() { runAgent = origRun }()
	stubDrafts(t, nil)
	publish = func(string, []bd.Edge, *bd.Closing) ([]string, error) { return nil, nil } // no Drafts: nothing recorded
	capt := eventlogtest.Capture(t)
	os.Setenv("TMUX_PANE", "") // no viewers to return on the test's own tmux; Cleanup restores

	run := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", run)
	t.Setenv("TMUX", "/tmp/user-tmux,1,0")
	t.Setenv("TMUX_PANE", "%3")
	t.Setenv("FOO", "fresh\nline2")
	t.Setenv("TERM", "xterm-256color")
	path, err := writeEnvFile()
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 || filepath.Dir(path) != run {
		t.Fatalf("env file %s: %v %v, want 0600 under %s", path, fi, err, run)
	}
	if b, _ := os.ReadFile(path); strings.Contains(string(b), "TMUX") {
		t.Errorf("env file carries TMUX: %q", b)
	}

	// The pane: the server's stale env, tmux's TERM, the file's path.
	t.Setenv("FOO", "stale")
	t.Setenv("TERM", "tmux-256color")
	t.Setenv(envFileVar, path)
	got := map[string]string{}
	runAgent = func([]string, <-chan struct{}) error {
		for _, k := range []string{"FOO", "TERM", "XDG_RUNTIME_DIR", "TMUX", envFileVar} {
			if v, ok := os.LookupEnv(k); ok {
				got[k] = v
			}
		}
		return nil
	}
	if rc := HitlRun([]string{"--bead=g", "--", "claude"}, meter(t)); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if want := map[string]string{"FOO": "fresh\nline2", "TERM": "tmux-256color", "XDG_RUNTIME_DIR": run}; !maps.Equal(got, want) {
		t.Errorf("agent env = %v, want %v", got, want)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("env file not deleted: %v", err)
	}

	// The file is gone now: a second run must refuse, never run the agent.
	os.Setenv(envFileVar, path)
	*capt = nil
	ran := false
	runAgent = func([]string, <-chan struct{}) error { ran = true; return nil }
	if rc := HitlRun([]string{"--bead=g", "--", "claude"}, meter(t)); rc == 0 || ran {
		t.Errorf("missing env file: rc = %d, ran = %v; want a refusal", rc, ran)
	}
	if got := eventlogtest.Names(*capt); !slices.Equal(got, []string{"hitl.launched", "hitl.failed"}) {
		t.Errorf("events = %v, want [hitl.launched hitl.failed]", got)
	}
}

// Every tmux call drops TMUX and TMUX_PANE so it nests inside the user's tmux
// (ADR 0042 §4).
func TestTmuxCmdStripsTMUX(t *testing.T) {
	t.Setenv("TMUX", "/tmp/user-tmux,1,0")
	t.Setenv("TMUX_PANE", "%3")
	cmd := tmuxCmd("/sock", "ls")
	if slices.ContainsFunc(cmd.Env, func(kv string) bool {
		return strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_PANE=")
	}) {
		t.Error("TMUX and TMUX_PANE must be stripped from tmux's env")
	}
	if !slices.Equal(cmd.Args, []string{"tmux", "-S", "/sock", "ls"}) {
		t.Errorf("args = %v", cmd.Args)
	}
}

// stubServer points the session package at a fake tmux server: tmux found, and a
// socket under a temp dir instead of the repo's common dir, so tests run anywhere.
func stubServer(t *testing.T) string {
	origLook, origSock := lookTmux, socket
	t.Cleanup(func() { lookTmux, socket = origLook, origSock })
	sock := filepath.Join(t.TempDir(), "tmux.sock")
	lookTmux = func() error { return nil }
	socket = func() (string, error) { return sock, nil }
	return sock
}

// A socket path over the unix limit is refused with a clear error before any
// directory is made (ADR 0036 §2).
func TestSocketTooLong(t *testing.T) {
	common := filepath.Join(t.TempDir(), strings.Repeat("x", maxSock))
	if _, err := socketIn(common); err == nil || !strings.Contains(err.Error(), "unix socket limit") {
		t.Errorf("err = %v, want the socket-length refusal", err)
	}
	if _, err := os.Stat(common); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a refused socket must make no dir: %v", err)
	}
	sock, err := socketIn(t.TempDir())
	if err != nil || filepath.Base(sock) != "tmux.sock" {
		t.Errorf("short path: sock=%q err=%v", sock, err)
	}
	if _, err := os.Stat(filepath.Dir(sock)); err != nil {
		t.Errorf("socket dir not made: %v", err)
	}
}

// The switch names its client explicitly with -c and its session by an exact =
// target (ADR 0065 §1); the client is resolved from the TUI's own pane. SwitchTo
// refuses without tmux.
func TestSwitchTo(t *testing.T) {
	stubServer(t)
	if got, want := switchArgs("/dev/pts/7", "mc-a_2"), []string{"switch-client", "-c", "/dev/pts/7", "-t", "=mc-a_2"}; !slices.Equal(got, want) {
		t.Errorf("switch argv = %v, want %v", got, want)
	}
	if got, want := clientArgs("%5"), []string{"list-clients", "-t", "%5", "-F", "#{client_tty}"}; !slices.Equal(got, want) {
		t.Errorf("client argv = %v, want %v", got, want)
	}
	lookTmux = func() error { return exec.ErrNotFound }
	if err := SwitchTo("g"); !errors.Is(err, ErrNoTmux) {
		t.Errorf("err = %v, want ErrNoTmux", err)
	}
}

// Sessions end to end over a fake tmux on PATH: a "no server running" stderr is an
// empty table and no error; any other failure is Sessions' error (ADR 0036 §5).
func TestSessionsNoServer(t *testing.T) {
	stubServer(t)
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	fake := func(stderr string) {
		script := "#!/bin/sh\necho '" + stderr + "' >&2\nexit 1\n"
		if err := os.WriteFile(filepath.Join(bin, "tmux"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fake("no server running on /repo/.git/mugicrew/tmux.sock")
	if m, err := Sessions(); err != nil || len(m) != 0 {
		t.Errorf("no server: sessions=%v err=%v, want empty and nil", m, err)
	}
	fake("open terminal failed")
	if _, err := Sessions(); err == nil {
		t.Error("a failed read must be an error, not an empty table")
	}
	// An unreadable socket may front a live server: its holder must not read as an orphan.
	fake("error connecting to /repo/.git/mugicrew/tmux.sock (Permission denied)")
	if _, err := Sessions(); err == nil {
		t.Error("an unreadable socket must be an error, not an empty table")
	}
}

// With tmux off PATH, Start refuses before placing anything (ADR 0036 §6).
func TestStartRefusesWithoutTmux(t *testing.T) {
	orig := lookTmux
	defer func() { lookTmux = orig }()
	lookTmux = func() error { return exec.ErrNotFound }
	if err := Start(bd.Bead{ID: "g"}, contract.AgentCommand{Argv: []string{"claude"}}); !errors.Is(err, ErrNoTmux) {
		t.Errorf("err = %v, want ErrNoTmux", err)
	}
}

// A fresh launch deletes the bead's leftover Drafts before tmux runs (ADR 0059
// §6): gone reads as never launched, and a repo-root leaf (a Signal triage) is
// never an orphan to drop. A failed delete launches nothing.
func TestStartDeletesGoneSessionDrafts(t *testing.T) {
	stubServer(t)
	run := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", run)
	calls := stubDrafts(t, errors.New("bd: locked"))
	origRoot := git.RepoRoot
	defer func() { git.RepoRoot = origRoot }()
	git.RepoRoot = func() (string, error) { return "/repo/root", nil } // a Signal leaf runs from the root; no checkout needed
	capt := eventlogtest.Capture(t)
	err := Start(bd.Bead{ID: "ws-sig", Kind: contract.KindSignal}, contract.AgentCommand{Argv: []string{"claude"}})
	if err == nil || err.Error() != "bd: locked" {
		t.Fatalf("err = %v, want the delete's error, before any tmux", err)
	}
	if !slices.Equal(*calls, []string{"delete ws-sig"}) {
		t.Errorf("calls = %q, want [delete ws-sig]", *calls)
	}
	if got := eventlogtest.Names(*capt); !slices.Equal(got, []string{"draft.failed"}) {
		t.Errorf("events = %v, want [draft.failed]", got)
	}
	if left, _ := os.ReadDir(run); len(left) != 0 {
		t.Errorf("env handoff left behind: %v", left)
	}
}

// parseList reads list-sessions -F listFormat rows: a live pane is alive, a dead
// one carries its exit status or the signal that killed it (the pane, or the agent
// under hitl-run, which exits 128+n for it), and an untagged
// session is skipped (ADR 0036 §1).
func TestParseList(t *testing.T) {
	out := "ws-1.2\tsid-a\t/wt/a\t0\t\t\t\t\t\n" +
		"ws-3\tsid-b\t\t1\t2\t\t\t\t\n" +
		"ws-4\tsid-c\t\t1\t\t9\t\t\t\n" +
		"ws-5\tsid-d\t\t1\t143\t\t\t\t\n" +
		"ws-6\tsid-e\t\t1\t255\t\t\t\t\n" +
		"ws-7\tsid-f\t\t0\t\t\tturn\t\t\n" +
		"ws-8\tsid-g\t\t0\t\t\task\t\t\n" +
		"ws-9\tsid-h\t\t0\t\t\tbogus\t\t\n" +
		"ws-10\tsid-i\t\t0\t\t\tturn\t1\t\n" +
		"ws-11\tsid-j\t\t1\t1\t\t\t1\t\n" +
		"ws-12\tsid-k\t\t0\t\t\tturn\t\t1760000000\n" +
		"\t\t\t0\t\t\t\t\t\n"
	got := parseList(out)
	want := map[string]Info{
		"ws-1.2": {Session: "sid-a", Worktree: "/wt/a", State: Alive},
		"ws-3":   {Session: "sid-b", State: Exited, RC: 2},
		"ws-4":   {Session: "sid-c", State: Exited, Signal: "killed"},              // SIGKILL
		"ws-5":   {Session: "sid-d", State: Exited, RC: 143, Signal: "terminated"}, // hitl-run's 128+SIGTERM
		"ws-6":   {Session: "sid-e", State: Exited, RC: 255},                       // past any signal: a plain rc
		// The waiting bit (ADR 0099 §3): turn and ask read as such; empty (unset, an
		// older mugicrew's session) and an unknown value read as not waiting.
		"ws-7": {Session: "sid-f", State: Alive, Waiting: WaitTurn},
		"ws-8": {Session: "sid-g", State: Alive, Waiting: WaitAsk},
		"ws-9": {Session: "sid-h", State: Alive},
		// Closing (ADR 0108 §2): a live pane with @closing set; a dead one is exited,
		// whatever @closing says (a failed close).
		"ws-10": {Session: "sid-i", State: Alive, Waiting: WaitTurn, Closing: true},
		"ws-11": {Session: "sid-j", State: Exited, RC: 1},
		// Auto-exit (ADR 0111): @autofinish's unix-seconds deadline; empty is disarmed.
		"ws-12": {Session: "sid-k", State: Alive, Waiting: WaitTurn, AutoExit: time.Unix(1760000000, 0)},
	}
	if !maps.Equal(got, want) {
		t.Errorf("parseList = %+v, want %+v", got, want)
	}
	// A live TUI session carries no @bead: not listed, not counted (ADR 0065 §3).
	if n := Live(parseList(out + "\t\t\t0\t\t\t\t\t\n")); n != 6 {
		t.Errorf("Live = %d, want 6: an untagged session must not count", n)
	}
	if len(parseList("")) != 0 {
		t.Error("empty output (no server) must list nothing")
	}
}

// markClosing against real tmux on a throwaway -f /dev/null server: the pane's
// session reads Closing through Sessions(), and a fresh or resumed pane's unmark
// clears it (ADR 0108 §2).
func TestMarkClosingRealTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	sock := filepath.Join(t.TempDir(), "s.sock")
	origSock := socket
	socket = func() (string, error) { return sock, nil }
	t.Cleanup(func() { socket = origSock; tmuxCmd(sock, "kill-server").Run() })
	pane, err := tmuxCmd(sock, "-f", "/dev/null", "new-session", "-d", "-s", "mc-c", "-P", "-F", "#{pane_id}",
		";", "set-option", "-t", "=mc-c:", "@bead", "mc-c").CombinedOutput()
	if err != nil {
		t.Fatalf("tmux new-session: %v: %s", err, pane)
	}
	closing := func() bool {
		s, err := Sessions()
		if err != nil {
			t.Fatal(err)
		}
		return s["mc-c"].Closing
	}
	markClosing(sock+",1,0", strings.TrimSpace(string(pane)))
	if !closing() {
		t.Fatal("marked session does not read Closing")
	}
	if err := runTmux(sock, unmarkArgs("mc-c")[1:]); err != nil {
		t.Fatal(err)
	}
	if closing() {
		t.Error("unmarked session still reads Closing")
	}
}

// A resume (ADR 0037 §4, ADR 0039 §1): hitl-run started with --resume records
// hitl.resumed instead of hitl.launched, then ends as at launch; the tmux argv
// respawns the exited session's dead pane (no -k, so a live one is refused) in
// the kept worktree, running the wrapper in resume mode.
func TestResume(t *testing.T) {
	origRun, origRemove := runAgent, removeWorktree
	defer func() { runAgent, removeWorktree = origRun, origRemove }()
	runAgent = func([]string, <-chan struct{}) error { return nil }
	removed := ""
	removeWorktree = func(wt string) error { removed = wt; return nil }
	stubDrafts(t, nil)
	publish = func(string, []bd.Edge, *bd.Closing) ([]string, error) { return nil, nil }
	t.Setenv("TMUX_PANE", "") // no viewers to return on the test's own tmux
	capt := eventlogtest.Capture(t)
	if rc := HitlRun([]string{"--resume", "--bead=g", "--session=s1", "--worktree=/wt", "--", "claude", "--resume", "s1"}, meter(t)); rc != 0 {
		t.Errorf("rc = %d", rc)
	}
	if got := eventlogtest.Names(*capt); !slices.Equal(got, []string{"hitl.resumed", "hitl.exited"}) {
		t.Errorf("events = %v, want [hitl.resumed hitl.exited]", got)
	}
	if (*capt)[0].Attrs["session"] != "s1" || removed != "/wt" {
		t.Errorf("resumed attrs %v, removed %q", (*capt)[0].Attrs, removed)
	}

	pane := resumePane(paneArgs("/abs/mugicrew", sessionTags{bd.Bead{ID: "g"}, "s1", "/wt"}, []string{"claude", "--resume", "s1"}))
	args := respawnArgs(tmuxName("mc-a.2"), "/wt", "/run/env", pane, bd.Bead{ID: "mc-a.2", Title: "Grill #1"})
	assertEnvArg(t, args)
	got := strings.Join(args, " ")
	if want := "respawn-pane -t =mc-a_2: -c /wt -e MUGICREW_ENV_FILE=/run/env -- /abs/mugicrew hitl-run --resume --bead=g --parent= --crewmate= --session=s1 --worktree=/wt -- claude --resume s1 ; "; !strings.HasPrefix(got, want) {
		t.Errorf("respawn argv =\n%s\nwant prefix\n%s", got, want)
	}
	// A resumed session gets the same bar and key table as a launch (ADR 0066 §3).
	assertBar(t, args, "mc-a.2 · Grill ##1")
	assertUnmarks(t, args) // a stale bit doesn't follow it into the new pane (ADR 0099 §6, ADR 0108 §2)
	if strings.Contains(got, "@session") {
		t.Errorf("respawn re-tags @session: %s", got)
	}
}

// A harness that can't resume (transcript pruned) spawns nothing: its error is
// Resume's, before any tmux call (ADR 0039 §2–3).
func TestResumeHarnessErrorSpawnsNothing(t *testing.T) {
	stubServer(t)
	gone := errors.New("transcript gone")
	err := Resume(bd.Bead{ID: "g"}, Info{Session: "s1", Worktree: "/nonexistent"}, failResume{gone})
	if !errors.Is(err, gone) {
		t.Errorf("err = %v, want the harness error", err)
	}
}

// A kept worktree that is gone is Resume's error, pointing at x, before any tmux
// call: tmux would otherwise fall back to $HOME and resume the agent there.
func TestResumeMissingWorktree(t *testing.T) {
	stubServer(t)
	err := Resume(bd.Bead{ID: "g"}, Info{Session: "s1", Worktree: filepath.Join(t.TempDir(), "gone")}, failResume{})
	if err == nil || !strings.Contains(err.Error(), "is gone") {
		t.Errorf("err = %v, want the missing-worktree refusal", err)
	}
}

type failResume struct{ err error }

func (failResume) Run(contract.Ctx, contract.Invocation) (contract.Result, error) {
	return contract.Result{}, nil
}
func (failResume) LaunchCommand(contract.Invocation) (contract.LaunchCommand, error) { return nil, nil }
func (f failResume) ResumeCommand(string) (contract.AgentCommand, error) {
	return contract.AgentCommand{}, f.err
}

// Drop (ADR 0037 §5): kill-session on the mugicrew server first, then a forced
// remove of @worktree — never the tree under a still-running agent. A failed kill
// removes nothing; a repo-root leaf ("" worktree) only kills.
func TestDropOrder(t *testing.T) {
	origKill, origRemove := killSession, gitRemoveWorktree
	defer func() { killSession, gitRemoveWorktree = origKill, origRemove }()
	stubServer(t)
	var steps []string
	var killErr error
	killSession = func(_ string, args []string) error {
		steps = append(steps, strings.Join(args, " "))
		return killErr
	}
	gitRemoveWorktree = func(wt string) error { steps = append(steps, "remove "+wt); return nil }
	var delErr error
	stubDrafts(t, nil)
	deleteDrafts = func(c string) ([]string, []bd.BranchOutcome, error) {
		steps = append(steps, "delete-drafts "+c)
		return nil, nil, delErr
	}

	if err := Drop(bd.Bead{ID: "mc-a.2"}, Info{Worktree: "/wt"}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"kill-session -t =mc-a_2", "delete-drafts mc-a.2", "remove /wt"}; !slices.Equal(steps, want) {
		t.Errorf("steps = %q, want %q", steps, want)
	}

	steps, killErr = nil, errors.New("no session")
	if err := Drop(bd.Bead{ID: "g"}, Info{Worktree: "/wt"}); err == nil || !slices.Equal(steps, []string{"kill-session -t =g"}) {
		t.Errorf("a failed kill must remove nothing: err=%v steps=%q", err, steps)
	}

	steps, killErr = nil, nil
	if err := Drop(bd.Bead{ID: "g"}, Info{}); err != nil || !slices.Equal(steps, []string{"kill-session -t =g", "delete-drafts g"}) {
		t.Errorf("repo-root leaf has no tree to remove: err=%v steps=%q", err, steps)
	}

	// A failed Draft delete still removes the tree: the session is gone, so a
	// retried drop couldn't kill it again.
	steps, delErr = nil, errors.New("bd: locked")
	capt := eventlogtest.Capture(t)
	if err := Drop(bd.Bead{ID: "g", Parent: "ch"}, Info{Worktree: "/wt"}); err == nil || !slices.Equal(steps, []string{"kill-session -t =g", "delete-drafts g", "remove /wt"}) {
		t.Errorf("failed delete: err=%v steps=%q", err, steps)
	}
	if got := eventlogtest.Names(*capt); !slices.Equal(got, []string{"draft.failed"}) {
		t.Errorf("events = %v, want [draft.failed]", got)
	}
	// ADR 0029 §3: the cleanup carries the common attrs, so a Chart-scope filter sees it.
	if a := (*capt)[0].Attrs; a["bead"] != "g" || a["parent"] != "ch" {
		t.Errorf("draft.failed attrs = %v, want bead=g parent=ch", a)
	}
}

// An alive drop races hitl-run's own clean-exit remove (kill-session doesn't wait):
// a tree already gone when the remove fails is a drop done, not an error.
func TestDropTreeAlreadyGone(t *testing.T) {
	origKill, origRemove := killSession, gitRemoveWorktree
	defer func() { killSession, gitRemoveWorktree = origKill, origRemove }()
	stubServer(t)
	stubDrafts(t, nil)
	killSession = func(string, []string) error { return nil }
	gitRemoveWorktree = func(string) error { return errors.New("not a working tree") }
	if err := Drop(bd.Bead{ID: "g"}, Info{Worktree: t.TempDir() + "/gone"}); err != nil {
		t.Errorf("gone tree: err = %v, want nil", err)
	}
	if err := Drop(bd.Bead{ID: "g"}, Info{Worktree: t.TempDir()}); err == nil {
		t.Error("a tree still on disk must surface the remove error")
	}
}

// Orphan classification (ADR 0041 §2): the worktree holding a branch × the session
// table. Only a mugicrew-wt-* tree that no session has as @worktree is an orphan;
// an empty table (no server running) orphans every mugicrew-wt-* holder.
func TestOrphan(t *testing.T) {
	const wt = "/tmp/mugicrew-wt-123/wt"
	sessions := map[string]Info{
		"mc-a.1": {Worktree: wt, State: Alive},
		"mc-a.2": {Worktree: "/tmp/mugicrew-wt-456/wt", State: Exited, RC: 1},
		"mc-a.3": {}, // repo-root leaf
	}
	for _, c := range []struct {
		wt       string
		sessions map[string]Info
		orphan   bool
		holder   string
	}{
		{wt, sessions, false, "mc-a.1"},                        // live session
		{"/tmp/mugicrew-wt-456/wt", sessions, false, "mc-a.2"}, // exited session
		{"/tmp/mugicrew-wt-789/wt", sessions, true, ""},        // no session has it
		{wt, nil, true, ""},                                    // no server running
		{"/home/me/repo-feature", nil, false, ""},              // not a mugicrew tree
		{"/tmp/mugicrew-wt-789/other", nil, false, ""},         // not NewWorktree's /wt leaf
		{"", nil, false, ""},                                   // no holder
	} {
		if got := HolderBead(c.wt, c.sessions); got != c.holder {
			t.Errorf("HolderBead(%q) = %q, want %q", c.wt, got, c.holder)
		}
		if got := Orphan(c.wt, c.sessions); got != c.orphan {
			t.Errorf("Orphan(%q, %d sessions) = %v, want %v", c.wt, len(c.sessions), got, c.orphan)
		}
	}
}

// DropOrphan is a forced remove with its temp parent, then a prune (ADR 0041 §3).
// A directory already gone (prunable) is no error: prune still frees the branch.
func TestDropOrphan(t *testing.T) {
	origRemove, origPrune := gitRemoveWorktree, pruneWorktrees
	defer func() { gitRemoveWorktree, pruneWorktrees = origRemove, origPrune }()
	var steps []string
	gitRemoveWorktree = func(wt string) error { steps = append(steps, "remove"); return errors.New("not a working tree") }
	pruneWorktrees = func() error { steps = append(steps, "prune"); return nil }

	parent := filepath.Join(t.TempDir(), "mugicrew-wt-1")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := DropOrphan(filepath.Join(parent, "wt")); err != nil {
		t.Fatalf("prunable orphan: %v", err)
	}
	if want := []string{"remove", "prune"}; !slices.Equal(steps, want) {
		t.Errorf("steps = %q, want %q", steps, want)
	}
	if _, err := os.Stat(parent); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("temp parent left behind: %v", err)
	}

	steps = nil
	if err := DropOrphan(t.TempDir()); err == nil || !slices.Equal(steps, []string{"remove"}) {
		t.Errorf("a tree still on disk must surface the remove error, no prune: err=%v steps=%q", err, steps)
	}
}

// Only tmux's "nothing is listening" errors read as no server (an empty table);
// any other list failure is an error, so it can't orphan a live session's tree.
func TestNoServer(t *testing.T) {
	for msg, want := range map[string]bool{
		"no server running on /repo/.git/mugicrew/tmux.sock\n":                                 true,
		"error connecting to /repo/.git/mugicrew/tmux.sock (Permission denied)\n":              false,
		"error connecting to /repo/.git/mugicrew/tmux.sock (Socket operation on non-socket)\n": false,
		"Error connecting to /repo/.git/mugicrew/tmux.sock (No such file or directory)\n":      true,
		"open terminal failed: missing or unsuitable terminal\n":                               false,
		"": false,
	} {
		if got := noServer(msg); got != want {
			t.Errorf("noServer(%q) = %v, want %v", msg, got, want)
		}
	}
}

// assertBar checks a HITL session's bar options in args (ADR 0066 §3): its bead on
// the left, both ways out on the right, the HITL key table, an empty window list,
// and no detach advertised.
func assertBar(t *testing.T, args []string, left string) {
	t.Helper()
	opt := func(name string) string {
		i := slices.Index(args, name)
		if i < 0 || i+1 >= len(args) {
			t.Errorf("%s not set in %v", name, args)
			return ""
		}
		return args[i+1]
	}
	for name, want := range map[string]string{
		"status-left": left, "status-left-length": "200",
		"status-right": "F12 back to TUI · /exit to finish", "key-table": "mchitl",
		"window-status-format": "", "window-status-current-format": "",
	} {
		if got := opt(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if bar := opt("status-left") + opt("status-right"); strings.Contains(bar, "detach") || strings.Contains(bar, "│") {
		t.Errorf("bar still advertises detach: %q", bar)
	}
}

// assertUnmarks checks a spawn or respawn argv unsets the session's waiting bit,
// Closing mark and Auto-exit deadline (ADR 0111: spawn and respawn start
// disarmed), each as its own ;-chained command on session mc-a_2.
func assertUnmarks(t *testing.T, args []string) {
	t.Helper()
	for _, opt := range []string{"@waiting", "@closing", "@autofinish"} {
		if !strings.Contains(" "+strings.Join(args, " ")+" ", " ; set-option -u -t =mc-a_2: "+opt+" ") {
			t.Errorf("argv does not unset %s:\n%s", opt, strings.Join(args, " "))
		}
	}
}

// An armed session's bar is hitl-run's countdown (Spec D4): barsArgs
// skips it and still sets the unarmed ones; all armed is no call at all.
func TestBarsArgsSkipsArmed(t *testing.T) {
	armed := map[string]Info{"mc-b.1": {State: Alive, AutoExit: time.Unix(1760000000, 0)}, "mc-a": {State: Alive}}
	got := strings.Join(barsArgs(map[string]bool{"mc-b.1": true, "mc-a": false}, armed), " ")
	if want := "set-option -t =mc-a: status-right F12 back to TUI · /exit to finish"; got != want {
		t.Errorf("barsArgs =\n %q\nwant\n %q", got, want)
	}
	if got := barsArgs(map[string]bool{"mc-b.1": true}, armed); len(got) != 0 {
		t.Errorf("barsArgs all armed = %q, want none", got)
	}
}

// The countdown bar reads M:SS, rounding a part second up (Spec FR14).
func TestAutoExitBar(t *testing.T) {
	for left, want := range map[time.Duration]string{
		105 * time.Second:                     "auto-exit in 1:45 · F11 cancels",
		59*time.Second + 100*time.Millisecond: "auto-exit in 1:00 · F11 cancels",
		9 * time.Second:                       "auto-exit in 0:09 · F11 cancels",
		10 * time.Minute:                      "auto-exit in 10:00 · F11 cancels",
		0:                                     "auto-exit in 0:00 · F11 cancels",
	} {
		if got := autoExitBar(left); got != want {
			t.Errorf("autoExitBar(%v) = %q, want %q", left, got, want)
		}
	}
}

// The done flip's argv (ADR 0066 §4): an open bead's session gets the normal right
// bar, a closed one's the loud green/reversed ✓ line; one ;-joined call, id order.
func TestBarsArgs(t *testing.T) {
	got := strings.Join(barsArgs(map[string]bool{"mc-b.1": true, "mc-a": false}, nil), " ")
	want := "set-option -t =mc-a: status-right F12 back to TUI · /exit to finish ; " +
		"set-option -t =mc-b_1: status-right #[fg=green,reverse] ✓ mc-b.1 closed — /exit to finish · F12 back to TUI "
	if got != want {
		t.Errorf("barsArgs =\n %q\nwant\n %q", got, want)
	}
}

// A TUI records its client's tty only where it isn't already the sole holder, and
// takes it from any other TUI session still holding it (ADR 0065 §5).
func TestRecordArgs(t *testing.T) {
	for _, tc := range []struct{ sessions, want string }{
		{"tui-1\t/dev/pts/3\nmc-a_1\t\n", ""},
		{"tui-1\t\nmc-a_1\t\n", "set-option -t =tui-1: @tty /dev/pts/3"},
		{"tui-1\t/dev/pts/4\n", "set-option -t =tui-1: @tty /dev/pts/3"},
		{"tui-1\t/dev/pts/3\ntui-2\t/dev/pts/3\n", "set-option -u -t =tui-2: @tty ; set-option -t =tui-1: @tty /dev/pts/3"},
	} {
		if got := strings.Join(recordArgs("/dev/pts/3", "tui-1", tc.sessions), " "); got != tc.want {
			t.Errorf("recordArgs over %q = %q, want %q", tc.sessions, got, tc.want)
		}
	}
}

// A clean exit switches each viewer with a TUI to it, never one without
// (ADR 0065 §6).
func TestReturnArgs(t *testing.T) {
	sessions := "tui-1\t/dev/pts/3\ntui-2\t/dev/pts/4\nmc-a_1\t\n"
	for _, tc := range []struct{ ttys, want string }{
		{"/dev/pts/3\n", "switch-client -c /dev/pts/3 -t =tui-1"},
		{"/dev/pts/3\n/dev/pts/9\n/dev/pts/4\n", "switch-client -c /dev/pts/3 -t =tui-1 ; switch-client -c /dev/pts/4 -t =tui-2"},
		{"/dev/pts/9\n", ""},
		{"", ""},
	} {
		if got := strings.Join(returnArgs(tc.ttys, sessions), " "); got != tc.want {
			t.Errorf("returnArgs(%q) = %q, want %q", tc.ttys, got, tc.want)
		}
	}
}

// DropDrafts records each deleted container Draft's branch outcome after
// draft.deleted, on the bead's attrs.
func TestDropDraftsRecordsBranches(t *testing.T) {
	stubDrafts(t, nil)
	deleteDrafts = func(string) ([]string, []bd.BranchOutcome, error) {
		return []string{"mc-c"}, []bd.BranchOutcome{{Branch: "chart/mc-c"}}, nil
	}
	capt := eventlogtest.Capture(t)
	if err := DropDrafts(bd.Bead{ID: "mc-s", Kind: contract.KindSignal}); err != nil {
		t.Fatal(err)
	}
	if got := eventlogtest.Names(*capt); !slices.Equal(got, []string{"draft.deleted", "branch.deleted"}) {
		t.Fatalf("events = %v", got)
	}
	if a := (*capt)[1].Attrs; a["branch"] != "chart/mc-c" || a["bead"] != "mc-s" {
		t.Errorf("branch.deleted = %v", a)
	}
}

// A clean HITL exit on an effort tree runs the backstop before the tree goes
// (Spec D6, FR 20): integrated, the tree is removed and then effort/<id>
// deleted; a conflict keeps both for the human (the orphan confirm relaunches it,
// FR 40) and the wrapper still exits clean; a transient keeps the branch only;
// hitl.exited says which (stranded, kept). A grilling's effort integrates like any
// leaf; a land step's detached tree never integrates; a failed run keeps all.
func TestHitlRunBackstopsEffortTree(t *testing.T) {
	origRun, origRemove := runAgent, removeWorktree
	defer func() { runAgent, removeWorktree = origRun, origRemove }()
	for _, tc := range []struct {
		name  string
		kind  contract.Kind
		agent func([]string, <-chan struct{}) error
		exit  int
		rc    int
		// stranded, kept: the backstop's outcome, on hitl.exited
		stranded, kept bool
		want           []string
	}{
		{"integrated", contract.KindSurgery, func([]string, <-chan struct{}) error { return nil }, 0, 0, false, false,
			[]string{"publish sg", "integrate", "remove", "delete effort/sg"}},
		{"conflict keeps tree and branch", contract.KindSurgery, func([]string, <-chan struct{}) error { return nil }, 2, 0, true, true,
			[]string{"publish sg", "integrate", "abort /tmp/mugicrew-wt-x/wt", "pause sg integrate-conflict"}},
		{"transient keeps the branch", contract.KindSurgery, func([]string, <-chan struct{}) error { return nil }, 1, 0, false, true,
			[]string{"publish sg", "integrate", "abort /tmp/mugicrew-wt-x/wt", "remove"}},
		{"grilling integrates onto its chart", contract.KindGrilling, func([]string, <-chan struct{}) error { return nil }, 0, 0, false, false,
			[]string{"publish sg", "integrate", "remove", "delete effort/sg"}},
		{"land step's detached tree never integrates", contract.KindCheckpoint, func([]string, <-chan struct{}) error { return nil }, 0, 0, false, false,
			[]string{"publish sg", "remove"}},
		{"failed run keeps everything", contract.KindSurgery, func([]string, <-chan struct{}) error { return exec.Command("sh", "-c", "exit 3").Run() }, 0, 3, false, false,
			nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evs := eventlogtest.Capture(t)
			steps := stubDrafts(t, nil)
			returnViewers, markClosing = func(string, string) {}, func(string, string) {}
			var did []string
			stubBackstop(t, &did, tc.exit, tc.exit == 2, "in_progress", "open", "")
			showBead = func(id string) (bd.Bead, error) {
				return bd.Bead{ID: id, Kind: tc.kind, Status: "in_progress", Parent: "cp"}, nil
			}
			runAgent = tc.agent
			removeWorktree = func(string) error { did = append(did, "remove"); return nil }
			if rc := HitlRun([]string{"--bead=sg", "--worktree=/tmp/mugicrew-wt-x/wt", "--", "claude"}, meter(t)); rc != tc.rc {
				t.Errorf("rc = %d, want %d", rc, tc.rc)
			}
			if got := append(slices.Clone(*steps), did...); !slices.Equal(got, tc.want) {
				t.Errorf("steps = %q, want %q", got, tc.want)
			}
			for _, e := range *evs {
				if e.Name == "hitl.exited" && ((e.Attrs["stranded"] == true) != tc.stranded || (e.Attrs["kept"] == true) != tc.kept) {
					t.Errorf("hitl.exited stranded, kept = %v, %v; want %v, %v", e.Attrs["stranded"], e.Attrs["kept"], tc.stranded, tc.kept)
				}
			}
		})
	}
}

// Drop removes an effort tree and keeps effort/<id> when it has commits ahead of
// the container, for the next launch to reuse; an empty one goes, so it never
// holds the Checkpoint's drain (Spec FR 48, 53).
func TestDropKeepsEffortBranch(t *testing.T) {
	origKill := killSession
	defer func() { killSession = origKill }()
	stubServer(t)
	stubDrafts(t, nil)
	killSession = func(string, []string) error { return nil }
	repo, wt := t.TempDir(), t.TempDir()+"/wt"
	t.Chdir(repo)
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "base"},
		{"branch", "checkpoint/cp"},
		{"worktree", "add", "-q", "-b", "effort/sg", wt, "checkpoint/cp"},
		{"-C", wt, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "surgery work"},
		{"worktree", "add", "-q", "-b", "effort/sg2", wt + "2", "checkpoint/cp"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := Drop(bd.Bead{ID: "sg", Kind: contract.KindSurgery, Parent: "cp"}, Info{Worktree: wt}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("tree still on disk: %v", err)
	}
	if out, err := exec.Command("git", "log", "-1", "--format=%s", "effort/sg").Output(); err != nil || strings.TrimSpace(string(out)) != "surgery work" {
		t.Errorf("effort/sg = %q (%v), want its commit kept", out, err)
	}
	if err := Drop(bd.Bead{ID: "sg2", Kind: contract.KindSurgery, Parent: "cp"}, Info{Worktree: wt + "2"}); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "rev-parse", "--verify", "--quiet", "effort/sg2").Run(); err == nil {
		t.Error("effort/sg2 has nothing ahead, want it deleted")
	}
	// A replace-mode bead's drop-only rewrite reads as 0 ahead; it is a rewrite to
	// publish, so its branch stays.
	if out, err := exec.Command("git", "worktree", "add", "-q", "-b", "effort/cf", wt+"3", "checkpoint/cp").CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v: %s", err, out)
	}
	if err := Drop(bd.Bead{ID: "cf", Kind: contract.KindConflict, Parent: "cp"}, Info{Worktree: wt + "3"}); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "rev-parse", "--verify", "--quiet", "effort/cf").Run(); err != nil {
		t.Error("effort/cf is a replace-mode effort, want it kept")
	}
}
