# ADR 0135: mugicrew Initiative is a branchless container kind, and fans out above the spine

Status: Accepted
Date: 2026-10-12

Amends: ADR 0002 (a third container kind, with no branch), ADR 0008 (fan-out
above the spine by Initiative membership is valid).

## Context

ADR 0128 named the Initiative (`kind:initiative`, a bd `epic`, owned by Luffy)
and left two taxonomy edits to the Spec. ADR 0002, as amended by ADR 0009, says
"container kinds are branches". ADR 0008 says every fan-out is an offshoot or
the Spec → Build slice. The Initiative breaks both as written.

## Decision

1. **The Initiative is a third container kind, and it has no branch.** Its
   member Checkpoints each land on their own, so there is nothing for an
   Initiative branch to hold, and no defog of its own to merge (ADR 0134).
   Routing leaves it at the repo root: no branch, no worktree, no land. It is
   a HITL container with no effort, so the loop never dispatches it and it
   takes no pool slot. "Container kinds are branches" now reads "the Chart and
   Checkpoint containers are branches".
2. **Its edges.** It parents (`parent-child`) every bead of its member chains
   (ADR 0128). It is `caused-by` the Signal triaged into it (ADR 0132), and
   is never parented itself.
3. **Fan-out above the spine is valid.** One Initiative holding several member
   chains is a third fan-out, beside offshoots and the Spec → Build slice.
   Succession stays 1:1, `Chart → Spec` included: the Initiative groups chains,
   it never makes one Chart spawn several Specs.

## Consequences

- `contract.Kinds` gains `initiative`, so the title backstop strips it like any
  other kind prefix (ADR 0097).
- Its registry row is `{Owner: luffy, Mode: HITL, Container: true}`. HITL alone
  keeps it out of the loop's candidates and the pool count, with no new code.
