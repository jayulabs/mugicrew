package tui

import (
	"slices"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ctxMenu is the right-click menu (ADR 0372 §2–5): a pointable ? for one bead row.
type ctxMenu struct {
	x, y   int           // box top-left after flips and clamping
	bead   string        // the row's bead id
	items  []key.Binding // the row's verbs, in hint-rank order, then g goto › if the row has Goto options
	rules  int           // drawn lines that are not items: the rule above a trailing g
	cur    int           // the one highlight; pointer motion and j/k/↑/↓ move it
	mw, mh int           // box size
}

// The menu's priority step items (ADR 0412 §7): they sit where +/- prio ranks, name no P-value,
// and running one leaves the menu open.
var (
	keyMenuRaise = key.NewBinding(key.WithKeys("+", "="), key.WithHelp("+", "raise prio"))
	keyMenuLower = key.NewBinding(key.WithKeys("-", "_"), key.WithHelp("-", "lower prio"))
)

// isStepItem reports whether b is the menu's raise or lower item.
func isStepItem(b key.Binding) bool {
	return slices.Equal(b.Keys(), keyMenuRaise.Keys()) || slices.Equal(b.Keys(), keyMenuLower.Keys())
}

// hasStep reports whether any item is a step item.
func (c *ctxMenu) hasStep() bool { return slices.ContainsFunc(c.items, isStepItem) }

// stepItems are the raise and lower items for priority p: raise is left out at P0, lower at P4.
func stepItems(p int) []key.Binding {
	var out []key.Binding
	if p > minPriority {
		out = append(out, keyMenuRaise)
	}
	if p < maxPriority {
		out = append(out, keyMenuLower)
	}
	return out
}

// menuItems are the verbs of the selected row: the ? reference's "this row" section merged
// with the tab's detail toggle and every bead verb the row's state offers (ignoring the role's hinting cut, so the
// ?-only verbs of an MR or closed row are in), de-duplicated by key, in hint-rank order.
func (m Model) menuItems() []key.Binding {
	left, _ := m.refSections()
	items := slices.Clone(left[0].keys)
	// d is in the navigation section; only a d labelled detail or close is the drawer toggle
	// (Stats' d dist or a modal's d drop is not).
	if i := slices.IndexFunc(left[2].keys, func(b key.Binding) bool {
		h := b.Help()
		return h.Key == "d" && (h.Desc == "detail" || h.Desc == "close")
	}); i >= 0 {
		items = append(items, left[2].keys[i])
	}
	hinted, _ := beadVerbBindings(m.gateData(), m.rowBead(), roleFull)
	items = append(items, hinted...)
	var out []key.Binding
	seen := map[string]bool{}
	for _, b := range items {
		if len(b.Keys()) == 0 || slices.ContainsFunc(b.Keys(), func(k string) bool { return seen[k] }) {
			continue
		}
		for _, k := range b.Keys() {
			seen[k] = true
		}
		out = append(out, b)
	}
	sort.SliceStable(out, func(i, j int) bool { return hintOf(out[i]).rank < hintOf(out[j]).rank })
	// +/- prio gives way, in its rank slot, to the raise and lower items its bead's priority allows.
	rb := m.rowBead()
	if i := slices.IndexFunc(out, func(b key.Binding) bool {
		return rb != nil && slices.Equal(b.Keys(), keyPrio.Keys())
	}); i >= 0 {
		out = slices.Concat(out[:i:i], stepItems(rb.Priority), out[i+1:])
	}
	// g is appended after the sort, never sorted; the menu's own label marks it as another menu.
	if g := m.gotoBindings(); len(g) > 0 && !slices.ContainsFunc(out, func(b key.Binding) bool { return slices.Contains(b.Keys(), "g") }) {
		out = append(out, key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "goto ›")))
	}
	return out
}

// openMenu opens the menu for the selected bead row at pointer cell (px, py).
func (m Model) openMenu(px, py int) Model {
	b := m.rowBead()
	items := m.menuItems()
	if b == nil || len(items) == 0 {
		m.menu = nil
		return m
	}
	rules, mw, mh := menuSize(items)
	x, y := px, py
	if x+mw > m.w {
		x = px - mw + 1
	}
	if y+mh > m.h {
		y = py - mh + 1
	}
	m.menu = &ctxMenu{x: max(x, 0), y: max(y, 0), bead: b.ID, items: items, rules: rules, mw: mw, mh: mh}
	return m
}

