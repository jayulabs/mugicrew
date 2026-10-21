package luffy

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/git"
)

// rerouteGraph extends the fast-lane fakes with what the reroute leg reads and
// writes: the host Checkpoint and its children, the Drafts created so far, and the
// metadata recorded on the Signal.
type rerouteGraph struct {
	*fakeSignalGraph
	beads     map[string]bd.Bead
	hasSpec   bool
	landed    bool
	kids      map[string][]bd.Bead // host children by kind
	gated     []bd.Bead            // open pivots the host blocks
	drafts    []bd.Bead
	created   []bd.Successor
	deps      [][]string
	meta      map[string]any
	cleared   []string
	notes     []string
	failAfter int // createDraft fails once this many have been created (0: never)
}

const hostID = "ws-cp"

func stubRerouteSeams(t *testing.T) *rerouteGraph {
	t.Helper()
	g := &rerouteGraph{fakeSignalGraph: stubSignalSeams(t), hasSpec: true, kids: map[string][]bd.Bead{}, meta: map[string]any{},
		beads: map[string]bd.Bead{hostID: {ID: hostID, Kind: contract.KindCheckpoint, Status: "in_progress", Title: "host"}}}
	swap(t, &show, func(id string) (bd.Bead, error) {
		if b, ok := g.beads[id]; ok {
			return b, nil
		}
		return bd.Bead{}, fmt.Errorf("no bead %s", id)
	})
	swap(t, &initiativeOf, func(b bd.Bead) (string, error) {
		if p := g.beads[b.Parent]; b.Parent != "" && p.Kind == contract.KindInitiative && p.Unclosed() {
			return p.ID, nil
		}
		return "", nil
	})
	swap(t, &specOf, func(string) (string, bool, error) { return "ws-spec-of-host", g.hasSpec, nil })
	swap(t, &repoLanded, func(_ git.Repo, tr string) (string, error) {
		if g.landed && tr == "Checkpoint: "+hostID {
			return "landedsha", nil
		}
		return "", git.ErrNotLanded
	})
	swap(t, &childrenOfKind, func(_ string, k contract.Kind) ([]bd.Bead, error) { return g.kids[string(k)], nil })
	swap(t, &surgeriesOf, func(string) ([]bd.Bead, error) { return g.kids["surgery"], nil })
	swap(t, &hostPivots, func(string) ([]bd.Bead, error) { return g.gated, nil })
	swap(t, &draftsOf, func(string) ([]bd.Bead, error) { return g.drafts, nil })
	swap(t, &createDraft, func(pred string, s bd.Successor, deps ...string) (string, error) {
		if g.failAfter > 0 && len(g.created) >= g.failAfter {
			g.failAfter = 0
			return "", errors.New("bd down")
		}
		id := fmt.Sprintf("ws-d%d", len(g.created)+1)
		g.created = append(g.created, s)
		g.deps = append(g.deps, deps)
		var meta bd.Meta
		if s.Metadata != "" {
			_ = json.Unmarshal([]byte(s.Metadata), &meta)
		}
		g.drafts = append(g.drafts, bd.Bead{ID: id, Kind: s.Kind, Status: "deferred", CausedBy: pred, Parent: s.Parent, DiscoveredFrom: discoveredIDs(deps), Metadata: meta})
		return id, nil
	})
	swap(t, &clearMetadata, func(id, key string) error { g.cleared = append(g.cleared, id+"/"+key); return nil })
	swap(t, &setMetadata, func(id, key string, v any) error { g.meta[id+"/"+key] = v; return nil })
	swap(t, &notify, func(_, format string, a ...any) { g.notes = append(g.notes, fmt.Sprintf(format, a...)) })
	swap(t, &carryTargets, func(pred, ord, latch string) ([]string, []bd.CarrySkip, error) {
		g.seq = append(g.seq, "carry "+pred+" "+ord+" "+latch)
		return nil, nil, g.carryErr
	})
	swap(t, &mrURL, func(bd.Bead) string { return "https://gl/mr/7" })
	return g
}

