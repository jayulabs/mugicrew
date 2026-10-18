# ADR 0299: HITL session keys follow byobu: F1 menu, F6 back to TUI, F10 bead pane, with cancel in the F1 menu

Status: Accepted
Date: 2026-10-18

Amends: ADR 0065 (mugicrew-tui-is-a-session-on-the-private-tmux-server) §5, ADR 0066 (mugicrew-tmux-status-bar-and-help-hints) §3, ADR 0111 (mugicrew-hitl-auto-exit-countdown)

The HITL key table (mchitl) bound F12 (back to the TUI), F11 (cancel an
Auto-exit) and F10 (bead pane), a set that follows no convention. It now
follows byobu's, since a HITL session is a tmux session: F1 help, F6 leave,
F10 as it was.

## Decision

1. **byobu over CUA.** CUA (htop, mc) is the other common F-key convention; F1
   help is common to both. byobu wins because this is a tmux session.
2. **mchitl binds exactly F1, F6 and F10.** F1 opens the "HITL keys" menu
   (mechanism: ADR 0296 (mugicrew-the-hitl-f1-menu-is-if-shell-over-two-display-menus…)).
   F6 does what F12 did, its label staying "back to TUI". F10 is unchanged.
3. **F12 and F11 are dropped, with no alias.** One key per action. F11 is the
   fullscreen key in Windows Terminal and GNOME Terminal, so it often never
   reached tmux, and a reply already cancels an Auto-exit (ADR 0111).
4. **Cancel auto-exit moves into the F1 menu**, after a separator, shown only
   while the session is armed. It disarms only the pressing session.
5. **Bar text.** Open: `F1 help · F6 back to TUI · /exit to finish` (F10 only
   in the menu). Closed: `✓ <id> closed — /exit to finish · F6 back to TUI`,
   still flipped loud (green, reversed, ADR 0066 §4), F10 only in the menu.
   Armed: `auto-exit in M:SS · F1 to cancel`.

## Consequences

- No listed terminal grabs F1 or F6 by default (`docs/research/terminal-default-f1-f6-f10-grabs.md`),
  and Claude Code binds no F-keys.
- F10's exposure is unchanged and accepted: GNOME Terminal 3.x takes it for its
  menubar while one shows, Apple keyboards need Fn for the whole top row, and
  Windows Terminal passing it through is untested.
