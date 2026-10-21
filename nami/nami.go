// Package nami holds Nami, the charting crewmate. She owns grilling/research
// (and chart-conflict) and stewards the chart (GLOSSARY.md): she maps the fog
// toward a Destination Luffy's triage settled (ADR 0129), and reconcile is the
// idempotent graph-driver behind a drained Chart (spec §8).
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
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/forge"
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
	inv, err := contract.NewInvocation(bead.ID, contexts.FocusUnder(researchPrompt(bead.ID), bead.ID, showBead))
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
	// A nested Chart's branch lives in the child tree <tree>/<dir>: head, check and
	// restore all run there, against the child's base (Spec FR 30).
	// The handle is the effort tree's <cwd>/<dir>, not RepoOfID's main checkout of
	// the child: restore must never reset a human's checkout.
	repo, err := childTreeOfID(b.Parent)
	if err != nil {
		return err
	}
	start, err := headIn(repo, "HEAD")
	if err != nil {
		return err
	}
	inv, err := contract.NewInvocation(bead.ID, contexts.FocusByID(chartConflictPrompt(bead.ID), b.Parent, showBead))
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
	if err := checkResolved(repo, base, tip); err != nil {
		return errors.Join(err, restoreResolved(repo, start))
	}
	// Publish only a checked rewrite, and check it before the replay of commits
	// integrated onto the Chart since Tip, which check 3 would flag as stray.
	var out strings.Builder
	switch rc := integrate(&out); rc {
	case 0:
	case 2: // logged integrate.conflict; with no agent left to resolve it, the backstop's re-run pauses it integrate-conflict (ADR 0102 §6)
		return nil
	case 4: // no cut point survived a rewrite: non-transient, the backstop's re-run pauses it integrate-conflict naming no-cut-point
		return nil
	case 5: // refused off its pinned tip: the backstop's re-run closes it moot, re-pins it once, or pauses it
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
// <title>", which ends its payload block; payloadStart opens that block (ADR 0079 (mugicrew-afk-chart-conflict-escalates…)).
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
		"then close it. Do not commit to the Chart branch or close %s: the AFK resolve applies the answers (ADR 0079 (mugicrew-afk-chart-conflict-escalates…)).", conflict.ID)
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

