package luffy

import (
	"encoding/json"
	"errors"
	"fmt"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// raiseGraph scripts what the probe reads: the Task, its held rows, and the host's
// children. Every write seam fails the test, since the probe writes nothing.
type raiseGraph struct {
	*rerouteGraph
	up []bd.Bead
}

func stubRaiseSeams(t *testing.T, task bd.Bead) *raiseGraph {
	t.Helper()
	g := &raiseGraph{rerouteGraph: stubRerouteSeams(t)}
	g.beads[task.ID] = task
	swap(t, &heldUp, func(string) ([]bd.Bead, error) { return g.up, nil })
	swap(t, &createDraft, func(string, bd.Successor, ...string) (string, error) {
		t.Error("the probe wrote a Draft")
		return "", errors.New("write")
	})
	swap(t, &setMetadata, func(string, string, any) error { t.Error("the probe wrote metadata"); return nil })
	swap(t, &clearMetadata, func(string, string) error { t.Error("the probe cleared metadata"); return nil })
	swap(t, &carryTargets, func(string, string, string) ([]string, []bd.CarrySkip, error) {
		t.Error("the probe carried")
		return nil, nil, nil
	})
	g.up = []bd.Bead{g.beads[hostID]}
	return g
}

var openTask = bd.Bead{ID: "ws-task", Kind: contract.KindTask, Status: "in_progress", Title: "walk"}

func TestProbeRaiseNamesCheckpointSpecAndBranch(t *testing.T) {
	stubRaiseSeams(t, openTask)
	var out strings.Builder
	if err := ProbeRaise(openTask.ID, &out); err != nil {
		t.Fatalf("ProbeRaise: %v", err)
	}
	for _, want := range []string{hostID, "ws-spec-of-host", "checkpoint/" + hostID} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output %q lacks %q", out.String(), want)
		}
	}
}

func TestProbeRaiseNamesChildRepo(t *testing.T) {
	g := stubRaiseSeams(t, openTask)
	h := g.beads[hostID]
	h.Labels = []string{"context:api"}
	g.beads[hostID], g.up = h, []bd.Bead{h}
	swap(t, &repoOf, func(bd.Bead) git.Repo { return "/ws/api" })
	var out strings.Builder
	if err := ProbeRaise(openTask.ID, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "git -C /ws/api") {
		t.Errorf("output %q does not name the repo for git -C", out.String())
	}
}

func TestProbeRaiseWarnsOfMRAndNotesSurgery(t *testing.T) {
	g := stubRaiseSeams(t, openTask)
	g.kids["mr-watch"] = []bd.Bead{{ID: "ws-w", Kind: contract.KindMRWatch, Status: "open"}}
	g.kids["surgery"] = []bd.Bead{{ID: "ws-sg", Kind: contract.KindSurgery, Status: "open", Title: "by hand"}}
	var out strings.Builder
	if err := ProbeRaise(openTask.ID, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"warning:", "https://gl/mr/7", "note:", "ws-sg"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output %q lacks %q", out.String(), want)
		}
	}
}

// Every refusal writes nothing (the stubs fail the test on a write) and names the
// offshoot-Signal fallback.
func TestProbeRaiseRefusals(t *testing.T) {
	for name, c := range map[string]struct {
		task  bd.Bead
		setup func(*raiseGraph)
		want  string
	}{
		"not a task": {bd.Bead{ID: "ws-task", Kind: contract.KindSignal, Status: "open"}, func(*raiseGraph) {}, "not a task"},
		"closed":     {bd.Bead{ID: "ws-task", Kind: contract.KindTask, Status: "closed"}, func(*raiseGraph) {}, "not open"},
		"none":       {openTask, func(g *raiseGraph) { g.up = nil }, "no open, unlanded Checkpoint"},
		"several": {openTask, func(g *raiseGraph) {
			g.beads["ws-cp2"] = bd.Bead{ID: "ws-cp2", Kind: contract.KindCheckpoint, Status: "open"}
			g.up = append(g.up, g.beads["ws-cp2"])
		}, "ws-cp, ws-cp2"},
		"no spec": {openTask, func(g *raiseGraph) { g.hasSpec = false }, "no Spec"},
		"landing": {openTask, func(g *raiseGraph) { g.kids["landing"] = []bd.Bead{{ID: "ws-l", Status: "open"}} }, "landing"},
		"landed":  {openTask, func(g *raiseGraph) { g.landed = true }, "no open, unlanded Checkpoint"},
	} {
		g := stubRaiseSeams(t, c.task)
		c.setup(g)
		var out strings.Builder
		err := ProbeRaise(c.task.ID, &out)
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "offshoot Signal") {
			t.Errorf("%s: err = %v, want %q and the offshoot Signal fallback", name, err, c.want)
		}
		if out.Len() != 0 {
			t.Errorf("%s: printed %q on a refusal", name, out.String())
		}
	}
}

