# ADR 0385: Jinbe records an open MR's head, head pipeline and merge status as MR Watch body lines, on change

Status: Accepted
Date: 2026-10-28

Spec mc-j0huj (An MR row's one word is stabilizing, all green, or GitLab's status).
A sibling of ADR 0227 (jinbe-records-a-gitlab-mr-s-sub-state…).

The TUI is I/O-free (ADR 0198 (tui-v2-execute-checkpoints-section…)), so the raw Forge
facts that make an MR **all green** have to be recorded where it can read them. ADR 0227
stores `MR status:` on the Landing, which dies with the Landing and says only GitLab's
first blocker. These lines live on the MR Watch, which outlives Landings.

## Decision

1. **The store is four display-only MR Watch body lines**, raw Forge values that
   nothing reads back to make a decision:
   - `MR head: <sha>`, the MR's full 40-character head SHA.
   - `MR pipeline: none` with no head pipeline, else `MR pipeline: <status> <id> <ran-on>`.
     The ran-on is a branch pipeline's own sha, so a stale pipeline from an earlier
     head records the older SHA; `-` when none is known (a merged-results or
     merge-train pipeline, see the ran-on lookup in Spec D5).
   - `MR threads: <open>/<resolvable>`, written only on ticks that read the
     discussions, so it keeps its last value on other ticks.
   - `MR merge status: <raw merge_status>`.
   The keys are constants in `git` beside `MR link:`'s, so the TUI parses exactly what
   Jinbe writes.
2. **`MR merge status:` records transients** (`unchecked`, `checking`,
   `cannot_be_merged_recheck`), unlike ADR 0227 §5. Keeping the last settled value would
   leave a stale `can_be_merged` after a target-branch push until the recheck ends.
   The accepted cost is a brief drop out of all green and a `bd update` there and back.
   `forge.MR` decodes the raw `merge_status` separately from `detailed_merge_status`;
   `blocking_discussions_resolved` is not decoded.
3. **Each line is written on change only**, with `setBodyLine`, compared against the
   Watch body Jinbe already holds. The lines that changed go in one `bd update`. A tick
   with no change costs nothing.
4. **Both ticks call one recorder.** The Watch reconcile's opened arm writes onto the
   Watch beside `MR link:`. `mirrorMR`'s opened arm writes where `MR status:` is
   recorded, after `conflictArm` declines, onto the container's open Watch on the same
   iid. A tick that closes the Landing, a locked or unknown MR state, and an outliving
   Watch's late pass write nothing.
5. **A failed write only warns** (ADR 0227 §6); the next tick retries.
6. **The TUI's one word is a waterfall**: stabilizing, then all green, then today's
   `mrStatusWord` word. Stabilizing comes from the Bead Graph alone. All green is
   `MR pipeline:` status `success` with ran-on equal to `MR head:`, `MR threads:` open
   `0`, and `MR merge status:` `can_be_merged`; any missing or unparseable line is not
   green. A closed container's row keeps `closing out` and shows neither new word.

## Considered Options

- **Lines on the Landing** (ADR 0227's home). Rejected: the facts are needed between
  Landings and after one closes.
- **Keeping the last settled merge status.** Rejected for the stale green it shows.
- **Recording an "on head" boolean.** Rejected: the comparison stays in the TUI so `d`
  can show both SHAs.
