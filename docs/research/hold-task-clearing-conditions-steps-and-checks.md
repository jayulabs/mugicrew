# Hold Tasks: clearing conditions, Steps and candidate Checks

Status: Research (input to the grillings "Choose raw glab api or a mugicrew read verb for hold Task Checks" and "Approve each hold Task's steps, markers and wording", mc-6848i)
Date: 2026-10-21
Bead: mc-6848i.2

## Purpose

This note covers every Go-filed hold Task body. For each one it gives the
condition under which the hold is really cleared (what Jinbe, Nami or the loop
reads again on its next tick), a draft `## Steps` in the crew doc task-body.md
format, and for each Check the exact read-only command Go could render when it
files the Task. Each Check is given two ways: as a raw `glab api` call (or a git
command, where the owning read is git), and as the one-word output a would-be
mugicrew read verb would print. It also lists which values the filer already
holds and which it would have to fetch.

Two things were settled at charting (Chart "Hold Tasks carry a Steps body Brook
can check", Notes) and are built on here. First, the body layout is a lead, then
the fact lines (the existing key lines, whole), then `## Steps`, then a trailing
`## Diagnostics`. Second, a fix with alternatives is one step whose Check
accepts any cleared outcome.

The verb names below (`mugicrew forge mr-state`, `mugicrew forge pipeline`,
`mugicrew landed`) are placeholders, so each Check can show its one-word
output. No such verbs exist today. Choosing between them and raw glab is the
probe grilling's job.

Tool pins used for every rendered command: `glab 1.124.0 (d43ccacec)`,
`git version 2.55.0`, `bd version 1.3.0 (dev)` (output of `glab version`,
`git --version` and `bd version` in this Workspace, 2026-10-21).

## 1. Facts shared by every body

1. **Idempotency is a substring match on the key.** `SpawnBlockingTaskIfAbsent`
   reuses an open Task that blocks the container when
   `strings.Contains(t.Description, key)` (bd/succession.go:611-621). The keys
   all end in `\n`: `Closed MR: !N\n` (jinbe/jinbe.go:572), `Merged MR: !N\n`
   (jinbe/jinbe.go:867), `Pipeline: N\n` (jinbe/jinbe.go:742, 1480) and
   `Foreign: <sha>\n` (bd/bd.go:931). A reformat keeps them matchable as long
   as the key text and its trailing newline appear anywhere in the body. Only
   **open** blocking Tasks are searched, so a closed Task lets the same key
   file again.
2. **The infra skip matches body text.** For a Landing, `pipelineFailed`
   skips the jobs and trace reads when an open Task blocking the container
   contains both `infraTaskLead` and `Pipeline: N\n`
   (jinbe/jinbe.go:743-750). `infraTaskLead` is the sentence "The retried
   pipeline failed again for an infra reason;" (jinbe/jinbe.go:714). The lead
   must therefore keep that sentence verbatim.
3. **Trace tails could match a key by accident.** `pipelineDetails` writes
   `Pipeline: N` first and then each job's 80-line trace tail inside a code
   fence (jinbe/jinbe.go:786-795). Because of fact 1, a trace that happened to
   contain `Pipeline: N\n` would satisfy the key. Moving the tails under
   `## Diagnostics` does not change this. Only the fact line placed before
   `## Steps` is the intended match.
4. **Brook closes only after a final pass of every Check.** It walks the steps,
   never advances past a failing Check, runs every Check again, and then asks
   before `bd close` (brook/prompts/task.md:56, 103-106). A Check has to be
   read-only or an idempotent no-op (crew doc task-body.md "Checks").
5. **Project encoding.** `forge.Project.ID` is
   `url.PathEscape(<namespace>/<project>)` (forge/forge.go:49, 114), so a
   nested path is encoded per segment. `go run` of
   `url.PathEscape("acme/platform/kid")` prints `acme%2Fplatform%2Fkid`. Every
   Forge call is `glab api -i --hostname <host> -X <method> projects/<ID>/<endpoint>`
   (forge/forge.go:504-522). The examples below use host `gitlab.example.com`,
   the Workspace project `acme/app` (`acme%2Fapp`) and the nested project
   `acme/platform/kid` (`acme%2Fplatform%2Fkid`).
6. **glab 1.124.0 cannot filter output.** Its `api` flags are `-F`, `--form`,
   `-H`, `--hostname`, `-i`, `--input`, `-X`, `--output json|ndjson`,
   `--paginate`, `--placeholder`, `-f` and `--silent` (`glab api --help`), with
   no `--jq`. A raw Check that wants one word has to pipe to `jq`. jq resolves
   to `/usr/bin/jq`, and the repo's flake.nix files do not mention it (a grep
   for `jq` and `glab` in `flake.nix` and `apps/mugicrew/flake.nix` finds
   nothing). A read verb would remove that dependency.
