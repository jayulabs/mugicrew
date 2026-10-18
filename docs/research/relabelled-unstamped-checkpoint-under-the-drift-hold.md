# Where a relabelled, unstamped Checkpoint is held under the drift hold

Status: Research (input to the Spec for "Cross-repo work splits into one context-labelled Checkpoint per repo, and nested-repo code can't land in the wrong repo", mc-uqpj.10)
Date: 2026-10-16
Bead: mc-uqpj.10.5

## Purpose

The Chart settled that relabelling an already-cut Checkpoint is refused by holding it
under `paused:context`, the same way ADR 0167 (one-workspace-many-repos…) holds stamp
drift, and is not re-cut. This doc finds where that check fits the ADR 0158
(an-undeclared-context-pauses…) tick scan, what it has to read, whether Jinbe's
pre-land re-check needs it, how the pause line should read, what it costs per tick
under ADR 0157 (subtree-contexts-cost-a-no-context…), and whether Zoro's cut follows
the Spec's label. All paths are relative to `apps/mugicrew/`, at `f45ab0e9`.

## 1. The case the existing scan misses

The case: a Checkpoint was cut unlabelled, so `checkpoint/<id>` is in the Workspace
repo and the bead has no `repo:` stamp. Later a human adds `context:<x>`, where `<x>`
is a `repo =` context.

- The scan's drift arm runs only when the bead has a stamp:
  `if name != "" && ok && bd.RepoStampOf(b) != ""` (`loop/loop.go:1070`). For an
  unstamped bead whose context is declared, `hold` stays `false` (`loop/loop.go:1069`).
- `contexts.Drift` has the same rule: a bead with no stamp never drifts
  (`contexts/contexts.go:189-192`).
- A test pins this: an unstamped labelled Checkpoint costs 0 reads and 0 acts
  (`loop/loop_test.go:2446-2448`).

So today nothing holds the relabelled Checkpoint. Every reader that resolves its repo
then points at the child, through `RepoDirOf` → `repos(root)[name]`
(`contexts/contexts.go:55-68`).

## 2. What happens today without a hold (it fails loudly, not silently)

- **Build cut.** `CutTree` reads the container and takes the nested arm
  (`session/session.go:73-82`). `cutNested` then runs `r.Materialize`
  (`session/session.go:118`), which runs `git worktree add -b effort/<id> <path>
  checkpoint/<id>` in the child (`git/git.go:2952`). The child has no
  `checkpoint/<id>`, so the add fails, the launch errors, and no Build code lands
  anywhere.
- **Land.** `legsFor` checks drift only on a stamped bead (`jinbe/jinbe.go:873`), so
  an unstamped one gets child legs. `lg.landedBase` misses (`ErrNotLanded`). Then
  `lg.head("refs/heads/checkpoint/<id>")` (`jinbe/jinbe.go:362`) is `r.Head`, which
  returns `ErrMissingRef` (`git/git.go:2740-2743`), and the Landing returns that error
  (`jinbe/jinbe.go:363-364`). The error comes before any land, MR or close.
- **Stamp.** Neither of these sites stamps. `StampCut` runs only from the Chart and
  Checkpoint cut sites: `luffy/triage.go:342`, `zoro/zoro.go:243` and
  `zoro/sweep.go:131`.

So the relabel can't fork work or land it in the wrong repo. What it does produce is a
repeated launch failure with a confusing cause. The hold turns that into a `‖` line
that names the fix. It is a clarity guard, not a safety guard.

## 3. Zoro's cut follows the Spec's label, and the stamp records it

- The Checkpoint copies the Spec's label and stamp when it is born:
  `Context: bd.ContextOf(spec), Repo: bd.RepoStampOf(spec)` (`zoro/zoro.go:118-119`).
- The cut is addressed by the Spec: `if r := repoOf(spec); r != ""` → `stampCut(spec,
  checkpointID, …)` then `repoCut(r, …)`, else `cutBranch` in the Workspace
  (`zoro/zoro.go:242-248`). The base is read from the Spec too
  (`zoro/zoro.go:178-209`).
- `StampCut` writes the dir of src's context, meaning the Spec's, and it writes it
  before the cut (`contexts/contexts.go:155-168`).

So a Checkpoint whose label disagrees with its Spec falls into one of two cases:

