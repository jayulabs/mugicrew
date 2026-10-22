package withdraw

import (
	"errors"
	"slices"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/session"
)

// A Task target closes withdrawn; the Tasks ordered after it close in an earlier
// batch with withdrawn: <task>; the Checkpoint it gated is released, not refused.
func TestWithdrawTaskSweepsTasksAfterIt(t *testing.T) {
	g := []bd.Bead{
		b("tk", contract.KindTask, ""),
		b("t2", contract.KindTask, "", "tk"),
		b("t3", contract.KindTask, "", "t2"),
		b("cp", contract.KindCheckpoint, "", "tk"),
	}
	calls := graph(t, g...)
	evs := eventlogtest.Capture(t)
	res, err := Withdraw("tk", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	want := []closeCall{{[]string{"t2", "t3"}, "withdrawn: tk"}, {[]string{"tk"}, "withdrawn"}}
	if !slices.EqualFunc(*calls, want, func(a, b closeCall) bool { return slices.Equal(a.ids, b.ids) && a.reason == b.reason }) {
		t.Errorf("calls = %v, want %v", *calls, want)
	}
	if !slices.Equal(res.Closed, []string{"t2", "t3", "tk"}) {
		t.Errorf("closed = %v", res.Closed)
	}
	e := (*evs)[0]
	if e.Name != "withdraw.done" || e.Attrs["bead"] != "tk" || e.Attrs["reason"] != "withdrawn" ||
		!slices.Equal(e.Attrs["swept"].([]string), []string{"t2", "t3"}) ||
		len(e.Attrs["swept_before"].([]string)) != 0 ||
		!slices.Equal(e.Attrs["released"].([]string), []string{"cp"}) ||
		len(e.Attrs["left_open"].([]string)) != 0 {
		t.Errorf("event = %+v", e)
	}
}

// A chain's withdraw sweeps its post-land Task instead of refusing, and spares a
// pre-land one on the Checkpoint before-closes with it (ADR 0351); a non-Task
// ordered after a swept Task still refuses.
func TestWithdrawChainSweepsTasks(t *testing.T) {
	g := append(fixture(),
		b("post", contract.KindTask, "", "cp"),
		b("pre", contract.KindTask, ""),
	)
	g[2].BlockedBy = []string{"pre"} // cp
	calls := graph(t, g...)
	p, err := PlanOf("cp", Opts{})
	if err != nil || len(p.Refusals) != 0 {
		t.Fatalf("plan = %+v, %v", p, err)
	}
	if len(p.Swept) != 1 || p.Swept[0].Bead.ID != "post" || len(p.Before) != 1 || p.Before[0].Bead.ID != "pre" ||
		p.Before[0].Before != "cp" || len(p.LeftOpen) != 0 {
		t.Errorf("plan sweep = %+v / %+v / %+v", p.Swept, p.Before, p.LeftOpen)
	}
	if got := bd.IDs(p.Set); !slices.Equal(got, []string{"post", "pre", "spec", "b1", "b2", "cp"}) {
		t.Errorf("set = %v", got)
	}
	res, err := Withdraw("cp", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 || !slices.Equal((*calls)[0].ids, []string{"post", "pre"}) || (*calls)[0].reason != "withdrawn: cp" || (*calls)[1].reason != "withdrawn" {
		t.Errorf("calls = %v", *calls)
	}
	if len(res.Before) != 1 || res.Before[0].Bead.ID != "pre" || len(res.Swept) != 1 {
		t.Errorf("result = %+v", res)
	}

	graph(t, append(g, b("sig", contract.KindSignal, "", "post"))...)
	_, err = Withdraw("cp", Opts{})
	var re *RefusedError
	if !errors.As(err, &re) || re.Refusals[0].Kind != OrderedAfter || !slices.Equal(re.Refusals[0].Beads, []string{"sig"}) {
		t.Errorf("err = %v, want sig ordered after", err)
	}
}

// A close that fails between the two batches re-reads what closed.
func TestWithdrawTaskFailureBetweenBatches(t *testing.T) {
	g := []bd.Bead{b("tk", contract.KindTask, ""), b("t2", contract.KindTask, "", "tk")}
	graph(t, g...)
	evs := eventlogtest.Capture(t)
	listAll = func() ([]bd.Bead, error) {
		out := slices.Clone(g)
		for i := range out {
			if out[i].ID == "t2" && n > 0 {
				out[i].Status = "closed"
			}
		}
		return out, nil
	}
	forceClose = func(ids []string, reason string) error {
		n++
		if n == 1 {
			return nil
		}
		return errors.New("boom")
	}
	n = 0
	if _, err := Withdraw("tk", Opts{}); err == nil {
		t.Fatal("want the close error")
	}
	e := (*evs)[0]
	if e.Name != "withdraw.failed" || e.Attrs["bead"] != "tk" || !slices.Equal(e.Attrs["closed"].([]string), []string{"t2"}) {
		t.Errorf("event = %+v", e)
	}
}

var n int

// A swept Task with a live session refuses the whole withdraw, with the session fix.
func TestWithdrawTaskLiveSessionRefuses(t *testing.T) {
	g := []bd.Bead{b("tk", contract.KindTask, ""), b("t2", contract.KindTask, "", "tk")}
	calls := graph(t, g...)
	_, err := Withdraw("tk", busy("t2"))
	var re *RefusedError
	if !errors.As(err, &re) || re.Refusals[0].Kind != InFlight || !slices.Equal(re.Refusals[0].Beads, []string{"t2"}) ||
		re.Refusals[0].Fix == inFlightFix || re.Refusals[0].Reason(KindWithdraw) != "in-flight effort" {
		t.Errorf("err = %v", err)
	}
	if len(*calls) != 0 {
		t.Errorf("closed %v", *calls)
	}
}

// A Task still blocked on the chain's Spec (before Carry) is swept with it, keeps an
// outside wait in Also, and a pre-land Task held open by a non-Checkpoint is named in
// the event's left_open, while swept_before stays empty.
func TestWithdrawChainSweepsPreCarryTaskAndReportsLeftOpen(t *testing.T) {
	g := append(fixture(),
		b("early", contract.KindTask, "", "spec", "other"),
		b("other", contract.KindBuild, ""),
		b("pre", contract.KindTask, ""),
	)
	g[2].BlockedBy = []string{"pre"}        // cp
	g[len(g)-2].BlockedBy = []string{"pre"} // other, outside the chain, waits on pre
	calls := graph(t, g...)
	evs := eventlogtest.Capture(t)
	p, err := PlanOf("cp", Opts{})
	if err != nil || len(p.Refusals) != 0 {
		t.Fatalf("plan = %+v, %v", p, err)
	}
	if len(p.Swept) != 1 || p.Swept[0].Bead.ID != "early" || !slices.Equal(p.Swept[0].Also, []string{"other"}) {
		t.Errorf("swept = %+v", p.Swept)
	}
	if _, err := Withdraw("cp", Opts{}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 || !slices.Equal((*calls)[0].ids, []string{"early"}) || (*calls)[0].reason != "withdrawn: cp" {
		t.Errorf("calls = %v", *calls)
	}
	e := (*evs)[0]
	if e.Name != "withdraw.done" || !slices.Equal(e.Attrs["left_open"].([]string), []string{"pre"}) ||
		len(e.Attrs["swept_before"].([]string)) != 0 || !slices.Equal(e.Attrs["swept"].([]string), []string{"early"}) {
		t.Errorf("event = %+v", e)
	}
}

// A chain's withdraw closes a pre-land Task in the first batch with withdrawn: <head>,
// names it in swept_before and not swept, and a transitive-only predecessor goes too.
func TestWithdrawChainBeforeClosesPreLandTasks(t *testing.T) {
	g := append(fixture(),
		b("post", contract.KindTask, "", "cp"),
		b("pre", contract.KindTask, ""),
		b("pre0", contract.KindTask, ""),
	)
	g[2].BlockedBy = []string{"pre"} // cp
	g[len(g)-2].BlockedBy = []string{"pre0"}
	calls := graph(t, g...)
	evs := eventlogtest.Capture(t)
	res, err := Withdraw("cp", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 || !slices.Equal((*calls)[0].ids, []string{"post", "pre", "pre0"}) || (*calls)[0].reason != "withdrawn: cp" ||
		!slices.Equal((*calls)[1].ids, []string{"spec", "b1", "b2", "cp"}) {
		t.Errorf("calls = %v", *calls)
	}
	e := (*evs)[0]
	if !slices.Equal(e.Attrs["swept"].([]string), []string{"post"}) ||
		!slices.Equal(e.Attrs["swept_before"].([]string), []string{"pre", "pre0"}) ||
		len(e.Attrs["left_open"].([]string)) != 0 || len(res.Before) != 2 || res.Before[1].Before != "pre" {
		t.Errorf("event = %+v, result = %+v", e, res)
	}
}

// A live session on a before-closed Task refuses the whole withdraw with the session fix.
func TestWithdrawChainBeforeClosedLiveSessionRefuses(t *testing.T) {
	g := append(fixture(), b("pre", contract.KindTask, ""))
	g[2].BlockedBy = []string{"pre"} // cp
	calls := graph(t, g...)
	_, err := Withdraw("cp", busy("pre"))
	var re *RefusedError
	if !errors.As(err, &re) || re.Refusals[0].Kind != InFlight || !slices.Equal(re.Refusals[0].Beads, []string{"pre"}) || re.Refusals[0].Fix != sessionFix {
		t.Errorf("err = %v", err)
	}
	if len(*calls) != 0 {
		t.Errorf("closed %v", *calls)
	}
}

// A racing claim on an open Task refuses with the session fix, not the effort one.
func TestWithdrawTaskFenceRaceUsesSessionFix(t *testing.T) {
	graph(t, b("tk", contract.KindTask, ""))
	casErr["tk"] = bd.ErrClaimedSince
	_, err := Withdraw("tk", Opts{})
	var re *RefusedError
	if !errors.As(err, &re) || re.Refusals[0].Kind != InFlight || re.Refusals[0].Fix != sessionFix {
		t.Errorf("err = %v", err)
	}
}

// An open unclaimed Task in the set is CAS-claimed by the fence.
func TestWithdrawTaskCASClaimsOpenTask(t *testing.T) {
	graph(t, b("tk", contract.KindTask, ""))
	if _, err := Withdraw("tk", Opts{}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cas, []string{"claim tk"}) {
		t.Errorf("cas = %v", cas)
	}
}

// An Exited Task's dead pane is no in-flight work and no lost work: the inventory
// names the session, cleanup kills it and reports it.
func TestWithdrawTaskExitedSessionKilled(t *testing.T) {
	w := newWorld(t)
	killed, _ := stubDrops(t)
	w.sessions["tk"] = session.Info{Session: "tk", State: session.Exited}
	tk := bd.Bead{ID: "tk", Kind: contract.KindTask, Status: "in_progress"}
	inv := take(t, Opts{}, tk)
	if inv.Loses() || len(inv.Beads) != 1 || inv.Beads[0].Session != "tk" {
		t.Fatalf("inventory = %+v", inv)
	}
	removed, kept := cleanup(inv, "tk", []bd.Bead{tk})
	if len(kept) != 0 || len(removed) != 1 || removed[0].Kind != ItemSession || !slices.Equal(*killed, []string{"tk"}) {
		t.Errorf("removed %v kept %v killed %v", removed, kept, *killed)
	}
}

// A live-session Task does not leave a session to kill; an idle one leaves nothing.
func TestInventoryIdleTaskLeavesNothing(t *testing.T) {
	w := newWorld(t)
	_ = w
	tk := bd.Bead{ID: "tk", Kind: contract.KindTask, Status: "in_progress"}
	if inv := take(t, Opts{}, tk); len(inv.Beads) != 0 {
		t.Errorf("inventory = %+v", inv)
	}
}

// A Task head has no backward reach: a Task blocking it, and one blocking a Task swept
// after it, stay out of the set, swept_before is empty, and left_open is as today.
func TestWithdrawTaskHeadHasNoBackwardReach(t *testing.T) {
	g := []bd.Bead{
		b("pre", contract.KindTask, ""),
		b("tk", contract.KindTask, "", "pre"),
		b("pre2", contract.KindTask, ""),
		b("t2", contract.KindTask, "", "tk", "pre2"),
	}
	calls := graph(t, g...)
	evs := eventlogtest.Capture(t)
	res, err := Withdraw("tk", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Closed, []string{"t2", "tk"}) || len(res.Before) != 0 || len(*calls) != 2 {
		t.Errorf("closed = %v, before = %v, calls = %v", res.Closed, res.Before, *calls)
	}
	e := (*evs)[0]
	if !slices.Equal(e.Attrs["swept"].([]string), []string{"t2"}) || len(e.Attrs["swept_before"].([]string)) != 0 ||
		len(e.Attrs["left_open"].([]string)) != 0 {
		t.Errorf("event = %+v", e)
	}
}

// An Exited Brook pane on a before-closed Task is no in-flight work: cleanup kills it.
func TestWithdrawChainBeforeClosedExitedSessionKilled(t *testing.T) {
	w := newWorld(t)
	killed, _ := stubDrops(t)
	g := append(fixture(), b("pre", contract.KindTask, ""))
	g[2].BlockedBy = []string{"pre"} // cp
	g[len(g)-1].Status = "in_progress"
	graph(t, g...)
	w.sessions["pre"] = session.Info{Session: "pre", State: session.Exited}
	p, err := PlanOf("cp", Opts{})
	if err != nil || len(p.Refusals) != 0 || len(p.Before) != 1 {
		t.Fatalf("plan = %+v, %v", p, err)
	}
	inv := take(t, Opts{}, p.Set...)
	if inv.Loses() || len(inv.Beads) != 1 || inv.Beads[0].Session != "pre" {
		t.Fatalf("inventory = %+v", inv)
	}
	removed, kept := cleanup(inv, "cp", p.Set)
	if len(kept) != 0 || len(removed) != 1 || removed[0].Kind != ItemSession || removed[0].Bead != "pre" || !slices.Equal(*killed, []string{"pre"}) {
		t.Errorf("removed %v kept %v killed %v", removed, kept, *killed)
	}
}
