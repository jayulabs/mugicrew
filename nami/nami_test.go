package nami

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
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

func (h *stubHarness) ResumeCommand(string) (contract.AgentCommand, error) {
	return contract.AgentCommand{}, nil
}

type stubCtx struct{ h contract.Harness }

func (c stubCtx) Harness() contract.Harness { return c.h }

// Launch(signal) → an agent-variety command whose prompt names the Signal and
// fixes the accept (`mugicrew reconcile`) / reject (`bd close --reason`) contract
// this ticket owns (§7.2–7.3), anchored to the bead via --name (BeadID).
func TestLaunchSignalComposesChartingSession(t *testing.T) {
	h := &stubHarness{}
	lc, err := Nami{}.Launch(stubCtx{h}, contract.Bead{ID: "ws-sig", Kind: contract.KindSignal})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if _, ok := lc.(contract.AgentCommand); !ok {
		t.Fatalf("Launch must return the agent variety, got %T", lc)
	}
	if h.inv.BeadID != "ws-sig" {
		t.Errorf("Invocation must anchor to the Signal id, got %q", h.inv.BeadID)
	}
	p := h.inv.Prompt
	// The Chart-scope guardrail: defog-only, implementation is downstream,
	// and children never inherit the Chart's kind. Pinned so an edit can't silently
	// drop it — that gap once let an impl bead land as a Chart child.
	for _, want := range []string{"ws-sig", "mugicrew reconcile ws-sig", "bd close ws-sig --reason",
		"out of scope", "--no-inherit-labels",
		// The fast lane (ADR 0035 §11): a fourth sibling outcome + its seeding verb.
		"### FAST LANE", "mugicrew reconcile ws-sig --spec ",
		// Scope-tracking titles: charting retitles the Chart in its body write,
		// the fast lane names what is built — no kind prefix.
		`bd update <chartID> --title "<Destination name>"`, `--spec <body-file> --title "<what is built>"`,
		"No kind prefix"} {
		if !strings.Contains(p, want) {
			t.Errorf("charting prompt missing %q", want)
		}
	}
	// A fast-laned Spec takes the standard template so Zoro slices it like any Spec:
	// the one spec-template Crew doc, so the two can't fork.
	_, fastLane, _ := strings.Cut(p, "### FAST LANE")
	fastLane, _, _ = strings.Cut(fastLane, "\n### ")
	if !slices.Contains(promptguard.CrewDocRefs(fastLane), "spec-template.md") {
		t.Errorf("charting FAST LANE must read the spec-template Crew doc:\n%s", fastLane)
	}
	assertSpecSections(t, "spec-template.md", promptguard.Docs(t)["spec-template.md"])
	// mugi-grill is retired: the fast lane replaced its Spec door.
	if strings.Contains(p, "mugi-grill") {
		t.Error("charting prompt still points at the retired mugi-grill")
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

// The Chart reconcileSignal seeds carries the shared placeholder body the prompts
// reference, and is a Draft by the Signal (ADR 0059 §7).
func TestChartSuccessorSeedsPlaceholderBody(t *testing.T) {
	c := chartSuccessor(bd.Bead{ID: "s", Title: "a signal"})
	if c.Kind != contract.KindChart || !c.Container {
		t.Errorf("Chart successor must be a kind:chart container, got kind=%q container=%v", c.Kind, c.Container)
	}
	if c.Title != "a signal" {
		t.Errorf("Chart title = %q, want the Signal's title verbatim", c.Title)
	}
	if c.Description != contract.ChartPlaceholderBody {
		t.Errorf("Chart seed body = %q, want the shared placeholder constant %q", c.Description, contract.ChartPlaceholderBody)
	}
	// ADR 0059 §7: the Chart is a Draft by the Signal until the charting session's
	// hitl-run publishes it with its fog, so promote-or-stamp never judges it early.
	if c.DraftedBy != "s" {
		t.Errorf("Chart successor DraftedBy = %q, want the Signal's id", c.DraftedBy)
	}
}

// The pure part of the Signal retrofit: the Chart's branch name
// reconcileSignal cuts is chart/<chartID>, built from git/'s naming rule (ADR
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
	body := chartConflictBody("mc-x", "deadbeef42", "t1psha", conflictADRClash, []string{"docs/adr/0031-a.md"})
	for _, want := range []string{"branch: chart/mc-x", git.BasePin("deadbeef42"), git.TipPin("t1psha"),
		"variant: " + conflictADRClash, "clashes:\n- docs/adr/0031-a.md"} {
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
	swap(t, &checkResolved, func(base, tip string) error { r.checks = append(r.checks, [2]string{base, tip}); return checkErr })
	swap(t, &restoreResolved, func(start string) error { r.restores = append(r.restores, start); return nil })
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
		Description: chartConflictBody("ws-chart", "b4se", "t1p", conflictRebase, nil)}
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

// Spec D11 rows 2/6: the charting session creates its defog children as Drafts by
// the Signal (the shared block, not a re-phrasing), so its hitl-run publishes the
// Draft Chart and its fog in one batch.
func TestChartingPromptCarriesDraftInstruction(t *testing.T) {
	got := chartingPrompt("ws-sig")
	if !strings.Contains(got, contract.DraftInstruction("ws-sig")) {
		t.Errorf("charting prompt must embed contract.DraftInstruction(signal):\n%s", got)
	}
	if !strings.Contains(got, "-s deferred -l kind:<grilling|research|prototype>,drafted-by:ws-sig") {
		t.Errorf("charting prompt's defog-child create must be a Draft by the Signal:\n%s", got)
	}
}

// Spec D5 (ADR 0096): ACCEPT and FAST LANE say reconcile does not close the
// Signal — mugicrew closes it at the clean-exit publish — and no longer that it does.
func TestChartingPromptSaysSignalClosesAtPublish(t *testing.T) {
	got := strings.Join(strings.Fields(chartingPrompt("ws-sig")), " ")
	if strings.Count(got, "does NOT close the Signal") != 2 || !strings.Contains(got, "mugicrew closes it when this session exits cleanly") {
		t.Errorf("charting prompt must say, on both legs, that mugicrew closes the Signal at publish:\n%s", got)
	}
	if strings.Contains(got, "and closes the Signal") {
		t.Errorf("charting prompt must no longer say reconcile closes the Signal:\n%s", got)
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
	for name, got := range map[string]string{"grilling": grillingPrompt("mc-x"), "research": researchPrompt("mc-x")} {
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
		"charting": {chartingPrompt("mc-x"), []string{"grilling.md", "domain-modeling.md", "chart-body.md", "spec-template.md"}},
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
		"chart-conflict":   {chartConflictPrompt("mc-x"), []string{"adr-renumber.md"}},
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
	for _, got := range []string{chartingPrompt("mc-x"), grillingPrompt("mc-x"), chartConflictPrompt("mc-x"), speccingPrompt("mc-x"), promoteOrStampPrompt("mc-x")} {
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
	for name, p := range map[string]string{"charting": chartingPrompt("mc-x"), "grilling": grillingPrompt("mc-x"), "promote-or-stamp": promoteOrStampPrompt("mc-x")} {
		flat := strings.Join(strings.Fields(p), " ")
		for _, want := range []string{"-l kind:task," + contract.DraftedBy("mc-x"), "never a Chart child", "Carry moves"} {
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

func (h *recordingHarness) ResumeCommand(string) (contract.AgentCommand, error) {
	return contract.AgentCommand{}, nil
}

// stubChartSeams swaps reconcileChart's bd/git legs for inert fakes (land/spawn/
// open-children succeed; showBead is caller-supplied) so the drained-Chart→Spec
// flow runs with no live bd or git. Returns a pointer to the slice of Chart IDs
// closed (by closeOnPublish outside an effort), so tests can assert whether — and how often — the
// Chart was closed. Restores the real funcs on cleanup. Mirrors loop_test.go.
func stubChartSeams(t *testing.T, show func(id string) (bd.Bead, error)) *[]string {
	t.Helper()
	origOpen, origLand, origSpawn, origShow := openChildren, landBranch, spawnSuccessor, showBead
	origSpawnChild, origLanded := spawnChildIfAbsent, landedBase
	t.Cleanup(func() {
		openChildren, landBranch, spawnSuccessor, showBead = origOpen, origLand, origSpawn, origShow
		spawnChildIfAbsent, landedBase = origSpawnChild, origLanded
	})
	closed := &[]string{}
	openChildren = func(string) (int, error) { return 0, nil }
	landBranch = func(_, _ string) (string, error) { return "landsha", nil }
	spawnSuccessor = func(string, bd.Successor) (string, bool, error) { return "ws-spec", true, nil }
	showBead = show
	swap(t, &closeOnPublish, func(ctx contract.Ctx, id, reason string, es []bd.Edge) error {
		if _, ok := ctx.(contract.Publisher); ok {
			return bd.CloseOnPublish(ctx, id, reason, es)
		}
		*closed = append(*closed, id)
		return nil
	})
	landedBase = func(string) (string, error) { return "", errors.New("not landed") }
	swap(t, &carry, func(string, string) ([]string, []bd.CarrySkip, error) { return nil, nil, nil })
	swap(t, &branchHead, func(string) (string, error) { return "chartsha", nil })
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
	landBranch = func(_, _ string) (string, error) { t.Fatal("a Chart with a held child must not land"); return "", nil }

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

// The Chart land records land.done with branch, into and the landed sha.
func TestChartLandIsAnEvent(t *testing.T) {
	evs := eventlogtest.Capture(t)
	stubChartSeams(t, func(string) (bd.Bead, error) {
		return bd.Bead{ID: "ws-spec", Status: "closed", Description: "the authored spec body"}, nil
	})
	reconcileChart(stubCtx{&recordingHarness{}}, defoggedChart("closed"))
	if len(*evs) != 1 || (*evs)[0].Name != "land.done" {
		t.Fatalf("events = %+v, want one land.done", *evs)
	}
	if a := (*evs)[0].Attrs; a["bead"] != "ws-chart" || a["branch"] != "chart/ws-chart" || a["into"] != git.Mainline() || a["sha"] != "landsha" {
		t.Errorf("land.done attrs = %v", a)
	}
}

// ADR 0030 §1, ADR 0069: Land's *ADRClash (a Chart adding an ADR number the locked
// base already holds) files exactly one chart-conflict member naming the
// adr-number-clash variant, the colliding files, the branch and the base, and the
// Chart doesn't land on. A re-run (the conflict still open) files no second.
func TestReconcileChartADRClashFilesOneConflictWithoutLanding(t *testing.T) {
	closed := stubChartSeams(t, func(string) (bd.Bead, error) { return bd.Bead{}, nil })
	landBranch = func(_, _ string) (string, error) {
		return "", &git.ADRClash{Clashes: []string{"apps/mugicrew/docs/adr/0031-chart.md (vs mainline apps/mugicrew/docs/adr/0031-main.md)"}, Base: "basesha"}
	}
	spawnSuccessor = func(string, bd.Successor) (string, bool, error) {
		t.Error("an ADR-number clash must spawn no Spec")
		return "", false, nil
	}
	var filed []bd.Successor
	// openChildren stays 0 (the stub default) so the re-run reaches the spawn's
	// open-child guard rather than short-circuiting on the drain check.
	spawnChildIfAbsent = func(parent string, s bd.Successor) (string, bool, error) {
		if len(filed) > 0 {
			return "ws-cc", false, nil
		}
		filed = append(filed, s)
		return "ws-cc", true, nil
	}

	for range 2 {
		if err := reconcileChart(stubCtx{&recordingHarness{}}, defoggedChart("open")); err != nil {
			t.Fatalf("reconcileChart: %v", err)
		}
	}
	if len(*closed) != 0 {
		t.Errorf("closed %v, want the Chart left open", *closed)
	}
	if len(filed) != 1 {
		t.Fatalf("want exactly one chart-conflict filed, got %d", len(filed))
	}
	if filed[0].Kind != contract.KindChartConflict {
		t.Errorf("filed kind = %q, want chart-conflict", filed[0].Kind)
	}
	for _, want := range []string{conflictADRClash, "chart/ws-chart", git.BasePin("basesha"), git.TipPin("chartsha"), "apps/mugicrew/docs/adr/0031-chart.md", "apps/mugicrew/docs/adr/0031-main.md"} {
		if !strings.Contains(filed[0].Description, want) {
			t.Errorf("conflict body missing %q, got %q", want, filed[0].Description)
		}
	}
}

// A *RebaseConflict files one chart-conflict pinning the land's base and the Chart
// branch tip the failed land saw (Spec D4), and the Chart doesn't land.
func TestReconcileChartRebaseConflictPinsBaseAndTip(t *testing.T) {
	closed := stubChartSeams(t, func(string) (bd.Bead, error) { return bd.Bead{}, nil })
	landBranch = func(_, _ string) (string, error) { return "", &git.RebaseConflict{Base: "basesha"} }
	var filed []bd.Successor
	spawnChildIfAbsent = func(_ string, s bd.Successor) (string, bool, error) {
		filed = append(filed, s)
		return "ws-cc", true, nil
	}
	chart := defoggedChart("open")
	chart.Title = "a chart"
	if err := reconcileChart(stubCtx{&recordingHarness{}}, chart); err != nil {
		t.Fatalf("reconcileChart: %v", err)
	}
	if len(*closed) != 0 || len(filed) != 1 {
		t.Fatalf("closed %v, filed %d; want the Chart open and one conflict", *closed, len(filed))
	}
	if want := "a chart conflicts with main"; filed[0].Title != want {
		t.Errorf("conflict title = %q, want %q (no kind prefix)", filed[0].Title, want)
	}
	base, tip, err := chartConflictPins(bd.Bead{ID: "ws-cc", Description: filed[0].Description})
	if err != nil || base != "basesha" || tip != "chartsha" || !strings.Contains(filed[0].Description, conflictRebase) {
		t.Errorf("pins = (%q, %q, %v), body %q; want basesha/chartsha rebase-conflict", base, tip, err, filed[0].Description)
	}
}

// The AFK arm's pin reader (Spec FR 36–38): a new body yields base and tip; a
// legacy body (lowercase base:, no Tip:) yields its base, an empty tip and a
// check-3-skipped warning; a body with no base is an error, not a guess.
func TestChartConflictPins(t *testing.T) {
	evs := eventlogtest.Capture(t)
	base, tip, err := chartConflictPins(bd.Bead{ID: "mc-n", Description: chartConflictBody("mc-x", "b4se", "t1p", conflictRebase, nil)})
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

// ADR 0069: a *MainDirty from the Chart's land files no chart-conflict (one
// sticky dirty main must not spawn a bead per Chart), leaves the Chart open, and
// is the returned error, recorded on land.failed for the next tick to retry.
func TestReconcileChartMainDirtyFilesNoBead(t *testing.T) {
	evs := eventlogtest.Capture(t)
	closed := stubChartSeams(t, func(string) (bd.Bead, error) { return bd.Bead{}, nil })
	landBranch = func(_, _ string) (string, error) { return "", &git.MainDirty{Path: "/repo"} }
	spawnChildIfAbsent = func(string, bd.Successor) (string, bool, error) {
		t.Error("a dirty main must file no chart-conflict")
		return "", false, nil
	}
	spawnSuccessor = func(string, bd.Successor) (string, bool, error) {
		t.Error("a dirty main must spawn no Spec")
		return "", false, nil
	}
	err := reconcileChart(stubCtx{&recordingHarness{}}, defoggedChart("open"))
	var dirty *git.MainDirty
	if !errors.As(err, &dirty) {
		t.Fatalf("reconcileChart = %v, want *MainDirty", err)
	}
	if len(*closed) != 0 {
		t.Errorf("closed %v, want the Chart left open", *closed)
	}
	if len(*evs) != 1 || (*evs)[0].Name != "land.failed" || !strings.Contains(fmt.Sprint((*evs)[0].Attrs["err"]), "clean main, then re-run") {
		t.Errorf("events = %+v, want one land.failed carrying the *MainDirty", *evs)
	}
}

// No clash → the Chart lands exactly as before, no conflict filed.
func TestReconcileChartNoADRClashLands(t *testing.T) {
	closed := stubChartSeams(t, func(string) (bd.Bead, error) {
		return bd.Bead{ID: "ws-spec", Status: "open", Description: "authored"}, nil
	})
	var landed int
	landBranch = func(_, _ string) (string, error) { landed++; return "landsha", nil }
	spawnChildIfAbsent = func(string, bd.Successor) (string, bool, error) {
		t.Fatal("no clash must file no chart-conflict")
		return "", false, nil
	}
	if err := reconcileChart(stubCtx{&recordingHarness{}}, defoggedChart("open")); err != nil {
		t.Fatalf("reconcileChart: %v", err)
	}
	if landed != 1 || len(*closed) != 1 {
		t.Errorf("want one land and the Chart closed, got landed=%d closed=%v", landed, *closed)
	}
}

// fakeSignalGraph is a stateful stand-in for the bd/git legs reconcileSignal and
// SeedSpec drive (ADR 0035 §10): the Signal's caused-by successors by kind, the
// trailers on mainline, and every write — so leg choice, the refusal to mix legs,
// and crash-rerun idempotence are checkable with no live bd or git. There is no
// close seam: reconcile never closes the Signal (ADR 0096), the publish does.
type fakeSignalGraph struct {
	succ     map[contract.Kind]string
	bodies   map[string]bd.Successor
	trailers map[string]bool
	spawns   int
	anchors  int
	cuts     []string // branches cut, in order
	failAt   string   // "anchor": crash there once
	seq      []string // carry calls, in order: "carry <pred> <succ>"
	carryErr error    // carry: the error bd.Carry returns
}

// drop deletes the Signal's successor Draft, as a session drop does (bd delete).
func (g *fakeSignalGraph) drop(k contract.Kind) { delete(g.succ, k) }

func stubSignalSeams(t *testing.T) *fakeSignalGraph {
	t.Helper()
	// AnchorIfAbsent takes the land lock under the git-common-dir: give it a scratch
	// repo, so the test needs no checkout (Nix sandbox) and never touches the real one.
	t.Chdir(t.TempDir())
	if out, err := exec.Command("git", "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	g := &fakeSignalGraph{succ: map[contract.Kind]string{}, bodies: map[string]bd.Successor{}, trailers: map[string]bool{}}
	swap(t, &successorOf, func(_ string, k contract.Kind) (string, bool, error) { id, ok := g.succ[k]; return id, ok, nil })
	swap(t, &spawnSuccessor, func(_ string, s bd.Successor) (string, bool, error) {
		if id, ok := g.succ[s.Kind]; ok {
			return id, false, nil
		}
		g.spawns++
		id := "ws-" + string(s.Kind)
		if g.spawns > 1 {
			id += fmt.Sprint(g.spawns) // a re-spawn after a drop gets a new id
		}
		g.succ[s.Kind], g.bodies[id] = id, s
		return id, true, nil
	})
	swap(t, &landedBase, func(tr string) (string, error) {
		if g.trailers[tr] {
			return "landedsha", nil
		}
		return "", git.ErrNotLanded
	})
	swap(t, &writeAnchor, func(tr string) (string, error) {
		if g.failAt == "anchor" {
			g.failAt = ""
			return "", errors.New("crash")
		}
		g.anchors++
		g.trailers[tr] = true
		return "anchorsha", nil
	})
	swap(t, &cutBranch, func(b, _ string) (bool, error) {
		if slices.Contains(g.cuts, b) {
			return false, nil
		}
		g.cuts = append(g.cuts, b)
		return true, nil
	})
	swap(t, &carry, func(pred, succ string) ([]string, []bd.CarrySkip, error) {
		g.seq = append(g.seq, "carry "+pred+" "+succ)
		return nil, nil, g.carryErr
	})
	return g
}

// swap points a package seam at v for the rest of the test.
func swap[T any](t *testing.T, seam *T, v T) {
	old := *seam
	t.Cleanup(func() { *seam = old })
	*seam = v
}

func openSignal() bd.Bead {
	return bd.Bead{ID: "ws-sig", Kind: contract.KindSignal, Status: "open", Title: "sharp fix"}
}

// ADR 0035 §10: --spec seeds one kind:spec leaf successor with the body verbatim
// and the defaulted title; reconcile then anchors "Spec: <id>" once, carries,
// emits one triage.fastlane, and never takes the Chart leg. It leaves the Signal
// open: the session's clean-exit publish closes it (ADR 0096).
func TestFastLaneSeedsAnchorsAndLeavesSignalForPublish(t *testing.T) {
	evs := eventlogtest.Capture(t)
	g := stubSignalSeams(t)
	sig := openSignal()
	if err := SeedSpec(sig, "", "the spec body"); err != nil {
		t.Fatalf("SeedSpec: %v", err)
	}
	if err := reconcileSignal(sig); err != nil {
		t.Fatalf("reconcileSignal: %v", err)
	}
	s := g.bodies["ws-spec"]
	if g.spawns != 1 || s.Kind != contract.KindSpec || s.Container || s.Description != "the spec body" || s.Title != "sharp fix" || s.DraftedBy != "ws-sig" {
		t.Errorf("spawns=%d spec=%+v, want one leaf kind:spec Draft by the Signal with the body and defaulted title", g.spawns, s)
	}
	if g.anchors != 1 || !g.trailers["Spec: ws-spec"] {
		t.Errorf("anchors=%d trailers=%v, want one \"Spec: ws-spec\"", g.anchors, g.trailers)
	}
	if want := []string{"carry ws-sig ws-spec"}; !slices.Equal(g.seq, want) {
		t.Errorf("seq = %v, want %v", g.seq, want)
	}
	if _, chart := g.succ[contract.KindChart]; chart || len(g.cuts) != 0 {
		t.Errorf("fast lane must not take the Chart leg (chart=%v cuts=%v)", chart, g.cuts)
	}
	if len(*evs) != 1 || (*evs)[0].Name != "triage.fastlane" || (*evs)[0].Attrs["spec"] != "ws-spec" || (*evs)[0].Attrs["anchor"] != "anchorsha" {
		t.Errorf("events = %+v, want one triage.fastlane naming the spec and anchor", *evs)
	}
}

// A re-run that finds the anchor already landed writes none and logs the found sha.
func TestFastLaneFoundAnchorLogsLandedSha(t *testing.T) {
	evs := eventlogtest.Capture(t)
	g := stubSignalSeams(t)
	g.succ[contract.KindSpec], g.trailers["Spec: ws-spec"] = "ws-spec", true
	if err := reconcileSignal(openSignal()); err != nil {
		t.Fatalf("reconcileSignal: %v", err)
	}
	if g.anchors != 0 || len(*evs) != 1 || (*evs)[0].Attrs["anchor"] != "landedsha" {
		t.Errorf("anchors=%d events=%+v, want no write and one triage.fastlane with anchor=landedsha", g.anchors, *evs)
	}
}

// Spec D6: every fast-lane failure — anchor read, anchor write, carry — logs one
// triage.failed at Error with spec and err.
func TestFastLaneFailureLogsTriageFailed(t *testing.T) {
	for _, crash := range []string{"read", "anchor", "carry"} {
		evs := eventlogtest.Capture(t)
		g := stubSignalSeams(t)
		g.succ[contract.KindSpec] = "ws-spec"
		switch crash {
		case "read":
			landedBase = func(string) (string, error) { return "", errors.New("git broke") }
		case "carry":
			g.carryErr = errors.New("bd down")
		default:
			g.failAt = crash
		}
		if err := reconcileSignal(openSignal()); err == nil {
			t.Fatalf("crash at %s: want an error", crash)
		}
		triage := slices.DeleteFunc(slices.Clone(*evs), func(e eventlogtest.Event) bool { return !strings.HasPrefix(e.Name, "triage.") })
		if len(triage) != 1 || triage[0].Name != "triage.failed" || triage[0].Level != slog.LevelError ||
			triage[0].Attrs["spec"] != "ws-spec" || triage[0].Attrs["err"] == nil {
			t.Errorf("crash at %s: events = %+v, want one triage.failed with spec and err", crash, *evs)
		}
	}
}

// ADR 0069: a dirty main refusing the fast-lane anchor files nothing and carries
// nothing; the *MainDirty surfaces as the error on the one triage.failed.
func TestFastLaneMainDirtyFilesNoBead(t *testing.T) {
	evs := eventlogtest.Capture(t)
	g := stubSignalSeams(t)
	g.succ[contract.KindSpec] = "ws-spec"
	swap(t, &writeAnchor, func(string) (string, error) { return "", &git.MainDirty{Path: "/repo"} })
	err := reconcileSignal(openSignal())
	var dirty *git.MainDirty
	if !errors.As(err, &dirty) {
		t.Fatalf("reconcileSignal = %v, want *MainDirty", err)
	}
	if g.spawns != 0 || len(g.cuts) != 0 || len(g.seq) != 0 {
		t.Errorf("a dirty main must write no bead: spawns=%d cuts=%v seq=%v", g.spawns, g.cuts, g.seq)
	}
	if len(*evs) != 1 || (*evs)[0].Name != "triage.failed" || !strings.Contains(fmt.Sprint((*evs)[0].Attrs["err"]), "clean main, then re-run") {
		t.Errorf("events = %+v, want one triage.failed carrying the *MainDirty", *evs)
	}
}

// An explicit --title wins over the default.
func TestSeedSpecExplicitTitle(t *testing.T) {
	g := stubSignalSeams(t)
	if err := SeedSpec(openSignal(), "custom", "body"); err != nil {
		t.Fatalf("SeedSpec: %v", err)
	}
	if got := g.bodies["ws-spec"].Title; got != "custom" {
		t.Errorf("title = %q, want the explicit one", got)
	}
}

// A crash at any step converges on re-run — with or without --spec: no second
// Spec, no second anchor, no Chart; the carry re-runs idempotently and nothing
// closes the Signal (the publish does, ADR 0096).
func TestFastLaneRerunAfterCrashConverges(t *testing.T) {
	for _, crash := range []string{"anchor", "carry"} {
		for _, reseed := range []bool{true, false} {
			g := stubSignalSeams(t)
			if crash == "carry" {
				g.carryErr = errors.New("bd down")
			} else {
				g.failAt = crash
			}
			sig := openSignal()
			if err := SeedSpec(sig, "", "body"); err != nil {
				t.Fatalf("SeedSpec: %v", err)
			}
			if err := reconcileSignal(sig); err == nil {
				t.Fatalf("crash at %s: want an error", crash)
			}
			g.carryErr = nil
			if reseed {
				if err := SeedSpec(sig, "", "body"); err != nil {
					t.Fatalf("re-SeedSpec: %v", err)
				}
			}
			if err := reconcileSignal(sig); err != nil {
				t.Fatalf("re-run after %s crash: %v", crash, err)
			}
			// A third run once the publish closed the Signal is a pure no-op.
			g.seq = nil
			sig.Status = "closed"
			if err := reconcileSignal(sig); err != nil {
				t.Fatalf("re-run on closed Signal: %v", err)
			}
			if g.spawns != 1 || g.anchors != 1 || len(g.seq) != 0 || len(g.cuts) != 0 {
				t.Errorf("crash=%s reseed=%v: spawns=%d anchors=%d seq=%v cuts=%v, want 1/1/none/none",
					crash, reseed, g.spawns, g.anchors, g.seq, g.cuts)
			}
			if _, chart := g.succ[contract.KindChart]; chart {
				t.Errorf("crash=%s reseed=%v: a re-run spawned a Chart", crash, reseed)
			}
		}
	}
}

// Refusal to mix legs: --spec on a Signal that already has a Chart successor fails
// loudly and writes nothing.
func TestSeedSpecRefusesChartedSignal(t *testing.T) {
	g := stubSignalSeams(t)
	g.succ[contract.KindChart] = "ws-chart"
	if err := SeedSpec(openSignal(), "", "body"); err == nil {
		t.Fatal("--spec on a charted Signal must error")
	}
	if g.spawns != 0 || g.anchors != 0 || len(g.seq) != 0 {
		t.Errorf("refusal must change nothing: spawns=%d anchors=%d seq=%v", g.spawns, g.anchors, g.seq)
	}
}

// The fast-lane Spec (ADR 0035 §10, Spec D2) is a leaf kind:spec carrying
// Nami's body verbatim; the title defaults to exactly the Signal's title.
func TestSpecFastLaneSuccessorShape(t *testing.T) {
	sig := bd.Bead{ID: "s", Title: "a signal"}
	s := specFastLaneSuccessor(sig, "", "the body")
	if s.Kind != contract.KindSpec || s.Container {
		t.Errorf("fast-lane Spec must be a kind:spec leaf, got kind=%q container=%v", s.Kind, s.Container)
	}
	if s.Title != "a signal" || s.Description != "the body" {
		t.Errorf("got title=%q body=%q, want the defaulted title and the body verbatim", s.Title, s.Description)
	}
	if s := specFastLaneSuccessor(sig, "custom", "b"); s.Title != "custom" {
		t.Errorf("explicit title overridden: %q", s.Title)
	}
}

// A closed Signal with no Spec (REJECT/DUPLICATE) never takes the fast lane; one
// already fast-laned and published (its Spec exists) re-runs as a no-op.
func TestSeedSpecClosedSignal(t *testing.T) {
	g := stubSignalSeams(t)
	sig := openSignal()
	sig.Status = "closed"
	if err := SeedSpec(sig, "", "body"); err == nil || g.spawns != 0 {
		t.Errorf("closed un-seeded Signal: err=%v spawns=%d, want an error and no spawn", err, g.spawns)
	}
	g.succ[contract.KindSpec] = "ws-spec"
	if err := SeedSpec(sig, "", "body"); err != nil || g.spawns != 0 {
		t.Errorf("closed fast-laned Signal: err=%v spawns=%d, want a no-op", err, g.spawns)
	}
}

// --spec is valid only on a Signal, and needs a body.
func TestSeedSpecUsageErrors(t *testing.T) {
	g := stubSignalSeams(t)
	if err := SeedSpec(bd.Bead{ID: "mc-c", Kind: contract.KindChart}, "", "body"); err == nil {
		t.Error("--spec on a non-Signal must be a usage error")
	}
	if err := SeedSpec(openSignal(), "", "  \n"); err == nil {
		t.Error("--spec with an empty body must error")
	}
	if g.spawns != 0 {
		t.Errorf("usage errors must spawn nothing, spawned %d", g.spawns)
	}
}

// A bare reconcile on a Signal with no successor takes the Chart leg: one Chart,
// its branch cut, the carry, no anchor — and the Signal left open for the publish.
func TestBareReconcileTakesChartLeg(t *testing.T) {
	g := stubSignalSeams(t)
	if err := reconcileSignal(openSignal()); err != nil {
		t.Fatalf("reconcileSignal: %v", err)
	}
	if _, chart := g.succ[contract.KindChart]; !chart || !slices.Equal(g.cuts, []string{"chart/ws-chart"}) || g.anchors != 0 ||
		!slices.Equal(g.seq, []string{"carry ws-sig ws-chart"}) {
		t.Errorf("chart=%v cuts=%v anchors=%d seq=%v, want the Chart leg", chart, g.cuts, g.anchors, g.seq)
	}
	if _, spec := g.succ[contract.KindSpec]; spec {
		t.Error("the Chart leg must not spawn a Spec")
	}
}

// FR18 / Spec D1: a Signal with both a Spec and a Chart successor is a loud
// error that mutates nothing — no anchor read or write, no spawn, cut, or carry.
func TestReconcileSignalBothSuccessorsErrorsWithoutMutation(t *testing.T) {
	g := stubSignalSeams(t)
	g.succ[contract.KindSpec], g.succ[contract.KindChart] = "ws-spec", "ws-chart"
	landedBase = func(string) (string, error) {
		t.Error("landedBase read with both successors")
		return "", git.ErrNotLanded
	}
	if err := reconcileSignal(openSignal()); err == nil {
		t.Fatal("both successors present must error")
	}
	if g.spawns != 0 || g.anchors != 0 || len(g.cuts) != 0 || len(g.seq) != 0 {
		t.Errorf("spawns=%d anchors=%d cuts=%v seq=%v, want nothing written", g.spawns, g.anchors, g.cuts, g.seq)
	}
}

// A Chart successor present means the Chart leg: no "Spec:" anchor is written.
func TestReconcileSignalChartedWritesNoAnchor(t *testing.T) {
	g := stubSignalSeams(t)
	g.succ[contract.KindChart] = "ws-chart"
	if err := reconcileSignal(openSignal()); err != nil {
		t.Fatalf("reconcileSignal: %v", err)
	}
	if g.anchors != 0 || g.spawns != 0 || len(g.cuts) != 1 {
		t.Errorf("anchors=%d spawns=%d cuts=%v, want the Chart leg with no anchor", g.anchors, g.spawns, g.cuts)
	}
}

// A failed LandedBase read is not absence: the leg errors and writes no anchor,
// so a transient git failure can't stamp a second "Spec: <id>" commit.
func TestFastLaneFailedAnchorReadWritesNothing(t *testing.T) {
	g := stubSignalSeams(t)
	g.succ[contract.KindSpec] = "ws-spec"
	landedBase = func(string) (string, error) { return "", errors.New("git log: boom") }
	if err := reconcileSignal(openSignal()); err == nil {
		t.Fatal("a failed anchor read must error")
	}
	if g.anchors != 0 || len(g.seq) != 0 {
		t.Errorf("anchors=%d seq=%v, want nothing written", g.anchors, g.seq)
	}
}

// Spec D1 (ADR 0096), Signal→Chart and Signal→Spec: reconcile of an open
// Signal carries its Ordering edges to the successor and never closes it — the
// clean-exit publish does; a carry error fails the reconcile; a re-run on the open
// Signal re-carries without a second successor; a closed Signal carries nothing.
func TestReconcileSignalCarriesAndLeavesCloseToPublish(t *testing.T) {
	for _, leg := range []contract.Kind{contract.KindChart, contract.KindSpec} {
		succ := "ws-" + string(leg)
		g := stubSignalSeams(t)
		if leg == contract.KindSpec {
			if err := SeedSpec(openSignal(), "", "body"); err != nil {
				t.Fatalf("SeedSpec: %v", err)
			}
		}
		if err := reconcileSignal(openSignal()); err != nil {
			t.Fatalf("%s leg: reconcileSignal: %v", leg, err)
		}
		if want := []string{"carry ws-sig " + succ}; !slices.Equal(g.seq, want) {
			t.Errorf("%s leg: seq = %v, want %v", leg, g.seq, want)
		}

		g.seq = nil
		if err := reconcileSignal(openSignal()); err != nil {
			t.Fatalf("%s leg: re-run: %v", leg, err)
		}
		if want := []string{"carry ws-sig " + succ}; !slices.Equal(g.seq, want) || g.spawns != 1 || len(g.cuts) > 1 || g.anchors > 1 {
			t.Errorf("%s leg: re-run seq=%v spawns=%d cuts=%v anchors=%d, want a re-carry and no second successor, branch or anchor",
				leg, g.seq, g.spawns, g.cuts, g.anchors)
		}

		g.seq, g.carryErr = nil, errors.New("bd down")
		if err := reconcileSignal(openSignal()); err == nil {
			t.Errorf("%s leg: a carry error must fail the reconcile", leg)
		}

		g.seq, g.carryErr = nil, nil
		sig := openSignal()
		sig.Status = "closed"
		if err := reconcileSignal(sig); err != nil {
			t.Fatalf("%s leg: re-entrant reconcileSignal: %v", leg, err)
		}
		if len(g.seq) != 0 {
			t.Errorf("%s leg: a closed Signal must not carry again, got %v", leg, g.seq)
		}
	}
}

// Spec FR 18–19: a re-run after a drop deleted the Draft, on the still-open
// Signal, spawns a successor with a new id — on the Chart leg with a new
// chart/<newID>, on the fast lane (re-seeded) with a fresh Spec: <newID> anchor.
func TestReconcileSignalRerunAfterDropSpawnsFreshSuccessor(t *testing.T) {
	t.Run("chart", func(t *testing.T) {
		g := stubSignalSeams(t)
		if err := reconcileSignal(openSignal()); err != nil {
			t.Fatalf("reconcileSignal: %v", err)
		}
		g.drop(contract.KindChart)
		if err := reconcileSignal(openSignal()); err != nil {
			t.Fatalf("re-run after drop: %v", err)
		}
		if g.spawns != 2 || !slices.Equal(g.cuts, []string{"chart/ws-chart", "chart/ws-chart2"}) ||
			!slices.Equal(g.seq, []string{"carry ws-sig ws-chart", "carry ws-sig ws-chart2"}) {
			t.Errorf("spawns=%d cuts=%v seq=%v, want a new Chart, branch and carry", g.spawns, g.cuts, g.seq)
		}
	})
	t.Run("fast lane", func(t *testing.T) {
		evs := eventlogtest.Capture(t)
		g := stubSignalSeams(t)
		for range 2 {
			if err := SeedSpec(openSignal(), "", "body"); err != nil {
				t.Fatalf("SeedSpec: %v", err)
			}
			if err := reconcileSignal(openSignal()); err != nil {
				t.Fatalf("reconcileSignal: %v", err)
			}
			g.drop(contract.KindSpec)
		}
		if g.spawns != 2 || g.anchors != 2 || !g.trailers["Spec: ws-spec"] || !g.trailers["Spec: ws-spec2"] {
			t.Errorf("spawns=%d anchors=%d trailers=%v, want a new Spec and a fresh anchor (the old one inert)", g.spawns, g.anchors, g.trailers)
		}
		if len(*evs) != 2 || (*evs)[1].Name != "triage.fastlane" || (*evs)[1].Attrs["spec"] != "ws-spec2" || (*evs)[1].Attrs["anchor"] != "anchorsha" {
			t.Errorf("events = %+v, want a triage.fastlane per run, the last naming the new Spec and anchor", *evs)
		}
	})
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
