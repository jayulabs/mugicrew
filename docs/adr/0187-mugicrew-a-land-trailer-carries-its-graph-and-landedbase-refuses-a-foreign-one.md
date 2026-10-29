# ADR 0187: A land trailer carries its Graph, and LandedBase refuses a foreign one

Status: Accepted
Date: 2026-10-14

Chart mc-t72m.2 (GitLab Merge Request land strategy for a single-repo
Workspace). Settles grilling mc-t72m.2.7. Under `land_strategy=gitlab`, several
people share one mainline on the Forge, but each person drives their own personal
Bead Graph. Two graphs that share an `issue_prefix` can mint the same bead id. The
`<Kind>: <id>` trailer is the only landed fact (ADR 0010, ADR 0145 §3), so a
foreign `Checkpoint: mc-abcd` on mainline would read as our bead's land to every
`LandedBase` caller. The graph id is the Dolt root commit hash
(docs/research/stable-per-graph-identity-for-the-graph-trailer.md).

## Decision

1. **The trailer block gains a `Graph:` line.** One helper writes the block for
   every land under every strategy: `Tip: <sha>` (when the land has one), then
   `<Kind>: <id>`, then `Graph: <root-hash>`. This covers the land squash, the
   Spec anchor and the Rollback revert. Under gitlab the block goes in both
   `squash_commit_message` and the MR description. `squash_commit_message` is the
   guarantee, and the description feeds the template fallback. Lineage merges and
   integrate squashes carry no `Graph:` line: lineage merges aren't keyed on a
   bead id, and integrate squashes never reach mainline.
2. **`LandedBase` checks it under gitlab only.** A trailer match whose `Graph:`
   is ours counts as landed. A match with no `Graph:` line is a legacy land and
   also counts as ours. A match whose `Graph:` differs returns
   `ForeignLanding`, which names both graph ids and the sha. That is neither
   landed nor `ErrNotLanded`. Under local the line is written but never read, so
   a later switch to gitlab finds clean history.
3. **The container is held on a Task.** On `ForeignLanding` the Landing
   stays open and the caller returns an error. Jinbe, Nami and the loop's
   candidate scan (a skipped container is never dispatched to a reconcile) all
   call `bd.HoldForeign`, which files a HITL Task, owned by Brook,
   that blocks the container. It names both graphs, the foreign sha and the fix:
   `bd rename` the bead, or `bd rename-prefix`, then re-cut the branch and MR.
   This is the same pattern as ADR 0185 §4–5.
4. **The other callers.** Zoro's cut base, Luffy's triage, the loop's Chart and
   Checkpoint checks hold the container and skip it (the tick goes on), and
   `integrateRefusal` propagates the error and refuses. The
   TUI's landed badge and `deleteLanded` read it as not landed and stay silent.
5. **`loop start` preflights under gitlab.** If mainline holds a trailer with
   our `issue_prefix` and a different `Graph:`, the loop refuses to start and
   points to the README. The loop does not rerun this check mid-run, because a
   later collision surfaces through `LandedBase` at the bead it hits.
6. **Each person needs a distinct `issue_prefix`.** The README's gitlab setup
   section says so. mugicrew does not derive the prefix from the GitLab
   credential.

## Considered Options

- **Treat legacy lands with no `Graph:` line as foreign.** Rejected. Every Chart
  and Checkpoint that landed before the switch would look foreign, which breaks
  Zoro's cut base and the TUI. A collision with a pre-Graph land goes undetected,
  but a shared prefix only becomes possible once a second person shares the
  Forge, which is after the switch.
- **Check only in Jinbe's gitlab Landing.** Rejected. The six other
  `LandedBase` callers would still take a foreign land as ours.
- **Use the GitLab username as `Graph:`.** Rejected. It collides when one person
  runs two unsynced graphs, and local lands have no credential to read. The
  username names the graph's sync ref instead: offshoot Signal "Each personal
  Bead Graph syncs to a ref on the Forge", outside `refs/dolt/`.
- **Derive `issue_prefix` from the GitLab username.** Rejected. Bead ids and
  branch names already carry the prefix, and the refusal plus the preflight
  already catch a shared prefix.

## Amendment: run-now refuses a foreign land and files the hold too

The shared eligibility verdict (Chart "One eligibility module for loop dispatch
and TUI run-now") reports a `ForeignLanding` from `LandedBase` as its own
cause, `foreign-landing`, carrying the foreign sha and both graph ids. It
refuses in both modes. That cause is not `paused:foreign-hold`: a foreign land
never writes a pause label, and run-now's override of `paused:foreign-hold`
(ADR 0055 (paused-beads) §6) does not reach it. The verdict stays pure. §3
gains a fourth filer: whichever caller reads the `foreign-landing` cause calls
`bd.HoldForeign`. That means the loop's candidate scan and, now, run-now's
press-time gate. So a press files the Errand without waiting for a running
loop, and the Errand stays idempotent on (container, foreign sha). If the
filing fails, the press is refused with that error, just as it fails the
loop's tick. The TUI row hint reads the snapshot without I/O and never sees
the trailer. It never files the hold, and the row reads as held only once the
Errand blocks it.
