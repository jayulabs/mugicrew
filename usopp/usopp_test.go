package usopp

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/promptguard"
)

// recordingHarness is the fake contract.Harness the handler tests dispatch
// through: it records every AFK invocation and can be made to fail. No live
// Claude Code. Mirrors franky_test's recordingHarness.
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

// usoppSeamRec captures the bd/git legs so the tests can assert the clean→Approval
// transition, the spawn-before-close ordering, and the pinned Approval shape
// without live bd/git. Mirrors frankySeamRec.
type usoppSeamRec struct {
	openCritiques int    // openChildrenOfKind(_, KindCritique) result
	head          string // pinned HEAD returned by the head seam
	spawned       []bd.Successor
	closed        []string
	events        []string  // "spawn"/"close" in call order — proves spawn-before-close
	spawnExisting bool      // model SpawnChildIfAbsent finding an existing open Approval
	openApprovals []bd.Bead // existing OPEN Approval children the obsoletion scans
	reasons       []string  // close reasons, parallel to closed
	pivots        []bd.Bead // the Checkpoint's open pivots (OpenPivots)
	raised        []bd.Bead // open pivots discovered-from the Review (PivotsDiscoveredFrom)
	raises        []string  // raisePivot calls: "<bead> <spec> <pivot ids>"
}

func stubUsoppSeams(t *testing.T, r *usoppSeamRec) {
	t.Helper()
	oShow, oClose, oSpawn, oOpen, oOpenBeads, oSpec, oHead := showBead, closeBead, spawnChild, openChildrenOfKind, openChildrenBeadsOfKind, specOf, head
	oPivots, oRaised, oRaise, oPublish, oSurgery := openPivots, pivotsDiscoveredFrom, raisePivot, closeOnPublish, surgeriesOf
	t.Cleanup(func() {
		showBead, closeBead, spawnChild, openChildrenOfKind, openChildrenBeadsOfKind, specOf, head = oShow, oClose, oSpawn, oOpen, oOpenBeads, oSpec, oHead
		openPivots, pivotsDiscoveredFrom, raisePivot, closeOnPublish, surgeriesOf = oPivots, oRaised, oRaise, oPublish, oSurgery
	})
	surgeriesOf = func(string) ([]bd.Bead, error) { return nil, nil }
	oChanged := changedPaths
	t.Cleanup(func() { changedPaths = oChanged })
	changedPaths = func(string, string) ([]string, error) { return nil, nil }
	openPivots = func(string) ([]bd.Bead, error) { return r.pivots, nil }
	pivotsDiscoveredFrom = func(string) ([]bd.Bead, error) { return r.raised, nil }
	// The raise's edges and close are bd.RaisePivot's (tested in bd); Usopp's job
	// is handing it the Review, the resolved Spec and the raised pivots.
	raisePivot = func(_ contract.Ctx, b bd.Bead, _, spec string, ps []bd.Bead) error {
		r.raises = append(r.raises, b.ID+" "+spec+" "+strings.Join(bd.IDs(ps), ","))
		return nil
	}
	// Outside an effort (stubCtx) the flow publishes the Review's close itself; in
	// one, bd.CloseOnPublish only hands it to the Publisher.
	closeOnPublish = func(ctx contract.Ctx, id, reason string, es []bd.Edge) error {
		if _, ok := ctx.(contract.Publisher); ok {
			return bd.CloseOnPublish(ctx, id, reason, es)
		}
		r.events = append(r.events, "close")
		r.closed = append(r.closed, id)
		r.reasons = append(r.reasons, reason)
		return nil
	}
	openChildrenBeadsOfKind = func(_ string, k contract.Kind) ([]bd.Bead, error) {
		if k == contract.KindApproval {
			return r.openApprovals, nil
		}
		return nil, nil
	}
	closeBead = func(id, reason string) error {
		if id == "ws-rev" {
			t.Errorf("the Review's close must go through the publish, not a direct close (Spec D6)")
		}
		r.events = append(r.events, "close")
		r.closed = append(r.closed, id)
		r.reasons = append(r.reasons, reason)
		return nil
	}
	spawnChild = func(_ string, s bd.Successor) (string, bool, error) {
		r.events = append(r.events, "spawn")
		r.spawned = append(r.spawned, s)
		return "ws-appr", !r.spawnExisting, nil
	}
	openChildrenOfKind = func(_ string, k contract.Kind) (int, error) {
		if k == contract.KindCritique {
			return r.openCritiques, nil
		}
		return 0, nil
	}
	specOf = func(string) (string, bool, error) { return "ws-spec", true, nil }
	head = func(string) (string, error) { return r.head, nil }
	showBead = func(id string) (bd.Bead, error) {
		if id == "ws-cp" {
			return bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open", Title: "the checkpoint title"}, nil
		}
		return openReview(), nil
	}
}

