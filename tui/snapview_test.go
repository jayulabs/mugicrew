package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/loop"
)

// snapGraph is a graph fixture whose open beads are all ready, with cp's tip at "abc".
func snapGraph(beads ...bd.Bead) graphData {
	return graphData{beads: beads, frontier: fixtureReady(beads), cpTips: map[string]string{"cp": "abc"}}
}

// parityGraph is one fixture reaching every run-now cause but foreign-landing (only the live
// adapter sees that) and the land, spec, run and retry actions.
func parityGraph() graphData {
	cpOf := func(id string) bd.Bead {
		return bd.Bead{ID: id, Kind: contract.KindCheckpoint, Container: true, Status: "open"}
	}
	mem := func(id, parent string) bd.Bead {
		return bd.Bead{ID: id, Kind: contract.KindBuild, Parent: parent, Status: "closed"}
	}
	appr := func(id, parent, pin string, status string) bd.Bead {
		b := bd.Bead{ID: id, Kind: contract.KindApproval, Parent: parent, Status: status, Description: "Commit: " + pin}
		if status == "closed" {
			b.CloseReason = bd.ApprovedReason
		}
		return b
	}
	pivotBlocked := cpOf("cp-pivot")
	pivotBlocked.BlockedBy = []string{"pivot"}
	heldLand := cpOf("cp-landheld")
	heldLand.BlockedBy = []string{"errand"}
	manual := cpOf("cp-manual")
	manual.Labels = []string{"paused:manual"}
	beads := []bd.Bead{
		cpOf("cp-defect"), mem("m1", "cp-defect"), {ID: "crit1", Kind: contract.KindCritique, Parent: "cp-defect", Status: "in_progress"},
		pivotBlocked, {ID: "pivot", Kind: contract.KindPivot, Status: "open"}, mem("m2", "cp-pivot"), {ID: "frozen", Kind: contract.KindBuild, Parent: "cp-pivot", Status: "open"},
		cpOf("cp-await"), mem("m3", "cp-await"), appr("a3", "cp-await", "abc", "open"),
		cpOf("cp-nomem"),
		cpOf("cp-review"), mem("m4", "cp-review"), appr("a4", "cp-review", "old", "closed"),
		cpOf("cp-land"), mem("m5", "cp-land"), appr("a5", "cp-land", "abc", "closed"),
		heldLand, mem("m6", "cp-landheld"), appr("a6", "cp-landheld", "abc", "closed"), {ID: "errand", Kind: contract.KindErrand, Status: "open"},
		cpOf("cp-undrained"), {ID: "m7", Kind: contract.KindBuild, Parent: "cp-undrained", Status: "open"}, mem("m8", "cp-undrained"), appr("a7", "cp-undrained", "abc", "closed"),
		cpOf("cp-landing"), mem("m9", "cp-landing"), {ID: "lnd", Kind: contract.KindLanding, Parent: "cp-landing", Status: "open"},
		cpOf("cp-landed"), mem("m10", "cp-landed"),
		cpOf("cp-surgery"), mem("m11", "cp-surgery"), {ID: "crit2", Kind: contract.KindCritique, Parent: "cp-surgery", Status: "closed"},
		{ID: "crit3", Kind: contract.KindCritique, Parent: "cp-surgery", Status: "open"}, {ID: "surg", Kind: contract.KindSurgery, Parent: "cp-surgery", Status: "open"},
		manual, {ID: "under-manual", Kind: contract.KindBuild, Parent: "cp-manual", Status: "open"},
		{ID: "chart-ok", Kind: contract.KindChart, Container: true, Status: "open"}, {ID: "k1", Kind: contract.KindResearch, Parent: "chart-ok", Status: "closed"},
		{ID: "chart-open", Kind: contract.KindChart, Container: true, Status: "open"}, {ID: "k2", Kind: contract.KindResearch, Parent: "chart-open", Status: "open"},
		{ID: "chart-held", Kind: contract.KindChart, Container: true, Status: "open"}, {ID: "k3", Kind: contract.KindResearch, Parent: "chart-held", Status: "closed"},
		{ID: "chart-landing", Kind: contract.KindChart, Container: true, Status: "open"}, {ID: "lnd2", Kind: contract.KindLanding, Parent: "chart-landing", Status: "open"},
		cpOf("cp-behind"), {ID: "bm-build", Kind: contract.KindBuild, Parent: "cp-behind", Status: "open", BlockedBy: []string{"bm-dep"}},
		{ID: "cp-other", Kind: contract.KindCheckpoint, Container: true, Status: "closed"}, {ID: "bm-dep", Kind: contract.KindBuild, Parent: "cp-other", Status: "closed"},
		{ID: "dep", Kind: contract.KindResearch, Status: "open"},
		{ID: "blk", Kind: contract.KindResearch, Status: "open", BlockedBy: []string{"dep"}},
		{ID: "cond", Kind: contract.KindResearch, Status: "open", BlockedBy: []string{"dep"}, CondBlockedBy: []string{"dep"}},
		{ID: "leaf", Kind: contract.KindResearch, Status: "open"},
		{ID: "done", Kind: contract.KindResearch, Status: "closed"},
		{ID: "p-failing", Kind: contract.KindResearch, Status: "open", Labels: []string{"paused:failing"}},
		{ID: "p-hold", Kind: contract.KindResearch, Status: "open", Labels: []string{"paused:foreign-hold"}},
		{ID: "p-checkup", Kind: contract.KindResearch, Status: "open", Labels: []string{"paused:checkup"}},
		{ID: "p-odd", Kind: contract.KindResearch, Status: "open", Labels: []string{"paused:failing", "paused:manual"}},
		{ID: "p-new", Kind: contract.KindResearch, Status: "open", Labels: []string{"paused:never-heard-of-it"}},
	}
	g := graphData{
		beads:    beads,
		frontier: fixtureReady(beads),
		efforts:  &git.EffortSet{Branches: map[string]bool{"m8": true, "k3": true}},
		landed:   map[string]string{"cp-landed": "f00"},
		// cp-other landed after cp-behind was cut: its land commit is off cp-behind's tip.
		blockLanded: map[string]string{"cp-other": "land1"},
		contains:    map[string]bool{"land1@abc": false},
		cpTips:      map[string]string{},
	}
	for _, b := range beads {
		if b.Kind == contract.KindCheckpoint {
			g.cpTips[b.ID] = "abc"
		}
	}
	return g
}

