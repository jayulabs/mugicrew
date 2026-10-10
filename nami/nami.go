// Package nami holds Nami, the charting crewmate. She owns signal/grilling/
// research (and chart-conflict) and stewards the chart (GLOSSARY.md). This slice wires the triage
// front door: Launch composes the charting session for a Signal, and reconcile
// is the idempotent graph-driver behind the accept path (spec §7–§8).
package nami

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/git"
)

// Nami is the concrete charting crewmate. It carries no state; bd access and the
// harness arrive on the ctx (spec §4).
type Nami struct{}

// Nami satisfies the crewmate contract.
var _ contract.Crewmate = Nami{}

// Run is the AFK path the loop dispatches. It branches on kind: a kind:research
// bead takes the agentic, harness-driven path (runResearch, spec §5)
// — a categorically different mechanism from the pure graph-driving every other
// kind falls through to. For those, the loop hands only ID+Kind, so Run re-reads
// the bead's current graph neighborhood, then reconciles it (spec §8.4 —
// Nami.Run(chart) IS reconcile(chart), §8.2).
func (Nami) Run(ctx contract.Ctx, bead contract.Bead) error {
	switch bead.Kind {
	case contract.KindResearch:
		return runResearch(ctx, bead)
	case contract.KindChartConflict:
		return runChartConflict(ctx, bead)
	}
	b, err := bd.Show(bead.ID)
	if err != nil {
		return err
	}
	return Nami{}.Reconcile(ctx, b)
}

// runResearch is Nami's agentic AFK path for a kind:research defog bead (spec §5).
// Unlike Reconcile's pure graph-driving, it reaches the harness:
// it composes a bead-anchored Invocation whose prompt inlines the whole research
// workflow (primary-sources discipline + the repo's docs/research/ landing
// convention — no packaged-skill selector, no allowedTools change),
// runs it headless via ctx.Harness().Run, then closes the bead itself with the
// agent's pointer line as the reason (ADR 0001/0003 — mutate the graph yourself).
// Idempotent roll-forward, mirroring reconcileChart: an errored Run leaves the bead
// open for the next tick; the close is Open()-guarded so a re-run after a completed
// run is a no-op; and the prompt tells the agent to reuse an existing doc so a crash
// between the agent's Write and this close converges on re-dispatch (no duplicate
// doc, no double close).
func runResearch(ctx contract.Ctx, bead contract.Bead) error {
	inv, err := contract.NewInvocation(bead.ID, researchPrompt(bead.ID))
	if err != nil {
		return err
	}
	res, err := ctx.Harness().Run(ctx, inv)
	if err != nil {
		return err // errored Run: leave the bead open, the next tick re-dispatches
	}
	// The agent ends its run with the pointer line "Wrote docs/research/<slug>.md:
	// <summary>"; that IS the close reason. Headless claude carries the
	// full assistant message, so match the LAST non-empty line, not the whole Output —
	// prose before the pointer would otherwise fail the prefix and the bead would NEVER
	// close. Only the pointer means a doc landed to close against —
	// an exit-0 soft failure that printed some other final line (e.g. "Unable to find
	// primary sources") must NOT false-close the bead, so require the prefix (subsumes
	// the empty case) and leave the bead for the next tick otherwise.
	reason := lastNonEmptyLine(res.Output)
	if !strings.HasPrefix(reason, researchDocPointer) {
		return nil
	}
	b, err := bd.Show(bead.ID)
	if err != nil {
		return err
	}
	if b.Open() {
		return bd.Close(bead.ID, reason)
	}
	return nil // already closed by a prior run: no double close
}

// resolvedOutcome prefixes the chart-conflict agent's success line, "Resolved:
// <summary>" (ADR 0080 §1); the summary is the close reason.
const resolvedOutcome = "Resolved:"

