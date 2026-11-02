package withdraw

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// defogGraph installs a Chart with a grilling leaf and a sibling research leaf under
// the Chart; no bead waits on either.
func defogGraph(t *testing.T, mutate ...func([]bd.Bead)) *[]closeCall {
	t.Helper()
	g := []bd.Bead{
		b("gr", contract.KindGrilling, "ch"),
		b("rs", contract.KindResearch, "ch"),
		b("ch", contract.KindChart, ""),
	}
	for _, m := range mutate {
		m(g)
	}
	return graph(t, g...)
}

// A defog leaf plans and closes alone, withdrawn: not its Chart, not its sibling.
func TestWithdrawDefogLeafAlone(t *testing.T) {
	calls := defogGraph(t)
	p, err := PlanOf("gr", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != KindWithdraw || !slices.Equal(bd.IDs(p.Set), []string{"gr"}) || len(p.Refusals) != 0 ||
		len(p.Swept)+len(p.Before)+len(p.LeftOpen)+len(p.Releases)+len(p.LatchReleases) != 0 {
		t.Errorf("plan = %+v", p)
	}
	if _, err := Withdraw("gr", Opts{}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || !slices.Equal((*calls)[0].ids, []string{"gr"}) || (*calls)[0].reason != bd.WithdrawnReason {
		t.Errorf("calls = %v", *calls)
	}
}

func TestWithdrawDefogLeafRefusals(t *testing.T) {
	calls := defogGraph(t)
	if _, err := PlanOf("gr", Opts{Release: []string{"rs"}}); err == nil {
		t.Error("want --release refused")
	}
	_, err := Withdraw("gr", busy("gr"))
	var re *RefusedError
	if !errors.As(err, &re) || len(re.Refusals) != 1 || re.Refusals[0].Kind != InFlight || len(*calls) != 0 {
		t.Errorf("err = %v, calls = %v", err, *calls)
	}
}

// The ordered-after refusal still guards a defog leaf a bead waits on.
func TestWithdrawDefogLeafOrderedAfterRefuses(t *testing.T) {
	defogGraph(t, func(g []bd.Bead) { g[2].BlockedBy = []string{"gr"} })
	p, err := PlanOf("gr", Opts{})
	if err != nil || len(p.Refusals) != 1 || p.Refusals[0].Kind != OrderedAfter {
		t.Errorf("plan = %+v, err = %v", p, err)
	}
}

// An escalation grilling is a plan error while its chart-conflict is open, accepted after.
func TestWithdrawEscalationGrillingGuard(t *testing.T) {
	cc := func(status string) func([]bd.Bead) {
		return func(g []bd.Bead) {
			x := b("cc", contract.KindChartConflict, "", "gr")
			x.Status = status
			g[2] = x
		}
	}
	defogGraph(t, cc("open"))
	_, err := PlanOf("gr", Opts{})
	if err == nil || !strings.Contains(err.Error(), "cc") || !strings.Contains(err.Error(), "withdraw the Chart") {
		t.Errorf("err = %v", err)
	}
	defogGraph(t, cc("closed"))
	if _, err := PlanOf("gr", Opts{}); err != nil {
		t.Errorf("after the conflict closes: %v", err)
	}
}

// A Chart withdraw still sweeps its defog subtree.
func TestWithdrawChartSweepsDefogSubtree(t *testing.T) {
	defogGraph(t)
	p, err := PlanOf("ch", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"gr", "rs", "ch"} {
		if !slices.Contains(bd.IDs(p.Set), id) {
			t.Errorf("set %v lacks %s", bd.IDs(p.Set), id)
		}
	}
}

// A lone prototype leaf whose throwaway tree holds work needs --discard: without it
// nothing is closed; with it only the prototype closes, withdrawn.
func TestWithdrawPrototypeLeafNeedsDiscard(t *testing.T) {
	calls := defogGraph(t, func(g []bd.Bead) { g[0] = b("pr", contract.KindPrototype, "ch") })
	var taken [][]string
	takeInventory = func(set []bd.Bead, _ Opts) (Inventory, error) {
		taken = append(taken, bd.IDs(set))
		return Inventory{Beads: []BeadLeftovers{{ID: "pr", Trees: []TreeLeftover{{Host: "/tmp/pr", Class: Removable, Dirty: 3}}}}}, nil
	}
	_, err := Withdraw("pr", Opts{})
	var de *DiscardRequiredError
	if !errors.As(err, &de) || !strings.Contains(err.Error(), "/tmp/pr") || !strings.Contains(err.Error(), "--discard") {
		t.Fatalf("err = %v, want a discard-required error naming the throwaway tree", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("wrote %v without --discard", *calls)
	}
	if _, err := Withdraw("pr", Opts{Discard: true}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || !slices.Equal((*calls)[0].ids, []string{"pr"}) || (*calls)[0].reason != "withdrawn" {
		t.Errorf("calls = %v", *calls)
	}
	for _, set := range taken {
		if !slices.Equal(set, []string{"pr"}) {
			t.Errorf("inventory taken over %v, want only the prototype", set)
		}
	}
}
