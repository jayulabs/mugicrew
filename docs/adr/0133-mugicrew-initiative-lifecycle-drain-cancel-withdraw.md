# ADR 0133: mugicrew Initiative lifecycle: derived state, a human "done" on drain, and guarded cancel and withdraw

Status: Accepted
Date: 2026-10-11

## Context

ADR 0128 made the Initiative a Luffy-owned container whose membership spans
every bead of its member chains, and gave Luffy the call "on drain" between a
next phase and done. This grilling settled when an Initiative is done, what
state it has, and what a pivot, rollback, failure or human drop on one member
means for it and its later phases. ADR 0132 phases it by rolling member
Signals, ordered by drain rather than by edges, and puts a Drained Initiative
in the Inbox for Luffy to cut the next phase or propose done.

## Decision

1. **State is derived, never stored.** Waterfall, first match wins:
   **closed** → **drained** (no unclosed member, Drafts included) → **under
   way**. Pause stays a label (ADR 0081).
2. **Drain prompts, it does not close.** The ADR 0132 cut session is the
   only place an Initiative is closed as done. Luffy checks `Done when` and
   proposes, the human says **done** (Luffy closes it) or takes a **next
   phase** (a new member Signal, so it is no longer drained). No separate
   decision bead.
3. **Phases can be added any time while unclosed**, always by a human act
   (a cut session, including one the human calls early, or adoption).
4. **Closed is final.** A done or cancelled Initiative is never reopened. New
   related work is a new Initiative or standalone chains.
5. **Pivot.** Correct, amend and dismiss stay inside the member chain. An
   escalated pivot's fresh Chart is a successor, so it joins the Initiative.
   Escalation is one more spawn site that copies the parent.
6. **Rollback.** The Rollback bead stays unparented. Its successor Checkpoint
   joins the Initiative if it is unclosed. If it is closed, the successor is
   born outside it and joins only by adoption. Nothing cascades to later phases
   that landed on top: fixing forward is the Rollback's job.
7. **Failure stays local** (`paused:failing`, ADR 0081 §1). Later phases
   already wait through ordering.
8. **Cancel** (the whole Initiative) closes every unclosed member chain, then
   the Initiative. Landed work stays: nothing is reverted. To keep a chain,
   release it first. Cancel is refused while any member has an in-flight
   effort, and the refusal names those beads.
9. **Withdraw** (one member chain, the Initiative goes on) closes the chain's
   unclosed head with no successor. Phases carry no edges (ADR 0132), but
   members can still be ordered, e.g. chains adopted with their edges. So
   withdraw is refused while an unclosed member is ordered after the chain
   (bd would read the closed blocker as satisfied and start work on a chain
   that never happened) or while the chain has an in-flight effort. The
   refusal lists them, and the human withdraws those or removes the edge
   first. Once the last chain is withdrawn, the Initiative drains and the cut
   session re-plans.

## Considered options

- **Auto-close on drain.** Rejected: "done" is a human authorization, and the
  human may want another phase.
- **Human-only close, no drain signal.** Rejected: a finished Initiative would
  sit open unnoticed.
- **Withdraw pauses the Initiative for a re-plan.** Rejected: it stops
  unrelated siblings. A refusal touches nothing.
- **Cancel aborts in-flight efforts.** Rejected: it kills running work behind
  the human's back. The per-effort abort stays the only way to do that.

## Consequences

- "Abandon" stays reserved for an abandoned effort. Dropping work in an
  Initiative is **cancel** (all of it) or **withdraw** (one chain).
- Successor spawns that must copy the Initiative parent: the four spine sites
  (ADR 0128), plus pivot escalation and the Rollback successor.
