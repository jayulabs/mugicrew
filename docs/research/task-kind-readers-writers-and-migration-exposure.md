# Readers and writers of the Task kind, and what each migration option breaks

Status: Research (input to the migration grilling "Decide how existing Task beads and Specs migrate to the new kind", mc-8xo5i)
Date: 2026-10-22
Bead: mc-8xo5i.4

## Purpose

The Chart "Rename the Task kind" will give the kind a new name. This note maps
every place in mugicrew that reads or writes the kind or its Spec section. For
each place it says:

- whether it runs on closed beads or on landed Spec bodies;
- what happens there when a bead carries a kind the registry does not know.

It also counts the beads exposed today. It then works out the concrete failure
under each migration option the grilling will weigh:

- (A) relabel in place;
- (B) read both labels;
- (C) relabel open beads only.

It decides nothing.

All paths are relative to `apps/mugicrew/` unless they start with `docs/` at
the repo root. Line numbers are at HEAD `7466811`. Bead counts are from `bd`
1.3.0 (dev) against the live graph on 2026-10-22.

The bead asked for the note under `apps/mugicrew/docs/research/`. This run's
dispatch prompt fixes the location as `docs/research/`, so it lives there,
beside the earlier Task notes (for example
`docs/research/hitl-leaf-kind-plug-points-for-task.md`).

## 1. How the kind is carried and resolved

- The kind is one label, `kind:<value>`. Readers recover it by stripping the
  `kind:` prefix:
  - `bd/succession.go:852-858` (`kindFromLabels`);
  - `bd/bd.go:1103` (CSV kinds read).
- No reader validates the stripped value. A bead labelled `kind:task` after the
  rename keeps `Kind == "task"`. Every comparison against the renamed constant
  then fails silently.
- `contract.KindTask Kind = "task"` is defined at `contract/contract.go:42`. It
  is listed in `Kinds` (`contract/contract.go:84`), which builds the
  `StripKindPrefix` regexp (`contract/contract.go:89-98`). It is also one of the
  kinds `AdmitsLandOnlyHold` accepts (`contract/contract.go:106-112`).
- Its registry row is
  `contract.KindTask: {Owner: "brook", Mode: HITL, Container: false}`
  (`registry/registry.go:67`).
- What an unknown kind does at the registry:
  - `registry.Lookup` returns `registry: unknown kind %q has no owner`
    (`registry/registry.go:76-81`).
  - `ModeOf` reads an unknown kind as HITL (`registry/registry.go:102-106`).
  - `IsMode` reports an unknown kind as in **no** mode, not even HITL
    (`registry/registry.go:113-119`).

  The last point matters most. Every TUI HITL predicate goes through
  `isHITL` → `registry.IsMode(..., HITL)` (`tui/graph.go:22-24`). So a bead
  whose kind is unknown is not treated as a HITL leaf anywhere in the TUI.

## 2. Writers: what files a kind:task bead or a `## Tasks` section

| # | Site | Writes | Runs on |
|---|---|---|---|
| W1 | `mugicrew task new`: verb dispatch `main.go:145-146`, usage `main.go:224`, graph gate `main.go:312`, flags and usage string `tasknew.go:24-40` | the CLI front door for a Task | live filing only |
| W2 | `bd/tasknew.go:38`: `Successor{Kind: contract.KindTask, ...}` with `stripTitle(..., string(contract.KindTask))` | the `kind:task` label on every Task the verb files | live filing only |
| W3 | `bd/succession.go:664-681`: `SpawnBlockingTaskIfAbsent` builds `Successor{Kind: contract.KindTask}` | hold Tasks | live filing; its dedupe reads open Tasks (R13) and closed Tasks (R14) |
| W4 | Jinbe `holdTask` (`jinbe/jinbe.go:969-970`), called at `:612` (`Closed MR: !N`), `:832`/`:841` (pipeline infra/real), `:1016` (`Merged MR: !N`), `:1698` (Rollback revert pipeline); plus `bd/bd.go:978` (`Foreign: <sha>` hold) | hold Tasks through W3 | live, on an open container |
| W5 | Spec template `crewdocs/spec-template.md:59-87` | the `## Tasks (optional)` section, its `### <title>` entries, the `after:`/`before:` lines, the `task: <id>` adopt line | text the Spec author writes |
| W6 | Nami speccing `nami/prompts/speccing.md:55-58` | adopted `task: <id>` entries for held Tasks into a corrected Spec | live speccing |
| W7 | Zoro pivot `zoro/prompts/pivot.md:155-160` | writes each adopted held Task into the corrected Spec's `## Tasks` as `task: <id>` / `before: land` | live pivot confirm |
| W8 | Zoro slicing `zoro/prompts/slicing.md:7, 84-102` | files each listed Task through `mugicrew task new` | live slicing of an open Spec |
| W9 | Nami charting, grilling, promote-or-stamp: `nami/prompts/charting.md:35-38`, `nami/prompts/grilling.md:32-35`, `nami/prompts/promote-or-stamp.md:78-81` | `mugicrew task new --drafted-by ...` from a defog session | live defog only |
| W10 | Crew doc `crewdocs/task-body.md` (header line 1, verb at :32) and Brook's prompt `brook/prompts/task.md:8, 78` | the Task body format; Brook rewrites the steps (`amended`) | live Task session |
| W11 | `bd/taskbody.go:8-22` (`TaskStep`, `TaskBody`), used by Jinbe at `jinbe/jinbe.go:604, 834, 888-909, 1014, 1688-1691` | hold-Task bodies | live filing |

