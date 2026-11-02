# ADR 0406: mugicrew Loops sharing a global binary survive its swap and drain-restart onto it

Status: Accepted
Date: 2026-10-29

## Context

One npm global mugicrew binary serves every Workspace on the machine. When a
loop runs an unattended Upgrade, or the human runs `mugicrew upgrade`, loops and
TUIs in other Workspaces keep running the old image. The research
`docs/research/npm-global-binary-replaced-under-a-running-loop.md` found that on
Linux this leaves them broken, not just a version behind. `npm i -g` renames the
old package aside and then deletes it. After that, every re-exec through a
fresh `os.Executable()` fails ENOENT, and the AFK backstop's transient exit
reopens and re-dispatches beads forever. Paths captured at startup still reach
the new binary, and a stat inode compare detects the swap cheaply. Settled by
the grilling "How loops in other Workspaces sharing the global binary meet an
unattended upgrade" on the Chart "mugicrew upgrades itself unattended when it
is safe to".

## Decision

1. **Accept the skew and make it survivable.** No machine-level registry of
   loops exists, and an Upgrade never waits for other Workspaces' loops.
2. **Re-execs use the path captured at startup.** Every site that re-execs
   mugicrew through a fresh `os.Executable()` uses the startup path instead. So
   after a swap the new binary answers, as it already does on darwin.
3. **Every loop detects a swap and drain-restarts.** A loop compares its
   binary on disk with the one it runs. On a change it stops dispatching new
   work at once and lets live sessions finish (never killing one, HITL
   included). It then restarts onto the new binary. This holds whether or not
   that loop is opted into unattended Upgrade. Opt-in governs only who runs
   npm.
4. **The human's `mugicrew upgrade` follows the same rule.** It gains no new
   gate. Other loops react to its swap as in §3. Its refusal while this
   Workspace's loop lock is held stays.
5. **Every TUI on the machine detects the swap** and shows the one-time status
   line telling the human to restart mugicrew, worded for an upgrade made
   anywhere on the machine, not only by this Workspace's loop.
6. **The first release with these fixes declares needs-a-human-step.** Loops
   on older releases can't survive a swap and nothing can see them. So that
   release installs only through a human `mugicrew upgrade`, and
   `docs/upgrading.md` tells the human to restart loops in every Workspace.
   Unattended swaps begin once every loop can survive them.

## Considered Options

- **A machine-level registry of loops, the upgrading loop refusing unless
  alone.** Rejected: it adds machine-wide state with its own liveness and
  stale-entry problems, the same class as AFK orphans. It also doesn't fix the
  human's `mugicrew upgrade`, or a loop that a registry misses.
- **Registry plus survivable skew.** Rejected: the registry adds nothing once
  §2 and §3 hold.
- **Re-exec `/proc/self/exe` so the old image answers.** Rejected: Linux-only,
  and it prolongs the skew rather than ending it.
- **A swapped loop waits for natural idle (24h cap), like the upgrading loop.**
  Rejected: until it restarts, its new agents run the new binary on PATH under
  an old dispatcher, so the skew runs both ways. Pausing dispatch closes that
  gap without killing anything.
