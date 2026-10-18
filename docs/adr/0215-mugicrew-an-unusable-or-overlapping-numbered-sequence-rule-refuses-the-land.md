# ADR 0215: An unusable or overlapping numbered-sequence rule refuses the Land

Status: Accepted
Date: 2026-10-16

From Chart mc-8okw (Per-Workspace numbered-sequence clash rules), grilling
"What a malformed or overlapping numbered-sequence rule does at Land". Most
`.mugicrew/config.toml` settings warn `config.unusable` and fall through. A clash
rule is there to keep a clash off mainline, so it follows `land_strategy`
instead (ADR 0147 §3): when a rule is unusable, Land refuses. It never skips the rule.

## Decision

1. **An unusable rule refuses the Land.** A rule is unusable if its template
   has no `{n}`, if its glob does not parse, or if its `renumber` is neither
   `rename` nor `sweep`. The Land fails with an error naming the rule and the
   file.
2. **A config file that fails to parse refuses too.** If the checked-in config
   exists but does not decode, Land cannot tell whether it held rules, so it
   refuses. A missing file means "no added rules": only the built-in ADR rule
   applies.
3. **Overlap is judged on real paths.** If any added or mainline path matches
   more than one rule, the built-in ADR rule among them, the config is unusable
   and the Land refuses (1). Templates that could overlap but match no common path
   are allowed. There is no precedence order: with one, two rules with different
   `renumber` modes would make the outcome depend on file order.
4. **The refusal is a plain error.** As with `land_strategy`, the Landing stays
   open and every retry fails the same way until a human fixes the config. It is
   not a Conflict, and no Signal is filed.
5. **One reader for every path.** Land's clash check, the Chart-conflict
   post-resolve check (`CheckResolved` assertion 2) and Land's re-check after a
   Checkpoint conflict all read rules through the same reader. An unusable config
   therefore fails a conflict session's check as well, so no path accepts a
   resolution that Land would refuse.
6. **`mugicrew doctor` checks the rules too, in addition to Land.** It reports
   a fail for each unusable rule, and for each overlap among the files in the
   tree. Land's refusal remains the guarantee.

## Considered Options

- **Warn and skip the rule** (as other config keys do): a clash in that
  sequence would reach mainline unnoticed, which defeats the rule.
- **First match wins on overlap**: the outcome would depend on file order when
  the rules' `renumber` modes differ.
- **Refuse whenever two templates could overlap**: needs glob intersection, which
  is hard to get right. Overlap on actual paths falls out of the matcher.
- **File a Signal on refusal**: it would need deduplication across retries. This
  is a config fix for a human, and `doctor` catches it earlier.
