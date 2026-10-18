package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/session"
)

// shaded is the titles (the word after the first matching prefix, sorted) of the rows the view
// paints with the ancestor shade (Graph's ancRow background).
func shaded(t *testing.T, m Model, prefixes ...string) []string {
	t.Helper()
	forceANSI256(t)
	var out []string
	for _, l := range strings.Split(m.View(), "\n") {
		if !strings.Contains(l, "48;5;236") || strings.Contains(l, "transcript") { // the hint bar names t transcript
			continue
		}
		for _, p := range prefixes { // the hint bar is shaded too: it carries no prefix
			if _, rest, ok := strings.Cut(ansi.Strip(l), p); ok {
				out = append(out, strings.Fields(rest)[0])
				break
			}
		}
	}
	slices.Sort(out)
	return out
}

func wantShaded(t *testing.T, got []string, want ...string) {
	t.Helper()
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("shaded = %v, want %v", got, want)
	}
}

func selDefog(m Model, key string) Model {
	d := m.tabs[tabDefog].(defogTab)
	d.sel = key
	m.tabs[tabDefog] = d
	return m
}

func selExec(m Model, key string) Model {
	e := m.tabs[tabExecute].(execTab)
	e.sel = key
	m.tabs[tabExecute] = e
	return m
}

func selIdea(m Model, key string) Model {
	i := m.tabs[tabIdeate].(ideaTab)
	i.sel = key
	m.tabs[tabIdeate] = i
	return m
}

func TestDefogShadesParentChainAndTwinBothWays(t *testing.T) {
	m := press(t, defogModel(t, 100, 40), runeKey("2"))
	// the tree row shades its chain, its group header and its Actionable twin
	wantShaded(t, shaded(t, selDefog(m, "ready"), "t "), "c", "c", "ini", "ready") // c: its tree row and its Charts row
	// the Actionable row shades its tree twin, chain and group header
	wantShaded(t, shaded(t, selDefog(m, "act:ready"), "t "), "c", "c", "ini", "ready")
}

func TestDefogShadesFoldedTwinsGroupHeader(t *testing.T) {
	m := press(t, defogFoldModel(t, t.TempDir()), runeKey("2"))          // the default folds: the tree is folded
	wantShaded(t, shaded(t, selDefog(m, "act:ready"), "t "), "c", "ini") // c: its Charts row
}

func TestDefogHeaderShadesNothing(t *testing.T) {
	m := press(t, defogModel(t, 100, 40), runeKey("2"))
	for _, k := range []string{"i:ini", actKey, "c"} {
		got := shaded(t, selDefog(m, k), "t ")
		if k == "c" { // a Chart is a bead row: its Initiative shades
			wantShaded(t, got, "ini")
			continue
		}
		wantShaded(t, got)
	}
}

func TestExecSideRowNeverShadesItsCheckpoint(t *testing.T) {
	m, _ := execModelFresh(t, execFixture())
	m = press(t, m, runeKey("3"))
	m = treesOpen(m)
	// ta waits on cpA; its Parent chain is empty and its group header is i1 (cpA's)
	got := shaded(t, selExec(m, "side:ta"), "t ")
	if slices.Contains(got, "cpA") {
		t.Errorf("the hinted Checkpoint shaded: %v", got)
	}
	wantShaded(t, got, "i1")
}

func TestExecMemberShadesCheckpointChain(t *testing.T) {
	m, _ := execModelFresh(t, execFixture())
	m = treesOpen(press(t, m, runeKey("3")))
	wantShaded(t, shaded(t, selExec(m, "act:ap"), "t "), "cpA", "cpA", "i1") // the tree row and its Checkpoints twin
}

func TestIdeateShadesCombinedRowAndHeaderBothWays(t *testing.T) {
	d := ini("ini", "open", "Ship v2", 1)
	beads := []bd.Bead{d, sig("a", "open", 1, "ini"), sig("x", "closed", 1, "ini")}
	m := ideaModel(t, 100, 30, beads, map[string]session.Info{})
	// a ready Signal: its tree twin and its Initiative header shade
	wantShaded(t, shaded(t, selIdea(m, "act:a"), "sig ", "INITIATIVE  P1 · Ship "), "a", "v2")
}

func TestIdeateCombinedRowAndItsHeaderShadeEachOther(t *testing.T) {
	d := ini("ini", "open", "Ship v2", 1)
	beads := []bd.Bead{ini("root", "open", "Root", 1), d, sig("x", "closed", 1, "ini"), sig("y", "in_progress", 2, "root")} // ini is drained: a combined row
	m := ideaModel(t, 100, 30, beads, map[string]session.Info{})
	forceANSI256(t)
	rows := func(k string) (out []string) {
		for _, l := range strings.Split(selIdea(m, k).View(), "\n") {
			if s := ansi.Strip(l); strings.Contains(l, "48;5;236") && strings.Contains(s, "Ship v2") || strings.Contains(l, "48;5;236") && strings.Contains(s, "all members") {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return
	}
	if got := rows("act:ini"); len(got) != 1 || !strings.HasPrefix(got[0], "▾ ○·  INITIATIVE") {
		t.Errorf("the combined row shades %q, want its header only (not the cue row)", got)
	}
	if got := rows("i:ini"); len(got) != 1 || !strings.Contains(got[0], "P1 · Ship v2 · ini") {
		t.Errorf("the header shades %q, want the combined row only", got)
	}
	if got := shaded(t, selIdea(m, "i:root"), "INITIATIVE  "); len(got) != 0 {
		t.Errorf("a plain header shaded %v", got)
	}
}

func TestSelRowIsBlueAndDistinctFromShade(t *testing.T) {
	if got := selRow.GetBackground(); got != lipgloss.Color("25") {
		t.Errorf("selRow background = %v, want 25", got)
	}
	if selRow.GetBackground() == ancRow.GetBackground() {
		t.Error("selRow background equals the ancestor shade's")
	}
	if got := selRow.GetForeground(); got != lipgloss.Color("15") {
		t.Errorf("selRow foreground = %v, want 15", got)
	}
	if !selRow.GetBold() {
		t.Error("selRow not bold")
	}
}
