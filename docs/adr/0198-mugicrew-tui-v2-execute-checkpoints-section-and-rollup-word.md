# ADR 0198: v2 Execute tab gains a Checkpoints section and a Rollup word on every open Checkpoint

Status: Accepted; §6 (tree row bracket) withdrawn by Spec "Checkpoint tree rows without the Rollup bracket": the Checkpoints section is the one place the word shows. §2 amended by ADR 0226 (a-gitlab-landing-s-mr-iid…): a gitlab Landing adds `!<iid>` and an MR sub-state qualifier.
Date: 2026-10-15

Chart mc-e3cc (Execute tab shows every open Checkpoint's rollup status).
Settles grilling mc-e3cc.4 after research mc-e3cc.2
(`docs/research/checkpoint-land-cycle-rollup-read-sources.md`) and prototype
mc-e3cc.3 (side ref `refs/mugicrew/prototypes/mc-e3cc`, replayed over 162
Checkpoints). **Reverses ADR 0170 §4**'s rejected Checkpoint branch-state chip,
and replaces the GLOSSARY Lifecycle branch-state (ADR 0002) with the **Rollup**.

ADR 0170 rejected the chip because "the snapshot can't tell a stale Approval
from one at HEAD". That objection no longer holds. One batched
`git for-each-ref refs/heads/checkpoint/*` (~1.5 ms against ~420 ms for
`bd list`) settles whether an Approval's pin is at the tip. Every other word
derives I/O-free from the bead snapshot and the in-memory event log. Once a
human approves and detaches, they need to see whether the Checkpoint landed or
is cycling through conflict and re-review.

## Decision

1. **The Rollup word**, first match wins:
   1. `landed`: a Landing closed `landed:`, the Checkpoint not yet closed
   2. `land failed`: an open Landing carries `paused:failing`. Any other
      `paused:*` reads `land paused` at the same rank.
   3. `landing`: an open Landing
   4. `conflict`: an open Conflict
   5. `blocked on <Kind>`: an open blocker of any kind (Task, Pivot, Signal …).
      Add ` +N` when there are more blockers.
   6. `fixing`: an open Critique or Surgery
   7. `awaiting approval`: an open Approval
   8. `in review`: an open Review
   9. `in build`: an open Build
   10. `approved`: an Approval closed approved, pinned at the tip, and no
       Landing yet
   11. `review due`: the members are drained, there is no open Pivot and no
       Approval at the tip
   12. `cut`: no members yet
   13. `idle ⚠`: there are members but no rule matched. This is a bug and is
       worth a Signal.

   `approved` and `review due` last one loop tick while the loop runs. They
   stay words because they persist when it is off.
2. **Qualifiers** ride beside the word, never inside it:
   - `round N`, only when N ≥ 2 (N is the number of Reviews cut)
   - `re-review`, once a Landing closed `obsolete:` or an Approval closed
     non-approved
   - `stale pin`, on `awaiting approval` when the pin ≠ the tip
   - `retrying`, on `landing` when its latest land/gate event is a failure or
     infra retry. The queued, attempting and gate-running phases are not shown.
3. **Checkpoints (N) section.** It sits between Live and the Initiative groups
   and starts unfolded (fold key `s:cps`).
   - One row per open published Checkpoint: v1's bead look, then the word and
     its qualifiers. Rows sort by rank, then `beadCmp`.
   - The rows are status only. They are not Actionable and are not counted in
     ●N. `enter` and `d` act as on the Checkpoint's tree row.
4. **Landed in last 24h (N)** is a subsection nested at the end of
   Checkpoints, folded by default.
   - A row lists a closed Checkpoint whose Landing closed `landed: <sha>`
     within 24h: v1's bead look plus `landed <sha8> · <age>`. Newest first.
   - `d` shows the Checkpoint's detail with the Landing as its why line.
   - `enter` refuses: "landed — nothing to launch".
5. **`d` explains the word** (extends ADR 0170 §5):
   - A `why:` line names the driving member, blocker or event. It carries the
     re-review cause (`after Landing obsolete: conflict`) and the stale-pin
     shas.
   - An `also:` line lists the other rules that matched, shown only when
     precedence decided between them.
6. **Tree row bracket** (the slot ADR 0170 §4 reserved): the bare word goes
   after the title and id, for example `[conflict]`. No qualifiers.

## Considered Options

- **re-review as its own word.** Rejected. It is a cycle count and lands on
  any word, so it is a qualifier.
- **Naming the blocker's id or title in the word.** Rejected. Task and Pivot
  rows already carry `holds land of → <cp>`, and `d` names the blocker.
- **Showing landing sub-phases (queued/attempting/gate running).** Rejected.
  They all mean "moving". Only `retrying` asks for attention. Superseded for
  gitlab by ADR 0226 (a-gitlab-landing-s-mr-iid…): awaiting a human merge is the
  steady state there.
- **Keeping the coarse branch-state (merged/approved/in-review/cut) beside the
  Rollup.** Rejected. Two derived "where is this Checkpoint" words would
  overload the term.
- **Merge-request words for remote-truth Workspaces.** Out of scope: offshoot
  Signal mc-lqbn. Until then `landing` covers an open MR. Now settled by ADR
  0226 (a-gitlab-landing-s-mr-iid…).
