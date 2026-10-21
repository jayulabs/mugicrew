# ADR 0185: An open Merge Request outlives its Landings; a Forge-side close or trailerless merge holds the container on a Task

Status: Accepted (amended by ADR 0189; §8 amended by Spec "Landings paced by a configurable poll interval measured from the previous effort's end" and by the Spec "Forge Reviews file a container's Merge Request feedback deduplicated…" (the MR Watch))
Date: 2026-10-14

Chart mc-t72m.2 (GitLab Merge Request land strategy for a single-repo
Workspace). Settles grilling mc-t72m.2.5. Under `land_strategy=gitlab`, a
Landing (ADR 0145: one attempt at one pin) opens a Merge Request and returns
pending (ADR 0147 §2). The MR's source branch is the container's own branch, and
GitLab allows only one open MR per source branch, so the MR usually lives longer
than the Landing that opened it. This ADR fixes what happens to the MR across
Landings. It amends ADR 0145 §1–3 and fills ADR 0147 §2's pending slot. Mechanics
are in docs/research/gitlab-merge-request-mechanics-through-glab.md.

## Decision

1. **One MR per container branch, reused across Landings.** A Landing reuses the
   open MR for `checkpoint/<id>` or `chart/<id>`, or creates one if none is open.
   It force-pushes its `Commit:` pin to the source branch and arms auto-merge with
   `sha=<pin>` and the squash message. It records `MR: !<iid>` on its body.
2. **Obsolete disarms auto-merge but leaves the MR open.** This applies to both
   `obsolete: branch advanced` and `obsolete: conflict`, whether from
   `detailed_merge_status=conflict` or a merge-train drop. Jinbe cancels
   auto-merge, files the conflict member at the fetched mainline (ADR 0145 §2),
   and closes the Landing. The resolved or advanced tip is re-pushed into the same
   MR by the next Landing, which re-arms it.
3. **Which MRs a Landing considers.**
   - An `opened` MR is reused.
   - A `merged` MR means: fetch, then read the trailer.
   - A `closed` MR is ignored, except by the Landing whose `MR:` line names it.
   A new Landing with no open MR creates a fresh one.
4. **A human closes the MR on the Forge.** The Landing whose MR it was closes
   `obsolete: merge request closed`. Jinbe files a **Task** (Brook-owned, HITL)
   blocking the container, which says to reopen or merge MR !N, or to abandon
   the container. No Landing spawns while that Task is open. After it closes, a
   reopened MR is reused; otherwise a new MR is created.
5. **A human merges the MR by hand.** If the trailer survived, `LandedBase`
   finds it after the fetch and the Landing closes `landed: <sha>` as usual. If
   the squash message lost the trailer, the MR is `merged` but mainline has no
   landed fact. The Landing stays open, Jinbe returns an error, and he files a
   Task blocking the container that names the MR and its merge sha. The trailer
   stays the only landed fact (ADR 0145 §3).
6. **A stale pin merged after the branch advanced.** The Forge can merge an
   armed pin after the local branch has moved on but before Jinbe disarms it.
   That pin was approved, so it closes as landed. But if the landed squash's
   `Tip:` differs from the branch head, Jinbe files an offshoot **Signal**
   discovered-from the Landing. The Signal names the landed `Tip:`, the head
   and the range `<tip>..<head>`. `deleteLanded` keeps a branch whose head is
   not the landed `Tip:`.
7. **A Chart lands the same way.** It goes through the same MR flow, with Forge
   CI as the gate and auto-merge armed. It has no Review or Approval, because a
   Chart is gated by defog and Spec acceptance. Only the trailer kind differs.
8. **A pending Landing is re-checked on a poll interval.** A pending Landing
   costs one fetch and one MR read per check. The loop re-checks it on `[gitlab]
   landing_poll_interval` (default 60s, minimum 15s), measured from the previous
   effort's end, pass or fail, instead of every tick. A Landing the loop has not
   run since it started is due at once. A 429 leaves the Landing open and is
   logged once, without an error each tick. No exponential or adaptive backoff.
   The same budget covers the **MR Watch**, which reads an open container's MR on
   the same interval between Landings. It skips its read while a Landing is
   pending, so one MR is read once per period.

## Considered Options

- **A fresh MR per Landing.** Rejected. GitLab forbids a second open MR on a
  branch, so each Landing would have to close the old one. That loses the MR's
  discussion, which the offshoot Signal "Brook files actionable Merge Request
  comments on the container" needs.
- **On a Forge-side close, waiting with the Landing open, or withdrawing the
  Approval.** Rejected. Re-spawning would reopen the MR and fight the human, and
  a silent re-Review hides a human decision. A closed MR asks for a human, so a
  Task asks for one.
- **Accepting `merge_commit_sha` as landed when the trailer is missing.**
  Rejected. LandedBase, Zoro and deleteLanded all key on the trailer, so a
  trailerless land would be invisible to them.
- **Accepting a stale-pin merge silently.** Rejected. The commits after the pin
  would be lost without anyone noticing.

## Consequences

- ADR 0145 §1–2 gain disarm-on-obsolete and the reason
  `obsolete: merge request closed`. §3 gains the trailerless-merge refusal. The
  Landing body gains an `MR: !<iid>` line.
- Jinbe files Tasks, which Brook owns, and offshoot Signals. A Task now also
  holds a container for Forge-side human action, alongside `before: land` Tasks
  (ADR 0113).
