# glamour's binary-size cost under CGO_ENABLED=0

Status: Research (input to the Idea tab for the v2 TUI Chart and its Phase 6 Polish glamour decision, mc-lu2x.5)
Date: 2026-10-12
Bead: mc-lu2x.5.2

## Purpose

Phase 6 (Polish) plans glamour Markdown rendering for v2. This phase leaves it
out. Before Phase 6 commits to it, this doc measures what
`github.com/charmbracelet/glamour` adds to the `CGO_ENABLED=0` mugicrew binary.
It compares the result with the 16,014,194 B baseline that ADR 0141 §9 recorded
when bubbles came in (+276,740 B, +1.7%). It also lists what glamour pulls in
and what could shrink it. Everything was built in a scratch copy, so go.mod is
unchanged on every branch.

## 1. Method

- Source: `git archive HEAD` of `apps/mugicrew` at `35f14dc`, unpacked to a
  scratch dir. No branch's go.mod was touched.
- Toolchain: `go version go1.26.7 linux/amd64`
  (`/nix/store/4l04h8656as4i291mpcg38wz9m4k8acp-go-1.26.7/bin/go`).
- Every build ran `CGO_ENABLED=0 go build -o <x>.bin .`, matching the flake's
  `env.CGO_ENABLED = "0"` (`apps/mugicrew/flake.nix:43`). The `-s -w`
  (stripped) builds are listed as a second column.
- Adding glamour: `go get github.com/charmbracelet/glamour@latest` resolved
  `glamour v1.0.0`. A file `v2/glamscratch.go` calls
  `glamour.Render("# hi\n\n\`x\`", "dark")` from an `init()`, guarded by an env
  var so the linker keeps it. v2 is linked into the binary through
  `main.go:59` (`v2tui "jayulabs.org/mugicrew/v2"`). The scratch binary did
  render ANSI Markdown when the env var was set.
- Control: same upgraded go.mod/go.sum, without the call (the "upgrade-only"
  build). This separates the cost of glamour's code from the cost of the
  version bumps it forces.
- Ablations: each one replaces the glamour call with a single call into one
  dependency, to show where the bytes go.

## 2. Headline delta