// runChartConflict is Nami's AFK arm for a chart-conflict (ADR 0080; hybrid, ADR
// 0019): the headless agent resolves and commits on its effort branch in this
// worktree, Go checks, publishes and does the graph. It reads the Base:/Tip: pins
// (no base is an error), records the session-start HEAD, and runs the session.
// Only a last non-empty line of "Resolved: <summary>" closes, and only once the
// post-resolve check passes against the carried base and Go's mugicrew integrate
// then publishes the rewrite (Spec FR 13–14); a failed check restores the
// session-start tip and fails the effort, publishing nothing. A Chart has no
// review gate, so the close alone re-drains it — no Review. "Escalate: <title>"
// files a grilling (escalateChartConflict, ADR 0079) and runs no check, so
// finished-rebase progress stays. Any other final line (empty, prose) is an
// error, so a confused agent counts toward ADR 0078's failure pause. No "already
// resolved" fast path (ADR 0080 §6): the prompt's roll-forward makes a
// re-dispatch one cheap session.
func runChartConflict(ctx contract.Ctx, bead contract.Bead) error {
	b, err := showBead(bead.ID)
	if err != nil {
		return err
	}
	base, tip, err := chartConflictPins(b)
	if err != nil {
		return err
	}
	start, err := branchHead("HEAD")
	if err != nil {
		return err
	}
	inv, err := contract.NewInvocation(bead.ID, chartConflictPrompt(bead.ID))
	if err != nil {
		return err
	}
	res, err := ctx.Harness().Run(ctx, inv)
	if err != nil {
		return err // errored Run: the bead stays open
	}
	line := lastNonEmptyLine(res.Output)
	if title, ok := strings.CutPrefix(line, escalateOutcome); ok {
		return escalateChartConflict(ctx, b, strings.TrimSpace(title), res.Output)
	}
	summary, ok := strings.CutPrefix(line, resolvedOutcome)
	if !ok {
		return fmt.Errorf("chart-conflict %s: final line %q is not a %s outcome", bead.ID, line, resolvedOutcome)
	}
	if err := checkResolved(base, tip); err != nil {
		return errors.Join(err, restoreResolved(start))
	}
	// Publish only a checked rewrite, and check it before the replay of commits
	// integrated onto the Chart since Tip, which check 3 would flag as stray.
	var out strings.Builder
	switch rc := integrate(&out); rc {
	case 0:
	case 2: // logged integrate.conflict; with no agent left to resolve it, the backstop's re-run pauses it integrate-conflict (ADR 0102 §6)
		return nil
	default:
		return fmt.Errorf("chart-conflict %s: mugicrew integrate exited %d; nothing was closed: %s", bead.ID, rc, strings.TrimSpace(out.String()))
	}
	if b, err = showBead(bead.ID); err != nil {
		return err
	}
	if b.Open() {
		return closeOnPublish(ctx, bead.ID, strings.TrimSpace(summary), nil)
	}
	return nil // already closed by a prior run: no double close
}

// escalateOutcome prefixes the chart-conflict agent's escalation line, "Escalate:
// <title>", which ends its payload block; payloadStart opens that block (ADR 0079).
const (
	escalateOutcome = "Escalate:"
	payloadStart    = "Blocks:"
)

// escalateChartConflict is the Go half of an escalation (ADR 0079 §4–6, Spec
// D6). An open grilling blocker already on the conflict files nothing, so a re-run
// can't stack escalations. Otherwise it files a grilling Draft child of the Chart,
// drafted-by the conflict, titled from the Escalate: line; its body is the payload
// (the last Blocks: line up to the Escalate: line) plus how to answer. It hands
// conflict blocks→grilling to the publish, so the grilling and its gate appear in
// one batch and the conflict leaves bd ready until a human closes the grilling.
// No Blocks: payload or an empty title is a failed effort, not an empty question.
func escalateChartConflict(ctx contract.Ctx, conflict bd.Bead, title, output string) error {
	open, err := openBlockers(conflict.ID, contract.KindGrilling)
	if err != nil || len(open) > 0 {
		return err
	}
	payload := escalationPayload(output)
	if title == "" || payload == "" {
		return fmt.Errorf("chart-conflict %s: %q needs a title and a Blocks: payload", conflict.ID, escalateOutcome+" "+title)
	}
	body := payload + fmt.Sprintf("\n\nAnswer each numbered question in this bead's notes under the same number, "+
		"then close it. Do not commit to the Chart branch or close %s: the AFK resolve applies the answers (ADR 0079).", conflict.ID)
	id, err := spawnChild(conflict.Parent, bd.Successor{Kind: contract.KindGrilling, Title: title, Description: body, DraftedBy: conflict.ID})
	if err != nil {
		return err
	}
	// ponytail: a fresh id can't hold the edge yet; kept per D6 so the publish
	// never carries a line bd would reject (one bad line rolls the batch back).
	var edges []bd.Edge
	if held, err := hasEdge(conflict.ID, id, "blocks"); err != nil {
		return err
	} else if !held {
		edges = append(edges, bd.Edge{From: conflict.ID, To: id, Type: "blocks"})
	}
	return edgesOnPublish(ctx, conflict.ID, edges)
}

// escalationPayload is the agent's payload block: from its last "Blocks:" line up
// to (not including) the final Escalate: line, trimmed; "" when there is none.
func escalationPayload(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i := len(lines) - 2; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), payloadStart) {
			return strings.TrimSpace(strings.Join(lines[i:len(lines)-1], "\n"))
		}
	}
	return ""
}

