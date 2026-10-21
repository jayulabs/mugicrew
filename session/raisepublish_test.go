package session

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/luffy"
)

// rr is a faked graph for a reroute's publish: the Signal ws-sig, its Drafts, a host
// ws-cp, and the batch and comments the publish makes.
type rr struct {
	sig      bd.Bead
	drafts   []bd.Bead
	host     bd.Bead
	landings []bd.Bead
	landed   bool
	hasSpec  bool
	present  map[string]bool // "from>to:type" edges bd already holds
	comments map[string][]string

	edges     []bd.Edge
	closing   *bd.Closing
	published bool
	posted    map[string]string
}

func swapSeam[T any](t *testing.T, p *T, v T) {
	t.Helper()
	old := *p
	t.Cleanup(func() { *p = old })
	*p = v
}

func newRR(t *testing.T) *rr {
	t.Helper()
	eventlogtest.Capture(t)
	g := &rr{
		sig:      bd.Bead{ID: "ws-sig", Kind: contract.KindSignal, Status: "open"},
		host:     bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open"},
		hasSpec:  true,
		present:  map[string]bool{},
		comments: map[string][]string{},
		posted:   map[string]string{},
	}
	swapSeam(t, &showBead, func(id string) (bd.Bead, error) {
		if id == "ws-cp" {
			return g.host, nil
		}
		return g.sig, nil
	})
	swapSeam(t, &listDrafts, func(string) ([]bd.Bead, error) { return g.drafts, nil })
	swapSeam(t, &successorOf, func(string, contract.Kind) (string, bool, error) { return "", false, nil })
	swapSeam(t, &specOf, func(string) (string, bool, error) { return "ws-spec", g.hasSpec, nil })
	swapSeam(t, &childrenOfKind, func(string, contract.Kind) ([]bd.Bead, error) { return g.landings, nil })
	swapSeam(t, &landedOnMainline, func(bd.Bead, string) (bool, error) { return g.landed, nil })
	swapSeam(t, &hasEdge, func(from, to, typ string) (bool, error) { return g.present[from+">"+to+":"+typ], nil })
	swapSeam(t, &gateEdges, func(cp, spec string, ids []string) ([]bd.Edge, error) {
		var out []bd.Edge
		for _, id := range ids {
			out = append(out, bd.Edge{From: cp, To: id, Type: "blocks"}, bd.Edge{From: id, To: spec, Type: "related"})
		}
		return out, nil
	})
	swapSeam(t, &publish, func(_ string, edges []bd.Edge, closing *bd.Closing) ([]string, error) {
		g.published, g.edges, g.closing = true, edges, closing
		// As production: the batch opens the Drafts (bd.Drafts lists only deferred), lands
		// its edges, and closes the Signal.
		g.drafts = nil
		for _, e := range edges {
			g.present[e.From+">"+e.To+":"+e.Type] = true
		}
		if closing != nil {
			g.sig.Status = "closed"
		}
		return []string{"d"}, nil
	})
	swapSeam(t, &commentsOf, func(id string) ([]bd.Comment, error) {
		var cs []bd.Comment
		for _, c := range g.comments[id] {
			cs = append(cs, bd.Comment{Text: c})
		}
		return cs, nil
	})
	swapSeam(t, &addComment, func(id, text string) error { g.posted[id] = text; return nil })
	return g
}

func pivotDraft(id string) bd.Bead {
	return bd.Bead{ID: id, Kind: contract.KindPivot, CausedBy: "ws-sig", Metadata: bd.Meta{luffy.RaiseCheckpointMeta: json.RawMessage(`"ws-cp"`)}}
}

func critiqueDraft(id string) bd.Bead {
	return bd.Bead{ID: id, Kind: contract.KindCritique, CausedBy: "ws-sig", Parent: "ws-cp"}
}

func (g *rr) merge(pivot string) {
	raw, _ := json.Marshal([]luffy.RaiseMerge{{Pivot: pivot, Evidence: "same FR"}})
	g.sig.Metadata = bd.Meta{luffy.RaiseMergesMeta: raw}
}

func edgeStrings(es []bd.Edge) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.From+">"+e.To+":"+e.Type)
	}
	return out
}