// menuSize is the rule count and box size of items.
func menuSize(items []key.Binding) (rules, mw, mh int) {
	kw, lw := 0, 0
	for _, it := range items {
		kw = max(kw, lipgloss.Width(it.Help().Key))
		lw = max(lw, lipgloss.Width(it.Help().Desc))
	}
	if hasGoto(items) {
		rules = 1
	}
	return rules, kw + 2 + lw + 4, len(items) + rules + 2 // border ×2, padding ×2, two spaces between key and label
}

// hasGoto reports whether the last item is the trailing g goto ›, which sits below a rule.
func hasGoto(items []key.Binding) bool {
	return len(items) > 0 && items[len(items)-1].Help().Desc == "goto ›"
}

// ruleRow is the drawn row (inside the box) of the rule, -1 when there is none.
func (c *ctxMenu) ruleRow() int {
	if c.rules == 0 {
		return -1
	}
	return len(c.items) - 1
}

// itemAt is the item under screen cell (x, y), -1 off the items and on the rule.
func (c *ctxMenu) itemAt(x, y int) int {
	if x <= c.x || x >= c.x+c.mw-1 || y <= c.y || y >= c.y+c.mh-1 {
		return -1
	}
	r := y - c.y - 1
	switch rr := c.ruleRow(); {
	case r == rr:
		return -1
	case rr >= 0 && r > rr:
		return r - c.rules
	}
	return r
}

// view is the boxed menu, one string line per row.
func (c *ctxMenu) view() []string {
	kw := 0
	for _, it := range c.items {
		kw = max(kw, lipgloss.Width(it.Help().Key))
	}
	bd := lipgloss.NewStyle().Foreground(accent)
	iw := c.mw - 2
	ls := []string{bd.Render("╭" + strings.Repeat("─", iw) + "╮")}
	for i, it := range c.items {
		if i == c.ruleRow() {
			ls = append(ls, bd.Render("├"+strings.Repeat("─", iw)+"┤"))
		}
		h := it.Help()
		pad := strings.Repeat(" ", kw-lipgloss.Width(h.Key))
		var body string
		if i == c.cur {
			body = selRow.Render(" " + h.Key + pad + "  " + h.Desc + strings.Repeat(" ", iw-4-kw-lipgloss.Width(h.Desc)) + " ")
		} else {
			body = " " + hintHelp.Styles.FullKey.Inline(true).Render(h.Key) + pad + "  " + hintHelp.Styles.FullDesc.Inline(true).Render(h.Desc) + strings.Repeat(" ", iw-4-kw-lipgloss.Width(h.Desc)) + " "
		}
		ls = append(ls, bd.Render("│")+body+bd.Render("│"))
	}
	return append(ls, bd.Render("╰"+strings.Repeat("─", iw)+"╯"))
}

// paintMenu overlays the open menu on a drawn frame, topmost.
func (m Model) paintMenu(frame string) string {
	if m.menu == nil || !m.mouseLive() {
		return frame
	}
	lines := strings.Split(frame, "\n")
	for i, box := range m.menu.view() {
		y := m.menu.y + i
		if y >= len(lines) {
			break
		}
		l := lines[y]
		left := ansi.Cut(l, 0, m.menu.x)
		left += strings.Repeat(" ", max(m.menu.x-lipgloss.Width(left), 0))
		lines[y] = left + "\x1b[0m" + box + "\x1b[0m" + ansi.Cut(l, m.menu.x+m.menu.mw, m.w)
	}
	return strings.Join(lines, "\n")
}

// menuSettle closes the menu once its bead is gone from the snapshot or the screen can no longer show it.
// A menu with step items re-reads its items for the bead's priority, keeping its position and
// clamping the highlight; the list selection follows the bead by ID, so a re-sort moves neither.
func (m Model) menuSettle() Model {
	if m.menu == nil {
		return m
	}
	if !m.mouseLive() || m.gateData().beadIndex(m.menu.bead) < 0 {
		m.menu = nil
		return m
	}
	if !m.menu.hasStep() {
		return m
	}
	if b := m.rowBead(); b == nil || b.ID != m.menu.bead {
		return m
	}
	c := *m.menu
	c.items = m.menuItems()
	if len(c.items) == 0 {
		m.menu = nil
		return m
	}
	c.rules, c.mw, c.mh = menuSize(c.items)
	c.cur = min(c.cur, len(c.items)-1)
	c.y = max(min(c.y, m.h-c.mh), 0)
	c.x = max(min(c.x, m.w-c.mw), 0)
	m.menu = &c
	return m
}

