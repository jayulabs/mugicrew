# ADR 0146: v2 Graph `enter` on an open Initiative head launches or attaches the cut session

Status: Accepted

Amended by ADR 0164 (§2: a paused Initiative head asks y/n before its cut session).
Amended by ADR 0238 (§3: the ON LAUNCH preview line is removed).
Amended by ADR 0239 (§1: the cut session claims the Initiative).
Date: 2026-10-13

Amends ADR 0144 §2. Pivot mc-u2r9 found that v2 dropped v1's `l`, the only
way to start Luffy's cut session (ADR 0132 §7), and ADR 0144 §2 made `enter`
refuse an Initiative head, so v2 could not cut at all.

## Decision

1. On an open Initiative head, Graph `enter` launches or attaches Luffy's cut
   session (ADR 0132 §7) and leaves the Initiative unclaimed, as v1's `l` did.
2. `enter` still does nothing, with the reason on the status line, on a closed
   bead with no session (a closed bead with a session attaches or resumes it,
   as v1 did, ADR 0038 §3, FR34), on a bead with a write in flight, or a
   container that is neither HITL nor an Initiative. It never falls back to
   folding.
3. The detail pane's ON LAUNCH preview agrees with what `enter` does.
4. Everything else in ADR 0144 §2 stands. Its list of rows that can't launch
   no longer includes an open Initiative head.

## Considered Options

- **Move the cut session to another key or tab.** Rejected. `enter` already
  means "launch or attach" on every other row, and v1's `l` is deliberately
  not kept as an alias (ADR 0144 §2).
- **Lose the cut session from v2 until a later phase.** Rejected. It is the
  only way to cut an Initiative, so v2 would not be usable in its place.
