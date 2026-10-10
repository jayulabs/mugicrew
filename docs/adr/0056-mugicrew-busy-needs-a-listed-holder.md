# ADR 0056: mugicrew busy needs a listed holder — an exit-128 cut is busy only when `git worktree list` names a holder; otherwise it is a failed effort

Status: Accepted
Date: 2026-10-04

## Context

`git.IsBusy` reads every exit 128 from `git worktree add` as busy. git uses
exit 128 for every fatal, including an invalid ref and repo, permission and
disk errors. So a real cut failure gets skipped silently forever (one cause,
the empty-parent `chart/`, has since been fixed in route). ADR 0054 provisionally
made `holder=""` a foreign hold. That conflicts with ADR 0055: the auto-clear
check reads "no holder listed" as "branch free", so a real failure would cycle
held ×3 → paused → resumed forever.

Evidence: porcelain `git worktree list` names linked, main, locked and prunable
holders. Its only blind spot is a holder stopped mid-rebase or mid-bisect. The
stderr is the only thing that tells those cases apart, and its wording changed
at git 2.43 and is translated. Decided in grilling.

## Decision

1. **Busy means exit 128 and a listed holder.** After an exit-128 cut,
   `git.WorktreeOn(branch)` must name a holder. Then ADR 0054 classifies it
   as own (`effort.busy`) or foreign (`effort.held`). The test lives in `git`,
   so the loop's `worktreeRun` and the TUI launch share it.
2. **No holder listed means a failed cut.** The loop starts the effort and
   fails it: `effort.failed` at Error, carrying git's stderr. The TUI shows
   `launch: <git error>` and not "busy". No new event.
3. **Never parse stderr.** A foreign holder stopped mid-rebase or mid-bisect
   is filed as a failed cut. git's own message, quoted in the error, still
   names the holder for the human.
4. **Not bounded here.** A failed cut releases its claim (ADR 0034) and retries
   on every tick, like any failing effort. A failed cut resets the
   consecutive `effort.held` count (ADR 0055 §4). Backoff for all failed
   efforts belongs to the retry-storm protection Signal.

## Considered and rejected

- **Keep `holder=""` as a foreign hold** (ADR 0054 as drafted). It needs an
  exception to the pause-clear rule, and it files a broken ref as "someone
  holds your branch".
- **A distinct `effort.cutfailed` event.** `effort.failed` already means
  "this bead's effort broke" and carries the error.
- **Stderr fallback to keep mid-rebase/bisect holders as busy.** Fragile
  across git versions and locales, for a rare, human-made case.
- **Pause after N failed cuts (`paused:cut-failed`).** A backoff only for cuts
  would be the odd one out among failed efforts. The general backoff belongs
  in that Signal.

## Consequences

- `IsBusy`'s ponytail ceiling ("any worktree-add fatal counts as busy") goes
  away. A real cut failure is loud: it logs at Error on every poll until it
  is fixed, or until the retry-storm Signal adds backoff.
- Every exit-128 cut costs one porcelain list (about 1.3 ms). That's already
  needed to name the holder (ADR 0054).
