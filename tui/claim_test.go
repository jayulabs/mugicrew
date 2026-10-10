package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/session"
)

// stubClaim records hitlClaim/hitlUnclaim and startSession calls in order.
func stubClaim(t *testing.T, claimErr, startErr error) *[]string {
	t.Helper()
	origC, origU, origStart, origSwitch, origList := hitlClaim, hitlUnclaim, startSession, switchSession, listAll
	t.Cleanup(func() {
		hitlClaim, hitlUnclaim, startSession, switchSession, listAll = origC, origU, origStart, origSwitch, origList
	})
	var calls []string
	hitlClaim = func(id string) error { calls = append(calls, "claim "+id); return claimErr }
	hitlUnclaim = func(id string) error { calls = append(calls, "unclaim "+id); return nil }
	startSession = func(b bd.Bead, _ contract.AgentCommand) error { calls = append(calls, "start "+b.ID); return startErr }
	switchSession = func(string) error { return nil }
	listAll = func() ([]bd.Bead, error) { return nil, nil }
	return &calls
}

func claimModel(sessions map[string]session.Info) Model {
	m := New(harnessCtx{}, map[string]contract.Crewmate{"nami": &sessionLaunchCrew{}})
	m.beads = []bd.Bead{{ID: "g1", Kind: contract.KindGrilling, Status: "open", Parent: "chart1"}}
	m.sessions = sessions
	m.selGraph = "g1"
	return m
}

// A fresh l claims its bead before the session starts (Spec FR9).
func TestLaunchClaimsBeforeStart(t *testing.T) {
	calls := stubClaim(t, nil, nil)
	next, _ := settleLaunch(claimModel(nil).launch())
	if want := []string{"claim g1", "start g1"}; !slices.Equal(*calls, want) {
		t.Errorf("calls = %v, want %v", *calls, want)
	}
	if next.status != "" {
		t.Errorf("status = %q, want clean", next.status)
	}
}

// A failed claim cancels the launch with a status; no session starts (FR10).
func TestLaunchFailedClaimCancels(t *testing.T) {
	calls := stubClaim(t, errors.New("status is in_progress, not open"), nil)
	next, _ := settleLaunch(claimModel(nil).launch())
	if want := []string{"claim g1"}; !slices.Equal(*calls, want) {
		t.Errorf("calls = %v, want %v", *calls, want)
	}
	if !strings.Contains(next.status, "claim") || !strings.Contains(next.status, "not open") || len(next.launching) != 0 {
		t.Errorf("status = %q launching = %v, want the claim error and nothing in flight", next.status, next.launching)
	}
}

// countingCrew counts Launch calls and returns err, else sessionLaunchCrew's command.
type countingCrew struct {
	sessionLaunchCrew
	n   *int
	err error
}

func (c countingCrew) Launch(ctx contract.Ctx, b contract.Bead) (contract.LaunchCommand, error) {
	*c.n++
	if c.err != nil {
		return nil, c.err
	}
	return c.sessionLaunchCrew.Launch(ctx, b)
}

// l resolves the launch command inside the launch Cmd, not on the event loop
// (Spec FR6, ADR 0101 §3): no Launch before the Cmd runs, one after.
func TestLaunchResolvesOffTheLoop(t *testing.T) {
	stubClaim(t, nil, nil)
	var n int
	m := claimModel(nil)
	m.crew = map[string]contract.Crewmate{"nami": &countingCrew{n: &n}}
	next, cmd := m.launch()
	if cmd == nil || n != 0 {
		t.Fatalf("cmd nil = %v, Launch calls = %d after Update; want a Cmd and none", cmd == nil, n)
	}
	if st := next.(Model).status; !strings.HasPrefix(st, "launching ") {
		t.Errorf("status = %q, want launching …", st)
	}
	cmd()
	if n != 1 {
		t.Errorf("Launch calls = %d after the Cmd ran, want 1", n)
	}
}

// A Launch error comes back on the launchDoneMsg as launch-failed status, and
// no claim is taken (FR7).
func TestLaunchErrorIsLaunchFailed(t *testing.T) {
	calls := stubClaim(t, nil, nil)
	var n int
	m := claimModel(nil)
	m.crew = map[string]contract.Crewmate{"nami": &countingCrew{n: &n, err: errors.New("bd show: boom")}}
	next, _ := settleLaunch(m.launch())
	if next.status != "launch: bd show: boom" || len(next.launching) != 0 {
		t.Errorf("status = %q launching = %v, want the launch failure and nothing in flight", next.status, next.launching)
	}
	if len(*calls) != 0 {
		t.Errorf("calls = %v, want no claim", *calls)
	}
}

