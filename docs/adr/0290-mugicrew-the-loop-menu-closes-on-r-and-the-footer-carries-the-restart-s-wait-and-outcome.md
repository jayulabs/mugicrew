# ADR 0290: The loop menu closes on `r`, and the footer carries the restart's wait and outcome

Status: Accepted
Date: 2026-10-17
Amends: ADR 0288 (mugicrew-the-tab-bar-shows-a-loop-state-word…) §4

Chart: A human restarts the loop onto main's latest build without stopping it.
Settles the grilling "How the loop menu reports a restart's progress and
refusals".

## Decision

1. **`r` closes the menu at once**, like every other menu option. The
   shelled-out `loop restart` waits for the build (about 15–40 s), but esc
   could never cancel that build: the SIGUSR2 request is already with the
   loop (ADR 0289 (mugicrew-loop-restart-asks-by-sigusr2…)). A menu held open
   would only block the board.
2. **The footer shows the wait.** While the build runs, the tab bar still
   reads `loop on`/`loop limited`. So the footer carries a pending status,
   `restarting loop: building main…`, until the verb returns.
3. **Every outcome reaches the footer in the CLI's own words, exit 0 too.**
   This includes already current, restart drain started, already restarting,
   build failed, the refusals (stopping, not from source, off) and an old
   build that ignored the request. Today the TUI flashes a loop verb's output
   only on failure. For `loop restart` it also flashes stdout on success,
   because "already current" changes no state, and without the flash the
   pending line would just vanish. There is no in-menu outcome text. The tab
   bar's `loop restarting` or `… stale` follows on its next read.
4. **While the verb is out, `o` still opens the menu.** Its details stay
   readable, and its options show dimmed with "restart in progress" and do
   nothing until the verb returns. Quitting the TUI meanwhile is safe: the
   loop finishes the build without it.

## Considered options

- **Keep the menu open with a progress line until the outcome:** rejected.
  It blocks the board for up to 40 s and offers no real cancel.
- **Rely on the tab bar's next state word alone:** rejected. "Already
  current" and the refusals change no state, so they would be silent.
