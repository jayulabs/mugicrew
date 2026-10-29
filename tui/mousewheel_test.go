package tui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func wheel(t *testing.T, m Model, y int, up bool) Model {
	t.Helper()
	b := tea.MouseButtonWheelDown
	if up {
		b = tea.MouseButtonWheelUp
	}
	m, _ = send(t, m.WithMouse(true), tea.MouseMsg{X: 5, Y: y, Button: b, Action: tea.MouseActionPress})
	return m
}

func bodyText(m Model, top int) string {
	return ansi.Strip(strings.Join(screenBody(m, top), "\n"))
}

func firstLine(m Model, top int) string { return normLine(screenBody(m, top)[0]) }

// The wheel moves the drawn window but not the selection.
func TestWheelScrollsListWithoutMovingSelection(t *testing.T) {
	forceANSI256(t)
	for _, s := range listSurfaces() {
		m := s.model(t, 14)
		want, before := s.sel(m), bodyText(m, s.top)
		after := wheel(t, m, s.top+1, false)
		if got := s.sel(after); got != want {
			t.Errorf("%s: the wheel moved the selection %q -> %q", s.name, want, got)
		}
		if bodyText(after, s.top) == before {
			t.Logf("%s: the list fits its window; nothing to scroll", s.name)
		}
	}
}

// A key snaps the list back: minimally when the selection is on screen, else re-centred.
func TestKeySnapsScrolledListBackToSelection(t *testing.T) {
	forceANSI256(t)
	scrolledAny := false
	for _, s := range listSurfaces() {
		m := s.model(t, 14)
		for range 40 {
			m = wheel(t, m, s.top+1, false)
		}
		if bodyText(m, s.top) == bodyText(s.model(t, 14), s.top) {
			continue
		}
		scrolledAny = true
		// space acts on the selection, which is off screen: the list re-centres on it first
		after := press(t, m, runeKey("k"))
		if selBodyLine(after, s.top) == "" {
			t.Errorf("%s: after a key the selection is not on screen", s.name)
		}
	}
	if !scrolledAny {
		t.Fatal("no surface scrolled")
	}
}

// scrolledSurface is the first surface whose list the wheel scrolls at h=14, with that model.
func scrolledSurface(t *testing.T) (listSurface, Model, Model) {
	t.Helper()
	for _, s := range listSurfaces() {
		m := s.model(t, 14)
		w := m
		for range 3 {
			w = wheel(t, w, s.top+1, false)
		}
		if bodyText(w, s.top) != bodyText(m, s.top) {
			return s, m, w
		}
	}
	t.Fatal("no surface scrolls")
	return listSurface{}, Model{}, Model{}
}

// A click keeps the list scrolled, with the clicked row selected under the pointer.
func TestClickKeepsScrolled(t *testing.T) {
	forceANSI256(t)
	s, _, m := scrolledSurface(t)
	top := firstLine(m, s.top)
	y := 0
	for i, l := range screenBody(m, s.top) { // a plain row: a fold would reshape the list
		if i > 0 && strings.Contains(l, "·") && !strings.ContainsAny(l, "▾▸") {
			y = i
			break
		}
	}
	if y == 0 {
		t.Fatal("no plain row on screen")
	}
	m = click(t, m, s.top+y)
	if got := firstLine(m, s.top); got != top {
		t.Errorf("%s: a click moved the window: first line %q, was %q", s.name, got, top)
	}
}

// A resize keeps the anchor row on top.
func TestScrolledListHoldsAnchorThroughResize(t *testing.T) {
	forceANSI256(t)
	s, _, m := scrolledSurface(t)
	top := firstLine(m, s.top)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: m.w - 7, Height: m.h})
	if got := firstLine(m, s.top); got != top {
		t.Errorf("%s: a resize moved the anchor: first line %q, was %q", s.name, got, top)
	}
}

// listView in isolation: the anchor holds, falls back to the nearest earlier survivor, then to Following.
func TestListViewAnchorFallback(t *testing.T) {
	keys := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	g := listGeom{n: len(keys), sel: 0, h: 4, keys: keys, at: func(int) hitTarget { return hitTarget{} }}
	lv := g.wheeled(listView{}, 3)
	if !lv.scrolled || lv.anchor != "d" {
		t.Fatalf("wheel by 3: %+v", lv)
	}
	at := func(keys []string) int { return lv.offsets(keys)(len(keys), 4, 0) }
	if got := at(keys); got != 3 {
		t.Errorf("anchor d is at %d, want 3", got)
	}
	if got := at(slices.Concat([]string{"x", "y"}, keys)); got != 5 {
		t.Errorf("rows inserted above: offset %d, want 5", got)
	}
	if got := at([]string{"a", "b", "e", "f", "g", "h", "i"}); got != 1 {
		t.Errorf("anchor removed: offset %d, want 1 (b, the nearest survivor above)", got)
	}
	if got := at([]string{"p", "q", "r", "s", "t", "u"}); got != followOffset(6, 4, 0) {
		t.Errorf("no survivor: offset %d, want Following", got)
	}
	if got := at(keys[:5]); got != 1 { // clamped to the new height
		t.Errorf("shorter list: offset %d, want 1", got)
	}
}

// snapped resumes from the held offset when the selection is in view, else re-centres.
func TestListViewSnapBack(t *testing.T) {
	keys := make([]string, 20)
	for i := range keys {
		keys[i] = string(rune('a' + i))
	}
	g := listGeom{n: 20, sel: 5, h: 6, keys: keys, at: func(int) hitTarget { return hitTarget{} }}
	lv := g.wheeled(listView{}, 3) // top 3, selection 5 in view
	if got := g.snapped(lv); !got.resume || got.off != 5 {
		t.Errorf("selection in view: %+v, want resume at 5", got)
	}
	lv = g.wheeled(lv, 9) // top 12, selection off screen
	if got := g.snapped(lv); got.scrolled || got.resume {
		t.Errorf("selection off screen: %+v, want Following", got)
	}
}

// The wheel over an open drawer scrolls the drawer, not the list.
func TestWheelOverDrawerScrollsDrawer(t *testing.T) {
	forceANSI256(t)
	s := listSurfaces()[0]
	m := press(t, s.model(t, 40), runeKey("d"))
	list := bodyText(m, s.top)[:200]
	m = wheel(t, m, 38, false)
	if g := graphOf(m); g.lv.scrolled {
		t.Error("the wheel over the drawer scrolled the list")
	} else if g.off == 0 {
		t.Log("the drawer text fits; nothing to scroll")
	}
	if bodyText(m, s.top)[:200] != list {
		t.Error("the wheel over the drawer moved the list")
	}
}

// A tab switch drops the scrolled offset on every tab; the selection stays.
func TestTabSwitchResetsScrolled(t *testing.T) {
	m := graphModel(t, 100, 30)
	g := m.tabs[m.tab].(graphTab)
	g.lv = listView{scrolled: true, anchor: "x"}
	want := g.sel
	m.tabs = slices.Clone(m.tabs)
	m.tabs[m.tab] = g
	tab := m.tab
	m = press(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.tab == tab {
		t.Fatal("tab did not switch")
	}
	if got := m.tabs[tab].(graphTab); got.lv.scrolled || got.sel != want {
		t.Errorf("after the switch: scrolled=%v sel=%q, want Following and %q", got.lv.scrolled, got.sel, want)
	}
}

func graphOf(m Model) graphTab { return m.tabAt(m.tab).(graphTab) }
