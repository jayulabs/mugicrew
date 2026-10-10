# ADR 0103: mugicrew sibling races without the branch hold: Jinbe spawns the Review from drained-Checkpoint state, Chart-body edits go through a locked append/drop verb, and duplicate pivots are accepted

Status: Accepted
Date: 2026-10-08
Amends: ADR 0070 §1 (serializer list) and §2, and its Consequences. Builds on ADR 0102.

## Context

ADR 0102 lets sibling leaves in one container run at once. ADR 0070 named
"the container's branch hold" (ADR 0068) as the serializer of several
read-then-writes, and that hold is gone. Research inventoried
them. It found:

- **R1.** Franky's last-member Review spawn gives zero Reviews when two
  members both read before either closes.
- **R2.** `surgery end` spawns a Review.
- **R3.** Pivot raise and merge.
- **R4.** Chart-body edits from defog sessions are whole-description
  `bd update`s, which are last-write-wins. This covers Decisions-so-far and
  also the Not-yet-specified and Out-of-scope tends.

ADR 0102 §9 already covers R5 (close before integrate).

Decided in grilling.

## Decision

1. **Jinbe spawns the Review from state.** `reconcileCheckpoint` runs under the
   Checkpoint's status-CAS claim (ADR 0052) and has two outcomes. With an
   Approval at head, it lands as today. Without one, it spawns the bare Review
   (`SpawnChildIfAbsent`) when the Checkpoint is **drained**. Drained means:
   - no open Build, Critique, Conflict or Surgery, where a child with a live
     `effort/<id>` ref counts as open (ADR 0102 §9);
   - no open pivot;
   - no open Review.

   `CheckpointReady`'s candidacy widens to match. Franky's `finishAndReview`
   only closes, and `readyForReview` goes away. The loop re-derives the spawn
   every tick, so there is no sibling decision and a crash between a close and
   a spawn can't strand the Checkpoint. Re-review becomes state-driven rather
   than event-driven, so a tip that moves past a stale Approval gets a fresh
   Review instead of stalling. This also removes the double Review that can
   happen at Cap 1 after a crash and re-dispatch.
2. **`surgery end` only integrates and closes.** The Surgery is its
   Checkpoint's only authoring member (ADR 0092 §2), so nothing runs beside it.
   `EndSurgery` no longer spawns the Review (§1 does). Its "no commits → close
   the Checkpoint abandoned" branch is unchanged.
3. **Duplicate pivots are accepted.** Two siblings may each raise a pivot on
   one flaw. The sweep closes the spare as `superseded:` (`zoro/sweep.go`), at
   the cost of one extra HITL item. The build and review prompts merge only
   into a pivot found by their blocks-edge query. That query lists only
   published pivots, so a creator's failure tail can't delete the merge
   target. The `bd/pivot.go` ponytail comment names that rule as its
   serializer. No code change beyond the comment and the prompt line.
4. **Chart-body edits go through `mugicrew chart append|drop <chart> <section>
   <line>`.** The verb re-reads the description, adds or removes one line in
   the named section, and writes it back, all under ADR 0102's one land lock
   (milliseconds). The defog skeleton (Decisions-so-far, graduating fog out of
   Not-yet-specified, Out-of-scope notes) uses the verb, not
   `bd update --description`. A human's TUI edit stays last-write-wins, as in
   ADR 0070 §2.

ADR 0070 §1's serializer list becomes: the claim on the container, the
predecessor or the creator bead, or the land lock. The branch hold is no longer
one.

## Considered and rejected

- **Close first, then spawn under an `--if-assignee ''` mutex on the
  Checkpoint.** It breaks spawn-before-close, a crash between the two stalls the
  Checkpoint, and it overloads the Checkpoint's assignee.
- **A Review Draft pre-created and blocked by every member.** Every later
  member needs a new edge, including critiques published in the Review's own
  batch, so the Review would be re-created on every pass.
- **Franky owning the drained-Checkpoint step.** It would add a second
  candidacy arm over the same Checkpoint claim that Jinbe already takes.
- **Decisions-so-far as `bd comments` or derived from close reasons.** It fixes
  one section of three. Not-yet-specified and Out-of-scope tends still race.
- **Re-read just before `bd update --description`.** This narrows the window
  but isn't atomic. A lost "graduated" removal can later lead to a duplicate
  defog bead.

## Consequences

- Usopp's `approveIfClean`/`obsoleteStaleApprovals` (R8) is safe again,
  because a Checkpoint has at most one open Review.
- Readers of Decisions-so-far are unchanged, because the section stays in the
  body.
- Still untested and out of this Chart: whether a late publish batch reopens a
  Draft the sweep force-closed (R9). It exists at Cap 1 and is filed as an
  offshoot Signal.
