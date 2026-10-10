# ADR 0041: a gone session is not resumable; an orphaned worktree is dropped on `l`, with a confirm

Status: Accepted
Date: 2026-10-03

Builds on: ADR 0036 (tmux's table is the only session list), ADR 0037 §7–§8 (an
exited session holds its branch; no startup sweep), ADR 0038 §3–§4 (`l`, the
drop confirm).

## Context

A killed tmux server or a reboot destroys every session, and claude dies from
SIGHUP. The table is then empty, so the session reads as _gone_ (`·`). The
transcript survives under `~/.claude`, but its id lived only in the dead
`@session` option. The worktree is orphaned and **still holds the branch**:
git keeps refusing the branch (`already used by worktree`) even when the
directory is gone (`prunable`), until `git worktree prune`. If `/tmp` is not
tmpfs, the directory survives too, together with any uncommitted edits. So
without a decision, `l` on that bead hits busy, and no session exists that
`x drop` could act on.

## Decision

1. **Gone is not resumable.** Only an _exited_ session resumes (ADR 0038 §3).
   After a crash or reboot, the bead offers a fresh launch only. Committed work
   is safe on the branch. A human who knows the id can still run
   `claude --resume` by hand. The Spec's "gone can be resumed or dropped" is
   read as: exited resumes or drops; gone launches fresh.
2. **An orphaned worktree is a `mugicrew-wt-*` git worktree that holds a bead's
   branch while no session on the mugicrew server has it as `@worktree`** (or no
   server is running). It is found lazily: when `l`'s fresh launch hits busy
   (`git.IsBusy`) and the holder is an orphan. A tree on the branch of a live AFK
   effort is never an orphan: the loop's worktree is a `mugicrew-wt-*` tree with
   no session, so the busy check names the effort instead (beyond the Spec's
   Decision 8, which only knew of sessions; amended).
3. **`l` then reuses the drop confirm:**
   `"<title>": branch held by an orphaned worktree <path> (crashed session) — y drop it and launch · n cancel`.
   `y` runs `worktree remove --force`, then `worktree prune`, then the launch.
   Only `y` proceeds. A holder that is not an orphan (a live or exited session,
   or a non-mugicrew worktree) keeps today's busy status.
4. **Nothing clears orphans automatically.** There is no startup sweep, no
   prune-before-launch, and no cleanup by the AFK loop: the loop keeps skipping
   the branch as busy until a human drops the orphan. Uncommitted edits are
   only ever discarded after a human says `y`.
5. **No bd write.** The dead agent's claim stays. The relaunch runs as the same
   human assignee and claims the bead again (ADR 0036 §4: a pure view).

## Considered options

- **Resume a gone session from a durable record:** folding `hitl.launched` goes
  against ADR 0033, a side file is the registry ADR 0036 rejected, and mapping
  `~/.claude/projects/<encoded cwd>` back to the orphan relies on claude's
  private path encoding. All of this for a rare event.
- **Automatic prune or force-remove:** it silently discards uncommitted work
  that survived a reboot.
- **A startup sweep:** work on every start for a rare case. It also needs the
  same confirm, but would ask it away from the bead that is blocked.
- **Manual cleanup only:** the busy status would name a path the human must
  clear with git by hand, which is a dead end inside the TUI.
