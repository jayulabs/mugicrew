# ADR 0063: mugicrew loop shows a restart drain as a draining variant and a stale build as a failed rev

Status: Accepted
Date: 2026-10-05
Amends: ADR 0060 §5, ADR 0051 §3–§4

Chart: Pinned loop restarts itself onto main's new build. Settles
the grilling "How do loop status and the TUI show a stale or restarting
build?". Builds on the restart lifecycle in ADR 0062.

## Context

ADR 0062 adds three phases to a pinned loop: a build that runs while the loop
still dispatches, a failed build that leaves the loop on the old build, and a
restart drain that ends in an exec rather than an exit. ADR 0060 §5 deferred
the stale flag to this Chart. The TUI is unpinned (ADR 0060 §4), so it can be
older than the loop whose lock body it reads. An unknown `state` falls to
`loopSegment`'s default case and renders as `loop off`.

## Decision

1. **A restart drain is `state: draining` plus `restart_to`.** `restart_to` is
   the full SHA of the build the loop will exec. It's written when the restart
   drain starts and cleared when a human stop turns the drain into a stop
   drain (ADR 0062 §6). There is no new state, so every existing reader keeps
   working, including `HeldError`, `loop stop`'s second-stop force and an old
   TUI.
   > **Amended (2026-10-16):** cancelling a stop drain that a stop converted from
   > a restart drain sets `restart_to` again, so the loop is back in the restart
   > drain; ADR 0236 (mugicrew-loop-run-re-arms-from-a-cancelled-stop-drain…).
2. **Only a failed build is shown as stale.** On an `on` loop, `failed_rev` is
   the full SHA whose build or eval failed (ADR 0062 §3). It's cleared when an
   unchanged outPath rewrites `rev`, and it's dropped on a restart, along with
   the in-memory failure. A build that's still running is not written to the
   body, because the `build.started` event already records it and nothing
   needs the human then.
3. **`loop status`:**
   - Stale: `on (pid N, main abc1234, /nix/store/…)` and then
     `stale: build of main def5678 failed (see build.failed in events)`.
   - Restart drain: `draining (pid N, main abc1234, /nix/store/…) → restarting
     onto main def5678`, and then the beads in flight. The first word stays
     `draining`, because it's the body's state.
   - A restart drain that a stop converted prints as a plain drain.
   > **Amended (2026-10-18, ADR 0295 (mugicrew-loop-status-prints-labelled-fields…)):** `loop status` prints these as labelled fields: `state: on (stale)` with a
   > `stale: build of main def5678 failed (see build.failed in events)` line,
   > and `state: draining` with `restarting onto: main def5678`. The in-flight
   > beads are a count, not titles. A converted restart drain still prints as a
   > plain drain.
4. **TUI tab bar (amends ADR 0051 §4):**
   - Stale: `loop on @abc1234 stale` in the warn color.
   - Restart drain: `loop restarting N @abc1234→def5678` in the warn color.
     Here "restarting" tells the human that nobody stopped the loop.
   - When the bar is narrow (`withRev=false`), it drops the revs:
     `loop on stale` and `loop restarting N`.
5. **TUI `o` toggle during a restart drain (amends ADR 0051 §3):** the footer
   says `loop off`, and `o` does a plain stop, which converts the drain to a
   stop drain. After that, `o` goes back to `force-stop` and its confirmation.
   This matches a CLI `loop stop`.
   > **Amended (2026-10-16, ADR 0236 (mugicrew-loop-run-re-arms-from-a-cancelled-stop-drain-by-signal-generation)):** after that, `o` opens the three-way
   > dialog: `y` force, `k` keep on, `n`/esc back.

## Considered options

- **A new `restarting` state:** rejected. An older TUI would render it as
  `loop off`, and every reader of `draining` would need to learn it.
- **Showing the build phase (`building: <sha>`):** rejected. It lasts about
  15–40 s, the loop keeps dispatching, and the human has nothing to do.
- **`o` always force-stops during any drain:** rejected. A human couldn't
  cancel a restart without abandoning the efforts in flight, and it would
  disagree with ADR 0062 §6.
- **A "cancel restart, keep running" action:** rejected. ADR 0062 §7 has no
  switch, and the next tick would start the restart again.
- **Tab bar shows `loop draining N @old→new`:** rejected. It reads as if
  someone pressed stop.

## Consequences

- An older TUI shows a restart drain as `loop draining N @abc1234` and doesn't
  show stale. That's degraded, but it's never wrong.
- `loop status --json` / `Status` gains `restart_to` and `failed_rev`.
