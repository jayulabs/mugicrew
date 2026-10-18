# ADR 0124: mugicrew guards a swept member's publish in the Dispatch tail and backstops stragglers in the loop tick

Status: Accepted
Date: 2026-10-11
Builds on: ADR 0059 (Drafts), ADR 0043/0045 (sweep), ADR 0102, ADR 0103 (Consequences: R9).

## Context

An in-flight Checkpoint member keeps running after Zoro's `closeOut` sweeps its
Checkpoint, and its Dispatch tail still publishes. bd 1.3.0 has no guard line in
`bd batch`: a batch reopens a force-closed Draft, and attaches or opens a bead
under a closed container (`docs/research/bd-publish-batch-guards-against-closed-beads.md`).
The windows are W1–W5 in `docs/research/swept-checkpoint-member-late-publish-windows.md`.
Usopp's Approval and Jinbe's Review/Conflict spawns are direct opens outside
`Publish`, so no publish-side guard reaches them. Decided on grilling bead mc-v3vk.3.

## Decision

1. **Publish side.** The Dispatch tail checks `swept` (exported from
   `session`: the member, or its parent Checkpoint, is closed `obsolete:`) just
   before `e.publish`, for every placement. A swept member's tail:
   - drops the intent's edges and close (today only the close is dropped);
   - closes every Draft that is not `kind:signal` with an `obsolete:` reason,
     recorded as `draft.obsoleted` (member, Checkpoint, ids). This is a
     whitelist, so a new Draft kind defaults to closed;
   - publishes the `kind:signal` Drafts as usual (`draft.published`). An
     offshoot about pre-existing code is still real; a stale one is triaged
     like any Signal.
   `hitl-run` (Surgery) needs no check: it publishes only Signals.
2. **Sweep-side backstop.** At the start of each loop tick, before candidates
   are computed, the loop retires stragglers of an `obsolete:`-closed
   Checkpoint:
   - every non-closed child, deferred Drafts included, is force-closed with
     that Checkpoint's `obsolete:` reason;
   - every open pivot whose every `blocks` target is closed is closed
     `superseded:`, as `closeOut` does.
   It finds candidates from the tick's existing open listing: an open or
   in-progress bead whose parent is absent from it has a closed parent, and only
   those parents are read. Each hit is recorded as `sweep.straggler` at warn
   level (Checkpoint, ids).
3. **Accepted window.** A Draft born between the publish-side check and the
   batch, or an Approval/Jinbe spawn after the sweep, is live for at most one
   tick. Dispatch runs only inside a tick, after the backstop, so it is never
   dispatched; the TUI may show it briefly. Upgrade path: a bd batch status
   guard. No `Open()` re-check is added to Usopp's `approveIfClean` or Jinbe.

## Considered and rejected

- **Publish side only.** It leaves W5 (direct spawns) and the check-to-batch
  window open.
- **Backstop only.** Correct, but every swept member's Drafts would go live and
  be swept again a tick later: noise in the Inbox and the eventlog.
- **Per-Draft `bd update --if-status deferred` CAS.** It gives up the batch's
  all-or-nothing and still misses Drafts born after the check.
- **Delete a swept member's Drafts via the fail path.** It loses Signals
  silently and leaves no record of the Critiques and pivots.

## Consequences

- An orphan pivot (no gate, `discovered-from` a swept member, reachable only
  through the window) is not retired. `pivotTargets` already refuses it ("blocks
  no Checkpoint"), so a human dismisses it once. It is a `ponytail:` ceiling.
- A live child under a Checkpoint closed by a land (not `obsolete:`) is left
  alone: a different bug.
- `sweep.straggler` counts how often the window fires, which says whether the
  ceiling matters.
