# ADR 0181: v2 Initiatives tab lists open and closed Initiatives as the entry to the focus view

Status: Accepted; §5 hint order superseded by ADR 0229 (…hints-bar-and-key-reference…) (membership kept)

Amended by ADR 0228 (§5: the i entry key becomes g f; enter still opens the focus).
Amended by ADR 0241 (§5: Initiatives' n is the shell menu's n i).
Amended by ADR 0258 (§5: E raw is removed from the Initiatives tab; E is a shell key opening $EXPLORER).
Date: 2026-10-14

Amends ADR 0171 §1 and §5, ADR 0174 §1, and ADR 0164 §7. Reverses ADR 0131's
rejected option "An Initiatives tab with drill-in", and sets aside ADR 0174's
reasoning for having no tab.

Chart mc-isdv (v2 Initiatives tab). This ADR settles grilling mc-isdv.4. That
grilling followed the human's try of prototype mc-isdv.3 (side ref
`refs/mugicrew/prototypes/mc-isdv`) and the research in mc-isdv.2
(`docs/research/initiatives-tab-seams.md`). It is modelled on ADR 0156
(Defog), ADR 0170 (Execute) and ADR 0174 (focus).

ADR 0131 rejected an Initiatives tab because it would be a second grouped view
of the same beads. ADR 0174 relied on that reasoning to give the focus view no
tab and no picker. Once the focus view existed, the only way into it was from a
row on another tab. An Initiative that is under way with no listed Signals has
no row on Ideate, Defog or Execute (ADR 0177 §1), so it could be reached only
by searching Graph's nesting. The new tab lists Initiatives without their
children and is the entry to the focus view. The grouped view of members is
still the focus view itself, and Graph keeps its nesting and the structure
edits, so there is still no second grouped view to keep in step.

## Decision

1. **Tab order** (amends ADR 0171 §1). The tabs are `1` Ideate, `2` Defog,
   `3` Execute, `4` Graph, `5` Initiatives, `6` History, then `#` Stats, `$`
   Logs and `*` Settings. Digits `7` and `8` are unbound. Initiatives is a
   working tab on the digit row. `tab`/`⇧tab` cycle through all nine tabs.
   Leaving a focus entered from this tab returns to it, so the back bar reads
   `from Initiatives` (ADR 0174 §2).
2. **Sections.**
   - The tab has two sections, `Open (N)` and `Closed (N)`.
   - Each row is one Initiative, shown without its children.
   - Open rows are in `beadCmp` order. Closed rows are ordered most recently
     closed first, by the bead's own close time, not by `updated_at`.
   - An empty section still shows its header.
   - With no Initiatives at all, the body is one dim line:
     `no Initiatives yet — Luffy creates them at triage`.
3. **Row line 1.**
   - The line holds the state glyph, prio, title, then the bead id (muted)
     right after the title.
   - A `●` follows the title when the Initiative's cut session is alive and
     waiting on you (your turn or needs approval).
   - A closed row whose close reason is `cancelled: …` adds a muted
     `cancelled` after the title.
   - The row shows no `landed N/M` and no drained, under way or paused tag.
     A pause shows through the glyph (`‖`), and `landed N/M` is in the detail.
4. **Row line 2**, muted, on open and closed rows alike:
   `checkpoints N open · N closed · N open descendants · N live · N waiting`.
   - **live** is shown in green and **waiting** in amber, each dim at 0.
   - Every count covers the Initiative's Parent subtree, with Drafts left out.
   - **checkpoints** counts the `kind:checkpoint` beads in the subtree.
   - **open descendants** counts every unclosed bead in the subtree, not only
     the member chain heads.
   - **live** counts the subtree's beads that have an alive session or an
     alive AFK effort. This is not Graph's `N live` (ADR 0175 §4), which only
     reaches the two levels Graph nests.
   - **waiting** is the focus view's ●N (ADR 0174 §6) over the same scope:
     the subtree plus the waiters on its Checkpoints. Pressing `i` lands on
     the same number. Each row's scope is worked out once per snapshot
     refresh and reused until the next one, never per render or key.
