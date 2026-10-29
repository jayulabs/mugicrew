# ADR 0377: Loop and run-now eligibility differ only by a named list of mode differences

Status: Accepted
Date: 2026-10-28

Chart One eligibility module for loop dispatch and TUI run-now (mc-3vp53) moves loop
dispatch, the run-now press gate and the TUI row hint onto one pure verdict. The drift
inventory (docs/research/eligibility-drift-across-loop-run-now-and-row-hint.md) found
differences between the three sites, and the row hint also gates the press, so any row
check that is stricter than the press is a hard refusal. The human ruled on each open
difference. The rule: **the two modes differ only by the list below. Any other
difference between the loop, the press and the row hint is a bug that the shared verdict
removes.** The row hint runs the same verdict over the TUI's loaded graph, and staleness
is the only way it may differ from the press.

## The named mode differences (loop vs run-now)

1. **Pacing.** The loop skips a latch, Landing or MR Watch inside its poll interval.
   Run-now has no pacing. Pacing is a loop-mode input supplied by the caller.
2. **Retryable pause causes.** The loop skips `failing`, `integrate-conflict`,
   `not-built`, `profile` and `foreign-hold` (`foreign-hold` per ADR 0055
   (paused-beads) §6). Run-now runs them, and the verdict's action is `retry` or
   `retry hold`. That five-cause list is the only list of passes. **Every other `paused:*`
   cause refuses in both modes**, `checkup` included (ADR 0272
   (a-chopper-checkup…)), as does any cause added later.
3. **Pre-lift.** Before judging, the loop lifts a stale `context`, `profile` or
   `foreign-hold` pause. Run-now judges the labels as they stand. Lifting is a caller
   step that runs before the verdict, never part of it.
4. **Force.** Run-now's confirmed blocked press (ADR 0142 (tui-v2-shell-keys…), ADR 0026 (tui-footer-command-surface))
   is a run-now-only verdict input. It waives only the frontier/Readmit leg: the bead's
   own blocker edges and the ones it inherits. It doesn't waive pauses, a container's
   freezing hold, drain, Landing holds or any Checkpoint leg.
5. **Review due.** Only the loop acts on a review-due Checkpoint (Jinbe spawns the
   Review). The verdict returns the cause `review-due` in both modes, and in run-now mode
   that cause refuses. This makes ADR 0170 (tui-v2-execute-tab-rows…) §"Review due" a
   rule.

## Settled as not a mode difference

- **A container's freezing hold is its own cause** (`container-frozen`, with the
  blocking bead as holder), judged apart from the frontier. `force` never waives it,
  because running a member under a Checkpoint that a Pivot has frozen defeats the Pivot
  (ADR 0270 (the-tui-shows-a-container-s-freezing-hold…)).
- **Straggler retirement** is loop housekeeping that shapes the frontier the loop
  passes in. It stays outside the verdict.
- **Bugs that the shared verdict removes:** run-now's missing ChartLandingHeld; the row
  counting an open Landing or MR Watch as an undrained Chart child (a Landing holds only
  through ChartLandingHeld, with its landed-trailer exception); the row leaving container
  and kind-less children out of a Chart's drain; the row ignoring whether the Approval is
  at the head (the snapshot compares against `cpTips`); the row refusing a
  landed-trailer roll-forward (run-now offers `land`, as the loop dispatches it); the row
  counting only `status=open` conflict and critique children instead of `Open()`.

Foreign landing (a trailer from another Bead Graph) is a cause distinct from
`paused:foreign-hold`. The sibling grilling "Whether run-now refuses a foreign-landing
trailer not yet labelled" (mc-3vp53.4) decides how it applies in each mode.

## Considered Options

- **Keep run-now's refusal table as a list of refusing causes.** Rejected: any cause not
  on it, such as `checkup`, silently passes, and so would every new cause.
- **Let `force` waive a member's freezing hold** (it sits in the live frontier leg).
  Rejected: the y/n is for the bead's own blockers, and a Pivot's freeze must hold.
