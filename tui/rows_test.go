package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// A live view always carries the dot slot (· for a bead with no verdict and no
// session); a non-live view drops it with no placeholder.
func TestSBRNDotSlotBelongsToLiveViews(t *testing.T) {
	b := sig("s1", "open", 1, "")
	var g graphData
	if got, want := ansi.Strip(g.sbrn(&b, "sig s1", true, 0)), "○· SIGNAL P1 · sig s1 · s1"; got != want {
		t.Errorf("live: %q, want %q", got, want)
	}
	if got, want := ansi.Strip(g.sbrn(&b, "sig s1", false, 0)), "○ SIGNAL P1 · sig s1 · s1"; got != want {
		t.Errorf("non-live: %q, want %q", got, want)
	}
	if got := ansi.Strip(g.sbrn(&b, "sig s1", false, 10)); !strings.HasPrefix(got, "○ SIGNAL     P1") {
		t.Errorf("kind pad: %q", got)
	}
}

// An Initiative group header on Ideate reads as the Initiative's SBRN, keeping its caret and count.
func TestIdeateGroupHeaderIsInitiativeSBRN(t *testing.T) {
	m := ideaModel(t, 100, 30, []bd.Bead{ini("ini", "open", "Ship v2", 1), sig("a", "open", 1, "ini"), sig("b", "open", 1, "ini")}, map[string]session.Info{})
	if v := ansi.Strip(m.View()); !strings.Contains(v, "▾ ○·  INITIATIVE  P1 · Ship v2 · ini") {
		t.Errorf("header not SBRN:\n%s", v)
	}
}

// A detail drawer's first line is the bead's SBRN, dot slot included, as in its row.
func TestIdeateDetailHeadingIsSBRN(t *testing.T) {
	bs := []bd.Bead{sig("a", "open", 1, "")}
	tab := ideaTab{ideaData: ideaData{beads: bs, w: 100, h: 30}}
	if got, _, _ := strings.Cut(ansi.Strip(tab.beadText(&bs[0])), "\n"); got != "○· SIGNAL P1 · sig a · a" {
		t.Errorf("heading %q", got)
	}
}

// Only leaf rows (sbrnLeaf) recolour the priority like the title; every other
// SBRN keeps prio()'s styling, AFK-kind or closed alike.
func TestSBRNPriorityRecolourIsLeafOnly(t *testing.T) {
	forceANSI256(t)
	var g graphData
	afk := bd.Bead{ID: "c1", Kind: contract.KindBuild, Status: "open", Title: "t", Priority: 1, Created: epoch}
	closed := sig("s1", "closed", 1, "")
	for _, b := range []bd.Bead{afk, closed} {
		if got := g.sbrn(&b, "t", true, 0); !strings.Contains(got, prio(b.Priority)) {
			t.Errorf("%s: sbrn recoloured priority: %q", b.ID, got)
		}
	}
	// an open AFK leaf keeps the P1 colour on its digit; the mute does not reach the badge
	got := g.sbrnLeaf(&afk, "t", 0, false)
	if !strings.Contains(got, prio(1)) || strings.Contains(got, softSt.Render("P1")) {
		t.Errorf("open AFK leaf: want prio(1), not muted P1: %q", got)
	}
	// a closed leaf restyles the whole badge
	if got := g.sbrnLeaf(&closed, "t", 0, false); !strings.Contains(got, closedSty.Render("P1")) {
		t.Errorf("closed leaf: want closed-styled P1: %q", got)
	}
}

