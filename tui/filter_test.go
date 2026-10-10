package tui

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// filterInbox is an Inbox with three HITL kinds, loaded through Update.
func filterInbox(t *testing.T) Model {
	t.Helper()
	m := New(nil, nil)
	m.w, m.h = 120, 30
	m.tab = tabInbox
	next, _ := m.Update(loadedMsg{beads: []bd.Bead{
		{ID: "g1", Kind: contract.KindGrilling, Status: "open", Title: "Launch flow"},
		{ID: "g2", Kind: contract.KindGrilling, Status: "open", Title: "Sort order"},
		{ID: "s1", Kind: contract.KindSignal, Status: "open", Title: "launch is slow"},
		{ID: "k1", Kind: contract.KindSurgery, Status: "open", Title: "Hand fix"},
	}})
	return next.(Model)
}

func inboxIDs(m Model) (ids []string) {
	for _, b := range m.inboxRows() {
		ids = append(ids, b.ID)
	}
	slices.Sort(ids)
	return ids
}

// search types pat into the / prompt and presses enter (or esc when cancel).
func search(t *testing.T, m Model, pat string, cancel bool) Model {
	t.Helper()
	m, _ = send(t, m, runeKey("/"))
	if m.editMode != editSearch {
		t.Fatalf("/ did not open the search prompt")
	}
	m.editBuf = line{} // the prompt opens on the previous pattern; type over it
	if pat != "" {
		m, _ = send(t, m, runeKey(pat))
	}
	end := tea.KeyMsg{Type: tea.KeyEnter}
	if cancel {
		end = tea.KeyMsg{Type: tea.KeyEsc}
	}
	m, _ = send(t, m, end)
	return m
}

// f steps all → each kind present, alphabetical → all (FR3).
func TestInboxKindCycle(t *testing.T) {
	m := filterInbox(t)
	var got []contract.Kind
	for range 4 {
		m, _ = send(t, m, runeKey("f"))
		got = append(got, m.filters[tabInbox].kind)
	}
	want := []contract.Kind{contract.KindGrilling, contract.KindSignal, contract.KindSurgery, ""}
	if !slices.Equal(got, want) {
		t.Errorf("f steps = %v, want %v", got, want)
	}
	if nextKind("zzz", []contract.Kind{"a"}) != "" || nextKind("b", []contract.Kind{"c", "a"}) != "c" {
		t.Error("nextKind must step past a kind no longer present")
	}
}

// / applies a case-insensitive pattern over id + title; esc cancels and an
// invalid pattern keeps the previous search, with the error in the status (FR4).
func TestInboxSearch(t *testing.T) {
	m := search(t, filterInbox(t), "LAUNCH", false)
	if got := inboxIDs(m); !slices.Equal(got, []string{"g1", "s1"}) {
		t.Fatalf("/LAUNCH = %v, want g1 s1", got)
	}
	if got := inboxIDs(search(t, filterInbox(t), "^k1", false)); !slices.Equal(got, []string{"k1"}) {
		t.Errorf("/^k1 = %v, want k1 (id is searched)", got)
	}
	c := search(t, m, "sort", true)
	if c.filters[tabInbox].src != "LAUNCH" || c.editMode != editNone {
		t.Errorf("esc while typing: src %q mode %v, want LAUNCH kept, prompt closed", c.filters[tabInbox].src, c.editMode)
	}
	bad := search(t, m, "(", false)
	if bad.filters[tabInbox].src != "LAUNCH" || !strings.HasPrefix(bad.status, "search: ") || !strings.Contains(bad.status, "`(`") {
		t.Errorf("invalid pattern: src %q status %q, want LAUNCH kept and the compile error", bad.filters[tabInbox].src, bad.status)
	}
	if got := search(t, m, "", false); got.filters[tabInbox].active() {
		t.Error("an empty pattern clears the search")
	}
}

