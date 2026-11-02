# What a Remnant's respin exit can reuse to cut a Checkpoint and replay stranded commits

Status: Research (input to the Spec of Chart "A Forge merge that strands in-flight Checkpoint work asks the human how to recover it", mc-tn7ks)
Date: 2026-10-29
Bead: mc-tn7ks.3

## Purpose

The Chart's respin exit cuts a new Checkpoint from fresh mainline against the landed Checkpoint's original Spec, with no new Spec. It cherry-picks the stranded `Tip..head` onto that Checkpoint and re-files the stranded children as its members in succession. This doc sets out what existing code and ADRs already provide for each part, and where they would misfire:

1. the "new Checkpoint on a fresh branch" that the pivot outcomes describe, and whether a second Checkpoint can serve a Spec that already cut one;
2. the ADR 0397 renumber replay as a replay precedent, and how a `Tip..head` replay meets Compose, the numbered-file clash check and the Conflict path;
3. how a kept `effort/*` branch cut from the old container can integrate into a different container, through `route`, `Base:`/`Tip:` pins and the cut ref, plus what the stranded range is when the branch was rewritten after the pin.

Every claim cites code at `apps/mugicrew/` (paths below are relative to it) or an ADR in `apps/mugicrew/docs/adr/`.

## 1. A second Checkpoint against a Spec that already cut one

### 1a. Pivot escalate does not itself cut a Checkpoint

- The glossary says escalate obsoletes "the old Spec/Checkpoint via `supersedes` and spawning a new Checkpoint on a fresh branch" (`GLOSSARY.md:724-728`). ADR 0005 (mugicrew-spec-amendment) §5 states the rule behind that wording: "The new Checkpoint cuts a fresh branch from the current base … born fresh, never re-targeted onto the old branch. The old branch is abandoned, not auto-deleted: its commits stay in git, cherry-pickable by a human."
- The code does less than that wording says. `zoro.Escalate` (`zoro/sweep.go:96`) spawns a fresh **Chart** (via `spawnDiscovered`, keyed on the pivot) and cuts `chart/<new>` from mainline (`zoro/sweep.go:147-153`). It supersedes the old Chart, carries Ordering edges, and force-closes the old Checkpoint's members and the Checkpoint obsolete (`closeOut`, `zoro/sweep.go:337`). No Checkpoint is spawned. The new Checkpoint comes much later, through the ordinary Chart → Spec → `reconcileSpec` chain. `zoro.Confirm` (`zoro/sweep.go:49`) also spawns a **Spec**, not a Checkpoint.
- So the pivot path gives two things. One is the principle that a replacement Checkpoint is born on a fresh branch and the old branch stays as a cherry-pick source (ADR 0005 §5). The other is the sweep's close-out mechanics. It has **no Checkpoint-spawning code** to reuse. It also always goes through a *new* Spec or Chart, which the respin explicitly does not want.

### 1b. The ordinary Spec → Checkpoint spawn refuses a second Checkpoint

- `reconcileSpec` spawns the Checkpoint with `spawnSuccessor(spec.ID, succ)` (`zoro/zoro.go:252`), which is `bd.SpawnSuccessorIfAbsent` (`bd/succession.go:401`). That function returns any existing `caused-by` successor of the wanted kind, whatever its status (`SuccessorOf`, `bd/succession.go:373`). A Spec that already cut a Checkpoint therefore always gets the old (closed, landed) one back.
- `checkpointBase` would also cut at the wrong place for a respin. A charted Spec cuts from its Chart's `Anchor:` pin or `Chart:` land trailer (`zoro/zoro.go:175-195`), not from fresh mainline.
- ADR 0048 (mugicrew-rollback-successor) context says exactly this: "`SpawnSuccessorIfAbsent(spec, checkpoint)` matches the closed old Checkpoint, so it cannot hang a second Checkpoint off the Spec." It rejected "Caused-by the Spec through a new spawn helper" because that "makes the one-Checkpoint-per-Spec guard conditional" (ADR 0048, Alternatives).

### 1c. The Rollback successor is the working precedent

The Rollback successor is a second Checkpoint for the same Spec, and it is already built:

