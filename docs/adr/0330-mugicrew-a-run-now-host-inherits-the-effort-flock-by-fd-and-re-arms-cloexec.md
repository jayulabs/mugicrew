# A run-now host inherits the effort flock by fd and re-arms CLOEXEC

Status: accepted

When the TUI dispatches a run-now effort, it hands the per-effort flock of ADR
0154 to the setsid-detached `mugicrew run <id>` host by passing the fd: the
claim and the lock never leave the kernel's lock table between the TUI and the
host. This amends ADR 0154's holder rule ("children never inherit the lock") with
one deliberate exception.

- **TUI.** It takes the hold (`liveness.Acquire`) and claims as today. It passes
  the hold's file as the host's only `exec.Cmd.ExtraFiles` entry, so the fd is 3
  and the argv is `mugicrew run <id> --lock-fd=3`, as `execRestart`'s
  `--lock-fd` does. After `Start` returns it closes its own copy. The fd stays
  O_CLOEXEC in the TUI, so no other child of the TUI, even one spawned at the
  same moment, inherits it. If `Start` fails, the hold never left the TUI, and
  the TUI releases it itself on its failure path.
- **Host.** It adopts fd 3 as its hold and re-arms O_CLOEXEC on it **before**
  spawning anything. Go's exec gives a child every fd that lacks O_CLOEXEC. If
  the host skipped the re-arm, its `reconcile`/`claude` session would inherit the
  lock and keep the effort reading alive after the host died.
- **No re-take.** The host never re-acquires the lock by path. It owns the open
  file description that already holds the lock. A re-take while the TUI holds
  the lock would need a second, handshaked hand-back, and the measurements below
  showed nothing for it to fix.

## Consequences

The prototype measured this (lineage: `refs/mugicrew/prototypes/mc-a9aa6`,
`apps/mugicrew/liveness/prototype_fdhandoff`): 7 scenarios × 10 runs, about
4.5M `HeldIn` probes from a separate process, with zero alive→dead→alive flaps.
The scenarios cover the TUI Waiting, quitting with its Wait in flight, being
SIGKILLed, and taking a SIGHUP to its process group. In each, the host stays in
its own session, is reparented to init, receives no signal, and keeps its
`run-<id>.out` writable. The TUI's `Wait` on its setsid child returns the
host's exit status or signal. A SIGKILLed host frees the lock within about 1ms
even with its session's grandchild still running. Without the re-arm, that
grandchild held the lock until it died. The argv scan matches
`mugicrew run <id>`, but only after the host execs (about 40ms after the fork),
and the flock covers that window. `liveness` gains `Hold.File` and an `Adopt`
that re-arms O_CLOEXEC.

## Amendment: the host mints its own effort id

The argv stays `mugicrew run <id> --lock-fd=3`, with no effort-id flag. The
host calls `dispatch.NewEffortID()` once, after it adopts fd 3 and before its
first event. That one id goes on its pre-Dispatch `run.failed` (ADR 0331
(mugicrew-a-run-now-host-adds-only-run-failed…)), its `effort.*` and
`claim.released` events, and the `effort.abandoned` of a second-signal
Abandon. This is the loop's rule: the process that abandons an effort mints its
id. When `Start` fails, the TUI's `run.failed` carries a one-off id that it
never hands over, because no host exists to share it. No other process needs the
host's id. Abort finds the host by bead id in its argv, and the TUI reports
`run X done/failed` from `Wait` and `run-<id>.out`.

Considered: the TUI mints at press time and passes `--effort=<eid>`. Rejected.
The id's only TUI-side use is a Start failure, and by then no host exists, so a
shared id joins nothing and the host just gains a flag to parse and check.

## Note: the Plan usage limit

Run-now still ignores the Plan usage limit (`o.Limit`), as it did inside the TUI
while the loop holds claude dispatch on it. A host that outlives the TUI makes
this no worse, and it is left as is.
