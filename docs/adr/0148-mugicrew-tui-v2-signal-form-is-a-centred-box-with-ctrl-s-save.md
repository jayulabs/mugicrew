# ADR 0148: v2 Signal create/edit form is a centred box that saves on ctrl+s

Status: Accepted
Amended by ADR 0258 (§2: labels and deps go to the bd CLI, since E raw is removed).
Date: 2026-10-13

The human tried prototype mc-lu2x.5.4 (side ref
`refs/mugicrew/prototypes/mc-lu2x.5`): the Idea tab's list, its '+ New Signal'
row and three layouts of the one create/edit form that Idea's and Graph's `n`
and `e` share. This ADR records what they picked. It refines the dialog rule
in ADR 0142 §7 for the one form that holds a multi-line field.

## Decision

1. **Layout.** The form is a centred box over the tab, the look of the
   existing v2 dialogs. It is not a side pane beside the list or a full-page
   form.
2. **Fields.** Title, priority and description, nothing else. Labels and deps
   stay with `E` raw edit, and Initiative membership stays with `a`/`A`.
3. **Priority** is not typed. When the field has focus, `+`/`-` step it
   through P0–P4, the keys Graph and Settings already use for a ±1 step. It
   can't hold an invalid value.
4. **Keys.** `ctrl+s` saves from any field. `tab`/`shift+tab` move between
   fields. `enter` moves to the next field from title or priority, and is a
   newline in the description. `esc` cancels. So `enter` does not submit this
   form, unlike every other v2 dialog. That deviation is the reason for this
   ADR.
5. **Description editor.** A bubbles `textarea` with a static cursor (the
   shell does not route blink messages). It brings one new module,
   `github.com/atotto/clipboard`. The prototype measured the CGO_ENABLED=0
   binary at +425,750 B (+2.5%) over 17.3 MB, prototype code included.

## Considered Options

- **Side pane (list stays visible) or full page.** Rejected by the human after
  trying all three.
- **`enter` submits, and the description needs another key for a newline.**
  Rejected. A description is markdown, and newlines are its common case.
