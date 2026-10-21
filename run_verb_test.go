package main

import (
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/dispatch"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/liveness"
)

// runStub scripts the host's seams: the bead the re-read returns, Dispatch's
// outcome, and what the bd writes did.
type runStub struct {
	bead        bd.Bead
	showErr     error
	outcome     dispatch.Outcome
	dispatchErr error
	unclaimErr  error
	unpauseErr  error
	block       chan struct{} // Dispatch waits on it when set

	unclaimed, unpaused, paused []string
	heldAtUnpause               []bool
	dispatched                  int
	effort                      string
	hold                        *liveness.Hold
}

func (s *runStub) install(t *testing.T) {
	t.Helper()
	oshow, ounclaim, opause, ounpause, odisp, oadopt, ocrew := runShow, runUnclaim, runPause, runUnpause, runDispatch, runAdopt, runCrew
	t.Cleanup(func() {
		runShow, runUnclaim, runPause, runUnpause, runDispatch, runAdopt, runCrew = oshow, ounclaim, opause, ounpause, odisp, oadopt, ocrew
	})
	runShow = func(string) (bd.Bead, error) { return s.bead, s.showErr }
	runUnclaim = func(id string) error { s.unclaimed = append(s.unclaimed, id); return s.unclaimErr }
	runPause = func(id, cause string) error { s.paused = append(s.paused, id+":"+cause); return nil }
	runUnpause = func(id, cause string) error {
		s.unpaused = append(s.unpaused, cause)
		s.heldAtUnpause = append(s.heldAtUnpause, liveness.Held(id))
		return s.unpauseErr
	}
	runAdopt = func(fd int, id string) (*liveness.Hold, error) {
		if fd != 3 {
			return nil, errors.New("bad fd")
		}
		return s.hold, nil
	}
	runCrew = func() map[string]contract.Crewmate {
		return map[string]contract.Crewmate{"nami": nil, "zoro": nil, "franky": nil}
	}
	runDispatch = func(_ contract.Ctx, _ contract.Crewmate, _ bd.Bead, out io.Writer, id string, _ dispatch.InflightFunc) (dispatch.Outcome, error) {
		s.dispatched++
		s.effort = id
		if s.block != nil {
			<-s.block
		}
		return s.outcome, s.dispatchErr
	}
}

func newRunStub(t *testing.T, labels ...string) *runStub {
	t.Helper()
	t.Setenv("BEADS_DIR", t.TempDir())
	b := bd.Bead{ID: "mc-r", Kind: contract.KindBuild, Status: "in_progress", Labels: labels}
	hold, err := liveness.Acquire(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hold.Release)
	s := &runStub{bead: b, hold: hold}
	s.install(t)
	return s
}

