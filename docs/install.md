# Install mugicrew

Three commands, then fix what doctor reports:

```bash
npm i -g @jayulabs/mugicrew     # or, in a JS repo: npm i -D @jayulabs/mugicrew
mugicrew init                   # bootstrap your bd store in this clone
mugicrew doctor                 # repeat, fixing each FAIL from its hint, until it passes
```

mugicrew bundles none of its prerequisites. `mugicrew init` never commits. It
prompts for an issue prefix (`--prefix <p>` skips the prompt), so use a distinct
prefix per human.

## Core

Always checked. Each FAIL names the minimum version and this OS's install command.

- `bd` 1.3.0 or later
- `dolt` (bd pins 2.2.0; doctor WARNs on 2.3.0 or later)
- `tmux` 3.5 or later
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
every git call to the Forge and restarts bd's sql-server once if the Graph push's
first attempt fails (ADR 0256,
the-graph-push-runs-under-an-injected-glab-helper…). A global
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
