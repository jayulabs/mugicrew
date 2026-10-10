package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// adversarialModel is an Appendix-A-shaped graph with titles built to overflow:
// very long, CJK wide-glyph, CHECKPOINT/CHART headers with two-digit counts, and
// in_progress rows carrying an age suffix — more rows than any pane holds.
func adversarialModel() Model {
	now := time.Now()
	long := strings.Repeat("a very long title that never ends ", 8)
	wide := strings.Repeat("端末の幅を超える長い題名", 6)
	m := New(nil, nil)
	m.hideClosedRoots = false
	m.beads = []bd.Bead{
		{ID: "cp", Kind: contract.KindCheckpoint, Container: true, Status: "in_progress", Title: "Checkpoint: " + long, Started: now.Add(-50 * 24 * time.Hour)},
		{ID: "ch", Kind: contract.KindChart, Container: true, Status: "open", Title: "Chart: " + wide},
	}
	kinds := []contract.Kind{contract.KindPrototype, contract.KindResearch, contract.Kind("unowned"), contract.KindGrilling}
	for i := 0; i < 24; i++ {
		b := bd.Bead{ID: fmt.Sprintf("l%02d", i), Kind: kinds[i%len(kinds)], Parent: []string{"cp", "ch"}[i%2],
			Status: []string{"open", "in_progress", "closed"}[i%3], Title: []string{long, wide}[i%2],
			Started: now.Add(-time.Duration(i) * 24 * time.Hour), Created: now.Add(-400 * 24 * time.Hour)}
		m.beads = append(m.beads, b)
	}
	// FR2 reviewer repro: a 3-digit child count and 4-digit ages (header, leaf,
	// Inbox) must widen the column, not lose their tails ("100/100  15").
	m.beads = append(m.beads, bd.Bead{ID: "big", Kind: contract.KindCheckpoint, Container: true, Status: "in_progress", Title: long, Started: now.Add(-1500 * 24 * time.Hour)})
	for i := 0; i < 100; i++ {
		m.beads = append(m.beads, bd.Bead{ID: fmt.Sprintf("b%03d", i), Kind: contract.Kind("unowned"), Parent: "big", Status: "closed", Title: long})
	}
	m.beads = append(m.beads,
		bd.Bead{ID: "old", Kind: contract.Kind("unowned"), Parent: "cp", Status: "in_progress", Title: wide, Started: now.Add(-1200 * 24 * time.Hour)},
		bd.Bead{ID: "sig", Kind: contract.KindSignal, Status: "open", Title: wide, Created: now.Add(-2000 * 24 * time.Hour)})
	for i := 0; i < 12; i++ { // Inbox: open HITL, some parentless (untriaged)
		m.beads = append(m.beads, bd.Bead{ID: fmt.Sprintf("s%02d", i), Kind: []contract.Kind{contract.KindSignal, contract.KindGrilling, contract.KindPrototype}[i%3],
			Status: "open", Title: []string{long, wide}[i%2], Created: now.Add(-999 * 24 * time.Hour)})
	}
	m.rebuildGroups()
	m.collapsed["big"] = true // its header keeps the 100/100 count; folding spares 100 rows per frame
	return m
}

var frameWidths = []int{40, 83, 84, 100, 200}

// FR1/FR2: every master row fits its column on one line, and only the title is
// cut — tag, glyphs, child count and age always show whole.
func TestMasterRowsFitColumn(t *testing.T) {
	for _, w := range frameWidths {
		m := adversarialModel()
		m.w, m.h = w, 30
		mw := m.masterColW()
		check := func(where, l string, want ...string) {
			if strings.Contains(l, "\n") || lipgloss.Width(l) > mw {
				t.Errorf("w=%d %s: row width %d > col %d or multi-line: %q", w, where, lipgloss.Width(l), mw, l)
			}
			for _, s := range want {
				if !strings.Contains(l, s) {
					t.Errorf("w=%d %s: row lost %q: %q", w, where, s, l)
				}
			}
		}
		for i, r := range navRows(m.groups, m.collapsed) {
			m.selGraph = r.sel         // also exercise the ❯ marker
			lines, _ := m.graphLines() // the rows exactly as graphView emits them
			l := lines[i]
			if r.beadID == "" {
				check("untriaged", l)
				continue
			}
			b := m.beadByID(r.beadID)
			want := []string{strings.ToUpper(string(b.Kind))}
			if b.Status == "in_progress" {
				want = append(want, strings.TrimSpace(ageSuffix(b)))
			}
			if r.header {
				c, n := m.childCounts(b.ID)
				want = append(want, fmt.Sprintf("%d/%d", c, n))
			}
			check("graph "+b.ID, l, want...)
		}
		inbox := inboxList(m.beads, m.sessions)
		for i := range inbox {
			b := &inbox[i]
			m.selInbox = b.ID
			check("inbox "+b.ID, fitRow(m.inboxRow(b, mw), mw), strings.ToUpper(string(b.Kind)), HumanAge(b.Created))
		}
	}
}

