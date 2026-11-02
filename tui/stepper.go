package tui

// The shared stepper primitive (ADR 0412 §1-3, §9): ◀ value ▶ around a stepped value, and
// the click targets it draws. Settings rows use it now; the forms' priority field reuses it.

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// stepTarget is one clickable part on a line: x0..x1 drawn, hx0..hx1 hit (end exclusive).
type stepTarget struct {
	line     int // 0 the row's first line, 1 the line under it
	x0, x1   int
	hx0, hx1 int    // an arrow's hit is its cell ±1 column; a label's is its drawn width
	key      string // "-", "+", "enter" or "esc": the key a press sends
	inert    bool   // a hidden arrow at a bound
}

// stepper draws ◀ value ▶: a side at its bound is a blank cell and an inert target, and
// hov (stepDown the ◀, stepUp the ▶, else none) shades that arrow with the hover navy. The
// targets' columns are relative to the string's start: ◀ first, then ▶.
func stepper(value string, atLo, atHi bool, hov int) (string, [2]stepTarget) {
	w := lipgloss.Width(value)
	arrow := func(glyph string, atBound, hovered bool) string {
		if atBound {
			return " "
		}
		st := lipgloss.NewStyle().Foreground(accent).Bold(true)
		if hovered {
			st = st.Background(hoverRow.GetBackground())
		}
		return st.Render(glyph)
	}
	s := arrow("◀", atLo, hov == stepDown) + " " + value + " " + arrow("▶", atHi, hov == stepUp)
	return s, [2]stepTarget{
		{x0: 0, x1: 1, hx0: -1, hx1: 2, key: "-", inert: atLo},
		{x0: w + 3, x1: w + 4, hx0: w + 2, hx1: w + 5, key: "+", inert: atHi},
	}
}

// saveLabel and discardLabel are the two targets a staged Settings row carries (ADR 0412 §4).
const (
	saveLabel    = "✓ save"
	discardLabel = "✗ discard"
)

// saveTargets draws "  ✓ save  ✗ discard" and its two targets on row line, each hit across
// its drawn label only, clipped to width w; hov is the hovered step key. A target the width
// cuts off entirely is dropped.
func saveTargets(line, w int, hov string) (string, []stepTarget) {
	label := func(text string, hovered bool) string {
		st := lipgloss.NewStyle().Foreground(accent).Bold(true)
		if hovered {
			st = st.Background(hoverRow.GetBackground())
		}
		return st.Render(text)
	}
	s := "  " + label(saveLabel, hov == "enter") + "  " + label(discardLabel, hov == "esc")
	ts := []stepTarget{
		{line: line, x0: 2, x1: 2 + lipgloss.Width(saveLabel), key: "enter"},
		{line: line, x0: 4 + lipgloss.Width(saveLabel), x1: 4 + lipgloss.Width(saveLabel) + lipgloss.Width(discardLabel), key: "esc"},
	}
	var out []stepTarget
	for _, t := range ts {
		t.x1 = min(t.x1, w)
		t.hx0, t.hx1 = t.x0, t.x1
		if t.x0 < w {
			out = append(out, t)
		}
	}
	return s, out
}

// hovSide is the stepper side a hover's step key names.
func hovSide(key string) int {
	switch key {
	case "-":
		return stepDown
	case "+":
		return stepUp
	}
	return stepNone
}

// targetKey is the key message a press on a target sends through the normal key path.
func targetKey(key string) tea.KeyMsg {
	switch key {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}

// stepClick is a left press at body cell (x, y) on a Settings step target: one step through
// the key path, never a selection or a double-click's enter, and an inert target swallows it.
func (m Model) stepClick(x, y int) (tea.Model, tea.Cmd, bool) {
	t, ok := m.settingsStepAt(m.h-chromeH, x, y)
	if !ok {
		return m, nil, false
	}
	m.click = lastClick{}
	if t.inert {
		return m, nil, true
	}
	next, cmd := m.sendKey(targetKey(t.key))
	return next, cmd, true
}