func TestRunHostExitCodes(t *testing.T) {
	foreign, failing := "paused:"+bd.PausedForeignHold, "paused:"+bd.PausedFailing
	boom, liftBoom := errors.New("boom"), errors.New("lift boom")
	for _, tc := range []struct {
		name       string
		labels     []string
		argv       []string
		showErr    error
		outcome    dispatch.Outcome
		dispErr    error
		unclaimErr error
		unpauseErr error
		code       int
		unclaimed  bool
		unpaused   []string
		events     []string
		lastLine   string // substring of the output's last line
		dispatched bool
	}{
		{name: "clean", code: 0, dispatched: true},
		{name: "error", dispErr: boom, code: 1, dispatched: true},
		{name: "aborted", outcome: dispatch.Outcome{Aborted: true}, code: 1, dispatched: true},
		{name: "stranded", outcome: dispatch.Outcome{Stranded: true}, code: 1, dispatched: true},
		{name: "limited", outcome: dispatch.Outcome{Limit: &contract.Limit{}}, code: 1, dispatched: true},
		{name: "busy", outcome: dispatch.Outcome{Cut: dispatch.CutBusy, Branch: "effort/mc-r"}, code: 11, lastLine: "effort/mc-r", dispatched: true},
		{name: "held", outcome: dispatch.Outcome{Cut: dispatch.CutHeld, Branch: "effort/mc-r", Holder: git.Holder{Path: "/x/tree"}}, code: 11, lastLine: "/x/tree", dispatched: true},
		{name: "busy unclaim errors", outcome: dispatch.Outcome{Cut: dispatch.CutBusy}, dispErr: boom, code: 1, dispatched: true},
		{name: "foreign-hold lifted on a cut", labels: []string{foreign}, outcome: dispatch.Outcome{Cut: dispatch.CutOK, Branch: "effort/mc-r"}, code: 0, unpaused: []string{bd.PausedForeignHold}, events: []string{"effort.resumed"}, dispatched: true},
		{name: "foreign-hold kept on a refusal", labels: []string{foreign}, outcome: dispatch.Outcome{Cut: dispatch.CutHeld}, code: 11, lastLine: "held", dispatched: true},
		{name: "failing lifted when clean", labels: []string{failing}, code: 0, unpaused: []string{bd.PausedFailing}, events: []string{"effort.resumed"}, dispatched: true},
		{name: "failing kept when the effort errs", labels: []string{failing}, dispErr: boom, code: 1, dispatched: true},
		{name: "lift error", labels: []string{failing}, unpauseErr: liftBoom, code: 12, unpaused: []string{bd.PausedFailing}, lastLine: bd.PausedFailing, dispatched: true},
		{name: "failed dispatch and lift error", labels: []string{foreign}, outcome: dispatch.Outcome{Cut: dispatch.CutOK}, dispErr: boom, unpauseErr: liftBoom, code: 1, unpaused: []string{bd.PausedForeignHold}, dispatched: true},
		{name: "bad lock-fd", argv: []string{"mc-r", "--lock-fd=9"}, code: 10, unclaimed: true, events: []string{"run.failed", "claim.released"}},
		{name: "unparseable lock-fd", argv: []string{"mc-r", "--lock-fd=x"}, code: 10, unclaimed: true, events: []string{"run.failed", "claim.released"}},
		{name: "failed re-read", showErr: boom, code: 10, unclaimed: true, events: []string{"run.failed", "claim.released"}},
		{name: "failed re-read and unclaim", showErr: boom, unclaimErr: liftBoom, code: 10, unclaimed: true, events: []string{"run.failed", "claim.failed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newRunStub(t, tc.labels...)
			s.showErr, s.outcome, s.dispatchErr, s.unclaimErr, s.unpauseErr = tc.showErr, tc.outcome, tc.dispErr, tc.unclaimErr, tc.unpauseErr
			evs := eventlogtest.Capture(t)
			argv := tc.argv
			if argv == nil {
				argv = []string{"mc-r", "--lock-fd=3"}
			}
			var out strings.Builder
			if code := runHost(argv, make(chan os.Signal), &out); code != tc.code {
				t.Errorf("exit = %d; want %d (output %q)", code, tc.code, out.String())
			}
			if (s.dispatched == 1) != tc.dispatched {
				t.Errorf("dispatched %d times; want dispatched=%v", s.dispatched, tc.dispatched)
			}
			if got := len(s.unclaimed) == 1; got != tc.unclaimed {
				t.Errorf("host unclaimed = %v; want %v", s.unclaimed, tc.unclaimed)
			}
			if strings.Join(s.unpaused, ",") != strings.Join(tc.unpaused, ",") {
				t.Errorf("lifted %v; want %v", s.unpaused, tc.unpaused)
			}
			for _, h := range s.heldAtUnpause {
				if !h {
					t.Error("hold released before a pause lift")
				}
			}
			if liveness.Held(s.bead.ID) && tc.argv == nil { // a refused fd was never adopted: the stub still holds it
				t.Error("hold still held after the host returned")
			}
			if tc.lastLine != "" {
				lines := strings.Split(strings.TrimSpace(out.String()), "\n")
				if last := lines[len(lines)-1]; !strings.Contains(last, tc.lastLine) {
					t.Errorf("last line %q lacks %q", last, tc.lastLine)
				}
			}
			var got []string
			effort := map[string]bool{}
			for _, e := range *evs {
				got = append(got, e.Name)
				if id, _ := e.Attrs["effort"].(string); id != "" {
					effort[id] = true
				}
			}
			for _, want := range tc.events {
				found := false
				for _, g := range got {
					found = found || g == want
				}
				if !found {
					t.Errorf("events %v lack %s", got, want)
				}
			}
			if len(effort) > 1 {
				t.Errorf("events carry %d effort ids %v; want one", len(effort), effort)
			}
			if s.dispatched == 1 && !effort[s.effort] && len(tc.events) > 0 {
				t.Errorf("events carry effort %v; Dispatch got %q", effort, s.effort)
			}
		})
	}
}

