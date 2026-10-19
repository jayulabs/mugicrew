# ADR 0307: agentprofile reads profile files and cfglayer orders the four

Status: Accepted
Date: 2026-10-19

Chart mc-ho5c7 (Agent Profiles declared in mugicrew's config.toml). Settles
grilling mc-ho5c7.6. Builds on ADR 0304 (a-profile-name-in-several-files…),
ADR 0305 (an-unusable-config-toml-profile-table…) and ADR 0306
(doctor-and-refusal-reasons-name-the-file…).

A profile name can now come from four files, and `resolveRecorded`
(profile.go), `mugicrew doctor` and the TUI Settings tab must all see the same
merged catalog, including each name's winning file and whether a bad table
shadows it. `agentprofile` must stay mugicrew-agnostic so it can lift into
`libs/harness`. So the read is split: `agentprofile` knows how to read a
profile file, and only mugicrew knows which files to read and in what order.

## Decision

1. **agentprofile reads and merges, path-agnostic.** It owns the profile-table
   schema, a strict whole-file reader (the OE catalog's rule, root ADR 0093
   §4), a per-table lenient reader that reads `[profiles.*]` from any TOML file,
   ignores every other key and returns a per-name error rather than dropping the
   file, the `$OE_ROOT`-else-root rule for the two OE paths, and a first-wins
   merge. Its readers return errors and never log. `agentprofile.Load` goes or
   shrinks to the OE-only pair.
2. **cfglayer.Profiles(root) orders the four files.** It reads them in ADR 0304
   (a-profile-name-in-several-files…) order, re-reads every call with no cache,
   and logs both events: `config.unusable` for a bad config.toml table and
   `agent-profiles.unusable` (unchanged name and fields) for a dropped OE layer.
   One read logs once.
3. **One entry per name.** The catalog maps each name to
   `agentprofile.Entry{Profile, Path, Err}`: `Path` is the winning file,
   absolute; a non-nil `Err` is a bad table shadowing the name as unrunnable.
4. **Outranked defects travel with it.** `cfglayer.Profiles` returns
   `Catalog{Winners, Unusable}`. `Unusable` lists every bad table in every
   file, outranked or not, and each OE layer dropped whole (no name, the
   file's error), so doctor reports all four files alike (ADR 0305
   (an-unusable-config-toml-profile-table…) §6).
5. **One reason builder.** main's `checkProfile(root, Entry)` builds every
   unrunnable reason and makes the path Workspace-relative.
   `ProfileReasons` (main.go, tui's `profiled`) takes `map[string]Entry`;
   `profile.unusable` takes `path` from the Entry, not from the reason text.
6. **A bad table's reason ends with its file.** It reads
   `declared unusable: <defect> (from <path>)`, the uniform shape of ADR 0306
   §3. This amends the example in ADR 0305 (an-unusable-config-toml-profile-table…) §5.
7. **Settings reads cfglayer directly.** tui's `readKindProfiles` calls
   `cfglayer.Profiles(root)` as it calls `loop.KindProfiles`, not through its
   ctx; `kindProfiles.catalogPath` goes, since ADR 0306 §5 fixes the flash text.

## Considered options

- **All four files in agentprofile:** rejected; it would have to know
  `.mugicrew/config.toml` and stop being liftable.
- **The mugicrew half in loop:** rejected; profiles are config, not a loop
  setting, and cfglayer already owns the config.toml paths and their event
  (as ADR 0157 placed contexts).
- **The mugicrew half in main, tui asking via ctx:** rejected; it routes a
  plain config read through the Ctx seam.
- **Parallel maps (profiles plus bad-name reasons):** rejected; one name has one
  winner, and two maps can drift.
- **Doctor re-reading each file, or reading the logs:** rejected; a second parse
  path, and doctor can't see logs.
