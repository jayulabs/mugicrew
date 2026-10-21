# ADR 0082: mugicrew abandon pause — every abandoned effort leaves its bead `paused:abandoned`; it never clears itself, run-now refuses it, and only the human lifts it

Status: Accepted
Date: 2026-10-07

## Context

A loop force stop or a run-now host's second signal abandons in-flight
efforts (ADR 0050 §2, ADR 0330). A confirmed TUI quit once did too (ADR 0051
amendment, superseded). The claim stays `in_progress`, and the
orphaned reconcile child and its `claude` keep running in the effort's tree. Until the Chopper Signal exists, the human recovers with `bd unclaim`
(plan of record, bd memory `mugicrew-loop-off-until-tui-confidence`). Once the
bead is unclaimed it is back in `bd ready`, and the next tick could dispatch a
new effort onto a branch the orphan may still be writing. The abandon pause
closes that gap. This amends ADR 0055 §2 (a fourth cause), ADR 0057 §1/§5
(its colour and `t`) and ADR 0078 §6 (precedence). Grilling
settled the surface.

## Decision

1. **Cause `paused:abandoned`.** It matches the `effort.abandoned` event, and
   it is written additively (ADR 0070 §3). Like every loop-written cause, it
   pauses only its own bead and never cascades (ADR 0081).
2. **Written wherever `effort.abandoned` is logged.** Today those are the
   loop force stop and a run-now host's second signal.
   A TUI quit is no longer a cause: the host outlives the TUI.
   Each abandoned effort's bead gets one label. Restart never abandons,
   because it drains fully (ADR 0062). SIGKILL of the loop or of a run-now host, a crash and terminal close can't
   write anything and stay unpaused and unlogged, as before.
   That gap is left to Chopper.
3. **Label first, then the event.** For each effort the host makes one
   `bd update <id> --add-label paused:abandoned` call. It is commutative
   (ADR 0070 §1). Then it logs `effort.abandoned`, which gains
   `paused: true|false` and, when the write failed, its `error`. A failed
   write logs at Warn and **does not block** the force stop or the quit,
   because abandon is the escape hatch and must not hang on bd. No separate
   `effort.paused` is logged, since `effort.abandoned` already says it.
4. **It never clears itself**, because the orphan's death isn't observable to
   the loop. In this Spec only the human lifts it:
   `bd label remove <id> paused:abandoned`. The intended recovery is: check
   the orphan is dead, remove its tree, then `bd unclaim` and lift the label
   in either order. The pause is what keeps the unclaim safe. **Chopper may
   later lift it**, but only once its liveness probe finds no live
   `mugicrew`/`claude` process for the bead **and** the orphan tree is gone.
   That belongs to Chopper's Chart, not here.
5. **Run-now refuses it.** `t` is hidden from the footer and refuses with
   the hint "abandoned effort may still be running; check, then
   `bd label remove <id> paused:abandoned`". Unlike failing (ADR 0078 §4),
   a run-now success can't lift it, because the run itself could race the
   orphan on the same branch.
6. **`p` is unchanged.** It adds or lifts `manual` only (ADR 0057 amendment) and never touches `abandoned`.
7. **Precedence: manual > abandoned > failing > foreign-hold.** Abandoned
   flags a possible live writer and needs a human check first. Failing only
   needs a retry decision. Manual stays on top because it is the human's
   explicit word.
8. **TUI.** `‖` is orange (208) for abandoned. It is distinct from failing's
   red but still reads as a warning. While the bead is still `in_progress`,
   `◐` takes the slot (ADR 0057 §1), so `‖` shows only after the unclaim.

## Considered and rejected

- **`paused:orphaned` / `paused:stranded`.** They don't match the existing
  `effort.abandoned` event name.
- **Logging `effort.abandoned` first, then pausing.** The event couldn't say
  whether the pause landed.
- **Blocking the force or quit on the bd write.** Force is the escape hatch
  for a stuck drain. A hung bd would trap it.
- **Run-now lifts it on success (as failing does).** The run would share a
  branch with a possibly live orphan.
- **Ranking abandoned below failing.** Failing is never dangerous to retry
  by hand. Abandoned can be.

## Consequences

- `bd` gains `PausedAbandoned`. `pauseSty` gains orange, `runNowLabel` and
  `t`'s refusal gain the abandoned case, and `PausedCauses` ranks
  `manual`, `abandoned`, `failing`, then the rest.
- The loop's force path and a run-now host's second-signal path each add one bd
  write per abandoned effort before `RecordAbandoned`. `RecordAbandoned`
  gains the `paused` and `error` fields.
- Siblings routed to the orphan's branch still hit `effort.busy`/
  `effort.held`. The pause covers only the abandoned bead itself.

## Amendment: precedence gains `integrate-conflict` (ADR 0104)

§7 becomes **manual > abandoned > integrate-conflict > failing >
foreign-hold**.

## Amendment: recovery keeps `effort/<id>` (ADR 0105 §5)

A Build or Critique now runs on its own `effort/<id>`. Abandon, crash,
unclaim and drop never delete that branch, and `effort.abandoned` gains
`branch` (the routed branch). §4's recovery becomes:

1. Check that the orphan is dead.
2. Run `bd unclaim` and lift `paused:abandoned`, in either order.
3. Press `l` or `t`. The stale tree goes through the orphan confirm (ADR 0105
   §2), and the launch reuses `effort/<id>`, so the orphan's commits roll
   forward.
