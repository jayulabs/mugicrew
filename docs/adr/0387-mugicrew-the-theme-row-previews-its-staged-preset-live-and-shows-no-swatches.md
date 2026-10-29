# ADR 0387: The Theme Settings row previews its staged preset live and draws no swatches beside it

Status: Accepted
Date: 2026-10-28

Chart Named color themes for the mugicrew TUI (mc-0f831) needed to settle what the Theme
row in the Settings tab shows. The human decided:

1. **`+/-` previews.** Every other Settings row stages an edit that changes nothing until
   `enter` (ADR 0109 (mugicrew-tui-settings-tab-and-staged-pool-size-edit)). Mouse mode
   switches capture only on save. The Theme row is the exception: each `+/-` step repaints
   the whole TUI in the staged preset, because the only way to judge a theme is the real
   screen. `esc` repaints the saved theme, and `enter` saves a machine-local override. The
   cycle is `dark → light → high-contrast → (unset)`. Previewing `(unset)` paints what the
   file resolves to without the local key: the shared value, or dark.
2. **A preview outlives the tab but not the process.** A staged theme stays painted across
   tab switches, as staged edits stay staged. Quitting discards it, so the next launch
   paints the saved theme. A refresh that moves the file under a staged theme shows the
   usual `(unsaved; file now …)` note, and the preview keeps the staged preset.
3. **Nothing beside the name.** The row reads `Theme  <preset>  (<source>)`, like every
   other row. Rejected: a strip of the current preset's colors, which only repeats what the
   live preview already shows; a strip of every preset in its own colors, which shows the
   light preset's colors on a dark terminal, where they look wrong.
4. **An unknown value names itself and its layer.** `theme = "solarized"` reads
   `dark  (config.toml: unknown "solarized" → dark)`, the `unknown …` part in the warn
   color. This is not the `unusable` wording, which means a file that does not parse. An
   unknown value in any layer falls back to dark, not to the next layer down.
5. **The gloss:** `light expects a light terminal background; +/- previews live, esc
   reverts; enter saves a machine-local override`. It states the one fact the preview can't
   show: light assumes a light terminal, since mugicrew never paints the background.