func discoveredIDs(deps []string) []string {
	var out []string
	for _, d := range deps {
		if id, ok := strings.CutPrefix(d, "discovered-from:"); ok {
			out = append(out, id)
		}
	}
	return out
}

func (g *rerouteGraph) nothingWritten(t *testing.T, what string) {
	t.Helper()
	if len(g.created) != 0 || len(g.meta) != 0 || len(g.cleared) != 0 || g.spawns != 0 || len(g.seq) != 0 {
		t.Errorf("%s: created=%d meta=%v spawns=%d carries=%v, want nothing written", what, len(g.created), g.meta, g.spawns, g.seq)
	}
}

func pivotItem(body string) RerouteItem    { return RerouteItem{Pivot: true, Body: body} }
func critiqueItem(body string) RerouteItem { return RerouteItem{Body: body} }

// Spec FR 14–16: each pivot is a kind:pivot Draft by the Signal, caused-by it, with
// raise_checkpoint metadata; each Critique is a Draft by the Signal parented to the
// host and caused-by the Signal; the first --critique after a --pivot is its Ask
// Critique, discovered-from the pivot.
func TestSeedRerouteDraftShape(t *testing.T) {
	g := stubRerouteSeams(t)
	items := []RerouteItem{pivotItem("pivot A"), critiqueItem("ask A"), critiqueItem("plain"), pivotItem("pivot B"), critiqueItem("ask B")}
	if err := SeedReroute(openSignal(), hostID, items, nil); err != nil {
		t.Fatalf("SeedReroute: %v", err)
	}
	if len(g.created) != 5 {
		t.Fatalf("created %d, want 5", len(g.created))
	}
	type shape struct {
		kind               contract.Kind
		body, parent, meta string
		deps               []string
	}
	var got []shape
	for i, s := range g.created {
		if s.DraftedBy != "ws-sig" || s.Container {
			t.Errorf("%+v: want a leaf Draft by the Signal", s)
		}
		got = append(got, shape{s.Kind, s.Description, s.Parent, s.Metadata, g.deps[i]})
	}
	meta := `{"raise_checkpoint":"ws-cp"}`
	want := []shape{
		{contract.KindPivot, "pivot A", "", meta, nil},
		{contract.KindCritique, "ask A", hostID, "", []string{"discovered-from:ws-d1"}},
		{contract.KindCritique, "plain", hostID, "", nil},
		{contract.KindPivot, "pivot B", "", meta, nil},
		{contract.KindCritique, "ask B", hostID, "", []string{"discovered-from:ws-d4"}},
	}
	if !slices.EqualFunc(got, want, func(a, b shape) bool {
		return a.kind == b.kind && a.body == b.body && a.parent == b.parent && a.meta == b.meta && slices.Equal(a.deps, b.deps)
	}) {
		t.Errorf("drafts = %+v\nwant     %+v", got, want)
	}
}

// Spec FR 30: one triage.reroute record, naming the host and the pivot, Critique and
// merge ids; nothing is carried here and the Signal is not closed.
func TestSeedRerouteRecordsTriageReroute(t *testing.T) {
	evs := eventlogtest.Capture(t)
	g := stubRerouteSeams(t)
	g.gated = []bd.Bead{{ID: "ws-old", Kind: contract.KindPivot, Status: "open"}}
	g.beads["ws-old"] = g.gated[0]
	if err := SeedReroute(openSignal(), hostID, []RerouteItem{pivotItem("p"), critiqueItem("c"), critiqueItem("ask")}, []RaiseMerge{{Pivot: "ws-old", Evidence: "same FR", After: 2}}); err != nil {
		t.Fatalf("SeedReroute: %v", err)
	}
	again := openSignal()
	raw, _ := json.Marshal(g.meta["ws-sig/raise_merges"])
	again.Metadata = bd.Meta{"raise_merges": raw}
	if err := reconcileSignal(again, ""); err != nil {
		t.Fatalf("flagless re-run: %v", err)
	}
	var re []eventlogtest.Event
	for _, e := range *evs {
		if e.Name == "triage.reroute" {
			re = append(re, e)
		}
	}
	if len(re) != 2 {
		t.Fatalf("events = %+v, want a triage.reroute per run", *evs)
	}
	a := re[0].Attrs
	if a["host"] != hostID || fmt.Sprint(a["pivots"]) != "[ws-d1]" || fmt.Sprint(a["critiques"]) != "[ws-d2 ws-d3]" || fmt.Sprint(a["merges"]) != "[ws-old]" {
		t.Errorf("attrs = %+v, want host, pivots, critiques and merges", a)
	}
	if fmt.Sprint(re[1].Attrs["merges"]) != "[ws-old]" || re[1].Attrs["host"] != hostID {
		t.Errorf("re-run attrs = %+v, want the host and merges read back from the Signal's metadata", re[1].Attrs)
	}
}

