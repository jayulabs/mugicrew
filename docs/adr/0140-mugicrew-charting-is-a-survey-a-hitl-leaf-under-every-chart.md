# ADR 0140: mugicrew charting is a Survey: a HITL leaf under every Chart

Status: Accepted
Date: 2026-10-12

Amends: ADR 0129 §5-7 (the Unmapped gate becomes a Survey), ADR 0130 §6
(branch deletion keys on the Survey session), ADR 0046 §5 (escalate's Chart
gets a Survey).

## Context

ADR 0129 held a triaged Chart for Nami's human-started charting session with a
derived state, the Unmapped Chart (an open Chart that has never had a child).
It rejected a new kind for the step: "a transient state does not earn
kind-hood". The result made Chart the only kind whose mode changes with its
state. The registry says AFK, yet the Chart waits in the Inbox for a human.
The derived test is rechecked in the loop's candidate gate, in Nami's launch
refusal and at about ten TUI sites. The charting session claims a container it
must never close, and gets none of the claim, crash and pause handling every
leaf gets for free. Everywhere else, a human step on an AFK container is its
own HITL leaf: Approval on a Checkpoint, Grilling and Prototype on a Chart.

## Decision

1. **Survey is a HITL leaf kind** (`kind:survey`, owner Nami). It is the
   charting session's bead. Every Chart is born with one as its first child,
   seeded as a Draft beside the Chart by the one shared helper, with the same
   `drafted-by:` so the two publish and delete together. A Chart's children are
   its Survey and its defog beads. The Survey is not a defog bead: speccing
   and promote-or-stamp skip it.
2. **Charting is work, not a state.** It has an owner (Nami), a session, and
   outcomes (fog seeded, Destination narrowed, offshoots split, hand-back,
   reject, duplicate). That is what earns kind-hood (ADR 0002); a waiting
   state does not, but the work that ends it does. Approval is the precedent:
   the human step before a Checkpoint lands is a leaf, not a derived
   "unapproved Checkpoint" state.
3. **The open Survey is the gate.** While it is open, the Chart is not drained,
   so the loop needs no special test. The Survey is an ordinary Inbox row, and
   the registry's `chart: nami, AFK` row is accurate.

### Amendments

- **ADR 0129 §5:** there is no Unmapped Chart. The open Survey child keeps a
  fresh Chart out of the drained-Chart candidates.
- **ADR 0129 §6:** the human launches the Survey from the Inbox, not the
  Chart; launching a Chart is refused like any other container.
- **ADR 0129 §7:** the session claims the Survey, never the Chart. Its Drafts
  (defog beads, offshoot and hand-back Signals, free-standing Tasks) are
  drafted by the Survey but parented by, appended to, and `discovered-from` the
  Chart. Keeping the Chart, the session closes neither bead: mugicrew closes
  the Survey in the clean-exit batch that publishes its fog. Pushing back, the
  session closes only the Chart (`bd close --force`, or `bd supersede` for a
  duplicate) and the batch then closes the Survey. A session quit before any
  fog was seeded releases the Survey back to the Inbox.
- **ADR 0130 §6:** branch deletion keys on a Survey session whose parent Chart
  ended closed with no Spec successor; `chart/<parent>` is deleted after the
  session's tree is torn down.
- **ADR 0046 §5:** escalate's fresh Chart gets a Survey, drafted by the pivot
  and published with the Chart. It no longer drains straight into
  promote-or-stamp; the evidence-seeded body is the charting session's
  starting point.

## Considered options

- **Keep the derived Unmapped Chart state.** Rejected: see Context.
- **Reuse Grilling as the charting leaf.** Rejected: a Grilling is a defog
  bead that only answers a question. A Survey can change the Destination and
  close its Chart. Grilling would then behave two ways depending on whether it
  was the charting one, which brings back the hidden mode this ADR removes.
- **Close both beads in one transaction** (the Chart close moved into the
  publish batch). Rejected for now: a wider diff, and closing the Chart first
  and the Survey in the batch already never leaves a drained Chart with no fog.
