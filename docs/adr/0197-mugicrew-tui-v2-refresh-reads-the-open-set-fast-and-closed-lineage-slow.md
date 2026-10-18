# ADR 0197: The v2 TUI refresh reads the open set fast and closed lineage slow

Status: Accepted; amends ADR 0153 §1
Amended by ADR 0232 (§1–§2: the slow tier, Graph entry and drop-out read the default closed set plus claimed on-demand beads by id, not `bd list --all`).
Date: 2026-10-15

Chart mc-wg4f (TUI refresh stays responsive and light under load). The
baseline prototype (mc-wg4f.2) and the bd read-path research
(docs/research/bd-cheap-read-paths-for-tui-refresh.md, mc-wg4f.3) found the
per-refresh `bd list --all -n 0 --json` to be the TUI's main host load: about
360 ms of client CPU plus about 300 ms of dolt CPU every 3 s, and 97% of its
7.9 MB is closed beads.

## Decision

ADR 0153 §1 said server mode is the whole fix, with no call-volume cuts and
no read cache. For the v2 TUI refresh, that no longer holds:

1. **Two tiers.** Every 3 s the TUI reads only the open set
   (`bd list --status open,in_progress,blocked,deferred`). Every 30 s it
   re-reads closed lineage with `bd list --all`. It also re-reads it at
   startup and on entering the Graph tab. Between reads, closed beads come
   from the last `--all` result: this is a read cache.
2. **A drop-out triggers a read.** If a bead was in the previous open set and
   is missing from the current one (closed or deleted), the TUI runs an
   immediate closed-lineage read. This keeps the Graph, and the HITL
   done-flip that keys on `Status == "closed"`, fresh within one fast tick.
3. The other slow-moving reads also move to the 30 s tier: History (mainline
   `git log`), kind profiles, pool and auto-exit, and efforts across repos.
   Sessions and waiting state, the loop lock and tip, and liveness stay on
   the 3 s tier.

ADR 0153 §§2–4 stand: mugicrew still starts no bd process of its own.

## Considered and rejected

- **Gate `--all` on `dolt_hashof_db()`.** Under crew load the hash moves
  every few seconds, so the gate rarely skips a read. It also still pays the
  roughly 0.25 s bd process floor.
- **`bd serve` HTTP or the events journal.** Both are cheaper per read. But
  `bd serve` is a long-lived process mugicrew would have to supervise, which
  ADR 0153 §2 rejects, and it leaks a db-proxy child on SIGTERM. The journal
  needs a workspace config write and is disabled today.
- **`--updated-after` deltas.** They miss edge writes, label writes and
  deletes, none of which bump `updated_at`.
