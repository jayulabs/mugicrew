# ADR 0139: A Kind profiles save of (unset) deletes the local key, and is the reset

Status: Accepted
Date: 2026-10-12

Spec mc-w2ob (Kind profiles section on the TUI Settings tab), FR33 and FR38.
Amends ADR 0110 §6 for `[kind_profiles]` only.

## Decision

1. **Saving `(unset)` on a Kind profiles row deletes that kind's key from the
   local `[kind_profiles]` table**, and drops the table when it becomes empty.
   The kind then falls back along ADR 0122 §3 (shared key, then `default`, then
   the built-in). This is the reset ADR 0110 §6 left out, for this setting only.
   The AFK pool and Auto-exit keep "no reset key".
2. **A kind-profile save touches only its own key.** Every other local key
   survives, unknown keys included. An inline `kind_profiles = {…}` is written
   back as a `[kind_profiles]` table. Comments and formatting are not kept, as
   with every local save.
3. **A local file that doesn't parse refuses the save** and is left untouched;
   the TUI reports `save failed: <err>`. The pool and Auto-exit saves still
   replace an unparseable file, as before.
4. **A key that is neither a registry kind nor `default` is refused at save
   time**, so the TUI never writes a key that warns `config.unusable` on every
   tick.

## Why

A mapping has many keys, so "delete the local file by hand" would throw away
every other override to reset one. Deleting one key is the natural inverse of
setting it, and the staged-unset text (ADR 0137 §3) already shows where the
kind will land. Refusing a malformed file matters more here than for a scalar:
a set or delete of one key must not silently discard the human's other local
settings.

## Considered Options

- *A separate reset key for every Settings row:* rejected. Pool and Auto-exit
  have one value each and a visible `shared N` gloss; a reset key there buys
  little and costs a key and a `?` line.
- *Write an empty string for (unset):* rejected. A set key stops the ADR 0122 §3
  chain, so `""` would shadow the shared key and `default` instead of falling
  back to them.
