# ADR 0367: A Bead Graph backs up off-machine by a Dolt push to a remote named `backup`

Status: Accepted
Date: 2026-10-28

Chart mc-wmn0w (A user's Bead Graph backs up off-machine to a Dolt destination
they name). Settles grilling mc-wmn0w.5. Amends ADR 0300 §1
(bead-graph-data-never-goes-to-a-project-remote).

## Decision

1. **The off-machine copy is a Dolt push, for every destination kind.** It is
   `CALL DOLT_PUSH(['--user', <user>,] 'backup', 'main')` from the bd-managed
   sql-server, to a Dolt remote with the fixed name `backup`, added once per clone
   with `CALL DOLT_REMOTE('add', 'backup', '<url>')`. file:// (an off-machine mount)
   and a credentialed remotesapi both go this way; `--user` is passed only when a
   remote user is set. ADR 0300 §1 now reads: a graph leaves the machine only as a
   `bd backup`, a `bd sync`, or this push, each to a destination the user names.
2. **No Dolt or bd bump.** Dolt 2.3.4 and bd 1.3.0 stay pinned: 2.6.1's backup,
   push and remotesapi auth code is byte-identical, so a bump fixes nothing.
3. **Credentials are per user, outside every repo.** `DOLT_REMOTE_PASSWORD` and,
   for a remotesapi, `DOLT_REMOTE_USER` live in `~/.config/mugicrew/backup.env`
   (mode 0600), which the Workspace devShell sources when present, so the
   bd-spawned sql-server inherits them at start. Any other export that reaches the
   server's env works too. Never repo config, never `.beads/.env`.
   `DOLT_REMOTE_USER` is our convention: Dolt itself never reads it; the push
   passes it as `--user`.
4. **Doctor needs no recorded time.** No `backup` remote means no off-machine
   backup is configured. The backup is stale when the oldest commit in
   `dolt_log('remotes/backup/main..main')` is over 24 h old; with nothing unpushed
   it is fresh however long ago the last push ran, so an idle graph never warns.
   No timestamp file is written.
5. **The local file:// `bd backup` is left alone.** It keeps bd's `default`
   backup and `dolt.last_sync`, is an optional complement (the only copy whose
   restore keeps the tables bd lists in `dolt_ignore`, such as `events`), and
   doctor neither counts it as off-machine nor warns about it.

## Considered Options

- **`bd backup sync` everywhere.** Keeps `last_sync` and `bd backup status`, but
  reaches a credentialed remotesapi only through a hand-set Dolt-internal param
  (`__DOLT__grpc_username` in `repo_state.json`) that `bd backup init` silently
  wipes; plain, it authenticates as root.
- **Split by destination kind** (`bd backup sync` for file://, push for a
  remotesapi). Gives file:// a complete restore, at the cost of two mechanisms, two
  staleness reads, and the off-machine copy taking bd's single `default` backup
  from the local one.
- **A timestamp file written after each push.** Ties doctor to whatever makes
  the push recur and warns on an idle graph; the unpushed-commit age measures what
  is actually at risk.

## Consequences

- A restore from the push is a `dolt clone` (plus a `project_id` fix), so the
  tables bd lists in `dolt_ignore` (`events`, the audit trail, among them) are
  not carried. Accepted for a disaster copy.
- The first push to a fresh destination database needs a one-time `--force`
  (no common ancestor), and a remotesapi push never creates the database.
- A broken credential surfaces as staleness: unpushed commits age past 24 h.
