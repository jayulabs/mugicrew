// Package sanji holds Sanji, the prototyping crewmate. He owns the one kind
// `prototype` — a defog bead resolved by a high-fidelity throwaway that settles
// an interface-design question during charting (ADR 0017). Prototype is HITL, so
// the loop never dispatches it: Launch composes the one session a human opens,
// and Run is a loud error. The throwaway lands to a LOCAL side ref that is never
// merged (ADR 0009). Beyond naming the throwaway's temp path, Sanji is
// pure-prompt — the launched agent crafts every bd/git command, the human only
// authorizes (§9.2, mirrors Nami's grilling).
package sanji

import (
	_ "embed"
	"fmt"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contexts"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// showBead is the bd read behind the focus line, swapped by tests.
var showBead = bd.Show

// Sanji is the concrete prototyping crewmate. It carries no state; the harness
// arrives on the ctx (spec §4), mirroring Nami and Zoro.
type Sanji struct{}

// Sanji satisfies the crewmate contract.
var _ contract.Crewmate = Sanji{}

// Run is the AFK path the loop would dispatch — but prototype is HITL, so this
// must never be reached. It errors loudly rather than doing loop work, mirroring
// Zoro's loud Launch default: a prototype bead is opened via Launch, never Run
// (ADR 0017 §1).
func (Sanji) Run(_ contract.Ctx, bead contract.Bead) error {
	return fmt.Errorf("sanji.Run: kind %q is HITL (prototype), not a loop kind — open it via Launch", bead.Kind)
}

// Launch composes the one HITL command a human opens for a prototype bead (ADR
// 0017 §1). The interface is as thin as the bare activation contract — no
// decision packet, no consolidation (Sanji is not Luffy): one AgentCommand
// wrapping prototypePrompt. A non-prototype kind is a loud error, mirroring
// Nami's Launch default.
func (Sanji) Launch(ctx contract.Ctx, bead contract.Bead) (contract.LaunchCommand, error) {
	if bead.Kind != contract.KindPrototype {
		return nil, fmt.Errorf("sanji.Launch: kind %q is not a launchable HITL kind", bead.Kind)
	}
	inv, err := contract.NewInvocation(bead.ID, contexts.FocusUnder(prototypePrompt(bead.ID, git.TempWorktreePath(bead.ID)), bead.ID, showBead))
	if err != nil {
		return nil, err
	}
	return ctx.Harness().LaunchCommand(inv)
}

// prototypeSideRef is the local ref a throwaway lands on (ADR 0009/0017 §3). It is
// keyed on the parent Chart id, which Launch does not carry (contract.Bead is
// ID+Kind only) — so <chart-id> is a placeholder the agent resolves from the
// bead's parent, not a Go value. The ref format mirrors route.prototypeRef (named
// here too, since route's is unexported and contract.Bead carries no parent).
const prototypeSideRef = "refs/mugicrew/prototypes/<chart-id>"

//go:embed prompts/prototype.md
var prototypeTmpl string

// prototypePrompt is the throwaway-prototype workflow the launched session runs
// (ADR 0017 §2): the distilled prototype Crew doc, whose every capture step
// lands on the side ref (ADR 0009), run on the defog-session skeleton shared
// with Nami's grilling (ADR 0087, ADR 0088). Rationale the prompt no longer
// cites (ADR 0088 §6): the session works on effort/<id>, integrated onto
// chart/<chart-id> (ADR 0102); separable work is an offshoot Signal (ADR 0006). scratch
// is the temp-shaped path the detached throwaway tree is cut at
// (git.TempWorktreePath, keyed on the bead), so a lingering one reads as
// mugicrew's and blocks nothing (ADR 0071). Pure — asserted through the Launch seam test, mirroring
// Nami's grillingPrompt.
func prototypePrompt(prototypeID, scratch string) string {
	return fmt.Sprintf(prototypeTmpl, prototypeID, prototypeSideRef, scratch, contract.DraftInstruction(prototypeID))
}
