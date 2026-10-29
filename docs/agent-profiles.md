# Agent Profiles

By default every AFK run and HITL session runs `claude` with no `--model`. To
run a bead kind on a different model or endpoint, declare Agent Profiles in
the OE's catalog or in mugicrew's `config.toml` and map kinds to them (root
ADR 0093, ADR 0122, ADR 0123, ADR 0125, ADR 0309). If you map nothing, nothing changes.

## The catalog

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

### A non-Anthropic model through a gateway

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

## Profiles in config.toml

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

As with `[context.*]` tables ([Declaring contexts](configuration.md#declaring-contexts)), put `[profiles.*]` tables last in `config.toml`: a
top-level key written after one (such as `concurrency`) is absorbed into that
table, not read as a setting.

## Mapping kinds to profiles

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

## Refusals

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

## Pitfalls

- **`CLAUDE_CONFIG_DIR` in a profile's `env` is unsupported.** mugicrew's
  transcript check and cost metering read mugicrew's own Claude config dir,
  so they would miss the session.
- **An `env` block in `~/.claude/settings.json` overrides profile env.**
  Claude Code ranks settings-file env above process env. For example, a user
  `ANTHROPIC_BASE_URL` there silently wins over a gateway profile. Remove the
  conflicting keys from that file.
