# ADR 0023: Responsive mugicrew TUI — patch the hand-rolled rendering, do not adopt bubbles/viewport

Status: Accepted — single-pane breakpoint raised from 60 to 84 cols by ADR 0072
Date: 2026-09-25

Chart: Responsive mugicrew TUI layout. Settles the bug-vs-model spike so the Spec can state HOW responsive rendering is implemented.

## Context

The mugicrew TUI (`apps/mugicrew/tui/`) lays out a fixed horizontal master/detail
split (`view.go` `splitPane` → `lipgloss.JoinHorizontal`). Two defects block a
phone-SSH-usable layout (floor 40×12, single-pane breakpoint 60 cols — ADR input research):

- The **master list is never windowed** — `graphView`/`inboxView` render *every*
  row and hand the whole slice to `splitPane`, so it overflows any terminal
  shorter than the list.
- There is **no single-pane mode** — every width gets the horizontal split, just
  squeezed; below ~44 cols the detail pane is starved to 16 inner columns.

The question: can this be **patched** — correct wrap/resize rendering,
single-pane collapse below the breakpoint (reusing the existing `focusDetail`/Tab
toggle, spec §9.4), and master-list scroll-follow-selection — or is
`charmbracelet/bubbles/viewport` (a **new** dependency) warranted? Bias, per the
Chart and ponytail: patch-first.

## Decision

**Patch the hand-rolled rendering. Do not add `bubbles/viewport`.**

A throwaway prototype (lineage on local ref
`refs/mugicrew/prototypes/<chart-id>`) drove the **real** rendering primitives (same
lipgloss v1.1.0) at 36×10, 40×12, 55×16, 60×20 and 100×30 against a 22-row master
list and a long wrapping description, measuring rendered rows vs terminal height:

| geometry            | today (rows, fits h?) | patched (rows, fits h?) |
|---------------------|-----------------------|-------------------------|
| below floor 36×10   | 22, no                | 3 ("too small"), yes    |
| floor 40×12         | 22, no                | 8, yes                  |
| marginal 55×16      | 22, no                | 12, yes                 |
| breakpoint 60×20    | 22, no                | 16, yes                 |
| wide 100×30         | 26, yes               | 26, yes                 |

The patch is small and reuses what already exists:

- **Master scroll-follow** reuses the existing `windowRows` helper (view.go:205,
  which already makes the *detail* pane wrap+scroll correctly). The only new
  logic is a ~6-line `followOffset(total, height, sel)` that centres the window
  on the selection. No new Model state — the selected row index already exists.
- **Single-pane collapse** below the breakpoint renders only the focused pane at
  full width, reusing `focusDetail` (Tab already flips it). ~8 lines.
- **"Terminal too small"** notice below the floor. ~4 lines.

Net ≈ 25 lines, zero new dependencies.

## Rationale

`bubbles/viewport` is a scroll-a-body-in-a-window component — exactly what
`windowRows` already does here, and the TUI already relies on it for the detail
pane. Adopting it would **add a dependency** *and* force restructuring the Model
to hold `viewport.Model` instances per pane, feed them content, and route their
`Update` — while still leaving single-pane collapse and master-follow as our own
layout logic (viewport does not provide either). It buys nothing the existing
helper doesn't, at a real cost. Patching is the smaller, dependency-free change.

## Consequences

- The Spec (the Chart's Destination) states responsive rendering is a **patch** of
  the hand-rolled layout, reusing `windowRows` + `focusDetail`; **no new dep**.
- Implementation is **Build** work (kind:build) under the Checkpoint the Spec
  cuts — not charted here (per the Chart's Notes). This ADR fixes the approach;
  it does not itself write the production patch.
- The throwaway prototype is **not merged** (ADR 0009); its lineage is kept only
  on `refs/mugicrew/prototypes/<chart-id>`.
- Reversible: if a future need exceeds windowing (e.g. horizontal scroll,
  mouse), revisiting bubbles is a contained, additive change.
