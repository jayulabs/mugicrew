# ADR 0028: the event log rotates at 10 MiB into one backup, checked on every emit

Status: Accepted
Date: 2026-09-29

Chart: Mugicrew session logs tab. Settles the grilling "Retention and
rotation policy for the event log". Builds on ADR 0033 and ADR 0027.

## Context

ADR 0027 puts every event in one O_APPEND file shared by the TUI, `mugicrew
loop`, and reconcile subprocesses. Volume is small (~300 B per event, ≤ ~5 KiB
for a failure carrying a 4 KiB stderr tail; tens of KB/day), but unbounded, and
a `MUGICREW_LOG_LEVEL=debug` run can grow it fast. Long-lived processes (the
TUI runs for hours) holding an O_APPEND fd keep writing to the same inode after
a rename, so a held-open handle would write into `.1`, then into an unlinked
inode after the next rotation, silently losing events.

## Decision

1. **Size-based rotation, one backup.** At most `events.jsonl` and
   `events.jsonl.1`. Cap is a hardcoded 10 MiB; Debug lines count toward it.
2. **Every emitter rotates, before every event, with no long-lived handle.**
   Each emit: `os.Stat` the path; if ≥ 10 MiB, `os.Rename` it to `.1`
   (clobbering the old backup); then open `O_WRONLY|O_APPEND|O_CREATE`, write
   one line, close. This refines ADR 0027 §2 (which did not fix handle
   lifetime). Events are rare, so the extra syscalls are free.
3. **Fail-open.** A failed stat or rename is ignored and the emit appends to the
   live file anyway; a failed open falls under ADR 0027 §5 (discard + one-time
   warning). Rotation never fails a command.
4. **Reading.** The Logs tab reads `.1` then the live file as one stream in
   append order (append order is time order, since every write hits the
   current file), skipping unparseable lines. A missing `.1` is normal.

## Considered and rejected

- **No rotation (append forever):** zero code, but unbounded growth inside
  `.git` and a tab that must cope with an ever-larger file.
- **Per-day files with age pruning:** breaks ADR 0027's single fixed path and
  forces the tab to glob and merge.
- **Truncate in place:** fights concurrent O_APPEND writers.
- **Long-lived handle, rotated at process start or by a prune step:** loses the
  TUI's events into rotated/unlinked inodes, as above.
- **flock around stat+rename:** closes the race below for ~10 more lines; not
  worth it yet (see Consequences).
- **Configurable cap / more backups:** no evidence 10 MiB × 1 is too small.

## Consequences

- History is bounded to ~20 MiB, roughly 3–6 months at current volume; older
  history is gone, which a non-authoritative log (ADR 0033) can afford.
- **Known race:** two processes crossing the cap in the same instant can both
  rename, the second clobbering `.1` with a near-empty file and losing one
  backup's worth of old history (never current events). Accepted; the code
  carries a `ponytail:` comment naming flock on a `.lock` file as the upgrade.
