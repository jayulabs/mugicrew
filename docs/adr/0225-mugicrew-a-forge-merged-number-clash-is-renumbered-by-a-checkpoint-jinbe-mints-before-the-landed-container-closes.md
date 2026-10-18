# ADR 0225: A Forge-merged number clash is renumbered by a Checkpoint Jinbe mints before the landed container closes

Status: Accepted
Date: 2026-10-16

From Chart mc-zfce.2 (A Forge-merged ADR-number clash under gitlab is detected and
renumbered), its grillings Shape of the renumber container Jinbe files after a Forge-merged
ADR clash and Whether a renumber Checkpoint's own new ADR number re-clashing on
mainline just recurses through closeLanded detection. Amends ADR 0030 (mugicrew-adr-number-collision-guard) §1 and §4. Builds on
ADR 0048 (mugicrew-rollback-successor), ADR 0069 (mugicrew-one-land-lock…), ADR 0189
(mugicrew-under-gitlab-a-human-merges…) and ADR 0219
(mugicrew-the-adr-number-guard-is-a-numbered-file-clash-check…), unchanged.

## Context

Under `land_strategy=gitlab` a human merges on the Forge, so two MRs that each add number N
can both reach mainline between Jinbe's ticks; the in-lock clash check (ADR 0069) never sees
the second. By then the later container has landed: there is no open parent to hang a
conflict member on, and only a container Landing moves mainline. Detection is Jinbe's, on
the gitlab "landed" exit; the later-landed container moves, by mainline first-parent order
(docs/research/forge-merged-adr-clash-landing-order-on-mainline.md).

## Decision

1. **A landed container's added number that clashes on mainline is renumbered after the
   land (amends ADR 0030 §1).** Only the numbers the just-merged container added are
   checked, for every configured sequence (ADR 0219); pre-existing duplicates still never
   block and stay a one-off cleanup.
2. **The carrier is a Spec-less Checkpoint Jinbe mints**, titled `Renumber <file> from
   <landed container>`, like Rollback's successor (ADR 0048): no `Spec:` line, `context:` and
   `repo:` copied from the landed container, joining its Initiative while that is unclosed,
   `checkpoint/<new>` cut from the fetched Forge main. One carrier for a landed Chart or
   Checkpoint alike.
3. **Its one member is a Franky-owned `kind:build`** whose body carries the number-clash
   wording (crew doc sequence-renumber.md, the clashing paths). The sweep scope keeps ADR
   0030 §5, read off the landed squash commit's diff plus the landed container's bead
   subtree. When the landed container is itself a renumber Checkpoint, the bead scope also
   takes the subtree of the container it was minted for, following `caused-by` through
   renumber Checkpoints to the first container that is not one: that subtree already cites
   the moved number. The same widening applies to a conflict member's "own beads" on an
   open renumber Checkpoint.
4. **It passes the full Checkpoint gates**: Review, Approval, Landing, the human's Forge
   merge. No lighter path: a wrong sweep silently changes what a number means, and Review is
   where that is caught.
5. **It is `caused-by` the landed container, deduped on (landed container,
   `kind:checkpoint`)**, so one clash files once and one carrier moves all of that
   container's clashing files together, keeping their order. The other side's container is
   named in the body only.
6. **Detection and filing run in `closeLanded`, before the container closes**, re-run each
   pass until it does. A closed landed container therefore implies its clash check ran; a
   crash between the merge and the filing heals on the next tick. The landed branch is not
   needed: the diff, mainline and the closed bead subtree carry everything.
7. **A renumber Checkpoint is itself a landed container: its own re-clash recurses, with no
   guard.** While its MR is open, a re-clash on the number it picked is the ordinary
   conflict member, filed by the pre-Review probe or the MR mirror's Compose trial on any
   base move. Only a merge racing Jinbe's tick reaches §6, which mints
   `Renumber <file> from <renumber Checkpoint>` exactly as for any container (its renamed
   file reads as added under `--no-renames`). Ordering is unchanged: when the renumber
   Checkpoint is the earlier side, the other container moves. No bound on the chain: each
   round needs a human Approval and Forge merge, and the renumber already reads current
   mainline for its next free number (crew doc sequence-renumber.md step 2).

## Consequences

- ADR 0030 §4's "the landing Chart moves" now reads "the later-landed container moves",
  including after its land.
- Two human touches (Approval and the Forge merge) for a mechanical rename; a lighter gate
  would need its own ADR.
- gitlab requires `squash_option=always`: the project-settings check refuses every other
  value (not only `never`), so GitLab locks the MR's squash box and a squash-off merge,
  which would land trailerless and skip detection, cannot happen. No client was on gitlab
  yet, so nobody migrates; ADR 0210's lineage MR already keeps `squash=true` for it.
- A hand-edited squash message that drops the trailer still slips detection: after the
  trailerless hold's fix commit, the landed base is that fix, not the commit that added
  the file. Accepted as a double human error; the trailerless Task's body asks the human to
  check whether the merge added an ADR number already on mainline.

## Considered Options

- **A Chart carrier.** Rejected: after landing a Chart mints a Spec and runs speccing, and
  Jinbe mints no Chart.
- **A seeded `kind:conflict` member.** Rejected: a conflict means a still-open parent that
  would not rebase.
- **Detect once on the Landing's close.** Rejected: a crash after that close loses the clash.
- **One carrier per clashing number.** Rejected: the files must move together to keep order.
