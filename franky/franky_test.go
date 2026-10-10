package franky

import (
	"errors"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/promptguard"
)

// recordingHarness is the fake contract.Harness the handler tests dispatch through:
// it records every AFK invocation and can be made to fail. No live Claude Code.
// Mirrors zoro_test's recordingHarness.
type recordingHarness struct {
	invs []contract.Invocation
	err  error
}

func (h *recordingHarness) Run(_ contract.Ctx, inv contract.Invocation) (contract.Result, error) {
	h.invs = append(h.invs, inv)
	return contract.Result{}, h.err
}
func (h *recordingHarness) LaunchCommand(contract.Invocation) (contract.LaunchCommand, error) {
	return nil, nil
}

func (h *recordingHarness) ResumeCommand(string) (contract.AgentCommand, error) {
	return contract.AgentCommand{}, nil
}

type stubCtx struct{ h contract.Harness }

func (c stubCtx) Harness() contract.Harness { return c.h }

// frankySeamRec captures the bd legs so the tests can assert the close and the
// pivot hand-off without live bd/git.
type frankySeamRec struct {
	closed  []string  // closed directly (closeBead)
	reasons []string  // close reasons, parallel to closed
	pivots  []bd.Bead // the Checkpoint's open pivots (OpenPivots)
	raised  []bd.Bead // open pivots discovered-from the bead (PivotsDiscoveredFrom)
	raises  []string  // raisePivot calls: "<bead> <spec> <pivot ids>"
	spec    string    // the Checkpoint's Spec via SpecOf ("" = none)
	surgery string    // the Checkpoint's Surgery via SurgeryOf ("" = none)
}

func stubFrankySeams(t *testing.T, r *frankySeamRec) {
	t.Helper()
	oShow, oClose := showBead, closeBead
	oPivots, oRaised, oRaise, oSpec, oSurgery := openPivots, pivotsDiscoveredFrom, raisePivot, specOf, surgeryOf
	t.Cleanup(func() {
		showBead, closeBead = oShow, oClose
		openPivots, pivotsDiscoveredFrom, raisePivot, specOf, surgeryOf = oPivots, oRaised, oRaise, oSpec, oSurgery
	})
	closeBead = func(id, reason string) error {
		r.closed = append(r.closed, id)
		r.reasons = append(r.reasons, reason)
		return nil
	}
	openPivots = func(string) ([]bd.Bead, error) { return r.pivots, nil }
	pivotsDiscoveredFrom = func(string) ([]bd.Bead, error) { return r.raised, nil }
	// The raise's edges and close are bd.RaisePivot's (tested in bd); Franky's job
	// is handing it the bead, the resolved Spec and the raised pivots.
	raisePivot = func(_ contract.Ctx, b bd.Bead, spec string, ps []bd.Bead) error {
		r.raises = append(r.raises, b.ID+" "+spec+" "+strings.Join(bd.IDs(ps), ","))
		return nil
	}
	specOf = func(string) (string, bool, error) { return r.spec, r.spec != "", nil }
	surgeryOf = func(string) (string, bool, error) { return r.surgery, r.surgery != "", nil }
}

func openBuild() bd.Bead {
	return bd.Bead{ID: "ws-build", Kind: contract.KindBuild, Status: "in_progress", Parent: "ws-cp"}
}

// Launch errors for every kind but surgery — build, critique and conflict are AFK.
func TestLaunchErrorsForEveryKind(t *testing.T) {
	for _, k := range []contract.Kind{contract.KindBuild, contract.KindCritique, contract.KindConflict, contract.KindReview} {
		if _, err := (Franky{}).Launch(stubCtx{&recordingHarness{}}, contract.Bead{ID: "x", Kind: k}); err == nil {
			t.Errorf("Launch of AFK kind %q must error", k)
		}
	}
}

// launchHarness hands back an agent command over the prompt it was given.
type launchHarness struct {
	recordingHarness
	inv contract.Invocation
}

func (h *launchHarness) LaunchCommand(inv contract.Invocation) (contract.LaunchCommand, error) {
	h.inv = inv
	return contract.AgentCommand{Argv: []string{"claude", inv.Prompt}}, nil
}

