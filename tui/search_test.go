package tui

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// searchBeads is a small snapshot: every status, two kinds, a kindless bead,
// distinct updated/created/priority orders and one updated tie.
func searchBeads() []bd.Bead {
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	day := func(n int) time.Time { return t0.Add(time.Duration(n) * 24 * time.Hour) }
	return []bd.Bead{
		{ID: "mc-a", Kind: contract.KindGrilling, Status: "open", Title: "Grill the pivot rule", Priority: 2, Updated: day(3), Created: day(1)},
		{ID: "mc-b", Kind: contract.KindBuild, Status: "in_progress", Title: "Build the bar", Priority: 0, Updated: day(5), Created: day(0)},
		{ID: "mc-c", Kind: contract.KindGrilling, Status: "blocked", Title: "Blocked grill", Priority: 1, Updated: day(1), Created: day(4)},
		{ID: "mc-d", Kind: contract.KindBuild, Status: "deferred", Title: "Deferred build", Priority: 3, Updated: day(3), Created: day(2)},
		{ID: "mc-e", Kind: contract.KindGrilling, Status: "closed", Title: "Closed PIVOT grill", Priority: 4, Updated: day(9), Created: day(3)},
		{ID: "mc-x", Status: "open", Title: "Kindless pivot grill", Updated: day(10)},
		{ID: "mc-v", Kind: contract.KindGrilling, Event: true, Status: "open", Title: "Event pivot grill", Updated: day(11)},
	}
}

func beadIDs(bs []bd.Bead) []string {
	var out []string
	for _, b := range bs {
		out = append(out, b.ID)
	}
	return out
}

// D3: kindless and event beads never list; each status facet; kind; multi-word AND over
// id + title; each sort's order with an id tiebreak; M is the kinded population.
func TestSearchResults(t *testing.T) {
	for _, c := range []struct {
		name string
		q    searchQuery
		want []string
	}{
		{"defaults: unclosed, newest updated, id tiebreak", searchQuery{status: statusUnclosed}, []string{"mc-b", "mc-a", "mc-d", "mc-c"}},
		{"any keeps closed", searchQuery{status: statusAny}, []string{"mc-e", "mc-b", "mc-a", "mc-d", "mc-c"}},
		{"open", searchQuery{status: "open"}, []string{"mc-a"}},
		{"in_progress", searchQuery{status: "in_progress"}, []string{"mc-b"}},
		{"blocked", searchQuery{status: "blocked"}, []string{"mc-c"}},
		{"deferred", searchQuery{status: "deferred"}, []string{"mc-d"}},
		{"closed", searchQuery{status: "closed"}, []string{"mc-e"}},
		{"kind", searchQuery{status: statusAny, kind: contract.KindBuild}, []string{"mc-b", "mc-d"}},
		{"words AND, any order, any case", searchQuery{status: statusAny, text: "  GRILL  pivot "}, []string{"mc-e", "mc-a"}},
		{"id fragment", searchQuery{status: statusAny, text: "mc-c"}, []string{"mc-c"}},
		{"word over id and title", searchQuery{status: statusAny, text: "mc-d deferred"}, []string{"mc-d"}},
		{"no match", searchQuery{status: statusAny, text: "pivot bar"}, nil},
		{"priority P0 first", searchQuery{status: statusAny, sort: sortPriority}, []string{"mc-b", "mc-c", "mc-a", "mc-d", "mc-e"}},
		{"created newest first", searchQuery{status: statusAny, sort: sortCreated}, []string{"mc-c", "mc-e", "mc-d", "mc-a", "mc-b"}},
	} {
		got, total := searchResults(searchBeads(), c.q)
		if !slices.Equal(beadIDs(got), c.want) || total != 5 {
			t.Errorf("%s: got %v of %d, want %v of 5", c.name, beadIDs(got), total, c.want)
		}
	}
}

// searchModel is a Model on the Search tab over searchBeads, as a load leaves it.
func searchModel(t *testing.T, w int) Model {
	m := New(nil, nil)
	m.beads, m.w, m.h = searchBeads(), w, 24
	m.rebuildGroups()
	m.fixSelection()
	return press(t, m, "S")
}

// S lands on Search in normal mode with the two header lines, the defaults'
// rows newest-updated first, the top row selected and its detail beside it.
func TestSearchTab(t *testing.T) {
	m := searchModel(t, 120)
	if m.tab != tabSearch || m.selSearch != "mc-b" {
		t.Fatalf("S: tab=%v sel=%q, want Search on mc-b", m.tab, m.selSearch)
	}
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	if !strings.HasPrefix(lines[1], "/ search") || !strings.HasPrefix(lines[2], "unclosed · any · ↓upd   4 of 5") {
		t.Errorf("header lines = %q / %q", lines[1], lines[2])
	}
	var order []string
	for _, l := range lines[3:7] {
		for _, b := range searchBeads() {
			if strings.Contains(l, b.Title) {
				order = append(order, b.ID)
			}
		}
	}
	if want := []string{"mc-b", "mc-a", "mc-d", "mc-c"}; !slices.Equal(order, want) {
		t.Errorf("rows = %v, want %v:\n%s", order, want, strings.Join(lines, "\n"))
	}
	if !strings.Contains(m.View(), "mc-b") || !strings.Contains(lines[3], "❯") {
		t.Errorf("selected row or its detail id missing:\n%s", strings.Join(lines, "\n"))
	}
	if m = press(t, m, "j"); m.selSearch != "mc-a" {
		t.Errorf("j: sel=%q, want mc-a", m.selSearch)
	}
	if next, _ := m.updateKey(tea.KeyMsg{Type: tea.KeyEnter}); !next.(Model).focusDetail {
		t.Error("enter did not open the detail")
	}
	if got := press(t, m, "S"); got.tab != tabSearch || got.selSearch != "mc-a" {
		t.Errorf("S on Search: tab=%v sel=%q, want a no-op", got.tab, got.selSearch)
	}
}