func TestPublishRerouteHandsGateMergeAndClose(t *testing.T) {
	g := newRR(t)
	g.drafts = []bd.Bead{pivotDraft("ws-p1"), critiqueDraft("ws-c1")}
	g.merge("ws-old")
	if err := PublishDrafts("ws-sig", nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"ws-cp>ws-p1:blocks", "ws-p1>ws-spec:related", "ws-old>ws-sig:caused-by"}
	if got := edgeStrings(g.edges); !slices.Equal(got, want) {
		t.Errorf("edges = %v, want %v", got, want)
	}
	if g.closing == nil || g.closing.ID != "ws-sig" || g.closing.Reason != "rerouted: ws-c1, ws-old, ws-p1" {
		t.Errorf("closing = %+v, want ws-sig rerouted: ws-c1, ws-old, ws-p1", g.closing)
	}
	if !strings.HasPrefix(g.posted["ws-old"], "Rerouted from ws-sig\n") || !strings.Contains(g.posted["ws-old"], "same FR") {
		t.Errorf("posted = %q, want the marker and evidence on ws-old", g.posted["ws-old"])
	}
}

func TestPublishCritiqueOnlyRerouteHandsOnlyTheClose(t *testing.T) {
	g := newRR(t)
	g.drafts = []bd.Bead{critiqueDraft("ws-c1")}
	if err := PublishDrafts("ws-sig", nil); err != nil {
		t.Fatal(err)
	}
	if len(g.edges) != 0 {
		t.Errorf("edges = %v, want none", g.edges)
	}
	if g.closing == nil || g.closing.Reason != "rerouted: ws-c1" {
		t.Errorf("closing = %+v, want rerouted: ws-c1", g.closing)
	}
}

func TestPublishRerouteFiltersPresentEdges(t *testing.T) {
	g := newRR(t)
	g.drafts = []bd.Bead{pivotDraft("ws-p1"), critiqueDraft("ws-c1")}
	g.merge("ws-old")
	g.present["ws-old>ws-sig:caused-by"] = true
	// the gate helper is the shared one, which filters by HasEdge itself; fake that here
	swapSeam(t, &gateEdges, func(cp, spec string, ids []string) ([]bd.Edge, error) { return nil, nil })
	if err := PublishDrafts("ws-sig", nil); err != nil {
		t.Fatal(err)
	}
	if len(g.edges) != 0 {
		t.Errorf("edges = %v, want none: all present", g.edges)
	}
}

func TestPublishRerouteRecheckFailuresKeepTheSignalOpen(t *testing.T) {
	for name, break_ := range map[string]func(g *rr){
		"host closed":   func(g *rr) { g.host.Status = "closed" },
		"no Spec":       func(g *rr) { g.hasSpec = false },
		"open Landing":  func(g *rr) { g.landings = []bd.Bead{{ID: "ws-l", Kind: contract.KindLanding, Status: "open"}} },
		"landed":        func(g *rr) { g.landed = true },
		"two hosts":     func(g *rr) { g.drafts[1].Parent = "ws-other" },
		"not a checkpt": func(g *rr) { g.host.Kind = contract.KindTask },
	} {
		t.Run(name, func(t *testing.T) {
			g := newRR(t)
			g.drafts = []bd.Bead{pivotDraft("ws-p1"), critiqueDraft("ws-c1")}
			break_(g)
			if err := PublishDrafts("ws-sig", nil); err != nil {
				// RecordDrafts returns the failure it recorded
				t.Logf("publish failed as wanted: %v", err)
			} else {
				t.Error("publish succeeded, want a failure")
			}
			if g.published {
				t.Error("the batch ran; the Signal must stay open and the Drafts deferred")
			}
		})
	}
}

// Merges with no reroute Drafts are a failed reroute's leftover (Spec decision 4): the
// Signal publishes as a plain Chart close, with no caused-by edge and no comment.
func TestPublishLeftoverMergesHandNoEdgeNorComment(t *testing.T) {
	g := newRR(t)
	g.merge("ws-old")
	swapSeam(t, &successorOf, func(_ string, k contract.Kind) (string, bool, error) {
		return "ws-chart", k == contract.KindChart, nil
	})
	if err := PublishDrafts("ws-sig", nil); err != nil {
		t.Fatal(err)
	}
	if g.closing == nil || g.closing.ID != "ws-sig" || g.closing.Reason != "" {
		t.Errorf("closing = %+v, want the plain close of ws-sig", g.closing)
	}
	if len(g.edges) != 0 || len(g.posted) != 0 {
		t.Errorf("edges = %v posted = %v, want neither from leftover merges", edgeStrings(g.edges), g.posted)
	}
}

