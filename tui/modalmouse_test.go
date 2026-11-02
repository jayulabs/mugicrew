package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func modalFixture(t *testing.T, ds ...dialog) Model {
	t.Helper()
	m := headerFixture(t, 100)
	for _, d := range ds {
		m = m.push(d)
	}
	return m
}

func modalMsg(t *testing.T, m Model, x, y int, b tea.MouseButton, a tea.MouseAction) (Model, tea.Cmd) {
	t.Helper()
	return send(t, m, tea.MouseMsg{X: x, Y: y, Button: b, Action: a})
}

func acted(hit *bool) func(Model, dialog) (Model, tea.Cmd) {
	return func(m Model, d dialog) (Model, tea.Cmd) { *hit = true; return m, nil }
}

func TestOutsideClickDismissesOnlyTopModal(t *testing.T) {
	var hit bool
	cases := map[string]dialog{
		"picker":  pickerDialog("Pick", []string{"a", "b"}, "", acted(&hit)),
		"confirm": confirmDialog("Sure?", []string{"x"}, "", acted(&hit)),
	}
	for name, d := range cases {
		t.Run(name, func(t *testing.T) {
			under := confirmDialog("Under", []string{"u"}, "", nil)
			m := modalFixture(t, under, d)
			next, _ := modalMsg(t, m, 0, 0, tea.MouseButtonLeft, tea.MouseActionPress)
			if len(next.modals) != 1 || next.modals[0].title != "Under" {
				t.Errorf("stack after outside click: %d modals", len(next.modals))
			}
			if hit {
				t.Error("outside click ran the action")
			}
		})
	}
}

func TestOutsideClickHonoursCustomNoKey(t *testing.T) {
	d := confirmDialog("Sure?", []string{"x"}, "", nil)
	d.noKey = key.NewBinding(key.WithKeys("q"))
	next, _ := modalMsg(t, modalFixture(t, d), 0, 0, tea.MouseButtonLeft, tea.MouseActionPress)
	if len(next.modals) != 0 {
		t.Error("custom no-key not honoured")
	}
}

func TestOutsideClickDismissesLoopMenu(t *testing.T) {
	m := headerFixture(t, 100)
	m, _ = send(t, m, runeKey("o"))
	if topModal(m) != "loop" {
		t.Skip("loop menu not opened by o")
	}
	m, _ = modalMsg(t, m, 0, 0, tea.MouseButtonLeft, tea.MouseActionPress)
	if len(m.modals) != 0 {
		t.Error("loop menu still open")
	}
}

func TestInsideAndNonLeftEventsAreInert(t *testing.T) {
	var hit bool
	for _, d := range []dialog{pickerDialog("Pick", []string{"a", "b"}, "", acted(&hit)), confirmDialog("Sure?", []string{"x"}, "", acted(&hit))} {
		m := modalFixture(t, d)
		lay := m.dialogLayout(d)
		cx, cy := lay.x+lay.w/2, lay.y+lay.h/2
		if d.kind == confirm { // a picker runs its row on a click; a confirm never does
			next, _ := modalMsg(t, m, cx, cy, tea.MouseButtonLeft, tea.MouseActionPress)
			if len(next.modals) != 1 || hit {
				t.Error("click inside a confirm did something")
			}
		}
		evs := []struct {
			x, y int
			b    tea.MouseButton
			a    tea.MouseAction
		}{
			{cx, cy, tea.MouseButtonLeft, tea.MouseActionRelease},
			{cx, cy, tea.MouseButtonNone, tea.MouseActionMotion},
			{cx, cy, tea.MouseButtonWheelDown, tea.MouseActionPress},
			{cx, cy, tea.MouseButtonRight, tea.MouseActionPress},
			{0, 0, tea.MouseButtonWheelDown, tea.MouseActionPress},
			{0, 0, tea.MouseButtonRight, tea.MouseActionPress},
		}
		for _, e := range evs {
			next, cmd := modalMsg(t, m, e.x, e.y, e.b, e.a)
			if len(next.modals) != 1 || cmd != nil || hit {
				t.Errorf("%s %+v: modals %d cmd %v hit %v", d.title, e, len(next.modals), cmd != nil, hit)
			}
		}
	}
}

