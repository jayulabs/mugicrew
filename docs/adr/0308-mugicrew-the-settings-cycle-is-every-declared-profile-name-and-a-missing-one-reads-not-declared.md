# ADR 0308: The Settings cycle is every declared profile name, and a missing name reads `not declared`

Status: Accepted
Date: 2026-10-19

Chart mc-ho5c7 (Agent Profiles declared in mugicrew's config.toml). Settles
grilling mc-ho5c7.7. Builds on ADR 0304 (a-profile-name-in-several-files…),
ADR 0305 (an-unusable-config-toml-profile-table…) and ADR 0306
(doctor-and-refusal-reasons-name-the-file…). Applies them to the Kind profiles
section (ADR 0136, ADR 0137, ADR 0139). Amends ADR 0123
(an-unrunnable-agent-profile…) §1 and ADR 0306 §3 for the wording of a missing
name.

Profile names now come from four files, and some of them may be bad tables
shadowing their name as unrunnable. The Settings `+`/`-` cycle and enter's save
were written for the OE catalog alone.

## Decision

1. **The cycle is every declared name.** The `+`/`-` slots are every name that
   any of the four files declares, plus `(unset)`, including a name shadowed as
   unrunnable by a bad table. Staging one shows ADR 0137 §5's marker for the
   staged target, e.g. `unrunnable: declared unusable in .mugicrew/config.toml:
   unknown key modle`. The cycle matches doctor's list and what a launch
   resolves, and a name visible on a row can always be cycled back to.
2. **One alphabetical list.** The names are sorted together, with no grouping by
   file. Settings rows carry no profile provenance (ADR 0306 §6), so grouping
   would be an order with no visible reason.
3. **The empty-catalog flash needs no names at all.** ADR 0306 §5's
   `no profiles declared in config.toml or agent-profiles.toml` fires only when
   the merged set is empty. A file holding only bad tables still gives slots.
4. **A missing name reads `not declared`.** A mapped name that no file declares
   reads `unrunnable: not declared` on its row. The same words replace
   `absent from the catalog` in ADR 0123 §1's refusal reason and in ADR 0306
   §3, which still carry no file. A bad-table name is declared, so it never
   reads `not declared`: it shows its bad-table reason with `(from <path>)`.
   *Catalog* stays the word for the OE's `agent-profiles.toml` and its local
   twin, and is not used for the merged four-file set.
5. **Enter's save is unchanged.** It writes only the name into the local
   `[kind_profiles]`, whichever file holds that profile. The success flash names
   no file (ADR 0306 §6). A bad target is already marked on the row.
6. **The save keeps profile tables as data, not as text.** The local
   `config.toml` may now hold hand-written `[profiles.*]` tables. A Kind
   profiles save rewrites the whole file, as ADR 0139 §2 says. Those tables,
   bad ones included, survive with their values, but their comments and
   layout do not.

## Considered options

- **Only usable names in the cycle:** rejected. An in-force bad-table name
  would behave like a missing one and could not be cycled back to, and the
  cycle would disagree with doctor.
- **Grouping the cycle by file:** rejected; see §2.
- **A comment-preserving edit of `[kind_profiles]`:** rejected for this Chart.
  It needs a format-preserving TOML editor, a bigger change than this Chart.
- **Refusing or warning on a save when the local file holds profile tables:**
  rejected. It punishes the normal case.
- **Keeping "catalog" for the merged set:** rejected. It suggests the OE file
  alone, which keeps its meaning.
