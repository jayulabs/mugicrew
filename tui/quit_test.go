package tui

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/dispatch"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
)

var (
	keyQ     = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}
	keyY     = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}
	keyCtrlC = tea.KeyMsg{Type: tea.KeyCtrlC}
)

func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func withRuns(titles ...string) Model {
	m := New(nil, nil)
	m.w = 200
	for i, t := range titles {
		id := string(rune('a' + i))
		m.runs = append(m.runs, dispatch.Inflight{ID: "e" + id, Bead: bd.Bead{ID: "r" + id, Kind: contract.KindResearch, Title: t}})
	}
	return m
}

// With no run-now effort in flight, q and ctrl+c quit at once (Spec FR48).
func TestQuitWithNoRunsQuits(t *testing.T) {
	for _, k := range []tea.KeyMsg{keyQ, keyCtrlC} {
		if _, cmd := New(nil, nil).Update(k); !quits(cmd) {
			t.Errorf("%s with 0 in flight must quit", k)
		}
	}
}

// The in-flight count rises on a successful claim only, and falls on runDoneMsg.
func TestRunNowCountsInFlight(t *testing.T) {
	origReady, origClaim, origDisp, origIP := readyFrontier, claimBead, runDispatch, inProgressBeads
	t.Cleanup(func() {
		readyFrontier, claimBead, runDispatch, inProgressBeads = origReady, origClaim, origDisp, origIP
	})
	r1 := bd.Bead{ID: "r1", Kind: contract.KindResearch, Parent: "c", Status: "open", Title: "One"}
	r2 := bd.Bead{ID: "r2", Kind: contract.KindResearch, Parent: "c", Status: "open", Title: "Two"}
	readyFrontier = func() ([]bd.Bead, error) { return []bd.Bead{r1, r2}, nil }
	inProgressBeads = func() ([]bd.Bead, error) { return nil, nil }
	var gotEffort string
	runDispatch = func(_ contract.Ctx, _ contract.Crewmate, _ bd.Bead, _ io.Writer, id string, _ dispatch.InflightFunc) (dispatch.Outcome, error) {
		gotEffort = id
		return dispatch.Outcome{}, nil
	}
	claimErr := errors.New("taken")
	claimBead = func(b bd.Bead) error {
		if b.ID == "r2" {
			return claimErr
		}
		return nil
	}

	m := New(nil, map[string]contract.Crewmate{"nami": &recordCrew{}})
	m.beads, m.selGraph = []bd.Bead{r1, r2}, "r1"
	next, cmd := settleRunNow(m.runNow())
	m = next.(Model)
	m.selGraph = "r2"
	next, _ = settleRunNow(m.runNow())
	if m = next.(Model); len(m.runs) != 1 || m.runs[0].Bead.ID != "r1" {
		t.Fatalf("runs = %+v; want only the claimed r1", m.runs)
	}
	done := cmd().(runDoneMsg)
	if gotEffort != m.runs[0].ID || done.run != gotEffort {
		t.Errorf("dispatch effort %q, done %q; want the tracked %q", gotEffort, done.run, m.runs[0].ID)
	}
	next, _ = m.Update(done)
	if m = next.(Model); len(m.runs) != 0 {
		t.Errorf("runDoneMsg must drop the effort, runs %+v", m.runs)
	}
}

// q with efforts in flight opens the confirm naming them; n and esc cancel.
func TestQuitConfirm(t *testing.T) {
	for _, k := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("n")}, {Type: tea.KeyEsc}} {
		next, cmd := withRuns("Build the lock", "Wire the TUI toggle").Update(keyQ)
		m := next.(Model)
		if cmd != nil || m.editMode != editConfirmQuit {
			t.Fatalf("q with 2 in flight must confirm, mode %v", m.editMode)
		}
		want := "2 run-now efforts will be abandoned: Build the lock, Wire the TUI toggle — quit?"
		if m.status != want || !strings.HasPrefix(m.helpLine(), renderKeys([]footerKey{{"y", "quit"}, {"n/esc", "cancel"}})) {
			t.Errorf("confirm: status %q help %q", m.status, m.helpLine())
		}
		next, cmd = m.Update(k)
		if m = next.(Model); cmd != nil || m.editMode != editNone || len(m.runs) != 2 {
			t.Errorf("%s must cancel: mode %v", k, m.editMode)
		}
	}
}

