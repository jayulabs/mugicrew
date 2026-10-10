// Package usopp holds Usopp, the AFK reviewer. He owns the one Checkpoint tail
// leaf kind — review — and mutates no tree: one bounded read-only pass over the
// built Checkpoint slice. The review agent runs the repo's two-axis
// code-review workflow whole, its human-report step replaced by findings→beads —
// it files a kind:critique child of the Checkpoint per confirmed blocking finding
// — then Usopp (Go) drives the hybrid clean→Approval transition: read the beads
// the agent wrote and, on zero open critiques, spawn the pinned Approval. Review
// is AFK, so Launch errors loudly, mirroring Franky's Launch.
package usopp

import (
	_ "embed"
	"fmt"
	"text/template"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// Usopp is the concrete reviewer crewmate. It carries no state; bd access and the
// harness arrive on the ctx (spec §4), mirroring Nami/Zoro/Franky.
type Usopp struct{}

// Usopp satisfies the crewmate contract.
var _ contract.Crewmate = Usopp{}

// Run is the AFK path the loop dispatches. It re-reads the bead's current graph
// neighborhood (the loop hands only ID+Kind) and runs the one review pass. An
// unowned kind is a loud error, mirroring Franky.Run.
func (Usopp) Run(ctx contract.Ctx, bead contract.Bead) error {
	b, err := showBead(bead.ID)
	if err != nil {
		return err
	}
	if b.Kind != contract.KindReview {
		return fmt.Errorf("usopp.Run: kind %q has no handler", b.Kind)
	}
	return runReview(ctx, b)
}

// Launch composes the HITL command a human opens — but review is AFK (loop-only),
// so this must never be reached: it errors loudly rather than compose a bad
// command, mirroring Franky's Launch guard.
func (Usopp) Launch(_ contract.Ctx, bead contract.Bead) (contract.LaunchCommand, error) {
	return nil, fmt.Errorf("usopp.Launch: kind %q is AFK (review), not launchable — dispatched via Run", bead.Kind)
}

// Seams over the bd/git legs runReview drives, so the clean→Approval transition
// is testable with a fake harness and no live bd/git (mirrors franky's var seam).
// These alias the real package funcs; the usopp test swaps them.
var (
	showBead                = bd.Show
	closeBead               = bd.Close
	spawnChild              = bd.SpawnChildIfAbsent
	openChildrenOfKind      = bd.OpenChildrenOfKind
	openChildrenBeadsOfKind = bd.OpenChildrenOfKindBeads
	specOf                  = bd.SpecOf
	surgeryOf               = bd.SurgeryOf
	head                    = git.Head
	openPivots              = bd.OpenPivots
	pivotsDiscoveredFrom    = bd.PivotsDiscoveredFrom
	raisePivot              = bd.RaisePivot
	closeOnPublish          = bd.CloseOnPublish
)

// reviewPrompt (review.md) runs the two-axis code-review Crew doc whole —
// Standards (incl. the Fowler smell baseline) and Spec — over the fixed diff
// main...checkpoint/<id> pinned at review start, with its findings going to
// findings→beads instead of a report. The effective-Spec/AMENDMENT rule, Critique
// filing and pivot raise-or-merge are the checkpoint-rules Crew doc, shared with
// Franky and Zoro (ADR 0005, ADR 0043–0045, ADR 0087, ADR 0088); the prompt keeps
// only the commands with its own ids filled in. On a Spec-less Checkpoint the
// Spec axis reads the Surgery, or reports "no spec available", and the pivot
// section is left out (Spec D6). Pure — asserted through the render
// test, mirroring Franky's implementPrompt.
//
//go:embed prompts/review.md
var reviewSrc string

var reviewTmpl = template.Must(template.New("review").Parse(reviewSrc))

// reviewArgs names review.md's slots so the runReview call site is
// transposition-safe: a silent swap (branch↔pin) of bare positional strings
// would slip past the compiler.
type reviewArgs struct {
	ReviewID     string // the Review bead id
	CheckpointID string // the Checkpoint under review, --parent for filed critiques
	SpecID       string // the Spec axis's source, empty on a Spec-less Checkpoint
	SurgeryID    string // the Spec-less Checkpoint's Surgery, the intent source in place of the Spec
	Branch       string // checkpoint/<id>
	Pin          string // the pinned SHA the diff is cut against
}

// Draft is the review's shared Draft block.
func (a reviewArgs) Draft() string { return contract.DraftInstruction(a.ReviewID) }

// reviewPrompt renders review.md from a reviewArgs. Pure, so the render is checkable
// without bd/git.
func reviewPrompt(a reviewArgs) string { return contract.Render(reviewTmpl, a) }

// runReview is the one review handler (FR26–FR31): pin the Checkpoint branch HEAD
// at review start (the Review was spawned BARE by Jinbe — Usopp pins it himself),
// resolve the Spec (else the Surgery) for the Spec axis, fork the read-only review agent (it files
// critique beads, a pivot, or nothing), then drive the tail: a raised pivot wins
// over the clean→Approval transition. An errored fork leaves the Review open for
// the next tick, mirroring runImplement.
func runReview(ctx contract.Ctx, bead bd.Bead) error {
	if !bead.Open() {
		return nil // already closed by a prior dispatch: no double close / double approve
	}
	if bead.Parent == "" {
		return fmt.Errorf("usopp: review bead %s has no parent Checkpoint membership edge", bead.ID)
	}
	branch := git.BranchName(string(contract.KindCheckpoint), bead.Parent)
	pin, err := head(branch)
	if err != nil {
		return err
	}
	specID, found, err := specOf(bead.Parent)
	if err != nil {
		return err
	}
	var surgeryID string
	if !found {
		if surgeryID, _, err = surgeryOf(bead.Parent); err != nil {
			return err
		}
	}
	prompt := reviewPrompt(reviewArgs{ReviewID: bead.ID, CheckpointID: bead.Parent, SpecID: specID, SurgeryID: surgeryID, Branch: branch, Pin: pin})
	inv, err := contract.NewInvocation(bead.ID, prompt)
	if err != nil {
		return err
	}
	if _, err := ctx.Harness().Run(ctx, inv); err != nil {
		return sweptOr(bead.ID, err)
	}
	// Re-read: a concurrent confirm sweep may have closed the Review (FR49).
	if bead, err = showBead(bead.ID); err != nil || !bead.Open() {
		return err
	}
	pivots, err := pivotsDiscoveredFrom(bead.ID)
	if err != nil {
		return err
	}
	if len(pivots) > 0 {
		return raisePivot(ctx, bead, specID, pivots)
	}
	return approveIfClean(ctx, bead, pin)
}

// sweptOr is a failed fork's err — unless a concurrent confirm sweep closed the
// Review out from under it (FR49): done, not a failure, so dispatch releases no
// claim. Mirrors Franky's.
func sweptOr(id string, err error) error {
	if b, serr := showBead(id); serr == nil && !b.Open() {
		return nil
	}
	return err
}

// approveIfClean is the hybrid clean→Approval transition (FR30/FR31). After the
// agent has filed its critique beads (or nothing), Go reads the Checkpoint's open
// kind:critique children — the beads the agent wrote, no brittle text parse — and
// on ZERO open spawns the Approval FIRST (stamped with the pinned SHA), THEN
// hands the Review's close to the publish in EVERY case — so the critiques the
// agent drafted become visible together with the close (Spec D6, D11 row 4). The
// Approval stays a one-call create (inventory row 15). The Approval is a membership child of the
// Checkpoint carrying the Commit: pin with NO caused-by edge and NO HEAD re-check
// (a stale pin self-heals). Both legs are idempotent — SpawnChildIfAbsent's
// open-child guard and runReview's Open() guard — so a re-dispatch converges
// without a second Approval or a double close. Its read-then-writes (and
// obsoleteStaleApprovals') are serialized by the Review's claim plus graph
// ordering: no Build or critique is open while a Review runs (ADR 0070).
func approveIfClean(ctx contract.Ctx, bead bd.Bead, pin string) error {
	open, err := openChildrenOfKind(bead.Parent, contract.KindCritique)
	if err != nil {
		return err
	}
	pivots, err := openPivots(bead.Parent)
	if err != nil {
		return err
	}
	if open == 0 && len(pivots) == 0 {
		cp, err := showBead(bead.Parent) // the Approval inherits the Checkpoint's title
		if err != nil {
			return err
		}
		if err := obsoleteStaleApprovals(bead.Parent, pin); err != nil {
			return err
		}
		if _, _, err := spawnChild(bead.Parent, approvalSuccessor(cp.Title, pin)); err != nil {
			return err
		}
	}
	return closeOnPublish(ctx, bead.ID, "", nil)
}

// obsoleteStaleApprovals closes any OPEN Approval of the Checkpoint whose Commit:
// pin no longer equals the reviewed HEAD, so this clean re-review's spawn cannot be
// swallowed by SpawnChildIfAbsent's open-child dedup reusing an Approval pinned to a
// superseded commit. That reuse leaves approvedAtHead false forever and
// Jinbe never lands — this is the "obsoleted and respawned off the new clean Review"
// self-heal GLOSSARY.md's Approval note and FR31/FR24 promise. An open Approval
// ALREADY at HEAD is left untouched for the spawn to reuse, so a plain re-dispatch
// (unchanged HEAD) neither churns nor double-approves.
func obsoleteStaleApprovals(checkpointID, pin string) error {
	open, err := openChildrenBeadsOfKind(checkpointID, contract.KindApproval)
	if err != nil {
		return err
	}
	for _, a := range open {
		if got, ok := git.ParseCommit(a.Description); !ok || got != pin {
			if err := closeBead(a.ID, "Approval obsoleted: pinned commit superseded by advanced HEAD"); err != nil {
				return err
			}
		}
	}
	return nil
}

// approvalSuccessor is the one Approval a clean review hands off (FR31): a
// membership child kind:approval leaf of the Checkpoint, carrying the Commit: pin
// captured at review start (git.CommitPin, ParseCommit greps it back), titled
// exactly the Checkpoint's title (the kind rides on the label). Pure, so
// the emitted shape (leaf, kind, pinned body) is checkable without a DB, mirroring
// Jinbe's reviewSuccessor.
func approvalSuccessor(checkpointTitle, pin string) bd.Successor {
	return bd.Successor{
		Kind:        contract.KindApproval,
		Container:   false,
		Title:       checkpointTitle,
		Description: "Clean review pass over the built Checkpoint slice at the pinned commit.\n" + git.CommitPin(pin),
	}
}
