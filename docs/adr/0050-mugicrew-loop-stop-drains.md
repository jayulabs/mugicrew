# ADR 0050: mugicrew loop stop drains in-flight efforts

Status: Accepted
Date: 2026-10-04

## Context

The Loop lifecycle control Spec lets a human stop the dispatch
loop from the TUI and from `mugicrew loop stop`. Today a SIGINT/SIGTERM makes
`main` return, which kills every dispatch goroutine where it stands. In-process
efforts (spec, review) lose their post-session step. Every effort skips its
claim release (ADR 0034) and its worktree teardown, so the bead stays
`in_progress`. Nothing heartbeats or reclaims it, so with a cap of 1 it blocks
its crewmate's AFK dispatch for good. Ctrl-C on a foreground loop also reaches
the effort children, because they share the loop's process group. Decided in grilling.

## Decision

1. **Stop drains.** On a stop signal the loop stops polling and dispatching at
   once. It then waits, with **no timeout**, for every in-flight dispatch
   goroutine to finish (post-session step, claim release, teardown), and exits.
   It keeps holding the single-loop lock while it drains, so `loop start` is
   refused with "loop is draining".
   > **Amended (2026-10-16):** `loop start` on a stop drain cancels it
   > (SIGUSR1) and the loop polls again; ADR 0236
   > (mugicrew-loop-run-re-arms-from-a-cancelled-stop-drain…).
2. **Force stop exits immediately.** `loop stop --force`, a second stop while
   draining, or a second Ctrl-C exits at once, as today. In-flight effort
   processes are not signalled: worktree efforts finish on their own. Whatever
   is stranded is recovered by graph repair, not by the loop (§6).
   > **Amended (2026-10-04):** `--force` is delivered as a
   > second stop signal (SIGTERM twice), never SIGKILL, so the loop can log
   > `effort.abandoned` and `loop.forced` before exiting (ADR 0029).
   >
   > **Amended (2026-10-16):** a cancelled stop drain (§1) fully resets the stop,
   > so the next stop starts a fresh drain with its own StopWindow, never a
   > force; only a second stop past that window forces. ADR 0236
   > (mugicrew-loop-run-re-arms-from-a-cancelled-stop-drain…).
3. **Only a TUI force with live efforts asks for confirmation.** A plain stop
   never asks, because it cannot lose work. A TUI force with efforts in flight
   asks: "N efforts in flight will be abandoned. Force stop?". The CLI
   `--force` flag counts as the confirmation.
   > **Amended (2026-10-16, ADR 0236 (mugicrew-loop-run-re-arms-from-a-cancelled-stop-drain-by-signal-generation)):** the TUI dialog is three-way:
   > `y` force, `k` keep on (shells out to `loop start`, which cancels the drain)
   > and `n`/esc back. It still lists the efforts a force would abandon.
4. **The loop has three states, read from its lockfile:** `off`, `on (pid)` and
   `draining (pid, N in flight: <bead titles>)`. On a stop signal the loop
   writes `draining` into the lockfile, so `loop status` and the TUI read one
   file and derive nothing. While draining, the TUI toggle offers force stop.
   > **Amended (2026-10-16, ADR 0236 (mugicrew-loop-run-re-arms-from-a-cancelled-stop-drain-by-signal-generation)):** it also offers `k` keep
   > on, and `n`/esc goes back.
   > **Amended (2026-10-18, ADR 0295 (mugicrew-loop-status-prints-labelled-fields…)):** `loop status` prints `state: <word>` and `pid: N` as labelled fields, and a
   > drain's in-flight beads are a count (`in flight: N`), not titles.
5. **Foreground and detached loops behave the same.** Effort children start
   with `Setpgid: true`, so a terminal Ctrl-C or hangup reaches only the loop,
   and SIGHUP joins SIGINT/SIGTERM as a drain signal. A first Ctrl-C drains and
   a second one forces.
6. **Stranded-claim recovery is out of scope.** It applies after a crash,
   SIGKILL or force stop. It is split off as its own Signal, pitched as a
   system-doctor crewmate (Chopper) that detects and repairs invalid or
   transient bead-graph states. This Spec only removes the false "self-heals on
   lease expiry" claim from `loop/loop.go`'s package doc and from the
   plan-of-record memory `mugicrew-loop-off-until-tui-confidence`.

## Considered options

- **Kill (today's behaviour).** Rejected: every stop would strand claims and
  worktrees, and nothing recovers them.
- **Drain with a timeout.** Rejected: efforts are claude sessions that can run
  for tens of minutes. Any timeout cuts some off and brings back the stranded
  state on a timer. A human who wants out now uses force (§2).
- **Force also SIGTERMs effort children.** Rejected: it kills worktree efforts
  that would otherwise finish cleanly.
- **Reclaim on `loop start`, or heartbeat plus `bd reclaim`.** Deferred to the
  Chopper Signal (§6). Acting on the ps liveness view clashes with ADR 0031,
  which treats liveness as a view only, and heartbeating needs a lease TTL.
  _Amended by ADR 0083:_ the clash covers automatic action only; a
  human-issued `mugicrew abort` may signal processes the scan finds.

## Consequences

- A stop can take as long as the longest in-flight effort. `draining` makes
  that visible, and force is always available.
- Until Chopper exists, a crash, SIGKILL or force stop still needs a manual
  `bd unclaim <id>` and orphan-worktree cleanup.
- The `draining` loop state is unrelated to a **drained** container (open, all
  children closed). See the CONTEXT.md glossary.
