package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// u on Graph and Inbox opens the shared create form headed "new Surgery"; enter
// files through the NewSurgery seam, then launches the new Surgery on its
// Checkpoint (Spec FR1–2, D10).
func TestSurgeryFormFilesThenLaunches(t *testing.T) {
	origNew, origStart := newSurgery, startSession
	t.Cleanup(func() { newSurgery, startSession = origNew, origStart })
	var got []string
	newSurgery = func(title, problem string) (string, string, error) {
		got = append(got, title, problem)
		return "sg-1", "cp-1", nil
	}
	var started bd.Bead
	startSession = func(b bd.Bead, _ contract.AgentCommand) error { started = b; return nil }

	for _, tb := range []tab{tabGraph, tabInbox} {
		got, started = nil, bd.Bead{}
		m := New(nil, map[string]contract.Crewmate{"franky": &sessionLaunchCrew{}})
		m.tab = tb
		m = press(t, m, "u")
		if m.editMode != editCreate || !strings.HasPrefix(ansi.Strip(m.createFormLine()), "new Surgery") {
			t.Fatalf("%v: u should open the form headed new Surgery, mode=%d form=%q", tb, m.editMode, ansi.Strip(m.createFormLine()))
		}
		for _, k := range []string{"F", "i", "x", "tab", "p"} {
			msg := runeKey(k)
			if k == "tab" {
				msg = tea.KeyMsg{Type: tea.KeyTab}
			}
			m, _ = send(t, m, msg)
		}
		m, cmd := settleWrite(send(t, m, tea.KeyMsg{Type: tea.KeyEnter}))
		if len(got) != 2 || got[0] != "Fix" || got[1] != "p" {
			t.Fatalf("%v: NewSurgery got %q, want (Fix, p)", tb, got)
		}
		if m.editMode != editNone || !m.launching["sg-1"] || cmd == nil {
			t.Fatalf("%v: enter should close the form and launch sg-1, mode=%d launching = %v", tb, m.editMode, m.launching)
		}
		m, _ = settleLaunch(m, cmd)
		if started.ID != "sg-1" || started.Parent != "cp-1" || started.Kind != contract.KindSurgery {
			t.Errorf("%v: started %+v, want the new Surgery under cp-1", tb, started)
		}
	}
}

// A launch that fails after filing leaves the surgery idle and says so (FR3); a
// failed filing names the orphan Checkpoint and launches nothing.
func TestSurgeryFormFailures(t *testing.T) {
	origNew, origStart := newSurgery, startSession
	t.Cleanup(func() { newSurgery, startSession = origNew, origStart })
	newSurgery = func(string, string) (string, string, error) { return "sg-1", "cp-1", nil }
	startSession = func(bd.Bead, contract.AgentCommand) error { return errors.New("boom") }

	m := New(nil, map[string]contract.Crewmate{"franky": &sessionLaunchCrew{}})
	m = press(t, m, "u")
	m, _ = send(t, m, runeKey("x"))
	m, _ = settleLaunch(settleWrite(send(t, m, tea.KeyMsg{Type: tea.KeyEnter})))
	if !strings.Contains(m.status, "boom") || !strings.Contains(m.status, "sg-1 idle") {
		t.Errorf("status = %q, want the launch error naming the idle sg-1", m.status)
	}

	newSurgery = func(string, string) (string, string, error) { return "", "cp-1", errors.New("cut failed") }
	m = press(t, New(nil, nil), "u")
	m, _ = send(t, m, runeKey("x"))
	m, _ = settleWrite(send(t, m, tea.KeyMsg{Type: tea.KeyEnter}))
	if len(m.launching) != 0 || !strings.Contains(m.status, "cut failed") || !strings.Contains(m.status, "cp-1") {
		t.Errorf("status = %q launching = %v, want the error naming orphan cp-1", m.status, m.launching)
	}
}

// n on Graph files a plain, unparented Signal whatever the selection (FR42); u
// and n do nothing on Search, History and Logs (FR43).
func TestSignalAndSurgeryKeysPerTab(t *testing.T) {
	orig := newSignal
	t.Cleanup(func() { newSignal = orig })
	var title string
	newSignal = func(t, _ string) (string, error) { title = t; return "sig-1", nil }

	m := New(nil, nil)
	m.beads = []bd.Bead{{ID: "cp", Kind: contract.KindCheckpoint, Container: true}}
	m.selGraph = "cp"
	m = press(t, m, "n")
	if m.editMode != editCreate || m.createFor != "" || m.createSurgery {
		t.Fatalf("n on Graph should open the plain Signal form, mode=%d for=%q", m.editMode, m.createFor)
	}
	m, _ = send(t, m, runeKey("x"))
	if m, _ = settleWrite(send(t, m, tea.KeyMsg{Type: tea.KeyEnter})); title != "x" || m.status != "filed Signal sig-1" {
		t.Errorf("n on Graph: filed %q status %q", title, m.status)
	}

	for _, tb := range []tab{tabSearch, tabHistory, tabLogs} {
		for _, k := range []string{"n", "u"} {
			m := New(nil, nil)
			m.tab = tb
			if m = press(t, m, k); m.editMode != editNone {
				t.Errorf("%v: %s opened mode %d, want a no-op", tb, k, m.editMode)
			}
		}
	}
}

// The footer shows u surgery beside n signal on Graph and Inbox, and u is the
// first hint dropped when narrow (FR44).
func TestSurgeryFooterDropsUFirst(t *testing.T) {
	for _, tb := range []tab{tabGraph, tabInbox} {
		m := New(nil, nil)
		m.tab, m.w = tb, 200
		if f := ansi.Strip(m.footer()); !strings.Contains(f, "n signal · u surgery") {
			t.Errorf("%v wide footer %q lacks n signal · u surgery", tb, f)
		}
		m.w = 70
		if f := ansi.Strip(m.footer()); strings.Contains(f, "u surgery") || !strings.Contains(f, " · ? · q") {
			t.Errorf("%v narrow footer %q: u should drop first", tb, f)
		}
	}
}

// An in_progress Surgery with no session reads idle — l to resume, not the
// stranded hitl claim; with a live session it renders as any running HITL row
// (Spec D10, FR21).
func TestIdleSurgeryRow(t *testing.T) {
	m := New(nil, nil)
	sg := bd.Bead{ID: "sg-1", Kind: contract.KindSurgery, Status: "in_progress", Parent: "cp-1"}
	if got := ansi.Strip(m.rowSuffix(&sg)); !strings.Contains(got, "idle — l to resume") || strings.Contains(got, "hitl") {
		t.Errorf("idle Surgery suffix = %q, want idle — l to resume and no hitl", got)
	}
	m.sessions = map[string]session.Info{"sg-1": {State: session.Alive}}
	if got := ansi.Strip(m.rowSuffix(&sg)); strings.Contains(got, "idle") || !strings.Contains(got, "hitl") {
		t.Errorf("live Surgery suffix = %q, want the running hitl look", got)
	}
}
