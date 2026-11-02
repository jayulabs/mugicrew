// Package luffy holds Luffy, the adjudicator. He owns three HITL kinds; an
// `initiative` launches his cut session (Spec D8). A
// `signal` is intake: Luffy triages it (accept, fast lane, reject, duplicate),
// settling its one Spec's scope and priority, and Reconcile drives the accept and
// fast-lane legs (ADR 0129). An `approval` is the Checkpoint tail leaf a clean
// review hands off: Luffy presents the decision packet so a human makes the one
// deliberate approve call. All are HITL by default: Launch composes the one
// session a human opens. Run routes a Signal to Reconcile and resolves an
// Approval headless (the Workspace auto-approve arm, Usopp's hybrid): an AFK
// session writes the packet, then Go checks it and closes the Approval approved.
// Any other kind errors loudly (mirrors Sanji). Approve is authorize-not-author: approval is pure-prompt —
// the launched agent assembles the packet (diff + clean-review evidence + critique history) and runs the one
// `bd close --reason=approved` on the human's explicit OK, and NEVER touches the
// inherited Commit: pin Jinbe's gate reads (§9, FR32–FR37).
package luffy

import (
	_ "embed"
	"fmt"
	"log/slog"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/registry"
)

// The bd reads Launch makes, as seams so the render tests stay off a real bd.
var (
	show           = bd.Show
	specOf         = bd.SpecOf
	surgeriesOf    = bd.SurgeriesOf
	discoveredFrom = bd.DiscoveredFromSources
	orderEdges     = bd.OrderEdges
	closeOnPublish = bd.CloseOnPublish
	edgesOnPublish = bd.EdgesOnPublish
	runSession     = bd.RunSession
)

// Luffy is the concrete adjudicator crewmate. It carries no state; the harness
// arrives on the ctx (spec §4), mirroring Sanji.
type Luffy struct {
	// Context is the context: label a reconciled Chart carries ("" none), set by
	// `reconcile --context`.
	Context string
}

// Luffy satisfies the crewmate contract.
var _ contract.Crewmate = Luffy{}

// Run is the reconcile verb's re-entry (dispatch.Reconcile runs crew.Run): a
// Signal is re-read with bd.Show, then reconciled, as Nami.Run did. Approval is
// resolved headless by runApproval; every other kind errors loudly, mirroring
// Sanji.Run (FR33). The receiver's Context carries through to the reconciled Chart.
func (l Luffy) Run(ctx contract.Ctx, bead contract.Bead) error {
	if bead.Kind == contract.KindApproval {
		return runApproval(ctx, bead)
	}
	if bead.Kind != contract.KindSignal {
		return fmt.Errorf("luffy.Run: kind %q is neither a Signal nor an Approval", bead.Kind)
	}
	b, err := show(bead.ID)
	if err != nil {
		return err
	}
	return l.Reconcile(ctx, b)
}

