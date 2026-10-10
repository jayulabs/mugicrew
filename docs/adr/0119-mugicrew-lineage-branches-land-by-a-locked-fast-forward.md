# ADR 0119: mugicrew lineage branches land by a locked fast-forward (`mugicrew land-lineage <branch>`), not through a Checkpoint

Status: Accepted
Date: 2026-10-10

## Context

`lineage sync <app>` (and `install`, and lineage's conflict recipe) leaves one
commit cut from main that carries a `Lineage: <src> <tip>` trailer. That
trailer must reach main unchanged, or the next merge finds the wrong base. In
an OE without mugicrew, `git merge --ff-only` is enough. Here, every move of
main goes through the land lock (ADR 0069). A bare fast-forward could race a
Checkpoint or Chart land and leave main's checkout with a reverse diff staged.
Decided in grilling (Chart "mugicrew as a Stock under Lineage,
synced and published").

## Decision

1. **A human runs `lineage sync`, then `mugicrew land-lineage <branch>`.** This
   is CLI only, with no TUI key. Mugicrew never calls the `lineage` binary. It
   knows only the trailer format.
2. **The verb fast-forwards.** Under the land lock, it checks that main is
   clean, checks the branch's shape, and advances main with `ffMainline`
   (through main's worktree when that worktree is checked out). It adds no
   squash, no mugicrew trailer, no bead and no review.
3. **The verb checks shape, not name.** The branch must be exactly one commit
   whose parent is main's current tip. That commit must carry at least one
   `Lineage:` trailer, read with git's trailer parser. A branch from lineage's
   conflict recipe (`lineage/resolve-<app>`) therefore lands the same way.
4. **If main moved, the verb refuses.** It keeps the branch and says to re-run
   `lineage sync`. Sync is plumbing only, so a re-run is cheap. No rebase runs
   under the lock.
5. **On success the verb deletes the spent branch.** On any refusal it keeps
   the branch.
6. **A conflict stops and is reported (v1).** The human resolves it with
   lineage's README recipe and lands the result with this verb instead of its
   final `git merge --ff-only`. Mugicrew's README carries that note, and
   lineage's README stays OE-agnostic.

## Considered Options

- **Ride a Surgery.** Rejected. A Surgery commits through `effort/<id>` and
  Integrate, whose `commit-tree` squash drops trailers, so both Integrate and
  Land would need a trailer carry. After a Promotion, a sync is often an empty
  diff on main, which leaves review nothing to look at.
- **A lineage-merge Checkpoint shape.** Rejected as the heaviest option. It
  would change Zoro, Jinbe, the TUI and pivots for a rare act driven by a
  human.
- **A verb that wraps `lineage sync`.** Rejected. It would make mugicrew
  depend on the binary and its PATH, against the triage boundary.
- **No verb: stop the loop, `git merge --ff-only`, restart the loop.**
  Rejected. It pauses dispatch on every Sync, and HITL integrates and lands
  can still race it.

## Consequences

- Main gains commits that are not land squashes and carry no `<Kind>:`
  trailer. Mugicrew's trailer readers (`LandedBase`, `MainlineLog`,
  `LandedTip`) match by line prefix and ignore them.
- The Chart's planned `git.Land` trailer carry is no longer needed by
  this route. It stays open for the day a lineage commit rides a Checkpoint.
  Until then, merging a lineage commit into a Checkpoint by hand loses its
  trailer silently.
- The verb skips Land's ADR-clash check. An ADR that a Sync brings in can
  collide with an Overlay ADR number. Stock authoring flow (Signal "Route
  mugicrew Stock work through stock/mugicrew") owns that.
