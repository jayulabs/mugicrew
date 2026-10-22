# ADR 0341: mugicrew a failing AFK Approval falls back to HITL by a marker, not a pause

Status: Accepted
Date: 2026-10-21

Chart "Approvals resolve AFK when auto-approve is on"; grilling "How a failed AFK
Approval session recovers, and what the human sees".
Research: `docs/research/afk-luffy-approval-launch-exit-and-publish.md` §5,
`docs/research/approval-mode-live-on-auto-approve-setting.md`.

When auto-approve is on, an AFK Luffy session resolves the Approval. A session that
fails (error, signal, or an exit without its outcome line) retries under the loop's
failure count. Without this ADR, the third consecutive failure would leave the
Approval `paused:failing`. Actionable lists a paused bead only in its paused band,
the queue tabs refuse to launch it, and nothing on the HITL path lifts the label. The
Checkpoint would sit until the human found and lifted the pause. Instead, the
failure is treated as a mode change: the Approval falls back to an ordinary HITL
Approval in the human's queue.

## Decision

1. **Retry, then fall back.** Failures count as for any AFK effort (Plan usage limits
   and human aborts don't). A failed try's Drafts are deleted, offshoot Signals
   included, and the next try or the human files them again.
2. **A fallback marker, not `paused:failing`.** At the third consecutive failure of an
   Approval resolved AFK, the loop writes a fallback marker label on it, and not
   `paused:failing`. The Spec names the label.
3. **The live-mode predicate reads it.** An Approval is AFK only when auto-approve is on
   AND it carries no fallback marker. A marked Approval is HITL.
4. **Sticky per Approval.** The marker stays until that Approval closes (approved,
   reworked, or retired as stale). Removing it by hand (`bd label remove`) is the only
   way to send it back to AFK. A fresh Approval for a new pin starts AFK.
5. **What the human sees.** The marked Approval sits in Actionable's ready frontier as
   a normal launchable HITL Approval, with an "auto-approve failed" row qualifier
   read from the marker. Pause semantics and the queue's paused-row refusal are
   unchanged.
6. **The HITL session.** It runs the normal Approval prompt and rebuilds the packet
   fresh. Its only addition: when the marker is present, the packet's Approval History
   gains one line, "auto-approve failed (AFK, 3 attempts); resolved HITL", before the
   approved ending.
7. **Loop death and crashes don't fall back.** `paused:abandoned` and a stale claim
   stay on their existing recovery (`Abandon`, Chopper recover). Once recovered, the
   Approval resolves AFK again.

## Considered Options

- **Stay paused (R1).** No code, but the Checkpoint stalls until the human notices a
  pause on the Graph.
- **Fall back on the first failure (R3).** A single transient harness failure would
  hand the work to the human.
- **Keep `paused:failing` as the trigger and exempt it from the queue refusal.** Rejected:
  a "paused" bead the human is expected to launch blurs pause (the loop won't dispatch
  it). It also needs new lifting code on the HITL path.
