# ADR 0309: mugicrew's config.toml layers may declare `[profiles.<name>]`, read by mugicrew alone, beside the OE catalog

Status: Accepted
Date: 2026-10-19

Chart mc-ho5c7 (Agent Profiles declared in mugicrew's config.toml). Amends root
ADR 0093 (agent-profiles-live-in-a-layered-oe-root-catalog) rather than
superseding it: the OE catalog, its shape and its rules stay as ADR 0093 says.
The detail is in ADR 0304 (a-profile-name-in-several-files…), ADR 0305
(an-unusable-config-toml-profile-table…), ADR 0306
(doctor-and-refusal-reasons-name-the-file…), ADR 0307
(agentprofile-reads-profile-files-and-cfglayer…) and ADR 0308
(the-settings-cycle-is-every-declared-profile-name…).

Root ADR 0093 put every Agent Profile in the OE's catalog, so that mapping a
kind to a model meant editing a file outside mugicrew's own settings. A
Workspace that wants one profile for itself, or a machine that wants one for its
own endpoint, had to touch the OE.

## Decision

1. **Both `config.toml` layers may declare profiles.** The machine-local
   `<git-common-dir>/mugicrew/config.toml` and the checked-in
   `.mugicrew/config.toml` may hold `[profiles.<name>]` tables in the shape of
   root ADR 0093 §1. mugicrew alone reads them. No other Application is
   promised that these tables exist or mean anything.
2. **Beside the OE catalog, not instead of it.** The OE's
   `<OE root>/agent-profiles.toml` and its machine-local twin are unchanged.
   *Catalog* keeps meaning only that pair. The four files together are "the
   declared names". Precedence, bad tables, provenance, the code split and the
   Settings cycle are ADR 0304, ADR 0305, ADR 0306, ADR 0307 and ADR 0308.
3. **A local profile stays machine-local.** This answers ADR 0093's rejected
   "The Workspace's catalog" option, which rejected the Workspace as the home
   of profiles because they describe a machine's harnesses, models and
   credentials. The machine-local `config.toml` is under `<git-common-dir>`, so
   a profile in it is never committed and never travels. Cross-Application
   sharing of a profile still lives in the OE catalog.
4. **A profile in `.mugicrew/config.toml` travels with that file.** Unlike
   ADR 0093 §6, which keeps the catalog outside `apps/` so that only a
   whole-OE template Projection carries it, the checked-in `config.toml` is
   the Workspace's own and goes wherever the Workspace's files go. A Workspace
   that does not want a profile's endpoint to travel declares it in the local
   file or the local OE twin.
5. **The OE catalog's rules are untouched.** A defect in an OE file still drops
   that layer whole and logs `agent-profiles.unusable` (root ADR 0093 §4). A
   `config.toml` carries other settings, so a bad `[profiles.<name>]` table
   there is tolerated per table, shadows its name as unrunnable, and logs
   `config.unusable` (ADR 0305).

## Considered options

- **Superseding root ADR 0093:** rejected. The catalog's sharing role, shape and
  whole-layer drop all stand; only the claim that nothing else declares
  profiles changes.
- **Profiles in a mugicrew file of their own:** rejected. It is a third settings
  home with its own layering, where `config.toml` already has the two layers.
- **Moving the OE catalog's profiles into `config.toml`:** rejected. That would
  stop another Application from sharing them.
