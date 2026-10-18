# ADR 0136: Kind profiles is an inline, cursor-scrolled section of the Settings pane

Status: Accepted
Date: 2026-10-12

Chart mc-snpl (Edit the kind → Agent Profile mapping from the TUI Settings tab).
Settles prototype mc-snpl.3 (side ref `refs/mugicrew/prototypes/mc-snpl`).
Extends ADR 0109 §2, where Settings is one pane of rows, each with a gloss.

## Decision

1. **Inline, not a drill-in.** Below the existing rows there is a blank line,
   then a `Kind profiles` header and one dimmed gloss for the whole section.
   Then come one row for `default` and one row per routing kind that launches
   a session (mc-snpl.1), in alphabetical order. `j/k` move through every
   Settings row, kind rows included, so a kind row is staged and saved the way
   the AFK pool row is.
2. **Default is set apart by position and a rule.** `default` is the
   section's first row. A dimmed `────` rule separates it from the kinds. It
   is never sorted in among them.
3. **The pane scrolls with the cursor.** About 17 rows do not fit at 24 or 12
   lines. The pane keeps the selected row's lines in view. When the cursor is
   on the top rows, the pane is shown from its top. A clipped edge is replaced
   by a dimmed `↑ N more` or `↓ N more` line.
4. **Columns.** The kind name is padded to the widest kind (14 columns, for
   `chart-conflict`). After it come two spaces, the profile name, and the
   source in parentheses (`local`, `config.toml`, `default`,
   `local; shared <p>`). The value is not padded. Anything too wide for the
   pane is cut with `…`. At 40 columns this leaves 24 columns for the value,
   enough for a short profile name and a short source.
5. **The unrunnable reason wraps instead of being cut.** When a row's reason
   does not fit on its line, it moves to a second, indented line under the
   row (`  unrunnable: <reason>`). The selected row's span then covers both
   lines when the pane scrolls.

## Considered options

- **Drill-in row** (one summary row, `enter` opens the list, `esc` returns):
  rejected. It gives the list the whole pane. But it adds a second mode where
  `enter` and `esc` mean different things, and an inline section that scrolls
  already fits at 40×12.
- **Default as the section header**, with the kinds indented and an
  inheriting row dimmed: rejected. Dimming a row that is still editable reads
  as "disabled", and putting the selection on the header line blurs the
  difference between a header and a row.
