# ADR 0320: A Forge Review surfaces as `forge_review.*` events, a `forge review` qualifier and `d` lines

Status: Accepted
Date: 2026-10-20

Chart mc-xw289 (Actionable Merge Request comments are filed deduplicated and
acked). Settles grilling mc-xw289.9. Today thread filing surfaces only as
`land.review_comment` / `land.thread_*` events on the Landing, and ADR 0319
(the-first-forge-review-on-an-already-open…) deletes that filing. Any `land.*`
event on a Landing becomes its latest `landEv` and hides `retrying`, which is
why ADR 0227 (jinbe-records-a-gitlab-mr-s-sub-state…) rejected an event-only
store. Rows are generic: no Rollup or Chart-word rule knows a Forge Review, and
while a Landing is pending `landing` outranks every member-based word. ADR 0226
(a-gitlab-landing-s-mr-iid-and-sub-state…)'s MR qualifiers are the precedent.

## Decision

1. **A `forge_review.*` event family**, recorded on the Forge Review bead (its
   container as `parent`), never on the Landing. ADR 0209's `revert.*` is the
   precedent: a `land.*` reader means a Landing, so `retrying` stays intact.
2. **Events**:
   - `forge_review.spawned` (Jinbe): `mr=!<iid>`, the mark it reads from, and
     the number of notes to read.
   - `forge_review.filed` (on close): filed, folded and skipped counts and the
     mark read through.
   - `forge_review.empty`: nothing actionable.
   - `forge_review.closed_out` (Jinbe): one per (bead, discussion) reply, with
     `resolved=true|false`.
   - `forge_review.award_failed` / `forge_review.close_out_failed`: Warn.
   - A mechanical skip that only advances the mark is at most Debug. The
     every-tick MR read emits nothing.
3. **A `forge review` qualifier** beside whatever word wins, on the Checkpoint
   Rollup and the Chart status word alike, while the container has an open
   Forge Review. It sits before `retrying` so the alarm stays last, for example
   `landing · !42 · threads open · forge review`. It is not a new Rollup word:
   a pending Landing stays pending through the pass, so the word would never
   show. On a Chart, an open Forge Review is not counted as AFK fog, so it
   never reads `defogging`.
4. **Pending close-outs show only in the container's `d`**:
   `close-outs: N due (await push)`, and `(K failing: <err>)` when a reply or
   resolve keeps failing. There is no Rollup qualifier, because close-outs
   clear without a human click on the next push.
5. **The Forge Review leaf row** is the generic leaf with a `FORGE-REVIEW` tag
   and its own `kindColors` entry. A Chart-parented one homes to the Defog tab,
   as a Landing does. Its `d` shows `!<iid>`, the `Read through note:` mark,
   and, once closed, the filed, folded and skipped counts.
6. **Filed Critiques and grillings stay generic rows.** They carry no MR
   marker. `d` shows their `Source:` lines.

## Considered Options

- **Reuse `land.*` on the Landing.** Rejected. It hides `retrying`.
- **Only the generic `effort.*` / `session.*` events.** Rejected. Spawns and
  close-outs happen in Jinbe, outside any session.
- **A Rollup word such as `triaging feedback`, ranked above `fixing`.**
  Rejected. It is invisible while a Landing is pending, which is when Forge
  Reviews mostly run.
- **A `N replies due` Rollup qualifier.** Rejected as noise. Close-outs are
  expected and clear themselves.
- **Spelling the qualifier `reading MR` or `reading comments`.** Rejected in
  favour of the glossary term, which also matches the row tag.
