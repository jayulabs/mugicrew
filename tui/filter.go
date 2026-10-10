package tui

import (
	"maps"
	"regexp"
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// listFilter is one tab's kind filter and regex search (Spec FR3–5, FR7):
// kept per tab on the Model, so it survives the 3s refresh but not a restart.
type listFilter struct {
	kind contract.Kind  // "" = every kind
	re   *regexp.Regexp // nil = no search
	src  string         // re's pattern as typed: the bar shows it, / re-opens on it
}

func (f listFilter) active() bool { return f.kind != "" || f.re != nil }

// filtered is whether tab t has any filter on (FR8's ▼, esc's hint): its kind or
// search, or on Logs the L bead scope.
func (m Model) filtered(t tab) bool {
	return m.filters[t].active() || t == tabLogs && m.logScope != ""
}

// match is the combined test (FR5): a row passes only when it matches both the
// kind and the search; text is what the tab's search reads.
func (f listFilter) match(k contract.Kind, text string) bool {
	return (f.kind == "" || k == f.kind) && (f.re == nil || f.re.MatchString(text))
}

// matchBead is the Graph/Inbox match: the search reads bead id + title (FR4).
func (f listFilter) matchBead(b bd.Bead) bool { return f.match(b.Kind, b.ID+" "+b.Title) }

// terms names the active filter for the filter bar: kind:<k> then /<src>/.
func (f listFilter) terms() []string {
	var ts []string
	if f.kind != "" {
		ts = append(ts, "kind:"+string(f.kind))
	}
	if f.re != nil {
		ts = append(ts, "/"+f.src+"/")
	}
	return ts
}

// nextKind is f's step (FR3): all → each kind present, alphabetical → all. A
// current kind no longer present steps to the next one after it.
func nextKind(cur contract.Kind, present []contract.Kind) contract.Kind {
	present = slices.Clone(present)
	slices.Sort(present)
	for _, k := range present {
		if k > cur {
			return k
		}
	}
	return ""
}

// inboxRows is the Inbox as shown: inboxList narrowed by the tab's filter.
func (m Model) inboxRows() []bd.Bead {
	f := m.filters[tabInbox]
	return slices.DeleteFunc(inboxList(m.beads, m.sessions), func(b bd.Bead) bool { return !f.matchBead(b) })
}

// filterGroups is the Graph filter (FR6), applied after hideClosedRootGroups: a
// group whose Chart head matches stays whole, and its key is returned in heads
// (the caller collapses those on apply); otherwise only matching members stay,
// the head kept for context; a group left with no match is dropped.
func filterGroups(groups []group, f listFilter) (out []group, heads []string) {
	for _, g := range groups {
		if g.head != nil && f.matchBead(*g.head) {
			out, heads = append(out, g), append(heads, g.key)
			continue
		}
		g.members = slices.DeleteFunc(slices.Clone(g.members), func(b bd.Bead) bool { return !f.matchBead(b) })
		if len(g.members) > 0 {
			out = append(out, g)
		}
	}
	return out, heads
}

// groupBeads lists every bead in groups, heads then members.
func groupBeads(groups []group) []bd.Bead {
	var bs []bd.Bead
	for _, g := range groups {
		if g.head != nil {
			bs = append(bs, *g.head)
		}
		bs = append(bs, g.members...)
	}
	return bs
}

// rowKinds is the kind of each of tab t's rows with no kind filter or search on:
// f's cycle reads it and its length is the filter bar's total. On Logs an event's
// kind is its bead's ("" for none or one not loaded), and scope picks whether the
// L bead scope still applies: f cycles within it, the bar's total ignores it.
func (m Model) rowKinds(t tab, scope string) []contract.Kind {
	var kinds []contract.Kind
	switch t {
	case tabGraph:
		for _, b := range groupBeads(m.graphAll) {
			kinds = append(kinds, b.Kind)
		}
	case tabInbox:
		for _, b := range inboxList(m.beads, m.sessions) {
			kinds = append(kinds, b.Kind)
		}
	case tabLogs:
		ks := m.beadKinds()
		for _, r := range m.logRowsBy(listFilter{}, scope) {
			kinds = append(kinds, ks[m.logs[r].Bead])
		}
	}
	return kinds
}

// cycleKind steps the tab's kind filter over the kinds in its unfiltered data.
func (m *Model) cycleKind() {
	kinds := m.rowKinds(m.tab, m.logScope)
	f := &m.filters[m.tab]
	f.kind = nextKind(f.kind, kinds)
	m.filterChanged(true)
}

// filterChanged settles the tab after its filter changes. On the Graph, the
// first apply snapshots the collapse map and an apply (fold: f or a / pattern,
// not a clear) folds the matched heads (FR6), a selection inside one moving to
// its head, and unfolds member-match groups so their matches show; a reload
// never re-folds or re-unfolds, so space can undo either. Clearing the whole
// filter restores the snapshot, dropping folds made while filtered.
func (m *Model) filterChanged(fold bool) {
	if m.tab == tabLogs {
		m.detailOffset = 0
	}
	if m.tab == tabGraph {
		switch {
		case !m.filters[tabGraph].active():
			if m.preFilter != nil {
				m.collapsed, m.preFilter = m.preFilter, nil
			}
		case m.preFilter == nil:
			m.preFilter = maps.Clone(m.collapsed)
		}
		m.rebuildGroups()
		if f := m.filters[tabGraph]; fold && f.active() { // f stepping back to all is not an apply
			m.foldMatches(f)
		}
	}
	m.fixSelection()
}

// foldMatches is filterChanged's apply on the Graph (FR6): head-match groups
// fold, a selection inside one moving to its head; member-match groups unfold.
func (m *Model) foldMatches(f listFilter) {
	_, heads := filterGroups(m.graphAll, f)
	for _, g := range m.groups {
		if !slices.Contains(heads, g.key) { // a member match: unfold so the match shows
			m.collapsed[g.key] = false
		} else {
			m.collapsed[g.key] = true
			if slices.ContainsFunc(g.members, func(b bd.Bead) bool { return b.ID == m.selGraph }) {
				m.selGraph = g.key
			}
		}
	}
}

// clearFilter is esc with no prompt open (FR7): the search first, then the kind,
// then on Logs the L bead scope.
func (m *Model) clearFilter() {
	f := &m.filters[m.tab]
	switch {
	case f.re != nil:
		f.re, f.src = nil, ""
	case f.kind != "":
		f.kind = ""
	case m.tab == tabLogs:
		m.logScope, m.logScopeKids = "", false
	}
	m.filterChanged(false)
}

// updateSearch drives the / prompt (FR4), the inline editor's keys: enter applies
// the case-insensitive RE2 pattern (empty clears the search), esc cancels. An
// invalid pattern keeps the previous search and shows the compile error.
func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.editMode, m.editBuf = editNone, line{}
	case tea.KeyEnter:
		src := m.editBuf.text
		m.editMode, m.editBuf = editNone, line{}
		f := &m.filters[m.tab]
		if src == "" {
			f.re, f.src = nil, ""
		} else {
			// Compile bare first so the error quotes the pattern as typed, not our (?i).
			if _, err := regexp.Compile(src); err != nil {
				m.status = "search: " + err.Error()
				return m, nil
			}
			f.re, f.src = regexp.MustCompile("(?i)"+src), src
		}
		m.filterChanged(src != "")
	default:
		m.editBuf = m.editBuf.apply(msg)
	}
	return m, nil
}
