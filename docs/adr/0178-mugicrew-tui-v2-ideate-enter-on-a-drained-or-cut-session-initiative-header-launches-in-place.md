# ADR 0178: v2 Ideate `enter` on a drained Initiative, or one with a cut session, launches or attaches the cut session in place

Status: Accepted; §6 amended by ADR 0238 (the ON LAUNCH preview line is removed)
Amended by ADR 0239 (§1: the launch path claims the Initiative).

Date: 2026-10-14

Chart mc-lu2x.31 (drained Initiatives on the Ideate tab). Settles grilling
mc-lu2x.31.3, which followed the human's try of prototype mc-lu2x.31.2 (variant
D, wording 3). Amends ADR 0172 §1 (an Initiative header's `enter` and hints) and
§3 (a drained header's row and fold). ADR 0177 §1 says which Initiatives Ideate
lists; this ADR says how they look and what their keys do. A **launchable**
Initiative header is one whose Initiative is drained or has a cut session.

## Decision

1. **`enter`** (amends ADR 0172 §1). On a launchable Initiative header, or on
   its hint row, `enter` launches or attaches Luffy's cut session through
   Graph's launch path (ADR 0146), which leaves the Initiative unclaimed and
   asks y/n when it is paused (ADR 0164 §2). The launch path's own status
   messages are the only status text. On any other Initiative header (under
   way, listed for its Signals, no cut session), `enter` does nothing and the
   status line reads `members still open · cut from Graph`.
2. **The drained hint row** (amends ADR 0172 §3). A drained Initiative's
   header has one selectable row under it reading
   `all members closed · enter: Luffy cuts the next phase, or closes it`.
   The row carries the Initiative's stateGlyph (for example `‖` when paused).
   The header shows the cut session's rowSuffix (for example `your turn`) at
   its right edge.
3. **A header with a cut session that is not drained** has no hint row (the
   drained wording would be false). Its stateGlyph and rowSuffix are on the
   header itself.
4. **Hints** (extends ADR 0172 §1's table). The `enter` label follows what
   `enter` will do: `cut` while there is no cut session, `attach` once there is.
   `attach` also covers an exited cut session, which `enter` resumes in its
   kept worktree as Graph does (`✕ exited` on the header); there is no
   separate `resume` label (grilling mc-lu2x.31.5).

   | row | hints, in order |
   |---|---|
   | drained Initiative header | enter cut\|attach · space fold · d detail · n new |
   | drained hint row | enter cut\|attach · d detail · n new |
   | not-drained header with a cut session | enter attach · space fold · d detail · n new (no `space` without Signals) |
   | any other Initiative header | space fold · d detail · n new (as ADR 0172) |

5. **Folds** (amends ADR 0172 §3). `space` on a drained header folds its hint
   row away and the folded header shows `(0)`. `space` on the hint row folds
   its header and moves the cursor to the header, as a folded-away Signal row
   does. A drained header starts unfolded and its fold persists under
   `i:<initID>` (ADR 0172 §4).
6. **Detail pane** (ADR 0146 §3). On a launchable Initiative header and on the
   drained hint row, the detail pane shows Graph's ON LAUNCH preview, so it
   agrees with `enter`. Other Initiative headers show none.

## Considered Options

- **`enter` on the hint row only** (prototype variant A). Rejected after the
  try: the header is where the cursor lands and where `enter` is expected.
- **A hint row the cursor skips, nothing to fold** (variant B), and **the hint
  inline on the header** (variant C). Rejected after the try.
- **`enter` launches on every Initiative header.** Rejected. An under-way
  Initiative does not wait on the human, and Graph still cuts it early.
- **Launch only on drained headers.** Rejected. A cut session's Draft un-drains
  its Initiative (ADR 0177), so `enter` would refuse to attach to a session
  that is waiting on you.
- **A second wording for the not-drained hint row.** Rejected. The rowSuffix
  already says the session's state, and a second row would add clutter.
