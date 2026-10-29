package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"

	"jayulabs.org/mugicrew/session"
)

func headerFixture(t *testing.T, w int) Model {
	t.Helper()
	sess := map[string]session.Info{
		"mem":   {State: session.Alive},
		"loose": {State: session.Alive, Waiting: session.WaitTurn},
	}
	m, _ := focusModel(t, w, 40, focusFixture(), sess, map[string]bool{"other": true})
	return m.WithMouse(true)
}

func headerPress(t *testing.T, m Model, x, y int, b tea.MouseButton) Model {
	t.Helper()
	m, _ = send(t, m, tea.MouseMsg{X: x, Y: y, Button: b, Action: tea.MouseActionPress})
	return m
}

// topModal is "sessions", "loop" or "" for the modal on top.
func topModal(m Model) string {
	if len(m.modals) == 0 {
		return ""
	}
	d := m.modals[len(m.modals)-1]
	switch {
	case d.sessions:
		return "sessions"
	case d.kind == loopMenu:
		return "loop"
	}
	return "other"
}

func TestHeaderSegmentsClickOpenTheirModal(t *testing.T) {
	for _, tc := range []struct {
		name  string
		w     int
		focus bool
		want  []string // span kinds, left to right
	}{
		{"wide", 160, false, []string{"sessions", "sessions", "sessions", "loop"}},
		{"narrow", 40, false, []string{"loop"}},
		{"focus", 160, true, []string{"loop"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := headerFixture(t, tc.w)
			if tc.focus {
				m = ideaFocus(t, m)
			}
			spans := m.headerSpans()
			if len(spans) != len(tc.want) {
				t.Fatalf("spans = %v, want kinds %v\n%s", spans, tc.want, ansi.Strip(strings.Split(m.View(), "\n")[0]))
			}
			for i, sp := range spans {
				kind := map[headerKind]string{headerSessions: "sessions", headerLoop: "loop"}[sp.kind]
				if kind != tc.want[i] {
					t.Errorf("span %d kind = %s, want %s", i, kind, tc.want[i])
				}
				for _, x := range []int{sp.x0, sp.x1 - 1} {
					if got := topModal(headerPress(t, m, x, 0, tea.MouseButtonLeft)); got != kind {
						t.Errorf("span %d click at %d opened %q, want %q", i, x, got, kind)
					}
				}
				for _, x := range []int{sp.x0 - 1, sp.x1} {
					if got := topModal(headerPress(t, m, x, 0, tea.MouseButtonLeft)); got != "" {
						// the neighbouring cell may be another segment's own first/last cell
						adj := false
						for _, o := range spans {
							adj = adj || x >= o.x0 && x < o.x1
						}
						if !adj {
							t.Errorf("span %d click one cell outside (%d) opened %q", i, x, got)
						}
					}
				}
			}
		})
	}
}

func TestHeaderSpansMatchTheDrawnText(t *testing.T) {
	m := headerFixture(t, 160)
	row := ansi.Strip(strings.Split(m.View(), "\n")[0])
	spans := m.headerSpans()
	text := func(i int) string { return ansi.Cut(row, spans[i].x0, spans[i].x1) }
	if !strings.HasSuffix(text(0), "HITL") || !strings.HasSuffix(text(1), "AFK") || !strings.HasSuffix(text(2), "waiting") || !strings.HasPrefix(text(3), "loop") {
		t.Errorf("spans cover %q %q %q %q in %q", text(0), text(1), text(2), text(3), row)
	}
}

func TestHeaderInertCells(t *testing.T) {
	m := headerFixture(t, 160)
	if got := topModal(headerPress(t, m, 2, 0, tea.MouseButtonLeft)); got != "" { // the brand
		t.Errorf("brand click opened %q", got)
	}
	sp := m.headerSpans()[0]
	if got := topModal(headerPress(t, m, sp.x0, 0, tea.MouseButtonRight)); got != "" {
		t.Errorf("right-click opened %q", got)
	}
	if got := topModal(headerPress(t, m, sp.x0, 1, tea.MouseButtonLeft)); got == "sessions" || got == "loop" {
		t.Errorf("row 1 click opened %q", got)
	}
}

func TestHeaderDoubleClickOpensOnce(t *testing.T) {
	m := headerFixture(t, 160)
	sp := m.headerSpans()[0]
	m = headerPress(t, m, sp.x0, 0, tea.MouseButtonLeft)
	if topModal(m) != "sessions" || len(m.modals) != 1 {
		t.Fatalf("first click: %d modals", len(m.modals))
	}
	m.modals = nil // closed again, the pair is still pending
	m = headerPress(t, m, sp.x0, 0, tea.MouseButtonLeft)
	if len(m.modals) != 0 {
		t.Errorf("second click of a double-click opened %q", topModal(m))
	}
}

func TestHeaderClickDoesNothingWithAModalOpen(t *testing.T) {
	m := headerFixture(t, 160)
	sp := m.headerSpans()[3]
	m = headerPress(t, m, sp.x0, 0, tea.MouseButtonLeft)
	if topModal(m) != "loop" {
		t.Fatal("loop menu did not open")
	}
	// the outside click dismisses the menu and is consumed: the header behind opens nothing
	m = headerPress(t, m, 3, 0, tea.MouseButtonLeft)
	if len(m.modals) != 0 {
		t.Errorf("outside click left %d modals, want 0", len(m.modals))
	}
}