// stubRaiseWriteSeams is stubRaiseSeams with the write seams recording, as the reroute's
// stubs do: Drafts carry no caused-by edge, only what the verb gave.
func stubRaiseWriteSeams(t *testing.T) *raiseGraph {
	t.Helper()
	g := stubRaiseSeams(t, openTask)
	swap(t, &createRaiseDraft, func(s bd.Successor, deps ...string) (string, error) {
		id := fmt.Sprintf("ws-d%d", len(g.created)+1)
		g.created = append(g.created, s)
		g.deps = append(g.deps, deps)
		var meta bd.Meta
		if s.Metadata != "" {
			_ = json.Unmarshal([]byte(s.Metadata), &meta)
		}
		g.drafts = append(g.drafts, bd.Bead{ID: id, Kind: s.Kind, Status: "deferred", Title: s.Title, Parent: s.Parent, DiscoveredFrom: discoveredIDs(deps), Metadata: meta})
		return id, nil
	})
	swap(t, &setMetadata, func(id, key string, v any) error { g.meta[id+"/"+key] = v; return nil })
	swap(t, &clearMetadata, func(id, key string) error { g.cleared = append(g.cleared, id+"/"+key); return nil })
	return g
}

func TestRaiseFromTaskLoneCritique(t *testing.T) {
	g := stubRaiseWriteSeams(t)
	if err := RaiseFromTask(openTask.ID, RaiseRequest{Critiques: []string{"# fix it\nbody"}}); err != nil {
		t.Fatalf("RaiseFromTask: %v", err)
	}
	if len(g.created) != 1 {
		t.Fatalf("created %d Drafts, want 1", len(g.created))
	}
	c := g.created[0]
	if c.Kind != contract.KindCritique || c.DraftedBy != openTask.ID || c.Parent != hostID || c.Title != "fix it" {
		t.Errorf("Critique = %+v, want a Critique drafted-by the Task, parented to the host", c)
	}
	if !slices.Equal(g.deps[0], []string{"discovered-from:ws-task"}) {
		t.Errorf("deps = %v, want only discovered-from the Task", g.deps[0])
	}
	if !slices.Equal(g.cleared, []string{"ws-task/raise_merges"}) {
		t.Errorf("cleared = %v, want leftover merges cleared", g.cleared)
	}
}

func TestRaiseFromTaskPivotWithAskCritique(t *testing.T) {
	evs := eventlogtest.Capture(t)
	g := stubRaiseWriteSeams(t)
	if err := RaiseFromTask(openTask.ID, RaiseRequest{Pivots: []string{"FR: 3\nwrong"}, Critiques: []string{"ask"}}); err != nil {
		t.Fatalf("RaiseFromTask: %v", err)
	}
	p, c := g.created[0], g.created[1]
	if p.Kind != contract.KindPivot || p.DraftedBy != "ws-task" || p.Metadata != `{"raise_checkpoint":"ws-cp"}` || p.Parent != "" {
		t.Errorf("pivot = %+v", p)
	}
	if !slices.Equal(g.deps[0], []string{"discovered-from:ws-task"}) {
		t.Errorf("pivot deps = %v", g.deps[0])
	}
	if c.Kind != contract.KindCritique || c.Parent != hostID || !slices.Equal(g.deps[1], []string{"discovered-from:ws-task", "discovered-from:ws-d1"}) {
		t.Errorf("Ask Critique = %+v deps %v, want parented and discovered-from the Task and the pivot", c, g.deps[1])
	}
	// the publish finds the raise by these Drafts alone
	pivots, critiques := RaiseDrafts(openTask, g.drafts)
	if hosts, err := RaiseHosts(pivots, critiques); err != nil || !slices.Equal(hosts, []string{hostID}) {
		t.Errorf("RaiseHosts = %v, %v, want the host", hosts, err)
	}
	var rec []eventlogtest.Event
	for _, e := range *evs {
		if e.Name == "task.raise" {
			rec = append(rec, e)
		}
	}
	if len(rec) != 1 {
		t.Fatalf("events = %+v, want one task.raise", *evs)
	}
	a := rec[0].Attrs
	if a["checkpoint"] != hostID || fmt.Sprint(a["pivots"]) != "[ws-d1]" || fmt.Sprint(a["critiques"]) != "[ws-d2]" || fmt.Sprint(a["merges"]) != "[]" {
		t.Errorf("attrs = %+v", a)
	}
}