Every writer runs only at filing or session time. None rewrites a closed bead
or a landed Spec body.

## 3. Readers: Go code that branches on the kind

"Closed?" says whether the reader looks at closed Tasks or landed Spec bodies.
"Under an unrecognised kind" is what happens to a bead labelled `kind:task`
once nothing compares equal to `"task"` any more.

### 3.1 Dispatch, launch and session

| # | Site | Logic | Closed? | Under an unrecognised kind |
|---|---|---|---|---|
| R1 | `brook/brook.go:152-155` `Launch` | refuses any kind ≠ KindTask | open only | before Brook is reached, `launchOwner` → `registry.Lookup` errors (`tui/graphview.go:910-914`); if a registry row still points at Brook, Brook errors `kind "task" is not a launchable HITL kind` |
| R2 | `tui/actions.go:204` | an `in_progress` Task resumes rather than claims | open only | an in-progress old Task is not treated as a resume and takes the claim path, if it launches at all (it does not; see R1) |
| R3 | `session/hitlrun.go:120` | a Task session's exit takes `raiseSession`'s publish plan (pivot raise Drafts) | open only | the raise Drafts it created stay unpublished; the exit takes the plain close path |
| R4 | `session/hitlrun.go:172` | an in_progress Task keeps its claim on session end | open only | the claim is released (`releaseClaim`), so the "Task waits claimed" rest state is lost |
| R5 | `session/raisepublish.go:140-141` | evidence marker `Raised by Task <id>` instead of `Rerouted from` | open only | the evidence comment carries `Rerouted from <id>` instead of `Raised by Task <id>` |
| R6 | `luffy/raise.go:60-71` (`mugicrew pivot raise <task>`, `main.go:165, 236`) | refuses unless `kind == KindTask` and open | open only | loud refusal `<id> is kind "task", not a task` (the prefix comes from `refuseRaise(leg, …)`); the in-session raise path is dead for an old Task |

### 3.2 Loop, land and hold