// Spec D4: l on a Surgery launches a session over the surgery prompt.
func TestLaunchSurgeryComposesSession(t *testing.T) {
	h := &launchHarness{}
	lc, err := Franky{}.Launch(stubCtx{h}, contract.Bead{ID: "ws-surg", Kind: contract.KindSurgery})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if _, ok := lc.(contract.AgentCommand); !ok {
		t.Fatalf("Launch must return the agent variety, got %T", lc)
	}
	if h.inv.BeadID != "ws-surg" || !strings.Contains(h.inv.Prompt, "mugicrew surgery end ws-surg") {
		t.Errorf("Launch must run the surgery prompt anchored to the Surgery, got %+v", h.inv)
	}
}

// The surgery prompt carries the idle line verbatim, the end verb with the id
// filled in, and no pivot section (Spec D4).
func TestSurgeryPromptRenders(t *testing.T) {
	p := surgeryPrompt("ws-surg")
	for _, want := range []string{
		"⏸ Surgery idle — /exit; l on its row resumes it.",
		"mugicrew surgery end ws-surg",
		"bd update ws-surg --claim",
		"bd comments add ws-surg",
		"Ending: N commits → review",
		"CONTRIBUTING.md",
		"merge-base",
		contract.DraftInstruction("ws-surg"),
		"kind:signal," + contract.DraftedBy("ws-surg") + " -s deferred",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("surgery prompt missing %q", want)
		}
	}
	for _, bad := range []string{"pivot", "Pivot", "paused", "%!"} {
		if strings.Contains(p, bad) {
			t.Errorf("surgery prompt must not carry %q", bad)
		}
	}
	promptguard.Check(t, "surgery", p)
}

// Spec D6 over both rendered prompts: no skill call, no ADR citation,
// every crew-doc reference resolves. Build carries implement's four steps inline
// and reads tdd.md and code-review.md; conflict carries the merge-conflict steps
// inline and reads adr-renumber.md; both read checkpoint-rules.md.
func TestFrankyPromptsSkillFree(t *testing.T) {
	b := implementPrompt(promptArgs{BeadID: "mc-x", SpecID: "ws-spec"})
	c := conflictPrompt(promptArgs{BeadID: "mc-y", Base: "deadbeef", SpecID: "ws-spec"})
	promptguard.Check(t, "build", b)
	promptguard.Check(t, "conflict", c)
	for name, tc := range map[string]struct {
		p    string
		docs []string
	}{
		"build":    {b, []string{"tdd.md", "code-review.md", "checkpoint-rules.md"}},
		"conflict": {c, []string{"adr-renumber.md", "checkpoint-rules.md"}},
	} {
		refs := promptguard.CrewDocRefs(tc.p)
		for _, d := range tc.docs {
			if !slices.Contains(refs, d) {
				t.Errorf("%s prompt reads no crew doc %s (refs %q)", name, d, refs)
			}
		}
	}
	for _, want := range []string{"full test suite", "typecheck"} {
		if !strings.Contains(b, want) {
			t.Errorf("build prompt lost implement step %q", want)
		}
	}
	for _, want := range []string{"git rebase --continue", "never `git rebase --abort`", "intent"} {
		if !strings.Contains(c, want) {
			t.Errorf("conflict prompt lost merge-conflict step %q", want)
		}
	}
	// The AFK seams come from the Spec and the bead, not a user (FR 24).
	if tdd := promptguard.Docs(t)["tdd.md"]; !strings.Contains(tdd, "acceptance criteria") || !strings.Contains(tdd, "build-sliceable") {
		t.Errorf("tdd.md does not source its seams from the Spec and the bead")
	}
}

// A Build or Critique works on its own effort branch and runs mugicrew integrate
// before its bead closes; its roll-forward check reads the effort branch against
// the Checkpoint (Spec D14, FR 56).
func TestBuildPromptIntegratesEffortBranch(t *testing.T) {
	b := implementPrompt(promptArgs{BeadID: "mc-x", Checkpoint: "ws-cp", SpecID: "ws-spec"})
	if !promptguard.IntegratesBeforeClose(b) {
		t.Error("build prompt must name mugicrew integrate before bd close")
	}
	for _, want := range []string{"effort/mc-x", "git log --oneline checkpoint/ws-cp..HEAD", "git rebase --continue", "git rebase --abort"} {
		if !strings.Contains(b, want) {
			t.Errorf("build prompt lacks %q", want)
		}
	}
}