// Launch composes the HITL command a human opens (spec §7.2, §9.3). Both of
// Nami's launchable kinds are agent-variety — a Signal opens an interactive
// charting session (the Chart is spawned later, inside, on accept — §7.3); a
// Grilling opens an interview session — formatted by the harness. Research and
// chart-conflict are AFK (loop-only, ADR 0080), not launchable.
func (Nami) Launch(ctx contract.Ctx, bead contract.Bead) (contract.LaunchCommand, error) {
	var prompt string
	switch bead.Kind {
	case contract.KindSignal:
		prompt = chartingPrompt(bead.ID)
	case contract.KindGrilling:
		prompt = grillingPrompt(bead.ID)
	default:
		return nil, fmt.Errorf("nami.Launch: kind %q is not a launchable HITL kind", bead.Kind)
	}
	inv, err := contract.NewInvocation(bead.ID, prompt)
	if err != nil {
		return nil, err
	}
	return ctx.Harness().LaunchCommand(inv)
}

// grillingPrompt is the interview workflow a Grilling defog bead's launched agent
// runs (spec §9.3: agent variety). It resolves the bead one way — close it once
// the question is answered and the answer recorded on the bead. Charting-quality
// interviewing is OOS; this fixes only the resolve contract. The session skeleton
// (claim, orient, capture as docs, tend, record and close, guardrail) is the
// defog-session Crew doc, shared with Sanji's prototype; the interview is the
// vendored grilling + domain-modeling Crew docs (ADR 0087, ADR 0088). Offshoot
// Signals are discovered-from the bead that found them (ADR 0006).
//
//go:embed prompts/grilling.md
var grillingTmpl string

func grillingPrompt(grillingID string) string {
	return fmt.Sprintf(grillingTmpl, grillingID, contract.DraftInstruction(grillingID))
}

// researchDocPointer is the prefix of the pointer line the research agent emits as
// its final line ("Wrote docs/research/<slug>.md: <summary>") — the line Nami takes
// as the close reason. Named once so the close guard and the prompt's emit line
// (%[2]s) derive from the same string and can't drift (mirrors wayfinderMapFields).
// Scope: this only keeps the guard and the emit line in lockstep; the prompt's
// landing-convention prose hardcodes "docs/research/" separately, so editing this
// moves the guard + emit line, not the landing path.
const researchDocPointer = "Wrote docs/research/"

// lastNonEmptyLine returns the last non-blank line of s, trimmed. Headless claude
// output carries the whole assistant message, so the pointer line the agent emits
// last sits behind any prose it printed first; the close guard matches on this line,
// not the whole Output.
func lastNonEmptyLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

// researchPrompt is the research workflow an AFK kind:research bead's agent runs
// (spec §5.3: the prompt carries the workflow, embedded in-repo, not
// the packaged skill). It reproduces the research skill's primary-sources discipline
// and the repo's docs/research/ landing convention verbatim, tells
// the agent to reuse an existing doc for this bead so crash-recovery converges, and
// hands the close back to Nami: the agent ends with the pointer line, Nami runs the
// bd close. Pure — asserted through the Nami.Run seam test.
//
//go:embed prompts/research.md
var researchTmpl string

func researchPrompt(researchID string) string {
	return fmt.Sprintf(researchTmpl, researchID, researchDocPointer)
}

// chartConflictPrompt is the headless resolve workflow runChartConflict runs (ADR
// 0030 §3–5, ADR 0079, ADR 0080): one prompt for both variants — rebase onto the
// carried base, or renumber the Chart's clashing ADRs above mainline's max and
// sweep references only in what the Chart owns — committing on the Chart branch
// and ending in one outcome line for Go; the agent never closes the bead. The
// payload (branch, Base:, Tip:, variant) is the bead body. Rationale the prompt
// no longer cites (ADR 0088 §6): the conflict policy (ADR 0011), no re-review on
// re-land (ADR 0009), the dispatched worktree holds the Chart branch (ADR 0071).
// The renumber procedure is the adr-renumber Crew doc, shared with Franky's
// conflict (ADR 0030).
//
//go:embed prompts/chart-conflict.md
var chartConflictTmpl string

func chartConflictPrompt(conflictID string) string {
	return fmt.Sprintf(chartConflictTmpl, conflictID)
}

// wayfinderMapFields aliases contract.WayfinderMapFields — the sections a Chart body
// carries (spec §8.1), now single-sourced in contract so nami's prompts and
// contract.ChartPlaceholderBody compose from one string and can't drift.
const wayfinderMapFields = contract.WayfinderMapFields