| Spec at cut | Checkpoint afterwards | Caught by |
|---|---|---|
| `repo =` context `a` (stamped, or stamped by `StampCut`) | relabelled `context:b` | the **existing** drift arm: stamp `a` ≠ `live[b]` (`contexts/contexts.go:194-195`) |
| unlabelled, or a `path =` context (never stamped: `RepoDirOf` → `""`, `contexts/contexts.go:159-161`) | relabelled to a `repo =` context | **nothing today**: the new check (§4) |

A Spec relabelled while it is still open would make a Zoro re-run address the child.
`checkpointBase` then fails before any cut. It reads the Chart or Spec trailer in the
child, which isn't there, and wraps `ErrNotLanded` (`zoro/zoro.go:201-207`). The
gitlab direct-Spec path is different: it pins a Workspace sha that the child can't
resolve. Either way, Zoro fails loudly and never forks.

## 4. Where the check fits and what it reads

**Site.** Add one more arm in `pauseContexts`, beside the drift arm
(`loop/loop.go:1066-1075`). It reuses that arm's lazy `live` table
(`loop/loop.go:1065`, `1071-1073`). The hold/lift switch already handles both
directions (`loop/loop.go:1076-1097`). The cascade needs no change, because
`PausedAncestor` already walks `paused:context` (`bd/bd.go:617-633`), so the
Checkpoint's Builds and Landing are skipped.

**Condition.** All of these must hold:

1. `b.Kind` is a container cut on `<kind>/<id>`. The Checkpoint is in scope. The
   Chart (`chart/<id>`, stamped at `luffy/triage.go:342`) has the same shape, and
   `git.BranchName(kind, id)` covers both.
2. `bd.ContextOf(b) != ""`, the name is declared, and `bd.RepoStampOf(b) == ""`.
3. The name is a `repo =` entry: `live[name] != ""`, where `live` is
   `contexts.Repos()` (`contexts/contexts.go:174-185`). `declaredContexts` can't tell
   this alone, because it flattens `repo` and `path` into one map
   (`contexts/contexts.go:33`).
4. `checkpoint/<id>` **exists in the Workspace repo**:
   `git.Repo("").HasBranch(branch)` (`git/git.go:2749-2752`; `Repo("")` runs in the
   cwd, `git/git.go:2717-2720`).

**Why the root probe and not the child probe.** "Missing in the child" alone would
also match the window between Zoro's spawn and its `StampCut` on a direct Spec
(`zoro/zoro.go:238-243`). Branch absent, stamp not yet written: a false hold, which
the Spec's re-run would then clear. "Present in the root" is the relabel's signature.
A child `checkpoint/<id>` without a stamp can't arise after ADR 0167, because
`StampCut` writes before it cuts and a failed write aborts the cut
(`contexts/contexts.go:150-153`, `163-167`). So the child probe adds nothing for
safety. Keep it only if pre-stamp legacy nested Checkpoints matter (see Open questions).

**Lift.** The bead stays in the scan while it is held, because the scan's filter
admits `PausedBy(b, PausedContext)` (`loop/loop.go:1056`). The hold lifts when:

- the `context:` label is removed (then `name == ""`, so `hold = false`); or
- the human cuts `checkpoint/<id>` in the child and adds `repo:<dir>` by hand. This
  is the dry run's hand repair. Once the bead is stamped, it leaves this arm and the
  drift arm judges it.

Re-cutting is out of scope per the Chart.

**Not reusable: the tick's ref read.** `RefsAcross` already reads the
`checkpoint/<id>` tips of the root and every child (`bd/succession.go:792-815`), but
the tick has to fetch them first:

- It merges the tips by id (`maps.Copy`, `bd/succession.go:811-812`), so it loses
  which repo holds each branch.
- The loop calls the effort-only wrapper, which discards the tips
  (`bd/succession.go:785-788`, `loop/loop.go:1223`).
- That read is lazy and runs only when a drain gate asks (`loop/loop.go:1228-1245`).

The targeted `HasBranch` probe is the cheaper fit.

## 5. Jinbe's pre-land re-check: not needed

