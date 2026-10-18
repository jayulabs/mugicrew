# A Checkup writes only through bd and allowed crew verbs, behind a denylist and one yes per finding

Status: Accepted
Date: 2026-10-17

Chart mc-qcor ("A HITL Chopper session repairs a broken bead on a human's
command"), grilling bead mc-qcor.6. A Checkup (ADR 0272
(mugicrew-a-chopper-checkup-is-a-hitl-kind…)) repairs graph-shape, content and
stuck-state faults with judgment, so its write surface is bounded by a
denylist the human's yes cannot override, and everything else waits for that
yes. Git stays outside it: the motivating case (a stale, zero-ahead effort
branch that re-pauses a Build as `paused:not-built`,
docs/research/not-built-retry-reuses-stale-effort-branch.md) is finished by a
command the human runs, not by Chopper.

## Decision

1. **Never, whatever the human says.** Delete a bead. Rewrite a closed bead's
   body or title. Run git, or touch an effort tree, branch or ref. Push or
   remote-sync (git or Dolt). Publish, edit or delete another session's
   Drafts. Change a bead whose effort holder reads alive (name
   `mugicrew abort <bead>` instead). Edit repo files (code, ADRs, glossary,
   CREW.md). Asked for one of these, Chopper refuses and names the human-side
   route: a command for the human to run, or an offshoot Signal.
2. **Allowed on the human's yes.** bd fields, labels and edges of any bead;
   notes and comments appended to any bead, closed included; status changes,
   reopen, and close with a reason naming the Checkup; removing any `paused:*`
   label, `paused:manual` included (the yes is the human's lift), saying
   whether the bead will re-dispatch or just re-pause. Chopper's own
   `paused:checkup` fence is not part of a repair plan: the session lifts it
   last (ADR 0272). Whole-body rewrites of an open, non-Chart bead, after a
   before/after diff. A Chart body is edited only one line at a time through
   `mugicrew chart append|drop`, since sibling sessions tend Charts at the
   same time.
3. **Crew verbs.** Allowed on a yes: `recover`, `abort`, `rebase`,
   `signal new`, `signal offshoot`, `chart append|drop`, and the read-only
   `transcript`. Never: `integrate`, landing and approval verbs, `loop`
   start/stop, `init`. These move branches or mainline, or change the crew's
   global state.
4. **One yes per finding.** For each finding, Chopper shows an ordered plan
   of the exact bd and mugicrew command lines, each with a one-line reason,
   and waits for an explicit "yes" in the chat. Silence or "go on" is not a
   yes, and neither is the harness's permission prompt (a backstop only).
   The plan runs in order and stops at the first failure, which is reported
   back to the human before anything else runs.
5. **Repairs that need git.** Chopper prints the exact command, the human runs
   it, and Chopper verifies the result before going on. A new crew verb that
   wraps it is filed only if the need recurs, as an offshoot Signal.
6. **New beads are Drafts**, published at the Checkup's clean exit. An
   offshoot Signal filed through `signal offshoot` follows that verb's own
   rules.

## Considered Options

- **An allowlist only, no per-change approval.** Rejected: faults need
  judgment, and the human must see each repair before it lands.
- **The human's yes overrides the denylist** (Franky's "say so once, then
  follow", ADR 0092 §5). Rejected: a denylist the human can talk past bounds
  nothing.
- **A narrow git exception for deleting a zero-ahead effort branch.**
  Rejected: it breaks the "never git" line for one case whose root cause is
  already an offshoot Signal (mc-qcor.2).
- **One approval per command, or one per session.** Rejected: one per command
  splits a single fix into many yeses, and one per session hides what runs.
- **Lift pauses only through the TUI.** Rejected: a stuck pause is the case
  where that path failed.
