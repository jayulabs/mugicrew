# ADR 0247: GitLab https credentials are glab's helper with its user set, and a second account is one GITLAB_TOKEN export

Status: Accepted

Amended 2026-10-17: §1 step 2 (the global helper) is no longer required by mugicrew; see Spec 'Every Forge git call authenticates through the injected glab helper, and doctor checks one credential line'.
Date: 2026-10-16

Chart mc-uqpj.2 (Standalone install and bootstrap of mugicrew in a client
Workspace). Settles prototype mc-uqpj.2.4. The Chart settled that mugicrew
supplies no credential helper; the runbook gives a tested recipe instead. A
probe on a clean HOME (glab 1.107.0, git 2.55.0, gitlab.com, kept on
`refs/mugicrew/prototypes/mc-uqpj.2`) ran each step against two real accounts.

## Decision

1. **One account per machine** is three commands, and all three are needed:

   ```sh
   glab auth login --hostname gitlab.com --git-protocol https --stdin < <token-file>
   git config --global credential.https://gitlab.com.helper '!glab auth git-credential'
   glab config set user <gitlab-username> --host gitlab.com
   ```

   `glab auth login` (with `--stdin` or `--token`) wires no git credential
   helper, so plain git fails. It also leaves glab's `user` empty, so the helper
   answers `username=` and GitLab refuses it (HTTP Basic: Access denied).
   glab's helper also refuses to fill when git asks for a username other than
   the configured one, so `credential.username` cannot stand in for step 3.
2. **A second account on the same machine** is a mode-600 token file and one
   export in that clone's shell:
   `export GITLAB_TOKEN=$(cat ~/.config/mugicrew/<account>.token)`.
   glab and glab's git helper both honour `GITLAB_TOKEN` over the stored login,
   so the one variable switches git and the Forge calls together.
3. **Token scope: `api` alone.** A personal access token with only `api`
   clones, pushes and deletes a branch over https and drives glab's API calls.
   `write_repository` is not needed.

## Considered Options

- **A per-clone token-file credential helper plus `GITLAB_TOKEN`** (the dry
  run's working setup). It works, but it pins only git to the clone: glab still
  reads the environment, so a shell without the export pushes as the second
  account while mugicrew opens MRs as the first. One variable for both cannot
  split.
- **A second glab config dir (`GLAB_CONFIG_DIR`) with a per-clone helper.** It
  works, but has the same split and more steps.
- **mugicrew ships a credential helper.** Ruled out at charting; the Chart
  keeps credentials out of mugicrew.

## Consequences

A shell without the export acts as the machine's first account for both git and
glab. The runbook says so. doctor's glab-user check shows which account is
active.
