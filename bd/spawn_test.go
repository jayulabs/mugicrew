package bd_test

import (
	"slices"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/bd/bdtest"
	"jayulabs.org/mugicrew/contract"
)

// fakeT is a testing.TB that records that Errorf was called instead of failing the test.
type fakeT struct {
	testing.TB
	failed bool
}

func (r *fakeT) Errorf(string, ...any) { r.failed = true }

// edgeTypes is id's outgoing edges as "type:to", sorted.
func edgeTypes(t *testing.T, f *bdtest.Fake, id string) []string {
	t.Helper()
	es, err := f.Edges(id, bd.Down, "")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Type+":"+e.ID)
	}
	slices.Sort(out)
	return out
}

func TestSpawnSuccessorIfAbsentCreatesThroughTheCore(t *testing.T) {
	f := bdtest.New(t,
		bdtest.Claimed(bd.Bead{ID: "t-pred", Kind: contract.KindSpec, Status: "open"}),
		bd.Bead{ID: "t-init", Kind: contract.KindInitiative, Status: "open", Container: true})
	s := bd.Successor{Kind: contract.KindCheckpoint, Container: true, Title: "Spec: cp", Description: "d", Parent: "t-init",
		DraftedBy: "t-pred", Context: "mugicrew", Serializer: bd.UnderClaim("t-pred")}
	id, made, err := bd.SpawnSuccessorIfAbsent("t-pred", s)
	if err != nil || !made {
		t.Fatalf("SpawnSuccessorIfAbsent = %q, %v, %v", id, made, err)
	}
	b := f.Bead(id)
	if b.Status != "deferred" || b.Title != "cp" || b.Parent != "t-init" || b.CausedBy != "t-pred" || !b.Container ||
		!b.HasLabel("kind:checkpoint") || !b.HasLabel("context:mugicrew") || !b.HasLabel("drafted-by:t-pred") {
		t.Errorf("spawned = %+v", b)
	}
	if again, made, err := bd.SpawnSuccessorIfAbsent("t-pred", s); err != nil || made || again != id {
		t.Errorf("re-run = %q, %v, %v; want the spawned %s, nothing created", again, made, err, id)
	}
}

func TestSpawnSuccessorIfAbsentRefusedWithoutItsClaim(t *testing.T) {
	ft := &fakeT{TB: t}
	bdtest.New(ft, bd.Bead{ID: "t-pred", Kind: contract.KindSpec, Status: "open"}) // not claimed
	_, made, err := bd.SpawnSuccessorIfAbsent("t-pred", bd.Successor{Kind: contract.KindCheckpoint, Title: "cp", Serializer: bd.UnderClaim("t-pred")})
	if err == nil || made || !ft.failed {
		t.Errorf("unclaimed spawn = %v, made %v, test failed %v; want a refusal", err, made, ft.failed)
	}
}

func TestSpawnDiscoveredIfAbsentLaysBothEdges(t *testing.T) {
	f := bdtest.New(t,
		bd.Bead{ID: "t-chart", Kind: contract.KindChart, Status: "open"},
		bd.Bead{ID: "t-pivot", Kind: contract.KindPivot, Status: "open"})
	s := bd.Successor{Kind: contract.KindSpec, Title: "s", Description: "d", DraftedBy: "t-pivot"}
	id, made, err := bd.SpawnDiscoveredIfAbsent("t-pivot", "t-chart", s)
	if err != nil || !made {
		t.Fatalf("SpawnDiscoveredIfAbsent = %q, %v, %v", id, made, err)
	}
	if got, want := edgeTypes(t, f, id), []string{"caused-by:t-chart", "discovered-from:t-pivot"}; !slices.Equal(got, want) {
		t.Errorf("edges = %v, want %v", got, want)
	}
	if b := f.Bead(id); b.Status != "deferred" || !b.HasLabel("kind:spec") {
		t.Errorf("spawned = %+v", b)
	}
	if again, made, err := bd.SpawnDiscoveredIfAbsent("t-pivot", "t-chart", s); err != nil || made || again != id {
		t.Errorf("re-run = %q, %v, %v; want %s, nothing created", again, made, err, id)
	}
}

func TestSpawnSurveyAndChildIfAbsentCreateUnderTheirClaims(t *testing.T) {
	f := bdtest.New(t,
		bdtest.Claimed(bd.Bead{ID: "t-sig", Kind: contract.KindSignal, Status: "open"}),
		bd.Bead{ID: "t-chart", Kind: contract.KindChart, Status: "deferred", Container: true},
		bdtest.Claimed(bd.Bead{ID: "t-cp", Kind: contract.KindCheckpoint, Status: "open", Container: true}))
	id, made, err := bd.SpawnSurveyIfAbsent("t-chart", "Survey it", "t-sig")
	if err != nil || !made {
		t.Fatalf("SpawnSurveyIfAbsent = %q, %v, %v", id, made, err)
	}
	if b := f.Bead(id); b.Parent != "t-chart" || b.Status != "deferred" || !b.HasLabel("kind:survey") || !b.HasLabel("drafted-by:t-sig") {
		t.Errorf("survey = %+v", b)
	}
	if again, made, err := bd.SpawnSurveyIfAbsent("t-chart", "Survey it", "t-sig"); err != nil || made || again != id {
		t.Errorf("survey re-run = %q, %v, %v", again, made, err)
	}
	cid, made, err := bd.SpawnChildIfAbsent("t-cp", bd.Successor{Kind: contract.KindApproval, Title: "a", CausedBy: "t-sig", DiscoveredFrom: "t-chart", Serializer: bd.UnderClaim("t-cp")})
	if err != nil || !made {
		t.Fatalf("SpawnChildIfAbsent = %q, %v, %v", cid, made, err)
	}
	if got, want := edgeTypes(t, f, cid), []string{"caused-by:t-sig", "discovered-from:t-chart", "parent-child:t-cp"}; !slices.Equal(got, want) {
		t.Errorf("child edges = %v, want %v", got, want)
	}
}

