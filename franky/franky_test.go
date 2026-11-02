package franky

import (
	"errors"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/bd/bdtest"
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

// cpGraph seeds a Checkpoint "ws-cp" caused-by a Spec "ws-spec" (when withSpec) and
// the given beads, and installs it as bd's core.
func cpGraph(t *testing.T, withSpec bool, beads ...bd.Bead) *bdtest.Fake {
	t.Helper()
	cp := bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open", Container: true}
	seed := []bd.Bead{cp}
	if withSpec {
		seed[0].CausedBy = "ws-spec"
		seed = append(seed, bd.Bead{ID: "ws-spec", Title: "the spec", Kind: contract.KindSpec, Status: "closed"})
	}
	return bdtest.New(t, append(seed, beads...)...)
}

func openBuild() bd.Bead {
	return bdtest.Claimed(bd.Bead{ID: "ws-build", Kind: contract.KindBuild, Parent: "ws-cp"})
}

func closeReason(f *bdtest.Fake, id string) string {
	b := f.Bead(id)
	if b.Open() {
		return "<open>"
	}
	return b.CloseReason
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
	cpGraph(t, true, bd.Bead{ID: "ws-surg", Title: "t-ws-surg", Kind: contract.KindSurgery, Status: "in_progress", Parent: "ws-cp"})
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
		"⏸ Surgery idle — /exit; enter on its row resumes it.",
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
	cpGraph(t, true,
		bd.Bead{ID: "ws-s0", Kind: contract.KindSurgery, Status: "closed", Parent: "ws-cp"},
		bd.Bead{ID: "ws-surg", Kind: contract.KindSurgery, Status: "in_progress", Parent: "ws-cp"},
		bd.Bead{ID: "ws-s2", Kind: contract.KindSurgery, Status: "in_progress", Parent: "ws-cp"},
		bd.Bead{ID: "open-build", Kind: contract.KindBuild, Status: "open", Parent: "ws-cp"},
		bd.Bead{ID: "done-build", Kind: contract.KindBuild, Status: "closed", Parent: "ws-cp"},
		bd.Bead{ID: "open-critique", Kind: contract.KindCritique, Status: "open", Parent: "ws-cp"},
		bd.Bead{ID: "done-critique", Kind: contract.KindCritique, Status: "closed", Parent: "ws-cp"})
	a, err := surgeryContext("ws-surg")
	if err != nil {
		t.Fatal(err)
	}
	if a.Checkpoint != "ws-cp" || a.SpecID != "ws-spec" || a.SpecTitle != "the spec" {
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
	for _, want := range []string{
		"not needed or is\nalready fixed",
		`bd close mc-x --reason "rejected: <why`,
		`bd close mc-x --reason "accepted: <what`,
		`bd close mc-x --reason "deferred: <why`,
		"relayed whole",
		`bd comments add mc-x "Rationale: <what changed and why>"`,
	} {
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
	for _, want := range []string{"effort/mc-x", "git log --oneline checkpoint/ws-cp..HEAD", "git rebase --continue", "git rebase --abort", `bd comments add mc-x "Not built: <why>"`, `bd comments add mc-x "Behind mainline: <what's missing>"`} {
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
	// The raise the tail keys on is a kind:pivot discovered-from this bead; the
	// retired human-label placeholder must be gone from both prompts (FR50).
	for id, p := range map[string]string{"mc-x": b, "mc-y": c} {
		for _, want := range []string{"kind:pivot", "discovered-from:" + id, "bd dep add"} {
			if !strings.Contains(p, want) {
				t.Errorf("prompt for %s must carry the pivot raise (%q)", id, want)
			}
		}
		const anchor = "bd create --type task --labels kind:critique"
		i := strings.Index(p, anchor)
		if i < 0 {
			if id == "mc-x" { // the Build prompt files Critiques; the conflict prompt does not
				t.Errorf("prompt for %s lacks the Critique create (%q)", id, anchor)
			}
		} else if n := strings.Index(p[i:], "--title"); n < 0 {
			t.Fatalf("prompt for %s: the Critique create has no --title", id)
		} else if create := p[i : i+n]; !strings.Contains(create, "--deps discovered-from:"+id) {
			t.Errorf("prompt for %s: the Critique create must carry --deps discovered-from:%s, got %q", id, id, create)
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
// bead, never an Errand.
func TestAFKPromptsFileExternalWorkAsSignal(t *testing.T) {
	for id, p := range map[string]string{
		"mc-x": implementPrompt(promptArgs{BeadID: "mc-x", SpecID: "ws-spec"}),
		"mc-y": conflictPrompt(promptArgs{BeadID: "mc-y", Base: "deadbeef"}),
	} {
		for _, want := range []string{"offshoot Signal, never an Errand", "signal-filing.md", "discovered-from " + id} {
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
	for k, want := range map[contract.Kind]string{
		contract.KindBuild: bd.BuiltReason, contract.KindCritique: bd.CompletedReason, contract.KindConflict: bd.ResolvedReason,
	} {
		b := openBuild()
		b.Kind = k
		f := cpGraph(t, false, b)
		if err := finish(b); err != nil {
			t.Fatalf("%s: finish: %v", k, err)
		}
		if got := closeReason(f, "ws-build"); got != want {
			t.Errorf("%s: closed with %q, want %q", k, got, want)
		}
		if kids, _ := bd.ChildCount("ws-cp"); kids != 1 {
			t.Errorf("%s: finish spawned a member, children = %d", k, kids)
		}
	}
}

// Spec D5, D6: a Critique with a Rationale: comment closes accepted: <it>; one
// with none closes completed; no other kind carries a reason from a comment.
func TestFinishCritiqueAcceptedWithRationale(t *testing.T) {
	b := openBuild()
	b.Kind = contract.KindCritique
	f := cpGraph(t, false, b)
	f.AddComment("ws-build", "Rationale: fixed the nil deref; declined the rename")
	if err := finish(b); err != nil {
		t.Fatal(err)
	}
	if got := closeReason(f, "ws-build"); got != "accepted: fixed the nil deref; declined the rename" {
		t.Errorf("reason = %q", got)
	}

	f2 := cpGraph(t, false, b)
	if err := finish(b); err != nil {
		t.Fatal(err)
	}
	if got := closeReason(f2, "ws-build"); got != bd.CompletedReason {
		t.Errorf("no rationale: reason = %q, want completed", got)
	}
}

func TestFinishNonCritiqueIgnoresRationale(t *testing.T) {
	for _, k := range []contract.Kind{contract.KindBuild, contract.KindConflict} {
		b := openBuild()
		b.Kind = k
		f := cpGraph(t, false, b)
		f.AddComment("ws-build", "Rationale: should be ignored")
		if err := finish(b); err != nil {
			t.Fatal(err)
		}
		want := bd.BuiltReason
		if k == contract.KindConflict {
			want = bd.ResolvedReason
		}
		if got := closeReason(f, "ws-build"); got != want {
			t.Errorf("%s: reason %q, want close %q", k, got, want)
		}
	}
}

// Roll-forward idempotency: a re-dispatch after the close already happened (bead
// re-read as closed) is a no-op — no double close, so a Critique's reason stands.
func TestFinishClosedBeadIsNoOp(t *testing.T) {
	for _, k := range []contract.Kind{contract.KindBuild, contract.KindCritique} {
		b := openBuild()
		b.Kind = k
		f := cpGraph(t, false, b)
		f.AddComment("ws-build", "Rationale: x")
		f.Mutate("ws-build", func(b *bd.Bead) { b.Status, b.CloseReason = "closed", "first close" })
		closed := f.Bead("ws-build")
		if err := finish(closed); err != nil {
			t.Fatalf("%s: finish: %v", k, err)
		}
		if got := closeReason(f, "ws-build"); got != "first close" {
			t.Errorf("%s: a closed bead must be left alone, reason now %q", k, got)
		}
	}
}

// Run(build) forks the implement agent then drives the tail through to the close.
// A Rollback successor's parent is caused-by the Rollback, not a Spec: the prompt
// carries the Spec SpecOf resolves through it (Spec Decision 6).
func TestRunBuildForksThenCloses(t *testing.T) {
	f := cpGraph(t, true, openBuild())
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
	if got := closeReason(f, "ws-build"); got != bd.BuiltReason {
		t.Errorf("build must close itself built, got %q", got)
	}
}

// A fork failure propagates so the bead stays open (not closed) — the loop
// re-dispatches next tick.
func TestRunForkFailureLeavesBeadOpen(t *testing.T) {
	f := cpGraph(t, true, openBuild())
	h := &recordingHarness{err: errors.New("build session crashed")}

	if err := (Franky{}).Run(stubCtx{h}, contract.Bead{ID: "ws-build", Kind: contract.KindBuild}); err == nil {
		t.Fatal("a fork failure must propagate")
	}
	if got := f.Bead("ws-build"); !got.Open() {
		t.Errorf("a fork failure must not close the bead, got %q", got.Status)
	}
}

// A raise: the agent created (or merged into) a pivot discovered-from its bead.
// Franky raises it against the Checkpoint's Spec — the gate edges land in the
// graph and the bead closes 'pivoted:' — for the implement and conflict tails alike.
func TestRunPivotRaiseGatesCheckpointAndClosesPivoted(t *testing.T) {
	for name, b := range map[string]bd.Bead{
		"build":    openBuild(),
		"conflict": bdtest.Claimed(bd.Bead{ID: "ws-conf", Kind: contract.KindConflict, Parent: "ws-cp", Description: "Base: cafef00d\n"}),
	} {
		piv := bdtest.Draft(b.ID, bd.Bead{ID: "ws-piv", Kind: contract.KindPivot, DiscoveredFrom: []string{b.ID}})
		f := cpGraph(t, true, b, piv)

		if err := (Franky{}).Run(stubCtx{&recordingHarness{}}, contract.Bead{ID: b.ID, Kind: b.Kind}); err != nil {
			t.Fatalf("%s Run: %v", name, err)
		}
		if got := closeReason(f, b.ID); got != "pivoted: ws-piv" {
			t.Errorf("%s closed with %q, want pivoted: ws-piv", name, got)
		}
		if p := f.Bead("ws-piv"); !slices.Contains(p.Related, "ws-spec") {
			t.Errorf("%s: the pivot must relate to the Spec, got %+v", name, p)
		}
		if cp := f.Bead("ws-cp"); !slices.Contains(cp.BlockedBy, "ws-piv") {
			t.Errorf("%s: the Checkpoint must be gated on the pivot, got %v", name, cp.BlockedBy)
		}
	}
}

// The pivot raise resolves the Spec through bd.SpecOf, so a successor Checkpoint a
// Rollback caused (ADR 0048 §2) still relates its pivot to the original Spec:
// ws-cp →caused-by→ Rollback →related→ old Checkpoint →caused-by→ Spec, which no
// one-hop caused-by read would find.
func TestPivotRaiseResolvesSpecThroughRollback(t *testing.T) {
	piv := bdtest.Draft("ws-build", bd.Bead{ID: "ws-piv", Kind: contract.KindPivot, DiscoveredFrom: []string{"ws-build"}})
	f := bdtest.New(t,
		bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open", Container: true, CausedBy: "ws-rb"},
		bd.Bead{ID: "ws-rb", Kind: contract.KindRollback, Status: "closed", Related: []string{"ws-old"}},
		bd.Bead{ID: "ws-old", Kind: contract.KindCheckpoint, Status: "closed", Container: true, CausedBy: "ws-orig-spec"},
		bd.Bead{ID: "ws-orig-spec", Kind: contract.KindSpec, Status: "closed"},
		openBuild(), piv)
	h := &recordingHarness{}

	if err := (Franky{}).Run(stubCtx{h}, contract.Bead{ID: "ws-build", Kind: contract.KindBuild}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.invs) != 1 || !strings.Contains(h.invs[0].Prompt, "bd show ws-orig-spec") {
		t.Errorf("the build prompt must name the resolved Spec, got %v", h.invs)
	}
	if p := f.Bead("ws-piv"); !slices.Contains(p.Related, "ws-orig-spec") {
		t.Errorf("the pivot must relate to the resolved Spec, got %+v", p)
	}
}

// A pivot with no Spec to relate to is a loud failure: the bead stays open.
func TestRunPivotRaiseWithoutSpecFails(t *testing.T) {
	piv := bdtest.Draft("ws-build", bd.Bead{ID: "ws-piv", Kind: contract.KindPivot, DiscoveredFrom: []string{"ws-build"}})
	f := cpGraph(t, false, openBuild(), piv)
	if err := (Franky{}).Run(stubCtx{&recordingHarness{}}, contract.Bead{ID: "ws-build", Kind: contract.KindBuild}); err == nil {
		t.Fatal("a raise with no Spec must fail")
	}
	if !f.Bead("ws-build").Open() {
		t.Error("the bead must stay open")
	}
}

// Run(conflict) reads the Base: pin off the bead body and hands it to the resolve
// agent, then drives the same tail.
func TestRunConflictReadsBasePin(t *testing.T) {
	conf := bdtest.Claimed(bd.Bead{ID: "ws-conf", Kind: contract.KindConflict, Parent: "ws-cp", Description: "Base: cafef00d\n"})
	cpGraph(t, true, conf)
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
	cpGraph(t, true, bdtest.Claimed(bd.Bead{ID: "ws-crit", Kind: contract.KindCritique, Parent: "ws-cp", Description: "Base: cafef00d\ngate failed: TestX"}))
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
	cpGraph(t, true, bdtest.Claimed(bd.Bead{ID: "ws-crit", Kind: contract.KindCritique, Parent: "ws-cp", Description: "a review finding"}))
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
	cpGraph(t, true, bdtest.Claimed(bd.Bead{ID: "ws-conf", Kind: contract.KindConflict, Parent: "ws-cp", Description: "no pin here"}))
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
	bdtest.New(t, bd.Bead{ID: "x", Kind: contract.KindReview, Status: "open"})
	if err := (Franky{}).Run(stubCtx{&recordingHarness{}}, contract.Bead{ID: "x", Kind: contract.KindReview}); err == nil {
		t.Error("a kind Franky doesn't own must error")
	}
}

// FR49: a confirm sweep closes the bead out from under its running effort. Whether
// the fork then returns cleanly or fails, the re-read sees it closed and the tail
// is a no-op — no error, no gate edge, no second close.
func TestRunOverSweepClosedBeadIsNoOp(t *testing.T) {
	piv := bdtest.Draft("ws-build", bd.Bead{ID: "ws-piv", Kind: contract.KindPivot, DiscoveredFrom: []string{"ws-build"}})
	f := cpGraph(t, true, openBuild(), piv)
	h := &sweepHarness{f: f, recordingHarness: recordingHarness{err: errors.New("session killed")}}
	if err := (Franky{}).Run(stubCtx{h}, contract.Bead{ID: "ws-build", Kind: contract.KindBuild}); err != nil {
		t.Errorf("Run = %v, want nil (swept is done)", err)
	}
	if got := closeReason(f, "ws-build"); got != "swept" {
		t.Errorf("swept bead reason = %q, want it left as swept", got)
	}
	if cp := f.Bead("ws-cp"); len(cp.BlockedBy) != 0 {
		t.Errorf("swept bead gated the Checkpoint: %v", cp.BlockedBy)
	}
}

// sweepHarness closes the bead mid-session, as a confirm sweep would.
type sweepHarness struct {
	recordingHarness
	f *bdtest.Fake
}

func (h *sweepHarness) Run(ctx contract.Ctx, inv contract.Invocation) (contract.Result, error) {
	h.f.Mutate(inv.BeadID, func(b *bd.Bead) { b.Status, b.CloseReason = "closed", "swept" })
	return h.recordingHarness.Run(ctx, inv)
}

// surgeryEndRec fakes the git legs EndSurgery drives: the branch's commit count
// and what integrate moves onto it. The beads are a bdtest graph.
type surgeryEndRec struct {
	f                 *bdtest.Fake
	commits           int
	mine              int    // this Surgery's own trailered squashes among commits
	effort            int    // commits on effort/<id> only, that integrate moves onto the branch
	intErr            error  // integrate's failure
	branch            string // the branch counted
	integrated        bool
	sgOpenAtIntegrate bool // the Surgery was still open when integrate ran
}

// stubSurgeryEnd seeds the Surgery under its Checkpoint, plus extra children of the
// Checkpoint (earlier Surgeries, Builds), and stubs the git and integrate seams.
func stubSurgeryEnd(t *testing.T, r *surgeryEndRec, edit func(sg, cp *bd.Bead), siblings ...bd.Bead) {
	t.Helper()
	sg := bd.Bead{ID: "ws-sg", Kind: contract.KindSurgery, Status: "in_progress", Parent: "ws-cp"}
	cp := bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open", Container: true}
	if edit != nil {
		edit(&sg, &cp)
	}
	r.f = bdtest.New(t, append([]bd.Bead{sg, cp}, siblings...)...)
	oAhead, oInt, oMine := commitsAhead, integrateSurgery, commitsByTrailer
	t.Cleanup(func() { commitsAhead, integrateSurgery, commitsByTrailer = oAhead, oInt, oMine })
	commitsByTrailer = func(_ git.Repo, base, branch, trailer string) (int, error) {
		r.branch = branch
		if trailer != "Surgery: ws-sg" {
			t.Errorf("trailer = %q", trailer)
		}
		return r.mine, nil
	}
	integrateSurgery = func(sg bd.Bead, _ git.Repo) error {
		r.integrated = true
		r.sgOpenAtIntegrate = r.f.Bead(sg.ID).Open()
		if r.intErr == nil {
			r.commits, r.mine, r.effort = r.commits+r.effort, r.mine+r.effort, 0
		}
		return r.intErr
	}
	commitsAhead = func(_ git.Repo, base, branch string) (int, error) {
		return r.commits, nil
	}
}

// state is whether Surgery and Checkpoint read open, as "sg=<reason|open> cp=<reason|open>".
func (r *surgeryEndRec) state() string {
	return "sg=" + closeReason(r.f, "ws-sg") + " cp=" + closeReason(r.f, "ws-cp")
}

// N > 0: the Surgery only closes, spawning no Review — Jinbe spawns it once the
// Checkpoint drains (Spec D8) — and the Checkpoint stays open.
func TestEndSurgeryWithCommitsOnlyCloses(t *testing.T) {
	r := &surgeryEndRec{commits: 3, mine: 3}
	stubSurgeryEnd(t, r, nil)

	if err := EndSurgery("ws-sg"); err != nil {
		t.Fatalf("EndSurgery: %v", err)
	}
	if want := "sg=ended cp=<open>"; r.state() != want {
		t.Fatalf("state = %s, want %s", r.state(), want)
	}
	if !r.sgOpenAtIntegrate {
		t.Error("the Surgery must still be open when integrate runs")
	}
	if r.branch != "checkpoint/ws-cp" {
		t.Errorf("counted %q, want checkpoint/ws-cp", r.branch)
	}
}

// N = 0: the Surgery closes abandoned, then its empty Checkpoint.
func TestEndSurgeryWithNoCommitsClosesBoth(t *testing.T) {
	r := &surgeryEndRec{}
	stubSurgeryEnd(t, r, nil)

	if err := EndSurgery("ws-sg"); err != nil {
		t.Fatalf("EndSurgery: %v", err)
	}
	const why = "abandoned: surgery ended with no commits"
	if want := "sg=" + why + " cp=" + why; r.state() != want {
		t.Fatalf("state = %s, want %s", r.state(), want)
	}
}

// A joined zero-commit Surgery closes only itself: Builds' commits ahead, or
// other children (a stacked surgery Checkpoint's earlier Surgery), keep the
// Checkpoint open (Spec D8).
func TestEndSurgeryZeroCommitsKeepsSharedCheckpoint(t *testing.T) {
	const why = "abandoned: surgery ended with no commits"
	earlier := bd.Bead{ID: "ws-sg0", Kind: contract.KindSurgery, Status: "closed", Parent: "ws-cp"}
	for name, r := range map[string]*surgeryEndRec{
		"builds ahead":    {commits: 2},
		"earlier sibling": {},
	} {
		var sibs []bd.Bead
		if name == "earlier sibling" {
			sibs = append(sibs, earlier)
		} else {
			sibs = append(sibs, bd.Bead{ID: "ws-b1", Kind: contract.KindBuild, Status: "closed", Parent: "ws-cp"})
		}
		stubSurgeryEnd(t, r, nil, sibs...)
		if err := EndSurgery("ws-sg"); err != nil {
			t.Fatalf("%s: EndSurgery: %v", name, err)
		}
		if want := "sg=" + why + " cp=<open>"; r.state() != want {
			t.Errorf("%s: state = %s, want %s", name, r.state(), want)
		}
	}
}

// surgery end integrates before it counts (Spec D8, FR 33): commits only on
// effort/<id> reach the Checkpoint and the Surgery reads as done, not empty. A
// failed integrate closes nothing.
func TestEndSurgeryIntegratesBeforeCounting(t *testing.T) {
	r := &surgeryEndRec{effort: 2}
	stubSurgeryEnd(t, r, nil)
	if err := EndSurgery("ws-sg"); err != nil {
		t.Fatalf("EndSurgery: %v", err)
	}
	if want := "sg=ended cp=<open>"; r.state() != want {
		t.Fatalf("state = %s, want %s", r.state(), want)
	}

	r = &surgeryEndRec{effort: 2, intErr: errors.New("integrate: conflict")}
	stubSurgeryEnd(t, r, nil)
	if err := EndSurgery("ws-sg"); err == nil {
		t.Error("EndSurgery = nil after a failed integrate")
	}
	if want := "sg=<open> cp=<open>"; r.state() != want {
		t.Errorf("state = %s, want %s: a failed integrate closes nothing", r.state(), want)
	}
}

// The verb refuses anything but an open Surgery under a Checkpoint, touching nothing.
func TestEndSurgeryRefusesBadBeads(t *testing.T) {
	for name, edit := range map[string]func(sg, cp *bd.Bead){
		"wrong kind":            func(sg, _ *bd.Bead) { sg.Kind = contract.KindBuild },
		"closed":                func(sg, _ *bd.Bead) { sg.Status = "closed" },
		"no parent":             func(sg, _ *bd.Bead) { sg.Parent = "" },
		"non-checkpoint parent": func(_, cp *bd.Bead) { cp.Kind = contract.KindChart },
	} {
		r := &surgeryEndRec{commits: 1, mine: 1}
		stubSurgeryEnd(t, r, edit)
		before := r.state()
		if err := EndSurgery("ws-sg"); err == nil {
			t.Errorf("%s: EndSurgery = nil, want a refusal", name)
		}
		if r.integrated || r.state() != before {
			t.Errorf("%s: a refusal must touch nothing, state %s -> %s, integrated=%v", name, before, r.state(), r.integrated)
		}
	}
}

// A read fault on the Checkpoint surfaces and closes nothing.
func TestEndSurgeryCheckpointReadFaultClosesNothing(t *testing.T) {
	r := &surgeryEndRec{commits: 1, mine: 1}
	stubSurgeryEnd(t, r, nil)
	r.f.FailOn("Show", "ws-cp", errors.New("dolt down"))
	if err := EndSurgery("ws-sg"); err == nil {
		t.Fatal("EndSurgery = nil after a failed Checkpoint read")
	}
	if want := "sg=<open> cp=<open>"; r.state() != want {
		t.Errorf("state = %s, want %s", r.state(), want)
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
	cpGraph(t, false, openBuild(), bd.Bead{ID: "ws-surg", Title: "t", Kind: contract.KindSurgery, Status: "closed", Parent: "ws-cp"})
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
	run := func(label string) []string {
		cp := bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open", Container: true, CausedBy: "ws-spec"}
		if label != "" {
			cp.Labels = []string{label}
		}
		bdtest.New(t, cp, bd.Bead{ID: "ws-spec", Kind: contract.KindSpec, Status: "closed"}, openBuild(),
			bd.Bead{ID: "ws-surg", Kind: contract.KindSurgery, Status: "in_progress", Parent: "ws-cp"})
		h := &recordingHarness{}
		if err := (Franky{}).Run(stubCtx{h}, contract.Bead{ID: "ws-build", Kind: contract.KindBuild}); err != nil {
			t.Fatal(err)
		}
		lh := &launchHarness{}
		if _, err := (Franky{}).Launch(stubCtx{lh}, contract.Bead{ID: "ws-surg", Kind: contract.KindSurgery}); err != nil {
			t.Fatal(err)
		}
		return []string{h.invs[0].Prompt, lh.inv.Prompt}
	}
	ctxRepo(t, "")
	base := run("")
	ctxRepo(t, ctxCfg)
	if got := run("kind:checkpoint"); !slices.Equal(got, base) {
		t.Error("unlabelled container changed a prompt")
	}
	got := run("context:mugicrew")
	for i := range base {
		if got[i] != base[i]+ctxFocus {
			t.Errorf("prompt %d is not old + focus line", i)
		}
	}
}

// ADR 0157: critique and conflict runs are the old render, plus exactly the focus
// line under a labelled container; no table or an unlabelled one changes nothing.
func TestFrankyFocusLineCritiqueConflict(t *testing.T) {
	run := func(label string) []string {
		cp := bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open", Container: true, CausedBy: "ws-spec"}
		if label != "" {
			cp.Labels = []string{label}
		}
		bdtest.New(t, cp, bd.Bead{ID: "ws-spec", Kind: contract.KindSpec, Status: "closed"},
			bdtest.Claimed(bd.Bead{ID: "ws-crit", Kind: contract.KindCritique, Parent: "ws-cp", Description: "a review finding"}),
			bdtest.Claimed(bd.Bead{ID: "ws-conf", Kind: contract.KindConflict, Parent: "ws-cp", Description: "Base: cafef00d\n"}))
		var out []string
		for _, b := range []contract.Bead{{ID: "ws-crit", Kind: contract.KindCritique}, {ID: "ws-conf", Kind: contract.KindConflict}} {
			h := &recordingHarness{}
			if err := (Franky{}).Run(stubCtx{h}, b); err != nil {
				t.Fatal(err)
			}
			out = append(out, h.invs[0].Prompt)
		}
		return out
	}
	ctxRepo(t, "")
	base := run("")
	ctxRepo(t, ctxCfg)
	if got := run("kind:checkpoint"); !slices.Equal(got, base) {
		t.Error("unlabelled container changed a prompt")
	}
	got := run("context:mugicrew")
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
	r := &surgeryEndRec{commits: 3, mine: 3}
	stubSurgeryEnd(t, r, func(_, cp *bd.Bead) { cp.Labels = []string{contextstest.Label} })
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
	f := cpGraph(t, true, bdtest.Claimed(bd.Bead{ID: "ws-conf", Kind: contract.KindConflict, Parent: "ws-cp",
		Description: "requested rebase\nBase: cafef00d\nTip: t1p\nvariant: requested"}))
	h := &recordingHarness{}
	if err := (Franky{}).Run(stubCtx{h}, contract.Bead{ID: "ws-conf", Kind: contract.KindConflict}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.invs) != 1 || !strings.Contains(h.invs[0].Prompt, "variant: requested") {
		t.Fatalf("prompt lacks requested arm: %v", h.invs)
	}
	if got := closeReason(f, "ws-conf"); got != bd.ResolvedReason {
		t.Errorf("want the conflict closed resolved, got %q", got)
	}
}

// A replay Conflict's prompt names the three-way merge-tree and the seed trailers,
// and tells Franky not to rebase onto the base.
func TestConflictPromptReplayArm(t *testing.T) {
	p := conflictPrompt(promptArgs{BeadID: "mc-x", Base: "b4se"})
	for _, want := range []string{"variant: replay", "git merge-tree --write-tree --merge-base=<Replay-from> b4se <Tip>",
		"Seed-trailer:", "Replayed: <Replay-from>..<Tip>", "Remnant: <id>", "ONE seed commit"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
}

// Every Franky prompt that runs mugicrew integrate carries the exit block
// spliced from the session exit constants (ADR 0328).
func TestPromptsCarryIntegrateExits(t *testing.T) {
	for name, got := range map[string]string{
		"build":    implementPrompt(promptArgs{BeadID: "mc-x", Checkpoint: "ws-cp"}),
		"conflict": conflictPrompt(promptArgs{BeadID: "mc-x", Base: "b4se"}),
		"surgery":  surgeryPrompt(surgeryArgs{ID: "ws-sg"}),
	} {
		// surgery nests the block under a numbered step, so compare line by line.
		for _, line := range strings.Split(contract.IntegrateExits(), "\n") {
			if !strings.Contains(got, line) {
				t.Errorf("%s prompt lacks the integrate exit line %q", name, line)
			}
		}
	}
}

// Spec FR 7–9: every committing Franky prompt (build, surgery, conflict; with and
// without a Spec or Surgeries) names the <type>(<bead-id>) commit form, and the
// conflict prompt still keeps replayed commits' messages and trailers.
func TestFrankyPromptsConventionalCommit(t *testing.T) {
	surgeries := []bd.Bead{{ID: "ws-s1", Title: "A surgery"}}
	prompts := map[string]string{
		"surgery/spec":    surgeryPrompt(surgeryArgs{ID: "ws-surg", Checkpoint: "ws-cp", SpecID: "ws-spec"}),
		"surgery/nospec":  surgeryPrompt(surgeryArgs{ID: "ws-surg", Checkpoint: "ws-cp"}),
		"build/spec":      implementPrompt(promptArgs{BeadID: "mc-x", Checkpoint: "ws-cp", SpecID: "ws-spec"}),
		"build/surgeries": implementPrompt(promptArgs{BeadID: "mc-x", Checkpoint: "ws-cp", Surgeries: surgeries}),
		"build/bare":      implementPrompt(promptArgs{BeadID: "mc-x", Checkpoint: "ws-cp"}),
		"conflict/spec":   conflictPrompt(promptArgs{BeadID: "mc-y", Base: "deadbeef", SpecID: "ws-spec"}),
		"conflict/surg":   conflictPrompt(promptArgs{BeadID: "mc-y", Base: "deadbeef", Surgeries: surgeries}),
		"conflict/bare":   conflictPrompt(promptArgs{BeadID: "mc-y", Base: "deadbeef"}),
	}
	for name, p := range prompts {
		if v := promptguard.CommitRuleViolations(p); len(v) > 0 {
			t.Errorf("%s prompt breaks the conventional-commit guard: %v", name, v)
		}
		if !strings.Contains(p, "<type>(<bead-id>): <subject>") {
			t.Errorf("%s prompt must spell out <type>(<bead-id>): <subject>", name)
		}
		if strings.HasPrefix(name, "conflict/") && !strings.Contains(p, "Keep every commit message and its trailers") {
			t.Errorf("%s prompt must keep replayed commits' messages and trailers", name)
		}
	}
}

// Spec mc-4cjq1 FR 15: a prompt that may write a numbered file points at
// crew doc sequence-number.md. The map takes every such prompt.
func TestNumberedFileWritersPointAtSequenceNumber(t *testing.T) {
	prompts := map[string]string{
		"franky build": implementPrompt(promptArgs{BeadID: "mc-x", Checkpoint: "ws-cp", SpecID: "ws-spec"}),
	}
	// The other crews' prompts are unexported embeds; read their sources.
	for name, path := range map[string]string{
		"nami charting":        "../nami/prompts/charting.md",
		"nami grilling-source": "../nami/prompts/grilling-source.md",
		"zoro pivot":           "../zoro/prompts/pivot.md",
		"sanji prototype":      "../sanji/prompts/prototype.md",
	} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		prompts[name] = string(b)
	}
	for name, p := range prompts {
		if !strings.Contains(strings.Join(strings.Fields(p), " "), "crew doc sequence-number.md") {
			t.Errorf("%s prompt does not reference crew doc sequence-number.md", name)
		}
	}
}

// A failed caused-by read is an error, not "no Spec": the build forks nothing and
// stays open for the next tick (FR 6).
func TestRunSpecReadFaultForksNothing(t *testing.T) {
	f := cpGraph(t, true, openBuild())
	f.FailOn("Edges", "ws-cp", errors.New("dolt down"))
	h := &recordingHarness{}
	if err := (Franky{}).Run(stubCtx{h}, contract.Bead{ID: "ws-build", Kind: contract.KindBuild}); err == nil {
		t.Fatal("a failed Spec read must fail the run")
	}
	if len(h.invs) != 0 || !f.Bead("ws-build").Open() {
		t.Errorf("forked %d sessions, build open = %v; want none, open", len(h.invs), f.Bead("ws-build").Open())
	}
}

// The Critique backstop runs once, before any raise or close, on the completed
// path and the pivoted path alike: a Critique Draft filed without --deps comes out
// discovered-from the Build, and its edge is written before the bead closes.
func TestTailTiesCritiquesBeforeRaiseOrClose(t *testing.T) {
	for _, pivoted := range []bool{false, true} {
		crit := bdtest.Draft("ws-build", bd.Bead{ID: "ws-crit", Kind: contract.KindCritique})
		beads := []bd.Bead{openBuild(), crit}
		if pivoted {
			beads = append(beads, bdtest.Draft("ws-build", bd.Bead{ID: "ws-piv", Kind: contract.KindPivot, DiscoveredFrom: []string{"ws-build"}}))
		}
		f := cpGraph(t, true, beads...)
		if err := (Franky{}).Run(stubCtx{&recordingHarness{}}, contract.Bead{ID: "ws-build", Kind: contract.KindBuild}); err != nil {
			t.Fatalf("pivoted=%v Run: %v", pivoted, err)
		}
		if c := f.Bead("ws-crit"); !slices.Contains(c.DiscoveredFrom, "ws-build") {
			t.Errorf("pivoted=%v: the Critique must be discovered-from ws-build, got %+v", pivoted, c)
		}
		if f.Bead("ws-build").Open() {
			t.Errorf("pivoted=%v: the build must close", pivoted)
		}
		if !f.EdgeBeforeClose("ws-crit", "ws-build", "ws-build") {
			t.Errorf("pivoted=%v: the tie must precede the close, calls %v", pivoted, f.Calls())
		}
	}
}

// closingHarness stands in for a Critique session that closes its own bead
// (franky/prompts/build.md: "rejected:", "accepted:", "deferred:").
type closingHarness struct {
	recordingHarness
	close func()
}

func (h *closingHarness) Run(ctx contract.Ctx, inv contract.Invocation) (contract.Result, error) {
	h.close()
	return h.recordingHarness.Run(ctx, inv)
}

// A Critique session that closes its own bead still gets the backstop: dispatch
// publishes that bead's Drafts, so a kind:critique Draft filed without --deps
// must come out discovered-from the Critique.
func TestTailTiesCritiquesWhenSessionClosesItsOwnBead(t *testing.T) {
	crit := bdtest.Claimed(bd.Bead{ID: "ws-self", Kind: contract.KindCritique, Parent: "ws-cp"})
	draft := bdtest.Draft("ws-self", bd.Bead{ID: "ws-new", Kind: contract.KindCritique})
	f := cpGraph(t, true, crit, draft)
	h := &closingHarness{close: func() {
		if err := f.Close([]string{"ws-self"}, "rejected: not needed", false); err != nil {
			t.Fatal(err)
		}
	}}
	if err := (Franky{}).Run(stubCtx{h}, contract.Bead{ID: "ws-self", Kind: contract.KindCritique}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if c := f.Bead("ws-new"); !slices.Contains(c.DiscoveredFrom, "ws-self") {
		t.Errorf("the Draft must be discovered-from ws-self, got %+v", c)
	}
}