// Spec FR 41–42, decision 7: reconcile carries the Signal's ordering dependents onto
// the host and its Latches onto the first pivot Draft, else the first Critique Draft;
// a flagless re-run re-asks the idempotent carry rather than stacking another.
func TestSeedRerouteCarries(t *testing.T) {
	g := stubRerouteSeams(t)
	if err := SeedReroute(openSignal(), hostID, []RerouteItem{critiqueItem("c1"), pivotItem("p"), critiqueItem("ask")}, nil); err != nil {
		t.Fatal(err)
	}
	if want := []string{"carry ws-sig ws-cp ws-d2"}; !slices.Equal(g.seq, want) {
		t.Errorf("carries = %v, want %v (Latches to the first pivot)", g.seq, want)
	}
	if err := reconcileSignal(openSignal(), ""); err != nil {
		t.Fatal(err)
	}
	if want := []string{"carry ws-sig ws-cp ws-d2", "carry ws-sig ws-cp ws-d2"}; !slices.Equal(g.seq, want) {
		t.Errorf("carries after re-run = %v, want the same targets", g.seq)
	}

	g2 := stubRerouteSeams(t)
	if err := SeedReroute(openSignal(), hostID, []RerouteItem{critiqueItem("c1"), critiqueItem("c2")}, nil); err != nil {
		t.Fatal(err)
	}
	if want := []string{"carry ws-sig ws-cp ws-d1"}; !slices.Equal(g2.seq, want) {
		t.Errorf("Critique-only carries = %v, want %v", g2.seq, want)
	}
}

// A failed carry fails the reconcile, leaving the Drafts for the next run to carry.
func TestSeedRerouteCarryErrorFails(t *testing.T) {
	g := stubRerouteSeams(t)
	g.carryErr = errors.New("bd down")
	if err := SeedReroute(openSignal(), hostID, []RerouteItem{critiqueItem("c")}, nil); err == nil {
		t.Fatal("want the carry error")
	}
}

// Spec FR 23, decision 4: a merge writes nothing to its target; the targets and
// evidence ride on the Signal's own metadata for the publish. The Signal's own
// Critique carries the Ask and is what the publish closes the Signal on.
func TestSeedRerouteRecordsMergesOnSignal(t *testing.T) {
	g := stubRerouteSeams(t)
	g.gated = []bd.Bead{{ID: "ws-old", Kind: contract.KindPivot, Status: "open"}}
	g.beads["ws-old"] = g.gated[0]
	if err := SeedReroute(openSignal(), hostID, []RerouteItem{critiqueItem("ask")}, []RaiseMerge{{Pivot: "ws-old", Evidence: "same FR"}}); err != nil {
		t.Fatalf("SeedReroute: %v", err)
	}
	got, ok := g.meta["ws-sig/raise_merges"].([]RaiseMerge)
	if !ok || len(got) != 1 || got[0].Pivot != "ws-old" || got[0].Evidence != "same FR" {
		t.Errorf("signal metadata = %v, want raise_merges naming ws-old with its evidence", g.meta)
	}
	if len(g.created) != 1 {
		t.Errorf("created = %v, want only the Ask Critique", g.created)
	}
	if !slices.Equal(g.deps[0], []string{"discovered-from:ws-old"}) {
		t.Errorf("Ask Critique deps = %v, want discovered-from the merged pivot (Spec FR 16)", g.deps[0])
	}
}

