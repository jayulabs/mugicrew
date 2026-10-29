# ADR 0368: A backup destination is seeded by hand, and only a human ever forces a push

Status: Accepted
Date: 2026-10-28

Chart mc-wmn0w (A user's Bead Graph backs up off-machine to a Dolt destination
they name). Settles grilling mc-wmn0w.8. Builds on ADR 0367
(a-bead-graph-backs-up-off-machine-by-dolt-push…).

## Decision

1. **The user creates a remotesapi destination by hand.** A remotesapi push never
   creates its database, so the user, as the Dolt host's admin, runs
   `CREATE DATABASE`, creates the push user and grants it (SUPER, per
   docs/research/off-machine-backup-mechanisms-measured.md). install.md's Backup
   section gives the SQL. mugicrew never administers the destination. A file://
   destination needs no seed: the push creates the directory.
2. **The first `--force` is the human's, at setup.** An empty `CREATE DATABASE`
   shares no ancestor with the graph, so install.md's one-time setup runs the first
   push by hand with `--force`. Whatever makes the backup recur never passes
   `--force`. Without it Dolt rejects a non-fast-forward push, so a later push
   cannot overwrite a destination that has diverged.
3. **One destination database per machine's graph.** install.md suggests naming
   it after the graph's database (e.g. `<graph-db>`), adding `_<hostname>` only
   when a second machine backs up the same graph. A rejected (non-fast-forward)
   push is resolved by the human: inspect, then switch to a fresh database or
   deliberately `--force` once.
4. **install.md explains the errors; nothing else does.** Its Backup section
   carries a troubleshooting list for `database not found`, `no common ancestor`
   and the non-fast-forward rejection, and doctor's unconfigured and stale hints
   point to it. doctor does not probe the destination, and the recurring push does
   not rewrap Dolt's errors.

## Considered Options

- **Force automatically while no `remotes/backup/main` tracking ref exists.**
  Nobody needs to remember the setup push, but a fresh clone or restore has no
  tracking ref, so the recurrence could force over a diverged backup.
- **Force only when the remote `main` is a lone empty init commit**, checked by
  fetch. Safe, but extra machinery for a step that happens once per destination.
- **A mugicrew step creates the destination database.** Needs admin credentials
  and the destination's SQL port, not just its remotesapi URL.