7. **The MR ref in running text.** `git.MRRef(path, iid)` is `!N` for the
   Workspace's project and `<path>!N` for a nested one (git/git.go:816). The
   Landing legs carry `mrPath = ""` for the Workspace and `proj.Path()` for a
   nested repo (jinbe/jinbe.go:921, 949, 1216-1220). An MR ref alone therefore
   **does not give the host or the Workspace project**. Go has to render them
   from `lg.proj`.
8. **No auto-merge.** mugicrew never arms auto-merge and never merges
   (forge/forge.go:1-4; ADR 0189 (under-gitlab-a-human-merges…) §1). A
   pipeline that passes still waits for a human merge.

## 2. Closed MR (mirrorMR's closed arm)

**Filed by** `mirrorMR`, when the Landing's own recorded MR reads `closed`
(jinbe/jinbe.go:565-576; ADR 0185 (an-open-merge-request-outlives…) §4). The
Task blocks the container, and the Landing is closed
`obsolete: merge request closed`.

Current body: the lead, then `Closed MR: !N`, then a `Fix:` line
(jinbe/jinbe.go:569-571).

**What clears it.** No Landing is open, so Jinbe re-reads no MR. The hold is the
Task itself:

- `reconcileCheckpoint` waits while `openBlockers` is non-empty
  (jinbe/jinbe.go:226).
- A Chart freezes on `openAnyBlockers` (nami/nami.go:667).
- The trailer check runs **before** that gate (jinbe/jinbe.go:208-214,
  nami/nami.go:617). An MR that is reopened and merged with its trailer
  therefore lands the container even while the Task is open.

After the Task closes, the next Landing reuses an open MR on the branch or
opens a fresh one (git/git.go:1476; ADR 0185 §4). The hold is cleared when one
of these holds: the MR is `opened` again, the MR is `merged` with the trailer
on mainline, or the container is closed (abandoned).

**Draft body**

    MR !N for <cp> was closed on the Forge, so its Landing was closed obsolete.
    Closed MR: !N

    ## Steps

    1. Reopen or merge MR !N, or abandon <cp>.   [irreversible]
       Do: on the Forge, reopen MR !N (merging needs it reopened first), and merge it if you mean to land now.
           Or abandon the container: `mugicrew withdraw <cp>`.
       Check: `<mr-state>` prints `opened` or `merged`, or `bd show <cp> --json | jq -r '.[0].status'` prints `closed`.

The `[irreversible]` marker applies to the merge and withdraw choices. A plain
reopen can be undone. The guide has to state the consequence of the choice
the human makes.

**The Check rendered**

| Form | Workspace MR | Nested MR |
|---|---|---|
| raw glab | `glab api --hostname gitlab.example.com projects/acme%2Fapp/merge_requests/12 \| jq -r .state` | `glab api --hostname gitlab.example.com projects/acme%2Fplatform%2Fkid/merge_requests/7 \| jq -r .state` |
| read verb | `mugicrew forge mr-state '!12'` prints `opened` | `mugicrew forge mr-state 'acme/platform/kid!7'` prints `merged` |

The states are `opened`, `merged`, `closed` and `locked` (forge/forge.go:141-143,
565-566). The verb would resolve the ref through `forge.ParseMRRef`, which
already takes `!N`, `<path>!N` or a URL (forge/forge.go:82-115).