// A merge's Ask Critique is the first --critique after its argv position that no
// earlier pivot or merge took, as for a --pivot.
func TestSeedRerouteMergeAskPairsByArgvPosition(t *testing.T) {
	g := stubRerouteSeams(t)
	g.gated = []bd.Bead{{ID: "ws-old", Kind: contract.KindPivot, Status: "open"}}
	g.beads["ws-old"] = g.gated[0]
	items := []RerouteItem{critiqueItem("plain"), pivotItem("p"), critiqueItem("ask p"), critiqueItem("ask merge")}
	if err := SeedReroute(openSignal(), hostID, items, []RaiseMerge{{Pivot: "ws-old", Evidence: "e", After: 2}}); err != nil {
		t.Fatalf("SeedReroute: %v", err)
	}
	want := [][]string{nil, nil, {"discovered-from:ws-d2"}, {"discovered-from:ws-old"}}
	for i, w := range want {
		if !slices.Equal(g.deps[i], w) {
			t.Errorf("draft %d deps = %v, want %v", i, g.deps[i], w)
		}
	}
}

// Whole: every recorded merge needs an Ask Critique discovered-from its pivot.
func TestReconcileRerouteRefusesMergeWithoutAsk(t *testing.T) {
	stubRerouteSeams(t)
	sig := openSignal()
	raw, _ := json.Marshal([]RaiseMerge{{Pivot: "ws-old", Evidence: "e"}})
	sig.Metadata = bd.Meta{"raise_merges": raw}
	drafts := []bd.Bead{{ID: "c", Kind: contract.KindCritique, CausedBy: sig.ID, Parent: hostID}}
	if err := reconcileReroute(sig, drafts); err == nil || !strings.Contains(err.Error(), "ws-old") {
		t.Errorf("err = %v, want a refusal naming the merge", err)
	}
	drafts[0].DiscoveredFrom = []string{"ws-old"}
	if err := reconcileReroute(sig, drafts); err != nil {
		t.Errorf("whole reroute: %v", err)
	}
}

