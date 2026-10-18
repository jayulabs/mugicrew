# ADR 0265: the TUI bead drawers end with one shared body and drop the closed and AFK preview lines

Status: Accepted
Date: 2026-10-17

Amends: ADR 0238 (mugicrew-tui-the-detail-drawers-drop-the-on-launch-preview-line) §3. Spec mc-qqr2.

## Context

Each drawer built its own body: header fields, then the description. Notes,
design and acceptance criteria were in the `bd list --json` snapshot but dropped
by the parser. Graph hid bd's default close reason, Defog and Exec printed their
own closed field, Graph added a "closed — kept for lineage." preview, and every
AFK bead ended with a launch-preview line repeating the hints bar's `enter`.

## Decision

1. **One shared body**, `graphData.beadBody`, ends every bead drawer: Graph, the
   F10 bead pane, Initiatives, Focus, Ideate, Exec and Defog, Initiative head
   rows included. The multi-bead summary rows (Defog `dSum`, Exec `eSum`) and the
   Logs tab are out. Order: close line, DESCRIPTION, NOTES, DESIGN, ACCEPTANCE,
   then COMMENTS (N), N from the snapshot's `comment_count`. Each section is a
   full-width `── LABEL ───` rule in the label colour over Markdown, and is left
   out when empty.
2. **The close line** `closed <age since closing> · <reason>` leads the body of
   every closed bead, in field style, with bd's default reason "Closed" shown.
3. **The per-tab closed/reason fields and Graph's lineage preview are gone.**
4. **Both AFK preview forms are gone** ("AFK — … Use edit to intervene." and "…
   Press enter to <x> it now, or edit to intervene."). A closed bead with no
   session, and an open AFK bead, end at the body.
5. ADR 0238 §3's list of remaining lines loses "closed" and "AFK". SESSION,
   running/dead, idle, claim-no-session, CHECKPOINT and CONTAINER stay.

6. **Comments** are cached on the Model by `(id, comment_count)`, so a new comment
   misses and refetches. `syncComments` fetches in a Cmd (`listComments`) only for
   the open drawer's bead. A miss or in-flight fetch renders `loading…`; a failed
   fetch renders `couldn't load comments: <err>`, and `f5` drops the error entries
   so they refetch. The cache is never pruned: one entry per bead viewed per
   count, a few KB a session.

ADR 0101 (mugicrew-tui-render-runs-no-subprocess) §1 and §4 still hold: comments
are fetched in a Cmd, and `beadBody` renders from the snapshot and that cache
only, never calling bd.
