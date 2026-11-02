package tui

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// modalLive is whether the mouse acts on the top modal when it is a picker, the Loop menu,
// a confirm dialog or the X withdraw dialog: the same capture, ? reference and floor
// conditions as formLive.
func (m Model) modalLive() bool {
	if !m.modalCaptured() {
		return false
	}
	switch m.modals[len(m.modals)-1].kind {
	case picker, loopMenu, confirm, xdialog:
		return true
	}
	return false
}

// modalMouse is the live modal's mouse path. In a picker, motion over an item moves the
// highlight (cur, and id when ids is set) and a left press on an item runs it as enter
// does. In the Loop menu motion hovers an option and a press on one sends its key. A left
// press outside the box dismisses the modal as its dismiss key does (esc, or a confirm's
// no-binding), through the modal key path so only the top modal pops; a press inside a
// confirm presses the hint under it, if any. Every other event is consumed.
func (m Model) modalMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	d := m.modals[len(m.modals)-1]
	if d.kind == xdialog {
		return m.xMouse(d, msg)
	}
	if msg.Action == tea.MouseActionMotion && d.kind == loopMenu {
		m.hover = m.loopHover(msg.X, msg.Y)
		return m, nil
	}
	if msg.Action == tea.MouseActionMotion && d.kind == confirm {
		m.hover = m.hintHover(msg.X, msg.Y)
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
		if d.kind == confirm {
			if sp, ok := m.hintAt(msg.X, msg.Y); ok {
				return m.sendKey(keyMsgOf(sp.key))
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

// hintHover is the hover for a pointer at (x, y) over a dialog's hint line.
func (m Model) hintHover(x, y int) hover {
	sp, ok := m.hintAt(x, y)
	if !ok { // nothing shades off a hint, so the pointer's row is no part of the hover
		return hover{}
	}
	return hover{set: true, x: x, y: y, opt: true, optSpan: sp}
}

// paintModalHover shades the hovered Loop menu option's or dialog hint's own text.
func (m Model) paintModalHover(frame string) string {
	if !m.hover.set || !m.hover.opt || !(m.modalLive() || m.formLive()) {
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

// xRowAt is the tree row of the X dialog d's drawn line at screen cell (x, y), or -1 when
// that cell is not on a drawn row (a chain, in the cancel dialog): the lead, summary,
// counter, warnings, hints, blank lines, the locked reason line and the border are inert.
func (m Model) xRowAt(d dialog, x, y int) int {
	lay := m.dialogLayout(d)
	if x <= lay.x || x >= lay.x+lay.w-1 {
		return -1
	}
	if f, ok := lay.fieldAt(y); ok {
		return f.field
	}
	return -1
}

// xMouse is the X dialog's mouse path. Motion over a tree row selects it as j/k does and
// over a hint hovers it; a left press on a row selects it, on a hint presses its key, and
// outside the box sends esc through the modal key path. Every other event is consumed.
func (m Model) xMouse(d dialog, msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action == tea.MouseActionMotion {
		m.hover = m.hintHover(msg.X, msg.Y)
		if i := m.xRowAt(d, msg.X, msg.Y); i >= 0 && !(d.xs.cancel != nil && d.xs.cancel.whyOn) { // j/k would type into the why line
			m = m.aimX(d, i, false)
		}
		return m, nil
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	lay := m.dialogLayout(d)
	if msg.X < lay.x || msg.X >= lay.x+lay.w || msg.Y < lay.y || msg.Y >= lay.y+lay.h {
		return m.sendKey(tea.KeyMsg{Type: tea.KeyEsc})
	}
	if sp, ok := m.hintAt(msg.X, msg.Y); ok {
		return m.sendKey(keyMsgOf(sp.key))
	}
	if d.xs.cancel != nil {
		if next, cmd, ok := m.cancelPress(d, lay, msg); ok {
			return next, cmd
		}
	}
	if i := m.xRowAt(d, msg.X, msg.Y); i >= 0 {
		return m.aimX(d, i, true), nil
	}
	return m, nil
}

// cancelPress is a left press inside the cancel dialog's box that is on its why line or a
// chain's close or release label; ok is false elsewhere. The why line is focused as tab
// does, and a label selects its chain and presses c or r, so a locked release flashes.
func (m Model) cancelPress(d dialog, lay formLayout, msg tea.MouseMsg) (tea.Model, tea.Cmd, bool) {
	_, hits := m.cancelBox(d)
	row, col := msg.Y-lay.y-1, msg.X-lay.x-loopOptPad
	if row == hits.why && !d.xs.cancel.whyOn {
		next, cmd := m.sendKey(tea.KeyMsg{Type: tea.KeyTab})
		return next, cmd, true
	}
	for _, c := range hits.choices {
		if c.row != row {
			continue
		}
		k := ""
		switch {
		case col >= c.closeX0 && col < c.closeX1:
			k = "c"
		case col >= c.relX0 && col < c.relX1:
			k = "r"
		default:
			return m, nil, false
		}
		next, cmd := m.aimX(d, c.chain, true).sendKey(keyMsgOf(k))
		return next, cmd, true
	}
	return m, nil, false
}

// aimX moves the top X dialog's selection to row i, as j/k does. In the cancel dialog a
// change of row clears the flash, and a press (focus) also returns focus from the why line.
func (m Model) aimX(d dialog, i int, press bool) Model {
	if cs := d.xs.cancel; cs != nil && (i != d.cur || press && cs.whyOn) {
		c := *cs
		c.flash = ""
		if press {
			c.whyOn = false
		}
		d.xs.cancel = &c
	}
	d.cur = i
	m.modals = slices.Clone(m.modals)
	m.modals[len(m.modals)-1] = d
	return m
}
