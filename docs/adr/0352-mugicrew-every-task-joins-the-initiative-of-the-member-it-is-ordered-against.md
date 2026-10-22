# ADR 0352: mugicrew every Task joins the Initiative of the member it is ordered against

Status: Accepted
Date: 2026-10-22
Amends: ADR 0113 (task-production-and-ordering-against-checkpoint-lands) §2; ADR 0117 (task-tui-surface-and-no-human-front-door) §3; ADR 0128 (initiative-container-membership-and-owner) §6; ADR 0133 (initiative-lifecycle-drain-cancel-withdraw) §8; ADR 0221 (require-initiatives-gates-surgery-release-and-reconcile…) §3 and its Considered Options reason for Tasks; ADR 0342 (withdraw-takes-a-task-and-a-chains-withdraw-sweeps-the-tasks-ordered-after-it) §1.

## Context

A Task was free-standing: never in an Initiative, so work ordered against a
member's land sat under No Initiative, outside the effort it served. ADR 0221
exempted Tasks because a member Task would hold the drain past the last land.
The Chart "Related beads join their cause's Initiative" reversed that: a Task
ordered against a member is part of that member's effort, and a drain that
waits for it is correct. ADR 0347 (placement of an ordered offshoot member),
ADR 0348 (a filing with no plain member to climb), ADR 0349 (releasing a member
Task) and ADR 0350 (`task new`) settled the details; this ADR records the core
rule and the text it amends.

## Decision

1. **Every Task joins the Initiative of the member it is ordered against, at
   filing and always.** It is a direct child of the Initiative, never a Chart
   child, Checkpoint member or Spec child (ADR 0113 §2 narrowed: the Task is
   still no member of the spine, but it is an Initiative member).
2. **Follow-up and prerequisite offshoots join their cause's Initiative**
   (ADR 0347), amending ADR 0128 §6 ("offshoots are born outside"). Offshoots
   ordered against nothing, Latches and Pivots stay outside.
3. **A member Task keeps the Initiative under way and counts in the drain.**
   ADR 0133 §8's cancel closes it with the member chains; ADR 0221 §3's
   exemption and its rejected-option reason are withdrawn, because holding the
   drain is the intended behavior. Release stays possible under Require
   Initiatives (ADR 0349).
4. **A Task with no member to order against stays outside.** The same holds for
   one ordered against members of two Initiatives or a closed one (ADR 0348,
   ADR 0350).
5. **Going forward only.** Tasks already free-standing are not migrated, and a
   later Chart adopt does not carry the Tasks its session drafted earlier.
6. **Filing is `mugicrew task new`** (ADR 0350), which amends ADR 0117 §3's
   "no `task new` verb". Withdrawing a Task (ADR 0342 §1) is unchanged, except
   that the Task is no longer "unparented".

## Considered Options

- **Keep Tasks free-standing and group them in the TUI.** Rejected: the drain
  and the rollup would still miss them.
- **Join the Chart, Checkpoint or Spec.** Rejected: a Task is off the spine and
  its order is by `blocks` edges (ADR 0113).
- **Migrate existing Tasks.** Rejected: their orders were not recorded as
  membership, so a guess would misplace them.
