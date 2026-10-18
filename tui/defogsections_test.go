package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

func defogKeys(m Model) (out []string) {
	for _, r := range m.tabAt(tabDefog).(defogTab).rows() {
		out = append(out, r.key)
	}
	return
}

// Every section is present when empty; the folded No Initiative shows only its header,
// the unfolded ones a dim placeholder the cursor never rests on.
func TestDefogSectionsEmpty(t *testing.T) {
	m := fresh(t, "2", nil, nil)
	if got := strings.Join(defogKeys(m), ","); got != "s:act,s:act/none,s:live,"+liveNone+",s:charts,"+chartsNone+",s:tree,s:tree/none,i:" {
		t.Errorf("rows = %q", got)
	}
	v := screen(m)
	for _, want := range []string{"Actionable (0)", "Live (0)", "Charts (0)", "no open Charts", "Initiatives (0)", "no Initiatives", "No Initiative (0)"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "no freestanding beads") || strings.Contains(v, "Backlog") {
		t.Errorf("folded No Initiative shows a placeholder, or Defog grew a Backlog:\n%s", v)
	}
	m = press(t, defogAt(t, m, "i:"), runeKey(" "))
	if !strings.Contains(screen(m), "no freestanding beads") {
		t.Errorf("unfolded empty No Initiative lacks its placeholder:\n%s", screen(m))
	}
	for range 10 {
		if k := defogSel(m); strings.HasSuffix(k, "/none") || k == liveNone {
			t.Fatalf("cursor rested on placeholder %q", k)
		}
		m = press(t, m, runeKey("j"))
	}
}

func looseBeads() []bd.Bead {
	b, _ := defogFixture()
	return append(b, dbead("lc", contract.KindChart, "open", "", 1), dbead("ll", contract.KindGrilling, "open", "lc", 1))
}

// J walks the four headers in order, ending on No Initiative; a saved i: fold applies.
func TestDefogSectionJumpAndCounts(t *testing.T) {
	_, sess := defogFixture()
	m := fresh(t, "2", looseBeads(), sess)
	var seen []string
	for range 5 {
		m = press(t, m, runeKey("J"))
		seen = append(seen, defogSel(m))
	}
	if got := strings.Join(seen, ","); got != "s:live,s:charts,s:tree,i:,i:" {
		t.Errorf("J walk = %q", got)
	}
	v := screen(m)
	if !strings.Contains(v, "Initiatives (1)") || !strings.Contains(v, "No Initiative (1) · ") {
		t.Errorf("headers:\n%s", v)
	}
	m = press(t, m, runeKey(" "))
	if v := screen(m); !strings.Contains(v, "▾ No Initiative (1)") || !strings.Contains(v, "t lc") {
		t.Errorf("unfolded No Initiative:\n%s", v)
	}
}

// Rows indent 2·depth columns after the gutter, then take the 2-column caret slot (blank on
// a row that cannot fold), so a child's first glyph sits one step right of its parent's title.
func TestDefogIndentFollowsDepth(t *testing.T) {
	_, sess := defogFixture()
	m := fresh(t, "2", looseBeads(), sess)
	m = press(t, defogAt(t, m, "i:"), runeKey(" "))
	m = press(t, defogAt(t, m, "i:ini"), runeKey(" "))
	m = press(t, defogAt(t, m, "c"), runeKey(" "))
	m = press(t, defogAt(t, m, chartsKey), runeKey(" ")) // its rows twin the tree's Charts
	m = defogAt(t, m, liveKey)                           // the cursor off the rows under test
	lines := strings.Split(ansi.Strip(m.View()), "\n")[chromeH-2:]
	line := func(s string) string {
		i := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, s) })
		if i < 0 {
			t.Fatalf("no line %q:\n%s", s, strings.Join(lines, "\n"))
		}
		return lines[i][min(1, len(lines[i])):] // past the gutter
	}
	col := func(s string) int { l := line(s); return len(l) - len(strings.TrimLeft(l, " ")) }
	for s, want := range map[string]int{"Actionable (": 0, "Live (": 0, "Charts (": 0, "Initiatives (": 0, "No Initiative (": 0,
		"t ready": 4, "INITIATIVE": 2, "t lc · lc": 2, "P1 · t c · c": 4, "t blk": 8, "▸ 1 closed": 6} {
		if got := col(s); got != want {
			t.Errorf("%q starts at column %d, want %d", s, got, want)
		}
	}
	if fold := strings.Index(line("INITIATIVE"), "▾"); col("P1 · t c · c") != fold+2 {
		t.Errorf("Chart at column %d, want under the Initiative's first glyph at %d", col("P1 · t c · c"), fold+2)
	}
	// The Chart's leaf glyph lands 2 columns right of the Chart's own glyph (4 past its caret), and its foldable
	// sibling's caret sits in the leaf's blank slot.
	if got, want := col("t blk"), col("P1 · t c · c")+4; got != want {
		t.Errorf("leaf at column %d, want %d", got, want)
	}
	if got := strings.Index(line("▸ 1 closed"), "▸"); got != col("t blk")-2 {
		t.Errorf("fold caret at column %d, want %d", got, col("t blk")-2)
	}
}
