# ADR 0382: mugicrew under gitlab the un-draft of the Checkpoint's Draft MR is the approval

Status: Accepted
Date: 2026-10-28
Amended by ADR 0388 (a-chart-lands-through-an-approval-carrying-a-chart-packet) (§1–§5 hold for a Chart, with the Chart Packet as the body and the patch-id rule in place of the pivot rule).
Amends: ADR 0020 (luffy-approval-crewmate) §1/§4; ADR 0185 (an-open-merge-request-outlives…) §1, §4 and §8; ADR 0346 (an-approval-is-afk…) §2/§3; ADR 0343 (an-mr-watch-closes-once-nothing-is-owed…).
Builds on: ADR 0189 (under-gitlab-a-human-merges…), ADR 0333 (the-tui-opens-a-merge-request…), ADR 0360 (mr-and-closed-rows-keep-rarer-bead-verbs…).

Spec: Under gitlab, a Checkpoint's Approval opens its Merge Request as a GitLab Draft and the human's un-draft approves it.

## Context

Under the gitlab land strategy a human approved twice: in a terminal Luffy session on the Decision Packet, then again on GitLab when merging. The review that matters, with the diff, the pipeline and the comment threads, came second and decided nothing. GitLab's own "not ready yet" is a Draft MR, and mugicrew did not use it.

## Decision

1. **The un-draft is the approval (amends ADR 0020 §1/§4).** Under gitlab, the Approval opens the Checkpoint's MR as a GitLab Draft whose description is the Decision Packet followed by the Landing's trailer block. Go records `MR:` and `MR link:` on the Approval and leaves it open. The human's un-draft ("Mark as ready") approves; the MR Watch closes the Approval `approved`, ending its Approval History with `approved (un-drafted on GitLab)`. The HITL fallback session for an `auto-approve:failed` Approval writes the Packet without closing, and its clean exit opens the same Draft MR.
2. **The MR opens at the Approval (amends ADR 0185 §1).** The Landing reuses the un-drafted MR, or opens one if none is open. A reused MR that is still a GitLab Draft keeps its prefix, so a Landing never un-drafts.
3. **A closed Draft MR holds the Checkpoint (amends ADR 0185 §4).** The MR Watch files the Brook Task (reopen or abandon) for a waiting Approval's closed MR. The Approval stays open: closing the MR is neither a rejection nor a rework.
4. **The Watch reads the draft flag (amends ADR 0185 §8).** Each paced tick, the MR Watch reads the MR's draft flag. Once it is false, and the Checkpoint has no open pivot, it closes the waiting Approval `approved`.
5. **AFK unless marked (amends ADR 0346 §2/§3).** Under gitlab an Approval is AFK whether or not auto-approve is on, and HITL only while it carries `auto-approve:failed`. Go closes `approved` only under auto-approve or when a re-approval skips: the MR is already un-drafted, an approved Approval vouches for it (not an auto-approved one while auto-approve is now off), and no `amended:` pivot closed since that Approval was created. A pivot amend re-drafts the MR and the Approval waits. Under local nothing changes.
6. **The MR Watch stays open on a waiting Approval's closed MR (amends ADR 0343).** So a reopen is read on the next tick; the Watch's close rules apply again once the Approval closes.
7. **Corollaries.** ADR 0189 §1 becomes literal: the human merges or arms auto-merge, and un-drafting does not merge. ADR 0333 §2's Merge-Requests-only scope for `w` is superseded by ADR 0360 for Approval rows, so `w` opens a waiting Approval's MR link.

## Considered Options

- **Un-draft also merges the MR**: rejected; it would change the land strategy and ADR 0189.
- **Telling a commit-driven re-draft from a human re-draft**: rejected; any draft state reads as a GitLab Draft, whoever set it.
