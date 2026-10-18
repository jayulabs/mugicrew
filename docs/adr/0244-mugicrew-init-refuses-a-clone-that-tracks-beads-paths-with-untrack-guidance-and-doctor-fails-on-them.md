# mugicrew init refuses a clone that tracks `.beads/` paths, with untrack guidance, and doctor FAILs on them

Chart mc-uqpj.2 (Standalone install and bootstrap of mugicrew in a client
Workspace), grilling mc-uqpj.2.3 (human, 2026-10-16). Follows ADR 0243
(mugicrew-init-wraps-bd-init-server-stealth…): over a clone whose index tracks
`.beads/` files no bd init flag is clean, so the tracked case is handled before
bd is called.

If `git ls-files .beads` lists any path (one is enough; no file is allowed,
since all of `.beads/` is machine-local), `mugicrew init` exits non-zero
without running bd. It lists the tracked paths and prints the fix: on a branch,
`git rm -r --cached .beads`, merged through the Workspace's usual MR or land
flow, then re-run `mugicrew init`. The guidance warns that pulling that commit
deletes those files from every other clone's working tree, so each other human
re-runs `mugicrew init` after pulling. It also names, as optional cleanup for
the same MR, the bd-managed block in CLAUDE.md/AGENTS.md and bd's root
`.gitignore` entries; init never gates on those, as the agent files belong to
the Workspace. `mugicrew doctor` FAILs on the same check with the same fix,
which catches an untrack landing after init and a `.beads` file re-added later.

## Considered Options

- **Init untracks on a branch and opens the MR** (`glab mr create`): Forge
  writes from a per-human bootstrap verb, and a separate path for
  `land_strategy=local`, for a once-per-Workspace step.
- **Local workaround** (delete local `.beads/`, bd init, `skip-worktree` the
  tracked files): `git pull` fails whenever main touches those files, and
  every new clone meets the same trap.
