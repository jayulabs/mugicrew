# Workspace init offers Context candidates, and a blank answer declares none

Status: accepted

Chart mc-o2i55 (Workspace init writes the shared config). `mugicrew init
workspace` declares Contexts of both kinds, `path` and `repo` (ADR 0166
(nested-repo-contexts-ship-in-core…)), mixed freely, since each entry is one
kind on its own. Contexts still default to none (ADR 0157
(subtree-contexts-cost-a-no-context…)): every prompt's blank answer keeps
that default.

- **Candidates are suggestions, offered interactively only.** Path candidates
  are the outermost dirs below the root holding a tracked package manifest
  (`git ls-files`: `go.mod`, `package.json`, `Cargo.toml`, `pyproject.toml`,
  `pom.xml`, `build.gradle(.kts)`, `*.csproj`, `mix.exs`, `flake.nix`). Repo
  candidates are ignored dirs, to depth 3, that are their own git repo roots.
  Init shows them as one numbered list (derived name, dir, kind and reason) and
  asks `Declare which?`, where blank means none. Then a free loop runs: name →
  dir → kind (the default is `repo` when the dir is a git repo root), ending on
  a blank name. Init asks for a name when the derived one (the basename, cut to
  the name charset) collides or is invalid. Without a terminal nothing is
  scanned or auto-declared, because a scripted init names its contexts.
- **Flag:** `--context name=dir` declares a path and `--context name=repo:dir`
  a repo (`name=path:repo:…` for a dir that starts with `repo:`). The flag can
  repeat. Like every flag, the set of `--context` flags **replaces** the
  declared set.
- **Format checks**, re-prompted in a terminal and refused without one. A name
  matches `^[a-z0-9][a-z0-9-]*$`, because it becomes a bare TOML key and a
  `context:` label. A dir is relative, cleaned, inside the Workspace, not the
  root, and an existing dir. A repo dir is a git repo root and is matched by
  the Workspace's `.gitignore`, which the cut refuses otherwise. Init never adds
  the entry. No name or dir may repeat. Live checks stay in doctor.
- **A re-run is keep or drop.** Init lists each declared context with
  `Keep? [Y/n]`, then offers undeclared candidates, then the free loop. A kept
  entry is rewritten as is. A changed dir or kind is a drop and a re-add, or a
  hand edit, because labelled containers point at the name. A dropped entry
  loses its whole table.
- **Gate stays out of scope.** Init never asks for a per-context `gate`, but a
  kept entry keeps it (with flags: when the name and dir match).
- **A local layer that hides the table is warned, not refused.** When
  `config.local.toml` declares `[context.*]`, it wins as a whole table, so init
  prints one line naming that file and still writes the shared one.

## Considered options

- Inferring the kind from the dir without asking: rejected, because a subtree
  that holds a `.git` (a submodule) would be guessed wrong in silence.
- `--context` adding to the declared set: rejected, because every other flag
  sets its key outright.
