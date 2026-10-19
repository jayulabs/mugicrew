# ADR 0314: v2 Ideate's Backlog holds a P3 or P4 Initiative only while no cut session is alive or launching

Status: Accepted

Date: 2026-10-19

Chart mc-zo0xj (P3 and P4 Initiatives list in the Ideate tab's Backlog). Settles
grilling mc-zo0xj.4. Amends ADR 0180
(ideate-actionable-lists-drained-and-cut-session-initiatives-as-one-row) §1 and
§5. How Backlog draws the Initiative and where its P0–P2 members list are left
to a separate grilling on this Chart.

## Decision

1. **Where the tree header sits.** A listed P3 or P4 Initiative whose cut
   session is alive (the agent working, or waiting on the human), or whose
   launch is in flight or held, keeps its tree header in the Initiatives
   section. It moves there when the launch starts, as a Backlog Signal leaves
   Backlog when it is claimed. With no cut session, or an exited one, drained
   or not, it sits in Backlog and leaves the Initiatives section. This mirrors
   an in-triage P3 Signal, except that an exited cut session sends the
   Initiative to Backlog (settled at triage).
2. **Bands** (amends ADR 0180 §1). A P3 or P4 Initiative is Actionable only
   in the waiting band, while its cut session waits on the human. It joins
   neither the frontier (drained with no session or an exited one) nor the
   paused band (own-paused and otherwise ready), as a Backlog Signal joins
   neither. `enter` on a paused one still asks the y/n.
3. **Live.** A P3 or P4 Initiative being worked or launched shows in Live
   with no Backlog cue. By §1 its twin header is always in the Initiatives
   section.
4. **`Backlog (N)`** counts every bead listed under Backlog exactly once,
   Initiatives and Signals alike. A member listed elsewhere is not counted.
5. **Badges** (amends ADR 0180 §5). Amber `●N` is unchanged: a waiting P3 or
   P4 cut session still adds 1. The light-blue Actionable count drops the P3
   and P4 Initiatives that §2 takes out of the frontier and paused bands, and
   still equals the Actionable header's count. Backlog has no badge.
6. **Which Initiatives list** is unchanged (ADR 0177 §1). A P3 or P4
   Initiative with no listed Signals and no cut session stays off Ideate,
   Backlog included.

## Considered Options

- **Move the header to Backlog as soon as the Initiative is P3 or P4,
  whatever its session.** Rejected. The Live or waiting row would twin a
  header folded away at the bottom of the tab, and an in-triage P3 Signal
  already stays in its group while it is being worked.
- **Move the header when the session shows alive, not at launch.** Rejected.
  For the launch window the Live row would twin a Backlog header and need a
  Backlog cue. Moving at launch matches a Signal leaving Backlog at its claim.
