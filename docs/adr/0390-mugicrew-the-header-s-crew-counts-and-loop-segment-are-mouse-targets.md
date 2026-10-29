# ADR 0390: The header's crew counts and loop segment are mouse targets

Status: Accepted
Date: 2026-10-29

This overrides one clause of ADR 0373 (the-mouse-selects-what-j-k-can-land-on…) §8: "The
header line, the status line and the hints bar stay inert." The header's HITL, AFK,
waiting and loop segments are the crew's state at a glance, and the natural follow-up is
the Sessions modal or the Loop menu. The Spec "Mouse mode polish: clickable header
segments, mouse editing in forms and filter lines, and a responsive detail-drawer wheel"
makes them targets. The status line and the hints bar stay inert, as ADR 0373 §8 says.

1. **Which segments are targets.** With Mouse mode on, a left click on the HITL count,
   the AFK count or the N waiting segment (when shown) opens the Sessions modal, as `S`
   does. A left click on the loop segment (any form: on, off, limited, starting…, stale)
   opens the Loop menu, as `o` does. N‖, the backup mark, the usage warning, the `·`
   separators, the brand and the padding stay inert.
2. **Hover.** A target takes the navy hover (colour 17, ADR 0372 §1), like ← esc back. It
   shades the segment's own text only. Motion that stays inside one segment is dropped by
   MotionFilter.
3. **Double- and right-click.** A double-click pairs on the segment target, like the tab
   strip, and counts as one click. A right-click on the header does nothing.
4. **Focus view and narrow widths.** In the focus view the counts belong to the back bar,
   and at narrow widths they drop out of the header; in both only the loop segment is a
   target. A segment cut by truncation is not a target.
5. **Live only.** Header clicks do nothing while a modal, a menu or the ? reference is
   open, as for every mouse path (`mouseLive`).
6. **Geometric hit-test.** `headerRightSpans` returns, from the same render View draws,
   the column span and kind of each clickable segment; `headerSpans` offsets them by the
   brand and padding. A refresh that changes the counts' widths moves the spans with it.
   No walk is involved.
