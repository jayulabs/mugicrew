# ADR 0291: a non-rotating sessions.jsonl beside the event log indexes a bead's sessions

Status: Accepted
Date: 2026-10-17

Chart: A bead's sessions stay findable for as long as Claude keeps their
transcripts. Settles the grilling "Where the bead-to-session index lives".

## Context

`transcript.Sessions` finds a bead's sessions only in the event log, which
rotates at 10 MiB (ADR 0028, mugicrew-event-log-rotation), about 6 days at
today's rate, while Claude keeps transcripts about 30 days. Every writer of a
session id already holds the event log, and `transcript.Sessions` is the only
lookup that outlives tmux
(docs/research/bead-to-session-store-write-read-sites-and-costs.md §1-2). The
index only needs to work on the machine that ran the session.

## Decision

1. **Store: `$(git-common-dir)/mugicrew/sessions.jsonl`**, beside
   `events.jsonl`, never rotated. It is history under ADR 0033
   (mugicrew-event-log-is-history-not-a-view): non-authoritative, so losing it
   costs findability only.
2. **Writer: a second sink of `eventlog.Log.Write`.** A record whose msg is
   `session.started`, `hitl.launched` or `hitl.resumed` is also appended,
   verbatim, to `sessions.jsonl` with no rotation step, fail-open like the log.
   No call site changes, and a future session emitter is covered for free.
3. **Line: the start event's JSON line byte for byte** (~255-280 B), so the
   index has the event log's form and `eventlog.ReadWhere` reads it unchanged.
4. **Lookup: merge.** `transcript.Sessions` reads `sessions.jsonl`, then `.1`,
   then the live file through its existing fold, deduped by session id. The
   session set is their union. Outcome, exit, duration, result and stderr stay
   log-derived and read as unknown once rotated out.

## Considered Options

- **The session id on the bead** (bd metadata, comment or label): ~200-260 ms
  and one Dolt commit per session (hundreds a day), an unsettled ADR 0070
  (mugicrew-one-bd-write-rule) class for metadata and comments, history put
  into coordination state, and an id synced to machines whose transcripts
  aren't there. Rejected.
- **An explicit index writer at each site**: touches every site and silently
  misses a new emitter. Rejected for the sink.
- **A trimmed ~150 B record**: needs its own encoder and reader. Rejected.
- **Index alone as the session list**: a gap in the index would hide a
  session the log still holds. Rejected for the merge.

## Consequences

- `sessions.jsonl` grows without bound (~4 MB a month at 500 sessions a day)
  and is read whole per lookup (~4 ms at 30 days); pruning or a cap is open.
- Sessions that predate the sink need a one-time seed from the current
  `events.jsonl.1` and live file; its trigger and idempotence are open.
