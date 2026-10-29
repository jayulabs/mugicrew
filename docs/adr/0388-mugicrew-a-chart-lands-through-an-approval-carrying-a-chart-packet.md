# ADR 0388: mugicrew a Chart lands through an Approval carrying a Chart Packet

Status: Accepted
Date: 2026-10-29
Supersedes: ADR 0004 (mugicrew-merge-tail)'s "no review gate" call for Charts (its §6 note, via ADR 0011, that a Chart has no review gate).
Amends: ADR 0145 (a-landing-is-one-pinned-land-attempt…) §1; ADR 0358 (a-chart-with-nothing-ahead-of-mainline-lands-as-a-bead-pin…) §1; ADR 0382 (under-gitlab-the-un-draft-of-the-checkpoint-s-draft-mr-is-the-approval) §1–§5.

Spec: A Chart lands through an Approval carrying a Chart Packet, and under gitlab the human's un-draft of its Draft MR approves it.

## Context

A Chart landed on mainline with no gate beyond defog. What it lands is lasting domain content: ADRs (some superseding earlier ones), GLOSSARY entries, research docs and crew docs. No human certified it, and under gitlab the Chart's MR carried only a mechanical copy of the Destination and Decisions-so-far. ADR 0382 made a Checkpoint's approval the human's un-draft of its Draft MR, with every seam container-generic; the Initiative is done only when the Chart is approved the same way.

## Decision

1. **A drained, stamped Chart lands through an Approval.** Nami's `reconcileChart` spawns the Approval where it spawned the Landing: a member of the Chart, titled as the Chart, pinned `Commit: <chart/<id> head>` with a `Patch-id:` line beside the pin. No Usopp Review precedes it: defog and the Approval are the review (supersedes ADR 0004's "no review gate").
2. **The Approval carries a Chart Packet.** It is the Decision Packet's analog, with the same six headings read for a Chart. A Luffy session writes it through the existing Approval runner with a Chart-specific prompt.
3. **The Landing follows the approval.** While an Approval is open the Chart is not drained. Once one is approved at the branch head, `reconcileChart` spawns the Landing pinned to the Approval's pin and caused by it. A branch that advances past an open or approved Approval's pin obsoletes it and spawns a fresh Approval, not a Landing (amends ADR 0145 §1).
4. **Nothing ahead gets no Approval.** Nami checks for nothing ahead of mainline before spawning the Approval and spawns the Landing directly, which lands the bead pin; Jinbe's land-time check stays as the backstop (amends ADR 0358 §1).
5. **Under gitlab, ADR 0382 §1–§5 hold for a Chart.** The Chart Packet is the Draft MR's description, in place of the Destination copy, and the un-draft approves. The re-approval skip is keyed on the squash diff's patch-id, not on a pivot: a plain rebase does not re-draft the MR, while an edit made by a chart-conflict resolution does. Under local the gate is an ordinary human Approval.
6. **Rework re-opens defog.** `mugicrew approval rework` on a Chart's Approval files grilling or research defog beads on the Chart, removes `chart:defogged`, and closes the Approval rework; the Chart re-defogs and a fresh Approval follows. `bd batch` has no label operation, so the code removes `chart:defogged` in a separate call before it publishes the new beads, not in the publish batch. Crash safety still holds: the open Approval keeps the Chart undrained until it closes, and a re-run adopts the Drafts.
7. **No migration.** A Chart that already has a Landing lands as before.

## Considered Options

- **A Usopp Review before the Approval**: rejected; a Chart has no code to review, and defog already certifies its decisions.
- **The pivot rule for re-approval**: rejected; a Chart has no Spec to amend, and its content changes only through commits, which the patch-id sees.
