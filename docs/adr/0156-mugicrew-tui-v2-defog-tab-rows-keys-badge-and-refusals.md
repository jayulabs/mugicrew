# ADR 0156: v2 Defog tab rows, keys and badge; enter refuses blocked and paused leaves

Status: Accepted; §7 hint order superseded by ADR 0229 (…hints-bar-and-key-reference…) (membership kept)

Amended by ADR 0164 (§5 and §7: Defog adopts the shared `enter` verbs; a drained Chart row runs spec).
Amended by ADR 0171 (§3: no hitl label; §8: ●N counts only waiting sessions).
Amended by ADR 0241 (n is no longer unbound: it opens the New menu).
Amended by Spec mc-j3mw8 (§1: a defogged Chart with an open child stays listed).
Amended by ADR 0258 (§7: E raw is removed from the Defog tab; E is a shell key opening $EXPLORER).
Date: 2026-10-13

Chart mc-lu2x.9 (Defog tab for the v2 TUI). Settles grilling mc-lu2x.9.4,
which followed the human's try of prototype mc-lu2x.9.3 and research
mc-lu2x.9.2. Adds the Defog tab's row to ADR 0144 §7 and fills in ADR 0141
§8's badge for this tab, alongside ADR 0149 (Idea tab).

## Decision

1. **What is listed.** Every open or in-progress, published Chart (Drafts are
   excluded) that does not carry `chart:defogged`, grouped by Initiative as on
   the Idea tab. A defogged Chart leaves the tab even before its Spec lands,
   because it no longer has fog waiting on the human, unless it still has an
   open child (open or in_progress, not a Draft, not an event). Then it stays
   listed and renders as any listed Chart, and it leaves once it has none. Its
   Landing, MR Watch, Forge Review and review grillings (ADR 0199, see
   a-chart-merge-request-comment-is-a-grilling…) are Defog's work, and review
   children can outlive the MR Watch.
2. **Rows and order.** Groups and rows use Graph's `beadCmp`, and 'No
   Initiative' comes last. Under a Chart come its Survey, then the other open
   HITL leaves, then each open AFK bead as its own row, then one '▸ N closed'
   row. That row is collapsed by default, is omitted when there is nothing
   closed, and expands into rows that can't be launched.
3. **Row marks.** An Initiative row is an INITIATIVE chip and its title, with
   no glyphs. Every other row reuses v1's look: `stateGlyph`+`rowDot`, the
   `kindTag` badge, then `P<n>`, title and id. The right edge is v1's
   `rowSuffix` (launching…, closing…, your turn / needs approval, age, hitl…).
   A blocked leaf shows `⊘`. A Chart has no rollup or fog bar, because its
   denominator shifts as new fog appears.
4. **`d` detail** (ADR 0144 §1):
   - On a Chart: state, status, fog/HITL/AFK counts, enter's verb,
     Destination and Not-yet-specified, and the counts of the other sections.
   - On a leaf or AFK row: kind/owner, status, state, session, blocked-by,
     inherited pause, enter's verb and the body.
   - On '▸ N closed': its members and their close reasons.
5. **enter** is mode-aware:
   - On a HITL leaf it launches or attaches.
   - On an AFK row it runs now, with Graph's eligibility check and its y/n
     dialog for a blocked bead.
   - On a Chart, Initiative or summary row it does nothing, and the status
     line says why. It never folds (ADR 0144 §2).
   - On a **blocked** HITL leaf it refuses and names the blocker.
   - On a **paused** HITL leaf, whether the pause is its own or inherited, it
     refuses and names the pause.
6. **Other keys.**
   - `space` folds the selected Chart, Initiative or '▸ N closed' row.
   - `p` toggles `paused:manual` (Graph's write and status; refused on a
     closed bead; a no-bead row ignores it), `x` means drop/abort/recover,
     `+/-` changes priority, and `E` is the raw edit, all as on Graph.
   - `E` stays bound but is listed only in `?` keys, never in the hints bar.
   - `n`, `e`, `z` and `t` are not bound.
7. **Hints bar** (ADR 0144 §7), in drop order. Every row ends with `? keys`.

   | row / mode | hints, in order |
   |---|---|
   | HITL leaf | enter launch/attach · d detail · p pause/unpause · x drop/abort/recover · +/- prio |
   | AFK row | enter run · d detail · p pause/unpause · x drop/abort/recover · +/- prio |
   | Chart row | space fold · d detail · p pause/unpause · x drop/abort/recover · +/- prio |
   | Initiative or '▸ N closed' row | space fold · d detail |
   | closed child row | d detail |
   | detail open | the row's hints, `d close`, plus `pgup/pgdn scroll` |

   `enter` shows only when it would act. On a blocked or paused leaf it is
   left out of the bar.
8. **The Defog tab's ●N** counts two kinds of HITL leaf under listed Charts:
   leaves whose session is waiting on the human (your turn or needs approval),
   and ready, unlaunched leaves. Blocked leaves and paused leaves (own or
   inherited pause) don't count.

## Considered Options

- **Confirm a blocked leaf with y/n, as Graph does.** Rejected. Pressing
  `enter` on Defog means "answer this fog now", and the question of a blocked
  leaf depends on another bead. Offshoot Signal 'One mode-aware enter (launch
  HITL, run AFK) across v2 tabs' may later bring the two tabs in line.
- **Glyph+word, glyph-only or next-step-word marks.** Rejected after the
  prototype. v1's look is already familiar and carries the live state.
- **A collapsed '▸ N/M afk' summary.** Rejected. Each AFK bead is worth seeing.
- **Keep defogged-but-unlanded Charts listed.** Rejected for a childless
  Chart: it has no fog left to clear and nothing else waiting on the human.
- **`E raw` in the hints bar.** Rejected. It is rarely used, so it stays in
  `?` only.
