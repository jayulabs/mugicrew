# ADR 0253: The transcript view follows a `continued-in` handoff under one header and skips repeated uuids

Status: Accepted
Date: 2026-10-17

Chart mc-pxfb (Show any bead's session transcripts, finished or live, in a
readable view). Settles grilling mc-pxfb.6. Builds on ADR 0250
(mugicrew-the-transcript-view-renders-turns-as-a-digest…) and amends ADR 0251
(mugicrew-transcript-verb-follows-by-default…) §3. Grounded in
docs/research/claude-session-transcript-lines-resume-and-pruning.md §3.3.

A HITL session can write a `continued-in` record (`continuedInSessionId`) and
go on under a new session id that mugicrew's event log never records (8 real
sessions so far, e.g. 90d4e8f5 → 21f99c25). Stopping at the record would show
the session as over when it went on, so the view follows it.

## Decision

1. **Follow.** When a transcript ends in a `continued-in` record, the view
   finds `continuedInSessionId` with the same project-dir glob and goes on
   rendering it in the same stream. If the destination is not found (pruned,
   another project dir), the view ends with the marker of §3 alone.
2. **Skip repeated lines by uuid.** The destination re-writes the source's
   lines under the new id. A destination line whose `uuid` was already rendered
   is skipped; lines without a uuid are not turns and are dropped anyway.
3. **One header, an inline marker.** The session keeps its one header (the
   recorded id and its event-log facts). At the jump the view prints one dim
   line: `↪ continued in <id8> (bg)`.
4. **Chains.** A destination that hands off again is followed too, stopping at
   any session id already visited.
5. **Live follow (amends ADR 0251 §3).** A following view that meets
   `continued-in` switches to tailing the destination; the recorded session's
   `hitl.exited` no longer ends the follow while a destination exists. The view
   stops when the destination stops growing. If the destination file does not
   exist yet, the lookup is retried for a short window, then the view falls
   back to §1's marker-only end.
6. **`--session` takes recorded ids only.** A destination id is not a session
   of the bead, so `--session <dest>` reads as an unknown session.

## Considered Options

- **Only a dim "continued in another session" line.** Rejected: it hides the
  tail of the session, which in the known cases is where the work ends.
- **Start the destination after the source's `continued-in` timestamp.**
  Rejected for uuid skipping, which is exact and needs no clock.
- **A second session header for the destination.** Rejected: it is one
  conversation, and the header's facts come from the event log, which does not
  know the destination.

## Consequences

The event log still does not record the destination id, and Session cost and
Agent time miss its spend. That is an offshoot Signal, not this Spec.
