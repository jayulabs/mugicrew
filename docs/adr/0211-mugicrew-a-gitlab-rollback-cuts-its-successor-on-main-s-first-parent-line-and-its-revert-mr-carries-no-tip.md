# ADR 0211: A gitlab Rollback cuts its successor on main's first-parent line, and its revert MR carries no Tip:

Status: Accepted
Date: 2026-10-15

Chart mc-t72m.15 (Every direct move of mainline has a Forge path under gitlab).
Settles grilling mc-t72m.15.9. Under `land_strategy=gitlab` a Rollback's revert
lands through a revert MR, and `handOff` keys on the merged SHA that LandedBase
returns after a fetch. Under the `merge` method that SHA can be the squash S, the
second parent of the merge commit M. Cutting at S gives a successor branch off
main's first-parent line, missing every land merged in between
(docs/research/rollback-revert-mr-reuse-of-gitlab-landing-legs.md §4-5).
Amends ADR 0048 §4 (where the successor is cut) and ADR 0209 (the `revert.done` sha).

## Decision

1. **Normalise the merged SHA to main's first-parent line.** When the merged SHA X
   is not on main's first-parent line, Jinbe replaces it with the first-parent
   commit M that brought it in (the oldest commit of
   `rev-list --first-parent --ancestry-path X..main`). `handOff` cuts
   `checkpoint/<new>` at M and runs `Reapply(branch, M)` unchanged: it writes
   `tree(M~1)`, main just before the merge, which inverts exactly what the merge
   brought in. Reapply's `tip == revert` precondition holds. Under `ff`, or when X
   is already on the first-parent line (e.g. the merge-commit template carries the
   trailer), X is used as is. No merge method is refused.
2. **`revert.done` logs the normalised SHA** (M), the commit the successor is cut
   at.
3. **The revert MR's trailer block has no `Tip:` line.** `landGitlabAt` and
   `AssertMRDescription` take the tip as `""` for a revert, so the block is
   `Reverts-Checkpoint: <id>` then `Graph: …`, the same as the local revert commit,
   and `LandedTip` never reads a revert as a land's tip. The MR title is
   `revert <squash>`, the revert commit's own subject.
4. A second Rollback's redundant open revert MR needs nothing new: ADR 0208 §6
   closes any revert MR still open when its Rollback closes.

## Considered Options

- **Refuse `merge_method=merge`** (for revert MRs or the whole project, in
  `ValidateSettings`). Rejected: Landings support it today.
- **Accept the stale cut at S until Compose rebases it.** Rejected: the successor's
  Build would work on old code.
- **Cut at M and reapply S's diff on top.** Rejected: it breaks Reapply's
  `tip == revert` precondition, and reapplying at M already does it.
- **Keep `Tip: <revert sha>` and reuse the MR legs unchanged.** Rejected: the local
  and gitlab reverts would carry different trailers, and `LandedTip` would misread it.
