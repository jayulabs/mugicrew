# ADR 0172: v2 Ideate list in Defog's row style, with foldable Initiatives whose folds persist per Workspace

Status: Accepted

Amended by ADR 0177 (§3: an Initiative is also listed when drained or while its cut session exists).
Amended by ADR 0315 (§3: the fold-away fallback covers any selected row, an Initiative included).
Amended by ADR 0178 (§1, §3: enter launches or attaches the cut session on a drained Initiative header, or one with a cut session; the drained hint row and its fold).
Date: 2026-10-13

Chart mc-lu2x.25 (Ideate list matches Defog's row style, with collapsible
Initiative rows). Settles grilling mc-lu2x.25.3, which followed the human's
try of prototype mc-lu2x.25.2, and records the Survey's calls from the Chart
Notes. Amends ADR 0149 §1 (hints) and §3 (row order and marks). The reference
look is the Defog tab (ADR 0156); the Defog tab itself is unchanged. Signal-row hints and rowSuffix are as ADR 0171 §3–§4 left them.

## Decision

1. **Keys and hints** (amends ADR 0149 §1). `space` folds and unfolds both
   Initiative headers and the Backlog header. `enter` no longer folds the
   Backlog (ADR 0144 §2).

   | row | hints, in order |
   |---|---|
   | Initiative header | space fold · d detail · n new |
   | Backlog header | space fold · n new |

   Initiative headers can be selected. `enter` on one does nothing and the
   status line explains why: launching an Initiative's cut session stays on
   Graph (ADR 0146). `d` shows the Initiative's detail.
2. **Signal rows** (amends ADR 0149 §3) use Defog's anatomy: stateGlyph and
   rowDot, `P<n>`, the title and a dim id, with rowSuffix at the right edge.
   The `◐ triage` mark becomes the shared ◐ glyph, and its waiting word
   moves to rowSuffix. The SIGNAL kind tag is shown.
3. **Groups.** Grouping and order follow ADR 0149 §3. Initiative headers are
   drawn as Defog's '▾ INITIATIVE-chip Title'. Loose Signals keep their 'No
   Initiative' header; they do not follow headerless as they do on Defog. Initiatives start
   unfolded and show `(N)` only while folded. The Backlog keeps its `(N)` and
   starts folded. If the selected Signal folds away, whether by a fold or a
   refresh, the cursor moves to its group's header.
4. **Folds persist across TUI restarts**, per Workspace and only on this
   machine, in `<git-common-dir>/mugicrew/ideate-folds.json` next to
   `config.toml` and `events.jsonl`. Every worktree shares it.
   - The file stores only folds that differ from the defaults, keyed
     `i:<initID>`, `i:` (No Initiative) and `backlog`.
   - It is written on every `space` toggle: a temp file in that directory is
     renamed over the old one.
   - On every save, keys whose Initiative is closed or gone are pruned, so a
     reopened Initiative comes back unfolded.
   - If a write fails, the status line says so and the folds stay in memory.
   - A file that does not parse is ignored (the defaults apply) and is
     replaced on the next toggle.

## Considered Options

- **Loose Signals headerless, as on Defog.** Rejected after the prototype try.
  The header keeps the Initiative groups and the Backlog visually parallel.
- **Folds only for the current session.** Rejected after the prototype try.
  Folding an Initiative is a lasting choice about what to look at.
- **Fold state in the user cache dir** (as the prototype did). Rejected. It
  would need a per-repo key inside the file, while `<git-common-dir>/mugicrew/`
  is already where per-Workspace, machine-local state lives.
- **Fold state in the bead graph** (for example, a label). Rejected. Folds are
  one person's view preference and must not sync to others.
- **Store every group's state.** Rejected. Storing only the differences keeps
  the file small and lets a changed default still apply to groups never
  touched.
- **Save on quit.** Rejected. Ctrl+C or a crash would lose the folds.