// No bead action is bound on Search (D5): the keys that act on the selection
// elsewhere change nothing and run nothing, and the footer offers none.
func TestSearchNoBeadActions(t *testing.T) {
	m := searchModel(t, 120)
	m.sessions = map[string]session.Info{"mc-b": {State: session.Exited}}
	for _, k := range []string{"l", "t", "p", "+", "-", "x", "e", "E", "n", "h", "d"} {
		next, cmd := m.updateKey(runeKey(k))
		got := next.(Model)
		if cmd != nil || got.editMode != editNone || got.status != "" || got.hideClosedRoots != m.hideClosedRoots || got.filters != m.filters {
			t.Errorf("%s acted on Search: cmd=%v mode=%v status=%q", k, cmd != nil, got.editMode, got.status)
		}
	}
	if f := ansi.Strip(m.footer()); f != "/ search · enter · o loop on · G reveal · f status · c kind · s sort · ? · q" {
		t.Errorf("footer = %q, want only Search's view keys", f)
	}
}

// Every result row fits its column on one line at every width — 40-col single
// pane up to the 60-col cap — and only the title is cut; a closed title is in
// the Graph's closed style.
func TestSearchRowsFitColumn(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)
	for _, w := range frameWidths {
		m := adversarialModel()
		m.w, m.h, m.search.status = w, 30, statusAny
		mw := m.masterColW()
		if w >= 200 && mw != masterCap {
			t.Errorf("w=%d: column %d, want the %d-col cap", w, mw, masterCap)
		}
		list, _ := m.searchRows()
		for i := range list {
			b := &list[i]
			m.selSearch = b.ID
			l := fitRow(m.searchRow(b, mw), mw)
			if lipgloss.Width(l) > mw || strings.Contains(l, "\n") {
				t.Errorf("w=%d %s: width %d > col %d: %q", w, b.ID, lipgloss.Width(l), mw, l)
			}
			for _, s := range []string{strings.ToUpper(string(b.Kind)), m.sortValue(b), strings.TrimSpace(ansi.Strip(ageSuffix(b)))} {
				if !strings.Contains(ansi.Strip(l), s) {
					t.Errorf("w=%d %s: row lost %q: %q", w, b.ID, s, ansi.Strip(l))
				}
			}
			if pre, suf := m.searchParts(b); b.Status == "closed" && !strings.Contains(l, closedSty.Render(fitRowTitle(b.Title, mw, pre, suf))) {
				t.Errorf("w=%d %s: closed title not in the closed style: %q", w, b.ID, l)
			}
		}
	}
}

// ADR 0072: past the existing fallbacks, every tab but the active one shrinks
// to its key letter — Inbox keeping its count — and the pause and loop segments
// stay, the restarting loop included; the wide bar reads in tab order.
func TestTabBarKeyLetterFallback(t *testing.T) {
	hitl := bd.Bead{Kind: contract.KindGrilling, Status: "open", Labels: []string{"paused:manual"}}
	beads := slices.Repeat([]bd.Bead{hitl}, 12)
	sessions := map[string]session.Info{}
	for i := range beads {
		beads[i].ID = fmt.Sprint(i)
		sessions[beads[i].ID] = session.Info{State: session.Alive}
	}
	m := New(nil, nil)
	m.beads, m.sessions, m.loop, m.w = beads, sessions, loopRestart, 200
	if got := ansi.Strip(m.tabBar()); !strings.HasPrefix(strings.TrimSpace(got), "Graph  Inbox(12)  Search  History  Logs  #Stats  *Settings") {
		t.Errorf("wide bar = %q, want Graph Inbox(12) Search History Logs #Stats *Settings", got)
	}
	m.w = floorCols
	// The active tab keeps its full name (ADR 0072 §3): most run past the floor here, clipped by fitFrame.
	full := map[tab]string{tabGraph: "Graph I(12) S H L # *", tabInbox: "G Inbox(12) S H L # *", tabSearch: "G I(12) Search H L # *", tabHistory: "G I(12) S History L # *", tabLogs: "G I(12) S H Logs # *", tabStats: "G I(12) S H L #Stats *", tabSettings: "G I(12) S H L # *Settings"}
	for tb, want := range full {
		m.tab = tb
		got := ansi.Strip(m.tabBar())
		if want += " 12‖ loop restarting 2"; got != want {
			t.Errorf("%v at %d cols: bar = %q (%d wide), want %q", tb, floorCols, got, lipgloss.Width(got), want)
		}
	}
	m.tab, m.filters[tabInbox].kind = tabGraph, contract.KindGrilling
	if got := ansi.Strip(m.tabBar()); !strings.HasPrefix(got, "Graph I(12)▼ S H L") {
		t.Errorf("filtered Inbox at the floor: %q, want I(12)▼", got)
	}
}

