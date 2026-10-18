# ADR 0176: v2 Defog, Execute and Graph folds, and Graph's filter, persist per Workspace

Status: Accepted
Date: 2026-10-14
Amended by ADR 0179 (§6: Defog and Execute folds, with Ideate's, share one folds.json in place of the per-tab files; Graph's files are unchanged).

Chart mc-r0bw (v2 Defog, Execute and Graph folds, and Graph's filter, persist
per Workspace). Records the Survey's calls from the Chart Notes and settles
grilling mc-r0bw.2 (the pruning rules). Extends ADR 0172 §4 from Ideate to
Defog, Execute and Graph. For v2 it supersedes ADR 0143 §7 (the query resets on
restart) and the seed-once of ADR 0175 §6. v1 (tui/) keeps ADR 0077.

## Decision

1. **Same file shape as Ideate** (ADR 0172 §4). Each tab keeps one file in
   `<git-common-dir>/mugicrew/`, next to `ideate-folds.json`:
   `defog-folds.json`, `exec-folds.json`, `graph-folds.json` and
   `graph-filter.json`. Each fold file stores only the folds that differ from
   the defaults. It is written on every `space` toggle by renaming a temp file
   over it.
2. **Graph folds are flips from a live default.** A group head is folded by
   default if and only if it is a closed root or the Unparented group (key
   `""`). The default is computed on every rebuild, not seeded once. A refresh
   still never re-folds a group the human opened: that open state is a flip,
   and flips persist. Only `collapsed` persists. `qfold` stays session-only,
   so a restored query opens its groups as it does today (ADR 0143 §4).
3. **Pruning on save.** Every save drops these keys from the file. The fold
   still applies for the rest of the session.
   - Defog and Execute drop a key when the bead its id part names is closed
     or gone. A trailing `/closed` is cut off first, so `<chartID>/closed` and
     `<cpID>/closed` go when their Chart or Checkpoint does. The same applies
     to `i:<initID>`, `<chartID>` and `<cpID>`. `s:act` and `i:` are never
     dropped.
   - Graph drops a key only when its bead is **gone** from the graph, not
     merely closed. `""` (Unparented) is never dropped.
   - Pruning runs only on save. Nothing is pruned at load or on refresh: a
     stale key's bead is not shown, so it is harmless until the next toggle.
4. **The Graph filter is saved when committed.** That means `enter` on words,
   a facet pick, each normal-mode `esc` clear, and a `G` reveal that changes
   the query. It is never saved on a keystroke. The restored query shows in
   the header line, the only startup notice.
5. **Failures** follow ADR 0172 §4. A file that cannot be read or does not
   parse is ignored, so the defaults apply (the `unclosed` baseline for the
   filter, ADR 0175), and the next save replaces it. A failed write is
   reported on the status line, and the state stays in memory. A saved query
   that parses but has a status not in the picker, or a kind that is not a
   contract kind, loses just that value: it falls back to the baseline
   (`unclosed`, any kind) and the rest is kept. A valid value that is missing
   from the snapshot stays active (ADR 0076 §3).

## Considered Options

- **Prune Graph keys for closed beads, as Ideate does.** Rejected. Graph's
  default folds closed roots, so the only reason to store a closed root's key
  is that the human opened it. Pruning on close would throw that choice away
  on the next save. The same goes for folding a closed nested chain head.
  Unlike Defog and Execute, Graph still shows closed beads under the `any` and
  `closed` statuses.
- **Keep the seed-once (ADR 0175 §6) and persist the whole `collapsed` map.**
  Rejected. Storing the seeded defaults would freeze them. A root that closes
  later would not fold, and the file would no longer have Ideate's
  differences-only shape.
- **Persist `qfold`.** Rejected. The folds made while filtering belong to that
  query, and a fresh query opens every group (ADR 0143 §4).
- **Save the filter on every keystroke.** Rejected. A half-typed query would
  survive a crash, so the TUI would come back to a filter the human never
  committed.
