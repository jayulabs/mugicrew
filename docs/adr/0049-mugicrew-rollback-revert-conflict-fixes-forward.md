# ADR 0049: mugicrew Rollback revert conflict — skip the revert and fix forward

Status: Accepted
Date: 2026-10-04

Amends: ADR 0047 §4 ("always a forward revert"), ADR 0048 §4–§5 (base, seed and
Build body when the revert is skipped).

## Context

ADR 0047 has Jinbe revert the target Checkpoint's squash `S` on main in pure Go.
When later lands touched the same lines, `git revert S` conflicts. Resolving it
is a judgment about how those later lands depend on the bad code, and pure Go
cannot make it. ADR 0004's `conflict` kind does not fit: it is a Franky-owned
Checkpoint member whose close forces a re-review, and a Rollback is an
unparented leaf. Decided in grilling.

## Decision

1. **On a revert conflict, Jinbe aborts the revert and fixes forward.** He
   writes nothing to main. A conflicting revert means later work is built on
   the bad code, so a revert is no longer the obvious undo.
2. **The rest of the ADR 0048 pass runs, with no seed.** The successor
   Checkpoint (same Spec, `caused-by` the Rollback) is cut at **main HEAD**,
   and there is no reapply commit because the code is still on main. Jinbe
   spawns its one Build. Review, Critiques and Approval follow by the normal
   loop.
3. **The Build carries:** the Rollback body (if the body is empty: "reproduce
   the breakage on main HEAD, then fix it"), the target squash `S`, the paths
   the revert conflicted on, and one line saying the revert was skipped because
   later lands built on it. The later lands are not listed. Franky derives them
   with `git log S..HEAD -- <paths>`. The git seam reports the conflicted paths
   the way `RebaseConflict` reports `Base`.
4. **The Rollback closes at handoff** with reason
   `fix-forward: revert conflicted`.
5. **Main stays broken until the fix lands.** This is accepted. A human who
   needs main green at once can revert by hand.

A partial revert never needs considering. Any revert that lands is a clean one,
and its reapply seed cannot conflict (ADR 0048 §4).

## Consequences

- No new kind and no new owner. Recovery stays one AFK pure-Go pass.
- "Main is undone with a revert commit" now holds only when the revert is
  clean. A fix-forward Rollback leaves no `Reverts-Checkpoint:` trailer. Its
  successor's body pins the decision instead (`Fix-forward: <base>` and the
  conflicted paths — its own key, so a `Base:` pin never reads as one):
  once that successor exists, a crash resume keeps fixing forward without
  retrying the revert, even if main changed so it would now apply cleanly. A
  crash before the successor exists re-attempts the revert and decides afresh,
  having written nothing. The successor spawn is idempotent per Rollback.
- The fix gets the full review loop, which a hand-resolved revert on main
  would not.

## Alternatives considered

- **Abort and raise a new `revert-conflict` bead that a HITL session resolves on
  main.** Rejected: it adds a 14th kind, puts a human on the critical path, and
  lands an unreviewed resolution on main.
- **Reuse `kind:conflict`.** Rejected: it is Checkpoint-only and Franky-owned,
  and its close re-spawns a Review.
- **Also revert the later lands that conflict.** Rejected: it takes good work off
  main and cascades into several successors.
