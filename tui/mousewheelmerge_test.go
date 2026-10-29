package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func wheelMsg(x, y int, up bool) tea.MouseMsg {
	b := tea.MouseButtonWheelDown
	if up {
		b = tea.MouseButtonWheelUp
	}
	return tea.MouseMsg{X: x, Y: y, Button: b, Action: tea.MouseActionPress}
}

// filtered runs msg through MotionFilter as the event loop does, then Update if it passes.
func viaFilter(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	out := MotionFilter(m, msg)
	if out == nil {
		return m
	}
	m, _ = send(t, m, out)
	return m
}

func scrolledModel(t *testing.T) (Model, listSurface) {
	for _, s := range listSurfaces() {
		m := s.model(t, 14).WithMouse(true)
		scrolls := bodyText(wheel(t, m, s.top+1, false), s.top) != bodyText(m, s.top)
		m.wheelAcc = &wheelAcc{} // the probe wheel armed the shared accumulator
		if scrolls {
			return m, s
		}
	}
	t.Fatal("no scrollable list surface")
	return Model{}, listSurface{}
}

func TestMergedWheelBurstEndsWhereSinglePressesDo(t *testing.T) {
	forceANSI256(t)
	m, s := scrolledModel(t)
	const n = 7
	single := m
	for range n {
		single, _ = send(t, single, wheelMsg(5, s.top+1, false))
	}
	burst := m
	for range n {
		burst = viaFilter(t, burst, wheelMsg(5, s.top+1, false))
	}
	burst, _ = send(t, burst, wheelFlush{})
	if got, want := bodyText(burst, s.top), bodyText(single, s.top); got != want {
		t.Errorf("merged burst differs from %d single presses:\n%s\nwant\n%s", n, got, want)
	}
	if burst.wheelAcc.armed {
		t.Error("accumulator still armed after the flush")
	}
}

func TestSingleWheelPressScrollsOnItsOwnUpdate(t *testing.T) {
	forceANSI256(t)
	m, s := scrolledModel(t)
	if MotionFilter(m, wheelMsg(5, s.top+1, false)) == nil {
		t.Fatal("an unarmed wheel press was swallowed")
	}
	after := viaFilter(t, m, wheelMsg(5, s.top+1, false))
	if bodyText(after, s.top) == bodyText(m, s.top) {
		t.Error("a single press did not scroll")
	}
}

func TestKeyInBurstSeesEveryEarlierDelta(t *testing.T) {
	forceANSI256(t)
	m, s := scrolledModel(t)
	want := m
	for range 6 {
		want, _ = send(t, want, wheelMsg(5, s.top+1, false))
	}
	want = press(t, want, runeKey("j"))
	got := m
	for range 6 {
		got = viaFilter(t, got, wheelMsg(5, s.top+1, false))
	}
	got = press(t, got, runeKey("j"))
	if bodyText(got, s.top) != bodyText(want, s.top) || s.sel(got) != s.sel(want) {
		t.Errorf("a key mid-burst acted on stale scroll state")
	}
}

func TestWheelAtAnotherCellOrOnStripIsNotMerged(t *testing.T) {
	forceANSI256(t)
	m, s := scrolledModel(t)
	m = viaFilter(t, m, wheelMsg(5, s.top+1, false)) // arms
	if MotionFilter(m, wheelMsg(5, s.top+2, false)) == nil {
		t.Error("a press at a different cell was merged")
	}
	if MotionFilter(m, wheelMsg(6, s.top+1, false)) == nil {
		t.Error("a press at a different column was merged")
	}
	if MotionFilter(m, wheelMsg(5, s.top+1, false)) != nil {
		t.Error("a same-cell press was not merged")
	}
	m.wheelAcc.x, m.wheelAcc.y = 5, 2 // armed on the tab strip
	if MotionFilter(m, wheelMsg(5, 2, false)) == nil {
		t.Error("a strip wheel press was merged")
	}
}

func TestWheelDoesNotMergeWithMouseOff(t *testing.T) {
	m, s := scrolledModel(t)
	m = viaFilter(t, m, wheelMsg(5, s.top+1, false))
	off := m.WithMouse(false)
	if MotionFilter(off, wheelMsg(5, s.top+1, false)) == nil {
		t.Error("merged with mouse mode off")
	}
}
