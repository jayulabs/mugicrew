# ADR 0163: mugicrew a Checkpoint takes many Surgeries, one open at a time

Status: Accepted
Date: 2026-10-13

Amends: ADR 0091 §2–3 (every Surgery gets a new Checkpoint; it is the only
member), ADR 0092 §2 (the only authoring member), ADR 0093 §5 (a zero-commit
end closes the Checkpoint), ADR 0094 §1 (review against the one Surgery bead; no Spec
on the Checkpoint) and §4 (the single Surgery packet line, already amended by
ADR 0162 §8). Builds on ADR 0160 and ADR 0162. Decided in
grilling (Chart "Surgery on an existing Checkpoint").

## Context

Each Surgery used to create its own Checkpoint and be its only authoring
member, so the code read "the Surgery of a Checkpoint" as a single bead. A
Surgery can now join an open Checkpoint (ADR 0162), so those reads and the end
rule had to change.

## Decision

1. **A Checkpoint takes many Surgeries over its life, at most one open at a
   time.** `u` joins the selected Checkpoint; `U` files a fresh one (ADR 0162).
2. **Eligibility.** A Surgery may join unless a Landing is live, a pivot gate is
   open, a Conflict or Base+Tip Critique is open (it is rewriting the branch), a
   Surgery is already open, or the Checkpoint is closed. In-flight Builds and
   squash-mode Critiques do not block it.
3. **End rule.** Ending a Surgery integrates first, then counts only the
   squashes carrying its own `Surgery: <id>` trailer. With none, the Surgery
   closes abandoned. The Checkpoint closes with it only when nothing is ahead
   of main and the Surgery is the Checkpoint's only child, i.e. the Surgery
   created it. With some, it hands off to the review tail as before.
4. **Plural review.** On a Spec'd Checkpoint the Spec stays the review intent.
   Each Surgery's trailered commits are checked against that Surgery bead, with
   no scope-creep findings. A surgery Checkpoint reviews against its Surgery
   beads in place of a Spec.
5. **Pivots stay Spec-keyed.** Pivot suppression follows the presence of a
   Spec, not of a Surgery: a Spec'd Checkpoint can still raise a pivot, a
   surgery Checkpoint cannot.

## Considered Options

- **Write a "created it" marker at filing.** Rejected: the end rule derives it
  (nothing ahead of main, only child).
- **Close the Checkpoint on any zero-commit end.** Rejected: it would discard
  other members' work.
