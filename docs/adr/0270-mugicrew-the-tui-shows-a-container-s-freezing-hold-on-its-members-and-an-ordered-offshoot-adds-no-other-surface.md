# ADR 0270: mugicrew the TUI shows a container's freezing hold on its members, and an ordered offshoot adds no other surface

Status: Accepted
Date: 2026-10-17

An ordered offshoot shows to the human through its bd edge, the body's Order: line,
and one TUI fix: a row reads blocked when an ancestor container carries an open
blocker that freezes it, mirroring the inherited pause (`inheritedPause`,
`tui/graphview.go`). Today `graphData.blocked` reads only the row's own blockers, so
under a Chart held by a prerequisite (ADR 0268 (a-chart-level-prerequisite-gates-the-whole-chart…))
the Defog tab shows its Survey and defog children ready and launches them with no
dialog, letting a human chart past the freeze
(docs/research/container-blocked-by-outside-signal-course-effects.md L4).

- A Chart freezes on any open blocker. A Checkpoint freezes only when `Readmit` would
  not admit it (a pivot or a kind outside ADR 0269 (…checkpoint-level-prerequisite-holds-only-the-land)'s
  allow-list); a land-only hold leaves its Builds and Approval reading ready, as the
  loop runs them.
- An inherited-blocked row gets the blocked glyph and the launch dialog, which names
  the container's open blocker.
- No eventlog event (as ADR 0267 (…signal-offshoot…)) and no order shown on the
  offshoot's own row: its edge already renders.
- The Decision Packet (crew doc `approval-body.md`) gains an Execution Notes line
  "Land held by <blocker title> until it lands" whenever the Checkpoint has an open
  blocker, so approving a land-held Checkpoint is not mistaken for landing it.

## Considered Options

- **Any open blocker on any ancestor reads blocked**: rejected; after ADR 0269 it would
  misreport every Build and Approval of a land-held Checkpoint.
- **Charts only**: rejected; a pivot-frozen Checkpoint would keep misreporting.