// The Review prompt's Spec id comes from bd.SpecOf, so a successor Checkpoint a
// Rollback caused (ADR 0048 §2) is reviewed against the original Spec, not "".
func TestReviewResolvesSpecThroughSpecOf(t *testing.T) {
	if reflect.ValueOf(specOf).Pointer() != reflect.ValueOf(bd.SpecOf).Pointer() {
		t.Error("usopp's Spec seam must default to bd.SpecOf, not a one-hop caused-by walk")
	}
	r := &usoppSeamRec{head: "beefcafe", raised: []bd.Bead{{ID: "ws-piv"}}}
	stubUsoppSeams(t, r)
	// Stubbed as SpecOf answers a Rollback-caused parent: the Spec reached through
	// the Rollback, which no one-hop caused-by read would find.
	specOf = func(id string) (string, bool, error) {
		if id != "ws-cp" {
			return "", false, nil
		}
		return "ws-orig-spec", true, nil
	}
	showBead = func(string) (bd.Bead, error) { return openReview(), nil }
	h := &recordingHarness{}

	if err := (Usopp{}).Run(stubCtx{h}, contract.Bead{ID: "ws-rev", Kind: contract.KindReview}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.invs) != 1 || !strings.Contains(h.invs[0].Prompt, "ws-orig-spec") {
		t.Errorf("the review prompt must name the resolved Spec, got %v", h.invs)
	}
	if want := []string{"ws-rev ws-orig-spec ws-piv"}; !reflect.DeepEqual(r.raises, want) {
		t.Errorf("the pivot must relate to the resolved Spec, got %v", r.raises)
	}
}

func openReview() bd.Bead {
	return bd.Bead{ID: "ws-rev", Kind: contract.KindReview, Status: "in_progress", Parent: "ws-cp"}
}

// Launch errors for every kind — review is AFK, not launchable.
func TestLaunchErrorsForEveryKind(t *testing.T) {
	for _, k := range []contract.Kind{contract.KindReview, contract.KindApproval, contract.KindBuild} {
		if _, err := (Usopp{}).Launch(stubCtx{&recordingHarness{}}, contract.Bead{ID: "x", Kind: k}); err == nil {
			t.Errorf("Launch of AFK kind %q must error", k)
		}
	}
}

// The prompt renders with every arg (and carries no stray % that fmt.Sprintf would
// turn into %!(...)) — guards a broken embed or a literal %.
func TestPromptRenders(t *testing.T) {
	p := reviewPrompt(reviewArgs{ReviewID: "ws-rev", CheckpointID: "ws-cp", SpecID: "ws-spec", Branch: "checkpoint/ws-cp", Pin: "deadbeef"})
	for _, want := range []string{"ws-rev", "ws-cp", "ws-spec", "checkpoint/ws-cp", "deadbeef"} {
		if !strings.Contains(p, want) {
			t.Errorf("review prompt missing %q", want)
		}
	}
	if strings.Contains(p, "%!") {
		t.Errorf("review prompt has a stray %%: %q", p)
	}
	// The raise Go keys on, per-FR pivot dedupe, in_progress-Critique dedupe, the
	// refiled-Critique cover, and the AMENDMENT reading rule (FRs 2–5, 17–19, 37);
	// the rule itself lives in the checkpoint-rules Crew doc the prompt reads.
	for _, want := range []string{contract.DraftInstruction("ws-rev"), "kind:critique,drafted-by:ws-rev -s deferred", "kind:pivot,drafted-by:ws-rev -s deferred", "discovered-from:ws-rev", "bd dep add <pivotID> ws-rev", "OWN FR", "in_progress", "refiled Critique"} {
		if !strings.Contains(p, want) {
			t.Errorf("review prompt missing %q", want)
		}
	}
	rules := promptguard.Docs(t)["checkpoint-rules.md"]
	for _, want := range []string{"AMENDMENT (", "latest wins"} {
		if !strings.Contains(rules, want) {
			t.Errorf("checkpoint-rules.md missing %q", want)
		}
	}
}

