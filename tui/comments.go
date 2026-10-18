package tui

import (
	"maps"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
)

// listComments is the comments read, a seam so tests never exec bd.
var listComments = bd.Comments

// commentKey is a bead's comments at one snapshot count: a new comment bumps the
// count, which misses the cache and refetches.
type commentKey struct {
	id string
	n  int
}

// commentEntry is one cache slot: loading until the fetch lands, then cs or err.
type commentEntry struct {
	cs      []bd.Comment
	err     error
	loading bool
}

// commentsMsg is a finished fetch for (id, n). It needs no generation: it is the
// right data for its key whenever it lands.
type commentsMsg struct {
	id  string
	n   int
	cs  []bd.Comment
	err error
}

// drawerBead is the bead the open drawer describes, nil with none open or on a row
// that stands for no bead. Initiatives and Focus share Graph's detail.
func (m Model) drawerBead() *bd.Bead {
	if !m.drawerOpen() {
		return nil
	}
	switch {
	case m.focus.id != "":
		return m.focusBead()
	case m.tab == tabGraph, m.tab == tabInits:
		return m.selection()
	}
	if t, ok := m.tabAt(m.tab).(interface{ drawerBead() *bd.Bead }); ok {
		return t.drawerBead()
	}
	return nil
}

// syncComments starts the open drawer's bead's comments fetch when none is cached
// or in flight. Level-triggered like syncScan: it runs after every update, and the
// loading entry it stores first keeps the next tick from fetching again.
func (m Model) syncComments() (Model, tea.Cmd) {
	b := m.drawerBead()
	if b == nil || b.CommentCount == 0 {
		return m, nil
	}
	k := commentKey{b.ID, b.CommentCount}
	if _, ok := m.comments[k]; ok {
		return m, nil
	}
	m.comments = maps.Clone(m.comments)
	if m.comments == nil {
		m.comments = map[commentKey]commentEntry{}
	}
	m.comments[k] = commentEntry{loading: true}
	// ponytail: no pruning; one entry per (bead viewed, count) a session, a few KB.
	return m, func() tea.Msg {
		cs, err := listComments(k.id)
		return commentsMsg{k.id, k.n, cs, err}
	}
}

func (m Model) foldComments(msg commentsMsg) Model {
	m.comments = maps.Clone(m.comments)
	m.comments[commentKey{msg.id, msg.n}] = commentEntry{cs: msg.cs, err: msg.err}
	return m
}

// retryComments drops the error entries so the next update refetches them (f5).
func (m Model) retryComments() Model {
	m.comments = maps.Clone(m.comments)
	maps.DeleteFunc(m.comments, func(_ commentKey, e commentEntry) bool { return e.err != nil })
	return m
}
