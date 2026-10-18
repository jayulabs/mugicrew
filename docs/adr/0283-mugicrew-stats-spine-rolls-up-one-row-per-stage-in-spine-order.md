# ADR 0283: `mugicrew stats <bead> --spine` shows one row per stage, in spine order, with a KIND column

Status: Accepted
Date: 2026-10-17

Chart: Session cost and Agent time rolled up over a bead's subtree. Settles the
grilling "Spine rollup CLI rows, and how --spine combines with --by and --json".
Amends ADR 0061 (mugicrew-stats-verb) §8 and §9 for `--spine` only.

A spine is a short chain of stage beads (Signal → Chart → Spec → Checkpoint),
each with many descendants, and the log carries no titles, so flat per-bead rows
sorted by cost would show bare ids with no sign of which stage they belong to.
The question a spine answers is "what did each stage cost", so:

1. **Rows:** one per stage bead the spine walk counts, each summing that bead and
   its whole parent subtree, then TOTAL. Stage subtrees are disjoint (no
   successor is a parent-edge child of its predecessor), so the rows add up to
   TOTAL. Detail for one stage is `stats <stage bead>`.
2. **Order:** spine order, not COST descending: by stage rank, then by creation
   time within a rank, so a superseded Spec or Chart sits just above its
   successor. No superseded marker; KIND and order show the pivot.
3. **KIND column** after BEAD, under `--spine` only.
4. **Empty stages** still get a row (0 sessions, `–` for metrics with no data),
   so the spine shape is always complete.
5. **`--by`** replaces the stage rows with group-key rows over the whole spine
   set, exactly as ADR 0061 §7; KIND is dropped.
6. **`--json`:** each stage row gains `"kind"`, and `rows` keeps spine order;
   `total` is unchanged. `--by` rows carry no `kind`. No top-level scope field:
   the caller knows it passed `--spine`.
7. **`--spine` without a bead** is a usage error (exit 2).

Which beads count as stages (pivot beads, Rollbacks, duplicate Signals) is the
walk's question, left to the Spec. The default subtree scope keeps one row per
bead plus TOTAL, sorted by COST (ADR 0061 §8).

## Considered and rejected

- **Flat per-bead rows (ADR 0061 §8 as is):** 30+ bare ids per spine, stage
  invisible.
- **Nested stage rows with indented descendants:** duplicates what
  `stats <stage bead>` already gives.
- **Stage × key rows under `--by`:** a row explosion nobody asked for.
