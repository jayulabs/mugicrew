# ADR 0006: mugicrew signal intake — origin, activation, triage, dedup

Status: Accepted
Date: 2026-09-19

## Context

`Signal` is the head of the spine (`Signal → Chart → Spec → …`), but every prior
ticket left its origin and lifecycle open: where Signals come from, who owns
them, how they are triaged before Nami charts, and how duplicates are handled. The taxonomy (ADR 0002) already fixes the machinery a Signal must
reuse — `kind:*` routing, `open → closed` per-bead lifecycle, the closed edge
enum — and the activation model (ADR 0003) fixes how ready beads are dispatched.
Signal is the one kind that stresses both, because it is the graph's entry point
from the outside world.

## Decision

### Nami owns the Signal kind and the intake interface

Owner is derived from `kind:signal` → **Nami**, the head of the funnel she
already runs (she stewards the Chart a Signal spawns and owns the defog kinds).
The intake interface — how a Signal is filed — is Nami's domain interface
(interface-follows-ownership, ADR 0001). A dedicated intake
crewmate was rejected: intake is a thin accept/reject/dedup decision, not worth
its own unit.

### Owner, creator, and claimer are three distinct things

Signal is the case that forces them apart, so the ontology states them once:

- **Owner** — derived from kind (Nami); nothing stored.
- **Creator** — bd's `created_by`. **Signal is the only kind whose creator can
  be external to mugicrew** — a human or a system files it, entering the graph
  as a chain head with no predecessor edge. (Every other kind is spawned by a
  crewmate.) An offshoot-Signal (below) has an *internal* creator.

  > **Amended by ADR 0012:** Signal is no longer the *only* external-entry kind.
  > A `mugi-grill` session files a **Spec** directly as a chain head (no Signal,
  > no Chart) when the design is fully settleable by interview alone. Spec joins
  > Signal as a kind that can enter the graph from outside; its owner is still
  > Zoro.

  > **Reverted by ADR 0035 (§13):** mugi-grill is retired, so Signal is once
  > again the **only** external-entry kind. Headless Specs filed earlier remain
  > as history.
- **Claimer** — bd's `assignee` + `in_progress`, the activation busy-signal
  (ADR 0003). Distinct from owner: owner is who a bead routes *to*, claimer is
  who has *picked it up*.

### Two sources: external filing and internal offshoot

1. **External filing** — any external actor, human *or* system, files a Signal
   via the intake interface as a chain head. The ontology models only the
   intake, never the source: an external monitor filing a `system-alert` Signal
   is in scope, even though **Chopper as an in-graph crewmate is not** (out of
   scope). "Chopper is out of scope" constrains what mugicrew *contains*, not
   who may *file*.
2. **Offshoot-Signal** — an existing bead spawns a Signal as a `discovered-from`
   offshoot when mid-work turns up genuinely new, *separate* work. This is
   distinct from a `pivot` (the current Spec is wrong) and a `critique` (a defect
   in the current work); it is a whole new thread that deserves its own chain.

### Activation is human-gated (a scoped exception to ADR 0003)

ADR 0003's dispatch loop auto-routes a ready leaf to its kind's owner. For
`kind:signal` that auto-routing is **off**: open Signals form an **intake
backlog** that a human curates. A human explicitly invokes Nami on a chosen
Signal, and *that* is what makes her claim (`assignee` + `in_progress`) and work
it. Everything downstream of the Chart still runs on ADR 0003 auto-dispatch —
only the front door is a human gate. Marked **provisional** ("at least for
now"): the gate can be lifted to full auto-dispatch later without touching the
rest of the model.

### Triage is the Signal's own `open → closed` transition

No separate triage bead, no stored triage-state — triage sits in the frame's
grain (progress = spawn-successor-and-close-self). An **open** Signal *is* the
untriaged intake queue; Nami triages by **closing** it. Two outcomes:

- **Accept** → close, spawning **exactly one Chart** in succession (`caused-by`).
- **Reject** → close with a reason, no successor.

**A Signal spawns at most one Chart — no fan-out.** `Signal → Chart` is strictly
1:1, keeping that spine edge uniform. A lumpy report that contains several
separable efforts is split at the *Signal* level: Nami accepts the primary
thread as one Chart and spins the extra threads out as **offshoot-Signals**
(`discovered-from`) that re-enter the backlog as their own heads. (This is
narrower than `Chart → Spec`, which *is* one-to-many — a single defogged Chart
may spawn several Specs. The 1:1 invariant is specific to `Signal → Chart`.)

> **Amended by ADR 0008:** `Chart → Spec` is now **1:1** too, so the whole
> succession spine is uniformly 1:1 and the parenthetical above no longer holds —
> separable work always splits at the Signal boundary, never into extra Specs.

### Duplicates are rejected via supersession

A duplicate is a reject flavor: the dup is filed as a bead, then closed with
`bd supersede <dup> --with <original>` (obsoletion edge). This keeps triage
uniformly "close the Signal", reuses the first-class `bd supersede`, and
preserves lineage — the graph records that several sources reported the same
thing. Drop-on-intake (never creating the bead) was rejected: it needs dedup
logic *before* a bead exists and silently discards the report.

### Variety is a label, not a kind — and deferred

Signal varieties (bug-report / idea / system-alert / …) share owner, edge, and
lifecycle. By the ADR 0002 rule — a distinction earns kind-hood only via a
distinct owner, edge, or lifecycle — they do **not** earn separate kinds (unlike
Fog's four methods, which differ in owner). If variety ever needs recording it
is an **orthogonal `signal-kind:*` label**, never a routing kind. The name and
value set are **deferred** (build-time, YAGNI) until a real second variety
appears.

## Consequences

- Nami owns the whole front of the funnel (intake → chart → defog → spec); the
  clean lift-out seam (ADR 0001) still holds — the intake interface is her API.
- The intake backlog is a human control point: nothing charts without a human
  pulling it. This is the one place the otherwise-autonomous pipeline pauses for
  a person, and it is reversible (drop the gate → full auto-dispatch).
- Reading "is this Signal handled" is the usual `open`/`closed` check; there is
  no new state to keep in sync.
- Splitting is uniform: both intake-time ("unrelated threads") and mid-work
  discovery ("new separate work") produce offshoot-Signals, so the graph has one
  way to say "new work found here".

## Alternatives considered

- **A dedicated intake crewmate.** Rejected: intake is a thin decision; a whole
  crewmate is weight for nothing, and Nami already owns the next stage.
- **Auto-dispatching Signals** (no human gate). Rejected for now: the entry point
  is exactly where a human wants to decide what the factory works on. Left
  reversible rather than forbidden.
- **A separate triage bead / stored triage-state.** Rejected: reintroduces the
  intermediate state the whole ontology derives away; "did it advance?" is
  already answered by whether a Chart was spawned.
- **Fan-out `Signal → many Charts`.** Rejected: breaks the 1:1 spine edge;
  offshoot-Signals cover the lumpy-report case using an edge we already have.
- **Drop-on-intake dedup.** Rejected: needs pre-bead dedup logic and silently
  loses the report; supersession keeps multi-report lineage.
- **Signal variety as multiple kinds.** Rejected: same owner/edge/lifecycle, so
  no kind-hood earned (ADR 0002); an orthogonal label suffices if ever needed.
