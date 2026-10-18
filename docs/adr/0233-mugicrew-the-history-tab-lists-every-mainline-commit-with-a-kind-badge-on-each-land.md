# ADR 0233: The History tab lists every mainline commit, with a kind badge on each land

Status: Accepted
Date: 2026-10-16

Spec mc-ydm8 (History lists every mainline commit as a row, with a kind badge on
each Chart, Checkpoint and Spec land).

History showed only Checkpoint lands and their reverts, folding everything else into
an inert `+N hidden commits` line. The human could not see when a Chart landed or a
Spec was anchored. That shape was never an ADR; it came from Spec clauses this ADR
replaces.

## Decision

1. **Row kinds.** Each first-parent mainline commit (newest 200, merged across the
   Workspace and declared repos) is exactly one row: a **land** (Checkpoint, Chart or
   Spec anchor, told apart by the trailer's kind), a **revert** (`↺ … revert of …`,
   unchanged), or a **plain** commit shown dimmed as `sha · [ctx ·] age · <subject>`.
   There is no hidden row and no `+N` line; every row is selectable.
2. **Kind badge.** A land row is `sha · [ctx ·] age · title (id)`, then the Rollback
   badge (warn style, computed only for Checkpoint lands) if any, then the kind badge
   last, reusing the Graph's `kindTag` (Chart and Checkpoint as their container chip,
   Spec as its coloured tag). It is built from the trailer's kind, so a land whose bead
   is missing from the snapshot still shows the bare id and its badge. Revert rows
   carry none.
3. **enter** reveals the row's bead in Graph on every land row and on a revert row
   (a bead missing from the snapshot answers `<id>: not in the graph`). It is unbound
   on a plain row.
4. **u** (help label `undo`; it was `r` before Spec D6, so `r` means one thing across the TUI) goes through the one `refusal` pattern, ahead of the Checkpoint guards: `a
   Chart land can't be rolled back`, `a Spec anchor can't be rolled back`, `not a
   land` for a plain row. A refusing row shows `u` only in `?`; the rollback form's
   submit recheck uses the same refusal. Checkpoint Rollback filing is otherwise
   unchanged.
5. **Last-wins trailers.** A trailer is a whole message line; when a commit carries
   several land-trailer lines the last wins (`TrailerBlock` writes the trailer at the
   message's end, so a prose `Spec: …` line in a body cannot outrank it). The revert
   trailer follows the same rule.

## Supersedes and amends

- **Supersedes**, by Spec title, "History tab in the mugicrew TUI with Rollback
  filing": FR3 as the only land row, FR5 hidden runs, its one-kind Git-seam and
  Folding decisions, and its Out of Scope "Rows for Chart lands or Spec anchors" and
  "Expanding a +N hidden line".
- **Amends** "Opt-in v2 TUI shell (mugicrew --v2) … ported from v1": FR45's folded
  `+N hidden` runs and its "History has no ADR of its own" clause; FR46's "the
  commit's bead" now covers Chart and Spec rows.
- **Keeps in force** ADR 0167 (one-workspace-many-repos-the-label-binds…), ADR 0047
  (rollback-kind-and-entry) §3 and §5, and ADR 0209 (a-pending-rollback-logs-revert-
  outcomes…) §4. ADR 0209's premise that the anchor commit is invisible in the TUI no
  longer holds under `land_strategy=local`: a Spec anchor is now a History row.
  ADR 0287 (a-direct-spec-s-anchor-is-a-bead-pin-under-every-land-strategy): new direct Specs add no History row (the anchor is a bead pin); ADR 0233 stays in force for legacy `Spec:` commits.