// Launch composes the one HITL command a human opens for an approval, a Signal or an Initiative.
// The interface is as thin as Sanji's — no decision-packet Go type, no
// consolidation layer, no post-close step (FR33): one AgentCommand wrapping
// approvalPrompt or triagePrompt. The packet is assembled in-session by the
// agent, not in Go. Any other kind is a loud error, mirroring Sanji's Launch
// default. The Go reads are the Checkpoint's Spec and Surgeries, which the packet
// names (Spec D7).
func (Luffy) Launch(ctx contract.Ctx, bead contract.Bead) (contract.LaunchCommand, error) {
	var prompt string
	switch bead.Kind {
	case contract.KindSignal:
		sig, _ := show(bead.ID) // a failed read only drops the Jira section
		var require []string
		if set := initiativesSetting(); sig.Parent == "" && set.Required {
			inis, err := openInitiatives()
			if err != nil {
				return nil, fmt.Errorf("luffy.Launch: %w", err)
			}
			require = append(require, requireSection(bead.ID, inis, set.ExternalRef))
		}
		require = append(require, memberSectionFor(sig)) // a parented Signal gets no requireSection
		prompt = triagePrompt(bead.ID, sig.ExternalRef, bd.ContextOf(sig), sig.Description, declaredContexts(), adoptionOffers(bead.ID), require...)
	case contract.KindInitiative:
		ini, err := show(bead.ID)
		if err != nil {
			return nil, fmt.Errorf("luffy.Launch: %w", err)
		}
		prompt = cutPrompt(bead.ID, !bd.HasDoneWhen(ini.Description))
	case contract.KindApproval:
		parent, chart, hasSpec, surgeries, err := intentBehind(bead.ID)
		if err != nil {
			return nil, fmt.Errorf("luffy.Launch: %w", err)
		}
		a, _ := show(bead.ID) // a failed read only drops the fallback line
		strategy, err := git.LandStrategyName()
		if err != nil {
			slog.Warn("luffy.launch_strategy_read_failed", "bead", bead.ID, "err", err) // reads as local, as StampModeSnapshots does
			strategy = git.LandStrategyLocal
		}
		prompt, err = withRespin(withCarried(approvalPrompt(bead.ID, chart, hasSpec, surgeries, a.HasLabel(registry.AutoApproveFailedLabel), strategy == git.LandStrategyGitlab), carriedLapsFor(a)), parent)
		if err != nil {
			return nil, fmt.Errorf("luffy.Launch: respin packet: %w", err)
		}
		prompt = contexts.FocusUnder(prompt, bead.ID, show)
	default:
		return nil, fmt.Errorf("luffy.Launch: kind %q is not a launchable HITL kind", bead.Kind)
	}
	inv, err := contract.NewInvocation(bead.ID, prompt)
	if err != nil {
		return nil, err
	}
	return ctx.Harness().LaunchCommand(inv)
}

// cutPrompt is an Initiative's cut session (Spec D8, ADR 0132–0134): Luffy reads
// the drained phase(s) against Done when, then either closes the Initiative on
// the human's "done" or files the next phase (a member chain, ADR 0252: one or
// more member Signals) as Draft children drafted-by the Initiative, marking ## Phases and adding ## Notes. It never parents defog
// to the Initiative nor edits a Chart.
//
//go:embed prompts/cut.md
var cutTmpl string

func cutPrompt(initiativeID string, bodyless bool) string {
	slot := ""
	if bodyless {
		slot = fmt.Sprintf(firstBodyTmpl, initiativeID)
	}
	split := ""
	if len(repoContexts()) > 0 {
		split = "\n\n## Cross-repo split\n\nThis Workspace declares repo contexts, so split a later phase the way phase 1 did. " + crossRepoSplit +
			" These blocks edges join members of one phase; the rule \"no edge between Charts\" is unchanged."
	}
	return fmt.Sprintf(cutTmpl, initiativeID, contract.DraftInstruction(initiativeID), slot, split) + placingMembers(initiativeID)
}

// placingMembers is the cut prompt's appended Go section (Spec D7; cut.md stays
// byte-identical): adopted heads go into an under-way phase, and later: lines
// become fresh members. The adopt verb never writes Phases.
func placingMembers(initiativeID string) string {
	return fmt.Sprintf(`

## Placing members

Do both in the same whole-body rewrite of the Initiative the cut already does.

1. Run `+"`bd list --parent %[1]s --all`"+` and list the members ## Phases does not name. Write each adopted unclosed Chart, Spec or Checkpoint head into an under-way phase line, never a later one: it is live work, so it is never parked.
2. Each `+"`later: <title> (from <signal>)`"+` line on the phase being cut becomes a fresh member Signal, filed per the member form above. Its description cites the closed Signal and carries its gist. Then mark the line with the new member's title. Never reopen the closed Signal.
`, initiativeID)
}

// firstBodyTmpl is the block cut.md's third slot takes when the Initiative is
// bodyless: the body is settled and written before any member is filed.
//
//go:embed prompts/firstbody.md
var firstBodyTmpl string