// liveOver is the real live adapter over bd/git reads stubbed from the same graph facts g
// holds, so a disagreement between its reads and the snapshot's shows up as a verdict diff.
func liveOver(g graphData) loop.View {
	byID := map[string]bd.Bead{}
	for _, b := range g.beads {
		byID[b.ID] = b
	}
	return loop.NewLiveViewOver(loop.LiveReads{
		ListChildren: func(id string) ([]bd.Bead, error) {
			var out []bd.Bead
			for _, b := range g.beads {
				if b.Parent == id {
					out = append(out, b)
				}
			}
			return out, nil
		},
		LandedBase: func(trailer string) (string, error) {
			for id, sha := range g.blockLanded {
				if trailer == git.TrailerFor("checkpoint", id) {
					return sha, nil
				}
			}
			for id, sha := range g.landed {
				if trailer == git.TrailerFor("checkpoint", id) || trailer == git.TrailerFor("chart", id) {
					return sha, nil
				}
			}
			return "", git.ErrNotLanded
		},
		Head: func(string) (string, error) { return "abc", nil },
		Show: func(id string) (bd.Bead, error) {
			if b, ok := byID[id]; ok {
				return b, nil
			}
			return bd.Bead{}, fmt.Errorf("no bead %s", id)
		},
		Blockers: func(id string) (open, closed []bd.Bead, err error) {
			for _, to := range byID[id].BlockedBy {
				x, ok := byID[to]
				if !ok || slices.Contains(byID[id].CondBlockedBy, to) {
					continue
				}
				if x.Open() {
					open = append(open, x)
				} else if x.Status == "closed" {
					closed = append(closed, x)
				}
			}
			return open, closed, nil
		},
		Contains:   func(_ bd.Bead, tip, sha string) (bool, error) { return g.contains[loop.ContainsKey(tip, sha)], nil },
		EffortRefs: func() (*git.EffortSet, error) { return g.efforts, nil },
	}, nil)
}

// The row's snapshot adapter and the press's live adapter give one run-now verdict per bead.
func TestSnapshotAdapterGivesTheLiveAdapterVerdict(t *testing.T) {
	g := parityGraph()
	seenCause, seenAction := map[loop.Cause]bool{}, map[loop.Action]bool{}
	for _, in := range []loop.Inputs{{}, {Force: true}} {
		for i := range g.beads {
			b := g.beads[i]
			want, err := loop.Judge(b, loop.ModeRunNow, g.widened(), in, liveOver(g))
			if err != nil {
				t.Fatalf("%s: %v", b.ID, err)
			}
			got, _ := loop.Judge(b, loop.ModeRunNow, g.widened(), in, snapView{g})
			if got.Runnable != want.Runnable || got.Action != want.Action || got.Cause != want.Cause || got.PauseCause != want.PauseCause || !slices.Equal(holderKeys(got.Holders), holderKeys(want.Holders)) {
				t.Errorf("%s (force=%v): snapshot %+v, live %+v", b.ID, in.Force, got, want)
			}
			if want.Runnable {
				seenAction[want.Action] = true
			} else {
				seenCause[want.Cause] = true
			}
		}
	}
	for _, c := range loop.Causes {
		if c != loop.CauseForeignLanding && c != loop.CausePaced && !seenCause[c] {
			t.Errorf("the fixture never reaches cause %s", c)
		}
	}
	for _, a := range []loop.Action{loop.ActionRun, loop.ActionSpec, loop.ActionLand, loop.ActionRetry, loop.ActionRetryHold} {
		if !seenAction[a] {
			t.Errorf("the fixture never reaches action %s", a)
		}
	}
}

