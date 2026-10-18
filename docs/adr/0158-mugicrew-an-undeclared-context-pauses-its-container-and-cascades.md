# An undeclared context pauses its container and cascades

Status: accepted

A `context:` label that names no declared `[context.*]` never falls back silently
to the whole Workspace. Instead it holds the container. This amends ADR 0055
(paused beads) and ADR 0081 (the manual cascade).

- **Triage refuses up front.** `mugicrew reconcile` checks the name against the
  declared contexts and exits non-zero on a miss, and Luffy re-asks the human. A
  hold is then only a backstop for drift: a context renamed or removed from
  config after labelling, or a `context:` label applied by hand.
- **A new cause, `paused:context`, is written on the container.** It is never
  written on each leaf. `InheritedPause` cascades `context` the same way it
  cascades `manual`, so every child under the container is skipped. This makes
  `context` the first cause other than `manual` that cascades. We rejected a
  per-leaf check of the container's label because it costs a lookup per leaf on
  every tick. The walk still runs only when a known bead carries `manual` or
  `context`. The TUI's `‖` glyph and pause line come with the label.
- **The loop checks every tick, not at launch.** Each tick scans the open,
  in-progress and deferred containers it already fetches, so even an idle
  container is caught when config drifts. Config is resolved only if a fetched
  bead carries `context:`. A no-context Workspace therefore pays zero reads and
  zero writes (ADR 0157).
- **The hold lifts itself, as `paused:profile` does (ADR 0123 §3).** Three human
  fixes count, and each is the whole fix:
  - relabel the container to a declared context;
  - remove the label, so it targets the whole Workspace;
  - declare the missing context in config.

  On the next tick the loop removes `paused:context` and logs `effort.resumed
  {cause context}`. There is no new verb. The TUI pause line names the
  undeclared context and these three fixes.
- **Run-now refuses**, as it does for `manual`: the focus line cannot resolve a
  path for an undeclared name. **In-flight efforts are not interrupted.** The
  hold gates only new dispatch.
- **Rank:** `context` sits just above `profile` in `PauseRank`. Both are holds a
  config fix can clear, but `context` is stickier because a human must act.
