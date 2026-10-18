package tui

import (
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
	var g graphData
	afk := bd.Bead{ID: "c1", Kind: contract.KindBuild, Status: "open", Title: "t", Priority: 1, Created: epoch}
	closed := sig("s1", "closed", 1, "")
	for _, b := range []bd.Bead{afk, closed} {
		if got := g.sbrn(&b, "t", true, 0); !strings.Contains(got, prio(b.Priority)) {
			t.Errorf("%s: sbrn recoloured priority: %q", b.ID, got)
		}
		st, _ := leafTitleStyle(&b, false)
		if got := g.sbrnLeaf(&b, "t", 0, false); !strings.Contains(got, st.Render("P1")) {
			t.Errorf("%s: leaf did not mute priority: %q", b.ID, got)
		}
	}
}

// A member row's priority badge takes its container's tint (P0 203, P1 217, else none)
// and keeps its own P-level; leaf restyle keeps the tint; a top-level row has none.
func TestSBRNMemberBadgeTintedByContainer(t *testing.T) {
	forceANSI256(t)
	cp := func(p int) []bd.Bead {
		return []bd.Bead{{ID: "cp", Kind: contract.KindCheckpoint, Container: true, Status: "open", Priority: p, Created: epoch},
			{ID: "m", Kind: contract.KindBuild, Status: "open", Title: "t", Priority: 3, Parent: "cp", Created: epoch}}
	}
	for _, c := range []struct {
		cp   int
		want string
	}{{0, "48;5;203"}, {1, "48;5;217"}, {2, ""}, {4, ""}} {
		g := graphData{beads: cp(c.cp)}
		for name, got := range map[string]string{
			"sbrn": g.sbrn(&g.beads[1], "t", true, 0),
			"leaf": g.sbrnLeaf(&g.beads[1], "t", 0, false),
		} {
			if has := strings.Contains(got, "48;5;"); has != (c.want != "") || c.want != "" && !strings.Contains(got, c.want) {
				t.Errorf("P%d container %s: %q, want bg %q", c.cp, name, got, c.want)
			}
			if !strings.Contains(ansi.Strip(got), "P3") {
				t.Errorf("%s: badge not member's own P3: %q", name, got)
			}
		}
	}
	g := graphData{beads: cp(0)}
	g.beads[1].Status = "closed"
	if got := g.sbrnLeaf(&g.beads[1], "t", 0, false); !strings.Contains(got, "48;5;203") {
		t.Errorf("closed leaf lost tint: %q", got)
	}
	top := sig("s", "open", 3, "")
	if got := g.sbrn(&top, "t", true, 0); strings.Contains(got, "48;5;") {
		t.Errorf("top-level row tinted: %q", got)
	}
	g = graphData{beads: []bd.Bead{ini("i", "open", "I", 0), sig("s", "open", 3, "i")}}
	if got := g.sbrn(&g.beads[1], "t", true, 0); strings.Contains(got, "48;5;") {
		t.Errorf("Initiative-child tinted: %q", got)
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
		{ID: "m", Kind: contract.KindTask, Status: "open", Title: "member", Parent: "live", Created: epoch},
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
