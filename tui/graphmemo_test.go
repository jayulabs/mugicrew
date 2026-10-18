package tui

import (
	"fmt"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// The deferred status facet reveals Drafts in the counts the moment rebuild runs,
// and leaving it hides them again (Spec FR4, FR5, FR6).
func TestGraphCountsFollowStatusFacet(t *testing.T) {
	m := sized(t, 120, 40)
	m, _ = send(t, m, loadedMsg{beads: draftBeads(), live: map[string]bool{}, sessions: map[string]session.Info{}})
	g := gr(m)
	if _, tot := g.childCounts("ch"); tot != 3 {
		t.Fatalf("default childCounts(ch) total = %d; want 3", tot)
	}
	g.q.status = "deferred"
	g = g.rebuild()
	if _, tot := g.childCounts("ch"); tot != 4 {
		t.Errorf("deferred facet childCounts(ch) total = %d; want 4", tot)
	}
	g.q.status = baseline.status
	if _, tot := g.rebuild().childCounts("ch"); tot != 3 {
		t.Errorf("back off deferred childCounts(ch) total = %d; want 3", tot)
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
