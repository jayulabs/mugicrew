# ADR 0106: mugicrew foreign hold is scoped to the branch a dispatch cuts — `effort/<id>`, never a ref-only container; a human checkout of a container is caught only by integrate's `held` refusal

Status: Accepted
Date: 2026-10-09
Amends: ADR 0054 §1/§2 (scope, own contention), ADR 0055 §5 (auto-clear), ADR 0105 §3 (pointer to this decision).

## Context

ADR 0054 detects a foreign hold when git refuses a worktree cut because another
worktree holds the branch. ADR 0055 pauses the bead `foreign-hold` after 3 such
skips and lifts the pause automatically. Both were written when a leaf's cut
checked out its container (`chart/<id>`, `checkpoint/<id>`).

ADR 0102 makes containers ref-only. Leaves and own-container sessions cut
`effort/<id>`, and Land runs in a detached tree. No mugicrew cut checks out a
container any more, so git never refuses a cut on one. The only remaining
check is integrate's `held` refusal (ADR 0102 §3), which ADR 0104 §9 turns
into `paused:integrate-conflict` on the leaf. ADR 0105 §3 left this question
open. Decided in grilling.

## Decision

1. **A foreign hold is a hold on the branch a dispatch cuts.** That branch is
   the bead's routed branch, `effort/<id>` for every committing session. A
   container ref is never cut, so a human checking out `chart/<id>` or
   `checkpoint/<id>` by hand is **not** a foreign hold. The loop adds no check
   on container refs, and no Chart or Checkpoint is ever paused `foreign-hold`
   for one.
2. **Integrate is the only check on a container.** Its `held` refusal (ADR
   0102 §3) stays. CASing a ref under a checked-out tree would bring back loss
   path C3. The backstop pauses the leaf `integrate-conflict` with `reason:
   held` (ADR 0104 §9). Only a successful integrate lifts that pause (ADR 0104
   §1). The loop does not lift it when the container's checkout goes away.
3. **Land ignores a hold on its container.** Land never moves the container
   (ADR 0102 §8), so it can't strand a tree. Under the lock, its pin check
   catches a human committing mid-land. Commits made before the land are part
   of the container and land with it. If the post-land `git branch -D` is
   refused because the tree still holds the branch, it logs at Warn, as
   `deleteLanded` already does. The leftover branch is harmless.
4. **ADR 0054's classification applies unchanged to `effort/<id>`.**
   - **Own:** the in-process run-now cut, or an *alive* session for the same
     bead. A bead has at most one live effort (ADR 0102 §1), so the arm that
     matches another in-flight bead's routed branch can never fire on an
     effort branch. Removing it is optional.
   - **Foreign:** a human's checkout of `effort/<id>`, an orphaned loop tree
     (ADR 0105 §3), an exited-for-resume session's tree, or the main worktree
     on the branch. These are counted, paused after 3 skips, and lifted
     automatically once the tree is gone (ADR 0055 §4/§5). The auto-clear
     reads `effort/<id>`.
   - **`t` (retry hold).** It drops a clean mugicrew-made orphan before it
     cuts (ADR 0105 §2). It never touches a tree that isn't temp-shaped (ADR
     0054 §5).
   - **Events.** `effort.held` and `effort.paused` keep their names and
     shapes. Their `branch` field is `effort/<id>`.

## Considered and rejected

- **Pausing the container bead `foreign-hold` when its ref is checked out.**
  This adds a check on every tick for a branch mugicrew never checks out.
  Integrate is the only writer, so integrate is the place to refuse.
- **Ignoring a container hold everywhere, integrate included.** Integrate's
  CAS would leave the human's tree stale. A commit from that tree would
  revert the integrate (C3).
- **Land refusing a held container, for symmetry with integrate.** Land
  writes nothing the hold could be stranded by, so a refusal would block a
  land with no safety gain.
- **Lifting a `held` integrate-conflict pause automatically when the hold
  clears.** It would add a second lifter and a clear check for a single
  `reason`. The human who made the checkout is there to press `t`.
- **Superseding ADR 0054/0055.** The mechanism (classify, count, pause,
  auto-clear) is unchanged. Only the branch it applies to has changed.

## Consequences

- `ForeignHeld` and the loop's `resume` work as they are once `route` returns
  `effort/<id>` for leaves. No new pause cause or event is added.
- The glossary's **Foreign hold** names the dispatch-cut branch and excludes
  container refs.
