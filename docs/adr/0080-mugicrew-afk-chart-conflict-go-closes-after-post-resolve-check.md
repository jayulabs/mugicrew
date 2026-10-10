# ADR 0080: mugicrew AFK chart-conflict: the agent commits, Go closes only after a post-resolve check; a failed check restores the branch and fails the effort

Status: Accepted
Date: 2026-10-05

Amends ADR 0011 §3 and ADR 0030 §3 (chart-conflict was HITL; the agent closed
its own bead). Builds on ADR 0019 (hybrid AFK), ADR 0078 (failure pause) and
ADR 0079 (escalation). Decided in grilling.

## Context

An unattended writer on `chart/<parent>` has no human backstop and no review
gate after it: the next Chart land re-checks only ADR numbers and the textual
rebase, so mainline content the agent dropped while resolving lands silently.
An AFK arm that returns nil without closing is unclaimed and re-dispatched every
tick. Uncommitted work and rebase state die with worktree teardown; only
committed branch state persists between runs.

## Decision

1. **Go closes (hybrid, ADR 0019 §3).** The agent resolves and commits on the
   Chart branch and never runs `bd close`. It ends with exactly one outcome
   line, matched on the last non-empty line of its output (like research):
   - `Resolved: <summary>` → Go runs the post-resolve check (§3), then closes
     the bead (Open()-guarded) with the summary as its reason;
   - `Escalate: <title>` + payload → Go files the grilling bead as ADR 0079
     §4 says, running **no** check.
2. **Any other outcome is a failed effort.** Go returns an error, so a soft
   failure or a crash counts toward ADR 0078's `paused:failing` instead of
   re-dispatching forever.
3. **Post-resolve check**, run in the worktree against the **carried base**
   (if main has moved since, `Land` re-checks and files a fresh conflict):
   1. the tree is clean, no rebase is in progress, and base is an ancestor of
      HEAD;
   2. no ADR-number clash against base (the same `adrNumberClashes` guard
      `Land` runs);
   3. **mainline-only paths are untouched:** every path outside
      `diff(merge-base(base, Tip), Tip)` is identical to base in HEAD, where
      `Tip` is the Chart tip pinned at filing (§5). This also catches a
      reference sweep that strayed outside its scope (ADR 0030 §5). There are
      two exemptions: (a) an added ADR whose slug (`NNNN-` stripped) matches a
      Chart ADR in that diff: a renumber (ADR 0030 §2); (b) a path mainline
      renamed, between merge-base(base, Tip) and base, from a path in that
      diff (git's default rename detection), where the Chart's edits now
      belong. Because
      `Tip` is fixed, the final `Resolved:` run covers commits from every
      earlier run, escalated or crashed. A bead with no `Tip:` pin skips this
      assertion and logs a warning.
4. **A failed check restores and fails.** Go resets the branch to the tip it
   had when this session started, and returns an error naming the failed
   assertion and the paths. The next tick starts clean; three failures pause
   the bead for the human (ADR 0078). Commits from earlier escalated runs are
   kept, since they predate the session.
5. **Payload pins.** `chartConflictBody` writes `git.BasePin(base)` (`Base:`)
   and a new `Tip: <sha>` line (the Chart branch tip when the land failed).
   Nami's reader also accepts lowercase `base:` for beads filed before this.
6. **No Go fast path on re-dispatch.** Every dispatch runs a session. The
   prompt keeps Franky's roll-forward stanza: inspect first; if the branch is
   already resolved and every closed grilling blocker's answers are applied,
   commit nothing and end `Resolved: already resolved`. Go's check still gates
   the close.

## Considered and rejected

- **The agent closes, as before.** Nothing could stand between the resolve and
  the close.
- **Return nil on an unknown outcome (research's shape).** An unbounded retry
  storm now that the failure pause exists.
- **Keep bad commits on a failed check**, or **escalate it to grilling.** Roll-
  forward is poor at undoing commits, and a dropped mainline file is an agent
  bug, not a judgment call (ADR 0079 §1).
- **A Go "already resolved" fast path.** In an adr-number-clash the agent can
  finish rebase + renumber, commit, then escalate an ambiguous sweep line; the
  fast-path predicate would then hold and Go would close without applying the
  binding answer. Re-dispatch after a resolve is rare (a crash between check
  and close), so one cheap session beats a second guarded code path.
- **Check 3 against the session-start tip.** A bad commit from an earlier run
  would escape it.
- **Mainline lines kept in conflicted paths** (every line mainline added since
  the old merge-base survives in HEAD). Deferred: it is heuristic and trips on
  legitimate rephrasings and on binding answers that choose the Chart's side.

## Consequences

- An orphaned mid-rebase tree after loop death still fails every cut; it stays
  under the general no-self-heal ceiling, surfaced by the failure pause.
- An AFK chart-conflict shares Nami's one loop slot (`loop.Cap = 1`) with
  research and Chart drains.
- `ChecklistCommand` stays dead; only its comment is reworded.
