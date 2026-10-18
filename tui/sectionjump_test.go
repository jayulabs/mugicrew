package tui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
)

// emptyTab is a model on tab key k with no beads, so every queue section is a placeholder.
func emptyTab(t *testing.T, k string) Model {
	t.Helper()
	stubCommonDir(t, t.TempDir())
	m := sized(t, 100, 30)
	m, _ = send(t, m, loadedMsg{beads: []bd.Bead{}, live: map[string]bool{}})
	return press(t, m, runeKey(k))
}

// curRow is the key of the row under the cursor on the tab at index tab, with whether
// it is a placeholder and the tab's rendered section-header keys in order.
func curRow(m Model, tab int) (cur string, onPlace bool, heads []string) {
	switch tt := m.tabAt(tab).(type) {
	case ideaTab:
		rows := tt.rows()
		for _, r := range rows {
			if r.sec {
				heads = append(heads, r.key)
			}
		}
		c := rows[tt.selIndex(rows)]
		return c.key, c.place, heads
	case defogTab:
		rows := tt.rows()
		for _, r := range rows {
			if r.kind == dSec || r.noInit() {
				heads = append(heads, r.key)
			}
		}
		c := rows[tt.selIndex(rows)]
		return c.key, c.kind == dPlace, heads
	case execTab:
		rows := tt.rows()
		for _, r := range rows {
			if r.isSection() {
				heads = append(heads, r.key)
			}
		}
		c := rows[tt.selIndex(rows)]
		return c.key, c.kind == ePlace, heads
	case initsTab:
		rows := tt.rows()
		for _, r := range rows {
			if r.b == nil {
				heads = append(heads, r.key)
			}
		}
		c := rows[tt.selIndex(rows)]
		return c.key, false, heads
	}
	panic("tab")
}

func TestQueueTabsCursorSkipsPlaceholders(t *testing.T) {
	for _, tc := range []struct {
		name, k string
		tab     int
	}{{"ideate", "1", tabIdeate}, {"defog", "2", tabDefog}, {"execute", "3", tabExecute}} {
		for _, down := range []string{"j", "k"} {
			m := emptyTab(t, tc.k)
			for i := range 8 {
				m = press(t, m, runeKey(down))
				if cur, place, _ := curRow(m, tc.tab); place {
					t.Fatalf("%s %s #%d: cursor on placeholder %q", tc.name, down, i, cur)
				}
			}
		}
	}
}

// sectionKeys are the key pairs that jump sections: J/K and their ctrl+down/up aliases.
var sectionKeys = []struct {
	name      string
	fwd, back tea.KeyMsg
}{
	{"J/K", runeKey("J"), runeKey("K")},
	{"ctrl+down/up", tea.KeyMsg{Type: tea.KeyCtrlDown}, tea.KeyMsg{Type: tea.KeyCtrlUp}},
}

func TestQueueTabsSectionJump(t *testing.T) {
	for _, tc := range []struct {
		name, k string
		tab     int
	}{{"ideate", "1", tabIdeate}, {"defog", "2", tabDefog}, {"execute", "3", tabExecute}} {
		for _, keys := range sectionKeys {
			m := emptyTab(t, tc.k)
			_, _, heads := curRow(m, tc.tab)
			wantHeads := []string{actKey, liveKey, treeKey}
			first := []string{actKey, liveKey, treeKey, treeKey, treeKey} // Ideate opens on the create row
			back := []string{liveKey, actKey, actKey, actKey}
			if tc.tab == tabIdeate {
				wantHeads = append(wantHeads, "i:", backlogKey)
				first = []string{actKey, liveKey, treeKey, "i:", backlogKey, backlogKey, backlogKey}
				back = []string{"i:", treeKey, liveKey, actKey, actKey, actKey}
			} else if tc.tab == tabDefog {
				wantHeads = slices.Insert(append(wantHeads, "i:"), 2, chartsKey)
				first = []string{liveKey, chartsKey, treeKey, "i:", "i:", "i:", "i:"} // Defog opens on Actionable
				back = []string{treeKey, chartsKey, liveKey, actKey, actKey}
			} else {
				first = []string{liveKey, treeKey, treeKey, treeKey, treeKey} // Execute (not yet re-sectioned) opens on Actionable
			}
			if tc.tab == tabExecute {
				wantHeads = append(wantHeads, "i:")
				wantHeads = slices.Insert(wantHeads, 2, specsKey, cpsKey)
				first = []string{liveKey, specsKey, cpsKey, treeKey, "i:", "i:", "i:", "i:"}
				back = []string{treeKey, cpsKey, specsKey, liveKey}
			}
			if !slices.Equal(heads, wantHeads) {
				t.Fatalf("%s headers %v", tc.name, heads)
			}
			var seen []string
			for range len(heads) + 2 { // past the last header: stops, no wrap
				m = press(t, m, keys.fwd)
				cur, _, _ := curRow(m, tc.tab)
				seen = append(seen, cur)
			}
			if want := first; !slices.Equal(seen, want) {
				t.Errorf("%s %s visits %v, want %v", tc.name, keys.name, seen, want)
			}
			seen = nil
			for range len(back) {
				m = press(t, m, keys.back)
				cur, _, _ := curRow(m, tc.tab)
				seen = append(seen, cur)
			}
			if want := back; !slices.Equal(seen, want) {
				t.Errorf("%s %s back visits %v, want %v", tc.name, keys.name, seen, want)
			}
		}
	}
}

