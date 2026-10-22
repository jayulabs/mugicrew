# ADR 0347: mugicrew an ordered offshoot member is placed in its cause's phase and never parked

Status: Accepted
Date: 2026-10-22
Amends: ADR 0252 (a-cross-repo-signal-splits-at-triage…) §3; ADR 0255 (member-triage-places-the-signal…) §1/§2/§3.

## Context

The Chart "Related beads join their cause's Initiative" settled that an offshoot
filed `--order follow-up` or `--order prerequisite` (ADR 0267
(an-offshoot-signal-is-filed-through-signal-offshoot…)) joins its cause's
Initiative as a member Signal and goes through member triage. ADR 0255 §2's
sibling rule would merge it into, or park it behind, its own same-context
cause. Parking closes it. For a prerequisite, a closed blocker counts as
satisfied, so the cause would land without it. For a follow-up, a parked member
never holds the drain, so the Initiative could drain without it.

A follow-up is blocked-by its cause's container, and ADR 0085's carry moves the
edge Chart → Spec → Checkpoint. Under ADR 0252 §7 it is therefore normally
triaged only after the cause has landed and closed, and until then it holds the
drain as an open member. A prerequisite blocks its cause, so it is triaged while
the cause is still live.

## Decision

1. **Follow-up: the cause's phase.** Member triage places an ordered follow-up
   in its cause's phase. If that phase has already drained, it goes in the phase
   now under way. A closed (landed) same-context sibling does not count under the
   one-member-per-context rule (ADR 0252 §3) or under the sibling rule (ADR 0255
   §2). This is a clarification, not an exception.
2. **Early triage doesn't change placement.** If a human run-now triages a
   follow-up before its cause lands, Luffy still places it in the cause's phase
   and leaves it blocked. Its blocked-by edge keeps its course from moving early.
   It is never parked.
3. **Prerequisite: co-member of the cause's phase.** As a named exception to
   ADR 0252 §3, an ordered prerequisite joins its cause's phase beside its
   same-context cause. The existing `blocks` edge orders the two. Its Phases
   entry reads `<title> [<context>, prerequisite of <cause title>]`.
4. **The sibling rule skips the ordered-against sibling.** ADR 0255 §2's merge
   and §3's park never apply to the sibling an offshoot is ordered against. The
   rule still applies to any other same-context sibling.
5. **Go marks the order.** ADR 0255 §1's member section gains one Go-built line
   for an ordered member: `Ordered: follow-up|prerequisite of <container title>
   (phase: <its Phases line>)`. Go reads the order edge and climbs to the
   container's member head. Following ADR 0252 §8, the line and the rule text
   that keys on it render only for an ordered member. The prompt files stay
   byte-identical.
6. **No new conflict path.** Same-context containers in flight together
   (a prerequisite's Chart landing ahead of the Chart it gates, ADR 0268
   (a-chart-level-prerequisite-gates-the-whole-chart…)) are left to the existing
   chart-conflict number-clash and requested-rebase machinery (ADR 0262) and to
   Conflict for code.

## Considered Options

- **Park as today.** Rejected: it releases a prerequisite's hold and drops a
  follow-up from the drain.
- **A new live phase line for a prerequisite.** Rejected: a phase that runs
  beside the one it precedes is the cause's phase's work under another name, and
  it needs the human's yes under ADR 0255 §1 every time.
- **Merge a prerequisite into a Signal or Chart cause.** Rejected: superseding
  it with the container it blocks removes the order edge.
- **Refuse early follow-up triage.** Rejected: placement doesn't depend on
  timing, and the edge already gates the course.
- **Prompt-only, reading the body's `Order:` line.** Rejected: that line names
  the container, not its phase.
- **Automatic rebase of a gated Chart when its prerequisite lands.** Rejected:
  a defog-time prerequisite is rare, and the existing machinery handles it.
