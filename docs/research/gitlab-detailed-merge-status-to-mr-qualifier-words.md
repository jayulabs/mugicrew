# GitLab detailed_merge_status values and their mapping to Merge Request qualifier words

Status: Research (input to the Spec "Execute rollup shows a gitlab Landing's Merge Request sub-state", mc-t72m.19)
Date: 2026-10-15
Bead: mc-t72m.19.2

## Purpose

The Chart settled that the Execute Rollup's `landing` word gets a display-only
qualifier built from the MR's `detailed_merge_status`. Candidate words are
pipeline running, needs rebase, awaiting merge, needs approval, threads open, and
draft. Unknown or transient statuses show no qualifier (Chart mc-t72m.19 Notes).
This doc pins GitLab's full enum for the version our Forge runs, explains how
GitLab picks one value, maps each value to a word or to none, flags overlaps with
outcomes Jinbe already records (ADR 0189 (under-gitlab-a-human-merges…) §4–5),
and checks whether the MR GET Jinbe already makes carries everything needed.

**Sources and pins.**

- **Forge version.** `glab api version` against gitlab.com on 2026-10-15 returned
  `{"version":"19.6.0-pre","revision":"0510b942f06",…,"enterprise":true}`.
  That revision resolves to `gitlab-org/gitlab@0510b942f062d3437e8908221b84a4f60e2da873`
  (committed 2026-10-15T14:56:29+02:00).
- **G** = files at that sha, read with `glab api projects/gitlab-org%2Fgitlab/repository/files/<path>/raw?ref=0510b942…`.
  So these are the docs and code that gitlab.com was serving, not `master`.
  (The earlier `docs/research/gitlab-merge-request-mechanics-through-glab.md` §4
  pinned `86b78fb4`, 19.5.0-pre, and listed only 11 of the values.)
- **M** = this repo: `apps/mugicrew/…` at this branch's HEAD.

## 1. The full enum

The REST docs list 24 values (G `doc/api/merge_requests.md:1332-1357`). The
GraphQL enum defines the same set: 22 in CE (G
`app/graphql/types/merge_requests/detailed_merge_status_enum.rb:9-74`) plus 2 in
EE, `requested_changes` and `security_policy_pipeline_check` (G
`ee/app/graphql/ee/types/merge_requests/detailed_merge_status_enum.rb:10-15`).
gitlab.com is EE (`"enterprise":true` above), so all 24 can occur.

| Value | Meaning (G `doc/api/merge_requests.md`) | Produced by |
|---|---|---|
| `preparing` | Merge request diff is being created | `merge_request.preparing?` (G `app/services/merge_requests/mergeability/detailed_merge_status_service.rb:14,29-31`) |
| `checking` | Git is testing if a valid merge is possible | `cannot_be_merged_rechecking? \|\| checking?` (dms `:15,33-35`) |
| `unchecked` | Git has not yet tested if a valid merge is possible | `merge_request.unchecked?` (dms `:16,37-39`) |
| `approvals_syncing` | The MR's approvals are syncing | `not_approved` while `temporarily_unapproved?` (dms `:19-20`; EE `ee/app/models/ee/merge_request.rb:894-896`) |
| `not_open` | MR must be open | `CheckOpenStatusService` (G `app/services/merge_requests/mergeability/check_open_status_service.rb:6,11`) |
| `draft_status` | MR is a draft | `CheckDraftStatusService` (`check_draft_status_service.rb:6,11`) |
| `status_checks_must_pass` | External status checks must pass | EE `check_external_status_checks_passed_service.rb:6` |
| `jira_association_missing` | Title/description must reference a Jira issue | EE `check_jira_status_service.rb:6` |
| `merge_time` | May not merge until a scheduled time | `check_merge_time_service.rb:6,11` |
| `merge_request_blocked` | Blocked by another MR (dependency) | EE `check_blocked_by_other_mrs_service.rb:6` |
| `requested_changes` | A reviewer has requested changes | EE `check_requested_changes_service.rb:6` |
| `title_regex` | Title doesn't match the project's regex | EE `check_merge_request_title_regex_service.rb:8` |
| `security_policy_violations` | Security policies must be satisfied | EE `check_security_policy_violations_service.rb:6` |
| `locked_lfs_files` | LFS files locked by others | `check_lfs_file_locks_service.rb:8` |
| `discussions_not_resolved` | All discussions must be resolved | `check_discussions_status_service.rb:6`, **inactive unless** `only_allow_merge_if_all_discussions_are_resolved?` (`:11`) |
| `commits_status` | Source branch must exist and contain commits | `check_commits_status_service.rb:6,11` (`has_no_commits? \|\| branch_missing?`) |
| `locked_paths` | Paths locked by others | EE `check_path_locks_service.rb:8` |
| `not_approved` | Approval is required | EE `check_approved_service.rb:9` |
| `security_policy_pipeline_check` | Security-policy pipelines must succeed | EE `check_security_policy_pipeline_status_service.rb:8` |
| `need_rebase` | MR must be rebased | `check_rebase_status_service.rb:6,15` (`should_be_rebased?`) |
| `conflict` | Conflicts between source and target | `check_conflict_status_service.rb:6,10-20` |
| `ci_must_pass` | A pipeline must succeed before merge | `check_ci_status_service.rb:8` |
| `ci_still_running` | A pipeline is still running | dms `:105` (`diff_head_pipeline_considered_in_progress?`) |
| `mergeable` | Branch can merge cleanly | dms `:104`, when no check is unsuccessful |

