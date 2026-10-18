package tui

// On-demand closed beads (ADR 0232): G, a Recents jump and an open Goto menu fetch the
// beads they need off the event loop and keep them loaded while a claim names them.

import (
	"maps"
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
)

var (
	listWithAncestors = bd.ListWithAncestors
	listChildren      = bd.ListChildren
	listDependents    = bd.DependentIDs
)

// demandMsg is a fetch's result. A reveal fetch carries the generation that started it
// and what to do once the beads are in; a Goto fetch has neither.
type demandMsg struct {
	forGoto bool // a Goto fetch, else a reveal
	gen     int  // the generation that started it: gotoGen for Goto, demandGen for a reveal
	beads   []bd.Bead
	err     error
	id      string            // a reveal fetch's bead
	undo    func(Model) Model // puts back the Graph query and tab a G scoped, when bd has no such bead
	then    func(Model, string) (Model, tea.Cmd)
}

// fetchReveal fetches id with its ancestors, then runs then. G (scope) shows Graph scoped
// to id at once with its cursor remembered, so the cursor lands on the bead when it
// arrives; a Recents jump leaves Graph alone.
func (m Model) fetchReveal(id string, scope bool, then func(Model, string) (Model, tea.Cmd)) (Model, tea.Cmd) {
	m.demandGen++
	gen := m.demandGen
	var undo func(Model) Model
	if scope {
		g, tab := m.tabAt(tabGraph).(graphTab), m.tab
		prev := g
		undo = func(m Model) Model {
			m.tabs = slices.Clone(m.tabs)
			m.tabs[tabGraph], m.tab = prev, tab
			return m
		}
		g.q, g.typing, g.visit, g.searching, g.want = query{words: id, status: statusAny}, false, false, true, id
		m.searched = g.q // a scoped reveal is not a word search
		m.searchGen++    // an in-flight word search for the old words is stale
		m.tabs = slices.Clone(m.tabs)
		m.tabs[tabGraph], m.tab = g.rebuild(), tabGraph
	}
	m.setStatus("fetching " + id + "…")
	return m, func() tea.Msg {
		beads, err := listWithAncestors(id)
		return demandMsg{gen: gen, beads: beads, err: err, id: id, then: then, undo: undo}
	}
}

// gotoFetch brings in the selected bead's one-hop neighbours that are not loaded: the
// beads its own edges name, its children, and the beads that point at it (blocks,
// successor).
func (m Model) gotoFetch() tea.Cmd {
	b := m.selection()
	if b == nil {
		return nil
	}
	var need []string
	for _, id := range slices.Concat([]string{b.Parent, b.CausedBy}, b.BlockedBy, b.Related, b.DiscoveredFrom) {
		if id != "" && beadIndex(m.beads, id) < 0 && !slices.Contains(need, id) {
			need = append(need, id)
		}
	}
	id, kids := b.ID, b.Container
	loaded := make(map[string]bool, len(m.beads))
	for _, x := range m.beads {
		loaded[x.ID] = true
	}
	gen := m.gotoGen
	return func() tea.Msg {
		deps, err := listDependents(id)
		for _, d := range deps {
			if !loaded[d] && !slices.Contains(need, d) {
				need = append(need, d)
			}
		}
		var got []bd.Bead
		if err == nil {
			got, err = listByID(need)
		}
		if err == nil && kids {
			var ch []bd.Bead
			ch, err = listChildren(id)
			got = append(got, ch...)
		}
		return demandMsg{forGoto: true, gen: gen, beads: got, err: err}
	}
}

// foldDemand takes a fetch's beads into the snapshot. A fetch for a stale
// generation is dropped.
func (m Model) foldDemand(msg demandMsg) (Model, tea.Cmd) {
	if (!msg.forGoto && msg.gen != m.demandGen) || (msg.forGoto && msg.gen != m.gotoGen) {
		return m, nil
	}
	if msg.err != nil {
		m.setStatus("fetch failed: " + msg.err.Error())
		if !msg.forGoto { // a failed G must not leave searching… and the remembered cursor behind
			g := m.tabAt(tabGraph).(graphTab)
			g.searching, g.want = false, ""
			m.tabs = slices.Clone(m.tabs)
			m.tabs[tabGraph] = g
		}
		return m, nil
	}
	extra, held := maps.Clone(m.extra), map[string]bool{}
	if extra == nil {
		extra = map[string]bd.Bead{}
	}
	moved := msg.undo != nil && m.tabAt(tabGraph).(graphTab).want == "" // a cursor move cancelled G's landing
	if !msg.forGoto {
		m.revealed = map[string]bool{}
	}
	for _, b := range msg.beads {
		extra[b.ID] = b
		if !msg.forGoto {
			m.revealed[b.ID] = true
		} else {
			held[b.ID] = true
		}
	}
	if msg.forGoto {
		m.gotoHeld = held
	}
	m.extra = extra
	m = m.withExtra()
	m.idx = newSnapIndex(m.beads)
	if m.tab == tabGraph {
		g := m.tabAt(tabGraph).(graphTab).rebuild() // still searching: a remembered cursor bead returns
		if !msg.forGoto {
			g.searching, g.want = false, ""
		}
		m.tabs = slices.Clone(m.tabs)
		m.tabs[tabGraph] = g
	}
	if gotoShown(m) { // the menu streams the new neighbours in
		d := m.modals[len(m.modals)-1]
		if next, ok := m.pop().gotoMenu(); ok {
			m = next
			if top := &m.modals[len(m.modals)-1]; d.cur < len(top.items) {
				top.cur = d.cur
			}
		}
	}
	if msg.then == nil || moved {
		return m, nil
	}
	if beadIndex(m.beads, msg.id) < 0 { // bd does not have it: no second fetch
		if msg.undo != nil {
			m = msg.undo(m)
		}
		m.setStatus(msg.id + ": not in the graph")
		return m, nil
	}
	return msg.then(m, msg.id)
}

func gotoShown(m Model) bool { return len(m.modals) > 0 && m.modals[len(m.modals)-1].gotoMenu }

// demandClaims is the ids this file's fetches keep loaded now: the last reveal's bead
// and ancestors while Graph is the tab, the Goto menu's neighbours while it is open.
func (m Model) demandClaims() map[string]bool {
	c := map[string]bool{}
	if m.tab == tabGraph {
		maps.Copy(c, m.revealed)
		maps.Copy(c, m.hits)
	}
	if gotoShown(m) {
		maps.Copy(c, m.gotoHeld)
	}
	return c
}

// withExtra appends the fetched beads that are still claimed and not already in the
// snapshot (open and default beats on-demand), dropping the rest.
func (m Model) withExtra() Model {
	if len(m.extra) == 0 {
		return m
	}
	claims := demandClaimed(m)
	have := make(map[string]bool, len(m.beads))
	for _, b := range m.beads {
		have[b.ID] = true
	}
	extra := map[string]bd.Bead{}
	for id, b := range m.extra {
		if !claims[id] {
			continue
		}
		extra[id] = b
		if !have[id] && !b.Event {
			m.beads = append(m.beads, b)
		}
	}
	m.extra = extra
	return m
}

// demandClaimed is demandClaims plus Recents and sessions, which keep a fetched bead too.
func demandClaimed(m Model) map[string]bool {
	c := claimedIDs(m.recents, m.sessions)
	maps.Copy(c, m.demandClaims())
	return c
}
