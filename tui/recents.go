package tui

import (
	"encoding/json"
	"maps"
	"os"
	"slices"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// maxRecents is how many beads Recents keeps (ADR 0184).
const maxRecents = 20

// recentsPath is the Workspace's Recents file under the common dir ("" when
// unresolved); a seam for tests.
var recentsPath = func() string { return foldsPath("recents.json") }

// loadRecents reads the Recents file, most recent first; a missing, unreadable or
// unparsable file is empty.
func loadRecents(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var ids []string
	if json.Unmarshal(b, &ids) != nil {
		return nil
	}
	return ids
}

// touchRecent re-reads the file, moves id to the front, drops ids known says are
// gone (nil known prunes nothing; id itself is never pruned: it was just acted on),
// caps the list and writes it atomically, so two TUIs on one Workspace merge.
func touchRecent(path, id string, known func(string) bool) ([]string, error) {
	ids := []string{id}
	for _, o := range loadRecents(path) {
		if o != id && (known == nil || known(o)) && !slices.Contains(ids, o) {
			ids = append(ids, o)
		}
	}
	ids = ids[:min(len(ids), maxRecents)]
	b, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	return ids, writeFileAtomic(path, b)
}

var (
	keyJumpPick     = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "jump"))
	keyRecentsClose = key.NewBinding(key.WithKeys("esc", "R"), key.WithHelp("esc/R", "close"))
)

// recentsMsg is a Recents save's result.
type recentsMsg struct {
	ids []string
	err error
}

// recordRecent is the one seam every act calls as it fires on bead id: the save runs
// off the event loop (ADR 0098) and a failure reaches the status line without
// blocking the act. It prunes against the loaded snapshot, or nothing before one
// loads or while the last bd read failed.
func (m Model) recordRecent(id string) tea.Cmd {
	if id == "" {
		return nil
	}
	var known func(string) bool
	if m.loaded && m.loadErr == nil {
		have := make(map[string]bool, len(m.beads))
		for _, b := range m.beads {
			have[b.ID] = true
		}
		for _, id := range m.recents { // another TUI's fresh id is claimed but not fetched until the slow tier
			have[id] = true
		}
		known = func(id string) bool { return have[id] }
	}
	path := recentsPath()
	// ponytail: two TUIs, or two acts within one save's latency in one TUI (each save is
	// its own Cmd), can race the read-merge-write and lose an id; ADR 0184 accepts that
	// ceiling, add a lock if it ever bites.
	return func() tea.Msg {
		ids, err := touchRecent(path, id, known)
		return recentsMsg{ids, err}
	}
}

// keyRecording runs k, recording the bead whose detail drawer it opened: the
// drawer-open act of every tab and the focus view, caught in one place.
func (m Model) keyRecording(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !key.Matches(k, keyDetail) || len(m.modals) > 0 || m.ref || m.capturing() || m.drawerOpen() {
		return m.key(k)
	}
	b := m.selection()
	next, cmd := m.key(k)
	if b == nil || !next.(Model).drawerOpen() {
		return next, cmd
	}
	return next, tea.Batch(cmd, m.recordRecent(b.ID))
}

// drawerOpen reports whether the active tab (or the focus view) shows its detail drawer.
func (m Model) drawerOpen() bool {
	if m.focus.id != "" {
		return m.focus.open
	}
	d, ok := m.tabAt(m.tab).(interface{ drawerOpen() bool })
	return ok && d.drawerOpen()
}

