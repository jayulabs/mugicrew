# ADR 0343: An MR Watch closes once nothing is owed, and judges a late close-out by how its container closed

Status: Accepted
Date: 2026-10-21

Chart mc-uigy1 (Every close of a Merge-Request-filed bead gets a close-out
reply, including closes after the last close-out pass). Settles grilling
mc-uigy1.5. Amends ADR 0185 §8 (the MR Watch's close) and ADR 0319
(a-filed-comment-is-closed-out…) §3 (when a close-out is due).

Charting settled that Jinbe alone posts every late reply, from an MR Watch that
outlives its container. The research
(docs/research/mr-watch-outliving-its-container-dispatch-gates-closers-and-forge-writes.md)
found that a landed Checkpoint does not need this: it cannot close while a
filed Critique is open, and its land's `finalCloseOut` already reaches every
child, holding the Checkpoint open on a failure. The Watch must outlive its
container only under a swept Checkpoint and a landed Chart. This ADR settles
when the Watch closes and what a late pass judges against.

## Decision

1. **What is owed.** A Watch owes a close-out while its container has a filed
   child (the container's filed kind, with a `Source:` line for the Watch's
   MR iid) that is unclosed, or closed with a discussion that has no
   `Closed out:` note.
2. **When the Watch closes.** Once nothing is owed **and** either the
   container is closed or the MR is merged or closed on the Forge. A merged or
   closed MR gets no Forge Review, so no new filed child can appear.
3. **A bound on failing writes.** A close-out that keeps failing under a
   closed container (a locked or deleted discussion, a 4xx) is retried each
   pass. 7 days after the container closed, the Watch closes anyway with
   `obsolete: close-out abandoned` and a Warn event. Surfacing it to a human is
   a separate question.
4. **A late pass** (container closed, or MR merged or closed) runs only the
   close-out arm, never the Forge Review arm. Its target depends on how the
   container closed:
   - **Landed**: the container's newest Landing closed `landed:`. The target
     is merged, `head` is the landed squash's `Tip:` (falling back to the
     merge sha), and trailers are read from `Tip` instead of the container
     branch that `deleteLanded` removes. Stranded is judged as
     `finalCloseOut` judges it.
   - **Swept**: the container closed `obsolete:` with no landed Landing, or
     its MR was closed or merged on the Forge outside a land. The target is
     **final but unmerged**: every closed owed child is due at once, with no
     wait for a push that cannot come, trailers are read from the kept
     container branch, and `head` is the MR's current head. The verdict comes
     from the close reason. "Merged" stays reserved for a real landing.
5. **Closers.** `reconcileMRWatch`'s container-closed, merged and closed arms
   run the late pass and close only per §2. Zoro's `closeOut` leaves the Watch
   out of its force-close, and `retireStragglers` spares an open MR Watch. The
   land path's `closeMRWatches` is unchanged: `finalCloseOut` has already
   settled every child or held the Checkpoint open. A swept container with no
   Watch gets none spawned. With no MR there is no `Source:` child to owe.

## Considered Options

- **Close only once the container is closed.** Rejected. It polls a dead MR
  until a slow container closes, and buys nothing, because a merged or closed
  MR files no new child.
- **Retry forever, or retry and file a stall Signal.** Rejected. A closed
  container should not hold a polling leaf indefinitely. The close reason and
  the Warn event record the gap.
- **Judge a swept container against the MR's head as an open MR does.**
  Rejected. The head is frozen at the last pushed pin, so a Critique
  integrated after it would wait forever.