// keyMsgOf is the key press a binding's key string stands for.
func keyMsgOf(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "ctrl+o":
		return tea.KeyMsg{Type: tea.KeyCtrlO}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// menuRun closes the menu and presses item i's key through the shell's normal key path, so the
// bead-verb router and its gates answer exactly as for the key.
//
// A step item (ADR 0412 §7) reinstates the same menu afterwards, highlight on the same index; the
// settle then re-reads its items for the bead's new priority.
func (m Model) menuRun(i int) (tea.Model, tea.Cmd) {
	k := keyMsgOf(m.menu.items[i].Keys()[0])
	c := *m.menu
	c.cur = i
	m.menu = nil
	next, cmd := m.keyRecording(k)
	if !isStepItem(c.items[i]) {
		return next, cmd
	}
	if nm, ok := next.(Model); ok && nm.menu == nil {
		nm.menu = &c
		return nm, cmd
	}
	return next, cmd
}

// menuKey is a key while the menu is open: j/k/↑/↓ move the highlight, enter runs it, a
// listed key runs its verb, anything else closes the menu and does nothing.
func (m Model) menuKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	c := *m.menu
	switch k.String() {
	case "j", "down":
		c.cur = min(c.cur+1, len(c.items)-1)
		m.menu = &c
		return m, nil
	case "k", "up":
		c.cur = max(c.cur-1, 0)
		m.menu = &c
		return m, nil
	case "enter":
		return m.menuRun(c.cur)
	}
	for i, it := range c.items {
		if slices.Contains(it.Keys(), k.String()) {
			return m.menuRun(i)
		}
	}
	m.menu = nil
	return m, nil
}

// menuMouse is a mouse event while the menu is open: motion over an item moves the highlight,
// a left press runs an item or closes the menu (consumed), a right press on another bead row
// moves the menu there and anywhere else closes it.
func (m Model) menuMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	c := *m.menu
	i := c.itemAt(msg.X, msg.Y)
	switch {
	case msg.Action == tea.MouseActionMotion:
		if i >= 0 && i != c.cur {
			c.cur = i
			m.menu = &c
		}
	case msg.Action != tea.MouseActionPress:
	case msg.Button == tea.MouseButtonLeft:
		if i >= 0 {
			return m.menuRun(i)
		}
		m.menu = nil
	case msg.Button == tea.MouseButtonRight:
		m.menu = nil
		return m.rightPress(msg)
	}
	return m, nil
}

// rightPress selects the bead row under the pointer, keeping a scrolled list scrolled, and
// opens the menu there; any other stop does nothing.
func (m Model) rightPress(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	y, ok := m.bodyLine(msg.Y)
	if !ok {
		return m, nil
	}
	var h hitTarget
	if m.focus.id != "" {
		h = m.focusHit(m.w, m.h-chromeH+1, y)
		if h.kind != hitRow || !h.bead {
			return m, nil
		}
		m = m.focusSelectKey(h.key)
		return m.openMenu(msg.X, msg.Y), nil
	}
	p, isP := m.tabAt(m.tab).(pointable)
	if !isP {
		return m, nil
	}
	h = p.hit(m.w, m.h-chromeH, y)
	if h.kind != hitHeader && (h.kind != hitRow || !h.bead) {
		return m, nil
	}
	sel := m
	sel.tabs = slices.Clone(m.tabs)
	sel.tabs[m.tab] = p.selectKey(h.key)
	sel = sel.takeBackSettings()
	sel.click = lastClick{}
	// A header keeps the selection only if it resolves to a bead with verbs.
	if h.kind == hitHeader && (sel.rowBead() == nil || len(sel.menuItems()) == 0) {
		return m, nil
	}
	return sel.openMenu(msg.X, msg.Y), nil
}
