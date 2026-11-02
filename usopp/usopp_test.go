package usopp

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/bd/bdtest"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/promptguard"
)

// recordingHarness is the fake contract.Harness the handler tests dispatch
// through: it records every AFK invocation, can run a hook standing in for the
// agent's writes, and can be made to fail. No live Claude Code.
type recordingHarness struct {
	invs  []contract.Invocation
	err   error
	onRun func(contract.Invocation) // the agent's graph writes
}

func (h *recordingHarness) Run(_ contract.Ctx, inv contract.Invocation) (contract.Result, error) {
	h.invs = append(h.invs, inv)
	if h.onRun != nil {
		h.onRun(inv)
	}
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

// recT is a testing.TB that records Errorf instead of failing the test, so a test can
// provoke the fake's ADR 0070 refusal and assert on it.
type recT struct {
	testing.TB
	errs []string
}

func (r *recT) Errorf(format string, args ...any) { r.errs = append(r.errs, format) }

// stubGit points the git seams at pin and no changed paths.
func stubGit(t *testing.T, pin string) {
	t.Helper()
	oHead, oChanged := head, changedPaths
	t.Cleanup(func() { head, changedPaths = oHead, oChanged })
	head = func(string) (string, error) { return pin, nil }
	changedPaths = func(string, string) ([]string, error) { return nil, nil }
}

func openReview() bd.Bead {
	return bd.Bead{ID: "ws-rev", Kind: contract.KindReview, Status: "in_progress", Parent: "ws-cp"}
}

func checkpoint(extra ...func(*bd.Bead)) bd.Bead {
	b := bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open", Title: "the checkpoint title", CausedBy: "ws-spec"}
	for _, e := range extra {
		e(&b)
	}
	return b
}

// reviewGraph seeds the Checkpoint, its Spec and the dispatched Review, which this
// process holds (bdtest.Claimed), so a write under its claim passes the fake.
func reviewGraph(t *testing.T, extra ...bd.Bead) *bdtest.Fake {
	t.Helper()
	return bdtest.New(t, append([]bd.Bead{
		checkpoint(),
		{ID: "ws-spec", Kind: contract.KindSpec, Status: "closed"},
		bdtest.Claimed(openReview()),
	}, extra...)...)
}

func approvals(t *testing.T) []bd.Bead {
	t.Helper()
	as, err := bd.ChildrenOfKind("ws-cp", contract.KindApproval)
	if err != nil {
		t.Fatal(err)
	}
	return as
}

func runReviewBead(t *testing.T, h *recordingHarness) error {
	t.Helper()
	return Usopp{}.Run(stubCtx{h}, contract.Bead{ID: "ws-rev", Kind: contract.KindReview})
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
// Review, never an Errand.
func TestReviewPromptFilesExternalWorkAsSignal(t *testing.T) {
	p := reviewPrompt(reviewArgs{ReviewID: "ws-rev", CheckpointID: "ws-cp", SpecID: "ws-spec"})
	for _, want := range []string{"offshoot Signal, never an Errand", "signal-filing.md", "discovered-from ws-rev"} {
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

// Clean pass: zero open critiques ⇒ the pinned Approval is spawned under the Review's
// claim and the Review closes after it.
func TestApproveIfCleanSpawnsPinnedApprovalBeforeClose(t *testing.T) {
	f := reviewGraph(t)
	if err := approveIfClean(stubCtx{}, f.Bead("ws-rev"), "cafef00d"); err != nil {
		t.Fatalf("approveIfClean: %v", err)
	}
	as := approvals(t)
	if len(as) != 1 {
		t.Fatalf("a clean pass must spawn exactly one Approval, got %v", as)
	}
	appr := as[0]
	if appr.Kind != contract.KindApproval || appr.Container || appr.Parent != "ws-cp" {
		t.Errorf("Approval must be a kind:approval leaf under the Checkpoint, got %+v", appr)
	}
	if appr.Title != "the checkpoint title" {
		t.Errorf("Approval title = %q, want the Checkpoint's title verbatim", appr.Title)
	}
	if pin, ok := git.ParseCommit(appr.Description); !ok || pin != "cafef00d" {
		t.Errorf("Approval must carry the Commit: pin captured at review start, got %q", appr.Description)
	}
	if appr.CausedBy != "ws-rev" {
		t.Errorf("Approval must be caused-by the clean Review, got %q", appr.CausedBy)
	}
	if got := f.Bead("ws-rev"); got.Open() {
		t.Errorf("the Review must close after spawning, got %+v", got)
	}
}

// The spawn lands before the close: a failed close leaves the Approval and the open
// Review, so "predecessor closed, successor missing" is unreachable.
func TestApproveIfCleanSpawnPrecedesClose(t *testing.T) {
	f := reviewGraph(t)
	f.FailOn("Batch", "ws-rev", errors.New("batch down"))
	if err := approveIfClean(stubCtx{}, f.Bead("ws-rev"), "cafef00d"); err == nil {
		t.Fatal("a failed close must surface")
	}
	if len(approvals(t)) != 1 || !f.Bead("ws-rev").Open() {
		t.Errorf("want the Approval spawned and the Review still open, got approvals=%v review=%+v", approvals(t), f.Bead("ws-rev"))
	}
}

// The Approval spawn runs under the dispatched Review's claim: with it unheld the fake
// refuses the spawn, and nothing is created or closed.
func TestApproveIfCleanRefusesTheSpawnWithTheClaimUnheld(t *testing.T) {
	rec := &recT{TB: t}
	f := bdtest.New(rec, checkpoint(), bd.Bead{ID: "ws-spec", Kind: contract.KindSpec, Status: "closed"}, openReview())
	review := openReview()
	review.Status = "open" // not claimed
	f.Seed(review)
	if err := approveIfClean(stubCtx{}, review, "cafef00d"); err == nil {
		t.Fatal("an unclaimed Review must not spawn the Approval")
	}
	if len(rec.errs) != 1 {
		t.Errorf("want one ADR 0070 violation, got %v", rec.errs)
	}
	if as, _ := bd.ChildrenOfKind("ws-cp", contract.KindApproval); len(as) != 0 || !f.Bead("ws-rev").Open() {
		t.Errorf("a refused spawn must write nothing, got approvals=%v", as)
	}
}

// Dirty pass: ≥1 open critique ⇒ Review still closed, but NO Approval spawned.
func TestApproveIfCleanClosesWithoutApprovalWhenCritiquesOpen(t *testing.T) {
	f := reviewGraph(t,
		bd.Bead{ID: "ws-c1", Kind: contract.KindCritique, Status: "open", Parent: "ws-cp"},
		bd.Bead{ID: "ws-c2", Kind: contract.KindCritique, Status: "open", Parent: "ws-cp"})
	if err := approveIfClean(stubCtx{}, f.Bead("ws-rev"), "cafef00d"); err != nil {
		t.Fatalf("approveIfClean: %v", err)
	}
	if as := approvals(t); len(as) != 0 {
		t.Errorf("open critiques must NOT spawn an Approval, got %v", as)
	}
	if f.Bead("ws-rev").Open() {
		t.Error("the Review must still close on a dirty pass")
	}
}

// A frozen Checkpoint (open pivot) gets NO Approval even on a clean pass, so the
// HITL Approval never reaches the Inbox under a pivot (FR14) — the Review still closes.
func TestApproveIfCleanOpenPivotHoldsApproval(t *testing.T) {
	f := reviewGraph(t, bd.Bead{ID: "ws-piv", Kind: contract.KindPivot, Status: "open"})
	if err := bd.Blocks("ws-cp", "ws-piv"); err != nil {
		t.Fatal(err)
	}
	if err := approveIfClean(stubCtx{}, f.Bead("ws-rev"), "cafef00d"); err != nil {
		t.Fatalf("approveIfClean: %v", err)
	}
	if as := approvals(t); len(as) != 0 {
		t.Errorf("an open pivot must hold the Approval back, got %v", as)
	}
	if f.Bead("ws-rev").Open() {
		t.Error("the Review must still close under a pivot")
	}
}

// A raise: the review agent created a pivot discovered-from the Review. Usopp hands
// the Review, its Spec and the pivot to the raise (the gate edges, the 'pivoted:'
// close) and spawns NO Approval.
func TestRunPivotRaiseHandsOffWithoutApproval(t *testing.T) {
	stubGit(t, "beefcafe")
	f := reviewGraph(t)
	h := &recordingHarness{onRun: func(contract.Invocation) {
		f.Seed(bd.Bead{ID: "ws-piv", Kind: contract.KindPivot, Status: "open", DiscoveredFrom: []string{"ws-rev"}})
	}}
	if err := runReviewBead(t, h); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if as := approvals(t); len(as) != 0 {
		t.Errorf("a pivoted review must spawn no Approval, got %v", as)
	}
	rev := f.Bead("ws-rev")
	if rev.Open() || rev.CloseReason != "pivoted: ws-piv" {
		t.Errorf("the Review must close pivoted, got %+v", rev)
	}
	if got := f.Bead("ws-cp").BlockedBy; !slices.Equal(got, []string{"ws-piv"}) {
		t.Errorf("the Checkpoint must be gated by the pivot, got %v", got)
	}
	if got := f.Bead("ws-piv").Related; !slices.Equal(got, []string{"ws-spec"}) {
		t.Errorf("the pivot must relate to the resolved Spec, got %v", got)
	}
}

// The Review prompt's Spec id and the pivot's relation come from bd.SpecOf, so a
// successor Checkpoint a Rollback caused (ADR 0048 §2) is reviewed against the
// original Spec, not "".
func TestReviewResolvesSpecThroughTheRollback(t *testing.T) {
	stubGit(t, "beefcafe")
	cp := checkpoint(func(b *bd.Bead) { b.CausedBy = "ws-rb" })
	bdtest.New(t, cp,
		bd.Bead{ID: "ws-rb", Kind: contract.KindRollback, Status: "closed", Related: []string{"ws-old-cp"}},
		bd.Bead{ID: "ws-old-cp", Kind: contract.KindCheckpoint, Status: "closed", CausedBy: "ws-orig-spec"},
		bd.Bead{ID: "ws-orig-spec", Kind: contract.KindSpec, Status: "closed"},
		bdtest.Claimed(openReview()))
	h := &recordingHarness{}
	if err := runReviewBead(t, h); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.invs) != 1 || !strings.Contains(h.invs[0].Prompt, "ws-orig-spec") {
		t.Errorf("the review prompt must name the resolved Spec, got %v", h.invs)
	}
}

// A re-dispatch finding the Approval already open at the pin reuses it: no second
// Approval, and the Review closes.
func TestApproveIfCleanIdempotentUnderRedispatch(t *testing.T) {
	f := reviewGraph(t)
	for range 2 {
		if err := approveIfClean(stubCtx{}, f.Bead("ws-rev"), "cafef00d"); err != nil {
			t.Fatalf("approveIfClean: %v", err)
		}
		f.Mutate("ws-rev", func(b *bd.Bead) { *b = bdtest.Claimed(openReview()) }) // the next dispatch
	}
	if as := approvals(t); len(as) != 1 {
		t.Errorf("re-dispatch must converge on one Approval, got %v", as)
	}
}

// Stale open Approval (pin != reviewed HEAD): a clean re-review OBSOLETES it before
// spawning, so SpawnChildIfAbsent can't reuse an Approval pinned to a superseded
// commit — the FR31 self-heal. The fresh Approval is pinned at the new HEAD.
func TestApproveIfCleanObsoletesStaleApproval(t *testing.T) {
	f := reviewGraph(t, bd.Bead{ID: "ws-appr-old", Kind: contract.KindApproval, Status: "open", Parent: "ws-cp", Description: git.CommitPin("oldsha")})
	if err := approveIfClean(stubCtx{}, f.Bead("ws-rev"), "newsha"); err != nil {
		t.Fatalf("approveIfClean: %v", err)
	}
	if old := f.Bead("ws-appr-old"); old.Open() {
		t.Errorf("a stale-pinned open Approval must be obsoleted, got %+v", old)
	}
	if got := f.Bead("ws-appr-old").CloseReason; got != bd.ApprovalObsoletedReason {
		t.Errorf("obsoleted Approval reason = %q, want %q", got, bd.ApprovalObsoletedReason)
	}
	open, err := bd.OpenChildrenOfKindBeads("ws-cp", contract.KindApproval)
	if err != nil || len(open) != 1 {
		t.Fatalf("a fresh Approval must be spawned, got %v, %v", open, err)
	}
	if pin, ok := git.ParseCommit(open[0].Description); !ok || pin != "newsha" {
		t.Errorf("the respawned Approval must pin the new HEAD, got %q", open[0].Description)
	}
	if f.Bead("ws-rev").Open() {
		t.Error("the Review must still close")
	}
}

// An open Approval ALREADY at HEAD is NOT obsoleted — SpawnChildIfAbsent reuses it,
// so a plain re-dispatch (HEAD unchanged) neither churns nor double-approves.
func TestApproveIfCleanKeepsApprovalAlreadyAtHead(t *testing.T) {
	f := reviewGraph(t, bd.Bead{ID: "ws-appr", Kind: contract.KindApproval, Status: "open", Parent: "ws-cp", Description: git.CommitPin("samehead")})
	if err := approveIfClean(stubCtx{}, f.Bead("ws-rev"), "samehead"); err != nil {
		t.Fatalf("approveIfClean: %v", err)
	}
	if !f.Bead("ws-appr").Open() {
		t.Error("an Approval already at HEAD must NOT be obsoleted")
	}
	if as := approvals(t); len(as) != 1 {
		t.Errorf("only the existing Approval should remain, got %v", as)
	}
	if f.Bead("ws-rev").Open() {
		t.Error("the Review should close")
	}
}

// Run(review) pins the branch HEAD, forks the read-only review agent anchored to
// the bead, then drives the clean→Approval transition through to spawn+close.
func TestRunReviewPinsForksThenApproves(t *testing.T) {
	stubGit(t, "beefcafe")
	f := reviewGraph(t)
	h := &recordingHarness{}
	if err := runReviewBead(t, h); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.invs) != 1 || h.invs[0].BeadID != "ws-rev" || !strings.Contains(h.invs[0].Prompt, "beefcafe") {
		t.Fatalf("review must fork the agent anchored to the bead, carrying the pinned SHA, got %v", h.invs)
	}
	as := approvals(t)
	if len(as) != 1 || f.Bead("ws-rev").Open() {
		t.Fatalf("review must spawn the Approval and close, got approvals=%v", as)
	}
	if pin, ok := git.ParseCommit(as[0].Description); !ok || pin != "beefcafe" {
		t.Errorf("the Approval must be pinned at the HEAD captured at review start, got %q", as[0].Description)
	}
}

// A fork failure propagates so the Review stays open (not closed) — the loop
// re-dispatches next tick. No spawn, no close.
func TestRunForkFailureLeavesReviewOpen(t *testing.T) {
	stubGit(t, "beefcafe")
	f := reviewGraph(t)
	h := &recordingHarness{err: errors.New("review session crashed")}
	if err := runReviewBead(t, h); err == nil {
		t.Fatal("a fork failure must propagate")
	}
	if len(approvals(t)) != 0 || !f.Bead("ws-rev").Open() {
		t.Error("a fork failure must not touch the graph")
	}
}

// A re-dispatch after the Review already closed is a no-op — no re-fork, no double
// approve, no double close.
func TestRunClosedReviewIsNoOp(t *testing.T) {
	stubGit(t, "beefcafe")
	closed := openReview()
	closed.Status = "closed"
	reviewGraph(t, closed)
	h := &recordingHarness{}
	if err := runReviewBead(t, h); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.invs) != 0 || len(approvals(t)) != 0 {
		t.Errorf("a closed Review must be a no-op, got invs=%v approvals=%v", h.invs, approvals(t))
	}
}

// An unowned kind is a loud error, mirroring Franky.Run.
func TestRunUnknownKindErrors(t *testing.T) {
	bdtest.New(t, bd.Bead{ID: "x", Kind: contract.KindBuild, Status: "open"})
	if err := (Usopp{}).Run(stubCtx{&recordingHarness{}}, contract.Bead{ID: "x", Kind: contract.KindBuild}); err == nil {
		t.Error("a kind Usopp doesn't own must error")
	}
}

// FR49 wiring: a confirm sweep closes the Review mid-session and the fork fails; the
// runner reports it done: no error, no Approval, no edge.
func TestRunOverSweepClosedReviewIsNoOp(t *testing.T) {
	stubGit(t, "beefcafe")
	f := reviewGraph(t, bd.Bead{ID: "ws-piv", Kind: contract.KindPivot, Status: "open", DiscoveredFrom: []string{"ws-rev"}})
	h := &recordingHarness{err: errors.New("session killed"), onRun: func(contract.Invocation) {
		f.Mutate("ws-rev", func(b *bd.Bead) { b.Status = "closed"; b.CloseReason = "swept" })
	}}
	if err := runReviewBead(t, h); err != nil {
		t.Errorf("Run = %v, want nil (swept is done)", err)
	}
	if len(approvals(t)) != 0 || f.Bead("ws-rev").CloseReason != "swept" || len(f.Bead("ws-cp").BlockedBy) != 0 {
		t.Error("swept Review touched the graph")
	}
}

// Spec D6 / FR 23: in a dispatched effort approveIfClean hands the Review's close
// to the publish, so the drafted critiques and the close become visible together;
// the Approval stays a one-call create.
func TestApproveIfCleanHandsCloseToPublish(t *testing.T) {
	f := reviewGraph(t)
	ctx := &publishingCtx{}
	if err := approveIfClean(ctx, f.Bead("ws-rev"), "cafef00d"); err != nil {
		t.Fatalf("approveIfClean: %v", err)
	}
	as := approvals(t)
	if len(as) != 1 {
		t.Fatal("a clean pass still spawns the Approval directly")
	}
	if !f.Bead("ws-rev").Open() || len(ctx.closes) != 1 || ctx.closes[0] != "clean: "+as[0].ID {
		t.Errorf("the Review's close must be handed to the publish once, as clean: <approval id> (want %q), handed=%v", "clean: "+as[0].ID, ctx.closes)
	}
}

func critiques(n int) []bd.Bead {
	var out []bd.Bead
	for i := 1; i <= n; i++ {
		out = append(out, bd.Bead{ID: "ws-crit" + strconv.Itoa(i), Kind: contract.KindCritique, Status: "open", Parent: "ws-cp"})
	}
	return out
}

func TestReviewCloseReason(t *testing.T) {
	c, p := critiques(2), []bd.Bead{{ID: "ws-p1"}, {ID: "ws-p2"}}
	for name, tc := range map[string]struct {
		critiques, pivots []bd.Bead
		approval, want    string
	}{
		"clean":              {nil, nil, "ws-appr", "clean: ws-appr"},
		"one critique":       {c[:1], nil, "", "critiqued: ws-crit1"},
		"several critiques":  {c, nil, "", "critiqued: ws-crit1, ws-crit2"},
		"pivots only":        {nil, p, "", "held: pivot ws-p1, ws-p2"},
		"critiques + pivots": {c[:1], p, "", "critiqued: ws-crit1"},
	} {
		if got := reviewCloseReason(tc.critiques, tc.pivots, tc.approval); got != tc.want {
			t.Errorf("%s: reason = %q, want %q", name, got, tc.want)
		}
	}
}

// Every outcome closes with its named reason.
func TestApproveIfCleanClosesWithNamedReason(t *testing.T) {
	pivot := bd.Bead{ID: "ws-piv", Kind: contract.KindPivot, Status: "open"}
	for name, tc := range map[string]struct {
		extra  []bd.Bead
		pivot  bool
		reused bool
		want   string // "" = clean: <the Approval's id>
	}{
		"clean":            {},
		"reused approval":  {extra: []bd.Bead{{ID: "ws-appr", Kind: contract.KindApproval, Status: "open", Parent: "ws-cp", Description: git.CommitPin("cafef00d")}}, reused: true},
		"critiques":        {extra: critiques(2), want: "critiqued: ws-crit1, ws-crit2"},
		"pivots":           {extra: []bd.Bead{pivot}, pivot: true, want: "held: pivot ws-piv"},
		"critiques+pivots": {extra: append(critiques(1), pivot), pivot: true, want: "critiqued: ws-crit1"},
	} {
		t.Run(name, func(t *testing.T) {
			f := reviewGraph(t, tc.extra...)
			if tc.pivot {
				if err := bd.Blocks("ws-cp", "ws-piv"); err != nil {
					t.Fatal(err)
				}
			}
			if err := approveIfClean(stubCtx{}, f.Bead("ws-rev"), "cafef00d"); err != nil {
				t.Fatalf("approveIfClean: %v", err)
			}
			want := tc.want
			if want == "" {
				as := approvals(t)
				if len(as) != 1 {
					t.Fatalf("a clean pass must leave one Approval, got %v", as)
				}
				if tc.reused && as[0].ID != "ws-appr" {
					t.Errorf("the Approval at head must be reused, got %v", as)
				}
				want = "clean: " + as[0].ID
			}
			if got := f.Bead("ws-rev").CloseReason; got != want {
				t.Errorf("reason = %q, want %q", got, want)
			}
		})
	}
}

// An in-process effort killed under abort (ADR 0083, FR45) surfaces as a failure:
// a harness Run error makes Usopp's Run return non-nil, never a success.
func TestRunPropagatesHarnessError(t *testing.T) {
	stubGit(t, "beefcafe")
	reviewGraph(t)
	h := &recordingHarness{err: errors.New("claude -p: signal: terminated")}
	if err := runReviewBead(t, h); err == nil {
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
	stubGit(t, "beefcafe")
	bdtest.New(t, checkpoint(func(b *bd.Bead) { b.CausedBy = "" }),
		bd.Bead{ID: "ws-surg", Title: "t", Kind: contract.KindSurgery, Status: "open", Parent: "ws-cp"},
		bdtest.Claimed(openReview()))
	h := &recordingHarness{}
	if err := runReviewBead(t, h); err != nil {
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
		stubGit(t, "beefcafe")
		bdtest.New(t, checkpoint(func(b *bd.Bead) { b.Labels = []string{label} }),
			bd.Bead{ID: "ws-spec", Kind: contract.KindSpec, Status: "closed"}, bdtest.Claimed(openReview()))
		h := &recordingHarness{}
		if err := runReviewBead(t, h); err != nil {
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

// FR 23 / D5: a no-context Workspace never reads the Checkpoint before the launch.
func TestReviewNoContextsNeverShowsCheckpoint(t *testing.T) {
	stubGit(t, "beefcafe")
	f := reviewGraph(t)
	ctxRepo(t, "")
	h := &recordingHarness{onRun: func(contract.Invocation) {
		if slices.Contains(f.Calls(), "Show ws-cp") {
			t.Error("bd show on the Checkpoint before launch with no contexts declared")
		}
	}}
	if err := runReviewBead(t, h); err != nil {
		t.Fatal(err)
	}
}

// runFocused drives one review of a Checkpoint with the given labels, in a repo whose
// shared config declares the context a → apps/a, and returns the rendered prompt and
// how many times the changed-paths seam ran.
func runFocused(t *testing.T, labels []string, paths []string) (string, int) {
	t.Helper()
	stubGit(t, "beefcafe")
	bdtest.New(t, checkpoint(func(b *bd.Bead) { b.Labels = labels }),
		bd.Bead{ID: "ws-spec", Kind: contract.KindSpec, Status: "closed"}, bdtest.Claimed(openReview()))
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
	calls := 0
	changedPaths = func(base, rev string) ([]string, error) {
		calls++
		if rev != "beefcafe" {
			t.Errorf("changedPaths rev = %q, want the pin", rev)
		}
		return paths, nil
	}
	h := &recordingHarness{}
	if err := runReviewBead(t, h); err != nil {
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
	stubGit(t, "workspacepin")
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
	bdtest.New(t, checkpoint(func(b *bd.Bead) { b.Labels = []string{"context:c"} }),
		bd.Bead{ID: "ws-spec", Kind: contract.KindSpec, Status: "closed"}, bdtest.Claimed(openReview()))
	changedPaths = func(string, string) ([]string, error) { t.Fatal("changedPaths for a repo context"); return nil, nil }
	h := &recordingHarness{}
	if err := runReviewBead(t, h); err != nil {
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

// The Critique backstop runs once, before any raise or close, on the clean,
// with-Critiques and pivoted paths alike: a Critique Draft the Review filed without
// --deps comes out discovered-from it, the edge written before the Review closes.
func TestTailTiesCritiquesBeforeRaiseOrClose(t *testing.T) {
	cases := map[string]struct {
		extra   []bd.Bead
		pivoted bool
	}{
		"clean":          {},
		"with-critiques": {extra: []bd.Bead{{ID: "ws-c1", Kind: contract.KindCritique, Status: "open", Parent: "ws-cp"}}},
		"pivoted":        {pivoted: true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			stubGit(t, "beefcafe")
			f := reviewGraph(t, c.extra...)
			h := &recordingHarness{onRun: func(contract.Invocation) {
				f.Seed(bdtest.Draft("ws-rev", bd.Bead{ID: "ws-crit", Kind: contract.KindCritique}))
				if c.pivoted {
					f.Seed(bd.Bead{ID: "ws-piv", Kind: contract.KindPivot, Status: "open", DiscoveredFrom: []string{"ws-rev"}})
				}
			}}
			if err := runReviewBead(t, h); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if crit := f.Bead("ws-crit"); !slices.Contains(crit.DiscoveredFrom, "ws-rev") {
				t.Errorf("the Critique must be discovered-from the Review, got %+v", crit)
			}
			if f.Bead("ws-rev").Open() {
				t.Error("the Review must close")
			}
			if !f.EdgeBeforeClose("ws-crit", "ws-rev", "ws-rev") {
				t.Errorf("tie must come before any raise or close, calls %v", f.Calls())
			}
		})
	}
}

// A Review session that closes its own bead still gets the backstop: dispatch
// publishes that bead's Drafts, so a kind:critique Draft filed without --deps
// must come out discovered-from the Review.
func TestTailTiesCritiquesWhenSessionClosesItsOwnBead(t *testing.T) {
	stubGit(t, "beefcafe")
	f := reviewGraph(t)
	h := &recordingHarness{onRun: func(contract.Invocation) {
		f.Seed(bdtest.Draft("ws-rev", bd.Bead{ID: "ws-crit", Kind: contract.KindCritique}))
		if err := f.Close([]string{"ws-rev"}, "done", false); err != nil {
			t.Fatal(err)
		}
	}}
	if err := runReviewBead(t, h); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if crit := f.Bead("ws-crit"); !slices.Contains(crit.DiscoveredFrom, "ws-rev") {
		t.Errorf("the Critique must be discovered-from the Review, got %+v", crit)
	}
}

// Both Critique creates (per-finding and aggregate) carry the Review's edge.
func TestReviewPromptCritiqueCreatesCarryReviewEdge(t *testing.T) {
	p := reviewPrompt(reviewArgs{ReviewID: "ws-rev", CheckpointID: "ws-cp", SpecID: "ws-spec", Branch: "checkpoint/ws-cp", Pin: "deadbeef"})
	i := strings.Index(p, "bd create --type task --labels kind:critique")
	if i < 0 {
		t.Fatal("review prompt lacks the per-finding Critique create")
	}
	n := strings.Index(p[i:], "--title")
	if n < 0 {
		t.Fatal("per-finding Critique create has no --title")
	}
	create := p[i : i+n]
	if !strings.Contains(create, "--deps discovered-from:ws-rev") {
		t.Errorf("per-finding create lacks the Review edge: %q", create)
	}
	j := strings.Index(p, "aggregate")
	if j < 0 {
		t.Fatal("review prompt lacks the aggregate Critique section")
	}
	m := strings.Index(p[j:], "severity gate")
	if m < 0 {
		t.Fatal("review prompt lacks the severity gate after the aggregate section")
	}
	agg := p[j : j+m]
	if !strings.Contains(agg, "--deps discovered-from:ws-rev") {
		t.Errorf("aggregate create lacks the Review edge: %q", agg)
	}
}

func reviewDescWrites(f *bdtest.Fake) int {
	n := 0
	for _, c := range f.Calls() {
		if c == "SetField ws-rev" {
			n++
		}
	}
	return n
}

func countCommitLines(body string) int {
	n := 0
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(l, "Commit:") {
			n++
		}
	}
	return n
}

// Review start writes one Commit: line onto the Review before the session forks, and
// the clean tail's Approval carries the same pin.
func TestRunReviewWritesThePinBeforeTheFork(t *testing.T) {
	stubGit(t, "beefcafe")
	f := reviewGraph(t)
	var atFork string
	h := &recordingHarness{onRun: func(contract.Invocation) { atFork = f.Bead("ws-rev").Description }}
	if err := runReviewBead(t, h); err != nil {
		t.Fatal(err)
	}
	if pin, ok := git.ParseCommit(atFork); !ok || pin != "beefcafe" || countCommitLines(atFork) != 1 {
		t.Errorf("at fork the Review body = %q, want exactly one Commit: beefcafe", atFork)
	}
	if got, _ := git.ParseCommit(approvals(t)[0].Description); got != "beefcafe" {
		t.Errorf("Approval pin = %q, want the Review's", got)
	}
}

// A re-dispatch replaces the line with the new head, writes nothing at the same head,
// and keeps every other line (Jinbe's not-carried note).
func TestRunReviewRedispatchRewritesThePin(t *testing.T) {
	note := "Renumber not carried: reason."
	f := reviewGraph(t)
	f.Mutate("ws-rev", func(b *bd.Bead) { b.Description = "Review.\n" + git.CommitPin("old") + "\n" + note })
	stubGit(t, "newhead")
	h := &recordingHarness{err: errors.New("crash")} // leaves the Review open
	if err := runReviewBead(t, h); err == nil {
		t.Fatal("session error must propagate")
	}
	want := "Review.\nCommit: newhead\n" + note
	if got := f.Bead("ws-rev").Description; got != want {
		t.Fatalf("after a session error the body = %q, want the pin written and note kept: %q", got, want)
	}
	before := reviewDescWrites(f)
	if err := runReviewBead(t, h); err == nil {
		t.Fatal("session error must propagate")
	}
	if reviewDescWrites(f) != before {
		t.Error("a re-dispatch at the same head must not write the description")
	}
}
