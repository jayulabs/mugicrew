# ADR 0221: Require Initiatives gates surgery, release and reconcile, and exempts Rollback successors and Tasks

Status: Accepted
Amended by ADR 0241 (§1: the U form's picker is now the Surgery form's target list).
Date: 2026-10-16

From Chart mc-8ymw.5 (Opt-in Require Initiatives and Enforce external ID
Workspace settings), grilling "Which Initiative a surgery Checkpoint and a Task
join under Require Initiatives, and what the gaps the inventory found must do".
The human ruled that enforcement bites only at triage, with no refusal at
crew spawn sites and no check at land. The beside-path inventory
(docs/research/spine-bead-create-and-reparent-paths-vs-require-initiatives.md)
found six paths that this triage-only gate does not cover (H1 to H6). This ADR
settles each one. Every rule below applies only while `required` is on. With
the setting off, behaviour is unchanged.

## Decision

1. **A fresh surgery Checkpoint takes its Initiative at filing (H1).**
   `mugicrew surgery new` gains `--initiative <id>`, which must name an open
   Initiative, and the TUI `U` form gains a picker of open Initiatives. The
   Checkpoint is born with that parent. Under `required`, filing without one is
   refused. Off the setting, the flag is optional. Surgery is the human's own
   filing verb, and the human is present at it, so this gate counts as triage's
   stand-in, not as a crew spawn-site refusal. `surgery new --checkpoint`
   inherits the joined Checkpoint's membership and does not change.
2. **"Rollbacks are exempt" covers the successor Checkpoint (H2).** The
   successor joins the rolled-back Checkpoint's Initiative while that
   Initiative is open, as ADR 0133 §6 says, and is born outside otherwise.
   Nothing reopens a closed Initiative, and the Rollback filing does not ask
   for one.
3. **Tasks are exempt (H3).** "Typically in one" describes, it does not
   enforce. Tasks stay free-standing on every crew path, as they have been since
   ADR 0113. A human may still `initiative adopt` a Task.
4. **Release refuses and points at adopt (H4).** `mugicrew initiative release`
   and the TUI `A` refuse with a message to use
   `mugicrew initiative adopt <head> <other>`, which already moves a head in one
   call. adopt stays the single move verb.
5. **reconcile is the triage gate in Go (H5).** ACCEPT (`reconcile`) and
   FAST LANE (`reconcile --spec`) refuse a Signal whose parent is not an open
   Initiative, so adoption must run first. INITIATIVE, REJECT and DUPLICATE are
   unaffected. This backs up the triage.md section rather than adding a spawn-site
   refusal: every triage outcome goes through reconcile.
6. **A done-close with live members is accepted (H6).** If an Initiative closes
   while members are unclosed, their later successors are born outside, as
   today. They appear in the No Initiative section like grandfathered work
   (ADR 0220 (mugicrew-switching-require-initiatives-on…)). No successor spawn
   refuses.

## Considered Options

- **Surgery: optional Initiative, adopted later.** Rejected: it leaves the
  "must be in one" rule unenforced on the one path triage never sees.
- **Rollback: require an Initiative at filing, or reopen the closed one.**
  Rejected: a revert is not new work, so picking an Initiative for it is
  friction for no gain, and reopening contradicts ADR 0133 §4.
- **Tasks: parent drafted Tasks under the drafting Chart's or Spec's Initiative.**
  Rejected: a member Task keeps its Initiative under way, so a Task blocked on a
  land would hold the drain past the last land, possibly for good.
- **Release with a required target.** Rejected: that is a second spelling of
  adopt.
- **Prompt-only triage gate.** Rejected: if a model skips adopt, the chain
  silently ends up outside every Initiative.
- **Refusing successor spawns under a closed Initiative.** Rejected: the human
  ruled out spawn-site refusals, and No Initiative already shows the orphans.
