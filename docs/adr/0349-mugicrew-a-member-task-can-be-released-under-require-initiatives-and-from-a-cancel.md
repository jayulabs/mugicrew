# ADR 0349: mugicrew a member Task can be released under Require Initiatives and from a cancel

Status: Accepted
Date: 2026-10-22
Amends: ADR 0221 (require-initiatives-gates-surgery-release-and-reconcile…) §3, §4; ADR 0313 (x-dialogs-show-the-chain-as-a-tree…) §3–§5 (the cancel dialog).

## Context

The Chart "Related beads join their cause's Initiative" makes a Task a member of
the Initiative of the member it is ordered against. Filing it gives the human a
flag to keep it outside. ADR 0221 §3 exempts Tasks from Require Initiatives, but
`ReleaseChain` (`bd/initiative.go`) and the TUI `A` (`tui/graphforms.go`) refuse
any release while the setting is on. So a Task could be born outside but never
released later. `planRelease` (`withdraw/withdraw.go`) also locked every member
Task, and that lock was never shown, because the cancel dialog lists only chain
heads. Cancel's `--release` refused a Task whether or not Require Initiatives
was on. (See docs/research/initiative-lifecycle-with-member-tasks-and-offshoot-signals.md
§5.)

## Decision

1. **Release of a member Task is allowed under Require Initiatives.** The §3
   exemption covers release. `ReleaseRequiredWhy` refuses only non-Task
   members. The TUI `A` gate lets a Task through. `planRelease` never locks a
   Task. §4's refusal stands for every other member. Keeping a Task outside at
   filing and releasing it later are the same freedom. Release is also how a
   human frees an Initiative whose drain is held by a Task that is blocked for
   good.
2. **Cancel can release a member Task, whether or not Require Initiatives is
   on.** Each unclosed member Task gets its own row in the cancel dialog, with
   the close/release choice defaulting to close. `--release` accepts a member
   Task as well as a chain head. A Task left on close closes
   `withdrawn: <initiative>`.
3. **A released Task survives the cancel's sweep.** A Task released in a cancel
   is left out of the Task sweep (`bd.SweepTasks`) and out of the ordered-after
   refusal, the same way withdraw's hold releases (`p.Releases`) already are.
   Its `blocks` edges stay as history. A closed blocker does not block in bd, so
   the Task becomes ready and free-standing once the cancel lands.

## Considered Options

- **Keep release refused, with the filing-time flag as the only way out.**
  Rejected: withdraw would be the only exit for a member Task, and it throws
  away the Task's work.
- **Tasks always close on cancel, shown as read-only rows (or only in the
  tally).** Rejected: a follow-up Task often outlives the reason for the
  cancel, and the release row matches decision 1.
- **Release also removes the Task's edges to the beads the cancel closes.**
  Rejected: it erases the record of why the Task exists, for no behavioural
  gain.
- **Lock a Task ordered after a closing bead.** Rejected: a member Task is
  normally ordered against a member of the same Initiative, so the release row
  would almost always be locked.
