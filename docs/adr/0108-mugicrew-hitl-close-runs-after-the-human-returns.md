# ADR 0108: A HITL session's close runs after the human is back in the TUI

Status: Accepted
Date: 2026-10-09

Amends ADR 0065 §6 (when viewers return) and ADR 0067 §2 (session-row markers).
Decided in grilling, with input from research.

## Context

After a clean `/exit`, `hitl-run` switches the human back to the TUI only as its
last step. Before that it publishes the Drafts (2–6 bd calls, measured at about
2–5 s) and, on an effort tree, runs the backstop integrate (more bd calls, a
rebase, and up to 60 s waiting on the land lock). Meanwhile the human sits on a
pane that shows no progress. The TUI does not block here: `hitl-run` owns the
close, and the close already survives a TUI quit.

## Decision

1. **Same owner, earlier return.** `hitl-run` keeps the whole close: the
   publish, the backstop or teardown, and the `hitl.exited` event. Only
   `returnViewers` moves, to just after the hangup check, and it runs only on a
   clean exit that a drop did not cause. The close then runs in a pane nobody is
   viewing. It survives a TUI quit and dies only with its pane. No detached
   handoff process is started.
2. **Closing is a session state.** `hitl-run` sets the `@closing` user option
   on its tmux session before the publish. The tick reads that option through
   `listFormat`, so render still starts no subprocess (ADR 0101). A Closing row
   keeps its `●` and shows a trailing `closing…`, styled like `launching…`. It
   shows `closing…` in place of `done — /exit`, or in place of the plain `●`
   for a Signal, whose bead closes only inside the publish (ADR 0096). When the
   session leaves the list, the existing `✓ <id> finished` flash fires.
3. **Two concepts, one look.** Closing belongs to the session: `hitl-run` owns
   it and every TUI senses it. An ADR 0107 in-flight word belongs to a single
   TUI: it is a write that TUI has pending, and it disappears with that TUI.
   The two share only the trailing `<verb>…` suffix style.
4. **A failed close.** A failed publish or teardown leaves a dead pane, which
   shows as the red `✕` row (ADR 0067). When the tick first sees a session go
   from Closing to exited, it flashes `✕ <id> close failed — resume or drop`
   once on the status line. Nothing pulls the human back to the pane. A
   backstop integrate conflict is not a failed close. It pauses the bead, and
   that pause shows as it does today.
5. **While Closing.** `l` (attach) and `x` (drop) on a Closing row are refused
   with `<id> is closing — wait`, through ADR 0107's refusal pattern. Other keys
   behave as they do today for the bead's state. Once a close fails (`✕`),
   resume and drop work as before. A manual `tmux kill-session` during the close
   can still race the publish or the rebase. That race is known and accepted.

## Considered Options

- **A detached (`setsid`) close process that outlives the pane:** rejected. It
  adds a process that has no pane and no row to report through. Today's owner
  already survives a TUI quit.
- **The TUI or the loop runs the close:** rejected. The close would then need
  the TUI or the loop alive, a regression from today.
- **No closing marker, or a distinct glyph:** rejected. With no marker, the row
  keeps saying `done — /exit` after the human has exited. A distinct glyph
  breaks ADR 0107's rule that the glyph keeps its meaning.
- **Pulling the human back to a failed close, or allowing attach while
  Closing:** rejected. The human left on purpose, and the `✕` path already
  handles anything that goes wrong.