// chartingPrompt is the /wayfinder-equivalent workflow the launched agent runs
// (spec §5.3: the prompt carries the workflow; §8.1: charting is done in-session,
// not in Go). It fixes only the accept/reject *contract* this ticket owns —
// charting quality is OOS. The accept verb is `mugicrew reconcile <signal>`
// (§8.2), which idempotently spawns the one Chart; the fast-lane verb is
// `mugicrew reconcile <signal> --spec <body-file>` (ADR 0035 §10), which spawns
// the one Spec and anchors it. Neither closes the Signal: the session's
// clean-exit publish does (ADR 0096).
// Rationale the prompt no longer cites (ADR 0088 §6): a Chart charts toward
// exactly one Spec (ADR 0008, ADR 0013); separable work is an offshoot Signal
// (ADR 0006); the fast lane's one predicate and its standard template (ADR 0035).
// The Chart-body meanings, the Spec template and the interview are Crew docs
// (chart-body, spec-template, grilling, domain-modeling; ADR 0087, ADR 0088).
//
//go:embed prompts/charting.md
var chartingTmpl string

func chartingPrompt(signalID string) string {
	return fmt.Sprintf(chartingTmpl, signalID, wayfinderMapFields, contract.DraftInstruction(signalID))
}

// speccingPrompt is the fork-of-to-spec workflow the AFK speccing session runs
// over a defogged Chart: read the Chart body + every closed defog bead
// + prototype side-refs, then author the one already-spawned Spec's body via bd
// update (no new bead, no triage label). Mirrors chartingTmpl/grillingTmpl. Names the five
// Chart-body sections via the shared wayfinderMapFields so they can't drift.
// Its Spec-body write is serialized by the loop's one container step per container.
// The seven sections are the spec-template Crew doc, shared with charting, and
// the section meanings the chart-body Crew doc (ADR 0088).
//
//go:embed prompts/speccing.md
var speccingTmpl string

func speccingPrompt(chartID string) string {
	return fmt.Sprintf(speccingTmpl, chartID, wayfinderMapFields)
}

// promoteOrStampPrompt is the AFK judge workflow reconcileChart dispatches over a
// drained-but-unstamped, charted Chart (ADR 0021/0022): read the Chart
// body AND its closed children (required — closed children are authoritative,
// Decisions-so-far advisory), bucket-sort every standing Not-yet-specified bullet,
// then PROMOTE remaining fog into fresh defog children, STAMP chart:defogged when
// nothing promotable stands, or ESCALATE one grilling child when a load-bearing
// bullet can't be classified — never blind-stamp. The agent mutates the graph
// itself; the handler is thin (no re-read, no close). Mirrors speccingTmpl; names
// the five Chart-body sections via the shared wayfinderMapFields and the stamp via
// contract.LabelDefogged (the loop's dispatch gate), so neither can drift. The
// section meanings and defog-create rules are the chart-body Crew doc (ADR 0088).
//
//go:embed prompts/promote-or-stamp.md
var promoteOrStampTmpl string

func promoteOrStampPrompt(chartID string) string {
	return fmt.Sprintf(promoteOrStampTmpl, chartID, wayfinderMapFields, contract.DraftInstruction(chartID), contract.LabelDefogged)
}

// Reconcile drives a bead to its conformant next state (spec §8.2), the single
// idempotent primitive behind both the accept path and (later) the
// drained-Chart Run. It takes the already-read bead (its graph neighborhood), so
// no caller reads twice.
func (Nami) Reconcile(ctx contract.Ctx, bead bd.Bead) error {
	switch bead.Kind {
	case contract.KindSignal:
		return reconcileSignal(bead)
	case contract.KindChart:
		return reconcileChart(ctx, bead)
	default:
		return fmt.Errorf("nami.Reconcile: kind %q has no reconcile handler", bead.Kind)
	}
}

// chartBranch is the git branch a Signal's Chart accrues on (ADR 0010 §3:
// chart/<id>), built via git/'s naming helper so the convention lives in one
// place. Pure, so the name reconcileSignal computes is checkable without git.
func chartBranch(chartID string) string {
	return git.BranchName(string(contract.KindChart), chartID)
}

// chartTrailer is the land trailer a drained Chart stamps its squash commit with
// (ADR 0010 §2: "Chart: <id>"), built via git/'s trailer rule so LandedBase greps
// back the exact same string Zoro re-derives. Pure, so the trailer
// reconcileChart computes is checkable without git.
func chartTrailer(chartID string) string {
	return git.TrailerFor(string(contract.KindChart), chartID)
}

