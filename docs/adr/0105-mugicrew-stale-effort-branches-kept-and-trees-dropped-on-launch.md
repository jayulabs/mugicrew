# ADR 0105: mugicrew stale effort branches — the ref is kept for roll forward, the tree is dropped through the orphan confirm, and a closed bead's live ref blocks its gates loudly

Status: Accepted
Date: 2026-10-08
Amends: ADR 0102 §9/§10 (gates, holds), ADR 0082 §2/§4 (event, recovery), ADR 0041 §2 (orphan), ADR 0104 §7 (drained hint).

## Context

ADR 0102 makes an effort branch outlive a dropped, failed or orphaned session.
A force stop, a confirmed TUI quit, a SIGKILL or a crash can leave
`effort/<id>`, its loop `mugicrew-wt-*` tree and the claim behind (ADR 0082).
§9 counts any live effort ref as open in the drain gates. So a bead that is
**closed** but still has a ref that no backstop will process blocks its
container forever. This happens when a host dies between the agent's close and
the backstop, or when a session obsoletes a bead that a failed effort left a
ref on. Lease self-heal stays with Chopper. Decided in grilling.

## Decision

1. **The ref stays.** Abandon, crash, unclaim and drop never delete
   `effort/<id>`. The next launch reuses it (ADR 0102 §1, roll forward), and
   its integrate decides: squash if ahead, delete if not. To discard the work,
   the human runs `git branch -D effort/<id>`. No verb is added.
2. **The tree goes through the orphan confirm.** A loop tree holding
   `effort/<id>` is an orphaned worktree (ADR 0041 §2) once no in-flight
   effort accounts for it, which is the case after the human has unclaimed the
   bead and lifted `paused:abandoned`.
   - `l` offers ADR 0041 §3's `y drop it and launch`.
   - `t` keeps ADR 0104 §3: it drops a clean tree and refuses a dirty one
     with a hint naming the tree.

   Nothing removes a tree automatically (ADR 0041 §4).
3. **If the loop gets there first, it is a foreign hold.** After the unclaim
   and the lift, a tick that dispatches the bead hits the stale tree. It skips
   the bead as a foreign hold and may pause it `foreign-hold` (ADR 0054/0070).
   That pause lifts once the human drops the tree. What foreign-hold means for
   effort branches is a separate question.
4. **A closed bead with a live ref blocks loudly, not automatically.**
   - Press-time drain gates refuse and name each closed child that still has
     `effort/<child>`, with both ways out: run `mugicrew integrate` in a tree
     on the branch, or discard it with `git branch -D effort/<child>`.
   - Automated gates (Franky's Review spawn, Jinbe) wait silently, as for
     any open child.
   - The TUI's "Chart not drained" hint (ADR 0104 §7) names those beads.
5. **What the human is told.** `effort.abandoned` gains `branch`. ADR 0082
   §4's recovery becomes:
   1. Check that the orphan is dead.
   2. Run `bd unclaim` and lift `paused:abandoned`.
   3. Press `l` or `t`, which handles the tree as in §2. The ref is reused.

## Considered and rejected

- **The loop drops a clean stale tree itself, or runs the backstop on closed
  beads with live refs.** This is self-heal (Chopper's job). It would also
  integrate work the human may have closed as obsolete on purpose.
- **Deleting the ref when its tree is dropped.** Committed work would be lost,
  and roll forward with it.
- **A `mugicrew effort drop <id>` verb.** `git branch -D` already does it.
- **Counting a ref in the gates only when it is ahead.** A ref that is ahead
  still blocks, and the check would disagree with ADR 0104 §7's footer, which
  reads no ahead counts.
- **`t` confirming a drop of a dirty tree, as `l` does.** Dirty abandoned AFK
  trees are rare. The refusal hint points to `l`.
- **A Warn event when an automated gate waits on a dangling ref.** It would be
  noise on every tick, or need state to deduplicate. Add one if this bites.

## Consequences

- No new verb, sweep or pause cause is added. Only the event field, the
  gates' refusal text and the TUI hint change.
- A ref can sit unnoticed under a closed bead until a gate names it. That is
  the accepted cost of not sweeping.
