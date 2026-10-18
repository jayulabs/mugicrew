# Where v2 shows bead bodies and how rendered Markdown would fit the drawer

Status: Research (input to the v2 TUI polish: Markdown bead bodies Spec, mc-lu2x.44)
Date: 2026-10-14
Bead: mc-lu2x.44.3

## Purpose

The Chart says Markdown renders only the body text in read-only views: the drawer
on every tab and each Chart-body section. The field header block and the launch
preview stay plain. This doc lists every v2 site that puts a bead body into a
drawer and traces the path from builder to screen: how the body sits with the
header and the preview, who wraps it and at what width, and how the drawer scrolls.
A probe then checks whether pre-rendered ANSI survives that path. The doc ends with
where one `render(body, width)` seam fits and whether it needs a cache. Code is cited
at the effort branch's base (`9330ed51`), under `apps/mugicrew/`. Library code is
cited at the versions pinned in `go.mod:7-10`: bubbles v0.21.0, bubbletea v1.3.10,
lipgloss v1.1.0 and x/ansi v0.10.1. lipgloss pulls in cellbuf
`v0.0.13-0.20250311204145-2c3ea96c31dd` (`go.mod:18`).

## 1. Body sites

Every body reaches the screen as part of one text string. The builder appends
`b.Description` raw, joining it to the other parts with `"\n\n"`:

| # | Site | Composition | Reached from |
|---|------|-------------|--------------|
| 1 | Graph `detailText` (`v2/graphview.go:683-687`) | field block, then `b.Description`, then `g.launchPreview(b)`: three parts | Graph drawer (`graphview.go:611-613`); the Initiatives-tab drawer reuses it (`v2/initstab.go:310-316`); focus fallback rows (`v2/focus.go:551`) |
| 2 | Defog `rowText`, non-Chart bead (`v2/defogdetail.go:133-138`) | field block, then `b.Description` | Defog drawer (`defogdetail.go:17-19`); focus Defog rows (`focus.go:543-545`) |
| 3 | Defog `rowText`, Chart bead (`defogdetail.go:83-101`) | field block, then **only** `## Destination` and `## Not-yet-specified`, each as `labelSt` heading + `strings.Join(lines,"\n")`; every other section becomes a `"<name>: N lines"` count line | same as 2 |
| 4 | Defog Initiative header row (`defogdetail.go:66-70`) | `title id`, the fixed "enter does nothing…" line, then `r.init.Description` raw | Defog drawer |
| 5 | Execute `rowText` (`v2/exectabdetail.go:181-185`) | field block, then `b.Description` | Execute drawer (`exectabdetail.go:18-19`); focus Execute rows (`focus.go:546-548`) |
| 6 | Execute Initiative header row (`exectabdetail.go:83`) | same shape as 4 | Execute drawer |
| 7 | Ideate `beadText` (`v2/ideatab.go:874-890`) | one `Sprintf`: title id, status/prio/parent/labels lines, then `b.Description`. Its comment says "raw markdown description" (`ideatab.go:873`) | Ideate drawer, with the launch preview appended for launchable rows (`ideatab.go:862-866`); focus Ideate rows (`focus.go:540-542`) |

There is no other body-to-drawer path. Logs' drawer shows events, not bead bodies
(`v2/logs.go:378-380`). The Initiatives tab's own rows show only a "no body yet"
cue (`initstab.go:395-405`).

`chartSections` (`defogdetail.go:28-41`) splits only on lines that begin with
`"## "`. It **drops blank lines** inside a section
(`cur != "" && strings.TrimSpace(l) != ""`, `defogdetail.go:35`). Lines before the
first heading are dropped too, because `cur == ""` there. Paragraph breaks, the
blank line that ends a list, and the blank lines around fenced code are all gone
before any renderer would see the text. The `counts` field also reads
`len(secs["not yet specified"])`, a count of non-blank lines, as "fog"
(`defogdetail.go:86`).

## 2. Wrap, width and scroll path

All seven sites go through one helper, `drawerVP` (`v2/rows.go:60-65`):

```go
vp := viewport.New(max(w-2, 1), max(h-drawerH(open, h)-1, 1))
vp.SetContent(lipgloss.NewStyle().Width(vp.Width).Render(text))
vp.SetYOffset(off)
```

- **Width.** The text is wrapped at `max(w-2, 1)`, where `w` is the tab body's
  width (`rows.go:59-61`). The drawer's height is the body minus the top half for
  the list and one rule row (`rows.go:52-57`, `rows.go:139-140`).
- **Wrapper.** `Style.Width(n).Render` runs `cellbuf.Wrap(str, n, "")`
  (lipgloss `style.go:365-369`). This is an ANSI-aware word wrap. It then pads every
  line to `n` (`alignTextHorizontal`, `style.go:427-440`).
