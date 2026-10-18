# ADR 0173: v2 Initiative focus drilldown swaps the tab bar for a back bar that carries the Initiative's crew counts

Status: Accepted

Amended by ADR 0174 (§1 and §2: ●N counts only waiting sessions, and `N waiting` leaves the back bar).
Date: 2026-10-13

Chart mc-jmr8 (Initiative focus view). The human tried prototype mc-jmr8.3
(side ref `refs/mugicrew/prototypes/mc-jmr8`). It was built on research
mc-jmr8.2 (docs/research/initiative-focus-view-rows-and-seams.md), with `i` as
the entry key and esc as the return. This ADR records the drilldown's chrome:
what replaces the tab bar, the header row and empty sections. The grilling
mc-jmr8.4 still settles rows, keys, badge and refusals. This ADR leaves those
open, and the rows and keys the prototype built were not contested.

## Decision

1. **No tab bar while focused.** The three tab-bar rows give way to a one-row
   **back bar** and a rule beneath it. The back bar reads
   `← esc back   <Initiative title> ●N` on the left. On the right it has the
   crew counts (§2) and then `from <Tab>`, naming the tab esc returns to.
   `●N` is the focus's own count of open HITL leaves that wait on the human
   or are ready. The body gains the row the tab bar used. Tab-switch keys still
   work and drop the focus. The tab you came from appears only in the back
   bar, not as a highlighted tab.
2. **Crew counts are scoped and move to the back bar.** While focused, the
   header's `N HITL · N AFK · N waiting · N‖` segments leave the header. Only
   the loop segment and any usage warning stay there. The segments show in the
   back bar, counted over the Initiative's scope: the Initiative, every bead
   under it by `Parent`, and the Tasks and Pivots waiting on its Checkpoints
   (`waiters`).
3. **Header row without landed N/M.** The Initiative's own row tops the list.
   It shows its state glyph, kind tag, priority, title and id, and the usual
   row suffix. It does not show landed N/M: Graph keeps the progress
   (ADR 0131 §3).
4. **Empty sections keep a placeholder.** The Ideate, Defog and Execute
   section headers always show. A section with no members, open or closed,
   shows one dim line (`no Signals`, `no Charts`, `no Specs or
   Checkpoints`). A blank line separates the sections, and the cursor skips
   it.

## Considered Options

- **Breadcrumb in the active tab** (`2 Defog › Title`), plain, with the origin
  tab's ●N, or with the focus's ●N. The human tried all three and picked the
  back bar. It says how to leave, and it frees a body row.
- **Scoped counts kept in the header**, or the global counts unchanged.
  Rejected. Global counts mislead inside a one-Initiative view, and the back
  bar keeps the scoped counts next to the title they describe.
- **Omitting empty sections.** Rejected. The three-section shape stays
  stable across Initiatives.
