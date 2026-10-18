# ADR 0297: The Stats tab flows Plan usage, Sessions, Distribution left to right and wraps by width

Status: Accepted
Date: 2026-10-18
Amends: ADR 0259 (mugicrew-the-stats-tab-burn-chart-is-a-block-area-chart…) and ADR 0266 (mugicrew-the-stats-tab-session-distribution-is-a-teal-bar-histogram…), their placement rules only

Chart: Stats tab layout that gives the Sessions table room. The prototype
"Stats tab layout variants rendered at the human's pane size" picked this
layout over today's, table-on-top, flexible-table and three-column variants
(side ref `refs/mugicrew/prototypes/mc-8m00`).

## Context

Today Sessions is pinned at 72 columns on the left with Plan usage and
Distribution stacked to its right, from 110 columns. On the human's 238×54 pane
the table's KEY truncates and its headers shorten, while the Distribution's
bars stretch about 150 columns and over half the rows sit empty.

## Decision

1. **Order is fixed: Plan usage (band and burn chart), then Sessions, then
   Distribution**, left to right, and top to bottom once they wrap.
2. **The blocks flow and wrap.** A block joins the current row while its
   minimum width fits beside the blocks already there, else it starts a new row
   below. Rows are separated by a blank line, and the layout follows the pane's
   width live.
3. **Each block has a min and a max width; a row's spare width grows its blocks
   toward their max**, in order. Plan usage and Sessions max out at twice the
   Distribution's max. The prototype used: Plan usage 36–92, Sessions 72–92,
   Distribution 30–46.

At 238 columns this gives one row, 92 | 92 | 46 (15 of 48 rows used): every
table column shows and KEY doesn't truncate. Under about 110 columns each
block takes its own row in the same order (37 of 48 rows at 100). In between,
Plan usage and Sessions share a row and the Distribution wraps under them.

## Consequences

The exact widths, thresholds and short-height give-way order are left to the
Spec. Sessions wants 93 columns with its two-space gaps, so a 92 max puts it in
its one-space squeeze.
