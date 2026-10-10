// Package luffy holds Luffy, the one HITL approval stop. He owns the one kind
// `approval` — the Checkpoint tail leaf a clean review hands off — and
// presents the decision packet so a human makes the one deliberate approve call.
// Approval is HITL, so the loop never dispatches it: Launch composes the one
// session a human opens, and Run is a loud error (mirrors Sanji). Approve is
// authorize-not-author: Luffy is pure-prompt — the launched agent assembles the
// packet (diff + clean-review evidence + critique history) and runs the one
// `bd close --reason=approved` on the human's explicit OK, and NEVER touches the
// inherited Commit: pin Jinbe's gate reads (§9, FR32–FR37).
package luffy

import (
	_ "embed"
	"fmt"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// The bd reads Launch makes, as seams so the render tests stay off a real bd.
var (
	show      = bd.Show
	surgeryOf = bd.SurgeryOf
)

// Luffy is the concrete approval crewmate. It carries no state; the harness
// arrives on the ctx (spec §4), mirroring Sanji.
type Luffy struct{}

// Luffy satisfies the crewmate contract.
var _ contract.Crewmate = Luffy{}

// Run is the AFK path the loop would dispatch — but approval is HITL, so this
// must never be reached. It errors loudly rather than doing loop work, mirroring
// Sanji.Run: an approval bead is opened via Launch, never Run (FR33).
func (Luffy) Run(_ contract.Ctx, bead contract.Bead) error {
	return fmt.Errorf("luffy.Run: kind %q is HITL (approval), not a loop kind — open it via Launch", bead.Kind)
}

// Launch composes the one HITL command a human opens for an approval bead. The
// interface is as thin as Sanji's — no decision-packet Go type, no consolidation
// layer, no post-close step (FR33): one AgentCommand wrapping approvalPrompt. The
// packet is assembled in-session by the agent, not in Go. A non-approval kind is a
// loud error, mirroring Sanji's Launch default. The one Go read is the Surgery
// behind a surgery Checkpoint, which the packet names (Spec D8).
func (Luffy) Launch(ctx contract.Ctx, bead contract.Bead) (contract.LaunchCommand, error) {
	if bead.Kind != contract.KindApproval {
		return nil, fmt.Errorf("luffy.Launch: kind %q is not a launchable HITL kind", bead.Kind)
	}
	surgery, err := surgeryBehind(bead.ID)
	if err != nil {
		return nil, fmt.Errorf("luffy.Launch: %w", err)
	}
	inv, err := contract.NewInvocation(bead.ID, approvalPrompt(bead.ID, surgery))
	if err != nil {
		return nil, err
	}
	return ctx.Harness().LaunchCommand(inv)
}

// approvalPrompt is the bespoke approval workflow the launched session runs
// (FR32–FR37): it carries its own instructions whole — no backing skill, no
// outside dependency (FR37) — to assemble the decision packet (the diff, the clean-review
// evidence, the critique history), surface a HEAD-vs-pin mismatch advisorily, and
// run the approve-only close on the human's explicit OK. The shared stance
// (refer-by-title, authorization-not-authorship, bd/git policy) is the Crewmate
// preamble's, not repeated here. Pure — asserted through
// the Launch seam + render tests, mirroring Sanji's prototypePrompt.
//
//go:embed prompts/approval.md
var approvalTmpl string

// surgeryBehind is the Surgery of the Approval's parent Checkpoint, or a zero
// Bead when the Checkpoint has none.
func surgeryBehind(approvalID string) (bd.Bead, error) {
	approval, err := show(approvalID)
	if err != nil || approval.Parent == "" {
		return bd.Bead{}, err
	}
	id, ok, err := surgeryOf(approval.Parent)
	if err != nil || !ok {
		return bd.Bead{}, err
	}
	return show(id)
}

// approvalPrompt renders the packet. A Surgery adds its one line to the packet
// (Spec D8); a zero Bead leaves the packet unchanged.
func approvalPrompt(approvalID string, surgery bd.Bead) string {
	line := ""
	if surgery.ID != "" {
		line = fmt.Sprintf(" On this surgery Checkpoint, open the packet with the line\n\n    Surgery: %s — %s\n\nso an approver who did not dictate the code reads its intent.", surgery.ID, surgery.Title)
	}
	return fmt.Sprintf(approvalTmpl, approvalID, line)
}
