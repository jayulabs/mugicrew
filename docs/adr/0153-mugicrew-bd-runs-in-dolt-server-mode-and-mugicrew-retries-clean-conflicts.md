# ADR 0153: mugicrew bd runs in Dolt server mode: bd's own autostart is the lifecycle, and mugicrew retries clean conflicts a bounded 3 times

Status: Accepted; amends ADR 0070
Date: 2026-10-13

## Context

In embedded mode every bd store call takes Dolt's exclusive LOCK for about
1.2 s, whatever the verb. A replay of seeded Cap 6 load measured wait p95 at
24.8 s. Call cuts, compaction and the two combined still failed or only just
met the Spec gate (p95 lock wait under 1 s, zero lock failures). Server mode
measured 0.10 s (docs/research/bd-cap6-replay-load-candidates.md). Server mode
keeps the claim CAS (ADR 0052) and batch atomicity (ADR 0059). It breaks ADR
0070's premise that "no write ever reports a conflict": `bd batch` and
`--graph` can exit 1 on `1213 (40001)`, and a concurrent `bd create --parent`
can mint a child ID that already exists. Both fail cleanly, nothing is written,
and running them again is safe
(docs/research/bd-server-mode-write-guarantees-and-operations.md).

Decided in grilling (mc-na37.5).

## Decision

1. **Server mode is the fix.** It is the whole fix: no call-volume cuts, no
   read cache, and no compaction stopgap. The migration compacts the store
   anyway.
2. **bd's autostart counts as mugicrew owning the lifecycle.** bd starts a
   detached server on the first call. There is one per repo, linked worktrees
   share it, a start lock prevents two starting at once, and the next call
   restarts it after a kill or a WSL sleep. mugicrew adds no start, stop or
   supervision of its own.
3. **Bounded retry, amending ADR 0070.** mugicrew's single `run` helper and
   `runBatch` retry a failed call up to 3 attempts with short jittered backoff
   (about 0.2, 0.5 and 1 s). They retry only when the output contains
   `1213 (40001)`, `already exists`, `connection refused` or
   `circuit breaker is open`. After the last attempt the error returns as
   today. An error naming `indeterminate` or `stage and commit after regular
   SQL commit` is never retried, as ADR 0070 already requires. The crew docs
   that make agents run `bd create --parent` tell them to re-run the same
   create on `already exists` or `1213`.
4. **Mode-agnostic.** mugicrew runs on embedded or server stores, and the
   retries do no harm under embedded. Migrating is a one-off manual step:
   `bd backup`, then `bd init --server`, then `bd backup restore`, with the loop
   stopped, taking about 30 s. mugicrew does not force it. New Workspaces are
   steered to server mode by the no-graph hint and the README: use
   `bd init --server` for Cap > 1.

## Considered and rejected

- **Compacted embedded mode plus call cuts.** At 1.00 s p95 it has no margin,
  the store grows back, and it was not tested above Cap 6.
- **A bd shim on PATH that retries for every caller.** It would be a second bd
  that has to be kept in step with the real one. Two Go helpers and one
  crew-doc line cover the same callers.
- **mugicrew supervising the server, or stopping it when the loop stops.** The
  next TUI read would just autostart it again. Supervision duplicates what bd
  already does.
- **A `mugicrew` migrate verb, or refusing embedded mode above Cap 1.**
  Migration is a single 30 s operation, and embedded mode still works at low
  Caps.

## Consequences

- If a store is never migrated, it stays embedded and slow at Cap 6.
  `bd init` defaults to embedded mode.
- A retried `--graph` can still create under a parent that was closed in the
  meantime (ADR 0070 Consequences). mugicrew's Go code does not use `--graph`.
