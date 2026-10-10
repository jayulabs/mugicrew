# ADR 0109: mugicrew TUI Settings is a `*Settings` tab, and its pool-size edit is staged and saved with `enter`

Status: Accepted
Date: 2026-10-09

Extends ADR 0058 §2 / ADR 0072 §3 (accent-key labels, 40-col shrink), following
the `#Stats` precedent, and applies ADR 0098 / ADR 0101 to the new page. Part of
the TUI Settings page Chart. How a save is
persisted, and what save success and failure look like, are left to a later decision.

## Decision

1. **Settings is a tab, not a body view like `?`.** It is the last tab, after
   `#Stats`. Its label is `*Settings`, with `*` (its jump key) in the accent
   colour, the same way `#Stats` shows `#`. When it is not the current tab it
   shrinks to `*` under the ADR 0072 §3 fallback.
2. **One full-width pane of setting rows.** There is no list/detail split, and
   `j/k` selects a row. Today the only row is the AFK pool:
   `AFK pool  3   (default)   2 in flight`. The source is `config.toml`,
   `default`, or `unusable → default 3` (Pool's warning case). Under the row is a
   dimmed one-line gloss. A later setting adds a row and changes no layout.
3. **`+`/`-` step a staged value. `enter` saves it and `esc` discards it.** The
   floor is 1, so `-` at 1 does nothing. There is no ceiling, mirroring
   `loop.Pool`. The row shows `3 → 5 (unsaved)`. Saving is the only write, so
   the loop, which re-reads every tick, never sees the values in between.
4. **A staged value survives tab switches.** The label shows `*Settings•`,
   where filtered tabs show `▼`. `esc` on the tab discards the staged value.
5. **The values come from the refresh, not from render.** The value, source and
   in-flight count are read in the refresh Cmd, cached on the Model and
   rendered from the cache (ADR 0101). If the file changes while a value is
   staged, the staged value stays and the row shows the new base:
   `5 → 4 (unsaved; file now 6)`.
6. **The save is a press-time Cmd (ADR 0098).** The pending guard is keyed by
   the setting name. A second `enter` while a save is in flight is refused with
   a status line. `enter` with nothing staged, or with the staged value equal to
   the current one, does nothing.
7. **Hints.** The footer shows `+/- change`, and adds `enter save · esc discard`
   only while a value is staged. `?`/`q` always stay (ADR 0058 §5). `?` lists
   every key the tab handles. On other tabs, `*` appears only in `?`, like the
   other jump keys.

## Considered Options

- *A view that replaces the body, like `?`.* It would have cost nothing in the
  tab bar. Rejected: the human wanted Settings to be a place in the bar. With
  `#Stats` as precedent, a symbol key can be the label's accent first letter.
- *Text entry.* Rejected: `+`/`-` already means "move a number" here, and steps
  have no parse-error path.
- *Write on every press.* Rejected: the loop would act on each value in between,
  and if a save becomes a commit, every step would be one.