// ctrl+c then ctrl+c quits, writing paused:abandoned on each run's bead before
// its effort.abandoned (paused=true), all before tea.Quit (ADR 0082).
func TestQuitCtrlCTwiceAbandons(t *testing.T) {
	evs := eventlogtest.Capture(t)
	origP := pauseBead
	t.Cleanup(func() { pauseBead = origP })
	var writes []string
	pauseBead = func(id, cause string) error {
		writes = append(writes, fmt.Sprintf("%s %s after %d events", id, cause, len(*evs)))
		return nil
	}
	next, _ := withRuns("A", "B").Update(keyCtrlC)
	if m := next.(Model); m.editMode != editConfirmQuit {
		t.Fatalf("ctrl+c with runs in flight must confirm, mode %v", m.editMode)
	}
	_, cmd := next.Update(keyCtrlC)
	if want := []string{"ra abandoned after 0 events", "rb abandoned after 1 events"}; !slices.Equal(writes, want) {
		t.Fatalf("pause writes before the quit = %q; want %q", writes, want)
	}
	if !quits(cmd) {
		t.Fatal("a second ctrl+c must quit")
	}
	if len(*evs) != 2 {
		t.Fatalf("events = %v; want two effort.abandoned", eventlogtest.Names(*evs))
	}
	for i, id := range []string{"a", "b"} {
		e := (*evs)[i]
		if e.Name != "effort.abandoned" || e.Attrs["effort"] != "e"+id || e.Attrs["bead"] != "r"+id || e.Attrs["crewmate"] != "nami" || e.Attrs["paused"] != true {
			t.Errorf("event %d = %+v", i, e)
		}
	}
}

// A failed abandon pause never blocks the quit: it is only recorded.
func TestQuitWithFailedPause(t *testing.T) {
	evs := eventlogtest.Capture(t)
	origP := pauseBead
	t.Cleanup(func() { pauseBead = origP })
	pauseBead = func(string, string) error { return errors.New("bd down") }
	next, _ := withRuns("A").Update(keyQ)
	if _, cmd := next.Update(keyY); !quits(cmd) {
		t.Fatal("y must quit even when the pause fails")
	}
	if len(*evs) != 1 || (*evs)[0].Attrs["paused"] != false || (*evs)[0].Attrs["error"] != "bd down" {
		t.Errorf("events = %+v; want one abandoned paused=false error=bd down", *evs)
	}
}

// An effort finishing mid-prompt leaves the prompt as is, the tick doesn't
// rewrite it, and y abandons only what is still in flight.
func TestQuitPromptSurvivesCompletion(t *testing.T) {
	evs := eventlogtest.Capture(t)
	next, _ := withRuns("A", "B").Update(keyQ)
	prompt := next.(Model).status
	next, _ = next.Update(runDoneMsg{id: "ra", run: "ea", err: errors.New("boom")})
	next, _ = next.Update(tickMsg{})
	m := next.(Model)
	if m.editMode != editConfirmQuit || m.status != prompt || len(m.runs) != 1 {
		t.Fatalf("prompt must stay: mode %v status %q runs %d", m.editMode, m.status, len(m.runs))
	}
	if _, cmd := m.Update(keyY); !quits(cmd) {
		t.Fatal("y must quit")
	}
	if len(*evs) != 1 || (*evs)[0].Attrs["effort"] != "eb" {
		t.Errorf("events = %+v; want one abandoned for eb", *evs)
	}
}

// A result that lands mid-prompt is deferred, and a cancel shows it.
func TestQuitCancelShowsDeferredResult(t *testing.T) {
	next, _ := withRuns("A").Update(keyQ)
	next, _ = next.Update(runDoneMsg{id: "ra", run: "ea", err: errors.New("boom"), tail: "t"})
	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m := next.(Model); m.status != "run ra failed: boom\nt" {
		t.Errorf("cancel status = %q; want the held failure", m.status)
	}
	next, _ = withRuns("A").Update(keyQ)
	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m := next.(Model); m.status != "quit cancelled" {
		t.Errorf("cancel status = %q", m.status)
	}
}

// Writes in flight count at quit (Spec FR26): q confirms when only
// pending writes exist and names them; y quits without waiting on them.
func TestQuitConfirmNamesPendingWrites(t *testing.T) {
	m := New(nil, nil)
	m.w, m.pending = 200, map[string]string{"grl1": "updating"}
	next, cmd := m.Update(keyQ)
	if m = next.(Model); cmd != nil || m.editMode != editConfirmQuit {
		t.Fatalf("q with a pending write must confirm, mode %v", m.editMode)
	}
	if want := "1 write in flight: grl1 — quit?"; m.status != want {
		t.Errorf("prompt = %q; want %q", m.status, want)
	}
	if _, cmd := m.Update(keyY); !quits(cmd) {
		t.Error("y must quit")
	}
	m = withRuns("A")
	m.pending = map[string]string{"b": "updating", "a": "updating"}
	next, _ = m.Update(keyQ)
	if want := "1 run-now efforts will be abandoned: A; 2 writes in flight: a, b — quit?"; next.(Model).status != want {
		t.Errorf("prompt = %q; want %q", next.(Model).status, want)
	}
}

// A write's done message landing mid-prompt is deferred (Spec FR50).
func TestQuitDefersWriteDone(t *testing.T) {
	stubSeams(t)
	m := New(nil, nil)
	m.beads = []bd.Bead{{ID: "a", Kind: contract.KindSignal, Status: "open", Priority: 2}}
	m.rebuildGroups()
	m.selGraph = "a"
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("+")})
	next, _ = next.Update(keyQ)
	prompt := next.(Model).status
	next, _ = next.Update(cmd())
	if m = next.(Model); m.status != prompt || m.deferredStatus != "a → P1" || len(m.pending) != 0 {
		t.Errorf("status %q deferred %q pending %v; want the prompt kept and the echo held", m.status, m.deferredStatus, m.pending)
	}
}