func TestQueueTabsJumpLandsOnFoldedHeader(t *testing.T) {
	m := emptyTab(t, "3")
	m = press(t, m, runeKey(" ")) // fold Actionable, the first row
	m = press(t, m, runeKey("J"))
	if cur, _, _ := curRow(m, tabExecute); cur != liveKey {
		t.Errorf("cursor %q", cur)
	}
	m = press(t, m, runeKey("K"))
	if cur, _, _ := curRow(m, tabExecute); cur != actKey {
		t.Errorf("cursor %q", cur)
	}
}

func TestInitsSectionJump(t *testing.T) {
	for _, keys := range sectionKeys {
		m := initsModel(t, initsFixture(), nil)
		_, _, heads := curRow(m, tabInits)
		if len(heads) < 2 {
			t.Fatalf("headers %v", heads)
		}
		var seen []string
		for range len(heads) + 1 {
			m = press(t, m, keys.fwd)
			cur, _, _ := curRow(m, tabInits)
			seen = append(seen, cur)
		}
		if want := append(slices.Clone(heads[1:]), heads[len(heads)-1], heads[len(heads)-1]); !slices.Equal(seen, want) {
			t.Errorf("%s visits %v, want %v", keys.name, seen, want)
		}
		for i := len(heads) - 2; i >= 0; i-- {
			m = press(t, m, keys.back)
			if cur, _, _ := curRow(m, tabInits); cur != heads[i] {
				t.Errorf("%s at %q, want %q", keys.name, cur, heads[i])
			}
		}
		m = press(t, m, keys.back)
		if cur, _, _ := curRow(m, tabInits); cur != heads[0] {
			t.Errorf("%s past the first header moved to %q", keys.name, cur)
		}
	}
}

func TestSectionJumpHint(t *testing.T) {
	for _, tc := range []struct{ k string }{{"1"}, {"2"}, {"3"}, {"5"}} {
		var m Model
		if tc.k == "5" {
			m = initsModel(t, initsFixture(), nil)
		} else {
			m = emptyTab(t, tc.k)
		}
		m = press(t, m, runeKey("?"))
		if !strings.Contains(m.View(), "section") {
			t.Errorf("tab %s: ? lacks J/K section", tc.k)
		}
	}
}

func TestCursorAvoidsPlaceholderAfterRefreshEmptiesSection(t *testing.T) {
	stubCommonDir(t, t.TempDir())
	m := sized(t, 100, 30)
	m, _ = send(t, m, loadedMsg{beads: []bd.Bead{sig("a", "open", 1, "")}, live: map[string]bool{}})
	m = press(t, m, runeKey("J")) // Actionable
	m = press(t, m, runeKey("j")) // its only row
	m, _ = send(t, m, loadedMsg{beads: []bd.Bead{}, live: map[string]bool{}})
	if cur, place, _ := curRow(m, tabIdeate); place {
		t.Errorf("cursor landed on placeholder %q", cur)
	}
}
