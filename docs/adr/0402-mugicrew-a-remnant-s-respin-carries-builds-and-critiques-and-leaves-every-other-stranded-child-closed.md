# ADR 0402: A Remnant's respin carries Builds and Critiques and leaves every other stranded child closed

Status: Accepted
Date: 2026-10-29

Chart "A Forge merge that strands in-flight Checkpoint work asks the human how to recover it" settled that every open child of a landed gitlab Checkpoint closes `obsolete: container landed; carried by <remnant>`, and that the Remnant is not takeable while a child still runs. So when the human picks an exit, each stranded child is either unstarted or finished and refused. This ADR fixes, per child kind, which ones a respin re-files onto the new Checkpoint and what drop and offshoot do with them.

## Decision

1. **Respin re-files work, not process.** These are re-filed as new **open** members of the respin Checkpoint. Each new bead `supersedes` its old, closed child and is listed in the Remnant's body:
   - an unstarted Build, with the same title and body;
   - a finished Build refused at integrate. It adopts the kept branch: `effort/<old>` and `refs/mugicrew/cut/<old>` are copied to the new id, so its commits integrate without a rerun;
   - every Critique, unstarted or finished. Its `Base:`/`Tip:` lines are dropped so it integrates in Squash mode (otherwise `offPin` closes it moot and deletes the branch). It adopts its kept branch if it has one.

   Every re-filed member is blocked by the respin's replay seed. Its body or kept branch was written against a container that may already hold part of `Tip..head`.
2. **Respin leaves these closed.**
   - A Conflict and any other replace-mode child (a requested rebase, a `Base:`-pinned Critique's rewrite) rewrote the old container's base, and a fresh cut has none. Its kept branch is evidence in the Remnant's diff only. Any conflict the replay hits gets a new Conflict through the normal path.
   - An Approval under re-review is replaced by the respin's own Draft-MR Approval (ADR 0382 (under-gitlab-the-un-draft…)).
   - A bare Review, Surgery, Forge Review, or Draft. The respin gets its own Review, and Drafts retire as ADR 0201 (a-landed-container-s-child-closes-obsolete…) §4 retires them.
3. **Errands.** A Hold Errand clears as it does today (`ClearHolds`, `cleared: <key line>`). A before-land Errand (no key line) carries under respin: its blocks edge is copied onto the respin Checkpoint, the same way a Chart's Errands carry to its Checkpoint. Under drop or offshoot it closes `obsolete: dropped by <remnant>`, or `obsolete: offshoot by <remnant>`.
4. **An open pivot blocks the Remnant.** A pivot can change the Spec, and a respin is cut against that Spec. So the human resolves the pivot before choosing any exit. It is never carried to, or superseded by, the respin.
5. **Drop** re-files nothing. It deletes the kept container branch and `effort/*` refs once the TUI confirms.
6. **Offshoot** files **one** Signal for the whole Remnant. The Signal lists every stranded child with its kept `effort/*` ref, plus the `Tip..head` range. It replaces today's one-Signal-per-child `carryStranded` path for this case. Kept refs stay for whoever picks the Signal up.

## Considered Options

- **Close unstarted Critiques and rely on the respin's Review to find the defect again.** Rejected: nothing guarantees a fresh Review finds a known defect again, and dropping the finding loses information.
- **Re-filed members as Drafts the human publishes one by one.** Rejected: choosing respin on the Remnant already authorizes the carry, so a second publish gate would be redundant.
- **Carry an open pivot onto the respin by moving its blocks edge.** Rejected in favour of resolving it first: the human would otherwise pick respin before knowing whether the Spec stands.
- **Re-file a Conflict onto the respin.** Rejected: a replace-mode rewrite has no base to apply to on a fresh cut.

## Consequences

- No code copies an effort branch and its cut ref between ids today. Adoption (point 1) needs it, and so does dropping the pins from a re-filed body.
- An open pivot is a second hold on the Remnant, next to "a child is still running". The Remnant's TUI row has to show which hold applies.
