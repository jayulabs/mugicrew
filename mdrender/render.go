// Package mdrender renders Markdown to pre-wrapped ANSI lines for the TUI's
// detail drawer. It is a leaf: goldmark's parser and x/ansi only, no bd, core,
// tea or lipgloss.
package mdrender

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// The fixed palette (the 256-color dark one), as paired SGR codes so spans nest
// without a full reset.
const (
	accent = "212"
	muted  = "244"
	code   = "180"
	link   = "111"
)

func fg(c, s string) string  { return "\x1b[38;5;" + c + "m" + s + "\x1b[39m" }
func bold(s string) string   { return "\x1b[1m" + s + "\x1b[22m" }
func ital(s string) string   { return "\x1b[3m" + s + "\x1b[23m" }
func uline(s string) string  { return "\x1b[4m" + s + "\x1b[24m" }
func strike(s string) string { return "\x1b[9m" + s + "\x1b[29m" }

var parser = goldmark.New(goldmark.WithExtensions(extension.Table, extension.TaskList, extension.Strikethrough)).Parser()

// Render renders src as ANSI lines, none wider than width cells: a blank line
// above and below, a 1-cell margin each side, the text wrapped between. Widths
// under 3 have no room for a margin and go without. Output is not newline-
// terminated beyond the closing blank line.
func Render(src string, width int) string {
	b := []byte(src)
	r := rd{src: b}
	pad, w := " ", width-2
	if w < 1 {
		pad, w = "", max(width, 1)
	}
	lines := indent(r.blocks(parser.Parse(text.NewReader(b)), w), pad, pad)
	for i, l := range lines {
		// ponytail: a nested marker can still outrun a tiny width; cut it rather than overflow.
		lines[i] = ansi.Truncate(l, max(width, 1), "")
	}
	return "\n" + strings.Join(lines, "\n") + "\n"
}

type rd struct{ src []byte }

// blocks renders n's block children, a blank line between them (none in a tight list item).
func (r rd) blocks(n ast.Node, w int) []string {
	tight := false
	if l, ok := n.Parent().(*ast.List); ok && n.Kind() == ast.KindListItem {
		tight = l.IsTight
	}
	var out []string
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if len(out) > 0 && !tight {
			out = append(out, "")
		}
		out = append(out, r.block(c, w)...)
	}
	return out
}

func (r rd) block(n ast.Node, w int) []string {
	switch n := n.(type) {
	case *ast.Heading:
		s := r.inline(n)
		switch n.Level {
		case 1:
			s = bold(uline(fg(accent, s)))
		case 2:
			s = bold(fg(accent, s))
		case 3:
			s = bold(s)
		default:
			s = bold(fg(muted, s))
		}
		return wrap(s, w)
	case *ast.List:
		var out []string
		i := n.Start
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			m := "• "
			if n.IsOrdered() {
				m = fmt.Sprintf("%d. ", i)
				i++
			}
			if len(out) > 0 && !n.IsTight {
				out = append(out, "")
			}
			mw := ansi.StringWidth(m)
			out = append(out, indent(r.blocks(c, w-mw), fg(muted, m), strings.Repeat(" ", mw))...)
		}
		return out
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		var out []string
		ls := n.Lines()
		for i := 0; i < ls.Len(); i++ {
			seg := ls.At(i)
			l := strings.TrimRight(strings.ReplaceAll(string(seg.Value(r.src)), "\t", "    "), "\r\n")
			for _, x := range strings.Split(ansi.Hardwrap(l, max(w-2, 1), true), "\n") {
				out = append(out, "  "+fg(code, x))
			}
		}
		return out
	case *ast.Blockquote:
		return indent(r.blocks(n, w-2), fg(muted, "│ "), fg(muted, "│ "))
	case *ast.ThematicBreak:
		return []string{fg(muted, strings.Repeat("─", max(w, 1)))}
	case *east.Table:
		return r.table(n, w)
	case *ast.HTMLBlock:
		var out []string
		line := func(s text.Segment) {
			out = append(out, wrap(fg(muted, strings.TrimRight(string(s.Value(r.src)), "\r\n")), w)...)
		}
		for i := 0; i < n.Lines().Len(); i++ {
			line(n.Lines().At(i))
		}
		if n.HasClosure() {
			line(n.ClosureLine)
		}
		return out
	}
	return wrap(r.inline(n), w) // paragraphs, and any other node: its text
}

