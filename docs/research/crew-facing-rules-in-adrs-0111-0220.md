# Crew-facing rules in ADRs 0111–0220 missing from or stale in the packaged crewmate text

Status: Research (input to the Spec for "Crew docs carry the mugicrew process knowledge a foreign Workspace's crewmates need", mc-8z95b)
Date: 2026-10-21
Bead: mc-8z95b.3

## Purpose

This doc answers which ADRs numbered 0111–0220 under `apps/mugicrew/docs/adr/` carry a crew-facing rule that is missing from, or stale in, the packaged crewmate text.

Packaged text means:
- `contract/CREWMATE.md`;
- `crewdocs/*.md`, including `vocabulary.md`;
- the role prompts `*/prompts/*.md`;
- the Go splices into those prompts.

Each ADR was run through the four-part lift test in the Chart Notes:
1. The rule governs a crewmate's action or judgement.
2. Nothing in the binary or a mugicrew verb enforces it, unless a refusal would mislead a crewmate.
3. No packaged text states it yet.
4. It reads as Workspace-agnostic guidance.

Where an ADR was superseded or amended, the audit used its effective rule. Proposed placements follow ADR 0088 (mugicrew-crewmate-content-split) §1–§3:
- single-consumer text goes inline in its role prompt;
- multi-consumer text goes in a crew doc;
- universal text goes in CREWMATE.md, which is capped at about 2 KB and is at 2045 bytes now (`wc -c contract/CREWMATE.md`).

All paths below are relative to `apps/mugicrew/`.

**Result:**
- **Two ADRs pass as missing:** ADR 0132 §1 (charting half) and ADR 0134 §3.
- **One stale gap:** ADR 0164 and ADR 0170 §9 contradict a resume-key line that the Surgery and Task prompts make the crewmate say to the human.
- **The other 107 ADRs fail the test.**

## 1. Passing ADRs — missing

### 1.1 ADR 0132 (initiative-born-at-triage-phased-by-rolling-member-signals) §1, charting half

- **Rule:** If charting shows that a Chart's scope spans more than one Checkpoint, don't keep the Chart and don't split its phases into offshoot Signals yourself. Hand the Chart back with a Signal that says so, so that triage can make it a multi-phase Initiative.
  - Source: `docs/adr/0132-mugicrew-initiative-born-at-triage-phased-by-rolling-member-signals.md:14-18` ("Multi-phase scope that Nami finds while charting goes back by the ADR 0130 hand-back, and Luffy triages that Signal as INITIATIVE").
- **Gap type:** missing.
  - The triage half is packaged (`luffy/prompts/triage.md:14-15`, `:82-111`).
  - The charting side is not. The "Hand back" item in `nami/prompts/charting.md:94-103` covers only "a Chart whose Destination is wrong" and a Chart with no fog.
  - Meanwhile "Split" (`charting.md:90-93`) invites filing separable work as offshoots. That is the opposite of what ADR 0132 asks for multi-phase scope.
  - A case-insensitive grep for `initiative|more than one checkpoint|multi-phase` over `nami/prompts/*.md`, `crewdocs/chart-body.md`, `crewdocs/defog-session.md` and `contract/CREWMATE.md` found no match.
- **Not enforced:** a hand-back is a plain `mugicrew signal offshoot` plus `bd close` (`charting.md:96-101`). No verb detects scope that spans several Checkpoints.
- **Consumer:** `nami/prompts/charting.md` only.
- **Placement:** inline in that prompt, one sentence in the "Hand back" item (ADR 0088 §3, single consumer). Suggested wording: "A Destination spanning more than one Checkpoint is wrong too: hand it back saying so, so triage can phase it; don't split it into phase Signals yourself."

### 1.2 ADR 0134 (initiative-carries-no-defog-of-its-own) §3

- **Rule:** A defog session never edits the body of the Initiative its Chart belongs to. Only the Initiative owner's cut session writes that body. Record findings as a doc, on your own bead and on the Chart. The cut session adds the Notes pointer.
  - Source: `docs/adr/0134-mugicrew-initiative-carries-no-defog-of-its-own.md:22-23`.
