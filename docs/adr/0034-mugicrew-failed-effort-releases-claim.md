# ADR 0034: mugicrew failed-effort claim release — caller-side, guarded by a status re-read

Status: Accepted (amended 2026-10-04: release on every ended effort, not only failures)
Date: 2026-09-29

## Context

The caller (loop or TUI run-now) claims a bead `in_progress` before dispatch. On a
failed effort nothing released that claim, so the bead left `bd ready` and was
stranded until a manual `bd unclaim` (a Signal). The Chart-stranding fix
put its release in the handler, reasoning that the caller cannot tell
a handler that closed the bead from one that did not — and unclaiming a closed
bead would reopen it.

## Decision

1. **Release lives once, in `dispatch.Dispatch`'s error path**, not per handler —
   both launch paths already route through it, so every failure exit (unroutable,
   worktree cut, reconcile subprocess, in-process `crew.Run`) is covered and
   future handlers inherit it.
2. **Guarded by a status re-read.** On error the caller re-reads the bead and
   releases the claim only if it is still `in_progress`. A bead the handler
   closed keeps its close; one a handler already released (now `open`) is left
   alone. The effort is still recorded `effort.failed`. This removes the
   reason for handler-side release.
3. **Fail safe when the re-read fails:** record `claim.failed` and leave the
   claim — today's manual-unclaim behavior, never a blind release.
4. **A released failure is plain `open`.** No backoff or parking here; with the
   loop off (plan of record) re-runs are human-paced. Retry-storm protection is
   a prerequisite for turning the loop on, tracked separately.

## Consequences

- Failed AFK efforts self-heal back onto the frontier; `t` works again after a
  failure.
- **Known ceiling:** with the loop on, a deterministically failing bead would
  re-dispatch every tick until retry-storm protection lands.
- The Chart-stranding fix's handler-side unclaim on its non-terminal success branch stays —
  this ADR covers failures only.

## Amendment (2026-10-04)

Scoping release to failures stranded non-terminal successes: Jinbe's Checkpoint
land returns `nil` after filing a `kind:conflict`, leaving the Checkpoint
`in_progress`. The loop reads `bd ready`, so it never re-landed after Franky
closed the conflict (seen in practice).

**Release now runs on every ended effort, success included**, under the same
re-read guard: a handler that closed its bead keeps the close; one still
`in_progress` is released. Once `Dispatch` returns no process holds the bead,
so no AFK handler ends with a meaningful claim. The Chart-stranding fix's handler-side unclaim
in Nami's promote-or-stamp branch is removed. Handlers no longer release their
own claims.

The retry-storm ceiling now also covers a handler that keeps returning `nil`
without changing the graph. The loop's candidate checks (open conflict/critique
children, approval at head, drain) are what keep such beads off the frontier.
