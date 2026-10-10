# ADR 0047: mugicrew Rollback — a human-filed kind that Jinbe reverts in pure Go

Status: Accepted
Date: 2026-10-04

Amends: ADR 0002 (adds a kind; Jinbe owns a leaf kind), ADR 0006 / ADR 0035 §13
(Signal is no longer the only kind that enters the graph from outside).

## Context

A Checkpoint can land and still break main (ADR 0004 §5: a clean rebase is not a
correct one). The Chart settled at triage that the undo is a forward
**revert commit**, that main is never rewound, and that a landed Checkpoint is
never reopened: recovery is a successor. The inventory
found that a revert
leaves `LandedBase` still finding the old `Checkpoint:` trailer, so the
successor must re-land under a new id. This ADR settles the entry point and the
owner. Decided in grilling.

## Decision

1. **New leaf kind `kind:rollback`.** "Rollback" is the bead and the whole
   operation (undo a landed Checkpoint and recover its work). "Revert commit" is
   only the git mechanism on main.
2. **A human files it directly.** No Signal and no triage: the target is already
   known, so intake adds only overhead. Rollback joins Signal as a kind that
   enters from outside. Agents never file one. A crewmate that sees main broken
   may file an ordinary offshoot Signal suggesting it.
3. **One Rollback, one Checkpoint.** It is unparented, with one `related` edge
   Rollback → target Checkpoint, which Go reads to find the target. Several bad
   lands are several Rollbacks, reverted newest first.
4. **Always a forward revert, even at HEAD.** At HEAD the revert simply cannot
   conflict. `main` never moves backwards.
5. **Jinbe owns it; the revert is pure Go and AFK.** The human's filing is the
   decision. Jinbe's Go reverts the Checkpoint's squash commit through the git
   seam (ADR 0010) and adds a `Reverts-Checkpoint: <id>` trailer. Dispatch is the
   loop, or a TUI `t` when the loop is off.
6. **Invalid and repeated targets write nothing.** A target that is not landed
   (open bead, or no `Checkpoint:` trailer on main) closes the Rollback with
   reason `invalid: <why>`. A target already carrying a `Reverts-Checkpoint:`
   trailer on main skips the revert and continues to the successor handoff, the
   same path as resuming after a crash.

Out of this ADR: the successor's shape and seeding (grilling "What shape does
the recovered work take?") and revert conflicts against later lands.

## Consequences

- Jinbe owns his first leaf kind; ADR 0002's kind list grows by one.
- Main keeps one write direction, and every rollback is a trailer fact on main.
- Two front doors now exist (Signal, Rollback); a third should need a reason as
  strong as "the target is already known".

## Alternatives considered

- **A Signal with a "rollback" triage outcome.** Rejected: triage overhead for a
  request whose target is already exact; the user would want a dedicated kind
  anyway.
- **A `mugicrew rollback` command with no bead.** Rejected: nothing carries the
  breakage evidence, and no record of the request.
- **Rewind main when the Checkpoint is at HEAD.** Rejected: breaks forward-only
  main and `LandedBase` (a closed Checkpoint with no trailer reads as never
  landed), for a cleaner history only.
- **Zoro owns it.** Rejected: the revert is a main write, which is Jinbe's path;
  Zoro may still cut the successor.
- **An agent session for the revert.** Rejected: a clean revert needs no
  judgment; conflicts are a separate question.
