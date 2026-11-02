package zoro

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/bd/bdtest"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/promptguard"
)

// recordingHarness is the fake contract.Harness the reconcileSpec tests dispatch
// through: it records every AFK invocation and can be made to fail. No live
// Claude Code. Mirrors nami_test's recordingHarness.
type recordingHarness struct {
	invs []contract.Invocation
	err  error  // non-nil → the slicing session failed
	lay  func() // called on Run to model the fork laying Builds onto the Checkpoint

	launched contract.Invocation // the HITL Invocation Launch composed
}

func (h *recordingHarness) Run(_ contract.Ctx, inv contract.Invocation) (contract.Result, error) {
	h.invs = append(h.invs, inv)
	if h.lay != nil {
		h.lay()
	}
	return contract.Result{}, h.err
}
func (h *recordingHarness) LaunchCommand(inv contract.Invocation) (contract.LaunchCommand, error) {
	h.launched = inv
	return contract.AgentCommand{}, nil
}

func (h *recordingHarness) ResumeCommand(string, bool) (contract.AgentCommand, error) {
	return contract.AgentCommand{}, nil
}

type stubCtx struct{ h contract.Harness }

func (c stubCtx) Harness() contract.Harness { return c.h }

// specSeamRec captures the git legs reconcileSpec drives (base trailer, cut,
// branch delete): the bd legs run against a seeded bdtest graph instead.
type cutCall struct{ name, base string }
type specSeamRec struct {
	trailers   []string  // landedBase: trailers grepped
	cuts       []cutCall // cutBranch: (name, base)
	deleted    []string  // branchOutcome: branches asked to delete
	branch     bd.BranchOutcome
	branchSeen bool // branchOutcome: whether the branch existed
}

// stubSpecSeams seeds the dispatched Spec, claimed as its dispatcher holds it, and
// (when found) the Chart it is caused-by, into a bdtest graph, and swaps the git
// legs for inert fakes: landedBase returns "landsha" unless baseErr is set (the
// missing-anchor hard fail). Restores the real funcs on cleanup.
func stubSpecSeams(t *testing.T, r *specSeamRec, chartID string, found bool, baseErr error) *bdtest.Fake {
	t.Helper()
	spec := openSpec()
	seeds := []bd.Bead{}
	if found {
		spec.CausedBy = chartID
		seeds = append(seeds, bd.Bead{ID: chartID, Kind: contract.KindChart, Status: "closed"})
	}
	f := bdtest.New(t, append(seeds, bdtest.Claimed(spec))...)
	oLand, oCut, oBranch := landedBase, cutBranch, branchOutcome
	t.Cleanup(func() { landedBase, cutBranch, branchOutcome = oLand, oCut, oBranch })
	landedBase = func(trailer string) (string, error) {
		r.trailers = append(r.trailers, trailer)
		if baseErr != nil {
			return "", baseErr
		}
		return "landsha", nil
	}
	cutBranch = func(name, base string) (bool, error) {
		r.cuts = append(r.cuts, cutCall{name, base})
		return true, nil
	}
	branchOutcome = func(_ git.Repo, branch string) (bd.BranchOutcome, bool) {
		r.deleted = append(r.deleted, branch)
		return r.branch, r.branchSeen
	}
	return f
}

func openSpec() bd.Bead {
	return bd.Bead{ID: "ws-spec", Kind: contract.KindSpec, Status: "open", Title: "a spec"}
}

// cpOf is the Checkpoint the Spec spawned, read back off the graph.
func cpOf(t *testing.T) string {
	t.Helper()
	id, ok, err := bd.SuccessorOf("ws-spec", contract.KindCheckpoint)
	if err != nil || !ok {
		t.Fatalf("the Spec has no Checkpoint successor: %q %v %v", id, ok, err)
	}
	return id
}

// seedCheckpoint is a Checkpoint a prior pass already spawned for the Spec.
func seedCheckpoint(f *bdtest.Fake) string {
	f.Seed(bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open", Container: true, Title: "a spec",
		Labels: []string{"kind:checkpoint"}, CausedBy: "ws-spec"})
	return "ws-cp"
}

// layBuilds is the slicing fork laying n Builds onto the Spec's Checkpoint.
func layBuilds(t *testing.T, f *bdtest.Fake, n int) {
	t.Helper()
	cp := cpOf(t)
	for i := range n {
		f.Seed(bd.Bead{ID: fmt.Sprintf("ws-b%d", i), Kind: contract.KindBuild, Status: "deferred", Parent: cp, Labels: []string{"kind:build"}})
	}
}

// layErrands is the Spec holding n Errands over discovered-from.
func layErrands(f *bdtest.Fake, n int) {
	for i := range n {
		f.Seed(bd.Bead{ID: fmt.Sprintf("ws-e%d", i), Kind: contract.KindErrand, Status: "deferred",
			Labels: []string{"kind:errand"}, DiscoveredFrom: []string{"ws-spec"}})
	}
}

// specClosure is how the Spec ended: its status and close reason.
func specClosure(f *bdtest.Fake) (status, reason string) {
	b := f.Bead("ws-spec")
	return b.Status, b.CloseReason
}

// var _ contract.Crewmate = Zoro{} is asserted in zoro.go; Launch errors for
// every kind but pivot (the one launchable HITL kind, ADR 0046).
func TestLaunchErrorsForNonPivotKinds(t *testing.T) {
	for _, k := range []contract.Kind{contract.KindSpec, contract.KindCheckpoint, contract.KindSignal} {
		if _, err := (Zoro{}).Launch(stubCtx{&recordingHarness{}}, contract.Bead{ID: "x", Kind: k}); err == nil {
			t.Errorf("Launch of kind %q must error (not a launchable HITL kind)", k)
		}
	}
}

// seedPivot seeds the claimed pivot ws-pv into a bdtest graph with the Checkpoint it
// blocks and the Spec it is related to; an empty id leaves that edge out.
func seedPivot(t *testing.T, cp, spec string) *bdtest.Fake {
	t.Helper()
	pv := bdtest.Claimed(bd.Bead{ID: "ws-pv", Kind: contract.KindPivot, Status: "open", Labels: []string{"kind:pivot"}})
	if spec != "" {
		pv.Related = []string{spec}
	}
	f := bdtest.New(t, pv)
	if spec != "" {
		f.Seed(bd.Bead{ID: spec, Kind: contract.KindSpec, Status: "closed", Labels: []string{"kind:spec"}})
	}
	if cp != "" {
		f.Seed(bd.Bead{ID: cp, Kind: contract.KindCheckpoint, Status: "open", Container: true, Labels: []string{"kind:checkpoint"}, BlockedBy: []string{"ws-pv"}})
	}
	return f
}

