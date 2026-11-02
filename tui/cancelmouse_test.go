package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/withdraw"
)

func cancelMouseOpen(t *testing.T, w int, plan func() withdraw.Plan) (Model, *[]cancelCall) {
	t.Helper()
	m, calls := cancelOpenAt(t, w, plan)
	return m.WithMouse(true), calls
}

// cancelSnap is the observable cancel state a click and its key must agree on.
func cancelSnap(m Model, calls *[]cancelCall) string {
	if len(m.modals) == 0 {
		return fmt.Sprintf("closed %q %v", m.status, *calls)
	}
	d := m.modals[len(m.modals)-1]
	if d.xs.cancel == nil {
		return fmt.Sprintf("other %q", d.title)
	}
	cs := d.xs.cancel
	return fmt.Sprintf("modals=%d cur=%d rel=%v why=%q on=%v review=%v flash=%q status=%q calls=%v",
		len(m.modals), d.cur, cs.release, cs.why.text, cs.whyOn, cs.review, cs.flash, m.status, *calls)
}

func TestCancelHintClickEqualsItsKey(t *testing.T) {
	typeWhy := func(m Model) Model {
		m = press(t, m, tabKey())
		for _, r := range "because" {
			m = press(t, m, runeKey(string(r)))
		}
		return m
	}
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	cases := []struct {
		name  string
		plan  func() withdraw.Plan
		setup func(Model) Model
		label string
		key   tea.KeyMsg
	}{
		{"review with empty why flashes", cancelPlan(), func(m Model) Model { return m }, "enter review", enter},
		{"review with a why", cancelPlan(), func(m Model) Model { return press(t, typeWhy(m), tabKey()) }, "enter review", enter},
		{"review yes", cancelPlan(), func(m Model) Model { return review(t, m) }, "y/enter discard and cancel", runeKey("y")},
		{"review back", cancelPlan(), func(m Model) Model { return review(t, m) }, "esc back", tea.KeyMsg{Type: tea.KeyEsc}},
		{"barred", func() withdraw.Plan {
			p := cancelPlan()()
			p.Refusals = []withdraw.Refusal{{Beads: []string{"ca"}}}
			return p
		}, func(m Model) Model { return m }, "enter go to blocker", enter},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mc, callsC := cancelMouseOpen(t, 160, c.plan)
			mc = c.setup(mc)
			x, y := findDrawn(t, mc, c.label)
			viaClick := xClick(t, mc, x, y)
			mk, callsK := cancelMouseOpen(t, 160, c.plan)
			viaKey := press(t, c.setup(mk), c.key)
			if a, b := cancelSnap(viaClick, callsC), cancelSnap(viaKey, callsK); a != b {
				t.Errorf("click %s\n key   %s", a, b)
			}
		})
	}
}

func TestCancelLabelClickEqualsSelectAndPress(t *testing.T) {
	for _, w := range []int{160, 70} {
		for _, locked := range []bool{false, true} {
			for _, label := range []string{"( ) release", "(●) close"} {
				name := fmt.Sprintf("w%d locked=%v %s", w, locked, label)
				t.Run(name, func(t *testing.T) {
					var lk []withdraw.Lock
					if locked {
						lk = []withdraw.Lock{{Head: "cb", Why: "adopt it elsewhere"}}
					}
					mc, callsC := cancelMouseOpen(t, w, cancelPlan(lk...))
					// the second chain's choice line is the last label of its text in the frame
					rows := strings.Split(viewOf(mc), "\n")
					x, y := -1, -1
					for yy, r := range rows {
						if strings.Contains(r, "T-cb") {
							y = yy // the chain's own line, or its choice line under it when narrow
							if w < xChoiceNarrow+10 {
								y++
							}
						}
					}
					if locked && label == "( ) release" {
						label = "🔒 locked"
					}
					if i := strings.Index(rows[y], label); i >= 0 {
						x = len([]rune(rows[y][:i]))
					}
					if x < 0 {
						t.Fatalf("no %q on line %q", label, rows[y])
					}
					// the cursor starts on the first chain, so the click must also select the second
					viaClick := xClick(t, mc, x, y)
					mk, callsK := cancelMouseOpen(t, w, cancelPlan(lk...))
					k := "r"
					if strings.Contains(label, "close") {
						k = "c"
					}
					viaKey := press(t, press(t, mk, runeKey("j")), runeKey(k))
					if a, b := cancelSnap(viaClick, callsC), cancelSnap(viaKey, callsK); a != b {
						t.Errorf("click %s\n key   %s", a, b)
					}
					if locked && k == "r" && !strings.Contains(viaClick.modals[0].xs.cancel.flash, "stays closed") {
						t.Error("locked release did not flash")
					}
				})
			}
		}
	}
}

func TestCancelWhyClickFocusesAndChainClickReturns(t *testing.T) {
	m, _ := cancelMouseOpen(t, 160, cancelPlan())
	x, y := findDrawn(t, m, "(tab to say why)")
	m = xClick(t, m, x, y)
	if !m.modals[0].xs.cancel.whyOn {
		t.Fatal("why click did not focus")
	}
	if again := xClick(t, m, x, y); !again.modals[0].xs.cancel.whyOn {
		t.Error("a second why click lost focus")
	}
	m = press(t, m, runeKey("c")) // typed into the why
	cx, cy := findDrawn(t, m, "T-cb")
	m = xClick(t, m, cx, cy)
	cs := m.modals[0].xs.cancel
	if cs.whyOn || m.modals[0].cur != 1 || cs.why.text != "c" {
		t.Errorf("chain click: whyOn=%v cur=%d why=%q", cs.whyOn, m.modals[0].cur, cs.why.text)
	}
}

func TestCancelOutsideClickStepsBackOrPops(t *testing.T) {
	m, _ := cancelMouseOpen(t, 160, cancelPlan())
	r := review(t, m)
	back := xClick(t, r, 0, 0)
	if len(back.modals) != 1 || back.modals[0].xs.cancel.review {
		t.Errorf("review outside click: %d modals review=%v", len(back.modals), back.modals[0].xs.cancel.review)
	}
	if next := xClick(t, back, 0, 0); len(next.modals) != 0 {
		t.Error("rows outside click kept the dialog")
	}
}

func TestCancelInertCells(t *testing.T) {
	m, _ := cancelMouseOpen(t, 160, cancelPlan(withdraw.Lock{Head: "ca", Why: "adopt it elsewhere"}))
	want := cancelSnap(m, &[]cancelCall{})
	for _, label := range []string{"Closes 2 chains", "🔒 adopt it elsewhere", "tab why", "j/k"} {
		x, y := findDrawn(t, m, label)
		if got := cancelSnap(xClick(t, m, x, y), &[]cancelCall{}); got != want {
			t.Errorf("click on %q changed the dialog:\n%s\n%s", label, got, want)
		}
	}
}
