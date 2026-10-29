package tui

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// moveTo is pointer motion to screen cell (x, y).
func moveTo(t *testing.T, m Model, x, y int) Model {
	t.Helper()
	m, _ = send(t, m.WithMouse(true), tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	return m
}

// hoverRows is the screen rows of the frame drawn with the hover shade.
func hoverRows(m Model) []int {
	var ys []int
	for y, l := range strings.Split(m.View(), "\n") {
		if strings.Contains(l, "48;5;17m") {
			ys = append(ys, y)
		}
	}
	return ys
}

// stopRow is the screen row of the first stop that is not the selected row.
func stopRow(t *testing.T, m Model) int {
	t.Helper()
	ls := strings.Split(m.View(), "\n")
	for y := m.bodyTop(); y < m.h-2; y++ {
		if m.bodyHit(y-m.bodyTop()).kind == hitRow && !strings.Contains(ls[y], "48;5;25m") {
			return y
		}
	}
	t.Fatalf("no unselected stop:\n%s", ansi.Strip(m.View()))
	return -1
}

func TestHoverShadesRowAndClearsOverInertLines(t *testing.T) {
	forceANSI256(t)
	m := queueFixtures(t)["defog"]
	y := stopRow(t, m)
	m = moveTo(t, m, 5, y)
	if got := hoverRows(m); len(got) != 1 || got[0] != y {
		t.Fatalf("hover rows = %v, want [%d]", got, y)
	}
	m = moveTo(t, m, 5, 0) // the header
	if got := hoverRows(m); len(got) != 0 {
		t.Errorf("hover over chrome = %v", got)
	}
}

func TestHoverNeverShadesSelectedRow(t *testing.T) {
	forceANSI256(t)
	m := queueFixtures(t)["ideate"]
	y := screenRow(t, m, strings.TrimSpace(ansi.Strip(selLine(m))))
	m = moveTo(t, m, 5, y)
	if got := hoverRows(m); len(got) != 0 {
		t.Errorf("selected row shows hover on %v", got)
	}
}

func TestHoverKeepsTextAndNothingBold(t *testing.T) {
	forceANSI256(t)
	m := queueFixtures(t)["defog"]
	y := stopRow(t, m)
	plain := strings.Split(m.View(), "\n")[y]
	m = moveTo(t, m, 5, y)
	got := strings.Split(m.View(), "\n")[y]
	if strings.TrimRight(ansi.Strip(got), " ") != strings.TrimRight(ansi.Strip(plain), " ") {
		t.Errorf("hover changed the text:\n%q\n%q", got, plain)
	}
	if strings.Count(got, "\x1b[1m") > strings.Count(plain, "\x1b[1m") {
		t.Error("hover made the row bold")
	}
}

func TestKeyClearsHoverUntilNextMotion(t *testing.T) {
	forceANSI256(t)
	m := queueFixtures(t)["defog"]
	y := stopRow(t, m)
	m = moveTo(t, m, 5, y)
	if len(hoverRows(m)) == 0 {
		t.Fatal("no hover")
	}
	if MotionFilter(m, tea.MouseMsg{X: 6, Y: y, Action: tea.MouseActionMotion}) != nil {
		t.Error("same-target motion passed the filter")
	}
	if MotionFilter(m, tea.MouseMsg{X: 6, Y: 0, Action: tea.MouseActionMotion}) == nil {
		t.Error("motion to another target was dropped")
	}
	m = press(t, m, runeKey("k"))
	if len(hoverRows(m)) != 0 {
		t.Error("a key left hover")
	}
	if MotionFilter(m, tea.MouseMsg{X: 5, Y: y, Action: tea.MouseActionMotion}) == nil {
		t.Error("the first motion after a key was dropped")
	}
}

func TestRefreshRehitsRestingLine(t *testing.T) {
	forceANSI256(t)
	m := queueFixtures(t)["defog"]
	y := stopRow(t, m)
	m = moveTo(t, m, 5, y)
	key := m.hover.t.key
	m, _ = send(t, m, loadedMsg{beads: nil})
	if m.hover.t.key == key && m.hover.t.kind != hitNone {
		t.Errorf("hover kept %q after its row left", key)
	}
	if got := hoverRows(m); len(got) != 0 {
		t.Errorf("hover shaded %v with no stop there", got)
	}
}

func TestHoverOnBackBarAndFooterLink(t *testing.T) {
	forceANSI256(t)
	m, _ := defaultFocus(t)
	m = ideaFocus(t, m)
	m = moveTo(t, m, 3, 1)
	if got := hoverRows(m); len(got) != 1 || got[0] != 1 {
		t.Errorf("back hover rows = %v", got)
	}
	if !strings.Contains(ansi.Strip(strings.Split(m.View(), "\n")[1]), "← esc back") {
		t.Error("back text lost")
	}
	m = moveTo(t, m, 40, 1)
	if got := hoverRows(m); len(got) != 0 {
		t.Errorf("rest of back bar shaded: %v", got)
	}

	f := blockerDefog(t, 30)
	var y int
	for i, l := range strings.Split(ansi.Strip(f.View()), "\n") {
		if strings.HasPrefix(l, "!") && strings.Contains(l, "gone") {
			y = i
		}
	}
	if y == 0 {
		t.Fatal("no footer row")
	}
	f = moveTo(t, f, 3, y)
	if got := hoverRows(f); len(got) != 1 || got[0] != y {
		t.Errorf("footer hover rows = %v, want [%d]", got, y)
	}
}

func TestHoverIgnoredWithMouseOff(t *testing.T) {
	forceANSI256(t)
	m := queueFixtures(t)["defog"]
	m, _ = send(t, m.WithMouse(false), tea.MouseMsg{X: 5, Y: 6, Action: tea.MouseActionMotion})
	if m.hover.set || len(hoverRows(m)) != 0 {
		t.Error("hover with mouse off")
	}
}

// The Settings selector is foreground-only, so hover must know the selected row from the
// model: the selected setting never shows hover, any other does.
func TestHoverOnSettingsSkipsSelectedRow(t *testing.T) {
	m, _ := settingsModel(t)
	forceANSI256(t)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	var selY, otherY = -1, -1
	for y := range m.bodyH() {
		r := m.bodyHit(y)
		if r.kind != hitRow {
			continue
		}
		if r.key == strconv.Itoa(m.setRow) && selY < 0 {
			selY = m.bodyTop() + y
		} else if r.key != strconv.Itoa(m.setRow) && otherY < 0 {
			otherY = m.bodyTop() + y
		}
	}
	if selY < 0 || otherY < 0 {
		t.Fatalf("fixture: selY=%d otherY=%d", selY, otherY)
	}
	if got := hoverRows(moveTo(t, m, 5, selY)); len(got) != 0 {
		t.Errorf("selected setting shows hover on %v", got)
	}
	if got := hoverRows(moveTo(t, m, 5, otherY)); len(got) == 0 {
		t.Error("another setting shows no hover")
	}
}