// Launch(pivot) → one session anchored to the pivot whose prompt carries the
// pivot, Go-resolved Checkpoint and Spec ids, the six-step flow, and the
// dismiss/amend legs with their check-before-write guards (ADR 0046 §2–4).
func TestLaunchPivotComposesSession(t *testing.T) {
	seedPivot(t, "ws-cp", "ws-sp")
	h := &recordingHarness{}
	if _, err := (Zoro{}).Launch(stubCtx{h}, contract.Bead{ID: "ws-pv", Kind: contract.KindPivot}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if h.launched.BeadID != "ws-pv" {
		t.Errorf("Invocation must anchor to the pivot, got %q", h.launched.BeadID)
	}
	p := h.launched.Prompt
	for _, want := range []string{
		"bd update ws-pv --claim",                          // 1. claim the lead
		"bd dep list ws-cp --direction=down -t blocks",     // 2. every open pivot on the Checkpoint
		"-t discovered-from",                               // 2. each raising bead
		"bd show ws-sp",                                    // 2. the Spec with its Amendments
		"..checkpoint/ws-cp",                               // 2. the built diff
		"intent holds", "already-built work becomes wrong", // 3. the amend line
		"together",                                                           // 4. decide all together
		"Re-read the open pivots",                                            // 5. newcomers first
		"mugicrew pivot dismiss <pivotID>", "mugicrew pivot amend <pivotID>", // dismiss/amend verbs
		"`dismissed: <gist>`", // dismiss close
		`--amendment "<FR> — <decision> — rejected: <options>"`,
		"the Spec (ws-sp)",  // amend writes the Amendment on the Spec
		"`amended: <gist>`", // amend close
		"Check before each write",
		"git worktree add",    // ADR only in its own worktree
		"git worktree remove", // ...cleaned up after it lands
		"a fast-laned Spec has none — nothing is superseded",       // escalate on a fast-laned Spec
		"cuts its `chart/` branch",                                 // escalate: the fresh Chart's branch
		"fresh `Anchor:` pin on the Spec bead",                     // confirm: a fast-laned Spec's lineage
		"The loop slices",                                          // confirm: the new Spec is sliced
		"already in the body",                                      // why the lead pivot still sweeps
		"folded into the FR it names", "starts with no Amendments", // confirm: the corrected body (FR39)
		"sibling pivots and by the raising beads", // confirm: carried FRs (FR39)
		"mktemp", "never write or commit there", // the body never dirties main
		`mugicrew pivot confirm ws-pv --spec "$body"`,          // confirm: the sweep verb (FR42), on the lead — the Spec Draft hitl-run publishes
		"re-run the same command",                              // FR48
		`mugicrew pivot escalate ws-pv --evidence "$evidence"`, // escalate: the sweep verb (FR43), on the lead likewise
		"write no dismiss, amend or confirm",                   // an escalate settles every pivot
	} {
		if !strings.Contains(p, want) {
			t.Errorf("pivot prompt missing %q", want)
		}
	}
	if strings.Contains(p, "NOT YET WIRED") || strings.Contains(p, "PLACEHOLDER") {
		t.Error("pivot prompt still carries the escalate placeholder")
	}
	if strings.Contains(p, "%!") {
		t.Errorf("pivot prompt has a stray %%: %q", p)
	}
}

// The pivot prompt makes the session self-contained: it never
// points at a bead in place of content, renders one fixed-shape pivot card per
// open pivot (late ones too) and a decision summary table, and shows a confirm
// or escalate body verbatim, again after every edit, before an explicit yes.
func TestLaunchPivotPresentsSelfContainedCards(t *testing.T) {
	seedPivot(t, "ws-cp", "ws-sp")
	h := &recordingHarness{}
	if _, err := (Zoro{}).Launch(stubCtx{h}, contract.Bead{ID: "ws-pv", Kind: contract.KindPivot}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	p := h.launched.Prompt
	for _, want := range []string{
		"Never point, always quote",                         // FR 17, FR 1
		"never send the human to a bead id or to `bd show`", // FR 17
		"pivot card",             // FR 1, 11
		"decision summary table", // FR 12
		"| Pivot | FR | Recommended outcome | Reason |", // FR 12
		"gets a full pivot card",                        // FR 13
		"never the whole diff unasked",                  // FR 9
		"the amended mechanism",                         // FR 3 amend After
		"the corrected FR",                              // FR 3 confirm After
		"the Critique refiled",                          // FR 3 dismiss After
		"this Spec retires; charting re-opens",          // FR 3 escalate After
		"the corrected Spec body verbatim",              // FR 14
		"before → after",                                // FR 14
		"the evidence body verbatim",                    // FR 15
		"again, verbatim, after every edit",             // FR 16
		"explicit yes",                                  // FR 16
	} {
		if !strings.Contains(p, want) {
			t.Errorf("pivot prompt missing %q", want)
		}
	}
	// FR 2–11: the card fields, in their fixed order.
	at := 0
	for _, field := range []string{
		"**Before**", "**After**", "**Consequence**", "**Evidence**", "**Raised by**",
		"**Amendments on this FR**", "**Built so far**", "**Recommendation**",
	} {
		i := strings.Index(p[at:], field)
		if i < 0 {
			t.Errorf("pivot card missing %s after offset %d (fields must be in order)", field, at)
			break
		}
		at += i + len(field)
	}
	// The confirm and escalate approval rules each sit ahead of their sweep.
	for _, c := range []struct{ rule, sweep string }{
		{"the corrected Spec body verbatim", "mugicrew pivot confirm ws-pv"},
		{"the evidence body verbatim", "mugicrew pivot escalate ws-pv"},
	} {
		if r, s := strings.Index(p, c.rule), strings.Index(p, c.sweep); r < 0 || s < 0 || r > s {
			t.Errorf("%q must come before %q", c.rule, c.sweep)
		}
	}
}

// A pivot whose Checkpoint or Spec cannot be resolved is a loud error, never a
// session with a blank id.
func TestLaunchPivotUnresolvedErrors(t *testing.T) {
	for _, c := range []struct{ cp, spec string }{{"", "ws-sp"}, {"ws-cp", ""}} {
		seedPivot(t, c.cp, c.spec)
		if _, err := (Zoro{}).Launch(stubCtx{&recordingHarness{}}, contract.Bead{ID: "ws-pv", Kind: contract.KindPivot}); err == nil {
			t.Errorf("cp=%q spec=%q: Launch must error", c.cp, c.spec)
		}
	}
}

// Acceptance (a)-(e): a Chart-backed Spec spawns its Checkpoint under the Spec's
// claim, derives the base from the Chart's landed base, cuts checkpoint/<cp> from
// that derived SHA (never main), forks the slicing session anchored to the Spec,
// and closes the Spec after the fork laid a Build.
func TestReconcileSpecChartBackedCutsFromChartLandedBase(t *testing.T) {
	r := &specSeamRec{}
	f := stubSpecSeams(t, r, "ws-chart", true, nil)
	h := &recordingHarness{lay: func() { layBuilds(t, f, 1) }}
	f.Mutate("ws-spec", func(b *bd.Bead) { b.Priority = 4 })

	if err := reconcileSpec(stubCtx{h}, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	cp := cpOf(t)
	if got := f.Bead(cp).Priority; got != 4 {
		t.Errorf("Checkpoint priority = P%d, want the Spec's own P4", got)
	}
	// The Checkpoint is a container kind:checkpoint epic caused-by the Spec.
	b := f.Bead(cp)
	if b.CausedBy != "ws-spec" || b.Kind != contract.KindCheckpoint || !b.Container || b.Title != "a spec" || !b.HasLabel("kind:checkpoint") {
		t.Errorf("Checkpoint = %+v, want a kind:checkpoint epic caused-by ws-spec titled like the Spec", b)
	}
	// (a) base derived by walking Spec→Chart and grepping the Chart trailer.
	if len(r.trailers) != 1 || r.trailers[0] != "Chart: ws-chart" {
		t.Errorf("Chart-backed Spec must grep the Chart landed base, got %v", r.trailers)
	}
	// (c) branch cut from the derived SHA, never main.
	if len(r.cuts) != 1 {
		t.Fatalf("must cut exactly one branch, got %v", r.cuts)
	}
	if r.cuts[0].name != "checkpoint/"+cp {
		t.Errorf("branch = %q, want checkpoint/%s", r.cuts[0].name, cp)
	}
	if r.cuts[0].base != "landsha" || r.cuts[0].base == git.Mainline() {
		t.Errorf("branch must cut from the derived SHA, never main, got %q", r.cuts[0].base)
	}
	// (d) slicing session forked, anchored to the Spec, prompt names it.
	if len(h.invs) != 1 || h.invs[0].BeadID != "ws-spec" {
		t.Fatalf("slicing session must fork once anchored to the Spec, got %v", h.invs)
	}
	if !strings.Contains(h.invs[0].Prompt, "ws-spec") {
		t.Errorf("slicing prompt must name the Spec, got %q", h.invs[0].Prompt)
	}
	// (e) Spec closed after the fork laid a Build (the artifact exists, FR22).
	if st, why := specClosure(f); st != "closed" || why != "sliced: "+cp {
		t.Errorf("the Spec must close sliced: <checkpoint>, got %q %q", st, why)
	}
}

// The Checkpoint spawn is serialized by the dispatched Spec's claim: without it
// the fake refuses the spawn and the reconcile fails before any cut.
func TestReconcileSpecSpawnRefusedWithoutTheSpecsClaim(t *testing.T) {
	rec := &errRecorder{TB: t}
	bdtest.New(rec, openSpec()) // open, unassigned: the dispatcher's claim is not held
	r := &specSeamRec{}
	oLand, oCut := landedBase, cutBranch
	t.Cleanup(func() { landedBase, cutBranch = oLand, oCut })
	landedBase = func(string) (string, error) { return "landsha", nil }
	cutBranch = func(n, b string) (bool, error) { r.cuts = append(r.cuts, cutCall{n, b}); return true, nil }

	if err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec()); err == nil {
		t.Fatal("a Checkpoint spawn without the Spec's claim must fail")
	}
	if len(rec.errs) != 1 || !strings.Contains(rec.errs[0], "claim on ws-spec") {
		t.Errorf("the fake must flag the unserialized spawn, got %v", rec.errs)
	}
	if _, ok, _ := bd.SuccessorOf("ws-spec", contract.KindCheckpoint); ok || len(r.cuts) != 0 {
		t.Errorf("a refused spawn leaves no Checkpoint (%v) and cuts nothing (%v)", ok, r.cuts)
	}
}

// errRecorder is a testing.TB that records Errorf instead of failing the test.
type errRecorder struct {
	testing.TB
	errs []string
}

func (e *errRecorder) Errorf(format string, args ...any) {
	e.errs = append(e.errs, fmt.Sprintf(format, args...))
}

// A failed caused-by read is an error, not a wrong base: nothing is spawned or cut.
func TestReconcileSpecFailedCausedByReadSurfaces(t *testing.T) {
	r := &specSeamRec{}
	f := stubSpecSeams(t, r, "ws-chart", true, nil)
	f.FailOn("Edges", "ws-spec", errors.New("dolt down"))

	err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec())
	if err == nil || !strings.Contains(err.Error(), "dolt down") {
		t.Fatalf("err = %v, want the failed read", err)
	}
	if len(r.trailers)+len(r.cuts) != 0 {
		t.Errorf("a failed read must derive no base (%v) and cut nothing (%v)", r.trailers, r.cuts)
	}
	for _, c := range f.Calls() {
		if strings.HasPrefix(c, "Create") {
			t.Errorf("a failed read must spawn nothing, saw %q", c)
		}
	}
}

// The Checkpoint copies the Spec's context: label (the repo: stamp rides with it).
func TestReconcileSpecCheckpointCarriesTheSpecsLabels(t *testing.T) {
	r := &specSeamRec{}
	f := stubSpecSeams(t, r, "ws-chart", true, nil)
	f.Mutate("ws-spec", func(b *bd.Bead) { b.Labels = []string{"context:apps-x"} })
	spec := openSpec()
	spec.Labels = []string{"context:apps-x"}
	oOf := repoOf
	t.Cleanup(func() { repoOf = oOf })
	repoOf = func(bd.Bead) git.Repo { return "" }
	h := &recordingHarness{lay: func() { layBuilds(t, f, 1) }}

	if err := reconcileSpec(stubCtx{h}, spec); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	if b := f.Bead(cpOf(t)); !b.HasLabel("context:apps-x") || !b.HasLabel("kind:checkpoint") {
		t.Errorf("Checkpoint labels = %v, want kind:checkpoint and context:apps-x", b.Labels)
	}
}

// FR22 / ADR 0015: the Spec closes ONLY once the fork laid a Build. A fork that
// returns nil while laying zero kind:build members leaves an empty Checkpoint, so
// the Spec must stay OPEN (not closed) for the loop to re-dispatch — the gate is
// the artifact existing, not the session merely having run.
func TestReconcileSpecEmptySkeletonLeavesSpecOpen(t *testing.T) {
	f := stubSpecSeams(t, &specSeamRec{}, "ws-chart", true, nil)
	h := &recordingHarness{} // nil err: the fork "succeeded" but laid neither Builds nor Errands

	err := reconcileSpec(stubCtx{h}, openSpec())
	if err == nil || !strings.Contains(err.Error(), "ws-spec") {
		t.Fatalf("a fork that laid no artifact must error naming the Spec, got %v", err)
	}
	if len(h.invs) != 1 {
		t.Fatalf("the slicing session must have been forked once, got %v", h.invs)
	}
	if st, _ := specClosure(f); st == "closed" {
		t.Error("the Spec must stay OPEN when the fork laid nothing")
	}
}

// A Spec that already holds Errands and no Builds is not re-forked (the re-fork
// republished a fresh copy of each Errand); it drains as an Errand-only Spec.
func TestReconcileSpecSkipsForkWhenErrandsExist(t *testing.T) {
	f := stubSpecSeams(t, &specSeamRec{}, "ws-chart", true, nil)
	layErrands(f, 1)
	h := &recordingHarness{}

	if err := reconcileSpec(stubCtx{h}, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	if len(h.invs) != 0 {
		t.Errorf("must NOT re-fork slicing when Errands already exist, ran %d", len(h.invs))
	}
}

// A fork that lays only Errands is a fine slice: it drains as Errand-only.
func TestReconcileSpecForkLayingOnlyErrandsIsNotAnError(t *testing.T) {
	f := stubSpecSeams(t, &specSeamRec{}, "ws-chart", true, nil)
	h := &recordingHarness{lay: func() { layErrands(f, 2) }}

	if err := reconcileSpec(stubCtx{h}, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	if len(h.invs) != 1 {
		t.Errorf("slicing must fork once, ran %d", len(h.invs))
	}
	if st, why := specClosure(f); st != "closed" || why != "sliced: "+cpOf(t) {
		t.Errorf("the Errand-only Spec must close sliced: <checkpoint>, got %q %q", st, why)
	}
}

// Errand-only: Carry, the Checkpoint closed nothing-to-land, the branch delete,
// then the Spec's close handed to the publish.
func TestReconcileSpecErrandOnlyCarriesClosesCheckpointDeletesBranchThenClosesSpec(t *testing.T) {
	r := &specSeamRec{branchSeen: true}
	f := stubSpecSeams(t, r, "ws-chart", true, nil)
	layErrands(f, 2)
	f.Seed(bd.Bead{ID: "ws-dep", Kind: contract.KindSpec, Status: "open", BlockedBy: []string{"ws-spec"}})

	if err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	cp := cpOf(t)
	if dep := f.Bead("ws-dep"); !slices.Contains(dep.BlockedBy, cp) {
		t.Errorf("the dependent must be carried onto %s, blocked by %v", cp, dep.BlockedBy)
	}
	if b := f.Bead(cp); b.Status != "closed" || b.CloseReason != "nothing to land: Errand-only Spec ws-spec" {
		t.Errorf("Checkpoint = %q %q, want closed nothing to land", b.Status, b.CloseReason)
	}
	if !slices.Equal(r.deleted, []string{"checkpoint/" + cp}) {
		t.Errorf("deleted = %v, want checkpoint/%s", r.deleted, cp)
	}
	if st, why := specClosure(f); st != "closed" || why != "sliced: "+cp {
		t.Errorf("Spec = %q %q, want closed sliced", st, why)
	}
}

// Re-entry with the Checkpoint already closed nothing-to-land: no cut, no fork,
// no second Checkpoint close; the Spec's close is handed to the publish.
func TestReconcileSpecErrandOnlyReentrySkipsCutForkAndCheckpointClose(t *testing.T) {
	r := &specSeamRec{}
	f := stubSpecSeams(t, r, "ws-chart", true, nil)
	cp := seedCheckpoint(f)
	const why = bd.NothingToLandReason + " Errand-only Spec ws-spec"
	f.Mutate(cp, func(b *bd.Bead) { b.Status, b.CloseReason = "closed", why })
	layErrands(f, 2)
	h := &recordingHarness{}

	if err := reconcileSpec(stubCtx{h}, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	if len(r.cuts) != 0 || len(h.invs) != 0 || len(r.deleted) != 0 {
		t.Errorf("re-entry must not cut, fork or delete, cuts=%v forks=%d deleted=%v", r.cuts, len(h.invs), r.deleted)
	}
	if st, reason := specClosure(f); st != "closed" || reason != "sliced: "+cp {
		t.Errorf("Spec = %q %q, want closed sliced: %s", st, reason, cp)
	}
	if b := f.Bead(cp); b.CloseReason != why {
		t.Errorf("the Checkpoint must not be closed again, reason %q", b.CloseReason)
	}
}

// reentryClosedCheckpoint seeds a Checkpoint already closed nothing-to-land and
// returns the harness whose re-fork lays errandsAfter Errands and buildsAfter Builds.
func reentryClosedCheckpoint(t *testing.T, r *specSeamRec, errandsAfter, buildsAfter int) (*bdtest.Fake, *recordingHarness) {
	t.Helper()
	f := stubSpecSeams(t, r, "ws-chart", true, nil)
	cp := seedCheckpoint(f)
	f.Mutate(cp, func(b *bd.Bead) {
		b.Status, b.CloseReason = "closed", bd.NothingToLandReason+" Errand-only Spec ws-spec"
	})
	return f, &recordingHarness{lay: func() { layErrands(f, errandsAfter); layBuilds(t, f, buildsAfter) }}
}

// FR19: re-entry with the Checkpoint closed nothing-to-land and zero Errands (a
// failed publish deleted the Drafts) re-forks slicing, with no cut, Checkpoint
// close or branch delete; the Spec's close follows the re-count.
func TestReconcileSpecReentryZeroErrandsReforksThenClosesSpec(t *testing.T) {
	r := &specSeamRec{}
	f, h := reentryClosedCheckpoint(t, r, 2, 0)

	if err := reconcileSpec(stubCtx{h}, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	if len(h.invs) != 1 {
		t.Errorf("re-entry with zero Errands must re-fork once, ran %d", len(h.invs))
	}
	if st, _ := specClosure(f); st != "closed" || len(r.cuts)+len(r.deleted) != 0 {
		t.Errorf("want only the Spec close, got status %q cuts %v deleted %v", st, r.cuts, r.deleted)
	}
}

func TestReconcileSpecReentryReforkLayingNothingLeavesSpecOpen(t *testing.T) {
	f, h := reentryClosedCheckpoint(t, &specSeamRec{}, 0, 0)

	if err := reconcileSpec(stubCtx{h}, openSpec()); err == nil {
		t.Fatal("a re-fork laying nothing must error")
	}
	if st, _ := specClosure(f); st == "closed" {
		t.Error("Spec must stay open")
	}
}

func TestReconcileSpecReentryReforkLayingBuildFailsLoud(t *testing.T) {
	f, h := reentryClosedCheckpoint(t, &specSeamRec{}, 0, 1)

	err := reconcileSpec(stubCtx{h}, openSpec())
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("a Build under the closed Checkpoint must fail loud, got %v", err)
	}
	if st, _ := specClosure(f); st == "closed" {
		t.Error("Spec must stay open")
	}
}

// An Errand-only Checkpoint closed for another reason fails loud; so does the
// Builds arm's closed Checkpoint (below).
func TestReconcileSpecErrandOnlyCheckpointClosedOtherwiseFailsLoud(t *testing.T) {
	r := &specSeamRec{}
	f := stubSpecSeams(t, r, "ws-chart", true, nil)
	cp := seedCheckpoint(f)
	f.Mutate(cp, func(b *bd.Bead) { b.Status, b.CloseReason = "closed", "Closed" })
	layErrands(f, 1)
	if err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec()); err == nil {
		t.Fatal("an Errand-only Checkpoint closed for another reason must hard-fail")
	}
	if st, _ := specClosure(f); st == "closed" || len(r.deleted) != 0 {
		t.Errorf("nothing may close or delete, spec %q deleted %v", st, r.deleted)
	}
}

// A Carry error on the Errand-only arm closes nothing.
func TestReconcileSpecErrandOnlyCarryErrorClosesNothing(t *testing.T) {
	r := &specSeamRec{}
	f := stubSpecSeams(t, r, "ws-chart", true, nil)
	layErrands(f, 1)
	f.Seed(bd.Bead{ID: "ws-dep", Kind: contract.KindSpec, Status: "open", BlockedBy: []string{"ws-spec"}})
	f.FailOn("AddEdge", "*", errors.New("bd down"))
	if err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec()); err == nil {
		t.Fatal("a carry error must fail the reconcile")
	}
	if st, _ := specClosure(f); st == "closed" || f.Bead(cpOf(t)).Status == "closed" || len(r.deleted) != 0 {
		t.Errorf("a failed carry must close and delete nothing, spec %q deleted %v", st, r.deleted)
	}
}

// A refused branch delete never fails the pass; the Spec still closes.
func TestReconcileSpecErrandOnlyRefusedBranchDeleteStillSucceeds(t *testing.T) {
	r := &specSeamRec{branchSeen: true, branch: bd.BranchOutcome{Branch: "checkpoint/x", Err: errors.New("held by worktree")}}
	f := stubSpecSeams(t, r, "ws-chart", true, nil)
	layErrands(f, 1)
	if err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	if st, _ := specClosure(f); st != "closed" {
		t.Errorf("the Spec must still close, got %q", st)
	}
}

// Builds and Errands together: the Checkpoint is not closed.
func TestReconcileSpecBuildsAndErrandsLeaveCheckpointOpen(t *testing.T) {
	r := &specSeamRec{}
	f := stubSpecSeams(t, r, "ws-chart", true, nil)
	cp := seedCheckpoint(f)
	layBuilds(t, f, 1)
	layErrands(f, 1)
	if err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	if st := f.Bead(cp).Status; st == "closed" || len(r.deleted) != 0 {
		t.Errorf("Builds present: Checkpoint %q, deleted %v", st, r.deleted)
	}
}

// Builds and Errands together take today's path: Carry, then the close.
func TestReconcileSpecBuildsAndErrandsCarryThenClose(t *testing.T) {
	f := stubSpecSeams(t, &specSeamRec{}, "ws-chart", true, nil)
	cp := seedCheckpoint(f)
	layBuilds(t, f, 1)
	layErrands(f, 2)
	f.Seed(bd.Bead{ID: "ws-dep", Kind: contract.KindSpec, Status: "open", BlockedBy: []string{"ws-spec"}})

	if err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	if dep := f.Bead("ws-dep"); !slices.Contains(dep.BlockedBy, cp) {
		t.Errorf("the dependent must be carried onto %s, blocked by %v", cp, dep.BlockedBy)
	}
	if st, why := specClosure(f); st != "closed" || why != "sliced: "+cp {
		t.Errorf("Spec = %q %q", st, why)
	}
}

// Nami-parity: a re-dispatch of an open Spec whose Checkpoint already
// carries >=1 kind:build member must NOT re-fork the expensive slicing session —
// it runs only the close leg. Mirrors reconcileChart's placeholder-body gate.
// The roll-forward keeps a re-fork safe (FR21) but wasteful.
func TestReconcileSpecSkipsForkWhenBuildsExist(t *testing.T) {
	f := stubSpecSeams(t, &specSeamRec{}, "ws-chart", true, nil)
	seedCheckpoint(f)
	layBuilds(t, f, 1) // Builds laid on a prior dispatch; the close leg never ran
	h := &recordingHarness{}

	if err := reconcileSpec(stubCtx{h}, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	if len(h.invs) != 0 {
		t.Errorf("must NOT re-fork the slicing session when Builds already exist, ran %d", len(h.invs))
	}
	if st, _ := specClosure(f); st != "closed" {
		t.Errorf("must still close the Spec on the re-dispatch, got %q", st)
	}
}

// The close leg is gated on the Checkpoint still being open: the slicing fork
// must lay Builds only, never close the Checkpoint. If it clobbered the epic,
// reconcileSpec fails loud and leaves the Spec OPEN rather
// than burying a closed-Spec / closed-Checkpoint / open-Build corruption.
func TestReconcileSpecClosedCheckpointFailsLoudLeavesSpecOpen(t *testing.T) {
	f := stubSpecSeams(t, &specSeamRec{}, "ws-chart", true, nil)
	cp := seedCheckpoint(f)
	layBuilds(t, f, 1) // Builds laid; the close leg is reached
	f.Mutate(cp, func(b *bd.Bead) { b.Status = "closed" })

	if err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec()); err == nil {
		t.Fatal("a Checkpoint closed before the Spec must hard-fail")
	}
	if st, _ := specClosure(f); st == "closed" {
		t.Error("the Spec must stay OPEN when the Checkpoint was clobbered")
	}
}

// Acceptance (b): a direct headless Spec cuts from its own anchor
// (LandedBase(Spec:specID)) — no walk beyond confirming there is no Chart.
func TestReconcileSpecHeadlessCutsFromSpecAnchor(t *testing.T) {
	r := &specSeamRec{}
	f := stubSpecSeams(t, r, "", false, nil)
	seedCheckpoint(f)
	layBuilds(t, f, 1)

	if err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	if len(r.trailers) != 1 || r.trailers[0] != "Spec: ws-spec" {
		t.Errorf("headless Spec must grep its own anchor, got %v", r.trailers)
	}
	if len(r.cuts) != 1 || r.cuts[0].base != "landsha" {
		t.Errorf("headless Spec must still cut from the derived anchor SHA, got %v", r.cuts)
	}
}

// Acceptance (f): a missing anchor hard-fails and cuts nothing (no spawn, no
// branch) — never a rev-parse main fallback.
func TestReconcileSpecMissingAnchorHardFailsCutsNothing(t *testing.T) {
	r := &specSeamRec{}
	stubSpecSeams(t, r, "", false, git.ErrNotLanded)

	if err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec()); err == nil {
		t.Fatal("a missing anchor must hard-fail")
	}
	if _, ok, _ := bd.SuccessorOf("ws-spec", contract.KindCheckpoint); ok {
		t.Error("a missing anchor must spawn no Checkpoint")
	}
	if len(r.cuts) != 0 {
		t.Errorf("a missing anchor must cut no branch, got %v", r.cuts)
	}
}

// A foreign land of the base trailer is a refusal: the error surfaces, nothing is cut (ADR 0187 §4).
func TestReconcileSpecForeignLandRefusesCutsNothing(t *testing.T) {
	r := &specSeamRec{}
	stubSpecSeams(t, r, "ws-chart", true, &git.ForeignLanding{Trailer: "Chart: ws-chart", Ours: "a", Theirs: "b", Sha: "f00d"})

	var fe *git.ForeignLanding
	if err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec()); !errors.As(err, &fe) {
		t.Fatalf("err = %v, want *ForeignLanding", err)
	}
	if _, ok, _ := bd.SuccessorOf("ws-spec", contract.KindCheckpoint); ok || len(r.cuts) != 0 {
		t.Errorf("a foreign land must spawn (%v) and cut (%v) nothing", ok, r.cuts)
	}
}

