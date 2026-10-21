package tui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// sectionGraph has an Initiative holding a nested Chart, two plain Charts and a
// parentless bead, so the top-level headers are ini, solo, other and Unparented.
func sectionGraph(t *testing.T) Model {
	t.Helper()
	stubCommonDir(t, t.TempDir())
	beads := []bd.Bead{
		{ID: "ini", Kind: contract.KindInitiative, Container: true, Status: "open", Title: "Ship it", Created: epoch},
		{ID: "nch", Kind: contract.KindChart, Container: true, Status: "open", Title: "Nested chart", Parent: "ini", Created: epoch},
		{ID: "nb", Kind: contract.KindBuild, Status: "open", Title: "Nested build", Parent: "nch", Created: epoch},
		{ID: "solo", Kind: contract.KindChart, Container: true, Status: "open", Title: "Solo chart", Created: epoch},
		{ID: "sb", Kind: contract.KindBuild, Status: "open", Title: "Solo build", Parent: "solo", Created: epoch},
		{ID: "other", Kind: contract.KindChart, Container: true, Status: "open", Title: "Other chart", Created: epoch},
		{ID: "ob", Kind: contract.KindBuild, Status: "open", Title: "Other build", Parent: "other", Created: epoch},
		{ID: "loose", Kind: contract.KindSignal, Status: "open", Title: "Loose signal", Created: epoch},
	}
	m := sized(t, 120, 40)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: map[string]session.Info{}})
	m, _ = send(t, m, runeKey("4"))
	return m
}

func topHeaders(m Model) (out []string) {
	for _, r := range gr(m).rows() {
		if r.header && r.depth == 0 {
			out = append(out, r.sel)
		}
	}
	return
}

func TestGraphSectionJumpLandsOnTopLevelHeadersOnly(t *testing.T) {
	m := sectionGraph(t)
	heads := topHeaders(m)
	if len(heads) != 4 {
		t.Fatalf("top-level headers = %v", heads)
	}
	m = selectBead(t, m, heads[0])
	for _, want := range heads[1:] {
		if m = press(t, m, runeKey("J")); sel(m) != want {
			t.Fatalf("J = %q, want %q", sel(m), want)
		}
	}
	if m = press(t, m, runeKey("J")); sel(m) != heads[3] {
		t.Errorf("J past the last header moved to %q", sel(m))
	}
	for i := 2; i >= 0; i-- {
		if m = press(t, m, runeKey("K")); sel(m) != heads[i] {
			t.Fatalf("K = %q, want %q", sel(m), heads[i])
		}
	}
	if m = press(t, m, runeKey("K")); sel(m) != heads[0] {
		t.Errorf("K before the first header moved to %q", sel(m))
	}
}

func TestGraphSectionJumpIsRelativeToCursor(t *testing.T) {
	m := selectBead(t, sectionGraph(t), "nb") // inside the Initiative's nested Chart
	heads := topHeaders(m)
	ini := slices.Index(heads, "ini")
	if m = press(t, m, runeKey("J")); sel(m) != heads[ini+1] {
		t.Errorf("J from nested build = %q, want %q", sel(m), heads[ini+1])
	}
	m = selectBead(t, m, "nb")
	if m = press(t, m, runeKey("K")); sel(m) != "ini" {
		t.Errorf("K from nested build = %q, want ini", sel(m))
	}
}

func TestGraphSectionCtrlArrowsMatchJK(t *testing.T) {
	m := sectionGraph(t)
	heads := topHeaders(m)
	m = selectBead(t, m, heads[0])
	if m = press(t, m, tea.KeyMsg{Type: tea.KeyCtrlDown}); sel(m) != heads[1] {
		t.Errorf("ctrl+down = %q, want %q", sel(m), heads[1])
	}
	if m = press(t, m, tea.KeyMsg{Type: tea.KeyCtrlUp}); sel(m) != heads[0] {
		t.Errorf("ctrl+up = %q, want %q", sel(m), heads[0])
	}
}

func TestGraphSectionJumpSkipsFoldedChildren(t *testing.T) {
	m := sectionGraph(t)
	heads := topHeaders(m)
	m = selectBead(t, m, heads[1])
	m = press(t, m, runeKey(" ")) // fold it
	if m = press(t, m, runeKey("J")); sel(m) != heads[2] {
		t.Errorf("J over a folded group = %q, want %q", sel(m), heads[2])
	}
}

func TestGraphSectionJumpRespectsFilter(t *testing.T) {
	g := gr(sectionGraph(t))
	g.q.words = "Other"
	g = g.rebuild()
	g.sel = g.rows()[0].sel
	n, _ := g.update(runeKey("J"))
	got := n.(graphTab)
	if indexRow(got.rows(), got.sel) < 0 {
		t.Errorf("J landed on %q, not a visible row", got.sel)
	}
}

func TestGraphSectionJumpCancelsCursorMemoryAndDetailScroll(t *testing.T) {
	m := sectionGraph(t)
	heads := topHeaders(m)
	g := gr(m)
	g.sel, g.want, g.off = heads[0], "gone", 7
	n, _ := g.update(runeKey("J"))
	if got := n.(graphTab); got.sel != heads[1] || got.want != "" || got.off != 0 {
		t.Errorf("after J sel %q want %q off %d", got.sel, got.want, got.off)
	}
}

func TestGraphSectionJumpOnEmptyGraphDoesNothing(t *testing.T) {
	m := sized(t, 120, 40)
	m, _ = send(t, m, runeKey("4"))
	for _, k := range []string{"J", "K"} {
		if m = press(t, m, runeKey(k)); sel(m) != "" {
			t.Errorf("%s on empty Graph selected %q", k, sel(m))
		}
	}
}

func TestGraphRefListsSectionJumpOffTheBar(t *testing.T) {
	m := sectionGraph(t)
	m = selectBead(t, m, topHeaders(m)[0])
	left, _ := m.refSections()
	var nav []string
	for _, s := range left {
		if s.head == "navigation" {
			for _, b := range s.keys {
				nav = append(nav, b.Help().Key+" "+b.Help().Desc)
			}
		}
	}
	if !strings.Contains(strings.Join(nav, ","), "J/K section") {
		t.Errorf("navigation = %v", nav)
	}
	l := lines(m.View())
	if bar := ansi.Strip(l[len(l)-1]); strings.Contains(bar, "J/K") {
		t.Errorf("bar shows J/K: %q", bar)
	}
}
