package bd_test

import (
	"slices"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/bd/bdtest"
	"jayulabs.org/mugicrew/contract"
)

func edgeTypeHeld(t *testing.T, f *bdtest.Fake, from, to string) string {
	t.Helper()
	rows, err := f.Edges(from, bd.Down, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == to {
			return r.Type
		}
	}
	return ""
}

func TestCarryOnFakeMovesOrderingAndLatches(t *testing.T) {
	f := bdtest.New(t,
		bd.Bead{ID: "t-pred", Status: "open", Kind: contract.KindSpec},
		bd.Bead{ID: "t-succ", Status: "open", Kind: contract.KindCheckpoint},
		bd.Bead{ID: "t-a", Status: "open", Kind: contract.KindBuild, BlockedBy: []string{"t-pred"}},
		bd.Bead{ID: "t-b", Status: "deferred", Kind: contract.KindBuild, BlockedBy: []string{"t-pred"}, CondBlockedBy: []string{"t-pred"}},
		bd.Bead{ID: "t-done", Status: "closed", Kind: contract.KindBuild, BlockedBy: []string{"t-pred"}},
		bd.Bead{ID: "t-latch", Status: "open", Kind: contract.KindLatch},
		bd.Bead{ID: "t-oldlatch", Status: "closed", Kind: contract.KindLatch},
	)
	// pred is blocked by the latches.
	if err := bd.Blocks("t-pred", "t-latch"); err != nil {
		t.Fatal(err)
	}
	if err := bd.Blocks("t-pred", "t-oldlatch"); err != nil {
		t.Fatal(err)
	}
	moved, skipped, err := bd.Carry("t-pred", "t-succ")
	if err != nil {
		t.Fatalf("Carry: %v", err)
	}
	if want := []string{"t-a", "t-b", "t-latch"}; !slices.Equal(moved, want) {
		t.Errorf("moved = %v, want %v", moved, want)
	}
	if len(skipped) != 1 || skipped[0] != (bd.CarrySkip{Bead: "t-done", Reason: "closed"}) {
		t.Errorf("skipped = %v, want t-done closed", skipped)
	}
	if got := edgeTypeHeld(t, f, "t-b", "t-succ"); got != "conditional-blocks" {
		t.Errorf("t-b → t-succ = %q, want conditional-blocks", got)
	}
	if got := edgeTypeHeld(t, f, "t-succ", "t-latch"); got != "blocks" {
		t.Errorf("t-succ → t-latch = %q, want blocks", got)
	}
	if got := edgeTypeHeld(t, f, "t-a", "t-pred"); got != "blocks" {
		t.Errorf("old edge must stay, got %q", got)
	}
}

func TestCarryOnFakeSkipsRefusedEdges(t *testing.T) {
	bdtest.New(t,
		bd.Bead{ID: "t-pred", Status: "open", Kind: contract.KindSpec},
		bd.Bead{ID: "t-succ", Status: "open", Kind: contract.KindCheckpoint},
		bd.Bead{ID: "t-succ.1", Status: "open", Kind: contract.KindBuild, BlockedBy: []string{"t-pred"}},
		bd.Bead{ID: "t-loop", Status: "open", Kind: contract.KindBuild, BlockedBy: []string{"t-pred"}},
		bd.Bead{ID: "t-clash", Status: "open", Kind: contract.KindBuild, BlockedBy: []string{"t-pred"}},
	)
	if err := bd.Blocks("t-succ", "t-loop"); err != nil { // succ depends on loop: loop → succ closes a cycle
		t.Fatal(err)
	}
	if err := bd.DiscoveredFrom("t-clash", "t-succ"); err != nil { // the pair already holds another type
		t.Fatal(err)
	}
	moved, skipped, err := bd.Carry("t-pred", "t-succ")
	if err != nil {
		t.Fatalf("Carry: %v", err)
	}
	if len(moved) != 0 {
		t.Errorf("moved = %v, want none", moved)
	}
	want := []bd.CarrySkip{{Bead: "t-clash", Reason: "type-clash"}, {Bead: "t-loop", Reason: "cycle"}, {Bead: "t-succ.1", Reason: "hierarchy"}}
	if !slices.Equal(skipped, want) {
		t.Errorf("skipped = %v, want %v", skipped, want)
	}
}

