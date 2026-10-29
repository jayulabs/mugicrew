# ADR 0374: A wheel-scrolled list holds its top row until a key snaps it back to the selection

Status: Accepted
Date: 2026-10-28

ADR 0372 (mugicrew-mouse-mode-hovers-in-a-navy-shade-and-the-wheel-scrolls…) has the
wheel scroll a list's viewport without moving the selection, so the selection can sit
off-screen and a list needs a stored offset. Today every list's offset is derived from
the selection by `followOffset`, which centres the selected row (clamped at the ends)
and keeps no state. Chart Optional mouse mode for the mugicrew TUI (mc-608nn) asked how
that scrolled viewport, hover and the selection meet keys, refreshes and resizes. The
human picked:

1. **A list's viewport is Following or Scrolled.** Following is today's behaviour.
   The wheel puts the list in Scrolled, which holds an **anchor**: the identity of the
   top visible row (bead id, History's sha, a header's key), not a bare offset.
2. **Any key snaps a Scrolled list back to Following, then acts on the selection.**
   j/k step from the selection, and enter, space and the row verbs act on it. Keys never
   act from the visible rows, so a verb never fires on a row the human can't see. This
   matches ADR 0372's rule that any key brings a slid tab strip back to the active tab.
3. **Snap-back scrolls as little as it can.** If the selection is already on screen,
   following resumes from the held offset and only moves to keep it visible. If it's
   off-screen, the list re-centres on it as `followOffset` does today.
4. **A click keeps Scrolled.** A click, double-click, right-click or fold-toggle click
   selects the row under the pointer, and that row stays under the pointer. Re-centring
   would move it away, and the second click of a double-click would then miss.
5. **A live refresh or a terminal resize keeps the anchor on top.** The offset is set so
   the anchor row is still the top visible row, clamped to the new height. If the anchor
   row is gone, the nearest surviving row before it takes its place. If no row
   survives, the list goes back to Following.
6. **Hover follows the screen line, not the row.** After a refresh, the resting
   pointer's line is hit-tested again against the new line→row map, and hover shows
   whatever row is now there, so it always shows what a click would hit. If that line
   holds no selectable row, hover is dropped.
7. **Any key clears hover until the pointer next moves.** That way only the selector
   shows where the keyboard is. The motion filter must count the first motion after a
   clear as a change of hover target.
8. **A tab switch doesn't keep a scrolled offset.** Leaving or coming back to a tab
   returns its lists to Following. The selection is still kept per tab, as today.

## Considered Options

- **Keys act from the visible rows** (j/k pick up at the top or bottom visible row).
  Rejected: the selection should stay the one thing keys act on, and the wheel is only
  for looking.
- **Hold the raw offset across a refresh.** Rejected: rows added or removed above the
  view would shift what is under the pointer.
- **Hover sticks to its row across a refresh.** Rejected: hover would stop showing
  what a click at the pointer would hit.
- **Keep each tab's scrolled offset across tab switches.** Rejected: a tab switch is a
  key, and this would add per-tab leave/return state for little gain.
