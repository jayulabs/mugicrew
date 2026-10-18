# ADR 0151: The land gate is observed through gate.* events only

Status: Accepted
Date: 2026-10-12

Chart mc-fv9r ("Lands refuse a commit whose pinned nix build fails"), grilling
mc-fv9r.6. A gated Checkpoint Landing now spends ~4m in an uncached nix build
(ADR 0150). The question was how that build is observed: which event-log entries
it writes, and whether the TUI or `loop status` shows a Landing that is building.

## Decision

1. **Own namespace, `gate.*`.** The loop's self-rebuild keeps `build.*` (rev, no
   bead). The gate's events carry a bead and outcomes the self-rebuild lacks.
   Every gate event carries the Landing's bead attrs plus `pin`, `base`,
   `composed` (the squash sha) and `attempt` (1–3):

   | event | level | extra fields |
   |---|---|---|
   | `gate.started` | Info | `ref` (refs/mugicrew/gate/<id>) |
   | `gate.passed` | Info | `dur_ms`, `out` (store path) |
   | `gate.failed` (builder failure) | Error | `dur_ms`, `drv`, `critique` (filed id, or `deduped`) |
   | `gate.infra` | Error | `dur_ms`, `err` (stderr tail, 4 KiB), `timeout` |
   | `gate.pending` (3 base moves, ErrLandPending) | Warn | none |

   A moved base has no event of its own: the next `gate.started` has
   `attempt` > 1. `land.done` / `land.failed` follow as before.
2. **Cache hits are read off `dur_ms`.** There is no `cached` field: 3s vs ~250s
   is plain to a reader. Add one (via `nix path-info` before the build) when
   something needs to act on it.
3. **No TUI or `loop status` change.** The loop claims the Landing in_progress
   for the reconcile, so its Graph row already shows it alive and aging. A
   Landing in_progress for minutes is the building state. Deriving a `building`
   tag from the log would make the log authoritative, against ADR 0033.
4. **No new surface for a stuck gate.** An infra failure is a returned error, so
   it counts toward `paused:failing` like any failed effort: after 3 in a row
   (PauseAfter) the Landing is paused, and that pause is how a stuck gate gets
   noticed. ErrLandPending returns nil and does not count. A busy main clears
   itself, so `gate.pending` at Warn is enough.

## Considered Options

- Reusing `build.*`: one filter for all nix builds, but it mixes two event
  shapes and loses the builder/infra split.
- A `building` tag on the Landing row, or listing it in `loop status`: rejected
  per §3.
- Exempting infra failures from `paused:failing` like ErrLandPending: rejected,
  because it would remove the one surface that already catches a stuck gate.
