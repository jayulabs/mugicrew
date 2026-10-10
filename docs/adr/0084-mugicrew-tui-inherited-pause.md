# ADR 0084: mugicrew TUI inherited pause — dim magenta `‖` below the member's own causes, named in the detail pane and `t`'s hint

Status: Accepted
Date: 2026-10-07

## Context

ADR 0081 makes a bead *inherited-paused* when an ancestor carries
`paused:manual`, and writes nothing onto the members. ADR 0057 §1, §2, §4, §5
and §6 cover only a bead's own pause causes. The TUI loads every bead with
`bd.ListAll`, so it already has the ancestors' labels. The grilling
settled how the TUI shows an inherited pause.

## Decision

1. **Row glyph.** An open inherited-paused member shows `‖` in **dim magenta**
   in the status-glyph slot. The slot ranks `⊘` > `◐` > own cause (manual,
   abandoned, failing, foreign-hold, in their own colours) > inherited > `○`.
   An own cause wins because it's the problem only that row has. Closed rows
   are unchanged.
2. **Detail-pane pause line.** After the own causes it adds
   `manual via <ancestor-id>` in dim magenta, separated by ` · ` (for example
   `failing · manual via <ancestor-id>`). This names the **nearest** paused ancestor
   only, because lifting that one is the first act that helps. It shows the id
   only, with no title, to save columns.
3. **`t` on an inherited-paused member.** `t` is hidden from the footer, as
   for an own manual pause. Pressing it anyway refuses with:
   `<id> is paused via <ancestor-id> — select it and press p, or run 'bd label
   remove <ancestor-id> paused:manual'`. The own-manual hint is checked first.
4. **`p` status line.**
   - Pausing a container: `— its open members are out of dispatch too`. If
     the container is in progress, it adds `; the running effort continues`.
   - Unpausing a container: `— its members resume`.
   - Unpausing a member that is still inherited-paused:
     `— still paused via <ancestor-id>`.
5. **The tab bar's `N‖` counts own pauses only.** Inherited members aren't
   counted, because nothing on their rows lifts the pause.

## Considered and rejected

- **Full magenta `‖`, identical to own manual.** The row wouldn't say that `p`
  on it won't unpause it.
- **A distinct glyph.** It would be one more symbol to learn, and the cause is
  still manual.
- **The inherited pause outranks own loop causes** (manual > … per ADR 0070
  §3). That would hide an orange abandoned or a red failing pause, which only
  the member itself can resolve.
- **Naming the ancestor's title, or every paused ancestor.** It costs columns,
  and the nearest ancestor is enough to act on.
- **Counting inherited members in `N‖`.** It inflates the count without adding
  any new place to act.

## Consequences

- `stateGlyph`, `pauseLine`, `runNowLabel`, `runNowHint` and `p`'s status
  text each need a nearest-paused-ancestor lookup over the loaded beads.
