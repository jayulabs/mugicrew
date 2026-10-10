# ADR 0117: mugicrew Task — an ordinary HITL row, idle like a Surgery, and no human front door for now

Status: Accepted
Date: 2026-10-09

Amends: ADR 0113 §1 (the human producer keeps no CLI/TUI affordance). Builds on
ADR 0095 §4 (the idle suffix), ADR 0115 §3 (an interrupted Task stays claimed).

## Context

A Task is a free-standing HITL bead (ADR 0113) that Brook guides a human through
(ADR 0114, ADR 0115). That left open what the TUI and loop show for it, how its
idle row reads, how a human files one, and how an approved Checkpoint held by an
open Task blocker shows. Today the TUI shows a Task in the Inbox through an
`isHITL` special case. The idle suffix is hard-wired to Surgery. Nothing files a
Task. A blocked, approved Checkpoint gets `⊘` and no `t` in the footer, but its
preview still says "approved & at HEAD. Press t". Decided in grilling.

## Decision

1. **Nothing new on screen.** A Task is an ordinary HITL row: in the Inbox when
   it is open and unblocked, and under Graph's Unparented group. There is no
   Task group, no column, and no "waiting on external" state. A Task blocked on a
   land shows `⊘`, like any blocked bead. The loop never launches it (ADR 0115
   §2). The `isHITL` special case goes away once Brook's registry row exists.
2. **Idle reads as for a Surgery.** A Task that is in progress with no session
   gets the same dim `idle — l to resume` suffix. The condition widens to
   surgery or task. The row shows no step progress; that lives in the bead's
   pass comments (ADR 0116 §5).
3. **No human front door.** mugicrew adds no `task new` verb, no TUI key and no
   form. A human may still file a Task by hand with raw `bd create` and
   `kind:task` (ADR 0113 §1 stands as a producer). Zoro and sessions remain the
   normal producers. A dedicated front door waits until real use shows its
   shape.
4. **A held land names its blocker.** An approved Checkpoint with an open
   blocker keeps `⊘`. Its preview reads "approved; land held by open blocker:
   <title>" instead of offering `t`, and the `t` refusal names the blocker the
   same way.

## Considered Options

- **A Task group or column in the Graph, or a "waiting on external" state.**
  Rejected: a Task is just a HITL bead, and waiting on the outside world is
  Brook's future state-mirroring role, out of this Spec (ADR 0115 §1).
- **Step progress on the idle row (`idle — step 3/7`).** Rejected: it needs a
  body parser, which ADR 0116 rules out.
- **A Surgery-style front door** (`mugicrew task new` plus a TUI key that files
  and launches Brook to draft the steps). Not now: kept for later.
- **A new "held" glyph or a `‖` pause cause for a held land.** Rejected: `⊘`
  already means "has an open blocker", and `‖` is the loop's Paused.
