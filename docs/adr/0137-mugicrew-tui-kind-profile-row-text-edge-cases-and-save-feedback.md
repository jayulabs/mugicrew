# ADR 0137: What a Kind profiles row says, staged and in force, and its save feedback

Status: Accepted
Date: 2026-10-12

Chart mc-snpl (Edit the kind → Agent Profile mapping from the TUI Settings tab).
Settles grilling mc-snpl.4. Fills in the rows of ADR 0136's Kind profiles
section. Applies ADR 0109 §5 (file changes under a staged row) and ADR 0110 §4–5
(source gloss, save feedback) to the mapping (ADR 0122), and ADR 0123 §1
(unrunnable profiles). Examples assume the 24-column value budget at 40 columns.

## Decision

1. **In force: `<kind>  <effective profile>  (<source>)`.** The source is
   `local` or `config.toml` when the kind's own key sets it. It is `default` when
   the kind inherits from the `default` row. Inside this section, `default` names
   that row, which shows its own layer just above. When nothing is mapped, the
   built-in runs and the row reads `approval  (built-in)`, with no name and no
   source. It never reads `default`, because on the pool and Auto-exit rows
   `default` means the built-in value. The `default` row follows the same rules:
   `default  opus  (local; shared sonnet)`, or `default  (built-in)`.
2. **The shared gloss is the same key only.** As in ADR 0110 §4,
   `local; shared <p>` shows the checked-in value of the kind's own key, and only
   when that key is set. It does not show the fallback chain.
3. **A staged row drops the source paren.** It reads `opus → sonnet (unsaved)`.
   A staged unset shows where the kind will land: `opus → unset: sonnet
   (unsaved)`, or `unset: (built-in)` when the kind falls back to the built-in.
4. **The cycle steps from the local key.** `+`/`-` move through the sorted
   catalog names plus `(unset)`, starting from the kind's local key, which is
   `(unset)` when the kind has none. "Staged equals current, so `enter` does
   nothing" compares against that local key. The `from` side of the staged text
   is the effective name.
5. **The unrunnable marker speaks for what the row would launch on.** In force,
   it names the in-force profile: `review  codex-gpt5  (local)  unrunnable:
   harness codex not on PATH`, in the warn colour, wrapping when narrow
   (ADR 0136 §5). While a value is staged, the marker names what the kind would
   launch on after the save: the staged name, or for unset the fallback name.
   The row never shows two reasons.
6. **A name missing from the catalog, and an empty catalog.** An in-force name
   that is absent from the catalog reads `unrunnable: not in catalog`. From it,
   `+` goes to the first name and `-` to the last slot. An empty or unusable
   catalog leaves only `(unset)` in the cycle. If the row has a local key,
   `+`/`-` stage unset. If it has none, they flash `no profiles in <catalog
   path>`.

   > **Amended by ADR 0306 (doctor-and-refusal-reasons-name-the-file…) §5 and
   > ADR 0308 (the-settings-cycle-is-every-declared-profile-name…) §4:** the
   > flash reads `no profiles declared in config.toml or agent-profiles.toml`,
   > and a name no file declares reads `unrunnable: not declared`, not `not in
   > catalog`. A name shadowed by a bad table is declared and shows its
   > `declared unusable: <defect> (from <path>)` reason.
7. **A file change under a staged row** compares the row's effective value, so
   an edit to the shared key or to `default` also counts. It reads as in ADR 0109
   §5: `opus → sonnet (unsaved; file now haiku)`, cut with `…` when too wide.
8. **Save feedback.** Success: `Kind profile review codex-gpt5 saved (this
   machine)`. Unset: `Kind profile review unset saved (this machine; now
   sonnet)`. Failure: `Kind profile review save failed: <err>`, and the staged
   value is kept so `enter` can retry (ADR 0110 §5). Saving an unrunnable
   profile adds nothing to the success line, because the row already marks it.

## Considered options

- **`(default; local)` for an inherited row:** rejected. The `default` row
  directly above already shows its layer, and the 24-column budget is tight.
- **`claudecode (built-in)` for the built-in:** rejected. The built-in has no
  profile name (ADR 0122 §4), and showing one would read like a catalog entry.
- **Glossing the fallback (what unset would give) beside every local
  override:** rejected. It changes ADR 0110 §4's meaning. The staged-unset
  text shows the fallback at the moment it matters.
- **Stepping the cycle from the effective name:** rejected. On an inherited
  row, the first `+` would skip a name, and "equal to current" would no
  longer match what the save writes.
- **A marker for both the in-force and the staged profile:** rejected. That
  puts two reasons on one row.
- **A missing name stepping to its alphabetical neighbours:** rejected. It
  adds logic for a rare case and buys almost nothing.
