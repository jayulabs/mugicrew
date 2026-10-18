package tui

// The Logs filter (FR24, ADR 0231 §2): any of /, f, L, e runs eventlog.Scan over the
// whole log off the event loop. Matches stream in newest first under "searching…",
// and a newer filter supersedes a running scan.

import (
	"context"
	"fmt"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
)

// logsScan is the filter's scan: its matches so far, newest first, and the handle
// of the scan still feeding them.
type logsScan struct {
	found     []eventlog.Event
	seen      map[eventlog.Key]bool // the keys of found, so fresh and head stay linear in the chunk
	tally     eventlog.Tally        // the whole-log events (base) the scan has walked so far
	ext       eventlog.Tally        // base events the tail added since the scan began
	searching bool
	gen       int    // which scan the found events belong to
	key       string // the filter that scan answers
	cancel    context.CancelFunc
}

// logsScanMsg is one chunk of a scan's matches; done ends it.
type logsScanMsg struct {
	gen   int
	evs   []eventlog.Event
	tally eventlog.Tally
	done  bool
	err   error
	wait  tea.Cmd // the read of the scan's next message
}

// syncScan starts a scan when the Logs filter changed since the last one, dropping
// the old scan's results. It runs after every update, so no filter edit can miss it.
func (m Model) syncScan() (Model, tea.Cmd) {
	l := m.tabAt(tabLogs).(logsTab)
	key := fmt.Sprint(l.active(), l.kind, l.query(), l.scope, l.kids, l.errs)
	if key == l.scan.key {
		return m, nil
	}
	if l.scan.cancel != nil {
		l.scan.cancel()
	}
	l.scan = logsScan{gen: l.scan.gen + 1, key: key}
	l.kindOf = m.logKindOf // the kinds this scan matches by, not the handed-over copy of the last update
	var cmd tea.Cmd
	if l.active() && m.logPath != "" {
		l.scan.searching = true
		if l.kind != "" && m.kindsStale() { // the scan waits for the kinds; foldKinds restarts it
			m, cmd = m.readKinds(false)
		} else {
			ctx, cancel := context.WithCancel(context.Background())
			l.scan.cancel = cancel
			cmd = startScan(ctx, m.logPath, l.scan.gen, l.prefilter(), l.matcher(), l.base().tally())
		}
	}
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabLogs] = l
	return m, cmd
}

var readKinds = bd.Kinds

// logKindsMsg is every bead's kind; pick opens the f picker once it lands.
type logKindsMsg struct {
	kinds map[string]contract.Kind
	err   error
	pick  bool
}

// kindsStale is whether logKindOf must be read again: never read, or older than the last slow tier.
func (m Model) kindsStale() bool { return m.logKindOf == nil || m.logKindAt.Before(m.slowAt) }

// readKinds reads every bead's kind off the event loop, one read at a time.
func (m Model) readKinds(pick bool) (Model, tea.Cmd) {
	if m.logKindBusy {
		m.logKindPick = m.logKindPick || pick // queue the f press for when the read lands
		return m, nil
	}
	m.logKindBusy = true
	return m, func() tea.Msg {
		ks, err := readKinds()
		return logKindsMsg{kinds: ks, err: err, pick: pick}
	}
}

// foldKinds keeps the kinds read and restarts the kind-filter scan that waited on them.
// A failed read falls back to the loaded beads' kinds.
func (m Model) foldKinds(s logKindsMsg) (Model, tea.Cmd) {
	m.logKindBusy, m.logKindAt = false, time.Now()
	m.logKindOf = s.kinds
	if m.logKindOf == nil {
		m.logKindOf = map[string]contract.Kind{}
	}
	if s.err != nil {
		m.setStatus("log kinds: " + s.err.Error())
	}
	l := m.tabAt(tabLogs).(logsTab)
	if l.kind != "" {
		l.scan.key = "" // syncScan starts the scan afresh
		m.tabs = slices.Clone(m.tabs)
		m.tabs[tabLogs] = l
	}
	pick := s.pick || m.logKindPick
	m.logKindPick = false
	if pick && m.tab == tabLogs {
		return m.pickLogKind()
	}
	return m, nil
}

func startScan(ctx context.Context, path string, gen int, pre []string, match func(eventlog.Event) bool, base eventlog.Base) tea.Cmd {
	ch := make(chan logsScanMsg)
	send := func(s logsScanMsg) bool {
		select {
		case ch <- s:
			return true
		case <-ctx.Done():
			return false
		}
	}
	go func() {
		var t eventlog.Tally
		err := eventlog.ScanTally(path, pre, match, func(evs []eventlog.Event) bool {
			return send(logsScanMsg{gen: gen, evs: evs, tally: t})
		}, base, &t)
		send(logsScanMsg{gen: gen, done: true, err: err, tally: t})
	}()
	return waitScan(ctx, ch)
}

// waitScan reads the scan's next message; a cancelled scan reads as a stale one.
func waitScan(ctx context.Context, ch chan logsScanMsg) tea.Cmd {
	return func() tea.Msg {
		select {
		case s := <-ch:
			s.wait = waitScan(ctx, ch)
			return s
		case <-ctx.Done():
			return logsScanMsg{gen: -1}
		}
	}
}

// foldScan adds a chunk of matches, unless its scan was superseded.
func (m Model) foldScan(s logsScanMsg) (tea.Model, tea.Cmd) {
	l := m.tabAt(tabLogs).(logsTab)
	if s.gen != l.scan.gen {
		return m, nil
	}
	l.scan.found = slices.Concat(l.scan.found, l.scan.take(s.evs))
	l.scan.tally = s.tally
	l = l.pin() // anchor what the reader sees before older rows land behind it
	if s.done {
		l.scan.searching = false
		if s.err != nil {
			m.setStatus("log scan: " + s.err.Error())
		}
		s.wait = nil
	}
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabLogs] = l
	return m, s.wait
}

// take is evs less the events already found, marking the rest found: a scan and a tail
// read can both see one. It mutates seen, which logsTab copies share; a new scan gets a new map.
func (s *logsScan) take(evs []eventlog.Event) []eventlog.Event {
	if s.seen == nil {
		s.seen = map[eventlog.Key]bool{}
	}
	return slices.DeleteFunc(slices.Clone(evs), func(ev eventlog.Event) bool {
		k := ev.Key()
		dup := s.seen[k]
		s.seen[k] = true
		return dup
	})
}

// head adds the filter's matches among evs, events the log has grown by, ahead of
// the scan's.
func (l logsTab) head(evs []eventlog.Event) logsTab {
	match := l.matcher()
	var add []eventlog.Event
	for i := len(evs) - 1; i >= 0; i-- {
		if match(evs[i]) {
			add = append(add, evs[i])
		}
	}
	base := l.base().matcher()
	for _, ev := range evs { // ponytail: an unmatched event the scan also walked counts twice; headTotal clamps only to the rows
		if base(ev) && !l.scan.seen[ev.Key()] {
			l.scan.ext.Lines++
			if ev.Level == "DEBUG" {
				l.scan.ext.Debug++
			}
		}
	}
	l.scan.found = slices.Concat(l.scan.take(add), l.scan.found)
	return l
}