// Spec FR 12: external work found mid-run is an offshoot Signal by the
// Review, never a Task.
func TestReviewPromptFilesExternalWorkAsSignal(t *testing.T) {
	p := reviewPrompt(reviewArgs{ReviewID: "ws-rev", CheckpointID: "ws-cp", SpecID: "ws-spec"})
	for _, want := range []string{"offshoot Signal, never a Task", "signal-filing.md", "discovered-from ws-rev"} {
		if !strings.Contains(p, want) {
			t.Errorf("review prompt missing %q", want)
		}
	}
}

// Spec D6 over the review prompt as rendered: no skill call, no ADR
// citation, every crew-doc reference resolves, and it reads the code-review and
// checkpoint-rules Crew docs.
func TestReviewPromptSkillFree(t *testing.T) {
	p := reviewPrompt(reviewArgs{ReviewID: "ws-rev", CheckpointID: "ws-cp", SpecID: "ws-spec", Branch: "checkpoint/ws-cp", Pin: "deadbeef"})
	promptguard.Check(t, "review", p)
	refs := promptguard.CrewDocRefs(p)
	for _, d := range []string{"code-review.md", "checkpoint-rules.md"} {
		if !slices.Contains(refs, d) {
			t.Errorf("review prompt reads no crew doc %s (refs %q)", d, refs)
		}
	}
}

// Clean pass: zero open critiques ⇒ Approval spawned FIRST (pinned), THEN Review
// closed — so "predecessor closed, successor missing" is unreachable.
func TestApproveIfCleanSpawnsPinnedApprovalBeforeClose(t *testing.T) {
	r := &usoppSeamRec{openCritiques: 0}
	stubUsoppSeams(t, r)

	if err := approveIfClean(stubCtx{}, openReview(), "cafef00d"); err != nil {
		t.Fatalf("approveIfClean: %v", err)
	}
	if len(r.events) != 2 || r.events[0] != "spawn" || r.events[1] != "close" {
		t.Fatalf("must spawn the Approval BEFORE closing the Review, got %v", r.events)
	}
	if len(r.spawned) != 1 {
		t.Fatalf("a clean pass must spawn exactly one Approval, got %v", r.spawned)
	}
	appr := r.spawned[0]
	if appr.Kind != contract.KindApproval || appr.Container {
		t.Errorf("Approval must be a kind:approval leaf, got %+v", appr)
	}
	if appr.Title != "the checkpoint title" {
		t.Errorf("Approval title = %q, want the Checkpoint's title verbatim", appr.Title)
	}
	if pin, ok := git.ParseCommit(appr.Description); !ok || pin != "cafef00d" {
		t.Errorf("Approval must carry the Commit: pin captured at review start, got %q", appr.Description)
	}
	if len(r.closed) != 1 || r.closed[0] != "ws-rev" {
		t.Errorf("must close the Review after spawning, got %v", r.closed)
	}
}

// Dirty pass: ≥1 open critique ⇒ Review still closed, but NO Approval spawned.
func TestApproveIfCleanClosesWithoutApprovalWhenCritiquesOpen(t *testing.T) {
	r := &usoppSeamRec{openCritiques: 2}
	stubUsoppSeams(t, r)

	if err := approveIfClean(stubCtx{}, openReview(), "cafef00d"); err != nil {
		t.Fatalf("approveIfClean: %v", err)
	}
	if len(r.spawned) != 0 {
		t.Errorf("open critiques must NOT spawn an Approval, got %v", r.spawned)
	}
	if len(r.closed) != 1 {
		t.Errorf("the Review must still close on a dirty pass, got %v", r.closed)
	}
}

// A frozen Checkpoint (open pivot) gets NO Approval even on a clean pass, so the
// HITL Approval never reaches the Inbox under a pivot (FR14) — the Review still closes.
func TestApproveIfCleanOpenPivotHoldsApproval(t *testing.T) {
	r := &usoppSeamRec{openCritiques: 0, pivots: []bd.Bead{{ID: "ws-piv"}}}
	stubUsoppSeams(t, r)

	if err := approveIfClean(stubCtx{}, openReview(), "cafef00d"); err != nil {
		t.Fatalf("approveIfClean: %v", err)
	}
	if len(r.spawned) != 0 {
		t.Errorf("an open pivot must hold the Approval back, got %v", r.spawned)
	}
	if len(r.closed) != 1 || r.closed[0] != "ws-rev" {
		t.Errorf("the Review must still close under a pivot, got %v", r.closed)
	}
}

