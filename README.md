# mugicrew

`mugicrew` is the agent crew dispatch CLI. The terms it uses are in
`GLOSSARY.md`, and its decisions are in `docs/adr/`.

## Install

```bash
npm i -g @jayulabs/mugicrew
```

Then run `mugicrew init` and `mugicrew doctor`; see [docs/install.md](docs/install.md).
The first human in a Workspace also runs `mugicrew init workspace`, which writes the
shared `.mugicrew/config.toml`; see
[First human in a Workspace](docs/install.md#first-human-in-a-workspace).

To cut an npm release, see [docs/release.md](docs/release.md).

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

## TUI key notes

`y` copies the selected bead's id with an OSC 52 escape sequence, so it needs
no clipboard tool on the host. tmux forwards OSC 52 only when `set-clipboard`
is `on` or `external`.

## Bd store mode

Cap > 1 wants a server-mode bd store. Each human runs `mugicrew init` in their own
clone, which creates one, and `mugicrew doctor` FAILs on an embedded store.

To move an existing embedded store to server mode, from the Workspace root:

1. Stop the loop.
2. `bd backup init <dir>` and `bd backup sync`.
3. Move `.beads/embeddeddolt/` aside.
4. `bd init --server --prefix <p> --stealth` with the same prefix (`mugicrew init`
   would skip it, since the leftover `.beads/metadata.json` still resolves).
5. `bd backup restore --force <dir>`.
6. Verify with `bd count`, `bd dolt status` and a fast `bd show <id>`.

bd autostarts a detached per-repo Dolt server on the first call, and all linked
worktrees share it. mugicrew neither starts nor stops it, and works on embedded
stores too.

## GitLab land strategy

`mugicrew init workspace --land-strategy gitlab` writes `land_strategy`; the TOML
below is the reference for hand edits.

Under `land_strategy=gitlab`, everyone who lands on the shared mainline needs
their own `issue_prefix` (`mugicrew init --prefix <p>`, or `bd rename-prefix <p>`).
Two Bead Graphs with one prefix mint the same bead ids, so `loop start` refuses
when another graph already lands with yours, and a colliding Landing holds its
container on a Task (ADR 0187).

Set it up once:

1. `land_strategy = "gitlab"` in the layered config.
2. `glab auth login --hostname <host>`. mugicrew stores no token; it runs
   `glab api` with your login and fails loudly if glab is missing or logged out.
3. In the GitLab project, set **Squash commits** to Require (every other
   setting is refused) and the **squash commit template** to `%{title}`, a blank
   line, then `%{description}`. The template carries the `<Kind>: <id>` trailer
   from the MR description onto mainline. `loop start` and the first push or MR
   create refuse a project that would drop it.

The Forge is the remote `branch.main.remote` names, else `origin`; the host and
project come from that remote's URL (ssh or https).

Nested repos (`[context.<name>] repo = <dir>`) land through their own GitLab
project, resolved the same way from the child's remote, which must be on the
Workspace's host. `land_strategy` is read from the Workspace only, so every
declared nested repo is on gitlab when the Workspace is. `loop start` runs the
auth, fetch, squash-settings and foreign-Graph checks once per distinct project
(the Workspace's, then each child's) and refuses with the context's name when a
child fails, has no parseable remote, or is on another host.

mugicrew opens the MR and never merges or arms auto-merge. You merge it on the
Forge, or arm auto-merge yourself. The MR's pipeline, not the local nix gate, is
the land gate (ADR 0150, ADR 0189).

### Bead Graph data and project remotes

A Bead Graph never goes to a project remote: no mugicrew path pushes it, and
mugicrew deletes no remote ref (ADR 0300). Back up or sync a graph only to a
destination you name, with `bd backup` or `bd sync`.

- On a user-named git remote bd creates a `__dolt_remote_info__` marker branch
  (`refs/heads/__dolt_remote_info__`). Set `DOLT_REMOTE_INFO_BRANCH=` (empty) in a
  shell export or `.env` line to suppress it. An existing
  `DOLT_REMOTE_INFO_BRANCH=` line in `.beads/.env` is harmless and may be deleted.
- Refs an older mugicrew pushed are yours to delete: find them with
  `git ls-remote <forge> 'refs/mugicrew/graphs/*'`, then
  `git push <forge> --delete refs/mugicrew/graphs/<you>`. For the old GitHub
  sync, delete `refs/dolt/data` and, if present, `refs/heads/__dolt_remote_info__`.

## Poll intervals

An open Latch is re-checked on the interval of the system it watches:
`[jira] poll_interval` for a Jira ticket, `[gitlab] poll_interval` for a GitLab
merge request. Each is a Go duration (default `15m`, minimum `1m`) in the layered
`config.toml`; an unparseable or under-minimum value logs `config.unusable` and
uses the default. Both are read once per loop start.

A pending Landing is re-checked on `[gitlab] landing_poll_interval`, measured from
when its previous effort ended, pass or fail. It is a Go duration in the same
layered `config.toml` (default `60s`, minimum `15s`); an unparseable or
under-minimum value logs `config.unusable` with `key=gitlab.landing_poll_interval`
and uses `60s`. It is read once per loop start. A Landing the loop has not run
since it started is due at once, so a restart re-checks every Landing once.

The `[jira] site` they poll is written by `mugicrew init workspace --jira-site <host>`
(a bare host, empty clears it); hand edits go in `[jira]` of `config.toml`.

## Agent Profiles

By default every AFK run and HITL session runs `claude` with no `--model`. To
run a bead kind on a different model or endpoint, declare Agent Profiles in
the OE's catalog or in mugicrew's `config.toml` and map kinds to them (root
ADR 0093, ADR 0122, ADR 0123, ADR 0125, ADR 0309). If you map nothing, nothing changes.

### The catalog

The catalog has two layers. Each is an `agent-profiles.toml` of
`[profiles.<name>]` tables:

1. Checked in, at the OE root: `$OE_ROOT/agent-profiles.toml`. The OE
   devShell exports `$OE_ROOT`. If it isn't set, mugicrew uses the Workspace
   root.
2. Machine-local, at `<git-common-dir>/agent-profiles.toml`. It sits outside
   every worktree and is never committed, so machine-specific endpoints and
   keys go here.

A local profile replaces a checked-in profile of the same name **whole**. Its
fields and `env` are not merged with the checked-in one. A local profile with
a new name adds to the catalog.

```toml
[profiles.sonnet]
harness = "claudecode"            # required; mugicrew supports only claudecode
model   = "sonnet"                # optional; empty or absent = claude's own default
env     = { MAX_THINKING_TOKENS = "8000" }   # optional
```

Each layer loads whole or not at all. Malformed TOML, a profile without
`harness` or an unknown key drops the whole layer. mugicrew then logs
`agent-profiles.unusable {file, reason}` and keeps going with the other
layer. A missing file is not an error. Both files are re-read at every launch,
so an edit takes effect on the next launch without a restart.

`env` values expand `${VAR}` from the environment mugicrew launches from, so
neither file needs to hold a secret. If `VAR` is unset, the profile can't run
(see Refusals). If it is set but empty (`VAR=`), it expands to empty.

Profile env reaches `claude` through process env only, never argv or
`--settings`: AFK through the child's environment, HITL through the 0600 pane
env file. Credentials therefore never show in `ps` or the tmux start command.

#### A non-Anthropic model through a gateway

A `claudecode` profile can point `claude` at any Anthropic-compatible
endpoint. Set the base URL and a credential in `env`, and set `model` to the
endpoint's model id. Some endpoints reject Claude-only request fields; the
`CLAUDE_CODE_DISABLE_*` flags turn those off. Anthropic does not officially
support this setup.

```toml
[profiles.deepseek]
harness = "claudecode"
model   = "deepseek-chat"
env     = { ANTHROPIC_BASE_URL   = "https://api.deepseek.com/anthropic",
            ANTHROPIC_AUTH_TOKEN = "${DEEPSEEK_API_KEY}",
            CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS = "1",
            CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING  = "1" }
```

### Profiles in config.toml

*Catalog* means only the two `agent-profiles.toml` files above. mugicrew's
`config.toml` layers may also declare `[profiles.<name>]` tables, read by
mugicrew alone (ADR 0309):

1. the machine-local `<git-common-dir>/mugicrew/config.toml`
2. the checked-in `.mugicrew/config.toml`

mugicrew reads four files, in this order, and the **first** one that declares a
name wins it whole, with no field or `env` merge (ADR 0304):

1. mugicrew local `config.toml`
2. OE local `<git-common-dir>/agent-profiles.toml`
3. mugicrew shared `.mugicrew/config.toml`
4. OE shared `$OE_ROOT/agent-profiles.toml`

A profile in the machine-local `config.toml` never leaves the machine. One in
`.mugicrew/config.toml` travels with that file. A profile other Applications
should share belongs in the OE catalog.

```toml
[kind_profiles]
build = "local-llama"

[profiles.local-llama]
harness = "claudecode"
model   = "llama-3-70b"
env     = { ANTHROPIC_BASE_URL = "http://localhost:4000" }
```

The bad-table rule differs from the catalog's. A malformed OE file drops its
whole layer and logs `agent-profiles.unusable`. A bad `[profiles.<name>]` table
in a `config.toml` (an unknown key, no `harness`, a field of the wrong type)
affects only its own name, which becomes unrunnable and shadows a same-name
profile in a lower file. It logs `config.unusable {path, key:
"profiles.<name>", reason}` on every read, and the file's other settings still
apply. A launch that resolves to it is refused with `declared unusable:
<defect> (from <path>)`. A name that no file declares reads `not declared`.

`mugicrew doctor` prints one line per declared name, OK or WARN, naming the file
whose table won. It adds a WARN for each mapped name that is `not declared`,
for each bad table that a higher file outranks, and for each dropped OE layer.
With nothing declared anywhere it prints `agent profiles  none declared`,
listing the four paths it read.

As with `[context.*]` below, put `[profiles.*]` tables last in `config.toml`: a
top-level key written after one (such as `concurrency`) is absorbed into that
table, not read as a setting.

### Mapping kinds to profiles

The mapping lives in a `[kind_profiles]` table in mugicrew's `config.toml`.
Keys are routing kinds as the registry spells them, plus `default`. Values
are declared profile names:

```toml
[kind_profiles]
default  = "sonnet"
build    = "opus"
research = "deepseek"
```

Like `concurrency` and `auto_exit`, each key falls through on its own between
the machine-local `<git-common-dir>/mugicrew/config.toml` and the checked-in
`.mugicrew/config.toml`. A bead's profile is the first of these that is set:

1. local `<kind>`
2. shared `<kind>`
3. local `default`
4. shared `default`
5. the unnamed built-in: `claude` with no `--model`

A line that names the kind beats any `default` line, whichever layer it is in.
A key that is neither a routing kind nor `default` logs
`config.unusable {path, key}` and is ignored on its own. The rest of the table
still applies.

Edit settings by hand in the machine-local file (the local layer). The
checked-in `.mugicrew/config.toml` is the Workspace's shared default and
changes only by landing a commit. Editing it in the main worktree makes main
dirty, and every land, anchor and revert is refused until main is clean.

A HITL session records its profile name (tmux `@profile`). Resume looks that
name up among the declared profiles and ignores the mapping, so a resumed
transcript stays on the profile that wrote it. An edit to that profile
applies on the next resume.

`mugicrew stats --by profile` (and the profile group in the TUI Stats tab)
compares spend and failures per profile. The built-in shows as `(built-in)`.

### Declaring contexts

`mugicrew init workspace --context <name>=[repo:|path:]<dir>` (repeatable) declares
them; the TOML below is the reference for hand edits.

A Workspace can declare Contexts, named subtrees a Chart or Checkpoint targets,
as `[context.<name>]` tables with a `path` key. No table means no contexts. The
local layer wins as a whole table, like other settings. Put `[context.*]` tables
last in `config.toml`: a top-level key written after one (such as `concurrency`)
is absorbed into that table, not read as a setting.

```toml
concurrency = 3

[context.mugicrew]
path = "apps/mugicrew"
```

A context can instead be a nested child repo: `repo = "<dir>"` (a dir from the
Workspace root) in place of `path`. An entry with both is refused and reads as
undeclared, as does a `repo` dir without its own `.git` (a container labelled for
it is held `paused:context`). A crewmate under a container labelled for a repo
context is told its dir is its own git repo, to run git there with `git -C <dir>`,
and the child's trunk. A child with a non-`main` trunk resolves it through its own
`origin/HEAD` (a cloned child already has one). A repo dir must be in the Workspace's
`.gitignore`: an effort on a nested leaf runs in a Workspace host with only the
child materialized in it, and a non-ignored dir would stage as a bogus gitlink,
so the cut refuses and names the dir. `mugicrew integrate` run from that host
(no arguments) integrates the one child on its `effort/<id>`; run from inside
the child it exits 1.

```toml
[context.vendored]
repo = "vendor/lib"
```

### Require Initiatives

`mugicrew init workspace --require-initiatives true [--external-ref true]` writes
these; the TOML below is the reference for hand edits.

Two opt-in settings live in an `[initiatives]` table of the checked-in
`.mugicrew/config.toml` only (the machine-local layer is ignored, so no machine
opts out of a shared rule):

```toml
[initiatives]
required = true      # Require Initiatives
external_ref = true  # Enforce external ID; needs required = true
```

Both are off when the table or key is absent. A malformed table, or
`external_ref = true` without `required = true`, warns `config.unusable` and
reads as both off; the Settings tab shows the resolved state read-only. Set the
ref of an Initiative born without one with `bd update <id> --external-ref R`.

### The land gate

A Checkpoint landed with the `local` strategy is first built with nix. Set
`gate = "<attr>"` at the top level of `config.toml` (before any `[context.*]`
table) to name the flake package that build targets; with no `gate` key the
land goes ungated and records a `gate.skipped` event. A context's own gate is
`gate = "<attr>"` inside its `[context.<name>]` table, read for that context
only: the top-level key is not inherited. A gate target that will not evaluate
is an infra failure: the Landing stays open and retries (`gate.infra`).

### Refusals

A mapped profile that can't run is refused. mugicrew **never falls back** to
another layer, to `default` or to the built-in. A profile can't run when:

- its name is not declared in any of the four files (a dropped OE layer
  declares nothing), or it is declared by a bad `[profiles.<name>]` table,
- its `harness` is not `claudecode`, or `claude` is not on `PATH`, or
- an `env` value names an unset `${VAR}`.

What a refusal does depends on the launch:

- **AFK:** before any claim or worktree, the loop pauses the bead
  `paused:profile` and runs no effort. Each tick, the loop lifts the pause
  from any bead whose kind now resolves to a runnable profile and logs
  `effort.resumed {cause: profile}`. Fixing the config is the whole fix.
- **HITL launch:** the TUI flashes `profile <name>: <reason>`. The bead is
  neither claimed nor paused.
- **HITL resume:** the TUI flashes the same way and spawns nothing. The
  session is kept, so resume works once the profile is fixed.

Each refusal logs `profile.unusable` at Warn
`{bead, kind, profile, reason, launch=afk|hitl|resume}`. On AFK it also logs
`effort.paused {cause: profile}`. A model or credential that the endpoint
rejects after `claude` has started is not a refusal. It is an ordinary
failure: `session.failed` on AFK, or an error in the HITL pane.

### Pitfalls

- **`CLAUDE_CONFIG_DIR` in a profile's `env` is unsupported.** mugicrew's
  transcript check and cost metering read mugicrew's own Claude config dir,
  so they would miss the session.
- **An `env` block in `~/.claude/settings.json` overrides profile env.**
  Claude Code ranks settings-file env above process env. For example, a user
  `ANTHROPIC_BASE_URL` there silently wins over a gateway profile. Remove the
  conflicting keys from that file.

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