- **Gap type:** missing.
  - The only packaged text on who writes the Initiative body is Luffy's own: `luffy/prompts/cut.md:11` ("You are the body's only writer"). Defog sessions never see it.
  - `crewdocs/defog-session.md` says nothing about Initiatives. Its Guardrail is `:89-93`, and grep for `initiative` finds nothing in it or in `nami/prompts/research.md`.
  - Because a Chart is a child of its Initiative (ADR 0135 §2), `bd show <chart>` puts the Initiative in front of every defog session.
- **Not enforced:** nothing guards a raw `bd update --description` on an Initiative. The locked body verb, `mugicrew chart append|drop`, works on Charts only (`crewdocs/defog-session.md:54-55`).
- **Consumers:**
  - `nami/prompts/grilling.md` and `sanji/prompts/prototype.md`, which both read `defog-session.md`;
  - `nami/prompts/research.md`, which keeps its Chart-tending text inline (`:48-51`).
- **Placement:**
  - One line in the Guardrail of `crewdocs/defog-session.md`, since it has several consumers (ADR 0088 §3).
  - A one-clause mirror inline in `nami/prompts/research.md` near its `chart append` instruction, because research does not read that crew doc.
  - Not CREWMATE.md: the rule is not universal, and that file is at its cap.

## 2. Passing ADRs — stale

### 2.1 ADR 0164 (tui-v2-enter-is-mode-aware-on-every-tab) and ADR 0170 (tui-v2-execute-tab-rows-keys-badge-and-refusals) §9

- **Rule (effective):** In the TUI, `enter` on the row of an idle Surgery or Task resumes it. Sources:
  - ADR 0164 §1, `docs/adr/0164-…:16-22` ("`enter` launches or attaches … resumes it, whatever its kind");
  - ADR 0170 §9, `docs/adr/0170-…:90-94` ("`enter` relaunches it against its existing claim, as … the 'idle — enter to resume' suffix says").
  - The binary matches: `tui/graphview.go:318` renders `idle — enter to resume`.
- **Stale text:** both prompts tell the human that `l` resumes the session.
  - `franky/prompts/surgery.md:71`: the verbatim line the Surgery session prints is `⏸ Surgery idle — /exit; l on its row resumes it.`
  - `brook/prompts/task.md:91`: "Tell the human the Task stays idle and l on its row resumes it."
  - In the current TUI, `l` is the next-tab key (`tui/shell.go:205`). So the crewmate points the human at the wrong key.
- **Test fit:**
  - It passes (1) only in the sense that the crewmate is told to say this line.
  - It passes (3) as stale text.
  - The line is Workspace-agnostic, since the TUI is the same for every Workspace.
- **Consumers:** `franky/prompts/surgery.md` and `brook/prompts/task.md`, one line each.
- **Placement:** fix inline in each prompt. Change "l on its row" to "enter on its row" (ADR 0088 §3).
  - The pins at `franky/franky_test.go:134` and `promptguard/promptguard_test.go:26` move with the prompt text.
  - The source glossary line `GLOSSARY.md:671` repeats "`l` on its row resumes it". That file is not packaged, and `crewdocs/vocabulary.md` does not carry the line, but it should be fixed with the prompts so it is not re-lifted later.

## 3. Failing ADRs

