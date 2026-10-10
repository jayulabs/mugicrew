// Package jinbe holds Jinbe, the pure-Go Checkpoint land. He is the Checkpoint
// handler (the container kind:checkpoint): his Run spawns the bare Review once the
// Checkpoint drains with no Approval at head (Spec D8), and lands the
// approved Checkpoint by rebase+squash onto mainline — no agent session runs to
// land — the reconcileChart twin, but AFK-Franky-flavored for the Checkpoint tail.
// He lands ONLY behind the approvedAtHead gate (a closed Approval whose
// Commit: pin still equals checkpoint/<id>'s tip, FR40) so a stuck or unreviewed
// Checkpoint fails safe — visible, never landed. A clean land closes the Checkpoint
// (the merged state, FR41); a rebase conflict becomes a Franky-owned kind:conflict
// member carrying the Base: pin (FR42). Idempotency keys on the land trailer: a
// post-land crash rolls forward and never strands. Land is AFK, so Launch errors
// loudly, mirroring Franky/Usopp.
//
// He also owns his first leaf kind, the human-filed kind:rollback (ADR 0047): a
// pure-Go forward revert of a landed Checkpoint's squash on main, then the handoff
// of the reverted work to a successor Checkpoint he seeds and stewards (ADR 0048).
package jinbe

import (
	"errors"
	"fmt"
	"strings"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/git"
)

// Jinbe is the concrete Checkpoint-land crewmate. It carries no state; bd access
// and the harness arrive on the ctx (spec §4), mirroring Nami/Zoro/Franky/Usopp.
type Jinbe struct{}

// Jinbe satisfies the crewmate contract.
var _ contract.Crewmate = Jinbe{}

// Run is the AFK path the loop dispatches. It re-reads the bead's current graph
// neighborhood (the loop hands only ID+Kind) and lands the Checkpoint or reverts
// the Rollback's target — no harness call, both are pure Go. An unowned kind is a
// loud error, mirroring Franky.Run.
func (Jinbe) Run(_ contract.Ctx, bead contract.Bead) error {
	b, err := showBead(bead.ID)
	if err != nil {
		return err
	}
	switch b.Kind {
	case contract.KindCheckpoint:
		return reconcileCheckpoint(b)
	case contract.KindRollback:
		return reconcileRollback(b)
	}
	return fmt.Errorf("jinbe.Run: kind %q has no handler", b.Kind)
}

// Launch composes the HITL command a human opens — but the Checkpoint land and the
// Rollback are AFK (loop-only), so this must never be reached: it errors loudly rather than compose
// a bad command, mirroring Franky's/Usopp's Launch guard.
func (Jinbe) Launch(_ contract.Ctx, bead contract.Bead) (contract.LaunchCommand, error) {
	return nil, fmt.Errorf("jinbe.Launch: kind %q is AFK (checkpoint land / rollback), not launchable — dispatched via Run", bead.Kind)
}

// Seams over the bd/git legs reconcileCheckpoint and reconcileRollback drive, so the approvedAtHead gate,
// the land-trailer idempotency roll-forward, and the conflict-raise path are testable
// with no live bd/git (mirrors franky/usopp's var seam). These alias the real package
// funcs; the jinbe test swaps them.
var (
	showBead             = bd.Show
	closeBead            = bd.Close
	spawnChild           = bd.SpawnChildIfAbsent
	closedChildrenOfKind = bd.ClosedChildrenOfKindBeads
	landBranch           = git.Land
	landedBase           = git.LandedBase
	approvedAtHead       = git.ApprovedAtHead
	branchHead           = git.Head
	rollbackTarget       = bd.RollbackTarget
	revertCommit         = git.Revert
	specOf               = bd.SpecOf
	spawnSuccessor       = bd.SpawnSuccessorIfAbsent
	successorOf          = bd.SuccessorOf
	cutBranch            = git.CutBranch
	reapply              = git.Reapply
	buildChildren        = bd.BuildChildren
	reviewDue            = bd.ReviewDue
	membersDrained       = bd.MembersDrained
	openBlockers         = bd.OpenBlockers
)

