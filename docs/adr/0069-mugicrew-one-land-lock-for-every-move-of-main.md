# ADR 0069: mugicrew one land lock for every move of main: a flock serializes each whole land, a failed land restores its branch, and failures are typed as conflict, ADR clash, dirty main, or transient

Status: Accepted
Date: 2026-10-05

## Context

Under ADR 0068 each container branch has one effort, so container branches
race only through `Land` itself. Main still has many writers, even at Cap 1:
Nami's Chart land, Jinbe's Checkpoint land, Spec anchors, Rollback's `Revert`,
`Reapply`, and the same operations driven from the CLI or TUI.

- `ffMainline` moves main with `merge --ff-only` whenever main is checked out
  (always, in this repo). That writes the main index and tree *before* its
  HEAD compare-and-swap. A lost race (`cannot lock ref 'HEAD'`) leaves the main
  index staging a reverse diff of the winner (48/300 trials). The dirt persists
  and blocks later lands on those paths.
- A land that fails at the final ff has already squashed its branch. For a
  Checkpoint the Approval's `Commit:` pin no longer equals the tip, so no gate
  ever picks it up again: it stalls silently until a human re-reviews.
- Callers can't tell a lost race (retry now) from a dirty main (needs a human).
  Exit codes overlap: 1 is both index.lock and dirt, 128 is both a clean CAS
  refusal and the dirtying HEAD-CAS loss. `git/` returns them all untyped.
- The ADR-number clash check reads main once, then `Land` reads it again. The
  window between is open on every attempt, and Checkpoint lands are not
  checked at all.

The Chart's default is detect-and-retry on existing serializers. The research
shows the existing primitive (`merge --ff-only`'s CAS) protects the ref but not
the index or tree, which is the condition under which the Chart allows a lock.

Decided in grilling.

## Decision

1. **One land lock.** A blocking flock on `<git-common-dir>/mugicrew/land.lock`
   (beside `loop.lock`) is held across the *whole* land operation: read base,
   the ADR-number clash check, rebase, squash, ff, and tree sync. It wraps
   `Land`, `Anchor`/`AnchorIfAbsent` (its `LandedBase` check included),
   `Revert` and `Reapply`, for every caller: loop, CLI and TUI. Under it, two
   mugicrew moves of main never interleave, so mugicrew has no lost races of
   its own. The update-ref and `--ff-only` CAS stay as a backstop against git
   used outside mugicrew.
2. **Lock wait times out.** Acquiring the lock gives up after 60s with an
   untyped (transient) error, so a hung git hook can't wedge dispatch forever.
3. **A failed land restores its branch.** `Land` records the branch's pre-land
   tip. On any failure after the rebase starts, it CAS-restores the branch to
   that tip (`update-ref <branch> <pre-tip> <current>`). The Checkpoint
   approval gate (`ApprovedAtHead`, `CheckpointReady`) stays strict: a pin
   matches the restored tip, and the next tick retries. ADR 0068 makes the
   landing effort the branch's only writer, so the restore races no one.
4. **The ADR-number clash check moves into the locked land, for every kind.**
   It runs against the exact base being landed on, for Charts and Checkpoints
   alike, and returns `*ADRClash`. Nami files its chart-conflict
   (adr-number-clash variant) as today; Jinbe files its Franky-owned conflict
   member, as for a rebase conflict.
5. **Land failures are a closed set of types.** `*RebaseConflict` (exists),
   `*ADRClash`, and `*MainDirty`. Any other error is transient: the effort
   fails, the claim is released, the next tick retries. No caller parses git
   exit codes.
6. **`*MainDirty` is a pre-check, strict.** Under the lock, before the branch
   is touched, the main worktree (if one has main checked out) must have no
   tracked changes and no non-ignored untracked files. Otherwise the land
   returns `*MainDirty`, logged on `land.failed`. No bead is filed: it is one
   condition blocking every land, and it stays until a human cleans main.

## Considered and rejected

- **Ref-first: always `update-ref` CAS, then sync the main worktree**
  (`read-tree -m -u` / `reset --keep`). No lock, but the tree sync still races
  a concurrent ff and needs its own repair logic.
- **Lock only the ff step.** Shorter hold, but lost races still happen at the
  CAS, so the retry and Checkpoint-stall paths stay, and the clash check still
  reads a different base than the land.
- **Let the approval gate accept a pure squash of the approved tip.** It
  loosens an invariant several readers rely on, to paper over a branch Land
  shouldn't have left mutated.
- **`*MainDirty` only on paths the land touches** (git's own "would be
  overwritten"). More permissive, but needs a path diff against status; dirt
  in main is already against the worktree rule (never commit in, or dirty, the shared `main`
  checkout; work in a linked worktree), so stopping on any
  dirt is the simpler rule.
- **File a HITL bead per `*MainDirty`.** One sticky condition would spawn a
  bead on every container trying to land.
- **Classify existing errors by exit code.** The codes overlap (§Context).

## Consequences

- Lands, anchors, reverts and reapplies run one at a time per repo. They take
  seconds, so the cost is small at any realistic Cap.
- Reverse-diff dirt in main can no longer come from mugicrew racing itself.
- A Checkpoint never stalls on a failed land; it retries until it lands or hits
  a typed failure that files a bead (conflict, clash) or needs a human
  (`*MainDirty`).
- The `mergeFFOnlyArgs` comment's "`--ff-only` keeps the CAS safety" claim
  holds only for the ref; the lock, not the CAS, is what keeps the tree clean.
- The clash check leaves Nami for `git/` (or a land wrapper under the lock), so
  both reconcilers share it.

> **Amended by ADR 0147:** Checkpoint and Chart lands now have a bead (a
> Landing) and run behind a land-strategy seam; the other moves of main stay
> bead-less.

## Amendment: every move of main runs the `*MainDirty` pre-check

§6 framed the pre-check for `Land` only. It is one guard in the land-lock
wrapper, run by every op that moves main: `Land`, `Anchor`/`AnchorIfAbsent`
and `Revert`. `Reapply` only moves its successor branch, so it never runs it.

- **Revert** fast-forwards real paths through the main worktree, just as
  `Land` does, so it needs the check for the same reason.
- **Anchor**'s empty commit changes no paths, so dirt can't hurt it. It runs
  the check anyway: one rule per resource, and a dirty main already blocks
  the first build land of any fast-laned Spec, so an exemption buys nothing.
- **Write leg only.** The check runs after the idempotency read. A trailer
  already on main returns its sha even when main is dirty, because nothing
  moves.
- **Callers.** The loop (Nami fast lane, Jinbe Rollback) files no bead. It
  records the typed error on its existing failure event (`triage.failed`,
  the revert record), and the next tick retries. The CLI (Zoro pivot confirm,
  `reconcile`) exits non-zero with "clean main, then re-run". A TUI action
  shows the same message in its status line. There is no new event name.

> **Amended by ADR 0166:** "one at a time per repo" is now "one land lock per Workspace", covering lands in declared child repos too.
