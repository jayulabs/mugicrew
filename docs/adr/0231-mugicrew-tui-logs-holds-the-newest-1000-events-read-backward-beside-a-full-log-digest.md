# ADR 0231: TUI Logs holds the newest 1000 events, read backward, beside a full-log digest

Status: Accepted
Date: 2026-10-16

Chart mc-1nvm (TUI Graph and Logs tabs bounded by what is shown). Settles
prototype mc-1nvm.4 (Logs windowed event read and streamed full-log filter); the
throwaway is on `refs/mugicrew/prototypes/mc-1nvm`.

Today the TUI parses the whole log (`.1` plus the live file, ADR 0028 §4) into
memory on first load and after every rotation. On a full ~20 MiB log (58.7k
events) that takes 1.4–2.4 s and leaves a 128 MiB heap. It costs ~40 µs per
parsed event, so the time grows with the number of events parsed, not with
file size. We bound it in three ways:

1. **The window.** Logs holds the newest **1000** events, read backward in chunks
   from the end of the live file and on into `.1` (~16 ms). Scrolling past the
   oldest loaded row loads the next 1000 from a cursor (file and byte offset,
   ~15 ms), and the window stays at whatever size scrolling has grown it to. The
   head still extends through `eventlog.ReadFrom` from the `State` the backward
   read returns. A rotation moves a cursor in the live file to the same offset
   in `.1`, and ends a cursor that was in the old `.1` ("nothing older"). The
   window keeps the rows it already read rather than re-reading. The selection
   stays pinned by event `Key` as before.
2. **The filter scan.** Any filter (/ words, f kind, L scope, e errors) scans the
   whole log backward, off the event loop (ADR 0098). Each chunk's matches are
   handed over when that chunk is read, not after a fixed number of hits, under
   'searching…'. Before parsing, each raw line goes through a prefilter that
   cannot reject a real match: every query word (and the L bead id, and
   `"level":"ERROR"` for e) must appear in the lowercased line. This holds
   because matched text never spans a space and JSON leaves those bytes
   unescaped. Words containing `"` or `\`, and a kind-only filter, parse every
   line. Prefiltered: ~75 ms. Parse every line: 1.1–1.4 s.
3. **The digest.** Stats (all-time sums), `landEvents` and the finished-flash
   read the whole log, and the Chart holds every tab other than Graph and Logs
   to exactly what it shows today. So the shell keeps, in full, only the events
   they read: `session.ended/failed`, `hitl.exited/failed`, `land.*`, `gate.*`,
   `effort.failed`, `effort.paused`. That is ~15% of the log. One prefiltered
   scan builds it off the event loop, `ReadFrom` extends it, and it is rebuilt
   when `ReadFrom` reports a full read.

This amends ADR 0028 §4: the TUI no longer reads both files whole on every
reload, and `eventlog.Read` stays for other readers.

## Considered options

- **N = 500 / 2000 / 5000:** ~10 / ~33 / ~85 ms cold. 1000 is dozens of screens
  of rows and keeps per-keypress rescans trivial; scrolling loads the rest.
- **Filter only the window:** misses older matches. The Chart requires the whole
  log.
- **Parse every line in the filter scan:** correct but ~15× slower than the
  prefilter for no gain.
- **Feed Stats and friends from the window:** changes what those tabs show, which
  breaks the Chart's hard constraint.
