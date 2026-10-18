package nami

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contexts/contextstest"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/promptguard"
)

// stubHarness records the Invocation each path composes so we can assert the
// prompt without spawning claude: inv for LaunchCommand (HITL), runInv for Run (AFK).
type stubHarness struct {
	inv    contract.Invocation
	runInv contract.Invocation
	out    string // Result.Output the AFK Run path returns
	err    error  // non-nil → the AFK Run errors
}

func (h *stubHarness) Run(_ contract.Ctx, inv contract.Invocation) (contract.Result, error) {
	h.runInv = inv
	return contract.Result{Output: h.out}, h.err
}
func (h *stubHarness) LaunchCommand(inv contract.Invocation) (contract.LaunchCommand, error) {
	h.inv = inv
	return contract.AgentCommand{Argv: []string{"claude", inv.Prompt}}, nil
}

func (h *stubHarness) ResumeCommand(string, bool) (contract.AgentCommand, error) {
	return contract.AgentCommand{}, nil
}

type stubCtx struct{ h contract.Harness }

func (c stubCtx) Harness() contract.Harness { return c.h }

// ADR 0129: Signal triage is Luffy's, so Nami keeps no Signal arm — Launch and
// Reconcile on a Signal are loud errors.
func TestSignalIsNoLongerNamis(t *testing.T) {
	if _, err := (Nami{}).Launch(stubCtx{&stubHarness{}}, contract.Bead{ID: "ws-sig", Kind: contract.KindSignal}); err == nil {
		t.Error("Launch of a Signal must error")
	}
	if err := (Nami{}).Reconcile(nil, bd.Bead{ID: "ws-sig", Kind: contract.KindSignal, Status: "open"}); err == nil {
		t.Error("Reconcile of a Signal must error")
	}
}

// Grilling is agent-variety: the harness formats a session whose prompt names
// the bead and closes it once answered (spec §9.3).
func TestLaunchGrillingIsAgentSession(t *testing.T) {
	h := &stubHarness{}
	lc, err := Nami{}.Launch(stubCtx{h}, contract.Bead{ID: "ws-grl", Kind: contract.KindGrilling})
	if err != nil {
		t.Fatalf("Launch grilling: %v", err)
	}
	if _, ok := lc.(contract.AgentCommand); !ok {
		t.Fatalf("grilling Launch must be agent variety, got %T", lc)
	}
	if h.inv.BeadID != "ws-grl" || !strings.Contains(h.inv.Prompt, "bd close ws-grl") {
		t.Errorf("grilling prompt must anchor+resolve the bead, got %q / %q", h.inv.BeadID, h.inv.Prompt)
	}
}

// Run(research) takes the agentic Harness.Run path (not the Reconcile fallback),
// anchors the session to the bead (BeadID), and carries the landing + close-with-
// pointer contract in the prompt — the AFK/Run mirror of the HITL/Launch tests.
// The empty stub Result short-circuits the close, so this touches no bd (the bd
// close/pointer legs are exercised end-to-end, not here).
func TestRunResearchComposesAFKResearchInvocation(t *testing.T) {
	h := &stubHarness{}
	if err := (Nami{}).Run(stubCtx{h}, contract.Bead{ID: "mc-r", Kind: contract.KindResearch}); err != nil {
		t.Fatalf("Run research: %v", err)
	}
	if h.runInv.BeadID != "mc-r" {
		t.Errorf("research Invocation must anchor to the bead, got %q", h.runInv.BeadID)
	}
	p := h.runInv.Prompt
	for _, want := range []string{"mc-r", "docs/research/", "bd close mc-r", "primary source"} {
		if !strings.Contains(p, want) {
			t.Errorf("research prompt missing %q", want)
		}
	}
}

// An exit-0 Run whose final line is non-empty but is NOT the "Wrote docs/research/"
// pointer (a soft failure that still printed something, e.g. "Unable to find primary
// sources") must leave the bead OPEN for the next tick — never false-close it against
// a doc that never landed (AC3). The guard short-circuits before any bd
// contact, so Run returns nil without shelling out to bd (mirrors the empty-Output
// short-circuit above: a non-pointer line reaching bd.Show would error on the fake id).
func TestRunResearchNonPointerOutputLeavesBeadOpen(t *testing.T) {
	h := &stubHarness{out: "Unable to find primary sources"}
	if err := (Nami{}).Run(stubCtx{h}, contract.Bead{ID: "mc-nonexistent-xyz", Kind: contract.KindResearch}); err != nil {
		t.Fatalf("non-pointer exit-0 Run must short-circuit before bd (leave bead open), got %v", err)
	}
}

// Finding 1: headless claude Output carries the full assistant message,
// so the pointer line the agent emits last sits behind any prose it printed first.
// The close guard matches the LAST non-empty line against researchDocPointer, not the
// whole Output — so a multi-line Output ending in the pointer still closes the bead,
// while a non-pointer final line leaves it open.
func TestLastNonEmptyLineDrivesCloseGuard(t *testing.T) {
	closes := func(out string) bool {
		return strings.HasPrefix(lastNonEmptyLine(out), researchDocPointer)
	}
	// Multi-line output ending in the pointer (prose before it) → closes.
	if !closes("Investigated the sources.\n\nWrote docs/research/tail-seams.md: two seams found.\n") {
		t.Error("pointer as the last non-empty line (prose before it) must close the bead")
	}
	// Non-pointer final line → stays open.
	if closes("Wrote docs/research/tail-seams.md: found it.\nUnable to verify one claim") {
		t.Error("a non-pointer final line must leave the bead open")
	}
	// Empty output → stays open.
	if closes("\n  \n") {
		t.Error("empty output must leave the bead open")
	}
}

// A non-launchable kind (AFK research, containers) is a loud error, not a bad
// command (spec §9.3).
func TestLaunchRejectsNonHITL(t *testing.T) {
	if _, err := (Nami{}).Launch(stubCtx{&stubHarness{}}, contract.Bead{ID: "x", Kind: contract.KindResearch}); err == nil {
		t.Error("Launch of an AFK/non-HITL kind must error")
	}
}

// The reconcile switch: an unowned kind is a loud error. The signal/chart bd legs
// touch bd, so they're covered by bd's argv/parse tests and specSuccessor below.

// Acceptance #2: an un-charted Chart (body still the placeholder) is a no-op seam —
// reconcile returns before any dispatch or bd I/O, whatever the label. The charting
// session hasn't written the body yet, so there is nothing to judge (ADR 0021). The
// showBead fatal proves the gate returns before touching bd.
func TestReconcileChartUnchartedIsNoop(t *testing.T) {
	h := &recordingHarness{}
	closed := stubChartSeams(t, func(id string) (bd.Bead, error) {
		t.Fatalf("un-charted gate must return before any bd I/O, showBead(%q) called", id)
		return bd.Bead{}, nil
	})
	chart := bd.Bead{ID: "ws-chart", Kind: contract.KindChart, Status: "open", Description: contract.ChartPlaceholderBody}
	if err := reconcileChart(stubCtx{h}, chart); err != nil {
		t.Errorf("un-charted Chart must be a silent no-op, got %v", err)
	}
	if len(h.invs) != 0 {
		t.Errorf("un-charted Chart must dispatch no session, ran %d", len(h.invs))
	}
	if len(*closed) != 0 {
		t.Errorf("un-charted Chart must close nothing, got %v", *closed)
	}
}

// Acceptance #1/#4/#5: a drained, charted, NOT-defogged Chart dispatches the
// promote-or-stamp judge anchored to the Chart, then returns thin — no post-dispatch
// re-read of the Chart/Spec and no close. The judge stamps or spawns itself, so the
// handler touches neither showBead nor closeBead (the showBead fatal enforces it).
func TestReconcileChartNotDefoggedDispatchesPromoteOrStamp(t *testing.T) {
	h := &recordingHarness{}
	closed := stubChartSeams(t, func(id string) (bd.Bead, error) {
		t.Fatalf("promote-or-stamp path must not re-read a bead, showBead(%q) called", id)
		return bd.Bead{}, nil
	})
	chart := bd.Bead{ID: "ws-chart", Kind: contract.KindChart, Status: "open", Description: "Destination: ship it"}
	if err := reconcileChart(stubCtx{h}, chart); err != nil {
		t.Fatalf("reconcileChart: %v", err)
	}
	if len(h.invs) != 1 {
		t.Fatalf("promote-or-stamp session must run exactly once, ran %d", len(h.invs))
	}
	if h.invs[0].BeadID != "ws-chart" {
		t.Errorf("session must anchor to the Chart, got %q", h.invs[0].BeadID)
	}
	if !strings.Contains(h.invs[0].Prompt, "ws-chart") {
		t.Errorf("promote-or-stamp prompt must name the Chart, got %q", h.invs[0].Prompt)
	}
	if len(*closed) != 0 {
		t.Errorf("the promote-or-stamp path must not close the Chart, got %v", *closed)
	}
}

// The emitted Spec is exactly one leaf kind:spec bead seeded with the placeholder
// body (§8.4/§8.6). specSuccessor fixes that shape; SpawnSuccessorIfAbsent (bd)
// guarantees the "exactly one" on re-run. The placeholder body IS the close gate
// (see reconcileChart), so it must match the constant exactly.
func TestSpecSuccessorIsPlaceholderSpecLeaf(t *testing.T) {
	s := specSuccessor(bd.Bead{ID: "c", Title: "a chart"})
	if s.Context != "" {
		t.Errorf("unlabelled Chart yields Context %q, want none", s.Context)
	}
	if s.Kind != contract.KindSpec {
		t.Errorf("successor kind = %q, want spec", s.Kind)
	}
	if s.Container {
		t.Error("a Spec is a leaf (bd task), not a container")
	}
	if s.Description != placeholderSpecBody {
		t.Errorf("Spec body must be the placeholder close-gate marker, got %q", s.Description)
	}
	if s.Title != "a chart" {
		t.Errorf("Spec title = %q, want the Chart's title verbatim", s.Title)
	}
	if s.DraftedBy != "c" {
		t.Errorf("Spec must be a Draft by the Chart (FR 21), got DraftedBy %q", s.DraftedBy)
	}
}

// publishingCtx is a dispatched effort's Ctx: it carries the contract.Publisher.
type publishingCtx struct {
	stubCtx
	closes []string
	edges  int
}

func (c *publishingCtx) CloseOnPublish(reason string) { c.closes = append(c.closes, reason) }
func (c *publishingCtx) EdgeOnPublish(bd.Edge)        { c.edges++ }

// Spec D6: in a dispatched effort reconcileChart spawns the Spec as a Draft by the
// Chart and hands the Chart's close to the publish instead of closing it itself,
// so the Spec and the close become visible in one batch.
func TestReconcileChartHandsCloseToPublish(t *testing.T) {
	authored := false
	closed := stubChartSeams(t, func(string) (bd.Bead, error) {
		if authored {
			return bd.Bead{ID: "ws-spec", Status: "deferred", Description: "the authored spec body"}, nil
		}
		return bd.Bead{ID: "ws-spec", Status: "deferred", Description: placeholderSpecBody}, nil
	})
	var spawned bd.Successor
	spawnSuccessor = func(_ string, s bd.Successor) (string, bool, error) { spawned = s; return "ws-spec", true, nil }
	ctx := &publishingCtx{stubCtx: stubCtx{&recordingHarness{authored: &authored}}}
	if err := reconcileChart(ctx, defoggedChart("open")); err != nil {
		t.Fatalf("reconcileChart: %v", err)
	}
	if spawned.DraftedBy != "ws-chart" {
		t.Errorf("Spec must be spawned as a Draft by the Chart, got DraftedBy %q", spawned.DraftedBy)
	}
	if len(*closed) != 0 {
		t.Errorf("reconcileChart must not close the Chart itself in an effort, closed %v", *closed)
	}
	if len(ctx.closes) != 1 {
		t.Errorf("the Chart's close must be handed to the publish once, got %d", len(ctx.closes))
	}
}

// The Chart's branch name reconcileChart lands is chart/<chartID>, built from git/'s naming rule (ADR
// 0010 §3). The real cut behavior is git/ Seam B's; here we fix only the name
// the handler computes.
func TestChartBranchName(t *testing.T) {
	if got := chartBranch("mc-x"); got != "chart/mc-x" {
		t.Errorf("chart branch = %q, want chart/mc-x", got)
	}
}