// Spec FR 17–19, 24, 27, 29, 23 and the Ask rule: every refusal writes nothing.
func TestSeedRerouteRefusals(t *testing.T) {
	ok := []RerouteItem{pivotItem("p"), critiqueItem("c")}
	cases := map[string]struct {
		setup func(g *rerouteGraph, sig *bd.Bead)
		items []RerouteItem
		merge []RaiseMerge
		want  string
	}{
		"signal closed": {func(_ *rerouteGraph, s *bd.Bead) { s.Status = "closed" }, ok, nil, "not open"},
		"initiative member": {func(g *rerouteGraph, s *bd.Bead) {
			s.Parent = "ws-ini"
			g.beads["ws-ini"] = bd.Bead{ID: "ws-ini", Kind: contract.KindInitiative, Status: "open"}
		}, ok, nil, "Initiative"},
		"jira":             {func(_ *rerouteGraph, s *bd.Bead) { s.ExternalRef = "jira-PROJ-1" }, ok, nil, "Jira"},
		"chart successor":  {func(g *rerouteGraph, _ *bd.Bead) { g.succ[contract.KindChart] = "c" }, ok, nil, "chart"},
		"spec successor":   {func(g *rerouteGraph, _ *bd.Bead) { g.succ[contract.KindSpec] = "s" }, ok, nil, "spec"},
		"initiative succ.": {func(g *rerouteGraph, _ *bd.Bead) { g.succ[contract.KindInitiative] = "i" }, ok, nil, "initiative"},
		"existing drafts": {func(g *rerouteGraph, _ *bd.Bead) {
			g.drafts = []bd.Bead{{ID: "ws-x", Kind: contract.KindPivot, CausedBy: "ws-sig"}}
		}, ok, nil, "drop and re-triage to change the reroute"},
		"host missing": {func(g *rerouteGraph, _ *bd.Bead) { delete(g.beads, hostID) }, ok, nil, hostID},
		"host closed":  {func(g *rerouteGraph, _ *bd.Bead) { b := g.beads[hostID]; b.Status = "closed"; g.beads[hostID] = b }, ok, nil, "not an open"},
		"host not cp": {func(g *rerouteGraph, _ *bd.Bead) {
			b := g.beads[hostID]
			b.Kind = contract.KindChart
			g.beads[hostID] = b
		}, ok, nil, "not an open"},
		"no spec": {func(g *rerouteGraph, _ *bd.Bead) { g.hasSpec = false }, ok, nil, "no Spec"},
		"open landing": {func(g *rerouteGraph, _ *bd.Bead) {
			g.kids["landing"] = []bd.Bead{{ID: "ws-l", Kind: contract.KindLanding, Status: "open"}}
		}, ok, nil, "landing"},
		"trailer on main":  {func(g *rerouteGraph, _ *bd.Bead) { g.landed = true }, ok, nil, "landed"},
		"pivot w/o ask":    {nil, []RerouteItem{pivotItem("p")}, nil, "Ask Critique"},
		"ask before pivot": {nil, []RerouteItem{critiqueItem("c"), pivotItem("p")}, nil, "Ask Critique"},
		"empty body":       {nil, []RerouteItem{pivotItem("  "), critiqueItem("c")}, nil, "empty"},
		"merge draft": {func(g *rerouteGraph, _ *bd.Bead) {
			g.beads["ws-old"] = bd.Bead{ID: "ws-old", Kind: contract.KindPivot, Status: "deferred"}
			g.gated = []bd.Bead{g.beads["ws-old"]}
		}, []RerouteItem{critiqueItem("c")}, []RaiseMerge{{Pivot: "ws-old", Evidence: "e"}}, "ws-old"},
		"merge not gating": {func(g *rerouteGraph, _ *bd.Bead) {
			g.beads["ws-old"] = bd.Bead{ID: "ws-old", Kind: contract.KindPivot, Status: "open"}
		}, []RerouteItem{critiqueItem("c")}, []RaiseMerge{{Pivot: "ws-old", Evidence: "e"}}, "ws-old"},
		"merge not pivot": {func(g *rerouteGraph, _ *bd.Bead) {
			g.beads["ws-old"] = bd.Bead{ID: "ws-old", Kind: contract.KindCritique, Status: "open"}
			g.gated = []bd.Bead{g.beads["ws-old"]}
		}, []RerouteItem{critiqueItem("c")}, []RaiseMerge{{Pivot: "ws-old", Evidence: "e"}}, "ws-old"},
		"merge w/o ask": {func(g *rerouteGraph, _ *bd.Bead) {
			g.beads["ws-old"] = bd.Bead{ID: "ws-old", Kind: contract.KindPivot, Status: "open"}
			g.gated = []bd.Bead{g.beads["ws-old"]}
		}, []RerouteItem{critiqueItem("c")}, []RaiseMerge{{Pivot: "ws-old", Evidence: "e", After: 1}}, "Ask Critique"},
		"nothing asked": {nil, nil, nil, "at least one --pivot or --critique"},
		"merge only": {func(g *rerouteGraph, _ *bd.Bead) {
			g.beads["ws-old"] = bd.Bead{ID: "ws-old", Kind: contract.KindPivot, Status: "open"}
			g.gated = []bd.Bead{g.beads["ws-old"]}
		}, nil, []RaiseMerge{{Pivot: "ws-old", Evidence: "e"}}, "needs a --critique carrying the Ask"},
	}
	for name, tc := range cases {
		g := stubRerouteSeams(t)
		sig := openSignal()
		if tc.setup != nil {
			tc.setup(g, &sig)
		}
		preexisting := len(g.drafts)
		err := SeedReroute(sig, hostID, tc.items, tc.merge)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.want)) {
			t.Errorf("%s: err = %v, want one mentioning %q", name, err, tc.want)
		}
		if len(g.drafts) != preexisting {
			t.Errorf("%s: drafts grew", name)
		}
		g.nothingWritten(t, name)
	}
}

