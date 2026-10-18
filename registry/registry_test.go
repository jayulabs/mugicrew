package registry

import (
	"slices"
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
		{contract.KindSignal, Entry{Owner: "luffy", Mode: HITL, Container: false}}, // ADR 0129: Luffy triages
		{contract.KindGrilling, Entry{Owner: "nami", Mode: HITL, Container: false}},
		{contract.KindResearch, Entry{Owner: "nami", Mode: AFK, Container: false}},
		{contract.KindChart, Entry{Owner: "nami", Mode: AFK, Container: true}},
		{contract.KindSurvey, Entry{Owner: "nami", Mode: HITL, Container: false}},       // the Chart's HITL charting leaf
		{contract.KindChartConflict, Entry{Owner: "nami", Mode: AFK, Container: false}}, // ADR 0011, ADR 0080
		{contract.KindSpec, Entry{Owner: "zoro", Mode: AFK, Container: false}},          // FR1: Zoro's Spec leaf
		{contract.KindPivot, Entry{Owner: "zoro", Mode: HITL, Container: false}},        // ADR 0043: Zoro's HITL triage leaf
		// Checkpoint tail: each kind resolves to its owner/mode/container
		// triple; Jinbe's row is the Checkpoint handler (container, owns no leaf kind).
		{contract.KindCheckpoint, Entry{Owner: "jinbe", Mode: AFK, Container: true, Mechanical: true}},
		{contract.KindPrototype, Entry{Owner: "sanji", Mode: HITL, Container: false}},
		{contract.KindBuild, Entry{Owner: "franky", Mode: AFK, Container: false}},
		{contract.KindCritique, Entry{Owner: "franky", Mode: AFK, Container: false}},
		{contract.KindConflict, Entry{Owner: "franky", Mode: AFK, Container: false}},
		{contract.KindReview, Entry{Owner: "usopp", Mode: AFK, Container: false}},
		{contract.KindApproval, Entry{Owner: "luffy", Mode: HITL, Container: false}},
		{contract.KindRollback, Entry{Owner: "jinbe", Mode: AFK, Container: false, Mechanical: true}},  // ADR 0047: Jinbe's first leaf kind
		{contract.KindLanding, Entry{Owner: "jinbe", Mode: AFK, Container: false, Mechanical: true}},   // ADR 0145/0147: lands its parent container
		{contract.KindLatch, Entry{Owner: "brook", Mode: AFK, Container: false, Mechanical: true}},     // Spec mc-8ymw.9 D1: Brook's Jira latch check
		{contract.KindSurgery, Entry{Owner: "franky", Mode: HITL, Container: false}},                   // ADR 0092: human-filed HITL leaf, never an AFK candidate
		{contract.KindTask, Entry{Owner: "brook", Mode: HITL, Container: false}},                       // ADR 0115: human-launched guide, never an AFK candidate
		{contract.KindDiscussion, Entry{Owner: "robin", Mode: HITL, Container: false, ReadOnly: true}}, // ADR 0194: read-only human-launched Robin session
		{contract.KindInitiative, Entry{Owner: "luffy", Mode: HITL, Container: true}},                  // ADR 0128: branchless HITL container above the spine
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

// SessionKinds is every kind whose sessions launch a harness — the Settings
// Kind profiles rows (ADR 0136). Jinbe's mechanical kinds stay off it.
func TestSessionKindsSortedAndExcludesMechanical(t *testing.T) {
	want := []contract.Kind{
		"approval", "build", "chart", "chart-conflict", "checkup", "conflict", "critique", "discussion",
		"grilling", "initiative", "pivot", "prototype", "research", "review",
		"signal", "spec", "surgery", "survey", "task",
	}
	got := SessionKinds()
	if len(got) != len(want) {
		t.Fatalf("SessionKinds() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SessionKinds() = %v, want %v", got, want)
		}
	}
	for k, e := range table {
		in := false
		for _, s := range got {
			in = in || s == k
		}
		if in == e.Mechanical {
			t.Errorf("kind %q: Mechanical=%v but in SessionKinds()=%v", k, e.Mechanical, in)
		}
	}
}

// AllKinds is every registry kind, sorted, each Mechanical one marked: the
// Settings Kind profiles section draws a — row for those.
func TestAllKindsSortedCoversEveryKind(t *testing.T) {
	got := AllKinds()
	if len(got) != len(table) {
		t.Fatalf("AllKinds() has %d kinds, registry has %d", len(got), len(table))
	}
	if !slices.IsSorted(got) {
		t.Errorf("AllKinds() not sorted: %v", got)
	}
	for _, k := range got {
		e, err := Lookup(k)
		if err != nil {
			t.Fatalf("AllKinds() lists %q: %v", k, err)
		}
		if want := slices.Contains([]contract.Kind{"checkpoint", "landing", "latch", "rollback"}, k); e.Mechanical != want {
			t.Errorf("kind %q: Mechanical=%v, want %v", k, e.Mechanical, want)
		}
	}
}