// typeText sends each rune of s through Update, a space as the space key.
func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		msg := runeKey(string(r))
		if r == ' ' {
			msg = tea.KeyMsg{Type: tea.KeySpace}
		}
		m, _ = send(t, m, msg)
	}
	return m
}

// searchHeader is the two Search header lines as rendered.
func searchHeader(m Model) (string, string) {
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	return lines[1], lines[2]
}

// FR13–20, 27, 30, 50: / enters text input; every keystroke refilters on words
// over title and id; enter keeps, esc reverts to the pre-/ text; in normal mode
// esc clears the text only; tab-jump, help and move keys are text in input.
func TestSearchTextQuery(t *testing.T) {
	m := searchModel(t, 120)
	m.search.status = statusAny
	m, _ = send(t, m, runeKey("/"))
	if m.editMode != editSearchText || ansi.Strip(m.helpLine()) != "enter keep · esc cancel" {
		t.Fatalf("/: mode=%v footer=%q, want text input", m.editMode, ansi.Strip(m.helpLine()))
	}
	m = typeText(t, m, "pivot")
	if l1, l2 := searchHeader(m); !strings.HasPrefix(l1, "/ pivot▏") || !strings.Contains(l2, "2 of 5") {
		t.Errorf("live after pivot: %q / %q, want 2 of 5", l1, l2)
	}
	m = typeText(t, m, " grill")
	if l1, l2 := searchHeader(m); !strings.HasPrefix(l1, "/ pivot grill▏") || !strings.Contains(l2, "2 of 5") {
		t.Errorf("pivot grill: %q / %q", l1, l2)
	}
	m.search.status = statusUnclosed // "pivot grill" finds "Grill the pivot rule" alone
	if list, _ := m.searchRows(); !slices.Equal(beadIDs(list), []string{"mc-a"}) || m.selectedBead() == nil {
		t.Errorf("pivot grill unclosed: %v", beadIDs(list))
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if l1, _ := searchHeader(m); m.editMode != editNone || m.search.text != "pivot grill" || !strings.HasPrefix(l1, "/ pivot grill") || strings.Contains(l1, "▏") {
		t.Errorf("enter: mode=%v text=%q header %q, want kept without a cursor", m.editMode, m.search.text, l1)
	}
	if f := ansi.Strip(m.footer()); !strings.HasPrefix(f, "/ search · enter · ") {
		t.Errorf("normal footer = %q", f)
	}
	for _, focus := range []bool{false, true} { // / outlives the rest of the view keys at the floor
		fm := m
		fm.w, fm.focusDetail = floorCols, focus
		if f := ansi.Strip(fm.footer()); !strings.HasPrefix(f, "/ search") || lipgloss.Width(f) > floorCols {
			t.Errorf("floor footer focus=%v = %q, want / kept", focus, f)
		}
	}
	m, _ = send(t, m, runeKey("/")) // re-enters on the current text
	m = typeText(t, m, "zz")
	if _, l2 := searchHeader(m); !strings.Contains(l2, "0 of 5") || m.selSearch != "" {
		t.Errorf("pivot grillzz: %q sel=%q, want none", l2, m.selSearch)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.editMode != editNone || m.search.text != "pivot grill" || m.selSearch != "mc-a" {
		t.Errorf("esc: mode=%v text=%q sel=%q, want reverted to pivot grill", m.editMode, m.search.text, m.selSearch)
	}
	m.search.status, m.search.kind = "open", contract.KindGrilling
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if l1, _ := searchHeader(m); m.search.text != "" || m.search.status != "open" || m.search.kind != contract.KindGrilling || !strings.HasPrefix(l1, "/ search") {
		t.Errorf("normal esc: text=%q facets=%s header %q, want text cleared only", m.search.text, m.search.facets(), l1)
	}

	// An id fragment finds its bead; ?, G, j, S, I, L, f are text, not keys.
	m = searchModel(t, floorCols) // single pane, detail focused: / brings the header back
	m.focusDetail = true
	m, _ = send(t, m, runeKey("/"))
	if l1, _ := searchHeader(m); m.focusDetail || !strings.HasPrefix(l1, "/ ▏") {
		t.Errorf("/ from single-pane detail: focusDetail=%v header %q, want the list shown", m.focusDetail, l1)
	}
	m = typeText(t, m, "?GjSILf")
	for _, k := range []tea.KeyType{tea.KeyDown, tea.KeyUp, tea.KeyLeft, tea.KeyRight, tea.KeyShiftTab} {
		m, _ = send(t, m, tea.KeyMsg{Type: k})
	}
	if m.tab != tabSearch || m.help || m.search.text != "?GjSILf" || m.editMode != editSearchText {
		t.Errorf("keys in input: tab=%v help=%v text=%q, want typed", m.tab, m.help, m.search.text)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m, _ = send(t, m, runeKey("/"))
	m = typeText(t, m, "MC-C")
	if list, _ := m.searchRows(); !slices.Equal(beadIDs(list), []string{"mc-c"}) || m.selSearch != "mc-c" {
		t.Errorf("id fragment: %v sel=%q", beadIDs(list), m.selSearch)
	}
}

// pickerLines is an open picker's entries as rendered below the header, cursor
// and current marks included.
func pickerLines(m Model) []string {
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	var out []string
	for _, l := range lines[3:] {
		if !strings.HasPrefix(l, "  ") && !strings.HasPrefix(l, "❯ ") {
			break // the footer
		}
		out = append(out, strings.TrimRight(l, " "))
	}
	return out
}

// FR21–25, D6: each picker's entries in order, the cursor on the current value
// (marked ✓), j/k move, enter applies, esc cancels unchanged, every other key —
// tab jumps and / included — is swallowed; the footer reads enter pick · esc cancel.
func TestSearchPickers(t *testing.T) {
	m := searchModel(t, 120)
	m.beads = append(m.beads, bd.Bead{ID: "mc-r", Kind: contract.KindResearch, Status: "closed", Title: "r"})
	for _, c := range []struct {
		key  string
		want []string
	}{
		{"f", []string{"  any", "❯ unclosed ✓", "  open", "  in_progress", "  blocked", "  deferred", "  closed"}},
		{"c", []string{"❯ any ✓", "  build", "  grilling", "  research"}},
		{"s", []string{"❯ updated ✓", "  priority", "  created"}},
	} {
		p, _ := send(t, m, runeKey(c.key))
		if p.editMode != editSearchPick || ansi.Strip(p.helpLine()) != "enter pick · esc cancel" {
			t.Fatalf("%s: mode=%v footer=%q", c.key, p.editMode, ansi.Strip(p.helpLine()))
		}
		if got := pickerLines(p); !slices.Equal(got, c.want) {
			t.Errorf("%s picker = %q, want %q", c.key, got, c.want)
		}
		for _, k := range []string{"G", "I", "L", "S", "/", "?", "q", "f", "c", "s", "tab", "left", "right"} {
			msg := runeKey(k)
			switch k {
			case "tab":
				msg = tea.KeyMsg{Type: tea.KeyTab}
			case "left":
				msg = tea.KeyMsg{Type: tea.KeyLeft}
			case "right":
				msg = tea.KeyMsg{Type: tea.KeyRight}
			}
			got, cmd := send(t, p, msg)
			if got.tab != tabSearch || got.editMode != editSearchPick || got.help || got.search != p.search || got.pickCur != p.pickCur || cmd != nil {
				t.Errorf("%s picker: %s not swallowed (tab=%v mode=%v help=%v)", c.key, k, got.tab, got.editMode, got.help)
			}
		}
		p, _ = send(t, p, runeKey("j"))
		p, _ = send(t, p, tea.KeyMsg{Type: tea.KeyEsc})
		if p.editMode != editNone || p.search != m.search {
			t.Errorf("%s esc: mode=%v query=%+v, want unchanged", c.key, p.editMode, p.search)
		}
	}

	for _, tb := range []tab{tabGraph, tabInbox, tabLogs} { // c and s are Search's alone
		o := m
		o.tab = tb
		for _, k := range []string{"c", "s"} {
			if got, _ := send(t, o, runeKey(k)); got.editMode != editNone {
				t.Errorf("%s on tab %v opened a picker", k, tb)
			}
		}
	}

	pick := func(m Model, key string, moves ...string) Model {
		m, _ = send(t, m, runeKey(key))
		for _, k := range moves {
			m, _ = send(t, m, runeKey(k))
		}
		m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		return m
	}
	m = pick(m, "f", "j", "j", "j", "j", "j", "j", "j", "k") // clamps at closed, then back to deferred
	if m.editMode != editNone || m.search.status != "deferred" {
		t.Errorf("f pick: mode=%v status=%q, want deferred", m.editMode, m.search.status)
	}
	m = pick(m, "f", "k", "k", "k", "k", "k", "k", "k") // clamps at any
	m = pick(m, "c", "j", "j")
	m = pick(m, "s", "j")
	if _, l2 := searchHeader(m); !strings.HasPrefix(l2, "any · grilling · ↓pri   3 of 6") {
		t.Errorf("any/grilling/pri header = %q", l2)
	}
	if got, _ := m.searchRows(); !slices.Equal(beadIDs(got), []string{"mc-c", "mc-a", "mc-e"}) || m.selSearch != "mc-e" {
		t.Errorf("rows %v sel=%q, want P1 first, the selection following mc-e", beadIDs(got), m.selSearch)
	}
	if got := pickerLines(func() Model { p, _ := send(t, m, runeKey("c")); return p }()); !slices.Equal(got, []string{"  any", "  build", "❯ grilling ✓", "  research"}) {
		t.Errorf("c reopened = %q, want the cursor on the active kind", got)
	}
}

// FR26, 28, 32: facets compose with the text (text AND status AND kind, then the
// sort); header line 2 names every value; each row's suffix follows the sort.
func TestSearchFacetsCompose(t *testing.T) {
	m := searchModel(t, 120)
	for _, c := range []struct {
		q        searchQuery
		head     string
		rows     []string
		suffixes []string
	}{
		{searchQuery{status: statusUnclosed}, "unclosed · any · ↓upd   4 of 5", []string{"mc-b", "mc-a", "mc-d", "mc-c"}, nil},
		{searchQuery{status: "open", kind: contract.KindGrilling, sort: sortPriority}, "open · grilling · ↓pri   1 of 5", []string{"mc-a"}, []string{"P2"}},
		{searchQuery{status: statusAny, kind: contract.KindGrilling, sort: sortCreated, text: "grill"}, "any · grilling · ↓cre   3 of 5", []string{"mc-c", "mc-e", "mc-a"}, nil},
		{searchQuery{status: statusAny, kind: contract.KindBuild, sort: sortPriority, text: "build"}, "any · build · ↓pri   2 of 5", []string{"mc-b", "mc-d"}, []string{"P0", "P3"}},
		{searchQuery{status: "closed", kind: contract.KindBuild}, "closed · build · ↓upd   0 of 5", nil, nil},
	} {
		m.search = c.q
		m.fixSelection()
		if _, l2 := searchHeader(m); !strings.HasPrefix(l2, c.head) {
			t.Errorf("%+v: header %q, want %q", c.q, l2, c.head)
		}
		got, _ := m.searchRows()
		if !slices.Equal(beadIDs(got), c.rows) {
			t.Errorf("%+v: rows %v, want %v", c.q, beadIDs(got), c.rows)
		}
		for i, s := range c.suffixes {
			if v := m.sortValue(&got[i]); v != s {
				t.Errorf("%+v %s: suffix %q, want %q", c.q, got[i].ID, v, s)
			}
		}
	}
	b := searchBeads()[0]
	for sk, want := range map[sortKey]string{sortUpdated: HumanAge(b.Updated), sortCreated: HumanAge(b.Created), sortPriority: "P2"} {
		m.search.sort = sk
		if got := m.sortValue(&b); got != want {
			t.Errorf("sort %s suffix = %q, want %q", sortName[sk], got, want)
		}
	}
}

// FR29: s, then c, then f, then G reveal drop as the footer narrows (after r, which every tab
// drops first), / outliving them; nothing overflows down to the 40-col floor.
func TestSearchFooterDropOrder(t *testing.T) {
	m := searchModel(t, 120)
	seen := map[string]int{}
	for w := 120; w >= floorCols; w-- {
		m.w = w
		f := ansi.Strip(m.footer())
		if lipgloss.Width(f) > w || !strings.HasPrefix(f, "/ search") {
			t.Errorf("w=%d: footer %q overflows or lost /", w, f)
		}
		for _, k := range []string{"G reveal", "f status", "c kind", "s sort"} {
			if strings.Contains(f, k) {
				seen[k] = w
			}
		}
	}
	if !(seen["s sort"] > seen["c kind"] && seen["c kind"] > seen["f status"] && seen["f status"] > seen["G reveal"] && seen["G reveal"] > floorCols) {
		t.Errorf("narrowest width each kept: %v, want s dropped before c before f before G, G gone by the floor", seen)
	}
}

// revealModel is on Search (status any) over a tree with two open Charts, a
// closed-root Chart and closed/open root Signals, h on as at startup.
func revealModel(t *testing.T) Model {
	m := New(nil, nil)
	m.w, m.h = 120, 24
	m.beads = []bd.Bead{
		{ID: "chA", Kind: contract.KindChart, Container: true, Title: "Chart A", Status: "in_progress"},
		{ID: "a1", Kind: contract.KindGrilling, Parent: "chA", Status: "open", Title: "a one"},
		{ID: "chB", Kind: contract.KindChart, Container: true, Title: "Chart B", Status: "open"},
		{ID: "b1", Kind: contract.KindGrilling, Parent: "chB", Status: "open", Title: "b one"},
		{ID: "chDone", Kind: contract.KindChart, Container: true, Title: "Chart done", Status: "closed"},
		{ID: "d1", Kind: contract.KindGrilling, Parent: "chDone", Status: "closed", Title: "d one"},
		{ID: "sigOpen", Kind: contract.KindSignal, Status: "open", Title: "sig open"},
		{ID: "sigDone", Kind: contract.KindSignal, Status: "closed", Title: "sig done"},
	}
	m.rebuildGroups()
	m.fixSelection()
	m = press(t, m, "S")
	m.search.status = statusAny
	return m
}

// D7 (FR38, 40, 41, 47): G on a result lands the Graph on it — a Chart on its
// header — flipping h only for a hidden closed root, unfolding only its group,
// and leaving Search's query and selection for S to return to.
func TestSearchReveal(t *testing.T) {
	for _, c := range []struct {
		id, status string
		h          bool
	}{
		{"a1", "", true},
		{"chB", "", true},
		{"sigDone", "showing closed root beads", false},
		{"chDone", "showing closed root beads", false},
		{"d1", "showing closed root beads", false},
	} {
		m := revealModel(t)
		m.search.text = c.id
		m.selSearch = c.id
		m.collapsed["chA"], m.collapsed["chB"] = true, true
		q := m.search
		m = press(t, m, "G")
		rows := navRows(m.groups, m.collapsed)
		i := indexRow(rows, m.selGraph)
		if m.tab != tabGraph || m.selGraph != c.id || i < 0 {
			t.Errorf("%s: tab=%v sel=%q visible=%v, want Graph on it", c.id, m.tab, m.selGraph, i >= 0)
			continue
		}
		if want := m.beads[slices.IndexFunc(m.beads, func(b bd.Bead) bool { return b.ID == c.id })].Container; rows[i].header != want {
			t.Errorf("%s: header=%v, want %v", c.id, rows[i].header, want)
		}
		if m.hideClosedRoots != c.h || m.status != c.status {
			t.Errorf("%s: h=%v status=%q, want %v %q", c.id, m.hideClosedRoots, m.status, c.h, c.status)
		}
		key, _ := groupOf(m.graphAll, c.id)
		for _, g := range []string{"chA", "chB"} {
			if m.collapsed[g] != (g != key) {
				t.Errorf("%s: %s collapsed=%v, want only the target's group unfolded", c.id, g, m.collapsed[g])
			}
		}
		if m = press(t, m, "S"); m.search != q || m.selSearch != c.id {
			t.Errorf("%s: back on Search query=%+v sel=%q, want %+v %q", c.id, m.search, m.selSearch, q, c.id)
		}
	}
}

// G clears a Graph filter only when it hides the target, restoring
// the pre-filter folds before unfolding the target's group; the note says so.
func TestSearchRevealGraphFilter(t *testing.T) {
	for _, c := range []struct {
		name, id string
		f        listFilter
		cleared  bool
		status   string
	}{
		{"kind hides", "a1", listFilter{kind: contract.KindSignal}, true, "cleared Graph filter"},
		{"regex hides", "a1", listFilter{re: regexp.MustCompile("(?i)b one"), src: "b one"}, true, "cleared Graph filter"},
		{"target matches", "a1", listFilter{kind: contract.KindGrilling}, false, ""},
		{"h and filter hide", "sigDone", listFilter{kind: contract.KindGrilling}, true, "showing closed root beads · cleared Graph filter"},
	} {
		m := revealModel(t)
		m.collapsed["chA"], m.collapsed["chB"] = true, true
		next, _ := m.switchTab(tabGraph)
		m = next.(Model)
		m.filters[tabGraph] = c.f
		m.filterChanged(true)
		m = press(t, m, "S")
		m.search.text, m.selSearch = c.id, c.id
		q := m.search
		m = press(t, m, "G")
		if back := press(t, m, "S"); back.search != q || back.selSearch != c.id {
			t.Errorf("%s: back on Search query=%+v sel=%q, want %+v %q", c.name, back.search, back.selSearch, q, c.id)
		}
		i := indexRow(navRows(m.groups, m.collapsed), m.selGraph)
		if m.tab != tabGraph || m.selGraph != c.id || i < 0 {
			t.Errorf("%s: tab=%v sel=%q visible=%v, want Graph on %s", c.name, m.tab, m.selGraph, i >= 0, c.id)
			continue
		}
		if m.filters[tabGraph].active() == c.cleared || m.status != c.status {
			t.Errorf("%s: filter active=%v status=%q, want cleared=%v %q", c.name, m.filters[tabGraph].active(), m.status, c.cleared, c.status)
		}
		if c.status != "" && !strings.Contains(ansi.Strip(m.View()), c.status) {
			t.Errorf("%s: View() lacks %q", c.name, c.status)
		}
		if !c.cleared {
			continue
		}
		key, _ := groupOf(m.graphAll, c.id)
		for _, g := range []string{"chA", "chB"} {
			if m.collapsed[g] != (g != key) {
				t.Errorf("%s: %s collapsed=%v, want the snapshot back, only the target's group unfolded", c.name, g, m.collapsed[g])
			}
		}
	}
}

// FR39: with no results G is a plain jump keeping the Graph's own selection.
func TestSearchRevealNoSelection(t *testing.T) {
	m := revealModel(t)
	m.selGraph = "b1"
	m.search.text = "nothing matches"
	m.fixSelection()
	m = press(t, m, "G")
	if m.tab != tabGraph || m.selGraph != "b1" || !m.hideClosedRoots || m.status != "" {
		t.Errorf("tab=%v sel=%q h=%v status=%q, want a plain jump to b1", m.tab, m.selGraph, m.hideClosedRoots, m.status)
	}
}

// reload is the 3s refresh delivering beads.
func reload(t *testing.T, m Model, beads []bd.Bead) Model {
	t.Helper()
	next, _ := m.Update(loadedMsg{beads: beads})
	return next.(Model)
}

// withBead is searchBeads with id's bead edited by f, or dropped when f is nil.
func withBead(id string, f func(*bd.Bead)) []bd.Bead {
	var out []bd.Bead
	for _, b := range searchBeads() {
		if b.ID == id {
			if f == nil {
				continue
			}
			f(&b)
		}
		out = append(out, b)
	}
	return out
}

// D8 (FR42–44): across reloads the selection follows its bead by id — hidden or
// not — and scrolls into view; a dropped bead hands it to the row now at its old
// index (clamped to the last); empty selects none, and results' return the top row.
func TestSearchSelectionSurvivesReload(t *testing.T) {
	m := searchModel(t, 120) // unclosed by updated: b a d c
	m.selSearch = "mc-d"
	m = reload(t, m, withBead("mc-d", func(b *bd.Bead) { b.Updated = b.Updated.Add(30 * 24 * time.Hour) }))
	if list, _ := m.searchRows(); list[0].ID != "mc-d" || m.selSearch != "mc-d" {
		t.Errorf("re-sort: %v sel=%q, want mc-d followed to the top", beadIDs(list), m.selSearch)
	}
	m = press(t, m, "G") // hidden: still tracked
	m = reload(t, m, searchBeads())
	if m = press(t, m, "S"); m.selSearch != "mc-d" {
		t.Errorf("reload while on Graph: sel=%q, want mc-d", m.selSearch)
	}

	m.selSearch = "mc-a" // index 1
	m = reload(t, m, withBead("mc-a", func(b *bd.Bead) { b.Status = "closed" }))
	if m.selSearch != "mc-d" {
		t.Errorf("drop-out: sel=%q, want mc-d now at index 1", m.selSearch)
	}
	m = reload(t, m, searchBeads()) // b a d c
	m.selSearch = "mc-c"            // the last row
	m = reload(t, m, withBead("mc-c", nil))
	if m.selSearch != "mc-d" {
		t.Errorf("last dropped: sel=%q, want clamped to mc-d", m.selSearch)
	}

	m = reload(t, m, nil)
	if _, l2 := searchHeader(m); m.selSearch != "" || m.selectedBead() != nil || !strings.Contains(l2, "0 of 0") {
		t.Errorf("empty: sel=%q header %q, want none", m.selSearch, l2)
	}
	if m = reload(t, m, searchBeads()); m.selSearch != "mc-b" {
		t.Errorf("results back: sel=%q, want top row mc-b", m.selSearch)
	}

	// A selection that moves from the top to the bottom of a long list stays on screen.
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	var many []bd.Bead
	for i := range 40 {
		many = append(many, bd.Bead{ID: fmt.Sprintf("ws-%02d", i), Kind: contract.KindBuild, Status: "open", Title: fmt.Sprintf("row %02d", i), Updated: t0.Add(time.Duration(i) * time.Hour)})
	}
	m = reload(t, m, many)
	m.selSearch = "ws-39" // newest: the top row
	many[39].Updated = t0.Add(-time.Hour)
	m = reload(t, m, many)
	if v := ansi.Strip(m.View()); m.selSearch != "ws-39" || !strings.Contains(v, "row 39") {
		t.Errorf("sel=%q, want ws-39 followed to the bottom and in view:\n%s", m.selSearch, v)
	}
}

// FR45: a vanished kind stays active — 0 of N — and listed, marked, in its picker.
func TestSearchVanishedKindStays(t *testing.T) {
	m := searchModel(t, 120)
	m.search.kind = contract.KindBuild
	var noBuild []bd.Bead
	for _, b := range searchBeads() {
		if b.Kind != contract.KindBuild {
			noBuild = append(noBuild, b)
		}
	}
	m = reload(t, m, noBuild)
	if _, l2 := searchHeader(m); m.search.kind != contract.KindBuild || !strings.Contains(l2, "unclosed · build · ↓upd   0 of 3") || m.selSearch != "" {
		t.Errorf("vanished kind: kind=%q header %q sel=%q", m.search.kind, l2, m.selSearch)
	}
	m = press(t, m, "c")
	if got, want := pickerLines(m), []string{"  any", "❯ build ✓", "  grilling"}; !slices.Equal(got, want) {
		t.Errorf("c picker = %q, want %q", got, want)
	}
}

// FR46: a reload never interrupts / input (text and esc baseline kept, results
// live) or an open picker (entries rebuilt, cursor kept by value, else the
// nearest neighbour by its old index).
func TestSearchReloadDuringInput(t *testing.T) {
	m := searchModel(t, 120)
	m.search.text = "grill"
	m, _ = send(t, m, runeKey("/"))
	m = typeText(t, m, " pivot")
	m = reload(t, m, append(searchBeads(), bd.Bead{ID: "mc-n", Kind: contract.KindBuild, Status: "open", Title: "new pivot grill"}))
	if _, l2 := searchHeader(m); m.editMode != editSearchText || m.search.text != "grill pivot" || !strings.Contains(l2, "2 of 6") {
		t.Errorf("reload in input: mode=%v text=%q header %q, want typing kept, live 2 of 6", m.editMode, m.search.text, l2)
	}
	m = typeText(t, m, "x")
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.search.text != "grill" {
		t.Errorf("esc after reload: text=%q, want the pre-/ grill", m.search.text)
	}

	withKinds := func(kinds ...contract.Kind) []bd.Bead {
		bs := searchBeads()
		for _, k := range kinds {
			bs = append(bs, bd.Bead{ID: "mc-" + string(k), Kind: k, Status: "open", Title: string(k)})
		}
		return bs
	}
	m = reload(t, searchModel(t, 120), withKinds(contract.KindResearch))
	m = press(t, m, "c")
	m, _ = send(t, m, runeKey("k")) // any build grilling research: up from any stays on any
	for range 2 {
		m, _ = send(t, m, runeKey("j"))
	}
	m = reload(t, m, withKinds(contract.KindChart, contract.KindResearch))
	if got, want := pickerLines(m), []string{"  any ✓", "  build", "  chart", "❯ grilling", "  research"}; m.editMode != editSearchPick || !slices.Equal(got, want) {
		t.Errorf("kind added: %q, want cursor kept on grilling", got)
	}
	m, _ = send(t, m, runeKey("j"))
	m = reload(t, m, withKinds(contract.KindChart))
	if got, want := pickerLines(m), []string{"  any ✓", "  build", "  chart", "❯ grilling"}; !slices.Equal(got, want) {
		t.Errorf("cursor's kind gone: %q, want its old index clamped to grilling", got)
	}
	m, _ = send(t, m, runeKey("k"))
	m = reload(t, m, withKinds(contract.KindResearch))
	if got, want := pickerLines(m), []string{"  any ✓", "  build", "❯ grilling", "  research"}; !slices.Equal(got, want) {
		t.Errorf("chart gone mid-list: %q, want grilling now at its index", got)
	}
}

// FR47–48: leaving Search by any route and coming back with S keeps the whole
// query and selection; a fresh Model starts on the defaults.
func TestSearchStateSurvivesTabSwitches(t *testing.T) {
	m := searchModel(t, 120)
	m.search = searchQuery{text: "grill", status: statusAny, kind: contract.KindGrilling, sort: sortCreated}
	m.selSearch = "mc-e"
	q := m.search
	for _, away := range []tea.KeyMsg{runeKey("G"), runeKey("I"), runeKey("L"), {Type: tea.KeyLeft}, {Type: tea.KeyRight}, {Type: tea.KeyShiftTab}} {
		m, _ = send(t, m, away)
		if m.tab == tabSearch {
			t.Fatalf("%v stayed on Search", away)
		}
		m = reload(t, m, searchBeads())
		if m = press(t, m, "S"); m.tab != tabSearch || m.search != q || m.selSearch != "mc-e" {
			t.Errorf("back from %v: query=%+v sel=%q, want %+v mc-e", away, m.search, m.selSearch, q)
		}
	}

	f := New(nil, nil)
	if f.search != (searchQuery{status: statusUnclosed}) || f.selSearch != "" {
		t.Errorf("fresh Model: query=%+v sel=%q, want empty · unclosed · any · updated", f.search, f.selSearch)
	}
	if f = searchModel(t, 120); f.selSearch != "mc-b" {
		t.Errorf("fresh Search sel=%q, want the top row", f.selSearch)
	}
}

// Spec FR1–5: G on an Inbox row runs the same reveal — Graph on the bead,
// h flipped only for a member of a hidden closed-root Chart, only its group
// unfolded — and leaves Inbox's filter, search and selection for I to return to.
// (Inbox lists no Charts, and a closed root it lists has a session so is never
// hidden: the header landing and closed-root leaf are TestSearchReveal's.)
func TestInboxReveal(t *testing.T) {
	for _, c := range []struct {
		id, status string
		h          bool
	}{
		{"a1", "", true},
		{"sigOpen", "", true},
		{"d2", "showing closed root beads", false},
	} {
		m := revealModel(t)
		m.beads = append(m.beads, bd.Bead{ID: "d2", Kind: contract.KindGrilling, Parent: "chDone", Status: "open", Title: "d two"})
		m.rebuildGroups()
		m = press(t, m, "I")
		kind := m.beads[slices.IndexFunc(m.beads, func(b bd.Bead) bool { return b.ID == c.id })].Kind
		m.filters[tabInbox] = listFilter{kind: kind, re: regexp.MustCompile(".")}
		m.selInbox = c.id
		if indexBead(m.inboxRows(), c.id) < 0 {
			t.Fatalf("%s: not an Inbox row", c.id)
		}
		m.collapsed["chA"], m.collapsed["chB"] = true, true
		f := m.filters[tabInbox]
		m = press(t, m, "G")
		if i := indexRow(navRows(m.groups, m.collapsed), m.selGraph); m.tab != tabGraph || m.selGraph != c.id || i < 0 {
			t.Errorf("%s: tab=%v sel=%q visible=%v, want Graph on it", c.id, m.tab, m.selGraph, i >= 0)
			continue
		}
		if m.hideClosedRoots != c.h || m.status != c.status {
			t.Errorf("%s: h=%v status=%q, want %v %q", c.id, m.hideClosedRoots, m.status, c.h, c.status)
		}
		key, _ := groupOf(m.graphAll, c.id)
		for _, g := range []string{"chA", "chB"} {
			if m.collapsed[g] != (g != key) {
				t.Errorf("%s: %s collapsed=%v, want only the target's group unfolded", c.id, g, m.collapsed[g])
			}
		}
		if m = press(t, m, "I"); m.filters[tabInbox] != f || m.selInbox != c.id {
			t.Errorf("%s: back on Inbox filter=%+v sel=%q, want %+v %q", c.id, m.filters[tabInbox], m.selInbox, f, c.id)
		}
	}
}

// FR2: an Inbox with no row (here fully filtered) makes G a plain jump keeping
// the Graph's own selection; FR7: the footer carries no G hint.
func TestInboxRevealNoSelection(t *testing.T) {
	m := revealModel(t)
	m = press(t, m, "I")
	if f := ansi.Strip(m.footer()); strings.HasPrefix(f, "G ") || strings.Contains(f, "· G ") {
		t.Errorf("Inbox footer %q, want no G hint", f)
	}
	m.selGraph = "b1"
	m.filters[tabInbox] = listFilter{re: regexp.MustCompile("nothing matches")}
	m.fixSelection()
	m = press(t, m, "G")
	if m.tab != tabGraph || m.selGraph != "b1" || !m.hideClosedRoots || m.status != "" {
		t.Errorf("tab=%v sel=%q h=%v status=%q, want a plain jump to b1", m.tab, m.selGraph, m.hideClosedRoots, m.status)
	}
}