// approvalPrompt is the bespoke approval workflow, in two modes over one shared
// core (Spec D9). The core (Orient, Assemble the Decision Packet, the HEAD-vs-pin
// advisory, write the doc to its body file) is the same text in both; the HITL
// head and tail are the session a human opens (FR32–FR37): it carries its own
// instructions whole — no backing skill, no outside dependency (FR37) — to write the Decision Packet
// (the diff, the clean-review evidence, the critique history), surface a
// HEAD-vs-pin mismatch advisorily, and run the approve-only close on the human's
// explicit OK. The AFK head and tail are Run's headless session. The shared stance
// (refer-by-title, authorization-not-authorship, bd/git policy) is the Crewmate
// preamble's, not repeated here. Pure — asserted through the Launch seam + render
// tests, mirroring Sanji's prototypePrompt.
//
//go:embed prompts/approval_core.md
var approvalCore string

//go:embed prompts/approval_hitl_head.md
var approvalHITLHead string

//go:embed prompts/approval_hitl_tail.md
var approvalHITLTail string

//go:embed prompts/approval_chart_core.md
var approvalChartCore string

// The HITL tail's Rework section, per parent kind: a Checkpoint's findings become rework
// Critiques, a Chart's become new grilling or research defog beads.
//
//go:embed prompts/approval_rework_checkpoint.md
var approvalReworkCheckpoint string

//go:embed prompts/approval_rework_chart.md
var approvalReworkChart string

//go:embed prompts/approve_local.md
var approveLocal string

//go:embed prompts/approve_gitlab.md
var approveGitlab string

//go:embed prompts/approval_afk_head.md
var approvalAFKHead string

//go:embed prompts/approval_afk_tail.md
var approvalAFKTail string

// approvalAFKWaitHead and approvalAFKWaitTail are the gitlab-without-auto-approve
// variant: the session writes the Packet and no History ending, because the human's
// un-draft of the Draft MR, not the session, approves.
//
//go:embed prompts/approval_afk_wait_head.md
var approvalAFKWaitHead string

//go:embed prompts/approval_afk_wait_tail.md
var approvalAFKWaitTail string

// paneProcedure is the shared sibling-doc-pane wording (Spec mc-dbxb); the
// triage prompt renders the same text. It is inserted as an argument, so its
// percent signs are not Sprintf-escaped.
//
//go:embed prompts/pane.md
var paneProcedure string

// intentBehind is whether the Approval's parent is a Chart (which picks the prompt and
// skips the Checkpoint-only intent read), and otherwise whether the parent Checkpoint
// serves a Spec and its Surgeries in filing order (none when the Approval has no parent).
func intentBehind(approvalID string) (parent bd.Bead, chart, hasSpec bool, surgeries []bd.Bead, err error) {
	approval, err := show(approvalID)
	if err != nil || approval.Parent == "" {
		return bd.Bead{}, false, false, nil, err
	}
	parent, err = show(approval.Parent)
	if err != nil {
		return bd.Bead{}, false, false, nil, err
	}
	if parent.Kind == contract.KindChart {
		return parent, true, false, nil, nil
	}
	if _, hasSpec, err = specOf(approval.Parent); err != nil {
		return bd.Bead{}, false, false, nil, err
	}
	surgeries, err = surgeriesOf(approval.Parent)
	return parent, false, hasSpec, surgeries, err
}

// coreFor is the shared prompt core for the Approval's parent kind.
func coreFor(chart bool) string {
	if chart {
		return approvalChartCore
	}
	return approvalCore
}

// The core's diff-handling clause: a HITL session answers the human from the
// diff, an unattended one only reads it for the packet.
const (
	diffOnHand    = "keep the full diff on hand and answer from it when\n  the human asks about a file or change"
	diffForPacket = "read the full diff for any file or change the\n  packet must name"
)

// The last line the AFK session writes into Approval History, the only mark an
// auto-approval leaves; which one names the layer that switched auto-approve on.
const (
	workspaceHistoryLine = "auto-approved (AFK, Workspace auto-approve on)"
	localHistoryLine     = "auto-approved (AFK, machine-local auto-approve on)"
	offHistoryLine       = "auto-approved (AFK, auto-approve off at run start)"
)