// chartSuccessor is the one Chart a Signal spawns on accept (§7.3): a container
// kind:chart bead seeded with contract.ChartPlaceholderBody, created as a Draft by
// the Signal — the charting session's hitl-run publishes it with its defog
// children, so the loop never sees it un-charted or unfogged (ADR 0059 §7). Pure
// and named once so the shape is checkable without a DB (mirrors specSuccessor).
func chartSuccessor(sig bd.Bead) bd.Successor {
	return bd.Successor{
		Kind:        contract.KindChart,
		Container:   true,
		Title:       sig.Title,
		Description: contract.ChartPlaceholderBody,
		DraftedBy:   sig.ID,
	}
}

// reconcileSignal is the accept path (§7.3). The graph picks the leg, never a
// flag (ADR 0035 §10): a Signal with a caused-by Spec successor (seeded by
// SeedSpec) takes the fast lane; otherwise ensure its one Chart successor, cut
// that Chart's git branch, then carry the Signal's Ordering edges to the Chart
// (ADR 0085). It never closes the Signal: the session's clean-exit publish does,
// in the batch that opens the Chart (ADR 0096), so a drop leaves it open and
// re-chartable. Every step is idempotent — SpawnSuccessorIfAbsent,
// branch-if-absent CutBranch, the anchor's LandedBase check, and Carry — so a
// re-run (or a crash mid-way) converges without a second successor, branch or
// anchor (ADR 0006/0009/0010). Both successors present is a mixed-leg graph: a loud
// error before any write (FR18, ADR 0006) — the fast lane must never run beside a Chart.
func reconcileSignal(sig bd.Bead) error {
	specID, fastLane, err := successorOf(sig.ID, contract.KindSpec)
	if err != nil {
		return err
	}
	chartID, charted, err := successorOf(sig.ID, contract.KindChart)
	if err != nil {
		return err
	}
	if fastLane && charted {
		return fmt.Errorf("reconcile: signal %s has both Spec %s and Chart %s; refusing to pick a leg", sig.ID, specID, chartID)
	}
	if fastLane {
		return reconcileFastLane(sig, specID)
	}
	chartID, _, err = spawnSuccessor(sig.ID, chartSuccessor(sig))
	if err != nil {
		return err
	}
	if _, err := cutBranch(chartBranch(chartID), git.Mainline()); err != nil {
		return err
	}
	if sig.Open() {
		return carryOrdering(sig, chartID)
	}
	return nil
}

// carryOrdering carries pred's Ordering edges to succ outside the publish batch
// (ADR 0085), and records the carry. A carry error fails the reconcile, leaving
// pred open for the next reconcile to retry.
func carryOrdering(pred bd.Bead, succ string) error {
	moved, skipped, err := carry(pred.ID, succ)
	return eventlog.RecordCarry(pred, succ, moved, skipped, err)
}

// specTrailer is the direct-Spec anchor trailer ("Spec: <id>", ADR 0014 §2), built
// via git/'s trailer rule so Zoro's LandedBase greps back the same string.
func specTrailer(specID string) string {
	return git.TrailerFor(string(contract.KindSpec), specID)
}

// reconcileFastLane is the fast-lane leg (ADR 0035 §4, §10): write the empty
// "Spec: <id>" anchor on mainline if LandedBase can't find it, then carry the
// Signal's Ordering edges to the Spec (ADR 0085) and log one triage.fastlane with
// the anchor sha — or one triage.failed for a failed read, write, or carry (Spec
// D6). It never closes the Signal: the clean-exit publish does (ADR 0096); a
// dropped session's anchor stays on mainline, inert. The Spec already exists —
// SeedSpec spawned it, and that successor is what routed here.
func reconcileFastLane(sig bd.Bead, specID string) error {
	trailer := specTrailer(specID)
	sha, err := git.AnchorIfAbsent(trailer, landedBase, writeAnchor)
	if err == nil && !sig.Open() {
		return nil
	}
	attrs := append(eventlog.BeadAttrs(sig), "spec", specID)
	if err == nil {
		// ponytail: logged on every run while the Signal is open, so a resumed
		// session logs it again; key it on the eventlog if it must be exactly-once.
		if err = carryOrdering(sig, specID); err == nil {
			attrs = append(attrs, "anchor", sha)
		}
	}
	return eventlog.Record("triage.fastlane", "triage.failed", err, attrs...)
}

