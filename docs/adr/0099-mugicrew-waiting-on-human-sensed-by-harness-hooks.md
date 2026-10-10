# ADR 0099: Waiting on the human is sensed by harness hooks behind the seam, recorded as `@waiting` on the session

Status: Accepted
Date: 2026-10-08

Narrows the earlier triage ruling "no harness hooks". Builds on ADR 0036 §1
and §4 and ADR 0037. Decided in grilling.

## Context

A detached HITL session can sit **waiting** on the human, and the TUI should
show it: the agent has ended its turn or raised a permission prompt or question,
and the human hasn't replied. tmux by itself can't tell this reliably. Its
silence and bell alerts latch on a detached session, `pane_title` never changes,
and `window_activity` age only shows that a pane has been quiet for about 10 s,
without saying which kind of wait. Only reading the screen tells the states
apart, and that depends on undocumented UI text. Claude Code hooks report every
edge exactly. The triage ruling, however, barred harness hooks, because Claude
Code is the only harness and no cross-harness hook standard exists.

## Decision

1. **Sensed by harness hooks.** The claudecode backend adds
   `--settings '{"disableAllHooks":false,"hooks":{…}}'` to both `LaunchCommand`
   and `ResumeCommand`. The `--settings` hooks merge with the user's and the
   Workspace's hooks, and no settings file is written. The `Harness` interface
   does not change. The backend stays bd-blind and bead-blind. A harness with
   no such hooks never shows waiting. Cross-harness hook machinery stays
   deferred. This narrows that ruling: hooks are allowed when they sit
   inside a harness backend and write only `@waiting`.
2. **One hidden writer, `mugicrew hitl-hook`.** Each injected hook runs this
   binary's own path (as `paneArgs` does for `hitl-run`) with the hidden
   `hitl-hook` subcommand. The subcommand reads the hook's stdin JSON, applies
   the edge rules in §4, and sets `@waiting`. It always exits 0 and prints
   nothing to stdout: exit 2 would block `Stop`/`UserPromptSubmit`, and stdout
   on `UserPromptSubmit` is injected into Claude's context. It finds its
   session through a mugicrew env var (for example
   `MUGICREW_HITL_PANE=<socket>,<pane id>`). hitl-run exports this var from the
   `TMUX`/`TMUX_PANE` values it reads before it strips them from the agent's
   environment.
3. **The bit is `@waiting` on the bead's session.** The value is `turn` (the
   turn ended) or `ask` (a permission prompt or question is up). Unset or empty
   means not waiting. The TUI tick reads it as one more `#{@waiting}` field in
   `listFormat`. This is not a bd write, so the session list stays a pure view
   of tmux (ADR 0036 §4).
4. **Edge rules.**
   - Set `turn` on `Stop` or `StopFailure`, even when `background_tasks` is not
     empty. A turn the agent starts itself fires `UserPromptSubmit`, which
     clears it.
   - Set `ask` on `PermissionRequest`, whatever its `agent_id`, and on a
     `Notification` of type `elicitation_dialog` or `elicitation_url_dialog`.
   - Clear either value on `UserPromptSubmit`.
   - Clear only `ask` on `PostToolUse` or `PostToolUseFailure`. These never
     clear `turn`, because a background subagent's tools can run after `Stop`.
   - Ignore `SubagentStop`.
5. **Only a reply clears it, not a look.** Reattaching or `switch-client` into a
   waiting session leaves it waiting. Waiting means "needs your reply", not
   "unseen".
6. **Exit and resume.** The session code (harness-neutral) unsets `@waiting`
   whenever it spawns or respawns a pane, so a resumed session starts clean.
   While the pane is dead, exited outranks waiting. The bead closing leaves the
   bit alone, because Done takes precedence at render time.

## Considered options

- **tmux only (`window_activity` age):** it notices about 10 s late, can't tell
  a finished turn from a permission prompt, probably reads a quiet long tool
  run as waiting, and flips back and forth when the human attaches.
- **tmux silence and bell alerts:** they latch once per attach on a detached
  session. The bell also misses turn end and rings the attached human's
  terminal.
- **Screen scraping (`capture-pane`):** it relies on undocumented,
  version-specific and width-dependent UI text, and costs an exec per session
  per tick.
- **Hooks with a `window_activity` cross-check to expire stale bits:** this is
  extra logic. The edges it would catch (interrupt, manual deny) happen while
  the human is at the pane. Add it if stale bits show up.
- **An inline `tmux set-option` hook with the bead id and socket baked in:**
  `ResumeCommand` gets no bead id, so the contract would have to change, and the
  edge rules would live in shell.

## Consequences

- **Known misses, all accepted:**
  - Ctrl-C interrupt and a manual deny leave a stale bit until the next hook
    fires.
  - The workspace trust dialog runs before any hook can, so it is never sensed.
  - A background subagent's `PostToolUse` can clear a main-thread `ask` early.
- A managed `disableAllHooks` or `allowManagedHooksOnly` policy silences the
  sensor. Waiting then simply never shows.
- In auto permission mode, fewer `ask` edges occur.
