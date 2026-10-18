# ADR 0145: A Landing is one pinned land attempt, obsoleted on conflict or advance

Status: Accepted (amended by ADR 0185)
Date: 2026-10-12

Chart mc-9kvd (Landing bead kind with pluggable landing strategies). Settles
grilling mc-9kvd.3 on the Landing's lifecycle beyond the happy path. It builds on
the Survey: a `landing` is a Jinbe-owned leaf member of its container (a Checkpoint
or a Chart), and it is spawned at today's land gate. Inputs are in
docs/research/landing-kind-and-land-strategy-touchpoints.md. This ADR amends
ADR 0004 §2/§4, ADR 0011 §1–2 and ADR 0016 §7 where they treat the land as a
bead-less step. ADR 0113 stands unchanged.

## Decision

1. **One Landing is one land attempt at one commit.** The Landing carries a
   `Commit: <sha>` body line:
   - on a Checkpoint, copied from the Approval it succeeds, which is the head at
     spawn;
   - on a Chart, the branch head at spawn.

   The land lands exactly that commit. If `refs/heads/<branch>` no longer equals
   the pin when the Landing runs, it does not land. It closes
   `obsolete: branch advanced`, and the container's gate takes over:
   - on a Checkpoint, `approvedAtHead` fails, so a new Review runs, then a new
     Approval, then a new Landing;
   - on a Chart, the next drained-and-stamped pass spawns a new Landing at the
     new head.

   Land's own in-operation pin and re-check (ADR 0102 §8) stay.
2. **A conflict closes the Landing.** On `*RebaseConflict` or `*ADRClash`, the
   Landing closes `obsolete: conflict`. Jinbe files the `conflict` (on a
   Checkpoint) or the `chart-conflict` (on a Chart) as a member of the
   **container**, never of the Landing, which is a leaf. Resolving the conflict
   moves the tip, so a fresh Landing spawns when the gate passes again. Open
   Landings never wait on a conflict.
3. **The trailer is the durable landed fact.** The `<Kind>: <id>` trailer on
   mainline is what makes a land true. A Landing's close mirrors that fact on the
   graph. Recovery has two layers:
   - **Primary: a re-dispatched Landing.** `AnchorIfAbsent` finds the trailer and
     returns its sha, and the Landing closes `landed: <sha>`.
   - **Backstop: the container's reconcile.** If it finds the trailer with a
     Landing still open (for example, a stranded claim), it closes the Landing
     first.

   The close order is always the Landing first, then the container. A container
   never closes over an open Landing.
4. **Succession.**
   - On a Checkpoint, the Landing spawns in succession to the Approval, as the
     Approval does to its clean Review, and it inherits the Approval's pin.
   - On a Chart, there is no predecessor bead (`chart:defogged` is a label), so
     the Landing spawns as a plain child pinned to the head.
   - Tasks ordered `before: land` keep blocking the **Checkpoint** (ADR 0113 §4).
     The gate that spawns the Landing includes the open-blocker check, so no
     Landing exists while such a Task is open.
   - `after: land` Tasks stay blocked by the Checkpoint. They are released when
     it closes, right after its Landing.
5. **Chart landed-time order.**
   1. Drained and stamped → spawn the Landing. Chart drain skips an open
      `landing` child.
   2. Jinbe lands, then closes the Landing.
   3. Nami's next pass sees the `Chart: <id>` trailer on mainline **and** no
      open Landing. It then spawns the Spec, runs speccing, and closes the
      Chart.

   The gate keys on the trailer, so a crash between the land and the Landing's
   close cannot stall the Chart: §3's backstop closes the stranded Landing.

> **Amended by ADR 0185:** under `land_strategy=gitlab` a Landing is one attempt
> at one pin, but the Merge Request outlives it: a later Landing reuses the
> container's open MR, and a Forge-side close obsoletes the Landing on a Task.

## Considered Options

- **A Landing that stays open, blocked by its conflict, and retries.** Rejected.
  Resolving a conflict always moves the tip. On a Checkpoint that already forces a
  new Review and Approval, so the open Landing would be pinned to a commit nobody
  approved. Close-and-respawn mirrors the Approval's obsolete-and-respawn.
- **`before: land` Tasks blocking the Landing.** Rejected. Zoro adds those edges
  when he cuts the Checkpoint, before any Landing exists, and the Checkpoint's
  open-blocker gate already keeps the Landing from spawning.
- **The Landing's close as the landed fact.** Rejected. Every reader keys on the
  mainline trailer (Zoro's `LandedBase`, Jinbe's roll-forward, `deleteLanded`),
  and a crash can separate the two.
