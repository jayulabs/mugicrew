# ADR 0292: sessions.jsonl rotates at the event log's MaxSize, like the event log

Status: Accepted
Date: 2026-10-17

Chart: A bead's sessions stay findable for as long as Claude keeps their
transcripts. Settles the grilling "Whether sessions.jsonl needs pruning or a
size cap" and amends ADR 0291
(mugicrew-a-non-rotating-sessions-jsonl…): the index is bounded, not
unbounded.

## Context

Measured on 2026-10-17, session-start lines average ~261 B and peak at 832 a
day (~217 KB a day, ~6.5 MB per 30 days), so unbounded growth adds ~40-80 MB a
year to a file read whole on every lookup. The event log already rotates with
a lock-free stat+rename at `MaxSize` (ADR 0028, mugicrew-event-log-rotation).

## Decision

1. **The sink is a second `eventlog.Log` at `sessions.jsonl`**, so it rotates
   to `sessions.jsonl.1` at `eventlog.MaxSize` (10 MiB) by the same code. The
   writer prunes itself: no prune verb, no rewrite, no new lock.
2. **Retention is by size, not age**: at least one full file (~46 days at
   peak, ~3 months at average), always above Claude's ~30 days.
3. **Lookup reads four files**: `sessions.jsonl.1`, `sessions.jsonl`,
   `events.jsonl.1`, `events.jsonl`, deduped by session id. Worst case ~40-50
   ms per lookup (up to 20 MiB of index plus 20 MiB of log); accepted.
4. **The rotation race is accepted** as in ADR 0028: two writers crossing the
   cap together can clobber `.1`, costing findability only (ADR 0033,
   mugicrew-event-log-is-history-not-a-view). Upgrade if it bites: flock
   around stat+rename.

## Considered Options

- **Unbounded growth**: no code, but lookup cost grows without limit. Rejected.
- **Age-based prune (drop lines older than ~30 days)**: rewrites the file
  under concurrent O_APPEND writers, so it needs a flock every writer takes
  plus a trigger. Rejected.
- **A smaller index-only cap**: a second constant and retention cut close to
  30 days on busy days. Rejected.
