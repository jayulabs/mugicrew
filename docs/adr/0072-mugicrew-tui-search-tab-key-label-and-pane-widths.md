# ADR 0072: mugicrew TUI Search tab — `S` jump, tab-bar slot, 40-col fallback, and wider pane geometry

Status: Accepted
Date: 2026-10-05

Amends: ADR 0058 (tab jumps, accent-letter labels), ADR 0051 §5 / ADR 0057 §4
(tab-bar fallbacks), ADR 0023 and ADR 0024 (60-col single-pane breakpoint).
Chart: Search tab. Settled in grilling.

## Context

The Search tab is the TUI's 4th tab. ADR 0058 gives every tab an uppercase jump
key, shown as the label's first letter in an accent colour. With every fallback
applied, the narrowest bar today is `Graph Inbox(2) Logs 3‖ loop draining 2`
(38 cols). A restarting loop already reaches 41 cols and is cut. ` Search` adds
7 cols, so the bar can't fit the 40-col floor without a new fallback step. The
list pane is 40% of the terminal, capped at 48 cols. Search rows carry more
fields than tree rows.

## Decision

1. **`S` jumps to Search.** It follows ADR 0058 §1: uppercase, and the label's
   first letter is the key. Pressing it on Search does nothing. `/` stays
   unbound here, and the query-entry grilling may claim it.
2. **Tab order: `Graph Inbox(N) Search Logs`**, with **S** in the accent colour.
   `←/→` follows the bar order. Search adds nothing to the bar: no result count
   and no query. Those belong in the body or the status line.
3. **New last 40-col fallback: the tabs you're not on shrink to their key
   letter.** Inbox keeps its count. Example: `G I(2) Search L 3‖ loop draining 2`
   (34 cols); the restarting case is ~37. It runs after the existing steps,
   whose order doesn't change: drop the padding, drop `N live`, tighten the
   separators, drop `@rev`. The tab you're on always shows its full name. The
   pause and loop segments still always stay (ADR 0051 §5, ADR 0057 §4).
4. **The list pane is capped at 60 cols on every tab** (it was 48). The rest of
   the rule is unchanged: 40% of the terminal, floored at the widest row's chrome
   (Spec FR2), with at least 20 cols left for the detail pane.
5. **The single-pane breakpoint moves from 60 to 84 cols** (ADR 0023, ADR 0024).
   Below 84 cols the TUI shows one pane at a time. The 40×12 floor is unchanged.
6. The Search row layout is designed to fit within the 60-col cap,
   and must also work full-width in single-pane mode down to 40 cols.

## Considered options

- *`/` as the jump key.* Rejected: the tab label can't show it. This is why ADR
  0058 rejected digits.
- *Search as the last tab.* Rejected in favour of keeping the bead views
  (Graph, Inbox, Search) together, before Logs.
- *Shrink every tab, including the one you're on (`G I(2) S L`).* Rejected: the
  tab you're on would be shown only by its colour.
- *Drop the `N‖` paused count, or let the edge truncate.* Rejected: ADR 0051
  and ADR 0057 say the pause and loop segments always stay.
- *A 60-col cap for Search only.* Rejected: one pane rule for every tab. This
  means Graph and Inbox also get wider on large terminals.

## Consequences

- Graph and Inbox look different on wide terminals: the list grows up to 60 cols.
  Terminals 60–83 cols wide, which used to get two panes, now get one.
- Graph's tree *behaviour* is unchanged. Only the pane geometry is shared.
