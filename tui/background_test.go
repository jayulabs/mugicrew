package tui

import (
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/claudecode"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/session"
)

// bgSeams stubs the start/resume/switch/claim seams, recording each call in order.
func bgSeams(t *testing.T, startErr, resumeErr error) *[]string {
	t.Helper()
	oS, oR, oW, oC, oU, oL, oRS := startSession, resumeSession, switchSession, hitlClaim, hitlUnclaim, listAll, readSessions
	t.Cleanup(func() {
		startSession, resumeSession, switchSession, hitlClaim, hitlUnclaim, listAll, readSessions = oS, oR, oW, oC, oU, oL, oRS
	})
	var calls []string
	startSession = func(b bd.Bead, _ contract.AgentCommand) error { calls = append(calls, "start "+b.ID); return startErr }
	resumeSession = func(b bd.Bead, _ session.Info, _ contract.Harness) error {
		calls = append(calls, "resume "+b.ID)
		return resumeErr
	}
	switchSession = func(id string) error { calls = append(calls, "switch "+id); return nil }
	hitlClaim = func(id string) error { calls = append(calls, "claim "+id); return nil }
	hitlUnclaim = func(id string) error { calls = append(calls, "unclaim "+id); return nil }
	listAll = func() ([]bd.Bead, error) { return nil, nil }
	readSessions = func() (map[string]session.Info, error) {
		return map[string]session.Info{"g1": {Session: "s", State: session.Exited}}, nil
	}
	return &calls
}

func bgModel(beads ...bd.Bead) Model {
	m := New(harnessCtx{claudecode.New("")}, map[string]contract.Crewmate{"nami": &sessionLaunchCrew{}, "franky": &sessionLaunchCrew{}})
	if len(beads) == 0 {
		beads = []bd.Bead{{ID: "g1", Kind: contract.KindGrilling, Status: "open", Parent: "chart1", Title: "Grill me"}}
	}
	m.beads = beads
	m.rebuildGroups()
	m.selGraph, m.selInbox = beads[0].ID, beads[0].ID
	return m
}