**Values.** The filer holds `lg.proj` (host and encoded ID), `iid`, `branch`
and `cp.ID` (jinbe/jinbe.go:512-568). It needs nothing fetched.

## 3. Trailerless merge of a land's MR (holdTrailerless from mirrorMR)

**Filed by** `holdTrailerless`, when the MR is `merged` but a second fetch
still finds no trailer (jinbe/jinbe.go:521-534, 860-871; ADR 0185 §5). The
Landing stays open, and the tick returns an error every time.

Current body: the lead, `Merged MR: !N`, `Merge sha: <sha>`, a `Fix:` line and
a `Check:` line about ADR clashes (jinbe/jinbe.go:863-866).

**What clears it.** Each tick, `reconcileLandingOnce` fetches the Forge's main
and fast-forwards local main (`fetchFF`, git/git.go:1302-1313). It then runs
`lg.landedBase(trailer)` (jinbe/jinbe.go:393-401). A match whose `Graph:` is
ours, or that has no `Graph:` line, counts as landed (git/git.go:3010-3037),
and the Landing closes `landed: <sha>`.

**The ADR-clash line is a real gap.** Post-land clash detection diffs
`landed^1..landed` (git/git.go:3355). A hand-pushed trailer commit is usually
empty, so detection sees nothing.

**Draft body**

    MR !N for <cp> was merged at <sha>, but no "Checkpoint: <cp>" trailer reached mainline.
    Merged MR: !N
    Merge sha: <sha>

    ## Steps

    1. Stamp the trailer onto the Forge's main.   [irreversible]
       Do: on a checkout of the Forge's <main> (not local main), commit
           `git commit --allow-empty -m "land <cp> trailer" -m "Checkpoint: <cp>" -m "Graph: <our graph id>"`
           and push it: `git push <remote> HEAD:<main>`.
       Check: `git fetch --quiet <remote> +refs/heads/<main>:refs/remotes/<remote>/<main> && git log refs/remotes/<remote>/<main> -E --grep='^Checkpoint: <cp>$' --format=%H -n 1` prints a sha.

    2. Check the merge added no clashing sequence number.
       Do: if a path below collides with a number mainline already held, file a renumber.
       Check: `git diff --name-only --diff-filter=A <sha>^1 <sha>` lists no ADR or other sequence-numbered path whose number another path on <main> holds.

Step 2 is `Ask:` unless a verb applies the Workspace's sequence rules
(`SequenceRules`, git/git.go:3363). Go can render the `Graph:` value at filing
time (`git.GraphID`, git/git.go:738), so the Do writes the exact block
`TrailerBlock` would (git/git.go:699-705). A nested container skips the
renumber anyway (jinbe/jinbe.go:275).

**The Check rendered.** The owning read is git, so the git form above is the
one faithful to Jinbe. The fetch only moves a remote-tracking ref, which makes
it an idempotent no-op in task-body.md's sense. For a nested container, the
same commands run under `git -C <dir>`. Two other forms:

