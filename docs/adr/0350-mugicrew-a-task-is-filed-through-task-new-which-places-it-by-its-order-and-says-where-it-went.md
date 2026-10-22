# ADR 0350: mugicrew a Task is filed through `task new`, which places it by its order and says where it went

Status: Accepted
Date: 2026-10-22
Amends: ADR 0348 (a-filing-with-no-plain-member-to-climb-resolves-through-its-container…) §4, for Task filing; crew doc task-body.md (the `Initiative:` fact line).

## Context

The Chart "Related beads join their cause's Initiative" settled that every Task
joins the Initiative of the member it is ordered against at filing, that "the
CLI and TUI give the human a flag to keep it outside", and that a born-outside
filing says why. The filing-path inventory
(docs/research/task-hold-task-and-offshoot-filing-paths-and-their-initiative-climb.md,
gap G5) found no mugicrew or TUI verb that files a Task: crew prompts run a raw
`bd create`, and Go filers create their own. The grilling "The human's
Task-filing surface, its keep-outside flag, and what a born-outside filing
shows" settled the surface.

## Decision

1. **One verb files a Task.** It is
   `mugicrew task new --drafted-by <bead> [--after <bead>]... [--before <bead>]... [--outside] --title T --description D`.
   `--after X` gives the Task a blocked-by edge on X (a land or another Task).
   `--before X` makes X blocked by the Task. Both are repeatable, which covers
   ADR 0113's three orders. The verb resolves the Initiative from the order
   targets (`InitiativeOf`), then runs one atomic `bd create` that carries the
   edges and the `--parent`, and prints the new id. Crew prompts use it in place
   of raw `bd create`. The Go filers (slicing, Hold Tasks) call the same bd-layer
   function, so there is one climb. `--drafted-by` is required, as on
   `signal offshoot`: every caller is a crewmate filing a Draft.
2. **`--outside` is the keep-outside flag.** It is a boolean, off by default, so
   a Task joins unless told otherwise. The human uses it by saying so in the
   session, and the crewmate passes it. Raw `bd create` stays available.
   **There is no TUI Task form:** the crew writes a Task's Steps body (HITL is
   authorization, not authorship). The Chart's "CLI and TUI give the human a
   flag" therefore means this flag.
3. **An unordered Task is allowed.** With no `--after`/`--before` it is born
   outside.
4. **A refused order edge fails the filing; placement never does.** If bd
   refuses an edge (`cycle`, `type-clash`, `hierarchy`), the verb exits
   non-zero and leaves no bead, because a Task's order is its timing. Members
   of two Initiatives, or a closed Initiative, mean born outside and exit 0
   (ADR 0267's skip-never-fail, applied to placement only).
5. **Every filing says where it went**, on stdout and as the body's first fact
   line, before `## Steps`, so the guide never reads it as part of a step. The
   line is `Initiative: <title>` or `Initiative: outside (<reason>)`. The
   reason is one of `kept outside`, `unordered`, `ordered against members of <A>
   and <B>`, `<Initiative title> is closed`, or `<bead title> has no
   Initiative`. Titles are cited, not ids. There is no eventlog event and no new
   TUI cue: the line already shows in the detail pane.
6. **The verb's climb replaces ADR 0348 §4's template argument for Tasks.** A
   session passes `--after <chartID>` (or the member), and the verb resolves the
   Initiative and its edge cases when it files the Task, not when the session
   launched. That gives a fresher answer than the launch-time id, which can go
   stale if the Initiative closes mid-session. It is still the one Go path §4
   wanted.

## Considered Options

- **A TUI New-menu Task row with a keep-outside toggle.** Rejected: it invites a
  hand-written Steps body, and nobody asked for one.
- **No verb: the rule lives in prompt templates (`--parent` or not).** Rejected
  for the reason ADR 0267 gave for offshoots: the climb and its skip reasons
  would be copied into every prompt.
- **`--initiative auto|none`.** Rejected in favour of a plain boolean that
  defaults to joining.
- **Skip a refused order edge and file anyway, as offshoots do.** Rejected: an
  unordered "mint the token before the land" Task can be run at the wrong time.
- **Keep §4's template argument and add `--initiative <id>` to the verb.**
  Rejected: two sources for one answer.
- **A trailing `Initiative:` line, as offshoots have `Order:`.** Rejected: the
  guide reads the whole body, and a line after the last step could be taken as
  part of it.
