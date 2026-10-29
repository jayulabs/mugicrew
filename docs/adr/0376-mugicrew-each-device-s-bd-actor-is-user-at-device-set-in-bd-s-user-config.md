# ADR 0376: mugicrew each Device's bd actor is `<user>@<device>`, set in bd's user config

Status: Accepted
Date: 2026-10-28
Amends: ADR 0052 (claim-is-status-cas) §2.

Chart mc-l7u7f (each device's bd actor is `<user>@<host>` from a stable
configured host name). Settled by its Survey mc-l7u7f.1. The Signal "Share one
Bead Graph across devices by push/pull through the homelab Dolt remote" relies
on it.

## Context

The bd actor resolves to the same bare user name (`<user>`) on every
machine. If one Bead Graph is shared across several devices, ADR 0240
(a-claim-held-elsewhere-refuses-launch…) §3 can't tell another device's live
claim from a stranded one. So `x` or `mugicrew recover` could release live work.
If the actor names the Device, the claim shows as another actor's, and the
cross-actor guard in ADR 0240 §2 and §4 already refuses to release it.

bd 1.3.0 resolves its actor as `$BEADS_ACTOR`, then the `actor:` key in its
user config `~/.config/bd/config.yaml`, then git `user.name`, then `$USER`. We
verified this in a scratch graph. mugicrew's copy of the order left out the
config step.

## Decision

1. **The actor is `<user>@<device>`.** `<user>` names the person and is the
   same on every Device. `<device>` is a stable Device name, not the raw
   hostname.
2. **It is configured once per Device, as `actor: <user>@<device>` in
   `~/.config/bd/config.yaml`.** That file is per-user and not tied to any
   Workspace. So a plain `bd unclaim` in any Workspace, this one or not, sees
   the same "you" as mugicrew, with no shellHook and no `BEADS_ACTOR` export.
   `$BEADS_ACTOR` still overrides it, as bd does.
3. **mugicrew asks bd for the actor** (amends ADR 0052 §2). It no longer
   mirrors bd's resolution order in Go. It reads the effective actor from bd
   once per process, so it can't drift from bd again.
4. **A missing Device name is a doctor warning, not a refusal.** If the
   resolved actor has no `@<device>`, mugicrew keeps working under the bare
   name, as it does today. `mugicrew doctor` warns and names the line to add to
   `~/.config/bd/config.yaml`.
5. **Bare-name claims are reassigned once, by `mugicrew doctor --fix`.** When
   the actor carries a Device, the doctor reports in-progress beads still
   assigned to its bare `<user>`, and `--fix` reassigns them to the full actor.
   That is sound only while every bare claim was made on this Device, which is
   true before any graph is shared. The bare name is not treated as "mine"
   afterwards.

## Considered Options

- **The user exports `BEADS_ACTOR` in their shell, or the
  Workspace's shellHook does.** Rejected: it covers only shells that run the
  hook. A plain `bd` in another Workspace would still see the bare name.
- **A mugicrew-owned Device file plus `eval "$(mugicrew env)"`.** Rejected:
  bd's own user config already reaches every `bd` call without shell setup.
- **Mirror bd's order in Go, adding the config.yaml step.** Rejected: that
  mirror is what had drifted.
- **Refuse to claim with no Device name.** Rejected: it would break every fresh
  install and every single-device user. The sharing Signal can tighten this.
- **`<user>` is the unix login on each Device.** Rejected: logins can differ
  between machines, and a claim should name the person who holds it.
- **Treat the bare name as still yours.** Rejected: once a second Device
  joins, that brings back the ambiguity this ADR removes.
