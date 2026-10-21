# ADR 0042: concurrent sessions are uncapped; attach shares, and nests inside the user's tmux

Status: Accepted
Amended by: ADR 0321 §4 (a nested TUI launch sets a second prefix).
Amended by: ADR 0065 §2, §4 (F12 back to the TUI; nesting happens once at TUI launch)
Date: 2026-10-04

Builds on: ADR 0036 (one mugicrew tmux server per repo, one session per bead),
ADR 0038 (`l` attaches through `tea.ExecProcess`, `F12` detaches).

## Context

The Chart left open whether many detached sessions may run at once, and what
"reattach one while another is attached" means. Because `l attach` runs under
`tea.ExecProcess`, the TUI is suspended while you're attached, so it can't take a
second `l`. Research §6 also found that `tmux attach` with `$TMUX` set (the TUI
running inside the user's own tmux) is refused with "sessions should be nested
with care", so a plain attach from there fails.

## Decision

1. **No cap on concurrent sessions.** Sessions on different beads share nothing.
   The one-session-per-bead rule and the branch-busy interlock already prevent
   collisions. Machine load is the human's call, and the tab bar's `N live` shows
   the count.
2. **Switching sessions = `F12` detach, then `l` on the other row.** mugicrew adds
   no switch key or picker. tmux's own same-server switching (`C-b s`, `C-b (`,
   `C-b )`) is left working but not advertised. A native switch is harmless,
   because a detach still returns the attach to the TUI, and the rows come from
   the tmux table.
3. **Two clients on one session are allowed** (plain `attach`, not `attach -d`,
   no refusal). For example, two TUIs in two terminals both pressing `l` on the
   same bead mirror one session. That's tmux's default and needs no code. Remote
   or shared attach stays out of scope.
4. **Inside the user's tmux, nest.** Every attach, launch and resume command
   runs with `TMUX` removed from its environment. The cost is a double status bar
   and a double prefix. `F12` still detaches the inner client, because the outer
   tmux passes unbound keys through. The dedicated server (ADR 0036 §2) stays.

## Considered options

- **A cap or warning past N sessions**: it guards against nothing the interlocks
  don't already cover.
- **An advertised in-tmux session picker (`F11`)**, or **disabling native
  switching**: new key surface or code, with no new ability.
- **Steal (`attach -d`) or refuse a second client**: both need code, and they
  break a harmless mirror.
- **Refusing to run inside tmux**: it locks out a common setup. **An outer popup
  or window** in the user's tmux was untested, and it couples mugicrew to the
  user's server, which ADR 0036 §2 avoids.