| # | Site | Logic | Closed? | Under an unrecognised kind |
|---|---|---|---|---|
| R7 | `contract/contract.go:106-112` `AdmitsLandOnlyHold`, used by `loop/loop.go:754` (`Readmit`) and the TUI | an open Task blocking a Checkpoint holds only the land | open blockers only | an open old-label before-land Task freezes the whole Checkpoint (members drop out of the ready frontier), like a pivot |
| R8 | `loop/loop.go:751-753, 774` | an open Rollback held only by Tasks is re-admitted | open blockers only | Jinbe stops seeing a Rollback held by an old hold Task |
| R9 | `jinbe/jinbe.go:813-817` | skip re-reading jobs when an open infra Task for this pipeline is already filed | open blockers | the guard misses, and Jinbe goes on to re-read jobs and file again (W3 dedupe also misses: R13) |
| R10 | `bd/mergesha.go:26-49` `ResolveMergeSha`, called by `bd.MergeShaOf` (`bd/mergesha.go:10-24`) from `jinbe/jinbe.go:323` and `nami/nami.go:764` | finds the `Merged MR: !N` hold Task among **all** blockers, closed included, and reads its `Merge sha:` line | **closed** (by design: "a human closes the hold once the trailer is stamped", `bd/mergesha.go:17`) | returns `no hold Task keyed ...`; the empty-commit land logs `land.merge_sha_missing` and runs clash detection without the merge sha (`jinbe/jinbe.go:318-330`): degraded, not a failure |
| R11 | `bd/succession.go:664-672` `SpawnBlockingTaskIfAbsent` dedupe (`OpenBlockersOfKind(..., KindTask)`) | idempotent on (container, key) over open Tasks | open | an open old-label hold Task is not seen, so a duplicate hold Task is filed |
| R12 | `bd/succession.go:683-713` `latestClosedWithKey` | a re-filed hold gets a `related` edge to the most recently closed Task with that key (ADR 0338) | **closed** | the `related` edge is silently skipped |
| R13 | `zoro/zoro.go:297-331` slicing gate via `bd.TaskChildren` (`bd/bd.go:1113-1129`) | an open Spec with any `kind:task` dependent over discovered-from (any status) is never re-forked; the Spec counts as sliced if it laid a Task | **closed** Tasks count, on purpose (`bd/bd.go:1115-1116`) | an open Spec whose only artifacts are old Tasks re-forks, and slicing re-files every Task as a duplicate (the comment at `zoro/zoro.go:300-302` names this as the hazard); only if that re-fork lays nothing does the reconcile fail `slicing laid neither a Build nor a Task` (`zoro/zoro.go:327-330`) |
| R14 | `zoro/zoro.go:354-374` Task-only Spec arm | Tasks with no Builds close the Checkpoint `nothing to land` | as R13 | reached only after the R13 re-fork has re-filed the Tasks under the new label |

### 3.3 Withdraw, cancel and Initiatives

| # | Site | Logic | Closed? | Under an unrecognised kind |
|---|---|---|---|---|
| R15 | `bd/sweep.go:13-15` `IsWithdrawable` | an unclosed, published Task is withdrawable | unclosed only | X on an old Task is refused (`withdraw/withdraw.go:252`: "not a chain head or a Task") |
| R16 | `bd/sweep.go:72, 153` `SweepTasks` | the forward/backward sweep and land-hold release run over Tasks only | unclosed only | old Tasks are neither swept nor released; a withdraw is refused instead (`OrderedAfter`, `withdraw/withdraw.go:262-266`) |
| R17 | `withdraw/withdraw.go:225-231, 241, 263, 307-310` | an Initiative cancel closes member Tasks first; `--release` of a member Task | unclosed only | an old member Task falls into `rest` and is closed as a plain subtree member, so a `--release` of it is refused (`:296`) |
| R18 | `bd/chainhead.go:82-85` `CancelSet` | a released member Task is kept out alone | unclosed only | it goes through `WithdrawSet`; the result depends on its edges |
| R19 | `bd/initiative.go:120-125` `ReleaseRequiredWhy` | a member Task is exempt from Require Initiatives | unclosed only | an old Task is told to `initiative adopt` |
| R20 | `offshoot.go:48` (`mugicrew signal offshoot`) | an unclosed Task directly under an Initiative is its own container for ordering | unclosed only | the offshoot is filed `Order: unordered` |
| R21 | `luffy/triage.go:385-401` `containerAmong` | a Task member of the Initiative is a follow-up container; "Status is not checked" (`:385-386`) | **closed** (a follow-up is normally triaged after its container closed, ADR 0347) | a follow-up Signal whose cause is a closed old Task finds no container, so triage loses the ordering |

### 3.4 TUI

| # | Site | Logic | Closed? | Under an unrecognised kind |
|---|---|---|---|---|
| R22 | `tui/graph.go:22-24` `isHITL`, used by `tui/exectab.go:170`, `tui/defogtab.go:111, 192, 276`, `tui/actions_io.go:330`, `tui/chartword.go:49-54`, `tui/defogdetail.go:156` | HITL-leaf routing (enter launches) | open | an old Task is no HITL leaf: enter hits the run-now path and is refused |
| R23 | `tui/exectab.go:245-251` (`gates` / `before land` / `after land` rows), `:348-381` `taskRows`, `:649-653` side rows | Exec tab Task rows | live (`execLive`) | old Tasks vanish from the Exec tab |
| R24 | `tui/initstab.go:149, 160` | Initiatives tab waiter rows | live | they vanish there too |
| R25 | `tui/graphtab.go:90-92` `restsClaimed` | an idle in_progress Task is at rest, not stranded | in_progress | an old in-progress Task shows as **stranded** |
| R26 | `tui/xdialog.go:131, 593-608, 680, 752`, `tui/canceldialog.go:165` | X-dialog texts and tags | unclosed | generic chain-head wording, and the X refusal from R15 |
| R27 | `tui/graphview.go:74` | colour `"248"` | any | the fallback colour |
| R28 | `tui/graphview.go:503`, `tui/exectabdetail.go:94` | header prose "Tasks and Pivots" | n/a | text only |

