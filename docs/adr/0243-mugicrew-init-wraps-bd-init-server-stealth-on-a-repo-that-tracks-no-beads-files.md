# mugicrew init wraps `bd init --server --non-interactive --stealth`, on a repo that tracks no .beads files

Chart mc-uqpj.2 (Standalone install and bootstrap of mugicrew in a client
Workspace). Prototype mc-uqpj.2.2, kept on `refs/mugicrew/prototypes/mc-uqpj.2`
(`apps/mugicrew/prototypes/bd-init-matrix/`: `run.sh` and its observed
`results.json`). Measured against bd 1.3.0.

`mugicrew init` runs exactly

    bd init --server --non-interactive --stealth --prefix <prefix>

In a repo that tracks no `.beads` files this leaves a server-mode store, makes
no commit, leaves `git status` clean and names `metadata.json`'s
`dolt_database` after the new prefix. `--stealth` writes only
`.git/info/exclude` (`.beads/`, `.claude/settings.local.json` and bd's Dolt
patterns): not the global git config its help text promises. It also skips the
agent files and git hooks, so `--skip-agents --skip-hooks` add nothing.
`mugicrew init` refuses to run this over a clone whose index tracks any
`.beads/` path: that case is handled before bd is called (sibling bead "What
mugicrew init does in a Workspace whose repo already tracks .beads files").

## Considered Options

- **Plain `--server`** (the dry run): bd auto-commits `.beads/`, `.gitignore`
  and the agent files to the current branch, main included.
  `.beads/dolt-server-config.yaml` holds this machine's port and an absolute
  `cfg_dir` path, and bd's generated `.beads/.gitignore` does not list it (only
  `dolt-server.{pid,log,lock,port,activity}`), so it is committed. Neither is
  `dolt-backup.json` listed, so any bd that writes it has it swept into the next
  init commit; bd 1.3's embedded auto-backup writes the ignored `backup/`
  instead, so the dry run's copy came from an older bd or an explicit backup
  setup.
- **`--setup-exclude`**: no commit, but the agent files and the root
  `.gitignore` are left untracked in the tree (only `.gitignore` with
  `--skip-*`).
- **Over a clone that tracks `.beads` files no flag is clean.** Every variant
  rewrites the tracked `dolt-server-config.yaml`; plain and `--skip-*` commit
  it (plain also `CLAUDE.md`); `--stealth` rewrites the tracked `config.yaml` to
  `no-git-ops` plus `sync.remote` and deletes the tracked root `.gitignore`.
  Under every variant `metadata.json` keeps the first human's `dolt_database`
  and `project_id`, though issues take the new prefix. `.git/info/exclude`
  never hides a tracked file, so this cannot be fixed by flags.
