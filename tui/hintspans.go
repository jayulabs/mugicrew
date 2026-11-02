package tui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// hintSpan is one drawn label segment of a dialog's hint line: its row (from the line's
// first) and columns [x0, x1) from the line's start, and the binding it stands for. A
// label the wrap breaks across rows has a span on each row it occupies.
type hintSpan struct {
	row, x0, x1 int
	b           key.Binding
}

// hintLine is a dialog's bindings as the hint line drawn in a box of inner width w,
// wrapped as drawn, and the span of every helpable binding's label. The spans are read
// back off the wrapped text, so they match exactly what is drawn, whatever the wrap
// breaks.
func hintLine(bs []key.Binding, w int) (string, []hintSpan) {
	flat, drawnBs := flatHintsOf(bs, w)
	// the spans keep each binding's own help: the collapse to key-only text rewrites the
	// drawn copies, and answering tells navigation hints apart by their help
	bs = helpable(bs)
	drawn := lipgloss.NewStyle().Width(w).Render(flat)
	var rows []string
	for _, r := range strings.Split(drawn, "\n") {
		rows = append(rows, ansi.Strip(r))
	}
	var spans []hintSpan
	row, from := 0, 0
	for i, b := range bs {
		if !b.Enabled() {
			continue
		}
		label := drawnBs[i].Help().Key
		if drawnBs[i].Help().Desc != "" {
			label += " " + drawnBs[i].Help().Desc
		}
		for _, word := range strings.Fields(label) {
			for row < len(rows) {
				i := strings.Index(rows[row][from:], word)
				if i >= 0 {
					x0 := ansi.StringWidth(rows[row][:from+i])
					spans = append(spans, hintSpan{row, x0, x0 + ansi.StringWidth(word), b})
					from += i + len(word)
					break
				}
				row, from = row+1, 0
			}
		}
	}
	return drawn, mergeWordSpans(spans)
}

// mergeWordSpans joins a label's words that sit side by side on one row into one span,
// so the gap between them is part of the label.
func mergeWordSpans(ws []hintSpan) []hintSpan {
	var out []hintSpan
	for _, s := range ws {
		if n := len(out); n > 0 {
			p := &out[n-1]
			if p.row == s.row && slices.Equal(p.b.Keys(), s.b.Keys()) && p.b.Help() == s.b.Help() && s.x0-p.x1 == 1 {
				p.x1 = s.x1
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

// navBindings are the hints that step through rows or fields: drawn and spanned, never
// targets (the mouse acts on the rows and fields themselves).
var navBindings = []key.Binding{keyDown, keyUp, keyMoveUD, keyField, keyFormField, keyWhyFocus, keyListFocus, keySgMove, keySgJK}

// answering is whether a click on b presses its key: every binding but a navigation one.
func answering(b key.Binding) bool {
	return len(b.Keys()) > 0 && !slices.ContainsFunc(navBindings, func(n key.Binding) bool {
		return slices.Equal(n.Keys(), b.Keys()) && n.Help() == b.Help()
	})
}

// hintAt is the answering hint of the top modal under screen cell (x, y), in screen
// columns, and ok false off every such hint. The hint line is the box's last content
// rows, however many it wraps to.
func (m Model) hintAt(x, y int) (loopOptCells, bool) {
	if len(m.modals) == 0 {
		return loopOptCells{}, false
	}
	d := m.modals[len(m.modals)-1]
	var inner int
	switch d.kind {
	case confirm, input, picker, form:
		inner = m.dialogWidth() - 4
		if d.kind == form {
			inner = m.formWidth() - 4
		}
	case surgery:
		inner = m.surgeryInner()
	case xdialog:
		inner = m.xWidth() - 4
	default:
		return loopOptCells{}, false
	}
	line, spans := hintLine(d.bindings(), inner)
	lay := m.dialogLayout(d)
	first := lay.y + lay.h - 1 - (strings.Count(line, "\n") + 1)
	off := lay.x + loopOptPad
	for _, sp := range spans {
		if answering(sp.b) && y == first+sp.row && x >= off+sp.x0 && x < off+sp.x1 {
			return loopOptCells{off + sp.x0, off + sp.x1, sp.b.Keys()[0]}, true
		}
	}
	return loopOptCells{}, false
}
