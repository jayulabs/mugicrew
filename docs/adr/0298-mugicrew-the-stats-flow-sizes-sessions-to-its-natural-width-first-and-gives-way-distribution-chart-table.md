# ADR 0298: The Stats flow sizes Sessions to its natural width first and gives way Distribution, chart, table

Status: Accepted
Date: 2026-10-18
Amends: ADR 0297 (mugicrew-the-stats-tab-flows-usage-sessions-distribution…), its max-width ratio and growth order

Chart: Stats tab layout that gives the Sessions table room. Settled in the
grilling "Flow layout's widths, wrap thresholds and give-way order".

## Decision

1. **Widths.** Plan usage 36–92, Distribution 30–46. Sessions has a min of 72,
   and its max is its *natural* width: two-space gaps, every column, KEY
   untruncated, measured from the rows it shows. The 2× rule holds only for
   Plan usage (92 = 2 × 46). A fixed 93, or a 47-wide Distribution with
   94-wide peers, was rejected. The table's width moves with its data, and
   94|94|47 overflows the human's 238-column pane.
2. **Gutters and thresholds.** There are 2 columns between blocks in a row
   and one blank line between rows. So Usage|Sessions share a row from 110
   columns (36+2+72) and all three share one from 142 (+2+30). Both
   thresholds follow from the minimums and are not separate constants.
3. **Spare width goes to Sessions first**, then Plan usage, then the
   Distribution, each up to its max. Strict left-to-right order would leave
   the table at 72 across the whole 110–141 band, and giving it room is what
   this Chart is for.
4. **The squeezes stay as they are.** Inside the width Sessions gets, a gap of
   1, then short headers, then dropping the averages still fire when KEY would
   fall under 8 columns. They were always width-driven, and retuning them
   would change the table's content, which the Chart puts out of scope.
5. **Short-height give-way, across the whole tab:** the Distribution hides
   whole behind its "(distribution hidden: too short)" placeholder, then the
   burn chart shrinks by its own rules (ADR 0259), and the Sessions rows go
   last while the no-cost footer stays. Within a row, the blocks sit side by
   side and each gives way only if it alone is taller than the height left.
6. **Nothing fills the space beside a Distribution that wraps alone.** It
   stays at most 46 wide and left-aligned. Stretching it brings back the wide,
   sparse bars ADR 0297 moved away from, and tucking it under a shorter block
   breaks the rule that rows are separated by a blank line.
