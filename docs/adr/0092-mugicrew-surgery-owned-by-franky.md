# ADR 0092: mugicrew Surgery — owned by Franky, a HITL leaf the human dictates and Franky writes

Status: Accepted
Date: 2026-10-07

Amends: the glossary's Checkpoint owner line (a third creator), Franky's
all-AFK kind set.

## Context

ADR 0091 settled how a Surgery enters the graph: a human files it, and filing
cuts a new Spec-less Checkpoint at main HEAD with the Surgery as its only
authoring member. The Kind-ownership invariant still needs exactly one owner
for `kind:surgery`, a structure, and the stance the session runs with, given
the crew rule "HITL is authorization, not authorship" (surgery Chart).
Decided in grilling.

## Decision

1. **Franky owns `kind:surgery`.** The session writes code onto a Checkpoint
   branch, where a Build would sit, and that is Franky's work. It is
   Franky's first HITL kind: the registry marks it HITL, the AFK loop never
   dispatches it, and his Launch serves it.
2. **A leaf**, the only authoring member of its surgery Checkpoint. Several
   sessions share the one bead; nothing hangs off it.
3. **Franky creates the surgery Checkpoint** at filing, as Jinbe creates a
   successor Checkpoint for a Rollback. Zoro stays the Checkpoint kind's
   steward; Jinbe still handles and lands it.
4. **The human directs, Franky writes.** Every edit and commit is Franky's,
   from the human's dictation. If the human hand-edits the worktree, Franky
   folds it into his next commit. "HITL is authorization, not authorship"
   holds as written: no preamble exception.
5. **The session prompt:** read the Workspace's CONTRIBUTING.md and README.md;
   commit after each change the human accepts (a clean exit deletes the
   worktree, and Jinbe squashes at land anyway); run the Workspace's build and
   tests before each commit and commit only on green unless the human says
   otherwise; when a dictated change looks too big for a surgery, say so once
   and offer an offshoot Signal, then follow the human.

Out of this ADR: the running summary, pausing and ending, and how
review treats the changes.

## Considered Options

- **Zoro.** Rejected: he is Checkpoint steward but writes no code; owning a
  coding session would drift his role.
- **Nami.** Rejected: she runs HITL interviews, but a surgery codes, it does
  not defog.
- **A new crewmate (Chopper).** Rejected: a new package for one kind, and the
  name is earmarked for the external monitor.
- **A container with a child bead per session.** Rejected: the persisting
  claim and appended notes already carry several sessions.
- **The human hand-writes, the agent assists.** Rejected: it would break the
  preamble's authorship line for one kind.
- **Zoro performs the cut for Franky.** Rejected: it gives Zoro a Spec-less
  path he otherwise never needs; the Rollback precedent already covers it.
- **Commit at the end, or only on request.** Rejected: a clean exit loses
  uncommitted work.
