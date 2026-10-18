package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
)

// p on a Defog, Execute or focus-view bead row runs Graph's pause toggle: the same
// writes, and the hints bar offers it just before x on open rows.
func TestQueueRowPauseKey(t *testing.T) {
	var calls *[]string
	defog := func(t *testing.T) Model { var m Model; m, calls = defogActModel(t); return m }
	exec := func(t *testing.T) Model { var m Model; m, calls = execModel(t, execFixture()); return m }
	for _, c := range []struct {
		name string
		m    func(t *testing.T) Model
		at   func(t *testing.T, m Model) Model
		want string
	}{
		{"defog leaf", defog, func(t *testing.T, m Model) Model { return defogAt(t, m, "ready") }, "pauseBead ready"},
		{"defog paused leaf", defog, func(t *testing.T, m Model) Model { return defogAt(t, m, "paused") }, "unpauseBead paused"},
		{"defog afk", defog, func(t *testing.T, m Model) Model { return defogAt(t, m, "afk") }, "pauseBead afk"},
		{"defog chart", defog, func(t *testing.T, m Model) Model { return defogAt(t, m, "c") }, "pauseBead c"},
		{"exec afk", exec, func(t *testing.T, m Model) Model { return execAt(t, unfolded(t, m), "sg") }, "pauseBead sg"},
		{"exec hitl", exec, func(t *testing.T, m Model) Model { return execAt(t, unfolded(t, m), "pz") }, "unpauseBead pz"},
		{"exec checkpoint", exec, func(t *testing.T, m Model) Model { return execAt(t, m, "cs") }, "pauseBead cs"},
		{"focus defog", func(t *testing.T) Model {
			var m Model
			m, calls = defaultFocus(t)
			return ideaFocus(t, m)
		}, func(t *testing.T, m Model) Model { return focusAt(t, m, "t g") }, "pauseBead g"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := c.at(t, c.m(t))
			h := hintsOf(m)
			verb := strings.Fields(c.want)[0]
			label := map[string]string{"pauseBead": "p pause", "unpauseBead": "p unpause"}[verb]
			if i, j := strings.Index(h, label), strings.Index(h, "x "); i < 0 || (j >= 0 && i > j) {
				t.Errorf("hints = %q, want %q before x", h, label)
			}
			n, cmd := send(t, m, runeKey("p"))
			settle(t, n, cmd)
			if !slices.Contains(*calls, c.want) {
				t.Errorf("calls = %v, want %q", *calls, c.want)
			}
		})
	}
}

// A closed bead row is refused with Graph's status, writes nothing and hints p under ? only.
func TestQueueRowPauseClosed(t *testing.T) {
	m, calls := defogActModel(t)
	m = defogAt(t, press(t, defogAt(t, m, "c/closed"), runeKey(" ")), "closed")
	if h := hintsOf(m); strings.Contains(h, "p pause") {
		t.Errorf("closed row hints p: %q", h)
	}
	if !slices.ContainsFunc(m.tabExtra(), func(b key.Binding) bool { return b.Keys()[0] == "p" }) {
		t.Errorf("? does not list p on a closed row")
	}
	n, cmd := send(t, m, runeKey("p"))
	if !strings.Contains(statusOf(n), "closed beads are never dispatched") {
		t.Errorf("status %q", statusOf(n))
	}
	settle(t, n, cmd)
	if len(*calls) != 0 {
		t.Errorf("wrote %v", *calls)
	}
}

// Rows without a bead, and the focus Initiative header, leave p alone.
func TestQueueRowPauseNoBead(t *testing.T) {
	m, calls := defogActModel(t)
	for _, id := range []string{"i:ini", "c/closed"} {
		at := defogAt(t, m, id)
		if n, cmd := send(t, at, runeKey("p")); cmd != nil || n.View() != at.View() {
			t.Errorf("%s: p did something", id)
		}
	}
	f, fcalls := defaultFocus(t)
	f = ideaFocus(t, f)
	if n, cmd := send(t, f, runeKey("p")); cmd != nil || ansi.Strip(n.View()) != ansi.Strip(f.View()) {
		t.Errorf("focus header: p did something")
	}
	if len(*calls)+len(*fcalls) != 0 {
		t.Errorf("wrote %v %v", *calls, *fcalls)
	}
}

// p on an Actionable row pauses its tree twin's bead, with the hint; Ideate's combined
// Initiative row leaves p alone, as the Initiative header does.
func TestActionableRowPauseKey(t *testing.T) {
	for _, c := range []struct {
		name string
		m    func(t *testing.T) (Model, *[]string)
		at   func(t *testing.T, m Model) Model
		want string
	}{
		{"defog", defogActModel, func(t *testing.T, m Model) Model { return defogAt(t, m, "act:ready") }, "pauseBead ready"},
		{"exec", func(t *testing.T) (Model, *[]string) { return execModel(t, execFixture()) }, func(t *testing.T, m Model) Model { return execAt(t, m, "act:ap") }, "pauseBead ap"},
		{"ideate", func(t *testing.T) (Model, *[]string) {
			return drainedModel(t, []bd.Bead{sig("r", "open", 1, "")}, nil)
		}, func(t *testing.T, m Model) Model { return ideaAt(t, m, "act:r") }, "pauseBead r"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, calls := c.m(t)
			m = c.at(t, m)
			if h := hintsOf(m); !strings.Contains(h, "p pause") {
				t.Errorf("hints = %q, want p pause", h)
			}
			n, cmd := send(t, m, runeKey("p"))
			settle(t, n, cmd)
			if !slices.Contains(*calls, c.want) {
				t.Errorf("calls = %v, want %q", *calls, c.want)
			}
		})
	}
	m, calls := drainedModel(t, drainedBeads(), nil)
	m = ideaAt(t, m, "act:d")
	if h := hintsOf(m); strings.Contains(h, "p pause") {
		t.Errorf("combined Initiative row hints p: %q", h)
	}
	n, cmd := send(t, m, runeKey("p"))
	settle(t, n, cmd)
	if len(*calls) != 0 {
		t.Errorf("wrote %v", *calls)
	}
}
