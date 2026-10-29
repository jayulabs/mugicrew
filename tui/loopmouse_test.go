package tui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"jayulabs.org/mugicrew/looplock"
)

func loopMenuModel(t *testing.T) Model {
	t.Helper()
	m := headerFixture(t, 100)
	m, _ = send(t, m, runeKey("o"))
	if topModal(m) != "loop" {
		t.Fatalf("loop menu not open: %q", topModal(m))
	}
	return m
}

// optCell is a cell inside the option whose key is k on the rendered options line.
func optCell(t *testing.T, m Model, k string) (int, int) {
	t.Helper()
	lay := m.dialogLayout(m.modals[len(m.modals)-1])
	for _, sp := range loopSpans(m) {
		if sp.key == k {
			return lay.x + 2 + sp.x0, lay.y + lay.h - 2
		}
	}
	t.Fatalf("no option %q in %+v", k, loopSpans(m))
	return 0, 0
}

func TestLoopOptionClickEqualsItsKey(t *testing.T) {
	for _, c := range []struct {
		name string
		st   looplock.Status
		keys []string
	}{
		{"off", loopOff, []string{"s", "esc"}},
		{"on", loopFromSource, []string{"r", "s", "esc"}},
		{"limited", looplock.Status{State: looplock.Limited, Pid: 7, Rev: "abc1234"}, []string{"r", "s", "esc"}},
		{"restarting", loopRestart, []string{"s", "esc"}},
		{"stop-drain", loopDraining, []string{"k", "f", "esc"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := loopMenuModel(t)
			m.loop = c.st
			var got []string
			for _, sp := range loopSpans(m) {
				got = append(got, sp.key)
				x, y := optCell(t, m, sp.key)
				viaClick, cmdC := modalMsg(t, m, x, y, tea.MouseButtonLeft, tea.MouseActionPress)
				k := tea.KeyMsg{Type: tea.KeyEsc}
				if sp.key != "esc" {
					k = runeKey(sp.key)
				}
				viaKey, cmdK := send(t, m, k)
				if len(viaClick.modals) != len(viaKey.modals) || (cmdC == nil) != (cmdK == nil) ||
					viaClick.loopBusy != viaKey.loopBusy || viaClick.loopRestarting != viaKey.loopRestarting ||
					viaClick.loopStarting != viaKey.loopStarting {
					t.Errorf("%s: click and key differ: modals %d/%d cmd %v/%v", sp.key, len(viaClick.modals), len(viaKey.modals), cmdC != nil, cmdK != nil)
				}
			}
			if !slices.Equal(got, c.keys) {
				t.Errorf("options %v, want %v", got, c.keys)
			}
		})
	}
}

func TestLoopEscCloseClickPops(t *testing.T) {
	m := loopMenuModel(t)
	x, y := optCell(t, m, "esc")
	m, _ = modalMsg(t, m, x, y, tea.MouseButtonLeft, tea.MouseActionPress)
	if len(m.modals) != 0 {
		t.Error("esc close click left the menu open")
	}
}

func TestLoopOptionClickGuards(t *testing.T) {
	for name, set := range map[string]func(*Model){
		"restarting": func(m *Model) { m.loopRestarting = true },
		"busy":       func(m *Model) { m.loopBusy = true },
	} {
		m := loopMenuModel(t)
		set(&m)
		for _, sp := range loopSpans(m) {
			if sp.key == "esc" || (name == "busy" && sp.key == "r") {
				continue
			}
			x, y := optCell(t, m, sp.key)
			next, cmd := modalMsg(t, m, x, y, tea.MouseButtonLeft, tea.MouseActionPress)
			if len(next.modals) != 1 || cmd != nil {
				t.Errorf("%s: click on %s not ignored", name, sp.key)
			}
		}
	}
}

func TestLoopInertCellsDoNothing(t *testing.T) {
	m := loopMenuModel(t)
	lay := m.dialogLayout(m.modals[0])
	y := lay.y + lay.h - 2
	spans := loopSpans(m)
	cells := [][2]int{
		{lay.x + 2 + spans[0].x1, y},   // the separator
		{lay.x + 3, lay.y + 1},         // title
		{lay.x + 3, lay.y + 3},         // status body
		{lay.x, y},                     // border
		{lay.x + lay.w - 1, y},         // border
		{lay.x + 5, lay.y + lay.h - 1}, // bottom border
	}
	for _, c := range cells {
		next, cmd := modalMsg(t, m, c[0], c[1], tea.MouseButtonLeft, tea.MouseActionPress)
		if len(next.modals) != 1 || cmd != nil {
			t.Errorf("click at %v acted", c)
		}
	}
}

