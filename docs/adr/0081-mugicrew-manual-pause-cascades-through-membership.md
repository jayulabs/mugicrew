# ADR 0081: mugicrew manual pause cascades through membership — a bead with an ancestor carrying `paused:manual` is out of AFK dispatch candidacy; loop-written causes stay local

Status: Accepted
Date: 2026-10-06

## Context

ADR 0055 §3 limits candidacy to a bead's own `paused:*` labels: pausing a
Chart or epic doesn't cascade. The pause-controls Chart wants a paused
Chart or epic to take its members out of dispatch. The cascade must be read
from the ancestor and must not be written onto each descendant (that option
was rejected in triage). The research found:

- Membership is the `parent-child` edge only. A Chart's Spec and Checkpoint
  are `caused-by` successors, not members.
- Live depth is ≤ 1, though bd allows deeper chains.
- Loop `candidates()` and TUI run-now are the only AFK dispatch paths.
- bd itself ignores `paused:*` labels.

The grilling settled the rule.

## Decision

1. **Only `paused:manual` cascades.** A bead is *inherited-paused* when any
   ancestor, at any depth over `parent-child`, carries `paused:manual`. The
   loop-written causes `foreign-hold` (ADR 0055) and `failing` (ADR 0078)
   describe the container's own branch or effort. They stay local to the bead
   that carries them.
2. **Membership only, not lineage.** The cascade follows `parent-child` and
   never `caused-by`. A paused Chart already can't reconcile (own pause, ADR
   0055 §3), so its Spec is never born. To stop a Checkpoint that already
   exists, pause that Checkpoint.
3. **Candidacy and run-now refuse it.** Loop `candidates()` skips an
   inherited-paused bead in every arm, as it skips an own `paused:manual`.
   Run-now (`t`) refuses it too (ADR 0055 §6 parity), with a hint that names
   the paused ancestor. Undoing it takes `p` on that ancestor.
4. **No per-member override.** `p` on a descendant toggles only that bead's
   own `manual`. To run one member of a paused container, unpause the
   container and pause the siblings. There is no exemption label.
5. **In-flight efforts run on** (ADR 0055 §7 unchanged). An inherited pause
   governs candidacy only. Stopping a running member is the per-effort abort
   verb's job.
6. **Same paths as own pause.** HITL launch (`l`) and `mugicrew reconcile <id>`
   check no pause today, and they don't start checking an inherited one.
   They are explicit human acts on a named bead.
7. **Nothing is written to descendants.** The cascade is computed when
   candidacy runs. Lifting the ancestor's `manual` restores every member at
   once. The pause-controls Spec chooses how the ancestor's labels are read
   (one paused-label list per tick, or the full graph).

## Considered and rejected

- **Every cause cascades.** A Chart whose reconcile failed three times
  (`failing`) says nothing about whether its research members should run. A
  Checkpoint's branch hold already blocks its members through ADR 0068.
- **Cascade along `caused-by`.** This would make pausing a Chart freeze its
  already-running Builds two hops downstream, which is surprising and
  redundant with pausing the Checkpoint.
- **One hop only.** It is cheaper today, but it is wrong as soon as an epic →
  task → subtask chain appears.
- **Run-now overrides an inherited pause.** This breaks ADR 0055 §6: undoing
  a manual pause takes a deliberate second act.
- **A per-member exemption label.** This adds a second state that nothing
  reads today.
- **A bd `blocks` edge on the container** (research §4.2). It also removes the
  container from its own arms, needs a blocker bead, and relies on
  undocumented bd behaviour.

## Consequences

- ADR 0055 §3's "does not cascade" and ADR 0057 §6's status line "children
  are unaffected" are superseded for `manual`. Both ADRs carry pointer
  amendments.
- A Chart or Checkpoint whose members are inherited-paused doesn't drain while
  they stay open. That is intended: the container is paused.
- How the TUI renders an inherited pause on a member row is still open
  (pause-controls Chart).

> **Amended by ADR 0158:** `paused:context` cascades through membership like `manual`; the walk runs only when a known bead carries one of them.
