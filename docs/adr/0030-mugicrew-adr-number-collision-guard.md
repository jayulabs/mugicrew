# ADR 0030: mugicrew ADR-number collision guard — a reused number is a chart-conflict, resolved in a HITL session

Status: Accepted
Date: 2026-09-29

Amends: ADR 0011 §3 (chart-conflict resolves as a HITL agent session, not a
checklist) and ADR 0011's premise that Chart collisions surface as rebase
conflicts.

## Context

Concurrent Charts branch from the same mainline base and number new ADRs
sequentially upward, independently — so two Charts claim the same next integer.
ADR 0011 assumed an ADR-number collision shows up as a rebase conflict. It does
not: `0016-promote-or-stamp` vs `0016-checkpoint-jinbe-land` are different
paths, so `git.Land` rebases cleanly and silently lands a second 0016. Mainline
already carries three such pairs (0023, 0024, 0026). Surfaced as a Signal
and specified in a Spec.

## Decision

1. **Keep sequential integers; detect at land.** No reservation at chart-cut, no
   collision-free ids. Before `git.Land`, `reconcileChart` checks every ADR number
   the Chart *adds*: one that already exists on mainline under any filename stops
   the land and files a `chart-conflict` (ADR 0011 §1–2, same open-child guard).
   Only added numbers are checked — pre-existing mainline duplicates never block.
   > **Amended by ADR 0225:** a post-land check also renumbers a Forge-merged
   > duplicate of a number the just-landed container added; pre-existing
   > duplicates still never block.
2. **Chart-only.** ADRs are written only by charting, so the Checkpoint land path
   is not guarded. Revisit if builds start writing ADRs.
   > **Amended by ADR 0069:** the clash check now runs inside Land, under the land
   > lock, against the exact base, for Charts and Checkpoints alike. A Checkpoint
   > clash files Jinbe's Franky-owned conflict member with a clash-variant body.
3. **chart-conflict is one kind, resolved as a HITL agent session** — both the
   textual rebase conflict and the ADR-number clash. The checklist variety is
   dropped: renumbering plus a reference sweep is too fiddly to hand-run (the
   escape hatch ADR 0011's rejected alternative left open).
4. **The landing Chart moves; mainline is fixed.** Its colliding ADRs shift to the
   next free numbers above mainline's max, keeping their relative order.
   > **Amended by ADR 0225:** the later-landed container moves, including after
   > its land.
5. **Reference sweep is scoped to what the Chart owns:** files the Chart changed
   (diff vs merge-base) and the Chart's own bead subtree (body + defog beads).
   Files that came only from mainline, and beads outside the Chart, mean
   *mainline's* ADR by that number and are never rewritten.

## Consequences

- A reused number becomes loud instead of a silent duplicate land.
- The chart-conflict session gains a renumber-and-sweep prompt; no new kind.
- Existing mainline duplicates are a one-off cleanup (later-landed file moves).
- ~~**Known ceiling:** a Checkpoint that adds an ADR is unguarded.~~ Lifted by ADR 0069.

## Alternatives considered

- **Git pre-commit/pre-merge hook.** Rejected: a hook can only block; it cannot
  file a bead, so the loop would stall on a bare error.
- **Allocate the number at land time (draft filenames on the branch).** Rejected
  for now: every "ADR NNNN" reference would be rewritten on every land, not only
  on a clash.
- **Collision-free ids (date/bead-id slugs).** Rejected: breaks the numbering
  convention and ~30 ADRs' cross-references for a rare, now-detectable event.
- **A separate `adr-collision` kind.** Rejected: owner and flow are identical to
  chart-conflict; a variant needs no new routing key.
