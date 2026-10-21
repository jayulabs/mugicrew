# ADR 0284: the `stats --spine` walk counts pivots and Rollbacks as stages and skips duplicate Signals

Status: Accepted
Date: 2026-10-17

Chart: Session cost and Agent time rolled up over a bead's subtree. Settles the
grilling "Which beads the spine walk counts as stages: pivots, Rollbacks,
duplicate Signals". Fills in what ADR 0283
(mugicrew-stats-spine-rolls-up-one-row-per-stage-in-spine-order) left to the walk.

A pivot or a Rollback has no parent and is reached by no succession edge, and
its session is logged as `bead=<it>, parent=""`. So a walk over `caused-by` and
parent edges alone would leave its cost out of every rollup. Therefore:

1. **Pivots are stages.** Every `kind:pivot` that `blocks` a spine Checkpoint
   counts, whatever its outcome. It gets its own row with KIND `pivot`, placed
   right after that Checkpoint. Other blockers (Jira latches, Tasks) don't count.
2. **Rollbacks and their successors are stages.** Going back, the walk steps
   successor Checkpoint → `caused-by` → Rollback → `related` → target
   Checkpoint (as `SpecOf` does). Going forward, it steps target Checkpoint →
   reverse `related` to a `kind:rollback` → its `caused-by` successor. The
   Rollback gets a row with KIND `rollback`, placed between the two Checkpoints.
   This matches ADR 0048 (mugicrew-rollback-successor): one Spec's lineage.
3. **`supersedes` is followed only between same-kind stages other than
   Signals** (Spec→Spec, Chart→Chart, Checkpoint→Checkpoint). Signal→Signal
   duplicates are skipped both ways: a carrier's spine leaves out its
   duplicates, and a duplicate's spine is just itself.
4. **A non-stage bead** (a defog bead, Critique, Build, Landing or Approval)
   takes the spine of its nearest stage ancestor by parent edges. With no stage
   ancestor (an Initiative, ADR 0135
   (mugicrew-initiative-is-a-branchless-container…); a free-standing Task; a
   discussion bead), `--spine` exits 2: "X is not on a spine".
5. **A rerouted Signal is a stage on its host's spine.** A pivot or Critique that
   is `caused-by` a `kind:signal` (a Signal-triage REROUTE) puts that Signal on
   the spine of the Checkpoint it serves: a pivot through its `blocks` Checkpoint,
   a Critique through its parent Checkpoint. It gets a row with KIND `signal`
   right after that host Checkpoint and ahead of the host's pivot rows, several
   ordered by creation time. `--spine <signal>` returns the host spine. The
   exclusion in item 3 stays for the walk's own root.

## Considered and rejected

- **Folding pivot or Rollback cost into the Checkpoint's row:** this changes
  what a stage row means ("stage bead plus its subtree").
- **Following every `supersedes`:** one carrier can absorb several Jira
  re-intake duplicates, which are separate intakes, not stages of its work.
- **Falling back to the subtree when there's no stage ancestor:** a silent
  fallback would hide that the bead isn't on a spine.
