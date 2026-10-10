# ADR 0064: mugicrew loop restart is tested through loop package vars, a Restart return and one real exec

Status: Accepted
Date: 2026-10-05
Amends: ADR 0062 §6

Chart: Pinned loop restarts itself onto main's new build. Settles
the grilling "How is a loop restart tested end to end without a real nix
rebuild?". Builds on ADR 0062 (lifecycle) and ADR 0063 (display).

## Context

The restart runs inside `loop.Run`'s tick, so `main.go`'s `resolveBuild` var
can't reach it. The loop package already stubs its dependencies as package
vars (`readyBeads`, `claimBead`, `runEffort`). `main.go` stubs `execLoop`.
`TestMain` re-runs the test binary as `main()` under `MUGICREW_TEST_MAIN`, and
`TestLoopStartDetachedEndToEnd` already uses the test binary as the "store
path". `fakeBd` puts a fake `bd` on `PATH`. `MainlineBuild` runs `nix` from
`PATH`. The flock surviving an exec (ADR 0062 §5) can only be proven by a real
exec.

## Decision

1. **Three package vars in `loop`:** `mainRev` (git.MainlineAt),
   `evalOutPath` (eval of main@rev's outPath) and `buildRev` (MainlineBuild
   pinned to a rev). They're separate so tests can fail or block eval and build
   independently.
2. **`loop.Run` returns `*loop.Restart{Rev, Bin}`** after a restart drain.
   `main` clears close-on-exec on the lock fd, logs `loop.restarting` and execs
   through `execLoop` with `loop [--detached] --rev=<new> --lock-fd=N`, keeping
   the hidden pinned marker. A stop that beats a restart is `Run` returning nil.
3. **One `onState(loop.State)` callback** replaces `draining func([]bd.Bead)`.
   `State` carries `State`, `Inflight`, `Rev`, `RestartTo` and `FailedRev`, and
   `main` maps it onto the lock body. Loop tests record the sequence of
   transitions.
4. **Test layers:**
   - In-process `loop` tests, with the three vars stubbed:
     1. Unchanged outPath rewrites `Rev`, with no build and no events.
     2. A failed build logs `build.started` and `build.failed`, sets
        `FailedRev`, and doesn't retry until main moves.
     3. A failed eval is `build.failed`.
     4. A build that's still running doesn't block dispatch.
     5. Main moving mid-build gives one build and `Restart{first}`.
     6. Main moving mid-drain causes no new eval.
     7. A stop during a restart drain clears `RestartTo` and returns nil, and
        a second stop gives `ErrForced`.
     8. A restart drain with nothing in flight returns `Restart` at once.
     9. A stop during a build that's still running (§5).
   - The `main` side, with `execLoop` stubbed: the restart argv for both loop
     kinds and the `loop.restarting` event.
   - A `looplock` round-trip test for `restart_to` and `failed_rev`.
   - **One real fd-handoff test (`looplock`):** the test binary takes the
     lock, clears CLOEXEC and execs itself with `--lock-fd`. The lock reads as
     held by the same pid, and a racing `Acquire` is refused.
   - **One real end-to-end test (`main_test`, detached only):** a real git
     repo, a fake `nix` on `PATH` that answers eval and build with a per-rev
     store dir whose `bin/mugicrew` is the test binary, and a short poll
     interval. A commit to main has to yield `build.started` → `build.done` →
     `loop.restarting` → `loop.started restarted=true` on the same pid. It runs
     in plain `go test` and checkPhase.
   - The opt-in `TestMainlineBuildRealNix` also checks that eval's outPath
     equals the build's store dir for the same rev. If they drift, the loop
     restarts forever.
5. **A stop during a build that's still running** (amends ADR 0062 §6): the
   stop drain cancels the `nix` child (CommandContext) and doesn't wait for it.
   A build isn't an effort, and an orphaned `nix` would re-point `loop-build`
   after the loop exits.

## Considered options

- **One coarse `checkMainline` var, or a `Builder` interface:** rejected. A
  coarse var can't fail eval and build separately or hold the build open, and
  an interface would have one implementation.
- **`loop` execs on its own:** rejected. The fd and exec details belong with
  `main`/`looplock`, which own the lock.
- **Handing `*looplock.Lock` to `Loop`, or a callback per field:** rejected.
  Snapshots can't show transitions, and per-field callbacks multiply the seam.
- **No real exec, with a stubbed handoff:** rejected. Only a real exec proves
  CLOEXEC was cleared and the new image adopted the fd.
- **A real-exec end-to-end test for the foreground kind too:** rejected. It
  differs only in argv, and the stubbed test covers that.
- **The stop drain waits for the build, or leaves it orphaned:** rejected.
  Waiting lengthens the stop for a build that will be thrown away, and an
  orphan touches GC roots after exit.

## Consequences

- The detached end-to-end test is the only one that crosses a process boundary
  for a restart. Every edge case stays in-process and fast.
- Real nix runs only behind `MUGICREW_NIX_TEST=1`, never in checkPhase.
