# ADR 0131: mugicrew TUI: Initiatives nest in the Graph tab, not a tab of their own

Status: Accepted; for v2, §4's `h` clause superseded by ADR 0175 (v1 keeps it); for v2, the rejected "Initiatives tab with drill-in" reversed by ADR 0181
Date: 2026-10-11

## Context

An Initiative (ADR 0128) parents every bead of its member chains, including
Chart and Checkpoint containers. Graph today groups one level deep, and makes
every container a flat head, so an Initiative and its member chains would
render as unrelated sibling heads.

## Decision

1. **Nest in Graph.** An Initiative is a group head; its member chain heads
   (Chart, Checkpoint) nest under it with their own members, and member leaves
   with no chain container (Signal, Spec) sit directly under it. Depth is
   capped at three: an Initiative is never a member of another Initiative, and
   adopting one into another is refused. No new tab.
2. **Phase order.** Member chains list in phase order (topological over the
   member `blocks` edges), `beadCmp` breaking ties.
3. **Row and detail.** The head row and the detail pane show `landed N/M` over
   member chains plus the live (open, unblocked) member's title; defog leaves
   are not counted.
4. **Filter and hide.** The existing Graph rule extends down the nesting: a
   matching Initiative keeps its subtree; a matching member keeps its
   Initiative and chain heads for context. `h` hides closed member chains and
   closed Initiatives (the landed count still counts them). Nothing else is
   added; Search already lists `kind:initiative`.
5. **Actions on the Initiative.** `p` cascades (ADR 0081) with wording naming
   member chains; `+`/`-` change the Initiative alone; `x` is refused (no
   effort); `l` launches Luffy.
6. **Adopt and release are TUI keys.** `a` on an unclosed chain head picks an
   open Initiative to adopt it into (a move, if already a member; the picker
   names the current one); `A` releases after a y/n. There is no key to create
   an Initiative: Luffy creates them at triage.
7. **Inbox** shows an Initiative only in a waits-on-human state, if the
   Initiative lifecycle defines one.

## Considered options

- **An Initiatives tab** with drill-in: rejected, a second grouped view of the
  same beads, and folding the Initiative in Graph already hides the effort.
- **Membership as a filter only**, Graph flat: rejected, the phases of one
  effort stay scattered across heads.
