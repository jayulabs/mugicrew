package tui

import (
	"slices"
	"testing"

	"jayulabs.org/mugicrew/bd"
)

func snapFixture() []bd.Bead {
	return []bd.Bead{
		{ID: "a", Title: "A"}, {ID: "b", Title: "B", Parent: "a"}, {ID: "c", Title: "C", Parent: "b"},
		{ID: "orphan", Title: "O", Parent: "gone"},
	}
}

func TestSnapIndexMatchesLinearScan(t *testing.T) {
	beads := snapFixture()
	idx := graphData{beads: beads, idx: newSnapIndex(beads)}
	lin := graphData{beads: beads}
	for _, id := range []string{"a", "b", "c", "orphan", "gone", ""} {
		if g, w := idx.beadByID(id), lin.beadByID(id); g.ID != w.ID || g.Title != w.Title || g.Parent != w.Parent {
			t.Errorf("beadByID(%q) = %+v; linear %+v", id, g, w)
		}
	}
	if b := idx.beadByID("gone"); b.Title != "gone" {
		t.Errorf("missing ID placeholder title = %q; want the ID", b.Title)
	}
}

func TestSnapIndexSeesPriorityEcho(t *testing.T) {
	beads := snapFixture()
	g := graphData{beads: beads, idx: newSnapIndex(beads)}
	g.beads = append([]bd.Bead(nil), beads...)
	g.beads[1].Priority = 0
	g.beads[2].Priority = 4
	if got := g.beadByID("c").Priority; got != 4 {
		t.Errorf("echoed priority = %d; want 4", got)
	}
}

func TestSnapIndexSubgraphMatchesLinearScan(t *testing.T) {
	beads := []bd.Bead{
		{ID: "r"}, {ID: "x", Parent: "r"}, {ID: "y", Parent: "x"}, {ID: "z", Parent: "r"},
		{ID: "w", Parent: "y"}, {ID: "orphan", Parent: "gone"},
		{ID: "c1", Parent: "c2"}, {ID: "c2", Parent: "c1"}, // a parent cycle
	}
	idx := graphData{beads: beads, idx: newSnapIndex(beads)}
	lin := graphData{beads: beads}
	for _, id := range []string{"r", "x", "gone", "c1", "nope"} {
		ids := func(g graphData) (out []string) {
			for _, b := range subgraph(g, id) {
				out = append(out, b.ID)
			}
			return
		}
		if g, w := ids(idx), ids(lin); !slices.Equal(g, w) {
			t.Fatalf("subgraph(%q) = %v; linear %v", id, g, w)
		}
	}
	var ids []string
	for _, b := range subgraph(idx, "r") {
		ids = append(ids, b.ID)
	}
	if want := []string{"x", "z", "y", "w"}; !slices.Equal(ids, want) {
		t.Errorf("subgraph(r) = %v; want BFS %v", ids, want)
	}
}
