# ADR 0001: mugicrew is a single Application that absorbs the Merger

Status: Accepted
Date: 2026-09-19

## Context

The system-wide glossary defined two Applications in the branch lifecycle: the
**Factory** (consumes Signals, produces ready-to-merge branches) and the
**Merger** (gates those branches and lands them). Designing the bead-graph
ontology forced a decision about how many Applications the factory
system actually is, and where the review/merge stages live.

Two questions were bundled:

1. Is the factory **one** Application with named internal stages, or is each
   stage its own independently-owned, independently-mutable Application with an
   orchestrator on top?
2. Is the Factory/Merger split load-bearing (e.g. a trust boundary), or just
   pipeline structure?

## Decision

**One Application, renamed Factory → `mugicrew`, spanning the whole lifecycle
Signal → landed branch.** Its stages are internal crewmates — Nami, Zoro, Franky,
Usopp, Luffy, Jinbe — coordinating through the bead graph, not Applications in
their own right.

**The Merger is absorbed into mugicrew's tail** (Usopp review → Luffy approval →
Jinbe merge) and removed as a distinct Application. A branch no longer crosses
an Application boundary; it is cut, reviewed, approved, and landed entirely
within mugicrew.

> **Amended by ADR 0009:** an effort now has *two* branches, not one — the Chart
> branch (charting artifacts, merges on defog) then the Checkpoint branch (code,
> cut from that landed base). "Container kinds are branches."

> **Amended by OE ADR 0088:** mugicrew and its terms now live only in the
> mugicrew glossary.

The former Factory→Merger handoff was pipeline structure only,
not a trust or gate boundary, so nothing is lost by removing it.

Each crewmate seam is kept as a clean contract — a bead in, a graph mutation (plus
side effects) out — so any crewmate can later be **lifted out** into its own
standalone Application without a rewrite, if per-stage mutability becomes the
goal. The concrete runtime shape of a crewmate (CLI, library, or long-running
process) is **not** decided here; it falls out of the Activation & orchestration
work.

## Consequences

- The system-wide glossary loses *Factory* and *Merger* and gains *mugicrew*;
  the mugicrew context glossary lives at `apps/mugicrew/CONTEXT.md`.
- Coarser mutability today: an owner edits mugicrew as a whole, not one stage in
  isolation. The clean crewmate seams are the hedge that keeps a future split
  cheap.
- Crewmate boundaries are convention until (unless) a crewmate is promoted to an
  Application; drift across them is a risk the seams are meant to contain.
- The bead-graph vocabulary is shared across the crewmates but versioned in one
  place — which is why the ontology effort exists.
- Follow-up: the code scaffold `apps/factory/` (under the scaffold's old package name)
  still needs renaming to `apps/mugicrew/`. Tracked in the
  migration handoff, not done as part of this decision.

## Alternatives considered

- **Each stage its own Application + an orchestrator Application on top.**
  Rejected as premature: it pays for N+1 Applications, an orchestrator not yet
  designed, and a frozen cross-app contract before the stages are known to be
  stable. Reconsider if demonstrating per-Application mutability *inside* the
  factory becomes the product thesis — the crewmate seams above keep that door
  open.

## Amendment: the crewmate–interface principle

This ADR left "clean crewmate seam" as an intuition. This amendment sharpens it.

**A crewmate owns its kind(s) *and* the domain interface for those kinds.** The
domain interface is the outward, kind-specific operation surface — richer than
the uniform activation contract (bead in → graph mutation out, ADR 0003). Luffy
is the exemplar: beyond closing an approval bead, it consolidates
the decision packet (diff, clean-review evidence, critique history) and exposes
the approve action.

The ontology fixes only **where** that interface lives — with the kind's owner,
never in another crewmate or a shared central module. It deliberately does
**not** specify any crewmate's concrete interface, and does **not** claim every
crewmate has a rich one: for many kinds the surface is as thin as the activation
contract; for approval it is rich. Concrete shape is designed **when the
crewmate is built**, one at a time — the same stance this ADR already took on
runtime shape. This ownership placement *is* the clean seam: lifting a crewmate
out into its own Application makes its domain interface that Application's public
API, no rewrite.

**The dispatch loop (ADR 0003) is activation substrate, not a crewmate.** Owning
≥1 kind is the practical crew-vs-plumbing test — the stateless loop owns none.
It is only a **characteristic**, though, not a strict definition: "crewmate"
stays open-textured so a future unit is not foreclosed.

(Ownership shape — every kind owned by exactly one crewmate, crewmates own many
kinds, no co-ownership — is stated as an invariant in ADR 0002, amended by the
same ticket.)
