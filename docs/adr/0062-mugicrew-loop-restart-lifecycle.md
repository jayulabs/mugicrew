# ADR 0062: mugicrew loop restart — build first, then drain, then exec in place holding the lock

Status: Accepted
Date: 2026-10-05
Amends: ADR 0050, ADR 0060 §5, ADR 0029 §5

## Context

The Chart "Pinned loop restarts itself onto main's new build" has the
pinned loop (ADR 0060) notice on its own tick that main's mugicrew build has
changed, and restart onto it. A restart is a drain (ADR 0050) followed by an
exec, keeping the single-loop lock. The research
measured the facts this decision rests on. The outPath eval takes about
1.2–2.6 s, and a Go-only rebuild takes about 15–16 s (about 40 s with
checkPhase). A failed build is not cached and leaves the `loop-build` out-link
untouched. `looplock.MainlineBuild` can run from a live loop. The lock fd is
opened close-on-exec, so a plain exec drops the flock. Decided in grilling.

## Decision

1. **Detect cheaply, then build before draining.** Each tick compares main's
   SHA with the loop's `rev`. Only when it has moved does the loop run
   `nix eval` on main's outPath. If the outPath differs, the loop builds and
   roots that exact rev (`MainlineBuild`) **while still dispatching**. Eval and
   build run off the tick goroutine, with at most one in flight. Only a
   successful build starts the drain, so the exec after it is instant.
2. **Rev moved, outPath unchanged:** the loop rewrites `rev` in the lock body
   to main's new SHA. The binary is identical, so "this build = main@rev"
   still holds. Nothing is logged.
3. **A failed build or eval keeps the old build running.** The loop remembers
   the failed rev in memory and does not retry until main moves to another
   rev. It logs `build.failed` and flags the build as stale (the display is
   decided in ADR 0063). A restarted loop forgets the failure, so a manual
   restart retries.
   > **Amended (2026-10-17):** the manual restart is `loop restart` (ADR 0289
   > (mugicrew-loop-restart-asks-by-sigusr2…)). It clears the in-memory failed
   > rev and retries without an exec first.
4. **No chasing main.** If main moves again while the build or the drain is
   running, the loop still drains and execs the build it made. The new image's
   first tick sees the newer rev and starts the next restart. An in-flight
   build is never discarded for a newer rev.
5. **Exec in place, holding the lock.** Foreground and detached loops both
   `syscall.Exec` the new store path (same pid, terminal and session; ADR 0050
   §5, ADR 0060 §1). Before the exec, the loop clears close-on-exec on the lock
   fd and passes its fd number to the new image next to the hidden pinned
   marker and the rev. The new image adopts that fd instead of re-acquiring, so
   the flock never lapses and no concurrent `loop start` can slip in.
6. **A human stop wins over a restart.** A stop during a restart drain turns it
   into a stop drain: when it finishes, the loop exits instead of exec'ing. A
   second stop forces, as in ADR 0050 §2. A stop drain never turns into a
   restart. A stop during a build that's still running cancels the `nix` child
   and doesn't wait for it (ADR 0064 §5).
   > **Amended (2026-10-16):** the stop keeps the built restart, so cancelling
   > that stop drain reverts it to the restart drain; ADR 0236
   > (mugicrew-loop-run-re-arms-from-a-cancelled-stop-drain…).
7. **Always on, no switch.** There is no flag or env var to turn off
   auto-restart. To hold a build, stop the loop. This follows ADR 0060 §2: a
   loop sitting on a stale build is the skew that ADR removed.
   > **Amended (2026-10-17):** automatic restart is still always on for a
   > watched loop, with no switch. `loop restart` adds on-demand restarts on
   > any from-source loop and is not a switch.
8. **Events (amends ADR 0029 §5):**

   | Event | Level | Extra attrs |
   |---|---|---|
   | `build.started` | Info | `rev` |
   | `build.done` | Info | `rev`, `store_path`, `dur_ms` |
   | `build.failed` | Error | `rev`, `dur_ms`, `err` (nix stderr, last 4 KiB at a line boundary) |
   | `loop.draining` | Info | adds `restart_to` (rev) on a restart drain |
   | `loop.restarting` | Info | `rev`, `store_path`, `dur_ms` (old image's uptime); written just before the exec |
   | `loop.started` | Info | adds `restarted=true` in the new image |

   `build.*` is its own noun, so the launcher's build can emit it too. A failed
   eval is logged as `build.failed`.

## Considered options

- **Drain, then build:** rejected. The loop would sit idle for the whole build
  time, and a failed build would have drained for nothing.
- **Retry a failed rev on a backoff, or stop the loop:** rejected. The build
  pins an exact rev, so a retry fails the same way and costs about 40 s each
  time. Stopping turns a broken land into a dispatch outage.
- **Rebuild the newest rev at exec, or abort the drain when main moves:**
  rejected. Either one lengthens or wastes the gap, and they never settle while
  lands keep coming. The tick check already handles the next move.
- **Leave `rev` as the launch rev when the outPath is unchanged:** rejected.
  The TUI and status would show every docs land as stale.
- **Accept the exec-to-reacquire gap:** rejected. A racing start could take the
  lock, and the restarting image would then exit silently. Handing over the fd
  costs a few lines.
- **A `--no-restart` switch, or off by default:** rejected (§7). Add one if a
  real need shows up.
- **Filter the flake `src` so `apps/mugicrew/CONTEXT.md` edits stop changing
  the outPath:** split off as its own Signal. An unneeded restart only costs a
  drain gap, and the filter has to keep every `//go:embed` input.

## Consequences

- About 1 in 6 lands (edits to `apps/mugicrew/CONTEXT.md`) restarts the loop
  onto a byte-identical binary until the `src` filter Signal lands.
- Every dispatch gap is one drain long, which can be tens of minutes. That gap
  is the price of the single-lock invariant (Hand-off restarts are out of
  scope on the Chart).
- The lock body's `rev` now tracks main for as long as the outPath is
  unchanged, not just the launch rev (amends ADR 0060 §5).
- No loop can restart until `packages.mugicrew` builds on main.
