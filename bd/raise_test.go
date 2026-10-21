package bd

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/contract"
)

// fakeGraph stands in for bd behind run/runBatch: an edge set ("typ from to")
// the dep-list reads answer from and the publish batch writes, plus the closes.
type fakeGraph struct {
	edges   map[string]bool
	adds    int
	batches int
	closed  []string // "id reason"
}

func stubGraph(t *testing.T, g *fakeGraph) {
	t.Helper()
	if g.edges == nil {
		g.edges = map[string]bool{}
	}
	oRun, oBatch := run, runBatch
	t.Cleanup(func() { run, runBatch = oRun, oBatch })
	run = func(args ...string) (string, error) {
		switch {
		case args[0] == "dep" && args[1] == "list": // dep list <from> --direction=down --json -t <typ>
			var rows []string
			for k := range g.edges {
				if f := strings.Fields(k); f[0] == args[len(args)-1] && f[1] == args[2] {
					rows = append(rows, fmt.Sprintf(`{"id":%q}`, f[2]))
				}
			}
			return "[" + strings.Join(rows, ",") + "]", nil
		case args[0] == "list": // Drafts: none
			return "[]", nil
		}
		return "", nil
	}
	runBatch = func(script string) error {
		g.batches++
		for _, l := range strings.Split(strings.TrimSpace(script), "\n") {
			f := strings.Fields(l)
			switch f[0] {
			case "dep":
				if k := f[4] + " " + f[2] + " " + f[3]; g.edges[k] {
					t.Errorf("publish handed an edge bd already holds: %s (one failing batch line rolls the publish back)", k)
				} else {
					g.edges[k] = true
					g.adds++
				}
			case "close":
				g.closed = append(g.closed, strings.TrimPrefix(l, "close "))
			}
		}
		return nil
	}
}

// publishingCtx is a dispatched effort's Ctx: it carries the contract.Publisher.
type publishingCtx struct {
	contract.Ctx
	closes []string
	edges  []Edge
}

func (c *publishingCtx) CloseOnPublish(reason string) { c.closes = append(c.closes, reason) }
func (c *publishingCtx) EdgeOnPublish(e Edge)         { c.edges = append(c.edges, e) }

var raiser = Bead{ID: "ws-build", Parent: "ws-cp"}

// Outside an effort the raise publishes Checkpoint blocks→pivot, pivot
// related→Spec and the 'pivoted:' close itself, in one batch. A re-run over the
// wired graph (a crash before the close stuck) adds no edge (FR9).
func TestRaisePivotPublishesGateAndCloseOutsideEffort(t *testing.T) {
	g := &fakeGraph{}
	stubGraph(t, g)
	for range 2 {
		if err := RaisePivot(nil, raiser, "ws-cp", "ws-spec", []Bead{{ID: "ws-piv"}}); err != nil {
			t.Fatalf("RaisePivot: %v", err)
		}
	}
	if !g.edges["blocks ws-cp ws-piv"] || !g.edges["related ws-piv ws-spec"] || g.adds != 2 {
		t.Errorf("raise must wire the gate and Spec edges once, got %v (adds=%d)", g.edges, g.adds)
	}
	if want := []string{`ws-build "pivoted: ws-piv"`, `ws-build "pivoted: ws-piv"`}; !reflect.DeepEqual(g.closed, want) {
		t.Errorf("closes = %v, want %v", g.closed, want)
	}
}

