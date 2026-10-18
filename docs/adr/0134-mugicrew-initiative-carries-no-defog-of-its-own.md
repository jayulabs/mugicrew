# ADR 0134: mugicrew An Initiative carries no defog of its own

Status: Accepted
Date: 2026-10-11

## Context

ADR 0128 makes the Initiative parent its member chains, and ADR 0132 gives it a
body (`Done when`, `Phases`, `Notes`) and a cut session. Neither said whether a
grilling, research or prototype bead may sit directly under an Initiative, to
share a question across phases. A defog bead commits its docs onto its parent's
branch, `chart/<parent-id>` (route), and an Initiative has no branch.

## Decision

1. **An Initiative only groups member chains.** It never parents a defog bead.
2. **A cross-phase question is defogged on the Chart of the first phase that
   needs it.** Its answer becomes a durable doc (ADR, glossary, research note),
   which reaches mainline when that Chart lands; later phases read it from
   mainline like any other doc.
3. **Luffy points to it in the cut session.** While he reads the drained phase
   against `Done when`, he adds a `Notes` line to the Initiative body for each
   doc a later phase should read. Defog sessions never edit the Initiative body.
4. **Concurrent phases get no mechanism.** When the human cuts early and two
   live phases need the same answer, Luffy's cut session says so in the new
   member Signal's body (which phase's Chart owns the question); no edges
   between defog beads of different Charts.

## Considered options

- **Initiative-level defog.** Rejected: it needs an Initiative branch and land
  to hold its docs, and a lone open research bead would keep the Initiative
  from draining (ADR 0133).
- **A discovery-only phase** (a member chain that lands docs, no code).
  Rejected for now: no case seen that the first needing phase can't carry.
- **The answering defog session writes the pointer.** Rejected: a crewmate
  would edit a Luffy-owned body, and the cut session already runs before the
  next phase is cut.
- **A blocking edge between defog beads of concurrent phases.** Rejected: a
  new cross-Chart edge for an edge case only an early cut creates.
