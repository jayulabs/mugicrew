# ADR 0338: A hold that outlives its closed Task re-files, related to it

Status: Accepted
Date: 2026-10-22

Chart mc-6848i (Hold Tasks carry a Steps body Brook can check), grilling
mc-6848i.5, from the research note
`docs/research/hold-task-clearing-conditions-steps-and-checks.md` §1 fact 1.
Amends ADR 0337 (hold-task-checks-prove-the-cleared-state…).

A Hold Task can close while its hold still stands. This happens when the
Chart real-failure body's Checks only approximate Jinbe's re-read, when the
foreign-land body ends on an `Ask:`, when a condition regresses after its
Check passed (an MR reopened, then closed again), or when the human abandons
the Task with `bd close` (brook/prompts/task.md §5). `SpawnBlockingTaskIfAbsent`
searches only open blocking Tasks, so the next tick files the same key again.

## Decision

1. **Re-file, never reopen.** The filer still files a fresh Task, with a
   fresh lead, fact lines, `## Steps` and `## Diagnostics` from the current
   read. Idempotency stays on open Tasks only.
2. **Relate it to the last closed Task.** When a closed Task that blocks the
   same container carries the same key, the new Task gets a bd `related` edge
   to the most recently closed one. Earlier ones are already linked from it,
   so the links form a chain.
3. **The body is unchanged.** It gets no `Re-filed after:` fact line, so the
   key lines and the `infraTaskLead` match stay as they are, and the link
   survives `bd rename-prefix`.
4. **The Spec states it** as behaviour, with a test: when a closed Task with
   the same key exists, a new Task is filed and related to it.

## Considered Options

- **Re-file as is:** rejected. The human and Brook could not see that the
  hold came back after a walk or an abandon.
- **Reopen the closed Task:** rejected. It keeps the old lead, stale
  Diagnostics and any Brook amendment, mixes two incidents in one record, and
  silently overrides a human's deliberate abandon close. Brook's drift rule
  would have handled the resume, so the cost was the stale body, not Brook.
- **A `Re-filed after: <id>` fact line:** rejected. Bead ids in the body go
  stale under `bd rename-prefix`, and a new body line would sit beside the
  matchable key lines.
