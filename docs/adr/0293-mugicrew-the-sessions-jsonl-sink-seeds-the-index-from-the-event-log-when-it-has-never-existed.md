# ADR 0293: the sessions.jsonl sink seeds the index from the event log when it has never existed

Status: Accepted
Date: 2026-10-17

Chart: A bead's sessions stay findable for as long as Claude keeps their
transcripts. Settles the grilling "How the one-time seed of sessions.jsonl
runs", building on ADR 0291 (mugicrew-a-non-rotating-sessions-jsonl…) and
ADR 0292 (mugicrew-sessions-jsonl-rotates-at-the-event-log-s-maxsize…).

## Context

Sessions started before the sink shipped exist only in `events.jsonl.1` and
the live file. On 2026-10-17 `.1` covered 2026-09-29 to 2026-10-16, with about
3,450 start lines. The next rotation, around 2026-10-23, drops events from
before 2026-10-16. Writers use plain `O_APPEND` with no lock, and lookup
already dedupes by session id.

## Decision

1. **Trigger: the sink, when the index has never existed.** Before it appends,
   the sink checks for `sessions.jsonl` and `sessions.jsonl.1`. If both are
   absent, it seeds. A rotation (ADR 0292) leaves `.1` in place, so it never
   triggers a seed. Deleting both files re-seeds on the next start event.
2. **Seed:** the sink copies the matching lines of `events.jsonl.1`, then the
   live file, verbatim and in that order, into a temp file beside the index.
   It then calls `os.Link` to put the temp file at `sessions.jsonl`, which is
   an atomic create-if-absent, and removes the temp file. A process that loses
   the link race just discards its temp file.
3. **One predicate:** `msg` is `session.started`, `hitl.launched` or
   `hitl.resumed`, **and** the line has a `session` attr. The seed and the
   sink both use it, so the sink also drops start events that have no session
   id.
4. **Idempotence by dedupe, not by filtering:** once the file exists, every
   sink write appends its own record. That record may already be in the seed
   (its log line was written first), or a racing writer may land its line
   twice. Such duplicates are accepted because lookup dedupes by session id.
   No line is lost.
5. **Seed failure skips the append.** If a read or write fails with anything
   other than not-exist, the sink appends nothing and leaves the index absent.
   The next start event retries, and this record's log line is still there
   for that retry to copy.
6. **Silent:** the seed records no event. It runs inside `Log.Write`, where
   an emit would recurse.

## Considered Options

- **An explicit seed verb**: someone has to remember to run it before
  rotation, and afterwards it is dead code. Rejected.
- **Lazy seed on lookup**: if no one looks before rotation, the old sessions
  are lost, and lookup already merges the log anyway. Rejected.
- **Line-level dedupe at seed time, or skipping the winner's own append**:
  adds code to save a few hundred bytes, and breaks when the log write itself
  failed. Rejected.
- **Hedging the 2026-10-23 deadline with a hand-copied `.1` as an extra seed
  source**: one-off code for a P3 gap that the retrospective already reports
  (ADR 0273, mugicrew-a-retrospective-accepts-the-event-log-horizon…).
  Rejected. If the Spec ships after that rotation, sessions from before
  2026-10-16 are lost.
- **Guarding on the live file alone**: every ADR 0292 rotation would re-seed
  up to about 20 MiB of log as duplicates. Rejected.