- Raw glab (less faithful, since it is not Jinbe's read):
  `glab api --hostname gitlab.example.com "projects/acme%2Fapp/repository/commits?ref_name=main&per_page=100" | jq -r '.[] | select(.message|test("(?m)^Checkpoint: mc-x$")) | .id'`
  prints a sha.
- Read verb: `mugicrew landed checkpoint mc-x` prints `landed`, or `absent`, or
  `foreign` (the three `LandedBase` outcomes, git/git.go:2891-2906).

**Values.** `holdTrailerless` holds `path`, `iid`, `sha`, `trailer` and `cp`
(jinbe/jinbe.go:860). It does **not** hold the host or project (the path is
`""` for the Workspace), the remote, `<main>` or the Graph id. Its caller
`mirrorMR` holds `lg` (proj, repo). It would have to pass `lg.proj`, and read
`ForgeRemote()`/`Mainline()` (git/git.go:1250-1258, 54) and `GraphID()`.

## 4. Trailerless merge of a Rollback's revert MR (holdTrailerless from revertMRArms)

**Filed by** the same function from `revertMRArms` (jinbe/jinbe.go:1326-1334).
The Task blocks the **Rollback** (`holdTask(rb, …)`), not the Checkpoint. The
trailer is `Reverts-Checkpoint: <cp>` (git/git.go:1999). The body carries the
same land-only clash `Check:` line, which does not fit a revert.

**What clears it.** `reconcileGitlabRollback` fetches, then reads
`lg.landedBase(reverts)` (jinbe/jinbe.go:1205, 1261). A hit hands off and
closes the Rollback (jinbe/jinbe.go:1262-1280).

**Draft body**

    Revert MR !N for <rb> was merged at <sha>, but no "Reverts-Checkpoint: <cp>" trailer reached mainline.
    Merged MR: !N
    Merge sha: <sha>

    ## Steps

    1. Stamp the revert trailer onto the Forge's main.   [irreversible]
       Do: as in §3 step 1, with "Reverts-Checkpoint: <cp>" and "Graph: <our graph id>".
       Check: the §3 git Check with --grep='^Reverts-Checkpoint: <cp>$' prints a sha; or `bd show <rb> --json | jq -r '.[0].status'` prints `closed`.

Drop the clash step. A revert removes the landed change and adds nothing.

**The Check rendered.** The read verb would be
`mugicrew landed reverts-checkpoint mc-x`, printing `landed`. The raw glab form
is §3's with the revert trailer.

**Values.** `rbLeg` holds `proj` and `repo` (jinbe/jinbe.go:1543-1553, 1216-1220).
The same gap as §3 applies: `holdTrailerless` is not passed them.

**A side observation, outside this bead.** A hand-pushed trailer commit `x`
makes the handoff key on `firstParent(x)` (jinbe/jinbe.go:1270). That is the
merge itself, which is the intended `M`, only when `x` sits directly on top of
it.

## 5. Landing pipeline: infra arm (pipelineFailed)

**Filed by** `pipelineFailed`, when the retried pipeline fails again and any
job's `failure_reason` is infra (jinbe/jinbe.go:752-770, 705-711; ADR 0188
(jinbe-watches-the-merge-request…) §3). The Landing stays pending, with the
line `Held: pipeline <id> <updated_at>` (jinbe/jinbe.go:683-695).

Current body: `infraTaskLead`, then "fix the runner, retry the pipeline on the
Forge, then close this Task.", then `[Merge Request: <path>!N]`,
`Pipeline: N`, `Pipeline URL:` and the per-job tails (jinbe/jinbe.go:762-766,
786-805).

**What clears it.** Jinbe's next tick reads the MR's `head_pipeline`
(jinbe/jinbe.go:547-557). A Forge retry keeps the id but moves `updated_at`,
so `heldPipeline` stops matching and the run is classified again
(jinbe/jinbe.go:681-691, 739-740). A pass leaves the Landing pending until a
human merges it (§1 fact 8). A new infra failure while the Task is still open
is skipped (fact 2).

**Draft body**

    The retried pipeline failed again for an infra reason; fix the runner and retry the pipeline on the Forge.
    [Merge Request: <path>!N]
    Pipeline: N
    Pipeline URL: <url>

    ## Steps

    1. Fix the runner.
       Do: repair the runner or scheduler named under Diagnostics (failure_reason).
       Ask: is the runner healthy again?
    2. Retry pipeline N.
       Do: Forge → pipeline N → Retry (or `glab api --hostname <host> -X POST projects/<ID>/pipelines/N/retry`).
       Check: `<pipeline-status N>` prints `success`.

    ## Diagnostics
    Job: …  Job URL: …  failure_reason: …  ```<80-line tail>```

Step 1 is an `Ask:` because runner state is not probeable at project scope
with what mugicrew reads today. While the retry is still running, step 2's
Check prints `running` or `pending`, which is a fail, not an error.

**The Check rendered**

- Raw glab:
  `glab api --hostname gitlab.example.com projects/acme%2Fplatform%2Fkid/pipelines/4242 | jq -r .status`
  prints `success`.
- Read verb: `mugicrew forge pipeline 'acme/platform/kid' 4242` prints `success`.

To be faithful to Jinbe, the Check could also confirm
`.updated_at != <filed updated_at>`. Go holds that value.

