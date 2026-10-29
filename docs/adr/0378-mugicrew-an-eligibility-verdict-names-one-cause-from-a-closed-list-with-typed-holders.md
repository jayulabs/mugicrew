# ADR 0378: An eligibility verdict names one cause from a closed list, with typed holders

Status: Accepted
Date: 2026-10-28

Chart One eligibility module for loop dispatch and TUI run-now (mc-3vp53) moves loop
dispatch, the run-now press gate and the TUI row hint onto one pure verdict that carries a
structured reason. Today only a few checks name what holds a bead, and
`CheckpointLandHold` collapses its not-ready legs into one `false`
(docs/research/eligibility-drift-across-loop-run-now-and-row-hint.md §3). The human
settled the reason's shape: **a not-runnable verdict names exactly one cause, the first
leg that fails in a fixed order, from a closed list. Each cause carries typed holders.**

## Precedence (the first failing leg is the cause)

1. `not-candidate`: the bead's kind or status is not one the verdict runs (HITL, closed, …).
2. `paused`: the bead's own `paused:*` labels (see Pause causes).
3. `inherited-pause`: nearest `manual`/`context` ancestor (ADR 0081). Holder: that ancestor.
4. `container-frozen`: a Checkpoint's freezing blocker on a member (ADR 0377
   (loop-and-run-now-eligibility-differ…)). Holder: the blocking bead.
5. `blocked`: not in the frontier widened by Readmit. Holders: the bead's own and
   inherited blockers. Force waives this leg and no other (ADR 0377 §4).
6. `paced`: loop mode only, inside the poll interval. No holder.
7. The arm's legs:
   - **AFK leaf:** `surgery-hold` (holders: the open Surgeries).
   - **Drained Chart:** `undrained`, then `landing-open` (unless the landed trailer is
     on mainline), then `foreign-landing`.
   - **Checkpoint:** `defect-open` (holders: the open conflict and critique children),
     then `foreign-landing`, then a landed trailer (runnable, action `land`), then
     `landing-open`. With an approved Approval at the head: `undrained`, then
     `land-held` (holders: the open incoming blockers), else runnable `land`. Without
     one: `pivot-open` (holders: the open Pivots), `undrained`, `awaiting-approval`
     (holder: the open Approval at the head), `no-members`, and last runnable with
     action `review` in loop mode, or cause `review-due` in run-now mode.

`undrained` is one cause for a Chart's children and a Checkpoint's members: same
predicate (`DrainBlockersOver`), same holders (open beads, and held closed beads with
their effort branches). `landing-open`'s holder is the Landing; `foreign-landing`'s is
the foreign sha (ADR 0187 (a-land-trailer-carries-its-graph…) amendment).

## Actions

A runnable verdict's action is `run`, `spec`, `land`, `retry`, `retry hold` or
`review`. `review` is loop-only: it is the dispatch that has Jinbe spawn the Review, and
run-now gets cause `review-due` there instead (ADR 0377 §5).

## Pause causes

`paused` carries the bead's highest-ranked cause (`bd.PauseRank`) verbatim, an unknown
cause's name included, so `pauseHint` can word it. In run-now mode the verdict is
runnable as `retry` or `retry hold` only when **every** cause on the bead is on ADR
0377's five-cause pass list. Otherwise it refuses.

## Holders

`Holders []Holder`, where a `Holder` is the whole `bd.Bead` (title and kind feed the TUI's
wording) plus an effort branch, which only held beads have. The foreign sha is a field
of its own, since it is the one holder that isn't a bead.

## Considered Options

- **Report every failing leg.** Rejected: the loop needs only yes or no, the TUI shows
  one hint, and listing every leg costs the loop reads on every tick.
- **Pause causes as a closed enum with an `unknown` bucket.** Rejected: only the pass
  list branches on the value, so a mapping would just be another place to drift. An
  unknown cause's name also stays visible.
- **Holder ids only, callers re-derive branches.** Rejected: the row and the press would
  re-derive from different effort sets.
- **`land` with a review-due annotation in loop mode.** Rejected: the action should say
  what the dispatch does.
- **Separate `chart-undrained`/`members-undrained` and `conflict-open`/`critique-open`.**
  Rejected: same predicate, and each holder's kind already tells them apart.