// FR3: stepping every selection down then up on every bead tab, the frame is exactly
// m.h lines, none wider than m.w — so the renderer's line diff never desyncs.
func TestViewIsTerminalExact(t *testing.T) {
	for _, w := range frameWidths {
		for _, h := range []int{12, 24} {
			for _, focus := range []bool{false, true} {
				var tm tea.Model = adversarialModel()
				tm, _ = tm.Update(tea.WindowSizeMsg{Width: w, Height: h})
				m := tm.(Model)
				m.focusDetail = focus
				tm = m
				for _, tb := range []string{"graph", "inbox", "search"} {
					for _, key := range []string{"j", "k"} {
						for i := 0; i < 45; i++ {
							// The body fits its pane before View's frame clamp, so no
							// pane is silently cropped (a wrapped detail title once was).
							bm := tm.(Model)
							body := bm.graphView(bm.paneHeight())
							switch tb {
							case "inbox":
								body = bm.inboxView(bm.paneHeight())
							case "search":
								body = bm.searchView(bm.paneHeight())
							}
							if n := lineCount(body); n > bm.paneHeight() {
								t.Fatalf("%dx%d %s focus=%v %s step %d: body %d lines > pane %d", w, h, tb, focus, key, i, n, bm.paneHeight())
							}
							v := bm.View()
							lines := strings.Split(v, "\n")
							if len(lines) != h {
								t.Fatalf("%dx%d %s focus=%v %s step %d: %d lines, want %d", w, h, tb, focus, key, i, len(lines), h)
							}
							for _, l := range lines {
								if lipgloss.Width(l) > w {
									t.Fatalf("%dx%d %s focus=%v %s step %d: line width %d: %q", w, h, tb, focus, key, i, lipgloss.Width(l), l)
								}
							}
							if focus { // detail focus: j/k scroll the detail, so step the list directly
								mm := tm.(Model)
								mm.move(map[string]int{"j": 1, "k": -1}[key])
								tm = mm
							} else {
								tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
							}
						}
					}
					tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRight})
				}
			}
		}
	}
}

// FR3 edge: the bd-read-failed and below-floor notices are frames too — exactly
// m.h lines, none wider than m.w — and a long error never pushes out the hint.
func TestNoticesAreTerminalExact(t *testing.T) {
	longErr := fmt.Errorf("dolt: %s", strings.Repeat("connection refused ", 6))
	for _, c := range []struct {
		w, h int
		err  error
		want []string
	}{
		{36, 10, nil, []string{"terminal too small", "need 40x12", "have 36x10"}},
		{20, 3, nil, []string{"terminal too small"}},
		{20, 3, longErr, []string{"bd read failed", "press r"}},
		{100, 24, longErr, []string{"bd read failed", "press r to retry, q to quit"}},
	} {
		m := New(nil, nil)
		m.w, m.h, m.loadErr = c.w, c.h, c.err
		v := m.View()
		lines := strings.Split(v, "\n")
		if len(lines) != c.h {
			t.Errorf("%dx%d err=%v: %d lines, want %d:\n%s", c.w, c.h, c.err != nil, len(lines), c.h, v)
		}
		for _, l := range lines {
			if lipgloss.Width(l) > c.w {
				t.Errorf("%dx%d err=%v: line width %d: %q", c.w, c.h, c.err != nil, lipgloss.Width(l), l)
			}
		}
		for _, s := range c.want {
			if !strings.Contains(v, s) {
				t.Errorf("%dx%d err=%v: lost %q:\n%s", c.w, c.h, c.err != nil, s, v)
			}
		}
	}
}

// The list pane is 40% of the width, capped at masterCap, always leaving ≥20
// cols for the detail (ADR 0072 §4–6, Spec FR 8).
func TestMasterWidthCap(t *testing.T) {
	m := New(nil, nil)
	for _, c := range []struct{ w, want int }{{84, 33}, {120, 48}, {150, 60}, {300, 60}} {
		m.w = c.w
		if got := m.masterWidth(); got != c.want || c.w-got < 20 {
			t.Errorf("w=%d: masterWidth = %d, want %d with ≥20 for detail", c.w, got, c.want)
		}
	}
}
