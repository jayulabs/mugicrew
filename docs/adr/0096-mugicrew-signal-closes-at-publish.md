# ADR 0096: mugicrew a charting session's Signal closes in its publish batch, not at reconcile

Status: Accepted
Date: 2026-10-07
Amends: ADR 0059 §5–§6, ADR 0085 §3–§4

## Context

On both Signal legs (Signal→Chart, Signal→Spec fast lane), `mugicrew reconcile`
closes the Signal partway through the charting session while its successor is
still a Draft. The session's clean exit then publishes the Drafts with no close.
A drop, a CLI relaunch (`signal triage`) or a gone-then-relaunch deletes the
Draft and the edges carried onto it, which leaves the Signal closed with no
successor, its dependents ready, and the TUI refusing to relaunch it. Chart→Spec and Spec→Checkpoint have no such gap, because they close in the
publish batch (`CloseOnPublish`). Decided in grilling.

## Decision

1. **Close at publish.** `reconcile` no longer closes the Signal on either leg.
   It still spawns the successor Draft, cuts `chart/<id>` or writes the `Spec:`
   anchor, and carries. The session's clean-exit publish (`hitl-run`'s
   `PublishDrafts`, and `runInteractive` for `signal triage`) closes the Signal
   in the same `bd batch` that opens its Drafts, which is the `CloseOnPublish`
   shape.
2. **The graph decides.** The batch closes the bead only when it is a Signal
   with a caused-by Chart or Spec among its Drafts (`bd.SuccessorOf`). A
   session that exits without a decision closes nothing. REJECT (`bd close`)
   and DUPLICATE (`bd supersede`) are agent closes with no successor Draft, and
   are unchanged; a batch close of an already-closed bead is a no-op.
3. **The carry stays at reconcile.** It is added onto the Draft, at reconcile,
   before the successor publishes, instead of "just before the close"
   (ADR 0085 §3). It stays outside the batch (ADR 0085 §4). Dependents are
   gated by both the open Signal and the deferred Draft. A drop deletes the
   carried edges with the Draft, and the dependents' original edge to the
   still-open Signal keeps them gated.
4. **Every session end leaves the Signal open and re-chartable.** That covers
   drop, live drop, gone-then-launch (the TUI now reaches `Start`'s
   `DropDrafts`), CLI relaunch, crash, and a failed publish (atomic: Signal
   open, Drafts deferred, pane kept for a resume or a drop). This amends
   ADR 0059 §6 for Signal sessions: deleting the Drafts no longer strands
   anything.
5. **No special state in between.** Between reconcile and publish the Signal
   is open and unclaimed, and the loop, `bd ready` and the TUI treat it like
   any other Signal with a live session.
6. **A re-run after a drop** spawns a successor with a new id. On the fast
   lane it writes a fresh `Spec: <new id>` anchor on the current mainline. The
   dropped Spec's anchor stays on mainline, inert: no reader keys on a Spec
   that no longer exists, and it is never adopted (it would pin the new Spec
   to a stale base) nor marked with a revert. The anchor is still written at
   reconcile, before the publish, so a visible Spec always has its anchor:
   a git commit and a `bd batch` cannot be atomic, and an open Spec with no
   anchor fails Zoro's `checkpointBase` hard.
7. **The prompt.** `charting.md` says mugicrew closes the Signal at publish,
   so it is still not the agent's to close (ADR 0059 §5). `triage.fastlane`
   is logged once the anchor and carry finish.

## Considered and rejected

- **Keep the reconcile close and reopen on drop.** Every path that loses the
  Draft (`Drop`, `DropDrafts`, fresh launch) would have to reopen the Signal,
  and gone-via-TUI and crash-without-drop never reach those paths. That repairs
  a close after the fact instead of not making it.
- **Reconcile records a close intent for the wrapper.** It adds state with its
  own lifecycle, while the graph (does a caused-by successor Draft exist?)
  already says the same thing.

## Consequences

- A hand-run `reconcile <signal>` outside a session (out of scope) now leaves
  the Signal open with a Draft nobody publishes, instead of closed with a
  stranded Draft.
- A dropped fast-lane session leaves an empty `Spec: <id>` commit on mainline
  for a Spec that no longer exists. `triage.fastlane` logs its sha; nothing
  tracks orphans.
- A dropped Chart-leg session's `chart/<id>` branch is still left behind; that
  is an offshoot Signal. Settled by Spec "Draft deletion also deletes the
  Draft's unworked container branch".