// The pure part of the drained-Chart retrofit: the trailer reconcileChart
// stamps its land with is "Chart: <chartID>", built from git/'s trailer rule (ADR
// 0010 §2) so LandedBase greps back the exact same string. The real land behavior is
// git/ Seam B's; here we fix only the trailer the handler computes.
func TestChartTrailer(t *testing.T) {
	if got := chartTrailer("mc-x"); got != "Chart: mc-x" {
		t.Errorf("chart trailer = %q, want %q", got, "Chart: mc-x")
	}
}

// A Chart-land conflict becomes a chart-conflict member carrying the base SHA
// payload (ADR 0011, ADR 0030): the bead body names the branch, the exact base
// commit to rebase onto (Base:), the Chart tip the failed land saw (Tip:, Spec
// D4), the variant the resolve session runs and any clashing ADRs. Pure — the
// spawn itself (idempotency, parent edge) is bd.SpawnChildIfAbsent's contract.
func TestChartConflictBodyCarriesBranchAndBase(t *testing.T) {
	body := ChartConflictBody("mc-x", "deadbeef42", "t1psha", ConflictNumberClash, []string{"docs/adr/0031-a.md (rule **/docs/adr/{n:4}-*.md) (vs mainline docs/adr/0031-b.md)"})
	for _, want := range []string{"numbered-file clash", "ADR 0219", "branch: chart/mc-x", git.BasePin("deadbeef42"), git.TipPin("t1psha"),
		"variant: " + ConflictNumberClash, "clashes:\n- docs/adr/0031-a.md (rule **/docs/adr/{n:4}-*.md)"} {
		if !strings.Contains(body, want) {
			t.Errorf("conflict body payload missing %q, got %q", want, body)
		}
	}
	// Resolved AFK now, not a launched session (Spec D4).
	if strings.Contains(body, "Launch this bead") {
		t.Errorf("conflict body must not ask to launch the bead, got %q", body)
	}
	// A session payload, not a hand-run checklist (ADR 0030 §3).
	if strings.Contains(body, "git checkout") {
		t.Errorf("conflict body must be a payload, not checklist steps, got %q", body)
	}
}

// Chart-conflict is AFK (ADR 0080): Launch refuses it like any other AFK kind,
// so no human can start a resolve that races the loop.
func TestLaunchRejectsChartConflict(t *testing.T) {
	if _, err := (Nami{}).Launch(stubCtx{&stubHarness{}}, contract.Bead{ID: "x", Kind: contract.KindChartConflict}); err == nil {
		t.Error("Launch of an AFK chart-conflict must error")
	}
}

// The AFK prompt pins only its contract tokens (Spec D7): the two outcomes,
// Go closes (no bd close), the roll-forward stanza, binding grilling answers read
// off the blocks edges, and the rebase abort on escalate.
func TestChartConflictPromptContract(t *testing.T) {
	p := chartConflictPrompt("ws-cf")
	for _, want := range []string{"bd show ws-cf", "Resolved: <one-line summary>", "Escalate: <title>",
		"Do NOT run `bd close`", "Resolved: already resolved", "binding", "bd dep list ws-cf", "git rebase --abort"} {
		if !strings.Contains(p, want) {
			t.Errorf("chart-conflict prompt missing %q", want)
		}
	}
	if strings.Contains(p, "bd close ws-cf") {
		t.Error("the AFK agent must not close its own bead (ADR 0080 §1)")
	}
}

// chartConflictRecord logs runChartConflict's checks, restores, and closes made
// outside an effort.
type chartConflictRecord struct {
	checks   [][2]string
	restores []string
	closes   []string
	// integrates logs each Go integrate as "after <n> checks";
	// integrateRC is its exit.
	integrates  []string
	integrateRC int
	// escalation: blockers and edgeHeld are inputs; spawned/bodies/edges are what
	// was filed and handed to the publish.
	blockers []bd.Bead
	edgeHeld bool
	spawned  []string // "parent kind drafted-by title"
	bodies   []string
	edges    []bd.Edge
}

// chartConflictSeams stubs runChartConflict's bd/git legs: showBead serves bead
// (Status mutable through the pointer) and the check returns checkErr.
func chartConflictSeams(t *testing.T, bead *bd.Bead, checkErr error) *chartConflictRecord {
	t.Helper()
	r := &chartConflictRecord{}
	swap(t, &showBead, func(string) (bd.Bead, error) { return *bead, nil })
	swap(t, &branchHead, func(string) (string, error) { return "startsha", nil })
	swap(t, &checkResolved, func(_ git.Repo, base, tip string) error {
		r.checks = append(r.checks, [2]string{base, tip})
		return checkErr
	})
	swap(t, &restoreResolved, func(_ git.Repo, start string) error { r.restores = append(r.restores, start); return nil })
	swap(t, &integrate, func(w io.Writer) int {
		r.integrates = append(r.integrates, fmt.Sprintf("after %d checks", len(r.checks)))
		fmt.Fprintf(w, "integrate exited %d", r.integrateRC)
		return r.integrateRC
	})
	swap(t, &openBlockers, func(string, contract.Kind) ([]bd.Bead, error) { return r.blockers, nil })
	swap(t, &spawnChild, func(parent string, s bd.Successor) (string, error) {
		r.spawned = append(r.spawned, parent+" "+string(s.Kind)+" "+s.DraftedBy+" "+s.Title)
		r.bodies = append(r.bodies, s.Description)
		return "ws-grill", nil
	})
	swap(t, &hasEdge, func(from, to, typ string) (bool, error) { return r.edgeHeld, nil })
	swap(t, &edgesOnPublish, func(ctx contract.Ctx, creator string, es []bd.Edge) error {
		r.edges = append(r.edges, es...)
		return bd.EdgesOnPublish(ctx, creator, es)
	})
	swap(t, &closeOnPublish, func(ctx contract.Ctx, id, reason string, es []bd.Edge) error {
		if _, ok := ctx.(contract.Publisher); ok {
			return bd.CloseOnPublish(ctx, id, reason, es)
		}
		r.closes = append(r.closes, reason)
		return nil
	})
	return r
}

func openChartConflict() bd.Bead {
	return bd.Bead{ID: "ws-cf", Kind: contract.KindChartConflict, Status: "open", Parent: "ws-chart",
		Description: ChartConflictBody("ws-chart", "b4se", "t1p", ConflictRebase, nil)}
}

// Resolved: + a passing check → one Open()-guarded close through the publish,
// carrying the summary; the check runs against the pinned Base:/Tip:.
func TestRunChartConflictResolvedClosesThroughPublish(t *testing.T) {
	b := openChartConflict()
	r := chartConflictSeams(t, &b, nil)
	h := &stubHarness{out: "Rebased onto base.\n\nResolved: kept both GLOSSARY.md entries\n"}
	ctx := &publishingCtx{stubCtx: stubCtx{h}}
	if err := (Nami{}).Run(ctx, contract.Bead{ID: "ws-cf", Kind: contract.KindChartConflict}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if h.runInv.BeadID != "ws-cf" || !strings.Contains(h.runInv.Prompt, "ws-cf") {
		t.Errorf("session must anchor to the bead, got %q", h.runInv.BeadID)
	}
	if len(r.checks) != 1 || r.checks[0] != [2]string{"b4se", "t1p"} {
		t.Errorf("checks = %v, want one against (b4se, t1p)", r.checks)
	}
	if len(ctx.closes) != 1 || ctx.closes[0] != "kept both GLOSSARY.md entries" || ctx.edges != 0 {
		t.Errorf("publish closes = %q edges %d, want one close with the summary, no edges", ctx.closes, ctx.edges)
	}
	if len(r.restores) != 0 {
		t.Errorf("a passing check must restore nothing, got %v", r.restores)
	}
	if want := []string{"after 1 checks"}; !reflect.DeepEqual(r.integrates, want) {
		t.Errorf("integrates = %q, want Go's one integrate after the check (Spec ws-vsq9 FR 13)", r.integrates)
	}

	// A re-run on the now-closed bead: no double close.
	b.Status = "closed"
	ctx = &publishingCtx{stubCtx: stubCtx{h}}
	if err := (Nami{}).Run(ctx, contract.Bead{ID: "ws-cf", Kind: contract.KindChartConflict}); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if len(ctx.closes) != 0 {
		t.Errorf("an already closed bead must not be closed again, got %q", ctx.closes)
	}
}

// Anything but a Resolved: or well-formed Escalate: final line — prose, empty
// output, an Escalate: with no Blocks: payload, or a harness error — fails the
// effort and leaves the bead open, running no check.
func TestRunChartConflictNonResolvedErrors(t *testing.T) {
	cases := map[string]*stubHarness{
		"prose":                {out: "Resolved: done\nI could not finish the rebase"},
		"empty":                {out: "\n  \n"},
		"escalate, no payload": {out: "I am unsure.\nEscalate: which ADR 0031?"},
		"harness error":        {err: errors.New("claude crashed")},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			b := openChartConflict()
			r := chartConflictSeams(t, &b, nil)
			ctx := &publishingCtx{stubCtx: stubCtx{h}}
			err := (Nami{}).Run(ctx, contract.Bead{ID: "ws-cf", Kind: contract.KindChartConflict})
			if err == nil {
				t.Fatal("want an error")
			}
			if h.err != nil && !errors.Is(err, h.err) {
				t.Errorf("harness error must be returned as-is, got %v", err)
			}
			if h.out != "" && !strings.Contains(err.Error(), lastNonEmptyLine(h.out)) {
				t.Errorf("error must name the final line, got %v", err)
			}
			if len(ctx.closes)+len(r.closes)+len(r.checks) != 0 {
				t.Errorf("no check and no close on a failed outcome, got %+v / %q", r, ctx.closes)
			}
		})
	}
}

const escalateOut = "Rebased; one hunk is contradictory.\n\nBlocks: ws-cf\nVariant: rebase-conflict\n\n" +
	"1. GLOSSARY.md:12\n   Mainline: a\n   Chart: b\nEscalate: Which Chart-conflict definition stands?\n"

