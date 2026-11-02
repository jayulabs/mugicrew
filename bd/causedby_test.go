package bd_test

import (
	"errors"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/bd/bdtest"
	"jayulabs.org/mugicrew/contract"
)

func causedByGraph(t *testing.T) *bdtest.Fake {
	return bdtest.New(t,
		bd.Bead{ID: "t-chart", Status: "open", Kind: contract.KindChart},
		bd.Bead{ID: "t-spec", Status: "open", Kind: contract.KindSpec, CausedBy: "t-chart"},
		bd.Bead{ID: "t-cp", Status: "open", Kind: contract.KindCheckpoint, CausedBy: "t-spec"},
	)
}

func TestCausedByAndSuccessorOfReadSeededEdge(t *testing.T) {
	causedByGraph(t)
	if id, ok, err := bd.CausedBy("t-spec", contract.KindChart); err != nil || !ok || id != "t-chart" {
		t.Errorf("CausedBy = %q, %v, %v; want t-chart", id, ok, err)
	}
	if _, ok, err := bd.CausedBy("t-spec", contract.KindResearch); err != nil || ok {
		t.Errorf("CausedBy(other kind) = %v, %v; want not found", ok, err)
	}
	if id, ok, err := bd.SuccessorOf("t-spec", contract.KindCheckpoint); err != nil || !ok || id != "t-cp" {
		t.Errorf("SuccessorOf = %q, %v, %v; want t-cp", id, ok, err)
	}
	if id, ok, err := bd.SpecOf("t-cp"); err != nil || !ok || id != "t-spec" {
		t.Errorf("SpecOf = %q, %v, %v; want t-spec", id, ok, err)
	}
}

func TestCausedBySuccessorOfSpecOfReturnEdgesError(t *testing.T) {
	f := causedByGraph(t)
	boom := errors.New("bd down")
	f.FailOn("Edges", "*", boom)
	if _, ok, err := bd.CausedBy("t-spec", contract.KindChart); !errors.Is(err, boom) || ok {
		t.Errorf("CausedBy = %v, %v; want the Edges error", ok, err)
	}
	if _, ok, err := bd.SuccessorOf("t-spec", contract.KindCheckpoint); !errors.Is(err, boom) || ok {
		t.Errorf("SuccessorOf = %v, %v; want the Edges error", ok, err)
	}
	if _, ok, err := bd.SpecOf("t-cp"); !errors.Is(err, boom) || ok {
		t.Errorf("SpecOf = %v, %v; want the Edges error", ok, err)
	}
}
