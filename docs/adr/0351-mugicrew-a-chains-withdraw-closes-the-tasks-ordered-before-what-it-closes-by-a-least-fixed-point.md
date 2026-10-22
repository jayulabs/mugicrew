# ADR 0351: mugicrew a chain's withdraw closes the Tasks ordered before what it closes, by a least fixed point

Status: Proposed
Date: 2026-10-22

Chart mc-l8q8k (A chain's withdraw also closes the pre-land Tasks that hold a
Checkpoint it closes). To be amended into ADR 0342
(mugicrew-withdraw-takes-a-task…) §2 and ADR 0340 (mugicrew-x-dialog-hangs-swept-tasks…)
§2 by its Spec.

ADR 0342 §2 leaves open the pre-land Tasks a chain's withdraw was ordered after,
so they outlive the Checkpoint they held. Today's `LeftOpen` reads only a
closing Checkpoint's direct blockers (`bd/sweep.go`). It cannot see a Task
ordered before another pre-land Task, and the rules allow that shape
(`docs/research/pre-land-task-shapes-on-the-bead-graph.md` §2).

## Decision

1. **Backward rule.** On a chain's withdraw, an unclosed, published Task joins
   the final set when every unclosed bead it blocks is already in it. The rule
   reaches through Tasks: if A blocks B and B blocks a closing Checkpoint, both
   close. It applies before any bead in the set, not only Checkpoints, so it
   also covers a Task before a swept Task, and a Hold Task on a withdrawn Chart
   or Rollback.
2. **Least fixed point with the forward sweep.** The forward rule (a Task
   blocked by an in-set bead joins, ADR 0342 §2) and the backward rule apply in
   turn until neither grows. The withdraw closes only what it is forced to: a
   Task that also blocks a bead the withdraw doesn't close stays open, even when
   closing it would sweep that bead too.
3. **Holding something outside the set leaves a Task open.** That includes
   another unclosed Checkpoint. Such a Task keeps its `┄◂` left-open row (ADR
   0340 §2). A Task closed by the backward rule blocks nothing outside the set,
   so it never adds to `Releases`; a land hold is still released only by a
   forward-swept Task, as today.
4. **Hold Tasks get no exemption.** Jinbe's and the loop's Hold Tasks
   (`bd.SpawnBlockingTaskIfAbsent`) close by the same rule as Tasks from a
   Spec.
5. **Same close as a swept Task.** Reason `withdrawn: <head>`. A live Brook
   session on any of these Tasks refuses the whole withdraw as in flight (ADR
   0342 §5). A dead pane is killed (ADR 0342 §6).
6. **Only a chain's withdraw.** A Task withdrawn on its own gains no backward
   reach.

## Considered options

- **Direct blockers of a closing Checkpoint only (today's `LeftOpen`).**
  Rejected: a transitive-only predecessor is left open holding nothing that is
  still live, which is the gap this decision closes.
- **Close a Task that also holds an outside Checkpoint, and release that
  Checkpoint.** Rejected: that Checkpoint's Spec still wants the Task done before
  it lands, and withdrawing another chain shouldn't silently drop that gate.
- **Greatest fixed point.** Rejected: it would close live work that the
  withdrawn chain never ordered.

## How the dialog, the event and the CLI name it

7. **A before-closed Task reads as closing, on `┄◂`.** A Task the backward rule
   closes is *before-closed*. It is the counterpart of a *swept* Task (forward
   rule). This amends ADR 0340 §2 and §4 and ADR 0342 §4:
   - **Row.** It hangs on `┄◂` under the first in-set bead it blocks, drawn at
     normal weight with no tag, the way a swept `┄▸` row is drawn. A chained
     predecessor nests one step under the Task it comes before. `┄◂` alone no
     longer means left open. The dim style and the `◌` tag mark that.
   - **Left open.** A left-open row hangs on `┄◂` under any in-set bead it
     blocks, not only a Checkpoint. Its tag reads `◌ left open: also before
     <titles>` when wide (truncated like `⇠ also after`) and `◌+N` when
     narrow. `holds this land` goes away.
   - **Lead.** A chain head counts each non-empty group: "Closes N beads as
     withdrawn: M in the chain, K Tasks ordered after it and J ordered before
     it". A group with no beads is left out. A Task head's lead is unchanged
     (§6).
   - **Summary.** No change: `· N left open` counts only Tasks that stay open.
   - **Event.** `withdraw.done` gains `swept_before` (ids, in close order)
     beside `swept`, which stays forward-only. `left_open` now also covers
     Tasks held open by a bead that is not a Checkpoint.
   - **CLI.** `printSwept` prints `swept X (withdrawn: <head>; before Y)` for
     a before-closed Task, where Y is its first in-set blocked bead, and
     `left open X: also before Z1, Z2` (one line per Task, ids joined) in
     place of `left open X: holds C`.

   Considered: a new glyph (`◂┄`) or a `✕ before this` tag for a before-closed
   row, a merged "ordered around it" count, and folding before-closed Tasks
   into `swept`. All were rejected: the edge already gives the direction and
   the dim `◌` gives "stays open", and an event reader should tell the two
   rules apart without re-walking the graph.
