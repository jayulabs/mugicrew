# ADR 0003: mugicrew activation — a dumb dispatch loop over the bead graph

Status: Accepted
Date: 2026-09-19

Amends: ADR 0002 (containers are now routed *when drained* — see "Container handler").

Amended by: ADR 0031 (2026-09-25): "the loop holds no state / re-derives every
tick" governs *coordination* state only — a pure runtime **view** of a process
the graph does not own (effort liveness) may live outside the graph when it is
non-authoritative for dispatch and re-derivable/discardable.

## Context

ADR 0002 settled the bead taxonomy (kinds, edges, owner-from-kind, derived
lifecycle) but **deliberately deferred the activation mechanism** to the
Activation & orchestration work: how a crewmate learns there is work
for it, what fires the on-demand spawns (Review/Critique/Approval), and each
crewmate's concrete runtime shape.

The pivotal question: do crewmates detect work **in-graph** (each self-polls its
frontier query) or does a **central orchestrator** drive them out-of-band? The
founding decision (ADR 0001) is that the crew coordinates *through the bead
graph*, not by calling each other. A central out-of-graph orchestrator would
demote the graph to a mere record and contradict that.

The runtime is greenfield: a Node + TypeScript monorepo (pnpm + Nx); no
dispatcher, watcher, or `bd`-driven loop exists yet, so nothing constrains the
choice. bd itself offers `bd ready` (frontier query), an opt-in
`bd_events_journal` (ordered, replayable, per-replica), and `bd serve` (loopback
HTTP API).

## Decision

**A single thin dispatch loop, part of the root `mugicrew` app, drives
activation. The bead graph holds all coordination state; the loop holds none.**

1. **In-graph coordination, mechanical dispatcher.** The loop's only job is to
   route ready work to its handler. It carries no workflow, no state machine,
   and no succession rules — it cannot route anything the `kind:` label does not
   already say. A central out-of-band orchestrator is rejected; the graph stays
   the coordinator.

2. **Dispatch rule, two shapes of ready work:**
   - a ready **leaf** (open, unblocked) → its kind's **owner** (the settled
     owner-from-kind function);
   - a **drained container** (open ∧ *zero open children*) → its kind's
     **handler**.

3. **Container handler** *(this amends ADR 0002)*. Owner-from-kind extends to
   container kinds: `chart → Nami`, `checkpoint → Jinbe`. A drained container is
   dispatchable work, not a special case swept up by whichever leaf closed last.
   **Steward** (creates the container) and **handler** (acts when it drains) may
   differ: Zoro stewards the `checkpoint`, Jinbe handles it. This replaces ADR
   0002's "containers are not routed to a worker."

4. **Throughput — busy-state derived from the graph, gated per-crewmate.** An
   `in_progress`/claimed bead means its crewmate is busy; the loop maps
   kind → crewmate via the owner function and counts that crewmate's in-flight
   beads. A per-crewmate concurrency cap (config, **default 1**) gates dispatch.
   Nothing is stored in the loop, so it re-derives every tick and a crash or
   restart loses zero coordination state.

5. **Cadence — blind poll, journal-tail as the sanctioned upgrade.** The loop
   polls the scoped `bd ready` on an interval. Stateless, so there is no
   checkpoint to corrupt or re-baseline after a Dolt sync. When poll latency or
   query cost is *measured* to matter, graduate to tailing `bd_events_journal`
   (incremental, precise); bd already supports it.

6. **Succession firing — the closing crewmate spawns its successor.** When a
   crewmate closes a bead it creates that bead's successor(s) in the same action
   (Usopp, closing a clean Review, creates Luffy's Approval). Creator ≠ owner is
   already modeled; the loop later routes the new open bead to its owner. The
   loop never creates spine beads. Container-completion transitions (Chart→Spec)
   are handled by the container's **handler** (item 3), not by a leaf's
   "last one out" check.

7. **Handlers must be idempotent.** Because a drained container is retried until
   it closes, its handler must *check-successor-exists before spawning* — Nami
   spawns the Spec only if none exists yet, then closes the Chart; Jinbe must
   not double-merge. More broadly, every graph-mutating action must be safe
   under retry.

8. **Runtime shape (non-binding guidance, not locked ontology).** Start with
   **in-process library calls** in one Node process, and plain `bd` CLI/embedded
   access. Keep the bead-in / graph-mutation-out seam clean so a heavy or risky
   crewmate (Franky's Builds) can be lifted **out-of-process** later, and so `bd
   serve` (loopback HTTP) can replace fork-per-call when overhead is measured to
   bite. These are recommended defaults; the ontology does not fix them.

**Amended by OE ADR 0007 (2026-09-19): runtime is Go, not Node + TypeScript.** The
decision above is language-agnostic and stands unchanged; only the runtime
context shifts. Item 8's "in-process library calls in one Node process" reads as
one Go process making in-process Go calls, with `bd` reached by shell-out to the
CLI (see OE ADR 0007 and the bd access layer decision).

## Consequences

- **Brook is not needed.** Activation stays in-graph, so the out-of-band channel
  (Brook) does not graduate; it moves to the map's Out of scope.
- **The dispatcher is stateless and self-healing.** All coordination state lives
  in the graph; a crash re-derives everything on the next tick. The price is
  that every action must be idempotent under retry (item 7).
- **Jinbe's merge tail inherits a trigger**: Jinbe is activated by a
  *drained Checkpoint*. What the merge does internally — whether it produces beads,
  how conflicts are handled, what flips the Checkpoint to `merged` — remains that
  ticket's decision.
- **Costs**: poll latency until journal-tail is adopted; per-crewmate cap 1
  serializes each crewmate until a cap is raised in config.
