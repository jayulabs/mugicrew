# ADR 0123: An unrunnable Agent Profile refuses the launch, never falls back

Status: Accepted
Date: 2026-10-10

Chart mc-8u0o (Per-kind Agent Profiles from an OE-level catalog). Settles
grilling mc-8u0o.5. Builds on the OE catalog (root ADR 0093) and the
`[kind_profiles]` mapping (ADR 0122), and amends both.

A bead's profile comes from the mapping, and the mapping names a profile in the
catalog. Either side can be wrong. Running some other model instead changes
cost, capability or endpoint without anyone noticing, so mugicrew refuses to
launch. This is unlike `concurrency` and `auto_exit`, where an unusable value
falls through to the next layer: a wrong pool size does no harm, but a wrong
model does.

## Decision

1. **Unrunnable profiles.** The resolved profile is unrunnable if any of these
   holds. Each is checked at resolve time, before anything runs:
   - it is absent from the catalog. That includes an empty catalog, a missing
     file, or a layer dropped as unusable;

     > **Amended by ADR 0308 (the-settings-cycle-is-every-declared-profile-name…):**
     > a name no profile file declares reads `not declared`, not "absent from
     > the catalog".

   - its `harness` is not one mugicrew supports, or that harness's binary is
     not on `PATH`;
   - an `env` value names a `${VAR}` that is **unset**. A variable that is set
     but empty still expands to empty, so `VAR=` blanks it on purpose.
2. **Refuse, don't fall back.** An unrunnable profile never falls through to
   the next layer, to `default` or to the built-in. The built-in runs only when
   the mapping names nothing for the bead (ADR 0122 §4 as amended).
3. **AFK: `paused:profile`.** Resolution runs before the claim and the worktree
   cut. An unrunnable profile pauses the bead at once with the new cause
   `profile`, and no effort runs. Each tick, the loop lifts `paused:profile` off
   any bead whose kind now resolves to a runnable profile, the same way it lifts
   `foreign-hold`. Fixing the config is the whole fix.
4. **HITL launch: refuse and flash.** The launch does not happen. The TUI
   flashes the profile and the reason. The bead is neither claimed nor paused.
5. **Resume: refuse and flash.** If the recorded `@profile` is unrunnable, or if
   the session records a harness that differs from the profile's current one,
   the resume is refused. The session record stays, so resuming works once the
   catalog is fixed. The mapping is never consulted as a fallback, because a
   transcript belongs to the harness and model that wrote it. Until the session
   records its harness (grilling "How mugicrew holds several harnesses behind
   the Harness seam"), the harness check does nothing.
6. **One event.** Every refusal logs `profile.unusable` at Warn
   {bead, kind, profile, reason, launch=afk|hitl|resume}. On AFK it comes
   alongside `effort.paused {cause: profile}`. There is no `effort.failed`,
   because no effort ran.
7. **Typo'd mapping keys.** A `[kind_profiles]` key that is neither a routing
   kind nor `default` logs `config.unusable` {path, key}, and only that key is
   ignored. The rest of the table still applies, and the kind it misspelled
   falls through to the next layer as usual.
8. **Run-time rejection is an ordinary failure.** If the endpoint rejects the
   model or the credential after the harness has started, mugicrew does not try
   to recognise it. AFK logs `session.failed` and counts toward
   `paused:failing`. In HITL the human sees the error in the pane.

## Amendments

- ADR 0309 (config-toml-layers-may-declare-profiles-beside-the-oe-catalog…), with
  ADR 0305 (an-unusable-config-toml-profile-table…), ADR 0306
  (doctor-and-refusal-reasons-name-the-file…) and ADR 0307
  (agentprofile-reads-profile-files-and-cfglayer…) §6: §1 gains a cause, a
  `[profiles.<name>]` table in a `config.toml` that is bad, which reads
  `declared unusable: <defect>`. Every reason for a name that was found ends
  `(from <path>)`, the winning file. §6's `profile.unusable` gains `path` beside
  `reason`, omitted when the name is not declared.

- Root ADR 0093 §2: an unset `${VAR}` makes the profile unrunnable instead of
  expanding to empty. The unset case matters because an empty
  `ANTHROPIC_AUTH_TOKEN` beside a third-party `ANTHROPIC_BASE_URL` risks the
  harness sending its own stored credential to that endpoint.
- Root ADR 0093 §6 and ADR 0122 §4: the built-in runs when **nothing is
  mapped**. A mapped name with no catalog behind it is refused.

## Considered options

- **Fall back along the chain with a warning** (like `concurrency`): rejected.
  It silently swaps the model, which is the failure ADR 0093 §4 exists to
  prevent.
- **AFK: count it toward `paused:failing`:** rejected. That pause needs a human
  to lift it bead by bead, even after the config is fixed.
- **AFK: skip the bead each tick with a deduped warning:** rejected. The bead
  sits on the frontier, and the TUI shows nothing against it.
- **Resume through the mapping when `@profile` is gone:** rejected. It switches
  model or harness in the middle of a transcript.
- **Mark the whole `[kind_profiles]` table unusable on a typo'd key:**
  rejected. It breaks per-key layering, so one typo knocks out every line.
- **Parse harness output to class run-time rejections as profile failures:**
  rejected. The cost buys nothing, since the fix is a catalog edit either way.
