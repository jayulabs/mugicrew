# Workspace configuration

## Declaring contexts

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

## Require Initiatives

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

## The land gate

A Checkpoint landed with the `local` strategy is first built with nix. Set
`gate = "<attr>"` at the top level of `config.toml` (before any `[context.*]`
table) to name the flake package that build targets; with no `gate` key the
land goes ungated and records a `gate.skipped` event. A context's own gate is
`gate = "<attr>"` inside its `[context.<name>]` table, read for that context
only: the top-level key is not inherited. A gate target that will not evaluate
is an infra failure: the Landing stays open and retries (`gate.infra`).

Instead of a nix build, the gate can be a shell command: set
`gate_command = "<shell string>"` in the same places as `gate` (the top level, or
a `[context.<name>]` table for a `repo =` context; the top-level key is not
inherited). A scope that sets both `gate` and `gate_command` is a config error
naming the scope: the land records `gate.infra` and the Landing stays open. The
command runs as `sh -c '<gate_command>'` in the Landing's tree, at the landed
repo's root (the child's root for a nested repo), in the loop's own environment
with no devShell wrapping (write `nix develop -c make test` if you want one),
under a 15 minute timeout. Exit 0 passes and the composed commit lands. Any other
ordinary exit is a builder failure: it files a base-pinned Critique and closes the
Landing `obsolete: build failed`. A command that cannot start (exec error, exit
126 or 127), the timeout, or a signal-killed shell is infra: `gate.infra`, the
Landing stays open to retry.

## The personal layer

`<os.UserConfigDir()>/mugicrew/config.toml` (`~/.config/mugicrew/config.toml` on
Linux) is a per-user layer outside any repo. It holds settings that govern the one
machine-wide binary, so no Workspace layer (checked-in or machine-local) is ever
read for them: setting one there has no effect.

```toml
[upgrade]
unattended = true  # let an opted-in loop Upgrade its npm global install
```

`[upgrade] unattended` is off when unset. `MUGICREW_UNATTENDED_UPGRADE` wins over
the key: `1`, `on` or `true` turns it on, `0`, `off` or `false` turns it off.
Opting in only lets a loop Upgrade, and only an `npm i -g` install; ProjectLocal,
pnpm/yarn global, npx, unknown and from-source builds always read off. The
Release notice opt-outs (`MUGICREW_RELEASE_NOTICE=0`, `[release] notice = false`)
turn it off too, and `MUGICREW_EFFORT` plays no part.

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
The same interval paces a Rollback with a recorded revert MR (an `MR:` line on its
bead), so a waiting gitlab Rollback reads the Forge at most once per interval; a
Rollback with no recorded MR stays due on every tick.

The `[jira] site` they poll is written by `mugicrew init workspace --jira-site <host>`
(a bare host, empty clears it); hand edits go in `[jira]` of `config.toml`.

## TUI key notes

`y` copies the selected bead's id with an OSC 52 escape sequence, so it needs
no clipboard tool on the host. tmux forwards OSC 52 only when `set-clipboard`
is `on` or `external`.
