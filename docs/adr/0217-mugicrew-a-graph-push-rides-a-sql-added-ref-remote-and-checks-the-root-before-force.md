# ADR 0217: A Graph push rides a SQL-added ref remote and checks the root before --force

Status: Superseded by ADR 0300
Date: 2026-10-15

Chart mc-t72m.16 (A personal Bead Graph publishes to its per-username ref on the
Forge). Settles prototype mc-t72m.16.3, which ran the route from
`docs/research/dolt-git-remote-custom-ref-push-and-fetch.md` end to end
through bd 1.3.0 in server mode (scratch graphs, a local bare repo as the
Forge; the throwaway script and its run log are on
`refs/mugicrew/prototypes/mc-t72m.16`).

## Decision

1. **Route.** The Graph ref remote is added once with
   `bd sql "CALL DOLT_REMOTE('add','<name ≠ origin>',<forge url>,'--ref','refs/mugicrew/graphs/<graph-name>')"`
   and pushed with `bd dolt push --remote <name> [--force]`. Proven: the ref
   is written, no `refs/dolt/data` is created on the Forge, and `sync.remote`
   and the sync remote's `refs/dolt/data` stay byte-identical.
2. **The push runs inside the bd sql-server.** In server mode the dolt CLI
   that bd shells out to forwards to the running server, so the push takes
   the server's `git_ref` param (the research hazard of `EnsureCLIRemote`
   re-adding the remote without `--ref` did not arise) and the **server's**
   environment. `DOLT_REMOTE_INFO_BRANCH=` set on `bd dolt push` alone does
   nothing; it suppresses `refs/heads/__dolt_remote_info__` only when it was
   in the environment that started the sql-server. Whoever starts the server
   for a graph that Graph pushes must set it.
3. **Check the root before every `--force`.** A `--force` Graph push from an
   unrelated graph silently replaced another graph's ref in the prototype.
   Before forcing, `CALL DOLT_FETCH('<name>')` then read the remote root
   (`SELECT commit_hash FROM dolt_log('remotes/<name>/main') WHERE commit_hash
   IN (SELECT commit_hash FROM dolt_commit_ancestors WHERE parent_hash IS NULL)`)
   and refuse unless it equals the local Graph id. A plain push already
   refuses a foreign graph (`no common ancestor`, ref unchanged) and a
   compacted own graph (`behind its remote counterpart`); after `bd compact`
   the Graph id is unchanged and `--force` succeeds.
4. **Read-back.** In a scratch bd server, `CALL DOLT_CLONE('--ref',
   'refs/mugicrew/graphs/<graph-name>', <forge url>, '<db>')`, then point the
   scratch `.beads/metadata.json` at it (`dolt_database` = `<db>`,
   `project_id` = the clone's `metadata._project_id`, else bd refuses with
   PROJECT IDENTITY MISMATCH), then `bd migrate schema` before any read: the
   `dolt_ignore`d tables (`leases`, `events`, …) never ride the ref, and the
   first `bd list` otherwise fails with `table not found: leases`. Root and
   head hashes then match the source and `bd list` reads the issues.

## Considered Options

- **Publish the root beside the data** (sibling ref or a ref name carrying the
  root) so the pre-force check needs no fetch. Not taken: the fetch cost 0.3–0.7 s on
  a scratch graph, a same-graph fetch moves only missing chunks, and a
  foreign-graph fetch costs a clone only in the case about to be refused.
- **Pass `DOLT_REMOTE_INFO_BRANCH=` per push.** Rejected: proven inert in
  server mode (§2).

## Consequences

- Not yet proven against a real GitLab (custom refs outside `refs/heads` over
  SSH, housekeeping), nor with a sql-server shared by several graphs.
- bd has no verb for §1 or §4; mugicrew owns that wiring until bd grows one.
