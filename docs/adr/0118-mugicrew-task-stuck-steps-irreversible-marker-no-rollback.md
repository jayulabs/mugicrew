# ADR 0118: mugicrew Task — a stuck step leaves the Task idle, irreversible steps are marked, and Tasks carry no rollback

Status: Accepted
Date: 2026-10-09

Amended by: ADR 0342 (mugicrew-withdraw-takes-a-task…) §2.
Amends: ADR 0116 §2 (a third marker) and §5 (one non-pass record). Settles the
rollback fog in ADR 0113's Consequences.

## Context

A Task's guide never advances past a failing check, and it records only passes
(ADR 0114, ADR 0116). An interrupted Task stays claimed and idle (ADR 0115 §3).
Four things were still open. What happens when a step's check keeps failing
and no fix works? Do steps that can't be undone need anything extra? Can a
Task carry rollback steps when the land that released it is rolled back? Does
the bead record anything about a failure? Decided in grilling.

## Decision

1. **A stuck step leaves the Task idle.** The human exits, the bead stays
   claimed, and `l` resumes it. If the blocker is work mugicrew could do (a
   code change, a missing capability), the guide files an offshoot Signal
   `discovered-from` the Task, with the human's approval, and adds no `blocks`
   edge: a Signal closes at publish (ADR 0096), so an edge would release the
   Task too early. If the step itself is wrong, the guide amends it (ADR 0116
   §8).
2. **The guide never closes a failed Task.** Abandoning a Task is the human's
   deliberate act outside the session: a `bd close` by hand, or a drop followed
   by a close. The guide only warns when the Task holds a Checkpoint's land:
   closing it releases that land (ADR 0113 §4) even though its step never
   happened.
3. **`[irreversible]` is a third marker.** Before the step's Do, the guide states
   the consequence and gets an explicit yes. This applies whoever performs the
   Do: the guide, the human (`[elevated]`), or a web UI. The author still
   writes any name or visibility pre-check as a step of its own.
4. **Tasks carry no rollback steps.** Jinbe's Rollback pass (ADR 0048, ADR 0049)
   ignores Tasks. A Task released by a land that is later rolled back has
   already run and stays closed. It does not re-run when the successor lands.
   If the external world needs undoing, a human or a session files a new Task.
5. **A stuck exit leaves one comment.** When a session exits while the current
   step still fails, the guide adds
   `step N (<title>) stuck: <what failed and what was tried>`, naming the
   offshoot if one was filed. ADR 0116 §6's never-record rules apply. The
   comment is history only: resume re-runs every check and never trusts it.

## Considered Options

- **Drop a stuck Task back to open:** rejected for the same reason as in ADR
  0115: half-done external state is real.
- **No marker, just prose in the Do:** rejected. Human-run and web-UI steps
  have no approval ask, so nothing would make the guide pause before them.
- **`## Rollback` steps that Jinbe turns into a new Task:** rejected. Whether
  to undo is a judgment call, and the right undo depends on main after the
  revert. Rollbacks are rare, and Jinbe's pass stays pure Go.
- **Listing released Tasks in the Rollback's successor body:** rejected as a
  graph walk for a case nobody has hit.
- **A comment on every failed check:** rejected as noise. A failure fixed in the
  same session doesn't matter later.
