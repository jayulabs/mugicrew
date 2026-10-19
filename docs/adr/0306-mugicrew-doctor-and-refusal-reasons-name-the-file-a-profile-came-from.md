# ADR 0306: Doctor and refusal reasons name the file a profile came from

Status: Accepted
Date: 2026-10-19

Chart mc-ho5c7 (Agent Profiles declared in mugicrew's config.toml). Settles
grilling mc-ho5c7.5. Builds on ADR 0304 (a-profile-name-in-several-files…),
ADR 0305 (an-unusable-config-toml-profile-table…) and ADR 0123
(an-unrunnable-agent-profile…).

A profile name can now come from four files, and shadowing is silent (ADR 0304
(a-profile-name-in-several-files…) §3). So a human fixing a profile needs to be
told which file won. Provenance goes where a fix starts, which is doctor and
the refusal reason. It stays off rows that are about something else.

## Decision

1. **The path, not a layer word.** Provenance is shown as the file's path:
   Workspace-relative when under the root (`.mugicrew/config.toml`), otherwise
   absolute. No surface shows `mugicrew local`, `OE shared` and so on.
2. **Doctor lists every declared profile.** `mugicrew doctor` prints one line
   per declared name: OK when runnable, WARN when not, each naming the file
   whose table won. A mapped name absent from every file names no file. With no
   profiles declared anywhere, doctor prints `agent profiles  none declared`,
   listing the four files it read. Bad tables keep their own lines (ADR 0305
   (an-unusable-config-toml-profile-table…) §6).
3. **Every unrunnable reason names the winning file.** Any reason for a name
   that was found (unsupported harness, binary not on PATH, unset `${VAR}`, a
   bad table) carries the file, e.g. `codex not on PATH (from
   .mugicrew/config.toml)`. The reason is built in one place, so the HITL
   launch and resume flashes, doctor's WARN and the Settings unrunnable reason
   all get it. `absent from the catalog` has no file.

   > **Amended by ADR 0308 (the-settings-cycle-is-every-declared-profile-name…):**
   > the reason reads `not declared`, still with no file.
4. **`profile.unusable` gains `path`.** The refusal event carries
   `path` (the winning file) beside `reason`, the same key `config.unusable`
   uses. It is omitted when the name is absent.
5. **The empty-catalog flash names both file kinds.** The Settings `+`/`-`
   flash on an empty catalog reads
   `no profiles declared in config.toml or agent-profiles.toml`, no longer the
   OE catalog's path alone.
6. **Elsewhere, no provenance.** The Settings Kind-profiles row keeps showing
   only the *mapping's* source, since a second source in its parenthesis would
   blur the two. `session.start` and the Stats profile grouping record what
   ran, not where it was configured.

## Considered options

- **Layer words** (`mugicrew local` …): rejected; one more term to learn, and the
  path already carries it.
- **Path and layer word together:** rejected; too long for a flash.
- **Doctor warnings only, or one summary line:** rejected; doctor is the one place to
  ask which file holds a name.
- **The profile's file on the Kind-profiles row:** rejected; the row is tight and
  already shows a source.
- **List all four paths in the empty-catalog flash:** rejected; too long for the
  flash line, and doctor lists them.
