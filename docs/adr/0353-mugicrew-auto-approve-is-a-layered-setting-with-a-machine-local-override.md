# ADR 0353: mugicrew auto-approve is a layered setting with a machine-local override

Status: Accepted
Date: 2026-10-22

Amends: ADR 0346 (mugicrew-an-approval-is-afk-while-workspace-auto-approve-is-on)
§1 (the setting is read from the shared layer only, and shown read-only) and its
rejected option "a machine-local or layered setting".
Builds on: ADR 0112 (mugicrew-auto-exit-duration-is-a-layered-setting) §2, §5–6 for
the layers, the row and the save.
Leaves: ADR 0341 (mugicrew-a-failing-afk-approval-falls-back…) unchanged: how an AFK
Approval runs or falls back is not touched.

Decided at triage and in the first Chart's Survey, carried by the Signal "Layered
auto-approve setting with a machine-local override, fully settled".

## Context

ADR 0346 made `[approvals] auto_approve` shared-only so that no machine could quietly
opt out of a shared rule. In practice the maintainer wants to turn the crew loose on
one machine (their own, attended or not) without turning it on for every clone of the
Workspace, and to turn it off on one machine while the shared setting stays on.

## Decision

1. **Layered both ways, as ADR 0112 §2.** `[approvals] auto_approve` is read from the
   machine-local `<git-common-dir>/mugicrew/config.toml` first, then the checked-in
   `.mugicrew/config.toml`, then the built-in **off**. The local layer overrides in
   both directions: local on wins over shared off, and local off wins over shared on.
   Every layer is re-read on every call, as before. Every reader goes through the
   one setting read, so the AFK mode snapshot (ADR 0346 §3) follows the layered value.
2. **Unusable layers fall through.** A missing file, table or key falls through
   silently. A decode error or mistyped value warns `config.unusable` and falls
   through: unusable local to shared, unusable shared to built-in off. This replaces
   ADR 0346 §1's "reads off" for the local layer; a typo in the shared layer still
   reads off, so it never approves anything.
3. **The Settings row shows its source** the way the AFK pool and Auto-exit rows do:
   `local`, `config.toml`, `default`, `unusable → <fallback>`, and `local; shared off`
   (or `shared on`) when a local value sits over a checked-in one.
4. **The row is a staged, saveable edit of the local layer.** `+`/`-` cycle the staged
   value on → off → (unset), as the Kind profiles rows do. `enter` saves and `esc`
   discards. A save rewrites only its own key (ADR 0112 §6); `(unset)` deletes the
   local key, dropping an emptied `[approvals]` table, so the machine follows the
   shared layer again. Turning it on needs no extra confirmation; the status line
   names the scope, e.g. `Auto-approve on saved (this machine)`.
5. **A save over a malformed local file is refused**, as Kind profiles' save is, with
   the status naming the file. It never replaces the file.

## Consequences

- One machine can let an Approval through with no human, and under `local` land its
  Checkpoint, even while the shared setting is off. This is the accepted cost: whoever
  can write the machine's git common dir can already land by hand.
- One machine can also opt out of a shared `on`, which ADR 0346 rejected; its
  Approvals stay HITL there while other machines approve them AFK.
- The setting's Glossary line (Approval, _Auto-approve_) names the layered setting.
- `mugicrew init workspace` still neither asks about nor owns the key.

## Considered Options

- **Shared-only (ADR 0346 §1, kept):** rejected. It forces a per-machine want into a
  checked-in change every clone inherits.
- **Local can only turn it on, never off:** rejected. ADR 0112's symmetric layers are
  the one shape the Settings tab already explains; a one-way override is a second rule.
- **A confirmation prompt before saving on:** rejected. The staged edit plus `enter`
  is already deliberate, and the status line names the scope.