// A row's badge reads P<eff> alone when its own level is effective, else P<eff>‹<own>
// with the own part dim; P0/P1 colour only the effective digit; nothing is tinted.
func TestSBRNBadgeShowsEffectiveThenOwn(t *testing.T) {
	forceANSI256(t)
	mk := func(cp int) graphData {
		return graphData{beads: []bd.Bead{
			ini("i", "open", "I", cp),
			{ID: "cp", Kind: contract.KindCheckpoint, Container: true, Status: "open", Priority: 3, Parent: "i", Created: epoch},
			{ID: "m", Kind: contract.KindBuild, Status: "open", Title: "t", Priority: 2, Parent: "cp", Created: epoch},
		}}
	}
	g := mk(0)
	m := &g.beads[2]
	for name, got := range map[string]string{
		"sbrn": g.sbrn(m, "t", true, 0),
		"leaf": g.sbrnLeaf(m, "t", 0, false),
	} {
		if !strings.Contains(ansi.Strip(got), "P0‹2") {
			t.Errorf("%s: want P0‹2 from the Initiative: %q", name, ansi.Strip(got))
		}
		if name == "sbrn" && !strings.Contains(got, prio(0)+dimSt.Render("‹2")) || strings.Contains(got, "48;5;") {
			t.Errorf("%s: P0 not on the effective digit only, or tinted: %q", name, got)
		}
	}
	// a Chart inside the P0 Initiative reads P0‹3
	if got := ansi.Strip(g.sbrn(&g.beads[1], "t", true, 0)); !strings.Contains(got, "P0‹3") {
		t.Errorf("container row: %q", got)
	}
	// own is effective: the single level
	g = mk(4)
	g.beads[1].Priority = 4
	g.beads[2].Priority = 1
	if got := ansi.Strip(g.sbrn(&g.beads[2], "t", true, 0)); !strings.Contains(got, "P1 ") || strings.Contains(got, "‹") {
		t.Errorf("own-effective badge: %q", got)
	}
	// a leaf restyle (closed) carries the whole badge, dim part included
	g = mk(0)
	g.beads[2].Status = "closed"
	st, _, _ := leafRestyle(&g.beads[2], false)
	if got := g.sbrnLeaf(&g.beads[2], "t", 0, false); !strings.Contains(got, st.Render("P0‹2")) || strings.Contains(got, "48;5;") {
		t.Errorf("closed leaf badge: %q", got)
	}
	// a Draft leaf is wholly dim
	g = mk(0)
	g.beads[2].Status = "deferred"
	if !g.beads[2].Draft() {
		t.Fatal("fixture is not a Draft")
	}
	if got := g.sbrnLeaf(&g.beads[2], "t", 0, false); !strings.Contains(got, dimSt.Render("P0‹2")) {
		t.Errorf("draft leaf badge not wholly dim: %q", got)
	}
	// an AFK leaf inheriting P0 keeps prio(0), with the own part soft
	g = mk(0)
	if got := g.sbrnLeaf(&g.beads[2], "t", 0, false); !strings.Contains(got, prio(0)+softSt.Render("‹2")) {
		t.Errorf("afk leaf badge: %q", got)
	}
	// a hot leaf inheriting P0 carries prio(0), the own part pink
	g = mk(0)
	if got := g.sbrnLeaf(&g.beads[2], "t", 0, true); !strings.Contains(got, prio(0)+pinkFg.Render("‹2")) {
		t.Errorf("hot leaf badge: %q", got)
	}
	// a hot leaf at P1 carries prio(1)
	g = mk(4)
	g.beads[1].Priority = 4
	g.beads[2].Priority = 1
	if got := g.sbrnLeaf(&g.beads[2], "t", 0, true); !strings.Contains(got, prio(1)) {
		t.Errorf("hot leaf badge: %q", got)
	}
	// an AFK leaf at P3 takes the soft style wholly
	g.beads[2].Priority = 3
	g.beads[1].Priority = 3
	if got := g.sbrnLeaf(&g.beads[2], "t", 0, false); !strings.Contains(got, softSt.Render("P3")) {
		t.Errorf("afk P3 badge: %q", got)
	}
}