| ADR | Reason | Note (citation) |
|---|---|---|
| 0111 | internal | Hooks and `hitl-run`; closing line spliced (`contract/contract.go:180-189`). |
| 0112 | internal | Human config key read by `hitl-hook` (ADR 0112 §2-4). |
| 0113 | covered | `franky/prompts/intent.md:24-29`, `usopp/prompts/review.md:134-136`, `zoro/prompts/slicing.md:93-95`. |
| 0114 | covered | `brook/prompts/task.md:16-64`. |
| 0115 | covered | `brook/prompts/task.md:2-4,76-77,101-106`. |
| 0116 | covered | `crewdocs/task-body.md:10-68`. |
| 0117 | internal | TUI rows and suffixes (ADR 0117 §1-4). |
| 0118 | covered | `brook/prompts/task.md:32-33,74-99`. |
| 0119 | internal | Human-only CLI verb (ADR 0119 §1). |
| 0120 | repo-specific | mugicrew's own flake and module path. |
| 0121 | repo-specific | One-off mugicrew cutover plan. |
| 0122 | internal | Launch-time profile resolution. |
| 0123 | internal | Refusal comes before any session exists (ADR 0123 §3-5). |
| 0124 | internal | Dispatch and loop code. |
| 0125 | internal | ctx assembly and env. |
| 0126 | internal | Human-triggered deterministic code. |
| 0127 | internal | `recover` verb behaviour. |
| 0128 | enforced | Parent inheritance in `bd/succession.go:326-335`. |
| 0129 | covered | `luffy/prompts/triage.md:37-62`, `nami/prompts/charting.md:9-20`. |
| 0130 | covered | `nami/prompts/charting.md:81-109`. |
| 0131 | internal | TUI Graph layout. |
| 0133 | covered | `luffy/prompts/cut.md:33-37,63`; withdraw enforced at `withdraw/withdraw.go:225`. |
| 0135 | internal | Branchless routing in Go. |
| 0136–0139 | internal | TUI Settings. |
| 0140 | covered | `nami/prompts/charting.md:12-19,104-109`, `speccing.md:24`. |
| 0141–0144 | internal | TUI v2 shell and keys. |
| 0145 | internal | Landing lifecycle in Jinbe (`contract/contract.go:69`). |
| 0146 | internal | TUI `enter` on an Initiative head. |
| 0147 | internal | Land-strategy seam in Go. |
| 0148, 0149 | internal | TUI forms. |
| 0150, 0151 | internal | Land-gate code and events. |
| 0152 | covered | Base-pinned Critique splice (`franky/franky.go:306-313`). |
| 0153 | covered | Retry line (`crewdocs/signal-filing.md:43`, `chart-body.md:54`). |
| 0154 | internal | Dispatcher flock. |
| 0155 | enforced | `chopper/recover.go:126` refuses and names `mugicrew abort`. |
| 0156 | internal | TUI Defog tab. |
| 0157 | internal | Prompt-builder rules for Go. |
| 0158 | enforced | `luffy/triage.go:408`; prompt splice at `:167`. |
| 0159 | covered | `usopp/prompts/review.md:63-77`. |
| 0160 | covered | `franky/franky.go:234`, `franky/prompts/build.md:58-61`. |
| 0161 | covered | `franky/franky.go:219-235`. |
| 0162 | internal | Human files Surgeries (ADR 0162 §1-7). |
| 0163 | covered | `usopp/prompts/review.md:44-53`. |
| 0164 | internal | TUI semantics; its crew-visible effect is §2.1. |
| 0165 | internal | TUI `b` key. |
| 0166 | internal | Go repo handle. |
| 0167 | enforced | Stamp and drift hold in loop and Jinbe. |
| 0168 | enforced | `main.go:1721`; focus line `contexts/contexts.go:247`. |
| 0169 | internal | Recover and prune code. |
| 0170 | internal | TUI tab; its crew-visible effect is §2.1. |
| 0171–0181 | internal | TUI v2 tabs, folds, badges, Initiatives tab. |
| 0182 | covered | `luffy/prompts/firstbody.md:4-17`, `cut.md:11-12`. |
| 0183, 0184 | internal | TUI Recents. |
| 0185, 0186 | internal | Jinbe MR handling (`jinbe/jinbe.go:568-576`). |
| 0187 | enforced | `bd/bd.go:913-923` (`HoldForeign`). |
| 0188, 0189 | internal | Jinbe pipeline and MR watch. |
| 0190 | internal | TUI birth form. |
| 0191 | covered | `claudecode/claudecode.go:217`; `robin/prompts/discussion.md:30-31`. |
| 0192 | internal | TUI modal; superseded in part by 0194. |
| 0193 | covered | `robin/prompts/discussion.md:6-33`. |
| 0194 | covered | `robin/prompts/discussion.md:36-61`. |
| 0195–0198 | internal | Liveness, rendering, refresh, rollup word. |
| 0199 | covered | `nami/prompts/grilling-source.md:1-15`. |
| 0200, 0201 | internal | `jinbe/threads.go:30`; `bd/succession.go:91`. |
| 0202 | covered | `crewdocs/vocabulary.md`; `contract/CREWMATE.md:12-16`. |
| 0203 | covered | `luffy/prompts/approval.md:78-96`. |
| 0204 | internal | `tui/chartword.go:142`. |
| 0205 | covered | Splice `luffy/triage.go:126-133`. |
| 0206, 0207 | enforced | `jira/intake.go:29`, `jira/jira.go:53`. |
| 0208, 0209 | internal | Jinbe revert-MR arms. |
| 0210–0213 | internal | Human `land-lineage`, Jinbe rollback, anchor pin. |
| 0214 | covered | `crewdocs/sequence-renumber.md:31-34`. |
| 0215 | enforced | `cfglayer/cfglayer.go:193`. |
| 0216–0218 | internal | Superseded by ADR 0300; binary or human verbs. |
| 0219 | covered | `crewdocs/sequence-renumber.md:5-9,31-36`; `nami/prompts/chart-conflict.md:12`. |
| 0220 | internal | `tui/initstab.go:396`. |

