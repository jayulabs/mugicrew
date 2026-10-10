package tui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// An in_progress Task with no session reads idle — l to resume, exactly as a
// Surgery does; with a live session it renders as any running HITL row (Spec
// FR43).
func TestIdleTaskRow(t *testing.T) {
	m := New(nil, nil)
	tk := bd.Bead{ID: "tk-1", Kind: contract.KindTask, Status: "in_progress"}
	if got := ansi.Strip(m.rowSuffix(&tk)); !strings.Contains(got, "idle — l to resume") || strings.Contains(got, "hitl") {
		t.Errorf("idle Task suffix = %q, want idle — l to resume and no hitl", got)
	}
	m.sessions = map[string]session.Info{"tk-1": {State: session.Alive}}
	if got := ansi.Strip(m.rowSuffix(&tk)); strings.Contains(got, "idle") || !strings.Contains(got, "hitl") {
		t.Errorf("live Task suffix = %q, want the running hitl look", got)
	}
}

// l on an open Task claims it before Brook's session starts, and x unclaims it
// as for any HITL leaf: unlike a Surgery, a Task has no claim exception (Spec
// FR36, D2).
func TestTaskLaunchClaimsAndDropUnclaims(t *testing.T) {
	calls := stubClaim(t, nil, nil)
	origDrop, origSess := dropSession, readSessions
	t.Cleanup(func() { dropSession, readSessions = origDrop, origSess })
	dropSession = func(b bd.Bead, _ session.Info) error { *calls = append(*calls, "drop "+b.ID); return nil }
	task := func(status string, sessions map[string]session.Info) Model {
		m := New(harnessCtx{}, map[string]contract.Crewmate{"brook": &sessionLaunchCrew{}})
		m.beads = []bd.Bead{{ID: "tk1", Kind: contract.KindTask, Status: status}}
		m.sessions = sessions
		m.selGraph = "tk1"
		return m
	}

	settleLaunch(task("open", nil).launch())
	if want := []string{"claim tk1", "start tk1"}; !slices.Equal(*calls, want) {
		t.Errorf("l: calls = %v, want %v", *calls, want)
	}

	*calls = nil
	settleLaunch(task("in_progress", nil).launch())
	if want := []string{"start tk1"}; !slices.Equal(*calls, want) {
		t.Errorf("l on idle: calls = %v, want %v", *calls, want)
	}

	*calls = nil
	m := task("in_progress", map[string]session.Info{"tk1": {Session: "s", State: session.Exited}})
	readSessions = func() (map[string]session.Info, error) { return m.sessions, nil }
	key := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
	mm, _ := m.Update(key("x"))
	settleWrite(mm.Update(key("y")))
	if want := []string{"drop tk1", "unclaim tk1"}; !slices.Equal(*calls, want) {
		t.Errorf("x: calls = %v, want %v", *calls, want)
	}
}

// l on an idle Task resumes without claiming, so a failed start releases
// nothing and the Task stays in_progress (AMENDMENT D2).
func TestIdleTaskFailedResumeKeepsClaim(t *testing.T) {
	calls := stubClaim(t, nil, session.ErrNoTmux)
	m := New(harnessCtx{}, map[string]contract.Crewmate{"brook": &sessionLaunchCrew{}})
	m.beads = []bd.Bead{{ID: "tk1", Kind: contract.KindTask, Status: "in_progress"}}
	m.selGraph = "tk1"
	settleLaunch(m.launch())
	if want := []string{"start tk1"}; !slices.Equal(*calls, want) {
		t.Errorf("calls = %v, want %v", *calls, want)
	}
}
