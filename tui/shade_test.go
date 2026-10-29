package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/session"
)

// shaded is the titles (the word after the first matching prefix, sorted) of the rows the view
// paints with the ancestor shade (Graph's ancRow background).
func shaded(t *testing.T, m Model, prefixes ...string) []string {
	t.Helper()
	forceANSI256(t)
	var out []string
	for _, l := range strings.Split(m.View(), "\n") {
		if !strings.Contains(l, "48;5;236") || strings.Contains(l, "transcript") || strings.Contains(ansi.Strip(l), "? keys") { // the hint bar names t transcript
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

// shadeBeads is a Checkpoint cp holding build b (nested member n under b), with an open
// MR Watch, an open and a closed Landing, and an Errand e that only hints cp.
func shadeBeads() func(string) *bd.Bead {
	cp, bld, mr, ln := contract.KindCheckpoint, contract.KindBuild, contract.KindMRWatch, contract.KindLanding
	m := map[string]*bd.Bead{
		"ini":   {ID: "ini", Kind: contract.KindInitiative, Status: "open"},
		"cp":    {ID: "cp", Kind: cp, Status: "open", Parent: "ini"},
		"b":     {ID: "b", Kind: bld, Status: "open", Parent: "cp"},
		"n":     {ID: "n", Kind: bld, Status: "open", Parent: "b"},
		"mr":    {ID: "mr", Kind: mr, Status: "open", Parent: "cp"},
		"ln":    {ID: "ln", Kind: ln, Status: "open", Parent: "cp"},
		"old":   {ID: "old", Kind: ln, Status: "closed", Parent: "cp"},
		"oldmr": {ID: "oldmr", Kind: mr, Status: "closed", Parent: "cp"},
		"blk":   {ID: "blk", Kind: ln, Status: "blocked", Parent: "cp"},
		"e":     {ID: "e", Kind: contract.KindErrand, Status: "open"},
	}
	return func(id string) *bd.Bead {
		if b, ok := m[id]; ok {
			return b
		}
		return &bd.Bead{ID: id}
	}
}

func TestShadeSetLightsOpenMRWatchAndLandingOnChain(t *testing.T) {
	rows := []shadeIn{
		{id: "ini", head: true, grp: "ini"}, {id: "cp"}, {id: "b"}, {id: "n"}, {id: "mr"}, {id: "ln"}, {id: "old"}, {id: "e"},
		{id: "mr", act: true}, {id: "ln", live: true}, {id: "oldmr"}, {id: "blk"},
	}
	shade := func(sel int) []int {
		var out []int
		for i := range shadeSet(shadeBeads(), rows, sel, rows, func(r shadeIn) shadeIn { return r }) {
			out = append(out, i)
		}
		slices.Sort(out)
		return out
	}
	const hdr, cp, b, n, mr, ln, _, e, mrAct, lnLive, _, blk = 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11
	for name, tc := range map[string]struct {
		sel  int
		want []int
	}{
		"member":            {b, []int{hdr, cp, mr, ln, mrAct, lnLive, blk}},
		"nested member":     {n, []int{hdr, cp, b, mr, ln, mrAct, lnLive, blk}},
		"checkpoint":        {cp, []int{hdr, mr, ln, mrAct, lnLive, blk}},
		"mr watch":          {mr, []int{hdr, cp, ln, mrAct, lnLive, blk}},
		"landing":           {ln, []int{hdr, cp, mr, mrAct, lnLive, blk}},
		"errand with hint":  {e, []int{hdr}}, // its group header only
		"initiative header": {hdr, nil},
	} {
		if got := shade(tc.sel); !slices.Equal(got, tc.want) {
			t.Errorf("%s: shaded %v, want %v", name, got, tc.want)
		}
	}
}

// shadedKeys is the keys of the rows that shadeSet shades for the selected key, over the
// rows a tab's builder produced.
func shadedKeys[R any](t *testing.T, bead func(string) *bd.Bead, rows []R, key func(R) string, in func(R) shadeIn, sel string) []string {
	t.Helper()
	at := slices.IndexFunc(rows, func(r R) bool { return key(r) == sel })
	if at < 0 {
		t.Fatalf("no row %s", sel)
	}
	var out []string
	for i := range shadeSet(bead, rows, at, rows, in) {
		out = append(out, key(rows[i]))
	}
	slices.Sort(out)
	return out
}

// Through Execute's row builder: a Checkpoint's open MR Watch row and open Landing rows (tree
// and Live) are shaded from its members and itself, never the closed ones, and an Errand that
// only hints the Checkpoint lights neither.
func TestExecShadeLightsMRWatchAndLanding(t *testing.T) {
	g := graphData{landName: git.LandStrategyGitlab, live: map[string]bool{"ln": true}, beads: []bd.Bead{
		dbead("i1", contract.KindInitiative, "open", "", 1),
		dbead("cp", contract.KindCheckpoint, "open", "i1", 1, isContainer),
		dbead("b", contract.KindBuild, "open", "cp", 1),
		dbead("mr", contract.KindMRWatch, "open", "cp", 2, described("MR: !7")),
		dbead("ln", contract.KindLanding, "in_progress", "cp", 3),
		dbead("mrOld", contract.KindMRWatch, "closed", "cp", 4, described("MR: !3")),
		dbead("lnOld", contract.KindLanding, "closed", "cp", 5),
		dbead("lnBlk", contract.KindLanding, "blocked", "cp", 6),
		dbead("e", contract.KindErrand, "open", "", 1, blockedBy("cp")),
	}}
	rows := g.execList(execTab{g: g}.expanded())
	shaded := func(sel string) []string {
		return shadedKeys(t, g.beadByID, rows, func(r eRow) string { return r.key }, eRow.shadeIn, sel)
	}
	mrs, lnLive := mrsPrefix+"mr", livePrefix+"ln"
	for name, tc := range map[string]struct {
		sel  string
		want []string
	}{
		"build":      {"b", []string{"cp", "cps:cp", "i:i1", lnLive, "ln", "lnBlk", "mr", mrs}},
		"checkpoint": {"cp", []string{"i:i1", lnLive, "ln", "lnBlk", "mr", mrs}},
		"mr row":     {mrs, []string{"cp", "cps:cp", "i:i1", lnLive, "ln", "lnBlk", "mr"}},
		"landing":    {"ln", []string{"cp", "cps:cp", "i:i1", lnLive, "lnBlk", "mr", mrs}},
		"errand":     {"side:e", []string{"i:i1"}}, // its group header only: the in → cp hint is not a Parent
	} {
		if got := shaded(tc.sel); !slices.Equal(got, tc.want) {
			t.Errorf("%s: shaded %v, want %v", name, got, tc.want)
		}
	}
}

// Through Defog's row builder: a Chart's open MR Watch row and open Landing are shaded from
// its child, and an Initiative header lights no MR row.
func TestDefogShadeLightsMRWatchAndLanding(t *testing.T) {
	g := graphData{landName: git.LandStrategyGitlab, beads: []bd.Bead{
		dbead("ini", contract.KindInitiative, "open", "", 1),
		dbead("c", contract.KindChart, "open", "ini", 1),
		dbead("kid", contract.KindGrilling, "open", "c", 1),
		dbead("mr", contract.KindMRWatch, "open", "c", 2, described("MR: !7")),
		dbead("ln", contract.KindLanding, "open", "c", 3),
		dbead("mrOld", contract.KindMRWatch, "closed", "c", 4, described("MR: !3")),
	}}
	rows := g.defogList(defogTab{g: g}.expanded())
	shaded := func(sel string) []string {
		return shadedKeys(t, g.beadByID, rows, func(r dRow) string { return r.key }, dRow.shadeIn, sel)
	}
	mrs := mrsPrefix + "mr"
	if got, want := shaded("kid"), []string{"act:kid", "c", "charts:c", "i:ini", "ln", "mr", mrs}; !slices.Equal(got, want) {
		t.Errorf("child: shaded %v, want %v", got, want)
	}
	if got := shaded("i:ini"); slices.Contains(got, mrs) {
		t.Errorf("Initiative header shaded the MR row: %v", got)
	}
}
