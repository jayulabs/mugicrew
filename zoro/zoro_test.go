package zoro

import (
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
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

// specSeamRec captures every seam call so the tests can assert the derived base,
// the branch cut, the spawn, and the close without live bd/git.
type cutCall struct{ name, base string }
type specSeamRec struct {
	walked     []string  // causedBy: spec ids walked
	trailers   []string  // landedBase: trailers grepped
	cuts       []cutCall // cutBranch: (name, base)
	spawned    []bd.Successor
	closed     []string         // Spec ids closed (via closeOnPublish, outside an effort)
	builds     int              // buildChildren: kind:build members the slicing fork laid on the Checkpoint
	tasks      int              // taskChildren: kind:task dependents the Spec holds over discovered-from
	seq        []string         // carry and close calls, in order: "carry <pred> <succ>" / "close <id>"
	carryErr   error            // carry: the error bd.Carry returns
	closeErr   error            // closeBead: the error closing the Checkpoint returns
	branch     bd.BranchOutcome // branchOutcome: what the branch delete reports
	branchSeen bool             // branchOutcome: whether the branch existed
}

// stubSpecSeams swaps reconcileSpec's bd/git legs for inert fakes so the flow runs
// with no live bd or git. causedBy returns (chartID, found); landedBase returns
// "landsha" unless baseErr is set (the missing-anchor hard fail). Restores the
// real funcs on cleanup. Mirrors nami_test's stubChartSeams.
func stubSpecSeams(t *testing.T, r *specSeamRec, chartID string, found bool, baseErr error) {
	t.Helper()
	oCause, oLand, oCut, oSpawn, oShow, oPublish, oBuilds, oTasks, oCarry := causedBy, landedBase, cutBranch, spawnSuccessor, showBead, closeOnPublish, buildChildren, taskChildren, carry
	t.Cleanup(func() {
		causedBy, landedBase, cutBranch, spawnSuccessor, showBead, closeOnPublish, buildChildren, taskChildren, carry = oCause, oLand, oCut, oSpawn, oShow, oPublish, oBuilds, oTasks, oCarry
	})
	carry = func(pred, succ string) ([]string, []bd.CarrySkip, error) {
		r.seq = append(r.seq, "carry "+pred+" "+succ)
		return nil, nil, r.carryErr
	}
	causedBy = func(id string, _ contract.Kind) (string, bool, error) {
		r.walked = append(r.walked, id)
		return chartID, found, nil
	}
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
	spawnSuccessor = func(_ string, s bd.Successor) (string, bool, error) {
		r.spawned = append(r.spawned, s)
		return "ws-cp", true, nil
	}
	// reconcileSpec never closes the Spec directly (Spec D6); sweep tests re-stub this.
	swapClose := closeBead
	t.Cleanup(func() { closeBead = swapClose })
	closeBead = func(id, reason string) error {
		if id == "ws-spec" {
			t.Errorf("reconcileSpec must not close the Spec directly")
		}
		r.seq = append(r.seq, "closecp "+id+" "+reason)
		return r.closeErr
	}
	oBranch := branchOutcome
	t.Cleanup(func() { branchOutcome = oBranch })
	branchOutcome = func(_ git.Repo, branch string) (bd.BranchOutcome, bool) {
		r.seq = append(r.seq, "delete "+branch)
		return r.branch, r.branchSeen
	}
	// Outside an effort (stubCtx) the flow publishes its own Drafts with the Spec's close.
	closeOnPublish = func(ctx contract.Ctx, id, reason string, es []bd.Edge) error {
		r.seq = append(r.seq, "close "+id)
		if _, ok := ctx.(contract.Publisher); ok {
			return bd.CloseOnPublish(ctx, id, reason, es)
		}
		r.closed = append(r.closed, id)
		return nil
	}
	showBead = func(id string) (bd.Bead, error) { return bd.Bead{ID: id, Kind: contract.KindSpec, Status: "open"}, nil }
	buildChildren = func(string) (int, error) { return r.builds, nil }
	taskChildren = func(string) (int, error) { return r.tasks, nil }
}

func openSpec() bd.Bead {
	return bd.Bead{ID: "ws-spec", Kind: contract.KindSpec, Status: "open", Title: "a spec"}
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

// stubPivotSeams swaps the reverse lookups Launch resolves (Checkpoint via
// reverse blocks, Spec via related); an empty id reports not-found.
func stubPivotSeams(t *testing.T, cp, spec string) {
	t.Helper()
	oCp, oSpec := pivotCheckpoint, pivotSpec
	t.Cleanup(func() { pivotCheckpoint, pivotSpec = oCp, oSpec })
	pivotCheckpoint = func(string) (string, bool, error) { return cp, cp != "", nil }
	pivotSpec = func(string) (string, bool, error) { return spec, spec != "", nil }
}

// Launch(pivot) → one session anchored to the pivot whose prompt carries the
// pivot, Go-resolved Checkpoint and Spec ids, the six-step flow, and the
// dismiss/amend legs with their check-before-write guards (ADR 0046 §2–4).
func TestLaunchPivotComposesSession(t *testing.T) {
	stubPivotSeams(t, "ws-cp", "ws-sp")
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
		"together",                              // 4. decide all together
		"Re-read the open pivots",               // 5. newcomers first
		"--parent ws-cp", "--no-inherit-labels", // dismiss/amend Critique on the Checkpoint
		"dismissed: <gist>", // dismiss close
		"AMENDMENT (<pivotID>): <FR> — <decision> — rejected: <options>",
		"only if `bd comments ws-sp` has no comment", // amend idempotency
		"amended: <gist>", // amend close
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
	stubPivotSeams(t, "ws-cp", "ws-sp")
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
		stubPivotSeams(t, c.cp, c.spec)
		if _, err := (Zoro{}).Launch(stubCtx{&recordingHarness{}}, contract.Bead{ID: "ws-pv", Kind: contract.KindPivot}); err == nil {
			t.Errorf("cp=%q spec=%q: Launch must error", c.cp, c.spec)
		}
	}
}

// Acceptance (a),(c),(d),(e): a Chart-backed open Spec derives its base from the
// Chart's landed base, cuts checkpoint/<cp> from that derived SHA (never main),
// forks the slicing session anchored to the Spec, and closes the Spec after the
// fork returns.
func TestReconcileSpecChartBackedCutsFromChartLandedBase(t *testing.T) {
	r := &specSeamRec{} // no Builds yet; the slicing fork lays one, then the Spec closes
	stubSpecSeams(t, r, "ws-chart", true, nil)
	h := &recordingHarness{lay: func() { r.builds = 1 }}

	if err := reconcileSpec(stubCtx{h}, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	// (a) base derived by walking Spec→Chart and grepping the Chart trailer.
	if len(r.walked) != 1 || r.walked[0] != "ws-spec" {
		t.Errorf("must walk caused-by from the Spec once, got %v", r.walked)
	}
	if len(r.trailers) != 1 || r.trailers[0] != "Chart: ws-chart" {
		t.Errorf("Chart-backed Spec must grep the Chart landed base, got %v", r.trailers)
	}
	// (c) branch cut from the derived SHA, never main.
	if len(r.cuts) != 1 {
		t.Fatalf("must cut exactly one branch, got %v", r.cuts)
	}
	if r.cuts[0].name != "checkpoint/ws-cp" {
		t.Errorf("branch = %q, want checkpoint/ws-cp", r.cuts[0].name)
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
	if len(r.closed) != 1 || r.closed[0] != "ws-spec" {
		t.Errorf("Spec must be closed once after the fork laid a Build, got %v", r.closed)
	}
}

// FR22 / ADR 0015: the Spec closes ONLY once the fork laid a Build. A fork that
// returns nil while laying zero kind:build members leaves an empty Checkpoint, so
// the Spec must stay OPEN (not closed) for the loop to re-dispatch — the gate is
// the artifact existing, not the session merely having run.
func TestReconcileSpecEmptySkeletonLeavesSpecOpen(t *testing.T) {
	r := &specSeamRec{builds: 0, tasks: 0} // fork returned nil but laid neither Builds nor Tasks
	stubSpecSeams(t, r, "ws-chart", true, nil)
	h := &recordingHarness{} // nil err: the fork "succeeded"

	err := reconcileSpec(stubCtx{h}, openSpec())
	if err == nil || !strings.Contains(err.Error(), "ws-spec") {
		t.Fatalf("a fork that laid no artifact must error naming the Spec, got %v", err)
	}
	if len(h.invs) != 1 {
		t.Fatalf("the slicing session must have been forked once, got %v", h.invs)
	}
	if len(r.closed) != 0 {
		t.Errorf("the Spec must stay OPEN when the fork laid nothing, got closed=%v", r.closed)
	}
}

// A Spec that already holds Tasks and no Builds is not re-forked (the re-fork
// republished a fresh copy of each Task); it drains as a Task-only Spec.
func TestReconcileSpecSkipsForkWhenTasksExist(t *testing.T) {
	r := &specSeamRec{builds: 0, tasks: 1}
	stubSpecSeams(t, r, "ws-chart", true, nil)
	h := &recordingHarness{}

	if err := reconcileSpec(stubCtx{h}, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	if len(h.invs) != 0 {
		t.Errorf("must NOT re-fork slicing when Tasks already exist, ran %d", len(h.invs))
	}
}

// A fork that lays only Tasks is a fine slice: it drains as Task-only.
func TestReconcileSpecForkLayingOnlyTasksIsNotAnError(t *testing.T) {
	r := &specSeamRec{}
	stubSpecSeams(t, r, "ws-chart", true, nil)
	calls := 0
	taskChildren = func(string) (int, error) { calls++; return (calls - 1) * 2, nil } // none before the fork, two after
	h := &recordingHarness{}

	if err := reconcileSpec(stubCtx{h}, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	if len(h.invs) != 1 {
		t.Errorf("slicing must fork once, ran %d", len(h.invs))
	}
	if len(r.closed) != 1 {
		t.Errorf("the Task-only Spec must close, got %v", r.closed)
	}
}

// Task-only: Carry, the Checkpoint closed nothing-to-land, the branch delete,
// then the Spec's close handed to the publish — in that order.
func TestReconcileSpecTaskOnlyCarriesClosesCheckpointDeletesBranchThenClosesSpec(t *testing.T) {
	r := &specSeamRec{tasks: 2, branchSeen: true, branch: bd.BranchOutcome{Branch: "checkpoint/ws-cp"}}
	stubSpecSeams(t, r, "ws-chart", true, nil)

	if err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	want := []string{"carry ws-spec ws-cp", "closecp ws-cp nothing to land: Task-only Spec ws-spec", "delete checkpoint/ws-cp", "close ws-spec"}
	if !slices.Equal(r.seq, want) {
		t.Errorf("seq = %v, want %v", r.seq, want)
	}
}

// Re-entry with the Checkpoint already closed nothing-to-land: no cut, no fork,
// no second Checkpoint close; the Spec's close is handed to the publish.
func TestReconcileSpecTaskOnlyReentrySkipsCutForkAndCheckpointClose(t *testing.T) {
	r := &specSeamRec{tasks: 2}
	stubSpecSeams(t, r, "ws-chart", true, nil)
	showBead = func(id string) (bd.Bead, error) {
		return bd.Bead{ID: id, Status: "closed", CloseReason: bd.NothingToLandReason + " Task-only Spec ws-spec"}, nil
	}
	h := &recordingHarness{}

	if err := reconcileSpec(stubCtx{h}, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	if len(r.cuts) != 0 || len(h.invs) != 0 {
		t.Errorf("re-entry must not cut or fork, cuts=%v forks=%d", r.cuts, len(h.invs))
	}
	if want := []string{"close ws-spec"}; !slices.Equal(r.seq, want) {
		t.Errorf("seq = %v, want %v", r.seq, want)
	}
}

// reentryClosedCheckpoint stubs a Checkpoint already closed nothing-to-land, with
// the Spec's Task count and Build count read before and after the re-fork.
func reentryClosedCheckpoint(t *testing.T, r *specSeamRec, tasksAfter, buildsAfter int) {
	t.Helper()
	stubSpecSeams(t, r, "ws-chart", true, nil)
	showBead = func(id string) (bd.Bead, error) {
		return bd.Bead{ID: id, Status: "closed", CloseReason: bd.NothingToLandReason + " Task-only Spec ws-spec"}, nil
	}
	taskCalls := 0
	taskChildren = func(string) (int, error) {
		taskCalls++
		if taskCalls == 1 {
			return 0, nil
		}
		return tasksAfter, nil
	}
	buildChildren = func(string) (int, error) {
		if taskCalls == 0 {
			return 0, nil
		}
		return buildsAfter, nil
	}
}

// FR19: re-entry with the Checkpoint closed nothing-to-land and zero Tasks (a
// failed publish deleted the Drafts) re-forks slicing, with no cut, Checkpoint
// close or branch delete; the Spec's close follows the re-count.
func TestReconcileSpecReentryZeroTasksReforksThenClosesSpec(t *testing.T) {
	r := &specSeamRec{}
	reentryClosedCheckpoint(t, r, 2, 0)
	h := &recordingHarness{}

	if err := reconcileSpec(stubCtx{h}, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	if len(h.invs) != 1 {
		t.Errorf("re-entry with zero Tasks must re-fork once, ran %d", len(h.invs))
	}
	if want := []string{"close ws-spec"}; !slices.Equal(r.seq, want) || len(r.cuts) != 0 {
		t.Errorf("seq = %v cuts = %v, want only the Spec close", r.seq, r.cuts)
	}
}

func TestReconcileSpecReentryReforkLayingNothingLeavesSpecOpen(t *testing.T) {
	r := &specSeamRec{}
	reentryClosedCheckpoint(t, r, 0, 0)
	h := &recordingHarness{}

	if err := reconcileSpec(stubCtx{h}, openSpec()); err == nil {
		t.Fatal("a re-fork laying nothing must error")
	}
	if len(r.seq) != 0 {
		t.Errorf("Spec must stay open, got %v", r.seq)
	}
}

func TestReconcileSpecReentryReforkLayingBuildFailsLoud(t *testing.T) {
	r := &specSeamRec{}
	reentryClosedCheckpoint(t, r, 0, 1)
	h := &recordingHarness{}

	err := reconcileSpec(stubCtx{h}, openSpec())
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("a Build under the closed Checkpoint must fail loud, got %v", err)
	}
	if len(r.seq) != 0 {
		t.Errorf("Spec must stay open, got %v", r.seq)
	}
}

// A Task-only Checkpoint closed for another reason fails loud; so does the
// Builds arm's closed Checkpoint (above).
func TestReconcileSpecTaskOnlyCheckpointClosedOtherwiseFailsLoud(t *testing.T) {
	r := &specSeamRec{tasks: 1}
	stubSpecSeams(t, r, "ws-chart", true, nil)
	showBead = func(id string) (bd.Bead, error) {
		return bd.Bead{ID: id, Status: "closed", CloseReason: "Closed"}, nil
	}
	if err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec()); err == nil {
		t.Fatal("a Task-only Checkpoint closed for another reason must hard-fail")
	}
	if len(r.seq) != 0 {
		t.Errorf("nothing may carry, close or delete, got %v", r.seq)
	}
}

// A Carry error on the Task-only arm closes nothing.
func TestReconcileSpecTaskOnlyCarryErrorClosesNothing(t *testing.T) {
	r := &specSeamRec{tasks: 1, carryErr: errors.New("bd down")}
	stubSpecSeams(t, r, "ws-chart", true, nil)
	if err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec()); err == nil {
		t.Fatal("a carry error must fail the reconcile")
	}
	if want := []string{"carry ws-spec ws-cp"}; !slices.Equal(r.seq, want) {
		t.Errorf("seq = %v, want %v", r.seq, want)
	}
}

// A refused branch delete never fails the pass; the Spec still closes.
func TestReconcileSpecTaskOnlyRefusedBranchDeleteStillSucceeds(t *testing.T) {
	r := &specSeamRec{tasks: 1, branchSeen: true, branch: bd.BranchOutcome{Branch: "checkpoint/ws-cp", Err: errors.New("held by worktree")}}
	stubSpecSeams(t, r, "ws-chart", true, nil)
	if err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	if len(r.closed) != 1 {
		t.Errorf("the Spec must still close, got %v", r.closed)
	}
}

// Builds and Tasks together: the Checkpoint is not closed.
func TestReconcileSpecBuildsAndTasksLeaveCheckpointOpen(t *testing.T) {
	r := &specSeamRec{builds: 1, tasks: 1}
	stubSpecSeams(t, r, "ws-chart", true, nil)
	if err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	for _, c := range r.seq {
		if strings.HasPrefix(c, "closecp") || strings.HasPrefix(c, "delete") {
			t.Errorf("unexpected %q with Builds present", c)
		}
	}
}

// Builds and Tasks together take today's path: Carry, then the close.
func TestReconcileSpecBuildsAndTasksCarryThenClose(t *testing.T) {
	r := &specSeamRec{builds: 1, tasks: 2}
	stubSpecSeams(t, r, "ws-chart", true, nil)
	h := &recordingHarness{}

	if err := reconcileSpec(stubCtx{h}, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	if want := []string{"carry ws-spec ws-cp", "close ws-spec"}; !slices.Equal(r.seq, want) {
		t.Errorf("seq = %v, want %v", r.seq, want)
	}
}

// Nami-parity: a re-dispatch of an open Spec whose Checkpoint already
// carries >=1 kind:build member must NOT re-fork the expensive slicing session —
// it runs only the close leg. Mirrors reconcileChart's placeholder-body gate.
// The roll-forward keeps a re-fork safe (FR21) but wasteful.
func TestReconcileSpecSkipsForkWhenBuildsExist(t *testing.T) {
	r := &specSeamRec{builds: 1} // Builds laid on a prior dispatch; the close leg never ran
	stubSpecSeams(t, r, "ws-chart", true, nil)
	h := &recordingHarness{} // no lay: if the fork ran, it'd be recorded

	if err := reconcileSpec(stubCtx{h}, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	if len(h.invs) != 0 {
		t.Errorf("must NOT re-fork the slicing session when Builds already exist, ran %d", len(h.invs))
	}
	if len(r.closed) != 1 || r.closed[0] != "ws-spec" {
		t.Errorf("must still close the Spec on the re-dispatch, got %v", r.closed)
	}
}

// The close leg is gated on the Checkpoint still being open: the slicing fork
// must lay Builds only, never close the Checkpoint. If it clobbered the epic,
// reconcileSpec fails loud and leaves the Spec OPEN rather
// than burying a closed-Spec / closed-Checkpoint / open-Build corruption.
func TestReconcileSpecClosedCheckpointFailsLoudLeavesSpecOpen(t *testing.T) {
	r := &specSeamRec{builds: 1} // Builds laid; the close leg is reached
	stubSpecSeams(t, r, "ws-chart", true, nil)
	showBead = func(id string) (bd.Bead, error) { return bd.Bead{ID: id, Status: "closed"}, nil }

	if err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec()); err == nil {
		t.Fatal("a Checkpoint closed before the Spec must hard-fail")
	}
	if len(r.closed) != 0 {
		t.Errorf("the Spec must stay OPEN when the Checkpoint was clobbered, got closed=%v", r.closed)
	}
}

// Acceptance (b): a direct headless Spec cuts from its own anchor
// (LandedBase(Spec:specID)) — no walk beyond confirming there is no Chart.
func TestReconcileSpecHeadlessCutsFromSpecAnchor(t *testing.T) {
	r := &specSeamRec{builds: 1}
	stubSpecSeams(t, r, "", false, nil)

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
	if len(r.spawned) != 0 {
		t.Errorf("a missing anchor must spawn no Checkpoint, got %v", r.spawned)
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
	if len(r.spawned)+len(r.cuts) != 0 {
		t.Errorf("a foreign land must spawn %v and cut %v nothing", r.spawned, r.cuts)
	}
}

// Idempotency: a re-run over an already-closed Spec runs no slicing session and
// closes nothing. (The spawn/cut legs stay idempotent via SpawnSuccessorIfAbsent
// and branch-if-absent CutBranch — their contracts, tested in bd/git.)
func TestReconcileSpecClosedSpecSkipsForkAndClose(t *testing.T) {
	r := &specSeamRec{}
	stubSpecSeams(t, r, "ws-chart", true, nil)
	h := &recordingHarness{}
	spec := openSpec()
	spec.Status = "closed"

	if err := reconcileSpec(stubCtx{h}, spec); err != nil {
		t.Fatalf("reconcileSpec re-run: %v", err)
	}
	if len(h.invs) != 0 {
		t.Errorf("a closed Spec must not re-fork the slicing session, ran %d", len(h.invs))
	}
	if len(r.closed) != 0 {
		t.Errorf("a closed Spec must not be closed again, got %v", r.closed)
	}
}

// A slicing-session failure propagates so the Spec stays open (not closed) — the
// loop re-dispatches next tick.
func TestReconcileSpecForkFailureLeavesSpecOpen(t *testing.T) {
	r := &specSeamRec{}
	stubSpecSeams(t, r, "ws-chart", true, nil)
	h := &recordingHarness{err: errors.New("slicing session crashed")}

	if err := reconcileSpec(stubCtx{h}, openSpec()); err == nil {
		t.Fatal("a fork failure must propagate so the Spec stays open")
	}
	if len(h.invs) != 1 {
		t.Errorf("the session must have been attempted once, got %d", len(h.invs))
	}
	if len(r.closed) != 0 {
		t.Errorf("the Spec must NOT be closed on a fork failure, got %v", r.closed)
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
	r := &specSeamRec{}
	stubSpecSeams(t, r, "ws-chart", true, nil)
	ctx := &publishingCtx{stubCtx: stubCtx{&recordingHarness{lay: func() { r.builds = 2 }}}}

	if err := reconcileSpec(ctx, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	if len(r.closed) != 0 {
		t.Errorf("reconcileSpec must not close the Spec itself in an effort, closed %v", r.closed)
	}
	if len(ctx.closes) != 1 {
		t.Errorf("the Spec's close must be handed to the publish once, got %d", len(ctx.closes))
	}
}

// Spec D2: the Spec's Ordering edges carry to its Checkpoint immediately
// before the Spec's close, so its dependents wait for the Checkpoint to land.
func TestReconcileSpecCarriesToCheckpointBeforeClose(t *testing.T) {
	r := &specSeamRec{builds: 1}
	stubSpecSeams(t, r, "ws-chart", true, nil)
	if err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec()); err != nil {
		t.Fatalf("reconcileSpec: %v", err)
	}
	if want := []string{"carry ws-spec ws-cp", "close ws-spec"}; !slices.Equal(r.seq, want) {
		t.Errorf("seq = %v, want %v", r.seq, want)
	}
}

// A carry error fails the reconcile before any close, so the Spec stays open for
// the next reconcile to retry (FR18); a closed Spec carries nothing again.
func TestReconcileSpecCarryErrorLeavesSpecOpen(t *testing.T) {
	r := &specSeamRec{builds: 1, carryErr: errors.New("bd down")}
	stubSpecSeams(t, r, "ws-chart", true, nil)
	if err := reconcileSpec(stubCtx{&recordingHarness{}}, openSpec()); err == nil {
		t.Fatal("a carry error must fail the reconcile")
	}
	if want := []string{"carry ws-spec ws-cp"}; !slices.Equal(r.seq, want) {
		t.Errorf("seq = %v, want %v (no close after a failed carry)", r.seq, want)
	}

	r = &specSeamRec{builds: 1}
	stubSpecSeams(t, r, "ws-chart", true, nil)
	spec := openSpec()
	spec.Status = "closed"
	if err := reconcileSpec(stubCtx{&recordingHarness{}}, spec); err != nil {
		t.Fatalf("re-entrant reconcileSpec: %v", err)
	}
	if len(r.seq) != 0 {
		t.Errorf("a closed Spec must not carry or close again, got %v", r.seq)
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

// Spec D5 (FR 6–9, 13): the slicing prompt files each of the Spec's
// Tasks as a free-standing Draft by the Spec (kind:task, no --parent), carries
// the three edge recipes, and rolls forward over the Spec's existing Tasks.
func TestSlicingPromptFilesSpecTasks(t *testing.T) {
	p := slicingPrompt("mc-x")
	for _, want := range []string{
		"## Tasks",
		"crew doc task-body.md",
		"bd dep list mc-x --direction=up -t discovered-from --json", // roll forward by edge
		"kind:task",
		"--deps discovered-from:mc-x",
		"every `before: land` line is skipped",
		"-l kind:task," + contract.DraftedBy("mc-x") + " --no-inherit-labels",
		"bd dep add <task> <checkpointID>", // after: land
		"bd dep add <checkpointID> <task>", // before: land
		"bd dep add <task> <T>",            // after: <T>
	} {
		if !strings.Contains(p, want) {
			t.Errorf("slicing prompt missing %q", want)
		}
	}
	if strings.Contains(p, "bd list --all -l kind:task") {
		t.Error("the drafted-by list query is replaced by the discovered-from edge")
	}
	for _, line := range strings.Split(p, "\n") {
		if strings.Contains(line, "kind:task,") && strings.Contains(line, "--parent") {
			t.Errorf("a Task is filed with no --parent: %q", line)
		}
	}
}

// FR 43–46: a Tasks entry with a task: <id> line is adopted, not filed.
func TestSlicingPromptAdoptsTaskByID(t *testing.T) {
	p := strings.Join(strings.Fields(slicingPrompt("mc-x")), " ")
	for _, want := range []string{
		"`task: <id>` line",
		"files nothing for an adopted entry",
		"left out of the title-dedupe enumeration",
		"bd show <id>",
		"open `kind:task`",
		"A closed Task is skipped with a line saying so",
		"fail the slice loudly",
		"added only if missing",
		"adds no `discovered-from` edge",
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
		"kind:critique," + contract.DraftedBy("ws-pv") + " -s deferred",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("pivot prompt missing %q", want)
		}
	}
}

// Run re-reads the dispatched Spec then reconciles it — the end-to-end dispatch
// path over the seams.
func TestRunReadsAndReconciles(t *testing.T) {
	r := &specSeamRec{} // the fork lays a Build → the dispatch drives through to close
	stubSpecSeams(t, r, "ws-chart", true, nil)
	h := &recordingHarness{lay: func() { r.builds = 1 }}

	if err := (Zoro{}).Run(stubCtx{h}, contract.Bead{ID: "ws-spec", Kind: contract.KindSpec}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.invs) != 1 || len(r.closed) != 1 {
		t.Errorf("Run must drive reconcileSpec to fork+close, got invs=%v closed=%v", h.invs, r.closed)
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

// stubInitiative stands in for bd.InitiativeOf: a bead parented under ws-init is
// in an unclosed Initiative; any other parent (none, or a closed one) yields none.
func stubInitiative(t *testing.T) {
	t.Helper()
	o := initiativeOf
	t.Cleanup(func() { initiativeOf = o })
	initiativeOf = func(b bd.Bead) (string, error) {
		if b.Parent == "ws-init" {
			return "ws-init", nil
		}
		return "", nil
	}
}

// Spec D2, FR11: the Checkpoint a Spec cuts is born inside the Spec's
// Initiative, and outside one when the Spec has none.
func TestReconcileSpecCheckpointJoinsTheSpecsInitiative(t *testing.T) {
	for _, tc := range []struct{ parent, want string }{{"ws-init", "ws-init"}, {"ws-closed-init", ""}, {"", ""}} {
		t.Run(tc.parent, func(t *testing.T) {
			r := &specSeamRec{builds: 1}
			stubSpecSeams(t, r, "ws-chart", true, nil)
			stubInitiative(t)
			spec := openSpec()
			spec.Parent = tc.parent
			if err := reconcileSpec(stubCtx{&recordingHarness{}}, spec); err != nil {
				t.Fatalf("reconcileSpec: %v", err)
			}
			if len(r.spawned) != 1 || r.spawned[0].Parent != tc.want {
				t.Errorf("spawned %+v, want one Checkpoint with Parent %q", r.spawned, tc.want)
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
	stubPivotSeams(t, "ws-cp", "ws-sp")
	launch := func(label string, noShow bool) string {
		oShow := showBead
		t.Cleanup(func() { showBead = oShow })
		showBead = func(id string) (bd.Bead, error) {
			if noShow {
				t.Fatal("bd show on a no-context Workspace")
			}
			return bd.Bead{ID: id, Labels: []string{label}}, nil
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
		r := &specSeamRec{}
		stubSpecSeams(t, r, "ws-chart", true, nil)
		h := &recordingHarness{lay: func() { r.builds = 1 }}
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
	r := &specSeamRec{builds: 1}
	stubSpecSeams(t, r, "", false, nil)
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
			swap(t, &causedBy, func(string, contract.Kind) (string, bool, error) { return "", false, nil })
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
			swap(t, &causedBy, func(string, contract.Kind) (string, bool, error) { return "", false, nil })
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
	swap(t, &causedBy, func(string, contract.Kind) (string, bool, error) { return "k-c", true, nil })
	swap(t, &repoOf, func(bd.Bead) git.Repo { return "kid" })
	var order []string
	swap(t, &repoFetch, func(git.Repo) (string, error) { order = append(order, "fetch"); return "tip", nil })
	swap(t, &repoLanded, func(_ git.Repo, tr string) (string, error) { order = append(order, tr); return "chartsha", nil })
	if got, err := checkpointBase(bd.Bead{ID: "k-s"}); err != nil || got != "chartsha" || len(order) != 2 || order[0] != "fetch" {
		t.Errorf("checkpointBase = %q, %v, order %v", got, err, order)
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
		"rewrite it to whatever as-specced work is left",
		"close it `dismissed: <pivotID>`",
		"you never reopen the Signal",
		"AMENDMENT (<pivotID>): …` comment on the Ask Critique", // FR49
		"closes it as obsolete",                                 // FR50
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

// ADR 0325: the pivot prompt reads a Task raiser, comments the verdict on the
// Task, and adopts the Checkpoint's held Tasks into a corrected Spec by id.
func TestPivotPromptReadsTaskRaiser(t *testing.T) {
	p := strings.Join(strings.Fields(pivotPrompt("ws-pv", "ws-cp", "ws-sp")), " ") // line wraps are not content
	if strings.Contains(p, "%!") {
		t.Fatalf("bad pivot prompt render: %q", p)
	}
	for _, want := range []string{
		"A Task raiser: a `kind:task` row", // Raised by read
		"stuck comment",
		"`Raised by Task` line",
		"pivot <pivotID> dismissed|amended: <gist>", // Task comment on dismiss/amend
		"which Critique to wait for",
		"the step may be wrong; amend it on resume", // ...
		"skip the step-1 refile",                    // Ask Critique already exists
		"the open `kind:task` rows",                 // held-Task listing
		"adopt it or leave it out",                  // confirm: put each to the human
		"### <Task title>",                          // confirm: Spec entry
		"task: <id>",                                // ...
		"before: land",                              // ...
		"no Steps body",                             // ...
		"stays open for the human to abandon",       // left-out Task
		"name each held Task and whether the human wants it adopted", // escalate evidence
	} {
		if !strings.Contains(p, want) {
			t.Errorf("pivot prompt missing %q", want)
		}
	}
}
