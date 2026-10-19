# ADR 0304: A profile name in several files resolves machine-local first, then mugicrew over the OE

Status: Accepted
Date: 2026-10-19

Chart mc-ho5c7 (Agent Profiles declared in mugicrew's config.toml). Settles
grilling mc-ho5c7.3. Builds on root ADR 0093 (agent-profiles-live-in-a-layered-oe-root-catalog)
and follows ADR 0122 (kind-profiles-is-a-layered-setting…) §3's spirit.

Once mugicrew's `config.toml` can declare `[profiles.<name>]`, one name can be
declared in four files. Profiles are overridden mostly for this machine's
hardware, endpoints or keys, so a machine-local table must beat any checked-in
one, whichever reader owns the file.

## Decision

1. **Order.** The first file that declares a name wins:
   1. mugicrew local, `<git-common-dir>/mugicrew/config.toml`
   2. OE local, `<git-common-dir>/agent-profiles.toml`
   3. mugicrew shared, `.mugicrew/config.toml`
   4. OE shared, `<OE root>/agent-profiles.toml`

   Local beats shared, and mugicrew beats the OE only within a tier.
2. **Whole replacement.** The winning table replaces every lower same-name
   table whole, with no field or `env` merge (root ADR 0093 §3, carried across
   all four files).
3. **Shadowing is silent.** A name declared in several files is legal and draws
   no warning, from `mugicrew doctor` or anywhere else, just as a local
   `concurrency` line silently hides the shared one. Which surfaces show the file a
   profile came from is the grilling "Which surfaces name the file a profile
   came from".

## Considered options

- **mugicrew over the OE** (mugicrew local, mugicrew shared, OE local, OE
  shared): rejected. A checked-in `.mugicrew/config.toml` would beat a machine's
  OE-local override of the same name, and the endpoint or key it carries.
- **A name in both is a collision** (unrunnable, doctor warns): rejected. A
  Workspace could not specialise a shared OE profile, and adding a profile to
  the OE catalog would break every Workspace that already declares that name.
- **A doctor note on shadowing:** rejected. Shadowing is what the layers are
  for.
