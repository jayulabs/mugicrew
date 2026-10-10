# ADR 0038: detached sessions surface on the bead row; `l` attaches or resumes, `x` drops

Status: Accepted
Amended by: ADR 0065 §3, §5 (`l attach` is switch-client; F12 returns to the TUI); ADR 0100 (`b` launches or resumes without switching)
Date: 2026-10-02

Builds on: ADR 0026 (footer command surface), ADR 0032 §5 (liveness column),
ADR 0036 (session list is tmux's table), ADR 0037 (exited keeps the worktree).

## Decision

1. **No Sessions tab.** A detached session shows in the existing liveness column
   on its bead's row (Graph and Inbox), driven by the tmux table: `●` alive, red
   `✕` exited, `·` none. Gone renders as `·`, the same as never launched, because
   a pure view of tmux can't tell them apart (reboot recovery is ADR 0041).
2. **A bead with a session is never hidden**: it stays visible under
   `h hide-closed` even when closed (the agent may close the bead before the human
   detaches), and in the Inbox, which otherwise lists only open HITL beads. The tab
   bar always shows `N live` (`0 live` included) beside `N open HITL`, so sessions
   in folded groups are still counted. (Inbox and `0 live` amended.)
3. **`l` is per-session state** (footer label in parentheses):
   - alive → reattach (`l attach`): `tea.ExecProcess(tmux attach -t =<bead>)`.
   - exited → resume in the kept worktree (`l resume`), through the `hitl-run`
     wrapper with `claude --resume <@session>`.
   - none → launch as today (ADR 0026).
   Attach and resume show **whatever the bead's status** (closed included) and
   skip the blocked-launch confirm, since nothing new is launched. A fresh start
   on an exited bead is drop, then `l`. `l` never asks "resume or fresh?".
4. **`x drop`** is shown whenever the selection has a session (alive or exited).
   It always asks first, using an inline confirm mode like blocked-launch:
   `drop "<title>"? kills the session and discards uncommitted changes in its
   worktree`, with `y drop · n/esc cancel` (only `y` drops).
5. **Detach inside the session:** the mugicrew tmux server binds `F12` to
   `detach-client` as a root key, and `C-b d` still works. Its status line reads
   `<bead id> · <title> │ F12 detach`. Both are scoped to mugicrew's own server
   (ADR 0036 §2), so nothing leaks into the user's tmux.
   (Amended by ADR 0065 §5 and ADR 0066 §3: F12 returns to the TUI; the line is
   `<id> · <title>` left and `F12 back to TUI · /exit to finish` right.)
6. **The detail header gets one session line**, shown only when a session exists:
   `session: alive · <worktree>` or `session: exited (rc=N)`, or `(signal=X)` when a signal killed the pane, as the Spec asks (amended by ADR 0039 §3: no "· resumable" claim). This is
   the only place the exit status shows. The wrapper (ADR 0037 §1) survives a
   signal that kills claude and exits 128+n, so an exit status over 128 is read
   back as signal n; `pane_dead_signal` itself is set only if the wrapper dies.
   (Amended.)
7. **No fallback UX.** tmux is required (ADR 0036 §6): with tmux missing, `l`
   refuses with a status message.

## Considered options

- **A dedicated Sessions tab or list**: a second place to look, and it would copy
  what the row already carries. One session per bead (ADR 0036 §3) makes the row
  the natural place.
- **`l` on an exited session asks resume-or-fresh**: fresh can't run while the
  exited session holds the branch, so the prompt would always reduce to "drop
  first".
- **Confirming drop only for alive sessions**: an exited session's worktree can
  also hold uncommitted work.
