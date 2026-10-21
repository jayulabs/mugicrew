# ADR 0322: A rerouted Signal is succeeded by its pivot or Critique through caused-by, and joins its host Checkpoint's spine

Status: Accepted
Date: 2026-10-21

Chart: Triage reroutes a Signal into a pivot or Critique on an in-flight
Checkpoint. Settles the grilling "Decide how a rerouted Signal's lineage is
recorded and how spine walkers read it". Reads the inventory
`docs/research/signal-reroute-reader-writer-inventory.md`.

A reroute folds a Signal's ask into an open, unlanded Checkpoint (the **host**)
as a pivot and/or a Critique, with no Chart or Spec. The lineage must live in the
graph, not only in the close reason. A reroute is the opposite of an offshoot:
the Signal's work does not split off, it joins work already in flight.

1. **The edge is `caused-by`.** Each pivot and each Critique a reroute files (or
   merges the Signal into) is `caused-by` the Signal, so it is the Signal's
   successor. Succession holds: the Signal closes into a visible successor, and
   readers that already key on `caused-by` (`SuccessorOf`, the publish close,
   reconcile's `refuseBeside`) widen by kind instead of learning a new edge.
2. **The Signal joins the host's spine** as a second Signal root. This is the
   one named exception to ADR 0281
   (mugicrew-a-retrospective-s-spine-follows-succession…)'s finding of one Signal
   per closure. The retrospective closure already follows `caused-by` both ways,
   so it needs no change: a retrospective on the host reads the Signal's triage
   session (how a defect got past Review), and one on the Signal returns the
   host's spine and is refused until the host lands.
3. **`stats --spine` counts it as a stage.** Amends ADR 0284
   (mugicrew-the-stats-spine-walk-counts-pivots…): a rerouted Signal gets a row
   with KIND `signal`, right after the host Checkpoint's row and ahead of its
   pivot rows. Several sort by creation time. The walk reaches it from its pivot
   or from its Critique's parent Checkpoint, and from the Signal reaches the host.
4. **Carry goes to the host Checkpoint.** Just before the Signal closes, its
   outgoing ordering (ADR 0085 (blocking-edges-carry-forward-on-succession)) is
   carried onto the host Checkpoint, so a dependent waits for the fix to land, as
   chained carries do (ADR 0085 §8). A dependent that is the host or its member
   is a `hierarchy` skip. The Signal's open Latches carry onto its `caused-by`
   successor (the pivot or Critique), as ADR 0223
   (an-orphaned-latch-closes-and-a-latch-carries…) says.
5. **Goto needs no change.** The Signal shows its pivot and Critique as
   successor rows (ADR 0271 (the-goto-menu-reaches-discovered-from…)), and each
   of them has `c` back to the Signal.
6. **An Initiative-member Signal is not rerouted.** It takes one of the other
   triage outcomes. Membership passes only through `parent-child` at spawn, and
   neither a pivot nor a host Critique can carry it, so the Initiative would
   drain or never count the member landed.

## Considered Options

- **`discovered-from`, matching the pivot raiser convention.** Rejected: it
  calls the rerouted work an offshoot, the opposite of what happened, and every
  `caused-by` reader (publish close, reconcile guards) would need its own reroute
  branch.
- **Keep the Signal a one-row spine, as a duplicate's is.** Rejected: it hides
  the Signal's triage cost and transcript from the spine its work landed on.
- **Carry onto the pivot or Critique, or nothing.** Rejected: dependents would
  be released when the pivot resolves or the Critique closes, before the fix
  lands.
- **Allow a member Signal and follow the edge for Initiative drain and
  progress.** Rejected for now: it makes the Initiative's state depend on a
  spine it does not own.
