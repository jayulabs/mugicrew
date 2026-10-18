# ADR 0115: mugicrew Brook — a new crewmate for the external world owns Task; its session is human-started and guide-closed

Status: Accepted (amended by ADR 0222 §7: Brook owns `kind:latch`, a free-standing leaf)
Date: 2026-10-09

Amends: ADR 0013 §3 (the reserved `task` kind gets an owner). Reuses the name
Brook, which ADR 0003 gave to an out-of-band activation channel and ruled out;
this Brook is a different thing.

## Context

A Task walks a human through changing the external world (the Task Chart). It is
not defog (so not Nami's), not code (so not Franky's, though Surgery is the
nearest precedent), and not authorization (so not Luffy's). The loop never
starts a HITL bead of any kind). Decided in grilling.

## Decision

1. **Brook is a new crewmate whose domain is the external world**: anything
   outside the bead graph and outside the code and git a Checkpoint changes. He
   owns `kind:task`. Later he is also meant to mirror external state into the
   graph (e.g. an app's deployment state) so other work can gate on it. That is
   not part of the Task Spec, and it is not Signal intake, which stays Nami's.
2. **Only a human starts a Task session**, with `l` or `b` in the TUI, like
   every HITL leaf. The loop never launches one, and no new start verb is added.
   A Task waits on the human's real-world availability.
3. **An interrupted Task stays claimed.** If the human exits before the Task is
   done, the bead stays in_progress and the row shows it idle and resumable, as
   a Surgery does. Resume re-runs every check (ADR 0114 §6). A drop (`x`) still
   unclaims.
4. **The guide closes its own bead**, only after a final pass in which every
   step's check passes and the human confirms in the session. There is no Go
   close verb. ADR 0108's close-after-return runs unchanged.
5. **The session runs at RepoRoot**: no worktree, no commits, no integrate. A
   step that needs a code change is a Checkpoint's job, ordered by blocks edges
   (ADR 0113).

## Considered Options

- **Nami:** rejected. Her kinds are intake and defog, and a Task is never defog.
- **Franky:** rejected, though Surgery is close. His kinds change code; a Task
  changes the outside world, and that domain is due to grow (state mirroring).
- **Luffy:** rejected. Approval is authorization, not doing.
- **A Go close verb (like `mugicrew surgery end`) or a TUI close:** rejected.
  The checks are the evidence and the human's in-session yes is the
  authorization; nothing is left for Go to do.
- **Unclaim an interrupted Task back to open:** rejected. Half-done external
  state is real; the bead should say so.
