# ADR 0238: the TUI detail drawers drop the ON LAUNCH preview line

Status: Accepted
Amended by ADR 0265 (§3: the remaining lines lose closed and AFK).
Date: 2026-10-16

Amends: ADR 0101 §2, ADR 0146 §3, ADR 0164 §9, ADR 0178 §6 and the "v2 ON
LAUNCH" row of ADR 0182 §5. Spec mc-04f5.

## Context

For an open, launchable HITL bead, the Graph drawer and the Ideate detail ended
with `ON LAUNCH — <Owner> opens <a session> for <kind>`. The human gains
nothing from it: the hints bar's `enter` already offers the launch, and the
line took space at the foot of every drawer. The ADRs above recorded it as an
intended affordance.

## Decision

1. **The ON LAUNCH line is gone**, in both forms: `ON LAUNCH — <Owner> opens
   <a session> for <kind>`, with its `— no body yet: settles it first` suffix,
   and the `ON LAUNCH: <err>` wiring-error form.
2. **The drawer ends at the bead's body.** When no launch-preview branch
   applies the preview is empty, and the Graph drawer and the Ideate detail
   append nothing, so there is no trailing blank line.
3. **Every other launch-preview line stays:** SESSION, closed, running/dead,
   idle, claim-no-session, CHECKPOINT, CONTAINER and AFK.
4. **`enter` is unchanged.** It still runs the launch-owner check, so a
   wiring error still shows when `enter` is pressed. The no-body-yet cue stays
   on the Ideate rows and the focus header.

ADR 0101 §1 and §4 (no subprocess from render, and its guard test) still apply
to the remaining lines. ADR 0220 §4 only mentions the line in passing and is not
amended.

## Considered Options

- **Keep the line and shorten it.** Rejected. It still repeats the hints bar.
