# ADR 0346: mugicrew an Approval is AFK while Workspace auto-approve is on

Status: Accepted
Date: 2026-10-22

Amends: ADR 0020 (mugicrew-luffy-approval-crewmate) §1 and its Consequences line
fixing the Approval row as `Mode: HITL`; the registry's static-mode rule (ADR 0002
(mugicrew-bead-taxonomy), owner stays derived from kind).
Builds on: ADR 0341 (mugicrew-a-failing-afk-approval-falls-back…), ADR 0224
(mugicrew-require-initiatives-and-enforce…) §1–2 for the shared-only setting shape.

Spec: Workspace auto-approve setting resolving Approvals through AFK Luffy sessions.

## Context

Approval was the one kind in the Checkpoint tail with no AFK path: a static HITL
registry row, and `Luffy.Run` refusing it loudly. A maintainer who trusts the crew's
Reviews wants one Workspace-wide switch that lets the crew approve for itself.

## Decision

1. **The setting.** `[approvals] auto_approve` (bool) in the checked-in shared
   `.mugicrew/config.toml`, read from the shared layer only, on every call. A missing
   file, table or key reads off; a decode error or mistyped value warns
   `config.unusable` and reads off, so a typo never approves anything. It is shown as a
   read-only Auto-approve row in Settings. `mugicrew init workspace` neither asks about
   nor owns the key.
2. **`Luffy.Run` gains an Approval arm** (amends ADR 0020 §1). It no longer errors
   "approval is HITL": it runs a headless AFK session from the repo root, and Go checks
   the result and closes the Approval `approved` in the same publish batch as the
   session's offshoot Drafts. `Luffy.Launch` (the HITL session) is unchanged.
3. **The Approval's mode is live, with one exception to the static-mode rule.** Every
   other kind's mode is still fixed by its registry row. An Approval's mode is derived
   from the kind, a per-tick snapshot of the auto-approve setting, and the
   `auto-approve:failed` marker (ADR 0341): AFK only when the setting is on and the bead
   carries no marker, else HITL. The owner stays derived from kind (ADR 0002). ADR 0020's
   "registry row `Mode: HITL`" Consequences line now describes the setting-off default.
4. **No claim-time mode marker.** The mode is turned by bead, not at claim. A flip while
   an Approval is claimed misreports it until its session ends; safety holds through the
   claim CAS, the per-effort flock and Chopper's held-flock check.

## Consequences

- Under `local`, an auto-approved Checkpoint lands with no human; that is the accepted
  consequence of turning the setting on. Under `gitlab` the human still merges the MR.
- The close reason is exactly `approved`, so Jinbe's land gate and the rollup need no
  change.

## Considered Options

- **A machine-local or layered setting** (as ADR 0112): rejected, since a shared rule
  must not be quietly opted out of per machine.
- **Storing the mode on the bead at claim**: rejected for its write and reconcile cost;
  the misreport window above is the accepted risk.