**Values.** The filer holds `lg.proj`, `lg.mrPath`, `iid`, and `pl.ID`,
`pl.WebURL`, `pl.SHA`, `pl.Ref` and `pl.UpdatedAt` (forge/forge.go:168-176),
plus the jobs. It needs nothing fetched.

## 6. Landing pipeline: Chart real-failure arm (pipelineFailed)

**Filed by** `pipelineFailed`, when the failure is real on a Chart. The Task
blocks the Chart, and the Landing is closed `obsolete: pipeline failed`
(jinbe/jinbe.go:772-781; ADR 0188 §5). A Checkpoint's real failure files a
Critique instead (jinbe/jinbe.go:777, 816-845), which is out of scope.

**What clears it.** Nothing is re-read while the Task is open. The Chart is
frozen on `openAnyBlockers` (nami/nami.go:667-669). After the Task closes,
Nami spawns a fresh Landing pinned to the branch head (nami/nami.go:670-679).
That Landing has no `Retried:` line, so a failed head pipeline is retried once
more (jinbe/jinbe.go:734-737).

The real clearing condition is therefore "the next MR pipeline would pass".
There are two ways to approximate it: main is green (when the failure lies in
main or the CI config), or a pipeline on a refreshed Chart head passes. A
branch pipeline retried at the same sha does not pick up a fix on main. A
merged-results pipeline does (forge/forge.go:178-180).

**Draft body**

    The retried pipeline failed on this Chart's branch; main or the CI config is likely broken.
    [Merge Request: <path>!N]
    Pipeline: N
    Pipeline URL: <url>

    ## Steps

    1. Fix main or the CI config.
       Do: land the fix on <main> through its own Chart or Checkpoint.
       Check: `<latest main pipeline>` prints `success`.
    2. Refresh the Chart onto the fixed main, if pipeline N was a branch pipeline.
       Do: `r` on the Chart's row in the TUI (files a rebase).
       Check: `git merge-base --is-ancestor <fixed main sha> refs/heads/chart/<id>` exits 0.

    ## Diagnostics
    …

Step 2 applies only when `pl.Ref` does not end in `/merge`. Go knows this at
filing time, so it can leave the step out.

**The Check rendered**

- Raw glab:
  `glab api --hostname gitlab.example.com "projects/acme%2Fapp/pipelines?ref=main&per_page=1" | jq -r '.[0].status'`
  prints `success`.
- Read verb: `mugicrew forge pipeline 'acme/app' main` prints `success`.

**Values.** The filer holds the same values as §5, plus `pl.Ref`. It reads
`<main>` locally (`Mainline()`/`Repo.Mainline()`, git/git.go:54, 2953).

## 7. Revert-MR pipeline: infra and real leads (revertPipelineFailed)

**Filed by** `revertPipelineFailed`, after one retry. Both leads file a Task
blocking the **Rollback** and record `Held:` (jinbe/jinbe.go:1463-1486; ADR
0208 (a-pending-rollback-s-revert-mr…) §4). The infra lead also starts with
`infraTaskLead` (jinbe/jinbe.go:1478). It is harmless, because the
Landing-side skip reads only the Checkpoint's blockers (jinbe/jinbe.go:743),
and a Rollback Task blocks the Rollback.

In this arm the body starts with `mrLead`, ahead of the lead sentence
(jinbe/jinbe.go:1480). The Landing arm puts it after (jinbe/jinbe.go:763).
That is a layout inconsistency for the Spec to settle.

**What clears it.** `revertMRArms` reads the MR every tick
(jinbe/jinbe.go:1321-1356):

- `merged` with the `Reverts-Checkpoint:` trailer hands off and closes the
  Rollback.
- `merged` without the trailer is §4.
- `closed` closes the Rollback `abandoned: revert MR closed`.

A Forge retry moves `updated_at` past `Held:` and re-arms the pipeline check
(jinbe/jinbe.go:1396). Either cleared outcome closes the Rollback, which makes
the Task moot.