// The prompts render with the id (and the conflict base) and carry no stray % that
// fmt.Sprintf would turn into %!(...) — guards a broken embed or a literal %.
func TestPromptsRender(t *testing.T) {
	b := implementPrompt(promptArgs{BeadID: "mc-x", SpecID: "ws-spec"})
	if !strings.Contains(b, "mc-x") || !strings.Contains(b, "bd show ws-spec") || strings.Contains(b, "%!") {
		t.Fatalf("bad build prompt render: %q", b)
	}
	c := conflictPrompt(promptArgs{BeadID: "mc-y", Base: "deadbeef", SpecID: "ws-spec"})
	if !strings.Contains(c, "mc-y") || !strings.Contains(c, "deadbeef") || !strings.Contains(c, "bd show ws-spec") || strings.Contains(c, "%!") {
		t.Fatalf("bad conflict prompt render: %q", c)
	}
	// The raise afterRun keys on is a kind:pivot discovered-from this bead; the
	// retired human-label placeholder must be gone from both prompts (FR50).
	for id, p := range map[string]string{"mc-x": b, "mc-y": c} {
		for _, want := range []string{"kind:pivot", "discovered-from:" + id, "bd dep add"} {
			if !strings.Contains(p, want) {
				t.Errorf("prompt for %s must carry the pivot raise (%q)", id, want)
			}
		}
		if strings.Contains(p, "bd label add") {
			t.Errorf("prompt for %s still carries the human-label placeholder", id)
		}
	}
	// FR 24: both prompts embed the shared Draft block for their own bead, and the
	// pivot creates (and build's critique) are Drafts by it.
	for id, p := range map[string]string{"mc-x": b, "mc-y": c} {
		for _, want := range []string{
			contract.DraftInstruction(id),
			"kind:pivot," + contract.DraftedBy(id) + " -s deferred",
		} {
			if !strings.Contains(p, want) {
				t.Errorf("prompt for %s missing %q", id, want)
			}
		}
	}
	if want := "kind:critique," + contract.DraftedBy("mc-x") + " -s deferred"; !strings.Contains(b, want) {
		t.Errorf("build prompt missing %q", want)
	}
	// Build/Critique/Conflict work to the effective Spec: AMENDMENT comments override
	// (FR38); the rule lives in the checkpoint-rules Crew doc both prompts read.
	for name, p := range map[string]string{"build": b, "conflict": c} {
		if !strings.Contains(p, "effective") || !slices.Contains(promptguard.CrewDocRefs(p), "checkpoint-rules.md") {
			t.Errorf("%s prompt must work to the effective Spec from checkpoint-rules.md", name)
		}
	}
	rules := promptguard.Docs(t)["checkpoint-rules.md"]
	if !strings.Contains(rules, "AMENDMENT (") || !strings.Contains(rules, "latest wins") {
		t.Errorf("checkpoint-rules.md must explain AMENDMENT comments, latest wins")
	}
}

// Spec FR 12: external work found mid-run is an offshoot Signal by this
// bead, never a Task.
func TestAFKPromptsFileExternalWorkAsSignal(t *testing.T) {
	for id, p := range map[string]string{
		"mc-x": implementPrompt(promptArgs{BeadID: "mc-x", SpecID: "ws-spec"}),
		"mc-y": conflictPrompt(promptArgs{BeadID: "mc-y", Base: "deadbeef"}),
	} {
		for _, want := range []string{"offshoot Signal, never a Task", "kind:signal," + contract.DraftedBy(id), "discovered-from:" + id} {
			if !strings.Contains(p, want) {
				t.Errorf("prompt for %s missing %q", id, want)
			}
		}
	}
}

