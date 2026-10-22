# ADR 0334: mugicrew goto bead rows and reveal land on Graph scoped to the group head

Status: Accepted; amends ADR 0228 (mugicrew-tui-v2-g-opens-a-goto-menu…) §4 and §5, ADR 0271 (mugicrew-the-goto-menu-reaches-discovered-from…) §5 and ADR 0232 (mugicrew-tui-on-demand-closed-beads-stay-loaded…) §1.
Date: 2026-10-21

Spec "Goto bead rows and reveal land on Graph scoped to the bead's group head".
Research: `docs/research/goto-landing-group-subtree-read-and-claim.md`.

The goto bead rows called the Recents jump (ADR 0183), scattering the human across
home tabs, and `r` scoped Graph to `{words: id, status any}`: the bead and its ancestors,
none of its group. Every goto bead move now lands on Graph, scoped to the target's group
head, so structure is followed where structure is shown.

## Decision

1. **One landing, `gotoLand`.** `p`, `c`, `o`, every origin and related row (by key or
   `j/k` + enter) and `r` land on Graph scoped to the target's group head with the target
   selected, from any tab and in place on Graph. It replaces the Recents jump as the bead
   rows' action (ADR 0228 §4, ADR 0271 §5) and the reveal message as `r`'s action
   (ADR 0228 §5). Recents enter, History enter and any other reveal sender keep today's
   behaviour. No landing writes a Recents interaction. It closes the Initiative focus view
   first.
2. **`r` is offered on Graph** whenever a bead is selected (amends ADR 0228 §4).
3. **The scope is a Graph query facet**, the head's id: Graph shows that group's whole
   subtree at status any; `/`, `s` and `f` narrow inside it; esc unwinds words, status
   (to any), kind, then the scope. It is a visit, never saved to `graph-filter.json`, and
   its folds live in the query's folds, never `folds.json`.
4. **The head** is the root of the parent chain when that is a container, else the bead
   itself (a self-scope: a Signal, or a child of a non-container). Containers nest only
   under Initiatives, so chains are at most two parent edges; the subtree read names that
   assumption and a test pins it. Caused-by is never followed.
5. **The subtree is read in two bd calls** (one `bd sql` for the head and ids, one
   `bd list --id` for the beads not already loaded), off the event loop, under a fresh
   demand generation on every landing. It never runs `bd list --all`.
6. **A fifth claim (amends ADR 0232 §1):** the scope's whole subtree, held while the
   scope is set and Graph is the tab, whatever narrowing rides on it. Seeded at press time
   with the bead and its known chain; filled from the read's ids; released on leaving
   Graph (and re-read on entry) and emptied by esc clearing the scope. It is excluded from
   the all-closed claim and from the global closed word search.
7. **In flight and on fold.** The landing switches to Graph at once with a provisional
   scope (grandparent, else parent when it is a container, else the bead) and
   `searching…`, the cursor on the bead, and the status line `fetching <head>'s group…`.
   On fold: a stale generation, or a scope esc or a newer landing replaced, is dropped; no
   rows undoes to the previous tab and Graph with `<id>: not in the graph`; an error keeps
   the provisional scope with `fetch failed: <err>; showing the loaded part of <head> (esc still clears)`;
   success sets the true head, fills the claim, opens only the landed bead's ancestor chain
   (a head opens to show its members) and says `scoped Graph to <head> (esc clears)` or
   `scoped Graph to <id>, no group (esc clears)`. A cursor move during the fetch only stops
   the cursor being pulled back; the scope still switches.

## Considered Options

- **Keep the Recents jump for the bead rows.** Rejected: it lands on a home tab, not on
  structure.
- **Follow caused-by lineage for the scope.** Rejected: the scope is the parent chain only.
- **Re-read the subtree on the slow tier.** Rejected: a visit does not need closed
  children added after landing; the next landing picks them up.
- **Read the whole graph (`bd list --all`) for a scope.** Rejected: two calls cost about
  0.7 s for the largest group against the whole store.
