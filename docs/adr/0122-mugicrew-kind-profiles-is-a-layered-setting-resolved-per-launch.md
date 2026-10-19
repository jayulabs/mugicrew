# ADR 0122: The kind → Agent Profile mapping is a layered `[kind_profiles]` setting, resolved at every launch

Status: Accepted
Date: 2026-10-10

Chart mc-8u0o (Per-kind Agent Profiles from an OE-level catalog). Settles
grilling mc-8u0o.3. Builds on the OE catalog (root ADR 0093) and mugicrew's
layered settings (ADR 0110, ADR 0112).

The OE declares its Agent Profiles, and mugicrew decides which bead kind runs on
which one. That mapping is a mugicrew setting, so it lives where every other
mugicrew setting lives.

## Decision

1. **Home and shape.** A `[kind_profiles]` table in mugicrew's `config.toml`.
   Its keys are routing kinds spelled as the registry spells them, plus
   `default`. Its values are profile names from the OE catalog:

   ```toml
   [kind_profiles]
   default  = "opus"
   research = "sonnet"
   build    = "deepseek"
   ```

   The table is not called `[profiles]`, so it can't be mistaken for the
   catalog's `[profiles.<name>]`.

   > **Amended by ADR 0309 (config-toml-layers-may-declare-profiles-beside-the-oe-catalog…):**
   > profile names come from any declared profile, not the OE catalog alone:
   > mugicrew's `config.toml` layers may declare `[profiles.<name>]` too (ADR 0304
   > (a-profile-name-in-several-files…)). The "not called `[profiles]`"
   > rationale no longer holds, since `[kind_profiles]` and `[profiles.<name>]`
   > now share a file; the name stays, as it is the mapping and not the
   > profiles.
2. **Layers per key.** Each key falls through on its own, the way `concurrency`
   and `auto_exit` do. The local `<git-common-dir>/mugicrew/config.toml` comes
   first, then the checked-in `.mugicrew/config.toml`. A local `research` line
   does not hide a checked-in `build` line.
3. **Kind beats default across layers.** A bead's profile is the first one set
   in this order: local `<kind>`, shared `<kind>`, local `default`, shared
   `default`, then the built-in. The line that names the kind wins wherever it
   lives. A machine that wants to override everything writes those kinds
   locally.
4. **The built-in has no name.** When nothing is mapped, or the catalog has no
   profiles, mugicrew runs claudecode with no `--model`, as before. No reserved
   profile name stands for this, so no catalog profile can collide with one.

   > **Amended by ADR 0123 (`mc-8u0o.5`):** the built-in runs only when nothing
   > is mapped; a mapped name absent from the catalog (even an empty one) is
   > refused, never run on the built-in.
5. **Resolved at every launch, never cached.** Each AFK `Run` and each HITL
   launch re-reads the mapping and the catalog.
6. **Resume reuses the recorded name.** A HITL launch records the resolved
   profile's name on its session: tmux `@profile`, and an attr on
   `hitl.launched`. Resume looks that name up in the catalog again and does not
   consult the mapping. A catalog edit to that profile's model or env therefore
   applies on resume. Claude Code takes `--model` per launch, so this is legal.
7. **No Settings tab row.** Edit the mapping and the catalog by hand. A Settings
   surface is later work.

## Considered options

- **A file of its own** (`.mugicrew/kind-profiles.toml` plus a local twin):
  rejected. It is a second settings home with a second layering.
- **The mapping inside the OE catalog:** rejected at triage. The catalog is the
  OE's, and kinds are mugicrew's vocabulary.
- **A local `[kind_profiles]` that replaces the shared table whole:** rejected.
  It breaks the per-key layering every other mugicrew setting has.
- **Layer first** (local `<kind>`, local `default`, then shared): rejected. A
  local `default` would silently override every shared kind line.
- **A reserved `builtin` profile name:** rejected. It is a second way to say
  "nothing configured".
- **Snapshotting harness + model on the session for resume:** rejected. The
  catalog stays the single source of truth. Env would have to be looked up by
  name anyway, because recording expanded secrets in tmux options leaks them.

## Consequences

- A profile whose harness changes, or that disappears, between launch and resume
  breaks the resume. With one harness this only bites on a removed profile.
  How that and every other unusable selection fails is left to the grilling
  "How an unusable Agent Profile selection fails".
