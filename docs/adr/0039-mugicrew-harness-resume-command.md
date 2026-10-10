# ADR 0039: the harness gains `ResumeCommand(sessionID)`; tmux wrapping lives in `session`

Status: Accepted
Date: 2026-10-02

Builds on: ADR 0036 (`@session`/`@worktree` in tmux's table), ADR 0037 (the
`hitl-run` wrapper, resume in the kept worktree). Amends: ADR 0038 §6 (header
wording).

## Context

Resuming an exited session (ADR 0038 §3) needs claude's resume argv. By then the
launch-time `AgentCommand` is gone (the TUI may have restarted); only tmux's
`@session` and `@worktree` survive. The harness is the single choke point for
spawning an agent (spec §5), and only one backend (claudecode) exists.

## Decision

1. **A new required `Harness` method:**
   `ResumeCommand(sessionID string) (AgentCommand, error)`. It is built at resume
   time from `@session`, and claudecode returns
   `AgentCommand{Argv: [claude --resume <uuid>], SessionID: <uuid>}` (since ADR
   0086, plus `--add-dir <crewdocs>`, a launch flag that resume re-passes). It returns
   the concrete `AgentCommand`, because a resume is never a checklist. The caller
   sets cwd to `@worktree` and wraps the command in `hitl-run`, as at launch.
2. **There is no "unsupported" sentinel or optional interface.** A harness that
   can't resume returns a plain error, which the TUI shows as the `l resume`
   status. Add a sentinel when a second harness exists.
3. **`ResumeCommand` checks that the transcript still exists**
   (`~/.claude/projects/*/<uuid>.jsonl`, which is pruned after about 30 days) and
   returns an error when it is gone. The claude path knowledge stays in the
   harness. ADR 0038 §6's header becomes `session: exited (rc=N)`, with no
   "· resumable" claim made in advance. If resume fails, the human uses `x drop`.
4. **The tmux wrapping lives in the existing `session` package**: start
   (socket, options, F12 binding, `hitl-run`), list, attach and drop. That package
   is already the shared HITL launch seam, so a future `mugicrew attach <bead>`
   verb can reuse it. The TUI keeps only key handling and rendering. Resume is
   its own `session.Resume`, a `respawn-pane` of the exited session's dead pane,
   not `Start(..., resume=true)` as the Spec's Decision 6 words it: the session
   and its `@` tags already exist, and a new-session would be refused as a
   duplicate.
5. **Only the TUI detaches.** The `mugicrew signal triage` CLI verb stays a plain
   foreground run with today's teardown.
6. **Inside the TUI, a Signal launches through Nami's `Launch` directly**, not
   through the `mugicrew signal triage` verb. That way it gets a pre-assigned
   session id and can be resumed. The verb remains for the CLI only.

## Considered options

- **A `ResumeArgv` field on `AgentCommand`:** it would have to be persisted as a
  tmux option to survive a TUI restart, which duplicates `@session`.
- **The TUI builds `claude --resume` itself:** that spawns an agent outside the
  harness choke point.
- **A `CanResume` method for the header:** a second method, and a filesystem
  glob on every render.
- **Leaving Signal on the verb and passing `--session-id`:** this adds a flag
  instead of removing an indirection.
