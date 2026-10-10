# ADR 0079: mugicrew AFK chart-conflict escalates to a grilling bead on the Chart; the conflict stays open, blocked by it, and the writer applies the answer

Status: Accepted
Date: 2026-10-05

## Context

Chart-conflict becomes an AFK Nami arm, amending ADR 0011 §3 and ADR 0030 §3
(the Chart). Triage settled that the AFK agent escalates to a **new grilling
bead on the Chart** only when it faces a real judgment call. Two facts constrain
the escalation design:

- an AFK arm that returns without closing is unclaimed and re-dispatched every
  tick, so escalation must be a graph change;
- rebase state dies with worktree teardown, so only commits from a *finished*
  rebase survive on `chart/<parent>`.

Grilling settled the decisions below. The close contract, how a re-dispatch
converges, and the post-resolve check belong to a later grilling.

## Decision

1. **Escalate only when a wrong guess would silently change meaning.** That
   means exactly two cases:
   - a conflicted hunk whose two sides make **contradictory claims**, such as one
     CONTEXT.md term with incompatible definitions, or two ADRs that decide
     opposite things;
   - an `ADR NNNN` line inside the sweep scope (ADR 0030 §5) whose referent,
     the Chart's ADR or mainline's, can't be told apart by reading it.

   The agent decides everything else itself: unions, reordering, adjacent edits,
   renumbering, and lines whose referent is clear.
2. **The chart-conflict stays open, blocked by the grilling bead.** On
   escalation the agent aborts any rebase that is in progress. Commits that a
   finished rebase already made stay on the branch. The blocks edge takes the
   conflict out of `bd ready`, so it is not re-dispatched. Closing the grilling
   bead makes the conflict ready again.
3. **The writer applies the answer, not the grilling session.** The grilling
   session only decides and records the answers. It never commits to the Chart
   branch and never closes the chart-conflict. Closing and re-filing the conflict
   was rejected because it churns beads and cuts the link between question and
   answer. So was having the grilling session resolve, because it would put HITL
   back into the resolve path.
4. **Hybrid filing (ADR 0019 style).** The agent never touches the graph. It
   ends its run with an `Escalate: <title>` outcome and the payload. Go then:
   - files the grilling bead as a Draft child of the Chart;
   - queues `EdgeOnPublish(chart-conflict blocked-by grilling)`;
   - returns nil.

   The effort publishes the bead and the edge atomically, so neither can exist
   without the other.
5. **Payload: one grilling bead per escalation.** It carries:
   - the chart-conflict it blocks, the variant, the Chart branch and the base SHA;
   - **numbered questions**, one per unsure item, each giving the path and line
     (or hunk), mainline's side and the Chart's side verbatim, and the agent's
     candidate resolutions with its recommendation.

   The grilling session records its answers in the bead's notes, numbered the
   same way. The re-dispatched writer reads the **closed grilling blockers** of
   its chart-conflict and treats their answers as **binding**.
6. **Idempotency and re-escalation.**
   - Go files no new grilling bead while the chart-conflict already has an
     **open** grilling blocker.
   - Re-escalating after an answer is allowed, with no cap, but questions that
     were already answered are never asked again.
   - If mainline has moved and the hunk an answer referred to is gone, the answer
     is stale. That counts as a new unsure item and triggers a fresh escalation,
     not a guess.

## Consequences

- The grilling bead is an ordinary HITL Nami bead. It routes to a worktree on
  the same `chart/<parent>` (ADR 0071), so it serializes with the chart-conflict.
  While it is open, the Chart is not a drain candidate.
- Each escalation is a bead the human can see. There is no cap, so a conflict
  that keeps escalating shows up as a growing list of grilling blockers.
