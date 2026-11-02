package tui

import (
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// stepForms are the three forms that carry a priority field.
var stepForms = []struct {
	name string
	open func(t *testing.T) Model
}{
	{"signal", func(t *testing.T) Model { return mouseForm(t) }},
	{"initiative", func(t *testing.T) Model { return formModel(t).WithMouse(true).push(newInitiativeForm(false)) }},
	{"initiative with ref", func(t *testing.T) Model { return formModel(t).WithMouse(true).push(newInitiativeForm(true)) }},
}

func topPrio(m Model) int { return m.modals[len(m.modals)-1].sf.prio }

// focusPrio focuses the priority field by a click on its label, as a human would.
func focusPrio(t *testing.T, m Model) Model {
	t.Helper()
	x, y := rowOf(t, m, "priority:")
	return formClick(t, m, x, y)
}

func TestFormPriorityArrowsOnlyWhenFocusedAndCaptured(t *testing.T) {
	for _, f := range stepForms {
		t.Run(f.name, func(t *testing.T) {
			m := f.open(t)
			if drawn(m, "◀") || drawn(m, "▶") {
				t.Error("arrows drawn with the priority field unfocused")
			}
			m = focusPrio(t, m)
			if topPrio(m) != 2 {
				t.Errorf("focusing click changed priority to %d", topPrio(m))
			}
			if !drawn(m, "◀ P2 ▶") {
				t.Errorf("no ◀ P2 ▶ once focused:\n%s", ansi.Strip(m.View()))
			}
			if off := m.WithMouse(false); drawn(off, "◀") || drawn(off, "▶") {
				t.Error("arrows drawn with Mouse mode off")
			}
		})
	}
}

func TestFormPriorityArrowsStepAndClamp(t *testing.T) {
	for _, f := range stepForms {
		t.Run(f.name, func(t *testing.T) {
			m := focusPrio(t, f.open(t))
			x, y := glyphCell(t, m, "◀")
			m = formClick(t, m, x, y)
			m = formClick(t, m, x-1, y) // the hit area reaches one column out
			if topPrio(m) != 0 {
				t.Fatalf("two ◀ from P2: P%d", topPrio(m))
			}
			if drawn(m, "◀") {
				t.Error("◀ drawn at P0")
			}
			m = formClick(t, m, x, y) // inert
			if topPrio(m) != 0 || m.modals[len(m.modals)-1].sf.field != fieldPrio {
				t.Errorf("click on the hidden ◀: P%d field %d", topPrio(m), m.modals[len(m.modals)-1].sf.field)
			}
			for range 4 {
				ax, ay := glyphCell(t, m, "▶")
				m = formClick(t, m, ax, ay)
			}
			if topPrio(m) != 4 || drawn(m, "▶") {
				t.Errorf("P%d, ▶ drawn %v, want P4 with ▶ hidden", topPrio(m), drawn(m, "▶"))
			}
			if sf := m.modals[len(m.modals)-1].sf; sf.field != fieldPrio {
				t.Errorf("field %d, a step left the priority field", sf.field)
			}
		})
	}
}

func TestFormWheelOverArrowsNeverEdits(t *testing.T) {
	m := focusPrio(t, mouseForm(t))
	x, y := glyphCell(t, m, "▶")
	for _, b := range []tea.MouseButton{tea.MouseButtonWheelUp, tea.MouseButtonWheelDown} {
		m, _ = send(t, m, tea.MouseMsg{X: x, Y: y, Button: b, Action: tea.MouseActionPress})
	}
	if topPrio(m) != 2 {
		t.Errorf("wheel changed priority to %d", topPrio(m))
	}
}

func TestFormCtrlSSavesSteppedPriority(t *testing.T) {
	var upd []string
	seam(t, &newSignal, func(string, string) (string, error) { return "s9", nil })
	seam(t, &updateBead, func(id string, a []string) error { upd = append([]string{id}, a...); return nil })
	m := typeText(t, mouseForm(t), "Fix it")
	m = focusPrio(t, m)
	x, y := glyphCell(t, m, "◀")
	m = formClick(t, m, x, y)
	m = press(t, m, ctrlS())
	if !slices.Equal(upd, []string{"s9", "--priority", "1"}) {
		t.Errorf("update %v", upd)
	}
}

func TestFormMotionOverArrowsHoversAndPassesFilter(t *testing.T) {
	m := focusPrio(t, mouseForm(t))
	x, y := glyphCell(t, m, "▶")
	motion := func(x, y int) tea.MouseMsg { return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion} }
	if MotionFilter(m, motion(x, y)) == nil {
		t.Fatal("motion onto an arrow was dropped")
	}
	m, _ = send(t, m, motion(x, y))
	if m.hover.step != "+" {
		t.Fatalf("hover step %q, want +", m.hover.step)
	}
	if MotionFilter(m, motion(x+1, y)) != nil {
		t.Error("motion within the same arrow passed")
	}
	if MotionFilter(m, motion(x, y)) != nil {
		t.Error("motion that changes nothing passed")
	}
	if MotionFilter(m, motion(0, 0)) == nil {
		t.Error("motion off the arrow was dropped")
	}
	m, _ = send(t, m, motion(0, 0))
	if m.hover.set {
		t.Error("hover kept after leaving the arrow")
	}
}
