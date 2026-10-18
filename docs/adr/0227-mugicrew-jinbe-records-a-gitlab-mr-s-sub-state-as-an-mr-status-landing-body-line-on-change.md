# ADR 0227: Jinbe records a gitlab MR's sub-state as an `MR status:` Landing body line, on change

Status: Accepted
Date: 2026-10-16

Chart mc-t72m.19 (Execute rollup shows a gitlab Landing's Merge Request sub-state).
Settles grilling mc-t72m.19.4 after research mc-t72m.19.3
(`docs/research/mr-sub-state-store-event-log-vs-landing-body-line.md`).

The Rollup is I/O-free (ADR 0198 (tui-v2-execute-checkpoints-section…)), so an
open MR's `detailed_merge_status` has to be recorded where the Rollup can read it.
ADR 0226 (a-gitlab-landing-s-mr-iid-and-sub-state…) says how it is shown. Jinbe
records it as a Landing body line. Unlike every other Jinbe Landing line,
nothing reads this one back to make a decision. It exists only to be shown.

## Decision

1. **The store is a body line**: `MR status: <raw detailed_merge_status>`, for
   example `MR status: need_rebase`. The line has no iid, because the `MR: !<iid>`
   line already names the MR. It has no timestamp either, so `d` shows no
   `since` (ADR 0226 §3). There is no event.
2. **The value is GitLab's raw status, not the qualifier word.** The mapping from
   status to word belongs to the TUI. It can change without rewriting any bodies.
3. **It is written on change only.** Jinbe compares the new status with the line in
   the Landing body it already holds and writes it with `setBodyLine` when they
   differ. A change costs one `bd update`, and a tick with no change costs nothing.
   The comparison survives a loop restart because the value lives in the bead
   graph. A new Landing starts with a blank body, so a status never carries over
   from an earlier Landing.
4. **The write happens at one point in `mirrorMR`'s opened arm**, after
   `threadArm` and `conflictArm` both decline and before the pipeline check. Every
   pending exit is covered: the fall-through, a held failed pipeline and a stalled
   one. A tick that closes the Landing never writes the line. A locked or unknown
   MR state records nothing.
5. **Transient statuses are skipped.** These are `preparing`, `checking`,
   `unchecked`, `approvals_syncing` and the empty status. The last real status
   stays in the body while GitLab rechecks. This saves the write on every
   recheck, and the qualifier no longer flickers off and on.
6. **A failed write only warns.** The tick goes on, and the next tick finds the
   body unchanged and tries again. A display write must never fail the reconcile,
   because a failed reconcile would read as `retrying`.

## Considered Options

- **Event only (`land.mr_status`).** Rejected. `landEv` keeps only the latest message
  per bead, so this event would hide `retrying`. It would also need a new attribute
  index and an in-memory map to detect changes, since ADR 0033
  (mugicrew-event-log-is-history…) bars reading the log back. A change-only event
  also disappears once it rotates out of the log, and the qualifier goes with it.
- **Both a body line and an event.** Rejected for now because nobody has asked
  for history in the Logs tab. An event can be added later with no migration, as
  long as `landEvents` skips it.
- **Storing the qualifier word.** Rejected. Changing the word map would then mean
  rewriting bodies, and `d` could no longer show the raw value.
