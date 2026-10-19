# Install mugicrew

Three commands, then fix what doctor reports:

```bash
npm i -g @jayulabs/mugicrew     # or, in a JS repo: npm i -D @jayulabs/mugicrew
mugicrew init                   # bootstrap your bd store in this clone
mugicrew doctor                 # repeat: run mugicrew doctor fix, then each Manual step, until it passes
```

mugicrew bundles none of its prerequisites. `mugicrew init` never commits. It
prompts for an issue prefix (`--prefix <p>` skips the prompt), so use a distinct
prefix per human.

## First human in a Workspace

`mugicrew init workspace` writes the settings the whole Workspace shares into
`.mugicrew/config.toml`. It runs once per Workspace, not once per human, and needs
no bd store. If `mugicrew init` printed the hint or `mugicrew doctor` warned that
the Workspace is not set up:

1. Pull `main` first: the first human's config may not have merged yet, or this
   clone is behind. If the hint is gone, you are done.
2. Otherwise one human cuts a linked worktree, since init refuses on `main`:
   `git worktree add ../<dir> -b <branch> main`.
3. In that worktree, run `mugicrew init workspace`. It asks for `land_strategy`,
   Require Initiatives, the Jira site and Contexts; each question has a flag
   (`--land-strategy`, `--require-initiatives`, `--external-ref`, `--jira-site`,
   `--context`). It never commits.
4. Land `.mugicrew/config.toml` by the steps init prints.
5. Re-run `mugicrew doctor`.

With no shared config the Workspace is valid and runs on defaults, so the hint
never blocks.

## Core

Always checked. Each FAIL's To-fix item names the minimum version and this OS's install command.

- `bd` 1.3.0 or later
- `dolt` (bd pins 2.2.0; doctor WARNs on 2.3.0 or later)
- `tmux` 3.4 or later (3.5 or later for mouse-picking in the F1 menu)
- `claude` 2.1.78 or later
- `git`

### dolt

Install the pinned 2.2.0 release (Linux amd64 shown; pick your platform's
tarball from the [release page](https://github.com/dolthub/dolt/releases/tag/v2.2.0)):

```bash
curl -fsSLO https://github.com/dolthub/dolt/releases/download/v2.2.0/dolt-linux-amd64.tar.gz
tar -xzf dolt-linux-amd64.tar.gz
sudo install dolt-linux-amd64/bin/dolt /usr/local/bin/dolt
```

## GitLab

Only when the Workspace sets `land_strategy = "gitlab"`. Needs `glab` and https
credentials for git.

Run `mugicrew doctor` first. Only if its glab-user or credential-fill check
FAILs, run one interactive login and choose gitlab.com, **Web**, **HTTPS**, and
**Yes** to "Authenticate Git with your GitLab credentials?":

```bash
glab auth login
```

That writes git's credential helper, sets glab's user and stores a token. Doctor's
credential fill catches a missing glab user or login, and re-running the login
fixes it.

### Scripted setups

For a non-interactive machine, use three lines instead:

```bash
glab auth login --hostname gitlab.com --git-protocol https --stdin < <token-file>
glab config set user <gitlab-username> --host gitlab.com
```

The token's scope is `api` alone.

mugicrew needs nothing beyond glab's login and user: it injects glab's helper into
every git call to the Forge (ADR 0300,
bead-graph-data-never-goes-to-a-project-remote…). A global
`credential.https://<host>.helper` entry is only for your own git over https
(clone, manual push):

```bash
git config --global credential.https://gitlab.com.helper '!glab auth git-credential'
```

Credentials cover one account per machine.

## Jira

Only when the Workspace sets `[jira] site`. Needs `acli`, then:

```bash
acli jira auth login
```

### acli

On Debian/Ubuntu, from Atlassian's apt repo (tested 1.3.39); macOS uses
`brew tap atlassian/homebrew-acli && brew install acli`:

```bash
sudo mkdir -p -m 755 /etc/apt/keyrings
curl -fsSL https://acli.atlassian.com/gpg/public-key.asc | sudo gpg --dearmor -o /etc/apt/keyrings/acli-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/acli-archive-keyring.gpg] https://acli.atlassian.com/linux/deb stable main" | sudo tee /etc/apt/sources.list.d/acli.list
sudo apt update && sudo apt install acli
```
