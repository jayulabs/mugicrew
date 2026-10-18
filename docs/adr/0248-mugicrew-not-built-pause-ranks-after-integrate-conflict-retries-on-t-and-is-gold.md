# ADR 0248: mugicrew not-built pause — `paused:not-built` ranks after integrate-conflict, `t` retries it, and its `‖` is gold

Status: Accepted
Date: 2026-10-17
Amends: ADR 0078 (failure-pause) §2/§5/§6/§7, ADR 0104 (integrate-conflict-pause-lifted-by-integrate) §4.

## Context

If a Build's backstop integrate exits 0 but no integrate trailer for the Build
is on its Checkpoint, the Build is reopened and paused under a new cause. That
was settled at charting for 'Defog-tab placeholder launch and
close-only-after-integrate Builds'. Grilling settled the cause's name and how
it appears.

## Decision

1. **Cause `paused:not-built`.** It names the cause, not the mechanism (ADR
   0055 §2), and it matches the `Not built: <why>` comment the Build prompt
   tells the agent to leave.
2. **Precedence: manual > abandoned > recovering > integrate-conflict >
   not-built > failing > context > profile > foreign-hold.** Like
   integrate-conflict, it means the work is missing and a human has to
   decide. That outranks failing, which is a retry decision.
3. **`t` is allowed and labelled `retry`.** It is a non-refusing row in the
   run-now cause table. A re-run that integrates lifts the cause and logs
   `effort.resumed {cause: not-built}`. `bd label remove` stays the human's
   escape hatch (ADR 0078 §4).
4. **`‖` is gold (220).** The other free colours were rejected: green reads
   as "fine", and grey reads as dimmed. The detail pane shows the cause only
   (ADR 0078 §7).
5. **Event.** `effort.paused {cause: not-built, branch, note}` logs at Warn.
   `note` is the Build's latest `Not built:` comment with the prefix
   stripped, and it is omitted when the Build left none. There is no `tree`,
   because the integrated arm has already torn the tree down.
6. **The failure counter ignores it.** The effort neither counts as failed nor
   resets the count (ADR 0078 §2), as for integrate-conflict (ADR 0104 §5).
   The handler did not fail, and the pause already stops re-dispatch.

## Considered and rejected

- **`paused:no-trailer`.** It names the mechanism, not the cause.
- **Refusing `t`, as manual does.** A re-run that integrates is the natural
  fix, and the guard re-checks it.
- **Leaving the reason off the event.** The human would have to go and read
  the bead's comments to learn why the Build was not built.
