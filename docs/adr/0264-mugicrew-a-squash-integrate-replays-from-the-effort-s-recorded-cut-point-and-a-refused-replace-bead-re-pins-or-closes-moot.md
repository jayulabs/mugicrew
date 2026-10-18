# A squash integrate replays from the effort's recorded cut point, and a refused replace bead re-pins or closes moot

Amends: ADR 0102 (leaf-efforts-integrate-onto-ref-only-containers) §1, §3 and §4; closes the Open line of ADR 0262 (mugicrew-a-requested-rebase-is-a-human-filed-conflict-variant…).

A replace-mode integrate (Conflict, Chart-conflict, Base-pinned Critique, and the human-filed `requested` variant) rewrites the container. Squash `Integrate` then rebases a sibling cut before the rewrite with `--onto <tip> <tip>`. That puts the old container commits back in the pick list, and only git's patch-id skip drops them. The result is a conflict on hunks the sibling never wrote, the silent resurrection of a commit the rewrite dropped, and a multi-commit re-integrate that conflicts with its own squash. A second replace bead pinned at the old Tip is refused and retried forever (docs/research/sibling-effort-integrate-after-container-rewrite.md §6, docs/research/old-container-tip-sources-and-refused-replace-retry-bound.md). Settled by grilling (bead "Sibling-replay policy for efforts cut before a requested rebase rewrites the container"):

- **Scope: every replace rewrite.** The rule is in the shared integrate path. It is not specific to the requested variant.
- **A recorded cut point.** `NewEffortWorktree` writes `refs/mugicrew/cut/<id>` at the commit it cuts at. Squash `Integrate` rebases `--onto <tip> <cut>`, so only the effort's own commits replay. After the CAS succeeds (never before), it moves the ref to the effort HEAD it integrated, so the ref is also the re-integrate key. The ref is deleted along with the effort branch. With no rewrite, this matches today's behaviour.
- **Fallback order.** `refs/mugicrew/cut/<id>`, then `git merge-base --fork-point <container> <effort>` from the container reflog (for branches cut before this rule), then today's tree-match `integratedThrough`, which applies only where the container has not been rewritten since the cut.
- **No cut point after a rewrite refuses.** If neither the ref nor `--fork-point` finds a cut point, the container's reflog lacks the effort's base, which means it was rewritten since the cut. Integrate then refuses with a distinct non-transient exit, integrates nothing, and the bead is paused for a human. It never falls back to the patch-id replay.
- **A refused replace bead self-resolves.** A replace integrate refused as "rewritten off its pinned tip" is no longer a transient exit 1. If the container already descends from the bead's `Base:`, the bead closes moot, as in the land-first case of ADR 0262. Otherwise it is re-pinned: `Tip:` is set to the current container tip, the stale effort branch is dropped and the bead is re-dispatched, so the fresh cut lands at the tip. If the re-pinned attempt is refused again, the bead pauses for a human.

## Considered options

- **Refuse or delay a rewrite while siblings are cut.** Rejected: ADR 0262 says a requested rebase blocks no sibling, and a HITL session exited-for-resume can stay cut for days.
- **Accept and document the failure cases.** Rejected: silent resurrection makes an unreported change to the container and attributes it to the wrong bead.
- **Read only the reflog (`--fork-point`).** Rejected as the primary source: unreachable entries expire after 30 days, a reflog is local to one repo, and a fresh clone or `gc` loses it. It stays as the fallback for branches cut before the rule.
- **`IntegrateReplace` stamps the overwritten tip.** Rejected: a sibling would have to walk a chain back to its own cut when a container is rewritten more than once.
- **An `Integrated-through:` trailer as the re-integrate key.** Rejected: the cut ref already advances to the integrated HEAD.
- **Pause or close a refused replace bead outright.** Rejected: moot-close and re-pin settle it without a human in the common case. Pausing remains only the fallback.
