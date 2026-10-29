# ADR 0356: mugicrew hitl-hook resolves its Workspace root from the pane's socket

Status: Accepted
Date: 2026-10-28
Amends: ADR 0112 (auto-exit-duration-is-a-layered-setting) on where the hook reads from; ADR 0111 (hitl-auto-exit-countdown) Spec D2's "setting of the event's cwd".
Leans on: ADR 0036 (detached-session-list-is-tmux-table) §2, the socket at `<git common dir>/mugicrew/tmux.sock`.

## Context

hitl-hook read auto_exit from the git top level of the Stop event's `cwd`.
Claude Code carries an in-project `cd` into that cwd, so an agent that `cd`s
into a nested `repo =` child (ADR 0167 (one-workspace-many-repos…)) made the
hook read the child's layers and arm the 1m default while the TUI showed the
Workspace's 30s. In every linked worktree it also read the branch's checked-in
shared layer, not the main checkout's that the TUI shows
(docs/research/hitl-hook-auto-exit-root-divergence-by-layout.md). The Chart
"Hook resolves auto_exit from the same layers the TUI shows" settled that the
hook reads the Workspace root the TUI uses.

## Decision

1. **The anchor is the socket in `MUGICREW_HITL_PANE`.** The hook takes the
   socket it already parses, steps up two dirs to the Workspace's git common
   dir, and takes the root as the first stanza of
   `git --git-dir=<common> worktree list --porcelain`: the same call as the
   TUI's `git.RepoRoot()`. Because the tmux server is per repo, the socket and
   the TUI's root resolve from the same repo by construction. The event `cwd`
   no longer takes part.
2. **No root, the default.** If the root can't be resolved (the git call fails,
   e.g. a Workspace torn down under a live session), the hook arms
   `DefaultAutoExit`, never a read from the event cwd: a quiet read of another
   tree's layers is the bug this fixes. Without `MUGICREW_HITL_PANE` the hook
   is already a no-op, so an older pane or a hook run outside a mugicrew pane
   never reaches the setting.
3. **The socket's location is now load-bearing.** Moving the socket away from
   `<common dir>/mugicrew/` (ADR 0036 §2) must move this resolution with it.

## Considered Options

- **A root env var hitl-run exports beside `MUGICREW_HITL_PANE`.** Exact and no
  git call at arm time, but a new hitl-run → agent contract, and sessions
  started before the change lack it until resumed, so it would still need this
  ADR's anchor as a fallback.
- **A `@root` tmux option.** Same gap, plus a re-set on every respawn.
- **`CLAUDE_PROJECT_DIR`.** Names the session's launch tree, not the root, so it
  still needs the git call; its behaviour is pinned only by a probe, and only
  Claude Code sets it.
- **Patching the cwd walk to skip a nested child.** Needs the `repo =` table,
  read from the very root the hook doesn't know.

## Consequences

- One `git worktree list` per arming Stop; every other edge reads no setting.
- A bare Workspace's root is the bare dir for both the TUI and the hook, so the
  two still agree (a ponytail of `git.RepoRoot`).
