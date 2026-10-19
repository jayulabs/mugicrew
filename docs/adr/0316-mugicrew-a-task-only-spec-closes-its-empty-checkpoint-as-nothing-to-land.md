# ADR 0316: A Task-only Spec closes its empty Checkpoint as nothing to land

Status: Accepted
Date: 2026-10-19

Spec mc-k1uj1. Amends the FR22 close gate of ADR 0015
(mugicrew-zoro-slicing-prompt), and widens ADR 0002 (mugicrew-bead-taxonomy).

ADR 0015 let reconcileSpec close a Spec only once its slicing laid at least one
Build. A Spec whose slicing laid only Tasks (work outside the code) therefore
never drained: it errored, or sat open beside a Checkpoint with nothing to land.

## Decision

1. **The close gate is "≥1 Build or ≥1 Task, else the run errors".** A slicing
   that lays neither still fails loud and leaves the Spec open.
2. **The Task-only arm.** With no Builds and at least one Task, reconcileSpec, in
   order: carries the Spec's ordering edges onto the Checkpoint; closes the
   Checkpoint directly (not via the publish) with the close reason
   `nothing to land:` (`bd.NothingToLandReason`) and a tail naming the Spec;
   deletes `checkpoint/<id>` in the Spec's repo when its tip is on mainline
   (a missing branch is skipped, one with work or held by a worktree is kept,
   and the outcome is recorded without failing the pass; no land lock is taken);
   and hands the Spec's close to the publish. A Checkpoint already closed
   nothing-to-land is a roll-forward: no cut, no fork, no second close. Any
   other closed Checkpoint is a loud error, as on the Builds arm.
3. **The reason is not `obsolete:`.** That prefix drives retireStragglers'
   cascade and the backstop's swept-parent handling; a nothing-to-land
   Checkpoint is complete, not swept.
4. **`discovered-from` Task → Spec is the slicing provenance and dedupe edge.**
   A Spec holding Tasks over that edge is never re-forked, since a re-fork would
   republish them. This widens ADR 0002, which gave `discovered-from` no such
   use for a Task.
5. **The `before: land` of ADR 0113 (mugicrew-task-production-and-ordering-against-checkpoint-lands)
   §4 does not apply to a Spec with no Builds.** Its Checkpoint never lands, so
   the Tasks it filed are released by the Checkpoint's close.
6. **Re-entry re-counts the Spec's Tasks.** With the Checkpoint already closed
   nothing-to-land, the cut and the Checkpoint close are skipped, but a Spec with
   zero Tasks (a failed publish deleted the Drafts) is re-forked: its `after:
   land` edges release at once on the closed Checkpoint. Still zero Tasks leaves
   the Spec open; any Build is a loud error; otherwise the Spec's close is handed
   to the publish.
