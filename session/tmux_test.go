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
				"--worktree=" + tc.worktree, argvArg(t, "claude", "--session-id", "sess-1")}, meter(t))
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
func meter(t *testing.T) func(string) func() contract.Metered {
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
			HitlRun([]string{"--bead=g", "--session=sess-1", "--worktree=/tmp/mugicrew-wt-x/wt", argvArg(t, "claude")}, m)
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
	origShow, origSucc, origMark, origList := showBead, successorOf, markClosing, listDrafts
	origEnd := endSession
	t.Cleanup(func() {
		publish, deleteDrafts, hupGrace, returnViewers = origPub, origDel, origGrace, origRet
		showBead, successorOf, markClosing, listDrafts = origShow, origSucc, origMark, origList
		endSession = origEnd
	})
	listDrafts = func(string) ([]bd.Bead, error) { return nil, nil }
	hupGrace = 50 * time.Millisecond
	calls := &[]string{}
	markClosing = func(tmuxEnv, pane string) { *calls = append(*calls, "closing "+tmuxEnv+" "+pane) }
	returnViewers = func(tmuxEnv, pane string) { *calls = append(*calls, "return "+tmuxEnv+" "+pane) }
	endSession = func(string, string) {} // never kill a real session; TestHitlRunEndsSessionOnCleanFinish records
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
			if rc := HitlRun([]string{"--bead=grl.1", "--session=s", "--worktree=/tmp/mugicrew-wt-x/wt", argvArg(t, "claude")}, meter(t)); rc != tc.rc {
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

// A clean finish kills its own session as its last act, after hitl.exited is
// recorded; a failed publish, settle or teardown, a drop, a non-zero exit and an
// empty pane kill nothing. A failed kill is TestHitlRunSurvivesKillError's.
func TestHitlRunEndsSessionOnCleanFinish(t *testing.T) {
	origRun, origRemove, origEnd, origRelease := runAgent, removeWorktree, endSession, releaseClaim
	defer func() { runAgent, removeWorktree, endSession, releaseClaim = origRun, origRemove, origEnd, origRelease }()
	ok := func([]string, <-chan struct{}) error { return nil }
	for _, tc := range []struct {
		name      string
		agent     func([]string, <-chan struct{}) error
		pane      string
		pubErr    error
		removeErr error
		settleErr error
		wantRC    int
		wantEnd   bool
	}{
		{"clean exit", ok, "%7", nil, nil, nil, 0, true},
		{"failed publish", ok, "%7", errors.New("locked"), nil, nil, 1, false},
		{"failed settle", ok, "%7", nil, nil, errors.New("bd: locked"), 1, false},
		{"failed teardown", ok, "%7", nil, errors.New("busy"), nil, 1, false},
		{"drop", func([]string, <-chan struct{}) error { return syscall.Kill(os.Getpid(), syscall.SIGHUP) }, "%7", nil, nil, nil, 0, false},
		{"non-zero exit", func([]string, <-chan struct{}) error { return exec.Command("sh", "-c", "exit 3").Run() }, "%7", nil, nil, nil, 3, false},
		{"empty pane", ok, "", nil, nil, nil, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capt := eventlogtest.Capture(t)
			stubDrafts(t, tc.pubErr)
			t.Setenv("TMUX", "/s,1,2")
			t.Setenv("TMUX_PANE", tc.pane)
			runAgent = tc.agent
			removeWorktree = func(string) error { return tc.removeErr }
			showBead = func(id string) (bd.Bead, error) {
				return bd.Bead{ID: id, Kind: contract.KindGrilling, Status: "in_progress"}, nil
			}
			releaseClaim = func(string) error { return tc.settleErr }
			var ended []string
			endSession = func(tmuxEnv, pane string) {
				ended = append(ended, tmuxEnv+" "+pane+" after "+eventlogtest.Names(*capt)[len(*capt)-1])
			}
			if rc := HitlRun([]string{"--bead=grl.1", "--session=s", "--worktree=/tmp/mugicrew-wt-x/wt", argvArg(t, "claude")}, meter(t)); rc != tc.wantRC {
				t.Errorf("rc = %d, want %d", rc, tc.wantRC)
			}
			var want []string
			if tc.wantEnd {
				want = []string{"/s,1,2 %7 after hitl.exited"}
			}
			if !slices.Equal(ended, want) {
				t.Errorf("endSession calls = %q, want %q", ended, want)
			}
		})
	}
}

// endSession returns nothing, so a failed kill cannot reach HitlRun's return
// code or events by construction; this runs the real one against a dead socket
// and holds that: rc 0, hitl.exited last and clean, and a hitl.ended warning after it.
func TestHitlRunSurvivesKillError(t *testing.T) {
	origRun, origRemove := runAgent, removeWorktree
	defer func() { runAgent, removeWorktree = origRun, origRemove }()
	capt := eventlogtest.Capture(t)
	origEnd := endSession
	stubDrafts(t, nil)
	endSession = origEnd // the real kill, aimed at a socket that does not exist
	t.Setenv("TMUX", filepath.Join(t.TempDir(), "no-such-socket")+",1,2")
	t.Setenv("TMUX_PANE", "%7")
	runAgent = func([]string, <-chan struct{}) error { return nil }
	removeWorktree = func(string) error { return nil }
	if rc := HitlRun([]string{"--bead=grl.1", "--session=s", "--worktree=/tmp/mugicrew-wt-x/wt", argvArg(t, "claude")}, meter(t)); rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
	// the warning is logged after hitl.exited, which stays the clean close event
	if got, want := eventlogtest.Names(*capt), []string{"hitl.launched", "draft.published", "hitl.exited", "hitl.ended"}; !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
}

// The kill targets the pane id (so its own session), never =<name>.
func TestKillSessionArgvTargetsPane(t *testing.T) {
	if got := killSessionArgv("%7"); !slices.Equal(got, []string{"kill-session", "-t", "%7"}) {
		t.Errorf("argv = %q, want kill-session -t %%7", got)
	}
}

// A clean exit closes the session's Signal in its publish batch only when the
// graph shows a caused-by Chart, Spec or Initiative successor (ADR 0096; Spec D2,
// and D3 for the Initiative).
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
		{"Signal with an Initiative closes", contract.KindSignal, contract.KindInitiative, nil, 0,
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
			if rc := HitlRun([]string{"--bead=sig", "--session=s", "--worktree=/tmp/mugicrew-wt-x/wt", argvArg(t, "claude")}, meter(t)); rc != tc.rc {
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

// A Survey session's clean exit closes the Survey in its publish batch when its
// parent Chart is closed (push-back) or its Drafts include a child of the Chart
// (fog seeded); otherwise it leaves it for settle to release back to the Inbox
// (Spec mc-r832 D8, FR 15–17). A failed graph read fails the publish.
func TestHitlRunClosesSurveyOnPublish(t *testing.T) {
	origRun, origRelease, origDelete := runAgent, releaseClaim, deleteBranch
	defer func() { runAgent, releaseClaim, deleteBranch = origRun, origRelease, origDelete }()
	pre := []string{"closing /s,1,2 %7", "return /s,1,2 %7"}
	for _, tc := range []struct {
		name    string
		chart   string   // the parent Chart's status
		parents []string // the Survey's Drafts' parents
		readErr error
		rc      int
		steps   []string
	}{
		{"pushed-back Chart closes the Survey", "closed", nil, nil, 0, append(slices.Clip(pre), "publish s1 close s1", "delete chart/ch1")},
		{"seeded fog closes the Survey", "open", []string{"", "ch1"}, nil, 0, append(slices.Clip(pre), "publish s1 close s1")},
		{"no fog leaves the Survey for settle", "open", []string{"", "other"}, nil, 0, append(slices.Clip(pre), "publish s1", "release s1")},
		{"failed Chart read keeps the session", "open", nil, errors.New("bd: locked"), 1, pre},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eventlogtest.Capture(t)
			steps := stubDrafts(t, nil)
			t.Setenv("TMUX", "/s,1,2")
			t.Setenv("TMUX_PANE", "%7")
			status, pub := "in_progress", publish
			publish = func(creator string, edges []bd.Edge, closing *bd.Closing) ([]string, error) {
				if closing != nil {
					status = "closed"
				}
				return pub(creator, edges, closing)
			}
			showBead = func(id string) (bd.Bead, error) {
				if id == "ch1" {
					return bd.Bead{ID: id, Kind: contract.KindChart, Status: tc.chart}, tc.readErr
				}
				return bd.Bead{ID: id, Kind: contract.KindSurvey, Status: status, Parent: "ch1"}, nil
			}
			listDrafts = func(string) ([]bd.Bead, error) {
				var ds []bd.Bead
				for _, p := range tc.parents {
					ds = append(ds, bd.Bead{Parent: p})
				}
				return ds, nil
			}
			releaseClaim = func(id string) error { *steps = append(*steps, "release "+id); return nil }
			deleteBranch = func(name string) error { *steps = append(*steps, "delete "+name); return nil }
			runAgent = func([]string, <-chan struct{}) error { return nil }
			if rc := HitlRun([]string{"--bead=s1", "--session=s", argvArg(t, "claude")}, meter(t)); rc != tc.rc {
				t.Errorf("rc = %d, want %d", rc, tc.rc)
			}
			if !slices.Equal(*steps, tc.steps) {
				t.Errorf("steps = %q, want %q", *steps, tc.steps)
			}
		})
	}
}

// A Survey session's clean exit settles its pushed-back Chart's branch (Spec
// mc-r832 D9, ADR 0130 §6 re-keyed): a closed Survey whose parent Chart is closed
// with no caused-by Spec successor deletes chart/<parent> after the teardown, on
// an effort tree only once the backstop has integrated onto it. A Chart with a
// Spec, or still open, keeps its branch.
func TestHitlRunSettlesChartOnCleanExit(t *testing.T) {
	origRun, origRemove := runAgent, removeWorktree
	defer func() { runAgent, removeWorktree = origRun, origRemove }()
	pub := []string{"closing /s,1,2 %7", "return /s,1,2 %7", "publish s1 close s1"}
	for _, tc := range []struct {
		name  string
		chart string // the parent Chart's status
		spec  bool   // a caused-by Spec succeeds the Chart
		tree  bool   // the session runs on its effort tree
		steps []string
	}{
		{"Chart closed without a Spec loses its branch", "closed", false, false, append(slices.Clip(pub), "delete chart/ch1")},
		{"Chart closed with a Spec keeps its branch", "closed", true, false, pub},
		{"open Chart keeps its branch", "open", false, false, []string{"closing /s,1,2 %7", "return /s,1,2 %7", "publish s1"}},
		{"closed Chart on the Survey's effort tree loses it after the backstop", "closed", false, true,
			append(slices.Clip(pub), "integrate", "remove", "delete effort/s1", "delete chart/ch1")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eventlogtest.Capture(t)
			steps := stubDrafts(t, nil)
			stubBackstop(t, steps, 0, false, "closed", "open", "")
			t.Setenv("TMUX", "/s,1,2")
			t.Setenv("TMUX_PANE", "%7")
			showBead = func(id string) (bd.Bead, error) {
				if id == "ch1" {
					return bd.Bead{ID: id, Kind: contract.KindChart, Status: tc.chart}, nil
				}
				return bd.Bead{ID: id, Kind: contract.KindSurvey, Status: "closed", Parent: "ch1"}, nil
			}
			successorOf = func(id string, want contract.Kind) (string, bool, error) {
				return id + ".succ", tc.spec && id == "ch1" && want == contract.KindSpec, nil
			}
			deleteBranch = func(name string) error { *steps = append(*steps, "delete "+name); return nil }
			runAgent = func([]string, <-chan struct{}) error { return nil }
			removeWorktree = func(string) error { *steps = append(*steps, "remove"); return nil }
			args := []string{"--bead=s1", "--session=s", argvArg(t, "claude")}
			if tc.tree {
				args = append([]string{"--worktree=/tmp/mugicrew-wt-x/wt"}, args...)
			}
			if rc := HitlRun(args, meter(t)); rc != 0 {
				t.Errorf("rc = %d, want 0", rc)
			}
			if !slices.Equal(*steps, tc.steps) {
				t.Errorf("steps = %q, want %q", *steps, tc.steps)
			}
		})
	}
}

