# ADR 0325: mugicrew a Task that raises a pivot or Critique stays stuck, and a corrected Spec adopts held Tasks by id

Status: Accepted
Date: 2026-10-21

Amends: ADR 0043 (pivot-raise-and-checkpoint-gate) §4 (a Task raiser does not
close `pivoted:`), ADR 0113 (task-production-and-ordering…) §4 (a Spec may
re-hold an existing Task).
Builds on: ADR 0118 (task-stuck-steps-irreversible-marker-no-rollback) §1, §2,
§5; ADR 0323 (a-signal-raises-a-pivot-through-caused-by…) §5–6 on the sibling
Chart. Reads `docs/research/task-raised-pivot-and-critique-reader-writer-inventory.md`.
Decided in grilling.

## Context

A Task stuck on a defect in the code, or a wrong FR, of the one open, unlanded
Checkpoint it holds now raises a `kind:pivot` and/or a `kind:critique` on that
Checkpoint. Every raiser until now was an effort that closes `pivoted:` and
leaves an unfinished FR for dismiss to refile (ADR 0043 §4). A Task is
different: it holds the Checkpoint's land (an incoming `blocks` edge), it closes
only on the human's yes (ADR 0115 §4, ADR 0118 §2), and it has no FR. Carry
never moves its incoming hold (ADR 0085 §1), so a confirm leaves the successor
unheld. Slicing dedupes Tasks by title against the new Spec's own dependents
only, so a corrected Spec that lists the Task again files a duplicate.

## Decision

1. **The raising Task stays claimed and stuck.** It never closes `pivoted:`:
   it is the land hold, and closing it would let the Checkpoint land once the
   pivot resolves though its step never happened. Its one stuck comment
   (ADR 0118 §5) names the pivot and/or Critique and what to wait for.
2. **The pivot and the Critique are `discovered-from` the Task**, the raiser
   convention of ADR 0043 §3, not `caused-by`: the Task is not succeeded, its
   step still has to happen. Zoro's raiser read finds it unchanged. A pivot
   pulls the Task onto the host Checkpoint's retrospective spine, which is
   accepted: a retrospective on a host whose Task is still open is premature.
3. **No `blocks` edge from the Task to either.** The blocker clears when the
   fix is on the Checkpoint branch, not when the pivot is triaged: a Task →
   pivot edge would release the Task early on dismiss or amend, and Task →
   Critique is a bd cycle (the Critique is the held Checkpoint's child). The
   human resumes with `l`; an early resume re-runs the checks and fails the
   same step harmlessly.
4. **A pivot raise by a Task always pairs with a Critique** carrying the Ask
   (the change the Task needs), `discovered-from` the pivot. It stands in for
   the unfinished-FR refile, as ADR 0323 §5 does for a Signal.
5. **Dismiss and amend** treat that Critique as ADR 0323 §6 does: dismiss
   rewrites it to the as-specced work left, else closes it
   `dismissed: <pivot>`; amend adds `AMENDMENT (<pivot>): …` to it. Zoro also
   adds one comment on the Task: `pivot <id> dismissed|amended: <gist>`, and
   either which Critique to wait for or that the step itself may be wrong and
   wants amending (ADR 0116 §8). Brook's resume reads it.
6. **Confirm and escalate: a corrected Spec adopts held Tasks by id.** A
   `## Tasks` entry may name an existing open Task (`task: <id>`). Slicing
   files nothing for it and only adds its ordering edges, so its
   `before: land` re-holds the new Checkpoint, as ADR 0113 §4 already has
   Zoro add holds. On confirm Zoro writes the entry into the corrected Spec in
   the pivot session; on escalate the evidence names the Task and the new
   Chart's Spec carries the entry. Whether the Task still applies is the
   human's call there: adopt it, or leave it out and abandon it by hand
   (ADR 0118 §2). Steps that no longer fit are amended on resume. The sweep
   never closes or carries a Task.
7. **§6 covers every Task holding a confirmed Checkpoint**, not only a raising
   one: a `before: land` Task on a Checkpoint confirmed by a Franky or Usopp
   pivot loses its hold the same way today.
8. **A Critique alone needs nothing automatic.** It runs through Build, Review
   and Approval as usual; the human sees it close (the Task's goto offshoot
   row) and resumes the Task.

## Considered Options

- **The Task closes `pivoted:` and something else holds the land.** Rejected:
  it releases a land on a step that never happened, and ADR 0118 §2 forbids
  the guide closing a failed Task.
- **`caused-by`, matching the Signal reroute (ADR 0322).** Rejected: it says
  the Task closed into the pivot, and every `caused-by` reader would treat it
  as a successor.
- **Task → pivot `blocks` edge.** Rejected: it frees the Task at triage, before
  the refiled Critique is built, and treats a pivot raise unlike a Critique
  raise.
- **The confirm sweep carries the hold onto the successor.** Rejected: it breaks
  ADR 0085 §1 and ADR 0113 §4, and skips the human's validity call.
- **The confirm sweep closes the Task as obsolete.** Rejected: half-done
  external state is real (ADR 0115 §3).
- **Fix only the raising Task, file the general case apart.** Rejected: one
  mechanism, and it would leave a known sibling bug behind.
- **Refile nothing on dismiss.** Rejected: the Task stays stuck with no carrier
  for the fix.
- **A tail comments on the Task when its Critique closes.** Rejected: new Go
  for a notice the goto row already gives.
