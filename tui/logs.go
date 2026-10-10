package tui

// The Logs tab (ADR 0027-0029, 0033): the event log rendered raw, newest
// first. It shows what happened and draws no alive/dead verdict: a started with no
// end is shown as exactly that (ADR 0033 §3); liveness is the probe's alone.

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/session"
)

// readLog is eventlog.Read, a var so the stat-unchanged test counts reads.
var readLog = eventlog.Read

// WithLog hands the model the process's event log: the path the Logs tab reads and
// the emitter's one-time warning for the status line.
func (m Model) WithLog(l *eventlog.Log) Model {
	m.evlog = l
	return m
}

// logsMsg is one Logs refresh: the stat sig, and the events only when it changed.
// err (the log is unavailable: unresolvable or unreadable) rides with changed.
type logsMsg struct {
	path    string
	sig     eventlog.Sig
	changed bool
	events  []eventlog.Event
	skipped int
	err     error
}

// loadLogs re-reads the event log only when its stat changed since the last load.
func (m Model) loadLogs() tea.Cmd {
	prev, l := m.logSig, m.evlog
	return func() tea.Msg {
		var msg logsMsg
		path, err := l.Path()
		msg.path = path
		if path == "" {
			msg.changed, msg.err = true, err
			return msg
		}
		msg.sig = eventlog.Stat(path)
		if msg.changed = msg.sig != prev; msg.changed {
			msg.events, msg.skipped, msg.err = readLog(path)
			if msg.err != nil {
				// A chmod moves neither size nor mtime: keep retrying until it reads.
				msg.sig = eventlog.Sig{}
			}
		}
		return msg
	}
}

// flashFinished flashes `✓ <id> finished` once per clean HITL exit the tick
// reads (ADR 0067 §2): a hitl.exited with no err (the CLI triage records a failed
// launch as hitl.exited too), nor a drop's. The first read only seeds what's
// seen, so a fresh TUI never flashes history. While an edit or confirm owns the
// status line the flash waits for a later read rather than clobber its prompt.
// ponytail: exitsSeen keeps every key for the process's life; one per clean exit is tiny.
func (m *Model) flashFinished() {
	seed := m.exitsSeen == nil
	if seed {
		m.exitsSeen = map[eventlog.Key]bool{}
	} else if m.editMode != editNone {
		return
	}
	var done []string
	for _, ev := range m.logs {
		if ev.Msg != "hitl.exited" || ev.Bead == "" || ev.Raw["err"] != nil || ev.Raw["dropped"] == true || m.exitsSeen[ev.Key()] {
			continue
		}
		m.exitsSeen[ev.Key()] = true
		if !seed {
			done = append(done, "✓ "+ev.Bead+" finished")
		}
	}
	if len(done) > 0 {
		m.setStatus(strings.Join(done, " · "))
	}
}

// flashCloseFailed flashes `✕ <id> close failed — resume or drop` once per
// session the last read showed Closing and this one shows exited (ADR 0108 §4).
// One that left the list finished cleanly: flashFinished's hitl.exited says so.
// The first read only seeds; an edit or confirm defers it, as flashFinished does.
func (m *Model) flashCloseFailed() {
	if m.closingSeen != nil && m.editMode != editNone {
		return
	}
	var failed []string
	for id := range m.closingSeen {
		if m.sessions[id].State == session.Exited {
			failed = append(failed, "✕ "+id+" close failed — resume or drop")
		}
	}
	m.closingSeen = map[string]bool{}
	for id := range m.sessions {
		if m.closing(id) {
			m.closingSeen[id] = true
		}
	}
	if len(failed) > 0 {
		slices.Sort(failed)
		m.setStatus(strings.Join(failed, " · "))
	}
}

// logRows are the visible events as indices into m.logs, newest first, through
// the filters: Debug hidden unless d (ADR 0027 §4), Error only under e, the
// L scope — bead==X, or for a container bead==X || parent==X (ADR 0029 §3) —
// and the tab's kind filter and search (Spec FR3–5).
func (m Model) logRows() []int { return m.logRowsBy(m.filters[tabLogs], m.logScope) }

// logRowsBy is logRows under filter f and bead scope ("" = unscoped).
// An event's kind is its bead's, so under a kind filter one with no bead or a
// bead not loaded never matches; the search reads msg, bead id and the attrs.
// ponytail: rescans every event on each render/keypress; cache the rows on
// load/filter change if a full log ever makes keys feel slow.
func (m Model) logRowsBy(f listFilter, scope string) []int {
	var kinds map[string]contract.Kind
	if f.kind != "" {
		kinds = m.beadKinds()
	}
	var rows []int
	for i := len(m.logs) - 1; i >= 0; i-- {
		ev := m.logs[i]
		switch {
		case ev.Level == "DEBUG" && !m.logDebug,
			ev.Level != "ERROR" && m.logErrors,
			scope != "" && ev.Bead != scope && !(m.logScopeKids && ev.Parent == scope):
			continue
		}
		if f.active() {
			text := ev.Msg + " " + ev.Bead
			for _, a := range ev.Attrs {
				text += " " + a.K + " " + a.V
			}
			if !f.match(kinds[ev.Bead], text) {
				continue
			}
		}
		rows = append(rows, i)
	}
	return rows
}