- **Second pass.** `viewport.View` renders the visible lines again through
  `lipgloss.NewStyle().Width(cw).Height(ch).MaxHeight(ch).MaxWidth(cw)`
  (bubbles `viewport/viewport.go:518-523`), at the same width. That means
  cellbuf.Wrap runs again and then `ansi.Truncate` (lipgloss `style.go:449-457`).
  **The drawer already wraps twice today.** It works because a line that already
  fits is left alone.
- **Third clamp.** The shell's `fit` truncates every frame line to the terminal
  width (`v2/shell.go:664-671`, applied at `shell.go:704-705`).
- **Scroll.** `keyScroll` is `pgup`/`pgdown`/`ctrl+u`/`ctrl+d`
  (`v2/graphtab.go:24`). Each handler rebuilds `detailVP()` (so it re-runs the
  builder and the wrap), calls `PageUp`/`PageDown`/`HalfPageUp`/`HalfPageDown`, and
  stores `vp.YOffset` back in `off` (`graphtab.go:257-269`; the same block is in
  `ideatab.go:820-832`, `initstab.go:361-370`, `defogtab.go:566`, `exectab.go:632`
  and `focus.go:480`). Moving the selection resets `off` to 0 (`graphtab.go:248`).
  `SetYOffset` clamps to the content's last page (bubbles `viewport.go:177-179`).
  A resize that shortens the content therefore clamps when drawn. The stored `off`
  can stay larger than the clamped value until the next scroll key.

The wrap only uses the line count. Scrolling is by **lines after wrapping**. So a
renderer that emits pre-wrapped lines changes nothing in the scroll logic.

## 3. Does pre-rendered ANSI survive the path?

This was tested with a throwaway probe in `package v2`, run with
`CGO_ENABLED=0 go test ./v2 -run TestZZProbe -v` on go1.26.7 linux/amd64 and then
deleted. It sent styled input through `drawerVP(true, 40, 40, s, 0).View()`
(content width 38) and compared the stripped text line by line. Exact output:

```
prewrapped@38 styled               nonblank in=3 out=3 maxw=38 textSame=true escKept=true
prewrapped@38 padded to 38         nonblank in=3 out=3 maxw=38 textSame=true escKept=true
prewrapped@38 padded to 40         nonblank in=3 out=3 maxw=38 textSame=true escKept=true
prewrapped@40 (too wide)           nonblank in=3 out=4 maxw=38 textSame=false escKept=true
code line 60 no spaces             nonblank in=1 out=2 maxw=38 textSame=false escKept=true
list hanging indent @38            nonblank in=2 out=2 maxw=38 textSame=true escKept=false
osc8 link                          nonblank in=1 out=1 maxw=38 textSame=true escKept=false
bg code padded to 38               nonblank in=1 out=1 maxw=38 textSame=true escKept=false
osc8 kept: true
bg@40 into 38: l0="\x1b[48;2;51;51;51m  code\x1b[0m                                " l1="                                      "
```

(`escKept` only tests for a bold SGR. It is false where the input had none.)

What the probe shows:

1. **Pre-wrapped at exactly the content width (`w-2`), it is not wrapped again.**
   SGR styling, hanging list indents and OSC 8 hyperlinks pass through unchanged.
   The same holds when lines are right-padded with spaces up to that width,
   glamour-style. Trailing spaces past the width are dropped, not wrapped, so a
   wider plain pad is harmless.
2. **Rendered wider than `w-2`, it does get wrapped again.** The 40-wide input
   became 4 lines instead of 3, breaking the renderer's layout. A long word or code
   line with no spaces is hard-broken at the width (`code line 60`: 1 line → 2), so
   code-block indentation is lost on the continuation line.
3. **A background-filled block wider than the width loses its fill.** cellbuf drops
   the trailing styled spaces, and the plain padding from `alignTextHorizontal` takes
   their place (`bg@40 into 38`). Code-block backgrounds must be padded to `w-2`
   exactly, no wider.

So the renderer must be given **`max(w-2, 1)`, the same value `drawerVP` computes**.
It should wrap at that width and pad to no more than that width. The existing
`drawerVP` → viewport → `fit` chain can then stay as it is: a no-op on fitting
lines, and a safety net otherwise. Neither `trunc` (`v2/settings.go:560`) nor
`ansi.Wrap` (used only at `settings.go:551`) is on the body path.

## 4. Where the `render(body, width)` seam belongs

