# ADR 0076: mugicrew TUI Search refresh keeps the query and selection

Status: Accepted
Date: 2026-10-06

Builds on: ADR 0073 (query entry), ADR 0074 (result row), ADR 0075 (default
status). Chart: Search tab. Settled in grilling.

## Context

The TUI re-reads the whole bd snapshot every 3s (and after its own actions).
On each read, Graph and Inbox keep their selection by bead id and otherwise
fall back to the first row. Search adds things a reload can disturb: a text
query, facets, `/` input, pickers, and an `updated` sort whose order changes
as beads change.

## Decision

1. **The selection follows the bead, keyed by id.** If a reload moves it (for
   example, sort=updated lifts it to the top), the cursor moves with it and the
   list scrolls to keep it visible.
2. **If the selected bead drops out** (it no longer matches, or it was deleted),
   the selection goes to its **nearest neighbour**: the row that now sits at
   the bead's old index in the list, clamped to the last row. An empty list
   has no selection. When results come back, the selection lands on the top
   row. *This departs from Graph and Inbox, which fall back to the top row.*
3. **A facet value that disappears from the snapshot stays active.** For
   example, kind=prototype with no prototype beads left reads `0 of N` in the
   header, and the `c` picker keeps listing the value (marked) while it is
   active. Nothing resets on its own.
4. **A reload never interrupts input.** During `/` text input, the text and
   its `esc` revert baseline are untouched, and the results under the header
   recompute live. In an open picker, the entries are rebuilt (the kind list
   can change), and the picker cursor stays on the same *value*. If that value
   is gone, the cursor goes to its nearest neighbour, as in §2.

## Considered options

- *Follow the row (the cursor keeps its index).* Rejected: `enter` and `G`
  would then act on a bead that slid in under the cursor.
- *Fall back to the top row (the Graph and Inbox convention).* Rejected for
  Search: you work down a result list, and a bead closed elsewhere would lose
  your place.
- *Reset a vanished facet to `any`, silently or with a status-line note.*
  Rejected: the header already names the active value (ADR 0075 §3), and a
  filter that changes behind your back is worse than an empty list.
- *Freeze the result list while typing or in a picker.* Rejected: a live
  recompute under an untouched input costs nothing and never shows stale rows.

## Consequences

- Search keeps its previous result list (or the selection's index) between
  reloads, so it can resolve the nearest neighbour.
- A vanished facet value can leave Search showing `0 of N` until the user
  changes the facet.
