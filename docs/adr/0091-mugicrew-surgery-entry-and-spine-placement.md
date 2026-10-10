# ADR 0091: mugicrew Surgery — a human-filed, Spec-less Checkpoint cut at main HEAD

Status: Accepted
Date: 2026-10-07

Amends: ADR 0047 Consequences (a third front door), ADR 0014 (a Checkpoint cut
with no Spec anchor).

## Context

A Surgery is a HITL session in which a human dictates small code changes (TUI
tweaks, minor bug fixes) and then hands them to the usual review tail (the
surgery Chart). Its purpose is to skip ceremony. Research found that
only Zoro cuts a Checkpoint, only from a Spec, and that pivots break on a
Checkpoint with no Spec. This ADR settles how a surgery enters the graph and
where it sits on the spine. Decided in grilling.

## Decision

1. **A human files `kind:surgery` directly**, from the TUI or CLI, with no
   Signal and no triage. It is a third front door, after Signal and Rollback.
   It clears ADR 0047's bar because the human is both the author and present,
   so intake has nothing to decide. Agents never file one; they may file an
   offshoot Signal suggesting one.
2. **Every surgery gets a new Checkpoint.** Surgery on an existing, Spec'd
   Checkpoint is out of scope: Critiques and pivots fix in-flight work.
3. **The Surgery is a member of its Checkpoint**, the Checkpoint's only
   authoring member, as a Build would be. It is not the Checkpoint's
   predecessor. The Checkpoint must exist while the surgery runs, because the
   surgery commits to its branch.
4. **No Spec and no predecessor.** A surgery Checkpoint is caused by nothing.
   On the spine it jumps straight to a Checkpoint, and `SpecOf` returns
   not-found. The Surgery's running summary is the only statement of intent,
   and pivots must not be possible on a surgery Checkpoint.
5. **The cut happens at filing, in one step.** Filing creates the Checkpoint
   and its Surgery member and cuts `checkpoint/<id>` from **main HEAD**, with
   no anchor commit. It then launches the first HITL session on that branch,
   with no AFK hop in between. The branch's merge-base is all the record of
   the base anyone needs; Jinbe rebases onto current main at land anyway.
6. **The human gives a title** (shared by both beads) and an optional one-line
   problem statement. Everything else accrues in the running summary.

Out of this ADR: the Surgery kind's owner, how a surgery pauses, resumes and
ends, and how review treats its changes.

## Considered Options

- **A fifth Signal-triage outcome.** Rejected: triage is the ceremony a
  surgery exists to skip.
- **A Spec written at the end from the summary, or a direct Spec plus a
  `Spec:` anchor up front.** Rejected: the Spec would describe the very code
  it reviews. Pivot confirm also needs a Spec caused by a Chart or Signal, so
  pivots would still break.
- **The Surgery as the Checkpoint's predecessor.** Rejected: there would be no
  branch to commit to during the surgery, and Carry would fire a stage early.
- **A `Surgery:` anchor commit on main as the base.** Rejected: it records only
  what the merge-base already gives.