// checkpointBranch is the branch a Checkpoint lands from (ADR 0010 §3: checkpoint/<id>)
// and checkpointTrailer the land trailer its squash commit carries (ADR 0010 §2:
// "Checkpoint: <id>"), both via git/'s naming rules so the same kind const names both
// and LandedBase greps back the exact string Land stamped. Pure, checkable without git.
func checkpointBranch(id string) string  { return git.BranchName(string(contract.KindCheckpoint), id) }
func checkpointTrailer(id string) string { return git.TrailerFor(string(contract.KindCheckpoint), id) }

// reconcileCheckpoint is the Checkpoint's container step, the reconcileChart twin,
// with two outcomes: land with an Approval at head (FR40–FR42), otherwise spawn the
// bare Review once drained (Spec D8, FR 29). In fixed order, each leg
// idempotent so a re-run (or a crash mid-way) converges:
//
//  1. Roll-forward idempotency (FR42): if the land trailer is already on mainline the
//     Checkpoint has landed (a crash between Land and close) — re-derive nothing, just
//     close it if still open and return. The trailer is the durable landed fact, so a
//     post-land crash never strands and never re-commits.
//  2. The approvedAtHead gate (FR40): land ONLY when a closed Approval's Commit: pin
//     equals the live tip of checkpoint/<id> — NOT "0 open children". No approval at
//     head ⇒ never land unreviewed: spawn the Review if one is due (spawnReviewIfDue),
//     else leave the Checkpoint open and visible. Approved but members not drained
//     (one still open or holding effort/<id>, e.g. a Conflict whose integrate didn't
//     publish) ⇒ wait silently (Spec FR 28, FR 47). So does an open
//     incoming blocker, e.g. a before-land Task (the land hold, Spec FR 15).
//  3. Land by rebase+squash onto mainline (git-native idempotent): the squash stamps
//     the "Checkpoint: <id>" trailer step 1 re-derives. A rebase conflict becomes one
//     Franky-owned kind:conflict member carrying the Base: pin instead of propagating
//     (mirrors fileChartConflict, but AFK — Franky resolves it); an *ADRClash (an
//     added ADR number the locked base already holds, ADR 0030/0069) files the same
//     member with its clash-variant body.
//  4. A clean land closes the Checkpoint — the merged state (FR41).
func reconcileCheckpoint(cp bd.Bead) error {
	if _, err := landedBase(checkpointTrailer(cp.ID)); err == nil {
		return closeIfOpen(cp, "") // already landed: roll forward to the close
	}
	approved, err := checkpointApproved(cp)
	if err != nil {
		return err
	}
	if !approved {
		return spawnReviewIfDue(cp)
	}
	if drained, err := membersDrained(cp.ID); err != nil || !drained {
		return err
	}
	if held, err := openBlockers(cp.ID); err != nil || len(held) > 0 {
		return err
	}
	sha, err := landBranch(checkpointBranch(cp.ID), checkpointTrailer(cp.ID))
	eventlog.RecordLand(cp, checkpointBranch(cp.ID), sha, err)
	if err != nil {
		var conflict *git.RebaseConflict
		if errors.As(err, &conflict) {
			return fileConflict(cp, conflict.Base, nil)
		}
		var clash *git.ADRClash
		if errors.As(err, &clash) {
			return fileConflict(cp, clash.Base, clash.Clashes)
		}
		return err
	}
	return closeIfOpen(cp, "")
}

