# ADR 0315: v2 Ideate's Backlog nests a P3 or P4 Initiative's whole group, drawn as in the Initiatives section

Status: Accepted
Date: 2026-10-19

Chart mc-zo0xj (P3 and P4 Initiatives list in the Ideate tab's Backlog). Settles
grilling mc-zo0xj.3, which followed the human's try of prototype mc-zo0xj.2
(variant A). Builds on ADR 0314
(tui-v2-ideate-backlog-holds-a-p3-or-p4-initiative-only-while-no-cut-session…),
which says when a P3 or P4 Initiative sits in Backlog, its bands, `Backlog (N)`
and the badges; this ADR says what moves with it and how Backlog draws it.
Amends ADR 0149 (tui-v2-idea-tab-keys-badge…) §1 and §3 and ADR 0172
(ideate-list-in-defogs-row-style…) §3.

## Decision

1. **Membership** (amends ADR 0149 §1's Backlog). When ADR 0314 §1 puts a P3 or
   P4 Initiative in Backlog, its whole group goes with it, out of the
   Initiatives section: its header, every listed member Signal at any priority
   (P0–P4), and its drained hint row. Its P3/P4 members nest under it; they are
   not listed flat.
2. **Being worked keeps the group out** (extends ADR 0314 §1). While any
   member is in triage (in_progress with a session), the whole group stays in
   the Initiatives section, as it does while a cut session is alive or
   launching. So nothing in Backlog is Actionable, and no Actionable or Live
   row twins a row inside Backlog.
3. **Members' bands.** A P3/P4 Initiative's P0–P2 members, while their group
   sits in Backlog, join neither the frontier nor the paused band, as a P3/P4
   Signal joins neither.
4. **Other P3/P4 Signals** stay flat in Backlog with their Initiative's title as
   a hint, as today: a loose one, or a member of a P0–P2 Initiative. Grouped
   beads follow their Initiative's priority, loose ones their own. The open
   Signal "Ideate Backlog holds only unparented P3/P4 Signals" asks for the
   opposite for a P0–P2 Initiative's members; it is a known divergence, left to
   its own triage.
5. **Order** (amends ADR 0149 §3). Inside Backlog, the Initiative groups come
   first in beadCmp order, then the flat Signals in beadCmp order, as the
   Initiatives section sits above No Initiative.
6. **Rendering.** A nested Initiative is drawn at depth 1 exactly as in the
   Initiatives section: a header with its members, or a drained header with
   its hint row (ADR 0178 §2; ADR 0180 §3's `· drained` when folded). An exited
   cut session shows `✕ exited` on the header (ADR 0178 §3, §4). ADR 0182 §5's
   bodyless cue carries over.
7. **Folds.** A nested header folds under the same `i:<initID>` key and starts
   folded (ADR 0179 §3), so a fold survives the Initiative moving between
   sections; it is stored and pruned as ADR 0179 §6 says. A folded Backlog's
   trailer `open` counts every open descendant it hides, each nested
   Initiative's whole open subgraph included (ADR 0179 §4).
8. **Keys and hints.** A nested header and its hint row keep the Initiatives
   section's keys and hints: `enter cut|attach` in place (ADR 0178 §1, §4), `b`
   (ADR 0165), `space`, `d`, `n`, `+/-`, and `members still open · cut from
   Graph` on a non-launchable header. The Backlog header keeps
   `space fold · n new`.
9. **Cursor** (amends ADR 0172 §3). The fold-away fallback covers any selected
   row, not only a Signal: when `+/-`, a launch or a refresh moves a group into
   a folded Backlog, the cursor moves to that group's new header, Backlog's.
10. **Shade.** ADR 0179 §5 is unchanged: selecting a nested row shades its
    Parent chain and the Backlog header.

## Considered Options

- **A leaf row in Backlog, members fall to No Initiative** (variant B), and
  **a leaf row in Backlog, members keep a plain header in Initiatives**
  (variant C). Rejected after the try: the Initiative and its work would sit
  apart, and its P0–P2 members would still read as takeable.
- **A member in triage stays nested in Backlog, Actionable there.** Rejected
  for §2: its waiting row would twin a row folded away at the bottom of the
  tab, which ADR 0314 rejects for a cut session.
- **Only the member in triage leaves, to No Initiative.** Rejected: it splits
  the group.
- **Interleave groups and flat Signals in one beadCmp order.** Rejected: groups
  first matches the sections above.
