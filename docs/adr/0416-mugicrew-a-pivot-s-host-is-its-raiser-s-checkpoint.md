# ADR 0416: mugicrew a pivot's host is its raiser's Checkpoint, derived for a member raiser and recorded for any other

Status: Accepted
Date: 2026-10-30
Amends: ADR 0043 (mugicrew-pivot-raise-and-checkpoint-gate) §3. Builds on ADR 0326
(mugicrew-a-task-raises-through-mugicrew-pivot-raise…) §4 and ADR 0401
(mugicrew-a-landing-s-follow-up-is-born-discovered-from-it) §1.

## Context

Chart mc-3cnhf.64, settled by its Survey mc-3cnhf.64.1. The five pivot creation
doors seemed to follow two rules (bead lifecycle Gaps entry 21). D1–D3 (Franky
Build/Critique, Franky Conflict, Usopp Review) are born discovered-from the
raiser, and no host is recorded. D4 (Luffy's reroute) is caused-by the Signal and
D5 (Brook's Errand raise) is discovered-from the Errand, and both store the host
in `raise_checkpoint` metadata. Both paths already add the gate in a publish
batch: `bd.RaiseIfPivoted` → `bd.RaisePivot` → `bd.CloseOnPublish` for D1–D3,
and `session.raiseSession` for D4/D5, both through `bd.GateEdges`. The split
follows a real difference. A D1–D3 raiser is a member of the Checkpoint and runs
as an AFK effort with a Go tail. A D4/D5 raiser has no parent and runs as a HITL
session whose Drafts a Go verb writes.

## Decision

The split is confirmed by design, stated as one rule:

1. **A pivot's host is the Checkpoint its raiser holds.** When the raiser is a
   member of the Checkpoint (D1–D3), Go derives the host from the raiser's
   parent at the effort tail. When the raiser is not a member (D4, D5), the Go
   verb that writes the Draft records the host as `raise_checkpoint` metadata,
   and the publish re-checks it.
2. **The gate goes out in the raiser's publish batch at every door.** The
   Checkpoint `blocks` → pivot edge and the pivot `related` → Spec edge are
   handed to the same batch that publishes the pivot. The raiser's own close
   comes from its kind: `pivoted:` for D1–D3, `rerouted:` for a Signal, and
   none for an Errand (ADR 0326 §6).
3. **One provenance edge to the raiser.** A pivot filed from a failed attempt
   (D1–D3, D5) is discovered-from its raiser, following ADR 0401 §1. A reroute
   pivot (D4) is caused-by its Signal, because a reroute replaces the Signal
   (which closes `rerouted:`) rather than following a failed attempt. bd holds
   only one edge per ordered pair, so no pivot carries both.
4. **No second host record.** Once a pivot is published, its gate edge is its
   host record (`bd.PivotCheckpoint`). D1–D3 pivots get no `raise_checkpoint`
   stamp, and nothing reads that metadata after publish.

## Considered options

- **Unify on metadata.** D1–D3 would move to a Go verb that stamps
  `raise_checkpoint`, and `raiseSession` would gate every pivot. Rejected: it
  rewrites the Franky and Usopp prompts and their merge path, and the effort
  tail would still have to close the raiser `pivoted:`, only to get a uniform
  record that matters only before publish.
- **Unify on derivation.** D4/D5 would resolve the host again at publish time.
  Rejected: ADR 0326 §2 records the host on purpose so the publish re-checks
  the same Checkpoint the verb checked.
- **Stamp `raise_checkpoint` on D1–D3 pivots too.** Rejected: it adds a second
  record that can drift from the gate edge, which ADR 0043 §2 rejects.
- **D4 discovered-from the Signal, alone or as well.** "As well" is
  impossible (one edge per pair). "Instead" breaks `luffy.RerouteDrafts` and
  the merge lineage, and loses the succession reading.