// holderKeys is each holder's id and effort branch.
func holderKeys(hs []loop.Holder) []string {
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = h.Bead.ID + "@" + h.Branch
	}
	return out
}

// The row and the press refuse a Checkpoint alike, per cause.
func TestRowReachesThePressRefusalPerCause(t *testing.T) {
	cp := bd.Bead{ID: "cp", Kind: contract.KindCheckpoint, Container: true, Status: "open"}
	appr := func(pin string) bd.Bead {
		return bd.Bead{ID: "a", Kind: contract.KindApproval, Parent: "cp", Status: "closed", CloseReason: bd.ApprovedReason, Description: "Commit: " + pin}
	}
	mem := bd.Bead{ID: "m", Kind: contract.KindBuild, Parent: "cp", Status: "closed"}
	for _, c := range []struct {
		name  string
		beads []bd.Bead
		land  map[string]string
		label string
		why   string
	}{
		{"in_progress Critique is defect-open", []bd.Bead{cp, mem, appr("abc"), {ID: "c", Kind: contract.KindCritique, Parent: "cp", Status: "in_progress"}}, nil, "", "open conflict/critique: c"},
		{"non-head Approval is ignored", []bd.Bead{cp, mem, appr("old")}, nil, "", "review due"},
		{"landed trailer rolls forward", []bd.Bead{cp, mem, {ID: "c", Kind: contract.KindCritique, Parent: "cp", Status: "closed"}}, map[string]string{"cp": "f00"}, "land", ""},
		{"at-head approval lands", []bd.Bead{cp, mem, appr("abc")}, nil, "land", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := snapGraph(c.beads...)
			g.landed = c.land
			b := g.beadByID("cp")
			if got := g.runNowLabel(b); got != c.label {
				t.Errorf("label = %q, want %q", got, c.label)
			}
			if got := g.runNowWhy(b); !strings.Contains(got, c.why) || (c.why == "") != (got == "") {
				t.Errorf("why = %q, want it to contain %q", got, c.why)
			}
		})
	}
}

// The force y/n opens on blocked, and not on a member a Pivot has frozen.
func TestForceDialogOpensOnBlockedOnly(t *testing.T) {
	pivot := bd.Bead{ID: "pv", Kind: contract.KindPivot, Status: "open", Title: "Bad FR"}
	cp := bd.Bead{ID: "cp", Kind: contract.KindCheckpoint, Container: true, Status: "open", BlockedBy: []string{"pv"}}
	dep := bd.Bead{ID: "dep", Kind: contract.KindResearch, Status: "open"}
	blocked := bd.Bead{ID: "blk", Kind: contract.KindResearch, Status: "open", BlockedBy: []string{"dep"}}
	member := bd.Bead{ID: "mem", Kind: contract.KindBuild, Parent: "cp", Status: "open"}
	g := snapGraph(cp, pivot, dep, blocked, member)
	if v := g.eligibility(g.beadByID("blk")); v.Cause != loop.CauseBlocked {
		t.Errorf("blocked leaf: cause %q", v.Cause)
	}
	if why := g.runNowWhy(g.beadByID("blk")); why != "" {
		t.Errorf("blocked leaf: forced press refused: %q", why)
	}
	if v := g.eligibility(g.beadByID("mem")); v.Cause != loop.CauseContainerFrozen {
		t.Errorf("frozen member: cause %q, want container-frozen", v.Cause)
	}
	if why := g.runNowWhy(g.beadByID("mem")); !strings.Contains(why, "gated by Bad FR") {
		t.Errorf("frozen member: why %q", why)
	}
}

