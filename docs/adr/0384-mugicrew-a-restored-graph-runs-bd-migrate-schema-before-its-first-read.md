# ADR 0384: A restored graph runs `bd migrate schema` before its first read

Status: Accepted
Date: 2026-10-28

Chart mc-wv2il (A Bead Graph restored from its backup remote opens every bead).
Settles prototype mc-wv2il.2. Builds on ADR 0369
(mugicrew-the-restore-drill-clones-the-pushed-backup…).

## Decision

After the restore drill clones the backup into the scratch graph and starts its
server (`bd dolt start`), it runs one more command, `bd migrate schema`, before
any read. That recreates, empty, every table bd lists in `dolt_ignore` (13 on
bd 1.3.0: leases, events, wisps, the wisp_* tables, ignored_schema_migrations and
the rest), which a push never carries. Then `bd show`, `bd ready` and `bd list`
work on the restored graph. Being one bd command with no hand-written SQL, it is
a documented drill step, not a `mugicrew backup restore` verb.

Why it works on bd 1.3.0: read-only opens (`bd count`, `show`, `ready`, `list`)
skip migrations, and their behind-drift check reads only the pushed
`schema_migrations`, which is current. Any writable open replays the ignored
migration lane from scratch, because its cursor table `ignored_schema_migrations`
is itself ignored and so absent on a clone. `bd migrate schema` is the explicit
writable open: it prints "Schema already at v66" yet creates the tables, and
leaves the working set clean. The read path not reporting this is a bd fault,
filed separately; the step stays until a pinned bd fixes it.

## Considered Options

- **`bd migrate`** works but also rewrites the version metadata.
- **`bd doctor --fix --yes`** works but also applies unrelated fixes (metadata,
  fingerprint, the Claude plugin, AGENTS.md/CLAUDE.md divergence).
- **Keeping the `bd init` DB and fetching plus `reset --hard` onto
  `remotes/backup/main`**: its ignored tables survive the reset, but it trades
  the clone for four raw Dolt steps and keeps local_metadata minted for another
  lineage.
- **Copying `SHOW CREATE TABLE` DDL**: hand-written SQL, and a plain copy fails on
  foreign-key order (wisp_child_counters before wisps).
- **Relying on whatever writable verb runs first** (such as `bd config get`):
  works by accident; it is why a careless scratch try hides the bug.
