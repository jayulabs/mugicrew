# ADR 0393: mugicrew Go clears a Hold Errand through a paced typed step keyed by its key line and blocked kind

Status: Accepted
Date: 2026-10-28
Amends: ADR 0185 (open-merge-request-outlives…) §8, the Forge read budget; the Errand Operations line (only the guide closes an Errand).

## Context

The Chart "Errand and Hold Errand closure outcomes: named reasons and a door for
a cleared or moot hold" settled that Go closes a Hold Errand whose fact a machine
can read, through a typed loop step keyed by the hold's key line, never while
its guide session is live. The research note
`docs/research/hold-errand-clearing-reads-and-moot-observability.md` found that
no running reconcile reads the deciding fact for every hold (none at all under a
held Chart), that some key lines are shared by several bodies, and that a
container landed under an open hold cannot close and its Landing goes
`paused:failing`. The grilling "Which Hold Errand keys Go clears, on what
cadence, and how the reads share the Forge budget" settled the rest.

## Decision

1. **Which holds.** Go clears the closed-MR (`Closed MR:`), trailerless land and
   revert MR (`Merged MR:`), Landing infra pipeline and revert-MR pipeline
   (`Pipeline:`) holds. The foreign land (`Foreign:`) and every Chart
   `Pipeline:` hold, branch or merged-results, stay closed only by Brook's guide.
2. **The variant is the key line plus the blocked bead's kind**, never body
   prose or Check text. Under a Checkpoint a `Pipeline:` hold is always infra
   (a real failure files a Critique); under a Rollback it reads every
   alternative the infra variant allows, which covers the real one.
3. **Clearing reads.** `Closed MR:` clears on `GetMR` `opened` or the
   container's trailer landed; `Merged MR:` on the trailer (a Rollback's
   `Reverts-Checkpoint:`) landed; a Checkpoint `Pipeline:` on `GetPipeline(id)`
   `success` or the trailer landed; a Rollback `Pipeline:` on the pipeline
   `success`, the trailer landed, or the revert MR `merged`. A landed trailer is
   always `cleared: <key line>`, whatever the hold's own route. `obsolete:` is
   kept for a fact that went away: `obsolete: revert merge request closed` on
   `GetMR` `closed`, and `obsolete: <container> closed` when the blocked bead is
   closed (supersede, forced hand close).
4. **Landed reads fetch like Jinbe**: `git.Repo.FetchMainline` then
   `LandedBase`, so the Errand closes only when Jinbe's next tick agrees.
5. **Cadence.** One typed loop step per open Hold Errand, under every blocked
   kind including a Chart, paced by `landing_poll_interval` since its last
   check, as a Landing is.
6. **Budget.** The step pays its own reads, at most one Forge REST read and one
   fetch per hold per `landing_poll_interval`. ADR 0185 §8's budget gains that
   line. It does not dedupe against a readmitted Landing's or Rollback's reads.
7. **Landing under an open hold.** The step closes the Errand first; the
   container's or Rollback's next reconcile then closes it normally, with no
   `--force`. The trailerless and foreign hold filers stop returning an error
   when their hold is already filed, so a held Landing or Rollback stays pending
   and never reaches `paused:failing`; the roll-forward needs no human `resume`.

## Considered Options

- **Parse the lead prose or add a `Hold:` fact line** to tell bodies apart:
  prose is amendable by Brook, and a new line leaves every existing hold
  guide-only. The blocked kind already decides.
- **Name a landed trailer `obsolete: <container> landed`**: the Check passed, so
  it is a clear; a bd-refused container close never happens while held anyway.
- **Ride the Landing, MR Watch and `revertMRArms` reads**: nothing reads under a
  held Chart, nor the closed-MR fact, so a separate path is needed anyway; one
  code path with a stated cost beats a shared cache.
- **`--force` the landed close over an open hold**: silently orphans a live hold.

## Consequences

- The Rollback's existing every-tick reads stay outside the budget as written;
  that is its own Signal.
- How Go reads "guide session live" is settled by ADR 0396 (the-hold-clearing-step-reads-guide-liveness-from-the-tmux-session-table…).
