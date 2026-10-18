# A Chopper Checkup is a HITL kind created at launch that fences its target with `paused:checkup`

Status: Accepted
Amends: ADR 0126 (mugicrew-chopper-recovers-stranded-beads…) §1–3, for a broken bead only
Date: 2026-10-17

Chart mc-qcor ("A HITL Chopper session repairs a broken bead on a human's
command"), grilling bead mc-qcor.5. A human who thinks a bead is broken (a wrong
edge or label, a malformed body, a stuck pause that recover can't classify) needs
a claude session where Chopper diagnoses it with them and repairs it. Most of
the repair work is bead and process operations, not code. The shape comparison
(docs/research/chopper-session-shapes-launch-claim-liveness.md) found that only a
session with its own bead fits launch, claim, tmux, Drafts, recover and event
attribution as they are. The cost is that it leaves the target unfenced, so this
ADR adds a fence.

## Decision

1. **Kind.** `checkup` is a HITL leaf owned by Chopper. It is free-standing and
   off the spine, like a Task (ADR 0113) or a Discussion (ADR 0194
   (mugicrew-a-robin-discussion-is-an-off-spine…)). Only a human launches it,
   the loop never dispatches it, and it has no parent. Its route is the
   `default` arm: RepoRoot, with no tree and no branch. This amends ADR 0126 §1
   ("Chopper owns no kind") and §2 ("no claude session") for the broken-bead
   case. Repairing a bead stranded by a dead effort stays recover's
   deterministic job.
2. **Created at launch, bound by an edge.** The launch creates the Checkup bead
   (not a Draft) with a `related` edge to its target, as `bd.NewRollback` does,
   and names the target in its body for the prompt. Then it runs the normal HITL
   launch on the Checkup bead, claimed like any HITL bead. A target has at most
   one open Checkup: launching again attaches to it.
3. **Fence.** The launch adds `paused:checkup` to the target. The loop skips the
   target, as it skips any paused bead. The cause doesn't cascade to the target's
   members. `recover <target>` refuses while the Checkup's session is Alive or
   Exited. Once that session is gone or the Checkup is closed, recover counts the
   cause as a leftover and lifts it, as it lifts `recovering`, so no new lifter
   is needed. A HITL launch on the target only gets the paused-bead confirm.
   The fence doesn't hold off an AFK effort already running on the target,
   because a pause never stops a running effort (ADR 0055). That case is a launch
   refusal, decided under "How a human launches a Chopper session".
4. **End.**
   - **Close:** the session closes its own bead as `repaired: …` or
     `not a bead fault: …`, lifts `paused:checkup` last, and its offshoot
     Signals (`discovered-from:<checkup>`) publish on the clean exit.
   - **Early `/exit`:** the Checkup stays claimed and keeps its fence, so the
     human can rejoin. Checkup joins the kinds that `settle` exempts.
   - **Drop (`x`):** deletes its Drafts, closes it as `dropped` and lifts the
     fence.
5. **Record.** The Checkup bead holds the session's record: its notes and close
   reason. Its events carry `crewmate=chopper kind=checkup`. This amends
   ADR 0126 §3 ("nothing enters the graph to describe the repair") for the
   broken-bead case. Whether the target also gets a note is left open on the
   Chart.

## Considered Options

- **No bead, fenced by a pause cause on the target** (as recover is). Rejected.
  Under a synthetic id, `hitl-run`'s clean exit fails to read the bead. Under
  the target's id, it is the shared-bead case. recover can't see the fence, and
  if the session dies, nothing lifts it.
- **The session runs on the target bead itself** (as a Surgery shares its
  bead). Rejected. It fences through liveness at no cost, but it takes the
  target's owner, route and tree (it could integrate `effort/<target>`), its
  claim, its Drafts, any session it already has and its event attribution. The
  launch gate also refuses an AFK target such as a stuck Build.
- **No fence, relying on per-change approval.** Rejected. A Checkup can stay
  open for a long time, and a loop dispatch or a recover run in the middle of a
  repair would race the edits the human approved.
- **Look up open Checkups through the edge instead of a label.** Rejected. The
  loop already skips any `paused:*` label, and a lookup would add a graph query
  to every tick.
- **Why ADR 0126's rejection of `kind:recovery` doesn't apply here.** That bead
  was to be filed by automation, and its claim could strand. A Checkup is filed
  by a human at launch, and its claim is an ordinary HITL claim that recover
  already handles.