// Spec FR 18: the landing refusal tells the human to triage again after the land.
func TestSeedRerouteLandingMessage(t *testing.T) {
	g := stubRerouteSeams(t)
	g.kids["landing"] = []bd.Bead{{ID: "ws-l", Kind: contract.KindLanding, Status: "in_progress"}}
	err := SeedReroute(openSignal(), hostID, []RerouteItem{critiqueItem("c")}, nil)
	if err == nil || !strings.Contains(err.Error(), "is landing") || !strings.Contains(err.Error(), "triage") {
		t.Errorf("err = %v, want the host-is-landing message", err)
	}
}

// Spec FR 22, 31: open pivots on other FRs and Require Initiatives on an unparented
// Signal do not refuse.
func TestSeedRerouteIgnoresOtherPivotsAndRequireInitiatives(t *testing.T) {
	g := stubRerouteSeams(t)
	g.gated = []bd.Bead{{ID: "ws-other", Kind: contract.KindPivot, Status: "open"}}
	if err := SeedReroute(openSignal(), hostID, []RerouteItem{critiqueItem("c")}, nil); err != nil {
		t.Errorf("SeedReroute: %v", err)
	}
	if len(g.created) != 1 {
		t.Errorf("created %d, want the one Critique", len(g.created))
	}
}

// Spec FR 20, 21: an MR warning with the URL when the host has an open MR Watch and
// no open Landing; a note naming an open Surgery. Neither refuses.
func TestSeedRerouteWarnsOfMRAndNotesSurgery(t *testing.T) {
	g := stubRerouteSeams(t)
	g.kids["mr-watch"] = []bd.Bead{{ID: "ws-w", Kind: contract.KindMRWatch, Status: "open"}}
	g.kids["surgery"] = []bd.Bead{{ID: "ws-sg", Kind: contract.KindSurgery, Status: "open", Title: "fix by hand"}, {ID: "ws-old-sg", Kind: contract.KindSurgery, Status: "closed"}}
	if err := SeedReroute(openSignal(), hostID, []RerouteItem{critiqueItem("c")}, nil); err != nil {
		t.Fatalf("SeedReroute: %v", err)
	}
	all := strings.Join(g.notes, "\n")
	if !strings.Contains(all, "https://gl/mr/7") || !strings.Contains(all, "ws-sg") || strings.Contains(all, "ws-old-sg") {
		t.Errorf("notes = %q, want the MR URL and only the open Surgery", all)
	}
	g2 := stubRerouteSeams(t)
	if err := SeedReroute(openSignal(), hostID, []RerouteItem{critiqueItem("c")}, nil); err != nil || len(g2.notes) != 0 {
		t.Errorf("err=%v notes=%v, want a quiet reroute with neither MR nor Surgery", err, g2.notes)
	}
}

// Spec decision 3, FR 28: a crash mid-reconcile leaves some Drafts; a flagless
// reconcile takes the reroute leg and writes no second set.
// A flagged re-run with no --merge-into replaces leftover merges (clears the key), and
// a Chart or fast-lane reconcile clears them too: no merges survive without Drafts.
func TestLeftoverRerouteMergesAreCleared(t *testing.T) {
	g := stubRerouteSeams(t)
	if err := SeedReroute(openSignal(), hostID, []RerouteItem{critiqueItem("c")}, nil); err != nil {
		t.Fatal(err)
	}
	if want := []string{"ws-sig/" + RaiseMergesMeta}; !slices.Equal(g.cleared, want) {
		t.Errorf("reroute re-run cleared %v, want %v", g.cleared, want)
	}

	g = stubRerouteSeams(t)
	sig := openSignal()
	sig.Metadata = bd.Meta{RaiseMergesMeta: json.RawMessage(`[{"pivot":"ws-old"}]`)}
	if err := reconcileSignal(sig, ""); err != nil {
		t.Fatal(err)
	}
	if want := []string{"ws-sig/" + RaiseMergesMeta}; !slices.Equal(g.cleared, want) {
		t.Errorf("chart leg cleared %v, want %v", g.cleared, want)
	}
}

