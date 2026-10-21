# The dispatcher holds a per-effort flock that liveness try-locks

Status: accepted

ADR 0025's argv scan reads an AFK effort as dead while the dispatcher holds the
claim with no `reconcile`/`claude` child running: before the worktree cut, and
after the child exits (integrate, retire, publish, release). To cover those
windows, the dispatcher holds a flock on `<BEADS_DIR>/mugicrew-efforts/<id>.lock`
from before the claim until after the release. `liveness.Alive` reads an effort
as alive if the argv scan matches it **or** the lock is held. This amends ADR
0025 §1–2 and keeps ADR 0031/0032: the kernel lock table is OS truth that a cold
reader can probe, and nothing is written to the graph.

- **Path.** Lock files go under the Bead Graph (BEADS_DIR) rather than the git
  common dir, like the loop lock (ADR 0089). Bead ids are unique per graph, so
  every Workspace on the graph sees the same hold.
- **Holder.** Open with `O_RDWR|O_CREATE` (Go's default `O_CLOEXEC` stays on, so
  child processes never inherit the lock, with one deliberate exception: the
  TUI clears it on a run-now hold's fd for the spawned run-now host only, and
  closes its own copy; see ADR 0330 (mugicrew-a-run-now-host-inherits-the-effort-flock-by-fd…).
  The host re-arms it, so reconcile and claude children still never inherit it). Lock with `flock(LOCK_EX|LOCK_NB)`,
  retried on `EWOULDBLOCK`/`EINTR` every few ms up to a short cap, as `git.go`
  does for the land lock. A probe briefly takes a free lock, and a single-shot
  acquire lost about a third of its attempts to hammering readers. The retried
  acquire lost none. Running out the cap means another dispatcher holds the bead.
- **Reader.** Open `O_RDONLY` without creating the file, then
  `flock(LOCK_SH|LOCK_NB)`. If the open fails (no file), or the lock is acquired
  (then dropped on close), the effort is dead. `EWOULDBLOCK` means alive. The
  probe never blocks (≤0.3ms against a held lock), and shared probes don't steal
  from each other. Because flock locks belong to the open file description, a
  TUI that holds the lock and probes from the same process still reads alive.
- **Leftover files.** Lock files are never deleted. Only a single inode per path
  is free of races. Deleting on release, even with an inode re-check, can let a
  reader that opened the old inode read dead while a new holder holds the
  replacement. Deleting on probe removes the file by name and gave two holders at
  once. The cost is one empty file per dispatched bead.

## Consequences

The kernel drops the lock the moment the holder dies (the first probe after a
SIGKILL read dead within about 17µs), even if an orphaned child keeps running.
That child is still visible to the argv scan. Lineage: the measurements are on
`refs/mugicrew/prototypes/mc-e8gn` (`apps/mugicrew/liveness/prototype_flockhold`).