// The evidence comment is posted once. A comment lost after the batch opened the Drafts
// is posted again by the re-publish, though that sees no Drafts, and never doubled.
func TestPublishMergeCommentPostedOnceAndReposted(t *testing.T) {
	g := newRR(t)
	g.drafts = []bd.Bead{critiqueDraft("ws-c1")}
	g.merge("ws-old")
	posts := 0
	failNext := true
	swapSeam(t, &addComment, func(id, text string) error {
		if failNext {
			failNext = false
			return errors.New("1213")
		}
		posts++
		g.comments[id] = append(g.comments[id], text)
		return nil
	})
	if err := PublishDrafts("ws-sig", nil); err == nil {
		t.Fatal("first publish: want the comment failure")
	}
	if len(g.drafts) != 0 || g.sig.Status != "closed" {
		t.Fatalf("batch did not land: drafts = %v status = %s", g.drafts, g.sig.Status)
	}
	if err := PublishDrafts("ws-sig", nil); err != nil {
		t.Fatal(err)
	}
	if posts != 1 {
		t.Errorf("posts = %d after the re-publish, want 1", posts)
	}
	if g.closing != nil || len(g.edges) != 0 {
		t.Errorf("re-publish handed closing = %+v edges = %v, want merges only", g.closing, edgeStrings(g.edges))
	}
	if err := PublishDrafts("ws-sig", nil); err != nil {
		t.Fatal(err)
	}
	if posts != 1 {
		t.Errorf("posts = %d after a third publish, want still 1", posts)
	}
	// A longer Signal id carrying this one as a prefix is not this Signal's marker.
	g.comments["ws-old"] = []string{"Rerouted from ws-sig2\n\nother"}
	if err := PublishDrafts("ws-sig", nil); err != nil {
		t.Fatal(err)
	}
	if posts != 2 {
		t.Errorf("posts = %d, want the lost marker re-posted", posts)
	}
}

func TestPublishReroutedSignalAlreadyClosedClosesNothing(t *testing.T) {
	g := newRR(t)
	g.sig.Status = "closed"
	g.drafts = []bd.Bead{critiqueDraft("ws-c1")}
	if err := PublishDrafts("ws-sig", nil); err != nil {
		t.Fatal(err)
	}
	if g.closing != nil {
		t.Errorf("closing = %+v, want none for an already-closed Signal", g.closing)
	}
}

// A drop deletes the Drafts and clears the Signal's raise_merges, leaving it open.
func TestDropDraftsClearsRerouteMerges(t *testing.T) {
	eventlogtest.Capture(t)
	var cleared []string
	swapSeam(t, &deleteDrafts, func(string) ([]string, []bd.BranchOutcome, error) { return []string{"d1"}, nil, nil })
	swapSeam(t, &clearMetadata, func(id, key string) error { cleared = append(cleared, id+"/"+key); return nil })
	sig := bd.Bead{ID: "ws-sig", Kind: contract.KindSignal, Metadata: bd.Meta{luffy.RaiseMergesMeta: json.RawMessage(`[]`)}}
	if err := DropDrafts(sig); err != nil {
		t.Fatal(err)
	}
	if want := []string{"ws-sig/raise_merges"}; !slices.Equal(cleared, want) {
		t.Errorf("cleared = %v, want %v", cleared, want)
	}
	// the passed copy may predate the reconcile: no metadata still clears
	cleared = nil
	if err := DropDrafts(bd.Bead{ID: "ws-sig", Kind: contract.KindSignal}); err != nil || !slices.Equal(cleared, []string{"ws-sig/raise_merges"}) {
		t.Errorf("a stale copy cleared %v (err %v), want raise_merges cleared", cleared, err)
	}
	cleared = nil
	swapSeam(t, &deleteDrafts, func(string) ([]string, []bd.BranchOutcome, error) { return nil, nil, errors.New("locked") })
	if err := DropDrafts(sig); err == nil || len(cleared) != 0 {
		t.Errorf("a failed delete must not clear merges: err %v cleared %v", err, cleared)
	}
}

