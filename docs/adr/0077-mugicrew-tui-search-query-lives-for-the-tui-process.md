# ADR 0077: mugicrew TUI Search query is kept across tab switches, not across restarts

Status: Accepted
Date: 2026-10-06

Builds on: ADR 0073 (query entry), ADR 0074 (`G` reveal), ADR 0075 (default
status), ADR 0076 (selection follows the bead). Chart: Search tab.
Settled in grilling.

## Context

A Search query (text, status/kind/sort facets, selection) is built up over
several keystrokes. ADR 0074 lets `G` reveal a result in the Graph, after which
the user may want `S` to return to the same results. The TUI persists no view
state between runs today: Graph's `h` toggle and collapsed groups live in memory
and reset on restart.

## Decision

1. **Tab switches keep the whole query.** Leaving Search by any route (`G/I/L`,
   `←/→`, a `G` reveal) and coming back with `S` restores the text, all three
   facets and the selection. The selection follows its bead by id (ADR 0076), so
   it survives reloads that happened while Search was hidden. Search can only be
   left from normal mode; `/` input and pickers swallow the tab keys (ADR 0073).
2. **A restart resets the query.** It lives in memory for the TUI process only.
   A new process opens Search on the defaults: empty text, `unclosed · any ·
   updated` (ADR 0075). Detaching from and reattaching to the TUI's tmux session
   (ADR 0065) is not a restart.
3. **No reset key.** `esc` in normal mode clears only the text (ADR 0073); facets
   return to their defaults through their pickers.

## Considered options

- *Reset on every return to Search, or keep facets but clear text.* Rejected:
  breaks reveal-and-return, and is inconsistent with Graph keeping its view state.
- *Persist the query to a state file.* Rejected: nothing else in the TUI is
  persisted, and a stale facet (e.g. a vanished kind showing `0 of N`, ADR 0076
  §3) greeting you at startup is a trap.
- *`esc` on empty text resets the facets, or a dedicated reset key.* Rejected:
  facets are deliberate and always named in the header; a stray `esc` shouldn't
  wipe them, and resetting is rare.

## Consequences

- Search holds its query state on the TUI model alongside Graph's view state.
- A forgotten facet filter persists across tab switches; the header line
  (ADR 0073 §6, ADR 0075) is what makes it visible.
