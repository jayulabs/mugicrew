# ADR 0204: v2 Defog tab gains a Charts section and a Chart status word on every open Chart

Status: Accepted
Date: 2026-10-16

Chart mc-02ij (Defog tab Charts section and Chart status word). Settles
grilling mc-02ij.4 after research mc-02ij.2
(`docs/research/chart-lifecycle-stage-read-sources.md`) and prototype
mc-02ij.3 (side ref `refs/mugicrew/prototypes/mc-02ij`, replayed over 106
Charts with 0 `idle ⚠`). This is the Chart counterpart of ADR 0198
(tui-v2-execute-checkpoints-section-and-rollup-word). Every word derives I/O-free
from the bead snapshot, sessions/liveness and effort refs. A Chart word whose
meaning matches a Rollup word reuses its spelling. Only Chart-only meanings get
new words. The Rollup is unchanged.

## Decision

1. **The Chart status word**, first match wins:
   1. `speccing`: a Landing closed `landed:`, and the Spec session holds the Chart
   2. `landed`: a Landing closed `landed:`, the Chart not yet closed (Rollup spelling)
   3. `land failed`: an open Landing carries `paused:failing`. Any other
      `paused:*` reads `land paused` at the same rank.
   4. `landing`: an open Landing
   5. `conflict`: an open chart-conflict
   6. `blocked on <Kind>`: an open blocker on the Chart itself. Add ` +N` for more.
   7. `failing` / `paused`: the Chart's own `failing` / `paused:*` label
   8. `charting` / `survey due`: an open Survey, in session / not in session
   9. `in defog session` / `your turn`: an open HITL defog child (grilling,
      prototype). Its session is working / it is unlaunched or its session waits
      on the human.
   10. `defogging` / `defog queued`: an open AFK defog child, running / unblocked
   11. `fog blocked on <Kind>`: every open defog child waits on a bead outside
       the Chart. Add ` +N` for more.
   12. `drafts pending`: an unpublished Draft child
   13. `integrate due`: a closed child still holds `effort/<child>` (the
       `DrainBlockersOver` held sense, not the glossary's **Held**)
   14. `judging`: drained, unstamped, promote-or-stamp session on the Chart
   15. `judge due`: drained, no `chart:defogged` yet
   16. `landing due`: stamped, drained, no Landing yet
   17. `idle ⚠`: no rule matched. This is a bug and is worth a Signal.

   `landed`, `judge due` and `landing due` last one loop tick while the loop
   runs. They stay words because they persist when it is off (as ADR 0198 keeps
   `approved` and `review due`).
2. **Qualifiers** ride beside the word, never inside it:
   - `round N`, only when N ≥ 2 (N is the number of Landings cut)
   - `re-land`, once a Landing closed `obsolete:`
   - `retrying`, on `landing` when its latest land/gate event is a failure or
     infra retry. The queued, attempting and gate-running phases are not shown.
3. **Charts (N) section** on the Defog tab only, between Live and Initiatives,
   unfolded by default.
   - One row per open published Chart, `chart:defogged` ones included: v1's bead
     look, then the word and its qualifiers. Rows sort by rank, then `beadCmp`.
   - The rows are status only. They are not Actionable and are not counted in
     ●N. `enter` and `d` act as on the Chart's tree row.
4. **Finished in last 24h (N)** is a subsection nested at the end of Charts,
   folded by default.
   - A row lists every Chart closed within 24h: v1's bead look plus
     `<outcome> · <age>`, and `landed <sha8> · <age>` for a landed one.
     Newest first.
   - The outcome, first match: `landed` (a Spec is caused by it), `handed back`
     (`handed back:`), `rejected` (`rejected`), `superseded` (empty reason),
     else `closed`.
   - `d` shows the Chart's detail with the deciding bead or close reason as its
     why line. `enter` refuses: "finished — nothing to launch".
5. **`d` explains the word** (as ADR 0198 §5): a `why:` line names the driving
   bead or event (e.g. "Grilling X ready to launch", "Landing Y closed obsolete:
   …"). An `also:` line lists the other rules that matched, shown only when
   precedence decided between them.
6. **No tree-row bracket**, and the Initiative focus view's Chart rows are left
   unchanged. The word is one pure function over `graphData`, so a later Signal
   can reuse it there.

## Considered Options

- **`landed · Spec due`.** Rejected. It means what the Rollup's `landed` means.
- **Landing sub-phases (queued/attempting).** Rejected, as in ADR 0198. Only
  `retrying` asks for attention.
- **Folding `fog blocked` into `blocked on <Kind>`.** Rejected. The Rollup word
  means the container's own blocker. This means the fog's.
- **`held` for an unintegrated closed child.** Rejected. It would overload the
  glossary's **Held** (integrate refused, bead still open).
- **A single `your turn` for all HITL fog.** Rejected. A working session is not
  waiting on the human.
- **A 7-day finished window.** Rejected. It held 54 rows against 13 for 24h.
