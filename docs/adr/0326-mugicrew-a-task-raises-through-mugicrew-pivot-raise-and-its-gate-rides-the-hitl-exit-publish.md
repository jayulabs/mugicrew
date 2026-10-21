# ADR 0326: mugicrew a Task raises a pivot or Critique through `mugicrew pivot raise`, and its gate rides the HITL exit's publish

Status: Accepted
Date: 2026-10-21

Chart: A Task's stuck exit raises a pivot or Critique on the Checkpoint it holds.
Settles the grilling "Decide where Go adds the Checkpoint gate for a pivot a
Task raises". Reads `docs/research/task-raised-pivot-and-critique-reader-writer-inventory.md`.
Amends: ADR 0043 (pivot-raise-and-checkpoint-gate) §4, ADR 0322
(a-signal-reroute-is-a-flag-seeded-reconcile-leg…) §3 and §4.
Builds on: ADR 0322 (a-signal-reroute-is-a-flag-seeded-reconcile-leg…), ADR 0324
(a-reroute-refuses-a-landing-or-landed-checkpoint…), ADR 0045 (concurrent-pivots) §1,
ADR 0115 (brook-owns-task-human-started-guide-closed) §5. Decided in grilling.

## Context

A Task session is HITL at RepoRoot with no `afterRun` (ADR 0115 §5), so the
Franky/Usopp tail that adds a pivot's Checkpoint gate (ADR 0043 §3) has no Brook
counterpart. The one Go point on a Task's exit is `HitlRun`'s `PublishDrafts`.
A Task is unparented: it holds its Checkpoint through an incoming `blocks` edge,
so `bead.Parent` names nothing. The sibling Chart's Signal reroute already
turned `PublishDrafts`' close into a publish plan that hands the gate (ADR 0322
§4).

## Decision

1. **A Go verb, run by Brook at the stuck exit** after the human approves:
   `mugicrew pivot raise <task> [--pivot <body-file>]... [--critique <body-file>]...
   [--merge-into <pivot> <evidence-file>]...`. Go creates each pivot and
   Critique as a Draft by the Task (`drafted-by:<task>`; a Critique parented to
   the Checkpoint). The agent writes every body, its evidence and its FR; Go
   writes every bead and edge, the ADR 0043 §3 split.
2. **Go names the Checkpoint; there is no flag.** It reads the Task's `up`
   `blocks` targets, keeps the `kind:checkpoint` ones open in bd, and drops any
   whose `Checkpoint:` trailer is on its repo's mainline. Exactly one must
   remain, with a Spec `SpecOf` resolves; none or several refuses before any
   write, telling Brook to fall back to the offshoot Signal.
3. **ADR 0324 applies unchanged.** The verb refuses an open Landing or a landed
   trailer, and warns on an open MR or an open Surgery; the publish re-checks
   every refusal; the pivot set is not re-checked.
4. **The gate keys on metadata, not on the creator's kind.** The pivot Draft
   records its Checkpoint as `{"raise_checkpoint": "<cp>"}` (renamed from ADR
   0322 §3's `reroute_checkpoint`: "reroute" is triage's word). The publish plan
   hands the Checkpoint `blocks` edge and the pivot's `related` edge to the Spec
   for every pivot Draft carrying it, whoever drafted it: one rule serves the
   Signal reroute and the Task raise. A Critique-only raise hands no gate.
5. **Go writes the Task-to-raise edges.** Whatever their type (left to "Decide
   what happens to a Task that raised a pivot or Critique, on each outcome"),
   an edge onto a Draft is written by the verb at Draft creation, and a merge
   edge onto a published pivot rides the publish batch. Brook never runs
   `bd dep add` for them.
6. **The raise does not close the Task.** `sessionClosing` gets no Task arm and
   the publish plan never closes it; it stays `in_progress` and claimed, as
   `settle` already leaves it. ADR 0043 §4's `pivoted:` close does not apply to
   a Task raiser: it would release the Task's land hold.
7. **Merge into an open same-FR pivot by flag** (ADR 0045 §1), as ADR 0322 §7:
   the agent judges same-FR; the verb checks the target is an open, published
   pivot gating this Checkpoint (never a Draft) and writes nothing; the evidence
   comment is posted after the batch, guarded by a marker line naming the Task.
8. **Idempotency, one raise per session.** A drop or relaunch deletes the raise
   Drafts and their metadata. A non-zero exit keeps them for a resume. A
   flagless re-run reports the existing raise Drafts and their Checkpoint; a
   flagged re-run while raise Drafts by the Task exist is refused ("drop the
   session to change the raise"), so Brook gathers every body before one call.
   A crash mid-publish wrote nothing; present edges are filtered.
9. **One eventlog record**, `task.raise`, written once the checks pass and the
   Drafts exist: the Task, the Checkpoint and the pivot, Critique and merge IDs.

## Considered options

- **A Task leg of `mugicrew reconcile`.** Literal reuse, but reconcile drives a
  bead to its next state, and a Task's is the human's yes. ADR 0322's reason for
  a reconcile leg (a plain re-run must not take the Chart leg) has no Task
  counterpart.
- **Brook `bd create`s the Drafts and metadata itself.** The hand-wiring ADR
  0035 (signal-triage-fast-lane) §10 and ADR 0322 reject.
- **A Task arm in the publish plan, keyed on the creator's kind.** A second rule
  for the same wiring.
- **A `--checkpoint` flag.** Only a way to get it wrong: Go can check the
  Chart's "exactly one open, unlanded Checkpoint the Task holds", and Brook at
  RepoRoot cannot reliably run the trailer read.
- **Let a later call add to existing raise Drafts.** Nothing reliable matches a
  partial set to its intent across calls.