| Build | Unstripped (B) | Δ vs current | `-s -w` (B) | Δ stripped |
|---|---:|---:|---:|---:|
| Current HEAD (baseline today) | 17,305,425 | — | 12,378,274 | — |
| Upgrade-only (glamour's version bumps, no call) | 17,299,208 | −6,217 (−0.0%) | 12,374,178 | −4,096 |
| **+ minimal `glamour.Render`** | **24,162,536** | **+6,857,111 (+39.6%)** | **18,075,810** | **+5,697,536 (+46.0%)** |

- **Against ADR 0141's 16,014,194 B baseline** (ADR 0141 §9): +6,857,111 B is
  **+42.8%**. That is about 25× what bubbles cost (+276,740 B, +1.7%).
- The baseline has grown since then. Today's binary is 17,305,425 B, which is
  +1,291,231 B (+8.1%) over ADR 0141's figure. So ADR 0141's 16.0 MB is no
  longer the real floor, and the percentage is lower against today's size
  (39.6%) only because the base grew.
- The version bumps are free: upgrade-only is 6 KB *smaller*. The whole cost
  comes from glamour's own dependency graph.

## 3. Where the bytes go (ablations)

Each row is a separate build: current HEAD plus a single call into one
dependency (unstripped Δ vs 17,305,425 B; stripped Δ vs 12,378,274 B). These
overlap: chroma `quick` includes `lexers` and `styles`. So the rows don't add
up to the glamour total.

| Single call added | Δ unstripped | Δ stripped |
|---|---:|---:|
| `quick.Highlight(...)` (chroma: lexers + styles + formatters) | +4,213,342 (+24.3%) | +3,796,992 |
| `lexers.Get("go")` | +3,538,357 (+20.4%) | +3,178,496 |
| `styles.Get("monokai")` | +1,064,037 (+6.1%) | +786,432 |
| `goldmark.Convert(...)` | +941,269 (+5.4%) | +651,264 |
| `bluemonday.StrictPolicy().Sanitize(...)` | +248,071 (+1.4%) | +188,416 |

- **chroma is about 61% of glamour's cost** (4.21 of 6.86 MB), and its lexer
  registry alone is about 52% (3.54 MB). `lexers` embeds its whole
  `embedded/` dir (`chroma/v2@v2.20.0/lexers/lexers.go:10`: `//go:embed embedded`,
  257 XML lexer files, 1,792,791 B on disk) and registers every lexer, so
  using one lexer links all of them. `styles` embeds `*.xml`
  (`styles/api.go:11`, 134,115 B on disk).
- glamour has no opt-out. `ansi/codeblock.go:8-10` imports `chroma/v2`,
  `chroma/v2/quick` and `chroma/v2/styles` with no condition, and calls
  `quick.Highlight` (`codeblock.go:137`). No file in `glamour@v1.0.0` carries a
  `//go:build` constraint (grep for `^//go:build` / `^// +build` in the module
  found none). Any `glamour.Render` therefore pays for every chroma lexer.
- The remaining ~1.4 MB is goldmark (+ goldmark-emoji's definition table),
  bluemonday (+ douceur, gorilla/css, x/net/html), regexp2 (chroma's regex
  engine), x/text (cases/language), and glamour/ansi itself.
- Symbol-level cross-check (`go tool nm -size`, upgrade-only vs glamour,
  grouped by module, sized symbols only): chroma 554,265; goldmark 350,632;
  x/text 191,111; glamour 166,056; bluemonday 165,941; regexp2 143,052;
  lipgloss 32,816; x/net 19,109. The embedded lexer data, pclntab and DWARF
  sit outside these sized symbols, which is why the sum (2.08 MB) is under the
  file delta.

## 4. Transitive dependencies glamour adds

New modules in go.mod after `go get glamour@latest` (diff against
`apps/mugicrew/go.mod` at HEAD):

- **added**: `charmbracelet/glamour v1.0.0`, `alecthomas/chroma/v2 v2.20.0`,
  `dlclark/regexp2 v1.11.5`, `yuin/goldmark v1.7.13`,
  `yuin/goldmark-emoji v1.0.6`, `microcosm-cc/bluemonday v1.0.27`,
  `aymerick/douceur v0.2.0`, `gorilla/css v1.0.1`, `muesli/reflow v0.3.0`,
  `charmbracelet/x/exp/slice`, `golang.org/x/net v0.38.0`,
  `golang.org/x/term v0.36.0`.
- **upgraded**: lipgloss v1.1.0 → v1.1.1-0.20250404203927-76690c660834
  (a pseudo-version, not a tag), x/ansi v0.10.1 → v0.10.2, x/cellbuf →
  v0.0.13, go-colorful v1.2.0 → v1.3.0, go-runewidth v0.0.16 → v0.0.17,
  x/sys v0.36.0 → v0.37.0, x/text v0.3.8 → v0.30.0.

Newly linked non-std packages (`go list -deps .`, glamour build minus
upgrade-only): chroma/v2 {root, formatters, formatters/html, formatters/svg,
lexers, quick, styles}; douceur/{css,parser}; glamour {root, ansi,
internal/autolink, styles}; lipgloss/table; x/exp/slice; regexp2 {root,
syntax}; gorilla/css/scanner; bluemonday {root, css}; reflow
{ansi, indent, padding, wordwrap}; goldmark {root, ast, extension,
extension/ast, parser, renderer, renderer/html, text, util}; goldmark-emoji
{root, ast, definition}; x/net/html {root, atom}; x/term; x/text {cases,
language, transform, unicode/norm, internal/...}. New std: `encoding/xml`,
`html`. The chroma html/svg formatters get linked even though only a
terminal formatter is used.

## 5. Options that shrink it

| Option | Expected size | Cost / caveat |
|---|---|---|
| glamour's own options (`WithStandardStyle`, `WithStyles`, `WithChromaFormatter`, ... `glamour.go:103-242`) | none | They change behaviour only. chroma stays linked through `ansi/codeblock.go` imports. |
| `-ldflags="-s -w"` | −1.16 MB of glamour's delta (+5.70 MB vs +6.86 MB) | Shrinks the base too. The relative cost *rises* to +46%. It's orthogonal to glamour. |
| goldmark only, with a small in-house ANSI renderer for the subset the Idea tab needs (headings, emphasis, lists, code spans/blocks unhighlighted) | ≈ +0.94 MB (+5.4%) floor (goldmark ablation) | Means writing and maintaining a renderer. No syntax highlighting. |
| Fork/`replace` glamour with `ansi/codeblock.go` de-chroma'd | ≈ +2.6 MB (glamour total minus chroma `quick` ablation; estimated, not built) | Carrying a fork. Also drops regexp2. |
| Plain lipgloss/no Markdown rendering (status quo) | 0 | No rendering. |

## Open questions / gaps

- **Fork estimate not built.** The ≈ +2.6 MB de-chroma'd figure is
  subtraction from separate ablations, not a measured build.
- **Nix output size not measured.** These are plain `go build` sizes. The
  flake's `buildGoModule` (`flake.nix:20-43`) sets no `ldflags` in the lines
  read, and whether nixpkgs' fixup strip lands nearer the unstripped or the
  `-s -w` column wasn't checked. ADR 0141 §9's 16,014,194 B doesn't say which
  build produced it either (it says "the prototype binary").
- **Toolchain drift.** This used go1.26.7 linux/amd64. The bd memory notes a
  go1.25 linux/386 toolchain elsewhere. 32-bit sizes will differ, and it's
  unknown whether ADR 0141's baseline came from that toolchain.
- **Pseudo-version lipgloss.** glamour v1.0.0 forces lipgloss to an untagged
  pseudo-version. Whether that changes v1/v2 rendering wasn't tested.
- **Policy threshold.** No ADR states a binary-size budget. Whether +40% is
  acceptable is a Phase 6 decision, not something this research settles.