// reconcileRollback undoes a landed Checkpoint on main (ADR 0047), in fixed order:
//
//  1. Resolve the target over the Rollback's related edge, and reject an invalid
//     one — none, still open, or no Checkpoint: trailer on main — by closing the
//     Rollback "invalid: <why>" with nothing written.
//  2. Revert the target's squash forward on main with a Reverts-Checkpoint: <id>
//     trailer. git.Revert is idempotent on that trailer, so a crash resume or a
//     second Rollback against the same target writes no second revert (it still
//     hands off its own successor — succession is per Rollback, ADR 0048 §2). A
//     conflict fixes forward instead (ADR 0049): nothing written to main. Any other
//     failure is loud and leaves the Rollback open; a manual unclaim and
//     re-dispatch converges by idempotency (ADR 0052: no lease).
//  3. Hand the work off (ADR 0048, handOff): successor Checkpoint, its branch cut
//     at the revert and seeded with a reapply — or, fixing forward, cut at main HEAD
//     with no seed — and its one Build.
//  4. Close the Rollback — only once the handoff exists (ADR 0048 §6), reason
//     "fix-forward: revert conflicted" when fixing forward. A fix-forward leaves no
//     Reverts-Checkpoint: trailer, so its resume keys on the successor instead: once
//     spawned it pins the decision (pinnedFixForward) and the revert is not retried.
func reconcileRollback(rb bd.Bead) error {
	if !rb.Open() {
		return nil // closed only at handoff (or as invalid): done, never re-hand-off
	}
	target, ok, err := rollbackTarget(rb.ID)
	if err != nil {
		return err
	}
	if !ok {
		return closeIfOpen(rb, "invalid: no related Checkpoint")
	}
	cp, err := showBead(target)
	if err != nil {
		return err
	}
	if cp.Open() {
		return closeIfOpen(rb, "invalid: "+target+" not landed")
	}
	squash, err := landedBase(checkpointTrailer(target))
	if errors.Is(err, git.ErrNotLanded) {
		return closeIfOpen(rb, "invalid: "+target+" has no land trailer")
	}
	if err != nil {
		return err
	}
	conflict, err := pinnedFixForward(rb.ID)
	if err != nil {
		return err
	}
	var sha string
	if conflict == nil {
		sha, err = revertCommit(squash, git.RevertsTrailer(string(contract.KindCheckpoint), target))
		eventlog.RecordRevert(rb, target, squash, sha, err)
		if err != nil && !errors.As(err, &conflict) {
			return err
		}
	}
	successor, err := handOff(rb, target, squash, sha, conflict)
	eventlog.RecordHandoff(rb, successor, err)
	if err != nil {
		return err
	}
	if conflict != nil {
		return closeIfOpen(rb, bd.FixForwardReason+" revert conflicted")
	}
	return closeIfOpen(rb, "")
}

// handOff brings the reverted work back as a successor Checkpoint (ADR 0048), each
// leg skip-if-done so a crash anywhere resumes cleanly (ADR 0003): spawn it caused-by
// the Rollback (idempotent per Rollback; no supersedes edge — the target is history),
// cut checkpoint/<new> at the revert commit, seed it with the reapply commit, and
// spawn its one Build carrying the breakage evidence. Fixing forward (conflict
// non-nil, ADR 0049) the branch is cut at main HEAD with no seed — the code is still
// on main — and the Build also carries the conflict. Returns the successor's id.
func handOff(rb bd.Bead, target, squash, revert string, conflict *git.RevertConflict) (string, error) {
	desc := "Successor of " + target + ", reverted on main by Rollback " + rb.ID + " (ADR 0048)."
	if conflict != nil {
		desc = "Successor of " + target + ", fixing forward for Rollback " + rb.ID + ": its revert conflicted, main is unchanged (ADR 0049).\n" +
			git.ConflictPin(conflict)
	}
	// The Spec line is required by Spec §4 ("its body states the Spec") for a
	// human or agent reading the body; Go readers still derive it with bd.SpecOf.
	spec, ok, err := specOf(target)
	if err != nil {
		return "", err
	}
	if ok {
		desc += "\nSpec: " + spec
	}
	cp, _, err := spawnSuccessor(rb.ID, bd.Successor{
		Kind:        contract.KindCheckpoint,
		Container:   true,
		Title:       "Rollback of " + target,
		Description: desc,
	})
	if err != nil {
		return "", err
	}
	branch := checkpointBranch(cp)
	base, body := revert, buildBody(rb.Description, target, squash)
	if conflict != nil {
		base, body = conflict.Base, fixForwardBody(rb.Description, squash, conflict.Paths)
	}
	if _, err := cutBranch(branch, base); err != nil {
		return cp, err
	}
	if conflict == nil {
		if _, err := reapply(branch, revert); err != nil {
			return cp, err
		}
	}
	// Any Build, open or closed: SpawnChildIfAbsent alone guards only an OPEN one, and
	// Franky may finish the Build before a crashed pass resumes.
	if n, err := buildChildren(cp); err != nil || n > 0 {
		return cp, err
	}
	_, _, err = spawnChild(cp, bd.Successor{
		Kind:        contract.KindBuild,
		Title:       "Fix what " + target + " broke on main",
		Description: body,
	})
	return cp, err
}

