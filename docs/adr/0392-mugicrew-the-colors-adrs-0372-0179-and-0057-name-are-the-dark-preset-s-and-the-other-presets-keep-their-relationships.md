# ADR 0392: The colors ADR 0372, 0179 and 0057 name are the dark preset's, and the other presets keep the relationships those ADRs protect

Status: Accepted
Date: 2026-10-29

Chart Named color themes for the mugicrew TUI (mc-0f831) adds light and high-contrast
presets beside dark (ADR 0386 (mugicrew-the-light-and-high-contrast-presets-fill-every-…)).
Three older ADRs name xterm colors: ADR 0372 (mugicrew-mouse-mode-hovers-in-a-navy-shade…)
names hover's navy, ADR 0179 (mugicrew-tui-v2-queue-tabs-share-actionable-two-tone-badges-…)
names the ancestor shade's gray and amber for waits-on-you, and ADR 0057
(mugicrew-tui-paused-beads) names the pause colors, with magenta for manual and a dimmer
magenta for inherited. Those ADRs are left as written.

1. **The names are the dark preset's values.** Every color those three ADRs name is what
   the dark palette holds, and dark is today's colors. Where one of them says "navy" or
   "amber", read it as the role's dark value.
2. **The other presets keep the relationships, not the colors.** Light and high-contrast
   fill each role with the values of ADR 0386, and hold what the old ADRs protect:
   - hover, the ancestor shade and the selection bar are three distinct backgrounds, and
     the ancestor shade differs from every other in-row background (ADR 0372, ADR 0179);
   - hover keeps the row's own colors, so light carries a shade ink only for text that
     has none (ADR 0372 §1, ADR 0386 §3);
   - amber means only "someone waits on you", with the usage caution on its own role
     (ADR 0179 §2, ADR 0386 §4);
   - every pause cause has its own color, inherited included, manual is a magenta and
     inherited a dimmer magenta (ADR 0057);
   - P1 is never the blocker pink.
3. **Tests pin the relationships per preset**, not the indices, so a later retune of a
   preset's values passes while a collapsed relationship fails.

## Consequences

- A reader of ADR 0372, 0179 or 0057 who sees a color name finds the dark value there and
  this ADR for the other presets, with no edit to the old text.
