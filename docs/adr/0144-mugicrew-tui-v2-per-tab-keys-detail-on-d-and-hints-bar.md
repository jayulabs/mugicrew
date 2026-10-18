# ADR 0144: v2 per-tab keys: detail moves to `d`, `space` folds, and the hints bar lists only verbs that apply

Status: Accepted — §7's drop order, "always ends with `? keys`" and dialog rows superseded by ADR 0229 (…hints-bar-and-key-reference…) (only-verbs-that-apply and row membership kept)

Amended by ADR 0164 (§2 and §7: `enter` is mode-aware, `t` is dropped).
Amended by ADR 0171 (§1: detail is a bottom drawer on every tab, Logs included).
Amended by ADR 0228 (§2, §3, §4 and §7: G and L go into the goto menu; g goto is the one shell key in the hints bar).
Amended by ADR 0241 (§2 and §7: n and u leave Graph; n new is a second bar exception beside g goto).
Amended by ADR 0258 (§7: E raw is removed; E is a shell key opening $EXPLORER).
Amended 2026-10-17 (mc-4ryp): History's rollback moved from `r` to `u` undo (§3, §7 and the History hint row), refusal texts unchanged; `r` is now retrospective.
Date: 2026-10-12

Chart mc-lu2x.1 (V2 TUI shell beside v1). Settles grilling mc-lu2x.1.6, the
per-tab mapping that ADR 0142 §2 and ADR 0143 §3 left open. Amends ADR 0141 §5:
`d`, not `space`, toggles the detail pane. Confirms ADR 0143 §4 (`space` folds
on Graph). Used ADR 0142 §6's free `r` on History for rollback; that moved to `u` undo (see the 2026-10-17 amendment).

## Decision

1. **`d` toggles the detail pane** on every tab that has one (Graph, Logs), and
   `esc` also closes it. **`space` folds the selected group on Graph** and does
   nothing on any other tab. Each key has one meaning everywhere it is bound.
2. **Graph.**
   - `enter` launches or attaches. On a row that can't launch (an Initiative
     head, a closed bead, a bead with a write in flight), it does nothing and
     the status line says why. It never falls back to folding.
   - `s` opens the status picker and `f` opens the kind picker (ADR 0143 §3).
   - v1's `b t p a A + = - x z e E n u h L` keep their v1 meanings. `l` is
     dropped, and is not kept as an alias. `G` is not offered on Graph.
   - `z` opens one modal dialog that holds the reason input: `enter` rejects
     and `esc` cancels. The create forms (`n`/`u`) and the edits (`e`/`E`)
     are modal boxes with no y/n step.
3. **History.** `enter` and `G` reveal the commit's bead in Graph. `L` opens
   Logs scoped to that bead. `u` undo (v1 `R`, first `r`) opens the rollback form as a modal
   dialog, and that form counts as ADR 0142 §7's dialog. `d`, `space` and `/`
   do nothing. Refusals stay on the status line.
4. **Logs.**
   - `enter` does nothing.
   - `v` shows debug events (v1 `d`), because `d` is now detail. `e` shows
     errors only.
   - `f` opens a kind picker, and `/` filters on words as you type, as on Graph
     (no regex). `esc` clears the text, then the kind, then the scope.
   - `G` reveals the event's bead in Graph, and `L` scopes Logs to that bead.
5. **Stats.** `w` window, `b` group-by and `f` kind cycle stay. Stats keeps
   the cycle because it has only a few kinds and no list to filter. `f5` also
   refreshes usage. Nothing else is bound.
6. **Settings** is built straight to ADRs 0136/0137, so it includes Kind
   profiles; v1's two-row tab is not copied. `j`/`k` pick the row, `+`/`-`
   stage a change, and `enter` saves the selected row while `esc` discards it.
   Saving opens no dialog.
7. **The hints bar lists only the verbs that apply to the selected row** (for
   example `z` only on an open, unclaimed Signal, `u` undo only on a commit that can
   be rolled back, Settings' `enter`/`esc` only while a value is staged).
   Shell keys stay out of it and live in `?`. The bar always ends with
   `? keys`. When it is too narrow it drops verbs from the right, in the order
   below, and `? keys` never drops.

   | tab / mode | hints, in order |
   |---|---|
   | Graph list | enter launch · b bg · space fold · d detail · / search · s status · f kind · t run · p pause · x drop/abort/recover · z reject · n new · u surgery · e edit · E raw · +/- prio · a/A adopt/release · h closed · L logs |
   | Graph, detail open | the same, `d close`, plus `pgup/pgdn scroll` |
   | `/` input (Graph, Logs) | enter keep · esc revert |
   | picker | j/k move · enter pick · esc cancel |
   | form or reason dialog | enter submit · esc cancel (plus `tab field` on two-field forms) |
   | y/n dialog | y/enter confirm · n/esc cancel |
   | History | enter/G reveal · L logs · u undo |
   | Logs | d detail · / search · f kind · v debug · e errors · G reveal · L scope · esc clear (only while a filter is active) |
   | Stats | w window · b group · f kind |
   | Settings | j/k row · +/- stage · enter save · esc discard |

## Considered Options

- **`space` toggles detail and fold moves to a letter (ADR 0141 §5 as
  written).** Rejected. The human wanted fold to stay on `space`, so the
  detail toggle moved instead.
- **`space` folds on a head row and toggles detail on a leaf row.** Rejected.
  It would give one key two meanings depending on the row.
- **The hints bar lists every verb on the tab.** Rejected. A key that is shown
  should always do something on the selected row.

Amended 2026-10-17: `r retrospective` is bound on Ideate, Defog, Execute, Graph, Initiatives and the focus view. It is hinted on a Signal, Chart, Spec, Checkpoint or Initiative row and answers with a status line on any other bead row. History and Logs do not bind it, and on Graph it stays text while the `/` filter is typing. Rebase moved to the `n` New menu. History's `u` undo opens the same rollback form with the same refusal texts (the `r` rollback wording in §3, §7 and the History hint row is superseded).