// A raise: the review agent created (or merged into) a pivot discovered-from the
// Review. Usopp hands the Review, its Spec and the pivots to the raise (which
// wires the gate and closes 'pivoted:') and spawns NO Approval.
func TestRunPivotRaiseHandsOffWithoutApproval(t *testing.T) {
	r := &usoppSeamRec{openCritiques: 0, head: "beefcafe", raised: []bd.Bead{{ID: "ws-piv"}}}
	stubUsoppSeams(t, r)
	showBead = func(string) (bd.Bead, error) { return openReview(), nil }

	if err := (Usopp{}).Run(stubCtx{&recordingHarness{}}, contract.Bead{ID: "ws-rev", Kind: contract.KindReview}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := []string{"ws-rev ws-spec ws-piv"}; !reflect.DeepEqual(r.raises, want) {
		t.Errorf("raises = %v, want %v", r.raises, want)
	}
	if len(r.spawned)+len(r.closed) != 0 {
		t.Errorf("a pivoted review must spawn no Approval and close nothing itself, got spawned=%v closed=%v", r.spawned, r.closed)
	}
}

// a no-op create (SpawnChildIfAbsent's guard) — no double approve, Review closed.
func TestApproveIfCleanIdempotentUnderRedispatch(t *testing.T) {
	r := &usoppSeamRec{openCritiques: 0, spawnExisting: true}
	stubUsoppSeams(t, r)

	if err := approveIfClean(stubCtx{}, openReview(), "cafef00d"); err != nil {
		t.Fatalf("approveIfClean: %v", err)
	}
	if len(r.spawned) != 1 || len(r.closed) != 1 {
		t.Errorf("re-dispatch must spawn-if-absent once and close, got spawned=%v closed=%v", r.spawned, r.closed)
	}
}

// Stale open Approval (pin != reviewed HEAD): a clean re-review OBSOLETES it before
// spawning, so SpawnChildIfAbsent can't reuse an Approval pinned to a superseded
// commit — the FR31 self-heal. The fresh Approval is pinned at the new HEAD.
func TestApproveIfCleanObsoletesStaleApproval(t *testing.T) {
	r := &usoppSeamRec{
		openCritiques: 0,
		openApprovals: []bd.Bead{{ID: "ws-appr-old", Kind: contract.KindApproval, Status: "open", Description: git.CommitPin("oldsha")}},
	}
	stubUsoppSeams(t, r)

	if err := approveIfClean(stubCtx{}, openReview(), "newsha"); err != nil {
		t.Fatalf("approveIfClean: %v", err)
	}
	if !contains(r.closed, "ws-appr-old") {
		t.Errorf("a stale-pinned open Approval must be obsoleted, closed=%v", r.closed)
	}
	if len(r.spawned) != 1 {
		t.Fatalf("a fresh Approval must be spawned, got %v", r.spawned)
	}
	if pin, ok := git.ParseCommit(r.spawned[0].Description); !ok || pin != "newsha" {
		t.Errorf("the respawned Approval must pin the new HEAD, got %q", r.spawned[0].Description)
	}
	if !contains(r.closed, "ws-rev") {
		t.Errorf("the Review must still close, closed=%v", r.closed)
	}
}

// An open Approval ALREADY at HEAD is NOT obsoleted — SpawnChildIfAbsent reuses it,
// so a plain re-dispatch (HEAD unchanged) neither churns nor double-approves.
func TestApproveIfCleanKeepsApprovalAlreadyAtHead(t *testing.T) {
	r := &usoppSeamRec{
		openCritiques: 0,
		spawnExisting: true,
		openApprovals: []bd.Bead{{ID: "ws-appr", Kind: contract.KindApproval, Status: "open", Description: git.CommitPin("samehead")}},
	}
	stubUsoppSeams(t, r)

	if err := approveIfClean(stubCtx{}, openReview(), "samehead"); err != nil {
		t.Fatalf("approveIfClean: %v", err)
	}
	if contains(r.closed, "ws-appr") {
		t.Errorf("an Approval already at HEAD must NOT be obsoleted, closed=%v", r.closed)
	}
	if len(r.closed) != 1 || r.closed[0] != "ws-rev" {
		t.Errorf("only the Review should close, got %v", r.closed)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// Run(review) pins the branch HEAD, forks the read-only review agent anchored to
// the bead, then drives the clean→Approval transition through to spawn+close.
func TestRunReviewPinsForksThenApproves(t *testing.T) {
	r := &usoppSeamRec{openCritiques: 0, head: "beefcafe"}
	stubUsoppSeams(t, r)
	showBead = func(string) (bd.Bead, error) { return openReview(), nil }
	h := &recordingHarness{}

	if err := (Usopp{}).Run(stubCtx{h}, contract.Bead{ID: "ws-rev", Kind: contract.KindReview}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.invs) != 1 || h.invs[0].BeadID != "ws-rev" || !strings.Contains(h.invs[0].Prompt, "beefcafe") {
		t.Fatalf("review must fork the agent anchored to the bead, carrying the pinned SHA, got %v", h.invs)
	}
	if len(r.events) != 2 || r.events[0] != "spawn" || r.events[1] != "close" {
		t.Errorf("review must drive the spawn-before-close transition, got %v", r.events)
	}
	if pin, ok := git.ParseCommit(r.spawned[0].Description); !ok || pin != "beefcafe" {
		t.Errorf("the Approval must be pinned at the HEAD captured at review start, got %q", r.spawned[0].Description)
	}
}

// A fork failure propagates so the Review stays open (not closed) — the loop
// re-dispatches next tick. No spawn, no close.
func TestRunForkFailureLeavesReviewOpen(t *testing.T) {
	r := &usoppSeamRec{openCritiques: 0, head: "beefcafe"}
	stubUsoppSeams(t, r)
	showBead = func(string) (bd.Bead, error) { return openReview(), nil }
	h := &recordingHarness{err: errors.New("review session crashed")}

	if err := (Usopp{}).Run(stubCtx{h}, contract.Bead{ID: "ws-rev", Kind: contract.KindReview}); err == nil {
		t.Fatal("a fork failure must propagate")
	}
	if len(r.closed) != 0 || len(r.spawned) != 0 {
		t.Errorf("a fork failure must not touch the graph, got closed=%v spawned=%v", r.closed, r.spawned)
	}
}

// A re-dispatch after the Review already closed is a no-op — no re-fork, no double
// approve, no double close.
func TestRunClosedReviewIsNoOp(t *testing.T) {
	r := &usoppSeamRec{head: "beefcafe"}
	stubUsoppSeams(t, r)
	closed := openReview()
	closed.Status = "closed"
	showBead = func(string) (bd.Bead, error) { return closed, nil }
	h := &recordingHarness{}

	if err := (Usopp{}).Run(stubCtx{h}, contract.Bead{ID: "ws-rev", Kind: contract.KindReview}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.invs) != 0 || len(r.spawned) != 0 || len(r.closed) != 0 {
		t.Errorf("a closed Review must be a no-op, got invs=%v spawned=%v closed=%v", h.invs, r.spawned, r.closed)
	}
}

// An unowned kind is a loud error, mirroring Franky.Run.
func TestRunUnknownKindErrors(t *testing.T) {
	stubUsoppSeams(t, &usoppSeamRec{})
	showBead = func(id string) (bd.Bead, error) {
		return bd.Bead{ID: id, Kind: contract.KindBuild, Status: "open"}, nil
	}
	if err := (Usopp{}).Run(stubCtx{&recordingHarness{}}, contract.Bead{ID: "x", Kind: contract.KindBuild}); err == nil {
		t.Error("a kind Usopp doesn't own must error")
	}
}

// FR49: a confirm sweep closes the Review mid-session. Fork ok or failed, the
// re-read sees it closed: no error, no Approval, no edge, no close.
func TestRunOverSweepClosedReviewIsNoOp(t *testing.T) {
	for name, forkErr := range map[string]error{"fork ok": nil, "fork failed": errors.New("session killed")} {
		r := &usoppSeamRec{head: "beefcafe", raised: []bd.Bead{{ID: "ws-piv"}}}
		stubUsoppSeams(t, r)
		reads := 0
		showBead = func(string) (bd.Bead, error) {
			b := openReview()
			if reads++; reads > 1 { // closed by the sweep after the dispatch read
				b.Status = "closed"
			}
			return b, nil
		}
		if err := (Usopp{}).Run(stubCtx{&recordingHarness{err: forkErr}}, contract.Bead{ID: "ws-rev", Kind: contract.KindReview}); err != nil {
			t.Errorf("%s: Run = %v, want nil (swept is done)", name, err)
		}
		if len(r.closed)+len(r.spawned)+len(r.raises) != 0 {
			t.Errorf("%s: swept Review touched the graph: closed=%v spawned=%v raises=%v", name, r.closed, r.spawned, r.raises)
		}
	}
}

// publishingCtx is a dispatched effort's Ctx: it carries the contract.Publisher.
type publishingCtx struct {
	stubCtx
	closes []string
	edges  []string
}

func (c *publishingCtx) CloseOnPublish(reason string) { c.closes = append(c.closes, reason) }
func (c *publishingCtx) EdgeOnPublish(e bd.Edge) {
	c.edges = append(c.edges, e.Type+" "+e.From+" "+e.To)
}

// Spec D6 / FR 23: in a dispatched effort approveIfClean hands the Review's close
// to the publish, so the drafted critiques and the close become visible together;
// the Approval stays a one-call create.
func TestApproveIfCleanHandsCloseToPublish(t *testing.T) {
	r := &usoppSeamRec{}
	stubUsoppSeams(t, r)
	ctx := &publishingCtx{}
	if err := approveIfClean(ctx, openReview(), "cafef00d"); err != nil {
		t.Fatalf("approveIfClean: %v", err)
	}
	if len(r.closed) != 0 || len(ctx.closes) != 1 || ctx.closes[0] != "" {
		t.Errorf("the Review's close must be handed to the publish once, closed=%v handed=%v", r.closed, ctx.closes)
	}
	if len(r.spawned) != 1 {
		t.Errorf("a clean pass still spawns the Approval directly, got %v", r.spawned)
	}
}

// An in-process effort killed under abort (ADR 0083, FR45) surfaces as a failure:
// a harness Run error makes Usopp's Run return non-nil, never a success.
func TestRunPropagatesHarnessError(t *testing.T) {
	stubUsoppSeams(t, &usoppSeamRec{head: "beefcafe"})
	showBead = func(string) (bd.Bead, error) { return openReview(), nil }
	h := &recordingHarness{err: errors.New("claude -p: signal: terminated")}
	if err := (Usopp{}).Run(stubCtx{h}, contract.Bead{ID: "ws-rev", Kind: contract.KindReview}); err == nil {
		t.Fatal("a harness Run error must make Usopp's Run fail")
	}
}

// Spec D1/D6 over the review prompt: the Spec and the Surgeries render
// independently; the pivot section keys on the Spec alone; a Spec-less Checkpoint
// runs one Surgery check per Surgery, a Spec'd one carves Surgery commits out of
// scope-creep. No render carries an empty `bd show `.
func TestReviewPromptIntentSource(t *testing.T) {
	sg := func(id, title string) bd.Bead { return bd.Bead{ID: id, Title: title} }
	one := []bd.Bead{sg("ws-s1", "tweak colour")}
	many := []bd.Bead{sg("ws-s1", "tweak colour"), sg("ws-s2", "abandoned idea")}
	a := reviewArgs{ReviewID: "ws-rev", CheckpointID: "ws-cp", Branch: "checkpoint/ws-cp", Pin: "deadbeef"}
	with := func(spec string, sgs []bd.Bead) reviewArgs { b := a; b.SpecID, b.Surgeries = spec, sgs; return b }
	cases := map[string]struct {
		p         string
		want, bad []string
	}{
		"spec only":      {reviewPrompt(with("ws-spec", nil)), []string{"bd show ws-spec", "kind:pivot,drafted-by:ws-rev", "Raise a pivot"}, []string{"Surgeries", "no spec available"}},
		"one surgery":    {reviewPrompt(with("", one)), []string{"- ws-s1: tweak colour", "once per Surgery", "missing", "implemented wrong", "no scope-creep check", "Approval"}, []string{"pivot", "no spec available"}},
		"spec+surgery":   {reviewPrompt(with("ws-spec", one)), []string{"bd show ws-spec", "- ws-s1: tweak colour", "Surgery: <id>", "scope-creep (b)", "Raise a pivot"}, []string{"no spec available"}},
		"many surgeries": {reviewPrompt(with("", many)), []string{"- ws-s1: tweak colour", "- ws-s2: abandoned idea", "gets no check"}, []string{"pivot", "no spec available"}},
		"headless":       {reviewPrompt(with("", nil)), []string{`"no spec available"`}, []string{"pivot", "Surgeries"}},
	}
	for name, c := range cases {
		for _, w := range c.want {
			if !strings.Contains(c.p, w) {
				t.Errorf("%s review prompt missing %q", name, w)
			}
		}
		for _, b := range append(c.bad, "%!", "<no value>") {
			if strings.Contains(c.p, b) {
				t.Errorf("%s review prompt must not carry %q", name, b)
			}
		}
		if emptyShow.MatchString(c.p) {
			t.Errorf("%s review prompt renders an empty bd show", name)
		}
		promptguard.Check(t, "review/"+name, c.p)
	}
	doc := promptguard.Docs(t)["code-review.md"]
	for _, w := range []string{"Surgery: <id>", "no scope-creep check", "per Surgery", `"no spec available"`} {
		if !strings.Contains(doc, w) {
			t.Errorf("code-review.md Spec axis missing %q", w)
		}
	}
}

var emptyShow = regexp.MustCompile("bd show [^a-z<]")

// runReview on a Spec-less Checkpoint with a Surgery member hands the review the
// Surgery as its intent source (Spec D6), re-reviews included.
func TestReviewResolvesSurgeryWhenNoSpec(t *testing.T) {
	r := &usoppSeamRec{head: "beefcafe"}
	stubUsoppSeams(t, r)
	specOf = func(string) (string, bool, error) { return "", false, nil }
	surgeriesOf = func(id string) ([]bd.Bead, error) { return []bd.Bead{{ID: "ws-surg", Title: "t"}}, nil }
	showBead = func(string) (bd.Bead, error) { return openReview(), nil }
	h := &recordingHarness{}
	if err := (Usopp{}).Run(stubCtx{h}, contract.Bead{ID: "ws-rev", Kind: contract.KindReview}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.invs) != 1 || !strings.Contains(h.invs[0].Prompt, "- ws-surg: t") || strings.Contains(h.invs[0].Prompt, "kind:pivot") {
		t.Errorf("the review prompt must read the Surgery and carry no pivot, got %v", h.invs)
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

// ADR 0157: the review prompt is the old render, plus exactly the focus line under a
// labelled Checkpoint; no table or an unlabelled one changes nothing.
func TestReviewFocusLine(t *testing.T) {
	run := func(label string) string {
		stubUsoppSeams(t, &usoppSeamRec{head: "beefcafe"})
		showBead = func(id string) (bd.Bead, error) {
			if id == "ws-cp" {
				return bd.Bead{ID: id, Kind: contract.KindCheckpoint, Status: "open", Labels: []string{label}}, nil
			}
			return openReview(), nil
		}
		h := &recordingHarness{}
		if err := (Usopp{}).Run(stubCtx{h}, contract.Bead{ID: "ws-rev", Kind: contract.KindReview}); err != nil {
			t.Fatal(err)
		}
		return h.invs[0].Prompt
	}
	ctxRepo(t, "")
	base := run("")
	ctxRepo(t, ctxCfg)
	if got := run("kind:checkpoint"); got != base {
		t.Error("unlabelled container changed the prompt")
	}
	if got := run("context:mugicrew"); got != base+ctxFocus {
		t.Error("labelled prompt is not old + focus line")
	}
}

// FR 23 / D5: a no-context Workspace never reads the Checkpoint.
func TestReviewNoContextsNeverShowsCheckpoint(t *testing.T) {
	stubUsoppSeams(t, &usoppSeamRec{head: "beefcafe"})
	ctxRepo(t, "")
	h := &recordingHarness{}
	showBead = func(id string) (bd.Bead, error) {
		// The Approval spawn after the run may title itself from the Checkpoint; the launch may not.
		if id == "ws-cp" && len(h.invs) == 0 {
			t.Fatal("bd show on the Checkpoint before launch with no contexts declared")
		}
		return openReview(), nil
	}
	if err := (Usopp{}).Run(stubCtx{h}, contract.Bead{ID: "ws-rev", Kind: contract.KindReview}); err != nil {
		t.Fatal(err)
	}
}

// runFocused drives one review of a Checkpoint with the given labels, in a repo whose
// shared config declares the context a → apps/a, and returns the rendered prompt and
// how many times the changed-paths seam ran.
func runFocused(t *testing.T, labels []string, paths []string) (string, int) {
	t.Helper()
	r := &usoppSeamRec{head: "beefcafe"}
	stubUsoppSeams(t, r)
	root := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if err := os.MkdirAll(filepath.Join(root, ".mugicrew"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".mugicrew", "config.toml"), []byte("[context.a]\npath = \"apps/a\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	show := showBead
	showBead = func(id string) (bd.Bead, error) {
		if id == "ws-cp" {
			return bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open", Labels: labels}, nil
		}
		return show(id)
	}
	calls := 0
	old := changedPaths
	t.Cleanup(func() { changedPaths = old })
	changedPaths = func(base, rev string) ([]string, error) {
		calls++
		if rev != "beefcafe" {
			t.Errorf("changedPaths rev = %q, want the pin", rev)
		}
		return paths, nil
	}
	h := &recordingHarness{}
	if err := (Usopp{}).Run(stubCtx{h}, contract.Bead{ID: "ws-rev", Kind: contract.KindReview}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return h.invs[0].Prompt, calls
}

func TestReviewListsOutOfScopePathsOnlyForALabelledCheckpoint(t *testing.T) {
	paths := []string{"apps/a/x.go", "libs/l/z.go", "GLOSSARY.md"}
	p, calls := runFocused(t, []string{"context:a"}, paths)
	if calls != 1 || !strings.Contains(p, "- libs/l/z.go") || strings.Contains(p, "apps/a/x.go") || strings.Contains(p, "- GLOSSARY.md") {
		t.Errorf("calls=%d, prompt out-of-scope section wrong:\n%s", calls, p)
	}
	for _, labels := range [][]string{nil, {"context:undeclared"}} {
		q, calls := runFocused(t, labels, paths)
		if calls != 0 || strings.Contains(q, "libs/l/z.go") {
			t.Errorf("labels %v: calls=%d, want no git work and no list", labels, calls)
		}
	}
	// Labelled but everything inside the subtree: byte-identical to unlabelled.
	inside, _ := runFocused(t, []string{"context:a"}, []string{"apps/a/x.go"})
	plain, _ := runFocused(t, nil, nil)
	if !strings.HasPrefix(inside, plain) || strings.Contains(inside, "Changes outside") {
		t.Errorf("an empty out-of-scope list must render the unlabelled prompt byte for byte")
	}
}

// A repo = context's Checkpoint is reviewed in the child: its branch head is the pin,
// no out-of-scope paths are computed, and the prompt carries the repo addendum.
func TestReviewOfANestedCheckpointPinsAndScopesInTheChild(t *testing.T) {
	stubUsoppSeams(t, &usoppSeamRec{head: "workspacepin"})
	ctxRepo(t, "[context.c]\nrepo = \"vendor/c\"\n")
	child, _ := os.Getwd()
	child += "/vendor/c"
	os.MkdirAll(child, 0o755)
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", child, "-c", "user.email=a@b", "-c", "user.name=t"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	git("commit", "-q", "--allow-empty", "-m", "r")
	git("branch", "checkpoint/ws-cp")
	childHead := git("rev-parse", "checkpoint/ws-cp")
	showBead = func(id string) (bd.Bead, error) {
		if id == "ws-cp" {
			return bd.Bead{ID: id, Kind: contract.KindCheckpoint, Status: "open", Labels: []string{"context:c"}}, nil
		}
		return openReview(), nil
	}
	changedPaths = func(string, string) ([]string, error) { t.Fatal("changedPaths for a repo context"); return nil, nil }
	h := &recordingHarness{}
	if err := (Usopp{}).Run(stubCtx{h}, contract.Bead{ID: "ws-rev", Kind: contract.KindReview}); err != nil {
		t.Fatal(err)
	}
	p := h.invs[0].Prompt
	for _, w := range []string{childHead, "git -C vendor/c", "own git repo"} {
		if !strings.Contains(p, w) {
			t.Errorf("prompt lacks %q", w)
		}
	}
	if strings.Contains(p, "workspacepin") {
		t.Error("pinned the Workspace's head")
	}
}
