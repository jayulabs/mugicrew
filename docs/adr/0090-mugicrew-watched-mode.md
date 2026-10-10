# ADR 0090: mugicrew watched mode — opt-in, from source only, and only a watched loop restarts

Status: Accepted
Date: 2026-10-08
Amends: ADR 0060 §1–§3, ADR 0062 §7, ADR 0063 §2

Chart: mugicrew runs on any Workspace, decoupled from its own source.
Settles the grilling "Pin down watched mode", which builds on the
research.

## Context

Today one directory, the git common dir derived from the cwd, does three jobs.
It is the Workspace, it is the repo whose `main` the loop watches, and it is
the flake the loop builds. Every loop pins (ADR 0060), and every pinned loop
auto-restarts (ADR 0062 §7). On a foreign Workspace, every land would trigger
a `nix eval` of a flake that has no `#mugicrew`, and so a permanent stale
flag. At triage it was settled that a built binary is a fixed tool, and that
watching is opt-in, valid only from source, and triggered only by landed
commits on the **source repo's** mainline. Nothing in the code can tell
"running from source" apart today: a `/nix/store` path proves only a Nix
build, `vcs.revision` has no path, and `MUGICREW_SRC` is ambient.

## Decision

1. **From source means stamped.** The dev wrapper (`mugicrewDev`) builds with
   `-ldflags -X` and stamps the source repo's absolute root into the binary.
   A binary without the stamp (any Nix-built or installed binary) is not
   from source.
2. **Every from-source loop pins (amends ADR 0060 §1).** It builds the
   source repo's mainline once and execs that store build, as ADR 0060 does
   today, but from the **source** repo, not the Workspace. An installed
   binary does not build. It runs the loop as itself.
3. **The launcher passes the source dir (amends ADR 0060 §3).** The pinned
   image is not stamped, so the launcher passes the source dir as a hidden
   marker next to `--rev` (and `--lock-fd` on restart). The image trusts its
   launcher as before.
4. **`--watch` opts into restart (amends ADR 0062 §7).**
   `mugicrew loop --watch`, `mugicrew loop start --watch`, and
   `mugicrew --watch` for the TUI, whose `o` passes it to `loop start`.
   Watching belongs to the loop and is written to the lock body as
   `watch: true`. `--watch` without the source stamp is an error. Only a
   watched loop runs the ADR 0062 restart lifecycle. To pick up a new build,
   an unwatched loop is stopped and started again.
5. **The watch reads the source's local mainline.** The source repo's
   mainline is found the same way as the Workspace's: `origin/HEAD` names
   the branch, falling back to `main`. The watch reads that branch's local
   ref, which is the one lands fast-forward. Workspace lands, fetches and
   dirty trees never trigger a restart.
6. **The `loop-build` out-link stays in the Workspace state dir**
   (`<Workspace state>/mugicrew/loop-build`). There is one per loop, so two
   Workspaces watched from the same source never race on one link.
7. **Display (amends ADR 0063 §2):**
   - Installed: `loop status` prints `on (pid N, <exe path>)` and the tab
     bar shows `loop on`, with no rev.
   - Pinned, unwatched: unchanged (`main abc1234, /nix/store/…`,
     `loop on @abc1234`). It is never stale, because nothing is watched.
   - Watched: `loop status` adds a line `watching <source repo> <mainline>`.
     The tab bar is unchanged. The stale and restarting displays (ADR 0063)
     apply only to watched loops.
8. **Testing loop changes (amends ADR 0060 §2's recipe).** Make a clone with
   your branch checked out as its mainline and use it as the *source*. Run
   its dev `mugicrew` with `-C <scratch Workspace>`. A NenOE clone used as
   both source and Workspace still works. There is still no `--dev` flag or
   unpinned escape hatch for a from-source loop.

When self-hosting on NenOE, the source repo and the Workspace are the same
repo, so a `--watch` loop behaves exactly like today's loop.

## Considered options

- **`--watch=<path>` naming the source:** rejected. Any built binary could
  then watch any path, which breaks "a built binary never watches".
- **`MUGICREW_SRC` as the signal:** rejected. It leaks into every devShell
  child, including the pinned loop.
- **Unwatched loops never pin:** rejected. That brings back the
  `~/.cache/mugicrew` branch skew that ADR 0060 removed.
- **Every from-source loop is watched:** rejected. Watching is opt-in.
- **Out-link in the source repo:** rejected. Two loops would race on it.
- **Superseding ADR 0060:** rejected. Its pin, its trusted launcher and the
  rev in the lock body all still hold.
