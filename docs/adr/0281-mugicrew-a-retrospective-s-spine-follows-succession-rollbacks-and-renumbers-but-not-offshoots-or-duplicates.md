# A retrospective's spine follows succession, Rollbacks and Renumbers, but not offshoots or duplicates

A retrospective targets a spine only when none of its beads is open, and it reads
every session on that spine. So the edges that make up a spine decide two things:
the refusal "<id>'s spine still has open beads" and the transcript set. The edge
closure is in `docs/research/bead-spine-edges-and-press-time-open-check.md` §3:
`caused-by` and `parent-child` with Initiative ends excluded, plus `related` and
`discovered-from` only from a Rollback or Pivot. This ADR settles the eight
ambiguous cases in that document's §4. Each choice keeps a retrospective on finished work from being
held open by work that is not part of the spine, while keeping every bead that
is part of it. Settled by the grilling mc-4ryp.6.10.

1. **Offshoot Signals are out**, for both the open check and the transcript set.
   An offshoot is not succession (ADR 0267
   (mugicrew-an-offshoot-signal-is-filed-through…)). Otherwise an open P3
   follow-up would hold a finished spine indefinitely, and the walk would chain
   into the offshoot's own Chart and Spec.
2. **Duplicate Signals (`supersedes`) are out.** A duplicate's triage session is
   about how triage spotted the duplicate, not about how the spine's work went.
   Pivot re-plans stay connected through `caused-by`, so ignoring `supersedes`
   loses no spine bead.
3. **A Rollback and its successor Checkpoint are in.** A Rollback filed after a
   retrospective reopens the spine. `r` refuses until the successor lands, and a
   later retrospective re-reads the whole spine, including the original Build
   next to its breakage.
4. **A Renumber Checkpoint is in.** An open Renumber holds the retrospective.
5. **A surgery Checkpoint is a target.** Its spine is the Checkpoint and its
   members (the Surgery, Review, Landing). A Surgery that joins an existing
   Checkpoint is just a member of that spine.
6. **An Initiative's own sessions** are those of the Initiative bead (its cut
   sessions) and of its `caused-by` Signal, when it has one. Both are in the
   open check as well.
7. **Task members of an Initiative are in.** An open Task member holds the
   Initiative's retrospective, which matches the rule that the Initiative is not
   drained while one is open. A Task with no session adds nothing to the set.
8. **Adopted chains' closed predecessors are in.** An Initiative's spines are
   the closures of its direct members. A chain adopted mid-way brings its
   predecessors, which stayed outside the Initiative. They are closed, so they
   never hold the check.

## Considered Options

- **Offshoots in at one hop, status ignored, or only when ordered
  `prerequisite`.** Rejected: an offshoot is separate work with its own spine,
  and it can be targeted on its own.
- **Duplicates in for the transcript set only.** Rejected: it adds sessions that
  are about triage of a different Signal, for no pattern on this spine.
- **Cut the spine at a Rollback**, making the Rollback and its successor a spine
  of their own. Rejected: the breakage is most useful read next to the work it
  broke.