// Idempotency: a re-run over an already-closed Spec runs no slicing session and
// closes nothing. (The spawn/cut legs stay idempotent via SpawnSuccessorIfAbsent
// and branch-if-absent CutBranch — their contracts, tested in bd/git.)
func TestReconcileSpecClosedSpecSkipsForkAndClose(t *testing.T) {
	f := stubSpecSeams(t, &specSeamRec{}, "ws-chart", true, nil)
	h := &recordingHarness{}
	spec := openSpec()
	spec.Status = "closed"

	if err := reconcileSpec(stubCtx{h}, spec); err != nil {
		t.Fatalf("reconcileSpec re-run: %v", err)
	}
	if len(h.invs) != 0 {
		t.Errorf("a closed Spec must not re-fork the slicing session, ran %d", len(h.invs))
	}
	if st, _ := specClosure(f); st == "closed" {
		t.Error("a closed Spec must not be closed again")
	}
}

// A slicing-session failure propagates so the Spec stays open (not closed) — the
// loop re-dispatches next tick.
func TestReconcileSpecForkFailureLeavesSpecOpen(t *testing.T) {
	f := stubSpecSeams(t, &specSeamRec{}, "ws-chart", true, nil)
	h := &recordingHarness{err: errors.New("slicing session crashed")}

	if err := reconcileSpec(stubCtx{h}, openSpec()); err == nil {
		t.Fatal("a fork failure must propagate so the Spec stays open")
	}
	if len(h.invs) != 1 {
		t.Errorf("the session must have been attempted once, got %d", len(h.invs))
	}
	if st, _ := specClosure(f); st == "closed" {
		t.Error("the Spec must NOT be closed on a fork failure")
	}
}

