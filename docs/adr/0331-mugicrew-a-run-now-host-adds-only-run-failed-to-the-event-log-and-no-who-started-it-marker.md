# A run-now host adds only run.failed to the event log, and no who-started-it marker

Status: accepted

A run-now effort is hosted by a setsid-detached `mugicrew run <id>` (ADR 0330
(mugicrew-a-run-now-host-inherits-the-effort-flock-by-fd…)). Its effort
lifecycle stays the `effort.*` events that `dispatch.Dispatch` writes in the
host. The event log gains exactly one event. Nothing new marks an effort as
started by run-now rather than by the loop.

- **No who-started-it marker.** Every event already carries `proc`, the CLI
  verb as typed (ADR 0029 (mugicrew-event-schema) §3, amendment). So a host's
  `effort.*`, `claim.*` and `session.*` events read `proc:"run"`, and the
  loop's read `proc:"loop"`. A post-mortem needs nothing more. We add no graph
  label and no `via` attr.
- **`run.failed`** (Error; bead attrs, `effort`, `err`) is written in two
  cases:
  - By the TUI (`proc:"tui"`) when `cmd.Start` of the host fails after the
    claim. The TUI then unclaims (`claim.released`) and releases the hold.
  - By the host (`proc:"run"`) when it fails before Dispatch, e.g. a bad
    `--lock-fd` or a failed bead re-read. The host then unclaims and exits.

  This mirrors `loop.failed` on a failed loop start. Like `loop.failed`, it is
  shown in the Logs tab only and is not added to the TUI flash filter. The TUI
  that started the host already reports the failure from `Start`'s error or
  from `Wait`'s exit status.
- **No `run.started` or `run.exited`.** A host carries one effort, so
  `effort.started` and `effort.ended`/`effort.failed` (or `effort.busy` or
  `effort.held`) already bracket its life. The loop's `loop.started` and
  `loop.stopped` are needed because the loop hosts many efforts.
- **`retrying()` reads the graph and liveness, not the event log.** After a
  TUI restart, `m.runs` is empty. The TUI now counts a bead as a run-now retry
  in flight when it is paused `paused:failing` or `paused:foreign-hold` **and**
  its effort is live. A live bead paused `paused:manual` (an abort winding
  down) still counts as paused. The event log stays history, never read as
  truth (ADR 0033 (mugicrew-event-log-is-history-not-a-view)).

## Considered Options

- **A graph label naming the starter.** Rejected. It is new coordination state
  that a crash would strand, and its only consumer (`retrying()`) can derive
  the same answer.
- **`effort.failed` for a spawn failure.** Rejected. No `effort.started`
  precedes it, which breaks the started→ended/failed pairing that the Logs tab
  and the retrospective rely on.

## Consequences

The Spec adds a `run.failed` row to the catalogue in ADR 0029 §5.