## 4. Readers: prompts and crew docs (agent-run, text-matched)

These match the literal label `kind:task` or the literal `task: <id>` /
`## Tasks` text. An agent applies them, so a mismatch makes the session go
wrong; nothing fails loudly.

| # | Site | Reads | Closed? |
|---|---|---|---|
| P1 | `zoro/prompts/slicing.md:66-83` | the `## Tasks` section of the open Spec being sliced; title-dedupe keeps only `kind:task` rows among the Spec's discovered-from dependents, "however they were published or closed" | **closed** Tasks count |
| P2 | `zoro/prompts/slicing.md:72-74, 109-125` | `task: <id>` adopt lines; the target "must be an open `kind:task`"; a non-Task id "must fail the slice loudly" | an adopted closed Task is skipped with a note |
| P3 | `zoro/prompts/pivot.md:39-47, 155-160` | Task raisers and held Tasks, as `kind:task` rows | open |
| P4 | `brook/prompts/task.md` (18 mentions), `crewdocs/task-body.md` | the body format and run record | open (the bead being walked) |
| P5 | `crewdocs/spec-template.md:59-87` | the section heading and adopt-line syntax that W5–W8 and P1–P2 agree on | landed Spec bodies are history only (§5.2) |
| P6 | lighter mentions: `crewdocs/checkpoint-rules.md` (2), `crewdocs/signal-filing.md` (1), `usopp/prompts/review.md` (1), `usopp/prompts/forge-review.md` (1), `franky/prompts/intent.md` (1), `README.md` (1), `GLOSSARY.md` (26) | wording | n/a |

**P2 under an unrecognised kind.** If the prompt is renamed and the adopted
bead still says `kind:task`, the instruction "an id that is not [the kind] must
fail the slice loudly" fires. A corrected Spec that adopts an old held Task
then cannot be sliced.

## 5. Today's exposure (the live graph, 2026-10-22)

### 5.1 kind:task beads

`bd sql "select i.status, count(*) from issues i join labels l on l.issue_id=i.id where l.label='kind:task' group by i.status"`
gives:

- `closed | 28`
- `in_progress | 1`

That is **29**, matching the epic's triage note. The figure of 35 in this
bead's question does not reproduce. `wisp_labels` holds no `kind:task`.

- **No open-status Task exists.** The only unclosed Task is the in-progress
  `mc-mwsdc` ("Walk the Signal-to-land walkthrough on a scratch repo…"):
  - no parent (outside any Initiative);
  - blocked by `mc-wtqut.7` (Checkpoint, closed);
  - blocks `mc-j43f.27` (Checkpoint, closed);
  - `related` to `mc-j43f.27.3` (Build, closed);
  - `mc-1gg34` (Signal, closed) is discovered-from it.

  So of the open-only readers, it can reach only those that act on the Task
  itself:

  - resume or launch: R1, R2;
  - session end: R3, R4, R5, R6;
  - stranded display: R25;
  - Exec-tab presence: R23;
  - withdraw: R15.

  The ones that act on a Task blocking a Checkpoint or Rollback (R7–R9, R11,
  R16) cannot reach it, because both Checkpoints are closed.
- **No open bead depends on a Task.** A query of `dependencies` for
  `blocks`/`discovered-from` edges whose target is `kind:task` and whose source
  is unclosed returns `(0 rows)`.
- **Parent edges.** 7 Tasks have a parent-child edge, every one closed:
  - Initiative `mc-zfce` (four Tasks);
  - Initiative `mc-j43f`;
  - Charts `mc-8wa` and `mc-r3t`.

  The two open Initiatives (`mc-4ryp`, `mc-jo1r`) hold no Task.
