# ADR 0249: v2 `t` opens a bead's transcript view in the Watch pane, and `enter` no longer watches

Status: Accepted
Date: 2026-10-17

Chart mc-pxfb (Show any bead's session transcripts, finished or live, in a
readable view). Settles grilling mc-pxfb.4. Amends ADR 0164
(…enter-is-mode-aware-on-every-tab) §3 and §5 and ADR 0229
(…hints-bar-and-key-reference…) §3. Before this ADR, `enter` on a live AFK row
opened the Watch pane (watch-run) and the Sessions modal's `enter` watched AFK
rows. The new transcript view also covers finished, failed and HITL sessions,
so it needs a key that does not depend on whether the bead is live, or on its
mode.

## Decision

1. **`t` opens the bead's transcript view**, for HITL and AFK sessions alike.
   `t` was freed by ADR 0164 §2. It is bound on every tab that lists beads
   (Graph, Ideate, Defog, Execute, Initiatives), in the focus view, and in the
   Sessions modal. Logs and History reach it through `g r` and then `t`.
2. **Rows.** `t` acts on any bead row that has a recorded session, whatever the
   bead's kind: Charts, Checkpoints, Initiative heads and closed beads included.
   Group headers, '+ New Signal' and '▸ N closed' rows have no bead and are
   outside it.
3. **Shown only where it acts, answers everywhere** (as ADR 0165 §4 does for
   `b`). The hints bar shows `t transcript` only on rows where the TUI knows
   a session. On any other bead row, `t` stays hidden and answers "no sessions
   recorded for <id>" on the status line, opening no pane. If a per-row "has a
   session" check turns out to be too expensive (today the TUI finds only the
   newest `session.started`), the hint shows on every bead row and the press
   still refuses on the status line. The Spec settles which.
4. **Where it opens.** `t` opens the view in the Watch pane: the read-only
   40% tmux split that watch-run uses. There is one pane at a time. `t` on
   another bead replaces its content, and `t` on the bead it already shows
   focuses it.
5. **`enter` no longer watches.** On a live AFK row, `enter` takes ADR 0164
   §3's refusal and reads "<id> is running — t shows it · x aborts it". The
   hints bar leaves `enter` out there (ADR 0164 §5).
6. **The Sessions modal's `enter` matches the bead's own `enter`.** It attaches
   or resumes a HITL session and refuses on a live AFK row with the text in 5.
   `enter watch` is removed. The modal's keys are `enter attach/resume` (HITL
   rows), `t transcript` and `x abort`. The modal has no jump key.
7. **Hints and `?`.** `t transcript` goes in the row section right after `x`,
   with a drop rank right after `x`'s in ADR 0229 §3's table. `?` keys lists
   `t  transcript (HITL/AFK)` on every tab that binds it.

## Considered Options

- **`w` (after the Watch pane) or `v` (view).** Rejected. The human picked `t`
  because one key serves HITL and AFK transcripts alike. `v` would also clash
  with Logs' `v debug`.
- **A goto menu row (`g s`).** Rejected. It takes two keys, stretches what
  goto means, and `g` does nothing inside a modal (ADR 0228 §2).
- **Keep `enter` opening the pane on a live AFK row.** Rejected. `t` is now
  the one way into a transcript, so `enter` only refuses there.
- **The Sessions modal's `enter` jumps to the bead's home tab (ADR 0183's
  Recents jump), on every row or only on AFK rows.** Rejected after
  discussion. The modal lists live sessions with the waiting ones first, so
  attaching is its main action. Matching the bead's tab `enter` keeps one
  meaning per bead, while a jump on AFK rows only would make `enter`
  mode-dependent. Recents is a history of places, so a jump fits there but not
  here.
- **One Watch pane per bead, a full tmux window, or a view inside the TUI.**
  Rejected. The Chart's Destination makes the Watch pane where the TUI shows
  the view, and reusing one pane keeps watch-run's focus-or-reuse behaviour.