// publishingCtx is a dispatched effort's Ctx: it carries the contract.Publisher.
type publishingCtx struct {
	stubCtx
	closes []string
}

func (c *publishingCtx) CloseOnPublish(reason string) { c.closes = append(c.closes, reason) }
func (c *publishingCtx) EdgeOnPublish(bd.Edge)        {}

// Spec D6 / FR 22: in a dispatched effort reconcileSpec hands the Spec's close to
// the publish instead of closing it itself, so the Draft Builds, their edges and
// the close become visible in one batch.
func TestReconcileSpecHandsCloseToPublish(t *testing.T) {
	f := stubSpecSeams(t, &specSeamRec{}, "ws-chart", true, nil)
	ctx := &publishingCtx{stubCtx: stubCtx{&recordingHarness{lay: func() { layBuilds(t, f, 2) }}}}

	if err := reconcileSpec(ctx, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	if st, _ := specClosure(f); st == "closed" {
		t.Error("reconcileSpec must not close the Spec itself in an effort")
	}
	if len(ctx.closes) != 1 {
		t.Errorf("the Spec's close must be handed to the publish once, got %d", len(ctx.closes))
	}
}

// Spec D2: the Spec's Ordering edges carry to its Checkpoint immediately
// before the Spec's close, so its dependents wait for the Checkpoint to land.
func TestReconcileSpecCarriesToCheckpointBeforeClose(t *testing.T) {
	f := stubSpecSeams(t, &specSeamRec{}, "ws-chart", true, nil)
	cp := seedCheckpoint(f)
	layBuilds(t, f, 1)
	f.Seed(
		bd.Bead{ID: "ws-after", Kind: contract.KindSpec, Status: "open", BlockedBy: []string{"ws-spec"}},
		bd.Bead{ID: "ws-cond", Kind: contract.KindSpec, Status: "open", BlockedBy: []string{"ws-spec"}, CondBlockedBy: []string{"ws-spec"}},
	)
	if err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	for _, id := range []string{"ws-after", "ws-cond"} {
		if b := f.Bead(id); !slices.Contains(b.BlockedBy, cp) || !slices.Contains(b.BlockedBy, "ws-spec") {
			t.Errorf("%s blocked by %v, want the Spec kept as lineage and %s added", id, b.BlockedBy, cp)
		}
	}
	if !slices.Contains(f.Bead("ws-cond").CondBlockedBy, cp) {
		t.Error("a conditional-blocks dependent must carry as conditional-blocks")
	}
	if st, _ := specClosure(f); st != "closed" {
		t.Errorf("the Spec must close after the carry, got %q", st)
	}
}

// A carry error fails the reconcile before any close, so the Spec stays open for
// the next reconcile to retry (FR18); a closed Spec carries nothing again.
func TestReconcileSpecCarryErrorLeavesSpecOpen(t *testing.T) {
	f := stubSpecSeams(t, &specSeamRec{}, "ws-chart", true, nil)
	seedCheckpoint(f)
	layBuilds(t, f, 1)
	f.Seed(bd.Bead{ID: "ws-after", Kind: contract.KindSpec, Status: "open", BlockedBy: []string{"ws-spec"}})
	f.FailOn("AddEdge", "*", errors.New("bd down"))
	if err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec()); err == nil {
		t.Fatal("a carry error must fail the reconcile")
	}
	if st, _ := specClosure(f); st == "closed" {
		t.Error("no close after a failed carry")
	}

	f = stubSpecSeams(t, &specSeamRec{}, "ws-chart", true, nil)
	seedCheckpoint(f)
	layBuilds(t, f, 1)
	spec := openSpec()
	spec.Status = "closed"
	if err := reconcileSpec(stubCtx{&recordingHarness{}}, spec); err != nil {
		t.Fatalf("re-entrant reconcileSpec: %v", err)
	}
	for _, c := range f.Calls() {
		if strings.HasPrefix(c, "AddEdge") || strings.HasPrefix(c, "Batch") || strings.HasPrefix(c, "Close") {
			t.Errorf("a closed Spec must not carry or close again, saw %q", c)
		}
	}
}

