# ADR 0383: The drawer body is cached per bead and width, and queued wheel events merge into one scroll

Status: Accepted
Date: 2026-10-28

Wheel scrolling of the bottom detail drawer felt unresponsive. Chart Mouse mode polish
(mc-cdies) prototyped why (bead "Measure why wheel scrolling of the detail drawer feels
unresponsive, and pick the fix"; throwaway on `refs/mugicrew/prototypes/mc-cdies`). On
the longest real bead body (35 KB, 466 drawer lines at 120 columns) one wheel event cost
10–20 ms median for Update + View, with a p90 of up to 34 ms. Most of that went to
building the drawer twice. `wheel` builds a `drawerVP` only to read back the new offset,
and View builds it again. Each build renders the Markdown (about 2–3 ms) and re-wraps the
whole text through lipgloss, even though only one screen of it is visible. Through tmux
the human's wheel sends **6 events per notch** (0 ms apart), and a fast spin sends
130–970 events/s. Bubble Tea v1 runs one Update and one View per event, so at 15 ms per
event the TUI keeps up with only about 67 events/s. Lag grew to 7.5 s, and the drawer
kept scrolling after the hand stopped. The human picked:

1. **The drawer's rendered body is cached per (bead, width).** The Markdown render and
   the wrapped drawer text are memoized, so a wheel event never re-renders an unchanged
   body. A changed body (a live refresh) or a new width is a cache miss. The `wheel`
   path stops building a viewport just to compute the offset. In the prototype this cut
   Update + View per event to about 3 ms (p90 5 ms). This replaces `mdBody`'s note that
   it keeps no cache.
2. **Wheel events queued behind a render merge into one scroll.** Their deltas are
   summed and applied in a single Update + View. In the probe, merging kept the worst lag
   at 32 ms even at today's 15 ms cost, and at 8 ms with the cache, for spins up to
   971 events/s. 1,122 events took 164 passes.
3. **One wheel event scrolls 1 line (`wheelStep` = 1, down from 3).** On a wheel that
   sends 6 events per notch, a notch then moves 6 lines, not 18. `wheelStep` is shared,
   so lists scroll 1 row per event too.

## Considered Options

- **Cache only.** Rejected: at about 3 ms per event a hard spin (350–440 events/s)
  still outruns the TUI and lags by a few hundred ms.
- **Merge only.** Rejected: it removes the lag, but each pass would still cost about
  15 ms and a single notch would take a frame to show.
- **Keep the step at 3.** Rejected: with 6 events per notch it jumps 18 lines.

## Consequences

- Bubble Tea v1 can't look ahead in its message queue, so merging uses an accumulator the
  Model shares by pointer. MotionFilter adds a same-cell body wheel press to it while it
  is armed and drops the event. An unarmed press passes through, and its Update arms the
  accumulator and returns a Cmd that sends a flush message. Update applies the pending
  delta before it handles any message, then disarms. The mechanism is settled in the Spec
  "Mouse mode polish: clickable header segments, mouse editing in forms and filter lines,
  and a responsive detail-drawer wheel" (section C, Merge).
- Each surface's drawer (Graph, Ideas, Defog, Execution, Initiatives, Logs and the focus
  view) gets the cache through the shared `mdBody`/`drawerVP` path, not per tab.
