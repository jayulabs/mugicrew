# ADR 0236: Loop.Run re-arms from a cancelled stop drain by signal generation

Status: Accepted
Date: 2026-10-16

Chart mc-ig6f, prototype mc-ig6f.2 (throwaway on
`refs/mugicrew/prototypes/mc-ig6f`). A loop in a stop drain can now be
cancelled with SIGUSR1, which `loop start` sends. The cancel sends the loop
back to polling, and no in-flight effort is abandoned. This amends the one-way
drain of ADR 0050 (mugicrew-loop-stop-drains) §1 and ADR 0062
(mugicrew-loop-restart-lifecycle) §6. A stop's channels close once and cannot
reopen, so the signal plumbing moves to a *generation* of channels.

## Decision

1. **One signal goroutine, alive for the whole of Run.** `runLoop` adds
   SIGUSR1 to its `signal.Notify` call. The goroutine owns the StopWindow
   bookkeeping (stopped, first) and the current generation, which is
   `{stop, force, cancel}`, each a close-once channel. These are its rules:
   - The first SIGTERM/INT/HUP closes `stop`.
   - A repeat inside StopWindow is ignored.
   - A repeat after StopWindow closes `force`.
   - SIGUSR1 while stopped closes `cancel` and installs a fresh generation
     under `l.mu`, so the next stop drains afresh with its own StopWindow and is
     never taken as a force.
   - SIGUSR1 while not stopped (polling, or an unconverted restart drain) is
     dropped.
2. **Run is an outer loop**: poll → `drain` → return, unless the drain was
   cancelled. On a cancel Run logs `loop.resumed {cause: cancel}`, sets
   `draining` to false, reports `liveLocked` (on or limited) and polls again. A
   checkMain context is created for each polling phase, because the stop
   cancelled the old one.
3. **A stop keeps `l.restart`.** On a stop, the drain clears only `restartTo`.
   If a stop converted a restart drain, cancelling that drain reverts it to the
   restart drain: `built` is still closed, so Run leaves polling at once and
   drains with `RestartTo` set again.
4. **drain's done arm reads the current generation.** It returns nil, nil if
   the current generation is stopped, and `l.restart` otherwise. This one rule
   covers both of these cases:
   - A cancel loses the race to the last effort: the loop exits, the lock goes
     off, and `loop start` falls through to a fresh start.
   - A cancelled converted drain finishes: the loop execs the restart.
5. **drain stops using `wg.Wait`.** Each cancelled drain would leave its
   `wg.Wait` goroutine waiting. If a resumed tick calls `wg.Add` while that
   waiter wakes, the WaitGroup panics with "reused before previous Wait has
   returned", and the panic kills the loop with efforts in flight. drain
   instead waits on a count of running efforts. Each effort decrements the
   count where it calls `wg.Done` today (after `hold.Release`) and then does a
   non-blocking send on a notify channel. drain exits once the count reaches 0.
6. **A drain discards nothing a resume needs.** The limited hold, `failed`,
   `checked` (latch cadence) and the held/failing counts are Loop fields that
   drain never touches. A build that the stop cancelled is not recorded as
   failed (settle drops it), so it is retried after the resume.

## Consequences

- Go reads pending signals in signal-number order, so SIGUSR1 (10) is read
  before SIGTERM (15) when both are pending: the cancel is dropped and the stop
  wins. `loop start` cannot cause this, because it signals only after the lock
  reads a stop drain.
- Go catches SIGUSR1 and takes no action when it isn't registered (os/signal
  docs). So a loop on a build from before the cancel ignores it, and
  `loop start`'s wait for the cancel to land must be bounded.

## Considered options

- **Fresh channels created by drain itself.** Rejected: the signal goroutine
  would close a channel it does not own, so a stop that races the swap can
  close an already-closed channel and panic.
- **A plain select loop over typed events in Run.** Rejected: a stop must stop
  a tick that is already under way (`leaving()` is checked mid-tick), and a
  closed channel does that from any goroutine.
