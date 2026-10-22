# ADR 0113: mugicrew Task — free-standing, filed by Zoro, a session or a human, ordered against Checkpoint lands by blocking edges

Status: Accepted
Amended by ADR 0352 (every-task-joins-the-initiative…) (§2: a Task joins the Initiative of the member it is ordered against).
Date: 2026-10-09

Amends: ADR 0013 §3 (gives the reserved `task` kind a place in the graph; its
owner and session are settled elsewhere on the Task Chart).

## Context

A Task is a HITL bead that walks a human through changing the external world
(the Task Chart). The mugicrew cutover needs Tasks ordered both after one
Checkpoint lands and before another lands (the loop restarts before the shims
land). A Checkpoint drains, then lands, only when its members are closed, and a
Checkpoint closes at land. bd refuses an edge between a bead and its own
ancestor, and Carry moves only a stage's outgoing ordering. Decided in grilling.

## Decision

1. **Producers.** A Task is filed by Zoro, from the Spec's Tasks section, when
   he slices; by a grilling or Chart session, as a Draft; or by a human from
   the CLI/TUI, with no Signal and no triage (the human is author and present,
   as in ADR 0091). Agents never file one mid-work; they file an offshoot
   Signal.
2. **Free-standing.** A Task is never a Chart child (ADR 0013 stands), never a
   Checkpoint member, never a Spec child. It is ordered only by `blocks` edges.
3. **After a land:** the Task is blocked-by Checkpoint X, and the land closes
   X, which releases it. Before X exists, a session blocks the Task on X's Chart
   or Spec, and Carry moves the edge onto X's land (a pivot carries the same
   way).
4. **Before a land:** Checkpoint Y is blocked-by the Task. The land check
   (`loop.CheckpointReady` and Jinbe's land leg) refuses while Y has an open
   blocker. Review and Approval still run; only the land waits. Carry does not
   move incoming edges, so only Zoro adds these, when he cuts Y.
5. **Task → Task** ordering is a plain `blocks` edge.
6. **The Spec's Tasks section** gives each Task's ordering relative to that
   Spec's own Checkpoint: `after: land`, `before: land`, and an optional
   `after: <Task>`. Ordering across Specs goes through Carry (§3).

## Considered Options

- **Task as a Checkpoint member.** Rejected: it can only say "before my own
  Checkpoint lands", and bd forbids an edge to its parent, so "after" is
  impossible.
- **Task as a Spec child.** Rejected: a Spec is a leaf and closes at slicing.
- **Agents filing Tasks as offshoots.** Rejected: it bypasses gated intake.

## Consequences

- The land check gains one guard: an open incoming blocker holds the land.
- A Task released by a land that is later rolled back has already run.
  Undoing an external step belongs to the Chart's failure/rollback fog.