// The detail pane marks an effort branch whose tip is its cut ref (integrated) and
// leaves a held one (tip ahead of cut, or no cut ref) bare (ADR 0394).
func TestDetailPaneMarksIntegratedEffort(t *testing.T) {
	efforts := &git.EffortSet{
		Branches:   map[string]bool{"done": true, "ahead": true, "nocut": true},
		Integrated: map[string]bool{"done": true},
	}
	for id, want := range map[string]bool{"done": true, "ahead": false, "nocut": false} {
		g := graphTab{}
		g.beads = []bd.Bead{{ID: id, Kind: contract.KindResearch, Status: "closed"}}
		g.sel, g.w = id, 80
		g.efforts = efforts
		got := ansi.Strip(g.detailBody())
		if !strings.Contains(got, "effort/"+id) {
			t.Fatalf("%s: pane lacks the effort branch:\n%s", id, got)
		}
		if has := strings.Contains(got, "effort/"+id+" (integrated)"); has != want {
			t.Errorf("%s: (integrated) marker = %v, want %v:\n%s", id, has, want, got)
		}
	}
}

// A Build whose Checkpoint tip lacks a landed cross-Checkpoint blocker's land commit reads
// behind-mainline off the snapshot facts; the cause hint says so and names the holder, and
// the forced re-judge (what a confirmed y/n runs) clears it.
func TestBehindMainlineRowAndConfirm(t *testing.T) {
	cp := bd.Bead{ID: "cp", Kind: contract.KindCheckpoint, Container: true, Status: "open"}
	ocp := bd.Bead{ID: "ocp", Kind: contract.KindCheckpoint, Container: true, Status: "closed"}
	dep := bd.Bead{ID: "dep", Kind: contract.KindBuild, Parent: "ocp", Status: "closed", Title: "Landed dep"}
	mem := bd.Bead{ID: "mem", Kind: contract.KindBuild, Parent: "cp", Status: "open", BlockedBy: []string{"dep"}}
	g := snapGraph(cp, ocp, dep, mem)
	g.blockLanded = map[string]string{"ocp": "land1"}
	g.contains = map[string]bool{"land1@abc": false}
	b := g.beadByID("mem")
	if v := g.eligibility(b); v.Cause != loop.CauseBehindMainline || len(v.Holders) != 1 || v.Holders[0].Bead.ID != "dep" {
		t.Fatalf("verdict = %+v, want behind-mainline held by dep", v)
	}
	why := g.runNowWhy(b)
	// confirmed re-judges with Force, so the row is runnable and the press asks y/n first.
	if why != "" || g.runNowLabel(b) != "run" {
		t.Errorf("confirmed: why %q label %q, want a runnable row", why, g.runNowLabel(b))
	}
	hint := causeHint(*b, g.eligibility(b))
	for _, w := range []string{"checkpoint/cp", "behind mainline", "Landed dep", "dep", "r requests a rebase"} {
		if !strings.Contains(hint, w) {
			t.Errorf("hint %q lacks %q", hint, w)
		}
	}
	g.contains = map[string]bool{"land1@abc": true}
	if v := g.eligibility(b); !v.Runnable {
		t.Errorf("land commit on the tip: verdict %+v, want runnable", v)
	}
}

// A press on a behind-mainline Build opens the run-anyway y/n, as it does on a blocked one.
func TestRunNowBehindMainlineOpensConfirm(t *testing.T) {
	g := parityGraph()
	n, cmd := Model{}.runNow(g, *g.beadByID("bm-build"))
	if cmd != nil || len(n.(Model).modals) != 1 || n.(Model).modals[0].title != "Run behind-mainline bead now?" {
		t.Errorf("cmd=%v modals=%+v, want the behind-mainline confirm", cmd != nil, n.(Model).modals)
	}
}

// A closed blocker (and its landed Checkpoint) absent from the loaded beads but read by id
// still holds the Build behind-mainline, and r targets the Build's Checkpoint.
func TestBehindMainlineFromUnloadedBlocker(t *testing.T) {
	cp := bd.Bead{ID: "cp", Kind: contract.KindCheckpoint, Container: true, Status: "open"}
	mem := bd.Bead{ID: "mem", Kind: contract.KindBuild, Parent: "cp", Status: "open", BlockedBy: []string{"dep"}}
	g := snapGraph(cp, mem)
	g.behind = map[string]bd.Bead{
		"dep": {ID: "dep", Kind: contract.KindBuild, Parent: "ocp", Status: "closed"},
		"ocp": {ID: "ocp", Kind: contract.KindCheckpoint, Container: true, Status: "closed"},
	}
	g.blockLanded = map[string]string{"ocp": "land1"}
	g.contains = map[string]bool{"land1@abc": false}
	b := g.beadByID("mem")
	if v := g.eligibility(b); v.Cause != loop.CauseBehindMainline {
		t.Fatalf("verdict = %+v, want behind-mainline", v)
	}
	if got := g.rebaseTarget(b); got != "cp" {
		t.Errorf("rebaseTarget = %q, want cp", got)
	}
}
