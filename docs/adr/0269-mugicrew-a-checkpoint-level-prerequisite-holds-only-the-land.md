# ADR 0269: mugicrew a Checkpoint-level prerequisite holds only the land

Status: Accepted
Date: 2026-10-17

When an offshoot Signal blocks the cause's Checkpoint as a discovered prerequisite,
only the Checkpoint's land waits; its Builds, Critiques and Review keep running.
Today bd hides a blocked Checkpoint and all its members from `bd ready`, and
`Readmit` (`loop/loop.go`) re-admits only a Checkpoint whose every open blocker is
`kind:task`, so a Signal hold freezes the whole Checkpoint for the prerequisite's full
Signal→Chart→Spec→Checkpoint course
(docs/research/container-blocked-by-outside-signal-course-effects.md §2–3, S1–S3).
That buys nothing: the prerequisite need only land before this code lands. So
`Readmit` treats a Checkpoint as land-held when every open blocker's kind is in an
allow-list: task, signal, chart, spec, checkpoint, initiative (the kinds an offshoot's
carried edge can take). The land itself stays held by the existing any-blocker guards
in `CheckpointReady` and Jinbe's `reconcileCheckpoint`. A mixed Task+Signal hold is now
land-only too (fixes S3).

This deliberately differs from ADR 0268 (a-chart-level-prerequisite-gates-the-whole-chart…):
a held Chart still freezes whole, because its remaining work is charting the Spec, not
code ready to build.

## Considered Options

- **Accept the whole-Checkpoint freeze** (as for Charts): no code, but idle Builds for days.
- **Route the edge through a before-land Task**: `Readmit` unchanged, but an extra bead
  per prerequisite that something must close.
- **Deny-list (any blocker but `kind:pivot`)**: rejected; an unknown future blocker kind
  should freeze (bd's native behaviour) and be noticed, not silently let Builds run.

## Consequences

- A pivot (ADR 0043 (pivot-raise-and-checkpoint-gate)), or any kind outside the list,
  among the open blockers still freezes the whole Checkpoint.
- Rollbacks stay Task-only: no offshoot edge targets one.
- Accepted: a Landing claimed before the edge lands past it (`reconcileLanding` reads no
  blockers, research L2), as Task holds already allow; a Review/Approval may go stale when
  the prerequisite lands and forces a rebase.
- The `loop/loop.go` comment "parent-child edges never gate `bd ready`" is corrected: it
  fails for a blocked parent.
- Out of scope: triage DUPLICATE/merge `bd supersede` releasing the container (research
  L3), split as its own Signal.
