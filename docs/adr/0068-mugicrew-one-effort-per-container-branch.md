# ADR 0068: mugicrew one effort per container branch: at any Cap, git's checkout lock keeps each container branch to one effort, and Cap N buys parallelism only across containers

Status: Accepted; §1 and §4 superseded by ADR 0102
Date: 2026-10-05

## Context

The Concurrency-safe shared state Chart targets a per-owner Cap
N > 1. Several efforts may then want members of the same container bead (two
Builds under one Checkpoint, two research leaves under one Chart), and they all
route to the same `checkpoint/X` or `chart/X` branch. Today every effort cuts a
worktree on that branch, and git's refusal of a second checkout is the only
interlock (`git/git.go:178`, ADR 0056).

The `git.go:178` upgrade path (a detached tree per effort, then a CAS integrate
onto the branch) loses commits in two ways. A CAS that moves a branch another
worktree has checked out strands that worktree, and its next commit reverts the
moved work (C3). `Land`'s rebase → `reset --soft` → commit drops a commit
another writer CASes onto the branch mid-land, and it still reports OK (C4).

The branch hold also hides two bd races. Franky's last-Build Review spawn can
spawn zero Reviews or two. A merged-into pivot Draft can be deleted by its
creator's failure tail (`bd/pivot.go:88-90`).

Decided in grilling.

## Decision

1. **One effort per container branch.** git's checkout lock is the rule and
   the guarantee, at any Cap. A per-owner Cap N > 1 gives parallelism only
   across different containers.
2. **Every checkout of a container branch goes through `git.NewWorktree`.**
   No session works inside another effort's tree, and none checks a container
   branch out at a path of its own. Two prompts must adopt this:
   - The chart-conflict prompt's "work there" in a live holder's tree
     (`nami/prompts/chart-conflict.md:14-20`).
   - Sanji's fixed-path prototype checkout (`sanji/prompts/prototype.md:40`).
3. **HITL sessions follow the same rule.** A grilling or prototype session
   holds its container branch the same way an AFK effort does, for as long as
   it is alive or exited-for-resume. ADRs 0054 and 0055 already classify and
   surface that hold.
4. **Selection skips containers the loop already holds.** `selectDispatch`
   skips a candidate whose routed branch an effort *this loop* has in flight
   already holds, and falls through to the next candidate. A same-container
   pick then doesn't waste a Cap slot or cost a claim/unclaim pair per tick.
   Holds by the TUI, a human or a foreign tree still go through cut → busy or
   held.
5. **The loop passes its in-flight reader to `ownContention`**
   (`loop/loop.go:453-456`, `:510-511`). A live TUI run-now holder is then
   own contention (`effort.busy`), not a foreign hold that pauses the bead.
   The own-vs-foreign definition of ADR 0054 §1 is unchanged, and so is
   ADR 0056.

## Considered and rejected

- **A private branch or detached tree per effort, integrated onto the
  container branch.** It needs detached-only trees, a `Land` that holds the
  branch exclusively or CASes its squash from the exact tip it rebased,
  rebase-and-retry on integrate, and atomic fixes for the Review spawn and
  merged pivots. That is a feature (parallel members inside one container),
  not a safety rule, and nobody needs that throughput yet. It is out of scope
  for this Chart.
- **Skip using bd's in_progress list** (it also covers TUI run-now). A
  stranded claim (crash; there's no lease self-heal before the Chopper Signal)
  would make its siblings get skipped silently forever, where today the
  orphaned tree surfaces as `effort.held`.
- **Refuse only at cut** (today). Correct, but at Cap N it wastes slots on
  same-container picks and churns claims every tick.
- **Not holding the branch for a HITL session until it commits.** A grilling
  session writes ADRs and CONTEXT to the branch, so it is a writer.

## Consequences

- The `git.go:178` and `dispatch.go:244-249` cap-1 comments stay true at
  Cap N. Their "upgrade path" becomes the rejected alternative above.
- The Franky Review-spawn race and the merged-pivot marker stay unreachable,
  because sibling efforts on one container never overlap.
- Raising the Cap helps a backlog spread across several Charts or Checkpoints,
  not a single wide Checkpoint.
