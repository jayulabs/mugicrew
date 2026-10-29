# ADR 0370: An off-machine backup recurs hourly from the loop through `mugicrew backup push`

Status: Accepted
Date: 2026-10-28

Chart mc-wmn0w (A user's Bead Graph backs up off-machine to a Dolt destination
they name). Settles grilling mc-wmn0w.6. Builds on ADR 0367
(a-bead-graph-backs-up-off-machine-by-dolt-push…).

## Decision

1. **One push is a mugicrew verb, `mugicrew backup push`.** It runs ADR 0367's
   `CALL DOLT_PUSH(['--user', $DOLT_REMOTE_USER,] 'backup', 'main')` against the
   bd-managed sql-server over a connection with no 10 s client cap (`bd sql`
   times out first on a large graph). It is a no-op when
   `dolt_log('remotes/backup/main..main')` is empty. It takes a non-blocking lock
   under `$(git rev-parse --git-common-dir)/mugicrew/`; when the lock is held it
   exits 0 with no event. It never starts or restarts the sql-server: a server
   without the credential in its env fails the push, and that is reported.
   Run by hand with no `backup` remote, it exits non-zero with a hint to
   install.md's Backup section. The namespace leaves room for siblings (such as
   a first-push seeding verb); the restore drill stays a documented procedure.
2. **The loop makes it recur, hourly.** While the loop process runs in any state
   but off, it runs the verb every hour, a constant like doctor's 24 h staleness.
   With no `backup` remote the timer does nothing, silently: doctor already warns
   that no off-machine backup is configured.
3. **The OS scheduler is the documented fallback.** install.md documents an
   hourly systemd user timer (its unit sets
   `EnvironmentFile=%h/.config/mugicrew/backup.env`, so a server bd starts from it
   inherits the credential) or a cron line, both calling the same verb, for
   machines that keep no loop running. The lock makes the two triggers safe
   together.
4. **A failed run surfaces in the event log and the TUI.** Every attempt that
   pushes writes `backup.pushed` or `backup.failed` (with the error) to the event
   log, whichever trigger ran it, and a failure exits non-zero (the journal or
   cron mail on the OS path). The TUI's status bar marks a failed latest attempt,
   read from the latest `backup.*` event, so it survives TUI restarts and shows
   OS-timer failures; the next success clears it. Doctor is unchanged: it warns
   only when no backup is configured or the backup is over 24 h stale.

## Considered Options

- **bd auto-backup.** In server mode it only syncs to the local `.beads/backup`
  and drives `bd backup sync`, never a push; it cannot reach the destination.
- **A documented push recipe, no verb.** Drifts, falls into `bd sql`'s 10 s
  timeout, and leaves nowhere to record a success or failure.
- **Push after each Land.** Misses graphs that change without Landing; an hourly
  timer already covers a Land within the hour.
- **OS scheduler only.** Mostly duplicates what a running loop can do, and puts
  every user through the server-env trap; kept as the fallback instead.
