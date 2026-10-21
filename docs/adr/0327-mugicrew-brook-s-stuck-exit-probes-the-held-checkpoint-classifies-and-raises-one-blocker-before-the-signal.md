# ADR 0327: mugicrew Brook's stuck exit probes the held Checkpoint, classifies the blocker, and raises one blocker before the Signal

Status: Accepted
Date: 2026-10-21

Chart: A Task's stuck exit raises a pivot or Critique on the Checkpoint it holds.
Settles the grilling "Decide the shape of Brook's stuck exit that offers a direct
pivot or Critique".
Amends: ADR 0118 (task-stuck-steps-irreversible-marker-no-rollback) §1 and §5
(the direct raise comes before the offshoot Signal; the stuck comment names it),
ADR 0326 (a-task-raises-through-mugicrew-pivot-raise…) §8 (a flagless first run
is a probe) and §1 (one blocker per raise).
Builds on: ADR 0325 (a-task-that-raises-a-pivot-stays-stuck…) §1, §4; ADR 0324
(a-reroute-refuses-a-landing-or-landed-checkpoint…); crew doc
checkpoint-rules.md's Critique-or-pivot test. Decided in grilling.

## Context

ADR 0326 gives Brook a verb, `mugicrew pivot raise`, and ADR 0325 says what
happens to the raising Task. Brook's stuck exit (`brook/prompts/task.md` §4)
still offers only an offshoot Signal. A Task session runs at RepoRoot, not on
the Checkpoint branch, and only Go applies the rule "exactly one open, unlanded
held Checkpoint". To classify a blocker, Brook has to find out which Checkpoint
that is.

## Decision

1. **A flagless first run is a probe.** If the Task has no raise Drafts yet,
   `mugicrew pivot raise <task>` with no flags writes nothing. It prints the
   Checkpoint it would name, that Checkpoint's Spec and branch, and any ADR 0324
   warning (open MR, open Surgery). Otherwise it prints the refusal (none,
   several, landing, landed). If raise Drafts already exist, the run reports
   them as ADR 0326 §8 says. Brook probes before it classifies. A refusal sends
   it straight to the offshoot Signal.
2. **Brook classifies read-only, and the human confirms.** Brook reads the
   Checkpoint's effective Spec (`bd show <spec>`, AMENDMENT comments included)
   and its code from the branch without checking it out
   (`git diff main...checkpoint/<id>`, `git show checkpoint/<id>:<path>`). It
   applies checkpoint-rules.md's test:
   - the code doesn't do what the effective Spec says: **Critique**;
   - following the Spec as written cannot give what the step needs: **pivot**;
   - the blocker is not in that Checkpoint's diff or Spec: **offshoot Signal**.

   When Brook is unsure, it files the Signal, and triage's reroute sorts it out.
3. **One raise is one blocker.** The Task is stuck on one step. The allowed
   shapes are: one Critique; one pivot plus one Ask Critique; or one merge into
   an open same-FR pivot plus one Ask Critique `discovered-from` that pivot.
   The verb refuses more than one `--pivot`/`--merge-into`, and requires an Ask
   Critique with either. This keeps the pivot-to-Ask pairing unambiguous. A
   second, unrelated defect Brook notices goes to an offshoot Signal.
4. **What each body carries** (value-free, under the never-record rules):
   - Pivot: title `<FR>: <what's wrong>`; body `FR: <FR>. Evidence: <why the
     Spec is wrong as written>` plus `Raised by Task <id>, step N (<title>):
     <what the step needs and what failed>`.
   - Ask Critique: title is the change the Task needs; body is the Ask (what
     the code must do so the step's Check can pass), where the code is, and
     `Unblocks Task <id> step N`.
   - Lone Critique: title is what's wrong; body is the defect, its paths on
     `checkpoint/<id>`, the fix, and the same `Unblocks Task` line. The human
     uses that line to match the Critique to the Task in the goto row.
5. **One approval ask, before the verb runs.** It names the classification,
   the Checkpoint by title and each body's title. For a pivot it states plainly
   that a pivot freezes the whole Checkpoint until it is triaged, and it shows
   any probe warning. There are three answers: *raise*, *file an offshoot
   Signal instead*, *neither*.
6. **The stuck comment names the raise**: `step N (<title>) stuck: <what failed
   and what was tried>; raised pivot <id> + Critique <id> on Checkpoint <id>,
   wait for Critique <id> to close`, or `raised Critique <id> …` for a lone
   Critique.
7. **When Brook falls back to the offshoot Signal**: the probe or the verb
   refuses, the human chooses the Signal, the classification is unsure, or the
   blocker is outside the Checkpoint. A blocker never gets both a raise and a
   Signal.

## Considered Options

- **Brook reads the Task's BLOCKS rows itself, and the verb's refusal is the
  backstop.** Rejected: it duplicates the one-Checkpoint rule, and Brook at
  RepoRoot can't reliably read the landed trailer (ADR 0326's reason for having
  no `--checkpoint` flag).
- **Classify from the Spec and the failure output, without reading the code.**
  Rejected: that can't tell a defect from a wrong FR.
- **The human classifies.** Rejected: HITL is authorization, not authorship.
  Brook proposes and the human confirms.
- **Several pivots in one raise.** Rejected: nothing pairs each Ask Critique
  with its pivot, and a stuck Task has one blocker.
