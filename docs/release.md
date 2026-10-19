# Releasing mugicrew to npm

A release is a Lineage publish of the Stock tree plus a version bump you make
first. Pushing the tree to the public repo's `main` runs the publish workflow,
which publishes the five `@jayulabs` packages: the four platform packages
(`mugicrew-linux-x64`, `mugicrew-linux-arm64`, `mugicrew-darwin-arm64`,
`mugicrew-darwin-x64`) and the launcher (`mugicrew`).

## Procedure

1. Bump `version` in all five npm package manifests: `npm/mugicrew/package.json`
   and the four `npm/mugicrew-<platform>/package.json`. All five must carry the
   same value.
2. In the launcher manifest (`npm/mugicrew/package.json`), bump the four
   `@jayulabs/mugicrew-*` `optionalDependencies` pins to that same value.
3. Make both changes together, in one change on `stock/mugicrew`.
4. Run `lineage publish mugicrew`.
5. Watch the publish workflow on the public repo until it is green. The publish
   job runs last, after the tests and smoke jobs.

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
- **`Every @jayulabs/mugicrew* package at <version> is already published, so
  nothing was published. Bump the version in all five npm package manifests and
  re-publish; see docs/release.md.`** (publish job). Every package is already on
  npm at that version, usually because the bump was forgotten. Bump the version
  and pins (steps 1 and 2) and run `lineage publish mugicrew` again. The job
  fails before it publishes anything.

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
