# ADR 0085: mugicrew blocking edges carry forward on succession — a predecessor's outgoing Ordering edges are added to its successor just before it closes

Status: Accepted
Date: 2026-10-07

## Context

A Signal, Chart or Spec is a *stage* that describes work, not the work. A human
who orders X after one ("P blocks X") means "X waits for this work to land". Each
stage closes into its successor as the course advances (ADR 0002, ADR 0035), and
bd treats a closed blocker as satisfied, so X reaches `bd ready` one stage too
early: during the charting session on the Signal legs, at the Builds' slicing on
Spec→Checkpoint, and at the obsolete close of a pivot sweep (ADR 0043).

bd has no built-in that moves blocking edges (`supersede`, `duplicate` and
`close` leave them). The Spec settles the rule.

## Decision

1. **The rule.** Just before a predecessor P closes into its successor S, every
   bead X that P orders along an outgoing `blocks` or `conditional-blocks` edge
   gains "X depends on S" with the **same type**. Incoming edges (Y orders P),
   `waits-for` and every lineage type are never carried. The carry is add-only:
   the old "X depends on P" edge stays as lineage, and nothing is removed.
2. **One seam.** `bd.Carry(pred, succ)` does the work: one
   `bd dep list <pred> --direction=up --json`, then one `bd dep add <X> <succ> -t
   <type>` per dependent. It is event-blind and idempotent (a same-type re-add
   exits 0 and counts as moved), so a re-run reconcile converges without a
   marker (ADR 0070 §1's claim serializes it).
3. **Five paths.** Each calls Carry immediately before the predecessor's close:
   - Signal→Chart (reconcileSignal, Chart leg), before `closeBead(sig)`.
   - Signal→Spec (reconcileFastLane), before `closeBead(sig)`.
   - Chart→Spec (reconcileChart), before `closeOnPublish(chart)`.
   - Spec→Checkpoint (zoro reconcileSpec), before `closeOnPublish(spec)`.
   - Pivot-obsoleted Checkpoint → corrected Spec (Confirm) or escalated Chart
     (Escalate), in the ADR 0043 §8 sweep before `closeOut(cp)`.
4. **Outside the publish batch.** The carry is direct per-pair `dep add` calls,
   not part of the ADR 0059 publish batch: the Signal legs' close is in no batch,
   and one refused pair in a `bd batch` would roll back the whole publish. Adding
   early is harmless — P is still open and gating — and a Draft (deferred)
   successor already gates X (research §1).
5. **Skips.** A dependent is skipped, not failed, when it is closed (no dead
   edge; deferred Drafts are carried), when it is S itself or S's parent-child
   ancestor/descendant (`hierarchy`), when the edge would close a cycle
   (`cycle`), or when the pair already holds a non-blocking edge (`type-clash`;
   bd keeps one edge per pair, and the carry never replaces lineage). bd keeps
   no trace of a skip, so X is left ungated by S in those cases.
6. **Failure.** A dep-list failure or any other dep-add error fails the carry
   and the reconcile returns **before** the close: P stays open, its dependents
   stay gated, and the next reconcile retries.
7. **The event.** Each carry records one eventlog event (`RecordCarry`):
   `carry.done` naming the predecessor, `successor`, `moved` ids and `skipped`
   `[{bead, reason}]` — at Warn when anything was skipped, Info otherwise — or
   `carry.failed` at Error with `err` and the `moved` that landed first. A carry
   that found nothing records nothing. The event is the per-pair warning; it
   shows in the TUI Logs tab only (no toast, status line or row marker).
8. **A Spec's dependents wait for the Checkpoint to land**, not for its Builds
   to be sliced: on Spec→Checkpoint the carried edge points at the Checkpoint,
   which closes on land. Chained carries compose, so an edge on a Signal ends up
   gating X until the Checkpoint lands.
9. **Pause and dismiss/amend need nothing.** Pause (ADR 0081) cascades along
   `parent-child` only and writes no edge, so a paused Checkpoint keeps its
   carried dependents gated. A dismissed or amended pivot leaves the Checkpoint
   open, and the pivot→Checkpoint gate is incoming, so nothing carries.

## Not covered

- **Hand `bd close` / `bd supersede`** on a predecessor, including the charting
  prompt's REJECT and DUPLICATE paths: they bypass mugicrew and bd has no hook.
  Hand `mugicrew reconcile` is covered — it runs the same code.
- **Rollback successor Checkpoints** (ADR 0048) and Review/Critique/Approval
  successors.
- **Backfill** of edges that already point at closed Signals, Charts or Specs.
- **Dropped-session stranding**: deleting a dropped session's Draft successor
  deletes its carried edges while the Signal stays closed.

## Considered and rejected

- **Move the edge (remove the old one).** It loses the lineage of why X was
  ordered, and `dep remove` inside a batch is unprobed.
- **Carry inside the publish batch.** One type-clash pair would roll back the
  Drafts' publish, and the Signal legs have no batch to join.
- **Fail the close on any refused pair.** One awkward pair would stall a course;
  the event names the skip instead.

## References

ADR 0002 (bead taxonomy), 0035 (Signal triage fast lane), 0043 (pivot raise and
Checkpoint gate), 0048 (Rollback successor), 0059 (Drafts and the publish
batch), 0070 (one bd write rule), 0081 (manual pause cascade). GLOSSARY
**Carry**.
