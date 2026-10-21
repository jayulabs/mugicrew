# ADR 0179: v2 queue tabs share Actionable sections, two-tone badges, folded trailers, ancestor shade and one folds file

Status: Accepted; §1 amended by Spec mc-qgtw (Actionable gains a paused band for own-paused ready beads); §1 amended again by Spec mc-llx8 (the paused band also lists own-paused AFK beads: on Execute every open Checkpoint member, on Defog every open Chart child); §1 amended again by Spec "Actionable's paused band lists every own-paused bead on its home tab" (mc-f22wp: the paused band lists every own-paused, otherwise-ready bead whose home tab is the tab)
Date: 2026-10-14

Chart mc-c3es (Queue-tab parity). Settles grilling mc-c3es.4 after the
prototype mc-c3es.3 and the research note
`docs/research/queue-tab-fold-persistence-in-one-file.md` (mc-c3es.2). For
the v2 Ideate, Defog and Execute tabs it amends ADR 0149 §4, ADR 0156,
ADR 0170, ADR 0171 §5 and ADR 0172 §3–§4, and extends ADR 0175 §4–§5 (counts
on folded headers) from Graph to these tabs. Graph, History and the rare tabs
are unchanged.

## Decision

1. **Actionable section.** Each tab opens with an unfolded, foldable
   **Actionable** section: sessions waiting on you first, then the tab's
   takeable frontier, each band in beadCmp order (membership per the Chart's
   Notes). Its fold key is `s:act` on every tab, as Execute's already is.
2. **Two-tone badge** (amends ADR 0171 §5, 0149 §4, 0156, 0170). Amber `●N`
   counts sessions waiting on you and wins whenever N > 0; otherwise a light
   blue `●N` (colour 111, as the Actionable header's count) shows the
   Actionable count; no badge when both are zero. The Actionable header keeps
   its light-blue count while the badge is amber. Amber still only means
   "someone waits on you" (ADR 0171's objection holds).
3. **Every container starts folded** (amends ADR 0172 §3): Initiatives,
   No Initiative, Backlog, Defog's Charts, Execute's Checkpoints, at every
   depth.
4. **Folded trailer** (extends ADR 0175 §4–§5): `<title> · N open · M live`.
   `open` counts every open descendant the fold hides (on Ideate an
   Initiative's whole open subgraph, as Graph counts), not direct children.
   A zero part is dropped; with both zero the header is the bare title.
5. **Ancestor shade** (as Graph's ancRow): selecting a row shades its Parent
   chain, the group header its tree twin sits under (folded or not), and its
   twin both ways (an Actionable row shades its tree twin, a tree row its
   Actionable twin). Execute's Task and Pivot rows shade their Parent chain
   only, never the Checkpoint their `in → cp` hint names: shade means
   containment, and the pink blocker marks already show blocking.
6. **One folds file** (amends ADR 0172 §4). Folds persist in
   `<git-common-dir>/mugicrew/folds.json`, shaped
   `{"ideate":{…},"defog":{…},"execute":{…}}`; each tab saves only its own
   section by read-modify-write through the temp-file rename, last writer
   wins. Each section stores keys flipped from the default (so, now, unfolded
   containers); toggling back to the default deletes the key. On save every
   tab prunes an `i:<id>`, Chart or Checkpoint key (with or without its
   `/closed` suffix) whose bead is closed or gone; `i:`, `s:act` and the
   Backlog key are never pruned. The Backlog key stays `"\u0000backlog"`
   (ADR 0172 §4's `backlog` was its description, not its spelling).

## Considered Options

- **Migrate or version `ideate-folds.json`.** Its `i:*` keys meant "folded"
  and would invert under the new default; migrating needs the bead list at
  load time and restores the all-unfolded view. The new file name is the
  version: nothing reads or deletes the old file, and a saved
  Backlog-unfolded choice is lost once.
- **Trailer forms** `(N)  M live`, two spaces, ` — `, brackets, right edge:
  rejected in the prototype for ` · `.
- **Shading the hinted Checkpoint** from an Execute Task/Pivot row: rejected,
  it would blur shade (containment) with pink (blocking).