func TestSpawnSurveyIfAbsentRefusedWithoutTheDraftersClaim(t *testing.T) {
	ft := &fakeT{TB: t}
	bdtest.New(ft, bd.Bead{ID: "t-sig", Kind: contract.KindSignal, Status: "open"}, bd.Bead{ID: "t-chart", Kind: contract.KindChart, Status: "deferred", Container: true})
	if _, made, err := bd.SpawnSurveyIfAbsent("t-chart", "Survey it", "t-sig"); err == nil || made || !ft.failed {
		t.Errorf("unclaimed survey spawn = %v, made %v, failed %v; want a refusal", err, made, ft.failed)
	}
}

func TestSpawnStampsThePredecessorsOwnPriority(t *testing.T) {
	spawners := map[string]func(pred string, s bd.Successor) (string, bool, error){
		"successor": bd.SpawnSuccessorIfAbsent,
		"discovered": func(pred string, s bd.Successor) (string, bool, error) {
			return bd.SpawnDiscoveredIfAbsent("t-origin", pred, s)
		},
	}
	for name, spawn := range spawners {
		for _, p := range []int{0, 4} {
			f := bdtest.New(t,
				bd.Bead{ID: "t-pred", Kind: contract.KindChart, Status: "open", Priority: p},
				bd.Bead{ID: "t-origin", Kind: contract.KindPivot, Status: "open"})
			id, made, err := spawn("t-pred", bd.Successor{Kind: contract.KindSpec, Title: "s"})
			if err != nil || !made {
				t.Fatalf("%s P%d: spawn = %q, %v, %v", name, p, id, made, err)
			}
			if got := f.Bead(id).Priority; got != p {
				t.Errorf("%s: successor of P%d born at P%d", name, p, got)
			}
		}
	}
}

func TestSpawnStampsOwnNotEffectivePriority(t *testing.T) {
	f := bdtest.New(t,
		bd.Bead{ID: "t-init", Kind: contract.KindInitiative, Status: "open", Container: true, Priority: 0},
		bd.Bead{ID: "t-pred", Kind: contract.KindChart, Status: "open", Parent: "t-init", Priority: 2})
	id, _, err := bd.SpawnSuccessorIfAbsent("t-pred", bd.Successor{Kind: contract.KindSpec, Title: "s", Parent: "t-init"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Bead(id).Priority; got != 2 {
		t.Errorf("successor born at P%d, want its predecessor's own P2", got)
	}
}

func TestSpawnPrioritySourceWinsOverThePredecessor(t *testing.T) {
	f := bdtest.New(t,
		bd.Bead{ID: "t-pred", Kind: contract.KindChart, Status: "open", Priority: 1},
		bd.Bead{ID: "t-cp", Kind: contract.KindCheckpoint, Status: "open", Priority: 4})
	id, _, err := bd.SpawnSuccessorIfAbsent("t-pred", bd.Successor{Kind: contract.KindSpec, Title: "s", PrioritySource: "t-cp"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Bead(id).Priority; got != 4 {
		t.Errorf("successor born at P%d, want the override source's P4", got)
	}
}

func TestSpawnWithoutAPredecessorOrSourceKeepsTheDefault(t *testing.T) {
	f := bdtest.New(t, bd.Bead{ID: "t-origin", Kind: contract.KindPivot, Status: "open", Priority: 0})
	id, made, err := bd.SpawnDiscoveredIfAbsent("t-origin", "", bd.Successor{Kind: contract.KindCritique, Title: "c"})
	if err != nil || !made {
		t.Fatalf("spawn = %q, %v, %v", id, made, err)
	}
	if got := f.Bead(id).Priority; got != 2 {
		t.Errorf("born at P%d, want bd's default P2", got)
	}
}

func TestSpawnLeavesAnExistingSuccessorsPriorityAlone(t *testing.T) {
	f := bdtest.New(t, bd.Bead{ID: "t-pred", Kind: contract.KindChart, Status: "open", Priority: 0})
	s := bd.Successor{Kind: contract.KindSpec, Title: "s"}
	id, _, err := bd.SpawnSuccessorIfAbsent("t-pred", s)
	if err != nil {
		t.Fatal(err)
	}
	f.Mutate(id, func(b *bd.Bead) { b.Priority = 3 })
	if again, made, err := bd.SpawnSuccessorIfAbsent("t-pred", s); err != nil || made || again != id {
		t.Fatalf("re-run = %q, %v, %v", again, made, err)
	}
	if got := f.Bead(id).Priority; got != 3 {
		t.Errorf("edited priority became P%d after a re-run", got)
	}
}

func TestSpawnFailedSourceReadCreatesNothing(t *testing.T) {
	bdtest.New(t, bd.Bead{ID: "t-pred", Kind: contract.KindChart, Status: "open"})
	_, made, err := bd.SpawnSuccessorIfAbsent("t-pred", bd.Successor{Kind: contract.KindSpec, Title: "s", PrioritySource: "t-missing"})
	if err == nil || made {
		t.Fatalf("spawn = %v, made %v; want the failed read to fail it", err, made)
	}
	if _, ok, _ := bd.SuccessorOf("t-pred", contract.KindSpec); ok {
		t.Error("a successor was created despite the failed read")
	}
}
