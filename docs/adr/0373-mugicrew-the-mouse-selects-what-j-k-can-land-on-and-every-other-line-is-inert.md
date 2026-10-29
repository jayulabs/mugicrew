# ADR 0373: The mouse selects what j/k can land on, and every other line is inert

Status: Accepted
Date: 2026-10-28

Chart Optional mouse mode for the mugicrew TUI (mc-608nn) left open what a click,
double-click and right-click do on lines that are not selectable rows. The human
answered with the research's list of non-row lines in hand
(`docs/research/tui-pointer-row-hit-test-geometry-per-tab.md` §4). This builds on
ADR 0372 (mouse-mode-hovers-in-a-navy-shade…), which already settled header fold
clicks (§6) and the clipped tab strip (§8). Those stand unchanged.

1. **The mouse selects exactly what j/k can land on, surface by surface.** A line
   that j/k skips can't be hovered or selected. A line j/k lands on can be. So the
   focus view's placeholder rows (`fPlace`) can be selected, while the queue tabs'
   placeholders can't. Nothing snaps to the nearest row.
2. **A continuation line belongs to its row.** An Initiatives row's counts line, a
   Settings setting's gloss line and a wrapped Kind-profile reason all hover and
   select the row above them, as if the pointer were on its first line.
3. **A stop that is not a bead row** (section and group headers, `▸ N closed`,
   Graph's unparented header, `+ New Signal`, drained cue rows, the Initiatives
   `Open`/`Closed` headers, the focus view's in-body headers):
   - A click follows ADR 0372 §6: it selects, and on a header with no bead it
     also toggles the fold.
   - A double-click does what enter does, which is nothing where enter is inert.
     It is not a second way to fold.
   - A right-click does nothing, and it does not move the selection.
4. **Every other line is inert.** This covers gap blanks, placeholders that are not
   stops, padding below the list, the drawer's rule, error and empty lines, filter
   and scope lines (Graph, Logs, History's `main · newest N`), and Settings' "Kind
   profiles" title, blurb, rule and `↑/↓ N more` markers. Every gesture does
   nothing there, and the hover shade clears as the pointer crosses them.
5. **A blocker footer `!` row is a link.** A click jumps the selection to that
   blocker and scrolls it into view, like the dialogs' "go to blocker". Hover
   shades it. Double-click and right-click do nothing. The `… +N more blockers`
   row is inert.
6. **An open drawer's body takes only the wheel.** A click, double-click or
   right-click in it does nothing. The menu opens only on a bead row, never for
   the bead the drawer shows.
7. **A click outside an open menu only closes it.** The click is consumed, so
   dismissing the menu never selects or folds anything.
8. **Chrome:**
   - Tab clicks and the strip follow ADR 0372 §7–8. A double-click on a tab is
     the same as a click, and a right-click does nothing.
   - In the focus view's back bar, only the `← esc back` text is a target: a
     click on it works like esc, and it alone takes the hover cue. The title, the
     `●N` badge, the counts, `from <tab>` and the rule are inert.
   - The header line, the status line and the hints bar stay inert.

## Considered Options

- **A single click on a header selects only, and a double-click folds it.** The
  human first agreed to this, then chose ADR 0372 §6's prototyped click-to-fold
  rule.
- **`‹`/`›` switch to the neighbouring tab (shift-tab/tab), and the wheel does
  nothing on the strip.** This needs no new state, but the human kept ADR 0372
  §8's sliding strip.
- **Clicking the hints bar runs its verb.** Left out, because the right-click menu
  already reaches the row's verbs. Clickable hints would also need per-segment
  geometry for a bar that drops hints by rank as it narrows (ADR 0229 (tui-v2-hints-bar-and-key-reference…)).
- **A uniform rule where placeholders are never selectable.** Rejected in favour of
  mirroring j/k on each surface, so the mouse and the keys never disagree about
  what a stop is.
