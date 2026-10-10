# ADR 0073: mugicrew TUI Search query entry: free text, three facet pickers, two header lines

Status: Accepted
Date: 2026-10-06

Amends: ADR 0058 §4 (text modes, the `?` overlay) and ADR 0026 (footer hints)
for the Search tab. Chart: Search tab. Settled in grilling.
Builds on ADR 0072 (which left `/` unbound for this decision).

## Context

The Search tab filters a flat bead list by free text, status and kind, and sorts
it by updated, priority or created. It needs a way to enter that query that
doesn't break the normal-mode keymap, and that stays readable at the 40-col floor.
The snapshot has about 15 kinds, so cycling through them with one key, with no
reverse, is painful.

## Decision

1. **The query is free text plus three facets: status, kind and sort.** There is
   no token syntax (`status:open …`).
2. **Text input.** `S` lands in normal mode, like every tab jump. `/` enters
   text input. In text input every printable key is text, including `?` and
   `G/I/L/S` (ADR 0058 §4), and `j/k`/arrows don't move the selection.
   `enter` leaves and keeps the query. `esc` leaves and reverts the text to what
   it was before `/`. In normal mode on Search, `esc` clears the text query.
3. **Filtering is live** on every keystroke. The text is split on whitespace,
   and a bead matches when every word appears in its title or id
   (case-insensitively, in any order).
4. **Each facet is a picker:** `f` status, `c` kind, `s` sort. A picker replaces
   the body below the header lines, like `?` does. `j/k` + `enter` chooses,
   `esc` cancels, the current value is marked, and every other key is swallowed.
   - Status: `any, open, in_progress, blocked, deferred, closed`.
   - Kind: `any`, then every `kind:*` value present in the snapshot, in
     alphabetical order. There is no hard-coded kind list.
   - Sort: `updated` (newest first), `priority` (P0 first), `created` (newest
     first). Directions are fixed, with no reverse.
5. **Beads with no `kind:` label are left out of Search entirely**, under every
   facet, the way event beads already are.
6. **Two header lines** at the top of the Search body, never in the tab bar
   (ADR 0072 §2):
   - `/ tab▏` while there is text, or a dim `/ search` hint when it is empty;
   - `open · grilling · ↓pri   12 of 540`. An unfiltered facet reads `any`.
7. **Footer** (ADR 0026):
   - normal mode: `/ search · f status · c kind · s sort · enter · <reveal> · ? · q`,
     which drops `s`, then `c`, then `f` (`/` is kept the longest);
   - text input: `enter keep · esc cancel`;
   - a picker: `enter pick · esc cancel`.

## Considered options

- *One token line (`status:open kind:grilling sort:pri tab`).* Rejected: it needs
  a parser and syntax to learn, and it overflows 40 cols quickly.
- *Free text plus cycle keys.* Rejected: about 15 kinds with no reverse key (shift
  is the tab jump) is painful. Pickers were then used for all three facets,
  for consistency.
- *Landing in text input on `S`.* Rejected: it breaks the `S` → `j/k` habit.
- *A `(none)` kind entry for beads with no kind.* Rejected: those beads are left
  out of Search entirely.
- *Facets in the status line.* Rejected: the status line stays for transient
  messages.

## Consequences

- Search spends two body rows on the header. At the 40×12 floor, that leaves
  fewer result rows.
- A bead with no kind can only be found by browsing the Graph.
- Which status is the default, and whether closed beads show by default, is still
  open on the Chart. This ADR fixes only the picker's entries.
