package tui

import (
	"errors"
	"fmt"
	"maps"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/stats"
)

// rollupLineage and rollupEvents are the rollup's bd and event log reads, seams so
// tests never exec bd or touch the log.
var (
	rollupLineage = bd.LoadLineage
	rollupEvents  = func() ([]eventlog.Event, error) {
		path, err := eventlog.Resolve()
		if err != nil {
			return nil, err
		}
		evs, _, err := eventlog.Read(path)
		return evs, err
	}
)

// rollupEntry is one cache slot for the drawer's ROLLUP: not loaded until the fetch
// lands, then both TOTAL rows (or err). notSpine marks a bead with no spine. gen is
// the snapshot it was read at; a later snapshot refetches while the old numbers show.
type rollupEntry struct {
	subtree, spine stats.Row
	notSpine       bool
	err            error
	loaded         bool
	loading        bool
	gen            int
}

// rollupMsg is a finished fetch for id. lin is the lineage it read, nil when it
// reused the cache, so the fold can cache a fresh one for the next bead.
type rollupMsg struct {
	id    string
	lin   *bd.Lineage
	gen   int
	entry rollupEntry
}

// computeRollup sums id's subtree and spine TOTALs over the whole retained log.
func computeRollup(lin *bd.Lineage, id string) rollupEntry {
	evs, err := rollupEvents()
	if err != nil {
		return rollupEntry{err: err}
	}
	scope := map[string]string{}
	for _, b := range lin.Subtree(id) {
		scope[b] = id
	}
	e := rollupEntry{}
	e.subtree, _ = stats.Sum(evs, stats.Filter{Scope: scope})
	stages, err := lin.Spine(id)
	switch {
	case errors.Is(err, bd.ErrNotOnSpine):
		e.notSpine = true
	case err != nil:
		e.err = err
	default:
		e.spine, _ = stats.Sum(evs, stats.Filter{Scope: bd.SpineScope(stages)})
	}
	return e
}

// syncRollup starts the open drawer's bead's rollup fetch when none is cached or in
// flight, or when a newer snapshot has loaded since a good one. Level-triggered like
// syncComments (ADR 0098); the Cmd loads the lineage (cached per snapshot) and the log
// off the event loop.
func (m Model) syncRollup() (Model, tea.Cmd) {
	b := m.drawerBead()
	if b == nil {
		return m, nil
	}
	old, ok := m.rollups[b.ID]
	if old.loading || (ok && (old.err != nil || old.gen == m.snapGen)) {
		return m, nil
	}
	m.rollups = maps.Clone(m.rollups)
	if m.rollups == nil {
		m.rollups = map[string]rollupEntry{}
	}
	old.loading = true // a refetch keeps showing the old numbers until it lands
	m.rollups[b.ID] = old
	id, gen := b.ID, m.snapGen
	var lin *bd.Lineage
	if m.lineageGen == gen {
		lin = m.lineage
	}
	return m, func() tea.Msg {
		msg := rollupMsg{id: id, gen: gen}
		if lin == nil {
			var err error
			if lin, err = rollupLineage(); err != nil {
				msg.entry = rollupEntry{err: err, loaded: true, gen: gen}
				return msg
			}
			msg.lin = lin
		}
		msg.entry = computeRollup(lin, id)
		msg.entry.loaded, msg.entry.gen = true, gen
		return msg
	}
}

func (m Model) foldRollup(msg rollupMsg) Model {
	m.rollups = maps.Clone(m.rollups)
	m.rollups[msg.id] = msg.entry
	if msg.lin != nil && msg.gen >= m.lineageGen {
		m.lineage, m.lineageGen = msg.lin, msg.gen
	}
	return m
}

// retryRollup drops the error entries so the next update refetches them (f5).
func (m Model) retryRollup() Model {
	m.rollups = maps.Clone(m.rollups)
	maps.DeleteFunc(m.rollups, func(_ string, e rollupEntry) bool { return e.err != nil })
	return m
}

// rollupLines is b's two ROLLUP label lines, subtree then spine, from the cache only.
func (g graphData) rollupLines(b *bd.Bead) string {
	e := g.rollups[b.ID]
	line := func(r stats.Row) string {
		switch {
		case !e.loaded:
			return softSt.Render("loading…")
		case e.err != nil:
			return softSt.Render("couldn't load: " + oneLine(e.err))
		case r.Sessions == 0:
			return softSt.Render("no sessions")
		}
		return fmt.Sprintf("%s · %s · %d %s", stats.Dur(r.WallMs), r.Cost.USD(), r.Sessions, plural(r.Sessions, "session"))
	}
	spine := line(e.spine)
	if e.loaded && e.err == nil && e.notSpine {
		spine = softSt.Render("not on a spine")
	}
	return labelField("subtree", line(e.subtree)) + "\n" + labelField("spine", spine)
}
