# ADR 0037: a detached session tears down its own worktree, only on a clean exit

Status: Accepted
Date: 2026-09-30

Builds on: ADR 0036 (the session list is tmux's table).

## Context

Today the TUI's `launchDoneMsg` handler force-removes the launched session's
worktree whenever `tea.ExecProcess` returns. Under tmux, that return only means
the attach client left: rc=0 for a detach and for an exit alike. A detached
session can also end while no TUI is running. `claude --resume` finds the
conversation from any cwd, but it is only correct in the session's own worktree.
So something other than the TUI has to decide when the worktree goes.

## Decision

1. **The pane runs a wrapper, not claude directly.** The tmux command is a
   hidden subcommand, `mugicrew hitl-run --worktree=<wt> -- <claude argv>`,
   called by the absolute path from `os.Executable()` (so the tmux server's PATH
   does not matter). It runs claude, and exits with claude's exit status.
   The wrapper catches SIGHUP, SIGINT and SIGTERM (`signal.Notify`, not
   `Ignore`, so claude still gets the default disposition): Ctrl-C in the pane
   is claude's to handle, and the hangup a drop's `kill-session` sends must not
   kill the wrapper before claude exits, or `hitl.failed signal=hangup` is never
   written. This replaces the Spec's "does not trap SIGHUP"; the event stays
   best-effort only because `kill-session` does not wait for the pane.
2. **Clean exit (rc=0): the wrapper tears the worktree down at once** with the
   existing `git.RemoveWorktree` plus the temp parent. The server has
   `remain-on-exit failed`, so the session then leaves the table and the branch
   is free. This matches today's behaviour (uncommitted changes are discarded).
   A teardown that fails after rc=0 fails the session: the wrapper exits with
   the git error's code (e.g. 128), not claude's 0, so the pane is kept and its
   `@worktree` stays droppable. The header then reads `exited (rc=128)` — the
   teardown's status, not claude's (a deviation from the Spec's exit-status
   passthrough).
3. **Failed exit (non-zero or a signal): nothing is torn down.** The dead pane
   stays listed as _exited_ with its status, and the worktree stays for a resume
   in the same cwd. It lives until it is resumed or dropped. An attached human
   sees the dead pane and detaches by hand.
4. **Resume runs through the same wrapper, in the kept worktree**, so a later
   clean exit tears it down as in 2.
5. **Drop = `kill-session` then `worktree remove --force`**, discarding
   uncommitted changes. Committed work is safe on the branch.
6. **The TUI never tears down.** Attach return only reloads.
7. **An exited session holds its branch like an alive one.** Git's one-checkout
   rule (the existing busy interlock) blocks sibling launches and the AFK loop
   on that branch. The busy status names the holding bead so the human knows
   what to drop.
8. **No startup sweep.** Worktrees orphaned by a killed tmux server or a reboot
   (no `@worktree` left to read) are the reboot/crash question.

The wrapper is the one place that sees every session end, so it also writes the
session's `hitl.*` events (see the amendment in ADR 0029).

## Considered options

- **The TUI sweeps on start or refresh:** it can't see a clean exit, because the
  session and its `@worktree` have already left the table. It would have to scan
  `/tmp/mugicrew-wt-*`, and nothing happens until someone opens the TUI.
- **Keep every worktree until an explicit drop:** after every normal session the
  Chart's branch stays busy until someone remembers to drop it.
- **An inline `sh -c` wrapper:** no new Go, but it copies the teardown into
  shell and has nowhere to emit an event from.
- **`remain-on-exit on`:** keeps clean exits listed too, but pins an attached
  client on a dead pane after every normal quit.

## Note: the pane's env comes through a handoff file (amendment, 2026-10-03)

The tmux server spawns every pane with the env it captured at its own start,
but claude in a launched or resumed pane must run under the launcher's current
env (Spec FR5 / Decision 2). Passing the env inline as `-e KEY=VAL` overflows
tmux's ~16KB client-command cap, so the amendment picked a file instead (option 1). Start and Resume write `os.Environ()` minus
`TMUX`/`TMUX_PANE` as NUL-separated `KEY=VAL` to a fresh 0600 file under
`$XDG_RUNTIME_DIR` (falling back to `os.TempDir()`). They pass only its path,
as `-e MUGICREW_ENV_FILE=<path>`, and remove the file if the tmux call fails.
`hitl-run` reads the file and deletes it at once, then replaces its env with
the file's contents, keeping tmux's `TERM`. A missing or unreadable file fails
the session (`hitl.launched` then `hitl.failed`; the agent never runs) rather than falling back to
the stale env.
