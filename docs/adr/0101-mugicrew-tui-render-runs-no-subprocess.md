# ADR 0101: mugicrew TUI render runs no subprocess; the launch preview is static

Status: Accepted
Date: 2026-10-08

Extends ADR 0098 (press-time bd calls off the event loop) from key handlers to
`View` and every render helper. Decided in grilling, on measured evidence.

## Context

The measurement showed the remaining freeze at one place. With an Approval selected,
`View → graphView → detailLayout → launchPreview → launchFor → Luffy.Launch →
surgeryBehind` ran 2–3 `bd show` on every render. Views took 21.3s and 3.1s at
1000 beads with the loop on, against a ~50ms budget. Zoro's `pivotTargets` reads
have the same shape. Each render also paid `NewInvocation`'s git reads.

The preview never showed anything those reads produced. It truncated each argv
element to 64 chars, and every prompt starts with the shared crewmate preamble,
so every HITL kind rendered the same `claude "You are a mugicrew crewmate: …"`
line.

## Decision

1. **No subprocess from render.** `View` and its helpers run no bd, git or tmux
   process.
2. **Static launch preview.** For a HITL bead the Graph detail shows `ON LAUNCH —
   <Crewmate> opens an agent session for <kind>`. It is built from the registry
   owner and the kind, with no `Launch` call. The dead `ChecklistCommand` branch
   goes.
3. **Press-time `launchFor` moves into the launch Cmd.** `launchNow` resolves the
   command inside `startLaunch`'s Cmd, not inline. This closes a case that ADR
   0098 §1 already forbids. The evidence is the earlier per-call bd cost
   (0.45–8s).
4. **Guard test.** A test puts recording fakes for `bd`, `git` and `tmux` on
   `PATH`, renders `View` with each kind selected in the Graph detail, and
   asserts zero invocations. This uses `PATH` rather than a seam var because
   bd's exec seam is unexported and the crewmates keep their own read vars.

## Considered Options

- Compute `launchFor` off the loop once per selection and cache it on the
  Model: rejected. It adds a Cmd, a message and a staleness key, all to render
  text that doesn't depend on the reads.
- Drop the preview only for kinds whose `Launch` reads bd: rejected. The other
  kinds would still pay git reads per render, and the detail would be
  inconsistent across kinds.