- **Edge.** It is `caused-by` the **Rollback**, spawned with `SpawnSuccessorIfAbsent(rollback, checkpoint)`, so it is idempotent per Rollback. There is no `supersedes` edge to the landed Checkpoint, which "landed and is history" (ADR 0048 §1–2). Code: `handOff`, `jinbe/jinbe.go:2026`, spawn at `jinbe/jinbe.go:2048`.
- **Spec reach.** Readers do not use the Checkpoint's own edge. `bd.SpecOf` returns the caused-by Spec, or else walks caused-by Rollback → Rollback's `related` target → repeat (`bd/succession.go:344-367`, Rollback leg at `:356`). The body also carries a human-readable `Spec: <id>` line (`jinbe/jinbe.go:2042`). The Spec-reading crewmates already go through `SpecOf`: Franky (`franky/franky.go:258`, `:295`), Usopp (`usopp/usopp.go:131`), Luffy (`luffy/luffy.go:241`), reroute/raise host checks (`luffy/reroute.go:328`, `bd/held.go:85`).
- **Base and seed.** Jinbe cuts `checkpoint/<new>` directly (`jinbe/jinbe.go:2065`), so `checkpointBase` is not on this path (ADR 0048 Consequences). It seeds the branch with one reapply commit (`jinbe/jinbe.go:2069`), chosen so that seeding "cannot conflict, so seeding never needs a human". Drift from later lands "is met at land time by the normal rebase and the Conflict path" (ADR 0048 §4). The seed is "a single squash-sized commit. Per-commit history is not restored" (ADR 0048 Consequences).
- **Re-entry.** One Build member is spawned, then the normal Build → Review → Approval → land (ADR 0048 §5).

For a respin, the matching shape is a Checkpoint `caused-by` the **Remnant**, spawned with `SpawnSuccessorIfAbsent(remnant, checkpoint)`, which is idempotent per Remnant. Its Spec would be reached through the Remnant's link to the landed Checkpoint. Building this needs a third leg in `specOf`'s walk (Remnant → landed Checkpoint), next to the Rollback leg at `bd/succession.go:356`. Without it, `SpecOf` returns not-found, as it does for a Surgery Checkpoint, and `bd.RaisePivot` refuses a pivot raised on it (`docs/bead-lifecycle.md:383`). One other walk copies `SpecOf` by hand and would need the same leg: `bd/lineage.go:195` ("as SpecOf does").

### 1d. A respin Checkpoint must not be caused-by the landed Checkpoint

- A Renumber Checkpoint is minted `SpawnSuccessor(landed.ID, …Kind: KindCheckpoint…)`, i.e. caused-by the landed container (`nami/renumber.go:83`). That happens at the same land detection, on the same landed Checkpoint, when it is the later side of a number clash (`jinbe/jinbe.go:303-321`).
- `nami.RenumberPred` identifies a Renumber Checkpoint *only* as "a kind:checkpoint caused-by a Chart or Checkpoint … so no marker is needed" (`nami/renumber.go:27-40`). `fileConflict` uses that test to widen a number-clash Conflict's "own beads" (`jinbe/jinbe.go:2337`).
- So a respin Checkpoint that is caused-by the landed Checkpoint would hit two problems. (a) `SpawnSuccessorIfAbsent(landed, checkpoint)` would return the Renumber Checkpoint, if one was minted, instead of spawning the respin. (b) A respin would be misread as a Renumber Checkpoint by `RenumberPred`/`RenumberScope`. The Rollback-style edge (caused-by the Remnant) avoids both.

## 2. Replaying `Tip..head`: precedents, Compose, the clash check and the Conflict path

### 2a. Existing replays are tree-level `merge-tree` replays

- **ADR 0397** (mugicrew-a-renumber-only-head-is-the-approved-squash-replayed…) §2 replays an approved squash onto a new base as one three-way tree merge: `git merge-tree --write-tree --merge-base=oldBase newBase old`. A conflict is a reject (code: `git/renumber.go:76`). Two points carry over. The replay writes only objects and never a ref, and a conflict means "the hand resolution is new content that needs a Review" (ADR 0397 §2). The renumber verdict itself (`VerifyRenumber`, `git/renumber.go:48`) does not carry over: it only judges whether a head *equals* an approved squash modulo renumber. A respin always needs the full Review and Draft-MR Approval (Chart Notes, ADR 0382), so there is nothing for it to verify.
- **Revert** builds its commit the same way: `merge-tree --write-tree --merge-base=<squash> <base> <squash>^` (`git/git.go:2235`). A conflict is a typed `*RevertConflict` with mainline untouched (ADR 0049).
- **Inference** (not yet exercised in code): `merge-tree --write-tree --merge-base=<Tip> <M> <head>`, where M is fresh mainline, gives the stranded work on M as one tree. A three-way merge does not need `Tip` to be an ancestor of `head`, so the same call also covers the rewritten-after-pin case in §3e. Like the Rollback seed, the result is one squash-sized commit. That costs nothing here, because every integrate already squashes (`git/git.go:2546` doc).

