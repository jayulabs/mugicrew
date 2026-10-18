# ADR 0208: A pending Rollback's revert MR: a human close abandons, a conflict rebuilds or fixes forward, a red pipeline holds on a Task

Status: Accepted
Date: 2026-10-15

Chart mc-t72m.15 (Every direct move of mainline has a Forge path under gitlab).
Settles grilling mc-t72m.15.5. Charting settled that under `land_strategy=gitlab`
a Rollback reverts through a revert MR on `rollback/<id>`, stays open while the
MR is pending (pinning `Commit:` and `MR:` on its body, no Landing bead), and
runs the ADR 0048 handoff once `Reverts-Checkpoint:` is on the fetched main. The
gitlab Landing's MR arms are Landing-shaped
(docs/research/rollback-revert-mr-reuse-of-gitlab-landing-legs.md §2). This ADR
says what each arm does for a Rollback. Amends ADR 0047 §6 (a third close
outcome), ADR 0049 §1 (its trigger under gitlab).

## Decision

1. **A human closing the revert MR closes the Rollback `abandoned: revert MR
   closed`.** No successor is spawned and the target Checkpoint is untouched.
   The human withdrew the revert; one who still wants recovery files a new
   Rollback.
2. **`need_rebase` is pending**, as for a Landing (ADR 0189 §3).
3. **A conflict rebuilds the revert, then fixes forward.** On the Forge's
   `has_conflicts` or `merge_error`, or a local `merge-tree` trial of the
   revert conflicting against a moved fetched main, Jinbe rebuilds the revert
   on the fetched main. Clean: he re-pins `Commit:` and force-pushes it to the
   same MR. Conflicted: the Rollback fixes forward exactly as ADR 0049 (successor
   at main HEAD carrying the conflicted paths, close `fix-forward: revert
   conflicted`), and Jinbe closes the revert MR.
4. **A failed pipeline follows ADR 0188 up to the real failure.** Jinbe retries
   once (`Retried: pipeline <id>`); an infra failure files a HITL Task blocking
   the Rollback. A real failure after the retry also files a HITL Task blocking
   the Rollback, carrying the pipeline URL and the failing jobs' tails (ADR 0188
   §6). The human merges anyway (the Rollback resumes to handoff) or closes the
   MR (§1). A red revert pipeline is ambiguous: main may be red for the very
   breakage being rolled back.
5. **No Approval.** Opening the revert MR rides the Rollback's filing (ADR 0047
   §2); the human's merge is the gate (ADR 0189 §1).
6. **Jinbe closes any revert MR still open when its Rollback closes**, for any
   reason (handoff, fix-forward, invalid). This covers the redundant MR of a
   second Rollback on an already-reverted target. It needs a forge close-MR
   call.
7. **Review threads on a revert MR are ignored.** The human merging reads
   them; a Rollback has no parent for a Critique.
8. **Crash order: the MR closes last.** Jinbe spawns the successor before he
   closes his own MR. A closed MR on a Rollback that already has a `caused-by`
   successor resumes that handoff or fix-forward; only a closed MR with no
   successor reads as §1's human close.

## Considered Options

- **A human close fixes forward, or asks via a Task.** Rejected: it starts
  work nobody asked for; refiling is cheap.
- **Every conflict stays pending for a human, or fixes forward with no
  rebuild.** Rejected: a clean rebuild is a rebase and needs no judgment; only
  a conflict on the current main means later work builds on the bad code (ADR
  0049's trigger).
- **A real pipeline failure fixes forward.** Rejected: it takes the merge
  choice from the human, who merges every MR anyway.
- **A `Closed-by: jinbe` body line to tell Jinbe's close from a human's.**
  Rejected: the successor's existence already says it (ADR 0048 §3 skip-if-done).
