# ADR 0417: mugicrew Every defog close names its outcome through one door

Status: Accepted
Date: 2026-10-30
Follows: ADR 0399 (mugicrew-every-spine-close-names-its-outcome…), ADR 0414 (mugicrew-pivot-dismiss-and-amend-are-go-verbs…), ADR 0415 (mugicrew-review-conflict-and-critique-completed-closes…), ADR 0389 (mugicrew-a-latch-whose-mr-is-closed-or-missing…).

## Context

Chart mc-3cnhf.68, finished in place by its Survey mc-3cnhf.68.1. Only the
completed close of a defog bead (grilling, research, prototype) has a
prescribed reason today: `answered:`, `Wrote docs/research/`, `prototyped:`.
crew doc `defog-session.md` "Obsolete the stale" says "close or supersede"
with a raw `bd` call and no reason. `withdraw` refuses a defog bead that is
not a chain head. The chart-conflict escalation grilling is closed by a raw
`bd close`, and the Forge Review grilling has no close instruction (bead
lifecycle Gaps entry 8). Readers such as promote-or-stamp, speccing and
chart-conflict take every closed defog child as a settled decision.

## Decision

1. **Obsolete is a Go verb.**
   `mugicrew obsolete <bead> --by <deciding bead>` closes it
   `obsolete: <deciding bead id>`, a `bd` constant beside `SupersededReason`.
   The deciding bead is the defog bead or Survey whose decision made it moot.
   The obsoleted bead gets no Decisions-so-far line.
2. **Superseded is a Go verb.** `mugicrew supersede <bead> --with <new>`
   closes it `superseded: <new id>` and adds the `supersedes` edge, through
   `bd.Retire`. A defog bead that duplicates another one's question closes
   this way. `duplicate:` stays a Signal and spine outcome.
3. **Both verbs are crew doors.** They are the door named by
   `defog-session.md` "Obsolete the stale", used by the session whose decision
   made the bead stale. Both refuse anything that is not an unclosed grilling,
   research or prototype.
4. **Withdraw takes one defog leaf.** `mugicrew withdraw <bead>` and TUI `X`
   accept an unclosed defog bead, which closes alone with `withdrawn`, the way
   a Latch does. This door is the human's only. A prototype with work in its
   throwaway tree still needs `--discard`. The sibling Signal "A single Build
   has no closure door for abandoned work" should align with this shape.
5. **Every grilling completes through the grilling launch.** The chart-conflict
   escalation grilling and the Forge Review grilling are launched like any
   grilling. Nami's session records the notes and closes the bead
   `answered: <summary>`. The escalation body sends the human to the launch
   key, not to `bd close`.
6. **An escalation grilling closes only answered.** While the chart-conflict
   that drafted it is open, `obsolete`, `supersede` and `withdraw` refuse it.
   To give up on the conflict, the human withdraws the Chart.
7. **Only completed closes are decisions.** promote-or-stamp, speccing and
   chart-conflict read a closed defog child as settled only when its reason is
   `answered:`, `Wrote docs/research/` or `prototyped:`. They skip an
   obsolete, superseded or withdrawn close.
8. **History is not rewritten** (ADR 0399 §4). Defog beads closed before this
   decision keep their reasons, and readers treat an unknown reason as legacy.
9. **The docs name the doors.** `defog-session.md`, the Nami and Sanji
   prompts, GLOSSARY.md's Grilling, Research, Prototype and Withdraw entries,
   and the closure tables in `docs/bead-lifecycle.md` name each door and its
   exact reason. Gaps entry 8 is marked resolved.

## Considered Options

- **Prescribe the reasons in the crew docs and keep the raw `bd` calls.**
  Rejected. The reasons would stay unchecked text, and the Initiative asks for
  code doors (as ADR 0414 did).
- **`obsolete: <gist>`.** Rejected. The decision lives on the deciding bead,
  and the Chart is only an index.
- **Wait for the sibling Build-withdraw Chart before choosing the withdrawn
  door.** Rejected. The Latch's lone-leaf withdraw is already a precedent, so
  the sibling can align with this decision.
- **A separate verb for withdrawing a defog bead.** Rejected. Withdraw is the
  human's one drop verb.
- **Re-escalating, or carrying on without answers, when an escalation grilling
  closes unanswered.** Rejected. Re-escalating can loop, and carrying on would
  apply nothing the human said.
