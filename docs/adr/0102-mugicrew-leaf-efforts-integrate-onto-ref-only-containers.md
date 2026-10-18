# ADR 0102: mugicrew leaf efforts integrate onto ref-only containers: each works on `effort/<bead-id>` and `mugicrew integrate` squash-CASes it onto its container under the land lock before the bead closes

Status: Accepted
Date: 2026-10-08
Supersedes: ADR 0068 §1 and §4. Amends ADR 0069 §1 and §3 (lock scope, restore).

## Context

The Per-effort branches Chart wants sibling efforts in one container
to run at once under the AFK pool. ADR 0068 rejected this because of loss paths
C3 (a CAS strands a tree that has the branch checked out) and C4 (Land drops a
commit CASed in mid-land), plus two bd races its branch hold kept unreachable.

Evidence:
- Research on container-branch checkout and move sites.
- Research on sibling-effort bead-graph races.
- A prototype (`refs/mugicrew/prototypes/<id>`): 40 rounds of 6
  sibling integrates racing one container land in a real scratch repo. With
  §3, §7 and §8 as written: 0 commits lost or reverted, container never
  checked out, CAS losers retried cleanly; late integrates refused, conflicts
  and spent retries kept their branch. Without the lock, 25 integrates CASed
  onto the container after it landed. The CAS alone is not enough. The
  integrate's already-landed check and its CAS must share the lock with
  Land's pin check.

Triage settled several points. Containers become ref-only:
no worktree checks one out. Only leaves get effort branches. Integrate runs
before close. An unresolvable conflict writes `paused:integrate-conflict`.
Commits ahead decide whether to integrate or delete the branch. This ADR
records the integrate contract.

Decided in grilling.

## Decision

1. **Effort branch `effort/<bead-id>`.** A bead has at most one live effort or
   detached session, so the bead id is unique enough. A branch that already
   exists is reused (roll forward). Otherwise it is cut from the container tip.
   Any session that commits uses one, own-container sessions included (charting,
   speccing: `effort/<container-id>`). "Container-level steps work as today"
   now means only that they are scheduled serially.

   > **Amended by ADR 0264:** the cut writes `refs/mugicrew/cut/<id>`, and
   > squash integrate rebases `--onto <tip> <cut>` (§3), so a sibling cut before
   > a replace rewrite replays only its own commits.
2. **Squash commit.** Integrate puts one commit on the container: subject
   `integrate <bead-id>`, trailer `TrailerFor(kind, id)`. The trailer is the
   idempotency key. If it is already on the container, the effort is already
   integrated, so a crash between the CAS and the close never integrates twice.
3. **`mugicrew integrate`**, the agent's last step before `bd close`. It takes
   no arguments: it reads the bead from the cwd's `effort/<id>` branch and the
   container from `route`. The steps:
   1. Outside the lock, rebase the effort branch onto the container tip, in
      the session's own tree.
   2. Under the land lock, if the tip is unchanged, `commit-tree` the squash
      with the tip as parent and CAS `update-ref` the container. This is
      milliseconds of lock.
   3. If the tip moved, release the lock and go back to step 1, at most 3
      times.

   Under the lock it refuses in two cases: any worktree has the container
   checked out (C3 becomes loud), or the container's land trailer is already
   on main.

   Exit codes: 0 integrated or nothing ahead, 2 rebase conflict (left stopped
   in the tree) or held, 3 the container already landed, 1 transient.
   Deleting the branch and tree is the caller's job.
4. **Replace mode for history-rewriting leaves.** This covers chart-conflict
   and Franky Conflict. Their effort branch is cut at the container's pinned
   Tip, and integrate is a replace-CAS (`update-ref <container> <rewritten>
   <pinned>`) with no squash. If the container moved past the pin, the verb
   first replays `pinned..tip` onto the effort branch (an interactive rebase
   of explicit picks that keeps the effort branch checked out, closing with a
   `Replayed:` trailer), with conflicts handled as in §5. The bead's kind
   selects this mode.

   > **Amended by ADR 0262:** replace mode now serves the Franky Conflict and
   > the Chart-conflict (each with the `rebase-conflict`, `number-clash` and
   > human-filed `requested` variants) and the Base-pinned Critique (ADR 0152).

   > **Amended by ADR 0264:** a replace integrate refused as "rewritten off
   > its pinned tip" is not transient. It closes moot if the container already
   > descends from the bead's `Base:`, else re-pins `Tip:` and re-dispatches on a
   > fresh cut, and pauses if refused again. Squash integrate with no
   > recoverable cut point after a rewrite refuses non-transiently.
