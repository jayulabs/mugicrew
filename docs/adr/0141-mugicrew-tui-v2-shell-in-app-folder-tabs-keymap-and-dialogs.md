# ADR 0141: The v2 TUI shell has in-app folder tabs (no tmux navbar), enter/space keys, and dialogs only for destructive actions

Status: Accepted (§6 amended by ADR 0142, §5 by ADR 0144); §4 "truncates with …" and §6 "the hints bar shows only those two" superseded by ADR 0229 (…hints-bar-and-key-reference…)

Amended by ADR 0171 (§5 and §8: # $ * keys for the rare tabs, detail is a bottom drawer, ●N counts only waiting sessions).
Date: 2026-10-12

Chart mc-lu2x.1 (V2 TUI shell beside v1). Settles prototype mc-lu2x.1.3 (side
ref `refs/mugicrew/prototypes/mc-lu2x.1`, run with `mugicrew --v2`). The human
tried both navbars live and three tab-bar styles. Builds on the research in
`docs/research/tmux-status-bar-as-tui-navbar.md`.

## Decision

1. **The navbar is in-app, never the tmux status bar.** The TUI session keeps
   `status off` (ADR 0066 §1 and ADR 0065 §1 stand unamended). The tabs render
   as **folder tabs**: each tab is a rounded box, and the active one is
   accented and open at the bottom into the content below (3 rows).
2. **The tab bar overflows by sliding.** When the tabs don't fit, the bar
   shows a window of whole tabs that always contains the active one. A `‹` or
   `›` marks tabs clipped off that side.
3. **Rows from top to bottom:**
   - a **header row**: `mugicrew` left, with the live / waiting / loop status
     right-aligned
   - the tab bar
   - the tab's content
   - a **status line** saying what the last action did (always visible)
   - a **hints bar** on its own background colour
4. **The hints bar is context-aware.** It is one line of key hints that
   changes with the tab (each tab's own verbs) and the mode (list, detail
   open, dialog open). It truncates with `…`.
5. **Keymap:**

   | key | action |
   |---|---|
   | `enter` | attach to a live session, or launch one, at once, with no confirmation |
   | `space` | open or close a detail pane beside the list (`esc` also closes it) |
   | `tab` `⇧tab` `←` `→` `1`–`8` | switch tabs |

   Tab order is the Chart's: Ideate / Defog / Execute / Graph / History / Logs /
   Stats / Settings.
6. **Confirmations are modal dialogs, and only destructive actions get one.**
   For example, closing a bead kills its session. The dialog is a centred,
   bordered box. `y`/`enter` confirms and `n`/`esc` cancels. While it is open,
   no other key reaches the tab, and the hints bar shows only those two.
7. **List selection is a full-row highlight.** The selected row has a
   background colour across the list's full width, and one column takes up the
   slack so the highlight spans the list.
8. **Tabs may carry a human-actionable count badge.** Defog and Execute show
   one, e.g. `Defog ●3`. Which items count belongs to those tabs' own phases
   (2–4); the shell only renders the badge.
9. **v2 uses bubbles v0.21.0** (`table`, `viewport`, `help`, `key`) with
   lipgloss. With `CGO_ENABLED=0` the prototype binary was 16,290,934 B against
   a 16,014,194 B baseline (+276,740 B, +1.7%). ADR 0023's "no bubbles" stays
   v1's rule and does not bind v2.

## Considered Options

- **The tmux status bar as navbar.** It works: clickable `#[range=user|N]`
  tabs forward `M-N` to the pane, and an `@tab` option drives the highlight.
  It was rejected because it brings no gain:
  - Each session shows its own bar, so the tabs vanish when you hop into a
    HITL session, and F12 already covers getting back.
  - It costs a row, as the in-app bar does.
  - Bubble Tea can take clicks itself.
  - It costs a tmux ≥3.4 floor, a third key table, 25–50 ms per switch, and
    amendments to ADR 0065 and ADR 0066.
  - It can draw only one row, so no folder tabs.
- **Underline tabs and pill tabs.** Both were tried. The human picked folder
  tabs.
- **Confirming launch and attach.** It was dropped because neither is
  destructive. Dialogs are kept for actions that lose something.