// Kind and search combine (FR5); esc clears the search, then the kind (FR7); the
// filter survives a reload; selection stays on a visible row.
func TestInboxFilterCombinedClearReloadSelection(t *testing.T) {
	m := filterInbox(t)
	m.selInbox = "s1"
	m, _ = send(t, m, runeKey("f")) // grilling
	if m.selInbox != "g1" && m.selInbox != "g2" {
		t.Errorf("filtered-out selection moved to %q, want a grilling row", m.selInbox)
	}
	m = search(t, m, "launch", false)
	if got := inboxIDs(m); !slices.Equal(got, []string{"g1"}) {
		t.Fatalf("kind:grilling + /launch/ = %v, want g1", got)
	}
	if m.selInbox != "g1" {
		t.Errorf("selection = %q, want g1", m.selInbox)
	}

	next, _ := m.Update(loadedMsg{beads: m.beads})
	r := next.(Model)
	if got := inboxIDs(r); !slices.Equal(got, []string{"g1"}) || r.selInbox != "g1" {
		t.Errorf("after reload rows %v sel %q, want filter kept", got, r.selInbox)
	}

	m, _ = send(t, r, tea.KeyMsg{Type: tea.KeyEsc})
	if f := m.filters[tabInbox]; f.re != nil || f.kind != contract.KindGrilling {
		t.Errorf("first esc: %+v, want search cleared, kind kept", f)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.filters[tabInbox].active() || len(m.inboxRows()) != 4 {
		t.Errorf("second esc: %+v, want everything shown", m.filters[tabInbox])
	}

	// A search matching nothing selects nothing, rather than a hidden row.
	m = search(t, m, "nomatch", false)
	if m.selInbox != "" || m.selectedBead() != nil {
		t.Errorf("empty filtered inbox selects %q", m.selInbox)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "no matching beads") {
		t.Errorf("empty filtered inbox view:\n%s", v)
	}
}

// The filter bar heads the list pane and the tab gets ▼ (FR8); filters are per tab.
func TestInboxFilterBar(t *testing.T) {
	m := filterInbox(t)
	if v := ansi.Strip(m.View()); strings.Contains(v, "FILTER") || strings.Contains(v, "▼") {
		t.Fatalf("no filter, no bar:\n%s", v)
	}
	m, _ = send(t, m, runeKey("f"))
	m = search(t, m, "launch", false)
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	// The 48-col master column has no room for the esc hint; the count stays.
	if want := "FILTER  kind:grilling  /launch/  — 1 of 4 "; !strings.HasPrefix(lines[1], want) {
		t.Errorf("list pane first line = %q, want %q", lines[1], want)
	}
	if got, want := ansi.Strip(filterBar([]string{"kind:grilling", "/launch/"}, 7, 132, 80)),
		"FILTER  kind:grilling  /launch/  — 7 of 132 · esc clears"; strings.TrimRight(got, " ") != want {
		t.Errorf("filterBar = %q, want %q", got, want)
	}
	if !strings.Contains(lines[0], "Inbox(4)▼") || strings.Contains(lines[0], "Graph▼") {
		t.Errorf("tab bar = %q, want only Inbox marked", lines[0])
	}
	m, _ = send(t, m, runeKey("G"))
	if m.filters[tabGraph].active() || !strings.Contains(ansi.Strip(m.View()), "Inbox(4)▼") {
		t.Error("the Inbox filter is its own: Graph unfiltered, the marker still shown")
	}
}

// The bar is pinned: selecting far down a long filtered Inbox scrolls the rows,
// never the bar (FR8), in both layouts.
func TestInboxFilterBarPinned(t *testing.T) {
	var beads []bd.Bead
	for i := range 60 {
		beads = append(beads, bd.Bead{ID: fmt.Sprintf("g%02d", i), Kind: contract.KindGrilling, Status: "open", Title: "grill"})
	}
	for _, w := range []int{120, 50} {
		m := New(nil, nil)
		m.w, m.h, m.tab = w, 20, tabInbox
		next, _ := m.Update(loadedMsg{beads: beads})
		m = search(t, next.(Model), "grill", false)
		for range 50 {
			m, _ = send(t, m, runeKey("j"))
		}
		lines := strings.Split(ansi.Strip(m.View()), "\n")
		if !strings.HasPrefix(lines[1], "FILTER") || !strings.Contains(ansi.Strip(m.View()), "❯") {
			t.Errorf("w=%d: bar or selection scrolled away:\n%s", w, strings.Join(lines, "\n"))
		}
		if len(lines) != m.h {
			t.Errorf("w=%d: frame is %d lines, want %d", w, len(lines), m.h)
		}
	}
}

// graphFixture: Chart c1 (grilling + spec members), Chart c2 (one build), and an
// Unparented signal.
func graphFixture() []bd.Bead {
	return []bd.Bead{
		{ID: "c1", Kind: contract.KindChart, Container: true, Status: "open", Title: "Launch chart"},
		{ID: "g1", Kind: contract.KindGrilling, Parent: "c1", Status: "open", Title: "Grill flow"},
		{ID: "s1", Kind: contract.KindSpec, Parent: "c1", Status: "open", Title: "Spec launch"},
		{ID: "c2", Kind: contract.KindChart, Container: true, Status: "open", Title: "Other chart"},
		{ID: "b2", Kind: contract.KindBuild, Parent: "c2", Status: "open", Title: "Build it"},
		{ID: "u1", Kind: contract.KindSignal, Status: "open", Title: "stray"},
	}
}

// groupIDs renders groups as "head:member,member" (Unparented's head is "-").
func groupIDs(groups []group) (out []string) {
	for _, g := range groups {
		h := "-"
		if g.head != nil {
			h = g.head.ID
		}
		var ms []string
		for _, b := range g.members {
			ms = append(ms, b.ID)
		}
		out = append(out, h+":"+strings.Join(ms, ","))
	}
	slices.Sort(out)
	return out
}

// The Graph filter's Chart-group rules (FR6), alike for kind and regex (FR5).
func TestFilterGroups(t *testing.T) {
	re := func(p string) *regexp.Regexp { return regexp.MustCompile("(?i)" + p) }
	for _, c := range []struct {
		name  string
		f     listFilter
		want  []string
		heads []string
	}{
		{"head match keeps all members, folds", listFilter{re: re("^c1 ")}, []string{"c1:g1,s1"}, []string{"c1"}},
		{"head kind match", listFilter{kind: contract.KindChart}, []string{"c1:g1,s1", "c2:b2"}, []string{"c1", "c2"}},
		{"member match shows head, hides siblings", listFilter{re: re("grill")}, []string{"c1:g1"}, nil},
		{"member kind match", listFilter{kind: contract.KindBuild}, []string{"c2:b2"}, nil},
		{"unparented member", listFilter{kind: contract.KindSignal}, []string{"-:u1"}, nil},
		{"no match drops every group", listFilter{re: re("nomatch")}, nil, nil},
		{"kind + regex must both match", listFilter{kind: contract.KindSpec, re: re("launch")}, []string{"c1:s1"}, nil},
		{"kind + regex, regex matches head of other kind", listFilter{kind: contract.KindGrilling, re: re("launch")}, nil, nil},
	} {
		got, heads := filterGroups(buildGraph(graphFixture()), c.f)
		if g := groupIDs(got); !slices.Equal(g, c.want) || !slices.Equal(heads, c.heads) {
			t.Errorf("%s: groups %v heads %v, want %v %v", c.name, g, heads, c.want, c.heads)
		}
	}
}

func filterGraph(t *testing.T) Model {
	t.Helper()
	m := New(nil, nil)
	m.w, m.h = 120, 30
	next, _ := m.Update(loadedMsg{beads: graphFixture()})
	return next.(Model)
}

// A head match folds on apply; space unfolds and a reload keeps it unfolded;
// clearing restores the pre-filter collapse map; esc clears search then kind (FR6–7).
func TestGraphFilterCollapse(t *testing.T) {
	m := filterGraph(t)
	m.collapsed["c2"] = true        // the human's own fold, from before the filter
	m, _ = send(t, m, runeKey("f")) // build: c2 is a member match only
	m, _ = send(t, m, runeKey("f")) // chart
	if m.filters[tabGraph].kind != contract.KindChart || !m.collapsed["c1"] || !m.collapsed["c2"] {
		t.Fatalf("kind:chart: kind %q collapsed %v, want both heads folded", m.filters[tabGraph].kind, m.collapsed)
	}
	m = search(t, m, "launch", false)
	if got := groupIDs(m.groups); !slices.Equal(got, []string{"c1:g1,s1"}) {
		t.Fatalf("kind:chart + /launch/ = %v, want c1 whole", got)
	}
	m.selGraph = "c1"
	m, _ = send(t, m, runeKey(" "))
	next, _ := m.Update(loadedMsg{beads: m.beads})
	m = next.(Model)
	if m.collapsed["c1"] {
		t.Error("space unfolded c1; a reload must not fold it again")
	}

	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if f := m.filters[tabGraph]; f.re != nil || f.kind != contract.KindChart {
		t.Fatalf("first esc: %+v, want search cleared, kind kept", f)
	}
	if m.collapsed["c1"] {
		t.Error("a partial clear must not fold c1 again")
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.filters[tabGraph].active() || m.preFilter != nil {
		t.Fatalf("second esc: %+v, want unfiltered", m.filters[tabGraph])
	}
	if m.collapsed["c1"] || !m.collapsed["c2"] {
		t.Errorf("cleared: collapsed %v, want the pre-filter map (c2 only)", m.collapsed)
	}
	if got := groupIDs(m.groups); len(got) != 3 {
		t.Errorf("cleared: groups %v, want all three", got)
	}
}

// The Graph's bar counts its matching beads, not the context-only head c1; ▼
// marks the tab; an empty match selects nothing (FR8).
func TestGraphFilterBar(t *testing.T) {
	m := search(t, filterGraph(t), "grill", false)
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	if want := "FILTER  /grill/  — 1 of 6"; !strings.HasPrefix(lines[1], want) {
		t.Errorf("list pane first line = %q, want %q", lines[1], want)
	}
	if !strings.Contains(lines[0], "Graph▼") {
		t.Errorf("tab bar = %q, want Graph▼", lines[0])
	}
	if !strings.Contains(m.footer(), "esc clear") {
		t.Errorf("footer = %q, want esc clear", m.footer())
	}
	m = search(t, m, "nomatch", false)
	if m.selGraph != "" || !strings.Contains(ansi.Strip(m.View()), "no matching beads") {
		t.Errorf("empty filtered graph: sel %q\n%s", m.selGraph, ansi.Strip(m.View()))
	}
}

// A fold on apply moves a selection inside the group to its head, not the top.
func TestGraphFilterFoldKeepsSelectionInGroup(t *testing.T) {
	m := filterGraph(t)
	m.selGraph = "b2"
	m = search(t, m, "^c2 ", false)
	if !m.collapsed["c2"] || m.selGraph != "c2" {
		t.Errorf("collapsed %v sel %q, want c2 folded and selected", m.collapsed, m.selGraph)
	}
}

// A member-only match unfolds its group on apply, so the match is a visible row
// even when the human had folded the group before filtering (FR6).
func TestGraphFilterMemberMatchUnfolds(t *testing.T) {
	m := filterGraph(t)
	m.collapsed["c2"] = true
	m = search(t, m, "^b2 ", false)
	if !slices.ContainsFunc(navRows(m.groups, m.collapsed), func(r row) bool { return r.sel == "b2" }) {
		t.Errorf("collapsed %v: b2 hidden in a folded c2", m.collapsed)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if !m.collapsed["c2"] {
		t.Error("cleared: want c2's pre-filter fold restored")
	}
}
