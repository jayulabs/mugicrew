<!-- hand-kept · sources: the kind:* constants in the contract package (contract.Kinds), the glossary's Operations lines (GLOSSARY.md), crew docs (crewdocs/), crewmate prompts, TUI keys, CLI verbs, the loop, doctor fixes · checked by bead_lifecycle_test.go (go test): every contract.Kinds value has a ## section and every kind-level ## section is a kind, and every literal fragment quoted in a "Close reason (exact)" column (placeholders written <name> are skipped) still appears in the Go source, prompts or crew docs; it does not check creation doors, Source identifiers or born-with edges -->

# Bead lifecycle reference

For each bead kind: how it is born (creation doors), how it ends (closure outcomes), and the edges it should be born with, compared against what each door creates. The code is the source of truth; `GLOSSARY.md` Operations lines are the starting point. This doc records today's paths and fixes nothing: each gap it finds is filed as an offshoot Signal and listed in [Gaps](#gaps).

## Legend

**Door types** (exactly these):

- **TUI key**: a key in the TUI.
- **reconcile**: `mugicrew reconcile`.
- **crewmate session**: a crewmate session, naming which crewmate and session. A raw `bd create` that a crew doc or prompt prescribes is filed under the crewmate session that runs it.
- **CLI verb**: a `mugicrew` verb.
- **loop**: a step of the dispatch loop.
- **doctor fix**: a `mugicrew doctor fix` row.

**Edge types**: `parent` (child to container), `blocks` (the blocked bead waits on the blocker), `caused-by` (a successor names what caused it), `discovered-from` (an offshoot names the bead it was found from), and others the code uses (named where they appear).

**Born-with marks**: ✓ the door creates the expected edge; ✗ it does not; n/a the edge does not apply to that door.

**Close-reason convention.** A close reason is lowercase `<outcome>[: <detail>]`: a fixed outcome word, then an optional detail after a colon and a space (`landed: …`, `obsolete: …`, `withdrawn: …`, `cleared: …`). Reasons that follow it can be classified by prefix; the spine kinds' bd-default `Closed` (entry 3) is meant to follow it too.