// FR 22: the slicing prompt lays every Build as a Draft by the Spec, using the
// shared Draft block verbatim, with title/description in the same create.
func TestSlicingPromptDraftsBuildsBySpec(t *testing.T) {
	p := slicingPrompt("mc-x")
	for _, want := range []string{
		contract.DraftInstruction("mc-x"),
		"kind:build," + contract.DraftedBy("mc-x"),
		"-s deferred",
		`--title "..." --description "..."`,
	} {
		if !strings.Contains(p, want) {
			t.Errorf("slicing prompt missing %q", want)
		}
	}
}

// Spec decision C (FR14–FR17): the slicing prompt files each of the Spec's Errands
// through mugicrew errand new, maps the ordering lines to its flags, and rolls
// forward over the Spec's existing Errands.
func TestSlicingPromptFilesSpecErrands(t *testing.T) {
	p := strings.Join(strings.Fields(slicingPrompt("mc-x")), " ")
	for _, want := range []string{
		"## Errands",
		"crew doc errand-body.md",
		"bd dep list mc-x --direction=up -t discovered-from --json", // roll forward by edge
		"kind:errand",
		"mugicrew errand new --drafted-by mc-x",
		"`after: land` → `--after <checkpointID>`",
		"`before: land` → `--before <checkpointID>`",
		"`after: <T>` → `--after <T>`",
		"every `before: land` line is skipped",
		"an entry left with no flags is filed `--after <checkpointID>`",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("slicing prompt missing %q", want)
		}
	}
	filing := p[:strings.Index(p, "### Adopted entries")] // adopted entries keep the edge-only path
	for _, bad := range []string{"bd list --all -l kind:errand", "NO `--parent`", "free-standing", "bd dep add <errand>", "bd dep add <checkpointID> <errand>", "-l kind:errand,"} {
		if strings.Contains(filing, bad) {
			t.Errorf("slicing prompt still carries %q", bad)
		}
	}
}

// Each --after <T> needs T's id when the later Errand is filed, so the slicing
// prompt files Errands in dependency order and says where <T> comes from.
func TestSlicingPromptFilesErrandsInDependencyOrder(t *testing.T) {
	p := strings.Join(strings.Fields(slicingPrompt("mc-x")), " ")
	for _, want := range []string{
		"File each Errand after every Errand its `after:` lines name, whatever order the `## Errands` section lists them in",
		"`<T>` is the id on the first stdout line of that Errand's own `mugicrew errand new`, or an adopted entry's id",
		"an adopted entry's `after: <filed T>` edge is added only after T is filed",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("slicing prompt missing %q", want)
		}
	}
}

// FR 43–46: an Errands entry with an errand: <id> line is adopted, not filed.
func TestSlicingPromptAdoptsErrandByID(t *testing.T) {
	p := strings.Join(strings.Fields(slicingPrompt("mc-x")), " ")
	for _, want := range []string{
		"`errand: <id>` line",
		"files nothing for an adopted entry",
		"left out of the title-dedupe enumeration",
		"bd show <id>",
		"open `kind:errand`",
		"A closed Errand is skipped with a line saying so",
		"fail the slice loudly",
		"added only if missing",
		"adds no `discovered-from` edge",
		"stays where it is",
		"initiative adopt",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("slicing prompt missing adopted-entry rule %q", want)
		}
	}
}

