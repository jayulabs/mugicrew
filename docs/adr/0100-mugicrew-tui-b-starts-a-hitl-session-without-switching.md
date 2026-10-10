# ADR 0100: TUI `b` starts or resumes a HITL session without switching into it

Status: Accepted; §4 superseded by ADR 0107
Date: 2026-10-08

Amends ADR 0038 §3 and ADR 0065 §1 (a launch or resume no longer always hands
over), and ADR 0026's bead-key table. Builds on ADR 0041 (orphan drop on
launch) and ADR 0099 (waiting on the human). Decided in
grilling.

## Context

Today `l` on a bead with no session (launch) or an exited session (resume)
always ends with `switch-client` into the new session. A human who wants to
start several sessions, or start one and keep triaging, has to F12 back each
time. The session itself is already running detached before the switch:
`session.Start`/`session.Resume` never touch a client, and only the TUI's
launch-done step calls `SwitchTo`. So a start without the switch is a TUI-only
change.

## Decision

1. **Trigger: `b`.** On Graph and Inbox, `b` does what `l` does for a launch or
   a resume, except that on success the TUI stays in front and nothing switches.
   It has the same refusals as `l` (not HITL, closed, container). On an alive
   session there is nothing to start: `b` sets the status
   `already running — l attach`. We rejected a modifier on `l` (`ctrl+l` is the
   terminal's clear-screen key, and the TUI can't see shift on a letter) and a
   prompt after `l` (an extra keypress on every foreground launch).
2. **Confirms carry the intent.** The blocked-launch confirm and the orphan-drop
   prompt (ADR 0041 §3) run as they do for `l`, and `y` still starts the session
   in the background. The blocked confirm reads
   `y launch in background · n/esc cancel`. The orphan prompt's text is unchanged.
3. **Feedback.** While the session starts, the row shows the existing
   `launching…` suffix. On success the status reads
   `started <title> — l attaches`. After that, the usual alive dot, `l attach`
   and the waiting marker (ADR 0099) tell the human the session is there and when
   to join. Joining is a plain `l attach`.
4. **One start in flight.** As with `l`, only one launch or resume runs at a
   time. Pressing `l` or `b` during it sets `still launching <title>…`. Running
   several starts in parallel would be a new Signal.
5. **Footer.** The bead keys gain `b bg` wherever `l` reads `launch`,
   `launch⚠` or `resume`. When the footer is too narrow, `b bg` is the first
   bead key dropped, ahead of `u`. The `?` overlay lists `b` as "launch or
   resume without switching".
6. **No new term.** A session started this way is an ordinary Detached session.
   Avoid "background session": no TUI runs in the background (ADR 0065 §4).
