# ADR 0317: A Forge Review files whatever asks for a change or an answer, and files when unsure

Status: Accepted
Date: 2026-10-20

Chart mc-xw289 (Actionable Merge Request comments are filed deduplicated and
acked). Settles grilling mc-xw289.4. A Forge Review is an AFK judging session,
so it can't ask anyone. Its rubric leans toward filing, because this Chart
exists to stop review feedback from being lost.

## Decision

1. **Filed:** change requests, nits, and questions ("why X?"). A question is
   filed as the container's usual kind (a Critique on a Checkpoint, a grilling
   on a Chart), worded "Reviewer asks why X: answer it, or change the work so
   the answer is evident". The bead's close reason carries the answer, so the
   close-out reply can relay it. A bot comment is filed only when it names a
   concrete defect or change.
2. **Not filed:** praise, LGTM, approvals, and bot summaries or overviews.
3. **A bare Request changes review is ignored.** It is only a system note plus
   a reviewer state, with nothing to act on, and it doesn't move
   `user_notes_count`. A Request changes review that has a summary comment
   produces an ordinary note, which is judged like any other.
4. **Unsure means file**, with a `Judged: unsure — <one-line why>` line in the
   body. Whoever picks it up may close it as not needed. That counts as
   closed with no fix, so it is closed out at its close.
5. **Filing nothing** closes the Forge Review
   `nothing actionable: read N notes through <id>`, with no Forge write.
6. **Usopp sees the container tip.** He gets a read-only tree at the tip and
   the diff against mainline. He uses them to judge, word and locate a bead,
   but never to skip a comment as already addressed. That call belongs to the
   fixer, who closes the bead as already fixed and so triggers close-out.
   Without that, the commenter would get no 👀 and no reply.
7. **A filed bead takes its container's priority.**

## Considered Options

- **Leave questions unfiled.** Rejected: an unanswered question is lost
  feedback.
- **Skip when unsure.** Rejected for the same reason. A wrong filing costs
  one close.
- **Hold the container on a bare Request changes review.** Rejected: there is
  nothing to hold it for, and the human can write a comment.
