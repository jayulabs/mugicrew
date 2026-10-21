# ADR 0319: The first Forge Review on an already-open Merge Request reads its whole history

Status: Accepted
Date: 2026-10-20

Chart mc-xw289 (Actionable Merge Request comments are filed deduplicated and
acked). Settles grilling mc-xw289.7. Once Jinbe reads every open container's
MR on every tick, a new MR gets its first Forge Review on its first note. An MR
with a backlog of unread notes exists only at the upgrade: an MR that is
already open on the first tick of the new Jinbe.

## Decision

1. **No mark means every note is unacked.** On an already-open MR the first
   Forge Review reads the whole history. Jinbe seeds no mark. Plain comments
   that the old intake rule never filed are recovered, which is the point of
   the Chart.
2. **`Thread:` beads are not adopted.** Beads filed by today's thread filing
   are not turned into `Source:` lines and are not listed in the snapshot as
   filed beads. Neither the old close leg nor the new close-out closes them out.
   The worst case is one duplicate bead per open old thread, closed by hand. In
   this Workspace no open bead carries a `Thread:` line.
3. **Notes are not flagged as predating the last Landing.** The judge files a
   stale ask as written. ADR 0317 (a-forge-review-files…) §6 and ADR 0318
   (a-forge-review-judges-a-tagged…) §4 let the fixer close it as already
   fixed. `Judged: unsure` keeps meaning unsure whether a note is actionable,
   not whether it is old.
4. **`notesSeen` retires at cut-over.** The persisted `user_notes_count` kept
   beside the Forge Review's mark becomes the only gate. Jinbe's in-memory
   `notesSeen` and `threadArm`'s filing are deleted in the same change, with no
   period in which both gates run.

## Considered Options

- **Seed the mark at the newest note.** Nothing stale is filed, but feedback
  left before the upgrade stays lost.
- **Seed the mark and adopt open `Thread:` beads as sources.** This needs a
  parser for the old format that is used only once, at upgrade time.
- **A `predates <pin>` flag in the snapshot.** It would fire almost only at
  the upgrade. In the prototype's first pass over !2859 (pass C0) it would
  have saved one stale bead out of nine.

## Consequences

- The upgrade's first pass costs about the same as an incremental pass ($0.05
  to $0.08 on !2859, about 70 notes). It may hold a pending Landing until it
  finishes, like any other Forge Review.