// AutoApprove is the layered [approvals] auto_approve read the AFK arm needs: whether
// it is on and the layer in force ("local" | "config.toml" | "default").
type AutoApprove struct {
	On     bool
	Source string
}

// ReadAutoApprove is the layered auto-approve read (ADR 0353), wired by main to
// loop.ReadAutoApprove (luffy cannot import loop) and faked by tests. Unwired it
// reads off.
var ReadAutoApprove = func() AutoApprove { return AutoApprove{Source: "default"} }

// MRPort is the seam to the container's Merge Request: Open opens it for an Approval (ADR 0185),
// Draft reads it for the re-approval rule (Spec FR 19–24).
type MRPort struct {
	Open  func(container, approval bd.Bead, draft bool) error
	Draft func(container bd.Bead) (open, draft bool, err error)
}

// ApprovalMR is wired by main to jinbe's (luffy and jinbe do not import each other) and faked
// by tests. Unwired, Open errors and Draft reports no open MR.
var ApprovalMR = MRPort{
	Open: func(container, approval bd.Bead, draft bool) error {
		return fmt.Errorf("luffy: ApprovalMR.Open is not wired")
	},
	Draft: func(container bd.Bead) (open, draft bool, err error) { return false, false, nil },
}

// afkHistoryLine picks the closing History line from one read: on from the machine-
// local layer names it; on from any other layer is the Workspace line; off (flipped
// after dispatch, ADR 0346 §4) is the off-at-run-start line.
func afkHistoryLine(a AutoApprove) string {
	switch {
	case !a.On:
		return offHistoryLine
	case a.Source == "local":
		return localHistoryLine
	}
	return workspaceHistoryLine
}

// approvedOutcome starts the AFK session's last line.
const approvedOutcome = "Approved:"

// surgerySlot is the core's Surgery line (Spec D7): on a surgery Checkpoint the
// Surgeries open the packet, on a Spec'd one they follow the Spec under a
// heading. No Surgeries leaves the packet unchanged.
func surgerySlot(hasSpec bool, surgeries []bd.Bead) string {
	if len(surgeries) == 0 {
		return ""
	}
	var lines strings.Builder
	for _, s := range surgeries {
		fmt.Fprintf(&lines, "\n    Surgery: %s — %s", s.ID, s.Title)
	}
	if hasSpec {
		return " After the Spec, list the Surgeries on this Checkpoint under the heading \"Surgeries on this Checkpoint:\", one line each:\n" + lines.String() + "\n\nso an approver who did not dictate the code reads its intent."
	}
	return " On this surgery Checkpoint, open the packet with these lines, one per Surgery:\n" + lines.String() + "\n\nso an approver who did not dictate the code reads its intent."
}

// fellBackLine is the history line a HITL session adds when the Approval fell
// back from AFK (Spec D9, ADR 0341 §3).
const fellBackLine = "auto-approve failed (AFK, 3 attempts); resolved HITL"

// approvalPrompt renders the HITL packet session; fellBack (the Approval carries
// auto-approve:failed) fills the conditional Approval History slot.
func approvalPrompt(approvalID string, chart, hasSpec bool, surgeries []bd.Bead, fellBack, gitlab bool) string {
	history := ""
	if fellBack {
		history = "\n\nThis Approval fell back from AFK auto-approve. Before the approved ending, add the line \"" + fellBackLine + "\" to the packet's Approval History."
	}
	rework := approvalReworkCheckpoint
	if chart {
		rework = approvalReworkChart
	}
	return fmt.Sprintf(approvalHITLHead+coreFor(chart)+approvalHITLTail, approvalID, surgerySlot(hasSpec, surgeries), "\n\n"+paneProcedure, history, diffOnHand, approveBullet(approvalID, gitlab), fmt.Sprintf(rework, approvalID))
}

// approveBullet is the prompt's approve step: local closes the Approval on the human's OK;
// under gitlab the OK only confirms the written Packet, because the session's clean exit
// opens the Draft MR and the human's un-draft there approves.
func approveBullet(approvalID string, gitlab bool) string {
	if gitlab {
		return approveGitlab
	}
	return fmt.Sprintf(approveLocal, approvalID)
}

