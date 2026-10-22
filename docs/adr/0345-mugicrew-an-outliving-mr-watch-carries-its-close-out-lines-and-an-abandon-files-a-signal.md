# ADR 0345: An outliving MR Watch carries its close-out lines, and an abandon files a Signal

Status: Accepted
Date: 2026-10-21

Chart mc-uigy1 (Every close of a Merge-Request-filed bead gets a close-out
reply, including closes after the last close-out pass). Settles grilling
mc-uigy1.6. Amends ADR 0343 (an-mr-watch-closes-once-nothing-is-owed…) §3,
which left surfacing an abandoned close-out to a human open.

Under ADR 0343 an MR Watch outlives its container and keeps running the
close-out arm. The only human-facing close-out signal was `recordCloseOuts`'
`Close-outs due:` / `Close-outs failing:` lines on the container body, shown by
the TUI's `d`. Nothing looks at a closed container, so late-pass progress,
failures and the 7-day abandon were invisible. The event log is history, not a
view (ADR 0033), so a Warn event alone reaches no one.

## Decision

1. **The lines move to the Watch.** Once the container is closed, every late
   pass writes `Close-outs due:` and `Close-outs failing:` on the MR Watch's
   own body, whether its target is merged (landed) or not (swept). The
   final-pass rule that returns a failure to hold the container open applies
   only while the container is open. Before the container closes, passes
   write to the container as today. The first late pass removes the
   container's lines, so a closed container shows no stale counts.
2. **The TUI shows them on the Watch.** An outliving Watch's Merge Requests
   row (exec tab) shows `closing out · N due · M failing` in place of the
   Landing status word, with zero counts left out. `d` on the Watch shows the
   same `close-outs` field a container shows. The row needs the closed
   container in the snapshot, so it must stay loaded while its Watch is open.
3. **An abandon files a Signal.** When ADR 0343 §3's 7-day bound abandons
   the Watch, Jinbe files a Signal in Go, as the stranded-commits Signal is
   filed:
   - Unparented and `discovered-from` the MR Watch.
   - Titled `<container title>: close-outs abandoned on !<iid>`,
     deduplicated by that exact title so a retried abandon files it once.
   - Its body names the container and how it closed (landed or swept), the
     MR ref and link, one line per owed child (bead, discussion id, last
     error), and a Fix line: reply to and resolve those threads by hand, or
     clear the lock, then close the Signal.

   The Watch closes `obsolete: close-out abandoned; carried by <signal>`. If
   filing the Signal fails, the Watch stays open and retries on the next pass:
   an abandon never closes without its Signal. The Warn event stays.
4. **A closed Watch's lines.** A Watch that closes because nothing is owed
   clears its lines. A Watch that closes on abandon keeps them as the final
   record beside its close reason.

## Considered Options

- **Keep the lines on the container, and have the TUI read closed
  containers.** Rejected. It would write to a closed bead and needs a new
  view. The Watch is the open bead that does the work, and it already has a
  row.
- **Warn event only on abandon.** Rejected. It leaves a reviewer thread
  unanswered with no one told.
- **A `bd human` flag on the closed Watch.** Rejected. The TUI doesn't show
  closed Watches, and a Signal goes through the normal human-gated triage.
