# ADR 0337: Hold Task Checks prove the cleared state, and a foreign land renames after its Task closes

Status: Accepted
Date: 2026-10-22

Chart mc-6848i (Hold Tasks carry a Steps body Brook can check), grilling
mc-6848i.4, from the research note
`docs/research/hold-task-clearing-conditions-steps-and-checks.md` (§2–§8) and
ADR 0336 (hold-task-checks-probe-through-mugicrew-probe…). Amends ADR 0336 §1.

Brook runs every Check again in a final pass before it closes a Task
(brook/prompts/task.md, Done). A Hold Task's Checks therefore have to pass on
the cleared state and keep passing, and each body's last Check has to agree
with the read Jinbe, Nami or the loop makes on its next tick. The rules
below, and the bodies after them, follow from that.

## Decision

1. **No "still held" pre-checks.** A Check that tests whether the hold still
   stands would fail in the final pass after the fix. The guide's first run of
   each Check already acts as that pre-check. A separate pre-check step is
   written only for a precondition that stays true after the fix (crew doc
   task-body.md's `[irreversible]` rule otherwise stands).
2. **`MR link:` fact line.** Every MR hold body carries `MR link: <web_url>`
   (`git.MRLinkKey`) after its key line. Each Forge Do points at it
   ("on the Forge at <MR link>"). The key lines keep their own whole lines.
   The pipeline bodies on Landings and on Rollbacks put the lead first and the
   MR as a `Merge request: <ref>` fact line. The `infraTaskLead` sentence
   still opens the infra lead verbatim.
3. **`[elevated]` stays sudo, root or admin.** Pushing a trailer to a
   protected Forge main is marked `[irreversible]` only, and its Do names the
   right it needs ("needs Maintainer on <project>").
4. **A closed MR's Check requires the trailer.** A closed MR has no open
   Landing, so a merge whose squash drops the trailer would go unnoticed.
   The Check accepts `probe mr` `opened`, `probe landed <cp>` `landed`, or the
   container closed. A bare `merged` fails. The Do carries the trailer-stamp
   fallback for that case. The revert pipeline bodies accept a bare `merged`,
   because their MR stays recorded on the Rollback and a trailerless merge
   there files the trailerless Task.
5. **The trailerless clash question is computed at filing.** `holdTrailerless`
   runs the post-land clash detection over the merge itself (`<sha>^1..<sha>`)
   and writes `Sequence clash: none` or the clashing paths. Only a clash adds
   a step ("File a renumber", `Ask:`). The revert body has no clash question.
6. **`probe landed` takes a Rollback id** too, reading its
   `Reverts-<Kind>: <cp>` trailer through the same `landedBase` Jinbe reads
   (amends ADR 0336 §1).
7. **A foreign land renames after its Task closes.** `bd rename-prefix`
   renames every bead, the walked Task included, and Brook closes by the id
   its prompt rendered. The body's steps are: close the old MR on the old
   branch, stop the loop (`Ask:`), then a last `Ask:` step whose Do gives the
   rename, the branch re-cuts and the loop restart for after the close. Loop
   start's preflight refuses while a foreign land of our prefix remains, so a
   clean start is the proof. The loop is stopped first because Jinbe and Nami
   reconcile only through its dispatch, and a tick between the close and the
   rename would file the Task again (only open Tasks are searched).

## The approved bodies

Each body is a lead, fact lines, `## Steps` and, for a pipeline,
`## Diagnostics` (ADR 0335). "Agrees" names the next-tick read its final
Check matches.

- **Closed MR** (agrees: `LandedBase`, or the Task itself). One step,
  `[irreversible]`: reopen or merge MR !N at <MR link>, or
  `mugicrew withdraw <cp>`; if already merged and the Check fails, stamp the
  trailer as the trailerless body does. Check as in rule 4.
- **Trailerless land MR** (agrees: `LandedBase`). Fact lines `Merged MR: !N`,
  `Merge sha:`, `MR link:`, `Sequence clash:`. Step 1 `[irreversible]`: stamp
  `Checkpoint: <cp>` and `Graph: <ours>` in an empty commit pushed to the
  Forge's <main>, needs Maintainer. Check `mugicrew probe landed <cp>` prints
  `landed`. Step 2 only on a clash (rule 5).
- **Trailerless revert MR** (agrees: `landedBase(reverts)`). One step, as
  above with `Reverts-Checkpoint: <cp>`. Its Do says to push before anything
  else lands on <main>. Check `probe landed <rb>` prints `landed`, or the
  Rollback is closed.
- **Landing pipeline, infra** (agrees: the re-read pipeline). One step: fix
  the runner named under Diagnostics, then Retry at <Pipeline URL>. Check
  `probe pipeline '<ref>' N` prints `success`, or `probe landed <cp>` prints
  `landed`.
- **Chart pipeline, real failure** (approximate: no read exists until a new
  Landing). Step 1: file a Signal for the breakage and land its fix; Check
  `probe pipeline '<ref>' --main` prints `success`. Step 2, only for a branch
  pipeline: `r` on the Chart's row; `Ask:` is the Chart rebased onto a green
  main (a Check would chase a moving main).
- **Revert-MR pipeline, real** (agrees: `revertMRArms`). One step,
  `[irreversible]`, no command: merge or close the revert MR at <MR link>.
  Check `probe mr '<ref>'` prints `merged` or `closed`.
- **Revert-MR pipeline, infra**. The Landing infra step, whose Check also
  accepts the MR `merged` or `closed`. No separate merge step.
- **Foreign land** (agrees: the next `LandedBase` and loop start's preflight).
  Never filed under `land_strategy=local`. It keeps the "Mainline of context
  <c> holds …" lead variant. The steps are as in rule 7, with step 1's Check
  `probe mr --branch <kind>/<id>` prints `none`.

## Considered Options

- **Accept a bare `merged` on a closed MR:** rejected. The Task would close,
  the next Landing would open an empty MR, and nothing would ever hold again.
- **Clash question as an `Ask:` step, or left to Jinbe:** rejected for this
  Spec. Go knows the merge sha at filing, so the check is exact and costs
  nothing. Jinbe's blind spot on an empty trailer commit goes to its own
  Signal.
- **Rename mid-walk, keyed on a `Task suffix:` line:** rejected. It needs
  Brook to re-find its own Task, which this Destination does not touch.
- **Widen `[elevated]` to Forge rights:** rejected. `[irreversible]` already
  gets the human's yes, and the Do names the right.
