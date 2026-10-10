# ADR 0005: mugicrew spec amendment — the pivot

Status: Accepted
Date: 2026-09-19

Amends: ADR 0002 (adds the `pivot` kind — 12 kinds become 13).
Amended by: ADR 0043 (raise contract; one Checkpoint `blocks` gate replaces §4's
per-Build edges), ADR 0044 (amend outcome), ADR 0045 (concurrent pivots), ADR 0046
(triage session); §6 routing is now HITL, not the AFK dispatch loop.

## Context

The spine (ADR 0002) flows one way: `Signal → Chart → Spec → Checkpoint → Build`.
The Spec is a **closed record**, not a parent. But work on a Checkpoint can prove
the Spec *itself* wrong — the plan, not the code. The `supersedes` (obsoletion)
edge exists for "a bead proven wrong and replaced," but the *upstream-flow*
decision — how that disproof travels back up the closed spine — was left open.

## Decision

**A disproven Spec re-enters the spine at the stage the error entered, carried by
a new `pivot` bead that Zoro triages. Confirmed pivots obsolete the old chain and
respawn it; dismissed pivots let work resume.**

1. **Re-enter at the error stage** *(the shape)*. A Spec can be wrong two ways,
   and the correction re-enters at the shallowest stage that can fix it:
   - a **slice/translation** error (the Chart was sound, the Spec mis-derived it)
     → a corrected **Spec** (Zoro);
   - a **wrong charting assumption** → a fresh **Chart** (Nami re-defogs) → new
     Spec.
   Both obsolete the wrong bead via `supersedes`. This mirrors the rest of the
   model (Critique → Franky, conflict → Franky, stale approval → re-Review — never
   further back than needed). Always re-charting from a `Signal` was rejected as
   paying the full re-chart tax for a trivial slice fix.

2. **A pivot is a bead: `kind:pivot`** *(this amends ADR 0002)*. A leaf, **owned
   by Zoro**, raised by the discovering crewmate (Franky on a Build, Usopp on a
   Review) as a `discovered-from` **offshoot** of that Build/Review, pointing at
   the Spec. It must be a distinct kind because owner is a fixed function of kind:
   a `critique` is Franky-owned and work-level, so it cannot route the decision to
   Zoro — the same reason `conflict` earned its own kind (ADR 0004). Named for the
   act it triggers.

3. **Zoro triages, with escalation.** The pivot lands with the Spec's owner:
   - *slice error* → Zoro obsoletes the Spec (`supersedes`) and produces a
     corrected one;
   - *charting-assumption error* → Zoro **escalates**: obsolete the Chart
     (`supersedes`), spawn a fresh Chart for **Nami** to re-defog → new Spec.
   One entry point (always the same bead), two outcomes from one triage — no
   second decision on the discovering side.

4. **Halt, then obsolete — with a dismiss path** *(the in-flight Checkpoint)*.
   There is no stored "paused" state, so a live pivot **blocks** the Checkpoint's
   remaining open Builds (ordering edge) — Franky stops, nothing discarded yet.
   Then:
   - **Zoro confirms** → the resolution obsoletes (`supersedes`) the old Spec *and*
     the old Checkpoint and its open Builds (closed as obsolete, kept for lineage);
     the corrected Spec spawns a **new Checkpoint** by normal succession.
   - **Zoro dismisses** (the Spec was fine; the confusion was work-level) → the
     pivot closes as dismissed, the block lifts, Builds resume — the finding may
     instead become a `critique`.
   A pivot is a claim, not a verdict; discarding in-flight work before Zoro rules
   would be premature, and the dismiss path mirrors approval-reject.

5. **The new Checkpoint cuts a fresh branch** from the current base — a Checkpoint
   is the branch, and the chain is immutable, so it is born fresh, never
   re-targeted onto the old branch. The old branch is **abandoned, not
   auto-deleted**: its commits stay in git, cherry-pickable by a human.

6. **Idempotency inherits ADR 0003.** *(Amended by ADR 0043: `kind:pivot` is
   HITL — surfaced in the Inbox for a human-launched Zoro session, never
   dispatched AFK.)* The pivot is a leaf routed by the standard
   dispatch loop (`kind:pivot → Zoro`); Zoro's spawns (corrected Spec, escalated
   Chart) are check-exists-before-spawn, per ADR 0003's blanket retry-safety rule.

## Consequences

- **13 kinds.** `pivot` joins the taxonomy; Zoro now owns `spec` and `pivot`
  (and still stewards the `checkpoint`). Franky and Usopp *raise* pivots but do
  not own them (creator ≠ owner, already normal).
- **The spine gains a back-edge without becoming mutable.** Upstream feedback
  flows by obsoleting-and-respawning closed beads, not by reopening or editing
  them — the chain stays append-only.
- **Salvage is out of scope.** Which old-branch commits are worth keeping is a
  human/Franky judgment; the graph does not model or automate it.
- **A pivot is expensive by design.** Confirming one discards a Checkpoint's
  in-flight work; the block-first, dismiss-capable flow keeps that cost from being
  paid on a false alarm.

## Alternatives considered

- **Re-Spec only (never re-chart).** Rejected: too shallow when the mistake was a
  charting assumption — re-slicing the same bad Chart repeats the error.
- **Always re-enter at the top as a fresh `Signal`.** Rejected: one clean path,
  but it re-charts even for a trivial slice fix.
- **Reuse `critique` as the carrier.** Impossible: owner-from-kind fixes a
  critique to Franky; it cannot route the plan-level decision to Zoro.
- **Obsolete the Checkpoint immediately on raise.** Rejected: discards in-flight
  work before Zoro confirms the pivot is real; no room for a dismiss.
- **Re-target the old branch for the corrected Checkpoint.** Rejected: breaks
  "a Checkpoint is born fresh" and the immutable-chain rule.
