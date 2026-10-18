# ADR 0171: v2 polish pass: symbol keys for rare tabs, detail is a bottom drawer everywhere, Ideate gets `x`, and ●N counts only waiting sessions

Status: Accepted; §1 and §5 amended by ADR 0181 (5 Initiatives, History on 6; Initiatives has no badge)
Date: 2026-10-14

Amends ADR 0141 §5 and §8, ADR 0142 §1, ADR 0144 §1, ADR 0149 §1, ADR 0156 §3 and §8, and ADR 0170 §4 and §8. Chart mc-lu2x.23 (from
Signal mc-wy3y, "Misc TUI v2 bugs") fixes a batch of v2 defects. Most of the
fixes are look-only. The ones below change recorded decisions. They were
prototyped and kept (mc-lu2x.23.2) and settled in grilling mc-lu2x.23.3.

## Decision

1. **Tab order and keys** (amends ADR 0141 §5 and ADR 0142 §1). The tabs run
   `1` Ideate, `2` Defog, `3` Execute, `4` Graph, `5` History, then `#`
   Stats, `$` Logs, `*` Settings. Digits `6`–`8` are unbound. Each tab's
   label leads with the key that reaches it (`# Stats`). ADR 0142 §1 dropped
   v1's `#` and `*` jumps. They come back here for a different reason: the
   rarely used tabs are kept off the digit row, so the digits stay for the
   working tabs. `tab`/`⇧tab` and `←`/`→` still cycle through all eight.
2. **Detail is a bottom drawer on every tab** (amends ADR 0141 §5 and ADR 0144
   §1). Graph and Logs drop their side-by-side detail, its breakpoint=84
   single-pane mode and the `[i/n]` title suffix. The detail opens as Ideate
   and Defog's does: the list keeps the top half of the body, then comes a
   rule, then the detail viewport. `d` toggles it and `esc` closes it, as
   before. ADR 0144 §7's "detail open" hints row is unchanged: the same
   verbs, `d close`, plus `pgup/pgdn scroll`.
3. **Ideate gets Graph's `x`** (amends ADR 0149 §1). On a Signal row, `x`
   drops, aborts or recovers the Signal's Luffy session exactly as on Graph,
   behind ADR 0142's dialog. It is hinted only when it applies. The Signal row
   becomes `enter triage · d detail · z reject · x drop/abort/recover · n new
   · e edit · E raw · +/- prio · a/A adopt/release`.
   `p` on an open Signal toggles Graph's `paused:manual` (same write, refusal and
   status), hinted `p pause`/`p unpause` just before `x`; a closed Signal lists
   it under `?` only. The row reads `… z reject · p pause · x … · n new …`.
4. **No hitl label** (amends ADR 0156 §3 and ADR 0170 §4). v1's `rowSuffix`
   no longer ends in a right-aligned `hitl…` label on any v2 tab. Where it
   stood, the row shows its normal age.
5. **●N counts only sessions waiting on the human** (amends ADR 0141 §8, ADR 0156 §8
   and ADR 0170 §8; ADR 0149 §4 already reads this way). A tab's badge counts the HITL
   sessions on its own listed rows that are waiting on you (your turn or needs
   approval). Nothing else counts.
   - **Ideate** is unchanged (ADR 0149 §4 already counted only these).
   - **Defog** stops counting ready, unlaunched leaves.
   - **Execute** stops counting its Actionable set. The Actionable section
     still lists it.
   - **Graph, History, Logs, Stats and Settings** show no badge.

## Considered Options

- **One ADR per amended area.** Rejected. These amendments are one pass from
  one Chart, and ADR 0164 set the precedent of one ADR amending several.
- **Keep `6`–`8` for the rare tabs.** Rejected. The human wanted the digit row
  kept for the five working tabs and the order to end on the rarely used ones.
- **Graph's ●N counts waiting sessions on its visible rows** (a global inbox)
  **or all visible beads.** Rejected. Graph shows every bead, so its badge
  would repeat the queue tabs' counts. A count of all beads is not something
  you can act on.
- **A different drawer split for Graph's deeper tree.** Rejected after the
  prototype. The human tried it and kept h/2.
- **Count ready, unlaunched leaves (Defog) or Actionable items (Execute).**
  Rejected. A badge should mean "someone is waiting on you". A ready leaf
  waits for you to choose it, and the tab's list already shows that.