// OpenFallbackMR is the clean-exit step of a gitlab auto-approve:failed Approval's HITL
// session (Spec FR 27): once the Packet is written it opens or reuses the container's MR
// as a GitLab Draft, records MR:/MR link:, and leaves the Approval open for the MR Watch.
// Anything else (another strategy or kind, an Approval without the fallback marker, one
// already closed, or a body with no Packet beyond the pin) does nothing.
func OpenFallbackMR(approvalID string) error {
	a, err := show(approvalID)
	if err != nil || a.Kind != contract.KindApproval || !a.Open() || !a.HasLabel(registry.AutoApproveFailedLabel) {
		return err
	}
	strategy, err := git.LandStrategyName()
	if err != nil || strategy != git.LandStrategyGitlab {
		return err
	}
	pin, ok := git.ParseCommit(a.Description)
	if !ok {
		return nil
	}
	if err := checkPacket(a.Description, pin, "", ""); err != nil {
		slog.Warn("luffy.fallback_mr_no_packet", "bead", approvalID, "err", err)
		return nil // the Packet was never written: nothing to open an MR with
	}
	container, err := show(a.Parent)
	if err != nil {
		return err
	}
	if err := ApprovalMR.Open(container, a, true); err != nil {
		return fmt.Errorf("luffy.OpenFallbackMR: approval %s: open MR: %w", approvalID, err)
	}
	return nil
}

// approvalPromptAFK renders the headless session Run drives: the shared core
// between the AFK head and tail, the tail carrying the Draft block.
func approvalPromptAFK(approvalID string, chart, hasSpec bool, surgeries []bd.Bead, historyLine string) string {
	return fmt.Sprintf(approvalAFKHead+coreFor(chart)+approvalAFKTail, approvalID, surgerySlot(hasSpec, surgeries), "", contract.DraftInstruction(approvalID), diffForPacket, historyLine)
}

// approvalPromptAFKWait renders the gitlab-without-auto-approve variant: the same core,
// a tail with no History ending line.
func approvalPromptAFKWait(approvalID string, chart, hasSpec bool, surgeries []bd.Bead) string {
	return fmt.Sprintf(approvalAFKWaitHead+coreFor(chart)+approvalAFKWaitTail, approvalID, surgerySlot(hasSpec, surgeries), "", contract.DraftInstruction(approvalID), diffForPacket)
}

