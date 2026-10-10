package registry

import (
	"testing"

	"jayulabs.org/mugicrew/contract"
)

// Lookup is the total owner-from-kind function (spec §4.2). These cases are the
// ticket's acceptance criteria: Nami's kinds resolve, an unknown kind is a
// loud error. (ADR 0013 dropped task from the defog set; ADR 0115 gives it to Brook.)
func TestLookupResolvesKinds(t *testing.T) {
	cases := []struct {
		kind contract.Kind
		want Entry
	}{
		{contract.KindSignal, Entry{Owner: "nami", Mode: HITL, Container: false}},
		{contract.KindGrilling, Entry{Owner: "nami", Mode: HITL, Container: false}},
		{contract.KindResearch, Entry{Owner: "nami", Mode: AFK, Container: false}},
		{contract.KindChart, Entry{Owner: "nami", Mode: AFK, Container: true}},
		{contract.KindChartConflict, Entry{Owner: "nami", Mode: AFK, Container: false}}, // ADR 0011, ADR 0080
		{contract.KindSpec, Entry{Owner: "zoro", Mode: AFK, Container: false}},          // FR1: Zoro's Spec leaf
		{contract.KindPivot, Entry{Owner: "zoro", Mode: HITL, Container: false}},        // ADR 0043: Zoro's HITL triage leaf
		// Checkpoint tail: each kind resolves to its owner/mode/container
		// triple; Jinbe's row is the Checkpoint handler (container, owns no leaf kind).
		{contract.KindCheckpoint, Entry{Owner: "jinbe", Mode: AFK, Container: true}},
		{contract.KindPrototype, Entry{Owner: "sanji", Mode: HITL, Container: false}},
		{contract.KindBuild, Entry{Owner: "franky", Mode: AFK, Container: false}},
		{contract.KindCritique, Entry{Owner: "franky", Mode: AFK, Container: false}},
		{contract.KindConflict, Entry{Owner: "franky", Mode: AFK, Container: false}},
		{contract.KindReview, Entry{Owner: "usopp", Mode: AFK, Container: false}},
		{contract.KindApproval, Entry{Owner: "luffy", Mode: HITL, Container: false}},
		{contract.KindRollback, Entry{Owner: "jinbe", Mode: AFK, Container: false}},  // ADR 0047: Jinbe's first leaf kind
		{contract.KindSurgery, Entry{Owner: "franky", Mode: HITL, Container: false}}, // ADR 0092: human-filed HITL leaf, never an AFK candidate
		{contract.KindTask, Entry{Owner: "brook", Mode: HITL, Container: false}},     // ADR 0115: human-launched guide, never an AFK candidate
	}
	for _, c := range cases {
		got, err := Lookup(c.kind)
		if err != nil {
			t.Errorf("Lookup(%q) returned error: %v", c.kind, err)
			continue
		}
		if got != c.want {
			t.Errorf("Lookup(%q) = %+v, want %+v", c.kind, got, c.want)
		}
	}
}

func TestLookupUnknownKindIsLoudError(t *testing.T) {
	// a kind nothing registers has no registry row.
	if _, err := Lookup(contract.Kind("unowned")); err == nil {
		t.Fatal("Lookup of an unowned kind must return a loud error, got nil")
	}
}
