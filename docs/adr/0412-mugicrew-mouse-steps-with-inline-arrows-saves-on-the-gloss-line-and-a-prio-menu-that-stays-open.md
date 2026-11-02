# ADR 0412: Mouse steps with inline ◀ ▶ arrows, a staged Settings row saves from its gloss line, and the priority menu stays open

Status: Accepted
Date: 2026-10-29

The Chart "Every +/- stepped edit in the TUI works with the mouse alone" (mc-60v6c)
settled the gestures and left their look open. Settings rows and the Signal and
Initiative forms' priority field get clickable steppers. A staged Settings row gets save
and discard targets. Bead rows get raise and lower items in a right-click menu that stays
open. A throwaway ran the real TUI's View on a fixture, with all-motion capture, at 80
and 120 columns. It is kept on `refs/mugicrew/prototypes/mc-60v6c`. The human tried each
knob and picked the following. This extends ADR 0372 (mouse-mode-hovers-in-a-navy-shade…)
and ADR 0373 (the-mouse-selects-what-j-k-can-land-on…), and amends them where noted.

1. **Steppers are `◀ value ▶`, inline around the value.** They are drawn only while
   capture is on, and only on the selected Settings row or the focused priority field.
   The arrows are accent (212) and bold. On a staged row they wrap the staged value:
   `AFK pool  2 → ◀ 3 ▶ (unsaved)  (local; shared 4)`. This applies to the pool,
   Auto-exit, Auto-approve, Mouse mode, Theme and Kind-profile rows alike. A long
   Kind-profile value or an unrunnable reason only truncates or wraps the text after the
   stepper. ◀ makes the number smaller and ▶ bigger. On a cycle row ▶ does what `+` does.
   The read-only Initiatives row has no stepper. Rejected: `‹ ›`, `◂ ▸` and `− +` glyphs,
   and a stepper cluster at the row's right edge.
2. **Each arrow's hit area is its own cell plus one column on either side.** Every press
   on an arrow is one step, so a double-click is two steps. A double-click on a step
   target is never enter. This is an exception to ADR 0373 §3.
3. **At a bound the arrow is hidden.** Its cell is blank and inert. The bounds are pool at
   1, Auto-exit at off, and priority at P0 and P4. Cycle rows have no bound. Rejected: a
   dimmed arrow, and an arrow drawn as usual but inert.
4. **A staged Settings row puts `✓ save  ✗ discard` on its gloss line.** The two targets
   start the gloss line, and the gloss follows them, dimmed and truncated. A Kind-profile
   row has no gloss line, so while it is staged it gains one extra line under it that
   carries the targets. ✓ save does what enter does and ✗ discard does what esc does.
   Each target's hit area is its drawn label. Per ADR 0373 §2, the gloss line still
   belongs to its row for selection and hover. Only the two labels act. Rejected:
   targets right after the `(unsaved)` marker, at the row's right edge, as `[save]
   [discard]`, and as `enter save  esc discard`.
5. **Saving Mouse mode off by a click ends capture at once.** The steppers and targets
   disappear, and the row reads `off (local)`. The keys still work. The human found this
   reads sanely, so no extra status line is added.
6. **The form's priority field shows its arrows only while it is focused.** A click on
   an unfocused priority field only focuses it, as today, and the arrows then appear.
   After that, a click on an arrow steps. This holds in both the Signal and the
   Initiative form. ctrl+s still saves the form. Rejected: arrows drawn always, with a
   click that steps and focuses, or steps and leaves focus where it was.
7. **The right-click menu replaces `+/- prio` with `+ raise prio` and `- lower prio`.**
   These amend ADR 0372 §2's verb list, and they sit where `+/-` ranked. The labels do
   not name a P-value. At a bound the item is left out (P0 has no raise, P4 no lower),
   which matches §3. A click on either item, or its key, steps the bead and leaves the
   menu open with the highlight still on that item. This is an exception to ADR 0372 §4,
   where running an item closes the menu. The menu closes on esc, a click outside it,
   any other key, or a non-step item, as ADR 0372 §4 and ADR 0373 §7 already say.
8. **When a step re-sorts the bead, the menu stays at the pointer.** It keeps acting on
   the same bead, and the list selection follows the bead to its new row. It does not
   act on whatever row now lies under the pointer. Rejected: a menu that moves with the
   row, and a menu that closes when the row moves.
9. **The step and save targets take ADR 0372's navy hover on their own cells.** The rest
   of the selected row stays unshaded. This is an exception to ADR 0372 §1, under which
   the selected row never shows hover. In the menu, the existing single highlight serves
   as hover. In a form, the priority arrows take the shade, so motion over a form passes
   the motion filter while the pointer moves between arrows.
10. **Neither the hints bar nor the `?` reference mentions the new targets.** They are
    drawn where they act. The hints bar already names `+/- stage`, `enter save` and
    `esc discard`.
