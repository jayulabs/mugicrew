# ADR 0147: A land is a Landing bead behind a land-strategy seam

Status: Accepted (amended by ADR 0185)
Date: 2026-10-13

Spec mc-sl6m, from Chart mc-9kvd. A Checkpoint or Chart land was a step inside
its container's reconcile. It is now a `landing` bead (lifecycle: ADR 0145), and
the land itself sits behind a seam so a remote **Forge** can later be the source
of truth for mainline. This ADR amends ADR 0016 §1/§6, ADR 0017 and the
Amendment of ADR 0069. It cross-references ADR 0145, which already amends ADR
0004 §2/§4, ADR 0011 §1–2 and ADR 0016 §7.

## Decision

1. **The seam is `AnchorIfAbsent`'s write leg.** A land strategy is
   `func(branch, trailer string) (sha string, err error)`, run by
   `git.Land(branch, trailer, strategy)` as the write leg of `AnchorIfAbsent`
   under the one land lock. The lock, `LandedBase` idempotency and the trailer
   stay common to every strategy (ADR 0069 §1). `local` is the former land write
   leg, moved unchanged. `gitlab` opens a Merge Request and returns pending
   (ADR 0185).
2. **Four outcomes, on the existing return shape.**
   - _landed(sha)_: `err == nil`.
   - _conflict_: `*RebaseConflict` or `*ADRClash`.
   - _pending_: the sentinel `git.ErrLandPending` (`errors.Is`) for a Merge
     Request that is open and not yet merged, so the GitLab strategy needs no seam
     change.

     > **Amended by ADR 0185:** the `gitlab` strategy now returns it, and the MR
     > lifecycle (reuse, close, trailerless merge) is ADR 0185's.
   - _error_: everything else, including `*MainDirty`.
3. **`land_strategy` is layered config; an unusable value is an error.** The key
   (`"local" | "gitlab"`, default `local`) resolves like `auto_exit`. Anything
   else fails the Landing naming the value. It never falls back to `local`: a
   remote-truth repo must not get a local mainline land by accident.
4. **Jinbe is the only land caller.** He owns `landing` for both Checkpoint and
   Chart. Nami and the Checkpoint reconcile spawn it but never call `Land`. The
   conflict classification and filing that Jinbe and Nami duplicated move into
   Jinbe's Landing.

## Considered Options

- **A Landing per kind, one for Nami and one for Jinbe.** Rejected: the land is
  the same operation, and two owners would duplicate its conflict filing.
- **A strategy that is a config-selected branch inside `Land`.** Rejected: the
  seam as a function keeps the lock and trailer out of every strategy's reach.

## Consequences

- ADR 0016 §1/§6: there is now a dispatched land bead, and Jinbe owns the
  `landing` leaf. ADR 0017: the `gitlab` strategy is the first remote seam, since
  the `git` package was local-only. ADR 0069's Amendment: Checkpoint and Chart
  lands have a bead; the other moves of main (Rollback reverts, Spec anchors)
  stay bead-less.
- A repo set to `gitlab` re-dispatched the Landing and logged `land.failed` each
  tick until the Merge Request sync existed. It now returns pending (ADR 0185).