// recentsOpen is R: the Recents picker over the frame, re-read from the file so
// another TUI's acts show; with nothing to list it says so and opens nothing.
func (m Model) recentsOpen() (tea.Model, tea.Cmd) {
	if n := len(m.modals); n > 0 && m.modals[n-1].recents {
		return m.pop(), nil
	}
	var ids, items []string
	var bs []*bd.Bead
	for _, id := range loadRecents(recentsPath()) {
		if i := beadIndex(m.beads, id); i >= 0 {
			ids, bs = append(ids, id), append(bs, &m.beads[i])
		}
	}
	if len(ids) == 0 {
		m.setStatus("no recent beads yet")
		return m, nil
	}
	g := m.gateData()
	tagW := 0
	for _, b := range bs {
		tagW = max(tagW, lipgloss.Width(kindTag(b)))
	}
	inner := m.pickerInner()
	for _, b := range bs {
		fixed := lipgloss.Width(g.sbrn(b, "", false, tagW))
		row := g.sbrn(b, ansi.Truncate(b.Title, max(inner-fixed, 1), "…"), false, tagW)
		if b.Status == "closed" {
			row = dimSt.Render(ansi.Strip(row))
		}
		items = append(items, row)
	}
	d := pickerDialog("Recents", items, "", func(m Model, d dialog) (Model, tea.Cmd) { return m.recentsJump(ids[d.cur]) })
	d.recents, d.shut = true, keyRecentsClose
	return m.push(d), nil
}

// homeTab is the queue tab that lists a bead of kind k (ADR 0183): Signals on Ideate,
// the Chart and its defog leaves on Defog, Initiatives on Initiatives, the rest of the
// spine on Execute. A Landing or Forge Review follows its container: a Chart's on Defog, a Checkpoint's on Execute.
func homeTab(b bd.Bead, beads []bd.Bead) int {
	switch b.Kind {
	case contract.KindLanding, contract.KindMRWatch, contract.KindForgeReview:
		if i := beadIndex(beads, b.Parent); i >= 0 && beads[i].Kind == contract.KindChart {
			return tabDefog
		}
	case contract.KindSignal:
		return tabIdeate
	case contract.KindChart, contract.KindGrilling, contract.KindResearch, contract.KindPrototype,
		contract.KindSurvey, contract.KindChartConflict:
		return tabDefog
	case contract.KindInitiative:
		return tabInits
	case contract.KindDiscussion, contract.KindCheckup:
		return tabGraph
	}
	return tabExecute
}

// minUnfold is cur plus the fewest flips from the fully expanded set exp that make a
// row hit; key is that row's key. A row already hitting under cur unfolds nothing; ok
// is false when no row hits even fully expanded.
// ponytail: one rebuild per flipped key, fine at TUI sizes.
func minUnfold[R any](cur, exp map[string]bool, list func(map[string]bool) []R, hit func(R) bool, keyOf func(R) string) (map[string]bool, string, bool) {
	find := func(f map[string]bool) (string, bool) {
		for _, r := range list(f) {
			if hit(r) {
				return keyOf(r), true
			}
		}
		return "", false
	}
	if k, ok := find(cur); ok {
		return cur, k, true
	}
	trial := maps.Clone(exp)
	if _, ok := find(trial); !ok {
		return cur, "", false
	}
	for _, k := range joinKeys(exp, cur) {
		if was := trial[k]; was != cur[k] {
			trial[k] = cur[k]
			if _, ok := find(trial); !ok {
				trial[k] = was
			}
		}
	}
	maps.DeleteFunc(trial, func(_ string, v bool) bool { return !v })
	k, _ := find(trial)
	return trial, k, true
}

// peeked records in peek the pre-jump value of every key the jump flipped from old to
// cur; a key already peeked keeps its first value.
func peeked(peek, old, cur map[string]bool) map[string]bool {
	out := maps.Clone(peek)
	for _, k := range joinKeys(old, cur) {
		if old[k] == cur[k] {
			continue
		}
		if out == nil {
			out = map[string]bool{}
		}
		if _, ok := out[k]; !ok {
			out[k] = old[k]
		}
	}
	return out
}

// unpeeked is flip with every peeked key back at its pre-jump value: what a fold save
// writes, so a jump's unfolds never reach the file (ADR 0183 §2).
func unpeeked(flip, peek map[string]bool) map[string]bool {
	if len(peek) == 0 {
		return flip
	}
	out := maps.Clone(flip)
	for k, was := range peek {
		if was {
			out[k] = true
		} else {
			delete(out, k)
		}
	}
	return out
}

