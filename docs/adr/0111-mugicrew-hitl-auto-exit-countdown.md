# ADR 0111: Auto-exit: the closing line arms a countdown in `hitl-run`, and a reply or F11 cancels it

Status: Accepted
Date: 2026-10-09

Amends ADR 0099 §1 and §4 (what the hooks write), ADR 0066 §3–4 (who owns the
HITL bar) and ADR 0067 §2 (session-row markers). Builds on ADR 0108. Decided in grilling, with input from research.

## Context

The human wants to walk away from a HITL session that needs nothing more from
them and know it will clean up after itself. A done session that nobody exits
keeps its Drafts unpublished and its tree alive. *Done* (the bead is closed)
cannot be the trigger, because Signal and Chart sessions close their bead only
at publish (ADR 0096). The closing line (ADR 0067 §1) is shown by every role.
`Stop`'s `last_assistant_message` reports it reliably. A SIGINT sent to the
claude pid gives the clean exit (rc 0) that publishes.

## Decision

1. **Trigger.** A `Stop` whose `last_assistant_message` contains the closing
   line's text (`The work for this session is finished.`, shared with
   `contract.go`) arms the countdown, but only when `background_tasks` and
   `session_crons` are both empty. Whether the bead is closed plays no part,
   so every role behaves the same. `StopFailure` never arms it. Every matching
   `Stop` re-arms it with a fresh full duration, including after a cancel.
2. **The clock is in `hitl-run`.** `hitl-hook` arms by setting
   `@autofinish=<deadline, unix seconds>` on the session and disarms by
   unsetting it. A goroutine in `hitl-run` watches the option. It is the
   claude process's parent and lives exactly as long as the session, so the
   countdown runs with no TUI and survives a TUI quit. The duration and the
   off switch are a separate decision (the same Chart).
3. **Disarm edges.** Any hook edge other than an arming `Stop` disarms:
   `UserPromptSubmit` (any reply, including a turn the agent starts itself),
   `PermissionRequest`, and the elicitation `Notification`s. In the pane,
   **F11** (bound in the HITL key table) disarms. Attaching, `l` and F12 do not
   disarm, because a look is not a reply (ADR 0099 §5). A spawned or respawned
   pane starts disarmed, as with `@waiting` (ADR 0099 §6), so a resumed
   session auto-exits only after it shows a new closing line.
4. **Firing.** When the deadline passes, `hitl-run` checks the session again:
   `@autofinish` must still be set and `@waiting` must still be `turn`. If both
   hold, it sends SIGINT to the claude pid. It never signals the pane's process
   group or itself (a hangup to `hitl-run` is a drop). Otherwise it disarms
   silently. A clean exit then runs the normal close: Closing, publish,
   `✓ <id> finished` (ADR 0108). It never fires over an open prompt or
   question.
5. **Display.** While armed, `hitl-run` owns the pane's `status-right` and
   rewrites it as it counts down: `auto-exit in 1:45 · F11 cancels`. On
   disarm it restores the normal bar. The TUI's `SetBars` skips sessions that
   have `@autofinish` set. The TUI row reads the deadline through `listFormat`
   (no subprocess, ADR 0101). Its trailing suffix `auto-exit in 2m…` (coarse,
   in the ADR 0108 `<verb>…` style) takes the place of `done — /exit`, or of
   the plain `●` for a Signal or Chart row. Once the session exits it becomes
   `closing…`.

## Considered Options

- **Bead closed as the trigger:** rejected. It never fires for Signal or Chart
  sessions (ADR 0096).
- **A detached timer started by the hook:** rejected. It is a loose process
  that nothing owns, it needs pid discovery to cancel, and it is a second writer
  beside `hitl-hook`.
- **The TUI tick as the clock:** rejected. The countdown would stop when the TUI
  quits, which is exactly when the human has stepped away.
- **`send-keys '/exit'` or a double `C-c` to exit:** rejected. A draft in the
  prompt box mangles `/exit`, while SIGINT to the pid is clean at an idle prompt.
- **Any keystroke cancels** (an `Any` binding in an armed key table): deferred.
  It is not established that tmux can pass that first key on to claude. F11 is
  certain and the bar names it.
- **A cancel key on the TUI row:** skipped. Replying, or attaching and pressing
  F11, already covers it.

## Consequences

- **A typed but unsubmitted draft is not sensed.** If the human starts typing
  and walks away, the countdown fires and SIGINT discards the draft. Accepted:
  F11 is the escape hatch.
- A quoted closing line in a later reply re-arms the countdown. This is harmless,
  because only a reply leads to such a turn, and the reply has already
  disarmed it.
- Esc mid-turn after the line has printed fires no `Stop`, so nothing arms. This
  is the safe direction.
