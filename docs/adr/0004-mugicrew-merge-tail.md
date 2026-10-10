# ADR 0004: mugicrew merge tail — Jinbe lands the Checkpoint

Status: Accepted
Date: 2026-09-19

Amends: ADR 0002 (adds the `conflict` kind — 11 kinds become 12).

## Context

ADR 0003 gave Jinbe his trigger — a **drained Checkpoint** (open, all children
closed) — but deferred the merge internals: what the land actually
does, whether merge/conflict work produces beads, and what flips the Checkpoint
to `merged`. Jinbe's handler must be idempotent under retry (no double-land),
per ADR 0003 item 7.

## Decision

**Jinbe lands a drained, approved Checkpoint by rebasing its branch onto the
current base and squashing to a single commit. A conflict is a bead; a clean
land closes the Checkpoint.**

1. **Merge strategy is always rebase + squash.** One commit onto the base, every
   time. No merge commits, no other strategy.

2. **`merged` ≡ the Checkpoint bead is closed.** Jinbe's successful land closes
   the Checkpoint, and nothing else ever closes it. The land *is* the merge; the
   derived-state waterfall (ADR 0002) is unchanged.

3. **Auto-land.** The closed Approval *is* the human authorization; once the
   Checkpoint drains, Jinbe lands with no second gate. A "hold the train" need is
   served by ADR 0003's per-crewmate cap (config), not a new bead.

4. **A conflict is a bead: `kind:conflict`** *(this amends ADR 0002)*. A leaf,
   **owned by Franky**, created by Jinbe (creator ≠ owner, already normal). On a
   rebase conflict Jinbe **aborts the rebase** and creates the `conflict` bead as
   a **member of the Checkpoint** (so the Checkpoint gains an open child, is no
   longer drained, and Jinbe won't retry). The bead **carries the target base
   commit hash** to rebase onto. It is named for the *problem*, not the action —
   both Franky and Jinbe rebase, so the kind is not `rebase`.

   > **Amended by ADR 0011:** this `conflict` kind stays Franky-owned and
   > Checkpoint-only. The *Chart*-land rebase conflict is a separate Nami-owned
   > kind, `chart-conflict` (owner-from-kind forbids co-ownership), and it does
   > **not** trigger §6's re-review — a Chart has no review gate.

5. **A clean rebase lands directly; only conflicts re-enter the loop.** Because
   merges serialize (ADR 0003 cap, default 1), the base often moves before Jinbe
   reaches a Checkpoint. A clean rebase onto the moved base produces a new
   squashed commit that nobody re-reviewed — it lands anyway. *(Known ceiling: a
   clean textual rebase can still be a semantic conflict. A CI gate that drops a
   clean-but-broken rebase back into the loop is future fog, not decided here.)*

6. **Conflict resolution forces a full re-review.** Franky closing the `conflict`
   bead **spawns a fresh Review** (ADR 0003 item 6, succession-firing) on the
   rebased commit → clean → new Approval → the **old Approval is obsoleted**
   (`supersedes`) by the new one, kept as lineage → Checkpoint drains again →
   Jinbe retries onto the now-current base. Manual conflict resolution changes
   the landed diff, so the blessed-commit invariant demands a new
   review.

7. **Idempotency is git-native, no stored flag.** A rebase moves the branch ref
   to the squashed commit `S`, and the fast-forward makes `base == branch == S`,
   so "already landed" is just "branch tip reachable from base." Jinbe's handler:
   - Checkpoint already closed → no-op.
   - Branch tip reachable from base (or branch already deleted) → already landed
     → close the Checkpoint.
   - Else rebase onto the current base tip: clean → fast-forward base, close the
     Checkpoint, delete the branch; conflict → create the `conflict` bead, leave
     the Checkpoint open.

   Re-running the rebase after a crash is a no-op, so no duplicate commit. Git is
   the source of truth, consistent with ADR 0002's "no stored state."

8. **The squash commit carries a `Checkpoint: <id>` trailer** linking the landed
   commit back to its Checkpoint bead. Idempotency does not need it (item 7);
   it is for audit and lineage.

## Consequences

- **The tail ends at `landed`.** Jinbe deletes the branch as a git side effect
  (not a bead). Post-land follow-ups — new Signals from the merged work,
  changelogs, notifications — are out of scope (downstream of the terminal
  state; Signal-intake is separate fog).
- **12 kinds.** `conflict` joins the taxonomy; Franky now owns `build`,
  `critique`, and `conflict`.
- **The merge train is free.** ADR 0003's cap-1 serializes Jinbe, so Checkpoints
  land one at a time; a landing that moves the base makes later Approvals stale,
  respawned by the item-6 machinery.
- **A CI gate is the named upgrade** for item 5's semantic-conflict ceiling.

## Alternatives considered

- **No conflict bead — raise a Critique instead.** Rejected: a merge conflict is
  a distinct problem with a distinct payload (the base hash to rebase onto) and a
  distinct resolver action (rebase, not fix-a-finding); overloading `critique`
  would blur both.
- **Skip re-review after conflict, hand straight back to Jinbe.** Rejected:
  conflict resolution edits the landed diff, so an un-reviewed commit would land.
- **Re-review every base movement (not just conflicts).** Rejected: too slow down
  the train; a clean rebase is trusted, with CI as the future backstop.
- **Merge commits / fast-forward without squash.** Rejected: rebase+squash keeps
  the trunk one-commit-per-Checkpoint and makes the git-native idempotency check
  (item 7) exact.
- **Stored "landed" flag for idempotency.** Rejected: git already knows; a stored
  flag would drift, against ADR 0002.
