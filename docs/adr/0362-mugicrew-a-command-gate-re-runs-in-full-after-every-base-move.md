# ADR 0362: A command gate re-runs in full after every base move

Status: Accepted
Date: 2026-10-28

Chart mc-ij6us (A land gate can be a build command run in a checkout of the
composed squash). Settles grilling mc-ij6us.5. Under ADR 0150
(the-land-gate-builds-the-composed-squash…) a moved base sends the gate back to
compose, at most 3 times per reconcile, with the nix store as the only memo, so
a re-gate costs ~3s. A `gate_command` gate has no memo: each attempt re-runs the
whole command. Measured on this Workspace, mainline moved with a median gap of
~11 minutes and a 10th percentile of ~3 minutes, so a ~10-minute suite misses
about every other attempt.

## Decision

1. **Every attempt runs the command in full.** A command gate keeps ADR 0150 §1
   (the commit that was gated is the commit that lands) and §4 (nothing is
   remembered across attempts or ticks). The gate still runs in the Landing's
   child process, so a long run holds only that Landing's claim, never the loop.
2. **The 3-attempt bound is shared.** `maxGateAttempts` stays one constant for
   both gate kinds. With the 15m timeout, one reconcile may hold its claim for up
   to ~45 minutes. That is visible as an aging in_progress Landing
   (ADR 0151 §3). The bound only sets how often `gate.pending` is logged, since
   the next tick retries regardless.
3. **`gate.pending` is unchanged.** It is a Warn event, does not count toward
   `paused:failing`, and has no escalation: a busy mainline clears itself, only
   more slowly (ADR 0151 §4). A starvation surface waits for a Signal from a
   starvation that was actually seen.

## Considered Options

- **Skip the re-run when the re-composed tree equals the tree that passed.**
  Rejected: correct but inert. A base move changes the composed tree unless
  mainline returns to an identical tree (a land and its Rollback), so it would
  almost never fire.
- **Land the clean re-compose ungated after a pass on the previous base.**
  Rejected: it ends starvation but breaks "gated = landed". Two changes that
  each pass, but conflict in meaning, could break mainline together. That is
  the gap a merge queue exists to close.
- **A bound of 1, or a time budget, for a command gate.** Rejected: it shortens
  the hold but adds a second constant and more `gate.pending` noise, and it does
  not change whether the Landing eventually lands.
- **Escalating after N consecutive `gate.pending`.** Rejected for now: it needs a
  memory across ticks or an event-log read-back, both ruled out by ADR 0150 §4
  and ADR 0151.