// runApproval is the AFK Approval arm (Spec D8, the Usopp hybrid): run the
// headless session that writes the Decision Packet, check its outcome line and the
// written packet, then hand the approved close to the publish with the session's
// Drafts. Any miss is an error and closes nothing.
func runApproval(ctx contract.Ctx, bead contract.Bead) error {
	a, err := show(bead.ID)
	if err != nil || !a.Open() {
		return err // already closed by a prior dispatch: no double close
	}
	pin, ok := git.ParseCommit(a.Description)
	if !ok {
		return fmt.Errorf("luffy.Run: approval %s carries no Commit: pin", bead.ID)
	}
	patchID, _ := git.ParsePatchID(a.Description) // a Chart's Approval carries one beside the pin; the session must keep it
	parent, chart, hasSpec, surgeries, err := intentBehind(bead.ID)
	if err != nil {
		return fmt.Errorf("luffy.Run: %w", err)
	}
	strategy, err := git.LandStrategyName()
	if err != nil {
		return fmt.Errorf("luffy.Run: approval %s: %w", bead.ID, err)
	}
	auto := ReadAutoApprove()
	line := afkHistoryLine(auto)                            // read once: the prompt and the check share it
	waits := strategy == git.LandStrategyGitlab && !auto.On // the human's un-draft approves: no History ending
	prompt := approvalPromptAFK(bead.ID, chart, hasSpec, surgeries, line)
	if waits {
		line, prompt = "", approvalPromptAFKWait(bead.ID, chart, hasSpec, surgeries)
	}
	prompt, err = withRespin(withCarried(prompt, carriedLapsFor(a)), parent)
	if err != nil {
		return fmt.Errorf("luffy: respin packet: %w", err)
	}
	inv, err := contract.NewInvocation(bead.ID, contexts.FocusUnder(prompt, bead.ID, show))
	if err != nil {
		return err
	}
	a, outcome, _, open, err := runSession(ctx, inv, show)
	if err != nil || !open {
		return err // swept or closed during the run: done, no further writes
	}
	if !strings.HasPrefix(outcome, approvedOutcome) {
		return fmt.Errorf("luffy.Run: approval %s: final line %q is not an %s outcome", bead.ID, outcome, approvedOutcome)
	}
	if err := checkPacket(a.Description, pin, patchID, line); err != nil {
		return fmt.Errorf("luffy.Run: approval %s: %w", bead.ID, err)
	}
	if strategy == git.LandStrategyGitlab {
		container, err := show(a.Parent)
		if err != nil {
			return fmt.Errorf("luffy.Run: approval %s: %w", bead.ID, err)
		}
		if waits {
			if open, draft, err := ApprovalMR.Draft(container); err != nil {
				return fmt.Errorf("luffy.Run: approval %s: read MR: %w", bead.ID, err)
			} else if open && !draft { // a re-approval: the un-draft may already vouch (ADR 0382 §5)
				if err := reapprove(ctx, container, a); err != nil {
					return fmt.Errorf("luffy.Run: approval %s: %w", bead.ID, err)
				}
				return nil
			}
		}
		// auto-approve on: the MR opens ready (a reused Draft MR is un-drafted), then the Approval
		// closes approved; off: it opens as a GitLab Draft and the Approval stays open, waiting
		if err := ApprovalMR.Open(container, a, waits); err != nil {
			return fmt.Errorf("luffy.Run: approval %s: open MR: %w", bead.ID, err)
		}
		if waits {
			return edgesOnPublish(ctx, bead.ID, nil) // no-close publish (ADR 0079 §5): offshoot Drafts publish, the Approval stays open
		}
	}
	return closeOnPublish(ctx, bead.ID, bd.ApprovedReason, nil)
}

// checkPacket requires the written Approval body to keep the claimed pin verbatim
// and first, the Patch-id: line the Approval carried (patchID, if any) verbatim directly
// after it, to hold a packet beyond them, and to end Approval History with the run's
// auto-approved line.
func checkPacket(body, pin, patchID, line string) error {
	pinLine := git.CommitPin(pin)
	lines := strings.SplitN(strings.TrimSpace(body), "\n", 3)
	if strings.TrimSpace(lines[0]) != pinLine {
		return fmt.Errorf("the body does not start with %q", pinLine)
	}
	head := pinLine
	if patchID != "" {
		patchLine := git.PatchIDPin(patchID)
		if len(lines) < 2 || strings.TrimSpace(lines[1]) != patchLine {
			return fmt.Errorf("the body does not keep %q directly after the pin", patchLine)
		}
		head += "\n" + patchLine
	}
	if len(strings.TrimSpace(body)) <= len(head) {
		return fmt.Errorf("the Decision Packet was not written")
	}
	if line == "" { // gitlab without auto-approve: Go writes the ending, so the session writes none
		if historyEndsAny(body) {
			return fmt.Errorf("Approval History ends with an auto-approved line, but auto-approve is off")
		}
		return nil
	}
	if !historyEndsAuto(body, line) {
		return fmt.Errorf("Approval History does not end with %q", line)
	}
	return nil
}

// historyEndsAuto is whether the body's last Approval History section ends, before
// the next heading or the end, with the auto-approved line.
func historyEndsAuto(body, line string) bool {
	last, in := bd.ApprovalHistoryLast(body)
	return in && last == line
}

// historyEndsAny is whether the body's Approval History ends with any auto-approved line.
func historyEndsAny(body string) bool {
	last, _ := bd.ApprovalHistoryLast(body)
	return strings.HasPrefix(last, bd.AutoApprovedPrefix)
}
