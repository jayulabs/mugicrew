package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Clicking a form, input or Surgery hint leaves the same Model as pressing its key.
func TestFormHintClickEqualsItsKey(t *testing.T) {
	sg := func(at, title string) func(*testing.T) Model {
		return func(t *testing.T) Model {
			m, _ := fileModel(t, false)
			m = sgTo(t, m, at)
			if title != "" {
				m = typeText(t, m, title)
			}
			return m.WithMouse(true)
		}
	}
	in := func(typed string) func(*testing.T) Model {
		return func(t *testing.T) Model {
			var ran bool
			m := modalFixture(t, inputDialog("In", []string{"a"}, "", acted(&ran))).WithMouse(true)
			if typed != "" {
				m = typeText(t, m, typed)
			}
			return m
		}
	}
	form := func(typed string) func(*testing.T) Model {
		return func(t *testing.T) Model {
			m := mouseForm(t)
			if typed != "" {
				m = typeText(t, m, typed)
			}
			return m
		}
	}
	cases := []struct {
		name, label, key string
		fix              func(*testing.T) Model
	}{
		{"form save refused", "ctrl+s save", "ctrl+s", form("")},
		{"form save", "ctrl+s save", "ctrl+s", form("A title")},
		{"form clean esc", "esc cancel", "esc", form("")},
		{"form dirty esc", "esc cancel", "esc", form("typed")},
		{"input submit", "enter submit", "enter", in("x")},
		{"input esc", "esc cancel", "esc", in("")},
		{"surgery files", "enter file", "enter", sg("+", "Fix")},
		{"surgery refused row", "enter file", "enter", sg("land", "Fix")},
		{"surgery empty title", "enter file", "enter", sg("free", "")},
		{"surgery esc", "esc cancel", "esc", sg("+", "Fix")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mc, mk := c.fix(t), c.fix(t)
			x, y := findHint(t, mc, c.label)
			viaClick, _ := modalMsg(t, mc, x, y, tea.MouseButtonLeft, tea.MouseActionPress)
			viaKey, _ := send(t, mk, keyMsgOf(c.key))
			if len(viaClick.modals) != len(viaKey.modals) || viaClick.status != viaKey.status || viaClick.View() != viaKey.View() {
				t.Errorf("click (%d modals, %q) != key (%d, %q)", len(viaClick.modals), viaClick.status, len(viaKey.modals), viaKey.status)
			}
		})
	}
}

// Navigation hints and cells off the answering hints change nothing, and hover shades
// only the hovered hint's own label.
func TestFormHintInertAndHover(t *testing.T) {
	for name, m := range map[string]Model{
		"form":    mouseForm(t),
		"surgery": func() Model { m, _ := fileModel(t, false); return m.WithMouse(true) }(),
	} {
		x, y := findHint(t, m, "tab")
		before := m.View()
		next, _ := modalMsg(t, m, x, y, tea.MouseButtonLeft, tea.MouseActionPress)
		if next.View() != before || len(next.modals) != 1 {
			t.Errorf("%s: tab hint click changed the model", name)
		}
		lay := m.dialogLayout(m.modals[0])
		for _, sep := range []int{lay.x + 1, lay.x + lay.w - 2} { // padding of the hint row
			next, _ = modalMsg(t, m, sep, y, tea.MouseButtonLeft, tea.MouseActionPress)
			if next.View() != before {
				t.Errorf("%s: padding click at %d changed the model", name, sep)
			}
		}
		label := "esc cancel"
		hx, hy := findHint(t, m, label)
		h, _ := modalMsg(t, m, hx, hy, tea.MouseButtonNone, tea.MouseActionMotion)
		if !h.hover.opt || h.hover.optSpan.x0 != hx || h.hover.optSpan.x1 != hx+len(label) {
			t.Errorf("%s: hover span %+v, want [%d,%d)", name, h.hover.optSpan, hx, hx+len(label))
		}
		if h.View() == before {
			t.Errorf("%s: hover did not shade", name)
		}
		off, _ := modalMsg(t, h, hx+len(label), hy, tea.MouseButtonNone, tea.MouseActionMotion)
		if off.hover.opt {
			t.Errorf("%s: separator hovered", name)
		}
		if off.View() != before {
			t.Errorf("%s: off-hint motion left a shade", name)
		}
	}
}
