package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// listSurface is one drawer-list surface under the pointer: how to build it, what it has
// selected, and where its body starts on screen.
type listSurface struct {
	name  string
	model func(t *testing.T, h int) Model
	sel   func(m Model) string
	top   int // the screen row of body line 0
	// counts reports a continuation line: it selects the row drawn on the line above.
	counts func(text string) bool
}

func typeKeys(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m = press(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func listSurfaces() []listSurface {
	graph := func(filter string) func(t *testing.T, h int) Model {
		return func(t *testing.T, h int) Model {
			m := graphModel(t, 100, 30)
			if filter != "" {
				m = typeKeys(t, press(t, m, runeKey("/")), filter)
				m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			}
			m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: h})
			return m
		}
	}
	logsAt := func(filter string) func(t *testing.T, h int) Model {
		return func(t *testing.T, h int) Model {
			m := logsModel(t, 100, 30)
			if filter != "" {
				m = typeScan(t, press(t, m, runeKey("/")), filter)
				m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			}
			m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: h})
			return m
		}
	}
	return []listSurface{
		{"graph", graph(""), func(m Model) string { return sel(m) }, 4, nil},
		{"graph filtered", graph("build"), func(m Model) string { return sel(m) }, 4, nil},
		{"focus", func(t *testing.T, h int) Model {
			m, _ := defaultFocus(t)
			m = ideaFocus(t, m)
			m, _ = send(t, m, tea.WindowSizeMsg{Width: 160, Height: h})
			return m
		}, func(m Model) string { return m.focus.sel }, 3, nil},
		{"initiatives", func(t *testing.T, h int) Model {
			m := initsModel(t, initsFixture(), nil)
			m, _ = send(t, m, tea.WindowSizeMsg{Width: 120, Height: h})
			return m
		}, func(m Model) string { return m.tabAt(tabInits).(initsTab).sel }, 4,
			func(s string) bool {
				return strings.Contains(s, "checkpoints ") || strings.Contains(s, "no body yet ·")
			}},
		{"logs", logsAt(""), func(m Model) string { return fmt.Sprint(logs(m).sel) }, 4, nil},
		{"logs scoped", logsAt("chart"), func(m Model) string { return fmt.Sprint(logs(m).sel) }, 4, nil},
	}
}

func screenBody(m Model, top int) []string {
	h := m.h - chromeH
	if m.focus.id != "" {
		h++
	}
	return strings.Split(m.View(), "\n")[top : top+h]
}

// selBodyLine is the body line View paints as selected, from column 1 (past the gutter or ❯).
func selBodyLine(m Model, top int) string {
	for _, l := range screenBody(m, top) {
		if strings.Contains(l, "48;5;25m") {
			return normLine(l)
		}
	}
	return ""
}

// normLine is a drawn line's words, without the selector or a blocker mark.
func normLine(l string) string {
	f := strings.Fields(strings.TrimPrefix(strings.TrimSpace(ansi.Strip(l)), "❯"))
	if len(f) > 0 && f[0] == "!" {
		f = f[1:]
	}
	// a fold click flips its glyph
	return strings.NewReplacer("▾ ", "", "▸ ", "", "▾", "", "▸", "").Replace(strings.Join(f, " "))
}

func click(t *testing.T, m Model, y int) Model {
	t.Helper()
	m, _ = send(t, m.WithMouse(true), tea.MouseMsg{X: 5, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	return m
}

// Geometry parity: for every body line, with the drawer open and shut, a click selects the
// row View drew on that line (a counts line, its row above) or changes nothing.
func TestListClickMatchesDrawnLine(t *testing.T) {
	forceANSI256(t)
	sawFooter := false
	for _, s := range listSurfaces() {
		for _, h := range []int{14, 20, 40} {
			for _, open := range []bool{false, true} {
				for steps := range 12 {
					m := s.model(t, h)
					for range steps {
						m = press(t, m, runeKey("j"))
					}
					if open {
						m = press(t, m, runeKey("d"))
					}
					want := s.sel(m)
					body := screenBody(m, s.top)
					for y, raw := range body {
						text := ansi.Strip(raw)
						if strings.HasPrefix(strings.TrimLeft(text, " "), "!") && y < len(body)/2 {
							sawFooter = true
						}
						after := click(t, m, s.top+y)
						ctx := fmt.Sprintf("%s h=%d open=%v steps=%d line %d %q", s.name, h, open, steps, y, text)
						if s.sel(after) == want {
							continue
						}
						if strings.TrimSpace(text) == "" || strings.HasPrefix(text, "─") || strings.HasPrefix(text, "╭") || strings.HasPrefix(text, "╰") {
							t.Errorf("%s: a blank/rule line selected %q", ctx, s.sel(after))
							continue
						}
						wantText := normLine(text)
						if s.counts != nil && s.counts(text) {
							if y == 0 {
								continue // its row is scrolled off the top
							}
							wantText = normLine(body[y-1])
						}
						if got := selBodyLine(after, s.top); !sameRow(got, wantText) {
							t.Errorf("%s: selected line %q, want the clicked line", ctx, got)
						}
					}
				}
			}
		}
	}
	if !sawFooter {
		t.Log("no case drew a blocker footer on these surfaces")
	}
}

// Every row j/k reaches is clickable, and nothing else is.
func TestListClickReachesExactlyTheStops(t *testing.T) {
	for _, s := range listSurfaces() {
		m := s.model(t, 80)
		var reach []string
		for range 60 {
			m = press(t, m, runeKey("j"))
			if k := s.sel(m); !slices.Contains(reach, k) {
				reach = append(reach, k)
			}
		}
		clicked := map[string]bool{}
		for y := range screenBody(m, s.top) {
			clicked[s.sel(click(t, m, s.top+y))] = true
			clicked[s.sel(click(t, press(t, m, runeKey("j")), s.top+y))] = true
		}
		for _, k := range reach {
			if !clicked[k] {
				t.Errorf("%s: j/k reaches %q but no line selects it", s.name, k)
			}
		}
		for k := range clicked {
			if !slices.Contains(reach, k) {
				t.Errorf("%s: a click selected %q, which j/k never lands on", s.name, k)
			}
		}
	}
}

// A click on an Initiatives counts line selects the row above it.
func TestInitiativesCountsLineSelectsItsRow(t *testing.T) {
	s := listSurfaces()[3]
	m := s.model(t, 40)
	body := screenBody(m, s.top)
	for y, raw := range body {
		if !s.counts(ansi.Strip(raw)) {
			continue
		}
		want := click(t, m, s.top+y-1)
		got := click(t, m, s.top+y)
		if s.sel(got) != s.sel(want) || s.sel(got) == s.sel(m) && y > 2 {
			t.Errorf("counts line %d selected %q, its row %q", y, s.sel(got), s.sel(want))
		}
		return
	}
	t.Fatal("no counts line drawn")
}
