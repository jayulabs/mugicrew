# ADR 0372: Mouse mode hovers in a navy shade, opens a boxed verb menu at the pointer, and the wheel scrolls without moving the selection

Status: Accepted
Date: 2026-10-28

Chart Optional mouse mode for the mugicrew TUI (mc-608nn) left open how hover and the
right-click menu look. A throwaway ran the real TUI's View on a fixture, with all-motion
mouse capture and the overlays painted on top. It is kept on
`refs/mugicrew/prototypes/mc-608nn`. It rendered Defog's Actionable, Live, Merge
Requests, Charts and tree rows, closed rows and Graph, at 80 and 120 columns. The human
tried it and picked:

1. **Hover is a navy shade that keeps the row's colours.** Background 17 is painted
   across the row's list width, like `shadeRow`. Foregrounds, the kind badge and the
   priority keep their own colours, and nothing turns bold. The selector (`selRow`, blue
   25, white bold) stays on the focused row, which never shows hover. Gray backgrounds
   were rejected: 237 sits next to Graph's ancestor shade `ancRow` (236). A gutter bar
   and an underline were too faint to read as a muted selector.
2. **The menu is a rounded box with an accent (212) border.** Each line is
   `key  label`, with the key bold and padded to one width. The verbs are the row's
   "this row" set from `?`, `?`-only ones included, in hint-rank order and drawn like
   the rest. They get no rule and no dimming. Rejected: a flat box in the hints bar's
   colours, and a header naming the bead.
3. **The menu's top-left corner sits on the pointer cell.** Where it would cross the
   right edge it flips left, so its right edge is the pointer column. Where it would
   cross the bottom it flips up, so its bottom line is the pointer line. Both flips can
   apply, and the menu is clamped to the screen. It may cover the row it acts on.
   Placing it under the row, at the pointer or at the list's left edge, was rejected.
4. **The menu has one highlight.** It starts on the first item, and the pointer (on
   motion) and j/k or ↑/↓ both move it. The highlight is `selRow`'s blue, the same as
   the list selection. `enter` or a click runs the highlighted item, and pressing a
   listed verb's key runs that verb. Any other key, `esc`, or a click outside closes
   the menu and does nothing else. A separate muted pointer highlight was rejected.
5. **A right-click moves the selection to the row first.** The menu then lists that
   row's verbs and runs them through the shared bead-verb router, as the keys do.
6. **A click on a header that is not a bead toggles its fold.** On a header with no
   bead (`▾ Actionable`, `▸ 1 closed`, a section heading) a click anywhere on the line
   selects the header and toggles it, like `space`. On a bead row that is also a tree
   node, only a click on its `▾`/`▸` caret (±1 column) toggles. A click on the rest of
   the row just selects it. A fold click never counts as half of a double-click.
7. **Tabs are clickable.** A click on a tab switches to it, like the tab's jump key. A
   hovered inactive tab brightens its border (245) and label (252).
8. **The wheel scrolls the area under the pointer and never moves the selection.** This
   replaces the Survey's rule that the wheel moves the selection like j/k in lists. Over
   a list, the wheel scrolls the list's viewport. Over the tab strip, when it is clipped
   (`‹ ›`), the wheel (vertical or horizontal) and clicks on `‹`/`›` slide the strip by
   one tab, and the active tab may slide out of view. A tab click or any key brings the
   strip back to following the active tab. Drawers and the doc pane scroll as before.

Mouse handling must stay cheap. Bubble Tea v1 calls `View` after every message, and
all-motion mode sends a message for every cell the pointer crosses. In the throwaway,
re-rendering on each motion cost about 2.4ms, and rebuilding its row map after each
wheel tick (a j/k walk, about 210ms) made the TUI fall behind. So motion is filtered
before `Update` (`tea.WithFilter`) down to changes of what is hovered: the row line, a
tab, or a menu item. Hit-testing is the geometric line→row map from
`docs/research/tui-pointer-row-hit-test-geometry-per-tab.md`, never a walk of the list.

## Considered Options

- **The wheel moves the selection like j/k (the Survey's rule).** Rejected by the
  human, who wants the wheel to scroll the view and leave the selection where it is.
  The trade-off is a stored viewport offset per list. Today every list's offset is
  derived from the selection (`followOffset`), so the selection can now sit off-screen.
- **Keep tab clicks out of this Spec (the Chart's Out-of-scope line).** Rejected by the
  human: tab clicks and the sliding strip are in. Clicking the hints bar stays out.
