# ADR 0302: doctor's To-fix items carry the fix, and mugicrew doctor fix applies the safe ones

Status: Accepted
Date: 2026-10-19

Chart mc-7u7g6 (grilling mc-7u7g6.4). Amends ADR 0245 (install-is-doctor-led…)
§2 and the glossary's **doctor** entry; the To-fix item's layout is ADR 0301
(doctor-prints-a-one-line-roll-call…).

## Decision

1. **ADR 0245 §2 now reads:** *Doctor's To-fix items carry the fix.* Each warn
   or FAIL row's To-fix item states its precondition as `Expected:` and its fix,
   classed **Safe** (a command `mugicrew doctor fix` runs) or **Manual** (steps
   you do yourself). A prerequisite FAIL's manual fix names the minimum version
   and this OS's install command. A missing bead graph's fix is Manual:
   `run mugicrew init`. An item whose row has an install.md section links it by
   anchor on its `More:` line.
2. **A fix is safe** only if it changes only this machine or clone (never shared
   history or a remote), needs no prompt, credential or sudo/system package
   install, is idempotent, and takes only values doctor can derive
   unambiguously. Reversibility is not required. Every other fix is manual
   (inventory: `docs/research/doctor-result-rows-preconditions-and-fix-safety.md`).
3. **Plain `mugicrew doctor` stays read-only.** `mugicrew doctor fix` is the
   separate verb that writes: it applies every safe fix without asking, lists
   the manual ones it left, re-checks, and exits 1 if any FAIL remains. It never
   touches the bead graph (Chopper's job).
4. **install.md's wording follows when `doctor fix` ships**, in the same Spec:
   line 8's comment becomes `# repeat: run mugicrew doctor fix, then each Manual
   step, until it passes`, and Core's `Each FAIL names…` becomes `Each FAIL's
   To-fix item names the minimum version and this OS's install command.`

## Considered Options

- **Supersede ADR 0245 whole.** Rejected: §1 and §3–7 are unchanged, and
  restating them only to replace §2 would fork the record.
