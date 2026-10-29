# ADR 0379: The eligibility verdict reads raw graph facts through one view interface

Status: Accepted
Date: 2026-10-28

Chart One eligibility module for loop dispatch and TUI run-now (mc-3vp53) runs one pure
verdict over a live bd/git adapter (loop tick, run-now press) and a snapshot adapter (the
TUI's loaded graph, for the row hint). ADR 0377 (loop-and-run-now-eligibility-differ…)
allows staleness as the only row-vs-press difference, and ADR 0378
(an-eligibility-verdict-names-one-cause…) fixes the legs and holders. The human settled
the seam between them: **one Go interface in package `loop` of raw graph reads. The
verdict computes every leg itself over the shared `…Over` core, so the two adapters can
differ only in what they read and how fresh it is.**

## The view

- `Children(id)`: a Container's children, open and closed, as whole beads.
- `Efforts()`: the effort-ref set.
- `Landed(b)`: the landed trailer's sha, plus the foreign sha when another Bead Graph
  landed it.
- `CheckpointTip(id)`: the `checkpoint/<id>` tip. The verdict compares it with the
  Approval's pin.
- `Blockers(b)`: the raw blocker beads with their edge type. The verdict filters them
  the live way (`Open()`, `blocks` edges only), so both adapters agree by construction.
- `Bead(id)`: resolves an inherited-pause walk hop that the adapter's preloaded set
  misses.

The frontier is not on the view. It is a verdict argument the caller has already
widened: the loop by Readmit and straggler retirement, run-now by Readmit and force
(ADR 0377 §4).

## Lifetime and batching

The live adapter is created once per tick or press. It memoises `Children`, `Landed` and
`CheckpointTip` by Container id for that lifetime, which cuts the Checkpoint arm to one
`bd children`. The effort set is a constructor-supplied `func() map[string]bool`. The
loop passes `tickEfforts`. The press passes its own `memoEfforts(bd.EffortRefsAcross)`,
which ends its double read. The snapshot adapter wraps `graphData` and does no I/O.

## Failure

A failed live read returns an error beside the verdict, never a cause: the list of 16
causes stays closed, and a cause means the graph says no. Callers keep today's policy:
the loop fails the tick and the press fails the press. The snapshot adapter never errors.

## What the snapshot refresh adds

So that the row differs from the press only by staleness:

- **Fast tier (3 s):** `bd ready --json -n 0`, the exact frontier, not one derived from
  the snapshot.
- **Slow tier (30 s):** one batched trailer `git log --grep` per repo. The foreign sha is
  dropped, because the row never sees `foreign-landing` (ADR 0187
  (a-land-trailer-carries-its-graph…) amendment). Without this read, the row would
  refuse a landed-trailer roll-forward as `landing-open`, and since the row gates the
  press, the press would be unreachable.
- **On a miss:** one `ListByID` for walk ancestors missing from the snapshot, on the
  drop-out/slow path, instead of reading a miss as "not paused".
- Effort refs, `checkpoint/*` tips and the trailer read stay on the slow tier. The row
  may be up to 30 s stale, and the press re-checks live.

## Considered Options

- **One method per leg** (`DrainBlockers`, `LandingOpen`, …). Rejected: each adapter
  would compute its own legs, which moves the drift into the adapters.
- **A struct of func fields** like the loop's `var` seams. Rejected in favour of an
  interface with two named implementations.
- **The frontier as a view method.** Rejected: straggler retirement and force shape it
  outside the verdict.
- **Per-bead read errors (the loop skips the bead) or a `read-failed` cause.** Rejected:
  that changes loop behaviour beyond this Chart, and it would open the closed cause list.
- **A snapshot-derived frontier, "not landed" in the snapshot, or a miss read as "not
  paused".** Rejected: each is an approximation, not staleness.