func TestRerouteRerunAfterCrashConverges(t *testing.T) {
	g := stubRerouteSeams(t)
	g.failAfter = 1
	items := []RerouteItem{pivotItem("p"), critiqueItem("ask")}
	if err := SeedReroute(openSignal(), hostID, items, nil); err == nil {
		t.Fatal("want the crash error")
	}
	// pivot exists, its Ask Critique does not: the flagless run cannot invent a body.
	n := len(g.created)
	err := reconcileSignal(openSignal(), "")
	if err == nil || !strings.Contains(err.Error(), "Ask Critique") {
		t.Errorf("flagless re-run over a half-filed pivot = %v, want a drop-and-re-triage error", err)
	}
	if len(g.created) != n || g.spawns != 0 {
		t.Errorf("created %d→%d spawns=%d, a flagless run must write no Draft and no Chart", n, len(g.created), g.spawns)
	}

	// A complete set converges: the flagless run takes the reroute leg, not the Chart leg.
	g2 := stubRerouteSeams(t)
	if err := SeedReroute(openSignal(), hostID, items, nil); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := reconcileSignal(openSignal(), ""); err != nil {
			t.Fatalf("flagless reconcile: %v", err)
		}
	}
	if len(g2.created) != 2 || g2.spawns != 0 || len(g2.cuts) != 0 {
		t.Errorf("created=%d spawns=%d cuts=%v, want the 2 Drafts and no Chart", len(g2.created), g2.spawns, g2.cuts)
	}
}

// Spec FR 25 and the mix guard: the Chart and fast-lane legs, SeedSpec and
// SeedInitiative refuse beside reroute Drafts, writing nothing.
func TestOtherLegsRefuseBesideRerouteDrafts(t *testing.T) {
	g := stubRerouteSeams(t)
	g.drafts = []bd.Bead{{ID: "ws-p", Kind: contract.KindPivot, Status: "deferred", CausedBy: "ws-sig"},
		{ID: "ws-c", Kind: contract.KindCritique, Status: "deferred", CausedBy: "ws-sig", Parent: hostID, DiscoveredFrom: []string{"ws-p"}}}
	g.succ[contract.KindSpec] = "ws-spec" // a fast-lane leg beside reroute Drafts
	if err := reconcileSignal(openSignal(), ""); err == nil {
		t.Error("fast-lane leg beside reroute Drafts must error")
	}
	delete(g.succ, contract.KindSpec)
	g.succ[contract.KindChart] = "ws-chart"
	if err := reconcileSignal(openSignal(), ""); err == nil {
		t.Error("Chart leg beside reroute Drafts must error")
	}
	delete(g.succ, contract.KindChart)
	if err := SeedSpec(openSignal(), "", "body", ""); err == nil {
		t.Error("SeedSpec beside reroute Drafts must error")
	}
	if err := SeedInitiative(openSignal(), "", "body", ""); err == nil {
		t.Error("SeedInitiative beside reroute Drafts must error")
	}
	if g.spawns != 0 || len(g.cuts) != 0 || g.anchors != 0 || len(g.seq) != 0 {
		t.Errorf("spawns=%d cuts=%v anchors=%d seq=%v, want nothing written", g.spawns, g.cuts, g.anchors, g.seq)
	}
}

// Beside a published reroute (a pivot or Critique caused-by the Signal, no longer a
// Draft) the Chart leg refuses too.
func TestChartLegRefusesBesidePublishedReroute(t *testing.T) {
	for _, k := range []contract.Kind{contract.KindPivot, contract.KindCritique} {
		g := stubRerouteSeams(t)
		g.succ[k] = "ws-succ"
		if err := reconcileSignal(openSignal(), ""); err == nil {
			t.Errorf("Chart leg beside a %s successor must error", k)
		}
		if g.spawns != 0 {
			t.Errorf("spawns=%d, want nothing written", g.spawns)
		}
	}
}

