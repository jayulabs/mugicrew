package tui

// filterClicker is an optional tab interface: a tab whose filter line takes a click while
// it is being edited. x is the screen column in a w wide body; the tab returns itself with
// the cursor moved, or unchanged when it isn't capturing.
type filterClicker interface {
	filterAt(w, x int) component
}

// filterAt puts the / input's cursor on the rune drawn at screen column x of the filter line.
func (g graphTab) filterAt(w, x int) component {
	if g.typing {
		g.edit = inputAt(w-1, x-1, g.edit, g.headerWith) // view draws the header after a space
	}
	return g
}

// filterAt puts the / input's cursor on the rune drawn at screen column x of the filter line.
func (l logsTab) filterAt(w, x int) component {
	if l.typing {
		l.edit = inputAt(w-1, x-1, l.edit, l.headerWith)
	}
	return l
}

// filterClick is a left press on body line y of the active tab: while its filter line is
// edited, a press on that line moves the cursor. ok is false when the press isn't one.
func (m Model) filterClick(x, y int) (Model, bool) {
	if y != 0 || m.focus.id != "" || !m.capturing() {
		return m, false
	}
	f, ok := m.tabAt(m.tab).(filterClicker)
	if !ok {
		return m, false
	}
	m.tabs = append([]component(nil), m.tabs...)
	m.tabs[m.tab] = f.filterAt(m.w, x)
	return m, true
}
