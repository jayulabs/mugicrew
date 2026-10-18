# ADR 0234: the New Surgery form is a title and a verbless target list grouped by Initiative

Status: Accepted
Date: 2026-10-16

Chart mc-36cr, prototype mc-36cr.5. The New menu's Surgery row is the TUI's only
Surgery path. It is one form that offers three kinds of target: an existing
Checkpoint to join, a new Checkpoint under an open Initiative, and a new
standalone Checkpoint. The human picked this shape from four rendered variants
over a live snapshot plus a fixture with every refusal. Eligibility comes from
docs/research/surgery-target-picker-join-eligibility-and-title-only-body.md.

## Decision

1. **Two fields, `title` and `target`.** `tab` moves focus between them.
   `↑`/`↓` move the target pick from either field. `j`/`k` also move it while
   `target` has focus. `enter` files and `esc` cancels. There is no filter.
2. **Rows carry no verbs; the row's kind is the action.** Every bead row is in
   SBRN, with kind tags padded to one width. A Checkpoint row means "join it".
   An Initiative row means "a new Checkpoint in it". The one non-bead row reads
   `+ New standalone Checkpoint`, in its own colour, not italic.
3. **Order, top to bottom, with a blank row between blocks:**
   - `+ New standalone Checkpoint`. It is hidden while Require Initiatives is on.
   - The standalone Checkpoints, unindented so they never read as an
     Initiative's members. A Checkpoint counts as standalone when its Parent is
     not an open Initiative, so this includes the members of a closed
     Initiative.
   - Each open Initiative's row, with its open Checkpoints indented under it.
   - Initiatives and Checkpoints sort in the snapshot's bead order. Within each
     block, eligible Checkpoints come first and refused ones after.
4. **Refused targets.** A closed Checkpoint is never listed. Any other
   JoinVerdict refusal is shown dimmed, with a `⊘ <short reason>` suffix:
   landing, pivot gate, branch rewriting, or surgery `<id>` open. The cursor
   can land on a refused row. `enter` there shows the reason, except on an open
   Surgery, where `enter` resumes it.
5. **The picked target** carries a `✓` and the picker's row shade.
6. **One fixed message slot.** The box always keeps a blank line and one message
   line under the list, so its size never changes while scrolling. In order of
   precedence, the line shows a form error, then on an Initiative row the
   white-italic hint `this will create a new Checkpoint in <id>`, then on a
   refused row its full refusal text. Otherwise it is empty. A Checkpoint row
   has no hint.
7. **Where the cursor starts.**
   - Opened over a Checkpoint or one of its members: on that Checkpoint, even
     if it is refused, so its reason shows.
   - Opened over an Initiative, or in its focus view: on that Initiative's row.
   - Opened over nothing: on `+ New standalone Checkpoint`, or on the first
     selectable row while Require Initiatives hides it.
8. **Scrolling.** A long list scrolls in a window that keeps the pick centred,
   with a `k/N` counter, as the existing pickers do.

## Considered Options

- **A flat list with `JOIN` / `NEW IN` / `NEW` tags.** Rejected. It loses which
  Checkpoints belong together, and the tags repeat what the row's kind already
  says.
- **Groups with a `+ new Checkpoint in X` row under each Initiative.** Rejected.
  The verb row doubles the Initiative's own row.
- **Two steps (pick the kind with `j`/`i`/`s`, then the target), with refused
  joins hidden behind a count.** Rejected. It costs an extra keystroke, and it
  hides the reason a Checkpoint the human expected cannot be joined.
- **Standalone Checkpoints in a "No Initiative" group at the bottom.** Rejected.
  At the bottom they read as members of the last Initiative's block.
