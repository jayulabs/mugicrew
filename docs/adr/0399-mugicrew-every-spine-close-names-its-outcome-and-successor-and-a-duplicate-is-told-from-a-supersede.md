# ADR 0399: mugicrew Every spine close names its outcome and successor, and a duplicate is told from a supersede

Status: Accepted
Date: 2026-10-29
Amends: ADR 0396 (mugicrew-the-hold-clearing-step-reads…) §5, which states the `<outcome>[: <detail>]` convention and leaves the spine kinds to follow it.

## Context

The success closes of Signal, Chart, Spec, Checkpoint and Build carry bd's
default `Closed`, so a Signal that was charted, fast-laned or turned into an
Initiative closes the same way. Rejects are free text (`Not worth doing:…`,
`by design:…`, `rejected from TUI`), so a reader can't classify them by prefix.
`bd supersede` writes no reason at all (an empty `close_reason`), so a
duplicate and a pivot replacement look the same as each other and as a
malformed close.

## Decision

1. **Success closes name the outcome and the successor.**
   - Signal: `charted: <chart id>`, `specced: <spec id>` (fast lane),
     `initiative: <initiative id>`.
   - Chart (drained into its Spec): `specced: <spec id>`.
   - Spec: `sliced: <checkpoint id>`.
   - Checkpoint: `landed: <sha>`, the same reason as its Landing.
   - Build: `built` (it has no successor).
2. **One reject prefix.** Every Signal reject is `rejected[: <why>]`, whether
   Luffy's triage or the TUI `z` writes it. A blank `z` writes a bare
   `rejected`. A sub-reason goes in the detail (`rejected: no code change: …`),
   never as a word of its own.
3. **A duplicate is told from a supersede.** mugicrew writes the reason before
   it adds the `supersedes` edge: it closes the bead with the reason, and
   `bd supersede` on a closed bead adds the edge only. A triage or charting
   duplicate and a Jira re-intake close `duplicate: <original id>`. Pivot
   confirm and escalate close `superseded: <new id>`. Prompts call the mugicrew
   door instead of running raw `bd supersede`.
4. **History is not rewritten.** Beads closed before this decision keep their
   reasons. Readers treat `Closed` and an empty reason as legacy and fall back
   to lineage edges. For example, `tui.chartOutcome` keeps its "a Spec is
   caused by it" test for landed, and keeps reading an empty reason as
   superseded.

## Considered Options

- **One `accepted: <successor>` word for every Signal accept**: the successor's
  kind would still have to be looked up to tell charted from fast-laned.
- **Keep bd's empty supersede reason and classify by the `supersedes` edge**:
  an empty reason can't be told from a close that lost its reason, and the edge
  doesn't say whether the bead was a duplicate or a replacement.
- **Backfill old reasons**: about 1,850 closed spine beads, most of them
  `Closed`, can't be reworded reliably, and their lineage edges already
  classify them.

## Consequences

- The bead lifecycle reference's Legend cites this ADR, and its spine closure
  tables quote these reasons. Gaps entry 3 is resolved by the Spec that
  implements it.
- The succession test fixture that expects `superseded by <id>` (a text bd
  never writes) is replaced.
- Nami's Chart reject and duplicate closes in the charting prompt are not
  covered here. They follow in their own Signal.
