# ADR 0342: mugicrew withdraw takes a Task, and a chain's withdraw sweeps the Tasks ordered after it

Status: Proposed
Amended by ADR 0352 (every-task-joins-the-initiative…) (§1: a Task is no longer unparented).
Date: 2026-10-22

Spec mc-xw3ez (Withdraw takes a Task, and a chain's withdraw sweeps its
post-land Tasks). Amends ADR 0310 (mugicrew-withdraw-drops-any-unclosed-chain…)
§1–§2, ADR 0118 (mugicrew-task-stuck-steps…) §2 and ADR 0029
(mugicrew-event-schema)'s withdraw rows. ADR 0340
(mugicrew-x-dialog-hangs-swept-tasks…) stays the dialog's ADR.

A Task is free-standing and unparented, so withdraw could never take one, and it
refused a chain while a Task was ordered after it. A human abandoning a Task had
only a hand `bd close`, which cleans nothing and warned only from Brook.

## Decision

1. **A Task is withdrawable (amends ADR 0310 §1).** `X` and `mugicrew withdraw
   <task>` take an unclosed Task that is not a Draft, closing it with reason
   `withdrawn`. Cancel and release still see chain heads only.
2. **Tasks are swept, not refused (amends ADR 0310 §2).** A chain's withdraw no
   longer refuses on a Task ordered after a bead it closes. It closes those
   Tasks first with reason `withdrawn: <head>`. The pre-land Tasks the chain was
   ordered after close with it too, by the least fixed point of ADR 0351
   (mugicrew-a-chains-withdraw-closes-the-tasks-ordered-before…): a Task whose
   unclosed dependents are non-empty and all in the closing set is
   before-closed; one that still holds anything outside the set is left open.
   A Task withdrawn on its own has no backward reach. The refusal still lists
   non-Task beads, with the same reason string. Releasing a Checkpoint's land hold by withdrawing a
   Task is not a refusal: the result names the `released` Checkpoints.
3. **Abandoning a Task is a withdraw (amends ADR 0118 §2).** Brook still never
   closes a failed Task; it points the human to `X` or `mugicrew withdraw
   <task>` and keeps its warning that this releases a land hold.
4. **Events (amends ADR 0029's withdraw rows).** `withdraw.done` stays on the
   target and gains `swept` (forward-only), `swept_before` (before-closed ids, in
   close order, empty when none), `left_open` and `released` (ids); the refusal
   reason strings are unchanged.
5. **A live Brook session refuses.** A Task with an alive session is in flight,
   so withdraw refuses and its fix reads "finish or drop its session first".
6. **A crashed Task's dead pane is killed.** A Task with an Exited session has
   no work in the pane, so cleanup kills it as the session leg of the report. It
   is not a discard and takes no confirmation; no tree or branch is removed.
7. **The resume race is accepted.** Brook holds no liveness flock and resume
   skips the claim, so between the fence's re-read and the close `l` on an idle
   Task can start a session on a bead that closes moments later; the guide
   re-checks the bead and finds it closed. Discussions and Checkups share the
   window. A flock on launch and resume would close it, at the cost of widening
   ADR 0154 (mugicrew-the-dispatcher-holds…)'s flock scope; neither changes.
8. **Slicing is unchanged.** An adopted `task: <id>` naming a closed Task is
   skipped as before (no file, no edges, a printed line).
