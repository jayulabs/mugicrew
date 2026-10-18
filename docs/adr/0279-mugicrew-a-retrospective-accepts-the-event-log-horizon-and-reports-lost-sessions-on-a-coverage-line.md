# A retrospective accepts the event log's horizon and reports lost sessions on a coverage line

The event log is the only index from a bead to its sessions. It rotates at
10 MiB (ADR 0028), which now holds about 6 days of history, while Claude keeps
transcripts for 30 days
(`docs/research/retrospective-session-findability-and-read-size.md` §3). A
retrospective on an older spine would silently under-count, because "no
sessions recorded" looks the same as "never ran" (same doc, §4). This Spec
accepts that horizon and makes the gap visible instead of closing it. A durable
bead→session index would have to change the transcript lookup, which this Chart
rules out of scope, so it goes to an offshoot Signal. Settled by the grilling
mc-4ryp.6.9.

1. **No new index here.** A retrospective reads only what the event log still
   indexes.
2. **Coverage line.** Robin prints this line before the pattern list, extending
   the header of ADR 0278 (mugicrew-a-retrospective-presents-one-flat…) §1. It
   is always printed, e.g. `Coverage: 22 of 29 beads have sessions; 5 closed
   before the event log's start (10-16 17:59) and are lost; 3 sessions are
   pruned (header only).`
3. **Lost vs never ran.** A bead with no findable session counts as **lost**
   when its kind runs sessions (anything but Checkpoint and Landing, the only
   kinds with no session start in the log) and it closed before the log's oldest
   event. A bead that closed after that event and has no session never ran or
   was closed by hand, and it is not a gap.
4. **Partial.** When anything is lost, Robin says the patterns are partial. She
   ranks and offers them as usual, with no numeric threshold and no refusal.
5. **Pruned sessions** are counted separately. These are logged sessions whose
   transcript is gone, so they render header-only.
6. **Prompt only.** Robin reads the oldest event's time from the first line of
   `$(git rev-parse --git-common-dir)/mugicrew/events.jsonl.1`, or of the live
   file when there is no `.1`. She takes `closed_at` from the bead set she
   already builds. There is no new Go code, and the r dialog does not warn.

## Considered Options

- **Raise MaxSize or keep more backups** (about 60 MiB covers the 30-day prune).
  Rejected for now: the Logs tab and the per-bead transcript verb re-read the
  whole log, so both slow down in proportion, and the rotation race remains.
- **Record session ids on the bead, or keep a small non-rotating index.** These
  are the durable fixes. They are deferred to the offshoot Signal because they
  change the transcript lookup.
- **Kind alone as the proxy.** Rejected: it flags beads closed by hand (a moot
  Conflict) as lost. The close time against the log's start separates those
  cases.
- **A warning in the r dialog.** Rejected: it adds a log read to every press,
  and the coverage line already shows the gap.
