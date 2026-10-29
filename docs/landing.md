# Rebasing and landing

## Rebasing an open Chart or Checkpoint

```bash
mugicrew rebase <chart-or-checkpoint-id>
```

Files a requested rebase that puts the open container's branch onto current
mainline (pinned at filing; under gitlab, the fetched Forge main). It refuses
(exit 1, files nothing) on a missing, closed, landed or branchless target or
another kind. It files nothing and exits 0 when the container is already on
mainline or a rebase is already open. Under gitlab it warns about an open MR.

## Landing lineage branches

`lineage sync` and `lineage install` print a `lineage/<src>-<hash>` branch cut
from `main`. Land it with:

```bash
mugicrew land-lineage <branch>
```

It fast-forwards `main` onto the branch under the same land lock every other
land takes, so it never races one, and keeps the branch's `Lineage:` trailer,
which the next merge takes its base from. It prints the landed sha and deletes
the spent branch. It refuses a dirty `main` checkout and any branch that isn't
one `Lineage:` commit on top of `main`. Don't land a lineage branch with a
bare `git merge --ff-only`.

Under `land_strategy = "gitlab"` it lands through the Forge instead: it checks
the project's squash settings, fetches the Forge's `main`, runs the same
refusals against it, pushes the branch and opens a squash MR whose title and
description are the commit's own subject and body (no `Tip:` or `Graph:`), then
prints the MR's URL and exits. A re-run reuses the open MR. A human merges it;
nothing watches it, and the local branch is kept. "Byte-for-byte" holds only
when the Forge's `main` doesn't move and nobody edits the squash message;
otherwise only the `Lineage:` value survives (amends ADR 0119 per ADR 0210).
lineage's own `git merge --ff-only` hint would diverge local `main` here, so
land through mugicrew.

On a lineage merge conflict, follow lineage's README resolve recipe, but
replace its final `git merge --ff-only lineage/resolve-<app>` (keep the
`git switch main` before it, so the branch isn't checked out when it's deleted):

```bash
git switch main && mugicrew land-lineage lineage/resolve-<app>
```

### Before a Publish

Overlay built up on `main` doesn't reach the published Stock until it is
Promoted (root ADR 0095). Before `lineage publish mugicrew`, run:

```bash
lineage promote mugicrew        # prints stock/mugicrew and "next: lineage sync mugicrew"
lineage sync mugicrew           # prints a lineage/stock/mugicrew-<hash> branch
mugicrew land-lineage <branch>
```

`promote` prints `nothing to promote` and writes nothing when the Stock is
already current, so the routine is safe to repeat. If it says
`stock/mugicrew is ahead of its last sync`, run `lineage sync mugicrew` and
land that branch first, then promote.
