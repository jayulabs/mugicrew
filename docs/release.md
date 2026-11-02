# Releasing mugicrew to npm

A release is a Lineage publish of the Stock tree plus a version bump you make
first. Pushing the tree to the public repo's `main` runs the publish workflow,
which publishes the five `@jayulabs` packages: the four platform packages
(`mugicrew-linux-x64`, `mugicrew-linux-arm64`, `mugicrew-darwin-arm64`,
`mugicrew-darwin-x64`) and the launcher (`mugicrew`).

A publish that changes only `site/**` runs the tests and smoke jobs, publishes nothing
to npm, and Pages deploys it.

## Procedure

1. Bump `version` in all five npm package manifests: `npm/mugicrew/package.json`
   and the four `npm/mugicrew-<platform>/package.json`. All five must carry the
   same value.
2. In the launcher manifest (`npm/mugicrew/package.json`), bump the four
   `@jayulabs/mugicrew-*` `optionalDependencies` pins to that same value.
3. If this release needs a human step before it installs (a `mugicrew doctor
   fix`, or a step in `docs/upgrading.md`), declare it in the launcher manifest in
   the same edit, with a section in `docs/upgrading.md` to link:
   `"mugicrew": { "humanStep": { "version": "<this version>", "doc":
   "docs/upgrading.md#<anchor>" } }`. A loop opted into unattended Upgrade never
   installs a marked release, nor any past it. When the next release no longer
   needs one, remove the field in its bump. Only the launcher carries it. The bump that first ships unattended
   Upgrade's swap survival sets it, with `"doc":
   "docs/upgrading.md#upgrading-onto-the-first-swap-surviving-release"`, so loops
   too old to survive a swap are never swapped unattended.
4. Make all the changes together, in one change on `stock/mugicrew`.
5. Run `lineage publish mugicrew`. It pushes the candidate branch.
6. Wait for the candidate's run of the publish workflow to go green, then run
   `lineage publish mugicrew` again. It pushes `main`, which reruns the suite
   and then publishes. The publish job runs last, after the tests and smoke jobs.

## Lineage does not bump the version

`lineage publish` never changes a version. A Projection is identified by its
tree hash, so Lineage cannot tell a release from any other change and will not
bump for you. Choosing the version is your decision, made in steps 1 and 2.

## Red runs and their fixes

- **`version mismatch: <pkg> is <a>, mugicrew is <b>`** (build job). A platform
  package's `version` differs from the launcher's. Set all five manifests to the
  same value (step 1) and publish again.
- **`pin mismatch: optionalDependencies @jayulabs/mugicrew-<platform> is <a>,
  mugicrew is <b>`** (build job). One of the launcher's four platform pins is not
  the launcher's own version. Fix the pins (step 2) and publish again.
- **`human-step mismatch: …`** (build job). `mugicrew.humanStep.version` in the
  launcher manifest isn't the version being published, or `humanStep.doc` isn't a
  path under `docs/upgrading.md`. Fix or remove the marker (step 3) and publish
  again.
- **`Every @jayulabs/mugicrew* package at <version> is already published, so
  nothing was published. Bump the version in all five npm package manifests and
  re-publish; see docs/release.md.`** (publish job). Every package is already on
  npm at that version, usually because the bump was forgotten. Bump the version
  and pins (steps 1 and 2) and run `lineage publish mugicrew` again. The job
  fails before it publishes anything.

- **The push to `main` is refused** (relayed by `lineage publish`). The public
  repo's ruleset refuses a push until its SHA carries every required check from a
  push run. Three cases:
  - *Pending.* The candidate's run hasn't finished. Wait for it, then publish again.
  - *Flake.* A check failed for a reason unrelated to the change. Re-run the
    failed jobs on the candidate's run, then publish again.
  - *Real failure.* A check failed because of the change. Fix it on
    `stock/mugicrew` and publish again, which pushes a new candidate.
  A renamed job or matrix cell leaves a required check that nothing reports, so
  `main` stays refused until the ruleset is edited to match.

## Recovery

- **Re-run a publish by hand.** Start the publish workflow with
  `workflow_dispatch` on `main`. It runs the same jobs and publishes whatever is
  missing at the current version. If every package is already published it
  stays green and warns `Nothing was published: every @jayulabs/mugicrew*
  package at <version> is already published.`
- **Partial publish.** If a run published some packages and then failed, re-run
  it (a dispatch, or re-run the failed workflow). It skips the packages already
  on npm (`already published, skipping`) and publishes only the missing ones,
  platform packages first, then the launcher. It does not fail or warn.

## Docs-only pushes

Every Lineage publish counts as a release, so a push that changed only docs and
did not bump the version fails the publish job by design. To get a green no-op
for such a push, run the workflow by hand with `workflow_dispatch`.
