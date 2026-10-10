# ADR 0067: HITL session end — one injected closing line; the TUI says finished or running, never "detached"

Status: Accepted
Date: 2026-10-05

Amends: ADR 0038 §1 (session-row markers). Builds on ADR 0065 §6 and ADR 0066.
Decided in grilling.

## Context

Before ADR 0065, the TUI blocked on a tmux attach. When the attach returned, the
TUI flashed `detached`, whether the human had pressed F12 or the session had ended
(`attachDoneMsg`). Under ADR 0065 a HITL session is reached by `switch-client`,
which returns at once, so that flash has nothing left to trigger it. The human
still had no clear signal in the conversation that the work was done and that
`/exit`, not F12, was the way out. A done session that is never exited also keeps
its Drafts unpublished, because only a clean exit publishes them.

## Decision

1. **One closing line, injected by mugicrew.** Every HITL prompt gets a shared
   instruction at launch from one source, the same way the Draft instruction is
   spliced in. Roles do not each carry their own copy. Right after the agent
   closes its bead, it prints, between two heavy `━` bars so it stands out:
   `✅ **The work for this session is finished.** You may close this chat.`
   `` `/exit` or hit Ctrl+C twice to close. ``
   `/exit` is literal for now (same deferral as ADR 0066 §5). The line is how
   done is signalled inside the conversation. The status-bar flip is how it is
   signalled outside it (ADR 0066 §4). Neither one defines done: done means the
   bead is closed.
2. **The TUI drops the `detached` flash.** Each HITL session row shows its state:
   - `●` alive
   - `● done — /exit` alive, but its bead is closed
   - red `✕` exited, i.e. a failed exit: resume or drop
   A cleanly finished session leaves the session list (ADR 0037). When the TUI's
   tick first sees its `hitl.exited`, it flashes `✓ <id> finished` once. No tmux
   hook detects F12 returns.
3. **No extra nudge for unpublished Drafts.** A done session that is still alive
   already shows two loud cues: the HITL bar's `✓ <id> closed — /exit to finish`
   and the row's `done — /exit`. The TUI does not count Drafts.
4. **Vocabulary.** *Done* means the bead is closed. *Finished* means the session
   exited cleanly. The human's verbs are "/exit to finish" and "F12 back to
   TUI". "Detached" remains a domain term (glossary: Detached session) and
   appears on no human-facing label.

## Considered options

- **The closing line in each role prompt:** the copies drift, and each new HITL
  role has to remember to include it.
- **A tmux `client-session-changed` hook to flash on F12 returns:** this adds
  tmux plumbing only to re-create a flash. The row markers already show the same
  state, derived from data the tick already reads.
- **Only the row glyphs, with no finished flash:** a cleanly finished session
  disappears from the list without a word, and the human can't tell it apart
  from a crash that left it _gone_.
- **A Drafts count on done rows, or a TUI-wide warning:** each refresh would cost
  more bd queries, and the two existing cues already say what to do.
