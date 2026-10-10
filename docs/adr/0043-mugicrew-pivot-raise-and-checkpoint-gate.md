# ADR 0043: mugicrew pivot raise contract and Checkpoint gate

Status: Accepted
Date: 2026-10-04

Amends: ADR 0005 §2 (what a pivot carries), §4 (the gate and the confirm sweep);
ADR 0019 §7 (retires the `human`-label placeholder).

## Context

ADR 0005 §4 has a live pivot block "the Checkpoint's remaining open Builds" with
per-Build ordering edges. The pivot surface inventory
shows these edges miss anything
spawned after the raise (Critique, Conflict, Review, Approval), the Build
already running, and Jinbe's land candidacy. Two gates also run inside an effort
and never read `bd ready`: Franky's `readyForReview` and Usopp's
`approveIfClean`. Separately, the `human`-label placeholder showed that
a pivot signalled by the agent alone is fragile. Decided in
grilling.

## Decision

1. **Gate = one `blocks` edge on the Checkpoint container, plus a "no open
   pivot" predicate** in `readyForReview` and `approveIfClean`. Per-Build edges
   are dropped. A blocked container keeps all its current and future members and
   itself (so Jinbe) off the ready frontier; the predicates hold the two
   in-effort spawn gates.
2. **The gate edge is the index.** A Checkpoint's live pivots are its direct
   `blocks` edges filtered to open `kind:pivot`. The pivot stays an offshoot
   (discovered-from the raising bead), not a member; no second edge.
3. **Split raise.** The agent creates the pivot bead: title, evidence, the
   unfinished FR, and `discovered-from` the raising bead. Go (Franky `afterRun`,
   Usopp's equivalent) then finds open pivots discovered-from that bead and
   idempotently adds the Checkpoint `blocks` edge and a `related` edge
   pivot → Spec, before the normal close. That pass spawns no Review/Approval.
4. **The raising bead closes normally**, reason `pivoted: <pivot>`. Its
   unfinished FR lives only in the pivot body: confirm/escalate re-derives it in
   the corrected Spec; dismiss (and amend, if kept) refiles it as a Critique on
   the same Checkpoint.
5. **An in-flight Build at raise time finishes.** No edge can stop it; the
   predicate stops it spawning a Review. Its commits stand on dismiss, are
   abandoned with the branch on confirm (ADR 0005 §5).
6. **Usopp dedupe:** the Checkpoint's open pivots (same read as §2) cover their
   finding. Nothing is refiled as a Critique or second pivot. After a dismiss
   the refiled Critique is the cover.
7. **Dismiss (and amend) cleanup = close the pivot.** Closing lifts the block;
   the inert edge stays for lineage. No `bd dep remove`.
8. **Confirm/escalate sweep:** supersede old Spec → new Spec (and old Chart →
   new Chart on escalate); close the old Checkpoint and **every** open member
   (Build, Critique, Conflict, Review, Approval) as obsolete; close the pivot as
   confirmed. The `Supersede` helper removes a pre-existing same-direction
   non-`supersedes` edge first. An in_progress member is closed out from under
   its running effort; the effort's close/succession treats an already-closed
   bead as done, not as an error.

## Consequences

- One edge to add, nothing to remove: dismiss is a single close with no
  partial-failure window.
- A Checkpoint with an open pivot is fully frozen — dispatch, Review, Approval
  and land all wait — which is what makes the `human`-label failure modes impossible:
  no parked AFK bead (the raiser closes), no unsurfaced gate (pivot is HITL in
  the Inbox), no re-filed finding (dedupe sees the pivot).
- Franky/Usopp effort tails must be idempotent against a bead closed by a
  concurrent sweep.

## Alternatives considered

- **Per-Build edges (ADR 0005 as written).** Rejected: miss later spawns, the
  running Build and the land gate.
- **Container edge only.** Rejected: the in-effort spawn gates don't read
  `bd ready`.
- **Pivot as a Checkpoint member.** Rejected: a second edge that can drift from
  the gate; the gate edge already finds it.
- **Agent writes all edges / Go writes the whole pivot.** Rejected: the first
  repeats the fragile agent-only signal; the second loses the agent's evidence.
- **`bd dep remove` on dismiss.** Rejected: extra step, loses lineage, buys
  nothing since closing lifts the block.
