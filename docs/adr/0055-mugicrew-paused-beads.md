# ADR 0055: mugicrew paused beads — a single-valued `paused:<cause>` state, set with `bd set-state`, takes a bead out of dispatch candidacy; only a pause whose cause is observable clears itself

Status: Accepted
Date: 2026-10-04

## Context

ADR 0054 classifies a refused worktree cut as a foreign hold and logs
`effort.held`. The loop still flips the bead open↔in_progress on every tick
(588 times on one bead). The Foreign-held busy skips Spec wants
the loop to stop re-claiming after N consecutive foreign-held skips and to
resume once the hold clears. Grilling widened this: the human also
wants to pause a bead by hand, and a manual pause must never clear itself. The
loop keeps no coordination state across ticks (ADR 0050 §2, `loop.go`), and
the TUI runs in a different process from a detached loop. So a pause that
lives only in loop memory would be invisible to the TUI and lost on restart.

## Decision

1. **Paused is a bead state on the graph.** A bead is paused when it carries
   a `paused:<cause>` label. The `paused` dimension is single-valued and is
   written with `bd set-state <id> paused=<cause> --reason "..."`, which
   records a closed event child (reason, actor, time) and replaces any other
   `paused:*` label. `bd state <id> paused` reads it back. A pause is lifted
   with `bd label remove <id> paused:<cause>`, and the event history remains.
2. **Name the cause, not the mechanism.** Every value maps to one clear rule.
   Only a cause the loop can observe may clear itself.
   - `paused:foreign-hold`: written by the loop after **3** consecutive
     `effort.held` skips. It clears itself.
   - `paused:manual`: written by the human. It **never** clears itself.
3. **Candidacy.** `candidates()` skips any bead with a `paused:*` label, for
   every candidate kind (AFK leaf, drained Chart, Checkpoint). It looks only
   at the bead itself. Pausing a Chart or epic does not cascade to its
   descendants (out of scope here; see the pause-controls offshoot Signal).
4. **The count lives in loop memory**, per bead. Only `effort.held` adds to
   it. Any other cut outcome resets it: a successful cut, `effort.busy` (own
   contention, ADR 0054), or a failed cut (ADR 0056). A restart loses at most 2 skips of count. Run-now's
   refusals run in the TUI process and are not counted.
5. **Auto-clear.** Each tick, the loop reads the porcelain worktree list
   (`git.WorktreeOn`) for beads paused `foreign-hold`. When a branch is free,
   or is held only by something ADR 0054 calls own, the loop removes
   `paused:foreign-hold` and logs `effort.resumed`. This is not lease
   self-heal (plan of record, ADR 0050 §6): it reclaims no claim and touches
   no worktree. It only stops skipping once the cause has gone. Removing the
   foreign tree is still the human's job, or the Chopper Signal's.
6. **Run-now.** Run-now overrides `paused:foreign-hold`: it tries the cut.
   If the cut is refused, it logs `effort.held` and the pause stays. If the
   cut succeeds, it removes the pause and logs `effort.resumed`. Run-now
   **refuses** a `paused:manual` bead and shows a hint to remove the label.
   Undoing a manual pause takes a deliberate second act.
7. **Pause never stops a running effort.** It governs candidacy only. An
   effort in flight when the bead is paused runs to its end. If that end
   releases the claim, the bead is open and paused, and is skipped.
8. **Events** (renames the reservation in ADR 0054 §4): `effort.paused` at
   Warn `{branch, holder, skips}` when the loop writes `paused:foreign-hold`,
   and `effort.resumed` at Info `{branch}` when it, or run-now, removes it.
   A manual pause is recorded by its bd event child, not in the event log.

## Considered and rejected

- **Count and mark both in loop memory.** Breaks the no-cross-tick-state rule.
  The TUI can't see it, and a restart clears it without anyone noticing.
- **Count on the graph too.** One bd write per skip, the same churn being
  removed.
- **`bd defer` / `bd human`.** Defer needs a date and hides the bead from
  `ready`. A human flag means "needs a decision". Both stretch past their
  meaning.
- **A `held:*` namespace.** Collides with **Foreign hold**, which is a
  branch's state, not a bead's.
- **`paused:max_retries`.** Names the mechanism. A second retry limit sharing
  it couldn't tell which clear check applies.
- **Stacking labels plus a `bd comment` for the reason.** The reason isn't
  tied to the state change. Single-valued handles collisions for free: a
  foreign-hold pause can never land on a manually paused bead (it is not a
  candidate, and run-now refuses it). A manual pause replaces a foreign-hold
  pause, and if the hold persists after the human unpauses, the loop pauses it
  again after 3 skips.

## Consequences

- Each pause adds a closed event child under the bead. Drain checks count
  open children only, so they are unaffected. Progress counts include the
  event children.
- ADR 0029's catalogue renames `effort.suppressed`/`.unsuppressed` to
  `effort.paused`/`effort.resumed`.

## Amendment: run-now lifts when the effort returns

§6's lift happens when run-now's dispatch returns with a clean cut, not at the
moment of the cut, so `effort.resumed` follows the effort's `effort.finished`
or `effort.failed`. While the effort runs the label stays, but `◐` hides the
`‖` and the loop's auto-clear reads only ready beads. The run-now host (ADR 0330)
lifts it on a clean cut whether or not the TUI lives; the loop's auto-clear
(§5) is the backstop after a host SIGKILL. Lifting at the
cut would need a hook in Dispatch, which none of §6's guarantees requires.

## Amendment: pauses are additive labels (ADR 0070 §3)

ADR 0070 §3 replaces §1's single-valued `set-state` dimension. A pause is
written with `bd update <id> --add-label paused:<cause>`, which writes no event
child, and a bead is paused while it carries any `paused:*` label. Each unpause
removes only its own cause, so lifting `foreign-hold` (§5, §6) never lifts
`manual`. A loop foreign-hold pause racing a manual pause leaves both labels,
a valid state, and `manual` outranks `foreign-hold` wherever one cause is
shown or checked. §2's causes and their clear rules are unchanged. The
"stacking labels" rejection is reversed: `set-state` is several store calls,
so its replacement guarantee was never atomic. The rejected comment-for-reason
half stays rejected; a pause records no reason (ADR 0057 amendment).
The Consequences' event children and `bd state <id> paused` go away.

## Amendment: a manual pause cascades through membership (ADR 0081)

§3's "does not cascade" holds for `foreign-hold` and `failing` only. A bead
with any `parent-child` ancestor carrying `paused:manual` is out of candidacy
and refused by run-now (ADR 0081).

## Amendment: a fourth cause, `abandoned` (ADR 0082)

§2 gains `paused:abandoned`: written by the loop force stop and a run-now host's
second signal for each abandoned effort's bead. It **never** clears itself, and
only the human lifts it (Chopper may later, ADR 0082 §4).

> **Amended by ADR 0158:** `paused:context` is a further cause, written on a container whose `context:` label names an undeclared context and lifted by the loop once the label or config is fixed.
