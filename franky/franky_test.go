package franky

import (
	"errors"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contexts/contextstest"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
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

func (h *recordingHarness) ResumeCommand(string, bool) (contract.AgentCommand, error) {
	return contract.AgentCommand{}, nil
}

type stubCtx struct{ h contract.Harness }

func (c stubCtx) Harness() contract.Harness { return c.h }

// frankySeamRec captures the bd legs so the tests can assert the close and the
// pivot hand-off without live bd/git.
type frankySeamRec struct {
	closed    []string  // closed directly (closeBead)
	reasons   []string  // close reasons, parallel to closed
	pivots    []bd.Bead // the Checkpoint's open pivots (OpenPivots)
	raised    []bd.Bead // open pivots discovered-from the bead (PivotsDiscoveredFrom)
	raises    []string  // raisePivot calls: "<bead> <spec> <pivot ids>"
	spec      string    // the Checkpoint's Spec via SpecOf ("" = none)
	surgeries []bd.Bead // the Checkpoint's Surgeries via SurgeriesOf
}

func stubFrankySeams(t *testing.T, r *frankySeamRec) {
	t.Helper()
	oShow, oClose := showBead, closeBead
	oPivots, oRaised, oRaise, oSpec, oSurgery := openPivots, pivotsDiscoveredFrom, raisePivot, specOf, surgeriesOf
	t.Cleanup(func() {
		showBead, closeBead = oShow, oClose
		openPivots, pivotsDiscoveredFrom, raisePivot, specOf, surgeriesOf = oPivots, oRaised, oRaise, oSpec, oSurgery
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
	surgeriesOf = func(string) ([]bd.Bead, error) { return r.surgeries, nil }
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
	r := &frankySeamRec{}
	stubFrankySeams(t, r)
	showBead = func(id string) (bd.Bead, error) {
		return bd.Bead{ID: id, Title: "t-" + id, Parent: "ws-cp"}, nil
	}
	oKids := childrenOfKind
	t.Cleanup(func() { childrenOfKind = oKids })
	childrenOfKind = func(string, contract.Kind) ([]bd.Bead, error) { return nil, nil }
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
	p := surgeryPrompt(surgeryArgs{ID: "ws-surg", Checkpoint: "ws-cp", CheckpointTitle: "Cp title"})
	for _, want := range []string{
		"⏸ Surgery idle — /exit; l on its row resumes it.",
		"mugicrew surgery end ws-surg",
		"bd update ws-surg --claim",
		"bd comments add ws-surg",
		"Ending: N commits → review",
		"CONTRIBUTING.md",
		"--grep='^Surgery: ws-surg$'",
		contract.DraftInstruction("ws-surg"),
		"signal-filing.md",
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

// A fresh-Checkpoint Surgery carries the opener and no context block; a joined
// one renders every block; no case has a pivot section.
func TestSurgeryPromptContextBlocks(t *testing.T) {
	fresh := surgeryPrompt(surgeryArgs{ID: "ws-surg", Checkpoint: "ws-cp", CheckpointTitle: "Cp title"})
	if !strings.Contains(fresh, "for Checkpoint ws-cp — Cp title") {
		t.Error("fresh prompt lacks the opener")
	}
	for _, absent := range []string{"as background", "Earlier Surgeries", "Open Builds", "Open Critiques"} {
		if strings.Contains(fresh, absent) {
			t.Errorf("fresh prompt must not render %q", absent)
		}
	}
	if !strings.Contains(fresh, "bd show ws-surg)\n\nA surgery spans") {
		t.Error("an empty context must leave the template's spacing untouched")
	}
	bead := func(id string) bd.Bead { return bd.Bead{ID: id, Title: "title " + id} }
	joined := surgeryPrompt(surgeryArgs{
		ID: "ws-surg", Checkpoint: "ws-cp", CheckpointTitle: "Cp title", SpecID: "ws-spec", SpecTitle: "Spec title",
		Earlier: []bd.Bead{bead("ws-s1"), bead("ws-s2")}, Builds: []bd.Bead{bead("ws-b1")}, Critiques: []bd.Bead{bead("ws-c1")},
	})
	for _, want := range []string{
		"bd show ws-spec", "Spec title", "never raise a pivot",
		"- ws-s1: title ws-s1\n- ws-s2: title ws-s2", "bd comments <id>",
		"- ws-b1: title ws-b1", "may need a rebase",
		"- ws-c1: title ws-c1", "never claim or close",
		"bd children <checkpoint-id>",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("joined prompt missing %q", want)
		}
	}
	for _, p := range []string{fresh, joined} {
		if strings.Contains(strings.ReplaceAll(p, "never raise a pivot", ""), "pivot") {
			t.Error("surgery prompt must carry no pivot section")
		}
	}
}

// surgeryContext reads the Checkpoint, its Spec, closed sibling Surgeries and
// open Builds and Critiques; the Surgery itself and closed Builds are left out.
func TestSurgeryContextFromBd(t *testing.T) {
	r := &frankySeamRec{spec: "ws-spec", surgeries: []bd.Bead{
		{ID: "ws-s0", Status: "closed"}, {ID: "ws-surg", Status: "in_progress"}, {ID: "ws-s2", Status: "in_progress"},
	}}
	stubFrankySeams(t, r)
	oKids := childrenOfKind
	t.Cleanup(func() { childrenOfKind = oKids })
	showBead = func(id string) (bd.Bead, error) {
		return bd.Bead{ID: id, Title: "t-" + id, Parent: "ws-cp"}, nil
	}
	childrenOfKind = func(_ string, k contract.Kind) ([]bd.Bead, error) {
		return []bd.Bead{{ID: "open-" + string(k), Status: "open"}, {ID: "done-" + string(k), Status: "closed"}}, nil
	}
	a, err := surgeryContext("ws-surg")
	if err != nil {
		t.Fatal(err)
	}
	if a.Checkpoint != "ws-cp" || a.CheckpointTitle != "t-ws-cp" || a.SpecID != "ws-spec" || a.SpecTitle != "t-ws-spec" {
		t.Errorf("checkpoint/spec = %+v", a)
	}
	if got := bd.IDs(a.Earlier); !slices.Equal(got, []string{"ws-s0"}) {
		t.Errorf("Earlier = %v", got)
	}
	if got := bd.IDs(a.Builds); !slices.Equal(got, []string{"open-build"}) {
		t.Errorf("Builds = %v", got)
	}
	if got := bd.IDs(a.Critiques); !slices.Equal(got, []string{"open-critique"}) {
		t.Errorf("Critiques = %v", got)
	}
}

// Spec D6 over both rendered prompts: no skill call, no ADR citation,
// every crew-doc reference resolves. Build carries implement's four steps inline
// and reads tdd.md and code-review.md; conflict carries the merge-conflict steps
// inline and reads sequence-renumber.md; both read checkpoint-rules.md.
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
		"conflict": {c, []string{"sequence-renumber.md", "checkpoint-rules.md"}},
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

// FR 31: a filed Critique may close as not needed or already fixed, with a full
// close reason.
func TestBuildPromptCritiqueMayCloseNotNeeded(t *testing.T) {
	b := implementPrompt(promptArgs{BeadID: "mc-x", Checkpoint: "ws-cp", SpecID: "ws-spec"})
	for _, want := range []string{"not needed or is\nalready fixed", `bd close mc-x --reason "<why`, "relayed whole"} {
		if !strings.Contains(b, want) {
			t.Errorf("build prompt lacks %q", want)
		}
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
	for _, want := range []string{"effort/mc-x", "git log --oneline checkpoint/ws-cp..HEAD", "git rebase --continue", "git rebase --abort", `bd comments add mc-x "Not built: <why>"`} {
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
		for _, want := range []string{"offshoot Signal, never a Task", "signal-filing.md", "discovered-from " + id} {
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

// A Critique carrying a Base: pin (the failed-gate-build Critique) rebases onto
// that pinned base before the test-first fix, in the one session.
func TestRunCritiqueBasePinRebasesThenFixes(t *testing.T) {
	r := &frankySeamRec{spec: "ws-spec"}
	stubFrankySeams(t, r)
	crit := bd.Bead{ID: "ws-crit", Kind: contract.KindCritique, Status: "in_progress", Parent: "ws-cp", Description: "Base: cafef00d\ngate failed: TestX"}
	showBead = func(id string) (bd.Bead, error) { return crit, nil }
	h := &recordingHarness{}

	if err := (Franky{}).Run(stubCtx{h}, contract.Bead{ID: "ws-crit", Kind: contract.KindCritique}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.invs) != 1 {
		t.Fatalf("want one session, got %v", h.invs)
	}
	p := h.invs[0].Prompt
	rebase, fix := strings.Index(p, "git rebase cafef00d"), strings.Index(p, "Test-first")
	if rebase < 0 || fix < 0 || rebase > fix {
		t.Errorf("prompt must rebase onto the pin, then fix test-first; got %q", p)
	}
}

// A Critique with no Base: pin (Usopp's finding) keeps the plain implement leg.
func TestRunCritiqueWithoutBasePinSkipsRebase(t *testing.T) {
	r := &frankySeamRec{spec: "ws-spec"}
	stubFrankySeams(t, r)
	crit := bd.Bead{ID: "ws-crit", Kind: contract.KindCritique, Status: "in_progress", Parent: "ws-cp", Description: "a review finding"}
	showBead = func(id string) (bd.Bead, error) { return crit, nil }
	h := &recordingHarness{}

	if err := (Franky{}).Run(stubCtx{h}, contract.Bead{ID: "ws-crit", Kind: contract.KindCritique}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.invs) != 1 || strings.Contains(h.invs[0].Prompt, "Rebase first") {
		t.Errorf("an unpinned Critique must not rebase, got %v", h.invs)
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
	mine    int    // this Surgery's own trailered squashes among commits
	kids    int    // the Checkpoint's children, closed included
	effort  int    // commits on effort/<id> only, that integrate moves onto the branch
	intErr  error  // integrate's failure
	branch  string // the branch counted
	events  []string
	reasons map[string]string
}

func stubSurgeryEnd(t *testing.T, r *surgeryEndRec) {
	t.Helper()
	r.reasons = map[string]string{}
	oShow, oClose, oAhead, oInt, oMine, oKids := showBead, closeBead, commitsAhead, integrateSurgery, commitsByTrailer, checkpointChildCount
	t.Cleanup(func() {
		showBead, closeBead, commitsAhead, integrateSurgery, commitsByTrailer, checkpointChildCount = oShow, oClose, oAhead, oInt, oMine, oKids
	})
	if r.kids == 0 {
		r.kids = 1
	}
	commitsByTrailer = func(_ git.Repo, base, branch, trailer string) (int, error) {
		r.branch = branch
		if trailer != "Surgery: ws-sg" {
			t.Errorf("trailer = %q", trailer)
		}
		return r.mine, nil
	}
	checkpointChildCount = func(string) (int, error) { return r.kids, nil }
	integrateSurgery = func(sg bd.Bead, _ git.Repo) error {
		r.events = append(r.events, "integrate "+sg.ID)
		if r.intErr == nil {
			r.commits, r.mine, r.effort = r.commits+r.effort, r.mine+r.effort, 0
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
	commitsAhead = func(_ git.Repo, base, branch string) (int, error) {
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
	r := &surgeryEndRec{beads: surgeryBeads(), commits: 3, mine: 3}
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

// A joined zero-commit Surgery closes only itself: Builds' commits ahead, or
// other children (a stacked surgery Checkpoint's earlier Surgery), keep the
// Checkpoint open (Spec D8).
func TestEndSurgeryZeroCommitsKeepsSharedCheckpoint(t *testing.T) {
	for name, r := range map[string]*surgeryEndRec{
		"builds ahead":    {beads: surgeryBeads(), commits: 2, kids: 3},
		"earlier sibling": {beads: surgeryBeads(), kids: 2},
	} {
		stubSurgeryEnd(t, r)
		if err := EndSurgery("ws-sg"); err != nil {
			t.Fatalf("%s: EndSurgery: %v", name, err)
		}
		if want := []string{"integrate ws-sg", "close ws-sg"}; !slices.Equal(r.events, want) {
			t.Errorf("%s: events = %v, want %v", name, r.events, want)
		}
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
		r := &surgeryEndRec{beads: beads, commits: 1, mine: 1}
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
	p := surgeryPrompt(surgeryArgs{ID: "ws-sg"})
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

// Spec D1/D6 over the build and conflict prompts: the Spec and the Surgeries
// render independently, each only when present; the pivot section keys on the
// Spec alone. No render carries an empty `bd show `.
func TestPromptsIntentSource(t *testing.T) {
	sg := func(id, title string) bd.Bead { return bd.Bead{ID: id, Title: title} }
	one := []bd.Bead{sg("ws-s1", "tweak colour")}
	many := []bd.Bead{sg("ws-s1", "tweak colour"), sg("ws-s2", "abandoned idea"), sg("ws-s3", "fix gap")}
	type tc struct {
		args      promptArgs
		want, bad []string
	}
	base := promptArgs{BeadID: "mc-x", Base: "deadbeef"}
	with := func(spec string, sgs []bd.Bead) promptArgs { a := base; a.SpecID, a.Surgeries = spec, sgs; return a }
	cases := map[string]tc{
		"spec only":      {with("ws-spec", nil), []string{"bd show ws-spec", "kind:pivot,drafted-by:mc-x"}, []string{"Surgeries", "no Spec"}},
		"one surgery":    {with("", one), []string{"Surgeries on this Checkpoint", "- ws-s1: tweak colour", "as a Critique"}, []string{"pivot", "headless", "Spec this"}},
		"spec+surgery":   {with("ws-spec", one), []string{"Spec this Checkpoint implements: ws-spec", "context for work touching their commits", "- ws-s1: tweak colour", "kind:pivot,drafted-by:mc-x"}, []string{"headless"}},
		"many surgeries": {with("", many), []string{"- ws-s1: tweak colour", "- ws-s2: abandoned idea", "- ws-s3: fix gap"}, []string{"pivot", "headless"}},
		"headless":       {with("", nil), []string{"headless: there is no Spec", "as a Critique"}, []string{"pivot", "Surgeries"}},
	}
	for kind, render := range map[string]func(promptArgs) string{"build": implementPrompt, "conflict": conflictPrompt} {
		for name, c := range cases {
			p, name := render(c.args), kind+"/"+name
			for _, w := range c.want {
				if !strings.Contains(p, w) {
					t.Errorf("%s prompt missing %q", name, w)
				}
			}
			for _, bad := range append(c.bad, "%!", "<no value>") {
				if strings.Contains(p, bad) {
					t.Errorf("%s prompt must not carry %q", name, bad)
				}
			}
			if emptyShow.MatchString(p) {
				t.Errorf("%s prompt renders an empty bd show", name)
			}
			promptguard.Check(t, name, p)
		}
	}
}

// The conflict prompt keeps commit messages and their trailers, since review and
// the end rule attribute commits by trailer.
func TestConflictPromptPreservesTrailers(t *testing.T) {
	p := conflictPrompt(promptArgs{BeadID: "mc-x", Base: "deadbeef"})
	for _, w := range []string{"`Surgery: <id>`", "`Build: <id>`", "trailers"} {
		if !strings.Contains(p, w) {
			t.Errorf("conflict prompt missing %q", w)
		}
	}
}

var emptyShow = regexp.MustCompile("bd show [^a-z<]")

// A Build on a Spec-less Checkpoint with a Surgery member gets the Surgery as its
// context (Spec D6), resolved through bd.SurgeriesOf.
func TestRunImplementResolvesSurgeryWhenNoSpec(t *testing.T) {
	if reflect.ValueOf(surgeriesOf).Pointer() != reflect.ValueOf(bd.SurgeriesOf).Pointer() {
		t.Error("franky's Surgeries seam must default to bd.SurgeriesOf")
	}
	r := &frankySeamRec{surgeries: []bd.Bead{{ID: "ws-surg", Title: "t"}}}
	stubFrankySeams(t, r)
	showBead = func(string) (bd.Bead, error) { return openBuild(), nil }
	h := &recordingHarness{}
	if err := (Franky{}).Run(stubCtx{h}, contract.Bead{ID: "ws-build", Kind: contract.KindBuild}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.invs) != 1 || !strings.Contains(h.invs[0].Prompt, "- ws-surg: t") || strings.Contains(h.invs[0].Prompt, "kind:pivot") {
		t.Errorf("the build prompt must carry the Surgery and no pivot, got %v", h.invs)
	}
}

// ctxRepo makes a git repo with the given .mugicrew/config.toml and chdirs into it.
func ctxRepo(t *testing.T, cfg string) {
	t.Helper()
	d := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", d).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	os.MkdirAll(d+"/.mugicrew", 0o755)
	os.WriteFile(d+"/.mugicrew/config.toml", []byte(cfg), 0o644)
	t.Chdir(d)
}

const ctxCfg = "[context.mugicrew]\npath = \"apps/mugicrew\"\n"

var ctxFocus = "\n\n" + contexts.FocusLine(map[string]string{"mugicrew": "apps/mugicrew"}, bd.Bead{Labels: []string{"context:mugicrew"}})

// ADR 0157: build, conflict and surgery prompts are the old render, plus exactly the
// focus line under a labelled container; no table or an unlabelled one changes nothing.
func TestFrankyFocusLine(t *testing.T) {
	run := func(label string, noShow bool) []string {
		r := &frankySeamRec{spec: "ws-spec"}
		stubFrankySeams(t, r)
		oKids := childrenOfKind
		t.Cleanup(func() { childrenOfKind = oKids })
		childrenOfKind = func(string, contract.Kind) ([]bd.Bead, error) { return nil, nil }
		// The Surgery launch reads its Checkpoint for its own packet
		// (surgeryContext), so only the build run guards the container read.
		launching := false
		showBead = func(id string) (bd.Bead, error) {
			if noShow && !launching && id != "ws-build" {
				t.Fatal("bd show for a container on a no-context Workspace")
			}
			if id == "ws-cp" {
				return bd.Bead{ID: id, Labels: []string{label}}, nil
			}
			b := openBuild()
			if id == "ws-surg" {
				b.ID = "ws-surg"
			}
			return b, nil
		}
		h := &recordingHarness{}
		if err := (Franky{}).Run(stubCtx{h}, contract.Bead{ID: "ws-build", Kind: contract.KindBuild}); err != nil {
			t.Fatal(err)
		}
		launching = true
		lh := &launchHarness{}
		if _, err := (Franky{}).Launch(stubCtx{lh}, contract.Bead{ID: "ws-surg", Kind: contract.KindSurgery}); err != nil {
			t.Fatal(err)
		}
		return []string{h.invs[0].Prompt, lh.inv.Prompt}
	}
	ctxRepo(t, "")
	base := run("", true)
	ctxRepo(t, ctxCfg)
	if got := run("kind:checkpoint", false); !slices.Equal(got, base) {
		t.Error("unlabelled container changed a prompt")
	}
	got := run("context:mugicrew", false)
	for i := range base {
		if got[i] != base[i]+ctxFocus {
			t.Errorf("prompt %d is not old + focus line", i)
		}
	}
}

// ADR 0157: critique and conflict runs are the old render, plus exactly the focus
// line under a labelled container; no table or an unlabelled one changes nothing.
func TestFrankyFocusLineCritiqueConflict(t *testing.T) {
	beads := map[string]bd.Bead{
		"ws-crit": {ID: "ws-crit", Kind: contract.KindCritique, Status: "in_progress", Parent: "ws-cp", Description: "a review finding"},
		"ws-conf": {ID: "ws-conf", Kind: contract.KindConflict, Status: "in_progress", Parent: "ws-cp", Description: "Base: cafef00d\n"},
	}
	run := func(label string, noShow bool) []string {
		r := &frankySeamRec{spec: "ws-spec"}
		stubFrankySeams(t, r)
		showBead = func(id string) (bd.Bead, error) {
			if id == "ws-cp" {
				if noShow {
					t.Fatal("bd show for a container on a no-context Workspace")
				}
				return bd.Bead{ID: id, Labels: []string{label}}, nil
			}
			return beads[id], nil
		}
		var out []string
		for _, k := range []contract.Kind{contract.KindCritique, contract.KindConflict} {
			h := &recordingHarness{}
			id := map[contract.Kind]string{contract.KindCritique: "ws-crit", contract.KindConflict: "ws-conf"}[k]
			if err := (Franky{}).Run(stubCtx{h}, contract.Bead{ID: id, Kind: k}); err != nil {
				t.Fatal(err)
			}
			out = append(out, h.invs[0].Prompt)
		}
		return out
	}
	ctxRepo(t, "")
	base := run("", true)
	ctxRepo(t, ctxCfg)
	if got := run("kind:checkpoint", false); !slices.Equal(got, base) {
		t.Error("unlabelled container changed a prompt")
	}
	got := run("context:mugicrew", false)
	for i := range base {
		if got[i] != base[i]+ctxFocus {
			t.Errorf("prompt %d is not old + focus line", i)
		}
	}
}

// A dotted id's "." is escaped in the start-step grep, so mc-x.1 cannot match mc-xy1.
func TestSurgeryPromptEscapesDottedID(t *testing.T) {
	p := surgeryPrompt(surgeryArgs{ID: "mc-x.1", Checkpoint: "mc-x", CheckpointTitle: "t"})
	if want := `--grep='^Surgery: mc-x\.1$'`; !strings.Contains(p, want) {
		t.Errorf("surgery prompt missing %q", want)
	}
}

// A Surgery joined to a nested Checkpoint is integrated and counted in the child
// (Spec FR 25): the repo and trunk the reads take are the child's.
func TestEndSurgeryReadsAndIntegratesInTheChild(t *testing.T) {
	_, kid := contextstest.Workspace(t)
	r := &surgeryEndRec{beads: surgeryBeads(), commits: 3, mine: 3}
	cp := r.beads["ws-cp"]
	cp.Labels = []string{contextstest.Label}
	r.beads["ws-cp"] = cp
	stubSurgeryEnd(t, r)
	var got []git.Repo
	oInt, oMine := integrateSurgery, commitsByTrailer
	integrateSurgery = func(sg bd.Bead, repo git.Repo) error { got = append(got, repo); return oInt(sg, repo) }
	commitsByTrailer = func(repo git.Repo, base, branch, trailer string) (int, error) {
		got = append(got, repo)
		if base != "main" {
			t.Errorf("base = %q, want the child's trunk", base)
		}
		return oMine(repo, base, branch, trailer)
	}
	if err := EndSurgery("ws-sg"); err != nil {
		t.Fatal(err)
	}
	if want := []git.Repo{git.Repo(kid), git.Repo(kid)}; !slices.Equal(got, want) {
		t.Errorf("repos = %v, want %v", got, want)
	}
}

// A requested rebase has no failed Landing behind it: the conflict prompt carries
// the requested arm and the shared close path closes it as for any conflict.
func TestRunRequestedConflictClosesWithoutLanding(t *testing.T) {
	r := &frankySeamRec{spec: "ws-spec"}
	stubFrankySeams(t, r)
	conf := bd.Bead{ID: "ws-conf", Kind: contract.KindConflict, Status: "in_progress", Parent: "ws-cp",
		Description: "requested rebase\nBase: cafef00d\nTip: t1p\nvariant: requested"}
	showBead = func(id string) (bd.Bead, error) { return conf, nil }
	h := &recordingHarness{}
	if err := (Franky{}).Run(stubCtx{h}, contract.Bead{ID: "ws-conf", Kind: contract.KindConflict}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.invs) != 1 || !strings.Contains(h.invs[0].Prompt, "variant: requested") {
		t.Fatalf("prompt lacks requested arm: %v", h.invs)
	}
	if len(r.closed) != 1 {
		t.Errorf("want the conflict closed once, got %v", r.closed)
	}
}
