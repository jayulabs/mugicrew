# ADR 0078: mugicrew failure pause — the loop writes `paused:failing` after 3 consecutive failed efforts; it never clears itself, and only the human or a successful run-now lifts it

Status: Accepted
Date: 2026-10-05

## Context

A bead whose handler fails deterministically is re-dispatched every tick
(retry storm). Triage chose a pause label over backoff, because a label
shows on the graph and survives a restart. This amends ADR 0055 §2 (a third
cause) and ADR 0057 §1/§5 (its glyph colour and `t`). The facts are in
the research. Grilling
settled the surface.

## Decision

1. **Cause `paused:failing`.** It names the cause, not the mechanism (ADR 0055
   §2). It is written additively (ADR 0070 §3).
2. **Count in loop memory, per bead, beside the hold count.** An effort counts
   as failed when Dispatch returns
   `err != nil && o.Cut != CutBusy && o.Cut != CutHeld`. This covers every
   `effort.failed` path, cut failures included (ADR 0056). `effort.ended`
   resets the count. Busy and held **neither add nor reset**, because nothing
   ran. A failure still resets the hold count (ADR 0056 §4). After **3**, the
   loop writes `paused:failing`. A restart loses at most 2, and so can a
   run-now success the loop never sees. Both are accepted.
3. **It never clears itself**, because success isn't observable while the bead
   is paused. The loop's `resume` never touches it.
4. **Run-now** (`t`, labelled `t retry`) is allowed on a failing bead. When the
   effort returns `err == nil`, it lifts `paused:failing` and logs
   `effort.resumed`. If the effort fails, the label stays and nothing is
   counted, because the TUI can't reach the loop's memory. There is no
   TUI-quit fallback (unlike ADR 0055's amendment), so a run that
   succeeds after the TUI quits leaves the label for the human to remove. On a
   bead that also carries foreign-hold, each cause lifts on its own condition:
   foreign-hold on a clean cut, failing on a clean return. `paused:manual`
   still refuses `t`.
5. **Events.** `effort.paused` and `effort.resumed` are reused, and both gain a
   `cause` field for every cause, foreign-hold included. A failing pause logs
   `effort.paused` at Warn
   `{cause: failing, failures: 3, error: <last effort's error>}`, and run-now's
   lift logs `effort.resumed {cause: failing}`. When the human removes the
   label, nothing is logged, the same as a manual unpause.
6. **Precedence: manual > failing > foreign-hold.** It applies wherever one
   cause is shown or decides behaviour: the `‖` colour, the order in the pause
   line, and `t`. Failing needs a human, and foreign-hold clears itself.
7. **TUI.** `‖` is red (the `warn` colour, 203) for failing. The detail pane
   shows causes only (ADR 0057 amendment), with no last error. The
   error is in the Logs tab's `effort.paused` and `effort.failed` lines.

## Considered and rejected

- **Backoff timers.** Rejected at triage. They are invisible on the graph and
  lost on restart.
- **`paused:max_retries`.** It names the mechanism (ADR 0055).
- **New event names** (`effort.failing`, …). One pause/resume pair with a
  cause field reads the same for every cause in the Logs tab.
- **Busy and held reset the failure count.** A bead that alternates held and
  failed would never reach either threshold.
- **Reset the count when the loop sees the label removed.** It adds code to
  save at most 2 failures, which is the same bound a restart already has.
- **Showing the last error in the detail pane.** Reading the event log per
  selection was already rejected (ADR 0057 amendment).

## Consequences

- `loop.go` must keep Dispatch's `err`, which today it discards.
- `bd` gains `PausedFailing`. `pauseSty` and `runNowLabel` gain entries, and
  `PausedCauses` ranks `manual`, then `failing`, then the rest.
- The existing foreign-hold `effort.paused`/`effort.resumed` lines gain
  `cause: foreign-hold`.

## Amendment: precedence gains `abandoned` (ADR 0082)

§6 becomes **manual > abandoned > failing > foreign-hold**.

## Amendment: precedence gains `integrate-conflict` (ADR 0104)

§6 becomes **manual > abandoned > integrate-conflict > failing >
foreign-hold**. §2: an effort that ends in a backstop integrate conflict
neither counts as failed nor resets the count. §5: the integrate-conflict
pause and lift reuse `effort.paused`/`effort.resumed` with
`cause: integrate-conflict`.