// Escalate: files a grilling Draft child of the Chart, drafted-by the conflict,
// carrying the Blocks: payload, and hands conflict blocks→grilling to the
// publish — no check, no restore, no close (ADR 0079, Spec D6).
func TestRunChartConflictEscalateFilesGrilling(t *testing.T) {
	b := openChartConflict()
	r := chartConflictSeams(t, &b, nil)
	ctx := &publishingCtx{stubCtx: stubCtx{&stubHarness{out: escalateOut}}}
	if err := (Nami{}).Run(ctx, contract.Bead{ID: "ws-cf", Kind: contract.KindChartConflict}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := []string{"ws-chart grilling ws-cf Which Chart-conflict definition stands?"}; !reflect.DeepEqual(r.spawned, want) {
		t.Errorf("spawned = %q, want %q", r.spawned, want)
	}
	if body := r.bodies[0]; !strings.HasPrefix(body, "Blocks: ws-cf\nVariant: rebase-conflict") ||
		!strings.Contains(body, "Chart: b") || !strings.Contains(body, "under the same number") ||
		strings.Contains(body, "Escalate:") || strings.Contains(body, "contradictory.") {
		t.Errorf("grilling body must be the Blocks: payload alone, got %q", body)
	}
	if want := []bd.Edge{{From: "ws-cf", To: "ws-grill", Type: "blocks"}}; !reflect.DeepEqual(r.edges, want) || ctx.edges != 1 {
		t.Errorf("edges = %v (handed %d), want %v handed to the publish", r.edges, ctx.edges, want)
	}
	if len(r.checks)+len(r.restores)+len(r.closes)+len(ctx.closes) != 0 {
		t.Errorf("Escalate: must run no check, restore or close, got %+v / %q", r, ctx.closes)
	}
}

// A re-run while an open grilling blocker stands files nothing and returns nil,
// so escalations never stack.
func TestRunChartConflictEscalateWithOpenGrillingFilesNothing(t *testing.T) {
	b := openChartConflict()
	r := chartConflictSeams(t, &b, nil)
	r.blockers = []bd.Bead{{ID: "ws-old", Kind: contract.KindGrilling, Status: "open"}}
	ctx := &publishingCtx{stubCtx: stubCtx{&stubHarness{out: escalateOut}}}
	if err := (Nami{}).Run(ctx, contract.Bead{ID: "ws-cf", Kind: contract.KindChartConflict}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(r.spawned)+len(r.edges)+ctx.edges != 0 {
		t.Errorf("an open grilling blocker must file nothing, got %+v", r)
	}
}

// An edge bd already holds is not handed over: one failing batch line rolls the
// whole publish back.
func TestRunChartConflictEscalateSkipsHeldEdge(t *testing.T) {
	b := openChartConflict()
	r := chartConflictSeams(t, &b, nil)
	r.edgeHeld = true
	ctx := &publishingCtx{stubCtx: stubCtx{&stubHarness{out: escalateOut}}}
	if err := (Nami{}).Run(ctx, contract.Bead{ID: "ws-cf", Kind: contract.KindChartConflict}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(r.spawned) != 1 || len(r.edges)+ctx.edges != 0 {
		t.Errorf("want the Draft filed and no edge re-added, got %+v", r)
	}
}

// A body with no readable base pin fails loudly, before any session runs.
func TestRunChartConflictMissingBaseErrors(t *testing.T) {
	b := openChartConflict()
	b.Description = "branch: chart/ws-chart\nvariant: rebase-conflict"
	chartConflictSeams(t, &b, nil)
	h := &stubHarness{out: "Resolved: x"}
	if err := (Nami{}).Run(stubCtx{h}, contract.Bead{ID: "ws-cf", Kind: contract.KindChartConflict}); err == nil {
		t.Fatal("a missing base pin must be an error")
	}
	if h.runInv.BeadID != "" {
		t.Error("no session may run against a missing base")
	}
}

// A failed post-resolve check restores the session-start tip and fails the
// effort with the check's error (naming the assertion); nothing is closed.
func TestRunChartConflictFailedCheckRestores(t *testing.T) {
	b := openChartConflict()
	checkErr := &git.ResolveCheckFailed{Assertion: 3, Reason: "mainline-only paths changed", Paths: []string{"GLOSSARY.md"}}
	r := chartConflictSeams(t, &b, checkErr)
	ctx := &publishingCtx{stubCtx: stubCtx{&stubHarness{out: "Resolved: done"}}}
	err := (Nami{}).Run(ctx, contract.Bead{ID: "ws-cf", Kind: contract.KindChartConflict})
	var failed *git.ResolveCheckFailed
	if !errors.As(err, &failed) || !strings.Contains(err.Error(), "check 3") {
		t.Errorf("err = %v, want the check's *ResolveCheckFailed naming its assertion", err)
	}
	if len(r.restores) != 1 || r.restores[0] != "startsha" {
		t.Errorf("restores = %v, want one to the session-start tip", r.restores)
	}
	if len(ctx.closes)+len(r.closes) != 0 {
		t.Errorf("a failed check must close nothing, got %q", ctx.closes)
	}
	if len(r.integrates) != 0 {
		t.Errorf("a failed check must publish nothing onto the Chart, got %q", r.integrates)
	}
}

// A passing check whose integrate exits 1 (transient) fails the effort naming
// the exit; one exiting 2 (a replay conflict, a held or landed Chart) returns nil
// for the backstop's re-run to pause it integrate-conflict (ADR 0102 §6). Neither
// closes.
func TestRunChartConflictFailedIntegrateClosesNothing(t *testing.T) {
	for rc, wantErr := range map[int]bool{1: true, 2: false} {
		b := openChartConflict()
		r := chartConflictSeams(t, &b, nil)
		r.integrateRC = rc
		ctx := &publishingCtx{stubCtx: stubCtx{&stubHarness{out: "Resolved: done"}}}
		err := (Nami{}).Run(ctx, contract.Bead{ID: "ws-cf", Kind: contract.KindChartConflict})
		if (err != nil) != wantErr || (wantErr && !strings.Contains(err.Error(), "exited 1")) {
			t.Errorf("exit %d: err = %v, want error %v naming the exit", rc, err, wantErr)
		}
		if len(ctx.closes)+len(r.closes) != 0 {
			t.Errorf("exit %d: a failed integrate must close nothing, got %q", rc, ctx.closes)
		}
	}
}

// The chart-conflict agent rewrites on its effort branch but never runs mugicrew
// integrate: Go publishes the rewrite only after its post-resolve check passes.
func TestChartConflictPromptLeavesIntegrateToGo(t *testing.T) {
	p := chartConflictPrompt("mc-x")
	if !strings.Contains(p, "effort/mc-x") {
		t.Error("chart-conflict prompt must say the session works on its effort branch")
	}
	if strings.Contains(p, "run `mugicrew integrate`") || strings.Contains(p, "re-run `mugicrew integrate`") {
		t.Error("the chart-conflict agent must not integrate: Go does, after its check")
	}
}

// Spec mc-r832 D6–D7 (FR 2, 11–13): launching a Survey opens Nami's charting
// session anchored on the Survey. The prompt claims and drafts as the Survey, but
// parents, appends to, links and pushes back on its parent Chart; hand-back and
// reject close the Chart with --force (the open Survey is still its child).
func TestLaunchSurveyRendersChartingPromptWithBothIDs(t *testing.T) {
	swap(t, &showBead, func(id string) (bd.Bead, error) {
		return bd.Bead{ID: id, Kind: contract.KindSurvey, Status: "open", Parent: "ws-chart"}, nil
	})
	h := &stubHarness{}
	if _, err := (Nami{}).Launch(stubCtx{h}, contract.Bead{ID: "ws-srv", Kind: contract.KindSurvey}); err != nil {
		t.Fatalf("Launch Survey: %v", err)
	}
	got := strings.Join(strings.Fields(h.inv.Prompt), " ")
	if h.inv.BeadID != "ws-srv" {
		t.Errorf("Invocation anchored on %q, want the Survey", h.inv.BeadID)
	}
	for _, want := range []string{
		"Survey: ws-srv (your bead — read it with: bd show ws-srv)",
		"Chart: ws-chart (its parent — read it with: bd show ws-chart)",
		"bd update ws-srv --claim",
		"-l kind:<grilling|research|prototype>,drafted-by:ws-srv",
		"--parent ws-chart --no-inherit-labels",
		"signal-filing.md",
		"mugicrew signal offshoot --from ws-srv --drafted-by ws-srv",
		"--order none",
		`bd close ws-chart --force --reason "handed back:`,
		`bd close ws-chart --force --reason "<why>"`,
		"bd supersede ws-chart --with",
		"mugicrew chart drop ws-chart Destination",
		"mugicrew chart append ws-chart Destination",
		"bd dep add <task> ws-chart",
		strings.Join(strings.Fields(contract.DraftInstruction("ws-srv")), " "),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("charting prompt missing %q:\n%s", want, h.inv.Prompt)
		}
	}
	for _, gone := range []string{
		"bd update ws-chart --claim", "drafted-by:ws-chart", "--parent ws-srv",
		"discovered-from:ws-srv", "discovered-from ws-chart", "--from ws-chart", "bd close ws-srv", "Signal under triage", "mugicrew reconcile", "FAST LANE",
	} {
		if strings.Contains(got, gone) {
			t.Errorf("charting prompt carries %q", gone)
		}
	}
}

// Spec mc-r832 D6 (FR 3): a Chart is a container, so launching one is refused
// like any other; a Survey with no parent Chart, or an unreadable one, errors.
func TestLaunchRefusesChart(t *testing.T) {
	if _, err := (Nami{}).Launch(stubCtx{&stubHarness{}}, contract.Bead{ID: "ws-chart", Kind: contract.KindChart}); err == nil {
		t.Error("Launch of a Chart must error")
	}
	for name, show := range map[string]func(string) (bd.Bead, error){
		"no parent":  func(id string) (bd.Bead, error) { return bd.Bead{ID: id, Kind: contract.KindSurvey}, nil },
		"read fails": func(string) (bd.Bead, error) { return bd.Bead{}, errors.New("bd: locked") },
	} {
		swap(t, &showBead, show)
		if _, err := (Nami{}).Launch(stubCtx{&stubHarness{}}, contract.Bead{ID: "ws-srv", Kind: contract.KindSurvey}); err == nil {
			t.Errorf("%s: Launch of the Survey must error", name)
		}
	}
}

// Spec mc-r832 D2 (FR 22–23): a Survey is not a defog bead, so promote-or-stamp
// and speccing skip a closed kind:survey child when they gather the Chart's
// closed children.
func TestJudgeAndSpeccingSkipClosedSurvey(t *testing.T) {
	for name, p := range map[string]string{"promote-or-stamp": promoteOrStampPrompt("mc-x"), "speccing": speccingPrompt("mc-x")} {
		if flat := strings.Join(strings.Fields(p), " "); !strings.Contains(flat, "Skip the kind:"+string(contract.KindSurvey)+" child") {
			t.Errorf("%s prompt must skip the closed kind:survey child:\n%s", name, p)
		}
	}
}

// Spec D11 row 7: the grilling session drafts its graduated fog by the grilling
// bead (the shared block, not a re-phrasing) BEFORE it closes that bead, so the
// close can never drain the Chart ahead of the fog it graduates.
func TestGrillingPromptDraftsFogBeforeClose(t *testing.T) {
	got := grillingPrompt("ws-grl")
	if !strings.Contains(got, contract.DraftInstruction("ws-grl")) {
		t.Errorf("grilling prompt must embed contract.DraftInstruction(grilling):\n%s", got)
	}
	draft := strings.Index(got, "-s deferred -l kind:<grilling|research|prototype>,drafted-by:ws-grl")
	closeAt := strings.Index(got, "bd close ws-grl")
	if draft < 0 || closeAt < 0 || draft > closeAt {
		t.Errorf("grilling prompt must create the fog Drafts (at %d) before the close (at %d):\n%s", draft, closeAt, got)
	}
}

// Spec D9: the defog prompts that tend the Chart body do it one line at
// a time through the locked verb, never a whole-description bd update.
func TestDefogPromptsTendChartThroughVerb(t *testing.T) {
	for name, got := range map[string]string{"charting": chartingPrompt("mc-srv", "mc-x"), "grilling": grillingPrompt("mc-x"), "research": researchPrompt("mc-x")} {
		if !strings.Contains(got, "mugicrew chart append") || strings.Contains(got, "bd update <chartID>") {
			t.Errorf("%s prompt must tend the Chart through mugicrew chart append:\n%s", name, got)
		}
	}
}

// The TUI claims a grilling bead on launch (Spec FR13), so the prompt
// never tells the session to claim it again.
func TestGrillingPromptDoesNotClaim(t *testing.T) {
	if got := grillingPrompt("ws-grl"); strings.Contains(got, "--claim") || !strings.Contains(got, "skipping its Claim step") {
		t.Errorf("grilling prompt must skip the claim:\n%s", got)
	}
}

// Spec D6 over the HITL charting and grilling prompts as rendered: no
// skill call, no ADR citation, every crew-doc reference resolves, and the docs
// each session runs on are named.
func TestChartingGrillingPromptsSkillFree(t *testing.T) {
	for name, c := range map[string]struct {
		got  string
		docs []string
	}{
		"charting": {chartingPrompt("mc-srv", "mc-x"), []string{"grilling.md", "domain-modeling.md", "chart-body.md"}},
		"grilling": {grillingPrompt("mc-x"), []string{"grilling.md", "domain-modeling.md", "defog-session.md"}},
	} {
		promptguard.Check(t, name, c.got)
		refs := promptguard.CrewDocRefs(c.got)
		for _, d := range c.docs {
			if !slices.Contains(refs, d) {
				t.Errorf("%s prompt reads no crew doc %s (refs %q)", name, d, refs)
			}
		}
	}
}

// Spec D6 over the four AFK nami prompts as rendered: no
// skill call, no ADR citation, every crew-doc reference resolves, and each reads
// the docs it runs on; research keeps its discipline inline and reads none.
func TestAFKPromptsSkillFree(t *testing.T) {
	for name, c := range map[string]struct {
		got  string
		docs []string
	}{
		"speccing":         {speccingPrompt("mc-x"), []string{"spec-template.md", "chart-body.md"}},
		"promote-or-stamp": {promoteOrStampPrompt("mc-x"), []string{"chart-body.md"}},
		"research":         {researchPrompt("mc-x"), nil},
		"chart-conflict":   {chartConflictPrompt("mc-x"), []string{"sequence-renumber.md"}},
	} {
		promptguard.Check(t, name, c.got)
		refs := promptguard.CrewDocRefs(c.got)
		for _, d := range c.docs {
			if !slices.Contains(refs, d) {
				t.Errorf("%s prompt reads no crew doc %s (refs %q)", name, d, refs)
			}
		}
	}
}

// Spec D6.5 / FR 35: promote-or-stamp splices the stamp label from
// contract.LabelDefogged, so it can't drift from the loop's dispatch gate.
func TestPromoteOrStampSplicesLabelDefogged(t *testing.T) {
	if strings.Contains(promoteOrStampTmpl, "chart:defogged") {
		t.Error("promote-or-stamp template must not write chart:defogged as a literal")
	}
	if !strings.Contains(promoteOrStampPrompt("mc-x"), "bd update mc-x --add-label "+contract.LabelDefogged) {
		t.Error("promote-or-stamp prompt must carry contract.LabelDefogged in its stamp command")
	}
}

func TestReconcileUnknownKindErrors(t *testing.T) {
	if err := (Nami{}).Reconcile(nil, bd.Bead{ID: "s", Kind: contract.Kind("spec")}); err == nil {
		t.Error("reconcile of a kind with no handler must error")
	}
}

// TestPromptsRender guards the go:embed'd prompt templates: a broken embed path
// or a stray literal % (which fmt.Sprintf turns into %!(...)) fails here.
func TestPromptsRender(t *testing.T) {
	for _, got := range []string{chartingPrompt("mc-srv", "mc-x"), grillingPrompt("mc-x"), chartConflictPrompt("mc-x"), speccingPrompt("mc-x"), promoteOrStampPrompt("mc-x")} {
		if !strings.Contains(got, "mc-x") || strings.Contains(got, "%!") {
			t.Fatalf("bad prompt render: %q", got)
		}
	}
}

// The speccing prompt (forked from to-spec): it embeds and formats the
// chart id in, and pins the fork specifics an edit must not silently drop — read
// the Chart body + closed defog beads + prototype side-refs, author the existing
// Spec bead via bd update (no new bead, no triage label), and carry all seven
// template sections in the Spec template order.
func TestSpeccingPromptCarriesForkSpecifics(t *testing.T) {
	p := speccingPrompt("mc-x")
	// The prototype side-ref path must format the chart id in (per-chart artifact).
	for _, want := range []string{
		"refs/mugicrew/prototypes/mc-x",                // prototype side-refs, per chart
		"bd dep list mc-x --direction=up",              // gather the closed defog children (dependents), not the upstream dep
		"defog bead",                                   // read every closed defog bead
		`bd update <specID> --title "<what is built>"`, // author into the existing bead, retitled...
		"No kind prefix",
		"do NOT create a bead", // ...not a new one
		"triage label",         // and no triage label
	} {
		if !strings.Contains(p, want) {
			t.Errorf("speccing prompt missing fork specific %q", want)
		}
	}
	// The seven sections moved to the one Spec template Crew doc (FR 30).
	if !slices.Contains(promptguard.CrewDocRefs(p), "spec-template.md") {
		t.Error("speccing prompt must read crew doc spec-template.md")
	}
	assertSpecSections(t, "spec-template.md", promptguard.Docs(t)["spec-template.md"])
}

// assertSpecSections pins all seven standard Spec template sections, in the
// template order, somewhere in p.
// Spec D5: the template's optional ## Tasks section follows Further
// Notes, leaving the seven sections and their order unchanged.
func TestSpecTemplateOptionalTasksAfterFurtherNotes(t *testing.T) {
	doc := promptguard.Docs(t)["spec-template.md"]
	assertSpecSections(t, "spec-template.md", doc)
	i, j := strings.Index(doc, "## Further Notes"), strings.Index(doc, "## Tasks")
	if j < 0 || j < i {
		t.Fatalf("spec-template.md: ## Tasks must follow ## Further Notes")
	}
	tasks := doc[j:]
	for _, want := range []string{"optional", "task-body.md", "after: land", "before: land", "after: <Task title>"} {
		if !strings.Contains(tasks, want) {
			t.Errorf("spec-template.md ## Tasks missing %q", want)
		}
	}
}

// Spec D5 (FR 4, 10): charting, grilling and promote-or-stamp may draft
// a free-standing Task blocked on a Chart or Spec, never a Chart child.
func TestChartSessionsMayDraftFreeStandingTask(t *testing.T) {
	for name, c := range map[string]struct{ p, drafter string }{
		"charting":         {chartingPrompt("mc-srv", "mc-x"), "mc-srv"},
		"grilling":         {grillingPrompt("mc-x"), "mc-x"},
		"promote-or-stamp": {promoteOrStampPrompt("mc-x"), "mc-x"},
	} {
		flat := strings.Join(strings.Fields(c.p), " ")
		for _, want := range []string{"-l kind:task," + contract.DraftedBy(c.drafter), "never a Chart child", "Carry moves"} {
			if !strings.Contains(flat, want) {
				t.Errorf("%s prompt missing %q", name, want)
			}
		}
	}
}

func assertSpecSections(t *testing.T, what, p string) {
	t.Helper()
	sections := []string{
		"## Problem Statement",
		"## Solution",
		"## Functional Requirements",
		"## Build-sliceable Implementation Decisions",
		"## Testing Decisions",
		"## Out of Scope",
		"## Further Notes",
	}
	last := 0
	for _, s := range sections {
		i := strings.Index(p, s)
		if i < 0 {
			t.Errorf("%s missing section %q", what, s)
			continue
		}
		if i < last {
			t.Errorf("%s: section %q is out of ws-plx order", what, s)
		}
		last = i
	}
}

// The promote-or-stamp judge prompt (ADR 0021/0022): it embeds the Chart
// id and the shared section names, and pins the judge contract an edit must not
// silently drop — the closed-children-required input, the never-derive-from-0-open
// rule, the three actions, the mandatory spawn flags, and the verbatim output block.
func TestPromoteOrStampPromptCarriesJudgeContract(t *testing.T) {
	p := promoteOrStampPrompt("mc-x")
	if !strings.Contains(p, wayfinderMapFields) {
		t.Errorf("prompt must name the shared Chart-body sections, got %q", p)
	}
	for _, want := range []string{
		"mc-x",                            // anchored to the Chart
		"bd dep list mc-x --direction=up", // gather the closed children (dependents) itself
		"closed children",                 // ...required, authoritative input (ADR 0022)
		"advisory",                        // Decisions-so-far is advisory only
		"NEVER derived from",              // the one rule: not from 0 open children
		"PROMOTE",                         // the three actions
		"STAMP",
		"ESCALATE",
		"bd update mc-x --add-label chart:defogged", // the stamp command
		"--parent mc-x", // spawn flags
		"--no-inherit-labels",
		contract.DraftInstruction("mc-x"),      // promoted children are Drafts by the Chart (FR 26)
		"DECISION: PROMOTE | STAMP | ESCALATE", // the output contract, verbatim
		"CONFIDENCE:",
		"PROMOTE_BEADS:",
		"REASONING:",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("promote-or-stamp prompt missing %q", want)
		}
	}
}

// recordingHarness is the fake contract.Harness the reconcileChart tests dispatch
// through: it records every AFK invocation, can be made to
// fail, and — like the real speccing session — flips `authored` on a successful Run
// so the stubbed showBead can report the Spec body as written. No live Claude Code.
type recordingHarness struct {
	invs     []contract.Invocation
	err      error // non-nil → the speccing session failed
	authored *bool // set true on a successful Run to mimic the session writing the body
}

func (h *recordingHarness) Run(_ contract.Ctx, inv contract.Invocation) (contract.Result, error) {
	h.invs = append(h.invs, inv)
	if h.err != nil {
		return contract.Result{}, h.err
	}
	if h.authored != nil {
		*h.authored = true
	}
	return contract.Result{}, nil
}
func (h *recordingHarness) LaunchCommand(contract.Invocation) (contract.LaunchCommand, error) {
	return nil, nil
}

func (h *recordingHarness) ResumeCommand(string, bool) (contract.AgentCommand, error) {
	return contract.AgentCommand{}, nil
}

// stubChartSeams swaps reconcileChart's bd/git legs for inert fakes (land/spawn/
// open-children succeed; showBead is caller-supplied) so the drained-Chart→Spec
// flow runs with no live bd or git. Returns a pointer to the slice of Chart IDs
// closed (by closeOnPublish outside an effort), so tests can assert whether — and how often — the
// Chart was closed. Restores the real funcs on cleanup. Mirrors loop_test.go.
func stubChartSeams(t *testing.T, show func(id string) (bd.Bead, error)) *[]string {
	t.Helper()
	origOpen, origSpawn, origShow := openChildren, spawnSuccessor, showBead
	origSpawnChild := spawnChildIfAbsent
	t.Cleanup(func() {
		openChildren, spawnSuccessor, showBead = origOpen, origSpawn, origShow
		spawnChildIfAbsent = origSpawnChild
	})
	closed := &[]string{}
	openChildren = func(string) (int, error) { return 0, nil }
	swap(t, &landedBase, func(string) (string, error) { return "landsha", nil })
	swap(t, &landingsOf, func(string) ([]bd.Bead, error) { return nil, nil })
	swap(t, &openBlockers, func(string, contract.Kind) ([]bd.Bead, error) { return nil, nil })
	swap(t, &openAnyBlockers, func(string) ([]bd.Bead, error) { return nil, nil })
	spawnSuccessor = func(string, bd.Successor) (string, bool, error) { return "ws-spec", true, nil }
	showBead = show
	swap(t, &closeOnPublish, func(ctx contract.Ctx, id, reason string, es []bd.Edge) error {
		if _, ok := ctx.(contract.Publisher); ok {
			return bd.CloseOnPublish(ctx, id, reason, es)
		}
		*closed = append(*closed, id)
		return nil
	})
	swap(t, &carry, func(string, string) ([]string, []bd.CarrySkip, error) { return nil, nil, nil })
	swap(t, &branchHead, func(string) (string, error) { return "chartsha", nil })
	swap(t, &postLandClashes, func(string) ([]git.PostLandClash, error) { return nil, nil })
	swap(t, &forgeProject, func(git.Repo) (forge.Project, error) { return forge.Project{}, nil })
	swap(t, &forgeFindOpenMR, func(forge.Project, string) (*forge.MR, error) { return nil, nil })
	return closed
}

func defoggedChart(status string) bd.Bead {
	return bd.Bead{ID: "ws-chart", Kind: contract.KindChart, Status: status, Labels: []string{contract.LabelDefogged}}
}

// Acceptance #1: a drained chart:defogged Chart lands, spawns exactly one Spec,
// invokes the speccing session once (anchored to the Spec), and closes the Chart
// only after the session authored the body.
func TestReconcileChartRunsSpeccingSessionThenCloses(t *testing.T) {
	authored := false
	h := &recordingHarness{authored: &authored}
	closed := stubChartSeams(t, func(string) (bd.Bead, error) {
		if authored {
			return bd.Bead{ID: "ws-spec", Status: "open", Description: "the authored spec body"}, nil
		}
		return bd.Bead{ID: "ws-spec", Status: "open", Description: placeholderSpecBody}, nil
	})

	if err := reconcileChart(stubCtx{h}, defoggedChart("open")); err != nil {
		t.Fatalf("reconcileChart: %v", err)
	}
	if len(h.invs) != 1 {
		t.Fatalf("speccing session must run exactly once, ran %d", len(h.invs))
	}
	if h.invs[0].BeadID != "ws-spec" {
		t.Errorf("session must anchor to the spawned Spec, got %q", h.invs[0].BeadID)
	}
	if !strings.Contains(h.invs[0].Prompt, "ws-chart") {
		t.Errorf("speccing prompt must name the Chart, got %q", h.invs[0].Prompt)
	}
	if len(*closed) != 1 || (*closed)[0] != "ws-chart" {
		t.Errorf("Chart must be closed exactly once after the body is authored, got %v", *closed)
	}
}

// A closed child still holding effort/<child> blocks the drained Chart (Spec
// D7, FR 27): reconcile waits, silently, with no land and no session, and
// the press-time refusal names the bead with both ways out (FR 46).
func TestReconcileChartWaitsOnAClosedChildWithAnEffortRef(t *testing.T) {
	h := &recordingHarness{}
	stubChartSeams(t, func(string) (bd.Bead, error) { return bd.Bead{}, nil })
	kids := []bd.Bead{{ID: "ws-chart.1", Status: "closed"}, {ID: "ws-chart.2", Status: "closed"}}
	efforts := map[string]bool{"ws-chart.2": true}
	openChildren = func(string) (int, error) {
		open, held := bd.DrainBlockersOver(kids, efforts)
		return open + len(held), nil
	}
	landedBase = func(string) (string, error) {
		t.Fatal("a Chart with a held child must not look at the land")
		return "", nil
	}

	if err := reconcileChart(stubCtx{h}, defoggedChart("open")); err != nil {
		t.Fatalf("reconcileChart: %v", err)
	}
	if len(h.invs) != 0 {
		t.Errorf("a held child must run no session, ran %d", len(h.invs))
	}
	_, held := bd.DrainBlockersOver(kids, efforts)
	hint := bd.HeldHint(held)
	for _, want := range []string{"ws-chart.2", "mugicrew integrate", "git branch -D effort/ws-chart.2"} {
		if !strings.Contains(hint, want) {
			t.Errorf("refusal %q must name %q", hint, want)
		}
	}
}

// Acceptance #2: a re-run over an already-authored, already-closed Chart runs no
// second session and closes nothing — no double-spawn, no double-close.
func TestReconcileChartReentrantSkipsSessionAndClose(t *testing.T) {
	h := &recordingHarness{}
	closed := stubChartSeams(t, func(string) (bd.Bead, error) {
		return bd.Bead{ID: "ws-spec", Status: "closed", Description: "the authored spec body"}, nil
	})

	if err := reconcileChart(stubCtx{h}, defoggedChart("closed")); err != nil {
		t.Fatalf("reconcileChart re-run: %v", err)
	}
	if len(h.invs) != 0 {
		t.Errorf("an authored Spec must not re-run the speccing session, ran %d", len(h.invs))
	}
	if len(*closed) != 0 {
		t.Errorf("an already-closed Chart must not be closed again, got %v", *closed)
	}
}

// Acceptance #3: a speccing-session failure leaves the Chart open (not closed) and
// the Spec on its placeholder body — the loop re-dispatches next tick.
func TestReconcileChartSessionFailureLeavesChartOpen(t *testing.T) {
	h := &recordingHarness{err: errors.New("speccing session crashed")}
	closed := stubChartSeams(t, func(string) (bd.Bead, error) {
		return bd.Bead{ID: "ws-spec", Status: "open", Description: placeholderSpecBody}, nil
	})

	err := reconcileChart(stubCtx{h}, defoggedChart("open"))
	if err == nil {
		t.Fatal("a session failure must propagate so the Chart stays open")
	}
	if len(h.invs) != 1 {
		t.Errorf("the session must have been attempted once, got %d", len(h.invs))
	}
	if len(*closed) != 0 {
		t.Errorf("the Chart must NOT be closed on a session failure, got %v", *closed)
	}
}

// A session that returns cleanly but leaves the body on the placeholder (a no-write
// synthesis) fails the effort and must NOT close the Chart — the close is gated on the artifact existing,
// not on the session merely having run (§8.4). The Chart stays open, loop re-dispatches.
func TestReconcileChartNoWriteLeavesChartOpen(t *testing.T) {
	h := &recordingHarness{} // Run succeeds but authors nothing
	closed := stubChartSeams(t, func(string) (bd.Bead, error) {
		return bd.Bead{ID: "ws-spec", Status: "open", Description: placeholderSpecBody}, nil
	})

	if err := reconcileChart(stubCtx{h}, defoggedChart("open")); err == nil {
		t.Fatal("a clean no-write must fail the effort, so its tail deletes the placeholder Draft Spec instead of publishing it")
	}
	if len(h.invs) != 1 {
		t.Errorf("the session must have run once, got %d", len(h.invs))
	}
	if len(*closed) != 0 {
		t.Errorf("the Chart must stay open when the body was not authored, got %v", *closed)
	}
}

// TestMain keeps stderr quiet (slog's default handler); use eventlogtest.Capture
// to assert events.
func TestMain(m *testing.M) {
	eventlogtest.Discard()
	os.Exit(m.Run())
}

// The AFK arm's pin reader (Spec FR 36–38): a new body yields base and tip; a
// legacy body (lowercase base:, no Tip:) yields its base, an empty tip and a
// check-3-skipped warning; a body with no base is an error, not a guess.
func TestChartConflictPins(t *testing.T) {
	evs := eventlogtest.Capture(t)
	base, tip, err := chartConflictPins(bd.Bead{ID: "mc-n", Description: ChartConflictBody("mc-x", "b4se", "t1p", ConflictRebase, nil)})
	if err != nil || base != "b4se" || tip != "t1p" || len(*evs) != 0 {
		t.Errorf("new body = (%q, %q, %v) events %+v; want (b4se, t1p, nil), no events", base, tip, err, *evs)
	}
	legacy := "Chart-land conflict. Launch this bead.\n\nbranch: chart/mc-x\nbase: 01dbase\nvariant: rebase-conflict"
	base, tip, err = chartConflictPins(bd.Bead{ID: "mc-l", Description: legacy})
	if err != nil || base != "01dbase" || tip != "" {
		t.Errorf("legacy body = (%q, %q, %v), want (01dbase, \"\", nil)", base, tip, err)
	}
	if len(*evs) != 1 || (*evs)[0].Level != slog.LevelWarn || (*evs)[0].Attrs["bead"] != "mc-l" {
		t.Errorf("events = %+v, want one warning on mc-l", *evs)
	}
	if _, _, err := chartConflictPins(bd.Bead{ID: "mc-z", Description: "branch: chart/mc-x\nvariant: rebase-conflict"}); err == nil {
		t.Error("a body with no base pin must be an error")
	}
}

// A drained, stamped Chart with no trailer spawns a plain Landing pinned to the
// Chart branch head and never lands, specs or closes (ADR 0145).
func TestReconcileChartSpawnsLandingAtHead(t *testing.T) {
	closed := stubChartSeams(t, func(string) (bd.Bead, error) { return bd.Bead{}, nil })
	landedBase = func(string) (string, error) { return "", git.ErrNotLanded }
	var filed []bd.Successor
	spawnChildIfAbsent = func(_ string, s bd.Successor) (string, bool, error) {
		filed = append(filed, s)
		return "ws-landing", true, nil
	}
	spawnSuccessor = func(string, bd.Successor) (string, bool, error) {
		t.Error("an unlanded Chart must spawn no Spec")
		return "", false, nil
	}
	chart := defoggedChart("open")
	chart.Title = "a chart"
	if err := reconcileChart(stubCtx{&recordingHarness{}}, chart); err != nil {
		t.Fatalf("reconcileChart: %v", err)
	}
	if len(filed) != 1 || filed[0].Kind != contract.KindLanding || filed[0].Title != "a chart" || filed[0].Description != git.CommitPin("chartsha") {
		t.Fatalf("filed = %+v, want one Landing titled like the Chart pinned at chartsha", filed)
	}
	if len(*closed) != 0 {
		t.Errorf("closed %v, want the Chart left open", *closed)
	}
}

// An open Signal blocking the Chart freezes it the same way (ADR 0268).
func TestReconcileChartBlockingSignalHoldsTheLanding(t *testing.T) {
	stubChartSeams(t, func(string) (bd.Bead, error) { return bd.Bead{}, nil })
	landedBase = func(string) (string, error) { return "", git.ErrNotLanded }
	openAnyBlockers = func(string) ([]bd.Bead, error) {
		return []bd.Bead{{ID: "ws-sig", Kind: contract.KindSignal, Status: "open"}}, nil
	}
	spawnChildIfAbsent = func(string, bd.Successor) (string, bool, error) {
		t.Error("an open blocking Signal must hold the Landing")
		return "", false, nil
	}
	if err := reconcileChart(stubCtx{&recordingHarness{}}, defoggedChart("open")); err != nil {
		t.Fatalf("reconcileChart: %v", err)
	}
}

// An open Task blocking the Chart (a human's closed-MR call) holds the next Landing
// until it closes (Spec FR 23).
func TestReconcileChartBlockingTaskHoldsTheLanding(t *testing.T) {
	stubChartSeams(t, func(string) (bd.Bead, error) { return bd.Bead{}, nil })
	landedBase = func(string) (string, error) { return "", git.ErrNotLanded }
	openAnyBlockers = func(string) ([]bd.Bead, error) {
		return []bd.Bead{{ID: "ws-task", Kind: contract.KindTask, Status: "open"}}, nil
	}
	spawnChildIfAbsent = func(string, bd.Successor) (string, bool, error) {
		t.Error("an open blocking Task must hold the Landing")
		return "", false, nil
	}
	if err := reconcileChart(stubCtx{&recordingHarness{}}, defoggedChart("open")); err != nil {
		t.Fatalf("reconcileChart: %v", err)
	}
}

// An open Landing with no trailer: return, no Spec, no second Landing.
func TestReconcileChartOpenLandingWaits(t *testing.T) {
	stubChartSeams(t, func(string) (bd.Bead, error) { return bd.Bead{}, nil })
	landedBase = func(string) (string, error) { return "", git.ErrNotLanded }
	landingsOf = func(string) ([]bd.Bead, error) { return []bd.Bead{{ID: "ws-landing", Status: "open"}}, nil }
	spawnChildIfAbsent = func(string, bd.Successor) (string, bool, error) {
		t.Error("an open Landing must not be respawned")
		return "", false, nil
	}
	spawnSuccessor = func(string, bd.Successor) (string, bool, error) {
		t.Error("an open Landing must spawn no Spec")
		return "", false, nil
	}
	if err := reconcileChart(stubCtx{&recordingHarness{}}, defoggedChart("open")); err != nil {
		t.Fatalf("reconcileChart: %v", err)
	}
}

// The trailer on mainline with a Landing still open closes the Landing landed:
// <sha> first, then spawns the Spec.
func TestReconcileChartTrailerClosesLandingThenSpecs(t *testing.T) {
	stubChartSeams(t, func(string) (bd.Bead, error) {
		return bd.Bead{ID: "ws-spec", Status: "open", Description: "authored"}, nil
	})
	var seq []string
	landingsOf = func(string) ([]bd.Bead, error) {
		return []bd.Bead{{ID: "ws-landing", Status: "open"}, {ID: "ws-old", Status: "closed"}}, nil
	}
	closeBead = func(id, reason string) error { seq = append(seq, "close "+id+" "+reason); return nil }
	spawnSuccessor = func(string, bd.Successor) (string, bool, error) {
		seq = append(seq, "spec")
		return "ws-spec", true, nil
	}
	if err := reconcileChart(stubCtx{&recordingHarness{}}, defoggedChart("open")); err != nil {
		t.Fatalf("reconcileChart: %v", err)
	}
	if want := []string{"close ws-landing landed: landsha", "spec"}; !slices.Equal(seq, want) {
		t.Errorf("seq = %v, want %v", seq, want)
	}
}

// Spec D2, Chart→Spec: once the Spec body is authored, the Chart's
// Ordering edges carry to the Spec immediately before the Chart's close; a carry
// error returns before any close; a closed Chart carries nothing again.
func TestReconcileChartCarriesToSpecBeforeClose(t *testing.T) {
	var seq []string
	stubChartSeams(t, func(string) (bd.Bead, error) {
		return bd.Bead{ID: "ws-spec", Status: "open", Description: "authored"}, nil
	})
	var carryErr error
	carry = func(pred, succ string) ([]string, []bd.CarrySkip, error) {
		seq = append(seq, "carry "+pred+" "+succ)
		return nil, nil, carryErr
	}
	closeOnPublish = func(_ contract.Ctx, id, _ string, _ []bd.Edge) error {
		seq = append(seq, "close "+id)
		return nil
	}
	if err := reconcileChart(stubCtx{&recordingHarness{}}, defoggedChart("open")); err != nil {
		t.Fatalf("reconcileChart: %v", err)
	}
	if want := []string{"carry ws-chart ws-spec", "close ws-chart"}; !slices.Equal(seq, want) {
		t.Errorf("seq = %v, want %v", seq, want)
	}

	seq, carryErr = nil, errors.New("bd down")
	if err := reconcileChart(stubCtx{&recordingHarness{}}, defoggedChart("open")); err == nil {
		t.Error("a carry error must fail the reconcile")
	}
	if want := []string{"carry ws-chart ws-spec"}; !slices.Equal(seq, want) {
		t.Errorf("seq = %v, want %v (no close after a failed carry)", seq, want)
	}

	seq, carryErr = nil, nil
	if err := reconcileChart(stubCtx{&recordingHarness{}}, defoggedChart("closed")); err != nil {
		t.Fatalf("re-entrant reconcileChart: %v", err)
	}
	if len(seq) != 0 {
		t.Errorf("a closed Chart must not carry or close again, got %v", seq)
	}
}

// Spec FR 56, D14: Nami's committing sessions (research, grilling on the
// defog-session skeleton, the Chart's promote-or-stamp) work on their own effort
// branch and name mugicrew integrate before any bd close. Chart-conflict's
// integrate is Go's, after its post-resolve check (TestChartConflictPromptLeavesIntegrateToGo).
func TestCommittingPromptsIntegrateEffortBranch(t *testing.T) {
	for name, got := range map[string]string{
		"research":         researchPrompt("mc-x"),
		"grilling":         grillingPrompt("mc-x"),
		"promote-or-stamp": promoteOrStampPrompt("mc-x"),
		"defog-session.md": promptguard.Docs(t)["defog-session.md"],
	} {
		if !promptguard.IntegratesBeforeClose(got) {
			t.Errorf("%s must name mugicrew integrate before bd close", name)
		}
		if !strings.Contains(got, "effort/") {
			t.Errorf("%s must say the session works on its effort branch", name)
		}
	}
}

// swap points a package seam at v for the rest of the test.
func swap[T any](t *testing.T, seam *T, v T) {
	old := *seam
	t.Cleanup(func() { *seam = old })
	*seam = v
}

// Spec D2, FR11: the Spec a drained Chart spawns is born inside the Chart's
// Initiative, and outside one when the Chart has none.
func TestReconcileChartSpecJoinsTheChartsInitiative(t *testing.T) {
	for _, tc := range []struct{ parent, want string }{{"ws-init", "ws-init"}, {"ws-closed-init", ""}, {"", ""}} {
		t.Run(tc.parent, func(t *testing.T) {
			stubChartSeams(t, func(string) (bd.Bead, error) {
				return bd.Bead{ID: "ws-spec", Status: "deferred", Description: "the authored spec body"}, nil
			})
			swap(t, &initiativeOf, func(b bd.Bead) (string, error) {
				if b.Parent == "ws-init" {
					return "ws-init", nil
				}
				return "", nil
			})
			var spawned bd.Successor
			spawnSuccessor = func(_ string, s bd.Successor) (string, bool, error) { spawned = s; return "ws-spec", true, nil }
			chart := defoggedChart("open")
			chart.Parent = tc.parent
			if err := reconcileChart(stubCtx{&recordingHarness{}}, chart); err != nil {
				t.Fatalf("reconcileChart: %v", err)
			}
			if spawned.Parent != tc.want {
				t.Errorf("Spec Parent = %q, want %q", spawned.Parent, tc.want)
			}
		})
	}
}

func TestSpecSuccessorCopiesTheChartsContext(t *testing.T) {
	s := specSuccessor(bd.Bead{ID: "c", Labels: []string{"kind:chart", "context:apps-x"}})
	if s.Context != "apps-x" {
		t.Errorf("Spec Context = %q, want apps-x", s.Context)
	}
	if s := specSuccessor(bd.Bead{ID: "c", Labels: []string{"context:apps-x", "repo:vendor/x"}}); s.Repo != "vendor/x" {
		t.Errorf("Spec Repo = %q, want vendor/x", s.Repo)
	}
}

// Grilling's focus line comes from its parent Chart; a no-context Workspace never reads it.
func TestLaunchGrillingFocus(t *testing.T) {
	launch := func(label string, noShow bool) string {
		oShow := showBead
		t.Cleanup(func() { showBead = oShow })
		showBead = func(id string) (bd.Bead, error) {
			if noShow && id != "ws-grl" { // the grilling's own body read is the Thread: probe
				t.Fatal("bd show of a container on a no-context Workspace")
			}
			if id == "ws-grl" {
				return bd.Bead{ID: id, Parent: "ws-chart"}, nil
			}
			return bd.Bead{ID: id, Labels: []string{label}}, nil
		}
		h := &stubHarness{}
		if _, err := (Nami{}).Launch(stubCtx{h}, contract.Bead{ID: "ws-grl", Kind: contract.KindGrilling}); err != nil {
			t.Fatal(err)
		}
		return h.inv.Prompt
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
	if got := launch("kind:chart", false); got != plain {
		t.Error("unlabelled container changed the prompt")
	}
	if got := launch("context:a", false); got != plain+"\n\n"+contexts.FocusLine(map[string]string{"a": "apps/a"}, bd.Bead{Labels: []string{"context:a"}}) {
		t.Errorf("labelled prompt is not old + focus line")
	}
}

// ADR 0157: every Nami prompt that gains a focus line is the old render with no
// [context] table, still the old render under an unlabelled container, and the old
// render plus exactly the focus line under a labelled one. With no table, no
// container is read through the bd show seam.
func TestNamiFocusLineByteIdentity(t *testing.T) {
	// ctr serves the container bead; on a no-context Workspace reading it is a failure.
	ctr := func(label string, noShow bool) func(string) (bd.Bead, error) {
		return func(id string) (bd.Bead, error) {
			switch id {
			case "ws-chart":
				if noShow {
					t.Fatal("bd show for a container on a no-context Workspace")
				}
				return bd.Bead{ID: id, Kind: contract.KindChart, Labels: []string{label}}, nil
			case "ws-leaf":
				if noShow {
					t.Fatal("bd show for a leaf on a no-context Workspace")
				}
				return bd.Bead{ID: id, Parent: "ws-chart"}, nil
			}
			return bd.Bead{ID: id, Status: "open", Description: "authored", Labels: []string{label}}, nil
		}
	}
	chartBead := func(label string, defogged bool) bd.Bead {
		c := bd.Bead{ID: "ws-chart", Kind: contract.KindChart, Status: "open", Description: "Destination: ship it", Labels: []string{label}}
		if defogged {
			c.Labels = append(c.Labels, contract.LabelDefogged)
		}
		return c
	}
	runs := map[string]func(label string, noShow bool) string{
		"speccing": func(label string, noShow bool) string {
			authored := false
			h := &recordingHarness{authored: &authored}
			stubChartSeams(t, func(id string) (bd.Bead, error) {
				if id == "ws-spec" && !authored {
					return bd.Bead{ID: id, Status: "open", Description: placeholderSpecBody}, nil
				}
				return ctr(label, noShow)(id)
			})
			if err := reconcileChart(stubCtx{h}, chartBead(label, true)); err != nil {
				t.Fatal(err)
			}
			return h.invs[0].Prompt
		},
		"promote-or-stamp": func(label string, noShow bool) string {
			h := &recordingHarness{}
			stubChartSeams(t, ctr(label, noShow))
			if err := reconcileChart(stubCtx{h}, chartBead(label, false)); err != nil {
				t.Fatal(err)
			}
			return h.invs[0].Prompt
		},
		"charting": func(label string, noShow bool) string {
			swap(t, &showBead, func(id string) (bd.Bead, error) {
				if id == "ws-survey" {
					return bd.Bead{ID: id, Parent: "ws-chart"}, nil
				}
				return ctr(label, noShow)(id)
			})
			h := &stubHarness{}
			if _, err := (Nami{}).Launch(stubCtx{h}, contract.Bead{ID: "ws-survey", Kind: contract.KindSurvey}); err != nil {
				t.Fatal(err)
			}
			return h.inv.Prompt
		},
		"research": func(label string, noShow bool) string {
			swap(t, &showBead, ctr(label, noShow))
			h := &stubHarness{}
			if err := (Nami{}).Run(stubCtx{h}, contract.Bead{ID: "ws-leaf", Kind: contract.KindResearch}); err != nil {
				t.Fatal(err)
			}
			return h.runInv.Prompt
		},
		"chart-conflict": func(label string, noShow bool) string {
			b := openChartConflict()
			chartConflictSeams(t, &b, nil)
			swap(t, &showBead, func(id string) (bd.Bead, error) {
				if id == "ws-cf" {
					return b, nil
				}
				return ctr(label, noShow)(id)
			})
			h := &stubHarness{}
			(Nami{}).Run(stubCtx{h}, contract.Bead{ID: "ws-cf", Kind: contract.KindChartConflict}) // the empty Output errors; the prompt is what is under test
			return h.runInv.Prompt
		},
	}
	repo := func(cfg string) {
		d := t.TempDir()
		exec.Command("git", "init", "-q", d).Run()
		os.MkdirAll(d+"/.mugicrew", 0o755)
		os.WriteFile(d+"/.mugicrew/config.toml", []byte(cfg), 0o644)
		t.Chdir(d)
	}
	focus := "\n\n" + contexts.FocusLine(map[string]string{"a": "apps/a"}, bd.Bead{Labels: []string{"context:a"}})
	for name, run := range runs {
		repo("")
		plain := run("context:a", true)
		repo("[context.a]\npath = \"apps/a\"\n")
		if got := run("kind:chart", false); got != plain {
			t.Errorf("%s: unlabelled container changed the prompt", name)
		}
		if got := run("context:a", false); got != plain+focus {
			t.Errorf("%s: labelled prompt is not old + focus line", name)
		}
	}
}

// A nested Chart's conflict resolves in the child: the session-start head, the
// post-resolve check and the restore all act on the child repo, against the pinned
// base (Spec FR 30). A no-context Workspace reads nothing about the Chart.
func TestRunChartConflictActsOnTheNestedChild(t *testing.T) {
	for _, nested := range []bool{true, false} {
		b := openChartConflict()
		var shown []string
		var checkedIn, restoredIn []git.Repo
		var kid string
		if nested {
			_, kid = contextstest.Workspace(t)
		} else {
			t.Chdir(t.TempDir())
		}
		chart := bd.Bead{ID: "ws-chart", Labels: []string{contextstest.Label}}
		checkErr := &git.ResolveCheckFailed{Assertion: 2, Reason: "ADR-number clash"}
		chartConflictSeams(t, &b, checkErr)
		swap(t, &showBead, func(id string) (bd.Bead, error) {
			shown = append(shown, id)
			if id == "ws-chart" {
				return chart, nil
			}
			return b, nil
		})
		swap(t, &checkResolved, func(r git.Repo, base, tip string) error { checkedIn = append(checkedIn, r); return checkErr })
		swap(t, &restoreResolved, func(r git.Repo, start string) error { restoredIn = append(restoredIn, r); return nil })
		ctx := &publishingCtx{stubCtx: stubCtx{&stubHarness{out: "Resolved: done"}}}
		err := (Nami{}).Run(ctx, contract.Bead{ID: "ws-cf", Kind: contract.KindChartConflict})
		var failed *git.ResolveCheckFailed
		if !errors.As(err, &failed) {
			t.Fatalf("nested=%v: err = %v, want the child's clash", nested, err)
		}
		want := git.Repo("")
		if nested {
			want = git.Repo(kid)
		}
		if !reflect.DeepEqual(checkedIn, []git.Repo{want}) || !reflect.DeepEqual(restoredIn, []git.Repo{want}) {
			t.Errorf("nested=%v: check in %q, restore in %q, want both in %q", nested, checkedIn, restoredIn, want)
		}
		if !nested && slices.Contains(shown, "ws-chart") {
			t.Errorf("a no-context Workspace read the Chart: %q", shown)
		}
	}
}

// The restore lands in the effort tree's child, never the human's main checkout of it.
func TestRunChartConflictRestoresInTheEffortTreeChild(t *testing.T) {
	ws, kid := contextstest.Workspace(t)
	effort := filepath.Join(t.TempDir(), "effort")
	contextstest.Git(t, ws, "worktree", "add", "-q", "-b", "effort/x", effort)
	tree := filepath.Join(effort, "kid")
	contextstest.Git(t, effort, "clone", "-q", kid, tree)
	startKid := contextstest.Git(t, tree, "rev-parse", "HEAD")
	contextstest.Git(t, kid, "commit", "-q", "--allow-empty", "-m", "human work")
	humanHead := contextstest.Git(t, kid, "rev-parse", "HEAD")
	t.Chdir(effort)

	b := openChartConflict()
	chart := bd.Bead{ID: "ws-chart", Labels: []string{contextstest.Label}}
	chartConflictSeams(t, &b, nil)
	swap(t, &showBead, func(id string) (bd.Bead, error) {
		if id == "ws-chart" {
			return chart, nil
		}
		return b, nil
	})
	swap(t, &checkResolved, func(git.Repo, string, string) error { return &git.ResolveCheckFailed{Assertion: 2, Reason: "x"} })
	swap(t, &restoreResolved, git.Repo.RestoreResolved)
	ctx := &publishingCtx{stubCtx: stubCtx{&commitHarness{stubHarness{out: "Resolved: done"}, func() {
		contextstest.Git(t, tree, "commit", "-q", "--allow-empty", "-m", "agent work")
	}}}}
	if err := (Nami{}).Run(ctx, contract.Bead{ID: "ws-cf", Kind: contract.KindChartConflict}); err == nil {
		t.Fatal("want the failed check")
	}
	if got := contextstest.Git(t, tree, "rev-parse", "HEAD"); got != startKid {
		t.Errorf("effort child head = %s, want restored to %s", got, startKid)
	}
	if got := contextstest.Git(t, kid, "rev-parse", "HEAD"); got != humanHead {
		t.Errorf("main checkout of the child moved to %s, want %s", got, humanHead)
	}
}

// An unusable sequences config fails the post-resolve check like any refusal: the
// session restores and the error surfaces.
func TestChartConflictUnusableSequenceConfigRestores(t *testing.T) {
	b := openChartConflict()
	r := chartConflictSeams(t, &b, errors.New("sequences: .mugicrew/config.toml: unusable"))
	err := (Nami{}).Run(&stubCtx{&stubHarness{out: "Resolved: done"}}, contract.Bead{ID: "ws-cf", Kind: contract.KindChartConflict})
	if err == nil || !strings.Contains(err.Error(), "config.toml") || len(r.restores) != 1 || len(r.integrates) != 0 {
		t.Errorf("err %v restores %v integrates %v, want the error, one restore, no integrate", err, r.restores, r.integrates)
	}
}

// commitHarness runs a side effect during the session, as the agent's commits would.
type commitHarness struct {
	stubHarness
	during func()
}

func (h *commitHarness) Run(c contract.Ctx, inv contract.Invocation) (contract.Result, error) {
	h.during()
	return h.stubHarness.Run(c, inv)
}

// Under gitlab a stale-pin trailer found by the Chart's reconcile files the Signal
// before the Landing closes landed (Spec FR 20).
func TestReconcileChartStalePinFilesSignal(t *testing.T) {
	stubChartSeams(t, func(string) (bd.Bead, error) {
		return bd.Bead{ID: "ws-spec", Status: "open", Description: "authored"}, nil
	})
	oStrategy := git.LandStrategyName
	t.Cleanup(func() { git.LandStrategyName = oStrategy })
	git.LandStrategyName = func() (string, error) { return git.LandStrategyGitlab, nil }
	landingsOf = func(string) ([]bd.Bead, error) { return []bd.Bead{{ID: "ws-landing", Status: "open"}}, nil }
	closeBead = func(string, string) error { return nil }
	var origins []string
	swap(t, &spawnSignal, func(origin, _, _ string) (string, bool, error) {
		origins = append(origins, origin)
		return "sig", true, nil
	})
	swap(t, &landedTip, func(string) (string, bool) { return "oldtip", true })
	if err := reconcileChart(stubCtx{&recordingHarness{}}, defoggedChart("open")); err != nil {
		t.Fatalf("reconcileChart: %v", err)
	}
	if len(origins) != 1 || origins[0] != "ws-landing" {
		t.Errorf("origins %v, want one Signal from the Landing", origins)
	}
}

// A nested Chart's land.landed carries context=/repo=/into= (Spec FR 30); a Workspace
// Chart's carries none.
func TestReconcileChartLandedEventNestedAttrs(t *testing.T) {
	for _, nested := range []bool{true, false} {
		stubChartSeams(t, func(string) (bd.Bead, error) {
			return bd.Bead{ID: "ws-spec", Status: "open", Description: "authored"}, nil
		})
		swap(t, &git.LandStrategyName, func() (string, error) { return git.LandStrategyGitlab, nil })
		swap(t, &landingsOf, func(string) ([]bd.Bead, error) { return []bd.Bead{{ID: "ws-landing", Status: "open"}}, nil })
		swap(t, &closeBead, func(string, string) error { return nil })
		swap(t, &landedTip, func(string) (string, bool) { return "", false })
		swap(t, &repoDirOf, func(bd.Bead) (git.Repo, string) {
			if nested {
				return git.Repo("/nonexistent/apps/a"), "apps/a"
			}
			return "", ""
		})
		evs := eventlogtest.Capture(t)
		chart := defoggedChart("open")
		chart.Labels = append(chart.Labels, "context:a")
		if err := reconcileChart(stubCtx{&recordingHarness{}}, chart); err != nil {
			t.Fatalf("reconcileChart: %v", err)
		}
		var got *eventlogtest.Event
		for i, e := range *evs {
			if e.Name == "land.landed" {
				got = &(*evs)[i]
			}
		}
		if got == nil {
			t.Fatalf("nested=%v: no land.landed event in %v", nested, eventlogtest.Names(*evs))
		}
		if nested && (got.Attrs["context"] != "a" || got.Attrs["repo"] != "apps/a" || got.Attrs["into"] != "main") {
			t.Errorf("nested attrs = %v, want context=a repo=apps/a into=main", got.Attrs)
		}
		if _, has := got.Attrs["repo"]; !nested && has {
			t.Errorf("Workspace land.landed carries nested attrs: %v", got.Attrs)
		}
	}
}

// A nested Chart's stale-pin check reads the landed tip in the child, never the
// Workspace's (Spec FR 11).
func TestReconcileChartStalePinReadsTheNestedChild(t *testing.T) {
	for _, nested := range []bool{true, false} {
		if nested {
			contextstest.Workspace(t)
		} else {
			t.Chdir(t.TempDir())
		}
		stubChartSeams(t, func(string) (bd.Bead, error) {
			return bd.Bead{ID: "ws-spec", Status: "open", Description: "authored"}, nil
		})
		swap(t, &git.LandStrategyName, func() (string, error) { return git.LandStrategyGitlab, nil })
		swap(t, &landingsOf, func(string) ([]bd.Bead, error) { return []bd.Bead{{ID: "ws-landing", Status: "open"}}, nil })
		swap(t, &closeBead, func(string, string) error { return nil })
		swap(t, &spawnSignal, func(string, string, string) (string, bool, error) { return "sig", true, nil })
		wsReads := 0
		swap(t, &landedTip, func(string) (string, bool) { wsReads++; return "oldtip", true })
		chart := defoggedChart("open")
		if nested {
			chart.Labels = append(chart.Labels, contextstest.Label)
		}
		if err := reconcileChart(stubCtx{&recordingHarness{}}, chart); err != nil {
			t.Fatalf("nested=%v: reconcileChart: %v", nested, err)
		}
		if (wsReads > 0) == nested {
			t.Errorf("nested=%v: Workspace tip reads = %d", nested, wsReads)
		}
	}
}

// A stale-pin MR merged after its Landing closed obsolete: the newest (closed) Landing
// is flagged (Spec FR 20).
func TestReconcileChartStalePinFlagsClosedLanding(t *testing.T) {
	stubChartSeams(t, func(string) (bd.Bead, error) {
		return bd.Bead{ID: "ws-spec", Status: "open", Description: "authored"}, nil
	})
	oStrategy := git.LandStrategyName
	t.Cleanup(func() { git.LandStrategyName = oStrategy })
	git.LandStrategyName = func() (string, error) { return git.LandStrategyGitlab, nil }
	landingsOf = func(string) ([]bd.Bead, error) {
		return []bd.Bead{{ID: "ws-landing", Status: "closed", CloseReason: "obsolete: branch advanced without landing"}}, nil
	}
	closeBead = func(string, string) error { return nil }
	var origins []string
	swap(t, &spawnSignal, func(origin, _, _ string) (string, bool, error) {
		origins = append(origins, origin)
		return "sig", true, nil
	})
	swap(t, &landedTip, func(string) (string, bool) { return "oldtip", true })
	if err := reconcileChart(stubCtx{&recordingHarness{}}, defoggedChart("open")); err != nil {
		t.Fatalf("reconcileChart: %v", err)
	}
	if len(origins) != 1 || origins[0] != "ws-landing" {
		t.Errorf("origins %v, want one Signal from the closed Landing", origins)
	}
}

// A drained Chart whose trailer another graph landed files the hold Task (FR 41).
func TestReconcileChartForeignLandFilesHold(t *testing.T) {
	stubChartSeams(t, func(string) (bd.Bead, error) { return bd.Bead{}, nil })
	foreign := &git.ForeignLanding{Trailer: "Chart: ws-chart", Ours: "a", Theirs: "b", Sha: "f00d"}
	swap(t, &landedBase, func(string) (string, error) { return "", foreign })
	var keys []string
	swap(t, &bd.SpawnBlockingTask, func(_, key, _, _ string) (string, bool, error) { keys = append(keys, key); return "ws-t", true, nil })
	var fe *git.ForeignLanding
	if err := reconcileChart(stubCtx{&recordingHarness{}}, defoggedChart("open")); !errors.As(err, &fe) {
		t.Fatalf("err = %v, want *ForeignLanding", err)
	}
	if !reflect.DeepEqual(keys, []string{"Foreign: f00d\n"}) {
		t.Errorf("hold keys = %q, want one Foreign: f00d", keys)
	}
}

// A grilling whose body carries a "Thread:" key line launches the reviewer-change
// variant; a quoted "> Thread:" or no line keeps the plain prompt byte-for-byte.
func TestLaunchGrillingThreadVariant(t *testing.T) {
	for name, c := range map[string]struct {
		body    string
		variant bool
	}{
		"key line":   {"Thread: abc123\nwhy", true},
		"later line": {"intro\nThread: abc123", true},
		"quoted":     {"> Thread: abc123", false},
		"none":       {"plain question", false},
	} {
		swap(t, &showBead, func(string) (bd.Bead, error) { return bd.Bead{Description: c.body}, nil })
		h := &stubHarness{}
		if _, err := (Nami{}).Launch(stubCtx{h}, contract.Bead{ID: "ws-grl", Kind: contract.KindGrilling}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		p := h.inv.Prompt
		if got := strings.Contains(p, "A reviewer asked for this change"); got != c.variant {
			t.Errorf("%s: variant=%v want %v", name, got, c.variant)
		}
		if c.variant {
			if !strings.Contains(p, contract.DraftInstruction("ws-grl")) || !strings.Contains(p, "bd close ws-grl") {
				t.Errorf("%s: variant must keep Draft instruction and close:\n%s", name, p)
			}
		} else if !strings.HasSuffix(p, grillingPrompt("ws-grl")) || strings.Contains(p, "reviewer asked") {
			t.Errorf("%s: plain prompt changed:\n%s", name, p)
		}
	}
}

func TestThreadGrillingPromptRendersCleanly(t *testing.T) {
	got := threadGrillingPrompt("mc-y")
	if strings.Contains(got, "%!") {
		t.Errorf("stray format error in prompt:\n%s", got)
	}
	if want := threadGrillingTmpl + "\n" + grillingPrompt("mc-y"); got != want {
		t.Errorf("prompt != preamble + grillingPrompt:\n%s", got)
	}
}

// Spec FR 28: the chart-conflict prompt routes number-clash and the legacy
// adr-number-clash token to the one renumber section.
func TestChartConflictPromptRoutesBothClashTokens(t *testing.T) {
	got := chartConflictPrompt("mc-x")
	for _, want := range []string{"`number-clash` (legacy: `adr-number-clash`", "## Variant: number-clash (or legacy adr-number-clash)", "sequence-renumber.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("chart-conflict prompt lacks %q", want)
		}
	}
}

// stubRenumberChart is a landed gitlab Chart over stubbed mint seams; seq records the
// order of the mint, the Landing close and the Spec spawn.
func stubRenumberChart(t *testing.T, clashes []git.PostLandClash, clashErr error) (seq *[]string, builds *int) {
	t.Helper()
	stubChartSeams(t, func(string) (bd.Bead, error) {
		return bd.Bead{ID: "ws-spec", Status: "open", Description: "authored"}, nil
	})
	swap(t, &git.LandStrategyName, func() (string, error) { return git.LandStrategyGitlab, nil })
	seq, builds = &[]string{}, new(int)
	swap(t, &landingsOf, func(string) ([]bd.Bead, error) { return []bd.Bead{{ID: "ws-landing", Status: "open"}}, nil })
	swap(t, &landedTip, func(string) (string, bool) { return "", false })
	swap(t, &closeBead, func(id, _ string) error { *seq = append(*seq, "close "+id); return nil })
	swap(t, &postLandClashes, func(string) ([]git.PostLandClash, error) { return clashes, clashErr })
	swap(t, &initiativeOf, func(bd.Bead) (string, error) { return "", nil })
	swap(t, &subtreeIDs, func(string) ([]string, error) { return nil, nil })
	swap(t, &cutBranch, func(string, string) (bool, error) { return true, nil })
	swap(t, &buildChildren, func(string) (int, error) { return *builds, nil })
	swap(t, &spawnChildIfAbsent, func(string, bd.Successor) (string, bool, error) { *builds++; return "ws-build", true, nil })
	swap(t, &spawnSuccessor, func(_ string, s bd.Successor) (string, bool, error) {
		if s.Kind == contract.KindCheckpoint {
			*seq = append(*seq, "renumber "+s.Title)
			return "ws-ren", true, nil
		}
		*seq = append(*seq, "spec")
		return "ws-spec", true, nil
	})
	return seq, builds
}

// The later side's landed Chart files one renumber Checkpoint with one Build before its
// Landing closes and its Spec spawns.
func TestReconcileChartLaterSideMintsRenumberFirst(t *testing.T) {
	seq, builds := stubRenumberChart(t, []git.PostLandClash{{Added: "docs/adr/0009-x.md", Others: []string{"docs/adr/0009-y.md"}, Mainline: "tip"}}, nil)
	if err := reconcileChart(stubCtx{&recordingHarness{}}, defoggedChart("open")); err != nil {
		t.Fatalf("reconcileChart: %v", err)
	}
	if want := []string{"renumber Renumber docs/adr/0009-x.md from ws-chart", "close ws-landing", "spec"}; !slices.Equal(*seq, want) || *builds != 1 {
		t.Errorf("seq = %v, builds = %d, want %v and one Build", *seq, *builds, want)
	}
}

// The earlier side (no clashes) files nothing and proceeds as today.
func TestReconcileChartEarlierSideFilesNoRenumber(t *testing.T) {
	seq, builds := stubRenumberChart(t, nil, nil)
	if err := reconcileChart(stubCtx{&recordingHarness{}}, defoggedChart("open")); err != nil {
		t.Fatalf("reconcileChart: %v", err)
	}
	if want := []string{"close ws-landing", "spec"}; !slices.Equal(*seq, want) || *builds != 0 {
		t.Errorf("seq = %v, builds = %d, want %v and no Build", *seq, *builds, want)
	}
}

// A check error leaves the Chart open (no close, no Spec); the re-run, with the
// Checkpoint and Build already filed, adds no second Build.
func TestReconcileChartCheckErrorLeavesChartOpenAndRerunDoesNotDuplicate(t *testing.T) {
	clashes := []git.PostLandClash{{Added: "docs/adr/0009-x.md", Others: []string{"docs/adr/0009-y.md"}, Mainline: "tip"}}
	seq, _ := stubRenumberChart(t, nil, errors.New("git down"))
	if err := reconcileChart(stubCtx{&recordingHarness{}}, defoggedChart("open")); err == nil {
		t.Fatal("want the check error")
	}
	if len(*seq) != 0 {
		t.Errorf("seq = %v, want nothing closed or spawned", *seq)
	}
	swap(t, &postLandClashes, func(string) ([]git.PostLandClash, error) { return clashes, nil })
	for range 2 {
		if err := reconcileChart(stubCtx{&recordingHarness{}}, defoggedChart("open")); err != nil {
			t.Fatalf("reconcileChart: %v", err)
		}
	}
	n := 0
	swap(t, &buildChildren, func(string) (int, error) { return 1, nil })
	swap(t, &spawnChildIfAbsent, func(string, bd.Successor) (string, bool, error) { n++; return "", false, nil })
	if err := reconcileChart(stubCtx{&recordingHarness{}}, defoggedChart("open")); err != nil || n != 0 {
		t.Errorf("re-run with a Build filed: err = %v, spawned %d Builds, want none", err, n)
	}
}

// Under land_strategy=local nothing new runs.
func TestReconcileChartLocalStrategyRunsNoClashCheck(t *testing.T) {
	stubRenumberChart(t, nil, nil)
	swap(t, &git.LandStrategyName, func() (string, error) { return git.LandStrategyLocal, nil })
	swap(t, &postLandClashes, func(string) ([]git.PostLandClash, error) {
		t.Error("the clash check must not run under local")
		return nil, nil
	})
	if err := reconcileChart(stubCtx{&recordingHarness{}}, defoggedChart("open")); err != nil {
		t.Fatalf("reconcileChart: %v", err)
	}
}

// ADR 0262: a requested rebase is a variant of the existing conflict bodies; both
// shapes round-trip through the pins and the variant read, and Route sends them
// to Replace at Tip: like any conflict.
func TestRequestedBodiesRoundTrip(t *testing.T) {
	for name, body := range map[string]string{
		"chart":      ChartConflictBody("mc-x", "b4se", "t1p", ConflictRequested, nil),
		"checkpoint": RequestedCheckpointBody("mc-cp", "b4se", "t1p"),
	} {
		base, _ := git.ParseBase(body)
		tip, _ := git.ParseTip(body)
		if base != "b4se" || tip != "t1p" || BodyVariant(body) != ConflictRequested {
			t.Errorf("%s: base %q tip %q variant %q", name, base, tip, BodyVariant(body))
		}
		if strings.Contains(body, "clashes:") {
			t.Errorf("%s: requested body must carry no clashes:", name)
		}
	}
	if !strings.Contains(ChartConflictBody("mc-x", "b", "t", ConflictRequested, nil), "not a land conflict") {
		t.Error("chart header must name a requested rebase")
	}
	if BodyVariant("Checkpoint-land rebase conflict\nBase: b\nTip: t") != "" {
		t.Error("a body without variant: reads as none")
	}
	if RequestedChartTitle("Foo") != "Foo rebases onto main" || RequestedCheckpointTitle("mc-cp") != "Checkpoint mc-cp rebases onto main" {
		t.Error("titles")
	}
}

func TestChartConflictPromptHasRequestedArm(t *testing.T) {
	if p := chartConflictPrompt("mc-x"); !strings.Contains(p, "## Variant: requested") {
		t.Error("chart-conflict prompt lacks a requested arm")
	}
}

func TestChartConflictBodyRequestedHeaderNamesTheRebase(t *testing.T) {
	body := ChartConflictBody("mc-x", "b4se", "t1p", ConflictRequested, nil)
	if !strings.HasPrefix(body, "Requested rebase") || strings.Contains(body, "reconcileChart re-lands") {
		t.Errorf("requested body = %q, want its own header with no land-conflict text", body)
	}
}

// Under gitlab a landed Chart's open MR on chart/<id> closes before the Landing, with a
// land.mr_closed event; none open makes no close call; a CloseMR error leaves Landing and
// Chart open; local makes no Forge call (Spec FR 3-11).
func TestReconcileChartClosesLandedMR(t *testing.T) {
	for _, tc := range []struct {
		name, strategy string
		open           *forge.MR
		closeErr       error
		wantClosed     []int
		wantOrder      []string // MR close, then the Landing
		wantFind       bool
	}{
		{"open MR", git.LandStrategyGitlab, &forge.MR{IID: 7}, nil, []int{7}, []string{"mr", "bead:ws-landing"}, true},
		{"none open", git.LandStrategyGitlab, nil, nil, nil, []string{"bead:ws-landing"}, true},
		{"close fails", git.LandStrategyGitlab, &forge.MR{IID: 7}, errors.New("boom"), []int{7}, []string{"mr"}, true},
		{"local", git.LandStrategyLocal, &forge.MR{IID: 7}, nil, nil, []string{"bead:ws-landing"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubChartSeams(t, func(string) (bd.Bead, error) {
				return bd.Bead{ID: "ws-spec", Status: "open", Description: "authored"}, nil
			})
			swap(t, &git.LandStrategyName, func() (string, error) { return tc.strategy, nil })
			swap(t, &landingsOf, func(string) ([]bd.Bead, error) { return []bd.Bead{{ID: "ws-landing", Status: "open"}}, nil })
			var order []string
			swap(t, &closeBead, func(id, _ string) error { order = append(order, "bead:"+id); return nil })
			finds, mrClosed := 0, []int(nil)
			swap(t, &forgeFindOpenMR, func(forge.Project, string) (*forge.MR, error) { finds++; return tc.open, nil })
			swap(t, &forgeCloseMR, func(_ forge.Project, iid int) error {
				mrClosed = append(mrClosed, iid)
				order = append(order, "mr")
				return tc.closeErr
			})
			evs := eventlogtest.Capture(t)
			err := reconcileChart(stubCtx{&recordingHarness{}}, defoggedChart("open"))
			if (err != nil) != (tc.closeErr != nil) {
				t.Fatalf("err = %v", err)
			}
			if !reflect.DeepEqual(mrClosed, tc.wantClosed) {
				t.Errorf("closed MRs %v, want %v", mrClosed, tc.wantClosed)
			}
			if (finds > 0) != tc.wantFind {
				t.Errorf("find calls = %d", finds)
			}
			if !slices.Equal(order, tc.wantOrder) {
				t.Errorf("order %v, want %v", order, tc.wantOrder)
			}
			if got := slices.Contains(eventlogtest.Names(*evs), "land.mr_closed"); got != (tc.closeErr == nil && len(tc.wantClosed) > 0) {
				t.Errorf("land.mr_closed = %v", got)
			}
		})
	}
}

// A nested Chart's MR is found and closed on the child's project (Spec FR 8).
func TestReconcileChartClosesLandedMRNested(t *testing.T) {
	stubChartSeams(t, func(string) (bd.Bead, error) {
		return bd.Bead{ID: "ws-spec", Status: "open", Description: "authored"}, nil
	})
	swap(t, &git.LandStrategyName, func() (string, error) { return git.LandStrategyGitlab, nil })
	swap(t, &closeBead, func(string, string) error { return nil })
	swap(t, &repoDirOf, func(bd.Bead) (git.Repo, string) { return git.Repo("/nonexistent/apps/a"), "apps/a" })
	var asked git.Repo
	swap(t, &forgeProject, func(r git.Repo) (forge.Project, error) { asked = r; return forge.Project{ID: "g%2Fchild"}, nil })
	var found, closedOn forge.Project
	swap(t, &forgeFindOpenMR, func(p forge.Project, _ string) (*forge.MR, error) { found = p; return &forge.MR{IID: 3}, nil })
	swap(t, &forgeCloseMR, func(p forge.Project, _ int) error { closedOn = p; return nil })
	evs := eventlogtest.Capture(t)
	chart := defoggedChart("open")
	chart.Labels = append(chart.Labels, "context:a")
	if err := reconcileChart(stubCtx{&recordingHarness{}}, chart); err != nil {
		t.Fatal(err)
	}
	if asked != "/nonexistent/apps/a" || found.ID != "g%2Fchild" || closedOn.ID != "g%2Fchild" {
		t.Errorf("project asked %q found %v closed %v", asked, found, closedOn)
	}
	for _, e := range *evs {
		if e.Name == "land.mr_closed" && e.Attrs["repo"] == "apps/a" {
			return
		}
	}
	t.Errorf("no nested land.mr_closed in %v", eventlogtest.Names(*evs))
}

// A Forge 429 on the landed MR close is held once: nil error, nothing closes, one
// land.rate_limited and no land.landed; the next tick that clears closes the MR, then the Landing.
func TestReconcileChartHoldsRateLimitedMRClose(t *testing.T) {
	closed := stubChartSeams(t, func(string) (bd.Bead, error) {
		return bd.Bead{ID: "ws-spec", Status: "open", Description: "authored"}, nil
	})
	rateLimited.Clear()
	swap(t, &git.LandStrategyName, func() (string, error) { return git.LandStrategyGitlab, nil })
	swap(t, &landingsOf, func(string) ([]bd.Bead, error) { return []bd.Bead{{ID: "ws-landing", Status: "open"}}, nil })
	var order []string
	swap(t, &closeBead, func(id, _ string) error { order = append(order, "bead:"+id); return nil })
	findErr := error(forge.ErrRateLimited)
	swap(t, &forgeFindOpenMR, func(forge.Project, string) (*forge.MR, error) {
		if findErr != nil {
			return nil, fmt.Errorf("read: %w", findErr)
		}
		return &forge.MR{IID: 7}, nil
	})
	swap(t, &forgeCloseMR, func(_ forge.Project, iid int) error { order = append(order, "mr"); return nil })
	evs := eventlogtest.Capture(t)
	for range 2 {
		if err := reconcileChart(stubCtx{&recordingHarness{}}, defoggedChart("open")); err != nil {
			t.Fatalf("err = %v, want the 429 held", err)
		}
	}
	if len(order) != 0 || len(*closed) != 0 || !slices.Equal(eventlogtest.Names(*evs), []string{"land.rate_limited"}) {
		t.Fatalf("order %v closed %v events %v", order, *closed, eventlogtest.Names(*evs))
	}
	findErr = nil
	if err := reconcileChart(stubCtx{&recordingHarness{}}, defoggedChart("open")); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(order, []string{"mr", "bead:ws-landing"}) {
		t.Errorf("order %v, want the MR then the Landing", order)
	}
}