// Finishing a Build, Critique or Conflict only closes it (Spec D8, FR 32):
// Franky spawns no Review, even as the last open member — Jinbe spawns it from
// drained-Checkpoint state.
func TestFinishOnlyCloses(t *testing.T) {
	for _, k := range []contract.Kind{contract.KindBuild, contract.KindCritique, contract.KindConflict} {
		r := &frankySeamRec{}
		stubFrankySeams(t, r)
		b := openBuild()
		b.Kind = k
		if err := finish(b); err != nil {
			t.Fatalf("%s: finish: %v", k, err)
		}
		if len(r.closed) != 1 || r.closed[0] != "ws-build" {
			t.Errorf("%s: closed %v, want [ws-build]", k, r.closed)
		}
	}
}

// Roll-forward idempotency: a re-dispatch after the close already happened (bead
// re-read as closed) is a no-op — no double close.
func TestFinishClosedBeadIsNoOp(t *testing.T) {
	r := &frankySeamRec{}
	stubFrankySeams(t, r)
	closed := openBuild()
	closed.Status = "closed"

	if err := finish(closed); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if len(r.closed) != 0 {
		t.Errorf("a closed bead must be a no-op, got closed=%v", r.closed)
	}
}

// Run(build) forks the implement agent then drives the tail through to the close.
func TestRunBuildForksThenCloses(t *testing.T) {
	// A Rollback successor's parent is caused-by the Rollback, not a Spec: the
	// prompt must carry the Spec SpecOf resolves through it (Spec Decision 6).
	r := &frankySeamRec{spec: "ws-spec"}
	stubFrankySeams(t, r)
	showBead = func(id string) (bd.Bead, error) { return openBuild(), nil }
	h := &recordingHarness{}

	if err := (Franky{}).Run(stubCtx{h}, contract.Bead{ID: "ws-build", Kind: contract.KindBuild}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.invs) != 1 || h.invs[0].BeadID != "ws-build" || !strings.Contains(h.invs[0].Prompt, "ws-build") {
		t.Fatalf("build must fork the implement agent anchored to the bead, got %v", h.invs)
	}
	if !strings.Contains(h.invs[0].Prompt, "bd show ws-spec") {
		t.Errorf("build prompt must carry the SpecOf-resolved Spec, got %q", h.invs[0].Prompt)
	}
	if len(r.closed) != 1 || r.closed[0] != "ws-build" {
		t.Errorf("build must close itself, got %v", r.closed)
	}
}

// A fork failure propagates so the bead stays open (not closed) — the loop
// re-dispatches next tick. No close.
func TestRunForkFailureLeavesBeadOpen(t *testing.T) {
	r := &frankySeamRec{}
	stubFrankySeams(t, r)
	showBead = func(id string) (bd.Bead, error) { return openBuild(), nil }
	h := &recordingHarness{err: errors.New("build session crashed")}

	if err := (Franky{}).Run(stubCtx{h}, contract.Bead{ID: "ws-build", Kind: contract.KindBuild}); err == nil {
		t.Fatal("a fork failure must propagate")
	}
	if len(r.closed) != 0 {
		t.Errorf("a fork failure must not touch the graph, got closed=%v", r.closed)
	}
}

// A raise: the agent created (or merged into) a pivot discovered-from its bead.
// Franky hands the bead, its Checkpoint's Spec and the raised pivots to the raise
// (which wires the gate and closes 'pivoted:') and does not close
// itself — for the implement and conflict tails alike.
func TestRunPivotRaiseHandsOffWithoutReview(t *testing.T) {
	for name, b := range map[string]bd.Bead{
		"build":    openBuild(),
		"conflict": {ID: "ws-conf", Kind: contract.KindConflict, Status: "in_progress", Parent: "ws-cp", Description: "Base: cafef00d\n"},
	} {
		r := &frankySeamRec{spec: "ws-spec", raised: []bd.Bead{{ID: "ws-piv"}}}
		stubFrankySeams(t, r)
		showBead = func(string) (bd.Bead, error) { return b, nil }

		if err := (Franky{}).Run(stubCtx{&recordingHarness{}}, contract.Bead{ID: b.ID, Kind: b.Kind}); err != nil {
			t.Fatalf("%s Run: %v", name, err)
		}
		if want := []string{b.ID + " ws-spec ws-piv"}; !reflect.DeepEqual(r.raises, want) {
			t.Errorf("%s raises = %v, want %v", name, r.raises, want)
		}
		if len(r.closed) != 0 {
			t.Errorf("%s a pivoted pass must close nothing itself, got closed=%v", name, r.closed)
		}
	}
}

