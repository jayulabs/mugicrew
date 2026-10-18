# ADR 0170: v2 Execute tab rows, keys and badge; idle Surgery and Task resume, and the shared gates refuse held runs up front

Status: Accepted; §3 amended by Spec mc-qgtw (Actionable gains a paused band for own-paused ready beads); §7 hint order superseded by ADR 0229 (…hints-bar-and-key-reference…) (membership kept)

Amended by ADR 0171 (§4: no hitl label; §8: ●N counts waiting sessions, not the Actionable set) and ADR 0198 (§4: Checkpoint rows gain a bracketed Rollup word).
Date: 2026-10-13

Chart mc-lu2x.20 (Execute tab for the v2 TUI). Settles grilling mc-lu2x.20.4,
which followed the human's try of prototype mc-lu2x.20.3 and research
mc-lu2x.20.2 (docs/research/execute-tab-rows-verbs-and-core-seams.md, whose
row table this ADR confirms). Modelled on ADR 0156 (Defog). Fills in ADR 0164
§8's Execute row mapping and ADR 0141 §8's badge for this tab, and amends
ADR 0164 §3.

## Decision

1. **What is listed.**
   - Every open, published Checkpoint (Drafts are excluded), whatever its
     branch state. Surgery Checkpoints and successor Checkpoints are included.
   - Every open, published Spec.
   - Tasks and Pivots that wait on, hold or gate a listed Checkpoint. A Pivot
     with no `blocks` edge on a listed Checkpoint yet is not listed: the raise
     tail gates it in the same pass, and Graph shows it until then.
2. **Layout.**
   - An **Actionable** section opens the tab, with a placeholder when it is
     empty.
   - Below it, groups by Initiative (`Parent`, as on Defog), with 'No
     Initiative' last. A surgery Checkpoint always falls under No Initiative.
   - Within a group: Checkpoints, then Specs, then the Tasks and Pivots, each
     in `beadCmp` order.
   - Checkpoints start collapsed. Unfolded, every open member gets its own row,
     then one '▸ N closed' row (collapsed, and omitted when nothing is closed).
   - Tasks and Pivots are never shown under a Checkpoint, because they are not
     members. Each sits under its own Initiative, falling back to the
     Checkpoint's Initiative (offshoot Signal mc-qh7m will parent them).
3. **Actionable** means these beads, open and neither blocked nor paused (own
   pause or inherited):
   - an Approval or Surgery member of a listed Checkpoint
   - a before-land Task that the Checkpoint is blocked by
   - a Pivot that gates a listed Checkpoint

   An idle Surgery or Task counts (see 6). An after-land Task is blocked by
   its open Checkpoint, so it is never Actionable. It shows in the tree with
   its hint. A member that is in Actionable also keeps its row in the tree.
4. **Row marks.**
   - Every bead row is v1's look, as on Defog (ADR 0156 §3): `stateGlyph`,
     `rowDot`, `kindTag`, `P<n>`, title and id, then v1's `rowSuffix` at the
     right edge.
   - A Checkpoint has no branch-state chip and no Build rollup. A Spec has no
     stage chip. Any bracketed status added later goes after the title and id.
   - Task and Pivot rows carry a context hint: 'holds land of → <cp>',
     'waits on land of → <cp>' or 'gates → <cp>'. An Actionable member also
     shows 'in → <cp>'.
5. **`d` detail** (ADR 0144 §1):
   - **Checkpoint:**
     - owner/pri and status
     - members closed/total, by kind
     - its before-land Tasks, after-land Tasks and gating Pivots
     - its Spec, from a snapshot `SpecOf` that also follows a Rollback's
       related target
     - blocked-by and pause
     - enter's verb or refusal, then the body
   - **Spec:** stage, its Checkpoint if one is cut, blockers, enter's verb or
     refusal, then the body.
   - **Leaf** (member, Task or Pivot): state word, session, its Checkpoint and
     relation, blocked-by, pause, enter's verb or refusal, then the body.
   - Headers and summary rows work as on Defog.
