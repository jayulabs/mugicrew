# ADR 0059: mugicrew drafts — every flow that creates beads creates them as Drafts, and the creator's wrapper publishes them in one step on a clean exit

Status: Accepted
Amended by: ADR 0129 (§7: one read-side guard returns, the Unmapped Chart test)
Date: 2026-10-04

## Context

Each `bd create` makes a bead visible in `bd ready` straight away. Editing a
Chart's body makes the Chart a loop candidate straight away. Several
mugicrew flows build part of the bead graph over many steps: a Spec is created
with a placeholder body that speccing fills in later, Builds are created before
their `dep add`, a Chart body is written before its defog children, a defog
bead is closed before its fog is graduated, critiques are filed one at a time
over a Usopp session, and a pivot is created before its gate edge. In each case
the loop can dispatch something before it is fully set up. The race inventory
found 7 real windows and 3 latent ones. The latent ones stay closed only
because the cap is 1 and branches are held, and raising concurrency would open
them. bd 1.3.0 offers exactly two safe primitives:
creating a bead complete in one transaction, or creating it `deferred` and
publishing it later. The human wanted one uniform rule rather than a fix at
each call site (decided in grilling).

## Decision

1. **Draft, then publish, is the one rule.** Every flow that creates beads and
   has any later setup step creates them as **Drafts**. This covers both Go
   code and agent prompts, and includes the flows that are harmless today:
   fast-lane `SeedSpec` then anchor, the confirm and escalate pivot sweeps.
   A one-call create with no later step stays as it is.
2. **A Draft is a bead with `status=deferred`.** mugicrew uses that status for
   nothing else. `bd ready` hides it, and the claim CAS (ADR 0052) refuses it.
   A Draft carries a `drafted-by:<bead-id>` label naming the bead of the effort
   or session that created it. That label scopes the publish, so two efforts on
   the same container never publish each other's drafts.
3. **Every gate counts a Draft as open.** `Bead.Open()` includes `deferred`.
   So a Chart or Checkpoint that has Drafts is not drained, and Franky's
   `readyForReview` sees them as open siblings.
4. **Only the creator's wrapper publishes, and only on a clean exit.** For AFK
   work that is the dispatch tail. For HITL work it is `hitl-run`'s clean-exit
   teardown; a clean exit on a hangup is a drop's kill-session, so it does not
   publish. Prompts never publish. The publish is a single `bd batch`: it adds
   the final edges, sets every Draft with this creator's `drafted-by` label to
   open, and closes the finishing bead when the flow closes one (for example
   Usopp's Review). After that, a best-effort call removes the labels; a label
   left behind does no harm.
5. **Prompts draft before they close.** An agent creates its Drafts (with
   `-s deferred` and the label) before it closes its own bead. Because a Draft
   counts as open, closing the bead cannot drain the container.
6. **Stranded Drafts.**
   - A failed AFK effort deletes its Drafts in the dispatch failure path,
     because ADR 0034 re-runs it from the start.
   - A HITL session that exits non-zero keeps its Drafts so the session can be
     resumed (ADR 0037).
   - Dropping a session, or finding it gone (ADR 0041), deletes its Drafts.
   - After a crash, the Drafts stay. They count as open, so they stall their
     container visibly instead of letting it drain wrongly. The Chopper Signal
     deletes Drafts whose creator is not running. Until Chopper
     exists, a human does it.
7. **No guards where beads are read.** No crewmate checks for a placeholder.
   The Signal's charting session drafts the Chart, so the Chart placeholder
   gate (`IsDrainedChartCandidate`'s `ChartPlaceholderBody` test) is retired.
8. **Claim safety does not change.** The status CAS already refuses Drafts.
   The one double-ownership window, a manual unclaim while the effort is still
   running belongs to
   Chopper: it unclaims only holders that are dead.

## Considered and rejected

- **Create complete, in order** (`bd create --graph` or `--deps` at create
  time, plus ordering rules). Nothing is ever stranded, but it is two rules
  rather than one. Ordering mistakes fail silently. `--graph` loses dotted
  child IDs and label inheritance. It does not cover content that arrives late,
  such as a Spec body, or Usopp's critiques against the Review close.
- **A hybrid of both.** That is two mechanisms to teach every prompt.
- **Agents publish their own Drafts.** A forgotten publish strands work. Doing
  it in the wrapper keeps enforcement in one place.
- **A label instead of `deferred`.** The bead would still be in `bd ready`, and
  the claim CAS would not refuse it.
- **Placeholder guards where beads are read** (for example, Zoro refusing a
  placeholder Spec). That is a fix at each call site, which the human rejected.
