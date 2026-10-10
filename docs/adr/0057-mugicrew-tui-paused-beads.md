# ADR 0057: mugicrew TUI paused beads — a pause takes the status-glyph slot, and `p` toggles a bare manual pause

Status: Accepted
Date: 2026-10-04

## Context

ADR 0055 makes a pause a `paused:<cause>` label (`foreign-hold` or `manual`),
written with `bd set-state`, which also writes a closed event bead under the
bead. The TUI must show paused beads, their cause and reason, and let the human
act on them. The Graph master pane is capped at 48 columns (`masterWidth`). Row
chrome never shrinks (Spec FR2), so any per-row suffix comes out of the
title (~35 columns). The tab bar must fit the 40-column mobile floor. Grilling settled this.

## Decision

1. **Row marker: the status-glyph slot, zero width.** An open paused bead shows
   `‖` (single-width, not the emoji `⏸`) where `○` would be: yellow for
   `foreign-hold` and magenta for `manual`. `⊘` (blocked) and `◐` (in progress)
   still take precedence. A blocked bead isn't a candidate anyway, and a
   pre-pause effort still running shows `‖` once it ends. The colour carries
   the cause on the row, and the detail pane spells it out.
2. **Detail pane line:** `paused  <cause> · <reason> · <actor> <age>`. It reads
   from the newest `paused` event bead under the bead, which is already loaded
   by `bd list --all`, so no extra bd call. If none is found, it shows the
   cause only.
3. **Event beads are not rows.** Beads of type `event` are hidden from the
   Graph everywhere, child counts included. This is a general rule, not a
   pause-only one: event beads are history, not work.
4. **Tab bar:** `· N‖` in the pause colour, shown only when N > 0. There is no
   split by cause, because the rows carry it.
5. **`t` on a paused bead.** On `foreign-hold` it is labelled `retry hold`
   and behaves as run-now does (ADR 0055 §6). On `manual` it is hidden from
   the footer. Pressing it anyway refuses with a hint to press `p` or
   `bd label remove <id> paused:manual`.
6. **`p` toggles a bare manual pause** on any bead that isn't closed:
   - unpaused → `bd set-state <id> paused=manual --reason "paused from TUI"`
   - `paused:manual` → `bd label remove <id> paused:manual`
   - `paused:foreign-hold` → manual, which stops auto-clear (ADR 0055 allows
     this replacement). The footer shows `p pause` here.
   - closed → refused, with the hint "closed beads are never dispatched".
   An in-progress bead or a container can be paused. The status line explains
   that the running effort continues, or that children are unaffected (no
   cascade, ADR 0055 §3). `p` asks no confirmation, because the same key
   reverses it. It writes nothing to the event log, which follows ADR 0055 §8.
7. **There is no lift key for `foreign-hold`.** `t` already retries the cut and
   removes the pause on success. A bare lift would just be re-paused after 3
   skips.

## Considered and rejected

- **A word suffix (`⏸ hold` / `⏸ manual`).** It costs 8 to 10 columns, a third
  of the title, and `⏸` renders 2 columns wide in some terminals.
- **Reading the reason with `bd state` or `bd show` when a bead is selected.**
  That adds a subprocess per selection when the event bead is already loaded.
- **Showing event beads as closed children.** A bead that gets paused often
  fills up with history rows that aren't work.
- **Keeping the toggle in the offshoot Signal.** A paused state shown
  in the TUI with no way to act on it there felt unfinished, and a bare toggle
  is small. Free-text reason entry needs an input widget, and it stays in the
  offshoot.

## Consequences

- When the pause glyph shows, an open bead loses its `○`. The colour and the
  detail pane remain the only places the cause appears.
- Unpausing leaves no event bead, the same as unpausing from the CLI.

## Amendment: `p` yields at the 40-col floor

ADR 0026 never drops bead keys, but a bead with a session (`l resume · x drop
· e edit · L logs │ q`, 39 cols) has no room for `p pause` at the 40-col floor.
There, after the view keys have dropped, `p`'s hint is dropped too; the key
still works. At wider widths `p` stays and the view keys drop right-first as
before, so `r`, `o`, `h` or `space` may leave the footer earlier than they did.

## Amendment: the pause line shows causes only

ADR 0070 §3 writes a pause with `bd update --add-label paused:<cause>`, which
writes no event bead. So §2's `<reason> · <actor> <age>` has nothing to read
from. The pause line now shows the cause or causes only (manual first), and
`PauseNote` and its parsing of the event-bead body go away.

- **No replacement record.** The manual reason was always "paused from TUI",
  which says no more than the cause. The foreign-hold holder and time are
  already in the loop's `effort.paused` line in the Logs tab. Who paused it
  follows from the cause: the human pauses manual, the loop pauses
  foreign-hold.
- **Rejected:** a `bd comments add` beside the label (a second write, plus a
  read per selection, which §2 already rejected); a hand-written event bead
  that copies bd's `set-state` shape; joining the event log in the detail
  pane (it covers foreign-hold only, and manual pauses would need a new
  emit). Revisit only if a pause reason gains a reader.

## Amendment: `p` adds or lifts `manual` only (ADR 0070 §3)

§6's writes follow ADR 0070 §3's additive labels:

- no `paused:manual` → `bd update <id> --add-label paused:manual`
- `paused:manual` → `bd label remove <id> paused:manual`

`manual` no longer replaces `foreign-hold`. On a `foreign-hold` bead `p` adds
`manual` and leaves `foreign-hold` in place. The loop may later lift
`foreign-hold` alone, and the bead stays paused on `manual`. Every check tests
"has `paused:manual`", not equality against one cause, so a bead with both
labels shows magenta `‖`, refuses `t` (§5), and `p` unpauses only `manual`.

## Amendment: `p` on a container pauses its members (ADR 0081)

`p` on a Chart or epic adds `paused:manual`, which now cascades to its members
(ADR 0081). §6's status-line text "children are unaffected" no longer applies
to a container. `t` on an inherited-paused member refuses, with a hint naming
the paused ancestor. How member rows render the inherited pause is left to the
pause-controls Chart.

## Amendment: `abandoned` is orange and refuses `t` (ADR 0082)

§1 gains orange (208) `‖` for `abandoned`, shown once the bead is no longer
`in_progress`. §5: `t` on an abandoned bead is hidden and refuses with a hint
to check the orphan and then `bd label remove <id> paused:abandoned`.

## Amendment: inherited pause on member rows (ADR 0084)

§1: an inherited-paused member shows dim magenta `‖`, ranked below the
member's own causes. §2: the pause line adds `manual via <ancestor-id>`.
§4: `N‖` counts own pauses only. §5: `t` is hidden, and its refusal names the
ancestor. §6: `p`'s status text on a container says its members are out of
dispatch too, or that they resume.

## Amendment: `integrate-conflict` is cyan and `t` retries it (ADR 0104)

§1 gains cyan (44) `‖` for `integrate-conflict`. §5: on an AFK leaf, `t` is
labelled `retry`, and it drops the kept tree first if that tree is clean.
On a HITL leaf, `l` launches through the orphan confirm. Only a successful
`mugicrew integrate` lifts the pause, never `t` returning clean. The detail
pane gains an `effort  effort/<id>` line when the ref exists.
