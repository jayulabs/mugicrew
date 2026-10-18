# ADR 0214: A numbered-sequence template compiles to a regexp

Status: Accepted
Date: 2026-10-15

Chart mc-8okw (Per-Workspace numbered-sequence clash rules). Settles prototype
mc-8okw.4. Charting already settled that a rule is a path template with one
`{n}` placeholder, that each matched directory has its own number space, and
that numbers compare by value. This ADR decides the matching mechanism and how
a renumber keeps the digit width, so the built-in ADR rule (ADR 0030,
adr-number-collision-guard) runs on the same matcher as configured sequences.
The throwaway is on `refs/mugicrew/prototypes/mc-8okw`.

## Decision

1. **Mechanism.** A template compiles to one anchored Go `regexp`, because
   `path.Match` has no `**`. The grammar:
   - `**/` means zero or more whole directories: `(?:.*/)?`.
   - `*` means any run of characters within one path segment: `[^/]*`.
   - `{n}` means one or more digits: `([0-9]+)`.
   - `{n:W}` means exactly W digits: `([0-9]{W})`.
   - Everything else is literal (`regexp.QuoteMeta`).

   A template must contain exactly one `{n}` or `{n:W}`, and it must be in the
   file name, so the number space (`path.Dir` of the match) is a concrete
   directory. Any other template is refused.
2. **The built-in ADR rule is `**/docs/adr/{n:4}-*.md`.** It gives the same
   directory and number as `adrParts` on every `TestADRNumber` case and on ten
   more edge cases (`0030-.md`, nested `docs/adr/x/docs/adr/`, `a.md.md`,
   `xdocs/adr/`, a slash after `docs/adr/`). Using `{n}` instead of `{n:4}`
   would diverge: `030-x.md`, `00030-x.md` and `1-x.md` would become ADRs and
   clash with `0030` by value. That is why fixed width exists.
3. **Value key.** The key is the digits with leading zeros stripped (`"0"` if
   all zeros). Comparing strings avoids overflow. `V1` clashes with `V001`,
   and `V0` with `V00`. A dotted Flyway version (`V1.1__x.sql`) does not match
   and is not checked.
4. **Renumber width.** A rename splices `%0*d` at the width of the renamed
   file's own matched digits over the capture (`FindStringSubmatchIndex`). The
   directory, prefix and slug are kept, and digits inside the slug are left
   alone. So `0012` becomes `0013`, `V9` becomes `V10`, `V001` becomes `V002`,
   and `V1` clashing with `V001` becomes `V3`, not `V003`. If the new path no
   longer matches its rule, the rename is refused. That only happens when a
   number outgrows a fixed `{n:W}`, such as ADR `9999` to `10000`. For the
   built-in rule, the result keeps what `adrSlug` returns.

## Considered Options

- **Width written by example (`{0000}`)**: rejected in favour of explicit
  `{n:W}`.
- **Padding to the width of the file it clashed with**: rejected. Keeping the
  renamed file's own width needs no knowledge of the other file.