// Launch composes the HITL command a human opens (spec §9.3). A Grilling opens
// an agent-variety interview session, formatted by the harness; a Survey opens
// the charting session on its parent Chart (Spec mc-r832 D6). A Chart is a
// container, a Signal is Luffy's to triage (ADR 0129), and research and
// chart-conflict are AFK (loop-only, ADR 0080); none is launchable here.
func (Nami) Launch(ctx contract.Ctx, bead contract.Bead) (contract.LaunchCommand, error) {
	var prompt string
	switch bead.Kind {
	case contract.KindGrilling:
		prompt = grillingPrompt(bead.ID)
		if b, err := showBead(bead.ID); err == nil && hasSourceLine(b.Description) {
			prompt = sourceGrillingPrompt(bead.ID)
		}
		prompt = contexts.FocusUnder(prompt, bead.ID, showBead)
	case contract.KindSurvey:
		b, err := showBead(bead.ID)
		if err != nil {
			return nil, err
		}
		if b.Parent == "" {
			return nil, fmt.Errorf("nami.Launch: survey %s has no parent Chart", bead.ID)
		}
		prompt = contexts.FocusByID(chartingPrompt(bead.ID, b.Parent), b.Parent, showBead)
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
	return fmt.Sprintf(grillingTmpl, grillingID, contract.DraftInstruction(grillingID), contract.IntegrateExits())
}

// sourceGrillingTmpl frames a Merge Request review thread's grilling: the
// reviewer left a question or ask on the Chart, so the session answers it in the
// close reason, closes it as not needed, or writes the change (ADR 0199).
//
//go:embed prompts/grilling-source.md
var sourceGrillingTmpl string

// sourceGrillingPrompt is the plain grilling prompt under the Source: framing, so the Draft instruction and the close contract carry over.
func sourceGrillingPrompt(grillingID string) string {
	return sourceGrillingTmpl + "\n" + grillingPrompt(grillingID)
}

// hasSourceLine reports a "Source:" key line in a grilling body; a quoted
// "> Source:" does not start a line with the key, so it never matches.
func hasSourceLine(body string) bool {
	_, ok := git.ParseLine(body, "Source:")
	return ok
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
	return fmt.Sprintf(researchTmpl, researchID, researchDocPointer, contract.IntegrateExits())
}

// chartConflictPrompt is the headless resolve workflow runChartConflict runs (ADR
// 0030 §3–5, ADR 0079, ADR 0080): one prompt for both variants — rebase onto the
// carried base, or renumber the Chart's clashing numbered files above mainline's max and
// sweep references only in what the Chart owns — committing on the Chart branch
// and ending in one outcome line for Go; the agent never closes the bead. The
// payload (branch, Base:, Tip:, variant) is the bead body. Rationale the prompt
// no longer cites (ADR 0088 §6): the conflict policy (ADR 0011), no re-review on
// re-land (ADR 0009), the dispatched worktree holds the Chart branch (ADR 0071).
// The renumber procedure is the sequence-renumber Crew doc, shared with Franky's
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

// chartingPrompt is the charting session a Survey's Launch opens (ADR 0129,
// Spec mc-r832 D7). It claims and drafts as the Survey, but works on the parent
// Chart: read the Destination and Notes Luffy wrote, map Notes /
// Not-yet-specified / Out-of-scope and seed the defog beads as Chart children,
// or push back (ADR 0130: narrow in place, split an offshoot Signal, hand back,
// or reject/duplicate on the human's assent), closing only the Chart. The
// clean-exit publish, Survey close and claim release are hitl-run's, in Go.
// Section meanings and defog-create rules are the chart-body Crew doc.
//
//go:embed prompts/charting.md
var chartingTmpl string

func chartingPrompt(surveyID, chartID string) string {
	return fmt.Sprintf(chartingTmpl, surveyID, wayfinderMapFields, contract.DraftInstruction(surveyID), chartID)
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
	return fmt.Sprintf(promoteOrStampTmpl, chartID, wayfinderMapFields, contract.DraftInstruction(chartID), contract.LabelDefogged, contract.IntegrateExits())
}

// Reconcile drives a bead to its conformant next state (spec §8.2), the single
// idempotent primitive behind both the accept path and (later) the
// drained-Chart Run. It takes the already-read bead (its graph neighborhood), so
// no caller reads twice.
func (Nami) Reconcile(ctx contract.Ctx, bead bd.Bead) error {
	switch bead.Kind {
	case contract.KindChart:
		return reconcileChart(ctx, bead)
	default:
		return fmt.Errorf("nami.Reconcile: kind %q has no reconcile handler", bead.Kind)
	}
}

// chartBranch is the git branch a Chart accrues on (ADR 0010 §3: chart/<id>),
// built via git/'s naming helper so the convention lives in one place. Pure, so
// the name reconcileChart lands is checkable without git.
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

// carryOrdering carries pred's Ordering edges to succ outside the publish batch
// (ADR 0085), and records the carry. A carry error fails the reconcile, leaving
// pred open for the next reconcile to retry.
func carryOrdering(pred bd.Bead, succ string) error {
	moved, skipped, err := carry(pred.ID, succ)
	return eventlog.RecordCarry(pred, succ, moved, skipped, err)
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
		Context:     bd.ContextOf(chart),
		Repo:        bd.RepoStampOf(chart),
	}
}

// Seams over the bd/git legs reconcileChart and the chart-conflict arm drive, so
// the drained-Chart→Spec flow (land → spawn → speccing session → close) is
// testable with a fake harness and no live bd/git. These alias the real package funcs; the nami test
// swaps them — the same package-level var seam loop.go uses for worktreeRun.
var (
	openChildren       = func(id string) (int, error) { return bd.OpenChildren(id, nil) }
	landedBase         = git.LandedBase
	spawnSignal        = bd.SpawnOffshootSignalIfAbsent
	landedTip          = git.LandedTip
	landingsOf         = bd.LandingsOf
	closeBead          = bd.Close
	spawnSuccessor     = bd.SpawnSuccessorIfAbsent
	showBead           = bd.Show
	updateBody         = bd.Update
	closeOnPublish     = bd.CloseOnPublish
	spawnChildIfAbsent = bd.SpawnChildIfAbsent
	branchHead         = git.Head
	isAncestor         = git.IsAncestor
	checkResolved      = git.Repo.CheckResolved
	restoreResolved    = git.Repo.RestoreResolved
	integrate          = integrateVerb
	openBlockers       = bd.OpenBlockersOfKind
	openAnyBlockers    = bd.OpenBlockers
	spawnChild         = bd.SpawnChild
	hasEdge            = bd.HasEdge
	edgesOnPublish     = bd.EdgesOnPublish
	carry              = bd.Carry
	initiativeOf       = bd.InitiativeOf
	postLandClashes    = git.PostLandClashes
	subtreeIDs         = bd.SubtreeIDs
	cutBranch          = git.CutBranch
	buildChildren      = bd.BuildChildren
	repoDirOf          = contexts.RepoDirOf
	forgeFindOpenMR    = forge.FindOpenMR
	forgeCloseMR       = forge.CloseMR
	forgeProject       = func(r git.Repo) (forge.Project, error) { return git.SameHostProject(r, git.Repo.ForgeProject) }
)

