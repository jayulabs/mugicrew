# mugicrew

`mugicrew` is the agent crew dispatch CLI. The terms it uses are in
`GLOSSARY.md`, and its decisions are in `docs/adr/`.

## Build

```bash
nix build ./apps/mugicrew#mugicrew   # from the OE root
nix build .#mugicrew                 # from apps/mugicrew/
```

Without nix, build it from `apps/mugicrew/`:

```bash
CGO_ENABLED=0 go build .
```

`mugicrew` invokes itself by bare name, so put the binary on `PATH`.

## Test

From `apps/mugicrew/`:

```bash
CGO_ENABLED=0 go test ./...
```

The `git` package's tests need `git` on `PATH`.

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

On a lineage merge conflict, follow lineage's README resolve recipe, but
replace its final `git merge --ff-only lineage/resolve-<app>` (keep the
`git switch main` before it, so the branch isn't checked out when it's deleted):

```bash
git switch main && mugicrew land-lineage lineage/resolve-<app>
```
