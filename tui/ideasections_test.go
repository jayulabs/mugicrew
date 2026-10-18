package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/session"
)

func ideaKeys(m Model) (out []string) {
	for _, r := range m.tabAt(tabIdeate).(ideaTab).rows() {
		out = append(out, r.key)
	}
	return
}

// Every section is present when empty; the folded ones show only their header, the
// unfolded ones a dim placeholder that the cursor never rests on.
func TestIdeateSectionsEmpty(t *testing.T) {
	m := ideaModelFresh(t, 100, 30, nil, nil)
	if got := strings.Join(ideaKeys(m), ","); got != ",s:act,s:act/none,s:live,"+liveNone+",s:tree,s:tree/none,i:,"+backlogKey {
		t.Errorf("rows = %q", got)
	}
	v := ansi.Strip(m.View())
	for _, want := range []string{"Actionable (0)", "Live (0)", "Initiatives (0)", "no Initiatives", "No Initiative (0)", "Backlog (0)"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "no freestanding beads") || strings.Contains(v, "nothing in the Backlog") {
		t.Errorf("folded empty sections show a placeholder:\n%s", v)
	}
	m = press(t, ideaAt(t, m, "i:"), runeKey(" "))
	m = press(t, ideaAt(t, press(t, m, runeKey("K")), backlogKey), runeKey(" "))
	if v := ansi.Strip(m.View()); !strings.Contains(v, "no freestanding beads") || !strings.Contains(v, "nothing in the Backlog") {
		t.Errorf("unfolded empty sections lack their placeholder:\n%s", v)
	}
	seen := map[string]bool{}
	for range 14 {
		seen[ideaSel(m)] = true
		m = press(t, m, runeKey("j"))
	}
	for k := range seen {
		if strings.HasSuffix(k, "/none") || k == liveNone {
			t.Errorf("cursor rested on placeholder %q", k)
		}
	}
}

func sectionBeads() []bd.Bead {
	return append(mixed(), sig("bk", "open", 4, ""))
}

// J walks the five section headers in order and stops at the ends.
func TestIdeateSectionJumpReachesNoInitiativeAndBacklog(t *testing.T) {
	m := ideaModel(t, 100, 30, sectionBeads(), map[string]session.Info{"mc-long-22": {State: session.Alive}})
	var seen []string
	for range 7 {
		m = press(t, m, runeKey("J"))
		seen = append(seen, ideaSel(m))
	}
	if got := strings.Join(seen, ","); got != "s:act,s:live,s:tree,i:,"+backlogKey+","+backlogKey+","+backlogKey {
		t.Errorf("J walk = %q", got)
	}
	if m = press(t, m, runeKey("K")); ideaSel(m) != "i:" {
		t.Errorf("K from Backlog = %q", ideaSel(m))
	}
}

// Header texts, the folded trailer and a saved fold on the reused keys.
func TestIdeateSectionHeaders(t *testing.T) {
	m := ideaModelFresh(t, 100, 30, sectionBeads(), nil)
	v := ansi.Strip(m.View())
	for _, want := range []string{"Initiatives (1)", "No Initiative (1) · 1 open", "Backlog (1) · 1 open"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	m = ideaAt(t, m, "i:")
	m = press(t, m, runeKey(" "))
	if v := ansi.Strip(m.View()); !strings.Contains(v, "▾ No Initiative (1)") || !strings.Contains(v, "sig loose") {
		t.Errorf("unfolded No Initiative:\n%s", v)
	}
}

// Rows indent 2·depth columns after the gutter, and a child's first glyph sits under its
// header's caret plus its own blank slot.
func TestIdeateIndentFollowsDepth(t *testing.T) {
	m := ideaModel(t, 100, 40, sectionBeads(), map[string]session.Info{"mc-long-22": {State: session.Alive, Waiting: session.WaitTurn}})
	m = press(t, ideaAt(t, m, backlogKey), runeKey(" "))
	m = ideaAt(t, m, "") // the cursor off the rows under test
	lines := strings.Split(ansi.Strip(m.View()), "\n")[chromeH-2:]
	col := func(s string) int {
		for _, l := range lines {
			if l = l[min(1, len(l)):]; strings.Contains(l, s) { // past the gutter
				return len(l) - len(strings.TrimLeft(l, " "))
			}
		}
		t.Fatalf("no line %q", s)
		return 0
	}
	for s, want := range map[string]int{"Actionable": 0, "Live (": 0, "Initiatives (": 0, "No Initiative (": 0, "Backlog (": 0,
		"nothing is running": 4, "INITIATIVE": 2, "sig loose": 4, "sig bk": 4} {
		if got := col(s); got != want {
			t.Errorf("%q starts at column %d, want %d", s, got, want)
		}
	}
	// the Signal under the Initiative: the nested twin comes after the Initiative header
	i := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "INITIATIVE") })
	member := lines[i+1][1:]
	if got := len(member) - len(strings.TrimLeft(member, " ")); got != 6 {
		t.Errorf("an Initiative's member starts at column %d, want 6: %q", got, member)
	}
	if fold := strings.Index(lines[i][1:], "▾"); fold+2 != 4 {
		t.Errorf("the Initiative's fold glyph is at column %d, its members' first glyph at 4 must sit two on", fold)
	}
}
