# ADR 0138: Settings keys act on the selected row, and the footer says so; no jump keys

Status: Accepted
Date: 2026-10-12

Chart mc-snpl (Edit the kind → Agent Profile mapping from the TUI Settings tab),
grilling mc-snpl.5. Sharpens ADR 0109 §4 and §7 for a Settings pane of about 19
rows (ADR 0136), where several rows may be staged at once.

## Decision

1. **`enter` and `esc` act on the selected row only.** `esc` discards the
   selected row's staged value, never every staged row, so ADR 0109 §4's "esc on
   the tab discards" means "the selected row's". There is no save-all key and
   no discard-all key.
2. **The footer shows `enter save · esc discard` only when the selected row is
   staged**, not whenever any row is. It shows no `N unsaved` count: the tab's
   `•` and each staged row's `(unsaved)` are enough.
3. **`+/- change` on every row.** A kind row's `+/-` cycles profile names, but
   the footer verb stays the same. A footer that changes per row is noise.
4. **The `?` lines for Settings:**
   - `j/k/↓/↑  select a row (the pane scrolls)`
   - `+/-  stage the row: pool ±1, Auto-exit ±30s, kind: next / previous profile or (unset)`
   - `enter  save the selected row's staged value (this machine only)`
   - `esc  discard the selected row's staged value`
   - a note: `saving (unset) removes this machine's key, so the kind falls back to shared, then default`

   No note about unrunnable profiles: the row shows its reason.
5. **No jump keys.** `j/k` walk the rows, and the `↑/↓ N more` lines show what
   is clipped. Add a jump only if this hurts in use.

## Considered options

- **`g/G` top/bottom**: ruled out. `G` is the Graph tab on every tab.
- **`home/end`, next staged or unrunnable row, jump to the section**: rejected
  for now. At 19 rows, holding `j` takes about a second, and each key costs a
  `?` line, a `settingsKeys` entry, and help-coverage upkeep.
- **Bulk discard on `esc`**: rejected. A bulk discard you can't see loses work
  across rows the human may not have in view.