func keyMsg(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// b launches or resumes exactly as l does but never switches (ADR 0100, Spec
// FR1–2, FR11): the status names the start and l, and a reload follows.
func TestBackgroundLaunchNeverSwitches(t *testing.T) {
	for _, c := range []struct {
		name     string
		sessions map[string]session.Info
		want     []string
	}{
		{"fresh", nil, []string{"claim g1", "start g1"}},
		{"resume", map[string]session.Info{"g1": {Session: "s", State: session.Exited}}, []string{"resume g1"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			calls := bgSeams(t, nil, nil)
			m := bgModel()
			m.sessions = c.sessions
			next, cmd := settleLaunch(m.Update(keyMsg("b")))
			if !slices.Equal(*calls, c.want) {
				t.Errorf("calls = %v, want %v (no switch)", *calls, c.want)
			}
			if next.status != "started Grill me — l attaches" || len(next.launching) != 0 || cmd == nil {
				t.Errorf("status = %q launching = %v reload = %v, want the started status and a reload", next.status, next.launching, cmd != nil)
			}
		})
	}
}

// b refuses what l refuses, with the same text (FR3), and is a no-op with no
// selection (FR5) or off Graph/Inbox (FR20).
func TestBackgroundLaunchRefusalsMatchL(t *testing.T) {
	calls := bgSeams(t, nil, nil)
	for _, b := range []bd.Bead{
		{ID: "r1", Kind: contract.KindResearch, Status: "open", Parent: "chart1"},
		{ID: "g2", Kind: contract.KindGrilling, Status: "closed", Parent: "chart1"},
		{ID: "hc", Kind: contract.KindGrilling, Status: "open", Container: true},
	} {
		l, _ := bgModel(b).Update(keyMsg("l"))
		bg, cmd := bgModel(b).Update(keyMsg("b"))
		if want := l.(Model).status; bg.(Model).status != want || want == "" || cmd != nil {
			t.Errorf("b on %s: status %q cmd %v, want l's %q and nothing started", b.ID, bg.(Model).status, cmd != nil, want)
		}
	}
	none := bgModel()
	none.selGraph = ""
	for _, m := range []Model{none, withTab(bgModel(), tabLogs), withTab(bgModel(), tabHistory), withTab(bgModel(), tabSearch)} {
		if next, cmd := m.Update(keyMsg("b")); cmd != nil || next.(Model).status != m.status || len(next.(Model).launching) != 0 {
			t.Errorf("b on tab %d sel %q: cmd %v status %q, want a no-op", m.tab, m.selGraph, cmd != nil, next.(Model).status)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("calls = %v, want none", *calls)
	}
}

func withTab(m Model, tb tab) Model { m.tab = tb; return m }

// b on an alive session starts nothing and says how to join (FR4).
func TestBackgroundLaunchAlive(t *testing.T) {
	calls := bgSeams(t, nil, nil)
	m := bgModel()
	m.sessions = map[string]session.Info{"g1": {Session: "s", State: session.Alive}}
	if next, cmd := m.Update(keyMsg("b")); cmd != nil || next.(Model).status != "already running — l attach" {
		t.Errorf("b on alive: cmd %v status %q, want already running", cmd != nil, next.(Model).status)
	}
	if len(*calls) != 0 {
		t.Errorf("calls = %v, want none", *calls)
	}
}

// parallelModel is three open grillings plus an alive session "up", Graph-selected.
func parallelModel() Model {
	m := bgModel(bd.Bead{ID: "g1", Kind: contract.KindGrilling, Status: "open", Parent: "chart1", Title: "Grill me"},
		bd.Bead{ID: "g2", Kind: contract.KindGrilling, Status: "open", Parent: "chart1", Title: "Other"},
		bd.Bead{ID: "up", Kind: contract.KindGrilling, Status: "open", Parent: "chart1", Title: "Alive one"})
	m.sessions = map[string]session.Info{"up": {Session: "s", State: session.Alive}}
	return m
}

// pressOn selects id and sends k, returning the model and the launch Cmd.
func pressOn(t *testing.T, m Model, id, k string) (Model, tea.Cmd) {
	t.Helper()
	m.selGraph = id
	next, cmd := m.Update(keyMsg(k))
	return next.(Model), cmd
}

func suffix(m Model, id string) string { return ansi.Strip(m.rowSuffix(m.beadByID(id))) }

// Launches run in parallel, one per bead (Spec FR18–21, FR25): each row
// reads launching…, l's status names the attach, and a second l/b on a bead
// already launching is refused.
func TestParallelLaunches(t *testing.T) {
	calls := bgSeams(t, nil, nil)
	m, l1 := pressOn(t, parallelModel(), "g1", "l")
	if l1 == nil || m.status != "launching Grill me… (attaches when ready)" {
		t.Fatalf("l: cmd %v status %q, want the attaching launch", l1 != nil, m.status)
	}
	m, b2 := pressOn(t, m, "g2", "b")
	if b2 == nil || m.status != "launching Other…" {
		t.Fatalf("b on g2 while g1 launches: cmd %v status %q, want a second launch", b2 != nil, m.status)
	}
	for _, id := range []string{"g1", "g2"} {
		if !strings.Contains(suffix(m, id), "launching…") {
			t.Errorf("%s row %q, want launching…", id, suffix(m, id))
		}
	}
	for _, k := range []string{"l", "b"} {
		if next, cmd := pressOn(t, m, "g1", k); cmd != nil || next.status != "still launching Grill me…" {
			t.Errorf("%s on g1 in flight: cmd %v status %q, want still launching", k, cmd != nil, next.status)
		}
	}
	if next, _ := pressOn(t, m, "g1", "x"); next.editMode != editNone {
		t.Error("x on a launching row must stay suppressed")
	}
	if len(*calls) != 0 {
		t.Errorf("calls = %v, want none before the Cmds run", *calls)
	}
	next, _ := m.Update(b2())
	m = next.(Model)
	if strings.Contains(suffix(m, "g2"), "launching…") || !strings.Contains(suffix(m, "g1"), "launching…") {
		t.Errorf("after g2 lands: g1 %q g2 %q, want only g1 launching", suffix(m, "g1"), suffix(m, "g2"))
	}
	next, _ = m.Update(l1())
	if want := []string{"claim g2", "start g2", "claim g1", "start g1", "switch g1"}; !slices.Equal(*calls, want) {
		t.Errorf("calls = %v, want %v (only l's launch switches)", *calls, want)
	}
}

// The latest l holds the one attach slot (FR22–24): a displaced l lands as b
// and says it was downgraded; an instant attach to an alive session clears it.
func TestLaunchAttachSlot(t *testing.T) {
	const downgraded = "started Grill me — l attaches (downgraded: a later l took the attach)"
	t.Run("second l wins", func(t *testing.T) {
		calls := bgSeams(t, nil, nil)
		m, l1 := pressOn(t, parallelModel(), "g1", "l")
		m, l2 := pressOn(t, m, "g2", "l")
		next, _ := m.Update(l1())
		if m = next.(Model); m.status != downgraded {
			t.Errorf("displaced l landed: status %q, want %q", m.status, downgraded)
		}
		next, _ = m.Update(l2())
		if want := []string{"claim g1", "start g1", "claim g2", "start g2", "switch g2"}; !slices.Equal(*calls, want) {
			t.Errorf("calls = %v, want %v", *calls, want)
		}
	})
	t.Run("instant attach clears", func(t *testing.T) {
		calls := bgSeams(t, nil, nil)
		m, l1 := pressOn(t, parallelModel(), "g1", "l")
		m, _ = pressOn(t, m, "up", "l")
		next, _ := m.Update(l1())
		if m = next.(Model); m.status != downgraded {
			t.Errorf("l after an instant attach: status %q, want %q", m.status, downgraded)
		}
		if want := []string{"switch up", "claim g1", "start g1"}; !slices.Equal(*calls, want) {
			t.Errorf("calls = %v, want %v (no switch to g1)", *calls, want)
		}
	})
	t.Run("b keeps the slot", func(t *testing.T) {
		calls := bgSeams(t, nil, nil)
		m, l1 := pressOn(t, parallelModel(), "g1", "l")
		m, _ = pressOn(t, m, "g2", "b")
		m.Update(l1())
		if want := []string{"claim g1", "start g1", "switch g1"}; !slices.Equal(*calls, want) {
			t.Errorf("calls = %v, want %v", *calls, want)
		}
	})
}

// onBusy never offers the orphan-drop confirm for a branch an in-flight launch
// holds (FR27): its tree is still being built.
func TestOnBusyInFlightLaunch(t *testing.T) {
	bgSeams(t, nil, nil)
	readSessions = func() (map[string]session.Info, error) { return nil, nil } // the tree has no session: an orphan by the table
	m, _ := pressOn(t, parallelModel(), "g2", "b")
	busyErr := exec.Command("sh", "-c", "exit 128").Run()
	oOn := git.WorktreeOn
	t.Cleanup(func() { git.WorktreeOn = oOn })
	git.WorktreeOn = func(string) (git.Holder, bool, error) { return git.Holder{Path: "/tmp/mugicrew-wt-9/wt"}, true, nil }
	g1 := *m.beadByID("g1")
	next, _ := m.Update(launchDoneMsg{b: g1, branch: "effort/g2", err: busyErr})
	if m = next.(Model); m.editMode != editNone || m.status != "g1: grilling branch busy — held by g2's launch, retry" {
		t.Errorf("mode %v status %q, want busy naming g2's launch and no orphan confirm", m.editMode, m.status)
	}
}

// A b success mid-edit only reloads (FR12); failures read as l's (FR13).
func TestBackgroundLaunchMidEditAndFailures(t *testing.T) {
	bgSeams(t, nil, nil)
	next, launch := bgModel().Update(keyMsg("b"))
	m := next.(Model)
	m.editMode, m.status = editConfirmDrop, "drop it?"
	if next, cmd := m.Update(launch()); next.(Model).status != "drop it?" || cmd == nil {
		t.Errorf("mid-edit: status %q, want the prompt kept and a reload", next.(Model).status)
	}

	surgery := bd.Bead{ID: "s1", Kind: contract.KindSurgery, Status: "in_progress", Title: "Fix"}
	exited := map[string]session.Info{"g1": {Session: "s", State: session.Exited}}
	for _, c := range []struct {
		name     string
		bead     []bd.Bead
		sessions map[string]session.Info
	}{
		{"launch", nil, nil},
		{"surgery", []bd.Bead{surgery}, nil},
		{"resume", nil, exited},
	} {
		run := func(k string) string {
			calls := bgSeams(t, errors.New("boom"), errors.New("boom"))
			m := bgModel(c.bead...)
			m.sessions = c.sessions
			next, _ := settleLaunch(m.Update(keyMsg(k)))
			if slices.ContainsFunc(*calls, func(s string) bool { return s[:6] == "switch" }) {
				t.Errorf("%s %s: switched on failure", c.name, k)
			}
			return next.status
		}
		if l, b := run("l"), run("b"); l != b || l == "" {
			t.Errorf("%s failure: b %q, want l's %q", c.name, b, l)
		}
	}
}

// b on a blocked bead opens l's confirm with the background footer; y starts in
// the background, n/esc cancel, and a later l on it is a foreground launch (FR6–7).
func TestBackgroundBlockedConfirm(t *testing.T) {
	blocked := bd.Bead{ID: "g1", Kind: contract.KindGrilling, Status: "blocked", Parent: "chart1", Title: "Grill me"}
	press := func(keys ...tea.Msg) (Model, []string) {
		calls := bgSeams(t, nil, nil)
		var next tea.Model = bgModel(blocked)
		for _, k := range keys {
			next, _ = settleLaunch(next.Update(k))
		}
		return next.(Model), *calls
	}

	m, calls := press(keyMsg("b"))
	l, _ := press(keyMsg("l"))
	if m.editMode != editConfirmLaunch || m.status != l.status || len(calls) != 0 {
		t.Fatalf("b on blocked: mode %v status %q calls %v, want l's confirm %q and nothing started", m.editMode, m.status, calls, l.status)
	}
	if !strings.Contains(m.helpLine(), "launch in background") || strings.Contains(l.helpLine(), "background") {
		t.Errorf("footers b %q / l %q, want the background variant only for b", m.helpLine(), l.helpLine())
	}

	m, calls = press(keyMsg("b"), keyMsg("y"))
	if !slices.Equal(calls, []string{"claim g1", "start g1"}) || m.status != "started Grill me — l attaches" {
		t.Errorf("b y: calls %v status %q, want a background start", calls, m.status)
	}
	for _, cancel := range []tea.Msg{keyMsg("n"), tea.KeyMsg{Type: tea.KeyEsc}} {
		if m, calls = press(keyMsg("b"), cancel); len(calls) != 0 || m.editMode != editNone {
			t.Errorf("b %v: calls %v mode %v, want cancelled", cancel, calls, m.editMode)
		}
		if _, calls = press(keyMsg("b"), cancel, keyMsg("l"), keyMsg("y")); !slices.Equal(calls, []string{"claim g1", "start g1", "switch g1"}) {
			t.Errorf("l y after a cancelled b: calls %v, want a foreground switch", calls)
		}
	}
}

// A b launch refused by an orphan holder opens the unchanged orphan prompt; y
// drops it and starts in the background (FR8).
func TestBackgroundOrphanConfirm(t *testing.T) {
	calls := bgSeams(t, nil, nil)
	origOn, origDrop, origProbe := git.WorktreeOn, dropOrphan, probeLiveness
	t.Cleanup(func() { git.WorktreeOn, dropOrphan, probeLiveness = origOn, origDrop, origProbe })
	const wt = "/tmp/mugicrew-wt-9/wt"
	busy := true
	startSession = func(b bd.Bead, _ contract.AgentCommand) error {
		*calls = append(*calls, "start "+b.ID)
		if busy {
			return exec.Command("sh", "-c", "exit 128").Run()
		}
		return nil
	}
	git.WorktreeOn = func(string) (git.Holder, bool, error) { return git.Holder{Path: wt}, true, nil }
	dropOrphan = func(p string) error { *calls = append(*calls, "drop "+p); busy = false; return nil }
	readSessions = func() (map[string]session.Info, error) { return nil, nil }
	probeLiveness = func([]string) map[string]bool { return nil }

	run := func(k string) Model {
		busy, *calls = true, nil
		m := bgModel()
		m.w, m.h = 200, 24
		m, _ = settleLaunch(m.Update(keyMsg(k)))
		return m
	}
	b, l := run("b"), run("l")
	if b.editMode != editConfirmOrphan || b.status != l.status {
		t.Fatalf("b busy: mode %v status %q, want l's orphan prompt %q", b.editMode, b.status, l.status)
	}
	*calls = nil
	m, _ := settleLaunch(settleWrite(b.Update(keyMsg("y"))))
	if !slices.Equal(*calls, []string{"drop " + wt, "claim g1", "start g1"}) || m.status != "started Grill me — l attaches" {
		t.Errorf("b orphan y: calls %v status %q, want drop then a background start", *calls, m.status)
	}
}

// The footer shows b bg right after l wherever l would start something (launch,
// launch⚠, resume), never where l attaches or is hidden, nor off Graph/Inbox (FR17).
func TestBackgroundFooterHint(t *testing.T) {
	beads := []bd.Bead{
		{ID: "h", Kind: contract.KindGrilling, Status: "open", Title: "HITL"},
		{ID: "hb", Kind: contract.KindGrilling, Status: "open", Title: "blocked", BlockedBy: []string{"h"}},
		{ID: "ex", Kind: contract.KindGrilling, Status: "open", Title: "exited"},
		{ID: "al", Kind: contract.KindGrilling, Status: "open", Title: "alive"},
		{ID: "afk", Kind: contract.KindResearch, Status: "open", Title: "AFK"},
		{ID: "hc", Kind: contract.KindGrilling, Status: "closed", Title: "closed"},
	}
	for _, c := range []struct {
		tab  tab
		sel  string
		want string // "" = no b bg
	}{
		{tabGraph, "h", "l launch · b bg · "},
		{tabInbox, "h", "l launch · b bg · "},
		{tabGraph, "hb", "l launch⚠ · b bg · "},
		{tabGraph, "ex", "l resume · b bg · "},
		{tabInbox, "ex", "l resume · b bg · "},
		{tabGraph, "al", ""},
		{tabGraph, "afk", ""},
		{tabGraph, "hc", ""},
		{tabSearch, "h", ""},
		{tabLogs, "h", ""},
		{tabHistory, "h", ""},
	} {
		m := New(nil, nil)
		m.beads, m.hideClosedRoots = beads, false
		m.rebuildGroups()
		m.sessions = map[string]session.Info{"ex": {State: session.Exited}, "al": {State: session.Alive}}
		m.w, m.h, m.tab = 200, 40, c.tab
		m.selGraph, m.selInbox = c.sel, c.sel
		f := ansi.Strip(m.footer())
		if c.want == "" && strings.Contains(f, "b bg") || c.want != "" && !strings.HasPrefix(f, c.want) {
			t.Errorf("%v %s: footer %q, want b bg iff %q", c.tab, c.sel, f, c.want)
		}
	}
}

// b bg is the first hint dropped when narrow, ahead of u: at a width where the
// footer without it just fits, it goes and every other hint stays (FR18).
func TestBackgroundFooterDropsFirst(t *testing.T) {
	for _, tb := range []tab{tabGraph, tabInbox} {
		m := New(nil, nil)
		m.beads = []bd.Bead{{ID: "h", Kind: contract.KindGrilling, Status: "open", Title: "HITL"}}
		m.rebuildGroups()
		m.h, m.tab, m.selGraph, m.selInbox = 40, tb, "h", "h"
		m.w = 200
		full := ansi.Strip(m.footer())
		without := strings.Replace(full, "b bg · ", "", 1)
		if without == full {
			t.Fatalf("%v: wide footer %q lacks b bg", tb, full)
		}
		m.w = lipgloss.Width(without)
		if f := ansi.Strip(m.footer()); f != without {
			t.Errorf("%v at %d cols: footer %q, want %q (only b bg dropped)", tb, m.w, f, without)
		}
	}
}
