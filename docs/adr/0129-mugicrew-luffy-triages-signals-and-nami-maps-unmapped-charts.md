# ADR 0129: mugicrew Luffy triages Signals; Nami maps an Unmapped Chart in a human-started session

Status: Accepted
Amended by: ADR 0130 (§8: Nami pushes back on the Destination by narrowing, splitting or a hand-back)
Amended by: ADR 0140 (§5-7: the Unmapped gate becomes a Survey leaf; the session claims and drafts as the Survey, parents on the Chart)
Date: 2026-10-11
Amends: ADR 0006, ADR 0035, ADR 0059 §7

## Context

ADR 0006 gave Nami the whole front of the funnel: triage of a Signal and
charting of the Chart it spawns, in one session. Accepting a Signal names a
Destination, which is a scope call. Deciding scope is adjudication, Luffy's
role (ADR 0128 already makes him the owner of the Initiative for the same
reason). Mapping the fog is wayfinding, Nami's role. This ADR splits the one
session into two and says how the Chart waits between them.

## Decision

1. **Luffy owns `kind:signal` and triages it.** Activation stays human-gated
   (ADR 0006): a human launches Luffy on a chosen open Signal. The four
   outcomes are unchanged (accept, fast lane, reject, duplicate).
2. **Accept: Luffy names the Destination and sets the priority.** He writes
   the Chart's Destination (the one Spec) and the bd priority (P0–P4) of the
   successor. He does not map the fog.
3. **Fast lane: Luffy writes the direct Spec** in the triage session. ADR
   0035's single human gate stays: approving the triage accepts the Spec. The
   fast-lane predicate holds only if the one deciding is the one writing.
4. **Triage ends at Luffy's exit.** A clean exit publishes the Chart (Draft by
   the Signal, branch `chart/<id>` already cut) and closes the Signal, as
   before (ADR 0096). No Signal stays open across two crewmates.
5. **Unmapped Chart.** An open Chart that has never had a child (open or
   closed) is waiting for its map. The loop's drained-Chart candidate test
   skips it. The state is derived, never stored, keeping ADR 0006's
   no-stored-triage-state grain. This re-arms a gate ADR 0059 §7 retired, but
   keyed on structure, not on the placeholder body.
6. **Nami's charting session is human-started.** The TUI Inbox lists Unmapped
   Charts as a derived row beside the HITL kinds; `l`/`b` launches Nami on
   one. The registry row stays `chart: nami, AFK`: the exception is derived,
   not a new kind or mode. `signal triage` follows the registry to Luffy; no
   new CLI verb.
7. **The charting session drafts as the Chart.** Its defog beads and Tasks
   are Drafts by the Chart, published at its clean exit. It claims the Chart
   while it runs and releases the claim on exit, never closing it: closing
   stays with Nami's AFK promote-or-stamp judge once the Chart is defogged.
   A crash leaves the Drafts, which keep the Chart from draining (ADR 0059).
8. **Charting always seeds fog.** A Chart with nothing to defog should have
   taken the fast lane; how Nami hands it back is left to the Spec.

## Considered options

- **Hold the Chart with a stored mark** (a `paused:` label, or a Draft kept
  past Luffy's exit). Rejected: pause is a human brake (ADR 0081), and a
  long-lived Draft hides the Chart and inherits crash-cleanup rules meant for
  in-flight sessions.
- **Derive from the body** (placeholder or empty sections). Rejected: a stray
  body edit flips the state.
- **A new HITL kind or mode for the charting step.** Rejected: a transient
  state does not earn kind-hood (ADR 0002).
- **Close the Signal when charting ends.** Rejected: one intake item would
  span two sessions and sit in progress while waiting on a human.
- **Keep ADR 0006's rejection of a dedicated intake crewmate.** It still
  holds: Luffy is not dedicated to intake. What 0006 got wrong is that scoping
  is wayfinding.

## Consequences

- ADR 0006: owner and intake interface move from Nami to Luffy; "Nami owns the
  whole front of the funnel" no longer holds.
- ADR 0035: every "Nami writes the fast-lane Spec / anchor" reads Luffy.
- ADR 0059 §7: one read-side guard returns, the Unmapped Chart test.
- The human gate gains a second stop: an Unmapped Chart waits in the Inbox.