// Spec D11 rows 12–14: the triage session's Critiques are Drafts by the pivot, so
// its hitl-run publishes them with the sweep's Spec or Chart.
func TestPivotPromptDraftsByPivot(t *testing.T) {
	p := pivotPrompt("ws-pv", "ws-cp", "ws-spec")
	if strings.Contains(p, "%!") {
		t.Fatalf("bad pivot prompt render: %q", p)
	}
	for _, want := range []string{
		contract.DraftInstruction("ws-pv"),
		"--drafted-by ws-pv", // both verbs file their Critique as the lead pivot's Draft
	} {
		if !strings.Contains(p, want) {
			t.Errorf("pivot prompt missing %q", want)
		}
	}
}

// Run re-reads the dispatched Spec then reconciles it — the end-to-end dispatch
// path over the seams.
func TestRunReadsAndReconciles(t *testing.T) {
	f := stubSpecSeams(t, &specSeamRec{}, "ws-chart", true, nil) // the fork lays a Build → the dispatch drives through to close
	h := &recordingHarness{lay: func() { layBuilds(t, f, 1) }}

	if err := (Zoro{}).Run(stubCtx{h}, contract.Bead{ID: "ws-spec", Kind: contract.KindSpec}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if st, _ := specClosure(f); len(h.invs) != 1 || st != "closed" {
		t.Errorf("Run must drive reconcileSpec to fork+close, got invs=%v spec=%q", h.invs, st)
	}
}

func TestReconcileUnknownKindErrors(t *testing.T) {
	if err := (Zoro{}).Reconcile(nil, bd.Bead{ID: "x", Kind: contract.KindChart}); err == nil {
		t.Error("reconcile of a kind with no handler must error")
	}
}

// The pure Checkpoint successor shape (§8.4): a container kind:checkpoint epic
// titled exactly the Spec's title. SpawnSuccessorIfAbsent (bd) guarantees the
// "exactly one" on re-run; here we fix only the emitted shape.
func TestCheckpointSuccessorShape(t *testing.T) {
	s := checkpointSuccessor(bd.Bead{ID: "ws-spec", Title: "a spec"})
	if s.Kind != contract.KindCheckpoint {
		t.Errorf("successor kind = %q, want checkpoint", s.Kind)
	}
	if !s.Container {
		t.Error("a Checkpoint is a container (bd epic), not a leaf")
	}
	if s.Title != "a spec" {
		t.Errorf("Checkpoint title = %q, want the Spec's title verbatim", s.Title)
	}
}

// The checkpoint branch naming helper (ADR 0010): checkpoint/<id>, built from
// git/'s rules off the one KindCheckpoint const so it can't drift. Also pins the
// "Checkpoint: <id>" trailer off that same const (drift guard) — Jinbe's land is
// what stamps it in production (out of scope for this slice); reconcileSpec cuts
// via BranchName and greps only Chart:/Spec: trailers.
func TestCheckpointNaming(t *testing.T) {
	if got := checkpointBranch("mc-x"); got != "checkpoint/mc-x" {
		t.Errorf("checkpoint branch = %q, want checkpoint/mc-x", got)
	}
	if got := baseTrailer(contract.KindCheckpoint, "mc-x"); got != "Checkpoint: mc-x" {
		t.Errorf("checkpoint trailer = %q, want Checkpoint: mc-x", got)
	}
}

// slicingPrompt formats the embedded template with the id (guards a broken embed
// path or a stray literal % that fmt.Sprintf would turn into %!(...)).
func TestSlicingPromptRenders(t *testing.T) {
	p := slicingPrompt("mc-x")
	if !strings.Contains(p, "mc-x") || strings.Contains(p, "%!") {
		t.Fatalf("bad slicing prompt render: %q", p)
	}
}

// An in-process effort killed under abort (ADR 0083) surfaces as a failure: a
// harness Run error makes Zoro's Run return non-nil, never a success.
func TestRunPropagatesHarnessError(t *testing.T) {
	stubSpecSeams(t, &specSeamRec{}, "ws-chart", true, nil)
	h := &recordingHarness{err: errors.New("claude -p: signal: terminated")}
	if err := (Zoro{}).Run(stubCtx{h}, contract.Bead{ID: "ws-spec", Kind: contract.KindSpec}); err == nil {
		t.Fatal("a harness Run error must make Zoro's Run fail")
	}
}

// Spec D6 over both rendered prompts: no skill call, no ADR citation,
// every crew-doc reference resolves; pivot reads checkpoint-rules and the ADR
// format, and slicing keeps its own rules but not the preamble's.
func TestZoroPromptsSkillFree(t *testing.T) {
	pivot := pivotPrompt("ws-pv", "ws-cp", "ws-sp")
	slicing := slicingPrompt("mc-x")
	promptguard.Check(t, "pivot", pivot)
	promptguard.Check(t, "slicing", slicing)
	for name, text := range map[string]string{"pivot": pivot, "slicing": slicing} {
		if v := promptguard.CommitRuleViolations(text); len(v) > 0 {
			t.Errorf("%s prompt breaks the conventional-commit rule: %q", name, v)
		}
	}
	refs := strings.Join(promptguard.CrewDocRefs(pivot), " ")
	for _, want := range []string{"checkpoint-rules.md", "ADR-FORMAT.md"} {
		if !strings.Contains(refs, want) {
			t.Errorf("pivot prompt does not read crew doc %s", want)
		}
	}
	for _, want := range []string{"<vertical-slice-rules>", "Wide refactors", "prefactor Build"} {
		if !strings.Contains(slicing, want) {
			t.Errorf("slicing prompt lost %q", want)
		}
	}
	for _, dup := range []string{"by their titles", "bare id", "own vocabulary"} {
		if strings.Contains(slicing, dup) {
			t.Errorf("slicing prompt repeats preamble text %q", dup)
		}
	}
}

// Spec D2, FR11: the Checkpoint a Spec cuts is born inside the Spec's
// Initiative, and outside one when the Spec has none.
func TestReconcileSpecCheckpointJoinsTheSpecsInitiative(t *testing.T) {
	for _, tc := range []struct{ parent, want string }{{"ws-init", "ws-init"}, {"ws-closed-init", ""}, {"", ""}} {
		t.Run(tc.parent, func(t *testing.T) {
			f := stubSpecSeams(t, &specSeamRec{}, "ws-chart", true, nil)
			f.Seed(
				bd.Bead{ID: "ws-init", Kind: contract.KindInitiative, Status: "open"},
				bd.Bead{ID: "ws-closed-init", Kind: contract.KindInitiative, Status: "closed"},
			)
			spec := openSpec()
			spec.Parent = tc.parent
			h := &recordingHarness{lay: func() { layBuilds(t, f, 1) }}
			if err := reconcileSpec(stubCtx{h}, spec); err != nil {
				t.Fatalf("reconcileSpec: %v", err)
			}
			if got := f.Bead(cpOf(t)).Parent; got != tc.want {
				t.Errorf("Checkpoint Parent = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCheckpointSuccessorCopiesTheSpecsContext(t *testing.T) {
	if s := checkpointSuccessor(bd.Bead{ID: "ws-spec", Labels: []string{"context:apps-x"}}); s.Context != "apps-x" {
		t.Errorf("Checkpoint Context = %q, want apps-x", s.Context)
	}
	if s := checkpointSuccessor(bd.Bead{ID: "ws-spec", Labels: []string{"context:apps-x", "repo:vendor/x"}}); s.Repo != "vendor/x" {
		t.Errorf("Checkpoint Repo = %q, want vendor/x", s.Repo)
	}
	if s := checkpointSuccessor(bd.Bead{ID: "ws-spec"}); s.Context != "" {
		t.Errorf("unlabelled Spec yields Context %q, want none", s.Context)
	}
}

// Pivot's focus line is the labelled Checkpoint's alone, appended to the old prompt.
func TestLaunchPivotFocus(t *testing.T) {
	launch := func(label string, noShow bool) string {
		f := seedPivot(t, "ws-cp", "ws-sp")
		f.Mutate("ws-cp", func(b *bd.Bead) { b.Labels = []string{label} })
		if noShow {
			f.FailOn("Show", "ws-cp", errors.New("bd show on a no-context Workspace"))
		}
		h := &recordingHarness{}
		if _, err := (Zoro{}).Launch(stubCtx{h}, contract.Bead{ID: "ws-pv", Kind: contract.KindPivot}); err != nil {
			t.Fatal(err)
		}
		return h.launched.Prompt
	}
	repo := func(cfg string) {
		d := t.TempDir()
		exec.Command("git", "init", "-q", d).Run()
		os.MkdirAll(d+"/.mugicrew", 0o755)
		os.WriteFile(d+"/.mugicrew/config.toml", []byte(cfg), 0o644)
		t.Chdir(d)
	}
	repo("")
	plain := launch("context:a", true)
	repo("[context.a]\npath = \"apps/a\"\n")
	if got := launch("kind:checkpoint", false); got != plain {
		t.Error("unlabelled container changed the prompt")
	}
	if got := launch("context:a", false); got != plain+"\n\n"+contexts.FocusLine(map[string]string{"a": "apps/a"}, bd.Bead{Labels: []string{"context:a"}}) {
		t.Errorf("labelled prompt is not old + focus line")
	}
}

// ADR 0157: the slicing prompt is the old render with no [context] table or an
// unlabelled Spec, and the old render plus exactly the focus line under a labelled one.
func TestSlicingFocusLineByteIdentity(t *testing.T) {
	run := func(label string) string {
		f := stubSpecSeams(t, &specSeamRec{}, "ws-chart", true, nil)
		h := &recordingHarness{lay: func() { layBuilds(t, f, 1) }}
		spec := openSpec()
		spec.Labels = []string{label}
		if err := reconcileSpec(stubCtx{h}, spec); err != nil {
			t.Fatal(err)
		}
		return h.invs[0].Prompt
	}
	repo := func(cfg string) {
		d := t.TempDir()
		exec.Command("git", "init", "-q", d).Run()
		os.MkdirAll(d+"/.mugicrew", 0o755)
		os.WriteFile(d+"/.mugicrew/config.toml", []byte(cfg), 0o644)
		t.Chdir(d)
	}
	repo("")
	plain := run("context:a")
	repo("[context.a]\npath = \"apps/a\"\n")
	if got := run("kind:spec"); got != plain {
		t.Error("unlabelled container changed the prompt")
	}
	if got := run("context:a"); got != plain+"\n\n"+contexts.FocusLine(map[string]string{"a": "apps/a"}, bd.Bead{Labels: []string{"context:a"}}) {
		t.Error("labelled prompt is not old + focus line")
	}
}

// A nested Spec's base is read from the child (never the Workspace's main) and its
// checkpoint branch is cut in the child; a trailer the child lacks is a hard error
// that cuts nothing (Spec FR11, D2, D3).
func TestReconcileSpecNestedReadsAndCutsInTheChild(t *testing.T) {
	r := &specSeamRec{}
	f := stubSpecSeams(t, r, "", false, nil)
	seedCheckpoint(f)
	layBuilds(t, f, 1)
	var reads, cuts []string
	var missing error
	oOf, oLand, oCut := repoOf, repoLanded, repoCut
	t.Cleanup(func() { repoOf, repoLanded, repoCut = oOf, oLand, oCut })
	repoOf = func(bd.Bead) git.Repo { return "/ws/child" }
	repoLanded = func(c git.Repo, tr string) (string, error) {
		reads = append(reads, string(c)+" "+tr)
		return "childsha", missing
	}
	repoCut = func(c git.Repo, name, base string) (bool, error) {
		cuts = append(cuts, string(c)+" "+name+" "+base)
		return true, nil
	}

	missing = git.ErrNotLanded
	if err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec()); err == nil {
		t.Fatal("a missing child trailer must hard-fail")
	}
	if len(cuts) != 0 || len(r.cuts) != 0 || len(r.trailers) != 0 {
		t.Errorf("missing trailer cut %v / %v or fell back to the Workspace read %v", cuts, r.cuts, r.trailers)
	}

	missing = nil
	var stamped []string
	oStamp := stampCut
	t.Cleanup(func() { stampCut = oStamp })
	stampCut = func(_ bd.Bead, id string, _ git.Repo, _ string) error {
		stamped = append(stamped, id)
		return nil
	}
	if err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec()); err != nil {
		t.Fatal(err)
	}
	if len(stamped) != 1 || stamped[0] == "" {
		t.Errorf("stamped = %v, want one stamp of the created Checkpoint", stamped)
	}
	if want := "/ws/child Spec: ws-spec"; reads[len(reads)-1] != want {
		t.Errorf("reads = %v, want last %q", reads, want)
	}
	if len(cuts) != 1 || !strings.HasSuffix(cuts[0], " childsha") || len(r.cuts) != 0 {
		t.Errorf("child cuts = %v, workspace cuts = %v, want one child cut from childsha", cuts, r.cuts)
	}
}

// A direct Spec cuts at its Anchor: pin under either strategy; with no pin it
// falls back to the legacy Spec: trailer under both; with neither it is a hard
// error naming the Spec and the triage.fastlane anchor attr.
func TestCheckpointBaseDirectSpec(t *testing.T) {
	for _, strat := range []string{git.LandStrategyLocal, git.LandStrategyGitlab} {
		t.Run(strat, func(t *testing.T) {
			swap(t, &landStrategy, func() (string, error) { return strat, nil })
			bdtest.New(t) // no Chart: the Spec is caused-by nothing
			swap(t, &repoOf, func(bd.Bead) git.Repo { return "" })
			var trailer error
			swap(t, &landedBase, func(string) (string, error) { return "trailersha", trailer })
			if got, err := checkpointBase(bd.Bead{ID: "ws-s", Description: "body\nAnchor: pinsha\n"}); err != nil || got != "pinsha" {
				t.Errorf("pinned: checkpointBase = %q, %v; want pinsha", got, err)
			}
			if got, err := checkpointBase(bd.Bead{ID: "ws-s", Description: "body"}); err != nil || got != "trailersha" {
				t.Errorf("legacy: checkpointBase = %q, %v; want trailersha", got, err)
			}
			trailer = git.ErrNotLanded
			_, err := checkpointBase(bd.Bead{ID: "ws-s", Description: "body"})
			if err == nil || !strings.Contains(err.Error(), "ws-s") || !strings.Contains(err.Error(), "triage.fastlane") {
				t.Errorf("no pin, no trailer: err = %v; want one naming the Spec and triage.fastlane", err)
			}
		})
	}
}

// A pinned nested Spec cuts at the pin with no fetch under local, one under gitlab.
func TestCheckpointBaseNestedPinnedFetchesOnlyUnderGitlab(t *testing.T) {
	for strat, want := range map[string]int{git.LandStrategyLocal: 0, git.LandStrategyGitlab: 1} {
		t.Run(strat, func(t *testing.T) {
			swap(t, &landStrategy, func() (string, error) { return strat, nil })
			bdtest.New(t) // no Chart: the Spec is caused-by nothing
			swap(t, &repoOf, func(bd.Bead) git.Repo { return "kid" })
			swap(t, &repoLanded, func(git.Repo, string) (string, error) { t.Fatal("trailer read"); return "", nil })
			fetched := 0
			swap(t, &repoFetch, func(git.Repo) (string, error) { fetched++; return "tip", nil })
			if got, err := checkpointBase(bd.Bead{ID: "k-s", Description: "body\nAnchor: pinsha\n"}); err != nil || got != "pinsha" || fetched != want {
				t.Errorf("checkpointBase = %q, %v, fetched %d; want pinsha, %d fetches", got, err, fetched, want)
			}
		})
	}
}

func TestCheckpointBaseNestedChartGitlabFetchesFirst(t *testing.T) {
	swap(t, &landStrategy, func() (string, error) { return git.LandStrategyGitlab, nil })
	bdtest.New(t,
		bd.Bead{ID: "k-s", Kind: contract.KindSpec, Status: "open", CausedBy: "k-c"},
		bd.Bead{ID: "k-c", Kind: contract.KindChart, Status: "closed", Description: "chart body"})
	swap(t, &repoOf, func(bd.Bead) git.Repo { return "kid" })
	var order []string
	swap(t, &repoFetch, func(git.Repo) (string, error) { order = append(order, "fetch"); return "tip", nil })
	swap(t, &repoLanded, func(_ git.Repo, tr string) (string, error) { order = append(order, tr); return "chartsha", nil })
	if got, err := checkpointBase(bd.Bead{ID: "k-s"}); err != nil || got != "chartsha" || len(order) != 2 || order[0] != "fetch" {
		t.Errorf("checkpointBase = %q, %v, order %v", got, err, order)
	}
}

// A Chart-backed Spec cuts from the Chart's Anchor: pin first, the legacy Chart: trailer
// second, and neither is a hard error — for a Workspace Chart and a nested-repo Chart.
func TestCheckpointBaseChartPinThenTrailer(t *testing.T) {
	for _, repo := range []git.Repo{"", "kid"} {
		t.Run("repo="+string(repo), func(t *testing.T) {
			swap(t, &landStrategy, func() (string, error) { return git.LandStrategyLocal, nil })
			f := bdtest.New(t,
				bd.Bead{ID: "ws-s", Kind: contract.KindSpec, Status: "open", CausedBy: "ws-c"},
				bd.Bead{ID: "ws-c", Kind: contract.KindChart, Status: "closed"})
			swap(t, &repoOf, func(bd.Bead) git.Repo { return repo })
			setChart := func(body string) { f.Mutate("ws-c", func(b *bd.Bead) { b.Description = body }) }
			var trailer error
			swap(t, &landedBase, func(string) (string, error) { return "trailersha", trailer })
			swap(t, &repoLanded, func(git.Repo, string) (string, error) { return "trailersha", trailer })
			spec := bd.Bead{ID: "ws-s"}
			setChart("body\nAnchor: pinsha\n")
			if got, err := checkpointBase(spec); err != nil || got != "pinsha" {
				t.Errorf("pinned: checkpointBase = %q, %v; want pinsha", got, err)
			}
			setChart("body")
			if got, err := checkpointBase(spec); err != nil || got != "trailersha" {
				t.Errorf("legacy: checkpointBase = %q, %v; want trailersha", got, err)
			}
			trailer = git.ErrNotLanded
			if _, err := checkpointBase(spec); !errors.Is(err, git.ErrNotLanded) {
				t.Errorf("no pin, no trailer: err = %v; want a hard ErrNotLanded", err)
			}
		})
	}
}

// Spec D11 (FRs 46–50): the pivot prompt reads a Signal raiser through
// caused-by and carries the Signal-raiser prose and Ask Critique handling.
func TestPivotPromptReadsSignalRaiser(t *testing.T) {
	p := strings.Join(strings.Fields(pivotPrompt("ws-pv", "ws-cp", "ws-sp")), " ") // line wraps are not content
	if strings.Contains(p, "%!") {
		t.Fatalf("bad pivot prompt render: %q", p)
	}
	for _, want := range []string{
		"bd dep list <pivotID> --direction=down -t caused-by --json", // FR46: the Signal raiser read
		"kind:signal",
		"## Triage (<signal>)",   // FR47: the Triage section quoted
		"Rerouted from <signal>", // FR47: the merge-comment variant
		"the report was wrong",   // FR47: dismiss prose
		"the Ask",                // FR47: in place of the unfinished FR
		"Ask Critique",           // FR48–50
		"pass `--rewrite <askID>=<file>` for each Ask whose as-specced work remains",
		"the verb closes the rest `dismissed: <pivotID>`",
		"you never reopen the Signal",
		"the verb comments the `AMENDMENT (<pivotID>): …` on each Ask Critique", // FR49
		"closes it as obsolete",                                                 // FR50
		"folds the Ask",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("pivot prompt missing %q", want)
		}
	}
}

// The skeleton's body cites its ADR with a slug, never by bare number.
func TestCheckpointSuccessorCitesADRWithSlug(t *testing.T) {
	d := checkpointSuccessor(bd.Bead{ID: "ws-spec", Title: "A spec"}).Description
	if bare := promptguard.BareADRCitations(d); len(bare) > 0 || !strings.Contains(d, "ADR 0015 (") {
		t.Errorf("skeleton body %q must cite ADR 0015 with its slug (bare: %q)", d, bare)
	}
}

// ADR 0325: the pivot prompt reads an Errand raiser, comments the verdict on the
// Errand, and adopts the Checkpoint's held Errands into a corrected Spec by id.
func TestPivotPromptReadsErrandRaiser(t *testing.T) {
	p := strings.Join(strings.Fields(pivotPrompt("ws-pv", "ws-cp", "ws-sp")), " ") // line wraps are not content
	if strings.Contains(p, "%!") {
		t.Fatalf("bad pivot prompt render: %q", p)
	}
	for _, want := range []string{
		"An Errand raiser: a `kind:errand` row", // Raised by read
		"stuck comment",
		"`Raised by Errand` line",
		"the verb comments on each raising Errand", // Errand comment on dismiss/amend
		"which Critique to wait for",
		"pass no `--critique` to Dismiss or Amend", // Ask Critique already exists
		"the open `kind:errand` rows",              // held-Errand listing
		"adopt it or leave it out",                 // confirm: put each to the human
		"### <Errand title>",                       // confirm: Spec entry
		"errand: <id>",                             // ...
		"before: land",                             // ...
		"no Steps body",                            // ...
		"stays open for the human to abandon",      // left-out Errand
		"name each held Errand and whether the human wants it adopted", // escalate evidence
	} {
		if !strings.Contains(p, want) {
			t.Errorf("pivot prompt missing %q", want)
		}
	}
}

// ADR 0414: Dismiss and Amend are one verb invocation each, with no raw bd write left,
// and the outcome-mixing rule and amend's ADR step stay.
func TestPivotPromptDismissAmendCallVerbs(t *testing.T) {
	p := strings.Join(strings.Fields(pivotPrompt("ws-pv", "ws-cp", "ws-sp")), " ")
	for _, want := range []string{
		`mugicrew pivot dismiss <pivotID> --gist "<gist>" [--critique "$critique"] [--rewrite <askID>="$rewrite"]... --drafted-by ws-pv`,
		`mugicrew pivot amend <pivotID> --gist "<gist>" --amendment "<FR> — <decision> — rejected: <options>" [--critique "$critique"] --drafted-by ws-pv`,
		"write no dismiss, amend or confirm",                      // outcome-mixing rule
		"only if the amendment is architectural beyond this Spec", // ADR step
	} {
		if !strings.Contains(p, want) {
			t.Errorf("pivot prompt missing %q", want)
		}
	}
	i, j := strings.Index(p, "### Dismiss"), strings.Index(p, "### Confirm")
	if i < 0 || j < i {
		t.Fatal("pivot prompt lost its Dismiss/Amend sections")
	}
	for _, raw := range []string{"bd create", "bd comments add", "bd close", "bd update <critiqueID>"} {
		if strings.Contains(p[i:j], raw) {
			t.Errorf("Dismiss/Amend still types %q", raw)
		}
	}
}

// ADR 0354: the slicing prompt refuses the retired Task forms and names the new ones.
func TestSlicingPromptRefusesRetiredTaskForms(t *testing.T) {
	p := strings.Join(strings.Fields(slicingPrompt("mc-x")), " ")
	for _, want := range []string{
		"carries a `## Tasks` heading or a `task: <id>` line, fail the slice loudly",
		"`## Errands`",
		"`errand: <id>`",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("slicing prompt missing retired-form refusal %q", want)
		}
	}
}

// seedRemnant seeds the Remnant ws-rm into a bdtest graph, related to the landed
// Checkpoint cp; an empty cp leaves the edge out.
func seedRemnant(t *testing.T, cp string) {
	t.Helper()
	rm := bd.Bead{ID: "ws-rm", Kind: contract.KindRemnant, Status: "open", Labels: []string{"kind:remnant"}}
	if cp != "" {
		rm.Related = []string{cp}
	}
	f := bdtest.New(t, rm)
	if cp != "" {
		f.Seed(bd.Bead{ID: cp, Kind: contract.KindCheckpoint, Status: "closed", Container: true, Labels: []string{"kind:checkpoint"}})
	}
}

// Launch(remnant) → one session anchored to the Remnant whose prompt carries the
// Go-resolved landed Checkpoint, the dry-run, the Renumber-wait variant, and exits
// run only on the human's yes; drop is never run and points to X.
func TestLaunchRemnantComposesSession(t *testing.T) {
	seedRemnant(t, "ws-cp")
	h := &recordingHarness{}
	if _, err := (Zoro{}).Launch(stubCtx{h}, contract.Bead{ID: "ws-rm", Kind: contract.KindRemnant}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if h.launched.BeadID != "ws-rm" {
		t.Errorf("Invocation must anchor to the Remnant, got %q", h.launched.BeadID)
	}
	p := strings.Join(strings.Fields(h.launched.Prompt), " ")
	for _, want := range []string{
		"bd update ws-rm --claim",
		"bd show ws-cp",
		"git merge-tree --write-tree --merge-base=<Tip>", // the dry-run
		"replays clean",
		"Renumber-wait variant", "Run no merge-tree", "will conflict with the Renumber's move",
		"mugicrew remnant ws-rm offshoot", "mugicrew remnant ws-rm respin",
		"only on the human's yes",
		"Never run `drop`", "press X on the Remnant's row",
		"leaves the Remnant open",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("remnant prompt lacks %q", want)
		}
	}
	// drop appears only in prohibitions: never as a runnable line.
	for _, l := range strings.Split(h.launched.Prompt, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "mugicrew remnant ws-rm drop") {
			t.Errorf("prompt runs drop: %q", l)
		}
	}
}

func TestLaunchRemnantWithoutLandedCheckpointErrors(t *testing.T) {
	seedRemnant(t, "")
	if _, err := (Zoro{}).Launch(stubCtx{&recordingHarness{}}, contract.Bead{ID: "ws-rm", Kind: contract.KindRemnant}); err == nil {
		t.Error("Launch of an unrelated Remnant must error")
	}
}