// A container's title style follows closed > hot > plain, and is never AFK-muted.
func TestContainerTitleStyle(t *testing.T) {
	forceANSI256(t)
	for _, k := range []contract.Kind{contract.KindChart, contract.KindCheckpoint, contract.KindInitiative} {
		for _, c := range []struct {
			status string
			hot    bool
			want   string
		}{{"closed", true, "closed"}, {"closed", false, "closed"}, {"open", true, "pink"}, {"open", false, "plain"}} {
			b := bd.Bead{Kind: k, Status: c.status}
			st, plain := containerTitleStyle(&b, c.hot)
			got := "plain"
			switch {
			case !plain && st.Render("x") == closedSty.Render("x"):
				got = "closed"
			case !plain && st.Render("x") == pinkFg.Render("x"):
				got = "pink"
			case !plain:
				got = "other"
			}
			if got != c.want {
				t.Errorf("%s %s hot=%v: %s, want %s", k, c.status, c.hot, got, c.want)
			}
		}
	}
}

// A closed container's Graph header title carries the closed style; an open one's does not.
func TestGraphClosedHeaderTitleStruck(t *testing.T) {
	forceANSI256(t)
	beads := []bd.Bead{
		{ID: "done", Kind: contract.KindChart, Container: true, Status: "closed", Title: "Done chart", Created: epoch},
		{ID: "live", Kind: contract.KindChart, Container: true, Status: "open", Title: "Live chart", Created: epoch},
		{ID: "m", Kind: contract.KindErrand, Status: "open", Title: "member", Parent: "live", Created: epoch},
	}
	m := sized(t, 120, 30)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: map[string]session.Info{}})
	m, _ = send(t, m, runeKey("4"))
	m = pick(t, m, "s", "any")
	v := m.View()
	if !strings.Contains(v, closedSty.Render("Done chart")) {
		t.Errorf("closed header not struck:\n%s", v)
	}
	if strings.Contains(v, closedSty.Render("Live chart")) {
		t.Errorf("open header struck:\n%s", v)
	}
}

// A closed Checkpoint's Execute row title carries the closed style; an open one's does not.
func TestExecClosedCheckpointTitleStruck(t *testing.T) {
	forceANSI256(t)
	var g graphData
	tab := execTab{g: g}
	for status, want := range map[string]bool{"closed": true, "open": false} {
		b := bd.Bead{ID: "c", Kind: contract.KindCheckpoint, Container: true, Status: status, Title: "Ckpt", Created: epoch}
		l, _ := tab.line(eRow{kind: eCP, b: &b}, 0, nil, "")
		if got := strings.Contains(l, closedSty.Render("Ckpt")); got != want {
			t.Errorf("%s: struck=%v, want %v: %q", status, got, want, l)
		}
	}
}

func TestRowCmpOrdersByEffectiveThenOwn(t *testing.T) {
	mk := func(cp int) graphData {
		return graphData{beads: []bd.Bead{
			{ID: "cp", Kind: contract.KindCheckpoint, Container: true, Status: "open", Priority: cp, Created: epoch},
			{ID: "in", Kind: contract.KindBuild, Status: "open", Priority: 2, Parent: "cp", Created: epoch},
			{ID: "own", Kind: contract.KindBuild, Status: "open", Priority: 1, Created: epoch},
			{ID: "done", Kind: contract.KindBuild, Status: "closed", Priority: 0, Parent: "cp", Created: epoch},
		}}
	}
	order := func(g graphData) []string {
		bs := slices.Clone(g.beads[1:])
		slices.SortStableFunc(bs, g.rowCmp)
		var ids []string
		for _, b := range bs {
			ids = append(ids, b.ID)
		}
		return ids
	}
	if got := order(mk(0)); !slices.Equal(got, []string{"in", "own", "done"}) {
		t.Errorf("P2 inheriting P0 must beat own P1, closed last: %v", got)
	}
	// raising or lowering the container re-ranks its members on the next reload
	if got := order(mk(3)); !slices.Equal(got, []string{"own", "in", "done"}) {
		t.Errorf("P3 container must not lift: %v", got)
	}
}
