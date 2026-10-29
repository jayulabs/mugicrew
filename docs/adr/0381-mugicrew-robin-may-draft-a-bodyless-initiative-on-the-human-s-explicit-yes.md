# ADR 0381: mugicrew Robin may draft a bodyless Initiative on the human's explicit yes

Status: Accepted
Date: 2026-10-28
Amends: ADR 0132 (initiative-born-at-triage…) §1; ADR 0190 (an-initiative-may-be-started-from-the-initiatives-tab…) §1; ADR 0194 (a-robin-discussion-is-an-off-spine-discussion-bead), Robin's one creatable kind.

## Context

A Robin discussion can reach agreement on a goal-state Initiative and its seed
text. Robin could only draft Signals, so the human re-entered the agreed text by
hand through `n i` on the Initiatives tab. The Chart "Robin creates one
Initiative from a discussion on the human's explicit yes" settled how she may
create one without taking Luffy's job of writing the body and cutting phases.

## Decision

1. **A third way in.** Besides triage (ADR 0132 §1) and the Initiatives tab
   (ADR 0190), Robin creates an Initiative, one per explicit yes from the human,
   with no cap per discussion.
2. **It is a Draft** (`-s deferred`, `drafted-by:<discussion>`), unlike a
   TUI-born Initiative (amends ADR 0190 §1's "no Draft stage" for this path
   only). Her wrapper publishes it at clean exit with her Signals. `bd.Publish`
   is already kind-agnostic. Dropping the discussion deletes it, and her
   edit/merge/discard rule for her own Drafts covers it.
3. **The body is a headingless seed.** Robin writes the proposed Done when,
   Phases and Notes as labelled prose with no `## Done when` heading. So
   `bd.HasDoneWhen` is false, the Initiative shows `no body yet`, and Luffy's
   first cut session settles the real body with the human (ADR 0182). Luffy
   stays the body's only writer.
4. **No adoption.** Robin adopts no existing Signal into it, including Signal
   Drafts from the same discussion: adoption stays an explicit human act
   (ADR 0128 §5), done in Luffy's cut session. She files no member Signals and
   cuts no phases. She names candidate Signals by title in the seed.
5. **Provenance:** one edge, `discovered-from:<discussion>`, as for her Signals
   (ADR 0194 §6). A retrospective spine does not follow it (ADR 0281).
6. **bd's default priority**; the human or Luffy changes it later. Created by
   `bd create` in Bash, it logs no `initiative.new` event; that is accepted.

## Considered Options

- **A real three-section body.** Rejected: the Initiative would skip Luffy's
  first-body branch, and Robin would become a second body writer.
- **Open at once, as from the tab.** Rejected: a discussion that is dropped
  would leave the Initiative behind, and publishing would no longer be the wrapper's job.
- **Robin adopts her own same-discussion Signal Drafts.** Rejected: it would grow the
  Initiative without the human's adoption act.
- **No edge, like a tab-born Initiative.** Rejected: nothing would point back to
  the discussion that agreed it.
- **The wrapper logs `initiative.new` on publish.** Rejected as extra machinery
  for one event.
