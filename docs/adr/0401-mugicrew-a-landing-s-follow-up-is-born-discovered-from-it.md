# ADR 0401: mugicrew A Landing's follow-up is born discovered-from it

Status: Accepted
Date: 2026-10-29
Amends: the Hold Errand Born-with table of `apps/mugicrew/docs/bead-lifecycle.md` (Gaps entry 36's "by design" for the Landing-raised holds).

## Context

When a Landing does not land, Jinbe files a follow-up, but the follow-ups link
back to it in different ways (Gaps entry 23 of
`apps/mugicrew/docs/bead-lifecycle.md`). The gate-build and pipeline Critiques
are discovered-from the Landing. The Conflict and Chart-conflict have only a
parent edge, because `fileConflict` is never given the Landing. The Hold
Errands (a closed MR, a failed pipeline, a merge without its trailer) have only
`blocks` to the container and a key line in the body. So the graph names the
land attempt that raised a Critique, but not the one that raised any other
follow-up. Settled by the Survey of the Chart "Every follow-up a Landing files
links back to what raised it by one edge".

## Decision

1. **The principle.** A bead Go files because an attempt failed is born
   discovered-from that attempt. This ADR rules only on Landing follow-ups. The
   sibling Signals "Critique doors disagree on the offshoot edge to what raised
   them" and "Pivot doors record the host and the raiser differently" may cite
   it for their own doors.
2. **One edge: discovered-from the Landing**, written in the same create as the
   bead (`--deps`), never as a second write. It applies to every follow-up a
   Landing files:
   - the gate-build Critique and the pipeline Critique (unchanged);
   - the Conflict, whether the land hits it or the MR-open conflict arm does;
   - the Chart-conflict a Chart's Landing hits;
   - the Hold Errands for a closed MR, a failed pipeline (infra variant
     included) and a merged MR without its trailer. These keep their `blocks`
     edge to the container, their key line stays their identity, and the
     `related` re-file edge (ADR 0338 (mugicrew-a-hold-that-outlives…)) is
     unchanged.
3. **A follow-up no Landing raised carries no provenance edge.** That covers the
   pre-review probe's Conflict, a requested rebase (Conflict or Chart-conflict,
   filed by the human), the revert-pipeline hold on a Rollback, and the
   foreign-land hold. Its parent or the bead it blocks is its raiser, and its
   body variant (`Found before review:`, requested, the key line) says why. The
   supersedes edge from a requested Conflict to the real one that replaces it
   (ADR 0261 (mugicrew-a-requested-rebase-defers…)) stays.
4. **A Chart's pipeline failure keeps filing a Hold Errand**, where a
   Checkpoint's files a Critique. A Chart has no Franky and no Build to fix
   code, and a red pipeline on a Chart's branch means mainline or the CI config
   is broken. That is a step in the outside world for a human, not a defect in
   the branch. The Errand is discovered-from the Landing under §2.
5. **Going forward only.** Open Conflicts, Chart-conflicts and Hold Errands that
   have no edge back are not migrated.

## Consequences

discovered-from makes these beads offshoots of the Landing, and that costs
little. ADR 0375 (an-offshoot-whose-cause-is-in-an-initiative…)'s climb applies
only when an offshoot Signal is filed. A Conflict or Critique is placed by its
parent, and an Errand by its container (ADR 0352
(every-task-joins-the-initiative…)). ADR 0281
(a-retrospective-s-spine-follows-succession…)'s spine already holds a Conflict
through its parent, and it follows discovered-from only from a Rollback or a
Pivot, so the spine is unchanged. ADR 0271 (the-goto-menu-reaches-discovered-from…)'s
goto menu gains an `o` origin row on each follow-up, and an offshoot row on the
Landing for each one.

## Considered Options

- **caused-by the Landing.** Rejected: caused-by means succession. It feeds the
  goto menu's `c` key and the Retrospective spine. But the Landing is not
  succeeded: it closes obsolete and the container's next Landing follows.
- **related to the Landing.** Rejected: related has no direction, and it is
  already the edge a re-filed hold uses for its closed predecessor.
- **discovered-from the Approval or Review for a pre-review Conflict.**
  Rejected: the probe runs instead of the first Review, so there is often
  neither.
- **A Critique for a Chart's pipeline failure.** Rejected: see §4.