## Open questions / gaps

- **Closed `kind:landing` children may be read as decisions.** Per ADR 0145 §5 (`docs/adr/0145-…:58-64`), a Chart's Landing child is closed before Nami's speccing pass, and obsoleted Landings stay as closed children. `nami/prompts/speccing.md:24-25` (and promote-or-stamp) skip only `kind:survey`, so a closed Landing could be read as a defog decision. No ADR in this range states the crew rule. That makes this a prompt bug, not a lift, and it may deserve its own Signal.
- **ADR 0113 §1 ("never file a Task mid-work; file an offshoot Signal") is only partly packaged.** It appears in Franky's build and conflict prompts and in Usopp's prompts, but not in surgery, prototype, research, pivot, discussion, approval or checkup, several of which file offshoots. If those roles count, this is a multi-consumer gap for `crewdocs/signal-filing.md`. It was scored as covered here.
- **ADR 0220 §3 ("triage may adopt into an Initiative with no ref") is only implied** by `luffy/triage.go:71-91`. A one-clause clarification in that splice would close it.
- **`brook/prompts/task.md:61-62` ("never merge … a container branch by hand or suggest doing so") may read too broadly.** Jinbe's closed-MR hold Task asks the human to "reopen or merge MR !N" (`jinbe/jinbe.go:570`, ADR 0185 §4). The line seems scoped to the behind-mainline case, but it is ambiguous.
- **ADR 0118's effective rule is moving.** ADR 0327 (outside this range) adds a pivot or Critique raise to a stuck Task exit, but no `mugicrew pivot raise` verb exists yet, so `brook/prompts/task.md:74-87` is not yet stale. Leave it to the 0221-onward audit.
- **ADR 0118 §4 (a Task carries no rollback steps) has no explicit packaged sentence.** It is implied by `crewdocs/task-body.md:12`. A strict reader could call it missing.
- **Jinbe-filed hold Tasks use free-form bodies** (ADR 0185 §4–5, 0187 §3, 0188), while Brook expects the `## Steps` format (`brook/prompts/task.md:8`). This is a Go body-format gap, not crew text, and may deserve a Signal.
- **The audit was desk-only.** Whether a crewmate in a foreign Workspace actually misreads the two missing rules has not been confirmed by a prototype run (Chart Not-yet-specified).
- **ADR 0210 and 0212 amendments were not read in full.** Their later amendments (ADR 0235, 0287) were judged from status lines only. Both fail as internal either way.
