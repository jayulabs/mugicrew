# ADR 0305: An unusable `[profiles.<name>]` table in config.toml shadows its name as unrunnable

Status: Accepted
Date: 2026-10-19

Chart mc-ho5c7 (Agent Profiles declared in mugicrew's config.toml). Settles
grilling mc-ho5c7.4. Builds on root ADR 0093 (agent-profiles-live-in-a-layered-oe-root-catalog)
and ADR 0123 (an-unrunnable-agent-profile…).

mugicrew's config.toml layers may declare `[profiles.<name>]` tables. A table
can be wrong, and the two obvious answers both fail. If one bad table drops
every profile in its file (the OE catalog's rule, root ADR 0093 §4), one typo
breaks every profile there. If only the bad profile is dropped, its name falls
through to a same-name profile in a lower layer, and a different model runs
without anyone noticing, which is the failure ADR 0123 refuses.

## Decision

1. **A bad table shadows its name.** A `[profiles.<name>]` table with an
   unknown key, no `harness`, or a field of the wrong type (`model = 3`, an
   `env` that is not a table of strings, a non-table `profiles.<name>`) puts
   `<name>` at its layer's place in profile precedence as **unrunnable**. A
   launch that resolves to it is refused as ADR 0123 says (`profile.unusable`,
   `paused:profile` on AFK, a flash on HITL and resume). The file's other
   profiles, and its other settings (`concurrency`, `auto_exit`,
   `[kind_profiles]`), still apply. Fixing the table lifts `paused:profile` on
   the next tick.
2. **A non-table `profiles` contributes nothing.** If `profiles` itself is not
   a table, there is no name to shadow: that file contributes no profiles and
   logs `config.unusable {path, key: "profiles"}`.
3. **A malformed config.toml is unchanged.** A TOML syntax error still makes
   the file a missing layer (`config.unusable {path, err}`), for profiles as for
   every other setting. Failing closed would not stop a model change, since the
   same error already drops the file's `[kind_profiles]`.
4. **One event, mugicrew's.** A bad table logs
   `config.unusable {path, key: "profiles.<name>", reason}` at Warn, where
   `reason` names the defect (`unknown key modle`, `no harness`,
   `model: wrong type`). `agent-profiles.unusable` stays the OE catalog's event.
5. **The refusal names the file.** The `reason` in `profile.unusable` and in
   the TUI flash names the file and the defect, e.g.
   `declared unusable in .mugicrew/config.toml: unknown key modle`.

   > **Amended by ADR 0307 (agentprofile-reads-profile-files-and-cfglayer…) §6:**
   > the reason reads `declared unusable: <defect> (from <path>)`, e.g.
   > `declared unusable: unknown key modle (from .mugicrew/config.toml)`.
6. **Every bad table is reported, even when outranked.** It logs on every read
   whether or not it decides a resolution, and `mugicrew doctor` lists each one,
   so it can't lie in wait until a higher layer is removed.
7. **The OE catalog keeps its rule.** `agent-profiles.toml` and its local twin
   still drop a whole layer on any defect (root ADR 0093 §4). The asymmetry is
   deliberate: config.toml carries other settings and is edited per Workspace,
   so it tolerates per entry, like `[kind_profiles]` (ADR 0123 §7).

## Considered options

- **Drop every profile in the file**: rejected; one typo breaks every profile
  in it.
- **Drop only the bad profile and fall through**: rejected; it silently swaps
  the model.
- **Fail closed while any config.toml is malformed**: rejected; the mapping
  already changes with the same error, and doctor surfaces it.
- **Log through `agent-profiles.unusable`**: rejected; the file is mugicrew's
  config, read by mugicrew alone.
- **Make the OE catalog shadow per profile too**: rejected here; changing the
  OE catalog's meaning is out of this Chart's scope.
