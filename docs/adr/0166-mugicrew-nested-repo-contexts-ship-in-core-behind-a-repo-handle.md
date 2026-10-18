# Nested-repo contexts ship in core, behind a repo handle

Status: accepted

A `[context.<name>] repo = "<dir>"` context, where a Checkpoint branches and lands
in a child repo, ships in core and not as a fork Overlay. The throwaway on
`refs/mugicrew/prototypes/mc-3hri.6` (prototype "Throwaway the nested-repo
binding's no-context path and measure its bloat") wired the minimum: the `repo`
key, a `git.Repo` handle, the child materialized in the effort tree, and the
land_strategy seam taking the repo. It measured the no-context path under ADR
0157's evidence model. An Overlay would have to fork the same four seams
(`session.CutTree`, Jinbe's landing, Zoro's cut, `DrainBlockers`), so it would
cost more than core does. Four rules keep the no-context cost at zero:

- **`git.Repo` is a separate handle, and the package funcs still mean the cwd's
  repo.** The handle's methods run `git -C <dir>` through the existing `run`.
  `git/git.go` gains 0 lines, and no existing caller changes. A context-aware
  site builds a `Repo` only after `bd.ContextOf` on a bead it already holds comes
  back non-empty (`contexts.RepoOf`). So Zoro, Jinbe and Usopp read no config
  for an unlabelled bead.
- **The land lock stays on the Workspace.** `Repo.Land` is `AnchorIfAbsent` with
  the child's `LandedBase`, under the cwd's `withLandLock`. The land trailer goes
  on the child's mainline, and there is no second `land.lock` in the child. The
  land_strategy seam is `LandStrategyFor(name, repo)`: an empty repo means
  today's `LandStrategyByName`, and only `local` acts per repo.
- **A launch gates on declared `repo =` contexts before reading the
  container.** Leaves don't carry `context:`, so `CutTree` reads config before
  any `bd show`, the same gate as ADR 0157's second rule. A nested effort tree
  is a Workspace host detached at mainline, with the child linked worktree on
  `effort/<id>` at `<host>/<dir>`. The child is torn down first.
- **`DrainBlockers` takes the union of effort refs across the declared repos,
  with the repo set resolved once per tick.** Without the union, a nested leaf's
  `effort/<id>` is invisible and its container drains early. If the set is read
  on every call, a no-context Workspace pays one config read for each drain-gate
  call on the tick (about as much as the `for-each-ref` that call already does).
  That breaks ADR 0157's zero per-tick cost, so the loop hoists the read.

Measured footprint: 8 files and +279/−21 lines, 119 of them the new
`git/repo.go`. There are 0 lines in `git/git.go`, dispatch, route, luffy, franky,
chopper or the TUI. A no-context prompt is byte-identical. A labelled nested-repo
prompt is +316 bytes (a subtree is +174). The only new config key is `repo`,
inside `[context.*]`. There are no new labels and no new crew-doc lines. The
existing suite passes unchanged.

## Consequences

- `contexts.Contexts` maps a repo entry to its dir, so the focus line words it
  like a subtree, plus a repo addendum. Usopp treats the whole child as in scope
  and runs no out-of-scope check for it. This fixes the phase-1 misreading of
  `repo =` as an empty path.
- `MainlineOf` falls back to `main` when there is no `origin/HEAD`. A child whose
  trunk has a different name must have `origin/HEAD` set, which a cloned child
  already has.
- The pre-land gate builds `#mugicrew` and is skipped for a nested land. Whether
  it gets a per-repo target is left to the Spec.

## Measured amendment

The built slice does not match the rules above, so the evidence they claimed is
superseded by what shipped:

- **The handle lives in `git/git.go`, not a separate `git/repo.go`.** There is no
  `git/repo.go`. `git/git.go` gained about 760 lines, and about 23 package funcs
  became `Repo("").X` wrappers, so the package funcs now delegate to the cwd
  handle. Their behaviour for the cwd's repo is unchanged, and no existing
  caller's signature changed. "Gains 0 lines" and "119 of them the new
  `git/repo.go`" no longer hold.
- **Measured footprint: 65 files, +4246/−425 lines**, not 8 files and +279/−21.
  The Spec's note that the footprint is re-measured in the Review applies to
  these numbers.
- The zero-cost no-context path (byte-identical prompts, no config read for an
  unlabelled bead, the hoisted `DrainBlockers` repo set) is the decision that
  stands. Only the "package funcs untouched" rule is replaced: they delegate to
  `Repo("")`.
