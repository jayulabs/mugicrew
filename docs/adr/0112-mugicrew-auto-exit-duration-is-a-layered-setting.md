# ADR 0112: Auto-exit's duration is a layered `auto_exit` setting, default 1m, where zero is off

Status: Accepted
Date: 2026-10-09

Completes ADR 0111 (auto-exit countdown, `0111-mugicrew-hitl-auto-exit-countdown.md`)
§2, which left the duration and the off switch open. Extends the Settings tab
(`0109-mugicrew-tui-settings-tab-and-staged-pool-size-edit.md`) with a second
row, and amends the machine-local override
(`0110-mugicrew-tui-pool-size-save-is-a-machine-local-override.md`) §1. Decided in grilling.

## Decision

1. **Key and shape.** `auto_exit = "1m"`, a Go duration string
   (`time.ParseDuration`). A bare integer (`120`), a negative value, or bad
   syntax is unusable.
2. **Layers, as the AFK pool.** The local
   `<git-common-dir>/mugicrew/config.toml` comes first, then the checked-in
   `.mugicrew/config.toml` (the Workspace's shared default), then the built-in
   default **1m**. A missing key falls through to the next layer. An unusable one
   logs `config.unusable` and falls through too.
3. **Zero is off.** `"0"` or `"0s"` turns auto-exit off: a closing line arms
   nothing, so the bar and the row show nothing new. There is no separate
   enabled flag.
4. **Read once, at arm time.** `hitl-hook` reads the setting when an arming
   `Stop` comes in and writes the deadline to `@autofinish`. A countdown that is
   already armed keeps its deadline. A change applies from the next closing
   line.
5. **A Settings row.** Below the AFK pool row:
   `Auto-exit  1m  (default)`, with a dimmed gloss under it. Its source reads
   the way the pool row's does (`local`, `config.toml`, `default`,
   `unusable → <fallback>`, and `local; shared 2m` when a local value overrides
   a checked-in one). `+`/`-` step a staged value by 30s. The floor is 0, shown
   as `off`, and there is no ceiling. Values display as `1m`, `1m30s` or `off`.
   The edit is staged and saved like the pool's (`enter` saves, `esc` discards).
6. **A save rewrites only its own key.** This amends the override ADR §1, which
   rewrites the whole file as `concurrency = N`. A save now reads the local
   file, sets its one key, and writes everything back, still atomically
   (temp file + rename). So saving the pool keeps a local `auto_exit`, and the
   reverse holds too.

## Considered Options

- **A per-user file (`~/.config`) or checked-in only:** rejected. A second home
  for settings is worse than either, and the pool's layers already give a
  shared default plus a machine-local dial.
- **Integer seconds (`auto_exit_seconds = 120`):** rejected. It is less readable,
  and a bare number is easy to misread.
- **A separate `auto_exit_enabled` flag:** rejected. That is two sources of truth
  for one behaviour.
- **Picking up changes while a countdown runs:** rejected. The deadline is the
  single value `hitl-run` watches. Re-reading the setting would give the clock
  a second input.
- **2m default:** the human chose 1m.

## Consequences

- A typo in `auto_exit` falls back to the next layer, possibly the 1m default,
  instead of turning auto-exit off. The warning and the Settings row's
  `unusable → <fallback>` source show it.
