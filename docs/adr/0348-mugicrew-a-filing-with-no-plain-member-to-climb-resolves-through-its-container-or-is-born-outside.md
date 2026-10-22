# ADR 0348: mugicrew a filing with no plain member to climb resolves through its container, or is born outside

Status: Accepted
Date: 2026-10-22
Amends: ADR 0267 (an-offshoot-signal-is-filed-through-signal-offshoot…), its container rule; ADR 0222 (a-jira-latch-is-one-per-key…), a Latch's "any bead" target.

## Context

The Chart "Related beads join their cause's Initiative" settled one rule for
every Task: it joins the Initiative of the member it is ordered against at
filing. If that member belongs to two Initiatives, or its Initiative is closed,
the Task is born outside and the filing says why. The filing-path inventory
(docs/research/task-hold-task-and-offshoot-filing-paths-and-their-initiative-climb.md
§8) found paths where that member is not simply knowable at filing. The grilling
"How each filing path with no plain member to climb finds its Initiative, or is
born outside" settled each one. The human's Task-filing verb (G5) is settled
separately.

## Decision

1. **A Rollback's Hold Task climbs** Rollback → `related` rolled-back Checkpoint
   → `InitiativeOf`, as the Rollback successor does (`jinbe/jinbe.go`). A
   Rollback reopens a member's work, so that work is not done yet. A closed
   Initiative falls under the born-outside rule.
2. **A Spec Task resolves through the slice's Checkpoint**, even when it is
   ordered only `after:` another Task, or sits in a Task-only Spec (whose
   Checkpoint still exists until it closes empty after the carry). Slicing
   always knows the Checkpoint, so this is one hop. It matches the Task chain,
   because the earlier Task joined the same Initiative.
3. **An adopted `task: <id>` slicing entry is left where it is.** No filing
   happens, so the rule does not fire. Moving the Task is the human's job
   through `initiative adopt` (ADR 0128's adoption is a human act). Slicing says
   in its output that the Task stays outside. A Task in another Initiative is
   the settled two-Initiatives case.
4. **Session-drafted Tasks get the Initiative id as a template argument.** Go
   resolves it at launch (`InitiativeOf` on the Chart) for charting, grilling
   and promote-or-stamp sessions. The two-Initiatives and closed-Initiative
   checks therefore stay in one Go path and never in the agent's head.
5. **A later adopt of a Chart does not carry its Tasks.** Adopt stays a
   one-call reparent of the head, and Carry still never reparents
   (`bd/succession.go`). Going forward only, as with the Chart's backfill
   ruling. The human adopts those Tasks by hand.
6. **`signal offshoot` accepts a member Task as its cause.** It now climbs the
   Task's parent Initiative, which amends ADR 0267's rule that the container is
   the cause's parent only when that parent is a Checkpoint or Chart. A
   follow-up or prerequisite offshoot of a member Task joins that Initiative. A
   Task outside any Initiative, and a Discussion (off-spine), still have no
   container and file unordered, so they stay outside. Brook's offshoot, which
   files with no order edge, stays outside unless it starts passing `--order`.
7. **Stale-pin and stranded-commits Signals stay outside.** They are unordered
   by construction (ADR 0267). They repair lost commits rather than add to an
   Initiative's Done when, and making them members would hold an Initiative that
   may already have drained.
8. **A Latch never blocks an Initiative.** `mugicrew latch jira|mr` refuses an
   Initiative as a `--blocks` target and names the member to block instead.
   bd passes a block down to descendants, so a blocked Initiative would quietly
   freeze every member Task. The Latch belongs on the bead that is actually
   waiting.

## Considered Options

- **G1 born outside always.** Rejected: the Rollback's work is still the
  Initiative's, and the successor already climbs this way.
- **G2 resolve through the Task chain.** Rejected: it gives the same answer at
  greater cost.
- **G3 reparent on slicing.** Rejected: it would be an automatic adoption.
- **G4 the agent reads the Chart's parent.** Rejected: it is nondeterministic,
  and the edge-case checks would be duplicated in prompts.
- **G7 join a still-open Initiative.** Rejected: it would hold a drained
  Initiative for repair work.
- **G8 allow, or leave the verb alone.** Rejected: the cascade is a silent
  freeze.