5. **On conflict, the agent resolves.** It resolves in its tree, runs
   `git rebase --continue`, and re-runs the verb. If it judges the conflict
   unresolvable (in HITL, after asking the human), it runs `git rebase
   --abort`, notes why on its bead, leaves the bead open, and exits. There is
   no give-up verb.
6. **The Go backstop is the only writer of `paused:integrate-conflict`.** It
   runs at the AFK tail after `reconcileInWorktree`, and at HITL clean exit
   before `removeWorktree`. It runs the same verb with no agent:
   - Successful effort, nothing ahead or integrated: delete the effort branch
     and tree.
   - Conflict (rebase, held): `rebase --abort`, write the pause, reopen the
     bead if it was already closed, and keep the branch and tree for the human.
   - Landed (exit 3): the commits can never integrate, so the backstop files
     them as an offshoot Signal for intake, closes the bead `obsolete:
     container landed; carried by <signal>`, removes the tree and keeps
     `effort/<id>`. If the Signal or the close fails it falls back to the
     conflict pause.
   - Transient: keep the branch, remove the tree. The next tick retries.
   - Failed effort: it does not integrate. It keeps the branch for roll
     forward and removes the tree.
7. **One lock.** ADR 0069's `land.lock` guards every move of main and of every
   container ref. A land moves main while it pins its container, so per-ref
   locks would need nesting, and the lock is non-nestable. An integrate queued
   behind a land re-reads the tip afterwards. If that container has just
   landed, §3 refuses and the backstop pauses the leaf.
8. **Land never writes the container.** It rebases in a detached tree at the
   container tip, squashes onto main and fast-forwards main. Under the lock it
   pins the container's pre-land tip and refuses if the container has moved
   off it. Land no longer writes the container, so C4 has nothing to drop, and
   `restoreBranch` goes away. Approval `Commit:` pins still match.
9. **Ordering against close.** A child that still has an `effort/<child>` ref
   counts as open in every drained or last-open-sibling check: Nami's Chart
   drain, Franky's Review spawn and Jinbe. An agent that closes before it
   integrates, or skips integrate, then cannot let its container land or
   review without its commits. Together with the reopen in §6, a conflicted
   leaf is open in every gate.
10. **HITL holds.** An exited-for-resume session holds only its tree and
    `effort/<id>`, so siblings keep running. Resume re-enters the tree, and its
    clean exit integrates onto whatever the container has become. Drop removes
    the tree (uncommitted work is lost, as today). It keeps `effort/<id>` if
    the branch has commits ahead, so the next launch reuses it. An orphaned
    worktree is treated the same way. `surgery end` runs integrate before it
    counts commits ahead.
11. **Prototype amends settled 5.** A prototype integrates its effort branch
    (ADR, GLOSSARY and Chart notes) like any leaf. The throwaway is built only
    in its separate detached tree and goes only to
    `refs/mugicrew/prototypes/<chart-id>`, never onto the effort branch.

## Considered and rejected

- **`effort/<bead-id>/<effort-id>`, one branch per effort.** Every retry would
  start clean, losing roll forward, and stale branches would pile up.
- **Fast-forward the rebased commits without squashing.** It gives no trailer
  to key idempotency on, and the container's land squashes anyway.
- **A `mugicrew integrate --give-up` verb.** It would make two writers of the
  pause. The backstop hits the same conflict mechanically.
- **One lock per ref.** It needs nested acquisition and an ordering rule, to
  save milliseconds of contention.
- **Always discarding the prototype's effort branch** (settled 5 as first
  stated). It would silently lose the prototype's ADR and GLOSSARY commits.
- **Go closes leaf beads after integrate.** It is robust, but it changes the
  close step in every leaf prompt and handler. The ref check in §9 gives the
  same guarantee at the gates.

## Consequences

- Sibling leaves in one container run at once. Cap N now helps a single wide
  Checkpoint.
- Only `git.Busy` on an effort branch means a stale tree. `selectDispatch`'s
  held-set skip remains only for container-level kinds.
- An effort branch outlives a dropped, failed or orphaned session, so cleanup
  of abandoned ones is new work (Chart fog).
- How a human lifts `paused:integrate-conflict`, and the TUI and event
  surface: ADR 0104.
