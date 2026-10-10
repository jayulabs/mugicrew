package tui

import (
	"slices"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// line is a single-line text input with a movable cursor (Spec FR8–FR13):
// the text and the cursor's rune index into it (0 … rune count). Shared by every
// inline input — title, raw, reject reason, filter, the create form's fields and
// Search's text — so their keys don't drift.
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
// under it, or the dim ▏ block at the end.
func (l line) render() string {
	r := []rune(l.text)
	if l.cur >= len(r) {
		return l.text + dim.Render("▏")
	}
	return string(r[:l.cur]) + cursorSty.Render(string(r[l.cur])) + string(r[l.cur+1:])
}
