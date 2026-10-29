package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/git"
)

// histManyCommits is n plain commits, newest first, so the list windows.
func histManyCommits(n int) []git.MainlineCommit {
	var cs []git.MainlineCommit
	for i := range n {
		cs = append(cs, git.MainlineCommit{SHA: fmt.Sprintf("%07dbbbb", i+1), Subject: fmt.Sprintf("commit %d", i+1)})
	}
	return cs
}

// History parity: every body line selects the commit View drew there, or is inert; line 0
// and an error or empty line never select.
func TestHistoryClickMatchesDrawnLine(t *testing.T) {
	for _, n := range []int{8, 60} {
		for _, h := range []int{16, 24} {
			m := sized(t, 100, h)
			m, _ = send(t, m, histLoad(histManyCommits(n), histBeads()))
			m, _ = send(t, m, runeKey("6"))
			for steps := range n {
				body := strings.Split(m.View(), "\n")[4 : 4+m.h-chromeH]
				want := histSel(m)
				for y, raw := range body {
					after := clickAt(t, m, 4+y)
					got := histSel(after)
					text := ansi.Strip(raw)
					if y == 0 {
						if got != want {
							t.Errorf("n=%d h=%d steps=%d: header line selected %q", n, h, steps, got)
						}
						continue
					}
					if got != want && !strings.Contains(text, got[:7]) {
						t.Errorf("n=%d h=%d steps=%d line %d %q selected %q", n, h, steps, y, text, got)
					}
					if got == want && strings.Contains(text, "commit") && !strings.Contains(text, want[:7]) {
						t.Errorf("n=%d h=%d steps=%d line %d %q drew a commit but selected nothing", n, h, steps, y, text)
					}
				}
				m = press(t, m, runeKey("j"))
			}
		}
	}
}

// An error line and an empty list are inert.
func TestHistoryClickErrorAndEmptyInert(t *testing.T) {
	m := sized(t, 100, 24)
	m, _ = send(t, m, histLoad(nil, histBeads()))
	m, _ = send(t, m, runeKey("6"))
	for y := range m.h - chromeH {
		if got := histSel(clickAt(t, m, 4+y)); got != "" {
			t.Errorf("empty list line %d selected %q", y, got)
		}
	}
}

// Settings parity, scrolled and unscrolled: a clicked line's text belongs to the row it
// selects, a marker or inert line leaves the row alone, and every row j/k reaches is
// clickable somewhere along the scroll.
func TestSettingsClickMatchesDrawnLine(t *testing.T) {
	for _, h := range []int{16, 24, 40} {
		m, _ := settingsModel(t)
		m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: h})
		last := firstKindRow + len(kindKeys()) - 1
		clickable := map[int]bool{}
		for step := 0; step <= last; step++ {
			body := strings.Split(m.View(), "\n")[4 : 4+m.h-chromeH]
			for y, raw := range body {
				text := strings.TrimSpace(ansi.Strip(raw))
				after := clickAt(t, m, 4+y)
				if after.setRow == m.setRow {
					continue
				}
				clickable[after.setRow] = true
				lines, _, rowOf := after.settingsLines()
				found := false
				for i, l := range lines {
					if rowOf[i] == after.setRow && strings.TrimSpace(ansi.Strip(l)) == text {
						found = true
					}
				}
				if !found || strings.Contains(text, " more") {
					t.Errorf("h=%d sel=%d line %d %q selected row %d", h, m.setRow, y, text, after.setRow)
				}
			}
			m = press(t, m, runeKey("j"))
		}
		for r := 0; r <= last; r++ {
			if !clickable[r] {
				t.Errorf("h=%d: j/k reaches row %d but no click selects it", h, r)
			}
		}
	}
}

// A gloss line selects its setting, and the ↑/↓ N more markers do nothing.
func TestSettingsClickGlossAndMarkers(t *testing.T) {
	m, _ := settingsModel(t)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	body := strings.Split(m.View(), "\n")[4 : 4+m.h-chromeH]
	for y, raw := range body {
		if strings.Contains(raw, "how long a HITL session") {
			if got := clickAt(t, m, 4+y).setRow; got != 1 {
				t.Errorf("gloss line selected row %d, want 1", got)
			}
		}
	}
	for range firstKindRow + 3 {
		m = press(t, m, runeKey("j"))
	}
	body = strings.Split(m.View(), "\n")[4 : 4+m.h-chromeH]
	markers := 0
	defer func() {
		if markers == 0 {
			t.Error("no ↑/↓ marker drawn; the fixture no longer scrolls")
		}
	}()
	for y, raw := range body {
		if strings.Contains(ansi.Strip(raw), " more") {
			markers++
			if got := clickAt(t, m, 4+y); got.setRow != m.setRow {
				t.Errorf("marker line %d selected row %d", y, got.setRow)
			}
		}
	}
}
