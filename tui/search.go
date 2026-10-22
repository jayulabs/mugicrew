package tui

// Graph words stream closed matches in under searching… (ADR 0232): open matches show
// from the snapshot at once; the closed ones are read off the event loop and held by the
// Graph query claim while Graph is the tab.

import (
	"maps"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
)

var searchClosed = bd.SearchClosed

// searchMsg is a closed word search's result: the hits, their ancestors and those
// ancestors' children. It carries the generation that started it.
type searchMsg struct {
	gen   int
	beads []bd.Bead
	err   error
}

// searchWanted reports whether q's words need the closed read: words match every status
// at the default (ADR 0175), and under any and closed.
func searchWanted(q query) bool {
	return strings.TrimSpace(q.words) != "" && (q.status == statusUnclosed || q.status == statusAny || q.status == statusClosed)
}

// searchSync starts, replaces or ends the closed search after the Graph query may have
// changed. A scoped reveal, a scope or a Recents visit is not a search; a status change between
// wanted statuses keeps the hits.
func (m Model) searchSync() (Model, tea.Cmd) {
	g := m.tabAt(tabGraph).(graphTab)
	q := g.query()
	if !searchWanted(q) || g.visit || q == g.scopedTo || q.scope != "" {
		if m.searched.words == "" {
			return m, nil
		}
		m.searched, m.hits = query{}, nil
		m.searchGen++ // an in-flight search is stale
		g.searching = false
	} else if q.words == m.searched.words {
		return m, nil
	} else {
		m.searched = q
		m.searchGen++
		g.searching = true
		gen, loaded := m.searchGen, make(map[string]bd.Bead, len(m.beads))
		for _, b := range m.beads {
			loaded[b.ID] = b
		}
		m.tabs = slices.Clone(m.tabs)
		m.tabs[tabGraph] = g
		return m, searchCmd(gen, q.words, loaded)
	}
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabGraph] = g.rebuild()
	return m, nil
}

// searchCmd reads the closed hits, then the ancestors each needs and those ancestors'
// children, so a closed hit's context rows count exactly (ADR 0143 §1).
func searchCmd(gen int, words string, loaded map[string]bd.Bead) tea.Cmd {
	return func() tea.Msg {
		hits, err := searchClosed(words)
		if err != nil {
			return searchMsg{gen: gen, err: err}
		}
		have := map[string]bool{}
		out := slices.Clone(hits)
		for _, b := range hits {
			have[b.ID] = true
		}
		ancestors := map[string]bool{}
		for need := parentsOf(hits, ancestors); len(need) > 0; {
			var fetch []string
			var next []bd.Bead
			for _, id := range need {
				if b, ok := loaded[id]; ok {
					next = append(next, b)
				} else if !have[id] {
					fetch = append(fetch, id)
				}
			}
			if len(fetch) > 0 {
				got, err := listByID(fetch)
				if err != nil {
					return searchMsg{gen: gen, err: err}
				}
				for _, b := range got {
					have[b.ID] = true
				}
				out, next = append(out, got...), append(next, got...)
			}
			need = parentsOf(next, ancestors)
		}
		for _, id := range slices.Sorted(maps.Keys(ancestors)) {
			kids, err := listChildren(id)
			if err != nil {
				return searchMsg{gen: gen, err: err}
			}
			for _, b := range kids {
				if !have[b.ID] {
					have[b.ID] = true
					out = append(out, b)
				}
			}
		}
		return searchMsg{gen: gen, beads: out}
	}
}

// parentsOf is the parents of bs not yet walked, recorded in seen.
func parentsOf(bs []bd.Bead, seen map[string]bool) (need []string) {
	for _, b := range bs {
		if b.Parent != "" && !seen[b.Parent] {
			seen[b.Parent] = true
			need = append(need, b.Parent)
		}
	}
	return need
}

// foldSearch takes a search's beads in as the new hits; a stale generation is dropped.
func (m Model) foldSearch(msg searchMsg) Model {
	if msg.gen != m.searchGen {
		return m
	}
	if msg.err != nil {
		m.setStatus("search failed: " + msg.err.Error())
	} else { // on error keep the old hits: a failed narrowed re-fetch must not blank them
		extra := maps.Clone(m.extra)
		if extra == nil {
			extra = map[string]bd.Bead{}
		}
		hits := map[string]bool{}
		for _, b := range msg.beads {
			extra[b.ID], hits[b.ID] = b, true
		}
		m.extra, m.hits = extra, hits
		m = m.withExtra()
		m.idx = newSnapIndex(m.beads)
	}
	g := m.tabAt(tabGraph).(graphTab) // after the beads moved: tabAt hands over the shell's data
	g.searching = false
	if m.tab == tabGraph {
		g = g.rebuild()
	}
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabGraph] = g
	return m
}

// keepClosers adds an open match that closed in this load to the hits, so it does not
// vanish from the result under the human (ADR 0232 §1).
func (m Model) keepClosers(next []bd.Bead) Model {
	if m.searched.words == "" || m.tab != tabGraph {
		return m
	}
	was := map[string]bool{}
	for _, b := range m.beads {
		was[b.ID] = b.Unclosed()
	}
	for _, b := range next {
		if b.Status == "closed" && was[b.ID] && matchWords(m.searched.words, b.Title+" "+b.ID) {
			m.hits = maps.Clone(m.hits)
			if m.hits == nil {
				m.hits = map[string]bool{}
			}
			m.hits[b.ID] = true
		}
	}
	return m
}