func (r rd) inline(n ast.Node) string {
	var sb strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c := c.(type) {
		case *ast.Text:
			sb.Write(util.UnescapePunctuations(util.ResolveEntityNames(c.Segment.Value(r.src))))
			if c.HardLineBreak() {
				sb.WriteString("\n")
			} else if c.SoftLineBreak() {
				sb.WriteString(" ")
			}
		case *ast.String:
			sb.Write(c.Value)
		case *east.Strikethrough:
			sb.WriteString(strike(r.inline(c)))
		case *east.TaskCheckBox:
			if c.IsChecked {
				sb.WriteString(fg(muted, "☑") + " ")
			} else {
				sb.WriteString("☐ ")
			}
		case *ast.CodeSpan:
			var lit strings.Builder
			for t := c.FirstChild(); t != nil; t = t.NextSibling() {
				if t, ok := t.(*ast.Text); ok { // literal: no unescape, no entities
					lit.Write(t.Segment.Value(r.src))
					if t.SoftLineBreak() {
						lit.WriteString(" ")
					}
				}
			}
			sb.WriteString(fg(code, lit.String()))
		case *ast.Emphasis:
			if c.Level == 2 {
				sb.WriteString(bold(r.inline(c)))
			} else {
				sb.WriteString(ital(r.inline(c)))
			}
		case *ast.Link:
			t := r.inline(c)
			sb.WriteString(fg(link, t))
			if d := string(c.Destination); d != ansi.Strip(t) {
				sb.WriteString(fg(muted, " ("+d+")"))
			}
		case *ast.AutoLink:
			sb.WriteString(fg(link, string(c.URL(r.src))))
		case *ast.RawHTML:
			for i := 0; i < c.Segments.Len(); i++ {
				s := c.Segments.At(i)
				sb.WriteString(fg(muted, string(s.Value(r.src))))
			}
		default:
			sb.WriteString(r.inline(c))
		}
	}
	return sb.String()
}

// table draws t as a box. A table that fits w at natural width is drawn as is;
// otherwise columns narrow in proportion to their natural width, never below
// the longest unbreakable word (cells wrap, a rule runs between body rows), and
// failing that down to min(longest word, minCol) cells (long words hard-break).
// When even that does not fit, each body row is stacked as Header: value lines.
func (r rd) table(t *east.Table, w int) []string {
	var rows [][]string
	var hdr []bool
	n := len(t.Alignments)
	for row := t.FirstChild(); row != nil; row = row.NextSibling() {
		var cells []string
		for c := row.FirstChild(); c != nil; c = c.NextSibling() {
			cells = append(cells, strings.ReplaceAll(r.inline(c), "\n", " "))
		}
		for len(cells) < n {
			cells = append(cells, "")
		}
		rows = append(rows, cells[:n])
		_, h := row.(*east.TableHeader)
		hdr = append(hdr, h)
	}
	nat, mn := make([]int, n), make([]int, n)
	for ri, cells := range rows {
		for i, c := range cells {
			if hdr[ri] {
				c = bold(c)
			}
			nat[i] = max(nat[i], ansi.StringWidth(c))
			mn[i] = max(mn[i], minWord(c))
		}
	}
	avail := w - 1 - 3*n
	if sum(nat) <= avail {
		return box(t, rows, hdr, nat, false)
	}
	if sum(mn) <= avail {
		return box(t, rows, hdr, prop(mn, nat, avail), true)
	}
	floor := make([]int, n)
	for i, x := range mn {
		floor[i] = min(x, minCol)
	}
	if sum(floor) <= avail {
		return box(t, rows, hdr, prop(floor, nat, avail), true)
	}
	return stacked(rows, hdr, w)
}

// minCol is the narrowest a column is squeezed to when its longest word must break.
const minCol = 4

func sum(xs []int) int {
	s := 0
	for _, x := range xs {
		s += x
	}
	return s
}