// unpeek drops key from peek: a human flip makes it the human's, saved normally.
func unpeek(peek map[string]bool, key string) map[string]bool {
	if _, ok := peek[key]; !ok {
		return peek
	}
	out := maps.Clone(peek)
	delete(out, key)
	return out
}

// recentsJump is enter on a Recents row: leave the focus view and select the bead on
// its home tab, unfolding the fewest containers that show it. It records no
// interaction. A bead the home tab doesn't list lands on Graph scoped to it (a visit: never saved).
func (m Model) recentsJump(id string) (Model, tea.Cmd) {
	i := beadIndex(m.beads, id)
	if i < 0 {
		return m.fetchReveal(id, false, Model.recentsJump)
	}
	home := homeTab(m.beads[i], m.beads)
	var (
		next component
		ok   bool
	)
	// ponytail: the four arms share a shape but differ in row type and expansion set;
	// a generic helper would need as many closures as the arms hold lines.
	switch t := m.tabAt(home).(type) {
	case ideaTab:
		old := t.flip
		list := func(f map[string]bool) []ideaRow { t.flip = f; return t.rows() }
		exp := expandAll(list, func(f map[string]bool, r ideaRow) bool { return (r.header() || r.sec) && ideaFolded(f, r.key) },
			func(r ideaRow) string { return r.key })
		var k string
		t.flip, k, ok = minUnfold(old, exp, list, func(r ideaRow) bool { return r.b != nil && r.b.ID == id },
			func(r ideaRow) string { return r.key })
		t.sel, t.off, t.open, t.peek = k, 0, false, peeked(t.peek, old, t.flip)
		next = t
	case defogTab:
		old := t.flip
		list := func(f map[string]bool) []dRow { t.flip = f; return t.rows() }
		var k string
		t.flip, k, ok = minUnfold(old, t.expanded(), list, func(r dRow) bool { return r.b != nil && r.b.ID == id },
			func(r dRow) string { return r.key })
		t.sel, t.off, t.open, t.peek = k, 0, false, peeked(t.peek, old, t.flip)
		next = t
	case execTab:
		old := t.flip
		list := func(f map[string]bool) []eRow { t.flip = f; return t.rows() }
		var k string
		t.flip, k, ok = minUnfold(old, t.expanded(), list, func(r eRow) bool { return r.id() == id },
			func(r eRow) string { return r.key })
		t.sel, t.off, t.open, t.peek = k, 0, false, peeked(t.peek, old, t.flip)
		next = t
	case initsTab:
		old := t.flip
		list := func(f map[string]bool) []iRow { t.flip = f; return t.rows() }
		var k string
		t.flip, k, ok = minUnfold(old, map[string]bool{initsClosed: true}, list,
			func(r iRow) bool { return r.b != nil && r.b.ID == id }, func(r iRow) string { return r.key })
		t.sel, t.off, t.open, t.peek = k, 0, false, peeked(t.peek, old, t.flip)
		next = t
	}
	if !ok {
		g := m.tabAt(tabGraph).(graphTab)
		g.q, g.typing, g.visit = query{words: id, status: statusAny}, false, true
		g, _, _ = g.rebuild().reveal(id)
		m.tabs = slices.Clone(m.tabs)
		m.tabs[tabGraph], m.tab, m.focus = g, tabGraph, focus{}
		m.subtree = nil // the visit replaces any scope
		m.setStatus("not on " + tabNames[home] + " → Graph (scoped to " + id + ", esc clears)")
		return m, m.graphEntered()
	}
	m.tabs = slices.Clone(m.tabs)
	m.tabs[home] = next
	m.tab, m.focus = home, focus{}
	return m, nil
}

// joinKeys is the sorted union of the keys of a and b.
func joinKeys(a, b map[string]bool) []string {
	union := maps.Clone(a)
	if union == nil {
		union = map[string]bool{}
	}
	maps.Copy(union, b)
	return slices.Sorted(maps.Keys(union))
}
