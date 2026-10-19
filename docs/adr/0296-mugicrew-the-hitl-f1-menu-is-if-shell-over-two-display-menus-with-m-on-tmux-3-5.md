# ADR 0296: the HITL F1 menu is an if-shell over two display-menus, with -M, on tmux ≥ 3.5

Status: Accepted; point 5 (tmux floor 3.5) superseded by ADR 0303 (the-hitl-f1-menu-passes-m-only…): floor 3.4, -M only on a known tmux ≥ 3.5
Date: 2026-10-18

Chart: HITL session F-keys follow the byobu convention. The prototype "F1
display-menu in the mchitl table against real tmux" settled this on tmux 3.7c
(side ref `refs/mugicrew/prototypes/mc-tq6w`).

## Decision

1. **One mchitl F1 binding in preamble(), an if-shell over two menus:**
   `if-shell -F '#{@autofinish}' '<armed menu>' '<unarmed menu>'`. The armed
   menu is `Back to TUI (F6)`, `Bead pane (F10)`, a separator, then
   `Cancel auto-exit (c)`. The unarmed menu has the first two entries only.
   A single menu can't do this: an entry whose name expands to "" is dropped,
   but the separator is a literal "" decided before expansion, so it would
   stay as a trailing line.
2. **Entry names omit the key.** tmux appends `(F6)` itself.
3. **Embedded commands go through `literal()`.** display-menu format-expands
   an item's command when the menu opens. Doubling `#` makes today's F12 and
   F10 command text run exactly as the direct bindings do. Without it, the
   bead id would be spliced into beadSplit's shell text.
4. **`display-menu -M -x R -y S`.** A menu opened by a key ignores the mouse
   unless it has `-M`: a click closes it without picking. With `-M`, a click
   picks an entry whether `mouse` is on or off. `-x R -y S` anchors the menu
   bottom-right, just above the bar.
5. **The tmux floor is 3.5**, the release that added `-M`. Everything else
   used here exists in 3.0. The devShell ships 3.7c.

## Considered options

- **Cancel first, then the separator, in one display-menu:** it works
  natively, because tmux skips a separator with nothing above it. Rejected to
  keep the charted layout, with Cancel last.
- **No `-M`, keys only (floor 3.0):** rejected. A click that silently closes
  the menu reads as broken.