// SeedSpec is the whole job of `reconcile <signal> --spec` (ADR 0035 §10): spawn
// the Signal's one caused-by kind:spec leaf successor carrying Nami's body
// verbatim, titled title or exactly the Signal's title. It only seeds — the
// reconcile that follows sees the Spec and takes the fast-lane leg. Idempotent
// via SpawnSuccessorIfAbsent; refuses a non-Signal, an empty body, a Signal that
// already has a Chart (no mixing legs, ADR 0006: one successor), and a closed
// Signal with no Spec (REJECT/DUPLICATE) — one already fast-laned and published is
// a no-op. Mid-session the Signal stays open (ADR 0096), so a re-run after a drop
// seeds a fresh Spec.
func SeedSpec(sig bd.Bead, title, body string) error {
	if sig.Kind != contract.KindSignal {
		return fmt.Errorf("reconcile --spec: %s is kind %q, not a signal", sig.ID, sig.Kind)
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("reconcile --spec: empty Spec body for %s", sig.ID)
	}
	if !sig.Open() {
		if _, seeded, err := successorOf(sig.ID, contract.KindSpec); err != nil || seeded {
			return err
		}
		return fmt.Errorf("reconcile --spec: signal %s is closed; refusing to fast-lane it", sig.ID)
	}
	chartID, charted, err := successorOf(sig.ID, contract.KindChart)
	if err != nil {
		return err
	}
	if charted {
		return fmt.Errorf("reconcile --spec: signal %s already has Chart %s; refusing to fast-lane it", sig.ID, chartID)
	}
	_, _, err = spawnSuccessor(sig.ID, specFastLaneSuccessor(sig, title, body))
	return err
}

// specFastLaneSuccessor is the one Spec a fast-laned Signal spawns (ADR 0035 §10):
// a leaf kind:spec carrying Nami's approved body verbatim, titled title or
// exactly the Signal's title — a Draft by the Signal, so the session's hitl-run
// publishes it only after its Spec: anchor is on mainline (Spec D11 row 11).
// Pure, like chartSuccessor and specSuccessor.
func specFastLaneSuccessor(sig bd.Bead, title, body string) bd.Successor {
	if title == "" {
		title = sig.Title
	}
	return bd.Successor{Kind: contract.KindSpec, Title: title, Description: body, DraftedBy: sig.ID}
}

// placeholderSpecBody is the seed body the emitted Spec carries until the speccing
// session authors the real one (§8.4): a visible marker that also IS the
// idempotency signal — reconcileChart runs the session only while the Spec body
// still equals this, and closes the Chart only once the session has replaced it.
const placeholderSpecBody = "Spec body — to be authored by the speccing session over the defogged Chart."

// specSuccessor is the one Spec a drained Chart spawns (§8.4): a leaf kind:spec
// bead seeded with the placeholder body, a Draft by the Chart. Pure, so the emitted shape (leaf, kind,
// placeholder marker) is checkable without a DB.
func specSuccessor(chart bd.Bead) bd.Successor {
	return bd.Successor{
		Kind:        contract.KindSpec,
		Container:   false,
		Title:       chart.Title,
		Description: placeholderSpecBody,
		DraftedBy:   chart.ID, // published with the Chart's close once the body is authored (Spec D6, FR 21)
	}
}

// Seams over the bd/git legs reconcileChart and reconcileSignal drive, so the
// drained-Chart→Spec flow (land → spawn → speccing session → close) and the
// Signal's Chart/fast-lane legs are testable with a fake harness and no live
// bd/git (ADR 0035). These alias the real package funcs; the nami test
// swaps them — the same package-level var seam loop.go uses for worktreeRun.
var (
	successorOf        = bd.SuccessorOf
	cutBranch          = git.CutBranch
	writeAnchor        = git.Anchor
	openChildren       = bd.OpenChildren
	landBranch         = git.Land
	spawnSuccessor     = bd.SpawnSuccessorIfAbsent
	showBead           = bd.Show
	closeOnPublish     = bd.CloseOnPublish
	landedBase         = git.LandedBase
	spawnChildIfAbsent = bd.SpawnChildIfAbsent
	branchHead         = git.Head
	checkResolved      = git.CheckResolved
	restoreResolved    = git.RestoreResolved
	integrate          = integrateVerb
	openBlockers       = bd.OpenBlockersOfKind
	spawnChild         = bd.SpawnChild
	hasEdge            = bd.HasEdge
	edgesOnPublish     = bd.EdgesOnPublish
	carry              = bd.Carry
)

// integrateVerb runs `mugicrew integrate` in the cwd, the chart-conflict's effort
// tree, as its agent would (by: agent), and returns the verb's exit; one that
// never ran reads as transient. A child process, not session.Integrate: session's
// tests import nami.
func integrateVerb(out io.Writer) int {
	self, err := os.Executable()
	if err != nil {
		return 1
	}
	cmd := exec.Command(self, "integrate")
	cmd.Stdout, cmd.Stderr = out, out
	var exit *exec.ExitError
	if err := cmd.Run(); errors.As(err, &exit) {
		return exit.ExitCode()
	} else if err != nil {
		return 1
	}
	return 0
}

