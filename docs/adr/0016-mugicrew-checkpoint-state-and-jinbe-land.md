# ADR 0016: mugicrew Checkpoint state (display-only read-model) + Jinbe land orchestration

Status: Accepted
Date: 2026-09-24

Builds on: ADR 0001 (chain-not-mutation; crewmates drive the graph), ADR 0002
(kind waterfall), ADR 0003 (stateless dispatch + lease re-dispatch), and ADR 0004
(Checkpoint land = rebase + squash). Refines ADR 0004/0002. Records the input to
the Spec for the Checkpoint lifecycle machinery and the Jinbe land step.

> Reconstructed from bd notes (the original file was recorded as
> written but never committed to its Chart branch). Content is the session's decisions verbatim.

> **Amended by ADR 0147:** §1/§6 — there is now a dispatched land bead: Jinbe
> owns a `landing` leaf, spawned at the land gate, and the Checkpoint reconcile
> no longer calls `Land` itself.

## Context

The Checkpoint (`checkpoint/<id>`) is the build branch that parents the
Build/Review/Critique/Approval beads (ADR 0004). Its lifecycle *is* the branch
lifecycle (`cut → in-review → approved → merged`). This defog decided **how** that
state is computed and **where** each tail orchestration step lands — and how Jinbe
lands the branch without re-reviewing on a clean rebase.

## Decision

**1. State is a DISPLAY-ONLY read-model, not a dispatched bead.** The Checkpoint
state is an enum derived on read over the ADR 0002 waterfall
(`merged → approved → in-review → cut`). No reconcile switches on it; orchestration
uses **targeted predicates** instead — `allBuildsClosed` / `openReviewExists` /
`cleanReviewExists` / `approvedAtHead`. Extend the `bd.BuildChildren` pattern with
review/approval counts. There is no dispatched "Checkpoint reconcile" bead.

**2. Intermediate spawns are LEAF-CLOSE-SPAWNS-NEXT (ADR 0001).** Orchestration
rides on leaf closes, not a container reconcile: Franky closing the last Build
spawns the Review; Usopp closing a clean Review spawns the Approval; Luffy closing
the Approval drains the Checkpoint structurally, and the loop hands it to Jinbe.

**3. SPAWN-BEFORE-CLOSE is mandatory on every tail edge** (matching the existing
spine). With idempotent `Spawn…IfAbsent` + ADR 0003 lease re-dispatch, the state
"predecessor closed, successor missing" is unreachable.

**4. Land gate = `approvedAtHead`, NOT "0 open children".** Jinbe lands only when
there is a closed Approval whose pinned SHA equals `rev-parse checkpoint/<id>`. A
Checkpoint drains *structurally* (it has no `chart:defogged`-style label), so
"0 open children" is too weak and opens a premature-drain hole. `approvedAtHead`
is ADR 0004's "drained, approved" made computable; it fails safe — a stuck
Checkpoint is visible and never lands unreviewed. The gate counts only an
approved close (ADR 0203 §5).

**5. Approval is COMMIT-PINNED; the pin flows Review → Approval by succession.**
Stored as a body line `Commit: <sha>` plus a parse helper (matching the
conflict-bead SHA-in-body precedent). A new `git.Head(branch)` helper supplies the
live HEAD.

**6. Jinbe is PURE-GO — a `reconcileChart` twin.** He runs
`Land(checkpoint/<id>, TrailerFor(checkpoint, id))`; on a `*RebaseConflict` he
files a `kind:conflict` member carrying `Base` (Franky-owned, **AFK** — unlike the
HITL chart-conflict); a clean land closes the Checkpoint. No agent session. His
registry row is the Checkpoint **handler**; he owns no leaf kind.

**7. Idempotency keys on the LAND TRAILER, not the pin** (corrects ADR 0004 §7's
ancestry check → the shipped `git.Land` trailer-grep). Pre-land gate =
`approvedAtHead`; the post-land durable fact = the trailer. `reconcileCheckpoint`:
if `LandedBase(trailer)` exists → close if open, return; else require
`approvedAtHead` → `Land` → close. Loop candidacy =
`open && (approvedAtHead || landed-trailer)`. A post-land crash rolls forward and
never strands.

**8. A CLEAN REBASE NEVER RE-REVIEWS; re-review is EVENT-DRIVEN.** Re-review is
triggered by conflict-close / critique-close / late-Build via succession — never
by a HEAD poll. The gate is pre-land (HEAD still equals the approved commit), so
mainline drift falls out for free. A stale Approval is actively obsoleted
(`supersede`) for hygiene, but the pin already makes it inert, so that is
safety-optional.

## Consequences

- New seams: `git.Head(branch)`; the body `Commit:` pin + parser; `approvedAtHead`
  plus review/approval counts via `bd.BuildChildren`; a loop Checkpoint
  drain-candidacy arm.
- Jinbe ships as pure Go with no agent prompt — the cheapest crewmate.
- Constrains the per-crewmate defog beads: Franky (spawns Review on
  last-Build-close; `kind:conflict` resolve → fresh Review), Usopp (pins
  Review to HEAD, spawns Approval on clean close), Luffy (closes
  the Approval carrying the inherited pin).

## Rejected / deferred

- **Unify Chart with a commit-pinned Approval bead** — rejected (user: DROP). A
  Chart has no distinct adjudicator or review gate (ADR 0009);
  `chart:defogged` self-certification and a Checkpoint's external certification
  are different jobs by design.
- **Deferred** (Chart Not-yet-specified): repeated obsolete-respawn churn; the
  semantic-conflict-after-clean-rebase case (ADR 0004 §5 CI-gate).