- **Hold Tasks.** A search of Task bodies for `Merged MR: !`, `Merge sha:`,
  `Closed MR: !`, `Foreign: ` and the infra lead
  (`jinbe/jinbe.go:780`) gives 0 for every key. No hold Task has ever been
  filed in this graph, so R10 and R12 have nothing to find today.
- **Spec provenance.** 7 Tasks hang off 6 Specs by `discovered-from` (all
  closed): `mc-8ozyz` (2), `mc-wtqut.3`, `mc-d0up1`, `mc-wtqut.6`, `mc-p1wk2`,
  `mc-jj3du`.

### 5.2 Specs carrying `## Tasks`

- **14** kind:spec bodies contain `## Tasks` anywhere.
- **11** carry it as a section heading (`description like '%\n## Tasks%'`):
  `mc-8ymw.9`, `mc-bcqs`, `mc-d0up1`, `mc-j43f.23`, `mc-jj3du`, `mc-p1wk2`,
  `mc-r832`, `mc-t72m.20`, `mc-wtqut.3`, `mc-wtqut.6`, `mc-zfce.7`.
- The other 3 (`mc-5iz7r`, `mc-mh9c7`, `mc-z5s0`) only mention the text in
  prose.
- All 14 are **closed**. None carries a `task: ` adopt line.

### 5.3 Open Checkpoints with an unsliced `## Tasks`

