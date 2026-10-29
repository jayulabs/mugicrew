package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// clickClock pins the double-click clock and returns a function that advances it.
func clickClock(t *testing.T) func(time.Duration) {
	t.Helper()
	prev, now := clock, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = prev })
	return func(d time.Duration) { now = now.Add(d) }
}

// clickXY is a left press at screen cell (x, y).
func clickXY(t *testing.T, m Model, x, y int) (Model, tea.Cmd) {
	t.Helper()
	return send(t, m.WithMouse(true), tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
}

// screenRow is the screen row whose text contains want, failing if none does.
func screenRow(t *testing.T, m Model, want string) int {
	t.Helper()
	for y, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if strings.Contains(l, want) {
			return y
		}
	}
	t.Fatalf("no row contains %q:\n%s", want, ansi.Strip(m.View()))
	return -1
}

func TestDoubleClickOpensWithinWindow(t *testing.T) {
	forceANSI256(t)
	adv := clickClock(t)
	m := queueFixtures(t)["ideate"]
	m = press(t, m, runeKey("j")) // off + New Signal, so the click selects it
	y := screenRow(t, m, "+ New Signal")
	m, _ = clickXY(t, m, 8, y)
	if len(m.modals) != 0 {
		t.Fatal("a single click opened the form")
	}
	adv(300 * time.Millisecond)
	m, _ = clickXY(t, m, 9, y)
	if len(m.modals) == 0 {
		t.Error("a second click within the window did not act as enter")
	}
}

func TestDoubleClickPastWindowDoesNotOpen(t *testing.T) {
	forceANSI256(t)
	adv := clickClock(t)
	m := queueFixtures(t)["ideate"]
	y := screenRow(t, m, "+ New Signal")
	m, _ = clickXY(t, m, 8, y)
	adv(451 * time.Millisecond)
	m, _ = clickXY(t, m, 8, y)
	if len(m.modals) != 0 {
		t.Error("a second click past the window opened the form")
	}
}

func TestHeaderClickFoldsAndNeverPairs(t *testing.T) {
	forceANSI256(t)
	clickClock(t)
	m := queueFixtures(t)["defog"]
	y := screenRow(t, m, "Live (1)")
	if !strings.Contains(ansi.Strip(m.View()), "t live") {
		t.Fatal("fixture: Live is not open")
	}
	m, _ = clickXY(t, m, 20, y)
	if strings.Contains(ansi.Strip(m.View()), "t live · live") && !strings.Contains(ansi.Strip(m.View()), "▸ Live") {
		t.Errorf("a header click did not fold:\n%s", ansi.Strip(m.View()))
	}
	m, _ = clickXY(t, m, 20, y)
	if !strings.Contains(ansi.Strip(m.View()), "▾ Live") {
		t.Errorf("a second header click did not unfold:\n%s", ansi.Strip(m.View()))
	}
}

func TestTreeRowCaretFoldsRestOnlySelects(t *testing.T) {
	forceANSI256(t)
	clickClock(t)
	m := queueFixtures(t)["defog"]
	y := screenRow(t, m, "▾ ○·  CHART  P1 · t c · c")
	col := strings.Index(ansi.Strip(strings.Split(m.View(), "\n")[y]), "▾")
	if col < 0 {
		t.Fatal("no caret on the Initiative row")
	}
	m1, _ := clickXY(t, m, col+30, y) // elsewhere: selects only
	if !strings.Contains(ansi.Strip(m1.View()), "t turn · turn  ") && strings.Count(ansi.Strip(m1.View()), "t turn") < 2 {
		t.Errorf("a click off the caret folded the row:\n%s", ansi.Strip(m1.View()))
	}
	m2, _ := clickXY(t, m, col+1, y)
	if strings.Count(ansi.Strip(m2.View()), "t turn") != strings.Count(ansi.Strip(m1.View()), "t turn")-1 {
		t.Errorf("a caret click did not fold its children:\n%s", ansi.Strip(m2.View()))
	}
}

func TestFooterBlockerLinkJumps(t *testing.T) {
	forceANSI256(t)
	m := blockerDefog(t, 30)
	var y int
	for i, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if strings.HasPrefix(l, "!") && strings.Contains(l, "gone") {
			y = i
		}
	}
	if y == 0 {
		t.Fatalf("no footer row for gone:\n%s", ansi.Strip(m.View()))
	}
	before := m.tab
	m, _ = clickXY(t, m, 3, y)
	if m.tab == before && !strings.Contains(cursorLine(m), "gone") {
		t.Errorf("a footer ! click did not jump to the blocker:\n%s", ansi.Strip(m.View()))
	}
}

func TestBackBarEscLeavesFocusRestInert(t *testing.T) {
	forceANSI256(t)
	m, _ := defaultFocus(t)
	m = ideaFocus(t, m)
	m2, _ := clickXY(t, m, 40, 1) // the title / counts
	if !focused(m2) {
		t.Error("a click on the rest of the back bar left the focus")
	}
	m3, _ := clickXY(t, m, 3, 1)
	if focused(m3) {
		t.Error("a click on ← esc back did not leave the focus")
	}
}
