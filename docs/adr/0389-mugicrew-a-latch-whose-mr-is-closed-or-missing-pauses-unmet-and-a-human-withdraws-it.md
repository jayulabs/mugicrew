# ADR 0389: A Latch whose MR is closed or missing pauses unmet, and a human withdraws it

Status: Accepted
Date: 2026-10-28

Extends: ADR 0223 (mugicrew-an-orphaned-latch-closes…), which closes a Latch
only when its fact holds or nothing waits on it.

Chart mc-3cnhf.10 (Latch closure outcomes: a closed or missing watched item and
a human drop door), settled by its Survey mc-3cnhf.10.1. Today a watched MR
closed unmerged makes Brook's check error until the 3-count failure pause, and
the Latch then waits on a raw `bd close` with no reason.

## Decision

1. **Unmet pause.** When Brook's check reads a definite "the fact will not hold
   as things stand" (a watched MR closed without merging, or an MR read that
   answers 404), it pauses the Latch at once with the new cause
   `paused:unmet`, with no 3-count. It never closes the Latch: the blocked
   work stays blocked. A human unpause or a run-now lifts it; the run-now
   re-reads, so a reopened MR polls again and a still-closed or missing MR
   pauses `unmet` again.
2. **Jira stays blip-tolerant.** acli prints one line for a deleted, forbidden
   or unreachable ticket (docs/research/acli-jira-status-category-and-gone-or-forbidden-reads.md
   §4), so a Jira Latch's failed read stays on the existing `paused:failing`
   count; only its wording changes, naming "gone, forbidden or unreachable"
   and withdraw as the fix.
3. **Withdraw drops a Latch.** `mugicrew withdraw <latch>` (and the TUI's
   withdraw key) accepts an unclosed Latch as it accepts an Errand: it closes
   the Latch alone, reason `withdrawn`, and the beads it blocked stay open,
   released; the edges stay as lineage. For a Latch target the blocked beads
   are not an ordered-after refusal; in-flight and Landing refusals are
   unchanged. Dropping a Latch for some blocked beads only is `bd dep remove`,
   and the last removal leaves the orphan close of ADR 0223 §1.
4. **Reasons.** A Latch's close reasons follow `<outcome>[: <detail>]`:
   `completed: jira <KEY> reached <status> (Done)`, `completed: mr <path>!<iid>
   merged`, `orphaned: no open bead waits on <system> <item>` (unchanged), and
   `withdrawn`.

## Considered Options

- Brook closes the Latch on a closed-unmerged MR: releases work whose outside
  fact never held, defeating the Latch; and a closed MR can be reopened.
- Keep the error and failing pause, fixing only the wording: three wasted
  intervals for a read that is not a blip, under a cause that says "flaky".
- An MR 404 on the failing count like Jira: GitLab, unlike acli, tells a 404
  apart; a 404 can also mean lost access, which a human resolves either way.
- A new `mugicrew latch drop` verb with its own reason: a second door for "the
  human drops a bead" beside Withdraw.
- `withdrawn: <system> <item>` or a free-text reason: the Latch title already
  names what it watched; bare `withdrawn` matches an Errand.
