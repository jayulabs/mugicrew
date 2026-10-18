# A Checkup notes every bead its repair changed, as the last step of the finding's plan

Status: Accepted
Amends: ADR 0272 (mugicrew-a-chopper-checkup-is-a-hitl-kind…) §5; ADR 0273 (mugicrew-a-checkup-writes-only-through-bd…) §2, §4
Date: 2026-10-17

Chart mc-qcor ("A HITL Chopper session repairs a broken bead on a human's
command"), grilling bead mc-qcor.9. ADR 0272 §5 puts a Checkup's record on the
Checkup bead and left open whether the target also gets a note. The next reader
of a repaired bead (a crewmate on a re-dispatched Build, or the human later)
reads that bead, not the Checkup. A label that quietly vanished, as
`paused:not-built` did in the motivating case, needs a reason on the bead
itself. The `related` edge names the Checkup but not what changed.

## Decision

1. **Always, when the bead changed.** Every bead whose own fields, labels or
   status a finding's plan changes gets one note. A bead that only gains an
   edge does not, since the edge shows on both ends. A Chart body is edited only
   through `mugicrew chart append|drop`, which needs no note.
2. **A note, not a comment.** `bd update <bead> --append-notes "…"`. Notes
   are what `bd show` and crewmate prompts print.
3. **Last step of the finding's plan.** The note command closes each finding's
   ordered plan, under that finding's one yes (ADR 0273 §4). It runs only if
   every earlier step succeeded, and the human sees its exact text before
   saying yes. Nothing is written at close.
4. **One fixed line.** `Checkup <checkup-id>: <what changed>. <why, one
   clause>.` The diagnosis stays on the Checkup bead.
5. **No repair, no note.** A Checkup that closes as `not a bead fault: …` or
   `dropped` writes nothing on its target.

## Considered Options

- **Optional, on the human's ask** (ADR 0273 §2 already allows it). Rejected:
  with no default it gets forgotten.
- **Never; the `related` edge is enough.** Rejected: a reader has to open a
  second bead to learn that a change was deliberate.
- **One summary note at close.** Rejected: no yes covers it, and an early
  `/exit` that never resumes leaves landed repairs unrecorded.
- **A bd comment.** Rejected: nothing in mugicrew writes comments, and
  crewmates don't read them.
