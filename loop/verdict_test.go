package loop

import (
	"errors"
	"slices"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// fakeView is an in-memory View.
type fakeView struct {
	beads    map[string]bd.Bead
	kids     map[string][]bd.Bead
	efforts  *git.EffortSet
	landed   map[string]Trailer
	tips     map[string]string
	blockers map[string][]Blocker
	err      string // the method name that errors
}

func (f fakeView) fail(m string) error {
	if f.err == m {
		return errors.New("read " + m)
	}
	return nil
}
func (f fakeView) Children(id string) ([]bd.Bead, error) { return f.kids[id], f.fail("Children") }
func (f fakeView) Efforts() (*git.EffortSet, error)      { return f.efforts, f.fail("Efforts") }
func (f fakeView) Landed(b bd.Bead) (Trailer, error)     { return f.landed[b.ID], f.fail("Landed") }
func (f fakeView) CheckpointTip(id string) (string, error) {
	return f.tips[id], f.fail("CheckpointTip")
}
func (f fakeView) Blockers(b bd.Bead) ([]Blocker, error) { return f.blockers[b.ID], f.fail("Blockers") }
func (f fakeView) Bead(id string) (bd.Bead, error) {
	b, ok := f.beads[id]
	if !ok {
		return bd.Bead{}, errors.New("no bead " + id)
	}
	return b, f.fail("Bead")
}

func mk(id string, kind contract.Kind, status string, labels ...string) bd.Bead {
	return bd.Bead{ID: id, Kind: kind, Status: status, Labels: labels, Title: "t " + id}
}

func child(parent bd.Bead, id string, kind contract.Kind, status string) bd.Bead {
	b := mk(id, kind, status)
	b.Parent = parent.ID
	return b
}

func blk(b bd.Bead) Blocker { return Blocker{Bead: b, Edge: EdgeBlocks} }

func ids(hs []Holder) []string {
	out := []string{}
	for _, h := range hs {
		out = append(out, h.Bead.ID)
	}
	return out
}

func TestJudge(t *testing.T) {
	foreign := &git.ForeignLanding{Trailer: "Chart: x", Ours: "g1", Theirs: "g2", Sha: "abc"}
	pin := func(sha string) string { return "Commit: " + sha }
	approval := func(cp bd.Bead, id, status, reason, sha string) bd.Bead {
		a := child(cp, id, contract.KindApproval, status)
		a.CloseReason, a.Description = reason, pin(sha)
		return a
	}
	chart := mk("ch", contract.KindChart, "open")
	cp := mk("cp", contract.KindCheckpoint, "open")
	leaf := mk("rs", contract.KindResearch, "open")
	crit := child(cp, "cr", contract.KindCritique, "open")
	build := func(id, status string) bd.Bead { return child(cp, id, contract.KindBuild, status) }
	closedBuild := build("b1", "closed")
	pivot := mk("pv", contract.KindPivot, "open")
	errand := mk("er", contract.KindErrand, "open")
	paused := func(b bd.Bead, causes ...string) bd.Bead {
		for _, c := range causes {
			b.Labels = append(b.Labels, "paused:"+c)
		}
		return b
	}
	always := func(bd.Bead) bool { return true }

	type tc struct {
		name     string
		b        bd.Bead
		mode     Mode
		frontier []bd.Bead // defaults to {b}
		in       Inputs
		view     fakeView
		want     Verdict // Holders compared by id; Foreign by pointer
		wantIDs  []string
		wantErr  bool
	}
	approved := func(sha string) bd.Bead { return approval(cp, "ap", "closed", bd.ApprovedReason, sha) }
	atTip := fakeView{tips: map[string]string{"cp": "tip1"}}
	both := []Mode{ModeLoop, ModeRunNow}
	var cases []tc
	add := func(c tc, modes ...Mode) {
		if len(modes) == 0 {
			modes = both
		}
		for _, m := range modes {
			c.mode = m
			cases = append(cases, c)
		}
	}
	with := func(v fakeView, f func(*fakeView)) fakeView {
		cp := v
		f(&cp)
		return cp
	}
	cpKids := func(v fakeView, kids ...bd.Bead) fakeView {
		return with(v, func(v *fakeView) { v.kids = map[string][]bd.Bead{"cp": kids} })
	}
	chKids := func(kids ...bd.Bead) fakeView { return fakeView{kids: map[string][]bd.Bead{"ch": kids}} }

	// not-candidate
	add(tc{name: "closed leaf", b: mk("x", contract.KindResearch, "closed"), want: Verdict{Cause: CauseNotCandidate}})
	add(tc{name: "hitl kind", b: mk("x", contract.KindGrilling, "open"), want: Verdict{Cause: CauseNotCandidate}})

	// runnable actions
	add(tc{name: "leaf run", b: leaf, want: Verdict{Runnable: true, Action: ActionRun}})
	add(tc{name: "drained chart spec", b: chart, view: chKids(mk("s", contract.KindSpec, "closed")), want: Verdict{Runnable: true, Action: ActionSpec}})
	add(tc{name: "checkpoint land at head", b: cp, view: cpKids(with(atTip, func(v *fakeView) {}), closedBuild, approved("tip1")),
		want: Verdict{Runnable: true, Action: ActionLand}})
	add(tc{name: "checkpoint review", b: cp, view: cpKids(atTip, closedBuild), want: Verdict{Runnable: true, Action: ActionReview}}, ModeLoop)
	add(tc{name: "checkpoint review-due refuses in run-now", b: cp, view: cpKids(atTip, closedBuild), want: Verdict{Cause: CauseReviewDue}}, ModeRunNow)

	// paused
	add(tc{name: "paused manual", b: paused(leaf, "manual"), want: Verdict{Cause: CausePaused, PauseCause: "manual"}})
	add(tc{name: "paused failing refuses in loop", b: paused(leaf, "failing"), want: Verdict{Cause: CausePaused, PauseCause: "failing"}}, ModeLoop)
	add(tc{name: "paused failing retries in run-now", b: paused(leaf, "failing"), want: Verdict{Runnable: true, Action: ActionRetry}}, ModeRunNow)
	add(tc{name: "paused foreign-hold retry hold", b: paused(leaf, "foreign-hold"), want: Verdict{Runnable: true, Action: ActionRetryHold}}, ModeRunNow)
	add(tc{name: "paused unmet refuses in loop", b: paused(leaf, "unmet"), want: Verdict{Cause: CausePaused, PauseCause: "unmet"}}, ModeLoop)
	add(tc{name: "paused unmet retries in run-now", b: paused(leaf, "unmet"), want: Verdict{Runnable: true, Action: ActionRetry}}, ModeRunNow)
	add(tc{name: "all pass causes retry", b: paused(leaf, "not-built", "integrate-conflict", "profile"), want: Verdict{Runnable: true, Action: ActionRetry}}, ModeRunNow)
	add(tc{name: "mixed causes refuse with top-ranked", b: paused(leaf, "failing", "manual"), want: Verdict{Cause: CausePaused, PauseCause: "manual"}}, ModeRunNow)
	add(tc{name: "paused checkup refuses in run-now", b: paused(leaf, "checkup"), want: Verdict{Cause: CausePaused, PauseCause: "checkup"}})
	add(tc{name: "unknown pause cause refuses verbatim", b: paused(leaf, "wat"), want: Verdict{Cause: CausePaused, PauseCause: "wat"}})
	add(tc{name: "retry applies to a chart arm", b: paused(chart, "failing"), view: chKids(), want: Verdict{Runnable: true, Action: ActionRetry}}, ModeRunNow)
	add(tc{name: "retry does not skip the arm", b: paused(chart, "failing"), view: chKids(mk("o", contract.KindSpec, "open")), want: Verdict{Cause: CauseUndrained}, wantIDs: []string{"o"}}, ModeRunNow)

	// inherited-pause
	under := leaf
	under.Parent = "ini"
	inherit := fakeView{beads: map[string]bd.Bead{"ini": paused(mk("ini", contract.KindInitiative, "open"), "manual")}}
	add(tc{name: "inherited manual pause", b: under, view: inherit, want: Verdict{Cause: CauseInheritedPause}, wantIDs: []string{"ini"}})
	deep := under
	deep.Parent = "mid"
	add(tc{name: "inherited pause via a walk miss", b: deep, view: with(inherit, func(v *fakeView) {
		mid := mk("mid", contract.KindInitiative, "open")
		mid.Parent = "ini"
		v.beads = map[string]bd.Bead{"ini": inherit.beads["ini"], "mid": mid}
	}), want: Verdict{Cause: CauseInheritedPause}, wantIDs: []string{"ini"}})
	add(tc{name: "inherited pause outranks container-frozen", b: under, view: inherit, want: Verdict{Cause: CauseInheritedPause}, wantIDs: []string{"ini"}})

	// precedence: paused before blocked
	add(tc{name: "paused outranks blocked", b: paused(leaf, "manual"), frontier: []bd.Bead{}, want: Verdict{Cause: CausePaused, PauseCause: "manual"}})

	// container-frozen
	frozenView := fakeView{
		beads:    map[string]bd.Bead{"cp": cp},
		blockers: map[string][]Blocker{"cp": {blk(pivot), blk(errand)}},
	}
	add(tc{name: "member of a pivot-frozen checkpoint", b: build2(leaf, "cp"), view: frozenView, want: Verdict{Cause: CauseContainerFrozen}, wantIDs: []string{"pv"}})
	add(tc{name: "land-only blocker does not freeze", b: build2(leaf, "cp"), view: with(frozenView, func(v *fakeView) { v.blockers = map[string][]Blocker{"cp": {blk(errand)}} }),
		want: Verdict{Runnable: true, Action: ActionRun}})
	add(tc{name: "force does not waive container-frozen", b: build2(leaf, "cp"), view: frozenView, in: Inputs{Force: true}, want: Verdict{Cause: CauseContainerFrozen}, wantIDs: []string{"pv"}}, ModeRunNow)
	add(tc{name: "frozen outranks blocked", b: build2(leaf, "cp"), frontier: []bd.Bead{}, view: frozenView, want: Verdict{Cause: CauseContainerFrozen}, wantIDs: []string{"pv"}})

	// blocked
	ownBlock := fakeView{blockers: map[string][]Blocker{"rs": {blk(errand), {Bead: pivot, Edge: "conditional-blocks"}, blk(mk("done", contract.KindErrand, "closed"))}}}
	add(tc{name: "outside the frontier is blocked with own open blockers", b: leaf, frontier: []bd.Bead{}, view: ownBlock, want: Verdict{Cause: CauseBlocked}, wantIDs: []string{"er"}})
	add(tc{name: "force waives blocked", b: leaf, frontier: []bd.Bead{}, view: ownBlock, in: Inputs{Force: true}, want: Verdict{Runnable: true, Action: ActionRun}}, ModeRunNow)
	add(tc{name: "force is ignored in loop mode", b: leaf, frontier: []bd.Bead{}, view: ownBlock, in: Inputs{Force: true}, want: Verdict{Cause: CauseBlocked}, wantIDs: []string{"er"}}, ModeLoop)
	add(tc{name: "force does not waive paused", b: paused(leaf, "manual"), frontier: []bd.Bead{}, in: Inputs{Force: true}, want: Verdict{Cause: CausePaused, PauseCause: "manual"}}, ModeRunNow)
	add(tc{name: "force does not waive undrained", b: chart, frontier: []bd.Bead{}, in: Inputs{Force: true}, view: chKids(mk("o", contract.KindSpec, "open")), want: Verdict{Cause: CauseUndrained}, wantIDs: []string{"o"}}, ModeRunNow)
	inChart := leaf
	inChart.Parent = "ch"
	chartBlocked := fakeView{
		beads:    map[string]bd.Bead{"ch": chart},
		blockers: map[string][]Blocker{"ch": {blk(errand)}},
	}
	add(tc{name: "inherits a chart's open blocker", b: inChart, frontier: []bd.Bead{}, view: chartBlocked, want: Verdict{Cause: CauseBlocked}, wantIDs: []string{"er"}})

	// paced
	add(tc{name: "paced in loop", b: leaf, in: Inputs{Paced: always}, want: Verdict{Cause: CausePaced}}, ModeLoop)
	add(tc{name: "run-now never sees pacing", b: leaf, in: Inputs{Paced: always}, want: Verdict{Runnable: true, Action: ActionRun}}, ModeRunNow)
	add(tc{name: "blocked outranks paced", b: leaf, frontier: []bd.Bead{}, in: Inputs{Paced: always}, want: Verdict{Cause: CauseBlocked}}, ModeLoop)

	// surgery-hold
	critBeads := fakeView{kids: map[string][]bd.Bead{"cp": {child(cp, "sg", contract.KindSurgery, "open"), child(cp, "sg2", contract.KindSurgery, "closed")}}, beads: map[string]bd.Bead{"cp": cp}}
	add(tc{name: "critique held by an open surgery", b: crit, view: critBeads, want: Verdict{Cause: CauseSurgeryHold}, wantIDs: []string{"sg"}})
	add(tc{name: "critique free once surgeries close", b: crit, view: with(critBeads, func(v *fakeView) {
		v.kids = map[string][]bd.Bead{"cp": {child(cp, "sg2", contract.KindSurgery, "closed")}}
	}),
		want: Verdict{Runnable: true, Action: ActionRun}})

	// undrained chart
	add(tc{name: "chart with an open child", b: chart, view: chKids(mk("o", contract.KindSpec, "open")), want: Verdict{Cause: CauseUndrained}, wantIDs: []string{"o"}})
	add(tc{name: "chart held closed child", b: chart, view: with(chKids(mk("h", contract.KindSpec, "closed")), func(v *fakeView) { v.efforts = &git.EffortSet{Branches: map[string]bool{"h": true}} }),
		want: Verdict{Cause: CauseUndrained}, wantIDs: []string{"h"}})
	add(tc{name: "chart closed child integrated is not held", b: chart, view: with(chKids(mk("h", contract.KindSpec, "closed")), func(v *fakeView) {
		v.efforts = &git.EffortSet{Branches: map[string]bool{"h": true}, Integrated: map[string]bool{"h": true}}
	}), want: Verdict{Runnable: true, Action: ActionSpec}})
	add(tc{name: "container child undrains a chart", b: chart, view: chKids(mk("ini", contract.KindInitiative, "open")), want: Verdict{Cause: CauseUndrained}, wantIDs: []string{"ini"}})
	add(tc{name: "kind-less child undrains a chart", b: chart, view: chKids(mk("k", "", "open")), want: Verdict{Cause: CauseUndrained}, wantIDs: []string{"k"}})
	add(tc{name: "open mr-watch does not undrain", b: chart, view: chKids(mk("w", contract.KindMRWatch, "open")), want: Verdict{Runnable: true, Action: ActionSpec}})
	chApproval := func(status, reason, body string) bd.Bead {
		a := child(chart, "ap", contract.KindApproval, status)
		a.CloseReason, a.Description = reason, body
		return a
	}
	add(tc{name: "chart with an open approval", b: chart, view: chKids(chApproval("open", "", pin("tip1"))), want: Verdict{Cause: CauseUndrained}, wantIDs: []string{"ap"}})
	add(tc{name: "chart with an open approval awaiting un-draft", b: chart, view: chKids(chApproval("open", "", pin("tip1")+"\nMR: !7")), want: Verdict{Cause: CauseUndrained}, wantIDs: []string{"ap"}})
	add(tc{name: "force does not waive a chart's open approval", b: chart, in: Inputs{Force: true}, view: chKids(chApproval("open", "", pin("tip1"))), want: Verdict{Cause: CauseUndrained}, wantIDs: []string{"ap"}}, ModeRunNow)
	add(tc{name: "force does not waive a chart's awaiting-un-draft approval", b: chart, in: Inputs{Force: true}, view: chKids(chApproval("open", "", pin("tip1")+"\nMR: !7")), want: Verdict{Cause: CauseUndrained}, wantIDs: []string{"ap"}}, ModeRunNow)
	add(tc{name: "chart whose approval closed approved lands as spec", b: chart, view: chKids(chApproval("closed", bd.ApprovedReason, pin("tip1"))), want: Verdict{Runnable: true, Action: ActionSpec}})
	add(tc{name: "open landing on a drained chart", b: chart, view: chKids(mk("l", contract.KindLanding, "open")), want: Verdict{Cause: CauseLandingOpen}, wantIDs: []string{"l"}})
	add(tc{name: "landing open but trailer on mainline backstops", b: chart, view: with(chKids(mk("l", contract.KindLanding, "open")), func(v *fakeView) { v.landed = map[string]Trailer{"ch": {Sha: "s", Landed: true}} }),
		want: Verdict{Runnable: true, Action: ActionSpec}})
	add(tc{name: "foreign landing on a chart", b: chart, view: with(chKids(), func(v *fakeView) { v.landed = map[string]Trailer{"ch": {Foreign: foreign}} }),
		want: Verdict{Cause: CauseForeignLanding, Foreign: foreign}})
	add(tc{name: "foreign-hold pause is not foreign landing", b: paused(chart, "foreign-hold"), view: chKids(), want: Verdict{Runnable: true, Action: ActionRetryHold}}, ModeRunNow)

	// checkpoint
	open := func(id string, k contract.Kind, s string) bd.Bead { return child(cp, id, k, s) }
	add(tc{name: "in_progress critique is defect-open", b: cp, view: cpKids(atTip, open("c1", contract.KindCritique, "in_progress")), want: Verdict{Cause: CauseDefectOpen}, wantIDs: []string{"c1"}})
	add(tc{name: "deferred conflict is defect-open", b: cp, view: cpKids(atTip, open("c2", contract.KindConflict, "deferred")), want: Verdict{Cause: CauseDefectOpen}, wantIDs: []string{"c2"}})
	add(tc{name: "defect-open outranks foreign landing", b: cp, view: with(cpKids(atTip, open("c1", contract.KindCritique, "open")), func(v *fakeView) { v.landed = map[string]Trailer{"cp": {Foreign: foreign}} }),
		want: Verdict{Cause: CauseDefectOpen}, wantIDs: []string{"c1"}})
	add(tc{name: "foreign landing on a checkpoint", b: cp, view: with(cpKids(atTip, closedBuild), func(v *fakeView) { v.landed = map[string]Trailer{"cp": {Foreign: foreign}} }),
		want: Verdict{Cause: CauseForeignLanding, Foreign: foreign}})
	add(tc{name: "landed trailer rolls forward to land", b: cp, view: with(cpKids(atTip, open("l", contract.KindLanding, "open"), build("b2", "open")), func(v *fakeView) { v.landed = map[string]Trailer{"cp": {Sha: "s", Landed: true}} }),
		want: Verdict{Runnable: true, Action: ActionLand}})
	add(tc{name: "checkpoint landing open", b: cp, view: cpKids(atTip, open("l", contract.KindLanding, "open"), closedBuild, approved("tip1")), want: Verdict{Cause: CauseLandingOpen}, wantIDs: []string{"l"}})
	add(tc{name: "approved at head, member open", b: cp, view: cpKids(atTip, build("b2", "open"), approved("tip1")), want: Verdict{Cause: CauseUndrained}, wantIDs: []string{"b2"}})
	add(tc{name: "approved at head, member held", b: cp, view: with(cpKids(atTip, closedBuild, approved("tip1")), func(v *fakeView) { v.efforts = &git.EffortSet{Branches: map[string]bool{"b1": true}} }),
		want: Verdict{Cause: CauseUndrained}, wantIDs: []string{"b1"}})
	add(tc{name: "approved at head, member integrated is not held", b: cp, view: with(cpKids(atTip, closedBuild, approved("tip1")), func(v *fakeView) {
		v.efforts = &git.EffortSet{Branches: map[string]bool{"b1": true}, Integrated: map[string]bool{"b1": true}}
	}), want: Verdict{Runnable: true, Action: ActionLand}})
	add(tc{name: "approved at head, incoming blocker", b: cp, view: with(cpKids(atTip, closedBuild, approved("tip1")), func(v *fakeView) { v.blockers = map[string][]Blocker{"cp": {blk(errand)}} }),
		want: Verdict{Cause: CauseLandHeld}, wantIDs: []string{"er"}})
	add(tc{name: "approval not at the head awaits a review", b: cp, view: cpKids(atTip, closedBuild, approved("old")), want: Verdict{Runnable: true, Action: ActionReview}}, ModeLoop)
	add(tc{name: "approval not at the head is not landable in run-now", b: cp, view: cpKids(atTip, closedBuild, approved("old")), want: Verdict{Cause: CauseReviewDue}}, ModeRunNow)
	add(tc{name: "unknown tip pins nothing", b: cp, view: cpKids(fakeView{}, closedBuild, approved("tip1")), want: Verdict{Runnable: true, Action: ActionReview}}, ModeLoop)
	add(tc{name: "open pivot", b: cp, view: with(cpKids(atTip, closedBuild), func(v *fakeView) { v.blockers = map[string][]Blocker{"cp": {blk(pivot)}} }), want: Verdict{Cause: CausePivotOpen}, wantIDs: []string{"pv"}})
	add(tc{name: "unapproved with an open member", b: cp, view: cpKids(atTip, build("b2", "open")), want: Verdict{Cause: CauseUndrained}, wantIDs: []string{"b2"}})
	add(tc{name: "open approval at the head awaits", b: cp, view: cpKids(atTip, closedBuild, approval(cp, "ap", "open", "", "tip1")), want: Verdict{Cause: CauseAwaitingApproval}, wantIDs: []string{"ap"}})
	add(tc{name: "no members", b: cp, view: cpKids(atTip), want: Verdict{Cause: CauseNoMembers}})
	add(tc{name: "pivot outranks undrained", b: cp, view: with(cpKids(atTip, build("b2", "open")), func(v *fakeView) { v.blockers = map[string][]Blocker{"cp": {blk(pivot)}} }), want: Verdict{Cause: CausePivotOpen}, wantIDs: []string{"pv"}})

	// errors
	for _, m := range []string{"Children", "Efforts", "Landed", "CheckpointTip", "Blockers", "Bead"} {
		v := with(cpKids(atTip, closedBuild), func(v *fakeView) {
			v.err = m
			v.beads = map[string]bd.Bead{"ini": mk("ini", contract.KindInitiative, "open"), "cp": cp}
		})
		b := cp
		b.Parent = "ini"
		var fr []bd.Bead // nil: the bead is in the frontier
		if m == "Blockers" {
			fr = []bd.Bead{} // outside it, so the blocked leg reads
		}
		add(tc{name: "error from " + m, b: b, frontier: fr, view: v, wantErr: true})
	}

	for _, c := range cases {
		t.Run(c.name+"/"+map[Mode]string{ModeLoop: "loop", ModeRunNow: "run-now"}[c.mode], func(t *testing.T) {
			frontier := c.frontier
			if frontier == nil {
				frontier = []bd.Bead{c.b}
			}
			got, err := Judge(c.b, c.mode, frontier, c.in, c.view)
			if c.wantErr {
				if err == nil || got.Runnable || got.Cause != "" {
					t.Fatalf("got (%+v, %v), want an error and no verdict", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Runnable != c.want.Runnable || got.Action != c.want.Action || got.Cause != c.want.Cause ||
				got.PauseCause != c.want.PauseCause || got.Foreign != c.want.Foreign {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
			if c.wantIDs != nil && !slices.Equal(ids(got.Holders), c.wantIDs) {
				t.Errorf("holders %v, want %v", ids(got.Holders), c.wantIDs)
			}
			if c.wantIDs == nil && c.want.Cause == "" && len(got.Holders) != 0 {
				t.Errorf("runnable verdict has holders %v", ids(got.Holders))
			}
		})
	}
}

// build2 is b re-parented under parent.
func build2(b bd.Bead, parent string) bd.Bead {
	b.Parent = parent
	return b
}

func TestJudgeHolderBranches(t *testing.T) {
	chart := mk("ch", contract.KindChart, "open")
	v := fakeView{kids: map[string][]bd.Bead{"ch": {mk("h", contract.KindSpec, "closed"), mk("o", contract.KindSpec, "open")}}, efforts: &git.EffortSet{Branches: map[string]bool{"h": true}}}
	got, err := Judge(chart, ModeRunNow, []bd.Bead{chart}, Inputs{}, v)
	if err != nil || got.Cause != CauseUndrained || len(got.Holders) != 2 {
		t.Fatalf("got %+v, %v", got, err)
	}
	for _, h := range got.Holders {
		if want := map[string]string{"o": "", "h": "effort/h"}[h.Bead.ID]; h.Branch != want {
			t.Errorf("holder %s branch %q, want %q", h.Bead.ID, h.Branch, want)
		}
	}
}

func TestCausesIsClosedAt16(t *testing.T) {
	seen := map[Cause]bool{}
	for _, c := range Causes {
		seen[c] = true
	}
	if len(Causes) != 16 || len(seen) != 16 {
		t.Errorf("Causes has %d entries, %d distinct, want 16", len(Causes), len(seen))
	}
}