**Draft body: real lead**

    The retried pipeline failed on the revert MR; main may be red for the very breakage being rolled back.
    [Merge Request: <path>!N]
    Pipeline: N
    Pipeline URL: <url>

    ## Steps

    1. Merge the revert MR anyway to hand off, or close it to abandon this Rollback.   [irreversible]
       Do: on the Forge, merge MR !N, or close it.
       Check: `<mr-state !N>` prints `merged` or `closed`.

**Draft body: infra lead.** It uses the same fact lines. The steps are §5's
steps 1-2 (`Ask:` runner, then retry with a pipeline Check), followed by the
real lead's merge-or-close step as step 3.

**The Check rendered.** These are §2's commands. The raw form is
`glab api --hostname gitlab.example.com projects/acme%2Fapp/merge_requests/31 | jq -r .state`.
The read verb is `mugicrew forge mr-state '!31'`, which prints `merged`.

**Values.** The filer holds `lg.proj`, `lg.mrPath`, `iid` and `pl`
(jinbe/jinbe.go:1463). It needs nothing fetched.

## 8. Foreign land (bd.HoldForeign), under both land strategies

**Filed by** `bd.HoldForeign` (bd/bd.go:923-935; ADR 0187 (a-land-trailer-carries…) §3). It has five callers:

- `reconcileCheckpoint` (jinbe/jinbe.go:210)
- `reconcileLandingOnce` (jinbe/jinbe.go:401)
- `mirrorMR`'s merged arm (jinbe/jinbe.go:530)
- Nami's Chart reconcile (nami/nami.go:619)
- the loop's candidate scan, `skipForeign` (loop/loop.go:858, 872, 929-940)

Current body: the lead, `Foreign: <sha>`, `Graph (theirs):`, `Graph (ours):`,
then a `Fix:` line naming `bd rename-prefix` (bd/bd.go:928-930).

**Under `land_strategy=local` this Task is never filed.** `Repo.LandedBase`
returns `*ForeignLanding` only on its gitlab branch. The local branch greps the
trailer and never reads `Graph:` (git/git.go:2995-3008). The only other
constructor of `ForeignLanding` is the `loop start` preflight
(git/git.go:1686-1688). `PreflightGitlab` is a no-op under local
(git/git.go:1596-1599). The `Graph:` line is written under local but never read
(ADR 0187 §2).

The Chart's note "Checks must also work under land_strategy=local" holds in a
weaker sense. Nami and the loop file this Task **without a Forge project in
hand**, so its Checks must not need `lg.proj`. They can use local git and bd
plus a project resolved through `git.ForgeProject` or `Repo.ForgeProject`.

**What clears it.** On the next pass, `LandedBase(TrailerFor(kind, cp.ID))`
returns landed or `ErrNotLanded` instead of `*ForeignLanding`
(contexts/contexts.go:335-351). After `bd rename-prefix`, the container's id,
and with it the trailer, no longer matches the peer's commit. The same rename
also clears the `loop start` refusal, which scans for any of our prefix's
trailers under another `Graph:` (git/git.go:1658-1690). A per-bead `bd rename`
would not clear it (bd/bd.go:929).

**Draft body**

    Mainline holds "Checkpoint: <cp>" at <sha> under another Bead Graph.
    Foreign: <sha>
    Graph (theirs): <theirs>
    Graph (ours): <ours>

    ## Steps

    1. Rename this graph's issue_prefix.   [irreversible]
       Do: `bd rename-prefix <p>`, choosing a prefix distinct from the other graph's.
       Check: `bd config get issue_prefix` prints something other than `<old prefix>`.
    2. Re-cut the branch under the new id.
       Do: `git branch -m <kind>/<old id> <kind>/<new id>`.
       Check: `git rev-parse --verify --quiet refs/heads/<kind>/<new id>` prints a sha.
    3. Close the old MR on the old branch.   [irreversible]
       Do: close the Forge MR whose source branch is <kind>/<old id>.
       Check: `<open MR on <kind>/<old id>>` prints `none`.
    4. Confirm mainline holds no foreign land of our prefix.
       Check: `<foreign scan>` prints `none`.

