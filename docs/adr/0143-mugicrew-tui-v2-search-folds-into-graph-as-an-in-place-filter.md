# ADR 0143: v2 Search folds into Graph as an in-place tree filter

Status: Accepted; for v2, §3 (the `any` default and the rejected `unclosed` option) and §8's `h` clause superseded by ADR 0175 (v1 keeps them)
Amended by the Spec "G reveal scopes Graph to the revealed bead" (§6: esc on an untouched scoped reveal returns straight to the baseline, not one facet per press).
Date: 2026-10-12

Chart mc-lu2x.1 (V2 TUI shell beside v1). Settles grilling mc-lu2x.1.5. For v2
only, this supersedes the Search tab ADRs 0072–0077. v1 is frozen and keeps
them. It builds on ADR 0142 §2 (`G` reveal) and §6 (`/` filters the current
tab's list).

## Context

v1 has two ways to find a bead. Graph's `/` is a regex applied on `enter`.
Search is its own flat tab with live word matching, status/kind/sort pickers
and a `G` reveal back into the tree. v2 has no Search tab (ADR 0142 §1), so
Graph has to absorb what Search did.

## Decision

1. **The query filters the tree in place.** The body shows the beads that
   match, plus their ancestors (Chart or Initiative head) as dimmed context
   rows. There is no flat result list and no sort. The tree keeps its phase
   order.
2. **`/` takes words and filters live.** The text is split on whitespace, and a
   bead matches when every word appears in its title or id
   (case-insensitively, in any order). Results update on every keystroke.
   `enter` keeps the query, and `esc` in the input reverts the text (as in
   ADR 0073 §2). There is no regex.
3. **Facets: status and kind, each a picker** (ADR 0073 §4's picker, not v1
   Graph's `f` cycle). The status entries are ADR 0075's. Both facets default
   to `any`, so the bare Graph looks like v1 Graph and `h` (hide closed roots)
   stays as it is. With no kind facet set, kindless beads stay in the tree. The
   facet letters belong to the per-tab mapping (mc-lu2x.1.6).
4. **Folding.** While a query is active, every group with a match shows
   expanded, and `space` still folds. Clearing the query restores the collapse
   state from before it (v1 Graph's `preFilter`).
5. **One header line** sits above the tree while text or a facet is active:
   `/ words · status · kind   N of M`. N counts matches only. Context rows are
   selectable but not counted. The line is hidden when nothing is active.
6. **`esc` in normal mode** clears the text first, then each facet, one per
   press.
7. **Lifetime and reload.** The query lives for the TUI process, across tab
   switches and the refresh tick, and resets on restart (ADR 0077). The
   selection follows its bead by id. When that bead drops out, the selection
   goes to its nearest neighbour (ADR 0076 §2). A facet value that disappears
   from the snapshot stays active (ADR 0076 §3).
8. **`G` onto a filtered-out bead clears the Graph query.** The status line
   says so. The rest of ADR 0074 §3 still applies: `h` flips off and only the
   target's group unfolds.

## Considered Options

- **A flat result list that replaces the tree, with `esc` to restore it and a
  reveal key.** Rejected. It gives Graph two bodies, and reveal becomes an extra
  hop. The in-place filter keeps every selection a real tree row.
- **An in-place filter plus a key that flattens it into a sorted list.**
  Rejected. Sort is all that would be kept, and sorting a tree by recency
  fights the grouping.
- **Default status `unclosed` (ADR 0075), replacing `h`.** Rejected for Graph.
  It hides the closed members of open Charts, and those are useful context.
- **Keep v1 Graph's fold-on-apply rule (a matching head folds its group) and
  recompute it live.** Rejected. It churns the folds as you type.
- **Keep the query on `G` and show the target as a one-off context row, or
  refuse the reveal.** Rejected. Reveal is an explicit request to see the bead.

## Consequences

- Search's `updated`/`priority`/`created` sort has no v2 home. "What changed
  lately" would need its own Signal.
- A forgotten query or facet persists across tab switches. The header line is
  what makes it visible.