(EE check files are under G `ee/app/services/merge_requests/mergeability/`; CE
ones are under G `app/services/merge_requests/mergeability/`.)

## 2. GitLab reports exactly one value: the first blocker in a fixed order

`DetailedMergeStatusService#execute` (dms `:13-23`) returns `preparing`, then
`checking`, then `unchecked`. After that it returns the identifier of the
**first** unsuccessful check, and only when every check passes does it fall
through to the CI status. A check is "unsuccessful" when it is `failed` **or
`checking`** (G `lib/gitlab/merge_requests/mergeability/check_result.rb:70-72`).

The checks run in this order: `mergeable_state_checks_by_tier` (trivial, then
light, then heavy), then the git-state checks `[rebase, conflict]` (G
`app/models/merge_request.rb:1763-1765,1738-1761`). EE appends its checks to each
tier after the CE ones (G `ee/app/models/ee/merge_request.rb:350-368`, the block
`{ |_tier, ce_checks, ee_checks| ce_checks + ee_checks }`). The effective order
on gitlab.com:

1. trivial: `not_open`, `draft_status`, `status_checks_must_pass`, `jira_association_missing`
2. light: `merge_time`, `merge_request_blocked`, `requested_changes`, `title_regex`, `security_policy_violations`
3. heavy: `locked_lfs_files`, `discussions_not_resolved`, *(CI, skipped here)*, `commits_status`, `locked_paths`, `not_approved`/`approvals_syncing`, `security_policy_pipeline_check`
4. git: `need_rebase`, `conflict`
5. CI last: `ci_still_running` or `ci_must_pass`, else `mergeable`

The CI check is skipped in the ordered pass (`check_params` = `{ skip_ci_check: true }`,
dms `:81-83`) and evaluated only when everything else passed (dms `:17,103-108`).

Consequences for the qualifier:

- **One word only, and it's the first blocker, not every blocker.** An MR that
  needs approval *and* a rebase reads `not_approved`. `need_rebase` shows only
  once approvals, threads, and draft are all clear. `ci_still_running` shows only
  when nothing else blocks.
- **`need_rebase` can be suppressed.** The rebase check is `inactive` unless
  `ff_merge_must_be_possible?` (ff or semi-linear, G `app/models/project.rb:3290-3292`),
  and also `inactive` when `automatic_rebase_enabled?`
  (`check_rebase_status_service.rb:11-13`). EE also skips it when the project
  uses merge trains with ff (G `ee/app/services/ee/merge_requests/mergeability/detailed_merge_status_service.rb:12-18`;
  `ee/app/models/merge_trains/train.rb:43-46`).