// childTreeOfID is the child tree <cwd>/<dir> of the repo = container id, "" when
// it names none (a no-context Workspace reads nothing about the Chart).
func childTreeOfID(id string) (git.Repo, error) {
	if contexts.RepoOfID(id, showBead) == "" {
		return "", nil
	}
	chart, err := showBead(id)
	if err != nil {
		return "", err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return git.Repo(contexts.ChildTree(chart, cwd)), nil
}

// headIn is ref's tip in repo, the Workspace's (through branchHead) when repo is "".
func headIn(repo git.Repo, ref string) (string, error) {
	if repo == "" {
		return branchHead(ref)
	}
	return repo.Head(ref)
}

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
//   - chart:defogged → spawn the Landing; once landed: spawn Spec + speccing session + gated close, below.
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
// The Chart never lands (ADR 0145): drained and stamped, it spawns a Landing pinned
// at the chart/<id> head and returns; Jinbe's Landing lands it, or files the
// chart-conflict member (ADR 0011, ADR 0030) and closes obsolete. The post-land
// steps run only once the "Chart: <id>" trailer is on mainline, after any
// still-open Landing is closed landed: <sha>.
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
		inv, err := contract.NewInvocation(chart.ID, contexts.Focus(promoteOrStampPrompt(chart.ID), chart))
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
	// The Chart never lands (ADR 0145): Jinbe's Landing does. The trailer on mainline
	// is the landed fact; until then a drained, stamped Chart only spawns its Landing.
	landings, err := landingsOf(chart.ID)
	if err != nil {
		return err
	}
	if sha, landed, err := contexts.Landed(chart, chartTrailer(chart.ID), landedBase); err != nil {
		if foreign := (*git.ForeignLanding)(nil); errors.As(err, &foreign) {
			return bd.HoldForeign(chart, foreign) // the hold exists even with no Landing (ADR 0187 §3)
		}
		return err
	} else if landed {
		name, err := git.LandStrategyName()
		if err != nil {
			return err
		}
		if name == git.LandStrategyGitlab { // before the Landing closes (Spec FR 20)
			l, hasLanding := bd.NewestLanding(landings)
			evBead := chart // the event bead, as Jinbe's: the Landing, else the Chart
			r, dir := repoDirOf(chart)
			var nested []any // a nested Landing's attrs, as Jinbe's legs (Spec FR 30)
			if r != "" {
				nested = contexts.NestedAttrs(chart, r, dir)
			}
			if hasLanding {
				evBead = l
				if err := FlagStalePin(l, chart, chartBranch(chart.ID), sha,
					func(sha string) (string, bool) { return contexts.LandedTip(chart, sha, landedTip) },
					func(ref string) (string, error) { return contexts.Head(chart, ref, branchHead) },
					contexts.AncestorIn(contexts.RepoOf(chart), isAncestor), spawnSignal); err != nil {
					return err
				}
			}
			err := closeLandedMR(evBead, chart.ID, r, nested) // before any bead closes, so a failure retries
			if herr := holdRateLimited(evBead, err, nested); err != nil {
				// a held 429 returns nil before anything closes; the next tick retries (ADR 0185 §8)
				return herr
			}
			if hasLanding && l.Open() { // CloseLandings below closes it; logged after the MR close so a retry doesn't re-log it
				eventlog.RecordLandOutcome(l, eventlog.LandLanded, git.RecordedMR(l.Description), append([]any{"sha", sha}, nested...)...)
			}
			if bd.RepoStampOf(chart) == "" { // a nested container skips the post-land renumber (out of scope: no child-scoped clash read)
				if err := mintRenumber(chart, sha); err != nil {
					return err
				}
			}
		}
		if err := bd.CloseLandings(landings, sha, closeBead); err != nil {
			return err
		}
	} else {
		for _, l := range landings {
			if l.Open() {
				return nil // its Landing is landing it
			}
		}
		if held, err := openAnyBlockers(chart.ID); err != nil || len(held) > 0 {
			return err // any open blocker (a human's Task, an offshoot Signal) freezes the Chart (ADR 0268)
		}
		head, err := contexts.Head(chart, "refs/heads/"+chartBranch(chart.ID), branchHead)
		if err != nil {
			return err
		}
		_, _, err = spawnChildIfAbsent(chart.ID, bd.Successor{
			Kind:        contract.KindLanding,
			Title:       chart.Title,
			Description: git.CommitPin(head),
		})
		return err
	}
	succ := specSuccessor(chart)
	if succ.Parent, err = initiativeOf(chart); err != nil {
		return err
	}
	specID, _, err := spawnSuccessor(chart.ID, succ)
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
		inv, err := contract.NewInvocation(specID, contexts.Focus(speccingPrompt(chart.ID), chart))
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

// mintRenumber files the renumber Checkpoint when the landed Chart is the later side
// of an ADR-number clash (ADR 0225); before the Landings close and the Spec spawns,
// so an error leaves the Chart open and the re-run files no duplicate. Nami only
// calls the mint: the Checkpoint routes to Jinbe from then on.
func mintRenumber(chart bd.Bead, sha string) error {
	clashes, err := postLandClashes(sha)
	if err != nil {
		return err
	}
	_, err = MintRenumber(chart, sha, clashes, RenumberSeams{
		SpawnSuccessor: spawnSuccessor, CutBranch: cutBranch, BuildChildren: buildChildren,
		SpawnBuild: spawnChildIfAbsent, InitiativeOf: initiativeOf, SubtreeIDs: subtreeIDs, CausedBy: bd.CausedBy,
	})
	return err
}

// The chart-conflict variants name which leg the resolve session runs (ADR 0030
// §3): a textual rebase conflict, or an added numbered file whose number mainline
// already holds in its own sequence (ADR 0219).
const (
	ConflictRebase      = "rebase-conflict"
	ConflictNumberClash = "number-clash"
	// ConflictRequested is a human-filed rebase onto the pinned base (ADR 0262): no
	// failed land stands behind it, and a clean rebase is a complete result.
	ConflictRequested = "requested"
)

// ChartConflictBody is the payload the chart-conflict bead carries for its
// resolve session (ADR 0011 §1, ADR 0030 §3): the branch, the mainline base SHA
// (git.BasePin), the Chart tip the failed land saw (git.TipPin), the variant, and
// (number-clash) the colliding numbered files, each with its template. Pure, so the payload is checkable
// without git or bd. Exported: Jinbe's Chart-parent Landing files the same bead.
func ChartConflictBody(chartID, base, tip, variant string, clashes []string) string {
	intro := "Chart-land conflict"
	if len(clashes) > 0 {
		intro = "Chart-land numbered-file clash"
	}
	intro += ` (ADR 0011 (mugicrew-chart-conflict-policy), ADR 0219 (mugicrew-the-adr…)). It is resolved AFK; closing it
re-drains the Chart and reconcileChart re-lands it.`
	if variant == ConflictRequested {
		intro = "Requested rebase (ADR 0262 (mugicrew-a-requested-rebase-is-a-human-filed-conflict-variant…)): a human asked for this Chart to be rebased onto mainline;\nit is not a land conflict. It is resolved AFK; closing it re-drains the Chart."
	}
	body := fmt.Sprintf(`%s

branch: %s
%s
%s
variant: %s`, intro, chartBranch(chartID), git.BasePin(base), git.TipPin(tip), variant)
	if len(clashes) > 0 {
		body += "\nclashes:\n- " + strings.Join(clashes, "\n- ")
	}
	return body
}

// RequestedChartTitle and RequestedCheckpointTitle are the titles of a requested
// rebase's bead (ADR 0262).
func RequestedChartTitle(chartTitle string) string { return chartTitle + " rebases onto main" }
func RequestedCheckpointTitle(cpID string) string  { return "Checkpoint " + cpID + " rebases onto main" }

// RequestedCheckpointBody is the body of a Checkpoint's requested-rebase Conflict:
// the same Base:/Tip: pins as a land conflict, variant: requested. Pure.
func RequestedCheckpointBody(cpID, base, tip string) string {
	return "requested rebase: Franky rebases checkpoint/" + cpID + " onto the pinned base, then this bead closes\n" +
		git.BasePin(base) + "\n" + git.TipPin(tip) + "\nvariant: " + ConflictRequested
}

// BodyVariant reads a conflict body's "variant:" line; a body without one (a
// Checkpoint land conflict) is "".
func BodyVariant(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "variant:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
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

// The Forge 429 dedup marker: a body line of the bead the event names, so "logged once"
// holds across ticks' fresh processes (ADR 0185 §8, graph state per ADR 0033).
const (
	rateLimitedPrefix = "Rate limited: "
	rateLimitedLine   = "Rate limited: logged"
)

// holdRateLimited returns err unless it is a Forge 429, which it logs once per bead and
// swallows so the bead stays open and the next tick retries. The first 429 sets the
// bead's Rate limited: line; the first outcome that isn't one drops it. Jinbe's twin
// cannot be shared (Jinbe imports Nami).
func holdRateLimited(b bd.Bead, err error, nested []any) error {
	logged := bd.HasBodyLine(b.Description, rateLimitedLine)
	if !errors.Is(err, forge.ErrRateLimited) {
		if logged {
			writeMarker(b, func(body string) string { return bd.RemoveBodyLine(body, rateLimitedPrefix) })
		}
		return err
	}
	if logged {
		return nil
	}
	attrs := append(eventlog.BeadAttrs(b), "mr", git.RecordedMR(b.Description))
	slog.Warn("land.rate_limited", append(attrs, nested...)...)
	writeMarker(b, func(body string) string { return bd.SetBodyLine(body, rateLimitedPrefix, rateLimitedLine) })
	return nil
}

// writeMarker applies edit to b's current body, not the tick-start copy. A failed read or
// write only warns: the next tick logs again and retries.
func writeMarker(b bd.Bead, edit func(body string) string) {
	if err := bd.EditBody(showBead, updateBody, b.ID, edit); err != nil {
		slog.Warn("land.dedup_write_failed", append(eventlog.BeadAttrs(b), "err", err)...)
	}
}

// closeLandedMR closes whatever MR is still open on a landed Chart's branch (the fetch/push
// race's orphan, ADR 0185 §6), on the Chart's own Forge project; the event rides the Landing
// when there is one. Jinbe does the same for a Checkpoint.
func closeLandedMR(evBead bd.Bead, chartID string, r git.Repo, nested []any) error {
	proj, err := forgeProject(r)
	if err != nil {
		return err
	}
	iid, err := forge.CloseOpenMR(proj, chartBranch(chartID), forgeFindOpenMR, forgeCloseMR)
	if err != nil || iid == 0 {
		return err
	}
	eventlog.RecordLandMRClosed(evBead, iid, "container landed", nested...)
	return nil
}

// StalePinTitle is the title of the stranded-commits Signal FlagStalePin files for cp.
func StalePinTitle(cp bd.Bead) string {
	return cp.Title + ": commits after the landed Tip: are not on mainline"
}

// FlagStalePin files the offshoot Signal (discovered-from the Landing) for a landed
// squash whose Tip: is not the container branch's head (a stale pin merged after the
// branch advanced): the commits after the pin would otherwise be lost (Spec FR 20,
// ADR 0185 §6). A squash without a Tip: line, or a branch already gone, files nothing;
// any other head-read error is returned so the Landing stays open and retries. A
// landed Tip: that is not an ancestor of the head means the branch was rewritten after
// the pin, so tip..head would list every rebased commit as lost: the body points at
// git range-diff instead (Spec FR 39).
func FlagStalePin(l, cp bd.Bead, branch, sha string, landedTip func(string) (string, bool),
	head func(string) (string, error), isAncestor func(a, b string) bool, signal func(origin, title, body string) (string, bool, error)) error {
	tip, ok := landedTip(sha)
	if !ok {
		return nil
	}
	h, err := head("refs/heads/" + branch)
	if git.MissingRef(err) || (err == nil && h == tip) {
		return nil
	}
	if err != nil {
		return err
	}
	var body string
	if isAncestor(tip, h) {
		body = fmt.Sprintf("%s landed with Tip: %s, but branch %s has advanced to %s.\nLanded Tip: %s\nHead: %s\nRange: %s..%s\n"+
			"Fix: the commits in that range are not on mainline. Land them as new work.\n", cp.ID, tip, branch, h, tip, h, tip, h)
	} else {
		body = fmt.Sprintf("%s landed with Tip: %s, but branch %s was rewritten after the landed pin and is now at %s.\nLanded Tip: %s\nHead: %s\n"+
			"Fix: the commits not on mainline are those `git range-diff %s...%s` shows as new or changed. Land them as new work.\n", cp.ID, tip, branch, h, tip, h, tip, h)
	}
	_, _, err = signal(l.ID, StalePinTitle(cp), body)
	return err
}
