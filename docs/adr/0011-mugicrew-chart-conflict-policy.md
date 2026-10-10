# ADR 0011: mugicrew chart-conflict policy — the Chart-land rebase conflict is a Nami-owned bead

Status: Accepted
Date: 2026-09-22

Amends: ADR 0004 §4 (its `conflict` kind stays Franky-owned and Checkpoint-only;
this adds a Chart-side sibling `chart-conflict`, owned by Nami) and ADR 0002
(a 13th kind).

## Context

`reconcileChart` lands the drained Chart branch by rebase + squash onto mainline
(ADR 0009/0010). `git.Land` returns a typed `*git.RebaseConflict{Base}` when that
rebase conflicts (`git/git.go`), and today `reconcileChart` propagates it raw — a
deliberate deferral to this fog (`nami/nami.go:201`, ponytail comment). Nothing
catches it, so a Chart-branch textual collision (two Charts editing `CONTEXT.md`,
or colliding on an ADR number) dead-ends as an error.

ADR 0004 §4 already models a rebase conflict as a bead — but as `kind:conflict`,
**owned by Franky**, a member of the **Checkpoint**, and §6 hangs a full re-review
off closing it (fresh Review → new Approval → supersede the old). That contract is
wrong for a Chart: owner-from-kind is total and single-valued with no co-ownership
(ADR 0002), so `kind:conflict` cannot also be Nami's; and a Chart has
**no review/approval gate** — charting merges on defog (ADR 0009) — so there is no
review to re-spawn.

## Decision

1. **A Chart-land conflict is a bead: `kind:chart-conflict`.** A leaf, **owned by
   Nami**, created by Nami's `reconcileChart` as a **member of the Chart**. It
   **carries the target base commit hash** (`RebaseConflict.Base`) to rebase onto,
   mirroring ADR 0004 §4's payload and ADR 0010's landed-base seam. It is a
   *distinct* kind from the Checkpoint's `conflict` because owner-from-kind forbids
   co-ownership — the Chart side needs its own routing key (`kind:chart-conflict →
   Nami`), not a reassignment of Franky's.

2. **`reconcileChart` files the bead instead of propagating.** On `*RebaseConflict`
   it creates one `chart-conflict` child carrying the base SHA. The open child makes
   the Chart no longer drained, so reconcile **stops** — no re-dispatch spin.
   Idempotent: if an open `chart-conflict` child already exists under the Chart, do
   not spawn a second (open-child guard, the `SpawnSuccessorIfAbsent` pattern).

3. **Resolve is a checklist HITL, not an agent session.** `Nami.Launch` dispatches
   `chart-conflict` through the `taskChecklist` variety (`nami/nami.go:76`): a human
   rebases `chart/<id>` onto the carried base, resolves the `CONTEXT.md` /
   ADR-number collision by judgment, and `bd close`s the bead. No new agent prompt.

   > **Amended by ADR 0030:** chart-conflict now resolves as a HITL agent
   > session (both variants), not a checklist; and an ADR-number clash is
   > detected before the rebase, since it never surfaces as a path conflict.

4. **Closing re-drains the Chart; no re-review.** With the conflict bead closed the
   Chart is drained again → `reconcileChart` re-runs → clean land. Unlike ADR 0004
   §6, **nothing is re-reviewed**: a Chart has no review/approval gate (ADR 0009),
   so there is no stale Approval to obsolete and no fresh Review to spawn. The
   human's hand-resolve is the gate.

## Consequences

- **13 kinds.** `chart-conflict` joins the taxonomy; Nami now also owns it
  (`grilling`, `research`, `task`, `chart-conflict`; steward of `chart`).
- The Chart side mirrors the Checkpoint side (ADR 0004) — a rebase conflict earns
  its own kind with its own base-SHA payload — but resolves in Nami's charting
  surface, not Franky's code/re-review path.
- **Known ceiling:** a clean textual rebase that is a *semantic* collision still
  lands (the same ceiling as ADR 0004 §5) — future CI fog, not decided here.

## Out of scope

- Wiring `chart-conflict` into `contract.go` / `registry.go` and building the
  `reconcileChart` catch + `Nami.Launch` case is separate follow-up work; this ADR is the
  contract it builds against.
- Franky's `conflict` / Checkpoint re-review path (ADR 0004) is unchanged.

## Alternatives considered

- **Reuse `kind:conflict`, reassign owner to Nami for a Chart parent.** Rejected:
  breaks owner-from-kind (ADR 0002) — owner would depend on the parent container,
  i.e. co-ownership of `conflict` — and drags in §6's Review/Approval machinery a
  Chart does not have.
- **File it as a plain `kind:task` + a marker label.** Rejected: cheaper, but ADR
  0004 already set the precedent that a rebase conflict is a distinct problem with a
  distinct payload and workflow deserving its own kind, and a distinct kind makes
  the idempotency guard a clean by-kind query.
- **Agent charting-resolve session.** Rejected as speculative: a CONTEXT/ADR-number
  collision is a human-judgment merge of a handful of steps; add an agent only if
  hand-resolve proves fiddly.