// reconcileChart is the drained-Chart handler (§8.4). It branches three ways on the
// drained Chart's body and label (ADR 0021/0022):
//
//   - body == the un-charted placeholder → return nil. The charting session hasn't
//     written the body (or spawned any defog children) yet, so there is nothing to
//     judge. A Draft Chart never reaches the loop (ADR 0059 §7); this guards a
//     published Chart whose session never wrote the body.
//   - charted but NOT chart:defogged → dispatch the promote-or-stamp judge, then
//     return. Thin dispatch-and-return: no post-dispatch re-read, no close. The
//     agent mutates the graph itself (PROMOTE spawns defog children, STAMP asserts
//     the label, ESCALATE spawns one grilling child) — the stamp-or-spawn IS the
//     state change, so there is nothing for the handler to verify after (ADR 0021).
//     Deliberately unlike the defogged branch below, whose close is gated on the
//     Spec body being authored and so must re-read.
//   - chart:defogged → land + spawn Spec + speccing session + gated close, below.
//
// The chart:defogged branch below lands only with no open children, and each leg is
// idempotent — git-native re-land is a no-op returning the SHA,
// SpawnSuccessorIfAbsent makes a re-run spawn a no-op, the placeholder-body check
// skips a second speccing session, and the open() guard a double close — so exactly
// one Spec is ever emitted and a post-crash roll-forward re-derives the landed base
// without a second commit. Landing runs before spawn so it stamps the "Chart: <id>"
// trailer Zoro re-derives via LandedBase.
//
// The speccing session (speccingPrompt) authors the Spec body via `bd update`;
// reconcileChart re-reads the Spec afterward and closes the Chart only when the
// body no longer equals the placeholder. The Spec is a Draft by the Chart, and the
// close is handed to the publish, so the Spec and the Chart's close become visible
// in one batch (ADR 0059, Spec D6). A session failure or a no-write fails the
// effort, whose tail deletes the Draft Spec; the Chart stays open and the loop
// re-dispatches — the close is gated on the artifact existing, not on the session
// merely having run.
//
// A git.Land rebase conflict (two Charts colliding on GLOSSARY.md / an ADR number)
// is turned into one chart-conflict bead — a MEMBER of the Chart carrying the
// base SHA — instead of propagating (ADR 0011). The open child undrains the Chart
// so the loop stops re-dispatching the same failing land (kills the retry spin);
// idempotent, so a re-run with the conflict still open files no second bead. A
// human resolves + closes it → the Chart re-drains → this handler re-lands cleanly.
// An ADR number the Chart adds that mainline already holds rebases cleanly, so
// Land checks it under the lock against the exact base and returns *ADRClash, which
// files the same chart-conflict, adr-number-clash variant (ADR 0030 §1, ADR 0069).
func reconcileChart(ctx contract.Ctx, chart bd.Bead) error {
	if chart.Description == contract.ChartPlaceholderBody {
		return nil // un-charted gate: nothing to judge until the charting session writes the body (ADR 0021)
	}
	if !chart.HasLabel(contract.LabelDefogged) {
		// Drained but not stamped: dispatch the promote-or-stamp judge anchored to the
		// Chart and return. The agent stamps chart:defogged or spawns fog children
		// itself; the handler stays thin (no re-read, no close — ADR 0021). The Chart
		// stays open, so dispatch releases the caller's claim (ADR 0034) and the next
		// tick re-dispatches into the land branch (after STAMP) or a re-drain-check.
		inv, err := contract.NewInvocation(chart.ID, promoteOrStampPrompt(chart.ID))
		if err != nil {
			return err
		}
		_, err = ctx.Harness().Run(ctx, inv)
		return err
	}
	open, err := openChildren(chart.ID)
	if err != nil {
		return err
	}
	if open > 0 {
		return nil // still draining; the loop pre-checks, but reconcile is self-contained
	}
	sha, err := landBranch(chartBranch(chart.ID), chartTrailer(chart.ID))
	eventlog.RecordLand(chart, chartBranch(chart.ID), sha, err)
	if err != nil {
		var conflict *git.RebaseConflict
		var clash *git.ADRClash
		var base, variant string
		var clashes []string
		switch {
		case errors.As(err, &conflict):
			base, variant = conflict.Base, conflictRebase
		case errors.As(err, &clash):
			base, variant, clashes = clash.Base, conflictADRClash, clash.Clashes
		default:
			return err
		}
		// Land never writes the branch (ADR 0102 §8), so the tip read now is the tip
		// the failed land pinned (Spec D4).
		// refs/heads/ so the read matches Land's own, never a same-named tag.
		tip, err := branchHead("refs/heads/" + chartBranch(chart.ID))
		if err != nil {
			return err
		}
		return fileChartConflict(chart, base, tip, variant, clashes)
	}
	specID, _, err := spawnSuccessor(chart.ID, specSuccessor(chart))
	if err != nil {
		return err
	}
	spec, err := showBead(specID)
	if err != nil {
		return err
	}
	if spec.Description == placeholderSpecBody {
		// Body not yet authored: run the speccing session over the Chart. It writes
		// the Spec body via `bd update` (speccing.md); on failure the Chart stays open.
		inv, err := contract.NewInvocation(specID, speccingPrompt(chart.ID))
		if err != nil {
			return err
		}
		if _, err := ctx.Harness().Run(ctx, inv); err != nil {
			return err
		}
		if spec, err = showBead(specID); err != nil {
			return err
		}
	}
	if spec.Description == placeholderSpecBody {
		// A clean no-write must not publish a placeholder Spec for Zoro to slice: fail
		// the effort so its tail deletes the Draft and the loop re-dispatches (ADR 0059 §6).
		return fmt.Errorf("speccing session left Spec %s on its placeholder body", specID)
	}
	if chart.Open() {
		if err := carryOrdering(chart, specID); err != nil {
			return err
		}
		return closeOnPublish(ctx, chart.ID, "", nil) // body authored: the Chart's terminal artifact exists, close it with the Spec's publish
	}
	return nil
}