For a nested container (both a `repo:` stamp and a `context:` label), the lead
opens "Mainline of context <c> holds …" in place of "Mainline holds …"
(bd/bd.go:924-927; bd/bd_test.go:883-884). The draft has to keep that variant,
and every git Check in steps 2 and 4 then runs under `git -C <dir>`.

**The Checks rendered**

- Step 1: `bd config get issue_prefix` prints `mc` in this Workspace today. It
  is the read `readIssuePrefix` wraps.
- Step 3, raw glab:
  `glab api --hostname gitlab.example.com "projects/acme%2Fapp/merge_requests?source_branch=checkpoint%2Fmc-x&state=opened" | jq length`
  prints `0`.
  Read verb: `mugicrew forge mr-state --branch checkpoint/mc-x` prints `none`.
  This mirrors `FindOpenMR` (forge/forge.go:373-387). The iid is not used,
  because Nami's and the loop's callers do not hold one.
- Step 4 has no raw glab form, because the read is git:
  `git log refs/remotes/<remote>/<main> -E --grep='^(Chart|Checkpoint|Spec|Reverts-Chart|Reverts-Checkpoint): <new prefix>-' --format='%H%x00%B'`.
  It passes when no matched message carries a `Graph:` other than ours. That
  is `preflightProject`'s own scan (git/git.go:1675-1689).
  Read verb: `mugicrew landed --foreign` prints `none`.

**Values.** `HoldForeign` holds `cp`, with its kind, id, and `context:`/`repo:`
labels, and `foreign{Trailer, Ours, Theirs, Sha}` (git/git.go:2893-2895). It
has to fetch three things:

- the old prefix (`bd config get issue_prefix`)
- the branch (`git.BranchName(kind, id)`, git/git.go:614)
- the project, host, remote and main through `contexts.RepoOf(cp)`, which none
  of its callers pass.

The new prefix and new id are unknown when the Task is filed. Steps 2-4 have to
resolve them at Check time, for example with `$(bd config get issue_prefix)`.

## 9. What main changed after this note's source pin