// A pre-Dispatch failure's run.failed is an Error event carrying the bead and an effort id.
func TestRunHostPreDispatchFailureIsRunFailed(t *testing.T) {
	s := newRunStub(t)
	s.showErr = errors.New("boom")
	evs := eventlogtest.Capture(t)
	runHost([]string{"mc-r", "--lock-fd=3"}, make(chan os.Signal), io.Discard)
	for _, e := range *evs {
		if e.Name == "run.failed" {
			if e.Level.String() != "ERROR" || e.Attrs["bead"] != "mc-r" || e.Attrs["effort"] == "" || e.Attrs["err"] == nil {
				t.Errorf("run.failed = %+v; want Error with bead, effort, err", e)
			}
			return
		}
	}
	t.Error("no run.failed")
}

// The first signal is absorbed; the second abandons the one effort and exits 13.
func TestRunHostSecondSignalAbandons(t *testing.T) {
	s := newRunStub(t)
	s.block = make(chan struct{})
	defer close(s.block)
	evs := eventlogtest.Capture(t)
	sig := make(chan os.Signal, 2)
	done := make(chan int, 1)
	go func() { done <- runHost([]string{"mc-r", "--lock-fd=3"}, sig, io.Discard) }()
	for s.dispatched == 0 {
		time.Sleep(time.Millisecond)
	}
	sig <- syscall.SIGTERM
	select {
	case code := <-done:
		t.Fatalf("host exited %d on the first signal", code)
	case <-time.After(50 * time.Millisecond):
	}
	sig <- syscall.SIGINT
	select {
	case code := <-done:
		if code != 13 {
			t.Errorf("exit = %d; want 13", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("host did not exit on the second signal")
	}
	if len(s.paused) != 1 || s.paused[0] != "mc-r:"+bd.PausedAbandoned {
		t.Errorf("paused = %v; want mc-r:abandoned", s.paused)
	}
	var abandoned bool
	for _, e := range *evs {
		if e.Name == "effort.abandoned" {
			abandoned = e.Attrs["effort"] == s.effort && e.Attrs["bead"] == "mc-r"
		}
	}
	if !abandoned {
		t.Errorf("no effort.abandoned for effort %q", s.effort)
	}
	if liveness.Held("mc-r") {
		t.Error("hold still held after abandon")
	}
}

// SIGHUP is ignored, so a host outlives its starter's hangup.
func TestRunSignalsIgnoreSIGHUP(t *testing.T) {
	sig, stop := runSignals()
	defer stop()
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-sig:
		t.Errorf("SIGHUP reached the host as %v", s)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestRunIsHiddenFromUsage(t *testing.T) {
	if strings.Contains(usage, "\n  mugicrew run") || strings.Contains(usage, "--lock-fd=") && strings.Contains(usage, "mugicrew run") {
		t.Error("usage lists the hidden run verb")
	}
}