- **Width reaches every builder.** Each tab has `w`: Defog `defogtab.go:341`,
  Execute `exectab.go:442`, Ideate `ideaData.w` `ideatab.go:380`, Graph
  `graphData.w` `graph.go:369`. The focus view and the Initiatives tab build their
  detail from tab values constructed on the spot (`focus.go:540-551`,
  `initstab.go:313`). Those values set only `g`/`gate`, but `graphData` comes from
  `gateData()`, which fills `w: m.w` (`v2/shell.go:146-150`). So every site can
  compute `max(g.w-2, 1)`. Today that formula lives only inside `drawerVP`
  (`rows.go:61`). It should become one shared helper, so the drawer's width and the
  renderer's width cannot drift apart.
- **Recommended shape.** One v2 function, for example `mdBody(body string, w int)
  string`. Each of the seven sites in §1 calls it on the `b.Description` /
  `r.init.Description` it already appends. The header block, the fixed hint lines
  and `launchPreview` stay out of it, which keeps them plain as the Chart settled.
  `drawerVP` stays unchanged. The renderer is pure presentation: it does no bd or
  core reads. That keeps the TUI a thin client, and it belongs on the v2 side. If
  it is goldmark plus in-house code, put the AST-to-ANSI walk in its own leaf
  package, with v2 owning only the styles. That keeps the new dependency in one
  place and lets the package be tested without the TUI. That is a style choice;
  nothing in the code forces it.
- **Edit paths stay raw.** Nothing in §1 is an edit surface. The Signal form, `E`
  raw and `e` edit read `b.Description` on their own paths, so they need no change.

## 5. Chart sections: render each section, not the whole body

- The Defog Chart detail shows a **selection** of sections with a `labelSt`
  heading in front of each (`defogdetail.go:94-97`), and a count line for the rest.
  Rendering the whole body would either undo that selection or force the renderer
  to know which Chart headings to keep. Rendering **each kept section's text** keeps
  the current layout: `labelSt` heading, then `mdBody(sectionText, w)`.
- **This needs a change in `chartSections`.** It drops blank lines
  (`defogdetail.go:35`), and Markdown needs them to separate paragraphs and lists.
  The Spec should keep the raw lines of each section, including blank ones, and
  move the non-blank filter to the one place that counts lines (`counts` at
  `defogdetail.go:86` and the `"N lines"` summary at `defogdetail.go:98`). Then
  those counts stay as they are while the renderer gets intact Markdown. `##`
  sub-headings inside a section cannot occur: any `## ` line starts a new section
  (`defogdetail.go:31`).
- A Chart opened in Graph, Execute or focus (site 1) shows the **whole** body
  through the generic path. There, render the whole body.

## 6. Re-render frequency, and whether a cache is needed

`detailText`/`rowText` have no memo. They run on every `View()`, because each
tab's `view` calls `drawer(..., t.detailVP(), ...)` (`graphview.go:1033`,
`defogtab.go:691`, `exectab.go:764`, `ideatab.go:1047`, `focus.go:690`,
`initstab.go:429`). They also run again on every scroll key (§2). bubbletea calls
`View` after each `Update`, so the triggers are every keypress, every resize, and
the 3 s live refresh tick (`v2/live.go:34`, `live.go:236`). They run only while a
drawer is open. A render therefore runs a few times per second at peak, on one body
of a few KB.

So a cache is **not required for correctness**. Whether one is needed for cost
depends on the renderer that mc-lu2x.44.2 picks. If a cache is added, key it on
`(body, width)`, for example a one-entry memo keyed on the description string and
`w-2`. That covers selection changes and resizes with no invalidation logic. The
v2 tabs are value types that get copied on each `Update` (`component` returns),
so the memo would have to live behind a pointer or in a package-level variable,
not in a tab field. Mark the ceiling with a `ponytail:` comment.

## Open questions / gaps

- **Render cost was not measured.** No Markdown renderer is in the module graph
  yet (`go.mod:7-19`), so per-render latency for a typical body is unknown. Measure
  it in the renderer prototype (mc-lu2x.44.2) before deciding on a cache (§6).
- **Light/dark.** Only `TrueColor` SGR was probed. How the renderer picks styles
  for the terminal background, and whether it should reuse v2's `softSt`/`labelSt`
  palette (`graphview.go:30-34`), is left to the renderer prototype and the
  Not-yet-specified styling question.
- **Wide runes.** The probe used ASCII only. cellbuf and ansi measure cell width,
  but CJK or emoji inside pre-wrapped lines were not tested.
- **Width-0 builders.** Some cross-tab builders are constructed without the tab's
  own `w`, for example `defogTab{g: g}` and `ideaTab{...}` in `focus.go:541-548`.
  They must read `g.w`/`gate.w`, not the zero tab field. A Spec that puts the width
  in the tab struct would pass width 0 here.
- **Stale `off` after resize** (§2): already true today with plain text. Rendering
  changes the line count more sharply on resize, so it may show up more. No fix is
  proposed here.
