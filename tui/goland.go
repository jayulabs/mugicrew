package tui

// The goto landing (Spec "Goto bead rows and reveal land on Graph scoped to the bead's
// group head", Decisions 5–7): every goto bead move switches to Graph scoped to the
// target's group head, the subtree read running off the event loop (ADR 0098).

import (
	"fmt"
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
)

// bestHead is the group head of bead id as far as the loaded beads show: its grandparent
// when the parent is loaded and the grandparent is a loaded container, else the parent when it is a container, else the
// bead itself. The subtree read corrects it when it lands.
func (m Model) bestHead(id string) (head string, chain []string) {
	i := beadIndex(m.beads, id)
	if i < 0 {
		return id, nil
	}
	p := m.beads[i].Parent
	pi := beadIndex(m.beads, p)
	if p == "" || pi < 0 {
		return id, nil
	}
	if gp := m.beads[pi].Parent; gp != "" {
		if gi := beadIndex(m.beads, gp); gi >= 0 && m.beads[gi].Container {
			return gp, []string{p, gp}
		}
		if m.beads[pi].Container {
			return p, []string{p}
		}
		return id, []string{p}
	}
	if m.beads[pi].Container {
		return p, []string{p}
	}
	return id, []string{p}
}

// gotoLand lands on bead id: Graph scoped to id's group head with id selected, a
// provisional scope at once and the true head once the subtree read folds in.
func (m Model) gotoLand(id string) (Model, tea.Cmd) {
	m.demandGen++
	gen := m.demandGen
	m.focus = focus{}
	prev := m.tabAt(tabGraph).(graphTab)
	undo := m.graphUndo()
	head, chain := m.bestHead(id)
	m = m.seedSubtree(append([]string{id}, chain...)...)
	g := prev.rebuild().scopeToPending(head, id) // Graph may be stale off-tab
	m.searched = g.q
	m.searchGen++ // an in-flight word search is stale
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabGraph], m.tab = g, tabGraph
	m.setStatus("fetching " + head + "'s group…")
	snap := make(map[string]bd.Bead, len(m.beads))
	for _, b := range m.beads {
		snap[b.ID] = b
	}
	fetch := func() tea.Msg {
		st, ok, err := listSubtree(id, snap)
		return demandMsg{gen: gen, land: true, id: id, prov: head, st: st, stOK: ok, err: err, undo: undo}
	}
	return m, fetch
}

// foldLanding takes the subtree read of a goto landing: the true head becomes the scope
// and the landing note replaces the status line. A cursor move during the fetch only
// stops the cursor being pulled back to the landed bead; a query or typing change keeps
// its words, facets and input, and only the scope moves to the true head.
func (m Model) foldLanding(msg demandMsg) (Model, tea.Cmd) {
	g := m.tabAt(tabGraph).(graphTab)
	if g.q.scope != msg.prov { // esc or a newer scope took it over
		return m, nil
	}
	setGraph := func(g graphTab) Model {
		m.tabs = slices.Clone(m.tabs)
		m.tabs[tabGraph] = g
		return m
	}
	switch {
	case msg.err != nil:
		g.searching, g.want = false, ""
		m = setGraph(g)
		m.setStatus("fetch failed: " + msg.err.Error() + "; showing the loaded part of " + msg.prov + " (esc still clears)")
		return m, nil
	case !msg.stOK:
		m = msg.undo(m)
		m.setStatus(msg.id + ": not in the graph")
		return m, nil
	}
	m = m.fillSubtree(msg.st.IDs...)
	m = m.foldBeads(msg.st.Beads)
	cursor, moved := g.sel, g.landMoved
	if narrowed := g.q != g.scopedTo || g.typing; narrowed {
		// The human narrowed during the fetch: keep it, move only the scope (FR19, FR33).
		g = m.tabAt(tabGraph).(graphTab) // over the beads just folded in
		if g.scopedTo != (query{}) {
			g.scopedTo.scope = msg.st.Head
		}
		if g.qfoldFor.scope == msg.prov {
			g.qfoldFor.scope = msg.st.Head
		}
		g.q.scope = msg.st.Head
		g = g.rebuild()
	} else {
		g = m.tabAt(tabGraph).(graphTab).rebuild() // over the beads just folded in
		g.landMoved = moved
		g = g.scopeTo(msg.st.Head, msg.id)
		if moved && indexRow(g.rows(), cursor) >= 0 {
			g.sel = cursor
		}
	}
	g.searching, g.want = false, ""
	m = setGraph(g)
	note := "scoped Graph to " + msg.st.Head + " (esc clears)"
	if msg.st.Self {
		note = "scoped Graph to " + msg.id + ", no group (esc clears)"
	}
	if n := len(msg.st.Strays); n > 0 { // the depth guard: a third nesting level is cut off
		note += fmt.Sprintf("; %d beads nest deeper than the scope reads", n)
	}
	m.setStatus(note)
	return m, nil
}