// beadKinds maps each loaded bead's id to its kind: the Logs kind lookup.
func (m Model) beadKinds() map[string]contract.Kind {
	ks := make(map[string]contract.Kind, len(m.beads))
	for _, b := range m.beads {
		ks[b.ID] = b.Kind
	}
	return ks
}

// logTerms are the Logs filter bar's terms: kind, search, then scope:<id>.
func (m Model) logTerms() []string {
	ts := m.filters[tabLogs].terms()
	if m.logScope != "" {
		s := "scope:" + m.logScope
		if m.logScopeKids {
			s += "+children"
		}
		ts = append(ts, s)
	}
	return ts
}

// logFilters names the active level filters for the tab bar.
func (m Model) logFilters() []string {
	var fs []string
	if m.logErrors {
		fs = append(fs, "errors only")
	}
	if m.logDebug {
		fs = append(fs, "+debug")
	}
	return fs
}

// logKeys are the Logs view keys, each named for what pressing it will do.
func (m Model) logKeys() []footerKey {
	ks := []footerKey{{"d", "+debug"}, {"e", "errors"}}
	if m.logDebug {
		ks[0].label = "-debug"
	}
	if m.logErrors {
		ks[1].label = "all"
	}
	return ks
}

// selectedLog is the selected event: m.selLog's if still visible, else the newest.
func (m Model) selectedLog() (int, *eventlog.Event) {
	rows := m.logRows()
	if len(rows) == 0 {
		return -1, nil
	}
	for i, r := range rows {
		if m.logs[r].Key() == m.selLog {
			return i, &m.logs[r]
		}
	}
	return 0, &m.logs[rows[0]]
}

// pinLog anchors a zero selLog ("the newest") on the event shown now, so it stops
// tracking newer events as they land. A set selLog stays put, even while filtered out.
func (m *Model) pinLog() {
	if _, ev := m.selectedLog(); ev != nil && m.selLog == (eventlog.Key{}) {
		m.selLog = ev.Key()
	}
}

func (m *Model) moveLog(d int) {
	rows := m.logRows()
	if len(rows) == 0 {
		return
	}
	i, _ := m.selectedLog()
	m.selLog = m.logs[rows[clampWrap(i+d, len(rows))]].Key()
}

func levelTag(level string) string {
	s := fmt.Sprintf("%-5s", level)
	switch level {
	case "ERROR":
		return warn.Render(s)
	case "WARN":
		return pinkFg.Render(s)
	}
	return dim.Render(s)
}

func (m Model) logsView(height int) string {
	mw := m.masterColW()
	sel, _ := m.selectedLog()
	var lines []string
	rows := m.logRows()
	bar := m.tabFilterBar(tabLogs, len(rows))
	if m.logErr != nil || len(rows) == 0 {
		lines = append(lines, m.logEmpty(mw-2))
	}
	for i, r := range rows {
		ev := m.logs[r]
		marker := "  "
		if i == sel {
			marker = selMarker.Render("❯ ")
		}
		rest := ev.Msg
		if ev.Bead != "" {
			rest += " " + ev.Bead
		}
		if ev.Short != "" {
			rest += " " + ev.Short
		}
		// Clock only: the date is in the detail's time attr; the master column is narrow.
		lines = append(lines, marker+dim.Render(ev.Time.Local().Format("15:04:05"))+" "+
			levelTag(ev.Level)+" "+trunc(rest, mw-17)) // 17 = marker, clock, level and their spaces
	}
	full := m.logDetail()
	detail := strings.Join(windowRows(full, m.detailInnerW(), height, m.detailOffset), "\n")
	return m.layout(bar, lines, sel, detail, height)
}

// logEmpty tells "nothing happened" from "can't read" (FR51), cut to width (0: whole).
func (m Model) logEmpty(width int) string {
	s, style := "no events yet", dim
	if len(m.logs) > 0 {
		s = "no matching events" // the filters or scope hide them all
	} else if m.logPath != "" {
		s += " — " + m.logPath
	}
	if m.logErr != nil {
		s, style = fmt.Sprintf("event log unavailable: %v", m.logErr), warn
	}
	if width > 0 {
		s = trunc(s, width)
	}
	return style.Render(s)
}

// logDetail lists every attr of the selected event; a multi-line value (an err
// chain, a stderr tail) keeps its lines, indented under its key.
func (m Model) logDetail() string {
	_, ev := m.selectedLog()
	if ev == nil || m.logErr != nil {
		return m.logEmpty(0)
	}
	kw := 11 // the key column widens for a long (group sub-)key
	for _, a := range ev.Attrs {
		kw = max(kw, len(a.K))
	}
	var b strings.Builder
	indent := "\n" + strings.Repeat(" ", kw+1) // under the key column + its space
	for _, a := range ev.Attrs {
		v := strings.ReplaceAll(strings.TrimRight(a.V, "\n"), "\n", indent)
		fmt.Fprintf(&b, "%s %s\n", label.Render(fmt.Sprintf("%-*s", kw, a.K)), v)
	}
	return strings.TrimRight(b.String(), "\n")
}

// logDetailMaxOff is how far the Logs detail scrolls at the current geometry.
func (m Model) logDetailMaxOff() int {
	rows := lineCount(lipgloss.NewStyle().Width(m.detailInnerW()).Render(m.logDetail()))
	return max(0, rows-m.paneHeight())
}
