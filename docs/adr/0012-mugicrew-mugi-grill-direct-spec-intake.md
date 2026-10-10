# ADR 0012: mugi-grill — grilling as a direct Spec intake

Status: Superseded by ADR 0035
Date: 2026-09-23

Amends: ADR 0006 (Signal is no longer the *only* external-entry kind) and
ADR 0008 (not every Spec descends from a Chart).

> **Superseded by ADR 0035 (§13):** mugi-grill is retired. Signal is again the
> only door: a sharp Signal takes the triage fast lane to a direct Spec with
> review kept, and anything needing an interview or an ADR is charted. The
> boundary table below is also stale — the patch-lane Signal was resolved as that fast lane
> (a Spec, reviewed), not a Chart-and-Spec-skipping patch. Existing headless
> Specs stay valid as history; ADR 0014's `Spec:` anchor stays in force.

## Context

The succession spine is `Signal → Chart → Spec → Checkpoint`, and prior ADRs
made it rigid: `Signal → Chart` is 1:1 (ADR 0006) and `Chart → Spec` is 1:1
(ADR 0008), so **every Spec descends from a defogged Chart**, which descends
from a Signal pulled off the intake backlog. ADR 0006 also fixed the graph's one
outside door: **"Signal is the only kind whose creator can be external to
mugicrew."**

A Chart exists to burn off *fog* — the four defog methods are grilling,
research, prototype, and task. Only grilling is pure human interview; the other
three are autonomous or hands-on investigation. When a change is small and
well-understood enough that **interview alone fully settles the design**, the
Chart machinery (a branch, a defog graph, a reconcile land) is pure ceremony:
the Signal exists only to spawn the Chart, and the Chart exists only to host one
grilling bead.

The `/grill-with-docs` skill already runs exactly that interview and, via
`domain-modeling`, already produces the artifacts a Chart would (ADRs, glossary
and `CONTEXT.md` edits). What it lacks is a defined output on the bead graph.
A separate Signal ("Signal-triage fast lane: emit a patch for well-defined single-unit
changes") raised the sibling case one notch more compressed — trivial changes
that do not warrant a Spec at all. This ADR resolves the Spec-level fast lane.

## Decision

**A `mugi-grill` session emits exactly one `kind:spec` bead directly, with no
Signal and no Chart predecessor.** Grilling *is* the compressed charting: the
session interviews to shared understanding, writes any ADR / glossary /
`CONTEXT.md` artifacts inline (committed to the working branch as usual — there
is no Chart branch to land), and files the Spec as its output.

**Spec becomes the second kind that can enter the graph from outside**, joining
Signal (ADR 0006). Its `created_by` is the grilling session (human + agent); its
**owner is still Zoro** by `kind:spec`, so the new Spec lands open in Zoro's
queue to be sliced into Builds — everything downstream of the Spec is unchanged.
The Spec is a chain **head** with no predecessor edge, exactly as an
externally-filed Signal is.

**Legitimacy gate.** mugi-grill is permitted **only when the design is fully
settleable by human interview alone** — no research, prototype, or task defog is
needed. If the work needs autonomous investigation or a prototype, it does *not*
qualify: file a Signal and let it chart normally. The gate keeps this a way to
skip the Chart *machinery*, never a way to skip the *thinking* a Chart's other
defog methods do. The skill states the gate up front and routes disqualified
work back to normal intake.

**Boundary with the patch lane.** Two distinct fast lanes at different compression:

| lane | skips | emits | when |
|---|---|---|---|
| patch lane | Chart *and* Spec | a patch / Build | trivial, single-unit, no design to settle |
| mugi-grill | Chart | one Spec | real design, settleable by interview, still needs slicing/build/review |

They are siblings, not the same effort; mugi-grill does not resolve the patch-lane Signal.

## Consequences

- **Two external-entry kinds now:** Signal (any external actor) and Spec (a
  mugi-grill session). ADR 0006's "only Signal" claim carries an inline
  amendment; the "chain head with no predecessor" shape is reused, not invented.
- **The spine is no longer strictly descent-linked:** a Spec may have no Chart
  and no Signal above it. ADR 0008's "produced by a defogged Chart" is now the
  *common* case, not the only one; it carries an inline amendment.
- **Downstream is untouched:** `Spec → Checkpoint` stays 1:1, Zoro still slices,
  review/approval/land are unchanged. The change is confined to how a Spec is
  *born*.
- **Auditability:** a direct-intake Spec is visibly headless (no `caused-by`
  Chart), which is the honest record of the shortcut — not a synthesized Chart
  papering over it.
- **Lineage of design rationale** lives in the ADR/glossary commits the session
  produces and in the Spec's own design section, not in a Chart bead.

## Alternatives considered

- **Synthesized compressed Chart** — have the session create and immediately
  close a Chart (and Signal) so the Spec keeps full 1:1 lineage. Rejected:
  ceremony beads whose only purpose is to satisfy an invariant we are
  deliberately relaxing; the "headless Spec" is a more honest record than a
  throwaway Chart.
- **Signal head only** — file a Signal, then fast-triage it straight to a Spec,
  skipping the Chart. Rejected: still bypasses the Chart (so it needs this ADR
  anyway) while adding a Signal bead that never sees the intake backlog — the
  worst of both.
- **Fold into the patch lane** — one fast lane for everything below a full Chart.
  Rejected: patch-lane and Spec-lane differ in what they emit and when they
  apply (see the table); merging them loses the Spec's slicing/review value for
  non-trivial design work.
