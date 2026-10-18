# ADR 0286: `stats.Filter` takes a resolved scope map, and the `bd` package walks the lineage from one flat read

Status: Accepted
Date: 2026-10-17

Chart: Session cost and Agent time rolled up over a bead's subtree. Settles the
grilling "How stats.Filter takes a resolved bead set, and whether ADR 0033 needs
a note once stats reads bd". Amends ADR 0061 (mugicrew-stats-verb) §2 and its
Consequences. Leaves ADR 0033 (mugicrew-event-log-is-history-not-a-view)
untouched.

The subtree default and `--spine` (ADR 0283
(mugicrew-stats-spine-rolls-up-one-row-per-stage-in-spine-order), ADR 0284
(mugicrew-the-stats-spine-walk-counts-pivots-and-rollbacks-as-stages-and-skips-duplicate-signals))
need a bead set only bd can give (docs/research/bead-spine-and-descendant-walks.md
§5), while `stats` stays a pure adder over the log. Therefore:

1. **`Filter.Scope map[string]string` replaces `Filter.Bead`.** It maps an
   event's bead id to its row key; nil counts every bead. The subtree maps each
   id to itself (rows stay one per bead, ADR 0061 §8); `--spine` maps each id
   to its stage bead (one row per stage). Under `--by` only membership counts.
   The drawer (ADR 0285
   (mugicrew-tui-the-bead-drawer-opens-its-body-with-a-rollup-section…)) sums
   each scope and reads TOTAL. The `ev.Parent` match goes; the Logs tab keeps
   its own rule.
2. **The walk lives in `bd`**, beside `SpecOf`: one read builds an in-memory
   lineage graph with pure `Subtree(id)` and `Spine(id)` (ordered stages, each
   with its subtree) methods. The verb and the drawer's Cmd share it; the
   drawer caches it per snapshot refresh. `SubtreeIDs` stays for its callers.
3. **The input is one flat `bd sql` dump** (~0.3 s, size-flat): parent-child,
   caused-by, supersedes, related and blocks edges, plus each bead's kind and
   created time. `bd.Bead`/`parseBeads` gain no `Supersedes` field.
4. **When bd can't answer:** a failed bd read exits 1, with no fallback to the
   log's bead/parent pairs (they undercount silently). A bead unknown to bd
   scopes to itself alone, so a deleted bead's history still sums; `--spine` on
   it exits 2. `mugicrew stats` with no bead makes no bd call.
5. **ADR 0033 needs no change.** bd supplies scope only; the log stays
   non-authoritative history and nothing decides dispatch from these numbers.
   ADR 0061's Consequences now read: `stats` reads history, and bd for scope
   only.

## Considered and rejected

- **A membership-only set, the verb regrouping rows into stages**: two places
  that know the spine's grouping instead of one map.
- **`stats` importing `bd`**: breaks the package's purity and its fixture tests.
- **`Supersedes` on `bd.Bead` over `ListAll`**: 2–4× slower and widens
  `parseBeads` for every caller.