ADR 0167 has Jinbe re-check drift because a land is irreversible (ADR 0167, "Drift
holds"). For this case, Jinbe already fails before any land: §2, `jinbe/jinbe.go:356-364`.

- **In-flight Landing.** A Landing that was already running when the hold arrived
  isn't interrupted (ADR 0158, "In-flight efforts are not interrupted"). It errors on
  the missing ref and does nothing irreversible.
- **New Landing.** The cascade gates new dispatch of the Landing.

Adding the root probe to `legsFor` would cost a git read on every unstamped land
(`jinbe/jinbe.go:872-879`) to buy only a cleaner error. **Recommend no change**, with
one test that pins the "missing ref, no land" behaviour.

## 6. The pause line's words

`contexts.PauseNote` already has a stamped-drift branch (`contexts/contexts.go:200-208`).
Without a new branch, the relabel case falls through to `bd.PauseNote`, which says
`"<x>" undeclared — relabel it, remove the label, or declare it…`
(`bd/bd.go:579-584`). That is wrong, because the context *is* declared. The TUI shows
this text in the pause cell (`tui/graphview.go:728`) and in the run-now refusal
(`tui/graphview.go:847`).

Proposed new branch, gated on `bd.RepoStampOf(b) == ""` with `context:` naming a
`repo =` entry. It keeps the existing `context (…)` shape:

```
context (cut in the Workspace repo before it was labelled context:<x> — remove the label, or cut checkpoint/<id> in <dir> and add repo:<dir>)
```

The branch must stay behind a label-only gate: an unlabelled bead and a stamped bead
take no extra read. That also keeps the `PauseNote` call per pause cell cheap
(ADR 0157). The branch needs `contexts.Repos()` to name `<dir>`, which matches the
read the stamped branch makes today (`contexts/contexts.go:202`).

## 7. Per-tick cost (ADR 0157)

| Workspace | Extra reads per tick |
|---|---|
| no `[context.*]` | **0**: `pauseContexts` returns before reading config when no fetched bead carries `context:` (`loop/loop.go:1061-1063`) |
| contexts declared, every labelled container stamped or `path =` | 0 for stamped beads, which take the drift arm. For an **unstamped labelled** container: **1** `contexts.Repos()` read, once per tick (config + `git rev-parse` + `IsRepoRoot` per entry, `contexts/contexts.go:174-185`; about 1.2 ms per ADR 0157). Then 0 git reads if the context is `path =` (condition 3 fails) |
| a relabelled, unstamped `repo =` container | that Repos read **+ 1** `git rev-parse --verify` in the root per such bead, per tick, until the human fixes it |

A `path =`-only Workspace pays the extra Repos read whenever a labelled container is
open. Check `len(cfglayer.Repos("."))` first to avoid it: that is the
"config first" gate `RepoDirOf` already uses (`contexts/contexts.go:57`), and it
costs one config read with no git call.

The test at `loop/loop_test.go:2446-2448` asserts `reads == 0` for an unstamped
labelled Checkpoint. It has to change, because that Checkpoint is now in scope.

## Open questions / gaps

- **Chart scope.** A relabelled cut Chart (`chart/<id>`) has the same gap. Use
  `git.BranchName(b.Kind, b.ID)` so one arm covers both, or keep the Spec to
  Checkpoints only?
- **Stamped but unlabelled.** A container with a `repo:` stamp and no `context:` is
  never held. `Drift` returns false with no context (`contexts/contexts.go:191`), and
  the scan admits only labelled or held beads (`loop/loop.go:1056`). Its Builds fail
  in the root on the missing branch. Should this be a sibling arm? It needs no git
  read, only a label check.
- **Hand stamp without a cut.** A human who adds `repo:<dir>` without cutting
  `checkpoint/<id>` in the child lifts the hold, because the drift arm sees a match,
  and the Builds then fail as in §2. Should the stamped arm also probe the child
  branch? That would be one git read per stamped container per tick, and ADR 0167
  chose against such reads.
- **Legacy Checkpoints.** Nested Checkpoints cut before the stamp existed (unstamped,
  branch in the child) are neither held nor harmed by the root-probe rule. A
  child-probe rule would falsely hold them. Confirm none exist in live graphs.
- **Not measured here.** The cost of `HasBranch` itself (one `git rev-parse` process
  per held bead per tick). ADR 0157's 1.2 ms figure covers the config read, not this
  probe.
