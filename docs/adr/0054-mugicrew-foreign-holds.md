# ADR 0054: mugicrew foreign holds — a refused cut is "own" only if a live effort or alive session explains its holder; everything else is a foreign hold, logged at Warn as `effort.held`

Status: Accepted
Date: 2026-10-04

## Context

When the dispatch loop's worktree cut (`git worktree add <path> <branch>`) is
refused with exit 128, `git.IsBusy` reads it as busy. `Dispatch` unclaims the
bead and logs `effort.busy` at Debug, and the next tick tries again. Once, a
stray linked worktree held a Checkpoint's branch, and the loop flipped
the bead open↔in_progress 588 times with nothing above Debug. The
Foreign-held busy skips Spec wants that case surfaced and
suppressed, while mugicrew's own contention stays a silent retry.

After a refusal, `git.WorktreeOn(branch)` (porcelain worktree list)
names the holder. It finds linked, main, locked and prunable holders. It returns
`""` for a holder stopped mid-rebase or mid-bisect, for a non-busy fatal, and
when the holder released in between. Path shape is not enough to tell own from
foreign: HITL launches and loop efforts both cut through `git.NewWorktree`, so
orphaned worktrees and *exited* HITL sessions have the `mugicrew-wt-*/wt` shape
too (`git.IsTempWorktree`), and both hold their branch indefinitely. Decided in
grilling.

## Decision

1. **Own contention.** A holder counts as own only when it has the temp shape
   (`git.IsTempWorktree`) **and** either an in-flight effort or an *alive*
   detached session (session list `HolderBead`) accounts for it. Own contention
   stays `effort.busy` at Debug `{branch}` (ADR 0029, unchanged) and does not
   count toward suppression.
2. **Foreign hold.** Every other refused cut is a foreign hold:
   - a non-mugicrew linked worktree,
   - the main worktree checked out to the branch,
   - a prunable holder (directory gone, admin entry never pruned), which gets
     `prunable=true`,
   - an orphaned worktree (temp shape, nothing behind it),
   - an *exited* detached session's worktree. That is a human waiting to
     resume or drop, so in effect it is a hold,
   - **no holder found** (`holder=""`). Not provably ours, so it surfaces.
     Whether a non-busy exit-128 stops being read as busy at all is up to
     a later decision, which may split this case off.
     *Superseded: no holder found is not a hold at all. It is a
     failed cut, `effort.failed`; see ADR 0056.*
3. **Event.** A foreign hold logs a new event, `effort.held` at Warn
   `{branch, holder, prunable}`. `effort.busy` keeps its meaning (own
   contention) and its level. Each name maps to one level, so the Logs tab
   filters on the name. Like `effort.busy`, it records no `effort.started`.
4. **Suppression events are reserved:** `effort.suppressed` at Warn
   `{branch, holder, skips}` when the loop stops re-claiming the bead, and
   `effort.unsuppressed` at Info `{branch}` when it resumes. The suppression
   grilling decides when they fire and may rename them.
   *Amended: renamed `effort.paused` / `effort.resumed`; see ADR 0055.*
5. **Never a fix.** Classifying a hold never removes, prunes or force-checks-out
   anything (`worktree add -f` is never a remedy). The human's tree and
   orphan cleanup belong to them and to the Chopper Signal.

## Considered and rejected

- **Shape only** (`IsTempWorktree` means own). An orphan or exited session
  would spin silently at Debug forever, which is the stray-worktree bug with a
  mugicrew-made tree.
- **Split `effort.busy` by level, adding a `holder` attr.** That gives one
  event two meanings and two levels, so filtering by name stops being enough.
- **Parse git's refusal stderr** for the holder. The wording changed at git
  2.43 and is translated (research §3).

## Consequences

- The classification needs the in-flight effort set and the session list
  where the refusal is handled. The loop has the effort set. Reading the
  session list costs one multiplexer query per refusal, and only on that path.
- A `holder=""` from a release race reads as one stray foreign skip. The next
  tick retries the cut, and N-consecutive suppression absorbs it.
- ADR 0029's catalogue gains `effort.held`, `effort.suppressed` and
  `effort.unsuppressed`.
