# ADR 0021: mugicrew promote-or-stamp candidacy gating — exclude un-charted Charts; no residual spin guard

Status: Accepted
Date: 2026-09-24

Informs: spec-mvp.md §8.4 (the reconcileChart promote-or-stamp seam + loop gating)
and §6.4 (loop candidate class). Builds on ADR 0022 (judge inputs), the fallback
decision, and the routing decision (which broadens
`IsDrainedChartCandidate` by dropping the `chart:defogged` clause).

> Note: this Chart's fallback and routing ADRs were never written as files and
> their intended numbers (0017/0018) are already taken on main by unrelated
> crewmate ADRs. Tracked for remediation by a Signal. This ADR takes 0021,
> the next free number against main.

## Context

ADR 0018 broadens the loop candidate class: `IsDrainedChartCandidate` drops its
`chart:defogged` clause so any drained (0 open children) AFK-container Chart
reaches `reconcileChart`, whose label check then discriminates promote-or-stamp
(not-defogged) from land (defogged).

That broadening exposes a gap. A freshly-cut Chart from `reconcileSignal` carries
the charting placeholder body ("Chart body — to be written by the charting
session as a wayfinder map (...)") and has zero children. It is drained +
not-defogged before the interactive charting session has written the body or
spawned the initial defog children — so it would become a spurious
promote-or-stamp candidate, dispatched (or admitted to the TUI run-now list,
which reuses the same predicate as its parity fence) before there is anything to
judge.

Two questions: where the un-charted gate lives, and whether the
"every run stamps OR spawns ≥1, else re-dispatch every tick" discipline needs a
residual guard beyond what ADR 0022/fallback/roll-forward already provide.

## Decision

1. **Gate un-charted Charts in BOTH the loop predicate and the handler** —
   mirroring this file's existing idiom, where the 0-open-children test and the
   label check each live in `IsDrainedChartCandidate` (for dispatch + TUI-parity)
   *and* in `reconcileChart` ("the loop pre-checks, but reconcile is
   self-contained").

   - `IsDrainedChartCandidate` gains `&& b.Description != placeholderChartBody`.
     `Description` is already on the ready bead, so the predicate stays pure — no
     new I/O. This keeps the frontier truthful (no wasted claim/dispatch churn)
     and keeps the TUI run-now fence from admitting un-charted Charts.
   - `reconcileChart`'s not-defogged branch returns `nil` while the body still
     equals the placeholder — before dispatching the judge — so a direct or stale
     call never runs a promote-or-stamp session over an un-charted Chart.

2. **Promote the charting placeholder body to a shared constant in `contract`.**
   It is currently an inline string in `reconcileSignal` (nami.go). Because the
   loop predicate now compares against it cross-package, it moves to `contract`
   (alongside `LabelDefogged`), and `reconcileSignal` / `reconcileChart` /
   `IsDrainedChartCandidate` all reference the one string so the gate can't drift.

3. **No residual spin guard.** The discipline is fully covered:
   - un-charted → gated by (1);
   - confident STAMP → sets the label, next tick lands via the defogged branch;
   - confident PROMOTE → spawns ≥1 child, un-drains → not a candidate;
   - ESCALATE → fallback spawns 1 grilling child, un-drains;
   - stale-fog false-promote → prevented by ADR 0022 (judge fed closed children).

   The only remaining case is *agent runs but mutates nothing* (harness crash /
   silent no-write). ADR 0017-intent's fallback makes "no mutation on a
   *completing* agent" impossible by construction, so this is only a *failing*
   run — transient, self-heals on re-dispatch, and visible via the loop's
   `Run %s: %v` log. This is exactly the speccing roll-forward already accepted
   (the state change is gated on the artifact/mutation, not on the session merely
   having run). The handler therefore stays thin (ADR 0018): unlike speccing it
   does not re-read/post-verify, because the agent's stamp-or-spawn *is* the state
   change — there is nothing for the handler to do after dispatch.

## Consequences

- One shared constant, one string compare added to a pure predicate, one
  early-return in the handler. No new loop state, no new guard, no post-dispatch
  verification.
- Ceiling/upgrade path (per the fallback decision): add de-dup + `bd human`
  fallthrough only if a real crash-spin is ever observed.
- The build lands downstream under the Checkpoint the Spec cuts.
