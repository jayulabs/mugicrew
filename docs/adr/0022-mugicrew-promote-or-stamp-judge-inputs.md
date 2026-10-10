# ADR 0022: mugicrew promote-or-stamp AFK judge — required inputs and failure direction

Status: Accepted
Date: 2026-09-24

> Renumbered from 0016 before land: 0016 collided with main's landed
> 0016-mugicrew-checkpoint-state-and-jinbe-land (concurrent Charts numbered
> upward independently). Systemic fix tracked by a Signal.

Informs: spec-mvp.md §8.3 (chart:defogged is an assertion, not "0 open children")
and §8.4 (the reconcileChart promote-or-stamp seam). Grounded by the
prototype (side ref `refs/mugicrew/prototypes/<chart-id>`).

## Context

The AFK promote-or-stamp run fires on a drained (0 open children),
not-`chart:defogged` Chart and must either PROMOTE remaining fog into fresh defog
beads or STAMP `chart:defogged`. §8.3 exists to forbid the dangerous error — a
**false STAMP** of a chart that is only transiently drained while real fog stands.
Open question: can an agent make this call reliably, and from what
context — the body alone, the closed children, the Decisions-so-far index?

The prototype ran a candidate judge over 4 drained/not-defogged charts (2
2 real, 2 synthetic) across three context slices (fog only / full
body / body + closed children), 8 fresh isolated sonnet sessions, scored against
human ground truth.

## Findings

1. **The safety-critical call is reliable from the body alone.** False-STAMP rate
   was 0/8. Both transient-drain danger cases were caught (PROMOTE, high
   confidence) with only the body.
2. **The natural failure direction is false-PROMOTE — the safe one.** The two
   misses (both on one real Chart) spawned a spurious defog bead rather than a bad stamp.
   A spurious bead is closed by a human; a false stamp lands an under-charted Spec.
3. **Body alone is not enough to avoid false-PROMOTE on *stale fog*.** A Chart's
   Not-yet-specified bullets can read as open while a now-**closed child** already
   answered them, if charting never back-updated the body (one real Chart: 3 "open"
   bullets, all resolved by ADR 0011, Decisions-so-far never updated). Only feeding
   the closed children (title + close reason) flipped the call to the correct STAMP.
4. **Decisions-so-far is a useful but unreliable signal** — authoritative only when
   charting recorded resolutions there (one real Chart did; the other did not).

## Decision

1. **The promote-or-stamp judge session is fed the Chart body AND the Chart's
   closed children (title + close reason), not the body alone.** Closed children
   are required context, not optional.
2. **Closed children close the promote-side spin** the same way §8.3 closes the
   stamp-side one: without them, a stale-fog chart false-promotes every run
   (spawn → un-drain → human closes → drain → false-promote again). The spin guard
   (frontier) must therefore reason over "fog already answered by a closed
   child," which requires the child data in-context.
3. **When the judge cannot confidently classify a load-bearing bullet, it
   ESCALATEs rather than stamps** (never blind-stamp; the fallback policy is
   a sibling fog item).

## Consequences

- The reconcileChart seam (§8.4) must gather the Chart's closed children before
  invoking the judge — a bd query, cheap, already expressible with existing
  primitives.
- Decisions-so-far stays advisory; the build must not treat an empty or stale
  Not-yet-specified section as a stamp signal on its own.
- Ceiling: not measured at scale or on the bare-UPPERCASE body format or
  charts with many closed children; revisit context-window budget if a Chart's
  closed-child set grows large. The stamp-safety result (0 false-stamps) is the
  load-bearing one and held across every slice.
