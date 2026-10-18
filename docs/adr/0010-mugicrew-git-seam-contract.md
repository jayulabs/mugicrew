# ADR 0010: mugicrew git seam contract — Chart land, the landed-base seam, and the `git/` package

Status: Accepted
Date: 2026-09-21

Amends: ADR 0009 (fixes the seam it left as fog — the git operation "merge on
defog" performs, and how the landed base is recorded and read).

## Context

ADR 0009 made container kinds branches: the Chart is a branch that "merges to
mainline when the Chart defogs," and the Checkpoint is cut "from that landed
base." But the *git operation itself* was unspecified, and it is load-bearing —
the merge mechanic **defines what "the landed base" commit is**, and the landed
base is the seam Zoro consumes to cut the Checkpoint branch. This ADR
pins that contract so the layer and everything downstream of it build
on a fixed seam. The four decisions interlock, so they were settled as one
interview.

## Decision

**1. Merge mechanic — rebase + squash, mirroring the Checkpoint land (ADR 0004).**
`reconcileChart`'s "merge on defog" rebases the Chart branch onto mainline,
squashes to a single commit carrying a `Chart: <chart-id>` trailer, and
fast-forwards mainline. Idempotency is **git-native, no stored flag** (ADR
0004 item 7): branch tip reachable from mainline ⇒ already landed. **The landed
base is that squash commit** (= mainline HEAD immediately after the land). We
accept losing the intermediate charting-commit boundaries on the trunk (the
artifact *content* survives) in exchange for one commit per Chart, an exact
landed base, and consistency with Jinbe's tail.

**2. Landed-base seam — git-native via the trailer; no bead field.** Zoro cuts
the Checkpoint on the *Spec*, which fires later, by which time mainline has moved
(merges serialize, ADR 0003 cap-1) — so "`rev-parse main` at cut time" is wrong;
it returns the latest trunk, not *this* Chart's landed base. Instead the `Chart:
<chart-id>` trailer **is** the record. Zoro walks `Spec —caused-by→ Chart` and
re-derives the SHA by trailer. Nothing is stored on a bead for a container's landed base (a direct Spec's anchor is a bead pin, ADR 0287) (the SHA is a derived,
immutable git fact — duplicating it into bd buys nothing and matches ADR 0004's
"git is the source of truth").

**3. Branch naming + mainline source.** Chart branches are `chart/<chart-id>`
(Checkpoints `checkpoint/<id>` by the same rule) — reconstructable from the bead
ID, never stored. **Mainline is a single package-level constant in `git/`
(default `"main"`), not current HEAD** — the checkout model is still open fog, so HEAD is an unreliable trunk pointer. The merge target and the
cut base read the same constant, so they cannot diverge.

**4. The `git/` package.** Mirrors `bd/` exactly: one private `run(...)`
shell-out choke point (footguns encapsulated), argv split into pure helpers so
they are checkable **without a real repo** (the `newSignalArgs` /
`successorCreateArgs` pattern). Minimal surface, and no more:

- `CutBranch(name, base string) (created bool, err error)` — idempotent
  **branch-if-absent** (mirrors `SpawnSuccessorIfAbsent`). `reconcileSignal`
  cuts `chart/<id>` from mainline; Zoro cuts `checkpoint/<id>` from the landed
  SHA. `base` is explicit at both call sites.
- `Land(branch, trailer string) (sha string, err error)` — the shared mechanic
  (decision 1). Called by `reconcileChart` (trailer `Chart: <id>`) **and** Jinbe
  (trailer `Checkpoint: <id>`). Returns the squash SHA, including on the
  idempotent no-op path, so a caller's roll-forward can re-derive it after a
  crash. A rebase conflict is surfaced as a **typed error carrying the target
  base SHA**; `git/` aborts the rebase and **never touches beads**.
- `LandedBase(trailer string) (sha string, err error)` — re-derives the landed
  commit by grepping the trailer (decision 2). Zoro's read path; also `Land`'s
  internal no-op derivation.

Two supporting exports round out the surface, deliberately and no further:

- `BranchName(kind, id string)` and `TrailerFor(kind, id string)` — the **pure
  naming helpers §3 mandates** be shared across packages (`reconcileChart` and
  Zoro both rebuild the same `chart/<id>` / `Chart: <id>` strings the seam greps).
  Pure and repo-free, so they carry no footgun; exporting them is what keeps the
  convention in one place.
- `AddWorktree(path, branch string)` / `RemoveWorktree(path string)` — the
  per-effort worktree add/teardown (ADR 0009). Shell-outs, so they belong **in
  this choke point**, not raw in the loop package; the loop's dispatch calls them
  and runs the effort with the worktree as cwd, keeping the three land/branch
  signatures dir-less.

> **Amended by ADR 0047/0049:** the surface grows by the Rollback's
> revert seam, through the same `run` choke point — `Revert(squash, trailer)`
> (one forward revert commit, idempotent on its trailer, needing git ≥ 2.40 for
> `merge-tree --merge-base=`), `Reapply` (the successor's seed), the typed
> `RevertConflict` (Base + conflicted paths, `RebaseConflict`'s twin), the trailer
> helper `RevertsTrailer`, and the pure body pin pair `ConflictPin` /
> `ParseConflict` (with `PathsPin`). Like `TrailerFor`, the pure helpers are
> exported so writer and reader share one format.

**Ownership: the Chart merge stays in Nami's `reconcileChart`.** It was
considered — and rejected — to expand Jinbe's scope to land Chart branches too
(see Alternatives). The *shared* rebase+squash+idempotency mechanic lives in
`git/.Land()`, which both `reconcileChart` and Jinbe call; that captures the
reuse without splitting ownership.

## Consequences

- Every seam the git-branch layer exposes is now named: the land op, the landed
  base, the branch names, the trunk pointer, and the three `git/` functions.
- Zoro is unblocked: it consumes `LandedBase("Chart: "+chartID)` after
  walking the caused-by edge from the Spec.
- **Chart merge-conflict *policy* is out of scope** and filed as an offshoot
  Signal. `git/.Land()` surfaces a conflict as a typed error, but ADR 0009 gives
  the Chart no conflict/review crew (unlike the Checkpoint's Franky path), so what
  `reconcileChart` does with a Chart-branch conflict is separate fog, not decided
  here.

## Alternatives considered

- **Expand Jinbe's scope to land Chart branches.** Rejected. (a) Jinbe's land is
  *gated* (approval precondition, conflict→Franky, Review respawn); the Chart
  branch has **no** gate (ADR 0009 §1), so it would fork his handler, not reuse
  it. (b) The Chart land is **not terminal**: it is a 3-step non-atomic op
  (merge + spawn the Spec + close the Chart), and spawning the Spec is Nami's
  succession act on the caused-by spine — splitting the op across two crewmates
  turns one idempotent roll-forward into a cross-crewmate handoff with an unclear
  crash owner. The reuse is captured in `git/.Land()` instead.
- **Plain merge / ff-only.** Plain merge preserves each artifact commit but gives
  a merge-commit landed base and no git-native idempotency; ff-only needs mainline
  unmoved. Rejected for the exact-landed-base + idempotency + tail-consistency of
  rebase+squash.
- **Store the landed SHA on the Spec bead.** A self-contained Spec (one read, no
  git call). Rejected: it duplicates an immutable git fact into bd for no gain;
  Zoro is already doing git ops, so `LandedBase` is native to it.
- **`rev-parse main` at Checkpoint-cut time.** Rejected: mainline has moved by
  then, so it yields the latest trunk, not this Chart's landed base.
- **Mainline = current HEAD.** Rejected: the checkout model is unsettled, so HEAD drifts as branches are checked in and out.
