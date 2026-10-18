# ADR 0177: v2 Ideate lists an Initiative when it is drained or while its cut session exists, and ●N counts it only while that session waits

Status: Accepted

Date: 2026-10-14

Chart mc-lu2x.31 (drained Initiatives on the Ideate tab). Settles grilling
mc-lu2x.31.4. Amends ADR 0149 §4 and ADR 0172 §3. Before this ADR, Ideate drew
an Initiative header only above listed Signals, so a drained Initiative, which
waits on the human, never showed there.

## Decision

1. **Which Initiatives are listed** (amends ADR 0172 §3). An open Initiative
   gets an Ideate header when any of these holds:
   - it has listed Signals (as before);
   - it is **drained** (no unclosed member, Drafts included, ADR 0133 §1);
   - its cut session exists, alive or exited. This is the rule Signals follow:
     an in_progress Signal is listed while it has a session.
   An under-way Initiative that has no listed Signals and no cut session stays
   off Ideate. Its unclosed members are Charts, Specs or Checkpoints, and Graph
   already shows it.
2. **●N** (amends ADR 0149 §4; ADR 0171 §5 stands). The badge also counts a
   listed Initiative whose cut session is alive and waiting on the human (your
   turn or needs approval). Being drained does not count by itself, and an
   exited cut session does not count.
3. **Paused.** A paused Initiative is listed by the same rule. `enter` asks
   the same y/n that Graph asks (ADR 0164 §2), because Ideate reuses Graph's
   launch path. A pause does not change the badge, which follows the session
   alone.
4. **Offshoots.** An offshoot Signal of a member stays under 'No Initiative'
   and does not un-drain the Initiative until the human adopts it.

## Considered Options

- **List drained Initiatives only.** Rejected. A cut session files the next
  phase as a Draft (ADR 0132 §5). That Draft un-drains the Initiative, so its
  header would leave Ideate while the session is asking the human to approve.
  A cut session called early on an under-way Initiative would not show at all.
  A session waiting on you should not drop off the tab whose badge counts it.
- **Count every drained Initiative in ●N.** Rejected. The badge means "blocked
  on you now" (ADR 0171 §5). A drained Initiative with no session waiting is
  already shown by its header.
- **List every under-way Initiative.** Rejected. Ideate lists Signals and
  handler waits, and Graph covers under-way Initiatives.

## Consequences

An Initiative that is listed only because its cut session exists is not
drained, so the drained hint row's wording does not fit it. That look is left
to a separate grilling on this Chart.
