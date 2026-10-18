package tui

// The goto menu (ADR 0228): g opens a picker of context moves, each fired by its own key.

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
)

var keyGoto = verbKey("g", "goto")

// gotoOpt is one menu row: the keys that fire it, what it does, and the move itself.
type gotoOpt struct {
	keys []string
	desc string
	row  string // the row's drawn text when it is not desc (a related bead); desc stays its hint
	run  func(Model) (Model, tea.Cmd)
}

func (o gotoOpt) binding() key.Binding {
	return key.NewBinding(key.WithKeys(o.keys...), key.WithHelp(strings.Join(o.keys, "/"), o.desc))
}

func optBindings(opts []gotoOpt) []key.Binding {
	var bs []key.Binding
	for _, o := range opts {
		if len(o.keys) > 0 { // a related row past the ninth has no key
			bs = append(bs, o.binding())
		}
	}
	return bs
}

// edger is a tab whose list has a first and a last selectable row: edge moves the cursor
// there, false when the list has none.
type edger interface {
	edge(last bool) (component, bool)
}

// edgeIdx is the index of the first (or last) row ok accepts, -1 for none.
func edgeIdx[R any](rows []R, last bool, ok func(R) bool) int {
	for n := range rows {
		i := n
		if last {
			i = len(rows) - 1 - n
		}
		if ok(rows[i]) {
			return i
		}
	}
	return -1
}

// tabEdge moves the active list's cursor (the focus view's, when open) to its first or
// last selectable row.
func (m Model) tabEdge(last bool) (Model, bool) {
	if m.focus.id != "" {
		rows := m.focusRows()
		i := edgeIdx(rows, last, func(r fRow) bool { return r.kind != fGap })
		if i < 0 {
			return m, false
		}
		m.focus.sel, m.focus.off = rows[i].key, 0
		return m, true
	}
	e, ok := m.tabAt(m.tab).(edger)
	if !ok {
		return m, false
	}
	c, ok := e.edge(last)
	if !ok {
		return m, false
	}
	m.tabs = slices.Clone(m.tabs)
	m.tabs[m.tab] = c
	return m, true
}

// gotoOpts builds the menu's rows for the current cursor, leaving out what does not apply.
func (m Model) gotoOpts() (opts []gotoOpt) {
	for _, e := range []struct {
		keys []string
		desc string
		last bool
	}{{[]string{"g", "t"}, "top", false}, {[]string{"G", "b"}, "bottom", true}} {
		if _, ok := m.tabEdge(e.last); ok {
			opts = append(opts, gotoOpt{e.keys, e.desc, "", func(m Model) (Model, tea.Cmd) { m, _ = m.tabEdge(e.last); return m, nil }})
		}
	}
	if m.focus.id == "" && m.resolveInitiative() != nil { // no nesting (ADR 0174 §1)
		opts = append(opts, gotoOpt{[]string{"f"}, "focus", "", func(m Model) (Model, tea.Cmd) {
			next, cmd := m.focusOpen()
			return next.(Model), cmd
		}})
	}
	b := m.selection()
	if b == nil {
		return opts
	}
	id, kids := b.ID, b.Container
	if m.tab != tabGraph {
		opts = append(opts, gotoOpt{[]string{"r"}, "reveal", "", func(m Model) (Model, tea.Cmd) {
			return m, func() tea.Msg { return revealMsg{id} }
		}})
	}
	opts = append(opts, gotoOpt{[]string{"l"}, "logs", "", func(m Model) (Model, tea.Cmd) {
		return m, func() tea.Msg { return scopeLogsMsg{id, kids} }
	}})
	jump := func(id string) func(Model) (Model, tea.Cmd) {
		return func(m Model) (Model, tea.Cmd) { return m.recentsJump(id) }
	}
	return append(opts, m.beadOpts(b, jump)...)
}

