# ADR 0094: mugicrew Surgery review — the Surgery bead stands in for the Spec, Critiques go AFK, no pivots

Status: Accepted
Date: 2026-10-07

Amends: crew doc code-review.md's Spec axis (a non-Spec intent source), ADR
0043 §1 (a raise checks for the Spec before it publishes the gate). Builds on
ADR 0091, ADR 0093.

## Context

A surgery Checkpoint has no Spec (ADR 0091), so `SpecOf` finds nothing and
today's review covers Standards only. Raising a pivot there publishes the
Checkpoint gate and then errors, which leaves the Checkpoint frozen. The
only statement of intent is the Surgery bead: its description (title plus the
problem statement) and one comment per session (ADR 0093 §2). Decided in
grilling.

## Decision

1. **The Spec axis reviews against the Surgery bead.** On a surgery
   Checkpoint, Usopp's Spec axis reads the Surgery's description and comments
   as its intent source. It reports only (a) a change the record states that
   is missing or partial, and (c) a stated change that is implemented wrong.
   It **drops the scope-creep check**, because the human authorized every
   change. Re-reviews after Critique rework use the same source.
2. **Critiques go to Franky AFK, as on any Checkpoint.** A Critique is a
   defect and needs no dictation. Franky's Critique (and Conflict) prompt gets
   the Surgery bead as context in place of the empty Spec. A human who
   disagrees with the outcome rejects at Approval and may file a new surgery.
   The Surgery is never reopened.
3. **Pivots are impossible, enforced twice.** On a Spec-less Checkpoint, the
   prompts for Usopp's review and Franky's Critique and Conflict runs leave out
   the pivot section. `RaisePivot` checks for the Spec **before** it publishes
   the Checkpoint gate. A stray pivot then errors without freezing anything,
   and the raising bead's failure pause brings in the human. This retires the
   `ponytail:` ceiling at `bd/pivot.go:85-87`. If the reviewer thinks the
   human's intent itself is wrong, that becomes a Critique, and the human
   settles it at Approval.
4. **Luffy's Approval packet names the Surgery** in one line,
   `Surgery: <id> — <title>`, so an approver who did not dictate the code, or
   who comes back days later, can read the intent.

## Considered Options

- **Standards-only review (today's headless behaviour).** Rejected: nothing
  checks that the diff does what the human asked.
- **The full Spec axis against the Surgery bead.** Rejected: scope-creep
  findings against code the human dictated are noise.
- **Critiques back to the human in a new surgery session.** Rejected: that
  brings back the ceremony a surgery exists to skip. Approval is already the
  human gate.
- **Pivots blocked only by the prompts, or only in Go.** Rejected: prompts
  alone leave the freeze-then-error path open, and Go alone lets the model
  keep trying.
