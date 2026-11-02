package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/withdraw"
)

func xMouseOpen(t *testing.T, plan func(string) (withdraw.Plan, error)) (Model, *[]string) {
	t.Helper()
	m, calls := xOpen(t, 120, 40, plan, ok)
	return m.WithMouse(true), calls
}

func xClick(t *testing.T, m Model, x, y int) Model {
	t.Helper()
	m, cmd := modalMsg(t, m, x, y, tea.MouseButtonLeft, tea.MouseActionPress)
	return settle(t, m, cmd)
}

func TestXHintClickEqualsItsKey(t *testing.T) {
	cases := []struct {
		name  string
		plan  func(string) (withdraw.Plan, error)
		label string
		key   string
	}{
		{"plain", cleanPlan, "y/enter withdraw", "y"},
		{"loses", lossyPlan, "D discard and withdraw", "D"},
		{"barred", refusedPlan, "enter go to blocker", "enter"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mc, callsC := xMouseOpen(t, c.plan)
			x, y := findDrawn(t, mc, c.label)
			viaClick := xClick(t, mc, x, y)
			mk, callsK := xMouseOpen(t, c.plan)
			k := runeKey(c.key)
			if c.key == "enter" {
				k = tea.KeyMsg{Type: tea.KeyEnter}
			}
			viaKey := press(t, mk, k)
			if len(viaClick.modals) != len(viaKey.modals) || viaClick.status != viaKey.status ||
				fmt.Sprint(*callsC) != fmt.Sprint(*callsK) {
				t.Errorf("click (%d modals, %q, %v) != key (%d, %q, %v)", len(viaClick.modals), viaClick.status, *callsC,
					len(viaKey.modals), viaKey.status, *callsK)
			}
			if len(viaClick.modals) == 1 && viaClick.modals[0].xs.warn != viaKey.modals[0].xs.warn {
				t.Errorf("warn %q != %q", viaClick.modals[0].xs.warn, viaKey.modals[0].xs.warn)
			}
		})
	}
}

func TestXDiscardClickRunsVerbAtOnce(t *testing.T) {
	m, calls := xMouseOpen(t, lossyPlan)
	x, y := findDrawn(t, m, "D discard and withdraw")
	xClick(t, m, x, y)
	if !strings.Contains(fmt.Sprint(*calls), "withdraw sig discard=true") {
		t.Errorf("calls %v", *calls)
	}
}

func TestXRowsHoverAndSelectLikeJK(t *testing.T) {
	m, _ := xMouseOpen(t, cleanPlan)
	x, y := findDrawn(t, m, "T-b2")
	moved, _ := modalMsg(t, m, x, y, tea.MouseButtonNone, tea.MouseActionMotion)
	byKey := press(t, press(t, m, runeKey("j")), runeKey("j"))
	if moved.modals[0].cur != byKey.modals[0].cur || moved.modals[0].cur == 0 {
		t.Errorf("motion cur %d, j/k cur %d", moved.modals[0].cur, byKey.modals[0].cur)
	}
	x0, y0 := findDrawn(t, m, "T-b1")
	clicked := xClick(t, moved, x0, y0)
	if clicked.modals[0].cur != 1 || len(clicked.modals) != 1 {
		t.Errorf("press cur %d modals %d", clicked.modals[0].cur, len(clicked.modals))
	}
}

func TestXScrolledWindowAnswersOnlyDrawnRows(t *testing.T) {
	m, _ := xOpen(t, 120, 16, func(string) (withdraw.Plan, error) {
		p, _ := cleanPlan("")
		for i := 0; i < 12; i++ {
			b := p.Set[0]
			b.ID, b.Title = fmt.Sprintf("x%d", i), fmt.Sprintf("Extra-%d", i)
			p.Set = append(p.Set, b)
		}
		return p, nil
	}, ok)
	m = m.WithMouse(true)
	d := m.modals[len(m.modals)-1]
	lay := m.dialogLayout(d)
	drawn := 0
	for y := lay.y; y < lay.y+lay.h; y++ {
		if i := m.xRowAt(d, lay.x+3, y); i >= 0 {
			drawn++
			if !strings.Contains(ansi.Strip(strings.Split(m.View(), "\n")[y]), xPlanTree(d.xs.plan)[i].b.Title) {
				t.Errorf("line %d answers row %d it does not draw", y, i)
			}
		}
	}
	if drawn == 0 || drawn >= len(xPlanTree(d.xs.plan)) {
		t.Errorf("drawn rows answering: %d of %d", drawn, len(xPlanTree(d.xs.plan)))
	}
}

func TestXInertLinesAndOutsideClick(t *testing.T) {
	m, _ := xMouseOpen(t, lossyPlan)
	d := m.modals[0]
	lay := m.dialogLayout(d)
	rows := map[int]bool{}
	for _, f := range lay.fields {
		rows[lay.y+1+f.row] = true
	}
	for y := lay.y; y < lay.y+lay.h; y++ {
		if rows[y] {
			continue
		}
		for _, x := range []int{lay.x + 3, lay.x + lay.w/2} {
			if _, ok := m.hintAt(x, y); ok {
				continue
			}
			next := xClick(t, m, x, y)
			if len(next.modals) != 1 || next.modals[0].cur != d.cur || next.modals[0].xs.warn != d.xs.warn {
				t.Errorf("click (%d,%d) changed the dialog", x, y)
			}
		}
	}
	under := confirmDialog("Under", []string{"u"}, "", nil)
	m.modals = append([]dialog{under}, m.modals...)
	next := xClick(t, m, 0, 0)
	if len(next.modals) != 1 || next.modals[0].title != "Under" {
		t.Errorf("outside click left %d modals", len(next.modals))
	}
}

func TestXCancelDialogOutsideClosesAndHintsPress(t *testing.T) {
	m, _ := xMouseOpen(t, cleanPlan)
	d := m.modals[0]
	d.xs.cancel = &cancelState{}
	m.modals[0] = d
	if next := xClick(t, m, 0, 0); len(next.modals) != 0 {
		t.Error("outside click kept the cancel dialog")
	}
	if _, ok := m.hintAt(m.dialogLayout(d).x+3, m.dialogLayout(d).y+m.dialogLayout(d).h-2); !ok {
		t.Skip("no hint under the probe cell")
	}
}

func TestXHoverShadesOnlyTheHintLabel(t *testing.T) {
	m, _ := xMouseOpen(t, cleanPlan)
	x, y := findDrawn(t, m, "y/enter withdraw")
	next, _ := modalMsg(t, m, x, y, tea.MouseButtonNone, tea.MouseActionMotion)
	if !next.hover.opt || next.hover.optSpan.x0 != x || next.hover.optSpan.x1 != x+len("y/enter withdraw") {
		t.Errorf("hover %+v", next.hover)
	}
	next, _ = modalMsg(t, m, x+len("y/enter withdraw")+1, y, tea.MouseButtonNone, tea.MouseActionMotion)
	if next.hover.opt {
		t.Error("separator hovered")
	}
}
