# ADR 0036: the detached-session list is tmux's own session table

Status: Accepted
Amended by: ADR 0065 §2 (list narrowed to `@bead` sessions)
Date: 2026-09-30

Extends: ADR 0031 (observational view outside the graph) to HITL sessions the TUI
launched.

## Context

Detachable HITL sessions keep running as live processes after the
TUI detaches or quits, so the TUI must rediscover "which sessions exist, for which
bead, in which worktree, alive or gone" from something durable. The candidates
were tmux's own session table, folding `hitl.*` events from the event log, or a
side file under the git dir.

## Decision

1. **tmux's session table is the only source.** mugicrew keeps no registry or
   copy. Each session carries its bead id, claude session id and worktree path
   as tmux user options (`@bead`, `@session`, `@worktree`). The TUI reads the
   table (`list-sessions -F`) on start and on refresh. **Alive** means listed
   with a live pane; **exited** means listed with a dead pane and an exit status;
   **gone** means not listed, or no server (after a reboot or a killed server).
2. **One tmux server per repo, owned by mugicrew:** the socket is
   `$(git rev-parse --git-common-dir)/mugicrew/tmux.sock` (`tmux -S`), next to
   `events.jsonl`. The list is simply "every session on this server", with no
   filtering. mugicrew's options and key bindings stay out of the user's tmux.
   Unix socket paths are limited to about 108 bytes, so a very deep repo path
   makes launch fail with a clear error.
3. **One session per bead:** the session is named after the bead id (tmux
   rewrites `.`/`:` to `_`; `@bead` keeps the exact id). tmux refuses a
   duplicate name, which matches the existing branch-busy check.
4. **A pure view, with no bd writes.** A live session leaves no mark on the bead.
   bd sees only the claim the agent inside the session makes itself. This meets
   ADR 0031 (a) and (b) and its §4 ban on process handles in the graph.
5. **The event log is never read for liveness** (ADR 0033). `hitl.*` events stay
   history.
6. **tmux is a required runtime dependency, pinned in the devShell.** This
   reverses Chart triage decision 5 (optional with a one-shot fallback). With
   tmux off PATH, launch refuses with a status message rather than falling back
   to the old unwrapped launch. There is one launch path and one teardown path.

## Considered options

- **Folding the event log:** it would report sessions as alive after a reboot
  until something writes an exit event, and it goes against ADR 0033 (history is
  not liveness).
- **A side file:** it duplicates tmux's table and drifts from it on a server kill.
- **The user's default tmux server or one machine-wide `-L mugicrew` server:**
  the first leaks bindings and options into the user's tmux, and both need
  filtering by repo.