// Spec D6 / FR 24: in a dispatched effort the raise writes nothing itself — the
// edges and the 'pivoted:' close ride in the publish, so the Draft pivot and its
// gate appear in one batch. An edge bd already holds (a merged-into pivot's gate)
// is not handed over.
func TestRaisePivotHandsGateAndCloseToPublish(t *testing.T) {
	g := &fakeGraph{edges: map[string]bool{"blocks ws-cp ws-old": true}}
	stubGraph(t, g)
	ctx := &publishingCtx{}
	if err := RaisePivot(ctx, raiser, "ws-cp", "ws-spec", []Bead{{ID: "ws-piv"}, {ID: "ws-old"}}); err != nil {
		t.Fatalf("RaisePivot: %v", err)
	}
	want := []Edge{{From: "ws-cp", To: "ws-piv", Type: "blocks"}, {From: "ws-piv", To: "ws-spec", Type: "related"}, {From: "ws-old", To: "ws-spec", Type: "related"}}
	if !reflect.DeepEqual(ctx.edges, want) {
		t.Errorf("edges handed to the publish = %v, want %v", ctx.edges, want)
	}
	if g.batches != 0 {
		t.Errorf("the raise must not publish itself in an effort, got %d batches", g.batches)
	}
	if !reflect.DeepEqual(ctx.closes, []string{"pivoted: ws-piv, ws-old"}) {
		t.Errorf("close handed to the publish = %v", ctx.closes)
	}
}

// Spec D7: a headless Checkpoint cannot relate its pivot to a Spec, so the raise
// errors before writing anything — no gate freezes the Checkpoint behind a pivot
// nobody can resolve; the raiser's failure path pauses it for the human.
func TestRaisePivotWithoutSpecErrorsAndWritesNothing(t *testing.T) {
	for name, ctx := range map[string]*publishingCtx{"outside": nil, "effort": {}} {
		g := &fakeGraph{}
		stubGraph(t, g)
		var c contract.Ctx
		if ctx != nil {
			c = ctx
		}
		if err := RaisePivot(c, raiser, "ws-cp", "", []Bead{{ID: "ws-piv"}}); err == nil {
			t.Fatalf("%s: a Spec-less raise must error", name)
		}
		if len(g.edges) != 0 || g.batches != 0 {
			t.Errorf("%s: must publish nothing, got edges=%v batches=%d", name, g.edges, g.batches)
		}
		if ctx != nil && len(ctx.edges)+len(ctx.closes) != 0 {
			t.Errorf("%s: nothing may be handed to the tail, got %v %v", name, ctx.edges, ctx.closes)
		}
	}
}

// A rerouted Signal is not the host's member: the Checkpoint passed in, not the
// raiser's parent, takes the gate edge.
func TestRaisePivotGatesTheExplicitCheckpoint(t *testing.T) {
	g := &fakeGraph{}
	stubGraph(t, g)
	if err := RaisePivot(nil, raiser, "ws-host", "ws-spec", []Bead{{ID: "ws-piv"}}); err != nil {
		t.Fatalf("RaisePivot: %v", err)
	}
	if !g.edges["blocks ws-host ws-piv"] || g.edges["blocks ws-cp ws-piv"] {
		t.Errorf("gate must be on the passed Checkpoint, got %v", g.edges)
	}
}

// The no-Spec error names the Checkpoint passed in.
func TestRaisePivotWithoutSpecNamesPassedCheckpoint(t *testing.T) {
	stubGraph(t, &fakeGraph{})
	err := RaisePivot(nil, raiser, "ws-host", "", []Bead{{ID: "ws-piv"}})
	if err == nil || !strings.Contains(err.Error(), "ws-host") || strings.Contains(err.Error(), "ws-cp") {
		t.Errorf("error must name ws-host, got %v", err)
	}
}

// GateEdges skips edges bd already holds.
func TestGateEdgesSkipsPresent(t *testing.T) {
	stubGraph(t, &fakeGraph{edges: map[string]bool{"blocks ws-cp ws-a": true, "related ws-b ws-spec": true}})
	got, err := GateEdges("ws-cp", "ws-spec", []string{"ws-a", "ws-b"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Edge{{From: "ws-cp", To: "ws-b", Type: "blocks"}, {From: "ws-a", To: "ws-spec", Type: "related"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GateEdges = %v, want %v", got, want)
	}
}
