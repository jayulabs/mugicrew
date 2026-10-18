# ADR 0066: tmux status bar and help hints — a bare TUI, a HITL bar that flips when the bead closes

Status: Accepted — §2 superseded by ADR 0229 (…hints-bar-and-key-reference…)
Date: 2026-10-05

Amends: ADR 0038 §5 (the HITL status line), ADR 0058 §4 (the `?` key reference).
Builds on ADR 0065. Decided in grilling.

## Context

Under ADR 0065 the TUI is a session on the private tmux server, and F12 in a HITL
session switches back to the human's TUI. The old HITL line `<id> · <title> │ F12
detach` now names the wrong action. It also never tells the human when the
session's work is done, which leaves them unsure whether to `/exit` or F12 away.
mugicrew passes no `-f`, so the user's `~/.tmux.conf` loads into the private server.
That means the prefix may not be `C-b`.

## Decision

1. **The TUI session has no tmux status bar** (`status off`). The TUI's own top bar
   and footer remain the only chrome (ADR 0024/0026).
2. **The `?` key reference gains a tmux section.** It reads the real prefix from
   tmux once at startup (`show -gv prefix`) and renders it in the reference's key
   notation:
   - `<prefix> ?  tmux keys — all tmux commands work`
   - `<prefix> d  leave tmux — quits mugicrew` (ADR 0065 §4)
3. **The HITL status bar:**
   - `status-left`: `<id> · <title>`
   - `status-right` while the bead is open: `F12 back to TUI · /exit to finish`
   - `status-right` once the bead is closed: `✓ <id> closed — /exit to finish · F12 back to TUI`,
     in a loud style (green / reverse).

   No prefix keys are advertised. The F12 label names where you go, not "detach"
   or "suspend", because the session keeps running (glossary: Detached session).
   A dead pane keeps the same bar, and tmux's "Pane is dead" plus F12 cover it.
4. **Done = the session's bead is closed.** The TUI's existing refresh tick
   derives each live HITL session's `status-right` from its bead's status and
   sets it with `set-option`. It is derived, not latched, so a reopened bead
   reverts the bar. Several TUIs set the same value, which is harmless. With no
   TUI running (a manual `tmux -S … attach`), the bar does not flip. That is
   accepted.
5. **`/exit` is literal** because Claude Code is the only harness. When a
   second harness lands, the harness supplies the finish wording (same deferral
   as the Chart's hooks ruling).

## Considered options

- **A TUI status bar** (key hints, or HITL count and loop state): it would duplicate
  the footer, the single command surface, and cost a row.
- **`F12 suspend`**: implies the agent pauses, but it keeps running. This is the
  ambiguity the Chart is removing.
- **Advertising `C-b d` / `C-b ?` on the HITL bar**: `d` quits the TUI too, and the
  prefix may be the user's own. The `?` page covers tmux help instead.
- **Done = the agent's closing line**: not observable outside the conversation. The
  closing line (ADR 0067) stays the in-conversation signal.
- **The `hitl-run` wrapper polling bd, or a tmux `#(…)` status command**: new bd
  polling per session (embedded-Dolt bd serializes loads). The `#()`
  command is also fragile about the shell's environment. The TUI tick already has
  the statuses.
