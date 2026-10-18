# ADR 0287: A direct Spec's anchor is a bead pin under every land strategy

Status: Accepted
Date: 2026-10-17

Spec mc-0em6 (direct Spec anchors pin on the bead under every land strategy).
Under `land_strategy=local` a direct Spec's anchor was still an empty
`Spec: <spec-id>` commit on mainline, a move of main with no bead and no Forge;
only gitlab pinned (ADR 0212). That left two anchor shapes and a bead-less write
to the shared mainline. This ADR makes the pin the one anchor.

**Supersedes** ADR 0014 (direct-spec-cut-base) §1 (the mechanism only: the
principle that the cut base is an exact base recorded at birth, never
`rev-parse main` at cut time, survives), §2 and §4; ADR 0212
(a-gitlab-spec-anchor-is-a-bead-pin…) §1, widened to every strategy with the
legacy read added after the pin. **Amends** ADR 0035 (signal-triage-fast-lane) §4.

## Decision

1. **A direct Spec's anchor is an `Anchor: <sha>` line on the Spec bead, set if
   unset.** Under local the pin is the mainline tip at triage; under gitlab it is
   the fetched Forge tip (ADR 0212). No commit is written under either strategy.
2. **Zoro's `checkpointBase` reads pin first, then the legacy trailer, then
   fails.** It reads the pin; if none, `LandedBase("Spec: <id>")` under both
   strategies; if neither, a hard error, never a `rev-parse main` fallback.
3. **A re-run on a legacy Spec returns the trailer sha and writes no pin.** A
   Spec anchored by an empty commit keeps resolving through its trailer.
4. **There is no migration.** Legacy `Spec:` commits stay on mainline untouched.
5. **A lost pin is recovered by hand** from the `anchor` attr of the Spec's
   `triage.fastlane` event (ADR 0209 §3).
6. **The readers are unchanged.** Every consumer of the anchor still gets a sha
   from the one `checkpointBase` seam.
7. **The local pin is a lock-free, fetch-free `rev-parse`** of mainline, so
   triage takes no land lock and touches no remote.

## Consequences

- `Repo.Anchor` is gone; only `Repo.revert` returns `ErrNestedGitlab` (ADR 0212
  §3, ADR 0235 §3).
- New direct Specs add no History row (ADR 0233 stays in force).
- The anchor is no longer a landed base: the pin is a recorded birth point, not a
  commit a land produced.

## Considered Options

- **Keep the local empty commit.** Rejected: a bead-less, lock-taking write to
  shared mainline, and two anchor shapes to read.
- **Migrate legacy `Spec:` commits to pins.** Rejected: the trailer still
  resolves, so a migration buys nothing.