// The pivot raise resolves the Spec through bd.SpecOf, so a successor Checkpoint a
// Rollback caused (ADR 0048 §2) still relates its pivot to the original Spec.
// The seam is stubbed as SpecOf answers a Rollback-caused parent: the Spec reached
// through the Rollback, which no one-hop caused-by read would find.
func TestPivotRaiseResolvesSpecThroughSpecOf(t *testing.T) {
	if reflect.ValueOf(specOf).Pointer() != reflect.ValueOf(bd.SpecOf).Pointer() {
		t.Error("franky's Spec seam must default to bd.SpecOf, not a one-hop caused-by walk")
	}
	r := &frankySeamRec{raised: []bd.Bead{{ID: "ws-piv"}}}
	stubFrankySeams(t, r)
	specOf = func(id string) (string, bool, error) {
		if id != "ws-cp" {
			return "", false, nil
		}
		return "ws-orig-spec", true, nil // ws-cp →caused-by→ Rollback →related→ old Checkpoint →caused-by→ Spec
	}
	showBead = func(string) (bd.Bead, error) { return openBuild(), nil }
	h := &recordingHarness{}

	if err := (Franky{}).Run(stubCtx{h}, contract.Bead{ID: "ws-build", Kind: contract.KindBuild}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.invs) != 1 || !strings.Contains(h.invs[0].Prompt, "bd show ws-orig-spec") {
		t.Errorf("the build prompt must name the resolved Spec, got %v", h.invs)
	}
	if want := []string{"ws-build ws-orig-spec ws-piv"}; !reflect.DeepEqual(r.raises, want) {
		t.Errorf("the pivot must relate to the resolved Spec, got %v", r.raises)
	}
}

// Run(conflict) reads the Base: pin off the bead body and hands it to the resolve
// agent, then drives the same tail.
func TestRunConflictReadsBasePin(t *testing.T) {
	r := &frankySeamRec{spec: "ws-spec"}
	stubFrankySeams(t, r)
	conf := bd.Bead{ID: "ws-conf", Kind: contract.KindConflict, Status: "in_progress", Parent: "ws-cp", Description: "Base: cafef00d\n"}
	showBead = func(id string) (bd.Bead, error) { return conf, nil }
	h := &recordingHarness{}

	if err := (Franky{}).Run(stubCtx{h}, contract.Bead{ID: "ws-conf", Kind: contract.KindConflict}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.invs) != 1 || !strings.Contains(h.invs[0].Prompt, "cafef00d") {
		t.Fatalf("conflict prompt must carry the Base: SHA, got %v", h.invs)
	}
	if !strings.Contains(h.invs[0].Prompt, "bd show ws-spec") {
		t.Errorf("conflict prompt must carry the SpecOf-resolved Spec, got %q", h.invs[0].Prompt)
	}
}

// A conflict bead with no Base: pin is malformed — fail loud, fork nothing.
func TestRunConflictNoBasePinFailsLoud(t *testing.T) {
	r := &frankySeamRec{}
	stubFrankySeams(t, r)
	showBead = func(id string) (bd.Bead, error) {
		return bd.Bead{ID: "ws-conf", Kind: contract.KindConflict, Status: "in_progress", Parent: "ws-cp", Description: "no pin here"}, nil
	}
	h := &recordingHarness{}

	if err := (Franky{}).Run(stubCtx{h}, contract.Bead{ID: "ws-conf", Kind: contract.KindConflict}); err == nil {
		t.Fatal("a conflict bead with no Base: pin must fail loud")
	}
	if len(h.invs) != 0 {
		t.Errorf("no base → no fork, got %v", h.invs)
	}
}