// A crash between Drafts must not lose the merges: they are on the Signal before the
// first Draft exists.
func TestSeedRerouteRecordsMergesBeforeFirstDraft(t *testing.T) {
	g := stubRerouteSeams(t)
	g.gated = []bd.Bead{{ID: "ws-old", Kind: contract.KindPivot, Status: "open"}}
	g.beads["ws-old"] = g.gated[0]
	g.failAfter = 1
	items := []RerouteItem{pivotItem("p"), critiqueItem("ask"), critiqueItem("merge ask")}
	if err := SeedReroute(openSignal(), hostID, items, []RaiseMerge{{Pivot: "ws-old", Evidence: "same FR", After: 2}}); err == nil {
		t.Fatal("want the crash error")
	}
	if _, ok := g.meta["ws-sig/raise_merges"]; !ok {
		t.Errorf("signal metadata = %v, want raise_merges written before the crash", g.meta)
	}
}

// Each Draft is titled from its own body, not the Signal's: a pivot by its FR line
// ("<FR>: <what's wrong>"), a Critique by its first heading.
func TestSeedRerouteTitlesDraftsFromTheirBodies(t *testing.T) {
	g := stubRerouteSeams(t)
	items := []RerouteItem{
		pivotItem("FR: 12 index choice\nEvidence: the index is not on prod"),
		critiqueItem("# most_cited drives from the wrong index\n\nbody"),
		critiqueItem("no heading here\nsecond line"),
	}
	if err := SeedReroute(openSignal(), hostID, items, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"FR: 12 index choice", "most_cited drives from the wrong index", "no heading here"}
	var got []string
	for _, s := range g.created {
		got = append(got, s.Title)
	}
	if !slices.Equal(got, want) {
		t.Errorf("titles = %q, want %q", got, want)
	}
}

// A flagless re-run checks the pivots' recorded host against the Critiques' host, as
// the publish recheck does.
func TestReconcileRerouteRefusesPivotHostDisagreement(t *testing.T) {
	stubRerouteSeams(t)
	sig := openSignal()
	drafts := []bd.Bead{
		{ID: "p", Kind: contract.KindPivot, CausedBy: sig.ID, Metadata: bd.Meta{RaiseCheckpointMeta: json.RawMessage(`"ws-other"`)}},
		{ID: "c", Kind: contract.KindCritique, CausedBy: sig.ID, Parent: hostID, DiscoveredFrom: []string{"p"}},
	}
	err := reconcileReroute(sig, drafts)
	if err == nil || !strings.Contains(err.Error(), "more than one host") {
		t.Errorf("err = %v, want a more-than-one-host refusal", err)
	}
}

// A blocked Landing is as much a Landing as an open one: the host refuses.
func TestCheckHostRefusesBlockedLanding(t *testing.T) {
	reads := NewHostReads(
		func(string) (bd.Bead, error) {
			return bd.Bead{ID: hostID, Kind: contract.KindCheckpoint, Status: "in_progress"}, nil
		},
		func(string) (string, bool, error) { return "ws-spec", true, nil },
		func(string, contract.Kind) ([]bd.Bead, error) { return []bd.Bead{{ID: "ws-l", Status: "blocked"}}, nil },
		func(bd.Bead, string) (bool, error) { return false, nil },
	)
	if _, _, err := CheckHost(hostID, reads); err == nil || !strings.Contains(err.Error(), "landing") {
		t.Errorf("err = %v, want a landing refusal", err)
	}
}

// A pivot Draft naming no host is refused at reconcile as at publish.
func TestRerouteHostRefusesPivotWithoutHost(t *testing.T) {
	_, err := RerouteHost([]bd.Bead{{ID: "p", Kind: contract.KindPivot}}, nil)
	if err == nil || !strings.Contains(err.Error(), "names no host") {
		t.Errorf("err = %v, want a names-no-host refusal", err)
	}
}
