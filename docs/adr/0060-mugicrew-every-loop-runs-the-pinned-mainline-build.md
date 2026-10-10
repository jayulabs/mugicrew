# ADR 0060: mugicrew loop pinning — every loop, foreground or detached, runs main's store build, with no unpinned escape hatch

Status: Accepted
Date: 2026-10-04
Amends: ADR 0053 §2

## Context

ADR 0053 §2 has `loop start` re-exec `os.Executable()`. Under the devShell
wrapper (flake.nix `mugicrewDev`), that is `~/.cache/mugicrew/mugicrew`. That
file is rebuilt from whichever checkout last ran `mugicrew`, so the loop's
children can run another branch's code. The Chart "Loop pinned to an immutable
mainline mugicrew build" pins `loop start` to the flake's
`packages.mugicrew` built from the repo's `main`.
This ADR settles three open questions: does the foreground `mugicrew loop` pin
too, is there an unpinned way to run a loop, and should the pinned binary verify
that it is pinned. They were decided in grilling.

## Decision

1. **Both loop verbs pin.** `mugicrew loop` and `mugicrew loop start` each build
   main's store path and exec it. The foreground loop replaces itself in place
   and stays in the terminal. `loop start` spawns it under `setsid` as before.
   Any process that takes `loop.lock` (ADR 0053 §1) runs a mainline build. The
   lock is the production-loop authority, so an unpinned holder would bring the
   version skew back.
2. **No unpinned escape hatch.** There is no `--dev` flag and no env var. To try
   loop changes, use a throwaway `git clone` with your branch checked out as
   `main`. The pin builds the invoking repo's `main`, so the clone's loop runs
   your code against its own lock, event log and beads, never the shared repo's.
3. **The pinned binary trusts its launcher.** A hidden argv marker tells the
   exec'd binary it is already pinned, so it runs the loop instead of building
   again. The detached loop's existing `--detached` marker (ADR 0053 §2) already
   does this. The pinned binary does not check that `os.Executable()` is under
   `/nix/store` or matches the out-link. Every supported route to the lock (the
   CLI verbs, and the TUI shelling out to `mugicrew loop start`, ADR 0051) goes
   through the launcher. Passing the hidden marker by hand is off-contract.
4. **Only the loop's process tree pins.** HITL tmux sessions launched from the
   TUI keep `hitl-run` as the TUI's `os.Executable()`, and their panes take the
   TUI's PATH. Those sessions are attended, so a human can already see any
   skew. The TUI itself is not pinned either, which keeps branch UI changes
   testable. A human's bare `mugicrew` in the devShell stays the dev wrapper
   (`mugicrewDev`). The pinned build reaches PATH only under the loop. Decided
   in grilling.
5. **The loop shows which build it runs.** The launcher resolves main's SHA
   once and builds `git+file://<common-dir>?ref=main&rev=<sha>#mugicrew`, so
   the reported rev and the store path always agree, even if a land happens
   mid-launch. It passes the SHA to the pinned binary next to the hidden
   marker. On acquire, the pinned loop writes `rev` (the full SHA) and
   `store_path` (`os.Executable()`) into the `loop.lock` body next to `pid` and
   `state`. Readers never use the out-link, because a start that builds and is
   then refused by the lock re-points the link while the old loop keeps
   running.
   - `loop status` prints `on (pid N, main <short-rev>, <store path>)`. When
     draining it prints the same build fields, then the in-flight beads.
   - The TUI tab bar shows only the rev: `loop on @<short-rev>`,
     `loop draining N @<short-rev>`.
   - An off loop, or a holder that has not yet written its body, looks the same
     as before.

   There is no stale-build flag. Comparing the rev with main's tip flags every
   land, even a docs-only one. Comparing the store path with main's current
   outPath needs a `nix eval` that takes seconds, and that comparison belongs
   to auto-restart. Decided in grilling.

## Considered options

- **Pin TUI-launched HITL sessions too (always, or only when the loop's
  out-link exists):** rejected. Every session start would cost a nix build or
  depend on an out-link, to fix skew an attending human can already see. The
  conditional variant would also behave differently depending on whether a loop
  had ever run.
- **Make bare `mugicrew` the pinned build and rename the dev wrapper, or add
  a `mugicrew-main`:** rejected. The loop verbs already pin themselves, so
  which name you type doesn't matter. Renaming breaks agent prompts that say
  `mugicrew`.

- **Pin only `loop start`, so a foreground loop runs the invoking binary:**
  rejected. That foreground loop would be the real dispatch loop running
  unlanded code, and its children would re-exec the shared cache binary.
- **`--dev` flag or `MUGICREW_UNPINNED=1`:** rejected. Either one dispatches the
  shared repo's beads with unlanded code. The env var would also leak through
  the TUI's env into a detached loop (ADR 0053 §4). Add a flag later if the
  clone recipe ever hurts.
- **Pinned binary refuses unless under `/nix/store` (or equal to the out-link
  target):** rejected. It only guards against deliberately bypassing a hidden
  marker. The exact check also turns a launch racing a move of `main` into a
  confusing refusal. Showing which build is running belongs to observability
  (§5), not to a startup refusal.
- **Read the running build from the out-link:** rejected. A refused start
  re-points it (§5).
- **Flag a stale build (rev ≠ main's tip, or store path ≠ main's outPath):**
  deferred. The rev check flags every land as noise, and the outPath check
  costs a nix eval and is the first half of auto-restart.
