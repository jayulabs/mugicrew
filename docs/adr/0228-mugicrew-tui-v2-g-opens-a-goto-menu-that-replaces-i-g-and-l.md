# ADR 0228: v2 `g` opens a goto menu that replaces `i`, `G` and `L`

Status: Accepted
Amended by ADR 0271 (§3, §4, §5, §6: o origin, origin and offshoot rows; discovered-from no longer out).
Amended by ADR 0241 (its ADR 0144 §7 exception note: now two exceptions, g goto and n new).
Date: 2026-10-16

Amends ADR 0142 §2, ADR 0144 §2, §3, §4 and §7, ADR 0174 §1 and ADR 0181 §5.

Chart mc-4rou, Spec mc-94e2. The human had three standalone context-move keys
(`i` Initiative focus, `G` reveal in Graph, `L` scoped Logs), each offered on a
different subset of tabs, with no key for a bead's parent, predecessor or linked
beads and none for the top or bottom of a list. A shell-wide `g` now opens one
goto menu, and the three keys are removed with no aliases.
Research: docs/research/goto-menu-superseded-key-touchpoints.md.

## Decision

1. **The menu is Recents' picker with per-row keys.** `g` opens a centred modal
   in the picker chrome of ADR 0184 §2. A picker dialog gains an optional key
   list beside its rows. A key that matches a row sets the cursor there and runs
   the dialog's act, as `enter` does. `j`/`k` (wrap-around), `enter` and the
   height-bounded window are reused. The menu sets no close key, because `g` is
   an option inside it, so only `esc` closes it. Pickers stay out of the modal
   `ctrl+c` quit, as Recents does.
2. **`g` is a shell key** checked after the modal, `?` and text-input guards, in
   the place of `R`/`D`/`S` and before the focus view's key handler, so it works
   in the focus view. It does nothing when the builder yields no rows (no list
   and no selected bead). It is in the binding set of every mode where it can
   open the menu, so binding coverage holds.
3. **Rows, in order, with the key carrying the meaning** (an inapplicable option
   is left out):
   - tab options: `g`/`t` top and `G`/`b` bottom of the current list (and of the
     focus view), skipping non-selectable rows as `j`/`k` do;
   - bead options: `f` Initiative focus, `r` reveal in Graph, `l` scoped Logs,
     `p` parent, `c` caused-by predecessor;
   - related beads.
4. **Applicability.** `f`: an Initiative resolves from the cursor row (the
   existing resolution, so a header is its own Initiative) and the focus view is
   not open, which keeps ADR 0174 §1's no-nesting. `r`: a bead is selected and
   the tab is not Graph (ADR 0144 §2). `l`: a bead is selected. `p`, `c`: the
   target resolves in the snapshot. Logs and History gain a `selected()` bead
   (the event's bead; the commit row's id), looked up in the snapshot. A
   missing id or an event with no bead means no selection.
5. **Actions reuse today's messages.** `r` sends the reveal message and `l` the
   scoped-Logs message (the bead plus its children for a container), exactly as
   `G` and `L` did. `f` calls the existing focus open. `p`, `c` and related rows
   call the Recents jump of ADR 0183 on the target. No goto option counts as a
   Recents interaction (ADR 0184 §1: a jump is a visit).
6. **Related beads** are one hop over the snapshot's edges, grouped in the order
   blocked-by, blocks, children, successors (caused-by reversed), related (either
   direction). Within a group open beads come before closed, in snapshot order.
   A bead in two groups shows once, in the first. The parent and predecessor are
   `p`/`c` and are not repeated. Each row is the Recents row with a relation tag
   in front, closed rows dimmed. The first nine carry keys `1`–`9`. Later rows
   have no key and are reached with `j`/`k` and `enter`. There are no group
   headers: the tag shows the group. `discovered-from`, `supersedes` and
   multi-hop relations are out (`p` then `g p` climbs further).
7. **The standalone `i`, `G` and `L` keys are removed**, with their bindings,
   handlers and hint entries together: the focus-entry `i`, Logs' `G` and `L`,
   History's `G` and `L`, and Graph's `L`. No alias remains.
8. **Hints and `?`.** The hints bar shows a single `g goto` on every tab and in
   the focus view wherever the menu would open, in place of `i focus`, `G
   reveal`, `enter/G reveal`, `L logs` and `L scope`. The `?` reference draws the
   goto options as a third group beside the tab keys and the shell keys. The
   menu's own hints list its option keys.

## Amendments

- **ADR 0142 §2.** `G` and `L` as shell-wide context moves become `g r` and
  `g l`. The menu carries the selection, which answers that ADR's rejected "Drop
  `G`/`L` too" option ("a digit can't carry the selection across tabs").
- **ADR 0144 §2, §3, §4.** The per-tab `G` and `L` mappings go: Graph's `L logs`
  (§2), History's `enter`/`G` reveal and `L` logs (§3), Logs' `G` reveal and `L`
  scope (§4). Each tab's bead moves are now `g r` and `g l`.
- **ADR 0144 §7.** "Shell keys stay out of the bar" has one exception: `g goto`
  shows in the hints bar. The hint table drops the `i focus`, `G reveal` and `L`
  entries named above.
- **ADR 0174 §1** and **ADR 0181 §5.** The `i` entry key becomes `g f`; where
  they say `i` works or refuses, read `g f` (an unresolved Initiative hides the
  option instead of refusing). Inside the focus view the option is hidden, so
  `i` stays unbound there. The Chart named ADR 0173, but ADR 0174 §1 owns the key.

## Considered Options and notes

- **No two ways.** History keeps `enter` for reveal beside `g r`, and Initiatives
  keeps `enter` for the focus beside `g f` (ADR 0181 §5). The menu is a uniform
  map of every context move, and `enter` is the tab's own primary action, so
  neither pair is the aliasing ADR 0142 ruled out.
- **Wording.** ADR 0143, ADR 0175 and ADR 0176's "`G` reveal" now means `g r`;
  the behaviour they describe is unchanged.
- **ADR 0138** ruled out `g`/`G` top/bottom because `G` was the Graph tab on
  every tab. That reason no longer holds, and `g`/`G` are top/bottom in the menu.
- **Remap (mc-czlu).** The menu first keyed focus on `i` and reveal on `f`, so
  `f` read "focus" but revealed. Focus is now `f` and reveal `r` (mnemonic
  **f**ocus, **r**eveal); `i` leaves the menu with no alias. Only the letters
  moved; applicability, actions and row order are unchanged.
- **Keep `i`/`G`/`L` as aliases** was rejected under ADR 0142's "no two ways".

## Consequences

Inside the menu `l`, `p`, `b`, `f`, `r` and `1`–`9` shadow their outside meanings
(next tab, pause, background, kind filter, retrospective, tab jumps). Outside it they are
unchanged. `t` stays unbound outside the menu (ADR 0164 §2).
