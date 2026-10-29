# ADR 0394: A closed effort whose tip is its cut ref does not hold the drain, and the loop reaps it

Status: Accepted
Date: 2026-10-29
Amends: ADR 0104 (integrate-conflict-pause-lifted-by-integrate…) §2 and §7; Spec D7 (a closed child holding effort/<id> is held).

Chart "A closed effort whose branch holds no work past its cut point does not hold its container's drain", settled by its Survey. Only the backstop's ExitIntegrated arm deletes `effort/<id>`. A human `mugicrew integrate` runs in the effort tree, where the branch is checked out, and deletes nothing. Once the bead closes, no backstop runs again, so the leftover ref reads as held (`DrainBlockersOver`, `MembersDrainedOver`) and the drain-gated press is refused, even though the work is already squashed onto the container. Hand integration is the documented way out of an integrate-conflict pause, so every such bead strands its container.

## Decision

1. **Tip equal to cut ref is integrated.** A won squash CAS moves `refs/mugicrew/cut/<id>` to the effort head it integrated (ADR 0264 (a-squash-integrate-replays-from-the-effort-s-recorded-cut-point…)). A branch whose tip equals its cut ref therefore holds nothing past what is on the container. A freshly cut branch that was never committed to holds no work either. The drain gates skip a closed child whose branch reads this way. They do this in the loop, in nami and in the TUI's I/O-free reader alike. The gate never depends on the reaping in point 5.
2. **One read, two sets.** The effort-ref read (`BranchRefs`/`EffortRefs`, per repo, child repos included) adds `refs/mugicrew/cut/` with `%(objectname)` to its one for-each-ref and returns a second set: the ids whose effort tip equals their cut ref. `DrainBlockersOver` and `MembersDrainedOver` skip ids in that set. The existing effort set keeps its "the branch exists" meaning, which is what the TUI detail pane and franky's surgery-end check read.
3. **Nothing to integrate still moves the cut ref.** When squash `Integrate` publishes nothing because the rebased head equals the container tip, it moves the cut ref to that head as a won CAS does. A second hand integrate, or one run after the backstop, then reads as integrated too.
4. **No cut ref, still held.** A branch with no cut ref keeps holding the drain, with today's `HeldHint`. This covers branches cut before ADR 0264 and branches where the cut-ref write failed. Integrating one by hand writes its cut ref on the winning CAS, after which point 1 applies. No `--fork-point` lookup is made, because it costs a git call per branch and has no I/O-free form.
5. **The loop reaps leftover refs.** On its tick the loop deletes `effort/<id>` and `refs/mugicrew/cut/<id>` when all of these hold: the bead is closed, its tip equals its cut ref, and no worktree has the branch checked out. It logs each deletion through `eventlog.RecordBranches`, as the backstop does. It never removes a tree. A kept tree, dirty or clean, is left to the human and to orphan-tree handling (ADR 0041 §3), and once the tree is gone the next tick reaps the branch. The gate ignores uncommitted changes in a kept tree: a closed bead's uncommitted edits are not work on its branch. With the loop off the refs remain, which is harmless under point 1.
6. **Wording.** A human integrate still prints the close instruction, and adds a clause saying the branch no longer holds the container and is reaped once the bead closes and no tree is on it. For example: `integrated; bead still open: bd close <id> (effort/<id> no longer holds <container>; the loop deletes it once the bead closes and no tree is on it)`. The "nothing to integrate … bead still open" line gets the same clause. The TUI detail pane shows `effort  effort/<id> (integrated)` for a branch in the integrated set.

The integrate verb still never closes the bead (ADR 0104 §2; ADR 0102 rejected Go closing leaf beads).

## Considered Options

- **Only reap, leave the gate alone.** Rejected: the press would stay refused until something ran a sweep, and the TUI's I/O-free reader would disagree with the loop between ticks.
- **Filter integrated ids out of the effort set at read time.** Rejected: the detail pane and franky's surgery-end check need to know the branch exists.
- **Reap in the TUI refresh or `mugicrew recover`.** Rejected: the TUI's refresh only reads, and recover is run by hand. The loop already reads these refs every tick.
- **Reap by removing a clean kept tree too.** Rejected: a human may still be working in that tree.
