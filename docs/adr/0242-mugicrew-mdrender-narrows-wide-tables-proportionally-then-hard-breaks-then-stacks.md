# mdrender narrows a wide table proportionally, then hard-breaks, then stacks it

Chart mc-l75u (Wide Markdown tables render as cell-wrapped box tables).
Prototype mc-l75u.3, kept on `refs/mugicrew/prototypes/mc-l75u`
(`apps/mugicrew/mdrender/prototype_narrow/`). Builds on
`docs/research/markdown-table-shapes-vs-mdrender-widths.md`. Amends the table
clause of ADR 0196 (mugicrew-tui-v2-renders-bead-bodies…): a table too wide
for its width no longer falls back to raw muted source.

A table is drawn in the first tier that fits its width (`1 + Σ(cw+3) ≤ w`):

1. **Box at natural width**, as today: no rules between body rows.
2. **Narrowed box.** Every column shrinks **proportionally** to its natural
   width, but never below its longest unbreakable word. A column that would
   drop below that floor is held at it, and the others share what is left in
   proportion. Cells word-wrap, and a rule runs between every pair of body rows.
3. **Hard-broken box.** When the longest words cannot fit, the floor drops to
   `min(longest word, 4)` cells. Columns share the width as in tier 2 and long
   words hard-break inside their cells.
4. **Stacked records**, the last resort when even 4 cells per column cannot
   fit (about ≥6 columns at the drawer floor). Each body row prints one
   `Header: value` line per cell, with the header bold and continuation lines
   indented 2. A muted rule runs between rows. Raw source is never shown.

A word can break after a space, a hyphen or a `/`. That way paths and
`file:line` code spans, which set most columns' floors, wrap at their
separators before they are hard-broken. Header cells stay bold and may wrap.
The header rule sits under the header's last line. Source alignments apply to
every wrapped line. A link keeps its muted ` (url)` suffix in a cell and
wraps like any other text. Styles open on a wrapped line are re-opened on the
next one and closed at each cell's edge, so borders stay unstyled.

## Considered Options

The human picked by eye, side by side on six real Decision Packet and Spec
tables and one synthetic table, at the doc-pane (T=120–240) and drawer
(W=40–120) widths.

- **Column sharing.** Proportional was picked over three others: browser-style
  fair share (the lead candidate: narrow columns keep their natural width and
  the widest shrink first), equal widths, and CSS 2.1 auto layout (min-content
  plus a share of the slack).
- **Last resort.** Hard-break then stacked was picked over two others:
  hard-break then raw source (the lead candidate), and stacked as soon as the
  longest words do not fit. The stacked-first option would stack most doc-pane
  tables at T≤160, where the research found hard-breaking is the common case.
- **Minimum column width.** 4 cells was picked over 3.
