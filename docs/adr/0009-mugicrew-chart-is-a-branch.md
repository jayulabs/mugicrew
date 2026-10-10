# ADR 0009: mugicrew Chart is a branch — container kinds are branches; prototypes to a side ref

Status: Accepted
Date: 2026-09-21

Amends: ADR 0001 (the Checkpoint is no longer the *only* branch) and ADR 0002
(the Chart is a branch, not just a "workspace").

## Context

ADR 0001/0002 made **the Checkpoint the branch** ("Checkpoint = epic = the
branch") and cast the **Chart** as Nami's charting *workspace*. But charting
emits **committable artifacts**: grilling produces ADRs and CONTEXT edits,
research produces `docs/research/*.md`, prototyping produces code. Those commits
had no structured home — ADR 0008's own artifacts (this ADR's siblings) landed
with no Chart branch to sit on. Surfaced after ADR 0008.

Two things differ across those artifacts: most are **decisions/knowledge you
build on** (ADRs, CONTEXT, research findings — research is even cited by ADRs,
e.g. the bd-primitives research in ADR 0002), but a **prototype is
throwaway by definition** (the prototype skill: a throwaway built to answer a
design question). Landing throwaway code in the mainline tree invites someone to
build on it.

## Decision

**Container kinds are branches; leaf kinds are beads on them.** This replaces
"the Checkpoint is the branch" with a symmetric invariant:

- **`chart`** is a branch — Nami's charting branch. Charting and its
  knowledge-bearing defog sessions (grilling → ADRs/CONTEXT, research →
  `docs/research/*`) **operate in a worktree of `chart/<id>`**, so their
  commits accrue on the branch; a fog-only Chart accrues nothing and lands an
  empty anchor commit. The branch **merges to mainline when the Chart defogs**,
  in the same commit-flow that records the Spec. (Prototype output does **not**
  land here — see §2; `task` is no longer a defog kind — ADR 0013.)
- **`checkpoint`** is a branch — the build branch, unchanged (its
  `cut → in-review → approved → merged` waterfall, ADR 0002, is build-specific
  and stays specific to it).

Consequences of the symmetry, made explicit:

1. **Chart-branch lifecycle is merge-on-defog — no review/approval gate.** The
   Checkpoint's heavy gate exists for code; charting's gate *is* the defog beads
   resolving and the Spec being accepted. There is no separate `cut/in-review/
   approved` distinction for a Chart branch — it is open (defogging) then merged
   (defogged, Spec spawned). Marked **provisional**: a review gate can be added
   later without disturbing the rest of the model.

2. **Prototype output goes to a side ref, never the mainline tree.** Prototype
   commits are pushed to a dedicated preservation namespace —
   `refs/mugicrew/prototypes/<chart-id>` (exact ref string is a build-time
   detail; the *namespace intent* is what this ADR fixes) — mirroring how beads
   keeps data on `refs/dolt/data`, off the working tree. The prototype is
   **reachable for lineage/audit but never merged**, because downstream work
   builds on the validated *design* (recorded in the Spec/ADR), not the
   throwaway that proved it. Research is **not** treated this way: it is
   knowledge ADRs cite, so it merges to mainline with the rest of the Chart
   branch.

3. **Merge order.** The Chart branch lands on defog (artifacts + Spec) → the
   Checkpoint branch is cut from that landed base. `Chart → Spec` and
   `Spec → Checkpoint` stay 1:1 (ADR 0008 / 0005); the branch topology now
   matches the succession spine: one branch per container, cut from its
   predecessor's landed state.

## Consequences

- Every committable charting artifact has a home: the Chart branch (merges) or
  the prototype side ref (preserved, never merged).
- The invariant simplifies: "container kinds are branches, leaf kinds are beads
  on them" is one rule, replacing "the Checkpoint is the branch, the Chart is a
  workspace."
- Mainline tree = **decisions + knowledge + code you build on** (ADRs, CONTEXT,
  research, Builds). Throwaway prototypes are the sole class kept off it.
- Two branches per effort (Chart, then Checkpoint) instead of one — accepted as
  the cost of giving charting a real merge, and it mirrors the spine.
- ADR 0001 and 0002 carry inline amendment notes; `apps/mugicrew/CONTEXT.md`
  Chart / Checkpoint / Prototype entries are updated.

## Alternatives considered

- **Keep the Chart a non-branch workspace** (status quo). Rejected: charting's
  ADRs/context/research commits then have no structured home and leak onto
  whatever branch happens to be checked out.
- **All Chart artifacts merge, including prototypes.** Rejected: throwaway
  prototype code in the mainline tree invites building on it — the exact
  anti-pattern "throwaway" is meant to prevent.
- **Research + prototype both to the side ref** (mainline = decisions only).
  Rejected: research is knowledge ADRs cite and already lives in
  `docs/research/` on the mainline; moving it off breaks those citations for a
  purity that buys nothing. Only the genuinely-throwaway prototype is isolated.
- **A Chart branch with the Checkpoint's full review/approval waterfall.**
  Rejected: that gate is for code; charting is gated by defog + Spec acceptance.
  Left addable later rather than imposed now.
