# ADR 0289: loop restart asks by SIGUSR2 and learns the outcome from the event log

Status: Accepted
Date: 2026-10-17

Chart mc-9j2q, prototype mc-9j2q.2 (throwaway on
`refs/mugicrew/prototypes/mc-9j2q`). `mugicrew loop restart` asks a running
from-source loop, watched or not, to build main and restart onto it. This ADR
settles two things: how the request reaches the loop, and how the CLI learns
the outcome. It builds on ADR 0062 (mugicrew-loop-restart-lifecycle) and ADR
0236 (mugicrew-loop-run-re-arms-from-a-cancelled-stop-drain…), and leaves their
stop and cancel signals unchanged.

## Decision

1. **The request is SIGUSR2.** `runLoop` adds SIGUSR2 to its `signal.Notify`
   call. A loop built before this change never notifies SIGUSR2. Go's runtime
   marks SIGUSR2 as notify-only, so such a loop drops it rather than dying.
   This was verified against go1.26.
2. **The signal goroutine queues the request.** The request goes onto a
   one-slot channel, so repeat requests merge into one. While stopped, the
   goroutine answers at once with `restart.requested {outcome: stopping}`.
3. **Run answers the request without waiting for a tick.** The answer is
   `request()`:
   - It clears the failed rev, so a failed main is retried (ADR 0062 §3's
     promise).
   - If a check is already in flight, it joins it (`outcome: building`,
     `joined: true`) and starts no second build.
   - If main is the running rev, it answers `outcome: current`.
   - On an installed loop it answers `outcome: unsourced`.
   - Otherwise it answers `outcome: building` and runs
     `checkMain(ctx, asked=true)`, which is let past the `!l.Watch` gate.

   A request still queued when a drain starts is answered by that drain:
   `outcome: draining` for a restart drain, `stopping` for a stop drain.
4. **Follow answers too when a request is pending.** When a request is
   pending and main builds the same outPath, follow logs a new
   `restart.current {rev}`. A watched tick on its own stays silent, as ADR 0062
   §2 says.
5. **The CLI's outcome channel is the event log.** Before signalling, the CLI
   reads the lock:
   - off: it refuses;
   - installed (no rev): it refuses;
   - stop drain: it refuses;
   - restart drain: it reports that a restart is already under way.

   Otherwise it notes the live log's end, sends SIGUSR2, and reads only the
   holder pid's events after that point:
   - `restart.requested` with outcome current, draining or stopping ends the
     wait;
   - `restart.current` ends the wait (already current);
   - `build.failed` exits non-zero with its `err`;
   - `loop.draining` with `restart_to` means success: the restart drain has
     started, and the CLI never waits out the drain;
   - `loop.draining` without `restart_to` means a stop took over.

   The wait for the ack is bounded by forceWait (10 s), as cancelDrain's is.
   With no `restart.requested` in that time, the CLI reports that an old build
   ignored the request: stop and start it. The ack is what tells "ignored"
   apart from "already current". The wait for the build itself is not bounded.
   It also ends if the lock leaves the pid.

## Considered Options

- **A request field in the lock body.** Rejected. The holder is the body's
  only writer, and `Lock.Write` pads over the whole file, so a second writer
  races it.
- **Reusing SIGUSR1, with a request file to tell it from a cancel.** Rejected.
  A stop landing between the CLI's lock read and its signal turns the request
  into a drain cancel, on old builds too.
- **Polling the lock body's `restart_to` and `failed_rev`.** Rejected:
  - `failed_rev` left over from before the request looks the same as a new
    failure;
  - the same-outPath case writes nothing to the body;
  - an ignored request looks the same as a loop that is already current;
  - the error text lives only in `build.failed`.

## Consequences

The CLI couples to these event names: `restart.requested`, `restart.current`,
`build.failed` and `loop.draining`. They become part of the loop's event
contract.

Two cases sit outside the ack table. A failed main lookup in the holder is only
`loop.tick_failed`, so the CLI's ack wait times out and it reports an old build.
A lock with no pid yet (a loop still starting) is refused unsignalled with "loop
is starting; retry".