5. **Keys on an Initiative row** (amends ADR 0164 §7 and ADR 0174 §1).
   - `enter` and `i` open the Initiative's focus, closed Initiatives
     included.
   - This is an exemption from ADR 0164 §1, like History's: `enter` does not
     launch or attach the cut session here. The cut session is reached only
     from the focus header (ADR 0174 §3).
   - `i` now works on five tabs: Graph, Ideate, Defog, Execute and
     Initiatives.
   - `d` shows Graph's Initiative detail in the bottom drawer
     (ADR 0171 §2).
   - `b`, `p`, `+/-`, `x`, `a`/`A`, `n` and `e`/`E` are not bound. Pressing
     one does nothing and shows no status text. Graph keeps them.
   - The hints bar reads `enter focus · d detail · ? keys`.
6. **Keys on a section header.**
   - `space` folds the section.
   - `enter` does nothing.
   - `i` refuses with "i: this row is not in an Initiative".
   - The hints bar reads `space fold · ? keys`.
7. **No badge** (amends ADR 0171 §5). The tab shows no ●N. The list of tabs
   with no badge becomes Graph, Initiatives, History, Logs, Stats and
   Settings. A waiting cut session is already counted on Ideate's badge
   (ADR 0177 §2), and it shows here as the row's `●`.
8. **Folds persist per Workspace.**
   - Folds live in the `"initiatives"` section of the shared `folds.json`
     (ADR 0179 §6), and that section stores only flips from the defaults.
   - By default Open is unfolded and Closed is folded.
   - The section keys are `s:open` and `s:closed`, and pruning never drops
     them.
9. **Changes under the tab.**
   - The cursor follows the selected Initiative's id.
   - When the selected Initiative closes or is cancelled, its row moves to
     Closed. If Closed is folded, the cursor moves to the `Closed (N)`
     header.
   - When an Initiative is born, its row appears in its `beadCmp` place and
     the cursor stays where it was.
   - When an Initiative leaves the snapshot, the cursor moves to the nearest
     row above (ADR 0174 §7).

## Considered Options

- **Keep no tab** (ADR 0131, ADR 0174). Rejected. An Initiative that is under
  way with no queue-tab row could reach its focus only through Graph's
  nesting.
- **Row line 1 with `landed N/M`, or with a drained, under way or paused
  tag.** Rejected after the prototype. The second line carries the counts,
  and `d` shows the progress.
- **Open members as unclosed chain heads**, or **live as Graph's two-level
  `liveN`.** Rejected. The tab and the focus view count the same subtree.
- **Count waiting over the Parent subtree only.** Rejected. The count would
  disagree with the focus ●N whenever a side Task or Pivot waits on a member
  Checkpoint.
- **Compute the waiting scope on every render.** Rejected. It costs about
  57 ms per row (research §5.2).
- **Drained Initiatives first in the open section.** Rejected after the
  prototype.
- **Order the closed section by `beadCmp`, or by `updated_at` as a stand-in
  for the close time.** Rejected. The most recently finished Initiative is
  the useful one to see first, and a later edit moves `updated_at`.
- **`enter` launches or attaches the cut session (ADR 0164 §1), `b` as its
  background twin, and only `i` opens the focus.** Rejected. The tab exists
  to enter the focus, and the next phase is decided from the focus header,
  where the members are in view.
- **A ●N of waiting cut sessions** (ADR 0171 §5, ADR 0177 §2), **adding the
  members' waiting sessions to it, or ADR 0179's light-blue count.**
  Rejected. The human chose no badge. Member sessions would be counted
  twice, and the tab has no Actionable set.
- **A per-tab `initiatives-folds.json`.** Rejected. ADR 0179 §6 keeps every
  tab's folds in one file.
- **When the selected Initiative closes, move the cursor to the nearest row
  above.** Rejected. Landing on the Closed header shows where the
  Initiative went.