// A clean exit releases the session's bead if it is still in_progress after the
// publish, so a premature quit (a double Ctrl+C mid-triage) strands no claim
// (Spec mc-rjvx FR 1–2): every kind but Task (it stays claimed and idle for l),
// Surgery (claimed at filing) and Discussion (left claimed for a rejoin). An
// Initiative is released here too (FR 5–7). A bead the
// session closed keeps its status (FR 3). A Chart is never a session bead, so a
// closed one deletes nothing here (Spec mc-r832 D9). A non-clean exit or a drop's
// hangup releases nothing (FR 9–10); a failed release fails the session (FR 11).
func TestHitlRunSettlesOnCleanExit(t *testing.T) {
	origRun, origRemove, origRelease := runAgent, removeWorktree, releaseClaim
	defer func() { runAgent, removeWorktree, releaseClaim = origRun, origRemove, origRelease }()
	pub := []string{"closing /s,1,2 %7", "return /s,1,2 %7", "publish c1"}
	for _, tc := range []struct {
		name   string
		kind   contract.Kind
		status string
		spec   bool  // a caused-by Spec succeeds the bead
		tree   bool  // the session runs on its effort tree
		relErr error // the release's failure
		rc     int   // the agent's exit; -1 is a drop's hangup
		want   int   // HitlRun's exit
		steps  []string
	}{
		{"closed Chart deletes nothing", contract.KindChart, "closed", false, false, nil, 0, 0, pub},
		{"in_progress Signal is released", contract.KindSignal, "in_progress", false, false, nil, 0, 0, append(slices.Clip(pub), "release c1")},
		{"in_progress grilling bead is released", contract.KindGrilling, "in_progress", false, false, nil, 0, 0, append(slices.Clip(pub), "release c1")},
		{"closed Signal is not released", contract.KindSignal, "closed", false, false, nil, 0, 0, pub},
		{"closed grilling bead keeps its chart", contract.KindGrilling, "closed", false, false, nil, 0, 0, pub},
		{"in_progress Task keeps its claim", contract.KindTask, "in_progress", false, false, nil, 0, 0, pub},
		{"in_progress Surgery keeps its claim", contract.KindSurgery, "in_progress", false, false, nil, 0, 0, pub},
		{"in_progress Discussion keeps its claim", contract.KindDiscussion, "in_progress", false, false, nil, 0, 0, pub},
		{"in_progress Checkup keeps its claim", contract.KindCheckup, "in_progress", false, false, nil, 0, 0, pub},
		{"closed Checkup publishes and returns", contract.KindCheckup, "closed", false, false, nil, 0, 0, pub},
		{"closed Discussion publishes and returns", contract.KindDiscussion, "closed", false, false, nil, 0, 0, pub},
		{"in_progress Initiative is released", contract.KindInitiative, "in_progress", false, false, nil, 0, 0,
			append(slices.Clip(pub), "release c1")},
		{"failed release fails the session", contract.KindSignal, "in_progress", false, false, errors.New("bd: locked"), 0, 1,
			append(slices.Clip(pub), "release c1")},
		{"non-clean exit does nothing", contract.KindChart, "closed", false, false, nil, 3, 3, nil},
		{"non-clean exit keeps a Signal's claim", contract.KindSignal, "in_progress", false, false, nil, 3, 3, nil},
		{"hangup (drop) keeps a Signal's claim", contract.KindSignal, "in_progress", false, false, nil, -1, 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eventlogtest.Capture(t)
			steps := stubDrafts(t, nil)
			stubBackstop(t, steps, 0, false, tc.status, "open", "")
			t.Setenv("TMUX", "/s,1,2")
			t.Setenv("TMUX_PANE", "%7")
			showBead = func(id string) (bd.Bead, error) { return bd.Bead{ID: id, Kind: tc.kind, Status: tc.status}, nil }
			successorOf = func(id string, want contract.Kind) (string, bool, error) {
				return id + ".succ", tc.spec && want == contract.KindSpec, nil
			}
			releaseClaim = func(id string) error { *steps = append(*steps, "release "+id); return tc.relErr }
			deleteBranch = func(name string) error { *steps = append(*steps, "delete "+name); return nil }
			runAgent = func([]string, <-chan struct{}) error {
				switch tc.rc {
				case 0:
					return nil
				case -1:
					return syscall.Kill(os.Getpid(), syscall.SIGHUP)
				}
				return exec.Command("sh", "-c", "exit 3").Run()
			}
			removeWorktree = func(string) error { *steps = append(*steps, "remove"); return nil }
			args := []string{"--bead=c1", "--session=s", argvArg(t, "claude")}
			if tc.tree {
				args = append([]string{"--worktree=/tmp/mugicrew-wt-x/wt"}, args...)
			}
			if rc := HitlRun(args, meter(t)); rc != tc.want {
				t.Errorf("rc = %d, want %d", rc, tc.want)
			}
			if !slices.Equal(*steps, tc.steps) {
				t.Errorf("steps = %q, want %q", *steps, tc.steps)
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
			if rc := HitlRun([]string{"--bead=g", "--worktree=/tmp/mugicrew-wt-x/wt", argvArg(t, "sh", "-c", tc.script)}, meter(t)); rc != tc.rc {
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
	t1 := sessionTags{bd.Bead{ID: "mc-a.2", Parent: "mc-a", Kind: contract.KindGrilling, Title: "Grill #1"}, "s1", "/wt", "deepseek"}
	args := launchArgs(tmuxName(t1.bead.ID), "/wt", paneArgs("/abs/mugicrew", t1, "/run/argv;", "/run/env;"), t1)
	joined := strings.Join(args, " ")
	assertEnvArg(t, args)
	// The tags follow new-session in the same tmux command.
	if want := strings.Join(preamble(), " ") + " ; new-session -d -s mc-a_2 -c /wt -- /abs/mugicrew hitl-run --bead=mc-a.2 --parent=mc-a --crewmate=nami --kind=grilling --session=s1 --worktree=/wt --profile=deepseek --argv-file=/run/argv\\; --env-file=/run/env\\; ; set-option -t =mc-a_2: @bead mc-a.2"; !strings.HasPrefix(joined, want) {
		t.Errorf("launch argv =\n%s\nwant prefix\n%s", joined, want)
	}

	for _, want := range []string{
		"set-option -t =mc-a_2: @bead mc-a.2",
		"set-option -t =mc-a_2: @session s1",
		"set-option -t =mc-a_2: @worktree /wt",
		"set-option -t =mc-a_2: @profile deepseek",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q:\n%s", want, joined)
		}
	}
	assertBar(t, args, "○ GRILLING P0 · Grill ##1 · mc-a.2")
	assertUnmarks(t, args)
	// -c is a tmux format too: a # in the cwd is escaped, in launch and respawn alike.
	for _, a := range [][]string{launchArgs("s", "/w#t", nil, t1), respawnArgs("s", "/w#t", nil, t1.bead)} {
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
	// F6 is bound only in the HITL key table, never in root (ADR 0065 §5); F12 is no longer bound.
	if strings.Contains(pre, "-n F6") || strings.Contains(pre, "bind-key -T root") || !strings.Contains(pre, "bind-key -T mchitl F6 ") {
		t.Errorf("preamble binds F6 outside the HITL table: %q", pre)
	}
	if strings.Contains(pre, "bind-key -T mchitl F12 ") {
		t.Errorf("preamble still binds F12: %q", pre)
	}
	// F1 opens the HITL keys menu, in the HITL table only (ADR 0296); F11 is not bound.
	if !strings.Contains(pre, " ; bind-key -T mchitl F1 ") || strings.Contains(pre, "-n F1 ") || strings.Contains(pre, "bind-key -T mchitl F11 ") {
		t.Errorf("preamble lacks the HITL table's F1 menu or still binds F11: %q", pre)
	}
	// F10 opens the session's bead pane, in the HITL table only.
	if !strings.Contains(pre, " ; bind-key -T mchitl F10 ") || strings.Contains(pre, "-n F10") {
		t.Errorf("preamble lacks the HITL table's F10 bead pane: %q", pre)
	}
	if !strings.Contains(pre, "bead-detail") || strings.Contains(pre, "bd show") || !strings.Contains(pre, "@beadpane") || !strings.Contains(pre, "less -R") || !strings.Contains(pre, "2>&1") {
		t.Errorf("F10 binding does not run bead-detail into less -R: %q", pre)
	}
	t1 := sessionTags{bd.Bead{ID: "g"}, "s", "", ""}
	tui := strings.Join(tuiArgs("tui-7", "/r#", tuiPane("/abs/mugicrew", "/c/tui.out", "/run/env")), " ")
	for _, a := range []string{strings.Join(launchArgs("g", "/r", nil, t1), " "), tui} {
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
	path, err := writeEnvFile(nil)
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

// A handoff file that is gone is no handoff for the TUI (a session from an older
// mugicrew carries the stale variable); one that exists but can't be read is still an error.
func TestApplyTUIEnvMissingOrUnreadable(t *testing.T) {
	t.Setenv("FOO", "kept")
	t.Setenv(envFileVar, filepath.Join(t.TempDir(), "gone"))
	if err := ApplyTUIEnv(); err != nil {
		t.Fatalf("missing file: %v", err)
	}
	if got := os.Getenv("FOO"); got != "kept" {
		t.Errorf("FOO = %q, env was not left as is", got)
	}
	if os.Geteuid() == 0 {
		return
	}
	p := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(p, []byte("FOO=x"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envFileVar, p)
	if err := ApplyTUIEnv(); err == nil {
		t.Error("unreadable file: want an error")
	}
}

// An AgentCommand's profile Env (ADR 0125) rides the 0600 env
// file on top of the launcher's env, so hitl-run hands it to the agent at launch
// and resume alike, and the built-in pane's hitl-run gets no --profile.
func TestEnvFileCarriesProfileEnv(t *testing.T) {
	saved := os.Environ()
	t.Cleanup(func() {
		os.Clearenv()
		for _, kv := range saved {
			k, v, _ := strings.Cut(kv, "=")
			os.Setenv(k, v)
		}
	})
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("ANTHROPIC_BASE_URL", "https://launcher.example")
	path, err := writeEnvFile(map[string]string{"ANTHROPIC_BASE_URL": "https://api.deepseek.com/anthropic", "ANTHROPIC_AUTH_TOKEN": "sk-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("env file %s: %v %v, want 0600", path, fi, err)
	}
	if err := applyEnvFile(path); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"ANTHROPIC_BASE_URL": "https://api.deepseek.com/anthropic", "ANTHROPIC_AUTH_TOKEN": "sk-secret"} {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want the profile's %q", k, got, want)
		}
	}
	if pane := strings.Join(paneArgs("/abs/mugicrew", sessionTags{bd.Bead{ID: "g"}, "s", "", ""}, "/run/argv", "/run/env"), " "); strings.Contains(pane, "--profile") {
		t.Errorf("built-in pane carries a profile attr: %s", pane)
	}
}

// assertEnvArg: a launch or resume argv passes no -e (the env file rides the pane
// command, so it never sticks to the session), and no TMUX/TMUX_PANE anywhere.
func assertEnvArg(t *testing.T, args []string) {
	t.Helper()
	if slices.Index(args, "-e") >= 0 {
		t.Errorf("want no -e, got %v", args)
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
	path, err := writeEnvFile(nil)
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
	got := map[string]string{}
	runAgent = func([]string, <-chan struct{}) error {
		for _, k := range []string{"FOO", "TERM", "XDG_RUNTIME_DIR", "TMUX", envFileVar} {
			if v, ok := os.LookupEnv(k); ok {
				got[k] = v
			}
		}
		return nil
	}
	if rc := HitlRun([]string{"--bead=g", argvArg(t, "claude"), "--env-file=" + path}, meter(t)); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if want := map[string]string{"FOO": "fresh\nline2", "TERM": "tmux-256color", "XDG_RUNTIME_DIR": run}; !maps.Equal(got, want) {
		t.Errorf("agent env = %v, want %v", got, want)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("env file not deleted: %v", err)
	}

	// The file is gone now: a second run must refuse, never run the agent.
	*capt = nil
	ran := false
	runAgent = func([]string, <-chan struct{}) error { ran = true; return nil }
	if rc := HitlRun([]string{"--bead=g", argvArg(t, "claude"), "--env-file=" + path}, meter(t)); rc == 0 || ran {
		t.Errorf("missing env file: rc = %d, ran = %v; want a refusal", rc, ran)
	}
	if got := eventlogtest.Names(*capt); !slices.Equal(got, []string{"hitl.launched", "hitl.failed"}) {
		t.Errorf("events = %v, want [hitl.launched hitl.failed]", got)
	}
}

// argvArg is the --argv-file flag handing argv to a test's HitlRun.
func argvArg(t *testing.T, argv ...string) string {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	path, err := writeArgvFile(argv)
	if err != nil {
		t.Fatal(err)
	}
	return "--argv-file=" + path
}

// The agent argv rides a 0600 file, not the tmux command: a prompt over tmux's
// 16 KiB cap, with newlines, quotes and non-ASCII text, reaches the agent
// unchanged, and hitl-run deletes the file once read.
func TestArgvFileRoundTrip(t *testing.T) {
	origRun := runAgent
	defer func() { runAgent = origRun }()
	stubDrafts(t, nil)
	prompt := strings.Repeat("line \"one\"\n'two' ünï 日本語\n", 1500)
	want := []string{"claude", "--settings", `{"a":"b"}`, prompt, "end;"}
	run := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", run)
	path, err := writeArgvFile(want)
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 || filepath.Dir(path) != run {
		t.Fatalf("argv file %s: %v %v, want 0600 under %s", path, fi, err, run)
	}
	var got []string
	runAgent = func(argv []string, _ <-chan struct{}) error { got = argv; return nil }
	if rc := HitlRun([]string{"--bead=g", "--argv-file=" + path}, meter(t)); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if !slices.Equal(got, want) {
		t.Errorf("agent argv differs from the written argv (%d vs %d args)", len(got), len(want))
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("argv file not deleted: %v", err)
	}
}

// A missing, empty or absent --argv-file never runs the agent: the first two
// record hitl.failed, the last is a usage error.
func TestHitlRunArgvFileFailure(t *testing.T) {
	origRun := runAgent
	defer func() { runAgent = origRun }()
	stubDrafts(t, nil)
	empty := filepath.Join(t.TempDir(), "empty")
	os.WriteFile(empty, nil, 0o600)
	for name, tc := range map[string]struct {
		arg    string
		rc     int
		events []string
	}{
		"missing": {"--argv-file=" + filepath.Join(t.TempDir(), "gone"), 1, []string{"hitl.launched", "hitl.failed"}},
		"empty":   {"--argv-file=" + empty, 1, []string{"hitl.launched", "hitl.failed"}},
		"absent":  {"--session=s", 2, nil},
	} {
		t.Run(name, func(t *testing.T) {
			capt := eventlogtest.Capture(t)
			ran := false
			runAgent = func([]string, <-chan struct{}) error { ran = true; return nil }
			if rc := HitlRun([]string{"--bead=g", tc.arg}, meter(t)); rc != tc.rc || ran {
				t.Errorf("rc = %d, ran = %v; want %d and no agent", rc, ran, tc.rc)
			}
			if got := eventlogtest.Names(*capt); !slices.Equal(got, tc.events) {
				t.Errorf("events = %v, want %v", got, tc.events)
			}
		})
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
	if _, err := TUIAlive(""); err == nil {
		t.Error("TUIAlive: an unreadable socket must be an error, not no TUI")
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

type okResume struct{ failResume }

func (okResume) ResumeCommand(string, bool) (contract.AgentCommand, error) {
	return contract.AgentCommand{SessionID: "s1", Argv: []string{"claude", "--resume", "s1"}}, nil
}

// readOnlyResume records the readOnly bit Resume asks its harness for.
type readOnlyResume struct {
	failResume
	got *bool
}

func (r readOnlyResume) ResumeCommand(_ string, ro bool) (contract.AgentCommand, error) {
	*r.got = ro
	return contract.AgentCommand{}, errors.New("stop")
}

// Resume takes read-only from the bead kind's registry row: a discussion asks
// for a read-only resume, another kind does not.
func TestResumeReadOnlyFollowsKind(t *testing.T) {
	stubServer(t)
	for kind, want := range map[contract.Kind]bool{contract.KindDiscussion: true, contract.KindSignal: false} {
		var got bool
		Resume(bd.Bead{ID: "g", Kind: kind}, Info{Session: "s1"}, readOnlyResume{got: &got})
		if got != want {
			t.Errorf("%s: readOnly = %v, want %v", kind, got, want)
		}
	}
}

// A failed tmux call leaves neither the env nor the argv handoff behind, at
// launch and at resume alike: no pane took them.
func TestTmuxFailureLeavesNoHandoffFiles(t *testing.T) {
	stubServer(t)
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(bin, "tmux"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	run := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", run)
	stubDrafts(t, nil)
	origRoot := git.RepoRoot
	defer func() { git.RepoRoot = origRoot }()
	git.RepoRoot = func() (string, error) { return "/repo/root", nil }
	b := bd.Bead{ID: "ws-sig", Kind: contract.KindSignal}
	if err := Start(b, contract.AgentCommand{Argv: []string{"claude"}}); err == nil {
		t.Error("Start: want tmux's error")
	}
	if left, _ := os.ReadDir(run); len(left) != 0 {
		t.Errorf("Start left handoff files: %v", left)
	}
	if err := Resume(b, Info{Session: "s1"}, okResume{}); err == nil {
		t.Error("Resume: want tmux's error")
	}
	if left, _ := os.ReadDir(run); len(left) != 0 {
		t.Errorf("Resume left handoff files: %v", left)
	}
}

// parseList reads list-sessions -F listFormat rows: a live pane is alive, a dead
// one carries its exit status or the signal that killed it (the pane, or the agent
// under hitl-run, which exits 128+n for it), and an untagged
// session is skipped (ADR 0036 §1).
func TestParseList(t *testing.T) {
	out := "ws-1.2\tsid-a\t/wt/a\t0\t\t\t\t\t\t\n" +
		"ws-3\tsid-b\t\t1\t2\t\t\t\t\t\n" +
		"ws-4\tsid-c\t\t1\t\t9\t\t\t\t\n" +
		"ws-5\tsid-d\t\t1\t143\t\t\t\t\t\n" +
		"ws-6\tsid-e\t\t1\t255\t\t\t\t\t\n" +
		"ws-7\tsid-f\t\t0\t\t\tturn\t\t\t\n" +
		"ws-8\tsid-g\t\t0\t\t\task\t\t\t\n" +
		"ws-9\tsid-h\t\t0\t\t\tbogus\t\t\t\n" +
		"ws-10\tsid-i\t\t0\t\t\tturn\t1\t\t\n" +
		"ws-11\tsid-j\t\t1\t1\t\t\t1\t\t\n" +
		"ws-12\tsid-k\t\t0\t\t\tturn\t\t1760000000\t\n" +
		"ws-13\tsid-l\t\t1\t1\t\t\t\t\tdeepseek\n" +
		"\t\t\t0\t\t\t\t\t\t\n"
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
		// The Agent Profile the session runs on, @profile (ADR 0122 §6); empty is the built-in.
		"ws-13": {Session: "sid-l", State: Exited, RC: 1, Profile: "deepseek"},
	}
	if !maps.Equal(got, want) {
		t.Errorf("parseList = %+v, want %+v", got, want)
	}
	// A live TUI session carries no @bead: not listed, not counted (ADR 0065 §3).
	if n := Live(parseList(out + "\t\t\t0\t\t\t\t\t\t\n")); n != 6 {
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

// TUIAlive reads the repo's private server (ADR 0127 §2): any tui-* session but
// the caller's own is a TUI alive; bead sessions don't count, and no server is none.
func TestTUIAliveRealTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	sock := stubServer(t)
	t.Cleanup(func() { tmuxCmd(sock, "kill-server").Run() })
	alive := func(exclude string) bool {
		t.Helper()
		ok, err := TUIAlive(exclude)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if alive("") {
		t.Error("no server reads as a TUI alive")
	}
	if out, err := tmuxCmd(sock, "-f", "/dev/null", "new-session", "-d", "-s", "mc-a_1").CombinedOutput(); err != nil {
		t.Fatalf("tmux new-session: %v: %s", err, out)
	}
	if alive("") {
		t.Error("a bead session reads as a TUI alive")
	}
	if out, err := tmuxCmd(sock, "new-session", "-d", "-s", "tui-7").CombinedOutput(); err != nil {
		t.Fatalf("tmux new-session: %v: %s", err, out)
	}
	if !alive("") || !alive("tui-8") {
		t.Error("tui-7 present does not read as a TUI alive")
	}
	if alive("tui-7") {
		t.Error("the caller's own tui-7 counts as a TUI alive")
	}

	// OwnSession names the TUI's own session from $TMUX_PANE; outside a pane, none.
	pane, err := tmuxCmd(sock, "display-message", "-p", "-t", "=tui-7:", "#{pane_id}").Output()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_PANE", strings.TrimSpace(string(pane)))
	t.Setenv("TMUX", "/elsewhere,1,0")
	if s, err := OwnSession(); err != nil || s != "" {
		t.Errorf("OwnSession in another server's pane = %q, %v; want none", s, err)
	}
	t.Setenv("TMUX", sock+",1,0")
	if s, err := OwnSession(); err != nil || s != "tui-7" {
		t.Errorf("OwnSession = %q, %v; want tui-7", s, err)
	}
	t.Setenv("TMUX_PANE", "")
	if s, err := OwnSession(); err != nil || s != "" {
		t.Errorf("OwnSession outside a pane = %q, %v; want none", s, err)
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
	if rc := HitlRun([]string{"--resume", "--bead=g", "--session=s1", "--worktree=/wt", argvArg(t, "claude", "--resume", "s1")}, meter(t)); rc != 0 {
		t.Errorf("rc = %d", rc)
	}
	if got := eventlogtest.Names(*capt); !slices.Equal(got, []string{"hitl.resumed", "hitl.exited"}) {
		t.Errorf("events = %v, want [hitl.resumed hitl.exited]", got)
	}
	if (*capt)[0].Attrs["session"] != "s1" || removed != "/wt" {
		t.Errorf("resumed attrs %v, removed %q", (*capt)[0].Attrs, removed)
	}

	pane := resumePane(paneArgs("/abs/mugicrew", sessionTags{bd.Bead{ID: "g"}, "s1", "/wt", ""}, "/run/argv", "/run/env"))
	args := respawnArgs(tmuxName("mc-a.2"), "/wt", pane, bd.Bead{ID: "mc-a.2", Title: "Grill #1"})
	assertEnvArg(t, args)
	got := strings.Join(args, " ")
	if want := "respawn-pane -t =mc-a_2: -c /wt -- /abs/mugicrew hitl-run --resume --bead=g --parent= --crewmate= --kind= --session=s1 --worktree=/wt --argv-file=/run/argv --env-file=/run/env ; "; !strings.HasPrefix(got, want) {
		t.Errorf("respawn argv =\n%s\nwant prefix\n%s", got, want)
	}
	// A resumed session gets the same bar and key table as a launch (ADR 0066 §3).
	assertBar(t, args, "○ — P0 · Grill ##1 · mc-a.2")
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
func (f failResume) ResumeCommand(string, bool) (contract.AgentCommand, error) {
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
	origDecl := declaredRepos
	defer func() { declaredRepos = origDecl }()
	declaredRepos = func(string) map[string]string { return nil } // a no-context Workspace: no child step
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
		"status-right": "F1 help · F6 back to TUI · /exit to finish", "key-table": "mchitl",
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

// An armed session's status-right is hitl-run's countdown (Spec D4): barsArgs
// skips that but still sets its status-left, and the unarmed ones' both; a
// discussion keeps its Robin form via the text the TUI hands in.
func TestBarsArgsSkipsArmed(t *testing.T) {
	armed := map[string]Info{"mc-b.1": {State: Alive, AutoExit: time.Unix(1760000000, 0)}, "mc-a": {State: Alive}}
	left := map[string]string{"mc-b.1": "◐ TASK P2 · B #1 · mc-b.1", "mc-a": "Why? · Robin discussion"}
	got := strings.Join(barsArgs(map[string]bool{"mc-b.1": true, "mc-a": false}, left, armed), " ")
	want := "set-option -t =mc-a: status-left Why? · Robin discussion ; " +
		"set-option -t =mc-a: status-right F1 help · F6 back to TUI · /exit to finish ; " +
		"set-option -t =mc-b_1: status-left ◐ TASK P2 · B ##1 · mc-b.1"
	if got != want {
		t.Errorf("barsArgs =\n %q\nwant\n %q", got, want)
	}
	if got := barsArgs(map[string]bool{"mc-b.1": false}, nil, armed); got != nil { // armed, no left text: nothing to set
		t.Errorf("barsArgs = %q, want nil", got)
	}
}

// The countdown bar reads M:SS, rounding a part second up (Spec FR14).
func TestAutoExitBar(t *testing.T) {
	for left, want := range map[time.Duration]string{
		105 * time.Second:                     "auto-exit in 1:45 · F1 to cancel",
		59*time.Second + 100*time.Millisecond: "auto-exit in 1:00 · F1 to cancel",
		9 * time.Second:                       "auto-exit in 0:09 · F1 to cancel",
		10 * time.Minute:                      "auto-exit in 10:00 · F1 to cancel",
		0:                                     "auto-exit in 0:00 · F1 to cancel",
	} {
		if got := autoExitBar(left); got != want {
			t.Errorf("autoExitBar(%v) = %q, want %q", left, got, want)
		}
	}
}

// The done flip's argv (ADR 0066 §4): an open bead's session gets the normal right
// bar, a closed one's the loud green/reversed ✓ line; one ;-joined call, id order.
func TestBarsArgs(t *testing.T) {
	got := strings.Join(barsArgs(map[string]bool{"mc-b.1": true, "mc-a": false}, nil, nil), " ")
	want := "set-option -t =mc-a: status-right F1 help · F6 back to TUI · /exit to finish ; " +
		"set-option -t =mc-b_1: status-right #[fg=green,reverse] ✓ mc-b.1 closed — /exit to finish · F6 back to TUI "
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

// DropDraftsWith carries its extra attrs on draft.deleted and the branch events.
func TestDropDraftsWithAddsAttrs(t *testing.T) {
	stubDrafts(t, nil)
	deleteDrafts = func(string) ([]string, []bd.BranchOutcome, error) {
		return []string{"mc-c"}, []bd.BranchOutcome{{Branch: "chart/mc-c"}}, nil
	}
	capt := eventlogtest.Capture(t)
	if err := DropDraftsWith(bd.Bead{ID: "mc-s", Kind: contract.KindSignal}, "target", "mc-t"); err != nil {
		t.Fatal(err)
	}
	for _, e := range *capt {
		if e.Attrs["target"] != "mc-t" || e.Attrs["bead"] != "mc-s" {
			t.Errorf("%s attrs = %v", e.Name, e.Attrs)
		}
	}
}

// A clean HITL exit on an effort tree runs the backstop before the tree goes
// (Spec D6, FR 20): integrated, the tree is removed and then effort/<id>
// deleted; a conflict keeps both for the human (the orphan confirm relaunches it,
// FR 40) and the wrapper still exits clean; a transient keeps the branch only;
// hitl.exited says which (stranded, kept). A grilling's effort integrates like any
// leaf, released first as still in_progress; a land step's detached tree never
// integrates; a failed run keeps all.
func TestHitlRunBackstopsEffortTree(t *testing.T) {
	origRun, origRemove, origRelease := runAgent, removeWorktree, releaseClaim
	defer func() { runAgent, removeWorktree, releaseClaim = origRun, origRemove, origRelease }()
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
			[]string{"publish sg", "release sg", "integrate", "remove", "delete effort/sg"}},
		{"land step's detached tree never integrates", contract.KindCheckpoint, func([]string, <-chan struct{}) error { return nil }, 0, 0, false, false,
			[]string{"publish sg", "release sg", "remove"}},
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
			releaseClaim = func(id string) error { *steps = append(*steps, "release "+id); return nil }
			runAgent = tc.agent
			removeWorktree = func(string) error { did = append(did, "remove"); return nil }
			if rc := HitlRun([]string{"--bead=sg", "--worktree=/tmp/mugicrew-wt-x/wt", argvArg(t, "claude")}, meter(t)); rc != tc.rc {
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

// Watch focuses the pane already tagged with the bead, else respawns a pane tagged for
// another bead, else splits -h 40%; exe and bead ride single-quoted (nested per level).
func TestWatchArgs(t *testing.T) {
	got := watchArgs("%3", "/opt/mu'gi/mugicrew", "mc-1")
	mine := "#{P:#{?#{==:#{@watchrun},mc-1},#{pane_id},}}"
	any := "#{P:#{?#{@watchrun},#{pane_id},}}"
	if len(got) != 7 || strings.Join(got[:5], " ") != "if-shell -F -t %3 "+mine {
		t.Fatalf("watchArgs = %q", got)
	}
	if want := "run-shell -t %3 -C 'select-pane -t " + mine + "'"; got[5] != want {
		t.Errorf("focus:\n got %s\nwant %s", got[5], want)
	}
	verb := "-e MUGICREW_WATCH_PANE=1 " + tmuxQuote("/opt/mu'gi/mugicrew") + " transcript 'mc-1'"
	respawn := "respawn-pane -k -t " + any + " " + verb + " ; set-option -p -t " + any + " @watchrun 'mc-1' ; select-pane -t " + any
	split := "split-window -h -l 40% -t %3 " + verb + " ; set-option -p @watchrun 'mc-1'"
	want := "if-shell -F -t %3 " + tmuxQuote(any) + " " + tmuxQuote("run-shell -t %3 -C "+tmuxQuote(respawn)) + " " + tmuxQuote(split)
	if got[6] != want {
		t.Errorf("reuse/split:\n got %s\nwant %s", got[6], want)
	}
	if q := tmuxQuote("a'b"); q != `'a'\''b'` {
		t.Errorf("tmuxQuote = %s", q)
	}
}

func TestWatchRefusesUnsafeIDs(t *testing.T) {
	t.Setenv("TMUX_PANE", "%1")
	if err := Watch("x'; kill-server #"); err == nil {
		t.Error("an id with tmux metacharacters was accepted")
	}
}

// A pane id is only trusted when $TMUX names this repo's private server.
func TestOwnPane(t *testing.T) {
	t.Setenv("TMUX_PANE", "%4")
	t.Setenv("TMUX", "/tmp/foreign,1,0")
	if got := ownPane("/tmp/mine"); got != "" {
		t.Errorf("foreign server's pane = %q, want none", got)
	}
	t.Setenv("TMUX", "/tmp/mine,1,0")
	if got := ownPane("/tmp/mine"); got != "%4" {
		t.Errorf("own pane = %q, want %%4", got)
	}
}

// DropOrphan on a nested effort removes the child worktree before the host, then
// prunes the Workspace and each declared child (Spec FR 53).
func TestDropOrphanNested(t *testing.T) {
	origRemove, origPrune, origDecl := gitRemoveWorktree, pruneWorktrees, declaredRepos
	origChild, origPruneChild, origRoot := removeChildWorktree, pruneChild, repoRoot
	defer func() {
		gitRemoveWorktree, pruneWorktrees, declaredRepos = origRemove, origPrune, origDecl
		removeChildWorktree, pruneChild, repoRoot = origChild, origPruneChild, origRoot
	}()
	root := t.TempDir()
	repoRoot = func() (string, error) { return root, nil }
	wt := filepath.Join(t.TempDir(), "mugicrew-wt-1", "wt")
	for dir, git := range map[string]string{"sub": "gitdir: x", "plain": ""} {
		if err := os.MkdirAll(filepath.Join(wt, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if git != "" {
			os.WriteFile(filepath.Join(wt, dir, ".git"), []byte(git), 0o644)
		}
	}
	var steps []string
	declaredRepos = func(string) map[string]string { return map[string]string{"a": "plain", "b": "sub"} }
	removeChildWorktree = func(r git.Repo, p string) error {
		steps = append(steps, "remove child "+string(r)+" "+p)
		return nil
	}
	gitRemoveWorktree = func(string) error { steps = append(steps, "remove host"); return nil }
	pruneWorktrees = func() error { steps = append(steps, "prune host"); return nil }
	pruneChild = func(r git.Repo) { steps = append(steps, "prune "+string(r)) }
	if err := DropOrphan(wt); err != nil {
		t.Fatal(err)
	}
	want := []string{"remove child " + filepath.Join(root, "sub") + " " + filepath.Join(wt, "sub"), "remove host", "prune host",
		"prune " + filepath.Join(root, "plain"), "prune " + filepath.Join(root, "sub")}
	if !slices.Equal(steps, want) {
		t.Errorf("steps = %q\nwant    %q", steps, want)
	}
}

// A child remove that fails, or a root that cannot be found, stops DropOrphan
// before the host tree is removed.
func TestDropOrphanNestedStopsOnError(t *testing.T) {
	origRemove, origDecl, origChild, origRoot := gitRemoveWorktree, declaredRepos, removeChildWorktree, repoRoot
	defer func() {
		gitRemoveWorktree, declaredRepos, removeChildWorktree, repoRoot = origRemove, origDecl, origChild, origRoot
	}()
	wt := filepath.Join(t.TempDir(), "mugicrew-wt-1", "wt")
	if err := os.MkdirAll(filepath.Join(wt, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "sub", ".git"), []byte("gitdir: x"), 0o644); err != nil {
		t.Fatal(err)
	}
	declaredRepos = func(string) map[string]string { return map[string]string{"b": "sub"} }
	removed := false
	gitRemoveWorktree = func(string) error { removed = true; return nil }
	repoRoot = func() (string, error) { return "", errors.New("no root") }
	if err := DropOrphan(wt); err == nil {
		t.Error("root failure: want error")
	}
	repoRoot = func() (string, error) { return t.TempDir(), nil }
	removeChildWorktree = func(git.Repo, string) error { return errors.New("busy") }
	if err := DropOrphan(wt); err == nil {
		t.Error("child remove failure: want error")
	}
	if removed {
		t.Error("host removed despite the failure")
	}
}

// A discussion's bar reads its title and Robin, not its id (ADR 0194 §2).
func TestDiscussionBar(t *testing.T) {
	for _, kv := range bar(bd.Bead{ID: "mc-d", Kind: contract.KindDiscussion, Title: "Why Franky?"}) {
		if kv[0] == "status-left" && kv[1] != "Why Franky? · Robin discussion" {
			t.Errorf("status-left = %q", kv[1])
		}
	}
}

func TestCheckupBar(t *testing.T) {
	for _, kv := range bar(bd.Bead{ID: "mc-c", Kind: contract.KindCheckup, Title: "Fix edge"}) {
		if kv[0] == "status-left" && kv[1] != "Fix edge · Chopper checkup" {
			t.Errorf("status-left = %q", kv[1])
		}
	}
}

// The launch-time bar is the launched bead's plain SBRN, no dot slot, its own
// status glyph.
func TestLaunchBarIsPlainSBRN(t *testing.T) {
	got := plainSBRN(bd.Bead{ID: "mc-a.2", Kind: contract.KindTask, Title: "Fix it", Status: "in_progress", Priority: 1})
	if want := "◐ TASK P1 · Fix it · mc-a.2"; got != want {
		t.Errorf("plainSBRN = %q, want %q", got, want)
	}
}

// The bead pane names the executable by absolute path as one quoted word, whatever
// shell or tmux metacharacters it holds, and takes its width inside the pane, in
// both the side split and the below-the-agent split.
func TestBeadSplitQuotesExe(t *testing.T) {
	want := `"'/a b/it'\\''s \$x\"y\\z/mugicrew' bead-detail `
	for name, split := range map[string]func(string) string{"side": beadSplitSide, "below": beadSplitBelow} {
		got := split(`/a b/it's $x"y\z/mugicrew`)
		if !strings.Contains(got, want) {
			t.Errorf("beadSplit %s = %q, want it to contain %q", name, got, want)
		}
		for _, w := range []string{`#{pane_width}`, `#{@bead}`, "2>&1 | less -R", "set-option -p @beadpane 1", "-l 40%"} {
			if !strings.Contains(got, w) {
				t.Errorf("beadSplit %s lacks %q: %q", name, w, got)
			}
		}
	}
	if side := beadSplitSide("x"); !strings.HasPrefix(side, "split-window -h -l 40% ") || strings.Contains(side, "{top-left}") {
		t.Errorf("side split = %q, want split-window -h -l 40%% of the active pane", side)
	}
	if below := beadSplitBelow("x"); !strings.HasPrefix(below, "split-window -t '{top-left}' -v -l 40% ") {
		t.Errorf("below split = %q, want split-window -t '{top-left}' -v -l 40%%", below)
	}
}

// F10 is a three-way choice made tmux-side: focus an open bead pane, else split
// below the agent pane when a @protopane pane exists, else split to the side. The
// F10 binding and the F1 menu's Bead pane entry carry the same command.
func TestF10BeadCommandThreeWay(t *testing.T) {
	cmd := beadCmd()
	if len(cmd) != 5 || cmd[0] != "if-shell" || cmd[1] != "-F" || cmd[2] != beadPane || cmd[3] != focusBead {
		t.Fatalf("beadCmd = %q, want if-shell -F beadPane focusBead <else>", cmd)
	}
	rest := cmd[4]
	proto := "#{P:#{?#{@protopane},#{pane_id},}}"
	want := cmdline("if-shell", "-F", proto, beadSplitBelow(beadExe()), beadSplitSide(beadExe()))
	if rest != want {
		t.Errorf("else branch = %q, want %q", rest, want)
	}
	pre := strings.Join(preamble(), " ")
	if !strings.Contains(pre, "bind-key -T mchitl F10 "+strings.Join(cmd, " ")) {
		t.Errorf("F10 binding lacks the else command %q: %q", rest, pre)
	}
	menu := strings.Join(menuArgv(false, false), " ")
	if !strings.Contains(menu, literal(cmdline(cmd...))) {
		t.Errorf("F1 menu Bead pane entry does not carry the F10 command: %q", menu)
	}
}

// hitl.exited carries continued_in only when the source transcript ends in a
// continued-in record, and the source's own spend (Spec mc-11b8 D3, FR4).
// It needs a git checkout: the follower's event log path comes from eventlog.Resolve.
func TestHitlRunRecordsContinuedIn(t *testing.T) {
	origRun, origRemove, origStart := runAgent, removeWorktree, startFollower
	defer func() { runAgent, removeWorktree, startFollower = origRun, origRemove, origStart }()
	var spawned [][]string
	startFollower = func(_ string, args []string) error { spawned = append(spawned, args); return nil }
	stubDrafts(t, nil)
	removeWorktree = func(string) error { return nil }
	// Handoff resolves the event log from the cwd's repo; the nix sandbox has none.
	t.Chdir(t.TempDir())
	if out, err := exec.Command("git", "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	cs := `{"type":"cost-state","totalCostUSD":1.5,"totalAPIDuration":3000}` + "\n"
	ci := `{"type":"continued-in","timestamp":"t","sessionId":"sess-1","continuedInSessionId":"dest-9"}` + "\n"
	for name, body := range map[string]string{"handoff": cs + cs + ci, "no handoff": cs} {
		t.Run(name, func(t *testing.T) {
			capt := eventlogtest.Capture(t)
			m := meter(t)
			dir := os.Getenv("CLAUDE_CONFIG_DIR") + "/projects/-wt"
			runAgent = func([]string, <-chan struct{}) error {
				os.MkdirAll(dir, 0o755)
				return os.WriteFile(dir+"/sess-1.jsonl", []byte(body), 0o644)
			}
			spawned = nil
			HitlRun([]string{"--bead=g", "--session=sess-1", "--worktree=/tmp/mugicrew-wt-x/wt", argvArg(t, "claude")}, m)
			evs := *capt
			a := evs[len(evs)-1].Attrs
			if (len(spawned) == 1) != (name == "handoff") || len(spawned) > 1 {
				t.Fatalf("follower spawns = %v", spawned)
			}
			if name == "handoff" {
				got := strings.Join(spawned[0], " ")
				for _, want := range []string{"hitl-follow", "--bead=g", "--session=sess-1", "--continued_in=dest-9", `--baseline={"Known":true,"USD":1.5`, "--exited_at=", "--events=/"} {
					if !strings.Contains(got, want) {
						t.Errorf("follower argv %q lacks %q", got, want)
					}
				}
			}
			if a["cost_usd"] != 1.5 {
				t.Errorf("cost_usd = %v, want 1.5", a["cost_usd"])
			}
			if got, _ := a["continued_in"].(string); (got == "dest-9") != (name == "handoff") || (name == "handoff" && got == "") {
				t.Errorf("continued_in = %v", a["continued_in"])
			}
		})
	}
}

// The F1 menu carries -M only when asked, armed or not (ADR 0303).
func TestMenuArgvMouse(t *testing.T) {
	for _, armed := range []bool{false, true} {
		if got := slices.Contains(menuArgv(armed, true), "-M"); !got {
			t.Errorf("menuArgv(%v, true) lacks -M", armed)
		}
		if got := slices.Contains(menuArgv(armed, false), "-M"); got {
			t.Errorf("menuArgv(%v, false) has -M", armed)
		}
	}
}

func stubMenuMouse(t *testing.T, v bool) {
	old := menuMouse
	menuMouse = func() bool { return v }
	t.Cleanup(func() { menuMouse = old })
}

func TestPreambleMouseGate(t *testing.T) {
	stubMenuMouse(t, true)
	if pre := strings.Join(preamble(), " "); strings.Count(pre, "'-M'") != 2 {
		t.Errorf("preamble on tmux >= 3.5 lacks -M on both menus: %q", pre)
	}
	stubMenuMouse(t, false)
	if pre := strings.Join(preamble(), " "); !strings.Contains(pre, "'display-menu'") || strings.Contains(pre, "'-M'") {
		t.Errorf("preamble on tmux < 3.5 or unknown has -M: %q", pre)
	}
}

func TestMenuMouseFrom(t *testing.T) {
	for _, c := range []struct {
		out  string
		err  error
		want bool
	}{
		{"tmux 3.4\n", nil, false},
		{"tmux 3.5\n", nil, true},
		{"tmux 3.5a\n", nil, true},
		{"tmux 3.7c\n", nil, true},
		{"tmux 4.0\n", nil, true},
		{"tmux next-3.6\n", nil, false},
		{"tmux master\n", nil, false},
		{"", nil, false},
		{"tmux 3.5\n", errors.New("boom"), false},
	} {
		if got := menuMouseFrom(c.out, c.err); got != c.want {
			t.Errorf("menuMouseFrom(%q, %v) = %v, want %v", c.out, c.err, got, c.want)
		}
	}
}
