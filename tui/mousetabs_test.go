package tui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func mouseAt(t *testing.T, m Model, x, y int, b tea.MouseButton, a tea.MouseAction) Model {
	t.Helper()
	m, _ = send(t, m.WithMouse(true), tea.MouseMsg{X: x, Y: y, Button: b, Action: a})
	return m
}

func tabModel(t *testing.T, w int) Model {
	t.Helper()
	m, _ := send(t, queueFixtures(t)["defog"], tea.WindowSizeMsg{Width: w, Height: 24})
	return m
}

func TestTabClickSwitchesDoubleIsOneRightNothing(t *testing.T) {
	m := tabModel(t, 120)
	sp := m.strip(-1).spans[tabSettings]
	m = mouseAt(t, m, sp.x0+2, 2, tea.MouseButtonRight, tea.MouseActionPress)
	if m.tab == tabSettings {
		t.Fatal("right-click switched tabs")
	}
	m = mouseAt(t, m, sp.x0+2, 2, tea.MouseButtonLeft, tea.MouseActionPress)
	if m.tab != tabSettings {
		t.Fatalf("tab = %d, want settings", m.tab)
	}
	m = mouseAt(t, m, sp.x0+2, 2, tea.MouseButtonLeft, tea.MouseActionPress)
	if m.tab != tabSettings {
		t.Fatal("double-click moved off the tab")
	}
}

func TestTabHoverBrightensInactiveOnly(t *testing.T) {
	forceANSI256(t)
	m := tabModel(t, 120)
	other := (m.tab + 1) % len(m.tabs)
	sp := m.strip(-1).spans[other]
	m = mouseAt(t, m, sp.x0+2, 2, tea.MouseButtonNone, tea.MouseActionMotion)
	if !m.hover.tabOn || m.hover.tab != other {
		t.Fatalf("hover = %+v", m.hover)
	}
	if !strings.Contains(strings.Split(m.View(), "\n")[2], "38;5;252m") {
		t.Error("hovered tab label is not 252")
	}
	if got := MotionFilter(m, tea.MouseMsg{X: sp.x0 + 3, Y: 2, Action: tea.MouseActionMotion}); got != nil {
		t.Error("same-tab motion passed the filter")
	}
}

func TestTabStripSlides(t *testing.T) {
	m := tabModel(t, 40)
	if !m.strip(-1).clipped {
		t.Fatal("strip not clipped at 40")
	}
	first := m.strip(-1).start
	m = mouseAt(t, m, 5, 2, tea.MouseButtonWheelDown, tea.MouseActionPress)
	if got := m.strip(-1).start; got != first+1 || !m.tabHeld {
		t.Fatalf("start = %d held=%v, want %d", got, m.tabHeld, first+1)
	}
	m = mouseAt(t, m, 5, 2, tea.MouseButtonWheelRight, tea.MouseActionPress)
	if m.strip(-1).start != first+2 {
		t.Fatalf("start = %d", m.strip(-1).start)
	}
	st := m.strip(-1)
	m = mouseAt(t, m, st.left[0], 2, tea.MouseButtonLeft, tea.MouseActionPress)
	if m.strip(-1).start != first+1 {
		t.Fatalf("‹ click start = %d", m.strip(-1).start)
	}
	for i := 0; i < 10; i++ { // active may leave view
		m = mouseAt(t, m, 5, 2, tea.MouseButtonWheelDown, tea.MouseActionPress)
	}
	if strings.Contains(ansi.Strip(strings.Split(m.View(), "\n")[2]), "‹ ") == false {
		t.Error("no ‹ after sliding right")
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.tabHeld || m.strip(-1).start != first {
		t.Errorf("key did not return to following: held=%v start=%d", m.tabHeld, m.strip(-1).start)
	}
	m = mouseAt(t, m, 5, 2, tea.MouseButtonWheelDown, tea.MouseActionPress)
	sp := m.strip(-1).spans[0]
	m = mouseAt(t, m, sp.x0+2, 2, tea.MouseButtonLeft, tea.MouseActionPress)
	if m.tabHeld {
		t.Error("tab click kept the held start")
	}
}

func TestWheelOnUnclippedStripInert(t *testing.T) {
	m := tabModel(t, 120)
	m = mouseAt(t, m, 5, 2, tea.MouseButtonWheelDown, tea.MouseActionPress)
	if m.tabHeld {
		t.Error("unclipped strip held")
	}
}

// A tab switch by click, like the jump key, returns a wheel-scrolled list to Following.
func TestTabClickUnscrollsLists(t *testing.T) {
	forceANSI256(t)
	lines := func(m Model, top int) []string {
		out := screenBody(m, top)
		for i := range out {
			out[i] = normLine(out[i])
		}
		return out
	}
	for _, s := range listSurfaces() {
		if s.name == "focus" {
			continue // a tab click leaves the focus view altogether
		}
		base := s.model(t, 14)
		m := base
		for range 40 {
			m = wheel(t, m, s.top+1, false)
		}
		want := lines(base, s.top)
		if slices.Equal(lines(m, s.top), want) {
			continue // this list fits its window
		}
		tab := m.tab
		other := tabIdeate
		if tab == tabIdeate {
			other = tabDefog
		}
		m = mouseAt(t, m, m.strip(-1).spans[other].x0+2, 2, tea.MouseButtonLeft, tea.MouseActionPress)
		m = mouseAt(t, m, m.strip(-1).spans[tab].x0+2, 2, tea.MouseButtonLeft, tea.MouseActionPress)
		if m.tab != tab {
			t.Fatalf("%s: tab = %d, want %d", s.name, m.tab, tab)
		}
		if got := lines(m, s.top); !slices.Equal(got, want) {
			t.Errorf("%s: after a tab round trip the list is still scrolled:\n%q\nwant:\n%q", s.name, got, want)
		}
		return
	}
	t.Fatal("no surface scrolled")
}

func TestEscOnOpenMenuLeavesHeldTabStrip(t *testing.T) {
	m := queueFixtures(t)["defog"]
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 40, Height: 24})
	m.tabHeld = true
	m.menu = &ctxMenu{items: nil, mw: 5, mh: 3}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if !m.tabHeld || m.menu != nil {
		t.Fatalf("tabHeld=%v menu=%v", m.tabHeld, m.menu)
	}
}

func TestResizeKeepsMenuClampedOnScreen(t *testing.T) {
	m := queueFixtures(t)["defog"]
	m = m.WithMouse(true)
	m.menu = &ctxMenu{x: 90, y: 20, mw: 10, mh: 5, bead: m.gateData().beads[0].ID}
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 60, Height: 15})
	if m.menu == nil || m.menu.x+m.menu.mw > 60 || m.menu.y+m.menu.mh > 15 {
		t.Fatalf("menu=%+v", m.menu)
	}
}