// beadOpts is the bead rows of b: p parent, c cause and o origin (when they resolve in the
// snapshot), then the one-hop related rows (ADR 0228 D8): grouped, open before closed, snapshot order,
// each bead once, the first nine keyed 1–9. All are drawn alike, in shared columns.
func (m Model) beadOpts(b *bd.Bead, jump func(string) func(Model) (Model, tea.Cmd)) []gotoOpt {
	groups := []struct {
		tag string
		has func(o *bd.Bead) bool
	}{
		{"blocked-by", func(o *bd.Bead) bool { return slices.Contains(b.BlockedBy, o.ID) }},
		{"blocks", func(o *bd.Bead) bool { return slices.Contains(o.BlockedBy, b.ID) }},
		{"child", func(o *bd.Bead) bool { return o.Parent == b.ID }},
		{"successor", func(o *bd.Bead) bool { return o.CausedBy == b.ID }},
		{"offshoot", func(o *bd.Bead) bool { return slices.Contains(o.DiscoveredFrom, b.ID) }},
		{"related", func(o *bd.Bead) bool { return slices.Contains(b.Related, o.ID) || slices.Contains(o.Related, b.ID) }},
	}
	var rows []*bd.Bead
	var tags []string
	var lead []string // keys of the leading p, c and o rows
	for _, l := range []struct{ key, tag, id string }{{"p", "parent", b.Parent}, {"c", "cause", b.CausedBy}} {
		if i := beadIndex(m.beads, l.id); l.id != "" && i >= 0 {
			rows, tags, lead = append(rows, &m.beads[i]), append(tags, l.tag), append(lead, l.key)
		}
	}
	seen := map[string]bool{b.ID: true, b.Parent: true, b.CausedBy: true} // p and c are not repeated
	// discovered-from sources in edge order: the first that resolves is o, the rest the origin
	// group, which keeps edge order (not open before closed)
	var origins []*bd.Bead
	for _, id := range b.DiscoveredFrom {
		if i := beadIndex(m.beads, id); i >= 0 && !seen[id] {
			seen[id] = true
			origins = append(origins, &m.beads[i])
		}
	}
	if len(origins) > 0 {
		rows, tags, lead = append(rows, origins[0]), append(tags, "origin"), append(lead, "o")
		for _, o := range origins[1:] {
			rows, tags = append(rows, o), append(tags, "origin")
		}
	}
	for _, g := range groups {
		for _, closed := range []bool{false, true} {
			for i := range m.beads {
				o := &m.beads[i]
				if !seen[o.ID] && (o.Status == "closed") == closed && g.has(o) {
					seen[o.ID] = true
					rows, tags = append(rows, o), append(tags, g.tag)
				}
			}
		}
	}
	tagW, relW := 0, 0
	for i, o := range rows {
		tagW, relW = max(tagW, lipgloss.Width(kindTag(o))), max(relW, len(tags[i]))
	}
	gd, inner := m.gateData(), m.pickerInner()
	var opts []gotoOpt
	for i, o := range rows {
		front := fmt.Sprintf("%-*s  ", relW, tags[i])
		fixed := lipgloss.Width(gd.sbrn(o, "", false, tagW)) + len(front) + 4 // the key column
		row := front + gd.sbrn(o, ansi.Truncate(o.Title, max(inner-fixed, 1), "…"), false, tagW)
		if o.Status == "closed" {
			row = dimSt.Render(ansi.Strip(row))
		}
		opt := gotoOpt{desc: tags[i], row: row, run: jump(o.ID)}
		if n := i - len(lead); n < 0 {
			opt.keys = []string{lead[i]}
		} else if n < 9 {
			opt.keys = []string{strconv.Itoa(n + 1)}
		}
		opts = append(opts, opt)
	}
	return opts
}

// gotoBindings is g when it would open the menu, for the hints bar and binding coverage.
func (m Model) gotoBindings() []key.Binding {
	if len(m.gotoOpts()) == 0 {
		return nil
	}
	return []key.Binding{keyGoto}
}

// gotoOpen is g: the menu over the frame, or nothing when no option applies.
func (m Model) gotoOpen() (tea.Model, tea.Cmd) {
	next, ok := m.gotoMenu()
	if !ok {
		return m, nil
	}
	next.gotoGen++ // an older menu's fetch landing now is stale
	return next, next.gotoFetch()
}

// gotoMenu pushes the menu over the current snapshot; neighbours that stream in later
// rebuild it (foldDemand).
func (m Model) gotoMenu() (Model, bool) {
	opts := m.gotoOpts()
	if len(opts) == 0 {
		return m, false
	}
	items := make([]string, len(opts))
	rowKeys := make([][]string, len(opts))
	for i, o := range opts {
		text := cmp.Or(o.row, o.desc)
		items[i], rowKeys[i] = fmt.Sprintf("%-4s", strings.Join(o.keys, "/"))+text, o.keys
	}
	d := pickerDialog("Goto", items, "", func(m Model, d dialog) (Model, tea.Cmd) { return opts[d.cur].run(m) })
	d.gotoMenu, d.rowKeys, d.opts = true, rowKeys, optBindings(opts)
	return m.push(d), true
}

// The tabs' edges: the same rows j/k land on, the detail drawer back at its top.

func (t ideaTab) edge(last bool) (component, bool) {
	rows := t.rows()
	i := edgeIdx(rows, last, ideaRow.selectable)
	if i >= 0 {
		t.sel, t.off = rows[i].key, 0
	}
	return t, i >= 0
}

func (t defogTab) edge(last bool) (component, bool) {
	rows := t.rows()
	i := edgeIdx(rows, last, dRow.selectable)
	if i >= 0 {
		t.sel, t.off = rows[i].key, 0
	}
	return t, i >= 0
}

func (t execTab) edge(last bool) (component, bool) {
	rows := t.rows()
	i := edgeIdx(rows, last, eRow.selectable)
	if i >= 0 {
		t.sel, t.off = rows[i].key, 0
	}
	return t, i >= 0
}

func (t initsTab) edge(last bool) (component, bool) {
	rows := t.rows()
	i := edgeIdx(rows, last, func(iRow) bool { return true })
	if i >= 0 {
		t.sel, t.off = rows[i].key, 0
	}
	return t, i >= 0
}

func (g graphTab) edge(last bool) (component, bool) {
	rows := g.rows()
	i := edgeIdx(rows, last, func(row) bool { return true })
	if i >= 0 {
		g.sel, g.off, g.want = rows[i].sel, 0, ""
	}
	return g, i >= 0
}

func (t histTab) edge(last bool) (component, bool) {
	rows := t.rows()
	i := edgeIdx(rows, last, func(histRow) bool { return true })
	if i >= 0 {
		t.sel = rows[i].sha
	}
	return t, i >= 0
}

func (l logsTab) edge(last bool) (component, bool) {
	rows := l.rows()
	if len(rows) == 0 {
		return l, false
	}
	i := 0
	if last {
		i = len(rows) - 1
	}
	l.sel, l.off = l.ev(rows[i]).Key(), 0
	return l, true
}