// A start failure after a claim releases it (ADR 0034's failed-effort release).
func TestLaunchStartFailureUnclaims(t *testing.T) {
	calls := stubClaim(t, nil, session.ErrNoTmux)
	next, _ := settleLaunch(claimModel(nil).launch())
	if want := []string{"claim g1", "start g1", "unclaim g1"}; !slices.Equal(*calls, want) {
		t.Errorf("calls = %v, want %v", *calls, want)
	}
	if !strings.Contains(next.status, "tmux") {
		t.Errorf("status = %q, want the start error", next.status)
	}
}

// Attach and resume of an existing session issue no claim (FR11).
func TestAttachResumeDoNotClaim(t *testing.T) {
	calls := stubClaim(t, nil, nil)
	origResume := resumeSession
	t.Cleanup(func() { resumeSession = origResume })
	resumeSession = func(bd.Bead, session.Info, contract.Harness) error { return nil }
	for _, st := range []session.State{session.Alive, session.Exited} {
		settleLaunch(claimModel(map[string]session.Info{"g1": {Session: "s", State: st}}).launch())
	}
	if len(*calls) != 0 {
		t.Errorf("calls = %v, want no claim on attach/resume", *calls)
	}
}

// y on the drop confirm unclaims an in_progress bead after removing its session
// and recording hitl.dropped; a bead not in_progress gets no unclaim (FR12).
func TestDropUnclaimsInProgress(t *testing.T) {
	calls := stubClaim(t, nil, nil)
	origDrop, origSess := dropSession, readSessions
	t.Cleanup(func() { dropSession, readSessions = origDrop, origSess })
	dropSession = func(b bd.Bead, _ session.Info) error { *calls = append(*calls, "drop "+b.ID); return nil }
	key := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

	for _, c := range []struct {
		status string
		want   []string
	}{
		{"in_progress", []string{"drop g1", "events [hitl.dropped]", "unclaim g1"}},
		{"open", []string{"drop g1"}},
	} {
		*calls = nil
		m := claimModel(map[string]session.Info{"g1": {Session: "s", State: session.Exited}})
		m.beads[0].Status = c.status
		readSessions = func() (map[string]session.Info, error) { return m.sessions, nil }
		capt, unclaim := eventlogtest.Capture(t), hitlUnclaim
		hitlUnclaim = func(id string) error { // the event record is ordered before the unclaim
			*calls = append(*calls, fmt.Sprint("events ", eventlogtest.Names(*capt)))
			return unclaim(id)
		}
		mm, _ := m.Update(key("x"))
		settleWrite(mm.Update(key("y")))
		hitlUnclaim = unclaim
		if !slices.Equal(*calls, c.want) {
			t.Errorf("%s: calls = %v, want %v", c.status, *calls, c.want)
		}
	}
}

// A Surgery is claimed by its own filing, so neither the u form's first session
// nor l on an idle one claims it, and x leaves it in_progress.
func TestSurgeryLaunchAndDropSkipClaim(t *testing.T) {
	calls := stubClaim(t, nil, nil)
	origNew, origDrop, origSess := newSurgery, dropSession, readSessions
	t.Cleanup(func() { newSurgery, dropSession, readSessions = origNew, origDrop, origSess })
	newSurgery = func(string, string) (string, string, error) { return "sg1", "cp1", nil }
	dropSession = func(b bd.Bead, _ session.Info) error { *calls = append(*calls, "drop "+b.ID); return nil }
	key := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
	surgery := func(sessions map[string]session.Info) Model {
		m := New(harnessCtx{}, map[string]contract.Crewmate{"franky": &sessionLaunchCrew{}})
		m.beads = []bd.Bead{{ID: "sg1", Kind: contract.KindSurgery, Status: "in_progress", Parent: "cp1"}}
		m.sessions = sessions
		m.selGraph = "sg1"
		return m
	}

	settleLaunch(settleWrite(surgery(nil).fileSurgery("Fix")))
	if want := []string{"start sg1"}; !slices.Equal(*calls, want) {
		t.Errorf("u form: calls = %v, want %v", *calls, want)
	}

	*calls = nil
	settleLaunch(surgery(nil).launch())
	if want := []string{"start sg1"}; !slices.Equal(*calls, want) {
		t.Errorf("l on idle: calls = %v, want %v", *calls, want)
	}

	*calls = nil
	m := surgery(map[string]session.Info{"sg1": {Session: "s", State: session.Exited}})
	readSessions = func() (map[string]session.Info, error) { return m.sessions, nil }
	mm, _ := m.Update(key("x"))
	settleWrite(mm.Update(key("y")))
	if want := []string{"drop sg1"}; !slices.Equal(*calls, want) {
		t.Errorf("x: calls = %v, want %v", *calls, want)
	}
}