- **The CI statuses are conditional.** The CI check is `inactive`, so the MR
  reads `mergeable` **even while a pipeline runs**, unless auto-merge is enabled
  or the project sets "Pipelines must succeed" (`only_allow_merge_if_pipeline_succeeds?`,
  `check_ci_status_service.rb:13-14`). mugicrew never arms auto-merge (ADR 0189 §1),
  and `forge.Settings` doesn't read that project flag (M `forge/forge.go:336-340`).
  So on a project without "Pipelines must succeed", `ci_still_running` appears
  only after a human arms auto-merge on the Forge.
- **`ci_must_pass`** covers any head pipeline that isn't successful (failed,
  canceled, missing, or still being created and not judged in progress):
  `mergeable_ci_state?` is false and the pipeline isn't pending
  (`check_ci_status_service.rb:16-22,35-46`). It is not "failed" specifically.

## 3. Transient values

These values mean "GitLab hasn't finished computing", not a human blocker:

- `preparing`, `checking`, `unchecked` (dms `:14-16`). Mergeability is checked
  asynchronously on read, so poll (G `doc/api/merge_requests.md:1323-1326`).
  Jinbe's tests already treat `unchecked` after a force-push as pending
  (M `jinbe/jinbe_test.go:698`).
- `approvals_syncing`: `not_approved` while approvals are being recomputed
  (dms `:19-20`).
- **`conflict` can be transient too.** `CheckConflictStatusService` returns
  `checking` (which counts as unsuccessful, so the identifier `conflict` is
  reported) when the source sha differs from the diff head, or when the merge
  status is neither `can_be_merged` nor `cannot_be_merged`
  (`check_conflict_status_service.rb:12-13,18-19`). This is a code-level reason
  for ADR 0189 §4's rule, already tested at M `jinbe/jinbe_test.go:700,801`
  ("stale conflict status").
- `ci_still_running` is short-lived but it is a real state, not GitLab computing.
  It's kept as a word (§4).

## 4. Mapping to qualifier words

"Overlap" means Jinbe already records an outcome for this condition: a
`land.<outcome>` event (M `eventlog/eventlog.go:276-287`) or a Landing close. A
word there would duplicate an existing outcome.

| Value | Word | Reason |
|---|---|---|
| `mergeable` | **awaiting merge** | Steady state under gitlab: only a human's merge click remains (ADR 0189 §1). Caveat: with the CI check inactive, it also covers "pipeline running" (§2). |
| `ci_still_running` | **pipeline running** | The pipeline is in progress (dms `:105`). Appears only when auto-merge is armed or "Pipelines must succeed" is set (§2). |
| `need_rebase` | **needs rebase** | The human clicks Rebase (ADR 0189 §3). Pending, never a conflict. |
| `not_approved` | **needs approval** | A human approval click. |
| `discussions_not_resolved` | **threads open** — *overlap flag* | Appears only if the project requires resolved threads (`check_discussions_status_service.rb:11`). **Overlap:** `threadArm` runs first in `mirrorMR` (M `jinbe/jinbe.go:454`). A thread Jinbe files closes the Landing `obsolete: review comment` with `land.review_comment` (M `jinbe/threads.go:245-251`), so no pending tick for it reaches the qualifier. What's left is threads Jinbe doesn't file or skipped (`thread_skipped`, M `eventlog/eventlog.go:286`). |
| `draft_status` | **draft** | A human must mark the MR ready. |
| `preparing`, `checking`, `unchecked`, `approvals_syncing` | none | Transient (§3). |
| `conflict` | none — **overlap** | Jinbe's conflict arm, keyed on `has_conflicts`/`merge_error`/local Compose trial, closes the Landing `obsolete: conflict` with `land.conflict` (M `jinbe/jinbe.go:491-523`; ADR 0189 §4). The status itself can also be transient (§3). |
| `ci_must_pass` | none — **overlap** | When the head pipeline failed, Jinbe already records `retried`, `pipeline_failed`, or `stalled` (M `jinbe/jinbe.go:460-472,613-620`; ADR 0189 §5). Other causes (canceled, no pipeline) are ambiguous (§2). |
| `not_open` | none | The MR's `state` isn't `opened`. `mirrorMR` handles merged/closed and treats `locked` as pending before any qualifier (M `jinbe/jinbe.go:438-452,478-488`). |
| `commits_status` | none | Branch missing or empty. Jinbe force-pushes the `Commit:` pin (ADR 0189 §1), so this is a transient right after the push, or a broken Forge state. |
| `requested_changes` | none (*candidate*) | A distinct human click (the reviewer clears it) with no candidate word. See gaps. |
| `merge_request_blocked` | none (*candidate*) | A dependency MR must merge first. A human-visible blocker with no candidate word. See gaps. |
| `merge_time` | none | Scheduled merge: no click is needed, it waits for time. |
| `status_checks_must_pass`, `jira_association_missing`, `title_regex`, `security_policy_violations`, `security_policy_pipeline_check`, `locked_paths`, `locked_lfs_files` | none | Project or compliance configuration, not a click on this MR. "Unknown" per the Chart's rule. |

