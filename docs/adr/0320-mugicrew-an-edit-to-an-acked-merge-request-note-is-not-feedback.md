# ADR 0320: An edit to an acked Merge Request note is not feedback

Status: Accepted
Date: 2026-10-20

Chart mc-xw289 (Actionable Merge Request comments are filed deduplicated and
acked). Settles grilling mc-xw289.10.

## Decision

1. **A note behind a closed Forge Review's mark is never read or judged
   again**, however it is edited later. New feedback is a new note: a reply
   or a fresh comment. Nothing notices an edit. It does not trigger a read and
   it adds no Forge read to any tick.
2. **The snapshot shows a context note as it reads now.** A note rewritten
   after it was read (the claude-review-bot turns its old reviews into
   "superseded" placeholders in place) appears with its current body, untagged
   and unrestored, even when a later reply answers text that is gone. The judge
   reads such a reply on its own. It is almost always a decline or an
   acknowledgement, and the prototype behind ADR 0318
   (a-forge-review-judges-a-tagged…) passed !3116 this way.

## Considered Options

- **Re-judge edited acked notes**, noticed every tick or only when a Forge
  Review runs anyway. Rejected:
  - The only real edits we have seen are bot placeholders, which must not be
    filed.
  - GitLab edits don't move `user_notes_count`
    (docs/research/gitlab-note-ids-awards-and-review-notes-for-ack-and-close-out.md §2).
    Noticing them would need a read that bypasses that count gate, or a
    per-note `updated_at` ledger.
- **Tag a note `edited`** when `updated_at` is later than `created_at`.
  Rejected as noise for little gain.
- **Restore a note's text as read**: from a filed bead's quote, or from bodies
  Jinbe stores on the Forge Review. Rejected: the bead quote covers only filed
  sources, and storing every body makes the ledger heavier.

## Consequences

- If a human edits a filed comment to change its ask, the edit goes unseen.
  The human has to reply instead.