func TestRaiseFromTaskMergeRecordsMergeAndAsk(t *testing.T) {
	evs := eventlogtest.Capture(t)
	g := stubRaiseWriteSeams(t)
	old := bd.Bead{ID: "ws-old", Kind: contract.KindPivot, Status: "open"}
	g.beads[old.ID], g.gated = old, []bd.Bead{old}
	m := RaiseMerge{Pivot: "ws-old", Evidence: "same FR"}
	if err := RaiseFromTask(openTask.ID, RaiseRequest{Critiques: []string{"ask"}, Merges: []RaiseMerge{m}}); err != nil {
		t.Fatalf("RaiseFromTask: %v", err)
	}
	if len(g.created) != 1 || !slices.Equal(g.deps[0], []string{"discovered-from:ws-task", "discovered-from:ws-old"}) {
		t.Errorf("created %d, deps %v, want one Ask Critique discovered-from the merge target", len(g.created), g.deps)
	}
	if got, _ := g.meta["ws-task/raise_merges"].([]RaiseMerge); len(got) != 1 || got[0] != m {
		t.Errorf("raise_merges = %v, want the merge recorded", g.meta)
	}
	for _, e := range *evs {
		if e.Name == "task.raise" && fmt.Sprint(e.Attrs["merges"]) == "[ws-old]" {
			return
		}
	}
	t.Errorf("events = %+v, want task.raise naming the merge", *evs)
}

func TestRaiseFromTaskRefusalsWriteNothing(t *testing.T) {
	old := bd.Bead{ID: "ws-old", Kind: contract.KindPivot, Status: "open"}
	draftPivot := bd.Bead{ID: "ws-dp", Kind: contract.KindPivot, Status: "deferred", Labels: []string{"drafted-by:ws-x"}}
	for name, c := range map[string]struct {
		req   RaiseRequest
		setup func(*raiseGraph)
		want  string
	}{
		"no flags":       {RaiseRequest{}, nil, "nothing to raise"},
		"pivot only":     {RaiseRequest{Pivots: []string{"p"}}, nil, "needs a --critique"},
		"merge only":     {RaiseRequest{Merges: []RaiseMerge{{Pivot: "ws-old", Evidence: "e"}}}, nil, "needs a --critique"},
		"two pivots":     {RaiseRequest{Pivots: []string{"a", "b"}, Critiques: []string{"c"}}, nil, "not several"},
		"pivot + merge":  {RaiseRequest{Pivots: []string{"a"}, Critiques: []string{"c"}, Merges: []RaiseMerge{{Pivot: "ws-old", Evidence: "e"}}}, nil, "not several"},
		"two critiques":  {RaiseRequest{Critiques: []string{"a", "b"}}, nil, "one --critique"},
		"empty body":     {RaiseRequest{Critiques: []string{" \n"}}, nil, "empty"},
		"merge a draft":  {RaiseRequest{Critiques: []string{"c"}, Merges: []RaiseMerge{{Pivot: "ws-dp", Evidence: "e"}}}, func(g *raiseGraph) { g.beads["ws-dp"] = draftPivot; g.gated = []bd.Bead{draftPivot} }, "not an open, published pivot"},
		"merge ungated":  {RaiseRequest{Critiques: []string{"c"}, Merges: []RaiseMerge{{Pivot: "ws-old", Evidence: "e"}}}, func(g *raiseGraph) { g.beads["ws-old"] = old }, "not an open, published pivot"},
		"existing raise": {RaiseRequest{Critiques: []string{"c"}}, func(g *raiseGraph) { g.drafts = []bd.Bead{{ID: "ws-dc", Kind: contract.KindCritique, Parent: hostID}} }, "drop the session"},
		"landing":        {RaiseRequest{Critiques: []string{"c"}}, func(g *raiseGraph) { g.kids["landing"] = []bd.Bead{{ID: "ws-l", Status: "open"}} }, "landing"},
		"none held":      {RaiseRequest{Critiques: []string{"c"}}, func(g *raiseGraph) { g.up = nil }, "offshoot Signal"},
	} {
		g := stubRaiseWriteSeams(t)
		if c.setup != nil {
			c.setup(g)
		}
		err := RaiseFromTask(openTask.ID, c.req)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
		if len(g.created) != 0 || len(g.meta) != 0 || len(g.cleared) != 0 {
			t.Errorf("%s: wrote created=%d meta=%v cleared=%v, want nothing", name, len(g.created), g.meta, g.cleared)
		}
	}
}

func TestProbeRaiseReportsExistingDrafts(t *testing.T) {
	g := stubRaiseSeams(t, openTask)
	g.drafts = []bd.Bead{
		{ID: "ws-dp", Kind: contract.KindPivot, Title: "FR: 3", Metadata: bd.Meta{RaiseCheckpointMeta: []byte(`"ws-cp"`)}},
		{ID: "ws-dc", Kind: contract.KindCritique, Title: "ask", Parent: hostID},
	}
	var out strings.Builder
	if err := ProbeRaise(openTask.ID, &out); err != nil {
		t.Fatalf("ProbeRaise: %v", err)
	}
	for _, want := range []string{"ws-dp", "ws-dc", hostID} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output %q lacks %q", out.String(), want)
		}
	}
}
