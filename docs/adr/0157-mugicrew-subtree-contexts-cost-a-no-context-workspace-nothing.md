# Subtree contexts cost a no-context Workspace nothing

Status: accepted

A Workspace with no `[context.*]` table must see no new prompt bytes, no new bd
reads, no new labels and no new config keys. The throwaway on
`refs/mugicrew/prototypes/mc-3hri.2` (prototype "Throwaway the no-context path of
subtree contexts and measure its bloat") wired the minimum: the config read, the
focus line, label copy on succession and Luffy's conditional question. It
golden-diffed the prompts with and without a table. Three rules keep the cost at
zero, and they are not obvious from the code.

- **Context text is appended in Go, never written into a template.** The focus
  line (`FocusLine`) and Luffy's context question are appended to a rendered
  prompt only when the input calls for them: the container carries a `context:`
  label, or contexts are declared. The embedded `.md` templates and the crew docs
  gain no lines, so a no-context prompt is byte-identical. The prototype measured
  the triage prompt at 7530 bytes either way, plus 176 bytes only when contexts
  are declared, and the speccing prompt at 2651 bytes, plus 163 bytes only when
  the Chart is labelled. A byte-identity test per changed builder pins this,
  because no existing test compares prompt bytes.
- **A launch gates on the declared contexts before reading any container.**
  Builders that don't have the container in hand (ten of them) first read config
  and skip the `bd show` when no contexts are declared. The config gate costs
  about 1.2 ms (2 file reads and 1 `git rev-parse`, the same as an existing
  `PoolSetting` read). An ungated `bd show` costs about 143 ms, so gating is
  about 120 times cheaper.
- **The contexts reader lives in a leaf package, not in `loop`.** A crewmate that
  imports `loop` creates an import cycle through `session`'s tests (luffy → loop →
  dispatch → session). So `Contexts`/`FocusLine` sit in a small package that
  `loop` and every crewmate can import, and the layer readers (`decodeLayer`,
  `resolve`) move there or into a shared config leaf.

Label copy is one conditional in `bd.labelArgs`, so a no-context create's argv is
unchanged. Each copy site is one assignment from a bead it has already loaded.
The prototype's footprint on no-context paths: 6 files, +106/−5 lines, and none
in `git/`, `dispatch`, `route`, `jinbe`, `usopp` or the TUI. The existing suite
passes unchanged. The per-tick cost is zero, because nothing new runs on the tick
until a fetched bead carries `context:` (the hold resolves config lazily).