Each kind section has a plain lead line (what it is · owner · leaf/container), then the three tables and a **Gaps found** line naming the entries in [Gaps](#gaps), the one numbered index of every finding and its Signal (entry 38 applies to every kind). Doors are numbered D1, D2, … within each table, the Door type cell holds exactly one legend type, and the Born-with columns are headed by those door ids.

## signal

An intake into the factory: the head of the spine, and with Rollback one of two kinds that enter the graph from outside · owner Luffy (routing owner; triage is human-gated) · leaf. Sources: `GLOSSARY.md` Signal; `registry` kind table (`contract.KindSignal`: owner luffy, HITL, not a container).

### Creation doors

D5–D9 are Drafts: born `deferred` with a `drafted-by:<creator>` label until the creator's session publishes them (`bd.Publish`, ADR 0059 (mugicrew-drafts…)).

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| TUI key | D1 `n` then `s` (New Signal form); also `enter` on the Ideas tab's create row | the human | none: an open, unparented task | `tui.newOpts`, `tui.openNewSignal`, `tui.createSignal` → `bd.NewSignal`; `tui.ideaTab` key handler |
| TUI key | D2 `n` then `j` (New Signal from a Jira issue; shown only with a `[jira]` site) | the human | none; carries `external_ref` `jira-<KEY>`; a re-intake of a live ref supersedes the new Signal onto the carrier | `tui.newOpts`, `tui.fileJira` → `jira.Intake` → `bd.NewSignalWithRef`, `bd.Supersede` |
| CLI verb | D3 `mugicrew intake jira <KEY>` | the human | as D2 | `main.runIntakeJira`, `jira.Intake` |
| CLI verb | D4 `mugicrew signal new --title --description` | the human; a Chopper checkup session may run it on the human's yes | none: an open, unparented task | `main.runSignalNew` → `bd.NewSignal`; Chopper prompt `chopper/prompts/checkup.md` |
| CLI verb | D5 `mugicrew signal offshoot --from <cause> --drafted-by <bead> --order follow-up\|prerequisite\|none` | any crewmate session that finds work (Franky, Usopp, Nami's charting session for Split and Hand back, Zoro, Sanji, Brook, Luffy) | `discovered-from` the cause; with an order, `blocked-by` (follow-up) or `blocks` (prerequisite) the cause's open Checkpoint or Chart; with an order, `parent` is that container's Initiative; unorderable falls back to unordered with the reason in the body | `main.runSignalOffshoot` (`offshoot.go`), `bd.NewOffshoot`; crew doc `signal-filing.md` Offshoot form; `nami/prompts/charting.md` |
| crewmate session | D6 Luffy's triage INITIATIVE step and cut session: raw `bd create --type task -s deferred --no-inherit-labels -l kind:signal,drafted-by:<bead> --parent <initiative>` | Luffy | `parent` Initiative only | crew doc `signal-filing.md` Initiative member form; `luffy/prompts/triage.md` INITIATIVE step 5; `luffy.go` cut text |
| crewmate session | D7 Robin's discussion session: a Signal Draft on the human's explicit yes, filed in the offshoot form | Robin | `discovered-from` the Discussion | `robin/prompts/discussion.md` Signals |
| crewmate session | D8 Chopper's checkup session: an offshoot Signal for a defect or fault it finds | Chopper | `discovered-from` the checkup | `chopper/prompts/checkup.md` |
| crewmate session | D9 Brook's client dry-run Errand, step Filing blockers: raw `bd create -l kind:signal,drafted-by:<Errand id> --deps discovered-from:<dry-run Signal>` | Brook | `discovered-from` the dry-run Signal | `docs/client-dry-run-runbook.md` Filing blockers |
| loop | D10 Jinbe's and Nami's Landing reconcile files the stale-pin Signal when a landed squash drops a pinned commit (gitlab) | the loop | `discovered-from` the Landing; open, not a Draft | `nami.FlagStalePin`, `bd.SpawnOffshootSignalIfAbsent` |
| loop | D11 the dispatch backstop files the stranded-commits Signal when integrate is refused because the container landed | the loop | `discovered-from` the stranded bead; open, not a Draft | `session.strandedSignal`, `bd.SpawnOffshootSignalTitledIfAbsent` |
| loop | D12 Jinbe's MR Watch files the abandoned close-out Signal | the loop | `discovered-from` the MR Watch; open, not a Draft | `jinbe.fileAbandoned` (`mrwatch.go`), `bd.SpawnOffshootSignalTitledIfAbsent` |

No reconcile door and no doctor fix creates a Signal. A triage reroute files Pivots and Critiques, not a Signal (it is a closure, below): `luffy.SeedReroute`, `session.raiseSession`.

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| accepted (charted) | none given: bd's default `Closed` (`bd.DefaultCloseReason`) | Luffy's triage session; closed by the clean-exit publish (`hitl-run`) in the batch that opens the Chart and Survey Drafts | Carry: `luffy.carryOrdering` re-points the Signal's blocks dependents to the Chart before the close | `session.sessionPlan` / `signalCharted`; `luffy.reconcileSignal` |
| fast-laned (Spec) | as accepted | as accepted | Carry to the Spec; the Spec Draft is published after its `Anchor:` pin is written | `session.signalCharted`; `luffy.reconcileFastLane` |
| opened an Initiative | as accepted | as accepted (`mugicrew reconcile <signal> --initiative`) | Carry to the Initiative; Initiative and phase-1 member Drafts published | `luffy.SeedInitiative`; `session.signalCharted` |
| rerouted | `rerouted: <pivot and Critique ids>` | Luffy's triage session; closed by the clean-exit publish after `mugicrew reconcile <signal> --checkpoint` | publishes the Pivot and Critique Drafts and the gate edges (host Checkpoint blocks the Pivot, Pivot related to the Spec); Carry splits ordering onto the host and the caused-by successor | `session.raiseSession`; `luffy.SeedReroute`; `bd.CarryTargets` |
| rejected | free text: Luffy's `bd close <id> --reason "<why>"`; the TUI's `z` writes the typed reason, else `rejected from TUI` | Luffy's triage session; the human by TUI `z` (open, unclaimed Signals only) | no successor; no Carry: closing releases what the Signal gated | `luffy/prompts/triage.md` REJECT; `tui.reject`, `tui.rejectDefault`, `tui.rejectable` |
| duplicate (superseded) | bd's own text from `bd supersede <dup> --with <original>` (not written by mugicrew; its shape is in the succession test fixture) | Luffy's triage DUPLICATE and sibling merge; `jira.Intake` on a re-intake | a `supersedes` edge keeps lineage; no Carry runs, and the Signal's outgoing blocks edges drop | `luffy/prompts/triage.md` DUPLICATE; `luffy.memberSection`; `bd.Supersede`, `jira.Intake`; `bd/succession_test.go` fixture |
| parked | `later: <title> (phase line on <initiative>)` | Luffy's triage of a member Signal | stays under its Initiative and never holds the drain; the cut session refiles it as a fresh member | `luffy.memberSection`; `luffy.go` cut text; `GLOSSARY.md` Initiative |
| withdrawn | `withdrawn` for a chain head; `cancelled: <initiative>` when an Initiative cancel sweeps it | the human: TUI `X`, `mugicrew withdraw <head>`, `mugicrew initiative cancel` | releases the chain's held edges; cleanup inventory; Errands ordered after it close `withdrawn: <target>` | `withdraw.Withdraw`, `withdraw.Cancel`; `main.runWithdraw`; `tui.keyWithdrawKey` |

### Born-with ordering

| Expected edge | Why | D1–D4 | D5 | D6 | D7 | D8 | D9 | D10–D12 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `discovered-from` the finder | provenance of an offshoot; the key the spawn helpers use to stay idempotent (`SignalsFrom`, `CarriersFrom`) | n/a | ✓ | n/a | ✓ | ✓ | ✓ | ✓ |
| `parent` the cause's Initiative | members include the ordered offshoots filed against a member (`GLOSSARY.md` Initiative) | n/a | ✓ when ordered, else n/a | ✓ | n/a | n/a | n/a | n/a |
| `blocked-by` / `blocks` the cause's container | a follow-up waits for its container to land, a prerequisite gates it (`signal-filing.md`) | n/a | ✓ when orderable, else unordered with a stated reason | n/a | ✓ if filed by the verb | ✓ if filed by the verb | ✗ | ✗ |
| Draft status (`deferred` + `drafted-by`) | a crewmate's find stays invisible until its session publishes (ADR 0059 (mugicrew-drafts…)) | n/a (human action) | ✓ | ✓ | ✓ | ✓ | ✓ | ✗ |

### Gaps found

Entries 1, 2, 3, 4, 5 in [Gaps](#gaps).

## chart

Nami's charting branch (`chart/<id>`) that wayfinds a Signal into a Spec · owner Nami (steward; handler once drained) · container (parents its Survey and defog beads). Sources: `GLOSSARY.md` Chart; `registry` kind table (`contract.KindChart`: owner nami, AFK, container).

### Creation doors

Both doors create the Chart as a Draft (by the Signal or the pivot) with its Survey as a Draft child; the session's clean-exit publish opens them together.

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| reconcile | D1 `mugicrew reconcile <signal>` (the ACCEPT leg) | Luffy's triage session | `caused-by` the Signal; `parent` the Signal's Initiative while unclosed; `drafted-by` the Signal; `context:` label; then the Survey child (`parent` the Chart); then Carry of the Signal's blocks dependents to the Chart; branch `chart/<id>` cut | `luffy.reconcileSignal`, `luffy.chartSuccessor`, `bd.SpawnSuccessorIfAbsent`, `bd.SpawnSurveyIfAbsent`, `luffy.carryOrdering` |
| CLI verb | D2 `mugicrew pivot escalate <pivot> --evidence <file> [--carries <ids>]` | Zoro's pivot-triage session | `caused-by` the original Signal; `discovered-from` the pivot; `parent` the swept Checkpoint's Initiative; `context:` and `repo:` copied; `drafted-by` the pivot; Survey child; `supersedes` the old Chart when the Spec was charted; `discovered-from` each carried Critique; Carry of the Checkpoint's blocks dependents to the Chart | `zoro.Escalate`, `bd.SpawnDiscoveredIfAbsent`, `bd.SpawnSurveyIfAbsent` |

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| defogged and landed (the Chart's Spec authored) | none given: bd's default `Closed` | the loop-dispatched Nami reconcile, by `closeOnPublish`, once the Spec body is authored | the Chart's Approval closed approved and the Landing closes `landed: <sha>`; Carry re-points the Chart's blocks dependents to the Spec; the Spec Draft is published in the same batch; a renumber Checkpoint may be minted first | `nami.reconcileChart`, `bd.CloseLandings`, `nami.carryOrdering` |
| handed back | `handed back: <why>` | Nami's charting (Survey) session, `bd close <chart> --force --reason`, after filing an offshoot Signal | the Survey closes on exit; the pushed-back `chart/<id>` branch is deleted when the session ends | `nami/prompts/charting.md` Hand back; `session.settle`; `tui.chartOutcome` |
| rejected | free text `<why>` | the charting session, on the human's explicit assent | as handed back | `nami/prompts/charting.md` Reject; `tui.chartOutcome` reads prefix `rejected` |
| duplicate (superseded) | bd's own text from `bd supersede <chart> --with <original>` | the charting session, on the human's assent | a `supersedes` edge keeps lineage; no Carry | `nami/prompts/charting.md` duplicate |
| superseded by escalation | bd's own text from bd's supersede | `mugicrew pivot escalate` | the fresh Chart takes the Checkpoint's Carry | `zoro.Escalate`, `bd.Supersede(old, new)` |
| withdrawn | `withdrawn`; `cancelled: <initiative>` | the human: `X`, `mugicrew withdraw`, `initiative cancel` (chain head with nothing caused by it) | releases held edges; `chart/<id>` branch cleanup | `withdraw.Withdraw`, `withdraw.Cancel` |
| retired Draft | `obsolete: <id> of <parent> was retired` | the dispatch tail, when the creating effort's bead was swept | the Draft's publish opens only Signals | `dispatch.effort.retire` |

### Born-with ordering

| Expected edge | Why | D1 | D2 |
| --- | --- | --- | --- |
| `caused-by` its predecessor Signal | lineage; `SpawnSuccessorIfAbsent` idempotency; `CausedBy` and `SpecOf` walks | ✓ | ✓ (the original Signal) |
| `parent` the predecessor's Initiative | a member chain stays under its Initiative (`bd.InitiativeOf`) | ✓ | ✓ (read off the Checkpoint) |
| Survey child | an open Survey keeps the Chart from draining until fog is seeded (ADR 0140 (mugicrew-charting-is-a-survey…)) | ✓ | ✓ |
| Carry of the predecessor's blocks dependents | dependents wait for the Checkpoint to land, not for the stage to close (ADR 0085 (mugicrew-blocking-edges-carry…)) | ✓ (after create) | ✓ (from the Checkpoint, not the closed Signal) |
| `discovered-from` the pivot | the sweep's re-run key | n/a | ✓ |
| Draft status | the branch must exist before the loop sees the Chart | ✓ | ✓ |

### Gaps found

Entries 3, 4, 6 in [Gaps](#gaps).

## survey

A Chart's HITL charting leaf: the bead of the human-started session in which Nami maps the Chart's fog with a human · owner Nami · leaf (exactly one per Chart, seeded with it). Source: `GLOSSARY.md` Survey; `registry.table` (nami, HITL, not a container).

**Chart membership.** Born as a Draft child of its Chart (`parent` edge), so the open Survey keeps the Chart from draining until the session seeds fog (`loop.IsDrainedChartCandidate`, ADR 0140). It holds no content: its body is `contract.SurveyBody`, and speccing and promote-or-stamp skip it (`nami/prompts/promote-or-stamp.md`, `nami/prompts/speccing.md`). Its close, in the same batch as the fog Drafts, is what lets the Chart drain.

### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| reconcile | D1 `mugicrew reconcile <signal>`, ACCEPT leg | the human's Luffy triage session (`luffy/prompts/triage.md` ACCEPT step 1) | `parent` to the new Chart; `drafted-by:` the Signal (a Draft). No `blocks`, `caused-by` or `discovered-from`. | `luffy.reconcileSignal` calls `bd.SpawnSurveyIfAbsent(chartID, sig.Title, sig.ID)`; `bd.surveyChild`, `bd.SpawnChild`, `bd.childCreateArgs` |
| CLI verb | D2 `mugicrew pivot escalate <pivot-id> --evidence <file>` | the human's Zoro pivot triage session | `parent` to the fresh Chart; `drafted-by:` the pivot (the Chart's own drafter). No other edge. | `zoro.Escalate` calls `bd.SpawnSurveyIfAbsent(newChart, sig.Title, pivotID)`; `main.runPivotEscalate` |

Both doors are idempotent: `bd.SpawnSurveyIfAbsent` returns an existing Survey in any status, so a closed Survey is never re-seeded. No TUI key, loop step or doctor row creates one (searched `tui/`, `loop/`, `doctor*.go`: no `KindSurvey` spawn).

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| completed (fog seeded) | none given, so bd records `Closed` (`bd.DefaultCloseReason`) | mugicrew, on the Survey session's clean exit: `hitl-run` publish (`session.sessionPlan` with `surveyDone` true when the session's Drafts include a child of the Chart) | The session's Drafts publish in the same batch (defog beads, offshoot Signals); the Chart can drain | `session.sessionPlan`, `session.surveyDone`, `bd.Closing`, `bd.publishScript`; `GLOSSARY.md` Survey Operations |
| completed (Chart pushed back) | none given, so `Closed` | as above; `surveyDone` is true when the parent Chart is closed (handed back, rejected or superseded by the session) | A Chart closed with no Spec successor has its `chart/<id>` branch deleted once the session tree is down | `session.surveyDone`, `session.settle`, `session.HitlRun`; `nami/prompts/charting.md` Push back |
| returned (not a closure) | n/a | `session.settle` on a clean exit with nothing seeded and the Chart open: releases the claim, so the Survey is open and unassigned again | Back in the Inbox for another session | `session.settle`; `GLOSSARY.md` Survey Operations |
| withdrawn | `withdrawn` | the human's Withdraw of the Chart's chain head (TUI `X`, `mugicrew withdraw`): the Survey is in the head's unclosed subtree | Closed leaves first with the rest of the subtree | `withdraw.Withdraw`, `bd.WithdrawSet` |
| cancelled | `cancelled: <initiative>` | the human's Cancel of the owning Initiative | as withdrawn | `withdraw.Cancel`, `bd.CancelSet` |

No Survey is closed `obsolete:`, `superseded` or `rejected`. Those outcomes exist only on the Chart (`tui.graphData.chartOutcome` reads `handed back:`, `rejected`, empty), never on its Survey.

### Born-with ordering

| Expected edge | Why | D1 | D2 |
| --- | --- | --- | --- |
| `parent` to its Chart | membership: the open Survey undrains the Chart (ADR 0140; `loop.IsDrainedChartCandidate`) | ✓ | ✓ |
| `drafted-by:` the Chart's own drafter | so publish and delete take Chart and Survey together; any other drafter strands the Survey (`bd.surveyChild` comment) | ✓ (the Signal) | ✓ (the pivot) |
| `blocks` | none needed: the Survey gates nothing by edge, only by being an open child | n/a | n/a |

### Gaps found

Entry 7 in [Gaps](#gaps).

## grilling

A defog bead resolved by interviewing a human · owner Nami · leaf. Source: `GLOSSARY.md` Grilling; `registry.table` (nami, HITL, not a container).

**Chart membership.** Born as a Draft child of its Chart (`parent` edge), published with its drafter's Drafts. Its Chart cannot drain while it is unclosed (Drafts count as unclosed); when it closes, the Chart's Decisions-so-far gains its line (`defog-session.md` step 6) and the drained-Chart judge reads it as a closed defog child (`promote-or-stamp.md`, `speccing.md`). Source: `GLOSSARY.md` Chart and Fog; `contract.DraftInstruction`.

### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| crewmate session | D1 Nami's Survey charting session, `charting.md` step 5 (raw `bd create`, `bd dep add` in step 6) | Nami, with the human | `parent` to the Chart; `drafted-by:` the Survey. `blocks` only if the session runs the separate `bd dep add` of step 6. | `nami/prompts/charting.md`; crew doc `chart-body.md` Creating a defog bead |
| crewmate session | D2 Nami's grilling session, `grilling.md` step 4 (raw `bd create`) | Nami, with the human | `parent` to the Chart; `drafted-by:` the grilling being resolved. No `blocks`. | `nami/prompts/grilling.md`; crew doc `defog-session.md` Graduate fog |
| crewmate session | D3 Sanji's prototype session, `prototype.md` step 5 (raw `bd create`) | Sanji, with the human | `parent` to the Chart; `drafted-by:` the prototype. No `blocks`. | `sanji/prompts/prototype.md` |
| crewmate session | D4 Nami's promote-or-stamp session on a drained Chart, PROMOTE or ESCALATE (raw `bd create`; ESCALATE files exactly one grilling) | Nami, AFK through the loop | `parent` to the Chart; `drafted-by:` the Chart itself. No `blocks`. | `nami/prompts/promote-or-stamp.md`; `nami.Nami.Reconcile` via `reconcileChart` |
| crewmate session | D5 Nami's chart-conflict session, `Escalate: <title>` outcome (Go, not a raw `bd create`) | Nami, AFK through the loop | `parent` to the Chart; `drafted-by:` the chart-conflict; plus `blocks`: the chart-conflict waits on the grilling, handed to the publish batch. No `discovered-from`. | `nami.escalateChartConflict` (`bd.SpawnChild`), `nami.runChartConflict`; `GLOSSARY.md` Chart Conflict Operations |
| crewmate session | D6 Usopp's Forge Review session on a Chart (raw `bd create`, `kind:` the container's filed kind, which is grilling for a Chart) | Usopp, AFK through the loop | `parent` to the Chart; `drafted-by:` the Forge Review; `discovered-from` the Forge Review. No `blocks`. | `usopp/prompts/forge-review.md` Filing a bead; `contract.FiledKind`; `usopp.forgeArgs` |

No TUI key, CLI verb, reconcile leg, loop step or doctor row creates a defog bead (searched `tui/`, `main.go`, `luffy/`, `doctor*.go`); the human's only handle is launching the session that resolves one.

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| completed | `answered: <one-line summary>` | Nami's grilling session, after `mugicrew integrate` exits 0 (the human launches it with the TUI launch key) | Decisions captured as docs on `effort/<id>`, integrated onto `chart/<id>`; a Decisions-so-far line appended to the Chart; may draft more defog beads | `nami.Nami.Launch` (`grillingPromptClosing`, `nami.go:275`); `nami/prompts/grilling.md`; crew doc `defog-session.md` step 6 |
| completed (chart-conflict escalation) | none prescribed: the body says only "Answer each numbered question in this bead's notes … then close it" | the human, by raw `bd close` | The chart-conflict's `blocks` edge on it clears, the conflict returns to `bd ready` and the AFK resolve applies the answers | `nami.escalateChartConflict`; `nami/prompts/chart-conflict.md` (reads closed `kind:grilling`) |
| completed (Forge Review) | none prescribed | not stated; resolved as any grilling | none stated | `usopp/prompts/forge-review.md` |
| withdrawn | `withdrawn` | the human's Withdraw of the Chart's chain head (TUI `X`, `mugicrew withdraw`): the defog bead is in the head's unclosed subtree | Closed leaves first with the rest of the subtree | `withdraw.Withdraw`, `bd.WithdrawSet` |
| cancelled | `cancelled: <initiative>` | the human's Cancel of the owning Initiative | as withdrawn | `withdraw.Cancel`, `bd.CancelSet` |
| obsoleted or superseded | none prescribed (the doc says "close or supersede them") | the session that made the decision invalidating it, by raw `bd close` or `bd supersede` | Leaves the frontier | crew doc `defog-session.md` Obsolete the stale |

No grilling is closed `rejected` or `landed`: TUI reject (`z`) takes only an open, unclaimed Signal (`tui.rejectable`).

### Born-with ordering

| Expected edge | Why | D1 | D2 | D3 | D4 | D5 | D6 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `parent` to the Chart | membership: undrains the Chart until resolved | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `drafted-by:` its creator | so the creator's publish takes it (`contract.DraftInstruction`) | ✓ | ✓ | ✓ | ✓ (the Chart) | ✓ (the conflict) | ✓ |
| `blocks` from the defog it depends on | the frontier: takeable only when blockers close (`chart-body.md`) | ✗ (a later optional `bd dep add`) | ✗ | ✗ | ✗ | n/a | n/a |
| conflict `blocks` on this grilling | the conflict leaves `bd ready` until the human answers (ADR 0079) | n/a | n/a | n/a | n/a | ✓ | n/a |
| `discovered-from` its creator | lineage to the bead that graduated it | ✗ | ✗ | ✗ | ✗ | ✗ | ✓ |

### Gaps found

Entries 8, 9 in [Gaps](#gaps).

## research

A defog bead resolved by autonomous agent investigation · owner Nami · leaf (AFK, the loop dispatches it). Source: `GLOSSARY.md` Research; `registry.table` (nami, AFK, not a container).

**Chart membership.** Born as a Draft child of its Chart (`parent` edge), published with its drafter's Drafts. Its Chart cannot drain while it is unclosed (Drafts count as unclosed); when it closes, the Chart's Decisions-so-far gains its line (`defog-session.md` step 6) and the drained-Chart judge reads it as a closed defog child (`promote-or-stamp.md`, `speccing.md`). Source: `GLOSSARY.md` Chart and Fog; `contract.DraftInstruction`.

### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| crewmate session | D1 Nami's Survey charting session, `charting.md` step 5 (raw `bd create`, `bd dep add` in step 6) | Nami, with the human | `parent` to the Chart; `drafted-by:` the Survey. `blocks` only if the session runs the separate `bd dep add` of step 6. | `nami/prompts/charting.md`; crew doc `chart-body.md` Creating a defog bead |
| crewmate session | D2 Nami's grilling session, `grilling.md` step 4 (raw `bd create`) | Nami, with the human | `parent` to the Chart; `drafted-by:` the grilling being resolved. No `blocks`. | `nami/prompts/grilling.md`; crew doc `defog-session.md` Graduate fog |
| crewmate session | D3 Sanji's prototype session, `prototype.md` step 5 (raw `bd create`) | Sanji, with the human | `parent` to the Chart; `drafted-by:` the prototype. No `blocks`. | `sanji/prompts/prototype.md` |
| crewmate session | D4 Nami's promote-or-stamp session on a drained Chart, PROMOTE or ESCALATE (raw `bd create`; ESCALATE files exactly one grilling) | Nami, AFK through the loop | `parent` to the Chart; `drafted-by:` the Chart itself. No `blocks`. | `nami/prompts/promote-or-stamp.md`; `nami.Nami.Reconcile` via `reconcileChart` |

No TUI key, CLI verb, reconcile leg, loop step or doctor row creates a defog bead (searched `tui/`, `main.go`, `luffy/`, `doctor*.go`); the human's only handle is launching the session that resolves one.

The research session itself files nothing: `nami/prompts/research.md` has no `bd create`.

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| completed | the agent's last line, which must start `Wrote docs/research/` (e.g. `Wrote docs/research/<slug>.md: <summary>`) | Nami's research run, through the loop; Go closes, the agent never does | One doc under `docs/research/` committed and integrated onto `chart/<id>`; the close is `Open()`-guarded | `nami.runResearch`, `nami.researchDocPointer`; `nami/prompts/research.md` |
| withdrawn | `withdrawn` | the human's Withdraw of the Chart's chain head (TUI `X`, `mugicrew withdraw`): the defog bead is in the head's unclosed subtree | Closed leaves first with the rest of the subtree | `withdraw.Withdraw`, `bd.WithdrawSet` |
| cancelled | `cancelled: <initiative>` | the human's Cancel of the owning Initiative | as withdrawn | `withdraw.Cancel`, `bd.CancelSet` |
| obsoleted or superseded | none prescribed (the doc says "close or supersede them") | the session that made the decision invalidating it, by raw `bd close` or `bd supersede` | Leaves the frontier | crew doc `defog-session.md` Obsolete the stale |

A final line without the pointer (for example "Unable to find primary sources") closes nothing and leaves the bead open for re-dispatch (`nami.runResearch`).

### Born-with ordering

| Expected edge | Why | D1 | D2 | D3 | D4 |
| --- | --- | --- | --- | --- | --- |
| `parent` to the Chart | membership: undrains the Chart until resolved | ✓ | ✓ | ✓ | ✓ |
| `drafted-by:` its creator | so the creator's publish takes it | ✓ | ✓ | ✓ | ✓ (the Chart) |
| `blocks` from the defog it depends on | the frontier (`chart-body.md`) | ✗ (a later optional `bd dep add`) | ✗ | ✗ | ✗ |
| `discovered-from` its creator | lineage to the bead that graduated it | ✗ | ✗ | ✗ | ✗ |

### Gaps found

Entries 8, 9, 10 in [Gaps](#gaps).

## prototype

A defog bead resolved by a high-fidelity prototype that validates interface design · owner Sanji · leaf (HITL). Source: `GLOSSARY.md` Prototype; `registry.table` (sanji, HITL, not a container).

**Chart membership.** Born as a Draft child of its Chart (`parent` edge), published with its drafter's Drafts. Its Chart cannot drain while it is unclosed (Drafts count as unclosed); when it closes, the Chart's Decisions-so-far gains its line (`defog-session.md` step 6) and the drained-Chart judge reads it as a closed defog child (`promote-or-stamp.md`, `speccing.md`). Source: `GLOSSARY.md` Chart and Fog; `contract.DraftInstruction`.

### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| crewmate session | D1 Nami's Survey charting session, `charting.md` step 5 (raw `bd create`, `bd dep add` in step 6) | Nami, with the human | `parent` to the Chart; `drafted-by:` the Survey. `blocks` only if the session runs the separate `bd dep add` of step 6. | `nami/prompts/charting.md`; crew doc `chart-body.md` Creating a defog bead |
| crewmate session | D2 Nami's grilling session, `grilling.md` step 4 (raw `bd create`) | Nami, with the human | `parent` to the Chart; `drafted-by:` the grilling being resolved. No `blocks`. | `nami/prompts/grilling.md`; crew doc `defog-session.md` Graduate fog |
| crewmate session | D3 Sanji's prototype session, `prototype.md` step 5 (raw `bd create`) | Sanji, with the human | `parent` to the Chart; `drafted-by:` the prototype. No `blocks`. | `sanji/prompts/prototype.md` |
| crewmate session | D4 Nami's promote-or-stamp session on a drained Chart, PROMOTE or ESCALATE (raw `bd create`; ESCALATE files exactly one grilling) | Nami, AFK through the loop | `parent` to the Chart; `drafted-by:` the Chart itself. No `blocks`. | `nami/prompts/promote-or-stamp.md`; `nami.Nami.Reconcile` via `reconcileChart` |

No TUI key, CLI verb, reconcile leg, loop step or doctor row creates a defog bead (searched `tui/`, `main.go`, `luffy/`, `doctor*.go`); the human's only handle is launching the session that resolves one.

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| completed | `prototyped: <one-line verdict>` | Sanji's prototype session, after `mugicrew integrate` exits 0 (the human launches it with the TUI launch key) | Decisions committed on `effort/<id>` and integrated onto `chart/<id>`; the throwaway stays on `refs/mugicrew/prototypes/<chart-id>` and never merges; a Decisions-so-far line appended to the Chart | `sanji/prompts/prototype.md` step 6; `sanji.Sanji.Launch`; `GLOSSARY.md` Prototype Notes |
| withdrawn | `withdrawn` | the human's Withdraw of the Chart's chain head (TUI `X`, `mugicrew withdraw`): the defog bead is in the head's unclosed subtree | Closed leaves first with the rest of the subtree | `withdraw.Withdraw`, `bd.WithdrawSet` |
| cancelled | `cancelled: <initiative>` | the human's Cancel of the owning Initiative | as withdrawn | `withdraw.Cancel`, `bd.CancelSet` |
| obsoleted or superseded | none prescribed (the doc says "close or supersede them") | the session that made the decision invalidating it, by raw `bd close` or `bd supersede` | Leaves the frontier | crew doc `defog-session.md` Obsolete the stale |

A withdraw or cancel of a prototype whose throwaway tree holds work needs `--discard` (`withdraw/inventory.go` counts the throwaway tree).

### Born-with ordering

| Expected edge | Why | D1 | D2 | D3 | D4 |
| --- | --- | --- | --- | --- | --- |
| `parent` to the Chart | membership: undrains the Chart until resolved | ✓ | ✓ | ✓ | ✓ |
| `drafted-by:` its creator | so the creator's publish takes it | ✓ | ✓ | ✓ | ✓ (the Chart) |
| `blocks` from the defog it depends on | the frontier; a prototype that needs a research fact is `chart-body.md`'s own example | ✗ (a later optional `bd dep add`) | ✗ | ✗ | ✗ |
| `discovered-from` its creator | lineage to the bead that graduated it | ✗ | ✗ | ✗ | ✗ |

### Gaps found

Entries 8, 9 in [Gaps](#gaps).

## spec

A closed record produced by a defogged Chart: the plan, sliced into Builds; its body is never edited · owner Zoro (the body is authored by Nami's speccing session) · leaf. Sources: `GLOSSARY.md` Spec; `registry` kind table (`contract.KindSpec`: owner zoro, AFK, not a container).

### Creation doors

All three doors create the Spec as a Draft (by the Chart, Signal or pivot), published by the creator's close or clean-exit publish.

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| reconcile | D1 the loop-dispatched `mugicrew reconcile <chart>` once the Chart is drained, stamped and landed | the loop (Nami) | `caused-by` the Chart; `parent` the Chart's Initiative while unclosed; `drafted-by` the Chart; `context:` and `repo:` copied; placeholder body until the speccing session authors it; Carry of the Chart's blocks dependents to the Spec | `nami.specSuccessor`, `nami.reconcileChart`, `nami.carryOrdering` |
| reconcile | D2 `mugicrew reconcile <signal> --spec <body-file> [--title]` (fast lane) | Luffy's triage session | `caused-by` the Signal; `parent` the Signal's Initiative; `drafted-by` the Signal; `context:`; an `Anchor:` pin written on the bead afterwards; Carry of the Signal's blocks dependents to the Spec | `luffy.SeedSpec`, `luffy.specFastLaneSuccessor`, `luffy.reconcileFastLane`, `bd.Anchor` |
| CLI verb | D3 `mugicrew pivot confirm <pivot> --spec <body-file> [--title] [--carries <ids>]` | Zoro's pivot-triage session | `caused-by` the old Spec's predecessor (its Chart, else its Signal); `discovered-from` the pivot; `parent` the swept Checkpoint's Initiative; `context:` and `repo:` copied; `drafted-by` the pivot; `Anchor:` pin when fast-laned; `supersedes` the old Spec; `discovered-from` each carried Critique; Carry of the Checkpoint's blocks dependents to the new Spec | `zoro.Confirm`, `zoro.spawnCorrectedSpec`, `bd.Supersede` |

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| sliced (succession to the Checkpoint) | none given: bd's default `Closed` | the loop-dispatched Zoro reconcile, by `closeOnPublish`, once Builds exist and the Checkpoint is still open | Carry re-points the Spec's blocks dependents to the Checkpoint; the Build and Errand Drafts publish in the same batch | `zoro.reconcileSpec`, `zoro.carryOrdering` |
| sliced into Errands only | the Spec closes with bd's default `Closed`; its Checkpoint closes `nothing to land: <kind>-only Spec <id>` | as sliced | Carry first; the Checkpoint's branch goes; Errand edges release | `zoro.reconcileSpec`, `bd.NothingToLandReason` |
| superseded (pivot confirm) | bd's own text from `bd supersede <old> --with <new>` | `mugicrew pivot confirm` | the old Spec is normally already closed, so supersede adds the edge only; the Checkpoint and members are swept obsolete | `bd.Supersede`; `zoro.Confirm` |
| withdrawn | `withdrawn`; `cancelled: <initiative>` | the human: `X`, `mugicrew withdraw`, `initiative cancel` (an unclosed Spec with nothing caused by it) | releases held edges | `withdraw.Withdraw`, `withdraw.Cancel` |
| retired Draft | `obsolete: <id> of <parent> was retired` | the dispatch tail when the creating effort's bead was swept | none | `dispatch.effort.retire` |

### Born-with ordering

| Expected edge | Why | D1 | D2 | D3 |
| --- | --- | --- | --- | --- |
| `caused-by` its predecessor | lineage; `CausedBy` walks Spec to Chart for the Checkpoint base | ✓ Chart | ✓ Signal | ✓ the old Spec's predecessor |
| `parent` the predecessor's Initiative | a member chain stays under its Initiative | ✓ | ✓ | ✓ |
| Carry of the predecessor's blocks dependents | dependents wait for the Checkpoint to land (ADR 0085 (mugicrew-blocking-edges-carry…)) | ✓ (after create) | ✓ (after create) | ✓ (from the Checkpoint) |
| `discovered-from` the pivot | the sweep's re-run key | n/a | n/a | ✓ |
| `supersedes` the old Spec | lineage of a corrected Spec | n/a | n/a | ✓ |
| `Anchor:` pin | the Checkpoint cuts from the pinned tip; a Chart-born Spec cuts from the Chart's landed base | n/a | ✓ (after create) | ✓ when fast-laned |
| Draft status | the body or anchor must exist before Zoro slices | ✓ | ✓ | ✓ |

### Gaps found

Entries 3, 4 in [Gaps](#gaps).

### Amendment

A decision appended to a Spec that replaces one FR's mechanism and keeps its intent · owner Zoro (writes it at triage) · not a bead: a marked comment on the Spec. Sources: `GLOSSARY.md` Amendment; `zoro/prompts/pivot.md` Amend.

#### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| crewmate session | D1 Zoro's pivot-triage session, Amend: `bd comments add <Spec> "AMENDMENT (<pivotID>): <FR> — <decision> — rejected: <options>"`, only if no comment starting `AMENDMENT (<pivotID>):` exists | Zoro (with the human) | none: a comment on the Spec; a Critique is refiled copying the FR and decision, and the pivot closes `amended: <gist>` | `zoro/prompts/pivot.md` Amend |

#### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| folded | none: a comment is never closed or removed | Zoro's pivot-triage session, Confirm step 1: a superseding Spec folds every Amendment into its body | the Spec stays append-only; the old Spec's comments stay as lineage | `zoro/prompts/pivot.md` Confirm; `GLOSSARY.md` Amendment |

#### Born-with ordering

| Expected edge | Why | D1 |
| --- | --- | --- |
| none | a comment carries no edge; the pivot it answers is named in its text | n/a |

#### Gaps found

Entry 11 in [Gaps](#gaps).

## checkpoint

A candidate for the Workspace's next mainline state; concretely the build branch `checkpoint/<id>` · owner Zoro (steward, creates it); Jinbe (handler, lands it); the glossary also names Franky for a surgery Checkpoint · container (parents its Build, Review, Critique, Approval and Landing beads). Sources: `GLOSSARY.md` Checkpoint; `registry` kind table (`contract.KindCheckpoint`: owner jinbe, AFK, container, mechanical).

### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| reconcile | D1 the loop-dispatched `mugicrew reconcile <spec>` | the loop (Zoro) | `caused-by` the Spec; `parent` the Spec's Initiative while unclosed; `context:` and `repo:` copied; born open (not a Draft); branch `checkpoint/<id>` cut at the Spec's base; Builds laid after by the slicing session; Carry of the Spec's blocks dependents before the Spec closes | `zoro.checkpointSuccessor`, `zoro.reconcileSpec`, `bd.SpawnSuccessorIfAbsent` |
| reconcile | D2 Jinbe's Rollback hand-off: the successor Checkpoint | the loop (Jinbe) | `caused-by` the Rollback; `parent` the rolled-back Checkpoint's Initiative only while unclosed; `context:` from the deciding bead, else the Rollback; `repo:`; branch cut at the revert commit (or main HEAD when fixing forward); one Build child | `jinbe.handOff` |
| reconcile | D3 the renumber Checkpoint minted on a landed container's pass | the loop (Jinbe for a Checkpoint, Nami for a Chart) | `caused-by` the landed container; `parent` its Initiative; `context:` and `repo:`; branch cut at the fetched Forge main; one Build child | `nami.MintRenumber`, `jinbe.mintRenumber` |
| CLI verb | D4 `mugicrew surgery new --title T [--problem P] [--initiative ID]` (fresh-Checkpoint form) | the human | `parent` the named open Initiative (required when Require Initiatives is on); no `caused-by`; no `context:` or `repo:`; branch cut at main HEAD; the Surgery leaf child, claimed | `main.runSurgeryNew`, `bd.NewSurgery` |
| TUI key | D5 `n` then `u` (New Surgery, fresh-Checkpoint target) | the human | as D4 | `tui.newOpts`, `tui.newSurgery` → `bd.NewSurgery` |

`mugicrew surgery new --checkpoint` and the join target of D5 add a Surgery to an existing Checkpoint (`bd.NewSurgeryOn`) and create no Checkpoint.

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| landed | none given: bd's default `Closed`, after the Landing closes `landed: <sha>` | the loop (Jinbe), `closeIfOpen` | the Landing closes first; MR Watches close `container closed`; a renumber Checkpoint may be minted first | `jinbe.closeLanded`, `bd.CloseLandings` |
| nothing to land | `nothing to land: <kind>-only Spec <id>` | the loop-dispatched Zoro reconcile | Carry first; the branch goes; the Spec closes after | `zoro.reconcileSpec`, `bd.NothingToLandReason` |
| obsoleted (pivot confirmed) | `obsolete: Spec <old> superseded by <new> (pivot <p> confirmed)` | `mugicrew pivot confirm`, forced | every open member except an MR Watch closes with the same reason; open sibling pivots close `superseded: <pivot>`; Carry to the new Spec first | `bd.ObsoleteSuperseded`, `zoro.closeOut` |
| obsoleted (pivot escalated) | `obsolete: Spec <old> escalated to Chart <new> (pivot <p> escalated)` | `mugicrew pivot escalate`, forced | as confirmed, Carry to the fresh Chart | `bd.ObsoleteEscalated`, `zoro.closeOut` |
| abandoned (surgery) | `abandoned: surgery ended with no commits` | Franky's `mugicrew surgery end`, when the Surgery was the only member and nothing is ahead of mainline | the Surgery closes with the same reason | `franky.EndSurgery` |
| withdrawn | `withdrawn`; `cancelled: <initiative>` | the human: `X`, `mugicrew withdraw`, `initiative cancel` (an unclosed Checkpoint with nothing caused by it) | releases held edges; branch cleanup | `withdraw.Withdraw`, `withdraw.Cancel` |

### Born-with ordering

| Expected edge | Why | D1 | D2 | D3 | D4–D5 |
| --- | --- | --- | --- | --- | --- |
| `caused-by` its predecessor | `SpecOf` finds the Spec the Checkpoint serves, through a Rollback's `related` target for a successor | ✓ Spec | ✓ Rollback | ✓ landed container | ✗ by design: a surgery Checkpoint has no Spec, so a pivot raised on it errors in `bd.RaisePivot` |
| `parent` the predecessor's Initiative | a member chain stays under its Initiative | ✓ | ✓ while unclosed | ✓ | ✓ when named |
| `context:` label | routes the Checkpoint to its declared context | ✓ | ✓ | ✓ | ✗ |
| `repo:` stamp | names the child repo a nested Checkpoint was cut in | ✓ | ✓ | ✓ | ✗ |
| Carry of the predecessor's blocks dependents | dependents wait for the Checkpoint to land (ADR 0085 (mugicrew-blocking-edges-carry…)) | ✓ (from the Spec) | n/a | n/a | n/a |
| Draft status | n/a: a Checkpoint is born open and its Builds are the Drafts | n/a | n/a | n/a | n/a |

### Gaps found

Entries 3, 6, 12 in [Gaps](#gaps).

### Renumber Checkpoint

A Spec-less Checkpoint filed on a landed gitlab container's reconcile pass when that container is the later side of a numbered-file clash · owner Jinbe · container (kind:checkpoint), caused by the landed container, with one Build member. Sources: `GLOSSARY.md` Renumber Checkpoint; crew doc `sequence-renumber.md`.

#### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| reconcile | D1 `nami.MintRenumber` on a landed Chart's pass, before its Landings close and its Spec spawns | the loop (Nami) | `caused-by` the Chart; `parent` its Initiative; `context:`, `repo:`; branch cut at the fetched Forge main; one Build | `nami.MintRenumber`, `nami.mintRenumber` |
| reconcile | D2 `jinbe.mintRenumber` on a landed Checkpoint's pass, before it closes | the loop (Jinbe) | `caused-by` the Checkpoint; otherwise as D1 | `jinbe.mintRenumber`, `nami.MintRenumber` |

#### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| landed | as a Checkpoint: bd's default `Closed` after the Landing's `landed: <sha>` | the loop (Jinbe) | runs the full Review, Approval, Landing and Forge merge path | `GLOSSARY.md` Renumber Checkpoint; `jinbe.closeLanded` |
| other | as a Checkpoint: obsoleted, withdrawn | as a Checkpoint | as a Checkpoint | the Checkpoint closure table above |

#### Born-with ordering

| Expected edge | Why | D1 | D2 |
| --- | --- | --- | --- |
| `caused-by` the landed container | the graph alone recognises a renumber Checkpoint: caused by a Chart or Checkpoint where an ordinary one is caused by a Spec | ✓ | ✓ |
| `parent` the container's Initiative | stays in the Initiative while unclosed | ✓ | ✓ |
| one Build child | the renumber and reference sweep | ✓ | ✓ |
| `context:` and `repo:` | route and nest as the container did | ✓ | ✓ |

#### Gaps found

No entry in [Gaps](#gaps) names this variant; the Checkpoint entries cover its doors.

## build

A unit of implementation work sliced from the Spec: a slice inside a Checkpoint · owner Franky · leaf (member of a Checkpoint). Sources: `GLOSSARY.md` Build; `registry` kind table (`contract.KindBuild`: owner franky, AFK, not a container).

### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| crewmate session | D1 Zoro's slicing session: raw `bd create --type task --labels kind:build,drafted-by:<Spec> -s deferred --no-inherit-labels --parent <checkpointID>`, one per Build missing from `bd children` | Zoro | `parent` the Checkpoint; a Draft by the Spec; `blocks` edges between Builds added afterwards by `bd dep add <blocked> <blocker>` | `zoro/prompts/slicing.md` Lay each Build; `zoro.reconcileSpec` |
| reconcile | D2 Jinbe's Rollback hand-off Build ("Fix what <target> broke on main") | the loop (Jinbe) | `parent` the successor Checkpoint; open, not a Draft; no `blocks` | `jinbe.handOff` → `bd.SpawnChildIfAbsent` |
| reconcile | D3 the renumber Build ("Renumber <file> and sweep its references") | the loop (Jinbe, Nami) | `parent` the renumber Checkpoint; open, not a Draft; no `blocks` | `nami.MintRenumber` → `bd.SpawnChildIfAbsent` |

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| built | none given: bd's default `Closed` | Franky's loop-dispatched build session, in the effort tail, after integrate | none: Jinbe spawns the Review once the Checkpoint drains | `franky.finish`, `franky.runImplement` |
| pivoted | `pivoted: <pivot ids>` | Franky's build session, via `bd.RaisePivot` in the publish batch | publishes the Pivot Drafts; the Checkpoint gains `blocks` edges to each pivot, each pivot `related` to the Spec, so the Checkpoint freezes | `bd.RaisePivot`, `bd.GateEdges`; `franky/prompts/build.md` |
| obsoleted (pivot confirmed or escalated) | `obsolete: Spec <old> superseded by <new> (pivot <p> confirmed)` or `obsolete: Spec <old> escalated to Chart <new> (pivot <p> escalated)` | `mugicrew pivot confirm` / `escalate`, forced | Drafts a swept member's effort created close `obsolete: <id> of <parent> was retired` | `zoro.closeOut`, `dispatch.effort.retire` |
| container landed | `obsolete:<sp>container landed<sp>; carried by <signal>` (`<sp>` is a space: the reason is `bd.ContainerLandedReason` + `; carried by ` + the Signal) | the dispatch backstop, when integrate is refused because the Checkpoint already landed | the stranded-commits Signal is filed (the signal section's D11); the `effort/<id>` branch is kept | `session.strandedSignal`, `bd.ContainerLandedReason` |
| cancelled | `cancelled: <initiative>` | the human: `initiative cancel` sweeps the whole member tree | held edges released | `withdraw.Cancel` |
| (not built) | none: the Build stays open and the backstop adds the label `paused:not-built` after a `Not built: <why>` comment | the effort backstop | none | `session.pauseNotBuilt`, `bd.PausedNotBuilt`; `franky/prompts/build.md` |

### Born-with ordering

| Expected edge | Why | D1 | D2 | D3 |
| --- | --- | --- | --- | --- |
| `parent` its Checkpoint | a Build is a member; Jinbe drains the Checkpoint on its members | ✓ | ✓ | ✓ |
| `blocks` between dependent Builds | only `blocks` gates `bd ready`, so the edges give the frontier its order | ✓ (separate `bd dep add` after each create) | n/a (one Build) | n/a (one Build) |
| Draft status | the whole slice, its edges and the Spec's close become visible in one batch | ✓ | ✗ (open at birth) | ✗ (open at birth) |

### Gaps found

Entries 3, 6, 13 in [Gaps](#gaps).

## review

One review pass over a Checkpoint's built slice, at the Checkpoint branch tip · owner Usopp · leaf, a Checkpoint member (`GLOSSARY.md` Review; `registry.go` owner table; `bd.IsMember`).

### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| loop | D1 Jinbe's Checkpoint reconcile spawns a bare Review once the members are drained and no Approval is at head | Jinbe, each tick | parent → Checkpoint only; no `Commit:` pin in the body (Usopp pins `git.Head` himself) | `jinbe.spawnReviewIfDue`, `jinbe.reviewSuccessor`, `bd.SpawnChildIfAbsent`, `bd.childCreateArgs`, `bd.ReviewDue` |

No crew doc or prompt prescribes a raw `bd create` of a Review.

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| completed, clean | none: empty reason, which bd records as `Closed` | Usopp's Review session tail (crewmate session, loop-dispatched) | With zero open Critiques and zero open pivots: open Approvals pinned to another commit are closed first, then the Approval is spawned, then the Review's close and the agent's Critique Drafts publish together | `usopp.approveIfClean`, `usopp.obsoleteStaleApprovals`, `bd.CloseOnPublish`, `bd.DefaultCloseReason` |
| completed, with Critiques | none: `Closed`, the same as clean | Usopp's Review session tail | The Critique Drafts publish with the close; no Approval is spawned; the open Critiques undrain the Checkpoint | `usopp.approveIfClean`, `bd.IsMember` |
| pivoted | `pivoted: <pivot ids>` | Usopp's Review session tail, when a pivot is discovered-from the Review | Checkpoint blocks → pivot and pivot related → Spec edges publish with the close; no Approval | `usopp.runReview`, `bd.RaisePivot`, `bd.GateEdges` |
| obsolete (Spec swept) | `obsolete: Spec <old> superseded by <new> (pivot <p> confirmed)` or `obsolete: Spec <old> escalated to Chart <new> (pivot <p> escalated)` | Zoro's pivot triage session, through the CLI verbs `mugicrew pivot confirm` / `pivot escalate`, force-closing every open member | The Checkpoint closes with the same reason; a Review born after the sweep is force-closed by the loop with the Checkpoint's reason (`sweep.straggler`) | `zoro.closeOut`, `bd.ObsoleteSuperseded`, `bd.ObsoleteEscalated`, `loop.retireStragglers` |

No outcome obsoletes a Review whose Checkpoint branch moved past the commit it was reviewing.

### Born-with ordering

| Expected edge | Why | D1 |
| --- | --- | --- |
| parent → Checkpoint | Membership: an open Review keeps the Checkpoint undrained, so the land waits (`bd.MembersDrainedOver`) | ✓ |
| `Commit:` pin of the reviewed commit (body line, not an edge) | `GLOSSARY.md` says a Review is pinned to the commit it reviewed | ✗ |

### Gaps found

Entries 14, 15 in [Gaps](#gaps).

## critique

A finding raised against the work, fixed test-first · owner Franky · leaf, a Checkpoint member (`GLOSSARY.md` Critique; `bd.IsMember`). An offshoot of a Build, Review, Landing or Approval, and of a pivot when it refiles the pivot's FR.

### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| crewmate session | D1 Franky's Build/Critique session files a defect beyond its slice with a raw `bd create` (Draft by the Build) | Franky | parent → Checkpoint | `franky/prompts/build.md`, crew doc `checkpoint-rules.md` "Filing a Critique" |
| crewmate session | D2 Usopp's Review session files one Critique per confirmed blocking finding, plus one aggregate nits-and-smells Critique when it filed a blocking one (raw `bd create`, Draft by the Review) | Usopp | parent → Checkpoint | `usopp/prompts/review.md`, crew doc `checkpoint-rules.md` |
| crewmate session | D3 Zoro's pivot triage session refiles the unfinished FR on dismiss or amend (raw `bd create` unless one is already discovered-from the pivot) | Zoro | parent → Checkpoint; discovered-from pivot | `zoro/prompts/pivot.md` "Dismiss" |
| crewmate session | D4 Usopp's Forge Review session files an actionable Merge Request note (raw `bd create` of `kind:critique` on a Checkpoint, Draft by the Forge Review) | Usopp | parent → Checkpoint; discovered-from the Forge Review | `usopp/prompts/forge-review.md`, `usopp.forgeArgs.FiledKind` |
| loop | D5 Jinbe's gated land files a Critique when the pinned nix build fails, carrying `Base:`/`Tip:` pins | Jinbe | parent → Checkpoint; discovered-from the Landing | `jinbe.gateOutcome`, `jinbe.gateCritiqueBody` |
| loop | D6 Jinbe's Forge pipeline mirror files a Critique when the Merge Request pipeline fails after one retry | Jinbe | parent → Checkpoint; discovered-from the Landing | `jinbe.pipelineFailed`, `jinbe.fileCritique` |
| CLI verb | D7 `mugicrew approval rework <approval> --critique <file>…` files the rework Critiques as Drafts of the Approval and publishes them with the Approval's close | Luffy's HITL approval session, on the human's OK | parent → Checkpoint | `bd.ReworkApproval`, `luffy/prompts/approval_hitl_tail.md` |
| reconcile | D8 `mugicrew reconcile <signal> --checkpoint <cp> --critique <file>` files a Signal's reroute Critique (Draft by the Signal) | Luffy's triage session | parent → host Checkpoint; caused-by the Signal; discovered-from its pivot (the Ask) or the `--merge-into` target | `luffy.SeedReroute`, `bd.CreateDraft` |
| CLI verb | D9 `mugicrew pivot raise <errand> --critique <file>` files an Errand's Critique (Draft by the Errand) | Brook's Errand session | parent → the held Checkpoint; discovered-from the Errand; discovered-from its pivot or merge target when paired | `luffy.RaiseFromErrand`, `bd.CreateRaiseDraft` |

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| completed | none: `Closed` | Franky's loop-dispatched session tail, no Rationale comment | The Checkpoint drains once no member is open; a Critique with `Source:` lines gets a close-out reply and resolve on its Merge Request thread | `franky.finish`, `jinbe.closeVerdict`, `jinbe.renderReply` |
| completed, with rationale | `accepted: <rationale>` | Franky's session tail, when the session left a `Rationale:` comment | As above; the reply reads accepted | `franky.finish`, `franky/prompts/build.md` |
| rejected | `rejected: <why it is not needed>` | Franky's session, a raw `bd close` before integrate | Relayed whole to whoever filed it; nothing committed | `franky/prompts/build.md` "A Critique that needs no change", `jinbe.verdictPrefixes` |
| accepted, already fixed | `accepted: <what already fixed it>` | Franky's session, a raw `bd close` | As rejected | `franky/prompts/build.md` |
| deferred | `deferred: <why it is valid, and what carries it>` | Franky's session, a raw `bd close` | An offshoot Signal he filed carries the work | `franky/prompts/build.md` |
| pivoted | `pivoted: <pivot ids>` | Franky's session tail, when a pivot is discovered-from the Critique | Gate edges publish with the close | `franky.runAndFinish`, `bd.RaiseIfPivoted`, `bd.RaisePivot` |
| dismissed (Ask Critique) | `dismissed: <pivot id>` | Zoro's pivot triage session, a raw `bd close` on a pivot's Ask Critique | The Ask is dropped, the Signal is never reopened | `zoro/prompts/pivot.md` "Signal raisers" |
| obsolete (Spec swept) | `obsolete: Spec <old> superseded by <new> (pivot <p> confirmed)` or `obsolete: Spec <old> escalated to Chart <new> (pivot <p> escalated)` | Zoro's session, through `mugicrew pivot confirm` / `escalate` | A `--carries` Critique first gets the new Spec or Chart discovered-from it (Carry of the obsoleted Checkpoint's ordering edges runs beside it); Jinbe's MR reply reads deferred if carried, closed if not | `zoro.closeOut`, `zoro.carryCritiques`, `zoro.carryCheckpoint`, `jinbe.sweptReply` |
| obsolete (Draft retired) | `obsolete: <bead> of <checkpoint> was retired` | Dispatch tail | The Draft never publishes | `dispatch.effort.retire` |

### Born-with ordering

| Expected edge | Why | D1 | D2 | D3 | D4 | D5 | D6 | D7 | D8 | D9 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| parent → Checkpoint | Membership: an open Critique undrains the Checkpoint and holds the land (`loop.CheckpointLandHold`) | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| discovered-from the bead that raised it | `GLOSSARY.md` calls it an offshoot, so the graph names what it was found from | ✗ | ✗ | n/a (pivot) | ✓ | ✓ | ✓ | ✗ | n/a (caused-by) | ✓ |
| caused-by the Signal | A reroute's Critique succeeds the Signal | n/a | n/a | n/a | n/a | n/a | n/a | n/a | ✓ | n/a |
| discovered-from the pivot | A refiled FR or an Ask names its pivot | n/a | n/a | ✓ | n/a | n/a | n/a | n/a | ✓ (when paired) | ✓ (when paired) |

### Gaps found

Entries 15, 16, 17 in [Gaps](#gaps).

## conflict

A rebase conflict or numbered-file clash on a Checkpoint, or a human's requested rebase · owner Franky · leaf, a Checkpoint member (`GLOSSARY.md` Conflict; `bd.IsMember`).

### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| loop | D1 Jinbe's pre-review probe finds a collision when it would spawn the Review and files the Conflict instead (body leads `Found before review:`) | Jinbe | parent → Checkpoint; supersedes edge from an unclaimed requested Conflict it replaced | `jinbe.spawnReviewIfDue`, `jinbe.fileConflict` (`preReview`), `jinbe.spawnConflict` |
| loop | D2 Jinbe's Landing hits a rebase conflict or number clash while landing (also the MR-open `conflictArm`), files the Conflict and closes the Landing `obsolete: conflict` | Jinbe | parent → Checkpoint; supersedes edge as D1 | `jinbe.landed`, `jinbe.conflictArm`, `jinbe.fileConflict`, `jinbe.spawnConflict` |
| TUI key | D3 The New menu's `r` row files a requested rebase of the selected open Chart or Checkpoint | the human | parent → Checkpoint | `tui.rebaseRequest`, `tui.canRebase`, `rebase.Request` |
| CLI verb | D4 `mugicrew rebase <id>` files the same requested rebase | the human | parent → Checkpoint | `runRebase`, `rebase.Request` |

All four go through `bd.SpawnChildIfAbsent`, so at most one Conflict is open per Checkpoint whatever its variant. Franky's `conflict.md` prescribes a raw `bd create` only for a pivot.

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| resolved | none: `Closed` | Franky's loop-dispatched session tail | The Checkpoint drains: Jinbe spawns a fresh Review (so the re-landed commit is re-reviewed and the stale Approval is obsoleted), or, for a D1 Conflict, re-probes and then spawns the first Review | `franky.runConflict`, `franky.finish`, `jinbe.spawnReviewIfDue` |
| pivoted | `pivoted: <pivot ids>` | Franky's session tail | Gate edges publish with the close | `franky.runAndFinish`, `bd.RaiseIfPivoted`, `bd.RaisePivot` |
| superseded | `superseded: <conflict title>` (the new Conflict's title, e.g. `Checkpoint <id> conflicts with main`) | Jinbe, loop, only for an open unclaimed requested Conflict when D1 or D2 files a real one | The old Conflict gets a supersedes edge to the new one; a claimed requested one is kept and the new one is swallowed | `jinbe.spawnConflict`, `bd.Supersede` |
| obsolete (Spec swept) | `obsolete: Spec <old> superseded by <new> (pivot <p> confirmed)` or `obsolete: Spec <old> escalated to Chart <new> (pivot <p> escalated)` | Zoro's session, through `mugicrew pivot confirm` / `escalate` | Closes with its Checkpoint | `zoro.closeOut` |

### Born-with ordering

| Expected edge | Why | D1 | D2 | D3 | D4 |
| --- | --- | --- | --- | --- | --- |
| parent → Checkpoint | Membership: an open Conflict undrains the Checkpoint, so the loop stops re-dispatching the failing land (`loop.CheckpointLandHold`) | ✓ | ✓ | ✓ | ✓ |
| supersedes old requested Conflict → new | A real Conflict replaces a human's unclaimed request | ✓ | ✓ | n/a | n/a |
| `Base:` and `Tip:` pins (body lines, not edges) | Franky rebases onto the base and cuts his effort at the tip | ✓ | ✓ | ✓ | ✓ |

### Gaps found

Entries 15, 18 in [Gaps](#gaps).

## approval

The stamp that gates the land · owner Luffy · leaf, one per Checkpoint or Chart (`GLOSSARY.md` Approval; ADR 0388 (a-chart-lands-through-an-approval-carrying-a-chart-packet)).

### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| crewmate session | D1 The tail of Usopp's Review session spawns the Approval when the Review closes clean, carrying the `Commit:` pin of the reviewed commit and the Checkpoint's title | Usopp | parent → Checkpoint only | `usopp.approveIfClean`, `usopp.approvalSuccessor`, `bd.SpawnChildIfAbsent` |
| loop | D2 Nami's Chart step spawns the Approval where it spawned the Landing: a drained, stamped Chart with no approved Approval at its head and no open blocker, titled as the Chart, `Commit: <chart/<id> head>` and `Patch-id:` pins; no Review; none for a Chart with nothing ahead of mainline | Nami | parent → Chart only | `nami.reconcileChart`, `nami.gateChart`, `bd.SpawnChildIfAbsent` |

No crew doc or prompt prescribes a raw `bd create` of an Approval.

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| approved | `approved` | Luffy's HITL approval session, a raw `bd close <id> --reason=approved` on the human's explicit OK; or, under auto-approve, Go's AFK arm after the packet check | The sole land trigger: the Checkpoint is `approved`, and Jinbe's Landing is spawned in succession to it | `luffy/prompts/approval_hitl_tail.md`, `luffy.runApproval`, `bd.ApprovedReason`, `bd.Bead.ApprovedClosed`, `jinbe.checkpointApproval` |
| rework | `rework: <critique ids>` | Luffy's HITL session, through the CLI verb `mugicrew approval rework` | The rework Critiques publish with the close; nothing lands. On a Chart, the findings are new grilling or research defog beads on the Chart, `chart:defogged` is removed, and the Chart re-defogs before a fresh Approval | `bd.ReworkApproval` |
| obsoleted | `Approval obsoleted: pinned commit superseded by advanced HEAD` | Usopp's Review session tail, for an open Approval whose pin is not the reviewed HEAD | A fresh Approval is spawned off the new clean Review. A Chart's open Approval whose pin is no longer the `chart/<id>` head is obsoleted by Nami, which spawns a fresh one at the new head | `usopp.obsoleteStaleApprovals`, `nami.gateChart` |
| obsolete (Spec swept) | `obsolete: Spec <old> superseded by <new> (pivot <p> confirmed)` or `obsolete: Spec <old> escalated to Chart <new> (pivot <p> escalated)` | Zoro's session, through `mugicrew pivot confirm` / `escalate` | Closes with its Checkpoint | `zoro.closeOut` |

A rework or obsoleted Approval carries no ordering edge and lands nothing (`GLOSSARY.md` Approval).

### Born-with ordering

| Expected edge | Why | D1 |
| --- | --- | --- |
| parent → Checkpoint or Chart | Membership: one Approval per container, so the derived `approved` state reads it (`bd.ReviewDueOver`) | ✓ |
| caused-by the clean Review | `GLOSSARY.md`: spawned in succession to that Review, so it certifies that Review's commit | ✗ |
| `Commit:` pin (body line, not an edge) | The land gate compares it to the branch tip (`approvedAtHead`) | ✓ |

### Gaps found

Entry 19 in [Gaps](#gaps).

## forge-review

One pass over a container's unacked Merge Request comments that files the actionable ones and acks every one it read · owner Usopp · leaf, a member of a Checkpoint or a Chart, at most one open per container, AFK (`GLOSSARY.md` Forge Review; `bd.IsMember`).

### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| loop | D1 Jinbe's pending-Landing tick reads the Merge Request and spawns a Forge Review over the notes that pass the pre-filter | Jinbe | parent → container; discovered-from the pending Landing | `jinbe.mirrorMR`, `jinbe.forgeReviewArm` |
| loop | D2 Jinbe's MR Watch tick, between Landings, runs the same arm | Jinbe | parent → container | `jinbe.reconcileMRWatch`, `jinbe.forgeReviewArm` |

No crew doc prescribes a raw `bd create` of a Forge Review.

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| filed | `filed <n>, folded <n>, skipped <n>: read through note <id>` | Usopp's Forge Review session tail, when it filed or folded a note | Its Drafts publish with the priority of the container; a pending Landing closes `obsolete: review comment` if any Draft was filed; Jinbe awards the filed notes later | `usopp.forgeReviewTail`, `contract.ForgeReviewCounts`, `jinbe.awardDue` |
| nothing actionable | `nothing actionable: read <n> notes through <id>` | Usopp's session tail | None | `usopp.forgeReviewTail` |
| obsolete (Spec swept) | `obsolete: Spec <old> superseded by <new> (pivot <p> confirmed)` or `obsolete: Spec <old> escalated to Chart <new> (pivot <p> escalated)` | Zoro's session, through `mugicrew pivot confirm` / `escalate` | Closes with its Checkpoint | `zoro.closeOut` |

### Born-with ordering

| Expected edge | Why | D1 | D2 |
| --- | --- | --- | --- |
| parent → container | Membership: an open Forge Review holds the land and the container's drain | ✓ | ✓ |
| discovered-from the pending Landing | Names the land the notes arrived on | ✓ | n/a (no Landing pending) |

### Gaps found

Entry 20 in [Gaps](#gaps).

## mr-watch

The leaf that reads a container's open Merge Request every paced tick, so a comment left between Landings spawns a Forge Review · owner Jinbe · leaf, a child of a Checkpoint or a Chart but never a member and never undraining it, at most one open per container, AFK and Mechanical (`GLOSSARY.md` MR Watch; `contract.PacedLeaf`; `bd.IsMember`).

### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| loop | D1 Jinbe's Landing tick records the first Merge Request of a container under the gitlab land strategy and spawns the Watch, copying the `MR:` and `MR link:` lines | Jinbe | parent → container only | `jinbe.reconcileLandingOnce`, `jinbe.spawnMRWatch`, `bd.SpawnChildIfAbsent` |

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| container closed | `container closed` | Jinbe, loop: when it lands the container, or on a Watch tick over a closed container with no MR recorded, a non-gitlab strategy, or nothing owed | None | `jinbe.closeMRWatches`, `jinbe.reconcileMRWatch`, `jinbe.lateMRWatch` |
| MR merged | `obsolete: merge request merged` | Jinbe's Watch tick, once nothing is owed | Close-outs are answered before it closes | `jinbe.lateMRWatch`, `jinbe.mrWatchOwes` |
| MR closed | `obsolete: merge request closed` | Jinbe's Watch tick, once nothing is owed | As merged | `jinbe.lateMRWatch` |
| abandoned | `obsolete: close-out abandoned; carried by <signal id>` | Jinbe's Watch tick, 7 days after the container closed with close-outs still failing | Files an unparented Signal discovered-from the Watch first; the Watch keeps its lines | `jinbe.abandonMRWatch`, `bd.SpawnOffshootSignalTitledIfAbsent`, `jinbe.abandonAfter` |

A Spec sweep does not close an MR Watch: `zoro.closeOut` and `loop.retireStragglers` spare it, and it later closes itself.

### Born-with ordering

| Expected edge | Why | D1 |
| --- | --- | --- |
| parent → container | The Watch hangs off its container without being a member | ✓ |
| blocks | None expected: a Watch must never gate or undrain its container | n/a |

### Gaps found

Entry 20 in [Gaps](#gaps).

## pivot

Work on a Checkpoint proves the Spec wrong for one FR · owner Zoro (triages) · leaf, an offshoot that blocks the whole Checkpoint while open (`GLOSSARY.md` Pivot; `bd/pivot.go`).

### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| crewmate session | D1 Franky's Build or Critique session raises a pivot with a raw `bd create` (Draft by the Build), or merges with `bd dep add <pivot> <build> -t discovered-from` | Franky | discovered-from the Build or Critique; no parent. The Checkpoint blocks → pivot and pivot related → Spec edges publish when Go closes the raiser `pivoted:` | `franky/prompts/build.md`, crew doc `checkpoint-rules.md` "Raising a pivot", `bd.RaisePivot` |
| crewmate session | D2 Franky's Conflict session, the same raw create | Franky | as D1, discovered-from the Conflict | `franky/prompts/conflict.md` |
| crewmate session | D3 Usopp's Review session, the same raw create or merge | Usopp | as D1, discovered-from the Review | `usopp/prompts/review.md` |
| reconcile | D4 `mugicrew reconcile <signal> --checkpoint <cp> --pivot <file>` files the Signal's reroute pivot (Draft by the Signal) with the host in `raise_checkpoint` metadata | Luffy's triage session | caused-by the Signal; no parent. The gate and related → Spec edges publish with the Signal's `rerouted:` close | `luffy.SeedReroute`, `session.raiseSession`, `bd.GateEdges` |
| CLI verb | D5 `mugicrew pivot raise <errand> --pivot <file>` files a stuck Errand's pivot (Draft by the Errand) with the host in `raise_checkpoint` metadata | Brook's Errand session | discovered-from the Errand; no parent. The gate publishes with the Errand's raise; the Errand stays open | `luffy.RaiseFromErrand`, `session.raiseSession` |

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| confirmed | `confirmed` | Zoro's triage session, through the CLI verb `mugicrew pivot confirm` (last leg of the sweep) | The corrected Spec is drafted (caused-by the old Spec's Chart, else its Signal; discovered-from the pivot); the old Spec is superseded; the Checkpoint's ordering edges Carry to the new Spec; `--carries` Critiques get the new Spec discovered-from them; every open member and the Checkpoint close obsolete; sibling pivots close superseded | `zoro.Confirm`, `zoro.spawnCorrectedSpec`, `zoro.carryCheckpoint`, `bd.Carry`, `zoro.closeOut` |
| escalated | `escalated` | Zoro's session, through `mugicrew pivot escalate` | A fresh Chart (caused-by the Signal) and its Survey are drafted and its branch cut; a charted old Chart is superseded; ordering edges Carry to the new Chart; members and the Checkpoint close obsolete | `zoro.Escalate`, `zoro.escalatedChartBody`, `zoro.carryCheckpoint` |
| dismissed | `dismissed: <gist>` | Zoro's session, a raw `bd close`, after refiling the FR as a Critique | The Checkpoint unfreezes and Builds resume | `zoro/prompts/pivot.md` "Dismiss" |
| amended | `amended: <gist>` | Zoro's session, a raw `bd close`, after the `AMENDMENT (<pivot>):` comment on the Spec and a refiled Critique | As dismissed; the Spec is not superseded | `zoro/prompts/pivot.md` "Amend" |
| superseded (sibling) | `superseded: <lead pivot id>` | Zoro's sweep, for every other open pivot of the swept Checkpoint | None | `zoro.closeOut` |
| superseded (gate closed) | `superseded: gated Checkpoint <ids> closed` | Loop, for a published pivot whose every gate target is closed | Logged `sweep.straggler` | `loop.retireStragglers` |
| obsolete (Draft retired) | `obsolete: <bead> of <checkpoint> was retired` | Dispatch tail, for a pivot Draft of a swept member | The pivot never publishes | `dispatch.effort.retire` |

No code or prompt writes a `rejected` pivot outcome: dismiss is the Zoro prompt's `dismissed:`.

### Born-with ordering

| Expected edge | Why | D1 | D2 | D3 | D4 | D5 |
| --- | --- | --- | --- | --- | --- | --- |
| discovered-from the raiser | Provenance, and the key Go finds the raise by (`bd.PivotsDiscoveredFrom`) | ✓ | ✓ | ✓ | n/a (caused-by) | ✓ |
| caused-by the Signal | A reroute pivot succeeds the Signal | n/a | n/a | n/a | ✓ | n/a |
| host Checkpoint recorded | Go needs the host to gate it | n/a (the raiser's parent) | n/a | n/a | ✓ (metadata) | ✓ (metadata) |
| Checkpoint blocks → pivot | A live pivot blocks the whole Checkpoint | ✗ at birth, ✓ at publish | ✗ / ✓ | ✗ / ✓ | ✗ / ✓ | ✗ / ✓ |
| pivot related → Spec | Zoro resolves the Spec from it (`bd.PivotSpec`) | ✗ / ✓ | ✗ / ✓ | ✗ / ✓ | ✗ / ✓ | ✗ / ✓ |

### Gaps found

Entries 17, 21 in [Gaps](#gaps).

## landing

One bead per attempt to land a container's branch on mainline, pinned (`Commit: <sha>`) to the base it will land · owner Jinbe (Checkpoint's Landing) and Nami (Chart's Landing creates it; Jinbe runs it) · leaf, AFK Mechanical, no session; at most one open per container (`registry.Specs[KindLanding]`; GLOSSARY.md "Landing").

### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| loop | D1 Checkpoint land gate: a closed Approval at head, members drained, no open blocker spawns the Landing, titled with the Checkpoint's, body `Commit: <approval pin>` | the loop (Jinbe's Checkpoint step) | parent (the Checkpoint); caused-by (the at-head Approval) | `jinbe.reconcileCheckpoint` → `bd.SpawnChildIfAbsent`, `bd.childCreateArgs` |
| loop | D2 Chart land gate: drained and stamped Chart with an approved Approval at the branch head, no open Landing and no open blocker, spawns the Landing with `Commit: <approval pin>`; a Chart with nothing ahead of mainline spawns it directly, with no Approval | the loop (Nami's Chart step) | parent (the Chart); caused-by (the at-head Approval) | `nami.reconcileChart`, `nami.gateChart` → `bd.SpawnChildIfAbsent` |

A branch that advanced past the pin makes a Checkpoint's gate respawn a Landing and a Chart's gate respawn an Approval (GLOSSARY.md "Landing mechanics"). No CLI verb, TUI key or crew-doc `bd create` makes a Landing; the crew prompts only tell Nami to ignore Landing children (`nami/prompts/speccing.md`).

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| landed | `landed: <sha>` | Jinbe via the loop, either on the Landing's own tick or from the container's reconcile (stranded Landing closes first) | Gitlab: stale-pin Signal flagged, final close-out, open MR closed, MR Watches closed `container closed`, renumber Checkpoint minted for a number clash; the container closes after | `jinbe.landed`, `landedClose`, `closeLanded`; `bd.CloseLandings`; `nami.reconcileChart`; GLOSSARY.md "Landing" |
| conflict | `obsolete: conflict` | Jinbe via the loop | Files the container's conflict bead (Checkpoint: Conflict; Chart: Chart-conflict); the container undrains; a gitlab MR is left open | `jinbe.landed`, `conflictArm`, `fileConflict` |
| pending | none (stays open) | Jinbe via the loop | Under gitlab records `MR: !<iid>` and `MR link:`; spawns an MR Watch | `jinbe.reconcileLandingOnce` (`git.ErrLandPending`), `spawnMRWatch` |
| branch advanced | `obsolete: branch advanced without landing` | Jinbe via the loop | A Checkpoint falls back to its Approval gate (new Review); a Chart's gate respawns an Approval, not a Landing | `jinbe.reconcileLandingOnce`, `gatedLand` |
| build failed | `obsolete: build failed` | Jinbe via the loop (local strategy, gated land) | Files a Critique on the Checkpoint, discovered-from the Landing; none if one is open at that base and tip | `jinbe.gateOutcome`, `gatedLand` |
| pipeline failed | `obsolete: pipeline failed` | Jinbe via the loop (gitlab, retried pipeline red) | Checkpoint: Critique discovered-from the Landing. Chart: HITL Errand blocking the Chart | `jinbe.pipelineFailed`, `fileCritique`, `holdErrand` |
| merge request closed | `obsolete: merge request closed` | Jinbe via the loop (gitlab, MR closed on the Forge) | HITL Errand blocking the container: reopen or merge the MR, or withdraw | `jinbe.mirrorMR` (closed arm), `holdErrand` |

No other close path writes a Landing: `mugicrew withdraw` refuses while a Landing is open (`withdraw.landingRefusals`), and a reroute refuses a landing or landed Checkpoint (ADR 0324 (mugicrew-a-reroute-refuses…)).

### Born-with ordering

| Expected edge | Why | D1 | D2 |
| --- | --- | --- | --- |
| parent → container | membership: the Landing is the container's member, at most one open, and the container closes after it (ADR 0145 (a-landing-is-one-pinned…)) | ✓ | ✓ |
| caused-by → at-head Approval | the Landing succeeds the Approval and copies its pin (GLOSSARY.md "Landing mechanics") | ✓ | ✓ (a pin-only Chart has none) |
| blocks | none expected: the container's gate, not an edge, orders it | n/a | n/a |

### Gaps found

Entries 22, 23 in [Gaps](#gaps).

## chart-conflict

The Chart-land counterpart of Conflict: a collision stopping a Chart's Landing, or a human's requested rebase of the Chart branch; three variants (rebase-conflict, number-clash, requested) · owner Nami · leaf, AFK, member of the Chart; at most one open per Chart (`registry.Specs[KindChartConflict]`; GLOSSARY.md "Chart-conflict").

### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| loop | D1 A Chart Landing's rebase conflict or number clash files `<Chart title> conflicts with main`, body carries the base, tip and variant pins | the loop (Jinbe's Landing step) | parent (the Chart) | `jinbe.fileConflict`, `spawnConflict`, `nami.ChartConflictBody` |
| CLI verb | D2 `mugicrew rebase <chart-id>` files the requested variant, pinned at the current mainline | the human | parent (the Chart) | `rebase.Request`, `runRebase` (`main.go`) |
| TUI key | D3 `n` then `r` on a selected open Chart files the same requested variant | the human | parent (the Chart) | `tui.Model.newOpts`, `tui.rebaseRequest`, `rebase.Request` |

All three go through `bd.SpawnChildIfAbsent`, so an open Chart-conflict of any variant swallows a second (`rebase.Request` reports `OpenConflict` or `AlreadyRequested`). When a Landing conflicts while an unclaimed requested one is open, `spawnConflict` closes the request first and links the new one with bd's supersede edge. Nami's escalation files a grilling Draft, not a Chart-conflict. No crew doc or prompt prescribes a raw `bd create` of one.

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| completed | the agent's text after `Resolved:` (free-form) | Nami's AFK arm: Go closes on publish after the post-resolve check passes and `mugicrew integrate` exits 0 | The Chart re-drains and spawns a fresh Landing; no re-review | `nami.runChartConflict`, `bd.CloseOnPublish`; GLOSSARY.md "Chart-conflict" |
| superseded | `superseded: <title>` (title of the Chart-conflict that replaces it) | Jinbe via the loop, when a Landing's conflict meets an unclaimed requested one | The old bead links to the new one by bd's supersede edge | `jinbe.spawnConflict`; ADR 0261 (a-requested-rebase-defers…) |
| moot | `moot: container already on base <base>` | the backstop after an off-pin integrate, when the Chart already descends from the Base | effort branch and tree torn down | `session.Backstop` (off-pin arm) |
| withdrawn | `withdrawn` | the human via `mugicrew withdraw` of the Chart (swept as a member) | none beyond the Chart's withdrawal | `withdraw.run` |

An escalation leaves the bead open: a grilling Draft, drafted-by the Chart-conflict, is published with a blocks edge that holds the conflict until the human closes the grilling (`nami.escalateChartConflict`).

### Born-with ordering

| Expected edge | Why | D1 | D2 | D3 |
| --- | --- | --- | --- | --- |
| parent → Chart | member of the Chart, so the open child undrains it and the loop stops re-dispatching the failing land (ADR 0011 (mugicrew-chart-conflict-policy)) | ✓ | ✓ | ✓ |
| discovered-from or caused-by → the Landing that hit it | to tell which land attempt it came from (cf. a gate Critique's discovered-from the Landing) | ✗ | n/a (human-filed) | n/a (human-filed) |
| blocks | none at birth: an escalation adds a grilling that blocks it at publish | n/a | n/a | n/a |

### Gaps found

Entries 23, 24 in [Gaps](#gaps).

## rollback

A human's request to undo one landed Checkpoint that broke main, and recover its work; a successor Checkpoint (same Spec, caused-by the Rollback) carries the fix · owner Jinbe · leaf, AFK Mechanical, unparented, `related` to its target Checkpoint (`registry.Specs[KindRollback]`; GLOSSARY.md "Rollback").

### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| TUI key | D1 History tab `u` ("undo") on a rollbackable Checkpoint land, then the Rollback form (title, breakage evidence) | the human | related (→ target Checkpoint), added by a second `bd dep add` after the create; copies the target's `context:` and `repo:` when nested | `tui.rollbackForm`, `bd.NewRollback`, `bd.newRelated` |

The key is refused for a plain commit, a revert, a Chart land, a Spec anchor, a pending Rollback and an already rolled-back land (`tui.refusal`). There is no CLI verb, and agents never file one (GLOSSARY.md "Rollback"). No crew doc or prompt prescribes a raw `bd create` of one.

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| completed (handed off) | none (bd's default) | Jinbe via the loop, once the successor exists | Revert commit on main (`Reverts-Checkpoint:` trailer), successor Checkpoint cut at the revert and seeded with the reapply, with one Build carrying the evidence; under gitlab a still-open revert MR is closed first | `jinbe.reconcileRollbackOnce`, `handOff`, `closeRollback` |
| fix-forward | `fix-forward: revert conflicted` | Jinbe via the loop | No revert; successor cut at main HEAD with no seed, its Build also carries the conflicted paths | `bd.FixForwardReason`, `jinbe.handOff`, `fixForwardRevert` |
| invalid | `invalid: no related Checkpoint`, `invalid: <id> not landed`, `invalid: <id> has no land trailer` | Jinbe via the loop | Nothing written to main; a recorded revert MR is closed | `jinbe.reconcileRollbackOnce`, `closeInvalidRollback` |
| abandoned | `abandoned: revert MR closed` | Jinbe via the loop (gitlab, a human closed the revert MR, no successor yet) | No successor | `jinbe.revertMRArms` |
| withdrawn | `withdrawn` | the human via `mugicrew withdraw` (a chain head with nothing caused by it) | Refused while a recorded revert MR may be open | `withdraw.plan`, `landingRefusals` |

An already-reverted target skips the revert and still hands off (`git.Revert` is idempotent on its trailer). A merged revert MR whose trailer is missing, and a red revert pipeline, hold on an Errand that blocks the Rollback and leave it open (`jinbe.holdTrailerless`, `revertPipelineFailed`).

### Born-with ordering

| Expected edge | Why | D1 |
| --- | --- | --- |
| related → target Checkpoint | names the one Checkpoint to undo; without it Jinbe closes the Rollback `invalid: no related Checkpoint` (ADR 0047 (mugicrew-rollback-kind-and-entry)) | ✓ (second write) |
| parent | none: a Rollback is unparented; the successor joins the target's open Initiative | n/a |
| blocks | none expected | n/a |

The successor Checkpoint is born caused-by the Rollback, parented to the target's Initiative while unclosed (`jinbe.handOff`, ADR 0048 (mugicrew-rollback-successor)).

### Gaps found

Entries 25, 26 in [Gaps](#gaps).

## surgery

A HITL session in which a human dictates small code changes on a Checkpoint and Franky writes them · owner Franky · leaf, a member of any open Checkpoint, at most one open per Checkpoint (GLOSSARY.md "Surgery"; `registry` Surgery entry: HITL, not a container).

### Creation doors

Surgery has two shapes, each its own door per surface. A **fresh** door files a surgery Checkpoint (an unparented `kind:checkpoint` epic, or an Initiative member, with no `caused-by` and no Spec, `checkpoint/<id>` cut at main HEAD) and the Surgery as its child. A **join** door files the Surgery as a child of an existing open Checkpoint with no Checkpoint create and no branch cut. Every door claims the Surgery at filing, so it starts idle (`in_progress`) and never enters `bd ready`. None is a Draft.

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| TUI key | D1 `n` then `u`, New Surgery form, a "new Checkpoint" row or a "new in Initiative" row (fresh); files, then launches the first session | human | Checkpoint: `parent` Initiative when one is picked, else none; no `caused-by`, no `blocks`. Surgery: `parent` the new Checkpoint | `tui.openSurgery`, `tui.fileSurgery`, `tui.surgeryRows`, `bd.NewSurgery`; ADR 0162 (mugicrew-surgery-on-an-existing…); GLOSSARY.md "Surgery" Operations |
| TUI key | D2 `n` then `u`, New Surgery form, an existing Checkpoint row (join); files, then launches the first session | human | Surgery: `parent` the Checkpoint; nothing else. Refused with a `bd.JoinRefusal` (reason shown as a status line) when the Checkpoint is closed, has an open Landing, an open Surgery, an open Conflict, a base/tip Critique, or an open Pivot | `tui.fileSurgery`, `bd.NewSurgeryOn`, `bd.JoinVerdict`; ADR 0163 (mugicrew-a-checkpoint-takes-many…) |
| CLI verb | D3 `mugicrew surgery new --title T [--problem P] [--initiative ID]` (fresh); prints the id, launches nothing | human | as the fresh TUI door | `main.runSurgeryNew`, `bd.NewSurgery`; ADR 0095 (mugicrew-surgery-tui-and-cli…) |
| CLI verb | D4 `mugicrew surgery new --title T --checkpoint ID` (join); prints the id, launches nothing | human | as the join TUI door | `main.runSurgeryNew`, `bd.NewSurgeryOn` |

No crewmate session files a Surgery. Franky's surgery prompt tells him "only a human files a Surgery" and to file an offshoot Signal for an oversized change instead (`franky/prompts/surgery.md`); crew doc signal-filing.md names a Surgery as something a crewmate never files. The loop, reconcile and doctor fix never file one. **Drafts on a clean exit:** a Surgery session may file offshoot Signal Drafts (`discovered-from` the Surgery); `session.PublishDrafts` publishes them on a clean exit and, since a Surgery is neither Signal, Errand nor Survey, closes nothing (`session.sessionPlan`). A drop deletes them (`session.DropDrafts`).

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| completed | none (empty reason) | Franky in the Surgery session, on the human's explicit word, CLI verb `mugicrew surgery end <id>`; refused unless the Surgery is open and under a Checkpoint. Only when this Surgery's `Surgery: <id>` trailered squashes are on the Checkpoint | Integrates `effort/<id>` first (a failure closes nothing); branch left; Jinbe spawns the Review once the Checkpoint drains. The Checkpoint stays open | `franky.EndSurgery`, `main.runSurgeryEnd`; `franky/prompts/surgery.md` "End"; GLOSSARY.md "Surgery" Operations |
| abandoned | `abandoned: surgery ended with no commits` | Franky, same door, when the Surgery has no trailered squashes | Also closes its Checkpoint with the same reason, but only when nothing is ahead of main and the Surgery is the Checkpoint's only child | `franky.EndSurgery` |
| withdrawn | `withdrawn` | Human, CLI verb `mugicrew withdraw <checkpoint>` (or the TUI withdraw): the open Surgery is in the unclosed subtree of the withdrawn Checkpoint. Refused while a session is open on it | Closed leaves first with its Checkpoint; branch and tree cleaned by `withdraw.cleanup`. A Surgery cannot be withdrawn alone (not a chain head) | `withdraw.Withdraw`, `bd.WithdrawSet`, `bd.IsChainHead` |
| cancelled | `cancelled: <initiative>` | Human, `mugicrew initiative cancel` (fresh Surgery whose Checkpoint is an Initiative member) | Closed with the member Checkpoint's subtree | `withdraw.Cancel`, `bd.CancelSet` |

A session **stop** (clean exit that is not an end) and a session **drop** are not closures: the Surgery stays `in_progress` and idle, and `enter` resumes it (`session.settle`, `tui.launchFailed`, the drop path in `tui` actions_io; GLOSSARY.md "Surgery" Notes). A rejected, superseded or obsoleted close does not exist for a Surgery.

### Born-with ordering

| Expected edge | Why | D1 | D2 | D3 | D4 |
| --- | --- | --- | --- | --- | --- |
| Surgery `parent` Checkpoint | Membership: the end rule, the Review spawn and the Critique hold all read the Checkpoint's children | ✓ | ✓ | ✓ | ✓ |
| Checkpoint `parent` Initiative (when filed under one) | The surgery Checkpoint is that Initiative's member; Require Initiatives refuses an empty one | ✓ | n/a | ✓ | n/a |
| No `caused-by` on the Checkpoint | A surgery Checkpoint has no Spec and no predecessor (Spec: none) | ✓ | n/a | ✓ | n/a |
| Claimed at filing (state, not an edge) | Starts idle, never enters `bd ready` or the loop | ✓ | ✓ | ✓ | ✓ |
| `blocks` against the Checkpoint's Critiques | An open Surgery holds its Checkpoint's Critiques from Franky. Not an edge today: `loop.SurgeryHold` scans for it each tick | ✗ | ✗ | ✗ | ✗ |

Sources: `bd.NewSurgery`, `bd.NewSurgeryOn`, `bd.Claim`; `loop.SurgeryHold`, ADR 0160 (mugicrew-an-open-surgery-holds…); GLOSSARY.md "Surgery".

### Gaps found

Entries 27, 28 in [Gaps](#gaps).

## discussion

A human-started, read-only session with Robin that explains the Workspace and, with the human's yes, drafts Signals and Initiatives · owner Robin · leaf, free-standing and off the spine like an Errand, never a member of a Chart, Checkpoint or Initiative (GLOSSARY.md "Discussion"; `registry` Discussion entry: HITL, `ReadOnly`).

### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| TUI key | D1 `n` then `d`, New Discussion; creates "Discussion <YYYY-MM-DD HH:MM>" open and unparented, then claims and launches Robin | human | none (no parent, no deps); not a Draft | `tui.discussionNew`, `tui.discussionCreated`, `bd.NewDiscussion`; ADR 0194 (mugicrew-a-robin-discussion-is…); GLOSSARY.md "Discussion" Operations |

No CLI verb, reconcile, loop or doctor fix files a Discussion ("only a human launches, resumes or drops one; the loop never does"). Robin's prompt allows only `bd create` for Signal Drafts and Initiative Drafts and says "You create no bead kind but a Signal or an Initiative" (`robin/prompts/discussion.md`). **Drafts on a clean exit:** each Signal Robin files is a Draft `discovered-from` the Discussion (`mugicrew signal offshoot`), and each Initiative she files on the human's yes is a bodyless Draft `discovered-from` it (the initiative section's D3); `session.PublishDrafts` publishes them on a clean exit and closes nothing, since a Discussion is not a Signal, Errand or Survey; the prompt closes the bead (`session.sessionPlan`).

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| completed | none (empty reason) | Robin in the session, on the human's word: `bd update <id> --title "<the topic>" --notes "<summary>"` then `bd close <id>` | Drafts already published by `PublishDrafts` on the clean exit | `robin/prompts/discussion.md` "Done"; GLOSSARY.md "Discussion" Operations |
| dropped | `dropped` | Human, TUI `x` on the row: `tui.discussionDrop` (no session) or the live-session drop in actions_io | Unpublished Drafts deleted first (`session.DropDrafts`) | `tui.discussionDrop`; ADR 0194 (mugicrew-a-robin-discussion-is…) |

An early `/exit` is not a closure: the Discussion stays open and claimed for a rejoin (`session.settle`). A Discussion is not a chain head, so `withdraw` refuses it, and it is no Initiative member, so `cancel` never reaches it (`bd.IsWithdrawable`).

### Born-with ordering

| Expected edge | Why | D1 |
| --- | --- | --- |
| none | Free-standing and off the spine; its Signals point back at it with `discovered-from`, so the edge is theirs | n/a |

Sources: `bd.NewDiscussion`, `bd.unparentedArgs`; GLOSSARY.md "Discussion".

### Gaps found

Entry 29 in [Gaps](#gaps).

### Retrospective

A Discussion Robin runs on a finished target (a Signal, Chart, Spec, Checkpoint or Initiative whose spine has no open bead): she reads the spine's sessions and drafts Signals from recurring patterns · owner Robin · leaf, an unparented Discussion titled "Retrospective <target title>" (GLOSSARY.md "Retrospective").

#### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| TUI key | D1 `r` on a row of an eligible kind, behind a confirm dialog and a closed-spine check; files "Retrospective <target title>" with `Target:` and `Spine:` key lines, then launches Robin with the retrospective framing | human | none from the create; then a `related` edge to the target in a separate write | `tui.retrospective`, `tui.retroStart`, `bd.NewDiscussion`, `bd.Related`; ADR 0281 (mugicrew-a-retrospective-s-spine…) |

No other door. A retrospective on an open spine is refused by the key; no CLI verb exists.

#### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| completed | none (empty reason) | Robin in the session, on the human's word: `bd update <id> --notes "…"` (no `--title`: keeps its title) then `bd close <id>` | as a Discussion | `robin/prompts/retrospective.md` framing |
| dropped | `dropped` | Human, TUI `x`, as a Discussion | as a Discussion | `tui.discussionDrop` |

#### Born-with ordering

| Expected edge | Why | D1 |
| --- | --- | --- |
| `related` → target | Binds the retrospective to what it reviews; the Spine key lines read from the body | ✓ (second write) |
| no `parent` | Off the spine like any Discussion | n/a |

Sources: `tui.retroStart`; GLOSSARY.md "Retrospective" Structure.

#### Gaps found

Entry 30 in [Gaps](#gaps).

## checkup

One human-started session in which Chopper diagnoses, with the human, a bead they think is broken (its target) and repairs it · owner Chopper · leaf, free-standing and off the spine like an Errand, bound to its target by a `related` edge; at most one open Checkup per target (GLOSSARY.md "Checkup"; `registry` Checkup entry: HITL, not `ReadOnly`).

### Creation doors

Both doors run the same find-or-file: refuse, reuse the target's open Checkup if any, else create one, then fence the target with `paused:checkup`. The two doors differ only in launching and in who is asked for the problem.

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| TUI key | D1 `c` on any bead row, with a one-line problem prompt (blank allowed); files "Repair <target title>" then launches Chopper | human | unparented; `related` → target (second write); target's `context:` and `repo:` labels copied when stamped; not a Draft; target labelled `paused:checkup` | `tui.checkupKey`, `tui.fileCheckup`, `chopper.FileCheckup`, `bd.NewCheckup`, `bd.newRelated`; ADR 0274 (mugicrew-a-checkup-launches-from…) |
| CLI verb | D2 `mugicrew checkup <bead> [--problem "<one line>"]`; launches and attaches the terminal | human | as the TUI door | `main.runCheckup`, `main.checkup`, `chopper.FileCheckup`; ADR 0272 (mugicrew-a-chopper-checkup-is-a-hitl…) |

No reconcile, loop, doctor fix or crewmate session files a Checkup; Chopper's prompt lists `signal new`/`offshoot` as the only filing verbs it may run (`chopper/prompts/checkup.md`). **Drafts on a clean exit:** Chopper may file offshoot Signal Drafts `discovered-from` the Checkup, published on a clean exit with no close, as for a Discussion (`session.sessionPlan`).

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| repaired | `repaired: <what changed>` | Chopper in the session, on the human's word, after a repair plan ran | The last act lifts `paused:checkup` from the target (`bd update <target> --remove-label paused:checkup`) | `chopper/prompts/checkup.md` "Done"; GLOSSARY.md "Checkup" Operations |
| not a bead fault | `not a bead fault: <why>` | Chopper, same door, when no change in its write surface fixes the symptom | Same fence lift; nothing written on the target | `chopper/prompts/checkup.md` "Done" |
| dropped | `dropped` | Human, TUI `x`: `tui.discussionDrop` or the live-session drop | `tui.closeCheckup` lifts `paused:checkup` from each related target that no other open Checkup still holds; unpublished Drafts deleted first | `tui.closeCheckup`, `tui.discussionDrop` |

A Checkup whose every finding the human declined stays open (`chopper/prompts/checkup.md`). Not withdrawable or cancellable, as for a Discussion. `mugicrew recover` also lifts a stranded `paused:checkup` from a target with no session (`chopper.liftable`).

### Born-with ordering

| Expected edge | Why | D1 | D2 |
| --- | --- | --- | --- |
| `related` → target | Binds the Checkup to its target and makes "at most one open per target" findable | ✓ (second write) | ✓ (second write) |
| `context:` / `repo:` stamp copied from target | A nested target's Checkup is held `paused:context` on drift like the target | ✓ | ✓ |
| `paused:checkup` on the target (label, not an edge) | Fences the target from the loop while it is repaired | ✓ (after the create) | ✓ (after the create) |
| no `parent` | Off the spine | n/a | n/a |

Sources: `bd.NewCheckup`, `bd.newRelated`, `chopper.FileCheckup`, `bd.OpenCheckupOn`.

### Gaps found

Entries 29, 30 in [Gaps](#gaps).

## initiative

An opt-in container above the spine that groups several Signal → Chart → Spec → Checkpoint chains, so one effort spans several Checkpoints · owner Luffy (steward and handler) · container (branchless, no land; `registry.table` luffy, HITL, container). Source: `GLOSSARY.md` Initiative; `registry.table`.

**Membership.** A member is a direct `parent` child of the Initiative: a Signal born in it, a chain head adopted into it, and each successor as it is born (`bd.InitiativeOf`). An Errand joins the Initiative of the member it is ordered against (`bd.errandPlacement`); a Latch and an unordered offshoot stay outside. A bead belongs to at most one Initiative.

### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| TUI key | D1 `n` then `i`, "New Initiative" (form with title, body, priority, and a ref field when Enforce external ID is on) | the human | none: an open, unparented epic carrying the typed seed text as its body and the ref as `external_ref`. No `parent`, `caused-by`, `drafted-by` or `blocks`. | `tui.newInitiativeForm`, `bd.NewInitiative`, `bd.unparentedArgs` |
| reconcile | D2 `mugicrew reconcile <signal> --initiative <body-file> [--title] [--ref]` | the human's Luffy triage session (`luffy/prompts/triage.md` INITIATIVE step 3) | `caused-by` the Signal; `drafted-by:` the Signal (a Draft); the Signal's outgoing `blocks`/`conditional-blocks` dependents and its open Latch blockers carried onto it (`blocks`); `external_ref` when `--ref` or a Jira Signal gives one. Never `parent` (Initiatives never nest). | `luffy.SeedInitiative`, `bd.SpawnSuccessorIfAbsent`, `bd.successorCreateArgs`, `luffy.carryOrdering`, `bd.Carry`, `bd.carryLatches` |
| crewmate session | D3 Robin's discussion session: raw `bd create --type epic -s deferred --no-inherit-labels -l kind:initiative,drafted-by:<Discussion> --deps discovered-from:<Discussion>`, on the human's explicit yes, one per yes | Robin | `drafted-by:` and `discovered-from` the Discussion (a Draft, bodyless: a headingless seed, no `## Done when`); no `parent`, `caused-by` or `blocks`; bd's default priority | `robin/prompts/discussion.md` Initiatives; ADR 0381 (robin-may-draft-a-bodyless-initiative…) |

Adoption is not a door: `mugicrew initiative adopt <head> <initiative>` (TUI `a`; Luffy's triage INITIATIVE step 5 offers it) re-parents an unclosed chain head alone with one `bd update --parent`, replacing any prior Initiative, and refuses a closed head, an Initiative head, a non-head or a closed target (`bd.AdoptChain`; `release` is `bd.ReleaseChain`). No loop step or doctor row creates an Initiative, and the only crewmate prompts that do are Luffy's and Robin's discussion prompt (D3, a bodyless Draft) (searched `loop/`, `doctor*.go`, prompts for a `KindInitiative` spawn).

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| completed (done) | `done: <how Done when holds>` | Luffy's cut session, by raw `bd close`, only on the human's explicit "done" | None: a closed Initiative is final and never reopened | `luffy/prompts/cut.md` DONE |
| cancelled (the human's Cancel) | `cancelled: <why>`, the human's required why; the members it sweeps close `cancelled: <initiative>` | the human's Cancel: `mugicrew initiative cancel <initiative> --why <text>` or the TUI `X` dialog (`tui.xdialog`), which refuses an empty why | Member Errands close first with `withdrawn: <initiative>`, then every unclosed member chain closes `cancelled: <initiative>`, then the Initiative closes `cancelled: <why>`; landed work stays; `--release <head>` keeps a chain out (a release, then it closes none of it); worktrees and branches go (`--discard`) | `withdraw.Cancel`, `withdraw.run`, `bd.CancelSet`; `tui.initstab` reads the `cancelled: ` prefix |
| cancelled (bodyless drop) | `cancelled: <why>` | Luffy's cut session on a bodyless Initiative, by raw `bd close`, when the human says to drop it | None: it has no members. A second door with the same shape, since the verb's in-flight fence refuses the Initiative's own alive session | `luffy/prompts/firstbody.md` |
| parked member (not a closure of the Initiative) | `later: <title> (phase line on <initiative>)` on a member Signal | Luffy's triage session | The Signal stays under the Initiative and never holds the drain | `luffy/triage.go` Park line; `GLOSSARY.md` Initiative |

An Initiative closes only `done: <how Done when holds>` or `cancelled: <why>`; the why of a cancel carries every case a missing outcome would: a rejected idea is rejected at Signal triage, and an Initiative replaced by another releases and adopts its chains into the other, then is cancelled. No Initiative is closed `withdrawn`, `obsolete:`, `superseded` or `rejected`: Withdraw refuses an Initiative (`withdraw.plan` routes it to Cancel), and TUI reject (`z`) takes only a Signal (`tui.rejectable`). A drained Initiative is not closed: it waits for the human (`GLOSSARY.md` Initiative Lifecycle).

### Born-with ordering

| Expected edge | Why | D1 | D2 | D3 |
| --- | --- | --- | --- | --- |
| `drafted-by:` the creator | so the creator's publish takes it (`contract.DraftInstruction`) | n/a (a human action, not a Draft) | ✓ (the Signal) | ✓ (the Discussion) |
| `caused-by` the Signal | lineage and one-successor guard (ADR 0006) | n/a (no predecessor) | ✓ | n/a (`discovered-from` the Discussion instead) |
| carried `blocks` from the Signal | members ordered after the Signal stay gated (ADR 0085) | n/a | intended (carried onto the Initiative itself: a dependent waits for the whole effort, ADR 0085) | n/a |
| `parent` | an Initiative never nests | n/a | n/a | n/a |
| Latch `blocks` on the Initiative | none: blocking an Initiative freezes every member through bd's block cascade (ADR 0348 §8) | n/a | none, not carried (Carry skips it as `initiative`) | n/a |

### Gaps found

Entries 31, 32, 33 in [Gaps](#gaps). Entry 32 is resolved by the Spec "An Initiative closes done or cancelled with its why, by every door".

## latch

A bead that blocks other work until a fact in the outside world is true (a Jira ticket Done, a GitLab MR merged) · owner Brook · leaf (AFK, mechanical, checked in-process with no session; `registry.table` brook, AFK, not a container). Free-standing: never a Chart child, Checkpoint member, Spec child or Initiative member. Source: `GLOSSARY.md` Latch; `registry.table`.

### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| CLI verb | D1 `mugicrew latch jira <KEY> --blocks <bead>... [--drafted-by <bead>]` | the human, or any session that runs it | `external_ref` `jira-latch-<KEY>`; a `blocks` edge per `--blocks` target (Latch blocks the target), created in the same `bd create` on first filing and added as missing edges on reuse. With `--drafted-by`: `drafted-by:` that bead (a Draft); otherwise an open bead. No `parent`, `caused-by` or `discovered-from`. Refuses an Initiative target; files nothing if the ticket is already Done; reuses a live Latch for the key and adds only missing edges. | `main.runLatchJira`, `bd.NewLatch`, `bd.LatchOf`, `bd.Blocks` (reuse only), `main.checkLatchTargets` |
| CLI verb | D2 `mugicrew latch mr <ref> --blocks <bead>... [--drafted-by <bead>]` | as above; needs `land_strategy = gitlab` | as above with `external_ref` `gitlab-mr-latch-<canonical>`; files nothing for an already-merged MR; refuses a closed MR | `main.runLatchMR`, `forge.MRCanonical` |

No TUI key, reconcile leg, loop step or doctor row creates a Latch, and no crew doc prescribes a raw `bd create` for one. The shared Drafts block (`contract.DraftInstruction`) that every bead-creating crewmate prompt embeds prescribes both verbs with `--drafted-by <self>`, `latch mr` marked for GitLab-landing Workspaces only (on any other Workspace it refuses with its existing message). Brook's body line `Watches: <jira|mr> <item>` is written by the verb and parsed by `brook.Watches`.

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| completed (Jira) | `completed: jira <KEY> reached <status> (Done)` | the loop's Brook latch check (in-process, no session) | Beads it blocked become unblocked | `brook.checkLatch`, `brook.closeLatch`; `loop` latch pacing |
| completed (MR) | `completed: mr <path>!<iid> merged` | as above | as above | `brook.checkMR` |
| orphaned | `orphaned: no open bead waits on <system> <item>` (for an mr: `orphaned: no open bead waits on mr <path>!<iid>`) | as above, when no open bead is blocked by the Latch | none; it never reopens | `brook.checkLatch` |
| withdrawn | `withdrawn` | the human: `mugicrew withdraw <latch>` / TUI `X` | The Latch closes alone; the beads it blocked are released and stay open; its `blocks` edges are kept | `withdraw.plan` |

`closeLatch` hands the close to the effort's publish batch when the ctx carries a Publisher, else closes directly. Brook never closes a Latch whose fact did not hold. A watched MR that is closed unmerged, or whose read answers 404, pauses the Latch `paused:unmet` at once (no failure count; the blocked work stays blocked); the fixes are to reopen the MR and run-now (a run-now that reads it open lifts the pause, merged closes `completed`, still closed or missing pauses again) or to withdraw the Latch; a human unpause also lifts it. A failed Jira read stays on the `paused:failing` count, because acli prints one line for a gone, forbidden or unreachable ticket; its message says the ticket may be gone, forbidden or unreachable and that `mugicrew withdraw <latch>` is the fix if it is gone. Dropping a Latch for only some of the beads it blocks is `bd dep remove` on those edges; removing the last one leaves the orphan close. Cancel reaches only an Initiative's members. Carry's effect on ordering is at creation of a successor, not at closure: `bd.carryLatches` makes an open Latch also block a blocked bead's successor.

### Born-with ordering

| Expected edge | Why | D1 | D2 |
| --- | --- | --- | --- |
| `blocks` on each waiting bead | the Latch exists only to hold them; with none open it is closed `orphaned` | ✓, in the create on first filing (missing-edge adds on reuse) | ✓, as jira |
| `drafted-by:` the filing session | so a session's publish takes it | ✓ only with `--drafted-by` (✗ otherwise) | ✓ only with `--drafted-by` (✗ otherwise) |
| `parent` | none: free-standing | n/a | n/a |
| `blocks` on an Initiative | none: refused, the cascade freezes every member | n/a (refused) | n/a (refused) |

### Gaps found

Entries 33, 34, 35 in [Gaps](#gaps). Entry 35 is resolved by the Spec "Latch closure outcomes: an unmet pause for a closed or missing MR, Withdraw for a Latch, and named close reasons".

## errand

A HITL walk-through of steps that change the external world (outside the bead graph and the code) · owner Brook · leaf: a direct Initiative child when ordered against a member, else outside; never a Chart child, Checkpoint member or Spec child (`registry.table` brook, HITL, not a container). Source: `GLOSSARY.md` Errand; crew doc `errand-body.md`.

**Initiative join.** The filing door puts the Errand in the Initiative of the member it is ordered against, or outside with the reason as its first body fact line (`Initiative: <title>` or `Initiative: outside (<reason>)`): `--outside`, no order target, a target with no or a closed Initiative, or targets in two open Initiatives. It is decided once at filing; an Errand is ordered only by `blocks` edges (`bd.errandPlacement`, `bd.InitiativePlacement`).

### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| CLI verb | D1 `mugicrew errand new --drafted-by <bead> [--after <bead>]... [--before <bead>]... [--outside] --title T --description D` | any session via the prompts below; the CLI itself has no human-only form | `drafted-by:` the drafter (a Draft); `discovered-from` the drafter; `blocked-by` each `--after` (the Errand waits on it); `blocks` each `--before`; `parent` the Initiative of the ordered member when one is open. A refused order edge files nothing. | `errandnew.runErrandNew`, `bd.NewErrand`, `bd.errandPlacement`, `bd.successorCreateArgs` |
| crewmate session | D2 Zoro's slicing session, a Spec's `## Errands` entry (`mugicrew errand new` before any Build; `after:`/`before:` lines become `--after`/`--before`) | Zoro, AFK through the loop | as the verb. Slicing adds no `discovered-from` Spec edge beyond the drafter's. | `zoro/prompts/slicing.md` |
| crewmate session | D3 Zoro's slicing session, an adopted `errand: <id>` entry (not a creation) | Zoro | no new bead: only `bd dep add` edges (`after: land` is Errand blocked by the Checkpoint; `before: land` is the Checkpoint blocked by the Errand; `after: <T>`) | `zoro/prompts/slicing.md` Adopted entries |
| crewmate session | D4 Nami's charting session (`--after <chart>`) | Nami, with the human | as the verb, ordered after the Chart so Carry moves the edge onto the Checkpoint when it is cut | `nami/prompts/charting.md` |
| crewmate session | D5 Nami's grilling session (`--after <chartID>`) | Nami, with the human | as the verb | `nami/prompts/grilling.md` |
| crewmate session | D6 Nami's promote-or-stamp session (`--after <chart>`) | Nami, AFK through the loop | as the verb | `nami/prompts/promote-or-stamp.md` |
| loop | D7 the Hold Errand doors (variant below), run from Jinbe's landing ticks and the foreign-land check | Jinbe (Go) | see Hold Errand | `jinbe.holdErrand`, `bd.SpawnBlockingErrandIfAbsent` |

`mugicrew task new` is retired and refuses (`runRetiredTask`). The relabel `mugicrew errand relabel` (doctor's task beads row) converts existing `kind:task` beads and creates none (`bd.RelabelLegacyTasks`; ADR 0355). No TUI key files an Errand (the TUI only resumes or launches one); an offshoot Signal is what an agent files mid-work instead (`GLOSSARY.md` Errand Notes).

### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| completed | `completed` | Brook's Errand guide, by `bd close <id> --reason completed` after a final all-checks pass and the human's in-session yes | Anything it blocked (a Checkpoint's land, another Errand) unblocks | `brook/prompts/errand.md` step 3; `GLOSSARY.md` Errand Operations |
| withdrawn | `withdrawn` | the human's Withdraw of the Errand itself: TUI `X` or `mugicrew withdraw <errand>` | worktrees and branches go; Errands ordered after it close `withdrawn: <errand>` (swept) | `withdraw.Withdraw`, `withdraw.run`, `bd.SweepErrands` |
| swept (a withdrawn chain's Errand) | `withdrawn: <head>` | the human's Withdraw of a chain head, for each unclosed published Errand ordered after (forward) or only before (backward) what closes; one still holding an outside bead is left open | a Checkpoint outside the set whose in-set blockers are all Errands is released, not refused | `withdraw.run`, `bd.SweepErrands` (ADR 0342, ADR 0351) |
| withdrawn by cancel | `withdrawn: <initiative>` | the human's Cancel of the owning Initiative, member Errands first | as cancel | `withdraw.run` |

An interrupted or stuck Errand stays claimed and idle (never closed, never unclaimed by the guide; `x` unclaims it). A pivot's confirm or escalate sweep neither closes nor carries an Errand (`zoro/prompts/pivot.md` Held Errands). No Errand is closed `rejected` or `superseded`, and only a Hold Errand is closed `obsolete:`.

### Born-with ordering

| Expected edge | Why | D1, D2, D4–D6 | D7 (Hold Errand D1–D5) |
| --- | --- | --- | --- |
| `drafted-by:` the drafter | so the drafter's publish takes it | ✓ | ✗ by design (born open, not a Draft: Go files it with no session to publish it) |
| `discovered-from` the drafter | lineage to the bead it was found from | ✓ | ✗ by design (`blocks` already takes the container's pair, bd keeps one edge per pair, and a hold may have no Landing) |
| `blocks` / `blocked-by` its order targets | an Errand is ordered only by edges (ADR 0113) | ✓ per `--after`/`--before`; ✗ when none given (then it is unordered and outside) | ✓ (`blocks` the container, atomic with the create) |
| `parent` the Initiative of its order target | membership, decided once at filing (ADR 0352) | ✓ when one is open | ✓ when the container's is open |
| `related` the closed predecessor | a hold that outlives its closed Errand re-files related to it (ADR 0338) | n/a | ✓ (a separate second write) |

### Gaps found

Entry 23 in [Gaps](#gaps).

### Hold Errand

An Errand Go files to block a container or Rollback until a Forge or mainline condition clears (a closed or trailerless merge request, a failed pipeline, a foreign land), found again by a whole fact line such as `Closed MR: !N` · owner Brook (Errand's owner) · leaf, Brook-owned. Source: `GLOSSARY.md` Hold Errand.

#### Creation doors

| Door type | Door | Used by | Edges created | Source |
| --- | --- | --- | --- | --- |
| loop | D1 Jinbe's Landing tick: a closed MR (`Closed MR: !<iid>`) | Jinbe (Go) | `blocks` the Checkpoint (atomic with the create); `parent` the Checkpoint's open Initiative when there is one; later `related` to a closed Errand with the same key. No `drafted-by`, `discovered-from` or `caused-by`. | `jinbe.holdErrand`, `bd.SpawnBlockingErrandIfAbsent` |
| loop | D2 Jinbe's Landing tick: a failed pipeline (`pipelineKey`), including the infra variant | Jinbe (Go) | as above | `jinbe.holdErrand` (lines naming `pipelineKey`) |
| loop | D3 Jinbe's Landing tick: a merged MR whose trailer is missing (`Merged MR: !<iid>`) | Jinbe (Go) | as above | `jinbe.holdTrailerless` |
| loop | D4 Jinbe's revert MR arms: a failed revert pipeline, blocking the Rollback | Jinbe (Go) | as above with `blocks` the Rollback; a Rollback's Initiative is found through the Checkpoint it undoes | `jinbe.revertMRArms`, `bd.initiativeOfTarget` |
| loop | D5 the foreign-land check at land: `Foreign: <sha>` | the loop, Jinbe and Nami (`bd.HoldForeign` callers) | as above | `bd.HoldForeign` (`bd/bd.go`), `bd.SpawnBlockingErrand` |

All five are idempotent on (container, key): an open blocking Errand whose body contains the key is returned. No TUI key, verb, reconcile leg or doctor row files one.

#### Closure outcomes

| Outcome | Close reason (exact) | Performed by / door | Side effects | Source |
| --- | --- | --- | --- | --- |
| completed | `completed` | the human, walking the hold with Brook's guide (`bd close` after the Check passes) | the container's land hold lifts | `brook/prompts/errand.md`; `jinbe.holdErrand` (body Checks) |
| cleared | `cleared: <key line>`, the key line verbatim, as in cleared: Closed MR: !12 | the loop's hold clearing step, when the hold's fact reads cleared: MR reopened, pipeline green, or the container's trailer on mainline (`Closed MR:`, `Merged MR:` and Checkpoint or Rollback `Pipeline:` holds only; never while a guide session is live) | the Errand closes forced (`bd.ForceClose`) and keeps its assignee, so an idle claim by another actor neither blocks nor is taken; the container's next reconcile closes it or readmits it, and that close is the one with no `--force` | `jinbe.ClearHolds` (`jinbe/holds.go`); ADR 0393 |
| obsolete: revert merge request closed | `obsolete: revert merge request closed` | the loop's hold clearing step, when a Rollback's revert MR reads closed on the Forge | as cleared: the Errand closes forced and keeps its assignee; the container's close is the one without `--force` | `jinbe/holds.go` |
| obsolete: container closed | `obsolete: <container> closed` | the loop's hold clearing step, when the bead the hold blocks was closed by any path but withdraw; any variant, `Foreign:` and Chart `Pipeline:` included | as cleared: the Errand closes forced and keeps its assignee; the container's close is the one without `--force` | `jinbe/holds.go` |
| withdrawn, swept, cancelled | as Errand | as Errand | a Checkpoint blocked only by swept Errands is released | `withdraw.run`, `bd.SweepErrands` |

#### Born-with ordering

| Expected edge | Why | D1–D5 |
| --- | --- | --- |
| `blocks` the container | the hold exists to stop the land | ✓ |
| `parent` the container's Initiative | membership (ADR 0352) | ✓ when open |
| `drafted-by` / `discovered-from` | provenance to the Landing or MR that raised it | ✗ by design: born open with no session to publish it (`SpawnBlockingErrandIfAbsent` sets neither); the key line in the body is the identity, since an MR number or sha can't live on an edge |
| `related` the closed predecessor | a re-filed hold keeps lineage (ADR 0338) | ✓ when one exists |

#### Gaps found

Entries 36, 37 in [Gaps](#gaps).

## Gaps

Every finding of the audit, numbered, each traced to the Signal filed for it. Each Signal's description says it was found by the bead lifecycle audit, names the Initiative "Every bead kind has clear creation, closure and ordering paths" by title and cites its entry here; the human adopts each into the Initiative with `mugicrew initiative adopt`. Findings that touch several kinds are one entry, listed under each kind's section by pointer. Entries marked _related_ share a root cause with another entry but were filed by different group Builds; they were left as separate Signals.

| # | Kind | Door or outcome | Problem | Signal |
| --- | --- | --- | --- | --- |
| 1 | signal | creation: code-filed offshoots (D10–D12) vs the offshoot verb (D5) | Code-filed offshoot Signals are born open with `discovered-from` and no order edge; the verb files a Draft with an order. | Code-filed offshoot Signals skip the Draft and order the offshoot verb applies |
| 2 | signal | creation: Initiative member (D5 vs D6) | Two filing doors give a member Signal different edges: D6 `parent` only, D5 `parent`, `discovered-from` and an order edge. | Initiative member Signals have two filing doors with different edges |
| 3 | signal, chart, spec, checkpoint, build | closure: accepted, completed, landed, built | The success closes of the spine kinds all carry bd's default `Closed`; reject is free text, duplicate is bd's own text, and the TUI reads an empty reason as superseded. _Related_ to entries 7, 15, 26, 28, 29, 37. | Spine closes lack distinguishable reasons |
| 4 | signal, chart, spec | closure: superseded, duplicate (Jira re-intake, Confirm, Escalate) | bd's supersede drops the superseded bead's outgoing `blocks` edges. Covered by an existing Initiative member; no new Signal. | Superseding a Signal drops its outgoing blocks edges and releases what it gated (existing Initiative member) |
| 5 | signal | creation and closure: non-code outcomes | Whether Signal varieties need a kind of their own. Covered by an existing Initiative member; no new Signal. | New Signal Kind: Experiment? (existing Initiative member) |
| 6 | chart, checkpoint, build | glossary Operations lines | The glossary's Operations lines for Chart, Checkpoint and Build disagree with the code (Chart close order, surgery Checkpoint creator, Build's omitted closes). | Glossary Operations lines disagree with the code for Chart, Checkpoint and Build |
| 7 | survey | closure: completed | The fog-seeded and pushed-back closes both carry bd's default `Closed`, and no door retires a lone Survey. _Related_ to entry 3. | A Survey closes with bd's default reason whatever its session did |
| 8 | grilling, research, prototype | closure: obsolete, superseded, withdrawn | No tool and no documented reason closes a moot defog bead. | A defog bead has no tool and no documented reason for closing obsolete, superseded or withdrawn |
| 9 | grilling, research, prototype | creation: all doors | Defog beads are born with different edges depending on the door; ordering between them is left to a later `bd dep add`. | Defog beads are born with different edges depending on the door that creates them |
| 10 | research | closure: no primary sources | A research run that finds no sources leaves the bead open for the loop to re-dispatch; no outcome exists for it. | A research bead that finds no primary sources has no closure path |
| 11 | spec (Amendment) | creation: Amendment | The Amendment is a prompt-only `bd comments add` convention with no code door. | Amendment is a prompt-only comment convention with no code door |
| 12 | checkpoint | creation: surgery Checkpoint (D4, D5) | Born without the `context:` and `repo:` stamps the other doors copy. | Surgery Checkpoint is born without the context and repo stamps other Checkpoint doors copy |
| 13 | build | closure: abandoned | No door closes a single abandoned Build; `withdraw` refuses it. | A single Build has no closure door for abandoned work |
| 14 | review | creation and closure: pin, obsolete | A Review's pin and obsolete outcome are not on its bead; only the Approval is pinned and obsoleted. | A Review's pin and obsolete outcome are not on its bead |
| 15 | review, conflict, critique | closure: completed, resolved | The completed closes write no reason. _Related_ to entry 3. | Review, Conflict and Critique completed closes write no reason |
| 16 | critique | creation: D1, D2, D7 vs D4–D6, D8, D9 | The doors disagree on the offshoot edge to what raised the Critique. _Related_ to entries 21 and 23. | Critique doors disagree on the offshoot edge to what raised them |
| 17 | critique, pivot | closure: dismissed, amended | The `dismissed:` and `amended` reasons exist only in the Zoro prompt. | Pivot dismissed and amended close reasons exist only in the Zoro prompt |
| 18 | conflict | creation: D3, D4 (requested rebase) | The glossary's human-only rule is prose; neither door checks it. | A requested Conflict's human-only rule is prose, not enforced |
| 19 | approval | creation: D1; closure: obsoleted | No `caused-by` edge to the Review, and an obsolete reason without the `obsolete:` prefix. | Approval is born without a caused-by edge to its Review and obsoletes with a non-obsolete reason |
| 20 | forge-review, mr-watch | closure: filed, nothing actionable, container closed | The close reasons are in no glossary line and mix prefix conventions. | MR Watch and Forge Review close reasons are undocumented and mix prefix conventions |
| 21 | pivot | creation: D1–D5 | Two ways to record the host and the raiser. _Related_ to entries 16 and 23. | Pivot doors record the host and the raiser differently |
| 22 | landing | closure: build failed, pipeline failed, merge request closed, branch advanced | Three reasons are undocumented and the glossary misquotes the branch-advanced reason. | A Landing's build failed, pipeline failed and merge request closed closes are undocumented, and the glossary misquotes the branch-advanced reason |
| 23 | landing, chart-conflict, critique, errand | creation: follow-ups of a Landing | Follow-up beads link back to the Landing by different edges, and the Conflict beads by none. _Related_ to entries 16 and 21. | A Landing's follow-up beads link back to it by different edges, and its conflict beads by none |
| 24 | chart-conflict | closure: resolved, moot, superseded | The reasons are free text or documented only in prose. | A Chart-conflict's resolved, moot and superseded closes are free-form or undocumented |
| 25 | rollback | creation: filing | Filing is two writes; a tick between them closes the Rollback `invalid` before its `related` edge exists. _Related_ to entries 27, 30 and 34. | A Rollback is filed in two writes, so a tick can close it invalid before its related edge exists |
| 26 | rollback | closure: completed, fix-forward, abandoned | The completed close writes no reason; the prefixes and the TUI-only door are not in the glossary. _Related_ to entry 3. | A Rollback's completed close writes no reason, and its fix-forward and abandoned prefixes and TUI-only door are not in the glossary |
| 27 | surgery | creation: fresh doors, join door; the Critique hold | Filing is not atomic, and the hold on Critiques is a loop scan, not an edge. _Related_ to entries 25, 30 and 34. | Surgery filing is not atomic and its hold on Critiques is not an edge |
| 28 | surgery | closure: completed, abandoned | Closes are undocumented, and an idle Surgery can end only through its own session. _Related_ to entry 3. | Surgery closes are undocumented and an idle Surgery can only end through its own session |
| 29 | discussion, checkup | closure: completed, dropped, repaired, not a bead fault | Reasons are undocumented, and the two Checkup closers lift the target's fence differently. _Related_ to entry 3. | Discussion and Checkup closure reasons are undocumented, and Checkup closers lift the target's fence differently |
| 30 | discussion (Retrospective), checkup | creation: TUI `r`, Checkup doors | Filed by different code with a second-write `related` edge; only Checkup stamps `context:` and `repo:`. _Related_ to entries 25, 27 and 34. | Retrospective and Checkup are filed by different code with a second-write related edge, and only Checkup stamps context and repo |
| 31 | initiative | creation: TUI key vs reconcile | The two doors give different edges and body. | Initiative is born with different edges and body by the TUI and by reconcile |
| 32 | initiative | closure: done, cancelled | Resolved by the Spec "An Initiative closes done or cancelled with its why, by every door". Was: reasons differ by door, `done:` is undocumented, and no obsolete, superseded or rejected path exists. | Initiative closure reasons differ by door and done is undocumented |
| 33 | initiative, latch | creation: reconcile Carry | Carry moves a converted Signal's `blocks` edges and Latches onto its Initiative, which the `latch` verb refuses. | Carry puts a converted Signal's blocks edges and Latches onto its Initiative (resolved: the Latch is skipped as `initiative`; the outgoing blocks carry is intended) |
| 34 | latch | creation: both verbs | No prompt prescribes a door, and the create and the `blocks` edge are separate writes. _Related_ to entries 25, 27 and 30. | A Latch has no prompted door and is born without its blocks edge in the same write |
| 35 | latch | closure: watched MR closed, target dropped | Resolved by the Spec "Latch closure outcomes: an unmet pause for a closed or missing MR, Withdraw for a Latch, and named close reasons". Was: no documented closure; Withdraw and Cancel cannot drop a Latch. | A Latch watching a closed merge request or a dropped target has no documented closure |
| 36 | errand (Hold Errand) | creation: `errand new` vs the Hold doors | Different lineage: `drafted-by` and `discovered-from` against neither. Resolved as documented-by-design: see the Errand and Hold Errand Born-with tables. | Errand is born with different lineage by errand new and by the Hold doors |
| 37 | errand (Hold Errand) | closure: completed, hold cleared | Resolved by the Spec "Go closes a cleared or moot Hold Errand, and every Errand close carries a named reason". Was: the reason is the default `Closed`, and no door closes a hold whose fact clears. _Related_ to entry 3. | Errand closure reasons are undocumented and no door closes a Hold Errand whose fact clears |
| 38 | all kinds | doc upkeep | Nothing keeps this hand-kept doc in step with `contract.Kinds` or the close reasons it quotes. A cross-kind finding of the audit's final Build. | Nothing keeps the bead lifecycle reference in step with the code |

### Cross-kind checks

- **Kinds the glossary names against kinds the code creates.** All 25 `contract.Kinds` constants have a section. The four glossary variants without a label (Retrospective, Hold Errand, Renumber Checkpoint, Amendment) are subsections of their base kind. No kind or variant is named by only one side; the glossary's Task is retired history for Errand.
- **Two kinds whose doors model the same request differently.** Found: filing a bead related to another (Rollback, Retrospective, Checkup, Latch, Surgery: entries 25, 27, 30, 34), and a follow-up linking back to what raised it (Critique, Pivot, Landing follow-ups: entries 16, 21, 23). Each was already filed by its group Build and is noted _related_ above rather than re-filed.
- **Close reasons.** The undocumented or bare closes recur across kinds (entry 3 and its _related_ entries). They were filed per kind group and left separate so each can be adopted on its own.
