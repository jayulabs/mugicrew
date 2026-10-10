# ADR 0065: the TUI is a session on mugicrew's private tmux server; HITL sessions are reached by switch-client

Status: Accepted
Amended by: ADR 0100 §1 (`b` launches or resumes without the hand-over)
Date: 2026-10-05

Amends: ADR 0036 §2, ADR 0038 §3 and §5, ADR 0042 §2 and §4. Leaves ADR 0053
unchanged. Decided in grilling.

## Context

The TUI was a plain alt-screen app outside tmux. `l attach` ran a nested
`tmux attach` under `tea.ExecProcess`, which suspended the TUI. That meant no
place for tmux status-bar hints on the TUI, and inside the user's own tmux every
attach nested. The Chart wants the TUI itself to be a tmux window. Research
shows that a pane can move its own client between sessions on the same server
with `switch-client`, and that a server-wide `detach-client` would drop the
whole terminal out of tmux.

## Decision

1. **Topology.** Each running TUI is its own session (`tui-<id>`, one window)
   on the repo's private server (ADR 0036 §2), next to the
   one-session-per-bead HITL sessions. Reaching a HITL session (`l attach`, and
   the hand-over after `l` launch/resume) is `switch-client` of the human's own
   client. The TUI keeps running in its pane. No HITL session is ever nested
   inside the TUI's pane.
2. **Always in tmux.** A bare `mugicrew` that is not already inside the private
   server (`$TMUX`'s socket field ≠ our socket) starts a TUI session there and
   attaches to it. There is no opt-out flag. Inside the user's own tmux it
   strips `TMUX`/`TMUX_PANE` and nests once, at launch (ADR 0042 §4, moved).
3. **One TUI per invocation.** Two terminals get two independent TUI sessions,
   as before. They are not mirrored. The session list (ADR 0036 §1–2) is narrowed
   to sessions carrying `@bead`, so TUI sessions are never listed or counted.
4. **Quit is a full quit. No TUI ever runs in the background.** The launcher
   runs the tmux client as a child (not `exec`) and ignores SIGHUP. When that
   client exits for any reason (`q`, `C-b d` from any session, terminal closed),
   it kills its own TUI session. The TUI window sets `remain-on-exit off`, so a
   crash quits rather than leaving a dead pane. The TUI's stderr goes to
   `<git-common-dir>/mugicrew/tui.out`, truncated per launch (the ADR 0053 §3
   pattern), so a panic trace survives. HITL sessions and the loop are
   untouched by a TUI quit.
5. **F12 = back to my TUI.** F12 is bound only in a HITL-only key table (the
   per-session `key-table` option), so it does nothing on a TUI session. It
   switches the pressing client to the TUI session recorded for that client
   (keyed by `client_tty`). That works after hopping between HITL sessions with
   `C-b (`/`)`, and per terminal when two terminals mirror one HITL session. If
   no TUI exists for that client (manual `tmux -S … attach`), F12 detaches as
   before. `C-b d` in a HITL session leaves tmux entirely, which by §4 also quits
   that terminal's TUI. The HITL session keeps running.
6. **A clean HITL exit returns its viewers.** Just before exiting 0, the
   `hitl-run` wrapper switches every client viewing its session to that
   client's TUI (the §5 lookup). Without this, tmux's default
   `detach-on-destroy` would detach the terminal, and §4 would then quit the
   TUI. A failed exit keeps its dead pane on screen (`remain-on-exit failed`),
   and F12 returns. How the TUI then labels finished vs detached is a separate
   decision (ADR 0067).
7. **The detached loop is unchanged (ADR 0053).** Its `setsid` re-exec escapes
   the pane's SIGHUP. Implementation adds one check: a loop started from a TUI
   pane survives killing that pane.

## Considered options

- **The TUI as a window inside HITL sessions, or one session with a window per
  bead:** breaks ADR 0036 §3 (the session table, one session per bead, is the
  list).
- **The TUI in tmux but keeping the nested attach:** tmux-in-tmux from one
  server, with two status bars. Worse than before.
- **Opt-in tmux (`--tmux`):** two attach paths and two F12 meanings to keep.
- **One shared TUI per repo (`new-session -A`):** two terminals would share one
  cursor and the size of the smaller terminal. Today's independent TUIs are lost.
- **Allowing a backgrounded TUI that the next `mugicrew` reattaches to:**
  rejected by the human, quit must be a full quit. A `client-detached` hook
  that kills a TUI session with no clients was also rejected: once the client
  has switched to a HITL session, the TUI session has zero clients while still
  in use.
- **F12 as `switch-client -l`, or `detach-on-destroy previous` on HITL end:**
  no code, but "last"/"previous" can be another HITL session rather than the
  human's TUI.
