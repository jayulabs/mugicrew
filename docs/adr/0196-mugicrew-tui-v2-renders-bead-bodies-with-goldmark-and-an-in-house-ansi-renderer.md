# ADR 0196: TUI v2 renders bead bodies with goldmark and an in-house ANSI renderer, not glamour

Status: Accepted
Date: 2026-10-15

Chart mc-lu2x.44 (v2 TUI polish: Markdown bead bodies). Prototype mc-lu2x.44.2,
kept on `refs/mugicrew/prototypes/mc-lu2x.44`
(`apps/mugicrew/v2/prototype_mdrender/`). Builds on the research in
`docs/research/glamour-binary-size-cost-cgo-disabled.md` and
`docs/research/v2-bead-body-sites-and-markdown-in-the-drawer.md`.

v2 bead bodies are rendered as Markdown by `github.com/yuin/goldmark` (the
parser only) and a small in-house ANSI renderer that walks goldmark's AST. Full
`charmbracelet/glamour` was rejected. The human compared both side by side on
four real bodies (a Signal, a Chart, a Spec, and a 22 KB Spec with code and
nested lists), at drawer widths from the 40-column floor up, and picked the
in-house renderer.

## Decision

1. **Dependency.** Add goldmark and nothing else: its core parser plus three
   of its own extensions, Table, TaskList and Strikethrough (amended by
   mc-lu2x.44.4; they live in the goldmark module and add about nothing). No
   glamour, chroma, bluemonday or goldmark-emoji.
2. **Output contract.** `render(body, width)` returns ANSI lines no wider than
   `width`, which is the drawer's `w-2`. Those lines pass through `drawerVP`'s
   wrap untouched (research mc-lu2x.44.3 §3). Text wraps with `x/ansi.Wrap`.
   Code lines hard-break at the width and keep their indent.
3. **Margins.** There is a 1-cell blank margin on each side (the text wraps at
   `width-2`) and one blank line above and below the body. The human chose
   these in the prototype.
4. **Styling.** It uses v2's fixed 256-colour palette (accent `212` for
   headings, `244` for muted text), with paired SGR set/unset codes so spans
   nest. There is no syntax highlighting and no terminal background query.
   One fixed palette serves dark and light terminals alike; light-terminal
   support, if wanted, is a separate change for all of v2's chrome.
5. **Covered subset** (grilling mc-lu2x.44.4). On top of the prototype's
   headings, paragraphs, emphasis, code, lists, blockquotes, links, breaks and
   muted raw HTML:
   - **Headings** carry no `#` marks and get a blank line above. h1 is bold
     accent underlined, h2 bold accent, h3 bold in the default colour, h4–h6
     bold muted.
   - **Tables** that fit in `width-2` are drawn with aligned columns,
     box-drawing rules, a bold header row and the source's `:--:` alignments.
     A table too wide is narrowed, hard-broken or stacked instead (amended by
     ADR 0242 (mugicrew-mdrender-narrows-wide-tables…)); raw source is never
     shown.
   - **Task lists** show `☐` for `[ ]` and a muted `☑` for `[x]`.
   - **Strikethrough** `~~x~~` uses SGR 9.
   - **Links** stay text plus a muted `(url)`. No OSC 8 hyperlinks: 7 bodies
     use them, inside tmux (ADR 0065) OSC 8 needs 3.4+ and the `hyperlinks`
     feature, and an unsupporting terminal can leak the escape.
6. **Chart bodies.** The Defog tab's Chart detail keeps its section labels and
   its line-count summaries for the other sections; each section it shows in
   full renders through the renderer on its own. Every other drawer renders a
   Chart body whole, so its `##` headings take the h2 style.

## Measured (CGO_ENABLED=0, go1.26.7 linux/amd64, chart branch at `ca19ed14`)

| Build | Unstripped (B) | Δ | `-s -w` (B) | Δ |
|---|---:|---:|---:|---:|
| Today | 19,522,895 | — | 14,160,034 | — |
| goldmark + in-house | 20,495,176 | +972,281 (+5.0%) | 14,835,874 | +675,840 |
| glamour v1.0.0 | 26,365,717 | +6,842,822 (+35.1%) | 19,849,378 | +5,689,344 |

Render time per body, averaged over 20 runs at widths 38/78/118. On an idle
machine, goldmark + in-house took 0.07–1.5 ms and glamour took 1.1–21 ms. Under
heavy load (load average 25 on 8 cores), goldmark + in-house took ≤ 7 ms. On
the same load, glamour took 36–38 ms on the 10.7 KB Spec and 72–97 ms on the
22 KB Spec, which breaks the 50 ms budget. This matters because the drawer
rebuilds its content on every scroll key.

## Considered Options

- **Full glamour v1.0.0.** It costs +6.84 MB, about 61% of which is chroma,
  with no opt-out. It forces lipgloss to an untagged pseudo-version
  (`v1.1.1-0.20250404203927-76690c660834`) and x/ansi to v0.10.2. Its output ran
  one column past the width it was given (39 at 38, 119 at 118), so `drawerVP`
  re-wraps it. Its auto style queries the terminal from inside a running
  bubbletea program. Highlighted code was not worth these costs.
- **A glamour fork without chroma.** Estimated at about +2.6 MB. It means
  carrying a fork. Rejected for the same reason as glamour, at a smaller cost.

## Consequences

- mugicrew maintains about 150 lines of renderer. A Markdown construct outside
  the covered subset falls back to its plain text. Which constructs are covered
  is item 5.
- go.mod gains one direct dependency. lipgloss v1.1.0 and x/ansi v0.10.1 stay
  where they are.
