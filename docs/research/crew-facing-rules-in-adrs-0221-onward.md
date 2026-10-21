# Crew-facing rules in ADRs 0221 onward that the packaged crewmate text lacks

Status: Research (input to Settle the final lift list and placement for each gap, mc-8z95b)
Date: 2026-10-21
Bead: mc-8z95b.4

## Purpose

This doc audits every ADR file from 0221 to the highest number present,
`apps/mugicrew/docs/adr/0327-…`. That is 112 files, and the duplicated numbers
0319, 0320 and 0322 each count as separate files. Each ADR is checked against
the Chart's lift test (Chart Notes, "Survey (human-settled)"). A rule is lifted
iff all four of these hold:

1. It governs a crewmate session's action or judgement.
2. Nothing in the binary or a mugicrew verb enforces it, unless the refusal
   would mislead a crewmate.
3. No packaged text states it yet: not `apps/mugicrew/contract/CREWMATE.md`, a
   crew doc (`apps/mugicrew/crewdocs/*.md`), or a role prompt
   (`apps/mugicrew/*/prompts/*.md` and the Go splices). Packaged text that is
   stale counts as not stating it.
4. It reads as Workspace-agnostic guidance.

Placement follows ADR 0088 (mugicrew-crewmate-content-split) §1, §3 and §7:

- text with one consumer goes inline in its role prompt;
- text with several consumers goes in a crew doc;
- universal text goes in CREWMATE.md, which is capped at about 2 KB. It is
  already at 2045 bytes (`wc -c apps/mugicrew/contract/CREWMATE.md`), so
  nothing below proposes it.

The audit was a desk audit of the tree at `chart/mc-8z95b`, which was `main` at
`8c9e4e1`. Where an ADR's implementation sits on an unlanded Checkpoint branch,
the doc says so.

## 1. Summary

Seven ADRs pass the test. 105 fail.

| ADR | Gap | Consumer | Placement |
|---|---|---|---|
| 0262 | missing (outside Brook) | build, surgery, research, charting, defog, review | `crewdocs/signal-filing.md` |
| 0264 | missing, and exit-list stale | build, conflict, surgery, research, defog skeleton | each inline integrate-exit list |
| 0275 | missing (§6 residue) | Chopper checkup | `chopper/prompts/checkup.md` |
| 0279 | stale | Robin retrospective | `robin/prompts/retrospective.md` |
| 0325 | missing, and `checkpoint-rules.md` stale | Zoro pivot, Brook task | `zoro/prompts/pivot.md`, `crewdocs/checkpoint-rules.md` |
| 0326 | missing | Brook task | `brook/prompts/task.md` |
| 0327 | stale | Brook task | `brook/prompts/task.md` |

ADRs 0325–0327 are already being implemented on `checkpoint/mc-11qbr` (§8).
Their gaps are on track to close when that Checkpoint lands, apart from the
`checkpoint-rules.md` line flagged in §6.

## 2. ADR 0262 (a-requested-rebase-is-a-human-filed-conflict-variant…): never file a requested rebase yourself

**Rule.** Never run `mugicrew rebase` and never rebase a container branch by
hand. If an open Chart or Checkpoint branch should be on current mainline,
suggest a requested rebase through an offshoot Signal, or ask the human in a
HITL session. Only the human triggers a container rewrite.

**Gap: missing for every role except Brook.**

- The ADR states it: "Agents never file one; they may suggest one by offshoot
  Signal" (ADR 0262, bullet "Human-filed only, no triage").
