# ADR 0052: mugicrew claims a bead with a status compare-and-swap, not `bd --claim`

Status: Accepted
Date: 2026-10-04

## Context

`bd.Claim` runs `bd update <id> --claim`. bd treats that as idempotent for the
same actor. The loop and the TUI's run-now (`t`) both shell out to `bd` as the
git user, so they are the same actor. If both read a bead as ready before
either claims it, both claims succeed and both dispatch the bead. On worktree
routes the loser gets "busy" and **unclaims the winner's live bead**. On
in-process routes `crew.Run` runs twice. The gap predates the Loop lifecycle
control Spec, but a TUI-startable loop makes "loop on + `t`" an
ordinary state. Decided in grilling.

## Decision

1. **The claim is a CAS.** `bd.Claim` runs
   `bd update <id> --if-status open --status in_progress --assignee <actor>`.
   bd applies it atomically. On a mismatch it writes nothing and exits 13.
   Both callers already treat a claim error as "skip" and record
   `claim.failed`.
2. **The actor is resolved once, the way bd resolves it:** `$BEADS_ACTOR`, then
   git `user.name`, then `$USER`. bd has no whoami command. The assignee must
   equal what a human's plain `bd unclaim` sees as "you".
3. **The race is not accepted risk.** The FR22 per-owner cap race was accepted
   because its worst case is one extra effort. This race corrupts state (a
   live bead reads `open`), so it is fixed in this Spec.

## Considered and rejected

- **A distinct actor per process** (e.g. `BEADS_ACTOR=mugicrew-loop`). This
  makes `--claim` exclusive, but then a human's plain `bd unclaim` refuses
  ("claimed by a different actor"). That breaks the manual recovery that ADR
  0050 and the plan of record rely on.
- **Offshoot Signal / accepted risk.** The fix is one function, and this Chart
  is what makes the race common.

## Consequences

- **Claims carry no bd lease.** `--claim` stamps a lease, and the CAS form does
  not. Nothing in mugicrew renews or reads leases: the loop never heartbeats,
  and the ADR 0003 "self-heals on lease expiry" wording is already being
  corrected by this Spec. Stranded-claim recovery (the Chopper Signal)
  must detect stranded claims without leases, e.g. by process-table liveness
  (ADR 0025/0031).
- The spec §6.5 comment "Idempotent if already claimed by us" on `bd.Claim` is
  no longer true. A second claim by us now fails, and that is the point.
