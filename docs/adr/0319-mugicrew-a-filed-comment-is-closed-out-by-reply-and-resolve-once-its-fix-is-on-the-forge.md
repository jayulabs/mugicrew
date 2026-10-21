# ADR 0319: A filed comment is closed out by reply and resolve once its fix is on the Forge

Status: Accepted
Date: 2026-10-20

Chart mc-xw289 (Actionable Merge Request comments are filed deduplicated and
acked). Settles grilling mc-xw289.6. Charting moved close-out from bead close
to push: the commenter should hear "addressed" only once the MR shows the fix.
A Landing pushes the container's Commit: pin to the MR branch, and skips the
push when the Forge already holds that sha (`git/git.go` `landGitlabAt`). So
the trigger is reachability, not the push event itself. Research
(docs/research/gitlab-note-ids-awards-and-review-notes-for-ack-and-close-out.md
§4) found that a reply to a plain comment turns it into an unresolved thread,
which can block merging.

## Decision

1. **Trigger.** Jinbe closes out a filed bead's source notes once the bead's
   integrate commit (its `<Kind>: <bead-id>` trailer) is reachable from what
   the Forge holds: the MR's pushed head, or the merged commit when a human
   merged after the fix was pushed. A bead with no integrate commit closed with
   no fix, and is closed out at its close. That includes a grilling that made
   no edit, and a bead closed as not needed or as already fixed (ADR 0317 §4,
   §6), even when the commit that fixed it is not pushed yet: its close reason
   says what fixed it. The same rule applies to Checkpoint and Chart MRs. A
   Chart re-lands through ADR 0199 §5's fresh Landing.
2. **Shape.** Every source note gets a reply, and its thread is resolved. A
   plain comment therefore becomes a resolved thread. It is never left as an
   unresolved one. The 👀 award stays, and no ✅ is added.
3. **Granularity.** One reply per (bead, discussion), posted when that bead is
   closed out, however many of the bead's source notes sit in the discussion. A
   discussion is resolved only once every bead fed by any of its notes is
   closed out.
4. **Template.** ADR 0200's reply and hidden marker carry over, with one
   change. `<sha8>` stays the bead's own integrate commit, not the pushed pin,
   and it now links because it has been pushed. `<reason>` becomes the whole
   close reason, cut at 2000 characters with `…`. It used to be the first line
   cut at 200. That way the answer to a filed question (ADR 0317 §1) is relayed
   in full. The marker stays the idempotency key.
5. **A human merge before the push.** When the merge lands the container
   without a bead's integrate commit, that commit is stranded and is carried by
   a stranded-commits Signal. The bead's source notes get the reply
   `not shipped with this MR: carried by **<Signal title>**`, with the usual
   marker, and are resolved.
6. **A reply after a close-out is input.** Charting round 2's mechanical skip
   of "notes on threads already resolved on the Forge" is narrowed. An unmarked,
   non-system note newer than a Jinbe marker in the same discussion is always
   input, even when the thread is resolved. This keeps today's reopen-leg
   behaviour (`threadAnchor`'s new reply).

## Considered Options

- **A ✅ award only, or award plus reply.** Rejected. An award can't carry the
  bead, the sha or a question's answer, and a second award is one more write
  that can fail.
- **Remove the 👀 at close-out.** Rejected. It is history ("seen, filed"), and
  removing it is an extra write that can fail.
- **Resolve a discussion per bead, or post one combined reply when all its
  beads are done.** Rejected. Resolving per bead makes the thread look done
  while a sibling bead is still open. A combined reply holds back
  acknowledgement of fixes that have already shipped.
- **Name the pushed pin instead of the integrate commit.** Rejected, as in ADR
  0200: the pin brings in unrelated commits.
- **Close out at a human merge regardless, or not at all.** Rejected. The
  first claims a fix that didn't ship. The second loses the feedback on the
  Forge, which is the problem this Chart exists to fix.
- **Hold no-fix close-outs until the next push.** Rejected. A bead closed as
  not needed would wait for a push that may never come.
- **Keep the resolved-thread skip as written** (the human must unresolve to
  reopen). Rejected. A "that didn't fix it" reply would be dropped silently.
