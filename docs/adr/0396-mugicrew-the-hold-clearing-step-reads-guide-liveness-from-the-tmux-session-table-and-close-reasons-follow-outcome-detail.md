# ADR 0396: mugicrew The hold clearing step reads guide liveness from the tmux session table, and close reasons follow `<outcome>[: <detail>]`

Status: Accepted
Date: 2026-10-29
Amends: ADR 0393 (go-clears-a-hold-errand-through-a-paced-typed-step…), its open Consequence on how Go reads "guide session live".

## Context

ADR 0393 lets Go close a Hold Errand whose fact cleared, never while its guide
session is live, and left the liveness read open. An idle claim, a flock and
`liveness.Alive` each answer a different question, and a guide resting claimed
and idle must not block a clear forever.

## Decision

1. **Liveness is the tmux session table.** The step reads `session.Sessions()`
   and treats a hold as live when its entry's `State` is `Alive` (`Closing`
   is a sub-state of Alive). It never reads the claim (an idle `in_progress`
   hold is claimed with no session), the per-effort flock (a hold takes none)
   or `liveness.Alive` (it reads a crewmate's heartbeat, not a guide). `None`
   (an idle claim) and `Exited` (a dead pane) may close.
2. **One read per tick, one re-read per close.** The table is read once, and
   only when a hold is due and decided. A failed read skips every clearing close
   that tick. Just before each `bd close` the table is read again and the hold
   is skipped if its guide went `Alive` in between.
3. **Accepted gaps.** The window between that re-read and `bd close` is not
   fenced: an idle hold resumed in it (resume takes no claim and no flock) or a
   guide on another clone or host (its table is not local). Brook's guide reads
   the Errand at resume and before its Done close, so the guide that loses
   either race finds it closed, says why and stops.
4. **The close is forced.** The step closes the Errand with `bd.ForceClose`
   (`bd close --force`). The liveness gate is the only guard: another actor's
   idle claim neither blocks the close nor is taken, and the Errand keeps its
   assignee. A plain `bd close` of an `in_progress` Errand claimed by another
   actor is refused.
5. **Close reasons are `<outcome>[: <detail>]`.** Lowercase, a fixed outcome
   word, then an optional detail after a colon and a space. Errand reasons are
   `completed` (the guide), `cleared: <key line>`, `obsolete: …` and
   `withdrawn…`. The bead lifecycle reference states it once in its Legend for
   the spine kinds to follow.

## Considered Options

- **Read the claim or flock**: an idle claim is not a session, so a hold left
  resting would block forever; a hold takes no flock.
- **Close as the claimant's `--actor`**: forges attribution on the close.
- **Reclaim first, then close**: takes the human's claim from them.
- **Fence the close with a lock**: the guide would need to take the lock too,
  a cost out of proportion to a window that the guide's re-reads already make
  harmless.

## Consequences

- A hold whose guide runs on another machine can be closed under it; the guide
  then finds the Errand closed.
- A broken tmux read delays clearing, never causes a wrong close.