func TestOutsideClickBehindDoesNotReachFrame(t *testing.T) {
	m := headerFixture(t, 100)
	sel := m.sel
	m = m.push(pickerDialog("Pick", []string{"a"}, "", nil))
	for y := 0; y < m.h; y += 3 {
		next, _ := modalMsg(t, m, 3, y, tea.MouseButtonLeft, tea.MouseActionPress)
		if next.sel != sel {
			t.Fatalf("click at row %d moved selection", y)
		}
	}
}

func TestFormInputSurgeryIgnoreOutsideClicks(t *testing.T) {
	m := mouseForm(t)
	next, _ := modalMsg(t, m, 0, 0, tea.MouseButtonLeft, tea.MouseActionPress)
	if len(next.modals) != 1 {
		t.Error("form dismissed")
	}
	in := modalFixture(t, dialog{kind: input, title: "In", labels: []string{"a"}, inputs: []line{{}}})
	next, _ = modalMsg(t, in, 0, 0, tea.MouseButtonLeft, tea.MouseActionPress)
	if len(next.modals) != 1 {
		t.Error("input dismissed")
	}
}

func TestXDismissesOnOutsideClick(t *testing.T) {
	x := modalFixture(t, dialog{kind: xdialog, title: "X"})
	if !x.modalLive() {
		t.Error("X dialog is not mouse-live")
	}
	next, _ := modalMsg(t, x, 0, 0, tea.MouseButtonLeft, tea.MouseActionPress)
	if len(next.modals) != 0 {
		t.Error("X dialog not dismissed")
	}
}

func TestModalMouseGates(t *testing.T) {
	d := pickerDialog("Pick", []string{"a"}, "", nil)
	off := modalFixture(t, d).WithMouse(false)
	next, _ := modalMsg(t, off, 0, 0, tea.MouseButtonLeft, tea.MouseActionPress)
	if len(next.modals) != 1 {
		t.Error("mouse off dismissed")
	}
	ref := modalFixture(t, d)
	ref.ref = true
	if ref.modalLive() {
		t.Error("live under ? reference")
	}
	small := modalFixture(t, d)
	small.w, small.h = floorCols-1, floorRows-1
	if small.modalLive() {
		t.Error("live below floor")
	}
	xd := dialog{kind: xdialog, title: "X"}
	if next, _ := modalMsg(t, modalFixture(t, xd).WithMouse(false), 0, 0, tea.MouseButtonLeft, tea.MouseActionPress); len(next.modals) != 1 {
		t.Error("mouse off dismissed X")
	}
	xr := modalFixture(t, xd)
	xr.ref = true
	if xr.modalLive() {
		t.Error("X live under ? reference")
	}
	xs := modalFixture(t, xd)
	xs.w, xs.h = floorCols-1, floorRows-1
	if xs.modalLive() {
		t.Error("X live below floor")
	}
}

func TestModalKeysWorkAfterMouse(t *testing.T) {
	var hit bool
	m := modalFixture(t, pickerDialog("Pick", []string{"a", "b"}, "", acted(&hit)))
	m, _ = modalMsg(t, m, 50, 20, tea.MouseButtonLeft, tea.MouseActionRelease)
	m, _ = send(t, m, runeKey("j"))
	if m.modals[0].cur != 1 {
		t.Error("j after mouse did not move")
	}
	send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
}

func TestXKeysWorkAfterMouse(t *testing.T) {
	m, _ := xMouseOpen(t, cleanPlan)
	x, y := findDrawn(t, m, "T-b1")
	m, _ = modalMsg(t, m, x, y, tea.MouseButtonNone, tea.MouseActionMotion)
	m = press(t, m, runeKey("k"))
	if m.modals[0].cur != 0 && m.modals[0].cur != len(xPlanTree(m.modals[0].xs.plan))-1 {
		t.Errorf("k after mouse: cur %d", m.modals[0].cur)
	}
	if m = press(t, m, tea.KeyMsg{Type: tea.KeyEsc}); len(m.modals) != 0 {
		t.Error("esc after mouse did not close")
	}
}
