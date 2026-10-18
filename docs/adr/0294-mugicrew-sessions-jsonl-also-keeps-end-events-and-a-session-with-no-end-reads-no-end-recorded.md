# ADR 0294: sessions.jsonl also keeps end events, and a session with no end reads "no end recorded"

Status: Accepted
Date: 2026-10-17

Chart: A bead's sessions stay findable for as long as Claude keeps their
transcripts. Settles the grilling "What a session's transcript header shows
once its end events have rotated out". Amends ADR 0291
(mugicrew-a-non-rotating-sessions-jsonl…) §3-4, ADR 0292
(mugicrew-sessions-jsonl-rotates-at-the-event-log-s-maxsize…) §2 and ADR 0293
(mugicrew-the-sessions-jsonl-sink-seeds-the-index…) §3.

## Context

`transcript.Sessions` takes outcome, exit, duration, result and stderr from
end events, and marks a session with none `live`. The `transcript` verb then
follows it (ADR 0251, mugicrew-transcript-verb-follows-by-default…). With
start events kept past rotation (ADR 0291), every session whose end has rotated
out would read `live` and be followed forever. A crash already does that
today, against ADR 0033 (mugicrew-event-log-is-history-not-a-view) §3.

Measured on 2026-10-17: `session.ended` averages 1,543 B, of which `result`
(the 1 KiB result tail) is ~1,190 B. No view shows it for an end that did not
fail. Copying end lines verbatim would grow the index ~5.5× at peak (1.26 MB a
day), so one 10 MiB file would hold ~8 days.

## Decision

1. **The sink also keeps end events.** Its predicate (ADR 0293 §3, shared by
   the seed) matches `session.ended`, `session.failed`, `hitl.exited` and
   `hitl.failed` as well as the three start msgs, with a `session` attr.
   `hitl.continued` stays out because `Sessions` does not fold it. The reader
   is unchanged: the index then `.1` then the live file replay one event
   sequence, and replaying a suffix of it again ends in the same state.
2. **`session.ended` is copied without its `result` key**, by the sink and by
   the seed alike. Every other line, `failed` ones included (the fallback
   shows their `result` and `stderr`), stays byte for byte. At peak that is
   ~579 KB a day: 18-36 days across `.1` and the live file, ~73+ days at the
   average rate.
3. **No end event anywhere reads "unended", never `live`.** That covers a
   crash, a session still running, or an end lost to an index gap.
   `Session.Outcome` is `"unended"`. `Header` prints `no end recorded` (dim
   yellow, where `live` was) with duration `—`, since any number would be a
   liveness guess.
4. **The follow is kept.** The verb still follows an unended last session by
   default, so a running one keeps its live view, and a crashed one sits idle
   until `q`. While following without a transcript the body reads `waiting for
   transcript…`. A static print reads `no transcript (not yet written, or
   pruned) · no end recorded`.

## Considered Options

- **Start facts only (end fields unknown after rotation)**: combined with the
  follow, any rotated-out session would be followed forever. Rejected.
- **End details from the transcript's tail** (`cost-state`, last timestamp):
  no exit code, no failed verdict, gone with the transcript. Rejected.
- **Verbatim end lines**: retention drops to 8-16 days at peak, under Claude's
  ~30. Rejected.
- **A larger cap for the index alone**: a second constant and up to ~60 MiB per
  lookup. Held as the upgrade if sustained peak days cut retention short.
- **Asking the liveness probe (ADR 0025) for `live`**: pulls the probe into the
  transcript reader. Left as a later nicety.
- **Not following an unended session**: loses the live view of a running
  session. Rejected.
