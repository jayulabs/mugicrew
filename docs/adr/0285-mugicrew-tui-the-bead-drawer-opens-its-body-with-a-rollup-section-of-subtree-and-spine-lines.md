# ADR 0285: the bead drawer opens its body with a ROLLUP section of subtree and spine lines over the whole retained log

Status: Accepted
Date: 2026-10-17

Chart: Session cost and Agent time rolled up over a bead's subtree. Settles the
prototype "Where the subtree and spine rollup lines sit in the bead detail
drawer". Amends ADR 0265 (mugicrew-tui-the-bead-drawers-end-with-one-shared-body…)
§1 (section order) and ADR 0061 (mugicrew-stats-verb) §11 (CLI only).

Three placements were tried in the real drawer: two header fields under `age`,
a section atop the shared body, and a section at its end. Header fields are
built per tab, so they would need wiring in every drawer; a section at the end
falls below the fold on any bead with a long description. A section in
`graphData.beadBody` reaches every bead drawer at once, and atop the body it is
visible on open. So:

1. **Placement:** a `── ROLLUP · all retained log ───` section in `beadBody`,
   after the close line and before DESCRIPTION, in every bead drawer ADR 0265 §1
   names. The multi-bead summary rows and the Logs tab stay out.
2. **Lines:** exactly two, as label fields: `subtree` then `spine`, each
   `<Agent time> · <Session cost> · N sessions` (TOTAL only), Agent time as
   `stats.Dur`, a Partial cost marked `$1.23+` as `stats.Cost.USD` does.
3. **Window:** the whole retained log, the CLI's default (ADR 0061 §3), not the
   Stats tab's `w` window; the rule's label says so.
4. **Loading and failure:** the bd lookup runs in a Cmd (ADR 0098
   (mugicrew-tui-press-time-bd-calls-off-the-event-loop)) and `beadBody` renders
   only cached results (ADR 0101 (mugicrew-tui-render-runs-no-subprocess)).
   Until a result lands each line reads soft `loading…`; a failed lookup reads
   soft `couldn't load: <err>`, and `f5` drops it to refetch, as comments do
   (ADR 0265 §6).
5. **No sessions:** the line stays and reads soft `no sessions`, so the section
   always has the same two lines.

## Considered and rejected

- **Following the Stats tab's window:** the drawer has no window control, and a
  rollup silently trimmed to 7d would disagree with `mugicrew stats <bead>`.
- **Hiding a line, or the section, with no sessions:** an absent line reads as
  "not loaded yet" or "not supported here".
