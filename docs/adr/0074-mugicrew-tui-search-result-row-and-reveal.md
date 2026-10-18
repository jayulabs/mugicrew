# ADR 0074: mugicrew TUI Search result row, and `G` reveals the selection in the Graph

Status: Accepted; for v2, §3's `h` flip superseded by ADR 0175 (v1 keeps it)
Date: 2026-10-06

Amends: ADR 0058 §1 (`G` is a plain tab jump) for the Search tab, and fills the
`<reveal>` footer slot left by ADR 0073 §7. Chart: Search tab. Settled
in grilling. Builds on ADR 0072 §6 (rows fit the 60-col cap and work
full-width down to 40 cols).

## Context

A Search result must fit the list pane at every width, from 40 cols in
single-pane mode up to the 60-col cap. It must also show why the row sits where
it does in the active sort. Graph and Inbox already share a row prefix
(selection marker, state glyph, liveness dot, kind tag) and cut only the title
(Spec FR1/FR2). Results also need a way back to the bead's place in the
tree. That place may be hidden by `h` (closed roots) or folded into a collapsed
group. ADR 0073 §5 leaves kindless beads out of Search, so every result has a
Graph row.

## Decision

1. **A result row is the Inbox row, with the sort key as its suffix:**
   `❯ ○· GRILLING <title>  3d`. The suffix shows the active sort: age since
   `updated`, age since `created`, or `P0`–`P4` for `priority`. The usual
   rowSuffix follows it (claim age / `hitl`, `done`), as it does on Inbox.
   There is one layout at every width and no id column. The id stays in the
   detail pane, and text queries still match it. Only the title is cut. A closed
   result's title uses the Graph's dimmed closed style.
2. **`G` on Search reveals the selected result.** It switches to Graph with
   that bead selected. A Chart (container) lands on its group header. With no
   selection (empty results), `G` is a plain jump that keeps Graph's own
   selection. This mirrors `L`, which scopes Logs to the selection on Graph and
   plain-jumps elsewhere (ADR 0029 §3, ADR 0058 §1).
3. **Reveal forces the target visible.** If `h` hides it (a closed root, or a
   member of a closed-root Chart), `h` is switched off and the status line reads
   `showing closed root beads`. If its group is collapsed, that group, and only
   that group, is expanded.
4. **Footer** (ADR 0073 §7): `/ search · f status · c kind · s sort · enter ·
   G reveal · ? · q`. At narrow widths, hints drop in this order: `s`, `c`, `f`,
   `G reveal`. `/` is kept the longest.

## Considered options

- *An id column, at least on wide panes.* Rejected: about 10 cols, a third of
  the title at 40 cols. Per-width layouts were also rejected in favour of one
  row whose title alone is cut.
- *A fixed suffix (always created-age, as on Inbox).* Rejected: under the
  priority or updated sort, the row wouldn't show why it is placed there.
- *A dedicated lowercase reveal key (`g`).* Rejected: `G` already means Graph,
  and `L` set the precedent of a jump key that carries the selection.
- *Refuse to reveal a hidden bead, or reveal only through collapse.* Rejected:
  reveal is an explicit request to see this bead. The `h` flip is announced and
  undone with one key.

## Consequences

- A reveal can change Graph view state: `h` turns off, and the target's group
  is expanded. Graph's previous selection is replaced.
- `G` now behaves differently on Search and Inbox. Whether Inbox's `G` should
  also reveal is split off as an offshoot Signal, outside this Chart.