// taskRaise fakes a Task creator ws-task whose raise Drafts carry no caused-by edge: the
// gate keys on the pivot's raise_checkpoint metadata alone (ADR 0326 §4).
func taskRaise(t *testing.T) *rr {
	g := newRR(t)
	g.sig = bd.Bead{ID: "ws-task", Kind: contract.KindTask, Status: "in_progress"}
	return g
}

func taskPivot(id string) bd.Bead {
	p := pivotDraft(id)
	p.CausedBy = ""
	return p
}

func taskCritique(id string) bd.Bead {
	c := critiqueDraft(id)
	c.CausedBy = ""
	return c
}

func TestPublishTaskRaiseHandsGateAndNoClose(t *testing.T) {
	g := taskRaise(t)
	g.drafts = []bd.Bead{taskPivot("ws-p1"), taskCritique("ws-c1")}
	if err := PublishDrafts("ws-task", nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"ws-cp>ws-p1:blocks", "ws-p1>ws-spec:related"}
	if got := edgeStrings(g.edges); !slices.Equal(got, want) {
		t.Errorf("edges = %v, want %v", got, want)
	}
	if g.closing != nil || g.sig.Status != "in_progress" {
		t.Errorf("closing = %+v status = %s, want the Task left in_progress", g.closing, g.sig.Status)
	}
}

func TestPublishTaskCritiqueOnlyRaiseHandsNoGate(t *testing.T) {
	g := taskRaise(t)
	g.drafts = []bd.Bead{taskCritique("ws-c1")}
	if err := PublishDrafts("ws-task", nil); err != nil {
		t.Fatal(err)
	}
	if !g.published || len(g.edges) != 0 || g.closing != nil {
		t.Errorf("published = %v edges = %v closing = %+v, want a bare open", g.published, edgeStrings(g.edges), g.closing)
	}
}

func TestPublishTaskMergeIsDiscoveredFromAndCommentedOnce(t *testing.T) {
	g := taskRaise(t)
	g.drafts = []bd.Bead{taskCritique("ws-c1")}
	g.merge("ws-old")
	posts := 0
	swapSeam(t, &addComment, func(id, text string) error {
		posts++
		g.comments[id] = append(g.comments[id], text)
		g.posted[id] = text
		return nil
	})
	if err := PublishDrafts("ws-task", nil); err != nil {
		t.Fatal(err)
	}
	if want := []string{"ws-old>ws-task:discovered-from"}; !slices.Equal(edgeStrings(g.edges), want) {
		t.Errorf("edges = %v, want %v", edgeStrings(g.edges), want)
	}
	if g.closing != nil {
		t.Errorf("closing = %+v, want none", g.closing)
	}
	if !strings.HasPrefix(g.posted["ws-old"], "Raised by Task ws-task\n") {
		t.Errorf("comment = %q, want the Raised by Task marker", g.posted["ws-old"])
	}
	if err := PublishDrafts("ws-task", nil); err != nil {
		t.Fatal(err)
	}
	if posts != 1 {
		t.Errorf("posts = %d across two publishes, want 1", posts)
	}
}

func TestPublishTaskRaiseRecheckFailureKeepsDraftsDeferred(t *testing.T) {
	g := taskRaise(t)
	g.drafts = []bd.Bead{taskPivot("ws-p1"), taskCritique("ws-c1")}
	g.landings = []bd.Bead{{ID: "ws-l", Kind: contract.KindLanding, Status: "open"}}
	if err := PublishDrafts("ws-task", nil); err == nil {
		t.Error("publish succeeded, want the Landing failure")
	}
	if g.published || g.sig.Status != "in_progress" {
		t.Errorf("published = %v status = %s, want Drafts deferred and the Task in_progress", g.published, g.sig.Status)
	}
}

func TestDropDraftsClearsTaskRaiseMerges(t *testing.T) {
	var cleared []string
	swapSeam(t, &deleteDrafts, func(string) ([]string, []bd.BranchOutcome, error) { return nil, nil, nil })
	swapSeam(t, &clearMetadata, func(id, key string) error { cleared = append(cleared, id+"/"+key); return nil })
	eventlogtest.Capture(t)
	if err := DropDrafts(bd.Bead{ID: "ws-task", Kind: contract.KindTask}); err != nil || !slices.Equal(cleared, []string{"ws-task/raise_merges"}) {
		t.Errorf("cleared %v (err %v), want raise_merges cleared", cleared, err)
	}
}
