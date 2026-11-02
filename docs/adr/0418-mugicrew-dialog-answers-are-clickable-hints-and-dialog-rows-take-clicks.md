# ADR 0418: Dialog answers are clickable hints, and dialog rows take clicks

Status: Accepted
Date: 2026-10-30

The Chart "Every TUI dialog can be answered with the mouse alone" (mc-j1f9c) asks that
every answer the keyboard can give a TUI dialog can also be given with the mouse alone,
while capture is on. This covers the confirm dialog, the X withdraw dialog and its
cancel review, and form and input submits. Pickers and the Loop menu already take
clicks. This extends ADR 0372 (mouse-mode-hovers-in-a-navy-shade…), ADR 0373
(the-mouse-selects-what-j-k-can-land-on…) and ADR 0412 (mouse-steps-with-inline-arrows…).

1. **The answering hints in a dialog's hint line are its click targets.** The Loop
   menu's options line already works this way. A click on a hint acts as its key does.
   This covers a confirm's `y`, `n` and alt action, the X dialog's `enter` withdraw,
   `D` discard work and `g` go to blocker, the cancel dialog's review, cancel, loss and
   back hints, `ctrl+s` save in the Signal and Initiative forms, `enter` submit in
   input dialogs, and the New Surgery form's file key. Each hint's hit area is its drawn
   label, and it takes ADR 0372's navy hover on its own cells, as ADR 0412 §9 does. The
   hint line already lists only the keys the dialog's current state accepts, so only
   those hints act. When a narrow box drops the descriptions, the key text that is left
   is the target. Navigation hints (`j/k`, `tab`) stay inert, because the mouse clicks
   the thing itself (§2, §3). Rejected: button labels drawn apart from the hints, and
   having both.
2. **Rows in the X and cancel dialogs are hovered and clicked.** Motion over a tree or
   chain row hovers it. A left click selects it, as j/k does. The lead, summary, counter,
   warnings and blank lines stay inert (ADR 0373). In the cancel dialog, each chain's
   `close` and `release` radio labels are their own targets, with the same hover. A
   click on one does what c or r does for that chain. A locked chain's `release` flashes
   the same "stays closed" message that r gives.
3. **The cancel dialog's why line focuses on click.** A click on the why line focuses
   it, as tab does. A click on a chain row returns focus to the list.
4. **A click outside the X or cancel dialog does what esc does in that state.** In the
   cancel review, esc steps back to the rows, so an outside click does too. This follows
   the confirm dialog's outside click, which already sends its no-key. Forms, input
   dialogs and the Surgery form stay inert outside, so a stray click cannot throw away
   typed text. A click inside a box but off a target is inert.
5. **Destructive answers act at once by mouse, as their keys do.** A click on
   `D discard work` (or on the cancel review's loss hint) acts directly. The hint is
   shown only in the state where its key is accepted, so the warning is already on
   screen. Rejected: a second, mouse-only confirm step.
6. **Neither the hints bar nor the `?` reference changes.** The targets are the hints
   themselves (as in ADR 0412 §10).
