# ADR 0398: A carried Approval is a Go-written successor at the land gate that reads through to its origin

Status: Accepted
Date: 2026-10-29

Chart "A container's pre-land renumber no longer loses the race to mainline's next numbered file" settled that a head differing from the last approved head only by a renumber carries that Approval, and ADR 0397 (mugicrew-a-renumber-only-head-is-the-approved-squash-replayed…) fixes the check. This ADR fixes where the check runs, what the carried Approval is, and how a human tells it from a stamp they gave.

## Decision

1. **Hook points.** A Checkpoint's check runs in Jinbe's pre-Review gate (`spawnReviewIfDue`), after a clean conflict/clash probe and just before the bare Review is spawned. A Chart's runs in Nami's `gateChart`, after off-head open Approvals are obsoleted and the Patch-id is computed (the empty-diff Landing case first), just before the Approval at the head is spawned. An accept spawns the carried Approval instead of the Review (Checkpoint) or the Approval session (Chart). The existing at-head gate then spawns the Landing on the next tick.
2. **Once per head, stateless.** The check keeps no memo. An accept puts an Approval at the head, and a reject spawns the Review or Approval, so either way the gate does not reach the check again for that head. A transient git error retries next tick.
3. **Last approved.** The newest Approval closed `approved`, carried or not. An auto-approved stamp carries too: the renumber adds nothing the AFK run did not already authorize.
4. **The carried Approval.** Go writes it (Jinbe or Nami), closed `approved` at birth, caused-by the Approval it carries. Its body:

   ```
   Approval carried from Approval <old title>: renumber-only.
   Commit: <new head>
   Patch-id: <recomputed>        (Chart only)
   ### Approval History
   carried (renumber-only from <old sha>: 0394 → 0395)
   ```

   The History line names each renumbered file's old and new number. No Packet is copied. The origin (the first non-carried Approval up the caused-by chain) holds the Packet and whether the stamp was a human's or AFK.
5. **A reject is surfaced.** The fallback Review or Approval gets a one-line note: `not carried: <rule point> (<first differing file>)`.
6. **TUI.** A carried Approval never enters Actionable. The container's rollup "approved" row reads `carried (0394 → 0395) from <old sha short>`, and the Approval's own row shows a dim `carried` word.
7. **Packets.** The next Decision Packet or Chart Packet Luffy writes for the container lists, under Approval History, the carried laps since the last non-carried stamp, so the human sees renumbers they never looked at.
8. **Re-approval under gitlab.** A carry leaves the MR un-drafted and never calls `reapprove`. Luffy's re-approval rule gains one clause: its auto-approved test (`historyEndsAny`) reads the History of the cutoff's origin, walking carried Approvals back to the first non-carried one. Otherwise a carry from an AFK stamp would let Go's own un-draft vouch for a skip. A Chart's Patch-id comparison still uses the cutoff itself, since the carried Approval's recomputed Patch-id is the last stamped content.

## Considered Options

- **Silent reject.** Rejected: the human would be asked again on a renumber lap with no reason given, and the rule in ADR 0397 could not be tuned from evidence.
- **Never carry from an auto-approved stamp.** Rejected: it buys nothing, as long as the AFK origin stays visible through the chain (points 4, 7, 8).
- **Copy the origin's Packet into the carried body.** Rejected: copies go stale and duplicate. The caused-by edge already reaches the origin.
- **A History suffix marking an AFK origin.** Rejected in favour of walking the chain, which is the one source of truth.

## Consequences

- Wherever the codebase reads a stamp's authorship (today, only the re-approval rule's auto-approved test), it must resolve carried Approvals to their origin.
- The exact-commit reading of an Approval is amended: a carried Approval certifies its `Commit:` by derivation from its origin's commit, not by a Review of it.