func TestHeaderHoverShadesExactlyTheSegment(t *testing.T) {
	forceANSI256(t)
	m := headerFixture(t, 160)
	for _, sp := range m.headerSpans() {
		h := moveTo(t, m, sp.x0, 0)
		row := strings.Split(h.View(), "\n")[0]
		if !strings.Contains(row, "48;5;17m") {
			t.Fatalf("no hover shade at %d", sp.x0)
		}
		plain := ansi.Strip(row)
		// the navy run starts at the segment and ends where the next unshaded cell begins
		i := strings.Index(row, "48;5;17m")
		before := ansi.StringWidth(ansi.Strip(row[:i]))
		if before != sp.x0 {
			t.Errorf("shade starts at %d, want %d (%q)", before, sp.x0, plain)
		}
	}
	h := moveTo(t, m, 2, 0)
	if strings.Contains(strings.Split(h.View(), "\n")[0], "48;5;17m") {
		t.Error("brand shaded")
	}
}

func TestHeaderMotionWithinASegmentIsDropped(t *testing.T) {
	m := headerFixture(t, 160)
	sp := m.headerSpans()[0]
	m = moveTo(t, m, sp.x0, 0)
	if got := MotionFilter(m, tea.MouseMsg{X: sp.x0 + 1, Y: 0, Action: tea.MouseActionMotion}); got != nil {
		t.Error("motion inside one segment passed")
	}
	if got := MotionFilter(m, tea.MouseMsg{X: sp.x1 + 5, Y: 0, Action: tea.MouseActionMotion}); got == nil {
		t.Error("motion off the segment was dropped")
	}
}

func TestHeaderInertWithMouseOff(t *testing.T) {
	m := headerFixture(t, 160).WithMouse(false)
	sp := m.headerSpans()[0]
	if got := topModal(headerPress(t, m, sp.x0, 0, tea.MouseButtonLeft)); got != "" {
		t.Errorf("opened %q", got)
	}
}

// inertFixture is headerFixture with a paused bead, a failed backup and a usage warning,
// the three header marks that are not targets.
func inertFixture(t *testing.T, w int) Model {
	t.Helper()
	beads := focusFixture()
	for i := range beads {
		if beads[i].ID == "loose" {
			beads[i].Labels = append(beads[i].Labels, "paused:manual")
		}
	}
	sess := map[string]session.Info{"mem": {State: session.Alive}, "loose": {State: session.Alive, Waiting: session.WaitTurn}}
	m, _ := focusModel(t, w, 40, beads, sess, map[string]bool{"other": true})
	m.digest = append(m.digest, eventlog.Event{Msg: "backup.failed"})
	m.usage = contract.PlanUsage{FiveHour: &contract.UsageWindow{Pct: 95}}
	return m.WithMouse(true)
}

func TestHeaderInertMarksAreNotTargets(t *testing.T) {
	m := inertFixture(t, 200)
	row := ansi.Strip(strings.Split(m.View(), "\n")[0])
	for _, want := range []string{"backup failed", "1‖", "5h 95%"} {
		if !strings.Contains(row, want) {
			t.Fatalf("header lacks %q: %q", want, row)
		}
	}
	spans := m.headerSpans()
	if len(spans) != 4 {
		t.Fatalf("spans = %v, want 4 targets in %q", spans, row)
	}
	text := func(i int) string { return ansi.Cut(row, spans[i].x0, spans[i].x1) }
	if !strings.HasSuffix(text(0), "HITL") || !strings.HasSuffix(text(1), "AFK") || !strings.HasSuffix(text(2), "waiting") || !strings.HasPrefix(text(3), "loop") {
		t.Errorf("spans cover %q %q %q %q after the backup mark in %q", text(0), text(1), text(2), text(3), row)
	}
	inert := func(s string) (int, int) {
		x0 := ansi.StringWidth(row[:strings.Index(row, s)])
		return x0, x0 + ansi.StringWidth(s)
	}
	for _, s := range []string{"backup failed (see Logs)", "1‖", "5h 95%"} {
		x0, x1 := inert(s)
		for x := x0; x < x1; x++ {
			if got := topModal(headerPress(t, m, x, 0, tea.MouseButtonLeft)); got != "" {
				t.Errorf("click on %q at %d opened %q", s, x, got)
			}
			if h := moveTo(t, m, x, 0); h.hover.hdr {
				t.Errorf("motion over %q at %d set a header hover", s, x)
			}
		}
	}
}

func TestClickInsideTheSessionsPickerLeavesTheStackAndLoopAlone(t *testing.T) {
	m := press(t, headerFixture(t, 160), runeKey("o"))
	if len(m.modals) != 1 {
		t.Fatalf("o opened %d modals", len(m.modals))
	}
	before := m.modals[0]
	loop := m.loop
	for _, y := range []int{m.h/2 - 1, m.h / 2, m.h/2 + 1} {
		n := headerPress(t, m, m.w/2, y, tea.MouseButtonLeft)
		if len(n.modals) != 1 || n.modals[0].id != before.id || n.modals[0].sessions != before.sessions || n.modals[0].kind != before.kind || n.modals[0].title != before.title {
			t.Errorf("click at row %d changed the modal stack: %+v", y, n.modals)
		}
		if !reflect.DeepEqual(n.loop, loop) {
			t.Errorf("click at row %d changed the loop state", y)
		}
	}
}
