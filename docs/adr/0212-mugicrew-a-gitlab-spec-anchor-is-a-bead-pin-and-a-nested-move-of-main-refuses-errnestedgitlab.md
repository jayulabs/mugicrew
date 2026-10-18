# ADR 0212: A gitlab Spec anchor is a bead pin, and a nested anchor or revert refuses with ErrNestedGitlab

Status: Accepted
Date: 2026-10-15

Chart mc-t72m.15 (Every direct move of mainline has a Forge path under gitlab).
Settles grilling mc-t72m.15.6 and records the anchor decision settled at charting
(docs/research/spec-anchor-readers-under-gitlab.md). Phase 1 refused a Spec anchor
and a Rollback revert under `land_strategy=gitlab` with `git.RemoteTruthRefusal`
(Spec FR 46). Once every Workspace move of mainline has a Forge path (this ADR,
ADR 0213 a-rollback-reverts-through-a-revert-mr…, and the lineage MR), that
refusal has no Workspace caller left. Amends ADR 0014 §2 and ADR 0035 §4 (the
anchor as a commit) under gitlab only; local is unchanged.

## Decision

1. **Under gitlab a Spec anchor writes no commit.** (Superseded by ADR 0287 (a-direct-spec-s-anchor-is-a-bead-pin-under-every-land-strategy): the pin applies under every strategy, with the legacy trailer read after it.) Luffy's fast lane and Zoro's
   confirm sweep fetch the Forge main (outside the land lock), then record its
   tip as an `Anchor: <sha>` line on the Spec bead, set if unset, as Jinbe's
   `recordMR` does. Zoro's `checkpointBase` reads that pin instead of
   `LandedBase("Spec: <id>")`. A missing pin is a hard error, as a missing anchor
   commit is (ADR 0014).
2. **`git.RemoteTruthRefusal` and `refuseUnderGitlab` are deleted.** Nothing
   in the Workspace refuses on the land strategy alone.
3. **Nested repos refuse with one sentinel, `ErrNestedGitlab`, moved into `git`.**
   `Repo.Anchor` and `Repo.revert` return it when the Repo is a child and the
   strategy is gitlab, before writing anything. Every caller (Luffy's
   `repoAnchor`, Zoro's sweep, Jinbe's `reconcileRollback`) goes through them. Jinbe's
   Landing check keeps refusing before any Forge call, now using the `git`
   sentinel. Today both nested legs move a child's local main with no guard.
   A nested direct Spec anchored this way would be cut into a Checkpoint whose
   Landing `ErrNestedGitlab` refuses anyway. Lifting it is the Initiative's
   phase 3 (one GitLab project per nested repo).

   > **Amended by ADR 0287 (a-direct-spec-s-anchor-is-a-bead-pin-under-every-land-strategy):** `Repo.Anchor` is gone; only `Repo.revert` returns
   > `ErrNestedGitlab`.
   >
   > **Amended by ADR 0235:** the Landing, the anchor and the Rollback now go
   > through the child's own GitLab project; `ErrNestedGitlab` survives only as
   > the local-only `Repo.Anchor`/`Repo.revert` backstop.
4. **`Graph:` applies to commits only.** A revert's trailer block keeps its
   `Graph:` line (ADR 0187 §1). A gitlab anchor's `Anchor:` pin is a bead-body
   line, not a trailer block, so it has none. A lineage commit carries none
   (ADR 0187 §1) and must land byte-for-byte.

## Considered Options

- **Keep `RemoteTruthRefusal`, narrowed to nested repos.** Rejected: two
  refusals then say the same "phase 3" thing.
- **No nested guard: a child has no Forge, so its local main is its truth.**
  Rejected: the anchor or revert would succeed only for the Landing after it to
  fail, so the refusal would just come later and less clearly.
- **Guard at each caller.** Rejected: the `git` Repo legs are the one point all
  callers go through.
