# ADR 0213: Under gitlab a Rollback reverts through a revert MR and stays open while it is pending

Status: Accepted
Date: 2026-10-15

Chart mc-t72m.15 (Every direct move of mainline has a Forge path under gitlab).
Records the Rollback decision settled at charting and recorded by grilling
mc-t72m.15.6 (docs/research/rollback-revert-mr-reuse-of-gitlab-landing-legs.md).
Phase 1 refused a Rollback revert under `land_strategy=gitlab`. ADR 0212
a-gitlab-spec-anchor-is-a-bead-pin… retires that refusal. ADR 0208
a-pending-rollback-s-revert-mr… and ADR 0209
a-pending-rollback-logs-revert-outcomes… settle the MR's arms and events. Amends
ADR 0047 §4–6 (the revert is a direct forward commit) and ADR 0048 (when the
successor is seeded) under gitlab only; local is unchanged.

## Decision

1. **Rollbacks are supported under gitlab, through a revert MR.** Jinbe builds
   the revert commit (`Reverts-Checkpoint: <id>` trailer with its `Graph:` line)
   on the fetched Forge main. He pushes it to `rollback/<id>` and opens or
   reuses an MR, using the gitlab Landing's legs (FetchMainline,
   `landGitlabAt` without `Tip:`, `AssertMRDescription`, forge reads, retry-once,
   429 hold).
2. **The Rollback pins its revert and stays open; there is no Landing bead.** Its body
   carries `Commit: <sha>` and `MR: !<iid>`, set if unset (commit-tree is
   time-dependent, so a re-run must reuse the pin). Each tick,
   `reconcileRollback` opens or reuses the MR.
3. **The handoff runs on the merge Jinbe sees, not on the pinned commit.** When
   the post-fetch `LandedBase` finds `Reverts-Checkpoint:` on main, Jinbe runs
   the ADR 0048 handoff keyed on that SHA. A Forge squash or rebase may have
   rewritten the pinned one. Where the successor is cut when the merge method
   lands a second-parent squash is left to "Where a Rollback's successor is cut
   when its revert MR merges as a second-parent squash".
4. **A human merges** (ADR 0189); Jinbe never merges the revert MR.

## Considered Options

- **Refuse Rollbacks under gitlab.** Rejected: recovery from a bad land is
  needed most when mainline is remote truth.
- **A Landing bead for the revert.** Rejected: the Landing's arms are
  Checkpoint-shaped (Compose trial, Critique filing, threads), and a Rollback
  already has its own lifecycle.
