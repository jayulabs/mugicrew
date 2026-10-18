package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// A row's prefix is the gutter, 2·depth columns, then the 2-column caret slot: a foldable row's
// glyph starts after the indent, any other row's text starts two columns later, so a child sits under
// its parent's title.
func TestExecIndentFollowsDepth(t *testing.T) {
	cp := contract.KindCheckpoint
	beads := []bd.Bead{
		dbead("i1", contract.KindInitiative, "open", "", 1),
		dbead("c1", cp, "open", "i1", 1, isContainer),
		dbead("m1", contract.KindBuild, "open", "c1", 1),
		dbead("c0", cp, "open", "", 1, isContainer),
		dbead("ap", contract.KindApproval, "open", "c0", 1),
	}
	tab := execTab{g: graphData{beads: beads}}
	tab.flip = tab.expanded()
	rows := tab.rows()
	lead := func(r eRow) int {
		l, _ := tab.line(r, 10, nil, "")
		l = ansi.Strip(l)[1:] // the gutter
		return len(l) - len(strings.TrimLeft(l, " "))
	}
	for _, r := range rows {
		want := 2*r.depth + 2
		if r.kind == eSec || r.kind == eSum || r.kind == eCP {
			want = 2 * r.depth
		}
		if got := lead(r); got != want {
			t.Errorf("%s (%s) at depth %d starts %d columns in, want %d", r.key, r.title, r.depth, got, want)
		}
	}
	byKey := map[string]eRow{}
	for _, r := range rows {
		byKey[r.key] = r
	}
	// the Checkpoint's glyph follows its 2-column caret; the member's glyph is one step past that
	if c, m := lead(byKey["c1"])+2, lead(byKey["m1"]); m != c+2 {
		t.Errorf("member glyph at %d, Checkpoint glyph at %d: want 2 columns right", m, c)
	}
	want := map[string]int{actKey: 0, "act:ap": 1, treeKey: 0, "i:i1": 1, "c1": 2, "m1": 3, noInitKey: 0, "c0": 1}
	for _, r := range rows {
		if d, ok := want[r.key]; ok && r.depth != d {
			t.Errorf("%s depth %d, want %d", r.key, r.depth, d)
		}
	}
}

func TestExecSectionsAlwaysShow(t *testing.T) {
	v := ansi.Strip(screen(fresh(t, "3", nil, nil)))
	for _, want := range []string{"Actionable (0)", "Live (0)", "Initiatives (0)", "No Initiative (0)"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q:\n%s", want, v)
		}
	}
	// Initiatives opens on its placeholder; No Initiative starts folded, so only its header
	if !strings.Contains(v, "no Initiatives") || strings.Contains(v, "no freestanding beads") {
		t.Errorf("placeholders:\n%s", v)
	}
}
