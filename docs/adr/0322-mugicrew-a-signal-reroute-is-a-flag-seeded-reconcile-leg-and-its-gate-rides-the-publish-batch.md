# ADR 0322: mugicrew a Signal reroute is a flag-seeded reconcile leg, and its Checkpoint gate rides the publish batch

Status: Accepted
Date: 2026-10-21

Builds on: ADR 0035 (signal-triage-fast-lane) §10, ADR 0043 (pivot-raise-and-checkpoint-gate)
§3, ADR 0045 (concurrent-pivots) §1, ADR 0096 (signal-closes-at-publish), ADR 0103
(sibling-races-become-container-steps…) §3. Decided in grilling.

## Context

Signal triage gains a reroute outcome: a Signal that reports a defect in an
open, unlanded Checkpoint's code, or a wrong FR in its Spec, becomes a
`kind:pivot` and/or a `kind:critique` on that Checkpoint, with no new Spec. The
inventory (`docs/research/signal-reroute-reader-writer-inventory.md`) found the
raise is effort-shaped: `bd.RaisePivot` takes the Checkpoint from `bead.Parent`
and is called only from Franky's and Usopp's tails; `PublishDrafts` hands no
edges, and its close keys on a caused-by Chart, Spec or Initiative. Two further
facts constrain the shape: `Bead.Open()` counts `deferred`, so a gate edge onto
a Draft pivot shows it to `OpenPivots`, the sweep and the TUI before publish;
and Go resolves only Checkpoint → Spec (`bd.SpecOf`), never the reverse.

## Decision

1. **A reconcile leg seeded by flags, not its own verb.** Luffy writes the
   bodies to files and runs `mugicrew reconcile <signal> --checkpoint <cp>
   [--pivot <body-file>]... [--critique <body-file>]... [--merge-into <pivot>
   <evidence-file>]...`. Go creates each pivot and Critique as a Draft by the
   Signal (`drafted-by:<signal>`; a Critique parented to the Checkpoint).
   `reconcileSignal` then picks the leg from the graph: a Signal with reroute
   Drafts takes the reroute leg. The ADR 0043 §3 split holds as content versus
   wiring: the agent writes every body, its evidence and its FR; Go writes every
   bead and edge.
2. **Reconcile checks the target.** `--checkpoint` names an open
   `kind:checkpoint` that `SpecOf` resolves a Spec for; otherwise it refuses
   before any write.
3. **A pivot Draft records its Checkpoint in metadata**
   (`{"reroute_checkpoint": "<cp>"}`), written by Go at creation. A Critique
   Draft's parent already names it. A drop deletes the record with the Draft,
   and no walker reads it: the gate edge stays the one index (ADR 0043 §2).
4. **The gate rides the publish batch.** `PublishDrafts`' close helper becomes
   a publish plan that returns edges as well as the close. For a Signal with
   reroute Drafts it hands, per pivot Draft, the Checkpoint `blocks` edge and
   the pivot's `related` edge to the Spec, plus §7's merge edges and the
   Signal's close, in the batch that opens the Drafts: the pivot and its gate
   become visible together and the Checkpoint is never briefly unfrozen. A
   Critique-only reroute hands no gate, only the close. `RaisePivot` takes the
   Checkpoint explicitly (`RaisePivot(ctx, raiser, checkpoint, spec,
   pivots)`): Franky and Usopp pass `bead.Parent`, triage passes the metadata
   Checkpoint, and both share one edge build that filters present edges.
5. **The publish re-reads the Checkpoint.** If it is no longer open, or
   `SpecOf` fails, the publish fails: Signal open, Drafts deferred, pane kept
   for a resume or a drop (ADR 0096 §4).
6. **Refuse to mix legs, before any write.** The reroute leg refuses a Signal
   with a caused-by Chart, Spec or Initiative successor (any status), and a
   Jira-mirrored Signal, as the Chart leg does. The Chart and fast-lane legs,
   `SeedSpec` and `SeedInitiative` refuse a Signal with reroute Drafts: the
   guard becomes "more than one leg's evidence". A reroute's Drafts all name one
   Checkpoint; a second `--checkpoint` is refused.
7. **Merge into an open same-FR pivot by flag** (ADR 0045 §1). The same-FR
   judgement is the agent's. Reconcile checks the target is an open, published
   pivot blocking this Checkpoint (a Draft pivot never qualifies, ADR 0103 §3)
   and writes nothing. The merge's lineage edge rides the publish batch. The
   evidence is posted as a comment after the batch succeeds, guarded by a
   marker line naming the Signal, so a re-publish re-posts a lost one and never
   doubles it.
8. **Idempotency.** A drop or relaunch deletes the reroute Drafts and their
   metadata; the Signal stays open and a re-run starts fresh with new IDs
   (ADR 0096 §6). After a crash mid-reconcile, a flagless re-run finds the
   Drafts and converges; a flagged re-run while reroute Drafts exist is refused
   ("drop and re-triage to change the reroute"), so no second set stacks. After
   a crash mid-publish nothing was written; present edges are filtered and a
   close of a closed bead is a no-op.
9. **One eventlog record**, `triage.reroute`, written by reconcile once its
   checks pass and the Drafts exist: the Checkpoint and the pivot, Critique and
   merge IDs, as `triage.fastlane` does.

Left to sibling decisions on this Chart: the Signal's close reason and what a
Signal raiser means to Zoro and to dismiss or amend (whether a Signal may be a
pivot's raising bead); the type of every Signal-to-pivot, Signal-to-Critique and
merge lineage edge, and the carry (how a rerouted Signal's lineage is recorded).

## Considered options

- **Agent `bd create`s the Drafts and reconcile discovers them.** Closest to the
  Franky/Usopp raise, but it is the hand-wiring ADR 0035 §10 rejected for the
  fast-lane Spec.
- **Its own `mugicrew reroute` verb.** A plain `reconcile` re-run after a crash
  would take the Chart leg and give the Signal a second successor (ADR 0035
  §10's reason).
- **Reconcile writes the gate onto the Drafts.** A Draft is `Open()`, so the
  pivot is swept or launched before publish, and the Checkpoint freezes while
  triage is still deciding.
- **Another hook adds the gate after publish.** It leaves a published, ungated
  pivot: the orphan the straggler sweep never retires.
- **Checkpoint recorded as a `related` edge pivot → Checkpoint.** A second edge
  beside the gate (rejected in ADR 0043 §2), and the stats and retrospective
  spines read `related` from a pivot.
- **Checkpoint recorded as an edge from the Signal.** The Signal outlives a drop,
  so a re-triage aimed elsewhere finds two.
- **A flagged re-run replaces the Draft bodies in place.** Nothing reliable
  matches old Drafts to new flags.
- **Merge comment at reconcile, or merge as a fresh pivot.** The first leaves
  evidence on the pivot for a dropped reroute; the second costs the human an
  extra decision when the sweep closes it `superseded:`.