### 2b. Commit-level replays: Integrate's own-work rebase and IntegrateReplace's replay

- Squash `Integrate` replays only an effort's own commits with `git rebase --onto <container-tip> <cut>` (`git/git.go:2575`). The start comes from `OwnWorkStart` (`git/git.go:2617`) and the recorded cut ref (ADR 0264 (mugicrew-a-squash-integrate-replays-from-the-effort-s-recorded-cut-point…)). With the old branch as the effort and `Tip` as its cut, this is `Tip..head` replayed onto a fresh container, commit by commit. A conflict stops in the tree as `*IntegrateConflict` (exit 2), and an agent resolves it under the normal integrate prompt (`session/integrate.go:91-95`).
- `IntegrateReplace`'s `replay` picks `base..tip` onto an effort through an interactive-rebase todo with a `Replayed:` trailer, so a resumed resolve replays only what is left (`git/git.go:2751-2790`).

### 2c. How the replayed branch meets Compose and the clash check

- `Compose` (`git/git.go:1825`) checks main clean, reads base = mainline and pin = branch, runs `numberClashes(pin, base)` (`:1835`), then `rebase base` (`:1845`). A rebase conflict is `*RebaseConflict` (`:1853`). `ProbeConflict` runs the same two checks read-only before Review (`git/git.go:2882-2899`, called from `spawnReviewIfDue`, `jinbe/jinbe.go:2154`).
- `numberClashes` counts only numbered files the branch **adds** relative to `merge-base(base, branch)` (`git/git.go:2904-2924`). A respin branch cut from fresh mainline M has M as that merge-base. As a result:
  - numbered files the landed squash added (`base..Tip`) are already on M under their landed numbers. They are not added by the respin, so they **cannot clash**;
  - only files added in `Tip..head` are checked, exactly like any new Checkpoint;
  - a `Tip..head` commit that **renames** a numbered file (a pre-land renumber lap after the pin, ADR 0397's case) shows up as an add of the new number. If that rename ran against the landed Checkpoint's own ADR, mainline now holds the file under the landed number. The replay would then rename mainline's file, which the clash check does not catch. This is the "number means mainline's file" hazard of ADR 0397 §4–5 (inference; see Open questions).
- A Renumber Checkpoint minted for the same land (§1d) moves the landed file's number on mainline. Its bead sweep scope is the landed container and its subtree (`RenumberScope`, `nami/renumber.go:41`), which does not include a respin Checkpoint. Stranded lines that cite the landed number would be left stale.

### 2d. How a seed conflict meets the Conflict path

- The Checkpoint Conflict path assumes a branch that already holds the work and conflicts with *newer* mainline. `fileConflict` (`jinbe/jinbe.go:2312`) files a `kind:conflict` with `Base:` (mainline) and `Tip:` (branch head). It routes as a replace leaf (`route/route.go:154-155`): Franky "re-runs the rebase onto the pinned base" and `IntegrateReplace`s the result. The variants are `rebase-conflict`, `number-clash` and the human-filed `requested` one (`nami/nami.go:903-907`, ADR 0262 (mugicrew-a-requested-rebase-is-a-human-filed-conflict-variant…)).
- A respin that fails to *seed* (the `Tip..head` replay onto M conflicts) leaves no branch holding the work. "Rebase onto the pinned base" therefore has nothing to rebase, and the existing variants do not fit as written. Two reuse routes follow from the code:
  1. **Seed in a leaf, like Integrate.** Cut `checkpoint/<new>` at M (empty). Make the replay a member effort whose branch is the old container head with its cut ref at `Tip`, so that `mugicrew integrate` runs `rebase --onto <M> <Tip>` (§2b). A conflict then stops in the tree for the agent, as for any Build, and the Chart's "replay conflicts through the normal Conflict path" means integrate's conflict exit 2. That path is already wired to resolve-or-pause (`session/backstop.go:61` doc).
  2. **A new Conflict variant**, `replay`, alongside `requested` (ADR 0262 shows that a variant can be filed without a failed land behind it). It is pinned `Base: M`, `Tip: <old head>`, with the range start `Tip` named in the body. Its leaf would run `rebase --onto M <Tip> <head>` rather than `rebase <Base>`.
  The Rollback precedent (ADR 0048 §4) seeds only where seeding *cannot* conflict, and leaves drift to land time. Neither the Remnant's seed nor a fix-forward variant has that guarantee (ADR 0049), so the respin needs one of the two routes above.

## 3. Integrating a kept `effort/*` branch into a different container

### 3a. What survives the land

- When a running child's integrate hits the landed container, `integrateRefusal` refuses `ConflictLanded` (`git/git.go:2793-2802`) → exit 3. The backstop's `ExitLanded` arm files the stranded Signal, closes the child `obsolete: container landed`, and **keeps** `effort/<id>` (`session/backstop.go:105-111`; ADR 0201 (mugicrew-a-landed-container-s-child-closes-obsolete…) §1–2).
- The loop's reaper deletes only efforts in the tick's *integrated* set, whose tip is the cut ref (`reapIntegrated`, `loop/loop.go:1472`; ADR 0394). A stranded effort has its own commits past the cut, so it survives with its cut ref `refs/mugicrew/cut/<id>` (`git/git.go:414`).

### 3b. Container and mode come from the bead, not the branch

- `mugicrew integrate` routes the bead (`session/integrate.go:55`). A Build/Critique/Conflict/Surgery integrates onto `checkpoint/<b.Parent>` (`route/route.go:112`). Mode is Replace for a Conflict, a chart-conflict, or a Critique carrying **both** `Base:` and `Tip:`, and Squash otherwise (`route/route.go:150-155`).
- So an old bead can never integrate into the respin. It is closed, its Parent is the landed Checkpoint, and that container refuses as landed. The Chart's re-filed member is a **new bead** with a new id, which routes to the respin container. But it names `effort/<new-id>` and `refs/mugicrew/cut/<new-id>`, not the kept ones.

### 3c. What the new bead needs to adopt the kept branch

- `NewEffortWorktree` cuts a fresh `effort/<id>` at the container tip, or at a replace leaf's pin, unless the branch already exists (`git/git.go:349-363`). To roll the kept work forward, the respin must first point `effort/<new>` at the kept `effort/<old>` head and `refs/mugicrew/cut/<new>` at the old cut. Nothing does this today (inference: no code copies an effort branch between ids).
- With both refs in place, the existing legs do the rest:
  - `reuseEffort` leaves a branch with own commits alone. Because its cut is not at the new container's tip, it only logs `effort.stale-base` (`git/git.go:374-407`; ADR 0105).
  - `Integrate` → `OwnWorkStart` (`git/git.go:2617-2634`). The cut ref exists. `merge-base(<respin tip>, effort)` is an old mainline commit, not the cut. The cut is an ancestor of the effort but not of that merge-base, so the start is the cut (`:2631`). The rebase is `rebase --onto <respin tip> <cut>` (`:2575`), which replays exactly the effort's own commits. This is container-agnostic **because of the cut ref**.
  - **Without a cut ref** (an effort cut before ADR 0264), `Integrate` falls back to `merge-base --fork-point <container> <effort>` against the *new* container's reflog. That container never held the effort's base, so the result is `ErrNoCutPoint` (`git/git.go:2558-2561`) → exit 4 → paused for a human (`session/backstop.go:113`).
  - `integratedThrough` is keyed on the bead's own trailer (`session/integrate.go:86`). A new id has no prior squash on the respin, so the whole own-work range integrates once.

### 3d. Trap: a re-filed bead that copies its `Base:`/`Tip:` pins closes moot and deletes the work

- If a re-filed Critique keeps the old Base-pinned body (`Base:` + `Tip:` of the landed container), it routes as Replace onto the respin with `Tip` = an old-container sha (`route/route.go:154`). `IntegrateReplace` then finds that the respin tip does not descend from that pin and refuses `ErrOffPin` (`git/git.go:2673`) → exit 5.
- The backstop's `offPin` closes the bead **`moot: container already on base`** whenever the container descends from `Base:` (`session/backstop.go:154-158`). A respin cut from fresh mainline always descends from the old `Base:`. `finish()` then **deletes the effort branch** (`session/backstop.go:150-158`).
- So a re-filed member must drop its `Base:`/`Tip:` lines and integrate in Squash mode. A stranded replace child (Conflict, requested rebase, Base-pinned Critique) rewrote the *old* container. The respin is itself a fresh cut, so that rewrite has no container to apply to. Its kept branch is evidence for the Remnant's diff, not a member to re-file (inference from the replace semantics in `route/route.go:42-44` and ADR 0264).

### 3e. The range when the branch was rewritten after the pin

- `FlagStalePin` reads `Tip:` off the landed squash (`git.Repo.LandedTip`, `git/git.go:3302`) and compares it with the container head (`nami/nami.go:1054`). If `Tip` is an ancestor of the head, the range is `Tip..head` (`nami/nami.go:1068`). Otherwise, "the branch was rewritten after the landed pin, so tip..head would list every rebased commit as lost", and the Signal points at `git range-diff Tip...head` instead (`nami/nami.go:1046-1053`, `:1073`).
- ADR 0263 explains how this happens. A requested rebase or a Conflict resolution rewrites the container through `IntegrateReplace`, while the open MR stays mergeable at the old approved pin, so a human merge lands that pin.
- For a kept child effort, the stranded range is computed separately (`session/stranded.go:29-54`):
  - Squash mode: `<container head now>..effort/<id>`, or the landed `Tip:` when the container ref is gone.
  - Replace mode: `<Base: pin>..effort/<id>` (`:38-41`).
  - The squash-mode range ignores the cut ref. For an effort whose container moved after its cut, this differs from the `<cut>..effort` that `Integrate` would actually replay (`git/git.go:2554-2575`).
- In the rewritten case no commit range is right, because `Tip`'s commits reappear rebased under new shas. Three ways forward:
  - **Rewrite's published head.** The rewrite's result is recorded only in the event log, as `integrate.done`'s `sha` attr for the replace bead whose `Tip:` equals the landed Tip (`eventlog/eventlog.go:534-547`), not on any bead. So `Tip'..head` needs an event-log read.
  - **Three-way replay** (§2a): `merge-tree --merge-base=Tip M head` needs no ancestry and applies the change `Tip→head` onto M. That change includes the mainline drift the rewrite absorbed, which M already holds as identical changes. The cost is that a conflict resolution inside the rewrite comes along as new content. That is right for a respin, because it gets a full Review anyway (inference).
  - **Range-diff:** keep `range-diff` as display only, for Zoro's HITL session (Chart Notes).

## Open questions / gaps

- **Which seed route.** Seed-in-a-leaf (§2d.1, old head as an effort with cut = `Tip`) or a `replay` Conflict variant (§2d.2). The leaf route needs no new variant but needs the effort-adoption step of §3c. The variant needs a new leaf prompt and a body range. Neither is built.
- **`SpecOf` and copied walks.** A Remnant leg must be added to `bd/succession.go:350-367` and to `bd/lineage.go:195`. The stats spine walk (ADR 0284 (mugicrew-the-stats-spine-walk-counts-pivots-and-rollbacks…)) and the TUI's snapshot `specOfCP` (`tui/exectabdetail.go:23`) were not read line by line and may need it too.
- **Renumbered landed ADR.** When the same land also mints a Renumber Checkpoint (§1d, §2c), stranded lines that cite the landed number are outside `RenumberScope`. A `Tip..head` rename of the landed ADR would rename mainline's file, and the clash check does not catch it. No guard exists. The ordering between the Renumber Checkpoint and the respin is unspecified.
- **Effort adoption.** No code copies `effort/<old>` + `refs/mugicrew/cut/<old>` to a new id. A pre-ADR-0264 kept effort with no cut ref cannot be adopted that way (exit 4, §3c). It needs an explicit cut, e.g. the old container head at the effort's cut time, which the reflog may have lost.
- **Stranded-range mismatch.** `strandedSignal`'s squash range (`container head..effort`) and `Integrate`'s replay start (the cut ref) can differ after the container moved past the cut. The Remnant's diff display should use the cut ref to match what a respin replays. Not verified against a real repo.
- **Rewritten-after-pin replay.** The three-way `merge-tree --merge-base=Tip` replay in §2a/§3e is an inference from the existing merge-tree uses. It was not run on a real rewritten Checkpoint. A throwaway prototype on a real stale-pin history would confirm it.
- **Nested contexts.** Child-repo legs (`contexts.RepoOf`, `Repo.CutBranch`, cut refs living in the child, `session/integrate.go:51`) were not traced for a respin.
