# ADR 0125: An Agent Profile is resolved where ctx is assembled; its env rides process env, never argv

Status: Accepted
Date: 2026-10-10

Amends: the mc-8u0o.1 finding that profile env merges into the waiting-hooks `--settings` JSON

## Context

Per-kind Agent Profiles (ADR 0093 catalog) mean a launch must reach the Harness
for its profile, where main builds one `claudecode.New("")` today. The Spec ships
no second harness backend. Decided on grilling bead mc-8u0o.4 (Chart "Per-kind
Agent Profiles from an OE-level catalog").

## Decision

1. **Resolve at ctx assembly.** Dispatch (per effort), the TUI (per launch) and
   resume (from the session record) resolve the profile and put a backend built
   for it in ctx. `ctx.Harness()`, the `Harness` interface, crewmates and
   `ResumeCommand(sessionID)` are unchanged: the backend already holds the
   profile's model and env.
2. **Construction is a name → constructor map in main**, one entry
   `claudecode`; the claudecode `Backend` carries `Model` and `Env`. A name not in
   the map is an unusable selection (its handling is a separate decision).
3. **The session records only `@profile`** beside `@session` ("" = the built-in
   default). Resume re-resolves it from the current catalog; a profile that has
   vanished fails the resume with a status, nothing spawned, rather than silently
   dropping a gateway env. No `@harness` yet: absent means claudecode, so the Pi
   backend adds it without migrating live sessions.
4. **Claude-specific spots stay claudecode-only** while it is the one backend:
   liveness's `claude` process-name match, `MeterCost` wired into `hitl-run`,
   `ReadUsage` wired into the TUI, `hitl-hook`'s parse of Claude hook JSON and the
   waiting-hooks `--settings`. They are the Pi backend Signal's checklist. The
   `--resume` in `hitl-run` / `resumePane` is mugicrew's own flag, already
   harness-neutral.
5. **Profile env goes through process env:** AFK via `cmd.Env`, HITL via a new
   `AgentCommand.Env` written into the 0600 pane env file. Not `--settings`:
   that is argv, so expanded `${VAR}` credentials would show in
   `/proc/<pid>/cmdline`, `ps` and the tmux pane start command.
6. **`CLAUDE_CONFIG_DIR` in a profile's env is unsupported** (documented): the
   transcript check and `MeterCost` read mugicrew's own config dir.

## Considered Options

- A router Harness resolving per Invocation (needs `Kind` on `Invocation`):
  rejected, more plumbing for the same result.
- `@profile` + `@harness` now, or snapshotting the model: rejected as unneeded
  with one harness; `@harness` can be added later with claudecode as its default.
- Interface methods for liveness / cost / usage now: rejected, one
  implementation each.
- `--settings` env everywhere: it would beat a user `~/.claude/settings.json`
  `env`, but puts secrets in argv.

## Consequences

An `env` block in the user's `~/.claude/settings.json` overrides a profile's
process env (settings-file env beats shell env); documented as a
misconfiguration, not worked around.