- **None.** No kind:spec is open (`0 rows`).
- The one open Checkpoint, `mc-zk8a8` ("Auto-approved Approval History names
  the setting's layer"), was cut from a closed Spec.
- Slicing reads `## Tasks` only from an open Spec (`zoro/zoro.go:297` gates the
  fork on `spec.Open()`; P1). So no landed Spec body is read by any code path
  again.

### 5.4 Code and docs volume

- **25** non-test Go files name `KindTask` or `kind:task`.
- **41** test files name them.
- **61** ADRs under `apps/mugicrew/docs/adr/` mention Task. Landed ADRs stay
  untouched (epic, "Settled at charting").

## 6. Failure under each migration option

The common assumption is that the Go constant, the registry row, the verb, the
prompts and the crew docs move to the new name in one land, and that the old
verb and adopt line fail loudly with the new form (epic, "Settled at
charting").

### (A) Relabel in place: every `kind:task` bead, closed included, gets the new label

- **Closed-bead readers keep working**, because they now see the new label:
  - merge sha, R10;
  - related re-file, R12;
  - TaskChildren gate, R13;
  - triage follow-up container, R21;
  - slicing dedupe, P1.
- **Landed Spec bodies stay unread** (§5.3), so their `## Tasks` / `task:` text
  needs no rewrite for any code path.
- **Concrete exposure:**
  - The in-progress `mc-mwsdc` keeps its claim. On its next launch the new
    prompt and crew doc describe a kind its label now matches, so the Task
    walks normally.
  - The relabel is a write to 29 beads. That includes closed ones, which bd
    labels in place.
  - The bead history shows a label change nobody made by hand. The migration
    should leave its own comment or event to explain it.
  - The prior `kind:task` value is lost from the label. Only the glossary
    *History* line records it.
- **Ordering.** If the relabel lands before the binary that knows the new kind,
  every reader in §3 misfires in the window between them (see (C)). The
  relabel and the rename have to ship together, or the relabel must run right
  after the new binary starts.

### (B) Read both labels: the code treats `task` and the new value as one kind

- **Mechanism.** It needs a single "is this the walk-through kind" predicate
  that replaces every `== contract.KindTask` in §3, about 40 comparison sites
  across the 25 files. In addition:
  - the registry needs both keys (`registry/registry.go:67`) or a
    normalisation at `kindFromLabels` (`bd/succession.go:852-858`) and at
    `bd/bd.go:1103`;
  - `Kinds` keeps `"task"`, so `StripKindPrefix` still strips `Task:` titles
    (`contract/contract.go:84-98`).

  Normalising at the two parse points is the narrow seam: every Go reader then
  sees one Kind.
- **Closed beads.** Covered, if the predicate is applied everywhere (R10, R12,
  R13, R21).
- **Gaps that remain:**
  - **Prompts (P1–P3)** text-match `kind:task` against `bd ... --json` output,
    which carries raw labels. Each prompt must name both labels, which keeps
    the old name in agent-facing text, the very confusion the Chart sets out
    to remove.
  - **Raw `bd` queries.** Any `bd list --label kind:<new>` a human or an agent
    runs misses the 29 old beads.
  - **Writers.** W2 and W3 write only the new label, so the dual read is
    permanent unless a later relabel retires it.
- **The `task: <id>` adopt line.** This is text in an open Spec, separate from
  the label. The epic already rules out keeping it as a silent alias.

### (C) Relabel open beads only: unclosed `kind:task` beads move; closed ones keep `kind:task`

- **Today, "open" is one bead**, the in-progress `mc-mwsdc`. It is relabelled
  and works as in (A).
- **The closed-bead readers break on the 28 closed Tasks:**
  - **R10** (`MergeShaOf`): a closed `Merged MR:` hold Task is not found, and
    the land logs `land.merge_sha_missing`. No such Task exists today (§5.1),
    so nothing breaks now. Any hold Task filed before the rename and closed
    after the relabel run would hit it.
  - **R12** (`latestClosedWithKey`): the ADR 0338 `related` edge is skipped for
    holds whose prior Task was closed under the old label. Also 0 today.
  - **R13** (`TaskChildren`): only open Specs are gated, and there are none
    (§5.3), so nothing breaks today. A Spec opened before the rename whose
    Tasks closed before the relabel would re-fork and re-file its Tasks.
  - **R21** (triage `containerAmong`): a follow-up Signal whose cause is a
    closed old member Task (the 4 under `mc-zfce`, 1 under `mc-j43f`, though
    those Initiatives are closed) finds no container and loses its ordering.
  - **P1** (slicing title-dedupe): on a re-slice of an open Spec, closed old
    Tasks are not enumerated, so their titles are filed again. 0 open Specs
    today.
- **TUI.** Closed beads render through the Graph tab's lineage, where an
  unknown kind gets the fallback colour (R27) and no Task-specific detail. That
  is cosmetic.
- **Race.** A bead that closes between the open-bead enumeration and the
  relabel stays on `kind:task`. The enumeration and relabel should run with the
  loop stopped. The epic hands the in-progress Task and "any Task launched
  mid-rename" to this grilling.

### Summary of live exposure

With 0 open Specs, 0 open beads depending on a Task, 0 hold Tasks ever filed
and 1 in-progress Task, the open-bead surface is a single bead. All three
options leave nothing broken today *provided the in-progress Task is migrated*.

They differ only in latent exposure:

- **(A)** leaves none.
- **(B)** leaves none in Go, but keeps the old name in prompts and in raw `bd`
  queries.
- **(C)** leaves the closed-bead readers R10, R12, R13, R21 and P1 unable to
  see the 28 closed Tasks. All but R21 bite only on a hold Task or open Spec
  that predates the rename. R21 bites on any later follow-up Signal triaged
  against a closed old member Task.

## Open questions / gaps

- **Unreproduced counts.** This bead's question cited 35 kind:task beads and
  14 Specs with `## Tasks`. The live graph gives 29 Tasks, and 14 Spec bodies
  that mention `## Tasks` of which 11 carry the section. The 35 may count
  something else (a different graph snapshot, or a label search that also
  matched other text).
- **Close-and-relabel.** Not checked: whether bd's `label` command on a closed
  bead updates `updated_at` or `closed_at`. If it touches `closed_at`, R12's
  "most recently closed" ordering (`bd/succession.go:708`) could shift. A
  `bd` 1.3.0 test on a scratch graph would settle it (mind the `BEADS_DIR` trap
  in the Workspace crew brief).
- **Session-store records.** Recorded HITL sessions for `mc-mwsdc` may key on
  kind (for example `session/tmux.go:606` reads the registry on resume, and an
  unknown kind "resumes with the edit tools"). Whether the session store
  persists the kind string was not traced.
- **`kind_profiles`.** A `kind_profiles` key `task` in a local or Workspace
  config is dropped with `config.unusable` after the rename
  (`loop/config.go:311-313`), and `SetLocalKindProfile` refuses it
  (`loop/config.go:198`). No such key exists in the Workspace's
  `.mugicrew/config.toml` (grep: no match). Machine-local configs were not
  checked.
- **Event log.** `eventlog/eventlog.go:255` reads the registry per bead. Past
  log lines with `kind: task` were not checked for any reader that replays
  them.
- **Test fixtures.** Whether any of the 41 test files assert on the literal
  `"task"` string, rather than the constant, was not enumerated. They change
  with the rename either way.
