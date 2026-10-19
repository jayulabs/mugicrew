# ADR 0301: doctor prints a one-line roll call, then a numbered To-fix list shared with init

Status: Accepted
Date: 2026-10-19

Chart mc-7u7g6 (prototype mc-7u7g6.3). Amends ADR 0244 (init-refuses-a-clone-that-tracks-beads…) on how the shared fix is printed.

## Decision

1. **Roll call first.** `mugicrew doctor` prints every result row as one line,
   `<verdict>  <name>: <detail>`, flat and in check order, with no section headings.
2. **Then "To fix".** After the roll call comes a `To fix` heading and one
   numbered item per warn or FAIL row, in roll-call order:
   - `N. <verdict> <name>`
   - `Expected: <precondition>.`
   - a safe fix as `Safe — doctor fix runs:` and the bare command on its own
     indented line; a manual fix as `Manual — do it yourself:` and one `•`
     bullet per step. A long fix (e.g. the tracked-beads fix) is split into steps,
     never one long line; lines wrap at the terminal width with a hanging indent.
   - `More: <docs/install.md anchor>` when the row has one.
   OK and skip rows never get an item.
3. **Summary last:** `<n> FAIL, <n> warn, <n> ok`, plus
   `· <n> safe fix: run mugicrew doctor fix` when any safe fix exists; on a clean
   run `all <n> checks ok`.
4. **Colour only on a TTY:** the verdict word (ok green, warn yellow, FAIL bold
   red), safe commands cyan, headings bold, `More:` dim. Piped output is plain.
5. **`mugicrew doctor fix`** prints `Applying safe fixes` (each `applied` or
   `failed` with the command and the tool's own output line; a failed safe fix
   names its manual fallback), then `Left for you (manual)` as roll-call lines,
   then `Re-check`: the full doctor output from 1–3. `doctor fix` dispatches
   before the event log opens and writes no event (ADR 0033
   (event-log-is-history-not-a-view) §1; ADR 0029 (event-schema…)'s 2026-10-04
   amendment).
6. **`mugicrew init`'s tracked-beads refusal reuses the same renderer**: the
   row's roll-call line and its To-fix item, so doctor and init cannot drift
   (ADR 0244's "same check with the same fix").

## Considered Options

- Sections (Core tools / Workspace / GitLab / Jira / Environment) with the fix
  block under each row: rejected, the fixes are scattered among OK lines.
- Flat with glyph sub-lines under each row (closest to today): rejected, a long
  manual fix still interrupts the roll call.
