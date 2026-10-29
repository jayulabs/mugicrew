package tui

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// modalLive is whether the mouse acts on the top modal when it is a picker, the Loop menu
// or a confirm dialog: the same capture, ? reference and floor conditions as formLive. The
// X withdraw dialog falls under neither and stays keyboard-only.
func (m Model) modalLive() bool {
	if !m.modalCaptured() {
		return false
	}
	switch m.modals[len(m.modals)-1].kind {
	case picker, loopMenu, confirm:
		return true
	}
	return false
}

// modalMouse is the live modal's mouse path. In a picker, motion over an item moves the
// highlight (cur, and id when ids is set) and a left press on an item runs it as enter
// does. In the Loop menu motion hovers an option and a press on one sends its key. A left
// press outside the box dismisses the modal as its dismiss key does (esc, or a confirm's
// no-binding), through the modal key path so only the top modal pops; a press inside a
// confirm does nothing. Every other event is consumed.
func (m Model) modalMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	d := m.modals[len(m.modals)-1]
	if msg.Action == tea.MouseActionMotion && d.kind == loopMenu {
		m.hover = m.loopHover(msg.X, msg.Y)
		return m, nil
	}
	if d.kind == picker {
		if i := m.pickerItemAt(d, msg.X, msg.Y); i >= 0 {
			switch {
			case msg.Action == tea.MouseActionMotion:
				return m.aimPicker(d, i), nil
			case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
				return m.aimPicker(d, i).sendKey(tea.KeyMsg{Type: tea.KeyEnter})
			}
		}
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	lay := m.dialogLayout(d)
	if msg.X >= lay.x && msg.X < lay.x+lay.w && msg.Y >= lay.y && msg.Y < lay.y+lay.h {
		if d.kind == loopMenu {
			if sp, ok := m.loopOptAt(msg.X, msg.Y); ok {
				return m.sendKey(sp.keyMsg())
			}
		}
		return m, nil
	}
	k := tea.KeyMsg{Type: tea.KeyEsc}
	if d.kind == confirm {
		if ks := d.noBinding().Keys(); len(ks) > 0 && ks[0] != "esc" {
			k = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(ks[0])}
		}
	}
	return m.sendKey(k)
}

// keyMsg is the key the option presses.
func (sp loopOptCells) keyMsg() tea.KeyMsg {
	if sp.key == "esc" {
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(sp.key)}
}

// loopOptAt is the Loop menu option under screen cell (x, y), in screen columns. The
// options line is the box's last content rows, however many it wraps to.
func (m Model) loopOptAt(x, y int) (loopOptCells, bool) {
	if len(m.modals) == 0 || m.modals[len(m.modals)-1].kind != loopMenu {
		return loopOptCells{}, false
	}
	lay := m.dialogLayout(m.modals[len(m.modals)-1])
	line, spans := m.loopOptsLineSpans(m.loopOptsInner())
	first := lay.y + lay.h - 1 - (strings.Count(line, "\n") + 1)
	off := lay.x + loopOptPad
	for _, sp := range spans {
		if y == first+sp.row && x >= off+sp.x0 && x < off+sp.x1 {
			return loopOptCells{off + sp.x0, off + sp.x1, sp.key}, true
		}
	}
	return loopOptCells{}, false
}

// loopHover is the hover for a pointer at (x, y) over the Loop menu.
func (m Model) loopHover(x, y int) hover {
	h := hover{set: true, x: x, y: y}
	if sp, ok := m.loopOptAt(x, y); ok {
		h.opt, h.optSpan = true, sp
	}
	return h
}

// paintModalHover shades the hovered Loop menu option's own text.
func (m Model) paintModalHover(frame string) string {
	if !m.hover.set || !m.hover.opt || !m.modalLive() {
		return frame
	}
	sp := m.hover.optSpan
	lines := strings.Split(frame, "\n")
	if m.hover.y < 0 || m.hover.y >= len(lines) {
		return frame
	}
	l := lines[m.hover.y]
	lines[m.hover.y] = ansi.Cut(l, 0, sp.x0) + shadeRow(ansi.Cut(l, sp.x0, sp.x1), sp.x1-sp.x0, hoverRow) + ansi.Cut(l, sp.x1, m.w)
	return strings.Join(lines, "\n")
}

// aimPicker moves the top picker's highlight to item i, as j/k does: cur, and id from ids.
func (m Model) aimPicker(d dialog, i int) Model {
	d.cur = i
	if d.ids != nil {
		d.id = d.ids[i]
	}
	m.modals = slices.Clone(m.modals)
	m.modals[len(m.modals)-1] = d
	return m
}

// pickerItemAt is the item index of the picker d's drawn line at screen cell (x, y), or -1
// when that cell is not on an item row: a heading, the blank lines, the k/N counter, the
// title, the hints and the border are all inert. The item row's shaded padding columns
// count as the row. The rows come from boxOf, so only the drawn items answer.
func (m Model) pickerItemAt(d dialog, x, y int) int {
	lay := m.dialogLayout(d)
	if x <= lay.x || x >= lay.x+lay.w-1 {
		return -1
	}
	if f, ok := lay.fieldAt(y); ok {
		return f.field
	}
	return -1
}
