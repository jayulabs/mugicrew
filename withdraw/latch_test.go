package withdraw

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// latchGraph installs a Latch blocking three beads; mutate, if given, edits them
// (lt, w1, w2, w3 in order) before the graph is installed.
func latchGraph(t *testing.T, mutate ...func([]bd.Bead)) *[]closeCall {
	t.Helper()
	g := []bd.Bead{
		b("lt", contract.KindLatch, ""),
		b("w1", contract.KindBuild, "", "lt"),
		b("w2", contract.KindChart, "", "lt"),
		b("w3", contract.KindBuild, "", "lt"),
	}
	for _, m := range mutate {
		m(g)
	}
	return graph(t, g...)
}

// A Latch withdraw plans just the Latch, no ordered-after refusal, and names the open
// beads it blocks as released (Spec mc-3cnhf.16 D6).
func TestWithdrawLatchPlansAlone(t *testing.T) {
	latchGraph(t, func(g []bd.Bead) { g[3].Status = "closed" })
	p, err := PlanOf("lt", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != KindWithdraw || !slices.Equal(bd.IDs(p.Set), []string{"lt"}) || len(p.Refusals) != 0 ||
		len(p.Swept)+len(p.Before)+len(p.LeftOpen)+len(p.Releases) != 0 {
		t.Errorf("plan = %+v", p)
	}
	if !slices.Equal(p.LatchReleases, []string{"w1", "w2"}) {
		t.Errorf("unblocked = %v", p.LatchReleases)
	}
}

// Running it closes only the Latch, withdrawn; the blocked beads are not written.
func TestWithdrawLatchClosesOnlyTheLatch(t *testing.T) {
	calls := latchGraph(t)
	res, err := Withdraw("lt", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || !slices.Equal((*calls)[0].ids, []string{"lt"}) || (*calls)[0].reason != "withdrawn" {
		t.Errorf("calls = %v", *calls)
	}
	if !slices.Equal(res.Closed, []string{"lt"}) || !slices.Equal(res.LatchReleases, []string{"w1", "w2", "w3"}) {
		t.Errorf("result = %+v", res)
	}
}

func TestWithdrawLatchInFlightRefuses(t *testing.T) {
	calls := latchGraph(t)
	_, err := Withdraw("lt", busy("lt"))
	var re *RefusedError
	if !errors.As(err, &re) || len(re.Refusals) != 1 || re.Refusals[0].Kind != InFlight || len(*calls) != 0 {
		t.Errorf("err = %v, calls = %v", err, *calls)
	}
}

func TestWithdrawLatchRefusesRelease(t *testing.T) {
	latchGraph(t)
	if _, err := PlanOf("lt", Opts{Release: []string{"w1"}}); err == nil {
		t.Error("want --release refused")
	}
}

func TestWithdrawDraftLatchNamesLatchInError(t *testing.T) {
	d := b("lt", contract.KindLatch, "")
	d.Status, d.Labels = "deferred", []string{"drafted-by:x"}
	graph(t, d)
	_, err := PlanOf("lt", Opts{})
	if err == nil || !strings.Contains(err.Error(), "Latch") {
		t.Errorf("err = %v", err)
	}
}
