# ADR 0223: An orphaned latch closes, and a latch carries onto its blocked bead's successor

Status: Accepted
Date: 2026-10-15

Chart mc-8ymw.6 (A Brook-owned latch that blocks work until a Jira ticket is
done). Settles grilling mc-8ymw.6.6. Extends ADR 0222
(mugicrew-a-jira-latch-is-one-per-key…), which covers a reopened ticket and
added edges but not lost ones. Carry (ADR 0085) only moves a closing bead's
outgoing ordering, so a latch's edge into a bead that closes in succession or is
obsoleted would be left on the closed predecessor, and the successor would run
ungated.

## Decision

1. **Orphaned latch.** A latch is orphaned when every bead it blocks is closed
   (any reason) and its ticket is not Done; Drafts count as open. At its next
   due check, `Run` closes it before any acli read, with reason
   `orphaned: no open bead waits on jira <KEY>`. A latch with some blocked
   beads still open keeps polling. Re-running the verb files a fresh latch
   (ADR 0222 §1: a closed latch does not count).
2. **Paused orphan.** A `paused:failing` latch is out of candidacy, so it is not
   swept: it stays open until a human unpauses or runs it now, and that first
   run closes it orphaned without a read.
3. **Latch carry.** `bd.Carry(pred, succ)` also adds `<latch> blocks <succ>` for
   every open `kind:latch` that blocks `pred`. Add-only, the old edge stays as
   lineage, and the carry event records it among the moved edges. Every
   succession and pivot path inherits it; Carry runs before `pred` closes, so
   the orphan check never sees the gap. A human's raw `bd close` or
   `bd supersede` is not carried; the latch goes orphaned and closes per 1.

## Considered Options

- Keep polling an orphan until Done: spends acli reads on a latch that blocks
  nothing.
- Leave an orphan open but out of candidacy: graph clutter nobody closes.
- No latch carry: a pivot on a gated Checkpoint lets the corrected work land
  before the ticket is Done, defeating the latch.
- Carry every incoming `blocks` edge: changes the meaning of all ordering, not
  just latches.
- A separate `CarryGates` step per call site: a call site can forget it.
