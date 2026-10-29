package tui

import (
	"fmt"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// A folded header's total counts its Drafts at the default facet and under draft
// (Spec FR9).
func TestGraphCountsFollowStatusFacet(t *testing.T) {
	m := sized(t, 120, 40)
	m, _ = send(t, m, loadedMsg{beads: draftBeads(), live: map[string]bool{}, sessions: map[string]session.Info{}})
	g := gr(m)
	if _, tot := g.childCounts("ch"); tot != 4 {
		t.Fatalf("default childCounts(ch) total = %d; want 4", tot)
	}
	g.q.status = "draft"
	g = g.rebuild()
	if _, tot := g.childCounts("ch"); tot != 4 {
		t.Errorf("draft facet childCounts(ch) total = %d; want 4", tot)
	}
}

// BenchmarkGraphView renders the Graph over ~2,600 beads: an O(rows × beads)
// regression in View shows as a number.
func BenchmarkGraphView(b *testing.B) {
	var beads []bd.Bead
	for i := range 26 {
		ini := fmt.Sprintf("i%d", i)
		beads = append(beads, bd.Bead{ID: ini, Kind: contract.KindInitiative, Container: true, Status: "open", Title: ini, Created: epoch})
		for j := range 10 {
			ch := fmt.Sprintf("i%dc%d", i, j)
			beads = append(beads, bd.Bead{ID: ch, Kind: contract.KindChart, Container: true, Status: "open", Title: ch, Parent: ini, Created: epoch})
			for k := range 9 {
				st := "closed"
				if k == 0 {
					st = "open"
				}
				beads = append(beads, bd.Bead{ID: fmt.Sprintf("%sb%d", ch, k), Kind: contract.KindBuild, Status: st, Title: "build", Parent: ch, Created: epoch})
			}
		}
	}
	g := graphTab{graphData: graphData{beads: beads, w: 120, h: 40}, qfold: map[string]bool{}}.rebuild()
	b.ResetTimer()
	for range b.N {
		_ = g.view(120, 40)
	}
}

// BenchmarkIdeaView renders the Ideas tab at ~1,300 and ~2,600 beads. Indexed, time grows
// near-linearly per doubling (each frame builds the idea lists once);
// with the index dropped (every lookup a linear scan) it grows ~x4 per doubling.
func BenchmarkIdeaView(b *testing.B) {
	for _, inits := range []int{13, 26} {
		var beads []bd.Bead
		for i := range inits {
			ini := fmt.Sprintf("i%d", i)
			beads = append(beads, bd.Bead{ID: ini, Kind: contract.KindInitiative, Container: true, Status: "open", Title: ini, Created: epoch})
			for j := range 99 {
				beads = append(beads, bd.Bead{ID: fmt.Sprintf("%ss%d", ini, j), Kind: contract.KindSignal, Status: "open", Title: "signal", Parent: ini, Created: epoch})
			}
		}
		for _, indexed := range []bool{true, false} {
			b.Run(fmt.Sprintf("beads=%d/idx=%t", len(beads), indexed), func(b *testing.B) {
				gd := graphData{beads: beads, w: 120, h: 40}
				if indexed {
					gd.idx = newSnapIndex(beads)
				}
				t := ideaTab{ideaData: ideaData{beads: beads, gate: gd, w: 120, h: 40}}
				b.ResetTimer()
				for range b.N {
					_ = t.view(120, 40)
				}
			})
		}
	}
}

// A Draft row reads draft in its status slot, even when blocked, and its title styles
// dim; a published child under a Draft is not dimmed (Spec FR3, FR4, FR6).
func TestDraftRowIsDim(t *testing.T) {
	g := graphData{beads: draftBeads()}
	d := g.beadByID("dbuild")
	if got := ansi.Strip(g.stateGlyph(d)); got != "draft" {
		t.Errorf("Draft status slot = %q; want draft", got)
	}
	if st, plain := leafTitleStyle(d, true); plain || st.GetForeground() != dim {
		t.Error("Draft leaf title not dim")
	}
	if st, plain := containerTitleStyle(g.beadByID("dch"), true); plain || st.GetForeground() != dim {
		t.Error("Draft container title not dim")
	}
	if st, _ := leafTitleStyle(&bd.Bead{ID: "kid", Kind: contract.KindBuild, Status: "open", Parent: "dch"}, false); st.GetForeground() == dim {
		t.Error("published child under a Draft is dimmed")
	}
}
