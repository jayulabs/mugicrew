# ADR 0053: mugicrew detached loop — a flock'd loop.lock is the single-loop authority, `loop start` is a setsid re-exec

Status: Accepted
Date: 2026-10-04
Amended by: ADR 0060 (§2: both loop verbs exec main's pinned store build)

## Context

The Loop lifecycle control Spec wants at most one dispatch loop
per repo. That loop must be discoverable and startable/stoppable from the TUI
and from `mugicrew loop start|stop|status`, and a TUI-started loop must outlive
the TUI. ADR 0050 (stop drains) and ADR 0051 (TUI toggle) already rely on "the
single-loop lock" and a lockfile that carries `draining`, but no record of that
decision existed. Decided in grilling.

## Decision

1. **The lock is the only single-loop authority.** `runLoop` takes
   `flock(LOCK_EX|LOCK_NB)` on `<git-common-dir>/mugicrew/loop.lock`. Every
   loop takes it, foreground or detached. If the lock is already held, the
   start is refused. The file holds the pid and the state (`on`/`draining`,
   ADR 0050), and those are valid only while the lock is held. Dispatch never
   reads it, so it is not coordination state (ADR 0031). No stale-lock code is
   needed: the kernel drops the lock on any death, and `O_CLOEXEC` keeps
   efforts from inheriting it.
2. **`loop start` re-execs `mugicrew loop` under `setsid`.** The loop becomes
   its own session leader, so it survives TUI quit and tab close. `loop status`
   tries the lock: a held lock means on, and the pid/state come from the file.
   `loop stop` sends SIGTERM to that pid, and a second stop forces (ADR 0050).
   The TUI shells out to the same verbs (ADR 0051), so it toggles the loop and
   never owns it.
3. **Detached stdio goes to `<git-common-dir>/mugicrew/loop.out`, next to
   `events.jsonl`.** The file is truncated on each `loop start` and never
   rotated. Every loop outcome is already an event. This file exists for what
   the event log cannot hold: Go panic traces, fatal runtime errors, and raw
   output that child tools write to the inherited fds (including the
   `reconcile` child, `dispatch.go:185`). One run's worth is enough, so ADR 0028
   rotation does not apply. A foreground `mugicrew loop` still writes to its
   terminal.
4. **The detached loop inherits its starter's env.** That is the TUI's or the
   CLI's, as a foreground loop does today. `loop stop`/`status` need only the
   lockfile, so their env does not matter. To change the loop's env, restart
   the loop.
5. **Known limit, no code: a detached loop dies with the WSL distro.** This
   happens on `wsl --shutdown`/terminate/reboot, and on the idle shutdown that
   comes about 15s after the last *Windows* client leaves. Linux processes never
   keep the distro alive. The opt-in fix is `instanceIdleTimeout=-1` in
   `.wslconfig`. The tmux session table (ADR 0036) has the same limit.

## Considered options

- **tmux-hosted loop (as ADR 0036 hosts sessions):** rejected. It cannot see a
  foreground loop, every stop would signal in-flight efforts, and it would need
  an ADR 0036 amendment, an env handoff and dead-pane handling.
- **Process-table scan as the enforcer (ADR 0025 style):** rejected as the
  enforcer because a scan and a start can race. It stays available as a
  cross-check view.
- **Detached stdio to `/dev/null`:** rejected, because it loses the only trace
  of a panic. A rotated log (ADR 0028) was rejected as unneeded machinery for
  one run's output.
- **systemd user unit to survive WSL idle:** rejected. Linux processes don't
  hold the VM open, so it would not help.
