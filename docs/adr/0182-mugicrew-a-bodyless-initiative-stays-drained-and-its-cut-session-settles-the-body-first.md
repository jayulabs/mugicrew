# ADR 0182: A bodyless Initiative stays drained, and its cut session settles the body first

Status: Accepted; §5 "v2 ON LAUNCH" row amended by ADR 0238 (the ON LAUNCH preview line is removed)
Date: 2026-10-14

Amends ADR 0181 §3 and §4. Chart mc-8xq5 (Start an Initiative from the
Initiatives tab). This ADR settles grilling mc-8xq5.4, using the research in
mc-8xq5.3 (`docs/research/cut-session-first-body-branch-and-adoption-filter-seams.md`).

An Initiative born from the Initiatives tab carries only its seed text: its
body has no `## Done when` heading. We call it **bodyless**. It has no members,
so it is drained at once and its cut session can run. That session must first
write the body, and the TUI must not describe the Initiative as "all members
closed". We keep the lifecycle unchanged. Bodyless is a qualifier computed at
render from the body. It is not a new Initiative state.

## Decision

1. **No new lifecycle state.** `bd.StateOf` stays closed → drained → under
   way. That lifecycle is about members; having a body is a separate axis.
   A one-line predicate in `bd` (the same `strings.Cut` on `## Done when` as
   luffy's `doneWhen`) tells a bodyless Initiative apart. `luffy` and `v2` both
   call it, and `v2` still does not import `luffy`.
2. **The first-body branch.** When the body lacks the heading, the cut
   session works in this order:
   - Luffy settles **Done when**, then **Phases** (coarse lines), then
     **Notes**, with the human.
   - Luffy distils the seed into those three sections. Whatever fits neither
     Done when nor Phases becomes a Notes line. The verbatim seed is not kept;
     bd's history has it.
   - Luffy writes the body to the bead **before** filing any member Signal.
   - Luffy then offers phase 1 as an ordinary CUT and files it only on the
     human's yes. The human may stop once the body is written. The Initiative
     stays drained, and its next launch is an ordinary cut.
3. **Whole-description writes.** `cut.md` drops "edit it one line at a time".
   Luffy is the body's only writer. It re-reads the body, then writes it with
   one `bd update --description`, for both the first body and later cuts.
4. **Launch reads the body.** `Luffy.Launch` reads the Initiative through
   `show`. If the read fails, the launch fails. The session never defaults to
   either branch.
5. **The TUI cue is `no body yet`.** Every surface that shows a drained
   Initiative keys on the predicate:

   | Surface | Bodyless wording |
   |---|---|
   | Ideate combined row | `· no body yet` replaces `· all members closed` |
   | Ideate tree header | `· drained · no body yet` |
   | Ideate cue row | `no body yet · enter: Luffy settles its body with you, then cuts phase 1` |
   | Initiatives row line 1 | muted `no body yet` in the `cancelled` slot (amends ADR 0181 §3) |
   | Initiatives row line 2 | `no body yet · its cut session settles Done when and Phases first`, in place of the counts until a member exists (amends ADR 0181 §4) |
   | Focus header | `· no body yet` suffix |
   | v2 ON LAUNCH | `… opens a cut session for initiative — no body yet: settles it first` |

   Unchanged: the glyph, the hints bars, the focus back bar, and the detail
   fields (`landed 0/0`, and the raw seed shown as the body). The v1 TUI gets
   the branch through `Luffy.Launch` but no cue.

## Considered Options

- **A `seeded` lifecycle state, checked before drained.** Rejected. Every
  `== InitiativeDrained` caller (the Ideate row, `launchable()`, the trailers)
  would have to learn a fourth value, or a seeded Initiative would drop off
  Ideate. It would also mix body and members in one lifecycle. The one case it
  would surface, an Initiative with a member and no heading, is reachable only
  by deleting the heading in Graph's edit. The adoption filter guards that
  case, and the next cut session repairs it.
- **Keep the seed verbatim** as a Notes line or a `## Seed` section. Rejected.
  It would break the three-section body that cut.md and triage assume.
- **Always cut phase 1 with the body**, or **never in the same session**.
  Rejected. Offering it costs nothing, because the ordinary cut path already
  handles "body, no members".
- **A `mugicrew initiative append|drop` line verb.** Rejected for now. A line
  verb guards against concurrent writers, and the body has only one automated
  writer.
- **Default to a branch when the body read fails.** Rejected. That would run
  the wrong prompt silently.
