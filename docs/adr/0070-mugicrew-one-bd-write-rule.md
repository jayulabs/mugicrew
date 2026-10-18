# ADR 0070: mugicrew one bd write rule: every bd mutation is a single commutative or CAS-guarded store call, or a read-then-write that names its serializer; pauses become additive labels and the Cap may overshoot by one

Status: Accepted; §1 serializer list and §2 amended by ADR 0103; Context premise and CAS-call retries amended by ADR 0153 (server mode)
Date: 2026-10-06

## Context

The Concurrency-safe shared state Chart needs one rule that keeps
bd/Dolt writes correct when efforts run concurrently at a per-owner Cap N > 1.

In embedded mode Dolt's exclusive database lock serializes every single bd
store call across processes, and no write ever reports a conflict. So there is
nothing for "detect-and-retry" to detect at the Dolt level. A bd command made
of several store calls is not atomic. That covers `set-state`, and every
mugicrew `…IfAbsent` spawn and `Open()`-guarded close, each of which is two
separate bd invocations. Whole-field edits (`--description`) are silently
last-write-wins. `--add-label` edits commute and all land.

Decided in grilling.

## Decision

1. **The bd write rule.** Every bd mutation is one of these:
   - a **single store call that commutes or is idempotent**: `--add-label`,
     label remove, `dep add`, `close`;
   - a **CAS-guarded single call**: `--if-status`/`--claim`, a `bd batch`
     publish, `bd create --graph`;
   - a **read-then-write that names its serializer**: the container's branch
     hold (ADR 0068), or the claim on the predecessor or creator bead.
   No new bd primitive and no new lock are added. The Spec lists each site with
   its serializer. A site with none is a defect.
2. **Chart-body read-modify-write is serialized by the branch hold.** Grilling,
   prototype and charting all hold `chart/<id>` for their session (ADR 0068),
   so no two of them write one Chart's body at once. A human's TUI edit stays
   last-write-wins, and the human sees the result.
3. **Pauses are additive labels.** `Pause` is `bd update --add-label
   paused:<cause>`, not `set-state`. A bead is paused while it has any
   `paused:*` label. Each unpause removes only its own cause. The TUI shows
   every cause, with `manual` outranking the others for display. A loop
   foreign-hold pause racing a manual pause leaves both labels, a valid state.
4. **The Cap may overshoot by one.** The TUI's check-then-claim
   (`tui/tui.go:1312-1330`) can race a loop tick and briefly admit N+1 efforts
   for one owner. That's accepted. The branch hold (ADR 0068) is the safety
   rule, the Cap is only a throughput/cost budget, and the next tick re-reads a
   true count. The ponytail comment is reworded for Cap N.

## Considered and rejected

- **A compare-and-swap on the Chart body, or moving Decisions-so-far into
  append-only comments.** The writers are already exclusive under ADR 0068.
- **Re-reading for `paused:manual` before a loop pause.** It's still
  check-then-write, with a narrower window. A flock around pause/unpause is a
  lock where a commutative write does the job.
- **An atomic claim-under-cap (a `claim.lock` flock around count+claim), or
  running TUI run-now through the loop.** An overshoot of one breaks no
  correctness invariant, and the charting policy adds locks only where the
  existing primitive can't hold.

## Consequences

- **Review vs Build re-check.** Review runs at the repo root with no branch
  hold, but the bead graph keeps it apart from Builds. Franky spawns the Review
  only when the Checkpoint has zero open members, and its critiques are Drafts
  published at the Review's close. So no Build or critique is open while a
  Review runs, and ADR 0068's claim holds: Franky's last-Build Review spawn and
  the merged-pivot Draft (`bd/pivot.go:88-90`) stay unreachable. Their ponytail
  comments name graph ordering and the branch hold as their serializers.
- `PausedCause` and the `set-state` dimension go away. Callers check for "any
  `paused:*`" and for one named cause.
  A pause records no reason, actor or time. The TUI pause line shows causes
  only (ADR 0057 amendment).
- `bd create --graph` under a parent another process just closed still
  succeeds (research §4). It is serialized only where the parent's claim or
  branch hold covers it, and a site that can't name one is listed as a defect.
- A `--graph` replay after `ErrCommitIndeterminate` duplicates nodes. Callers
  don't blindly retry a non-zero `--graph`.
