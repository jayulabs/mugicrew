# ADR 0083: mugicrew per-effort abort — `mugicrew abort <bead-id>` / TUI `x` pauses the bead `manual`, then SIGTERMs the processes the liveness scan finds for it

Status: Accepted
Date: 2026-10-07

## Context

The human has no way to stop one in-flight AFK effort short of force-stopping
the whole loop (ADR 0050 §2) or quitting the TUI, and both of those abandon
every effort and leave its leftovers (ADR 0082). No host keeps a pid per effort,
and the reconcile child and its `claude` each lead their own process group. If the host is alive and its child dies, the existing failure path already
cleans up: it deletes the Drafts, removes the tree and releases the claim (§3).
The release would put the bead back in `bd ready` unless a pause is already on
it. Grilling settled the surface.

## Decision

1. **Scope: AFK efforts only.** That covers loop-dispatched efforts and run-now
   (`t`) efforts, on both routes. HITL sessions keep `x` drop (ADR 0057), which
   already is their abort.
2. **Verb.** On the CLI, `mugicrew abort <bead-id>`. It targets the bead, not an
   effort id, which stays internal. In the TUI, `x` on a row aborts when the row
   has **no** HITL session and the liveness view shows a live AFK process for
   the bead. A session always wins `x`, and an abort of such a bead goes through
   the CLI.
3. **Confirmation (cf. ADR 0050 §3).** The TUI always asks y/n with an
   abort-specific prompt. The CLI never prompts, because typing the verb is the
   confirmation.
4. **Preconditions.** If the bead isn't `in_progress`, the abort is refused
   with "nothing in flight".
5. **Order: pause, event, signal.**
   1. One `bd update <id> --add-label paused:manual` call (ADR 0070 §1/§3). If
      it fails, **nothing is signalled**: abort exits non-zero with the bd
      error, and the TUI shows it in the status line. A kill with no pause
      would be re-dispatched on the next tick. This is the opposite of ADR 0082
      §3, where the force must never block.
   2. Log `effort.aborted {bead, by: cli|tui, pids: [...], paused: true}`.
   3. SIGTERM each matched process's group.
6. **Mechanism: the aborter signals directly.** It finds the bead's processes
   with the liveness scan (`/proc` argv holding the bead id, `argv[0]`
   `mugicrew` or `claude`; ADR 0025). This is an explicit human command, not an
   automated reaction to liveness. So it amends the ADR 0050 "Considered
   options" reading that acting on the ps liveness view clashes with ADR 0031:
   that clash still holds for automatic reclaim (Chopper), but not for a
   human-issued abort. No host channel and no pid store are added.
7. **Cause `paused:manual`.** The human chose this, `p` lifts it, and it already
   ranks highest (ADR 0082 §7). It is not `abandoned`: with a live host, the
   failure path tears the tree down, so no orphan writer is left. No new cause
   is added.
8. **Cleanup is the host's existing failure path.** The host's `cmd.Run` (or,
   on the in-process route, `crew.Run`) returns the signal error. A run-now
   host (ADR 0330) matches §6's argv scan like the loop's children do: abort
   SIGTERMs it, it absorbs that first signal, and its failure path runs off the
   child's exit or the ADR 0155 pre-spawn pause re-read.
   `effort.failed` records it truthfully, the Drafts are deleted, the tree is
   removed and the claim is released. The bead stays paused. The loop skips
   `countFailure` when the post-failure re-read (the one `release` already
   does) shows `paused:manual`, so an abort never builds toward
   `paused:failing`. Run-now never counts failures anyway.
9. **No live host.**
   - If the scan finds nothing, abort pauses only and reports "no live process;
     paused only".
   - If it finds orphans from an earlier force stop or a run-now host's second signal, abort kills
     them too, because the human asked for this bead to stop.
   - In both cases there is no host left to clean up. The claim, tree and
     Drafts stay for the human or Chopper, and abort never unclaims or
     removes a tree (plan of record: no self-heal). It adds no
     `paused:abandoned`, because `manual` already outranks it.
10. **SIGTERM only.** There is no SIGKILL escalation (`ponytail:`; add one if
    aborted efforts are seen surviving). The CLI prints the pids it signalled,
    so a survivor is visible. The agent's tool subprocesses may sit in other
    groups. Any survivor races the host's tree removal, the same race
    `session.Drop` already accepts.

## Considered and rejected

- **Abort reaches HITL sessions too.** It would duplicate `x` drop.
- **Ask the host (a request file plus a signal, and a per-effort `exec.Cmd`
  handle).** It is new IPC, and it needed a channel to each host. A run-now
  host is now a `mugicrew run <id>` process the argv scan already finds.
- **Reuse `paused:abandoned`, or add `paused:aborted`.** Abandoned warns of an
  orphan writer that a live-host abort doesn't leave behind. A new cause would
  need a glyph and rank for no reader.
- **A new TUI key.** `x` already means "stop what's running on this row".
- **Signalling even when the pause write failed.** The bead would be
  re-dispatched on the next tick.
- **SIGKILL escalation now.** It is speculative until a survivor is observed.

## Consequences

- New `abort` subcommand, an `x` branch, an abort confirm prompt and an
  `effort.aborted` event (ADR 0029 catalogue). `countFailure` gains the
  `paused:manual` skip.
- The Spec must verify that every crewmate's `Run` passes the `claude -p` error
  up on the in-process route (research gap), or a killed in-process effort
  could be read as a success.
- The bead-id scan can't tell two efforts on the same bead apart. That is
  harmless here, because abort means "stop everything on this bead".