// fileChartConflict spawns the one open chart-conflict member that undrains the
// Chart (ADR 0011): idempotent via SpawnChildIfAbsent (open-child guard), so a
// re-run with the conflict still open files no second bead. Returns nil on
// success — the conflict is now a bead, not a propagated error.
func fileChartConflict(chart bd.Bead, base, tip, variant string, clashes []string) error {
	_, _, err := spawnChildIfAbsent(chart.ID, bd.Successor{
		Kind:        contract.KindChartConflict,
		Container:   false,
		Title:       chart.Title + " conflicts with main",
		Description: chartConflictBody(chart.ID, base, tip, variant, clashes),
	})
	return err
}

// The chart-conflict variants name which leg the resolve session runs (ADR 0030
// §3): a textual rebase conflict, or an added ADR number mainline already holds.
const (
	conflictRebase   = "rebase-conflict"
	conflictADRClash = "adr-number-clash"
)

// chartConflictBody is the payload the chart-conflict bead carries for its
// resolve session (ADR 0011 §1, ADR 0030 §3): the branch, the mainline base SHA
// (git.BasePin), the Chart tip the failed land saw (git.TipPin), the variant, and
// (adr-number-clash) the colliding ADR files. Pure, so the payload is checkable
// without git or bd.
func chartConflictBody(chartID, base, tip, variant string, clashes []string) string {
	body := fmt.Sprintf(`Chart-land conflict (ADR 0011, ADR 0030). It is resolved AFK; closing it
re-drains the Chart and reconcileChart re-lands it.

branch: %s
%s
%s
variant: %s`, chartBranch(chartID), git.BasePin(base), git.TipPin(tip), variant)
	if len(clashes) > 0 {
		body += "\nclashes:\n- " + strings.Join(clashes, "\n- ")
	}
	return body
}

// chartConflictPins reads a chart-conflict body's base and tip pins for the AFK
// arm (Spec FR 36–38, D4). Base comes from git.ParseBase, falling back to the
// lowercase "base:" line beads filed before the Tip: change carry; no readable
// base is an error, never a guess. No Tip: (a legacy bead) is an empty tip and a
// logged warning: the post-resolve check skips its check 3.
func chartConflictPins(b bd.Bead) (base, tip string, err error) {
	base, _ = git.ParseBase(b.Description)
	if base == "" {
		base = legacyBase(b.Description)
	}
	if base == "" {
		return "", "", fmt.Errorf("chart-conflict %s: body has no readable base pin", b.ID)
	}
	if tip, _ = git.ParseTip(b.Description); tip == "" {
		slog.Warn("chart_conflict.no_tip", append(eventlog.BeadAttrs(b), "skip", "post-resolve check 3")...)
	}
	return base, tip, nil
}

// legacyBase greps the pre-Tip: body's lowercase "base: <sha>" line.
func legacyBase(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "base:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
