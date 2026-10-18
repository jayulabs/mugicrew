package tui

import (
	"slices"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// line is a single-line text input with a movable cursor (the bubbles
// textinput is a heavier fit for the one-line dialogs): the text and the
// cursor's rune index into it (0 … rune count). Shared by every inline input —
// title, raw, reject reason, filter and the create form's fields — so their
// keys don't drift.
type line struct {
	text string
	cur  int
}

// lineAt is a pre-filled input with the cursor at the end, so typing appends (FR11).
func lineAt(s string) line { return line{s, utf8.RuneCountInString(s)} }

// apply edits l by one keypress: text and space insert at the cursor, backspace
// deletes the rune before it, delete the rune under it, ← / → move one rune,
// home / ctrl+a and end / ctrl+e jump. Any other key leaves l untouched.
func (l line) apply(msg tea.KeyMsg) line {
	r := []rune(l.text)
	switch msg.Type {
	case tea.KeyRunes, tea.KeySpace:
		ins := msg.Runes
		if msg.Type == tea.KeySpace {
			ins = []rune{' '}
		}
		r = slices.Insert(r, l.cur, ins...)
		l.cur += len(ins)
	case tea.KeyBackspace:
		if l.cur > 0 {
			r = slices.Delete(r, l.cur-1, l.cur)
			l.cur--
		}
	case tea.KeyDelete:
		if l.cur < len(r) {
			r = slices.Delete(r, l.cur, l.cur+1)
		}
	case tea.KeyLeft:
		l.cur = max(l.cur-1, 0)
	case tea.KeyRight:
		l.cur = min(l.cur+1, len(r))
	case tea.KeyHome, tea.KeyCtrlA:
		l.cur = 0
	case tea.KeyEnd, tea.KeyCtrlE:
		l.cur = len(r)
	}
	l.text = string(r)
	return l
}

// render draws the text with its cursor (FR12): a reverse-video cell on the rune
// under it, or the dim ▏ block at the end. It stays within w cells (down to w=3): when the text is
// longer it shows a window holding the cursor, with … for the clipped run on the left,
// the right or both (FR2–4). ponytail: one rune is one cell, wide runes may overshoot; at w≤2 with both sides clipped it
// overshoots to 3 cells (… + cursor + …), clamp if tiny widths ever matter.
func (l line) render(w int) string {
	r := []rune(l.text)
	n := len(r) // cells: the runes, plus the ▏ block when the cursor sits at the end
	if l.cur >= n {
		n++
	}
	w = max(w, 1)
	cell := func(i int) string {
		switch {
		case i == len(r):
			return dimSt.Render("▏")
		case i == l.cur:
			return cursorSty.Render(string(r[i]))
		}
		return string(r[i])
	}
	s, e := 0, n
	if n > w {
		s = min(max(l.cur-(w-1)/2, 0), n-w)
		e = s + w
	}
	left, right := s > 0, e < n
	lo, hi := s, e
	if left {
		lo++
	}
	if right {
		hi--
	}
	if c := max(hi-lo, 1); l.cur < lo {
		lo, hi = l.cur, l.cur+c
	} else if l.cur >= hi {
		lo, hi = l.cur+1-c, l.cur+1
	}
	var b strings.Builder
	if lo > 0 {
		b.WriteString("…")
	}
	for i := lo; i < hi; i++ {
		b.WriteString(cell(i))
	}
	if hi < n {
		b.WriteString("…")
	}
	return b.String()
}

// fitHeader builds a header line w wide around the input in: the input is the part
// that shrinks, by however far the rest of the line overflows w.
func fitHeader(w int, in line, build func(input string) string) string {
	full := in.render(utf8.RuneCountInString(in.text) + 1)
	h := build(full)
	if over := lipgloss.Width(h) - w; over > 0 {
		h = build(in.render(max(lipgloss.Width(full)-over, 1)))
	}
	return h
}