Any value outside these 24 (a future GitLab addition) maps to none.

## 5. The MR GET Jinbe already makes carries everything

- `mirrorMR` reads the MR once per pending tick, by the Landing's recorded iid
  (M `jinbe/jinbe.go:430-437`, via `forge.GetMR`, `GET merge_requests/:iid`,
  M `forge/forge.go:185-196`).
- `forge.MR` already decodes `detailed_merge_status` (M `forge/forge.go:85`),
  `state` (`:83`), `iid` (`:82`), `web_url` (`:93`), and `head_pipeline` with
  its `status` (`:91,97-100`). Those are every input to the §4 mapping, the `!iid`
  the Rendering bead may show, and a link for `d`.
- **No new Forge read is needed.** The mapping uses only `DetailedMergeStatus`.
  If the Spec wants "pipeline running" even when the CI check is inactive (§2),
  `HeadPipeline.Status` in the same struct covers it, still with no new read. That
  would be a Spec choice, since the Chart settled on `detailed_merge_status`.
- The value is in hand on every pending path of `mirrorMR` (`:453-477`), and
  `locked` (`:478-479`) too. What's missing is a recorded fact. Today the pending
  tick discards it (Chart Notes; the store is mc-t72m.19.3 / mc-t72m.19.4).

## Open questions / gaps

- **"Pipeline running" under default settings.** Without "Pipelines must
  succeed" or a human-armed auto-merge, a running pipeline reads `mergeable` →
  "awaiting merge" (§2). Does the Spec accept that, or derive pipeline running
  from `head_pipeline.status` (no new read)? Our Forge project's
  `only_allow_merge_if_pipeline_succeeds` wasn't observed. `forge.Settings`
  doesn't read it.
- **`requested_changes` and `merge_request_blocked`** are distinct human
  blockers with no candidate word. Should they get words ("changes requested",
  "blocked by MR") or stay none? This is for the Rendering bead's grilling.
- **One status at a time** (§2): the qualifier names the first blocker in
  GitLab's order, not all of them. A rebase need hides behind a missing approval.
  `d` can't list the others without a mergeability-checks read (GraphQL
  `mergeabilityChecks`, not checked here).
- **`threads open` residue**: it isn't confirmed whether a Jinbe-skipped
  thread (system-started or individual note) keeps `discussions_not_resolved`
  on a fresh Landing. Bot-authored resolvable threads are filed like any other,
  so they don't linger unfiled. That depends on `fileNew`'s filter
  (M `jinbe/threads.go`), which wasn't traced here.
- No live MR was observed in each state. The mapping rests on G code and docs at
  the deployed revision. A GitLab upgrade can add values (they map to none) or
  reorder checks.
