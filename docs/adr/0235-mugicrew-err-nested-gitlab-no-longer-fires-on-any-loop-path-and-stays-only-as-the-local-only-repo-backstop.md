# ADR 0235: ErrNestedGitlab no longer fires on any loop path and stays only as the local-only Repo backstop

Status: Accepted
Date: 2026-10-16

Spec mc-t72m.28 (Nested repos land, anchor and roll back through their own same-host
GitLab project). Amends ADR 0212 (a-gitlab-spec-anchor-is-a-bead-pin…) §3, which
made every nested move of mainline under gitlab refuse with `git.ErrNestedGitlab`.

## Decision

1. **Jinbe's Landing refusal is lifted.** `reconcileLandingOnce` runs the gitlab
   Landing path on a nested container's legs: child fetch, Graph-checked child
   landed base, conflict arm, `PinnedGitlab` on the child, then the MR lifecycle
   against the child's project. ~~The Graph ref is still pushed to the Workspace's
   project only.~~ (Amended by ADR 0300: no Graph ref is pushed.)
2. **A nested Rollback runs the revert-MR leg** on the child's repo and project,
   not the Workspace's.
3. **`ErrNestedGitlab` fires on no path the loop drives.** It remains only as the
   backstop on the local-only `Repo.Anchor` and `Repo.revert` legs (ADR 0287 (a-direct-spec-s-anchor-is-a-bead-pin-under-every-land-strategy): `Repo.Anchor` is gone, so the backstop covers `Repo.revert` only), which gitlab
   no longer reaches: a nested anchor under gitlab is the ADR 0212 bead pin.
4. **A child remote on another host refuses with `*git.DifferentHost`**, at loop
   start and again at Landing time, before any push or MR call.
5. **The post-land `mintRenumber` stays skipped for a nested container.** The
   skip is a scope decision, not a refusal.
