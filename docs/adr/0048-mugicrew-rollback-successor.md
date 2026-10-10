# ADR 0048: mugicrew Rollback successor — a new Checkpoint Jinbe seeds at the revert commit

Status: Accepted
Date: 2026-10-04

Amends: ADR 0047 (the "successor handoff" step), ADR 0002 (Jinbe stewards the
successor Checkpoint; a Checkpoint may be caused by a Rollback, not only a Spec).

## Context

ADR 0047 has Jinbe forward-revert a bad Checkpoint land and then "hand the work
on to a successor". The landed Checkpoint is never reopened, and its
`Checkpoint: <id>` trailer stays on main, so the work must re-land under a new
id. The old Spec is
closed, and `SpawnSuccessorIfAbsent(spec, checkpoint)` matches the closed old
Checkpoint, so it cannot hang a second Checkpoint off the Spec (§4–§5). This ADR
settles the successor's shape. Decided in grilling.

## Decision

1. **The successor is a new Checkpoint under the same Spec.** No
   Rollback-specific triage: a Rollback almost always means the code broke main,
   not the plan. If work on the successor proves the Spec wrong, an ordinary
   Pivot is raised on it (ADR 0043/0044).
2. **Edges.** The new Checkpoint is `caused-by` the **Rollback** (succession),
   spawned with `SpawnSuccessorIfAbsent(rollback, checkpoint)`, so it is
   idempotent per Rollback. There is no `supersedes` edge to the old Checkpoint:
   it landed and is history, not stale work being replaced. The Spec is reached
   by walking Rollback → `related` → old Checkpoint → `caused-by` → Spec.
3. **Jinbe does the whole recovery in one idempotent pure-Go pass**, with no
   agent: revert → spawn the Checkpoint → cut and seed its branch → spawn its
   Build → close the Rollback. Each step is skip-if-done, so a crash resumes
   cleanly (ADR 0003). Jinbe is the steward of this Checkpoint as well as its
   land handler.
4. **Base and seed.** `checkpoint/<new>` is cut **at the revert commit**, and its
   first commit is a **reapply** (`git revert <revert-sha>`). Reverting the
   commit just made cannot conflict, so seeding never needs a human. Drift from
   later lands is met at land time by the normal rebase and the Conflict path
   (ADR 0004).
5. **Re-entry at Build.** Jinbe spawns one Build as a member of the new
   Checkpoint. Its body is the Rollback's breakage evidence: "main broke because
   …; fix it". If the Rollback body is empty, the revert still happens and the
   Build says to reproduce the breakage on main at the target's land, then fix
   it. From there the normal loop runs: Build → Review → Approval → land.
6. **The Rollback closes at handoff**, once the successor Checkpoint, its seeded
   branch, and its Build exist. Re-landing is the successor's job.

## Consequences

- A Checkpoint may now be caused by a Rollback rather than a Spec. Readers that
  need the Spec (Builds, Review, any future `checkpointBase` use) must follow
  the walk in §2. Jinbe sets the base directly, so `checkpointBase` is not on
  this path.
- A successor that breaks main again is simply the target of another Rollback,
  which chains naturally.
- The seed is a single squash-sized commit. Per-commit history is not restored
  (it lives only in the reflog, about 30 days).
- This settles the Chart's "seeding mechanics" item (no prototype needed) and
  its "breakage evidence" item (the Rollback body becomes the Build body).

## Alternatives considered

- **A Zoro triage gate before cutting.** Rejected: it adds a HITL session to
  every Rollback and duplicates Pivot triage, which already covers the rare case
  where the Spec was wrong.
- **A fresh Signal.** Rejected: it discards a reviewed diff and repeats charting
  that was already done.
- **Caused-by the Spec through a new spawn helper.** Rejected: new code, and it
  makes the one-Checkpoint-per-Spec guard conditional.
- **Zoro creates the successor.** Rejected: it needs a handoff edge and a new
  routed kind for work that is pure plumbing.
- **Cut at main HEAD, a clean branch, or a reflog restore.** Rejected: seeding
  could conflict with nobody to resolve it; redoing the work wastes it; the
  reflog expires.
- **Straight to Review.** Rejected: a reviewer already missed this breakage
  once. A Build makes sure someone actually fixes it.
- **Keep the Rollback open until the successor lands.** Rejected: it needs a
  blocking edge and a later wake-up for no gain, since lineage is on
  `caused-by`.