6. **enter and b.** Execute is a queue tab (ADR 0164 §4 and §8, ADR 0165) and
   routes as Defog does:
   - HITL rows go to `launchAs`, AFK rows to `runNow`.
   - A blocked or paused HITL leaf is refused, naming the blocker or pause.
   - A blocked AFK row (a Spec or Build with an open Ordering edge) keeps the
     shared "Run blocked bead now?" y/n, as on Defog.
   - Header, '▸ N closed' and placeholder rows have no bead: "nothing to launch
     or run here — space folds".
7. **Other keys and the hints bar** are Defog's (ADR 0156 §6–§7, as amended by
   ADR 0164 §6):

   | row / mode | hints, in order |
   |---|---|
   | HITL row | `enter <verb>` · `b bg` · `d detail` · `p` pause/unpause · `x` drop/abort/recover · `+/-` prio |
   | AFK row | `enter <verb>` · `d detail` · `p` pause/unpause · `x` drop/abort/recover · `+/-` prio |
   | Checkpoint row | `enter <verb>` · `space fold` · `d detail` · `p` pause/unpause · `x` drop/abort/recover · `+/-` prio |
   | Section, Initiative or '▸ N closed' row | `space fold` · `d detail` |
   | closed row | `d detail` |

   `enter` and `b` show only where they would act. Every row ends with
   `? keys`.
8. **Execute ●N** is the Actionable count.
9. **The shared gates** (the hints, Graph and Defog all get these):
   - **Idle Surgery and Task resume.** `launchWhy` no longer refuses an
     in-progress Surgery or Task with no session. `enter` relaunches it
     against its existing claim, as `launchNow` already does and as the
     "idle — enter to resume" suffix says.

     This amends ADR 0164 §3: "an in-progress HITL bead with no session ('x
     recovers it')" now covers every other HITL kind. For a Surgery, the claim
     taken at filing is its resting state. For a Task, being idle is how it
     waits (GLOSSARY "Task"). Neither claim is stranded.
   - **The Surgery hold, with no I/O.** `runNowWhy` refuses a Critique whose
     Checkpoint has an open Surgery (an open `kind:surgery` bead with the same
     `Parent`). It uses the press-time text: "held by open Surgery <id> —
     ending the Surgery releases it" (ADR 0160).
   - **The parent gate, with no I/O.** `runNowWhy` refuses a member whose
     Checkpoint has an open blocker other than a Task, e.g. "<cp> is gated by
     <title> (<id>) — not running". This mirrors `loop.Readmit`: when every
     blocker is a Task, the members still run.
   - **Review due.** A Checkpoint whose Review is due still refuses `enter`,
     but the text says "review due — the loop spawns it" instead of "needs a
     closed Approval". If telling review-due apart needs git I/O at view time,
     the existing text stays.

## Considered Options

- **Keep the "x recovers it" refusal for idle Surgery and Task.** Rejected.
  It leaves a Surgery that was dropped, its normal state, unlaunchable without
  a recover first, and the badge counts a row that `enter` refuses.
- **Exempt idle Surgery and Task on Execute only.** Rejected. Then `enter`
  would mean different things on different tabs.
- **A Checkpoint branch-state chip (cut/in-review/approved), or a Build
  rollup.** Rejected after the prototype. The snapshot can't tell a stale
  Approval from one at HEAD, and the rows read fine without either.
- **Show waiting Tasks under their Checkpoint too.** Rejected. They are not
  members, and the hint names the Checkpoint.
- **An `enter review` verb on a review-due Checkpoint.** Rejected. The loop
  spawns the Review on its next tick.
- **Refuse blocked AFK rows as a queue tab.** Rejected. Forcing a run past an
  Ordering edge is a deliberate override, and Defog keeps the y/n too.
- **Leave the Surgery-hold and parent-gate checks to press time.** Rejected.
  The hints offered `run` on rows that were always refused.