// minWord is s's longest unbreakable run: it ends at a space or after a - or /.
func minWord(s string) int {
	m, cur := 0, 0
	for _, r := range ansi.Strip(s) {
		switch r {
		case ' ':
			cur = 0
		case '-', '/':
			m = max(m, cur+1)
			cur = 0
		default:
			cur += ansi.StringWidth(string(r))
			m = max(m, cur)
		}
	}
	return m
}

// fill finds the largest level L with Σ max(lo, min(hi, L)) ≤ avail, then hands
// the leftover cells one at a time to the column furthest below its hi.
func fill(lo, hi []int, avail int) []int {
	cw := make([]int, len(lo))
	at := func(L int) int {
		s := 0
		for i := range lo {
			cw[i] = max(lo[i], min(hi[i], L))
			s += cw[i]
		}
		return s
	}
	L := 1
	for L < avail && at(L+1) <= avail {
		L++
	}
	s := at(L)
	for s < avail {
		b := -1
		for i := range cw {
			if cw[i] < hi[i] && (b < 0 || hi[i]-cw[i] > hi[b]-cw[b]) {
				b = i
			}
		}
		if b < 0 {
			break
		}
		cw[b]++
		s++
	}
	return cw
}

// prop shrinks every column from nat by one ratio to fit avail, holding any
// that would fall below lo at lo and sharing the rest among the others.
func prop(lo, nat []int, avail int) []int {
	n := len(lo)
	cw := make([]int, n)
	fixed := make([]bool, n)
	for changed := true; changed; {
		changed = false
		left, natLeft := avail, 0
		for i := range lo {
			if fixed[i] {
				left -= lo[i]
			} else {
				natLeft += nat[i]
			}
		}
		for i := range lo {
			if fixed[i] {
				cw[i] = lo[i]
				continue
			}
			cw[i] = nat[i] * left / max(natLeft, 1)
			if cw[i] < lo[i] {
				fixed[i], changed = true, true
			}
		}
	}
	return fill(cw, nat, avail)
}

var sgr = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// carry re-opens, on each wrapped line, the styles still open from the line
// before, and closes them all at the line's end, so cell borders stay plain.
func carry(ls []string) []string {
	var fgc, b, it, ul, st string
	for i, l := range ls {
		ls[i] = fgc + b + it + ul + st + l + "\x1b[0m"
		for _, m := range sgr.FindAllStringSubmatch(l, -1) {
			switch p := m[1]; {
			case strings.HasPrefix(p, "38;"):
				fgc = m[0]
			case p == "0" || p == "":
				fgc, b, it, ul, st = "", "", "", "", ""
			case p == "39":
				fgc = ""
			case p == "1":
				b = m[0]
			case p == "22":
				b = ""
			case p == "3":
				it = m[0]
			case p == "23":
				it = ""
			case p == "4":
				ul = m[0]
			case p == "24":
				ul = ""
			case p == "9":
				st = m[0]
			case p == "29":
				st = ""
			}
		}
	}
	return ls
}

// cellLines word-wraps s at w. A run ends at a space or after a - or /, the
// same runs minWord measures, and runs are packed greedily so no line splits a
// run that fits; only a run wider than w is hard-broken. x/ansi's Wrap breaks
// mid-word even when a break point fits, so it is not used.
func cellLines(s string, w int) []string {
	w = max(w, 1)
	var out []string
	var cur, run strings.Builder
	cw := 0
	space := false // a space separates the next run from cur
	flush := func() {
		if cw > 0 {
			out = append(out, cur.String())
		}
		cur.Reset()
		cw, space = 0, false
	}
	add := func() {
		r := run.String()
		run.Reset()
		rw := ansi.StringWidth(r)
		sp := 0
		if space {
			sp = 1
		}
		switch {
		case rw == 0:
			cur.WriteString(r) // bare SGR codes ride along
			return
		case rw > w:
			flush()
			ps := strings.Split(ansi.Hardwrap(r, w, false), "\n")
			out = append(out, ps[:len(ps)-1]...)
			r = ps[len(ps)-1]
			rw = ansi.StringWidth(r)
		case cw > 0 && cw+sp+rw > w:
			flush()
		}
		if cw > 0 && space {
			cur.WriteByte(' ')
			cw++
		}
		cur.WriteString(r)
		cw += rw
		space = false
	}
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			if m := sgr.FindStringIndex(s[i:]); m != nil && m[0] == 0 {
				run.WriteString(s[i : i+m[1]])
				i += m[1]
				continue
			}
		}
		r, n := utf8.DecodeRuneInString(s[i:])
		i += n
		switch r {
		case ' ':
			add()
			space = true
		case '-', '/':
			run.WriteRune(r)
			add()
		default:
			run.WriteRune(r)
		}
	}
	add()
	flush()
	if len(out) == 0 {
		out = []string{""}
	}
	return carry(out)
}

