# ADR 0075: mugicrew TUI Search default status facet: `unclosed`

Status: Accepted
Date: 2026-10-06

Amends: ADR 0073 §4 (the status picker entries) and its Consequences (the
default status was left open). Chart: Search tab. Settled in grilling.

## Context

ADR 0073 fixed the status picker entries as `any, open, in_progress, blocked,
deferred, closed` but didn't say which one is active when Search first opens.
The bead list includes closed beads, and in the snapshot they are the vast
majority (657 of 682, about 96%). ADR 0074 dims closed titles.

## Decision

1. **The status picker gains an `unclosed` entry:** every status except
   `closed`. The entries are now
   `any, unclosed, open, in_progress, blocked, deferred, closed`.
   `any` stays first, as the unfiltered anchor.
2. **`unclosed` is the default** when Search first opens, so closed beads are
   hidden until the user picks `any` or `closed`. The picker's current-mark
   sits on `unclosed`.
3. **The header always names the active value** (ADR 0073 §6 is unchanged).
   The opening header reads `unclosed · any · ↓upd   24 of 682`. `any` still
   means unfiltered, so there is no hidden implicit default.

Whether a changed facet survives tab switches or restarts is not decided here
(ADR 0077).

## Considered options

- *`any` by default.* Rejected: about 96% of the rows would be closed, so the
  default view would be an archive of dimmed titles.
- *`open` by default.* Rejected: it hides in_progress, blocked and deferred
  beads, which is surprising for "find a bead".
- *The name `live`.* Rejected: it collides with Effort liveness and "live
  process" in CONTEXT.md. *`not closed`* (too long) and *`!closed`* (code-ish)
  were also considered.
- *`unclosed` first in the picker.* Rejected: `any` stays the first entry, as
  the unfiltered one.

## Consequences

- To find a recently closed bead, the user presses `f` and picks `any` or
  `closed`.
- The status picker has seven entries.