§1–§8 cite the tree the Chart branch was cut from (`6bb72a2`, "land
chart/mc-8z95b"). The Chart's later commits only touch this note. Main has since
moved to `6c95c6a`, and three changes there bear on the drafts.

1. **Line numbers shift in jinbe/jinbe.go.** Line numbers after `mirrorMR`
   move. On `main` (`git grep -n` against `main:apps/mugicrew/jinbe/jinbe.go`),
   the closed-MR body is at 570 (here 569), the closed-MR `holdTask` call at 573
   (572), `infraTaskLead` at 741 (714), `pipelineFailed` at 760 (733), `mrLead`
   at 827 (800), `holdTrailerless` at 887 (860), `revertMRArms` at 1358 (1320)
   and `revertPipelineFailed` at 1501 (1463). The body texts and keys are
   byte-identical, so §1's facts 1–3 still hold.
2. **Landings and Rollbacks now carry `MR link: <web_url>`.** It is
   `git.MRLinkKey` (main:apps/mugicrew/git/git.go:814-823). `recordMR` and
   `setMRLines` write it beside `MR:` (main:apps/mugicrew/jinbe/jinbe.go:451,
   936, 1347), and `backfillMRLink` adds it to older Landings
   (main:apps/mugicrew/jinbe/jinbe.go:521, 614-638). The value is
   `forge.MR.WebURL` (main:apps/mugicrew/forge/forge.go:161). Every hold
   filer already has it at filing time, so none has to fetch it:
   - `mirrorMR`'s closed arm and the `holdTrailerless` call from it hold `mr`
     (main:apps/mugicrew/jinbe/jinbe.go:517, 535).
   - `revertMRArms` holds `mr` too (main:apps/mugicrew/jinbe/jinbe.go:1372).
   - `pipelineFailed` holds the Landing `l`, and `revertPipelineFailed` holds
     `rb`. Their link is `git.RecordedMRLink(l.Description)` or
     `git.RecordedMRLink(rb.Description)`.

   An `MR link:` fact line would give the Do steps of §2–§7 a URL Brook can
   hand the human ("reopen or merge at <link>") without rendering the host and
   project. The web URL embeds the host and the unencoded project path, nested
   ones included. It does **not** remove the gap noted under §3 and §4:
   `holdTrailerless` takes only `path`, `iid` and `sha`, so a raw `glab api`
   Check still needs `lg.proj` (or a URL parse) passed in. `HoldForeign` (§8)
   has no MR, so it gains nothing.
3. **Brook's merge ban is now scoped.** brook/prompts/task.md on main says
   "never merge or rebase a container branch by hand to catch it up … This ban
   covers only bringing a branch up to date with mainline. When a step's Do
   asks the human to merge a merge request on the Forge, that merge is the
   human's act: walk them through it as a Do with no command"
   (main:apps/mugicrew/brook/prompts/task.md:67-70). That settles the conflict
   with §2's "reopen or merge" alternative and §7's "merge/close": both are Do
   steps with no command. Their Checks stay as drafted. The final pass and close
   that §1 fact 4 cites moved to main:apps/mugicrew/brook/prompts/task.md:163-166.
   The new Stuck exit is a direct pivot or Critique raise, but only on "an open
   Checkpoint this Task holds" (main:apps/mugicrew/brook/prompts/task.md:89-138).
   It therefore reaches holds filed on a Checkpoint container, not §6's Chart
   real-failure body, whose Task blocks a Chart.

Main has since moved again, to `0596d99` ("land checkpoint/mc-r0oi9"). That
step changes nothing above: `git diff --stat 6c95c6a 0596d99` touches only the
five `apps/mugicrew/npm/*/package.json` files (9 insertions, 9 deletions,
version bumps). No Go source, prompt or ADR changed, so every `main:` citation
in this section is still valid at `0596d99`.

Main is now at `03ae691` ("lineage merge stock/mugicrew"), whose only parent is
`0596d99`. `git diff --shortstat 0596d99 03ae691` prints nothing, so the trees
are identical and every `main:` citation above holds at `03ae691`.

## Open questions / gaps

- **The foreign hold renames its own Task.** `bd rename-prefix` renames every
  bead, the walked Task included. Brook's close is `bd close <task id>`
  (brook/prompts/task.md:106), and the guide's record is keyed on that id. Is
  the walk resumable across the rename, or should step 1 be the last step?
- **`holdTrailerless` lacks the project.** Rendering any Forge Check needs
  `lg.proj` to be passed in (§3, §4). `HoldForeign` likewise needs a project
  resolved from `cp` (§8).
- **The trailerless clash step has no probe.** Detection is blind to an empty
  trailer commit (git/git.go:3355). The step stays `Ask:` unless a verb applies
  `SequenceRules`. The revert body should drop the step altogether.
- **No marker for Forge rights.** Pushing to a protected Forge main usually
  needs Maintainer rights. task-body.md's `[elevated]` means sudo, root or
  admin, so for now this push is marked only `[irreversible]`. The wording
  grilling should decide whether to widen `[elevated]`.
- **A closed MR merged without its trailer.** If the human picks "merge" in §2
  and the squash drops the trailer, no Landing is open to catch it: the new
  Landing has no `MR:` line, and the trailerless arm reads only a recorded MR
  (jinbe/jinbe.go:513-516). §2's Check accepts `merged`. Should it also require
  the trailer?
- **jq is not pinned.** It is from the system and not in the devShell, and
  glab 1.124.0 has no `--jq`. Every raw Check that prints one word depends on
  jq (§1 fact 6).
- **The Chart real failure can only be approximated.** "Main is green" or "the
  Chart is rebased onto it" stands in for "the next MR pipeline passes"
  (§6). Jinbe gives no read until a new Landing exists.
- **Trace tails can satisfy the key.** A trace tail containing `Pipeline: N`
  satisfies the substring key (§1 fact 3). A key anchored to the start of a
  line is outside this bead.
- **Chart behind main.** The `MR link:` line and Brook's scoped merge ban
  (§9) are on main, not on this Chart's branch. Should the Spec assume an
  `MR link:` fact line, and is the Chart rebased first?
- **mrLead order.** The Landing and Rollback pipeline bodies put
  `Merge Request:` in different places (§7).