func TestLoopOptionHoverShadesOnlyItsText(t *testing.T) {
	forceANSI256(t)
	m := loopMenuModel(t)
	sp := loopSpans(m)[0]
	x, y := optCell(t, m, sp.key)
	m, _ = modalMsg(t, m, x, y, tea.MouseButtonNone, tea.MouseActionMotion)
	lines := strings.Split(m.View(), "\n")
	row := lines[y]
	navy := "\x1b[48;5;17m"
	if !strings.Contains(row, navy) {
		t.Fatalf("no navy on the option row: %q", row)
	}
	lay := m.dialogLayout(m.modals[0])
	x0 := lay.x + 2 + sp.x0
	first, last := strings.Index(row, navy), strings.LastIndex(row, navy)
	if w := ansi.StringWidth(ansi.Strip(row[:first])); w != x0 {
		t.Errorf("shade starts at %d, want %d", w, x0)
	}
	if w := ansi.StringWidth(ansi.Strip(row[:last])); w > x0+(sp.x1-sp.x0) {
		t.Errorf("shade runs to %d, past the option's end %d", w, x0+(sp.x1-sp.x0))
	}
	m, _ = send(t, m, runeKey("x"))
	if strings.Contains(m.View(), navy) {
		t.Error("key did not clear hover")
	}
}

func TestMotionFilterLoopOptions(t *testing.T) {
	m := loopMenuModel(t)
	spans := loopSpans(m)
	lay := m.dialogLayout(m.modals[0])
	y := lay.y + lay.h - 2
	a := tea.MouseMsg{X: lay.x + 2 + spans[0].x0, Y: y, Action: tea.MouseActionMotion}
	if MotionFilter(m, a) == nil {
		t.Fatal("first motion onto an option dropped")
	}
	m, _ = send(t, m, a)
	a2 := a
	a2.X++
	if MotionFilter(m, a2) != nil {
		t.Error("motion within one option passed")
	}
	b := a
	b.X = lay.x + 2 + spans[1].x0
	if MotionFilter(m, b) == nil {
		t.Error("motion onto a different option dropped")
	}
	c := a
	c.X, c.Y = lay.x+3, lay.y+1
	if MotionFilter(m, c) == nil {
		t.Error("motion off the option (to clear hover) dropped")
	}
	m, _ = send(t, m, c)
	if MotionFilter(m, c) != nil {
		t.Error("resting on inert cells passed")
	}
	c.Y++
	if MotionFilter(m, c) != nil {
		t.Error("moving vertically across inert cells passed")
	}
}

// A wrapped options line takes its click targets from the drawn rows.
func TestLoopWrappedOptionsClickDrawnLabels(t *testing.T) {
	m := headerFixture(t, 40)
	m.loop = looplock.Status{State: looplock.Draining}
	m, _ = send(t, m, runeKey("o"))
	if topModal(m) != "loop" {
		t.Fatalf("loop menu not open: %q", topModal(m))
	}
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	find := func(label string) (int, int) {
		for y, r := range rows {
			if i := strings.Index(r, label); i >= 0 {
				return ansi.StringWidth(r[:i]), y
			}
		}
		t.Fatalf("no %q in frame", label)
		return 0, 0
	}
	for _, c := range []struct{ label, key string }{{"k keep on", "k"}, {"f force stop", "f"}, {"esc", "esc"}, {"close", "esc"}} {
		x, y := find(c.label)
		viaClick, cmdC := modalMsg(t, m, x, y, tea.MouseButtonLeft, tea.MouseActionPress)
		k := tea.KeyMsg{Type: tea.KeyEsc}
		if c.key != "esc" {
			k = runeKey(c.key)
		}
		viaKey, cmdK := send(t, m, k)
		if len(viaClick.modals) != len(viaKey.modals) || (cmdC == nil) != (cmdK == nil) || viaClick.status != viaKey.status {
			t.Errorf("click on drawn %q differs from key %s", c.label, c.key)
		}
	}
	x, y := find("close")
	next, _ := modalMsg(t, m, x, y, tea.MouseButtonLeft, tea.MouseActionPress)
	if len(next.modals) != 0 || strings.Contains(next.status, "resuming") {
		t.Errorf("close click: modals %d status %q", len(next.modals), next.status)
	}
}

func loopSpans(m Model) []loopOptSpan {
	_, spans := m.loopOptsLineSpans(m.loopOptsInner())
	return spans
}
