# ADR 0024: Single-pane mugicrew TUI chrome — adaptive help line, list-position in the detail header

Status: Accepted — §1 superseded by ADR 0026 (footer command surface); §3 amended by ADR 0051 (Inbox(N), loop indicator); 60-col breakpoint raised to 84 by ADR 0072
Date: 2026-09-25

Chart: Responsive mugicrew TUI layout. Settles the chrome question
(graduated from the prototype spike once ADR 0023 fixed the layout mechanism:
patch, single-pane reuses `focusDetail`/Tab). Fixes what chrome each pane shows
in sub-60-col single-pane mode so the Build knows exactly what to render.

## Context

Below the 60-col breakpoint the TUI collapses to one pane at a time,
Tab flipping `focusDetail` (ADR 0023). The two-pane chrome does not survive the
narrowing:

- The help legend (`view.go:158-161`) is ~90 chars — 3 wrapped lines at 40 cols,
  eating a quarter of the 12-row floor.
- With the master list hidden, the human can see *what* bead they're on (the
  detail header already carries kind/mode/title/id/state/owner/chart/age) but not
  *where* it sits in the hidden list.
- The tab bar's fitness at 40 cols was unverified.

## Decision

**In single-pane mode only** (two-pane chrome at ≥60 cols is unchanged):

1. **Adaptive, per-focus, single-line help legend**, truncated to width. The
   detail pane's `actionBar` (`view.go:441`) already renders the contextual
   *action* keys (`l launch · t run · e edit`), so the help line carries only
   **navigation** — no duplication, no help-overlay machinery. The master pane's
   own affordances are NOT in `actionBar` (it is a detail-pane element), so the
   master-focus legend must carry them:

   - master-focus, Graph: `j/k move · space fold · h hide-closed · tab→detail · q`
   - master-focus, Inbox: `j/k move · n new · tab→detail · q`
   - detail-focus (either tab): `j/k scroll · tab→list · q`

2. **List-position indicator in the detail header**: append `[i/n]` (1-based
   index within the current list / list length) to the detail title line
   (`detailLayout`, `view.go:352`). Rendered only in single-pane mode — redundant
   when the master list is visible alongside. The existing identity header
   answers "what am I looking at"; `[i/n]` answers "where am I".

3. **Tab bar kept verbatim.** `Graph`/`Inbox` (padded) + `N open HITL` renders
   ~27 cells — fits the 40-col floor with margin, even at a 2-digit count. No
   change.

## Rationale

Navigation-only help + reuse of the already-contextual `actionBar` keeps the help
line to one line without a help-overlay mode (which ADR 0023's ~25-LOC budget and
ponytail both discourage). Dropping the master affordances would make `n`/`space`
/`h` undiscoverable on a phone, so they ride the master-focus line where
`actionBar` cannot reach them. `[i/n]` is the minimum that restores positional
context the hidden list used to give, for ~2 lines, without reintroducing the
list.

## Consequences

- The Spec's responsive-layout section states the single-pane chrome as above.
- Implementation is **Build** work (kind:build) under the Checkpoint the Spec
  cuts — not charted here. This ADR fixes the chrome contract; the exact key
  strings above are the target.
- No new Model state beyond what single-pane already needs: the selection index
  and list length for `[i/n]` already exist at the call site.
