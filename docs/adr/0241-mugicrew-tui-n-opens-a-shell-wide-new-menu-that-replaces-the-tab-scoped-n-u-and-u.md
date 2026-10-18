# ADR 0241: `n` opens a shell-wide New menu that replaces the tab-scoped `n`, `u` and `U`

Status: Accepted
Date: 2026-10-16

Amends ADR 0144 §2 and §7, ADR 0162 §1–5, ADR 0174 §5, ADR 0181 §5, ADR 0190
§2–3, ADR 0156, ADR 0221 §1, ADR 0228 and ADR 0229 §1, §3 and §5. Spec mc-3ljm.

Each create path had its own key on only some tabs: `n` was New Signal on Graph
and Ideate but New Initiative on Initiatives and unbound elsewhere, `u`/`U` were
Graph-only Surgery, a Discussion was reachable only as the D picker's create row,
and a Jira issue had no TUI path. `n` now opens one **New menu** on every tab and
in the Initiative focus view, so every create path is two keys from anywhere.
ADR 0142's "no two ways" is cited, not amended: the old keys go with no alias.

## Decision

1. **The menu.** A centred picker dialog in the goto menu's per-row-key chrome
   (ADR 0228 §1), marked as the New menu. Rows in fixed order: `s` New Signal, `j`
   New Signal from a Jira issue, `i` New Initiative, `u` New Surgery, `r` New rebase
   request, `d` New Discussion. A row key fires at once; `↑`/`↓` and `enter` also pick. Unlike goto
   it sets a close key, `n`/`esc` (as Recents and Discussions do), because `n` is
   not one of its rows. While it is open no shell or tab key fires.
2. **Shell dispatch.** `n` is a shell key routed right after `g`: after the modal,
   `?` and text-input guards, before the focus view's handler. It is inert in a text
   input, a dialog, another modal and `?`, so a `y/n` confirm's `n` still means
   "no". It is not in the global shell-key group. It always opens (New Signal is
   never hidden). On open the menu captures the selection (bead, its Checkpoint,
   resolved Initiative, or the focus view's Initiative) for the Surgery form's
   default cursor.
3. **Hidden, not refused** (ADR 0228's rule). Row visibility is computed when `n`
   is pressed. `j` is hidden while `[jira] site` is unset (a malformed site counts
   as set, so pressing it reports what is wrong). `u` is hidden only when the
   Surgery target list would be empty: Require Initiatives on, no open Initiative
   and no open Checkpoint. A grandfathered Checkpoint that can still be joined
   keeps the row. `r` is hidden unless the selection captured at open (the
   focus view's cursor row included) is an open Chart or Checkpoint, the rebase
   request's `canRebase` test (ADR 0262).
4. **`j` shadows the picker's `j`-move.** Row keys match before the picker's `j`/`k`
   move, so the Jira row wins. The menu's bindings list the row keys, the close key
   and a move binding whose help reads `↑/↓ move`, never `j/k move`, so hints and
   binding coverage are right whether or not the row shows.
5. **Existing forms unchanged.** `s`, `i` and `d` open the existing New Signal form
   (never scoped to a focused Initiative), the New Initiative form (with its
   external-ref field under Enforce external ID) and a new Discussion as the D
   picker's `+ New discussion` row does. `D` stays the Discussions picker.
6. **Surgery.** `u` opens the one New Surgery form recorded by ADR 0234, which
   replaces Graph's `u` and `U`, the title/problem form and the fresh-Surgery
   Initiative picker. Its default cursor, filing calls, refusals and message slot
   are ADR 0234's. Refusal and failure texts lose their `u:`/`U:` prefixes.
7. **Jira dialog.** A one-field key dialog: `enter` submits, `esc` cancels, an empty
   key is refused in the form without calling acli. The key is trimmed and
   uppercased, then intake (the core moved into package `jira`, shared with
   `mugicrew intake jira`) runs off the event loop through `startWrite` on its own
   pending slot with `filing <key>…` on the status line (ADR 0098); a second press
   in flight is refused with the pending refusal. Success reports
   `filed <id> from <KEY>`, with `(<typed> is now <KEY>)` on a rename and
   `, superseded onto <carrier>` on a supersede; the cursor does not move. A
   failure before filing reopens the dialog holding the key and the error. The one
   partial failure (filed, supersede failed) goes to the status line and does not
   reopen, so a retry cannot file a third carrier.
8. **ADR 0107 §1 exception.** ADR 0107 §1 keeps create feedback in the form head.
   The Jira dialog closes on submit and reports on the status line, because no
   form should stay open over acli's 1.5–2.5 s read.
9. **Tab keys removed.** The tab-scoped `n` (Graph, Ideate, Initiatives) and Graph's
   `u`/`U` and `r` (rebase) are gone with no alias; the rebase request is the New
   menu's `r` row, pinned to the selection captured at open, which re-checks the
   pending refusal on submit and keeps the outcome flashes. Ideate's `enter` on the `+ New Signal` row still
   opens the New Signal form: that is the tab's own primary action on its create
   row, not an alias of `n`.
10. **Hints and reference.** `n new` is pinned in the nav section beside `g goto` on
    every tab and in the focus view, never twice (nav order `g`, `n`, `space`, `d`,
    `esc`); `u surgery`, `U fresh surgery` and `r rebase` leave Graph's hints. `?` appends an
    `n new ›` group of the menu's options beside `g goto ›`, before global. The
    empty Initiatives line reads `no Initiatives yet — n i to start one`, and
    Luffy's Approval prompt points a human at the Surgery row (`n u`).

## Amendments

- **ADR 0144 §2, §7 and its Graph hint row.** `n` and `u` are gone from Graph's
  keys and hints. §7's "Shell keys stay out of the bar" now has a second exception
  beside `g goto`: `n new`.
- **ADR 0162 §1–5.** `u`/`U` and the title/problem form become the New menu's
  Surgery form. §6–8 stand.
- **ADR 0174 §5.** In the focus view `n` opens the New menu; `a`/`A` stay unbound.
- **ADR 0181 §5** and **ADR 0190 §2–3.** The Initiatives tab's `n` is the shell
  menu's `n i`; the empty line becomes `no Initiatives yet — n i to start one`.
- **ADR 0156.** Defog's "`n` is not bound" no longer holds: `n` opens the menu.
- **ADR 0221 §1.** The `U` form's Initiative picker is now the Surgery form's
  target list, where `+ New standalone Checkpoint` is hidden under Require
  Initiatives.
- **ADR 0228.** Its ADR 0144 §7 exception note ("one exception: `g goto`") now names
  two exceptions.
- **ADR 0229 §1, §3, §5.** §1: `n new` in nav, `u`/`U` gone. §3: `n new` pinned,
  `u`/`U` ranks gone. §5: the `n new ›` group.

Wording notes, not amendments: ADR 0149, ADR 0171, ADR 0172, ADR 0178 and ADR 0180
(Ideate hint rows ending `n new`), ADR 0164 and ADR 0165 (Graph/Ideate hint
strings) and ADR 0148 ("Idea's and Graph's `n`") name keys that now read as the
menu's rows.
