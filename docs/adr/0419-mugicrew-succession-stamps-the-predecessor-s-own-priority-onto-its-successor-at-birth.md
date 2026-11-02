# ADR 0419: Succession stamps the predecessor's own priority onto its successor at birth

Status: Accepted
Date: 2026-10-30

Amends: ADR 0410 (mugicrew-effective-priority-is-the-most-urgent…): its §6 and
its rejected "stamp the container's priority at creation" option are about
container *members*, not successors.

Chart mc-nbdy8 (Triaged priority carries along succession to the Spec and
Checkpoint), settled by its Survey mc-nbdy8.1. Luffy sets a priority at triage
on the Chart, fast-lane Spec or Initiative, but no successor spawn set one, so
every Spec and Checkpoint further down the spine was born at bd's default P2 and
the dispatch level a human saw drifted from the one triage agreed.

## Decision

1. **Stamp at birth.** A successor is created with its predecessor's **own**
   priority as its own priority. It is not the predecessor's effective
   priority: an Initiative's level still reaches the successor live through
   ADR 0410, because the successor is born into the same Initiative.
2. **Every succession, one seam.** The stamp happens in the successor-creation
   seam (`SpawnSuccessorIfAbsent`, `SpawnDiscoveredIfAbsent`), so it covers
   Signal→Chart, Signal→Spec (fast lane), Chart→Spec, Spec→Checkpoint, the
   renumber Checkpoint minted from a landed one, a pivot's corrected Spec and an
   escalated Chart. A non-succession create through the same seam, such as a
   pivot dismiss's Critique, is not stamped. Luffy's explicit `--priority` at
   triage still runs after the spawn and wins.
3. **A pivot's successor stamps from the Checkpoint it replaces.** The corrected
   Spec and the escalated Chart take the obsoleted Checkpoint's own priority,
   not the original Signal's that the escalated Chart is `caused-by`. That
   Checkpoint is the bead whose ordering carries to them (ADR 0085), and the one
   a human may have `+`/`-`'d.
4. **Only at creation.** A re-run that finds an existing successor leaves its
   priority alone, so a human's `+`/`-` on a Draft or published successor is
   never overwritten.
5. **No backfill.** Specs and Checkpoints born at P2 before this decision are
   fixed by hand with `+`/`-`.

## Considered Options

- **Count closed predecessors in effective priority** (walk `caused-by` as well
  as the parent chain). Rejected: the most-urgent rule would pin a Spec at its
  Chart's level, so a later `-` on the Spec's own priority could not take
  effect, which triage settled it must.
- **Stamp the predecessor's effective priority.** Rejected: it freezes an
  Initiative's level into own priority, the frozen copy ADR 0410 rejected.

ADR 0410's objection to stamping, that a later `+`/`-` on the source never
reaches the copy, does not apply to succession: the predecessor closes into its
successor, so nobody edits it afterwards.