// An unowned kind is a loud error, mirroring Nami.Run.
func TestRunUnknownKindErrors(t *testing.T) {
	stubFrankySeams(t, &frankySeamRec{})
	showBead = func(id string) (bd.Bead, error) {
		return bd.Bead{ID: id, Kind: contract.KindReview, Status: "open"}, nil
	}
	if err := (Franky{}).Run(stubCtx{&recordingHarness{}}, contract.Bead{ID: "x", Kind: contract.KindReview}); err == nil {
		t.Error("a kind Franky doesn't own must error")
	}
}

// FR49: a confirm sweep closes the bead out from under its running effort. Whether
// the fork then returns cleanly or fails, the re-read sees it closed and the tail
// is a no-op — no error, no edge, no Review, no close.
func TestRunOverSweepClosedBeadIsNoOp(t *testing.T) {
	for name, forkErr := range map[string]error{"fork ok": nil, "fork failed": errors.New("session killed")} {
		r := &frankySeamRec{raised: []bd.Bead{{ID: "ws-piv"}}, spec: "ws-spec"}
		stubFrankySeams(t, r)
		reads := 0
		showBead = func(string) (bd.Bead, error) {
			b := openBuild()
			if reads++; reads > 1 { // closed by the sweep after the dispatch read
				b.Status = "closed"
			}
			return b, nil
		}
		if err := (Franky{}).Run(stubCtx{&recordingHarness{err: forkErr}}, contract.Bead{ID: "ws-build", Kind: contract.KindBuild}); err != nil {
			t.Errorf("%s: Run = %v, want nil (swept is done)", name, err)
		}
		if len(r.closed)+len(r.raises) != 0 {
			t.Errorf("%s: swept bead touched the graph: closed=%v raises=%v", name, r.closed, r.raises)
		}
	}
}

// surgeryEndRec fakes the bd and git legs EndSurgery drives: the beads it shows
// and the branch's commit count. events orders the closes.
type surgeryEndRec struct {
	beads   map[string]bd.Bead
	commits int
	effort  int    // commits on effort/<id> only, that integrate moves onto the branch
	intErr  error  // integrate's failure
	branch  string // the branch counted
	events  []string
	reasons map[string]string
}

func stubSurgeryEnd(t *testing.T, r *surgeryEndRec) {
	t.Helper()
	r.reasons = map[string]string{}
	oShow, oClose, oAhead, oInt := showBead, closeBead, commitsAhead, integrateSurgery
	t.Cleanup(func() { showBead, closeBead, commitsAhead, integrateSurgery = oShow, oClose, oAhead, oInt })
	integrateSurgery = func(sg bd.Bead) error {
		r.events = append(r.events, "integrate "+sg.ID)
		if r.intErr == nil {
			r.commits, r.effort = r.commits+r.effort, 0
		}
		return r.intErr
	}
	showBead = func(id string) (bd.Bead, error) {
		b, ok := r.beads[id]
		if !ok {
			return bd.Bead{}, errors.New("no bead " + id)
		}
		return b, nil
	}
	closeBead = func(id, reason string) error {
		r.events = append(r.events, "close "+id)
		r.reasons[id] = reason
		return nil
	}
	commitsAhead = func(base, branch string) (int, error) {
		r.branch = branch
		return r.commits, nil
	}
}

