# ADR 0410: Effective priority is the most urgent of a bead's own and its container ancestors'

Status: Accepted
Date: 2026-10-29

Amended by: ADR 0419 (mugicrew-succession-stamps-the-predecessor-s-own…): a
successor is born with its predecessor's own priority; §6 and the rejected
stamp-at-creation option concern container members only.

Chart mc-k1x5v (Dispatch priority scheme and how the TUI shows it), settled by
its Survey mc-k1x5v.1. The loop sorted dispatch candidates by own priority
first, with the parent Checkpoint's or Chart's priority only as a tiebreak and an
Initiative counting for nothing, while the TUI tinted a badge from that parent
only at P0 or P1. A human could not tell where a bead would be dispatched
relative to others, and Initiative priority did not reach its members.

## Decision

1. **Effective priority.** A bead's effective priority is the most urgent (lowest
   P-number) of its own priority and the priority of every open container
   ancestor up its parent chain: Checkpoint, Chart and Initiative alike.
2. **The loop sorts on it.** Dispatch orders candidates by (effective priority,
   own priority), both ascending, then `bd ready` order. The TUI shows exactly
   the value the loop sorts on.
3. **The badge shows both levels when they differ.** A row's priority badge
   reads `P0‹2`: the effective level, then the bead's own level dimmed. When own
   is the effective level, the badge is just `P1`. The effective digit takes the
   P0/P1 text colours; the container background tint is dropped. Every bead row
   that shows a priority uses this badge.
4. **`+`/`-` edits own priority.** The drawer names the source,
   "dispatches as P0 from <ancestor kind> <title>"; an edit that leaves the
   effective level unchanged flashes that line.
5. **Rows sort by effective priority** within a TUI section, so list order
   matches dispatch order. No separate dispatch-rank view.
6. **Nothing stamps priority onto members.** A Forge Review files at the default
   priority and inherits live, superseding ADR 0317's item 7
   (mugicrew-a-forge-review-files-whatever-asks…).

## Considered Options

- **Ancestors dominate** (sort by Initiative, then container, then own).
  Rejected: an urgent bead inside a P3 container could never pass a P2
  container's work.
- **Own first, Initiative as a further tiebreak** (the old rule extended).
  Rejected: members mostly sit at the default P2, so own priority is noise and a
  P0 container barely matters.
- **Stamp the container's priority at creation.** Rejected: a later `+`/`-` on the
  container never reaches existing members, and the badge can't show inheritance.
- **Badge shows only the effective level, or own text with a five-level colour
  ramp.** Rejected: the first hides why `+`/`-` seems to do nothing; the second
  can't be read at a glance.
