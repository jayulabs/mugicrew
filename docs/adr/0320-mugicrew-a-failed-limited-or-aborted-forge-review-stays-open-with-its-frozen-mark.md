# ADR 0320: A failed, limited or aborted Forge Review stays open with its frozen mark

Status: Accepted
Date: 2026-10-20

Chart mc-xw289 (Actionable Merge Request comments are filed deduplicated and
acked). Settles grilling mc-xw289.8. Unacked means the notes after the last
*closed* Forge Review's mark. A Forge Review is a member of its container, so
while it is open the container is not drained and no new Landing is cut. ADR
0318 (a-forge-review-judges-a-tagged…) and ADR 0319
(a-filed-comment-is-closed-out…) cover only a pass that succeeds.

## Decision

1. **A Forge Review that fails or hits a Plan usage limit stays open and is
   retried as the same bead.** Failed and limited efforts already delete
   their Drafts, so a pass that dies files nothing: it is all or nothing. The
   bead keeps the mark and note snapshot Jinbe wrote at spawn. The loop
   re-dispatches it as it would any failed or limited effort. Failures count
   toward a _failing_ pause, and a limit hit never does. Only a successful close
   records a mark, so "last closed" always means "last successful". Notes left
   meanwhile wait for the next Forge Review, because at most one is open per
   container.
2. **A retry judges the frozen note snapshot, but reads the filed beads live.**
   The notes are not refreshed. A later "done" never stops a filing anyway
   (ADR 0318 §4), and edited notes are the Chart's separate question. The
   beads already filed from the MR (status, close reason, sources) can change
   between runs. Usopp reads them from the graph at run time, not from the
   snapshot, so a fold or a fresh file is judged against today's beads.
3. **An aborted Forge Review is paused _manual_ and stays open.** The
   container stays undrained until the human lifts the pause or closes the
   bead. An abort is a human hold, and draining past unread feedback is what
   the Chart exists to stop.
4. **A stuck Forge Review is visible on its container.** A container held
   undrained by a paused Forge Review (_failing_ or _manual_) says so on its
   own row, not only on the member's row. The qualifier's shape belongs to the
   Chart's surface grilling.
5. **A pending Landing is unaffected.** It stays pending while the Forge
   Review is stuck, as it does while one runs, and a human merge on the Forge
   is still accepted (ADR 0199 (a-chart-merge-request-comment-is-a-grilling…)
   §6). Notes stranded by that merge take ADR 0319's human-merge "carried by"
   path.

## Considered Options

- **Close the failed pass without a mark and spawn a fresh one.** The fresh one
  would re-snapshot and pick up newer notes. Rejected: it needs a "closed but
  not counted" exception in the unacked rule, and it bypasses the loop's
  existing failure, limit and pause handling.
- **Refresh the whole snapshot before each retry.** Rejected: only the filed
  beads change in a way that alters a verdict, and they are read live.
- **Close an aborted Forge Review so the container can drain.** Rejected: an
  abort would then silently let a Landing go out past unread feedback.
