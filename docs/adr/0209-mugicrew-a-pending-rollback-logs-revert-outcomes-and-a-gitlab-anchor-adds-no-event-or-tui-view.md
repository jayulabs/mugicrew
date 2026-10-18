# ADR 0209: A pending Rollback logs revert.<outcome> events; a gitlab anchor and a revert MR add no TUI view

Status: Accepted
Date: 2026-10-15

Chart mc-t72m.15 (Every direct move of mainline has a Forge path under gitlab).
Settles grilling mc-t72m.15.7. Under `land_strategy=gitlab` a Rollback stays open
while its revert MR is pending (ADR 0208 a-pending-rollback-s-revert-mr…), and a
Spec anchor is an `Anchor: <sha>` body pin, not a commit
(docs/research/spec-anchor-readers-under-gitlab.md). This ADR says what the event
log and the TUI show for both. Amends ADR 0029 §5 and ADR 0047 on what
`revert.done` means under gitlab.

## Decision

1. **The revert MR's arms log a `revert.<outcome>` family that mirrors `land.*`**,
   never `land.*` itself: a `land.*` reader means a Landing. Levels follow
   `RecordLandOutcome`: Info for progress, Warn for what ends or holds the
   Rollback.

   | Arm | Event | Level | Attrs beyond the bead's |
   |---|---|---|---|
   | MR opened or reused, each tick (`need_rebase` included) | `revert.pending` | Info | target, mr, pushed, opened, commit |
   | Clean rebuild on the fetched main | `revert.rebuilt` | Info | mr, old commit, new commit |
   | Conflicted rebuild, fix forward (ADR 0049) | `revert.conflict`, then `handoff.done` | Warn | paths, mr |
   | Pipeline retried once | `revert.retried` | Info | mr, pipeline |
   | Held on a HITL Task (infra or real failure) | `revert.pipeline_failed` | Warn | mr, pipeline, infra, task |
   | Merged | `revert.done`, then `handoff.done` | Info | mr, sha |
   | A human closed the MR | `revert.closed` | Warn | mr |
   | Jinbe closes the MR as its Rollback closes (ADR 0208 §6) | `revert.mr_closed` | Info | mr, reason |

2. **Under gitlab `revert.done` fires only when the merge is seen.** Its `sha` is
   the post-fetch LandedBase that carries `Reverts-Checkpoint:`, not the pinned
   `Commit:`, which a Forge squash or rebase may rewrite. The pinned commit rides
   `revert.pending` as `commit`. Under local, `revert.done` is unchanged.
3. **A gitlab anchor adds no event.** `triage.fastlane`'s `anchor` attr carries the
   recorded sha unchanged; the land strategy says whether it is a pin or a commit.
   (ADR 0287 (a-direct-spec-s-anchor-is-a-bead-pin-under-every-land-strategy): the attr is always a pin, except on a legacy re-run, which returns the
   commit sha. It is now the manual recovery source for a lost pin.)
   Zoro's sweep stays silent, as it is today.
4. **No TUI code.** A pending Rollback's `MR:` and `Commit:` lines, and a Spec's
   `Anchor:` line, show in the detail pane's body, as a Landing's `MR:` line does
   today; History keeps its "rollback pending" badge. A Rollback held on a HITL
   Task shows through the Task and its blocking edge. The Logs tab renders the new
   events generically.

## Considered Options

- **Reuse `land.*` on the Rollback bead.** Rejected: it mixes Rollbacks into every
  Landing filter.
- **Log only `revert.done` and `handoff.done`.** Rejected: a revert MR can sit
  for days with no trace of why.
- **An `anchor.recorded` event, or an `anchored=` attr.** Rejected: nothing reads
  the anchor attr, and the anchor commit is invisible in the TUI today.
- **Show a revert MR's URL and state in the TUI.** Deferred: a richer MR view
  belongs to Landings and Rollbacks together, not to Rollbacks alone.