// buildBody is the successor Build's body (ADR 0048 §5): the Rollback's breakage
// evidence, or — filed with an empty body — the concrete first step.
func buildBody(rollbackBody, target, squash string) string {
	if strings.TrimSpace(rollbackBody) != "" {
		return rollbackBody
	}
	return "Reproduce the breakage on main at " + target + "'s land (" + squash + "), then fix it."
}

// fixForwardBody is a fix-forward successor Build's body (ADR 0049 §3): the evidence
// (or reproduce-then-fix on main HEAD), the target squash, the conflicted paths, and
// why the revert was skipped. The later lands are left for Franky to derive.
func fixForwardBody(rollbackBody, squash string, paths []string) string {
	if strings.TrimSpace(rollbackBody) == "" {
		rollbackBody = "Reproduce the breakage on main HEAD, then fix it."
	}
	return rollbackBody + "\n\nTarget squash: " + squash +
		"\n" + git.PathsPin(paths) +
		"\nThe revert was skipped because later lands built on it (git log " + squash + "..HEAD -- <paths>)."
}

// pinnedFixForward reads back a fix-forward decision a prior pass already acted on:
// the Rollback's successor Checkpoint exists and its body carries the
// ConflictPin (Fix-forward: base, conflicted paths) handOff stamped. Once that
// successor is spawned the decision is final, so a resume never retries the revert —
// main may have changed so it now applies cleanly, which would revert under a
// successor cut for fixing forward.
// No successor, or a clean-revert one (no Fix-forward: pin), returns nil: decide afresh.
func pinnedFixForward(rbID string) (*git.RevertConflict, error) {
	succ, ok, err := successorOf(rbID, contract.KindCheckpoint)
	if err != nil || !ok {
		return nil, err
	}
	b, err := showBead(succ)
	if err != nil {
		return nil, err
	}
	c, _ := git.ParseConflict(b.Description)
	return c, nil
}

// spawnReviewIfDue spawns the bare Review of a drained Checkpoint with no Approval
// at head (bd.ReviewDue). Jinbe re-derives it every tick under the Checkpoint's
// status-CAS claim, so no member has to decide it is last and a crash between a
// member's close and the spawn strands nothing; SpawnChildIfAbsent plus the
// open-Review leg of ReviewDue make a re-run spawn no second one (ADR 0103). An
// open Approval at head waits for the human; a stale one gets a fresh Review,
// whose pass obsoletes it.
func spawnReviewIfDue(cp bd.Bead) error {
	due, err := reviewDue(cp.ID, approvedAtHead)
	if err != nil || !due {
		return err
	}
	_, _, err = spawnChild(cp.ID, reviewSuccessor(cp.ID))
	return err
}

