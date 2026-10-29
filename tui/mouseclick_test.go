package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/session"
)

// queueSel is the active queue tab's stored selection key.
func queueSel(m Model) string {
	switch m.tab {
	case tabIdeate:
		return ideaSel(m)
	case tabDefog:
		return defogSel(m)
	}
	return execSel(m)
}

// clickAt is a left press on body line y (the body starts at screen row 4).
func clickAt(t *testing.T, m Model, y int) Model {
	t.Helper()
	m, _ = send(t, m.WithMouse(true), tea.MouseMsg{X: 5, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	return m
}

// queueBody is View's body lines, ANSI kept.
func queueBody(m Model) []string {
	return strings.Split(m.View(), "\n")[4 : 4+m.h-chromeH]
}

// pastGutter drops a line's first cell: the blocker gutter, or the selector that replaced it.
func pastGutter(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}
	return string(r[1:])
}

// selLine is the body line View paints as selected, from column 1 (past the gutter or ❯), or "".
func selLine(m Model) string {
	for _, l := range queueBody(m) {
		if strings.Contains(l, "48;5;25m") {
			return pastGutter(ansi.Strip(l))
		}
	}
	return ""
}

func queueFixtures(t *testing.T) map[string]Model {
	t.Helper()
	ide := ideaModel(t, 100, 30, sectionBeads(), map[string]session.Info{"mc-long-22": {State: session.Alive}})
	_, dsess := defogFixture()
	def := press(t, treesOpen(fresh(t, "2", defogBeads(), dsess)), runeKey("2"))
	exe, _ := execModel(t, execFixture())
	return map[string]Model{"ideate": ide, "defog": def, "execute": exe}
}

func defogBeads() []bd.Bead { b, _ := defogFixture(); return b }

// Geometry parity: for every body line, a click selects the row View drew there, and a line
// View drew as a gap, padding, rule, footer, drawer body or header changes nothing; and the
// lines a click selects are exactly the stops j/k reach.
func TestQueueTabClickMatchesDrawnLine(t *testing.T) {
	forceANSI256(t)
	sawFooter := false
	for name, base := range queueFixtures(t) {
		for _, h := range []int{16, 24, 40} {
			for _, open := range []bool{false, true} {
				for steps := range 14 {
					m := base
					m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: h})
					for range steps {
						m = press(t, m, runeKey("j"))
					}
					if open {
						m = press(t, m, runeKey("d"))
						if !drawerIsOpen(m) {
							continue
						}
					}
					want := queueSel(m)
					body := queueBody(m)
					for y, raw := range body {
						text := ansi.Strip(raw)
						if strings.HasPrefix(strings.TrimLeft(text, " "), "!") && y >= len(body)/2 {
							sawFooter = true
						}
						after := clickAt(t, m, 4+y)
						ctx := fmt.Sprintf("%s h=%d open=%v steps=%d line %d %q", name, h, open, steps, y, text)
						if queueSel(after) == want {
							continue // inert, or the row already selected
						}
						if got := selLine(after); !sameRow(got, pastGutter(text)) {
							t.Errorf("%s: selected line %q, want the clicked line", ctx, got)
						}
						if strings.TrimSpace(text) == "" || strings.HasPrefix(text, "─") || strings.HasPrefix(text, "╭") || strings.HasPrefix(text, "╰") {
							t.Errorf("%s: a gap/rule line selected %q", ctx, queueSel(after))
						}
					}
				}
			}
		}
	}
	if !sawFooter {
		t.Error("no case drew a blocker footer; the fixtures no longer cover it")
	}
}

func drawerIsOpen(m Model) bool {
	switch t := m.tabAt(m.tab).(type) {
	case ideaTab:
		return t.open
	case defogTab:
		return t.open
	case execTab:
		return t.open
	}
	return false
}

// Every stop j/k lands on is clickable, and nothing else is.
func TestQueueTabClickReachesExactlyTheStops(t *testing.T) {
	for name, base := range queueFixtures(t) {
		m, _ := send(t, base, tea.WindowSizeMsg{Width: 100, Height: 70})
		var reach []string
		for range 60 {
			m = press(t, m, runeKey("j"))
			if k := queueSel(m); !slices.Contains(reach, k) {
				reach = append(reach, k)
			}
		}
		clicked := map[string]bool{}
		for y := range m.h - chromeH {
			for _, start := range []Model{m, press(t, m, runeKey("j"))} {
				clicked[queueSel(clickAt(t, start, 4+y))] = true
			}
		}
		for _, k := range reach {
			if !clicked[k] {
				t.Errorf("%s: j/k reaches %q but no line selects it", name, k)
			}
		}
		for k := range clicked {
			if !slices.Contains(reach, k) {
				t.Errorf("%s: a click selected %q, which j/k never lands on", name, k)
			}
		}
	}
}

// The header, status line and hints bar are inert, and an open drawer's body takes no click.
func TestQueueTabClickInertChromeAndDrawer(t *testing.T) {
	for name, base := range queueFixtures(t) {
		m := press(t, base, runeKey("j"))
		for _, y := range []int{0, m.h - 2, m.h - 1} { // rows 1–3 are the tab strip, which answers clicks (mousetabs_test.go)
			if got := clickAt(t, m, y); queueSel(got) != queueSel(m) {
				t.Errorf("%s: screen row %d selected %q", name, y, queueSel(got))
			}
		}
		m = press(t, m, runeKey("d"))
		if !drawerIsOpen(m) {
			continue
		}
		for y := m.h - chromeH - 1; y > (m.h-chromeH)/2; y-- {
			if got := clickAt(t, m, 4+y); queueSel(got) != queueSel(m) {
				t.Errorf("%s: drawer body line %d selected %q", name, y, queueSel(got))
			}
		}
	}
}

// hit names a blocker footer row as a link to its blocker, the open drawer's body as the
// drawer, and its rule and the padding above it as nothing.
func TestQueueTabHitKinds(t *testing.T) {
	m, _ := execModel(t, execFixture())
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m = press(t, m, runeKey("d"))
	bodyH := m.h - chromeH
	link := false
	for range 20 { // walk the stops until one has a blocker scrolled off the window
		p := m.tabAt(m.tab).(pointable)
		for y := range bodyH {
			h := p.hit(m.w, bodyH, y)
			switch {
			case (y == bodyH/2 || y == bodyH-1) && h.kind != hitNone, y > bodyH/2 && y < bodyH-1 && h.kind != hitDrawer:
				t.Fatalf("sel %q line %d = %+v, want the box edges inert and the rows between the drawer", queueSel(m), y, h)
			case h.kind == hitBlockerLink:
				link = true
				if y >= bodyH/2 || !strings.Contains(execFixtureBlockers, " "+h.key+" ") {
					t.Errorf("sel %q line %d = %+v, not a blocker link in the list area", queueSel(m), y, h)
				}
			}
		}
		m = press(t, m, runeKey("j"))
	}
	if !link {
		t.Error("no stop drew a blocker footer link")
	}
}

// execFixtureBlockers are the ids execFixture's beads are blocked by.
const execFixtureBlockers = " tb bl sp pg pg2 cpA "

// sameRow is whether got is the clicked row want, allowing for a fold click: the glyph flips
// and a folded row gains a trailing count.
func sameRow(got, want string) bool {
	n := strings.NewReplacer("▾", "", "▸", "")
	g, w := strings.Join(strings.Fields(n.Replace(got)), " "), strings.Join(strings.Fields(n.Replace(want)), " ")
	return strings.HasPrefix(g, w) || strings.HasPrefix(w, g)
}