func TestSupersedeOnFakeOverOtherTypeEdge(t *testing.T) {
	f := bdtest.New(t,
		bd.Bead{ID: "t-old", Status: "open", Kind: contract.KindSpec},
		bd.Bead{ID: "t-new", Status: "open", Kind: contract.KindSpec},
	)
	if err := bd.Related("t-old", "t-new"); err != nil {
		t.Fatal(err)
	}
	if err := bd.Supersede("t-old", "t-new"); err != nil {
		t.Fatalf("Supersede: %v", err)
	}
	if got := edgeTypeHeld(t, f, "t-old", "t-new"); got != "supersedes" {
		t.Errorf("edge = %q, want supersedes", got)
	}
	if b, _ := bd.Show("t-old"); b.Status != "closed" {
		t.Errorf("old status = %q, want closed", b.Status)
	}
	if err := bd.Supersede("t-old", "t-new"); err != nil { // already superseded: a no-op
		t.Errorf("repeat Supersede: %v", err)
	}
}

func TestDiscoveredFromOnFakeIsIdempotent(t *testing.T) {
	f := bdtest.New(t,
		bd.Bead{ID: "t-a", Status: "open", Kind: contract.KindSpec},
		bd.Bead{ID: "t-b", Status: "open", Kind: contract.KindCritique},
	)
	for range 2 {
		if err := bd.DiscoveredFrom("t-a", "t-b"); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := f.Edges("t-a", bd.Down, "discovered-from")
	if err != nil || len(rows) != 1 || rows[0].ID != "t-b" {
		t.Errorf("rows = %v, %v; want one t-b", rows, err)
	}
}

// EndedSkips lists open blocks and conditional-blocks dependents as skipped ended, and
// leaves closed dependents and non-ordering edges out.
func TestEndedSkipsOnFake(t *testing.T) {
	bdtest.New(t,
		bd.Bead{ID: "p", Status: "open"},
		bd.Bead{ID: "a", Status: "open", BlockedBy: []string{"p"}},
		bd.Bead{ID: "b", Status: "deferred", BlockedBy: []string{"p"}, CondBlockedBy: []string{"p"}},
		bd.Bead{ID: "c", Status: "closed", BlockedBy: []string{"p"}},
		bd.Bead{ID: "r", Status: "open", Related: []string{"p"}},
	)
	got, err := bd.EndedSkips("p")
	if err != nil {
		t.Fatal(err)
	}
	if want := []bd.CarrySkip{{Bead: "a", Reason: "ended"}, {Bead: "b", Reason: "ended"}}; !slices.Equal(got, want) {
		t.Errorf("EndedSkips = %v, want %v", got, want)
	}
}

// LiveHead's real reads point the right way: caused-by Up, then supersedes Down.
func TestLiveHeadOnFake(t *testing.T) {
	bdtest.New(t,
		bd.Bead{ID: "s", Status: "closed"},
		bd.Bead{ID: "c", Status: "open", CausedBy: "s"},
		bd.Bead{ID: "a", Status: "closed"},
		bd.Bead{ID: "z", Status: "open"},
	)
	if err := bd.Supersede("a", "z"); err != nil {
		t.Fatal(err)
	}
	for from, want := range map[string]string{"s": "c", "a": "z", "c": "c"} {
		if got, ok, err := bd.LiveHead(from); err != nil || !ok || got != want {
			t.Errorf("LiveHead(%s) = %q, %v, %v; want %q", from, got, ok, err, want)
		}
	}
}