func surgeryBeads() map[string]bd.Bead {
	return map[string]bd.Bead{
		"ws-sg": {ID: "ws-sg", Kind: contract.KindSurgery, Status: "in_progress", Parent: "ws-cp"},
		"ws-cp": {ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open", Container: true},
	}
}

// N > 0: the Surgery only closes, spawning no Review — Jinbe spawns it once the
// Checkpoint drains (Spec D8) — and the Checkpoint stays open.
func TestEndSurgeryWithCommitsOnlyCloses(t *testing.T) {
	r := &surgeryEndRec{beads: surgeryBeads(), commits: 3}
	stubSurgeryEnd(t, r)

	if err := EndSurgery("ws-sg"); err != nil {
		t.Fatalf("EndSurgery: %v", err)
	}
	if want := []string{"integrate ws-sg", "close ws-sg"}; !slices.Equal(r.events, want) {
		t.Fatalf("events = %v, want %v", r.events, want)
	}
	if r.reasons["ws-sg"] != "" {
		t.Errorf("Surgery close reason = %q, want none", r.reasons["ws-sg"])
	}
	if r.branch != "checkpoint/ws-cp" {
		t.Errorf("counted %q, want checkpoint/ws-cp", r.branch)
	}
}

// N = 0: the Surgery closes abandoned, then its empty Checkpoint.
func TestEndSurgeryWithNoCommitsClosesBoth(t *testing.T) {
	r := &surgeryEndRec{beads: surgeryBeads()}
	stubSurgeryEnd(t, r)

	if err := EndSurgery("ws-sg"); err != nil {
		t.Fatalf("EndSurgery: %v", err)
	}
	if want := []string{"integrate ws-sg", "close ws-sg", "close ws-cp"}; !slices.Equal(r.events, want) {
		t.Fatalf("events = %v, want %v", r.events, want)
	}
	if !strings.HasPrefix(r.reasons["ws-sg"], "abandoned") {
		t.Errorf("Surgery close reason = %q, want abandoned", r.reasons["ws-sg"])
	}
}

// surgery end integrates before it counts (Spec D8, FR 33): commits only on
// effort/<id> reach the Checkpoint and the Surgery reads as done, not empty. A
// failed integrate closes nothing.
func TestEndSurgeryIntegratesBeforeCounting(t *testing.T) {
	r := &surgeryEndRec{beads: surgeryBeads(), effort: 2}
	stubSurgeryEnd(t, r)
	if err := EndSurgery("ws-sg"); err != nil {
		t.Fatalf("EndSurgery: %v", err)
	}
	if want := []string{"integrate ws-sg", "close ws-sg"}; !slices.Equal(r.events, want) {
		t.Fatalf("events = %v, want %v", r.events, want)
	}
	if r.reasons["ws-sg"] != "" {
		t.Errorf("Surgery close reason = %q, want none", r.reasons["ws-sg"])
	}

	r = &surgeryEndRec{beads: surgeryBeads(), effort: 2, intErr: errors.New("integrate: conflict")}
	stubSurgeryEnd(t, r)
	if err := EndSurgery("ws-sg"); err == nil {
		t.Error("EndSurgery = nil after a failed integrate")
	}
	if want := []string{"integrate ws-sg"}; !slices.Equal(r.events, want) {
		t.Errorf("events = %v, want %v: a failed integrate closes nothing", r.events, want)
	}
}

// The verb refuses anything but an open Surgery under a Checkpoint, touching nothing.
func TestEndSurgeryRefusesBadBeads(t *testing.T) {
	for name, edit := range map[string]func(map[string]bd.Bead){
		"wrong kind": func(m map[string]bd.Bead) { b := m["ws-sg"]; b.Kind = contract.KindBuild; m["ws-sg"] = b },
		"closed":     func(m map[string]bd.Bead) { b := m["ws-sg"]; b.Status = "closed"; m["ws-sg"] = b },
		"no parent":  func(m map[string]bd.Bead) { b := m["ws-sg"]; b.Parent = ""; m["ws-sg"] = b },
		"non-checkpoint parent": func(m map[string]bd.Bead) {
			b := m["ws-cp"]
			b.Kind = contract.KindChart
			m["ws-cp"] = b
		},
	} {
		beads := surgeryBeads()
		edit(beads)
		r := &surgeryEndRec{beads: beads, commits: 1}
		stubSurgeryEnd(t, r)
		if err := EndSurgery("ws-sg"); err == nil {
			t.Errorf("%s: EndSurgery = nil, want a refusal", name)
		}
		if len(r.events) != 0 {
			t.Errorf("%s: a refusal must touch nothing, got %v", name, r.events)
		}
	}
}

// A Conflict resolves on its own effort branch, cut at the Checkpoint's pinned Tip,
// and publishes the rewrite with mugicrew integrate before its bead closes (Spec
// D14, FR 13, 56).
func TestConflictPromptIntegratesEffortBranch(t *testing.T) {
	p := conflictPrompt(promptArgs{BeadID: "mc-x", Base: "b4se", SpecID: "ws-spec"})
	if !promptguard.IntegratesBeforeClose(p) {
		t.Error("conflict prompt must name mugicrew integrate before bd close")
	}
	for _, want := range []string{"effort/mc-x", "git rebase --continue", "git rebase --abort"} {
		if !strings.Contains(p, want) {
			t.Errorf("conflict prompt lacks %q", want)
		}
	}
}

// A Surgery works on its own effort branch and runs mugicrew integrate before
// surgery end; on a conflict it can't resolve it asks the human before aborting
// (Spec D14, FR 12, 56).
func TestSurgeryPromptIntegratesEffortBranch(t *testing.T) {
	p := surgeryPrompt("ws-sg")
	if !promptguard.IntegratesBeforeClose(p) {
		t.Error("surgery prompt must name mugicrew integrate before bd close")
	}
	if i, e := strings.Index(p, "mugicrew integrate"), strings.Index(p, "mugicrew surgery end"); i < 0 || e < 0 || i > e {
		t.Error("surgery prompt must run mugicrew integrate before mugicrew surgery end")
	}
	for _, want := range []string{"effort/ws-sg", "git rebase --continue", "ask the human", "git rebase --abort"} {
		if !strings.Contains(p, want) {
			t.Errorf("surgery prompt lacks %q", want)
		}
	}
}

// Spec D6 over the build and conflict prompts in their three cases: a
// Spec'd Checkpoint keeps the Spec and the pivot section; a surgery Checkpoint
// carries its Surgery as context in place of the Spec; a headless one says it
// has no Spec. Neither Spec-less render carries a pivot or an empty `bd show `.
func TestPromptsIntentSource(t *testing.T) {
	b := promptArgs{BeadID: "mc-x", Base: "deadbeef"}
	specd, surgery, headless := b, b, b
	specd.SpecID, surgery.SurgeryID = "ws-spec", "ws-surg"
	type tc struct {
		p         string
		want, bad []string
	}
	cases := map[string]tc{}
	for kind, render := range map[string]func(promptArgs) string{"build": implementPrompt, "conflict": conflictPrompt} {
		cases[kind+"/spec'd"] = tc{render(specd), []string{"bd show ws-spec", "kind:pivot,drafted-by:mc-x"}, []string{"Surgery", "no Spec"}}
		cases[kind+"/surgery"] = tc{render(surgery), []string{"bd show ws-surg", "Surgery", "as a Critique"}, []string{"pivot", "headless"}}
		cases[kind+"/headless"] = tc{render(headless), []string{"headless: there is no Spec", "as a Critique"}, []string{"pivot", "Surgery"}}
	}
	for name, c := range cases {
		for _, w := range c.want {
			if !strings.Contains(c.p, w) {
				t.Errorf("%s prompt missing %q", name, w)
			}
		}
		for _, bad := range append(c.bad, "%!", "<no value>") {
			if strings.Contains(c.p, bad) {
				t.Errorf("%s prompt must not carry %q", name, bad)
			}
		}
		if emptyShow.MatchString(c.p) {
			t.Errorf("%s prompt renders an empty bd show", name)
		}
		promptguard.Check(t, name, c.p)
	}
}

var emptyShow = regexp.MustCompile("bd show [^a-z<]")

// A Build on a Spec-less Checkpoint with a Surgery member gets the Surgery as its
// context (Spec D6), resolved through bd.SurgeryOf.
func TestRunImplementResolvesSurgeryWhenNoSpec(t *testing.T) {
	if reflect.ValueOf(surgeryOf).Pointer() != reflect.ValueOf(bd.SurgeryOf).Pointer() {
		t.Error("franky's Surgery seam must default to bd.SurgeryOf")
	}
	r := &frankySeamRec{surgery: "ws-surg"}
	stubFrankySeams(t, r)
	showBead = func(string) (bd.Bead, error) { return openBuild(), nil }
	h := &recordingHarness{}
	if err := (Franky{}).Run(stubCtx{h}, contract.Bead{ID: "ws-build", Kind: contract.KindBuild}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.invs) != 1 || !strings.Contains(h.invs[0].Prompt, "bd show ws-surg") || strings.Contains(h.invs[0].Prompt, "kind:pivot") {
		t.Errorf("the build prompt must carry the Surgery and no pivot, got %v", h.invs)
	}
}