// box draws rows at column widths cw: cells wrap to their column, a row is as
// tall as its tallest cell, and when narrowed a rule runs between body rows.
func box(t *east.Table, rows [][]string, hdr []bool, cw []int, narrowed bool) []string {
	rule := func(l, m, rt string) string {
		parts := make([]string, len(cw))
		for i, x := range cw {
			parts[i] = strings.Repeat("─", x+2)
		}
		return fg(muted, l+strings.Join(parts, m)+rt)
	}
	bar := fg(muted, "│")
	out := []string{rule("┌", "┬", "┐")}
	for ri, cells := range rows {
		if narrowed && ri > 0 && !hdr[ri] && !hdr[ri-1] {
			out = append(out, rule("├", "┼", "┤"))
		}
		col := make([][]string, len(cw))
		h := 1
		for i, x := range cw {
			c := cells[i]
			if hdr[ri] {
				c = bold(c)
			}
			col[i] = cellLines(c, x)
			h = max(h, len(col[i]))
		}
		for k := 0; k < h; k++ {
			line := bar
			for i, x := range cw {
				c := ""
				if k < len(col[i]) {
					c = col[i][k]
				}
				gap := max(x-ansi.StringWidth(c), 0)
				l, rt := 0, gap
				switch t.Alignments[i] {
				case east.AlignRight:
					l, rt = gap, 0
				case east.AlignCenter:
					l, rt = gap/2, gap-gap/2
				}
				line += " " + strings.Repeat(" ", l) + c + strings.Repeat(" ", rt) + " " + bar
			}
			out = append(out, line)
		}
		if hdr[ri] {
			out = append(out, rule("├", "┼", "┤"))
		}
	}
	return append(out, rule("└", "┴", "┘"))
}

// wrap word-wraps s at w cells (long words hard-break) into lines. Prose keeps
// plain x/ansi wrapping: cellLines' / breaks and quirk patches matter only
// where a narrow column forces them.
func wrap(s string, w int) []string { return strings.Split(ansi.Wrap(s, max(w, 1), ""), "\n") }

// indent prefixes the first line with first and the other non-blank lines with rest.
func indent(ls []string, first, rest string) []string {
	for i := range ls {
		if i == 0 {
			ls[i] = first + ls[i]
		} else if ls[i] != "" {
			ls[i] = rest + ls[i]
		}
	}
	return ls
}

// stacked prints each body row as one Header: value line per cell (wrapped at
// w-2, continuations indented 2), rows split by a muted rule; the header row
// is not printed on its own, unless the table has no body rows: then its
// header cells print as the one record.
func stacked(rows [][]string, hdr []bool, w int) []string {
	w = max(w, 1)
	var heads []string
	var out []string
	add := func(c string) { out = append(out, indent(cellLines(c, w-2), "", "  ")...) }
	for ri, cells := range rows {
		if hdr[ri] {
			heads = cells
			continue
		}
		if len(out) > 0 {
			out = append(out, fg(muted, strings.Repeat("─", w)))
		}
		for i, c := range cells {
			if i < len(heads) && heads[i] != "" {
				c = bold(heads[i]) + fg(muted, ":") + " " + c
			}
			add(c)
		}
	}
	if len(out) == 0 {
		for _, c := range heads {
			add(bold(c))
		}
	}
	for i, l := range out {
		out[i] = ansi.Truncate(l, w, "")
	}
	return out
}