- Only `apps/mugicrew/brook/prompts/task.md:58-64` says it ("Never run the
  mugicrew rebase verb yourself, and never merge or rebase a container branch
  by hand"). That text is scoped to a Task's failing step.
- Nothing enforces it. `apps/mugicrew/rebase_verb.go:17-44` files the request
  for any caller and has no agent check.

**Consumers.** Every session that can notice a stale container:

- `franky/prompts/build.md` and `franky/prompts/surgery.md`
- `nami/prompts/research.md` and `nami/prompts/charting.md`
- `crewdocs/defog-session.md`
- `usopp/prompts/review.md`

**Placement.** Add one line under the Offshoot form in
`crewdocs/signal-filing.md`. The rule has several consumers, and its remedy is
the offshoot filing that doc already owns.

## 3. ADR 0264 (a-squash-integrate-replays-from-the-effort-s-recorded-cut-point…): integrate exits 3–5

**Rule.** Handle a refused `mugicrew integrate` as follows:

- The container was rewritten past your cut (no cut point), or a replace bead
  is off its pinned tip: do not repair or re-rebase branches by hand. Note the
  output on your bead and stop. mugicrew pauses, re-pins or closes the bead.
- The container already landed: also note it and stop.

**Gap: missing, and the existing exit lists are stale.**

- `session/integrate.go:30-36` defines six exits: 0 integrated, 1 transient,
  2 conflict/held, 3 landed, 4 no cut point, 5 off pin. `main.go:1671-1675`
  documents the same set.
- The no-cut-point error says "the container was rewritten; nothing integrated"
  (`git/git.go:2364`).
- Every packaged integrate list names only exits 0, 1 and 2, and files "landed"
  under exit 2:
  - `franky/prompts/build.md:80-86`
  - `franky/prompts/conflict.md:67-72`
  - `franky/prompts/surgery.md:85-92`
  - `nami/prompts/research.md:39-44`
  - `crewdocs/defog-session.md:80-85`
- The misfiled "landed" is ADR 0102's gap (see Open questions). This ADR adds
  only exits 4 and 5.
- Only `nami/prompts/promote-or-stamp.md:89` has a catch-all "any other exit".
- This passes criterion 2 through its exception. The binary enforces the
  refusal, but "container was rewritten" invites a crewmate to repair the
  branch, which is the wrong action.

**Placement.** In each inline list, widen "or exit 1" to "or any other exit"
and correct "(held or landed)" so it reads exit 2 for held and exit 3 for
landed. The lists are already per role, and the defog skeleton's copy lives in
its crew doc. A shared integrate-exit crew doc would be the ADR 0088 home if
the five lists are ever consolidated. Leave that to the consuming Spec.

## 4. ADR 0275 (a-checkup-diagnoses-cheap-reads-first…): every plan declined

**Rule.** If the human declines every repair plan, the Checkup closes with
neither `repaired:` nor `not a bead fault:`. It stays open, and the human
drops it.

**Gap: missing.** This is ADR 0275 §6, third bullet.
`chopper/prompts/checkup.md:79-92` gives only the two close reasons. "Declined"
appears only inside the `repaired` reason (`:85`), so an all-declined session
could be closed as `not a bead fault`. §1–§5 are already stated at
`checkup.md:8-50`.

**Placement.** Inline in the Done section of `chopper/prompts/checkup.md`. It
has a single consumer.

## 5. ADR 0279 (a-retrospective-accepts-the-event-log-horizon…): coverage horizon

**Rule.** A retrospective's coverage line counts a bead as lost only if it
closed before the oldest record in the session index. That record is the first
line of `sessions.jsonl.1`, else `sessions.jsonl`. If neither exists, fall back
to the event log.

**Gap: stale.**

- `robin/prompts/retrospective.md:23-27` takes the horizon from
  `events.jsonl.1`, else `events.jsonl`. That was ADR 0279 §3's rule when the
  ADR said "No new index here" (§1).
- ADRs 0291–0294 later added `sessions.jsonl`, which rotates separately.
  Session lookup now reads "sessions.jsonl.1, sessions.jsonl, events.jsonl.1,
  events.jsonl" (`transcript/sessions.go:24`).
- After `events.jsonl` rotates, the event-log horizon moves past sessions the
  index still finds. Beads that closed in between are then miscounted
  "closed before the event log's oldest event".
- ADR 0279 §6 says it is prompt-only, so nothing enforces it.

**Placement.** Inline in step 3 of `robin/prompts/retrospective.md`. It has a
single consumer (ADR 0280 §1 already places the procedure there).

## 6. ADR 0325 (a-task-that-raises-a-pivot-stays-stuck…): a Task raiser never closes `pivoted:`

**Rule.**

- A Task that raises a pivot or Critique on its held Checkpoint never closes
  `pivoted:`. It stays claimed and stuck, and its stuck comment names the raise.
- The pivot reviewer handles the raise by outcome:
  - on dismiss or amend, it comments the outcome on the Task;
  - on confirm or escalate, the corrected Spec's `## Tasks` adopts every open
    Task holding the old Checkpoint as `task: <id>`.

**Gap: missing, and `checkpoint-rules.md` is stale.**

- The ADR sets the rule in §1, §5, §6 and §7.
- `crewdocs/checkpoint-rules.md:63-65` reads: "Go … closes your bead
  `pivoted: <pivot>` in the same publish as your Drafts". That is wrong for a
  Task raiser.
- `zoro/prompts/pivot.md:83-135` has no Task comment and no `task: <id>`
  adoption.
- On `checkpoint/mc-11qbr`, the slicing side is already written
  (`zoro/prompts/slicing.md:73-117`, `crewdocs/spec-template.md:76-84`). The
  Zoro pivot side is still open there.

**Consumers.** `zoro/prompts/pivot.md` and `brook/prompts/task.md`.

**Placement.**

- The Zoro duties go inline in `zoro/prompts/pivot.md`, since they have a
  single consumer.
- The "Task raiser stays stuck" exception goes in `crewdocs/checkpoint-rules.md`
  after the Raise section. It has two consumers, and it fixes the stale line
  that Brook will read once ADR 0327 points it at that doc.

## 7. ADR 0326 (a-task-raises-through-mugicrew-pivot-raise…) and ADR 0327 (brook-s-stuck-exit-probes-the-held-checkpoint…): Brook's raise

**Rule (0326).** After the human approves, a Task guide raises only through
`mugicrew pivot raise <task> [--pivot f] [--critique f] [--merge-into <pivot> f]`.
It never hand-wires `bd create` or `bd dep add` for the raise, and never closes
the Task. It gathers every body before its single call, and judges same-FR
itself to choose `--merge-into`.

**Rule (0327).** At a stuck exit, the guide works in this order:

1. Probe with a flagless `pivot raise`. On a refusal, go straight to the
   offshoot Signal.
2. Classify the blocker against the effective Spec and the Checkpoint diff:
   - code that departs from the Spec is a Critique;
   - a Spec that cannot give what the step needs is a pivot;
   - anything outside both is a Signal;
   - when unsure, file a Signal.
3. Raise exactly one blocker, after one approval ask that warns a pivot freezes
   the Checkpoint.
4. Name the raise in the stuck comment, and never file both a raise and a
   Signal for one blocker.

**Gap: 0326 missing, 0327 stale.** `brook/prompts/task.md:81-87` offers only
"an offshoot Signal … on the human's approval", and its stuck-comment template
(`:79`) has no raise clause. Neither the verb nor its prompt text is on `main`.
The text is also unchanged on `checkpoint/mc-11qbr`. Once built, the verb
enforces the one-pivot limit and the Ask Critique requirement (ADR 0327 §3).
The probe, the classification and the "never both" rule stay agent judgement.

**Placement.** Inline in §4 of `brook/prompts/task.md`, since it has a single
consumer. It should point to `crewdocs/checkpoint-rules.md` for the
Critique-or-pivot test (`:40-42`) rather than restate it.

## 8. In-flight implementation (0322–0327)

- **`checkpoint/mc-11qbr` (Task raise, ADRs 0325–0327).** Its open Builds are
  the natural carriers for §6 and §7.
- **`checkpoint/mc-clzli` (Signal reroute, ADRs 0322–0324).** It already
  states the reroute rules:
  - `crewdocs/checkpoint-rules.md:68-73`
  - `luffy/prompts/triage.md:36-47,125-151`
  - `zoro/prompts/pivot.md:29-113`

  Those ADRs are therefore listed as covered or enforced below, contingent on
  that branch landing. If it is abandoned, 0322–0323 become missing.

Both branches exist locally (`git branch --list 'checkpoint/mc-11qbr'
'checkpoint/mc-clzli'`).

## 9. ADRs that fail the test

- 0221 covered (`luffy/triage.go:83-95`)
- 0222 covered (`contract/contract.go:162`, latch verb takes no parent)
- 0223 internal
- 0224 covered (`luffy/triage.go:79-95`)
- 0225 covered (`crewdocs/sequence-renumber.md:12-18`)
- 0226 internal
- 0227 internal
- 0228 internal
- 0229 internal
- 0230 internal
- 0231 internal
- 0232 internal
- 0233 internal
- 0234 internal
- 0235 internal
- 0236 internal
- 0237 covered (`luffy/prompts/pane.md:7-13`)
- 0238 internal
- 0239 internal
- 0240 internal
- 0241 covered (`luffy/prompts/approval.md:85`)
- 0242 internal
- 0243 internal
- 0244 internal
- 0245 internal
- 0246 internal
- 0247 internal
- 0248 covered (`franky/prompts/build.md:52`)
- 0249 internal
- 0250 internal
- 0251 enforced (`transcript_verb.go:73`)
- 0252 covered (`luffy/triage.go:150-193`)
- 0253 internal
- 0254 internal
- 0255 covered (`luffy/triage.go:266-291`)
- 0256 internal (superseded by 0300)
- 0257 internal (superseded by 0300)
- 0258 internal
- 0259 internal
- 0260 internal
- 0261 enforced (`rebase_verb.go:33-36`)
- 0263 enforced (`rebase_verb.go:27-29`)
- 0265 internal
- 0266 internal
- 0267 covered (`crewdocs/signal-filing.md:16-32`)
- 0268 internal
- 0269 internal
- 0270 covered (`crewdocs/approval-body.md:42`)
- 0271 internal
- 0272 covered (`chopper/prompts/checkup.md:1-6,83-92`)
- 0273 covered (`chopper/prompts/checkup.md:37-75`)
- 0274 internal
- 0276 covered (`chopper/prompts/checkup.md:45-50`)
- 0277 covered (`robin/prompts/retrospective.md:14-22`)
- 0278 covered (`robin/prompts/retrospective.md:33-35,59-74`)
- 0280 covered (`robin/prompts/retrospective.md:1-13`)
- 0281 enforced (`bd/spine.go:28`)
- 0282 covered (`robin/prompts/retrospective.md:48-56`)
- 0283 internal
- 0284 internal
- 0285 internal
- 0286 internal
- 0287 covered (`luffy/prompts/triage.md:74-79`)
- 0288 internal
- 0289 internal
- 0290 internal
- 0291 internal (its crewmate effect is §5)
- 0292 internal
- 0293 internal
- 0294 internal
- 0295 internal
- 0296 internal
- 0297 internal
- 0298 internal
- 0299 internal
- 0300 covered (`contract/CREWMATE.md:3-4`)
- 0301 internal
- 0302 internal
- 0303 internal
- 0304 internal
- 0305 internal
- 0306 internal
- 0307 internal
- 0308 internal
- 0309 internal
- 0310 internal
- 0311 internal
- 0312 internal
- 0313 internal
- 0314 internal
- 0315 internal
- 0316 covered (`zoro/prompts/slicing.md:11,97-98`)
- 0317 covered (`usopp/prompts/forge-review.md:22-45`)
- 0318 covered (`usopp/prompts/forge-review.md:38-57,95-106`)
- 0319 (a-filed-comment-is-closed-out…) internal
- 0319 (the-first-forge-review-on-an-already-open…) covered (`usopp/prompts/forge-review.md:43-45`)
- 0320 (a-failed-limited-or-aborted-forge-review…) internal
- 0320 (a-forge-review-surfaces-as-forge-review-events…) internal
- 0320 (an-edit-to-an-acked-merge-request-note…) internal
- 0321 internal
- 0322 (a-rerouted-signal-s-pivot-or-critique…) covered (on `checkpoint/mc-clzli`)
- 0322 (a-signal-reroute-is-a-flag-seeded-reconcile-leg…) covered (on `checkpoint/mc-clzli`)
- 0322 (the-reroute-check-is-a-go-read-verb…) covered (on `checkpoint/mc-clzli`)
- 0323 covered (on `checkpoint/mc-clzli`)
- 0324 enforced (on `checkpoint/mc-clzli`, `luffy/reroute.go:280-302`)

No ADR in this range failed only as repo-specific.

## Open questions / gaps

- **ADR 0264 is a borderline pass.** It passes only through the "refusal would
  mislead" exception. The settling bead should confirm that "container was
  rewritten" really invites a hand repair, or else drop it to enforced.
- **Exit-list drift beyond 0264 (traced).** "Exit 2 … (held or landed)"
  contradicts `ExitLanded = 3` (`session/integrate.go:33`). That exit predates
  this range: ADR 0102 (leaf-efforts-integrate-onto-ref-only-containers) already
  assigns "3 the container already landed" (`docs/adr/0102-…md:66`, `:98`).
  The "landed" half of the stale lists belongs to the 0001–0110 audit
  (mc-8z95b.2, which flags it under 0102). Only exits 4 and 5 are 0264's.
- **No pause causes for Chopper.** `chopper/prompts/checkup.md:14` tells it to
  read "the lifter of each" pause cause, but no packaged text lists the causes
  or their lifters, including `paused:not-built` (ADR 0248). This is not
  specific to one ADR here, so it was not counted. It may belong to the
  glossary-terms bead (mc-8z95b.5).
- **0322–0327 depend on unlanded Checkpoints.** Their verdicts hold only if
  `checkpoint/mc-clzli` and `checkpoint/mc-11qbr` land as planned. The settling
  bead should check that `mc-11qbr`'s open Builds carry the §6 and §7 text,
  including the stale line in `checkpoint-rules.md`.
- **Unaudited beads.** Grilling, prototype and Approval beads under those
  Charts were not inspected for further crew-facing text.
- **Placement path.** The bead names `apps/mugicrew/docs/research/` for this
  doc, while the research prompt names `docs/research/`. The bead's path was
  used.
