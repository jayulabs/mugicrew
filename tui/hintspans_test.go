package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func hintConfirms(ran *string) map[string]dialog {
	act := func(tag string) func(Model, dialog) (Model, tea.Cmd) {
		return func(m Model, d dialog) (Model, tea.Cmd) { *ran += tag; m.setStatus("ran " + tag); return m, nil }
	}
	alt := confirmDialog("Sure?", []string{"body"}, "", act("y"))
	alt.alt, alt.altKey = act("a"), key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "other"))
	custom := confirmDialog("Sure?", []string{"body"}, "", act("y"))
	custom.noKey = key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit"))
	return map[string]dialog{"plain": confirmDialog("Sure?", []string{"body"}, "", act("y")), "alt": alt, "custom": custom}
}

func findDrawn(t *testing.T, m Model, label string) (int, int) {
	t.Helper()
	for y, r := range strings.Split(ansi.Strip(m.View()), "\n") {
		if i := strings.Index(r, label); i >= 0 {
			return ansi.StringWidth(r[:i]), y
		}
	}
	t.Fatalf("no %q in frame", label)
	return 0, 0
}

func TestConfirmHintClickEqualsItsKey(t *testing.T) {
	cases := []struct{ dlg, label, key string }{
		{"plain", "y/enter confirm", "y"}, {"plain", "n/esc cancel", "n"},
		{"alt", "a other", "a"}, {"alt", "y/enter confirm", "y"},
		{"custom", "q quit", "q"}, {"custom", "y/enter confirm", "y"},
	}
	for _, c := range cases {
		var ranC, ranK string
		mc := modalFixture(t, hintConfirms(&ranC)[c.dlg])
		mk := modalFixture(t, hintConfirms(&ranK)[c.dlg])
		x, y := findDrawn(t, mc, c.label)
		viaClick, _ := modalMsg(t, mc, x, y, tea.MouseButtonLeft, tea.MouseActionPress)
		viaKey, _ := send(t, mk, runeKey(c.key))
		if len(viaClick.modals) != len(viaKey.modals) || viaClick.status != viaKey.status || ranC != ranK {
			t.Errorf("%s %q: click (%d modals, %q, ran %q) != key (%d, %q, ran %q)", c.dlg, c.label,
				len(viaClick.modals), viaClick.status, ranC, len(viaKey.modals), viaKey.status, ranK)
		}
	}
}

func TestConfirmHintWrappedAndCollapsedStayClickable(t *testing.T) {
	// at the floor width the long labels collapse to key-only text; a key text too long
	// for even that wraps across rows, and every word of it still answers
	cases := []struct {
		name, altKey string
		words        map[string]string
	}{
		{"collapsed", "a", map[string]string{"y/enter": "y", "a": "a", "n/esc": "n"}},
		{"wrapped", "ctrl+alt+shift+delete", map[string]string{"y/enter": "y", "ctrl+alt+shift+delete": "ctrl+alt+shift+delete", "n/esc": "n"}},
	}
	for _, c := range cases {
		var ran string
		m := headerFixture(t, floorCols)
		d := hintConfirms(&ran)["alt"]
		d.altKey = key.NewBinding(key.WithKeys(c.altKey), key.WithHelp(c.altKey, "other"))
		m = m.push(d)
		_, spans := hintLine(d.bindings(), m.dialogWidth()-4)
		if rows := spans[len(spans)-1].row; c.name == "wrapped" && rows == 0 {
			t.Fatal("hint line did not wrap")
		}
		for word, k := range c.words {
			x, y := findHint(t, m, word)
			via, _ := modalMsg(t, m, x, y, tea.MouseButtonLeft, tea.MouseActionPress)
			ranClick := ran
			ran = ""
			pressed, _ := send(t, m, keyMsgOf(k))
			if len(via.modals) != len(pressed.modals) || via.status != pressed.status || ranClick != ran {
				t.Errorf("%s %q: click (%d modals, %q, ran %q) differs from key %s (%d, %q, ran %q)", c.name, word,
					len(via.modals), via.status, ranClick, k, len(pressed.modals), pressed.status, ran)
			}
			ran = ""
		}
	}
}

// findHint is the last drawn occurrence of label: the hint line is the box's bottom rows.
func findHint(t *testing.T, m Model, label string) (int, int) {
	t.Helper()
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	lay := m.dialogLayout(m.modals[len(m.modals)-1])
	for y := lay.y + lay.h - 2; y >= lay.y; y-- {
		if i := strings.LastIndex(rows[y], label); i >= 0 {
			return ansi.StringWidth(rows[y][:i]), y
		}
	}
	t.Fatalf("no %q in frame", label)
	return 0, 0
}

func TestConfirmInertCellsAndNoNavigation(t *testing.T) {
	var ran string
	m := modalFixture(t, hintConfirms(&ran)["plain"])
	x, y := findDrawn(t, m, "•")
	for _, dx := range []int{0} {
		next, _ := modalMsg(t, m, x+dx, y, tea.MouseButtonLeft, tea.MouseActionPress)
		if len(next.modals) != 1 || ran != "" {
			t.Error("separator click did something")
		}
	}
	if answering(keyDown) || answering(keyField) || answering(keySgJK) || !answering(keyYes) {
		t.Error("answering set wrong")
	}
}

func TestConfirmHintHoverShadesOnlyItsLabel(t *testing.T) {
	forceANSI256(t)
	var ran string
	m := modalFixture(t, hintConfirms(&ran)["plain"])
	x, y := findDrawn(t, m, "n/esc cancel")
	m, _ = modalMsg(t, m, x, y, tea.MouseButtonNone, tea.MouseActionMotion)
	row := strings.Split(m.View(), "\n")[y]
	navy := "\x1b[48;5;17m"
	first, last := strings.Index(row, navy), strings.LastIndex(row, navy)
	if first < 0 {
		t.Fatalf("no navy: %q", row)
	}
	if w := ansi.StringWidth(ansi.Strip(row[:first])); w != x {
		t.Errorf("shade starts at %d, want %d", w, x)
	}
	if w := ansi.StringWidth(ansi.Strip(row[:last])); w > x+len("n/esc cancel") {
		t.Errorf("shade runs to %d", w)
	}
	a := tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion}
	if MotionFilter(m, a) != nil {
		t.Error("resting on the same hint passed")
	}
	yx, yy := findDrawn(t, m, "y/enter confirm")
	if MotionFilter(m, tea.MouseMsg{X: yx, Y: yy, Action: tea.MouseActionMotion}) == nil {
		t.Error("moving to another hint dropped")
	}
}

func TestCollapsedNavigationHintsStayInert(t *testing.T) {
	// a box too narrow for descriptions collapses every hint to key-only text; the
	// navigation ones must still not answer
	for _, nav := range navBindings {
		bs := []key.Binding{nav, keySgFile, keyCancel}
		for _, w := range []int{80, 12} {
			_, spans := hintLine(bs, w)
			for _, sp := range spans {
				if sp.b.Help().Key == nav.Help().Key && answering(sp.b) {
					t.Errorf("w=%d: navigation hint %q answers", w, nav.Help().Key)
				}
			}
		}
	}
}
