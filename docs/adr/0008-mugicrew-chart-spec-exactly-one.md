# ADR 0008: mugicrew Chart→Spec is exactly-one — succession is uniformly 1:1

Status: Accepted
Date: 2026-09-21

Amends: ADR 0002 (narrows `Chart → Spec` from "one or more" to exactly-one).

## Context

ADR 0002 defined a defogged Chart as spawning **"one or more Specs in
succession (a single investigation may resolve into several
independently-buildable Specs)."** It was the one **one-to-many** edge on the
succession spine.

ADR 0006 later fixed the *other* end of the spine: `Signal → Chart` is strictly
**1:1**, and lumpiness is split **up** at the Signal boundary — a lumpy report
becomes offshoot-Signals (`discovered-from`), one Chart each. That established
the model's rule for where separable work splits.

That leaves `Chart → Spec` as the lone fan-out edge on an otherwise-1:1 spine
(`Spec → Checkpoint` is also 1:1 — ADR 0005 respawns exactly one Checkpoint).
The question surfaced while building the drained-Chart→Spec route, and the
position was revised: the majority case is one Spec per Chart, and exactly-one
may be the right ontology. This ADR resolves the crux. The MVP build map does not
re-decide the ontology and its MVP is unaffected — the reconcile handler already
stubs exactly one Spec, forward-compatible either way.

## Decision

**A defogged Chart spawns exactly one Spec in succession. `Chart → Spec` is
1:1.** The whole succession spine (`Signal → Chart → Spec → Checkpoint`) is now
**uniformly 1:1**; every fan-out in the model is either an **offshoot**
(`discovered-from`) or the **Spec → Build slice**.

> **Amended by ADR 0012:** "produced by a defogged Chart" is now the *common*
> case, not the only one. A `mugi-grill` session may file a **Spec directly** as
> a chain head with no Chart above it, when the design is fully settleable by
> interview alone. `Chart → Spec` stays 1:1; the amendment is only that a Spec
> can also be born headless, without a Chart.

> **Updated by ADR 0035:** mugi-grill is retired (§13). A Spec with no Chart
> above it is now born only by the Signal-triage fast lane, caused by a sharp
> Signal. `Chart → Spec` stays 1:1.

> **Clarified (authorship):** the Spec *body* is authored by **Nami**, not Zoro
> — a speccing session over the defogged Chart writes it (charting's terminal
> act), superseding the placeholder stub the MVP reconcile handler emits. Zoro
> owns the kind only for the *next* step: slicing the authored Spec into Builds.
> See the Spec entry in `apps/mugicrew/CONTEXT.md`.

The crux was whether a single coherent investigation legitimately yields several
*independent* Specs. It does not — "independently-buildable" is the signature of
separable work, and the two shapes that hid under "one or more Specs" both have a
home already:

1. **Shared defog, one branch, several pieces.** The fan-out point is downstream:
   **`Spec → Build` is one-to-many** — Zoro slices one Spec into many Build beads.
   "One or more Specs" was smuggling the Spec→Build slice up a level. These are
   Builds of **one** Spec.
2. **Independent branches.** Because `Spec → Checkpoint` is 1:1, independent
   branches mean separate Checkpoints, hence separate Specs, hence separate
   **efforts**. Separable work surfaced mid-investigation is exactly the
   **offshoot-Signal** case (ADR 0006): spin it out as a `discovered-from`
   Signal that re-enters the backlog as its own head → its own Chart → its own
   Spec.

The dichotomy is clean with no middle ground lost:

| the Chart's defog reveals | resolution |
|---|---|
| coherent work, one branch, several pieces | **one Spec**, sliced into many Builds |
| genuinely independent efforts | **offshoot-Signals** → own Charts → own Specs |

> **Amended by ADR 0135:** fan-out *above* the spine is valid too: an
> Initiative holds several member chains, beside offshoots and the Spec → Build
> slice. Succession stays 1:1, `Chart → Spec` included.

## Consequences

- **One rule for the spine:** succession is always 1:1; fan-out is always an
  offshoot edge or the Spec→Build slice. The lone exception is removed.
- **One split point for separable work:** the Signal boundary (ADR 0006), whether
  the lumpiness is seen at intake or discovered mid-defog. The graph has one way
  to say "new separate work found here."
- `Spec → Build` remains the model's fan-out into buildable units — unchanged; it
  is where multiplicity was always meant to live.
- The MVP is unaffected: the reconcile handler stubs exactly one Spec.
- ADR 0002 and ADR 0006 carry inline amendment notes; `apps/mugicrew/CONTEXT.md`
  Chart and Spec entries are updated to "exactly one Spec".

## Alternatives considered

- **Keep "one or more Specs"** (ADR 0002 as written). Rejected: it is the sole
  fan-out on the succession spine and duplicates the split ADR 0006 already puts
  at the Signal boundary — two places to split separable work, and ambiguity
  about which to use.
- **Re-charter shared context as offshoot-Signals anyway** even when it is one
  branch. Rejected: pays the full re-defog tax (the objection ADR 0005 raised
  against always re-chartering) for work that is really Builds of one Spec.
- **Let the Chart produce several Specs but forbid independent Checkpoints.**
  Rejected: incoherent — `Spec → Checkpoint` is 1:1, so several Specs *are*
  several branches; there is no "several Specs, one branch" shape to model.