// reviewSuccessor is the one Review member a drained Checkpoint spawns: a bare
// kind:review leaf with NO Commit: pin, because Usopp pins git.Head at review
// start himself. Pure, so the emitted shape is checkable without a DB.
func reviewSuccessor(checkpointID string) bd.Successor {
	return bd.Successor{
		Kind:        contract.KindReview,
		Container:   false,
		Title:       "Built slice of checkpoint " + checkpointID,
		Description: "Review the built Checkpoint slice — Usopp pins the branch HEAD at review start (no Commit: pin seeded here).",
	}
}

// checkpointApproved is the approvedAtHead gate (FR40): approved iff ANY closed Approval
// child's Commit: pin still equals checkpoint/<id>'s live tip. It scans ALL closed
// Approvals, not the first: after a stale-Approval self-heal a
// Checkpoint carries the stale obsoleted Approval (pin behind HEAD) AND the real at-head
// one, and a first-match read hits the stale pin and strands the land. No closed Approval
// at the tip (all pins behind it, a post-approval commit moved HEAD) is not approved, and
// a drained Checkpoint then gets a fresh Review (Spec FR 31).
func checkpointApproved(cp bd.Bead) (bool, error) {
	approvals, err := closedChildrenOfKind(cp.ID, contract.KindApproval)
	if err != nil {
		return false, err
	}
	for _, a := range approvals {
		if approvedAtHead(a.Description, checkpointBranch(cp.ID)) {
			return true, nil
		}
	}
	return false, nil
}

// fileConflict raises the one open kind:conflict member that undrains the Checkpoint
// on a rebase collision (mirrors fileChartConflict — but Franky-owned AFK, unlike the
// HITL chart-conflict): idempotent via SpawnChildIfAbsent's open-child guard, so a
// re-run with the conflict still open files no second bead. The undrain is what keeps
// the loop off it: loop.CheckpointReady excludes a Checkpoint with an open conflict/
// critique child, so this bead being open drops it from land-candidacy until
// Franky resolves it. The member carries the Base: pin (git.BasePin) Franky's runConflict
// greps back to rebase onto, and the Tip: pin (git.TipPin) of the Checkpoint tip the
// failed land saw: Land never writes the branch (ADR 0102 §8), so the tip read now is
// it, and Franky's effort is cut there and replace-CASes it (Spec D3).
// Non-empty clashes is the ADR-number-clash variant: the body names the colliding
// ADRs to renumber instead. Returns nil on success — the conflict is now a bead,
// not a propagated error.
func fileConflict(cp bd.Bead, base string, clashes []string) error {
	tip, err := branchHead("refs/heads/" + checkpointBranch(cp.ID))
	if err != nil {
		return err
	}
	body := "Checkpoint-land rebase conflict: checkpoint/" + cp.ID + " would not rebase cleanly onto mainline. Franky re-runs the rebase onto the pinned base, then this bead closes and the Checkpoint re-lands.\n"
	if len(clashes) > 0 {
		body = "Checkpoint-land ADR-number clash (ADR 0030): checkpoint/" + cp.ID + " adds an ADR number mainline already holds under another filename. Franky rebases onto the pinned base and renumbers the branch's ADR to the next free number (fixing references to it), then this bead closes and the Checkpoint re-lands.\nclashes:\n- " + strings.Join(clashes, "\n- ") + "\n"
	}
	_, _, err = spawnChild(cp.ID, bd.Successor{
		Kind:        contract.KindConflict,
		Container:   false,
		Title:       "Checkpoint " + cp.ID + " conflicts with main",
		Description: body + git.BasePin(base) + "\n" + git.TipPin(tip),
	})
	return err
}

// closeIfOpen closes b with reason behind the Open() guard so a re-dispatch after a
// committed-but-not-closed crash converges without a double close (mirrors the reconcile
// close guards).
func closeIfOpen(b bd.Bead, reason string) error {
	if b.Open() {
		return closeBead(b.ID, reason)
	}
	return nil
}
