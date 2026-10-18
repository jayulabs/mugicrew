package tui

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// jumpModel is a loaded model whose fold file lives in a temp common dir and whose
// Recents lists the ids, most recent first.
func jumpModel(t *testing.T, ids ...string) (Model, string) {
	t.Helper()
	common := t.TempDir()
	old := commonDir
	commonDir = func() (string, error) { return common, nil }
	t.Cleanup(func() { commonDir = old })
	return recentsModel(t, focusFixture(), ids...), common
}

// jumpTo opens Recents and presses enter on id's row.
func recentJump(t *testing.T, m Model, id string) Model {
	t.Helper()
	m = press(t, m, runeKey("R"))
	if !recentsOpen(m) {
		t.Fatal("no Recents")
	}
	for range 30 {
		if beadAtRow(m.modals[0].items[m.modals[0].cur], id) {
			return press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		}
		m = press(t, m, runeKey("j"))
	}
	t.Fatalf("no row for %s", id)
	return m
}

func beadAtRow(row, id string) bool {
	return strings.HasSuffix(ansi.Strip(row), id)
}

// selectExec moves the Execute cursor to the row with key.
func selectExec(t *testing.T, m Model, key string) Model {
	t.Helper()
	for range 60 {
		if m.tabAt(tabExecute).(execTab).sel == key {
			return m
		}
		m = press(t, m, runeKey("j"))
	}
	t.Fatalf("no row %q", key)
	return m
}

// jumpFlip is the active queue tab's flip set.
func jumpFlip(m Model) map[string]bool {
	switch t := m.tabAt(m.tab).(type) {
	case ideaTab:
		return t.flip
	case defogTab:
		return t.flip
	case execTab:
		return t.flip
	case initsTab:
		return t.flip
	}
	return nil
}

func TestRecentsJumpHomeTabSelectsBead(t *testing.T) {
	for _, c := range []struct {
		id  string
		tab int
	}{{"s2", tabIdeate}, {"g", tabDefog}, {"mem", tabExecute}, {"ini2", tabInits}} {
		m, _ := jumpModel(t, c.id)
		m = press(t, m, runeKey("6")) // away from home
		m = recentJump(t, m, c.id)
		if b := m.selection(); m.tab != c.tab || len(m.modals) != 0 || b == nil || b.ID != c.id {
			t.Errorf("%s: tab %d modals %d selection %v; want tab %d", c.id, m.tab, len(m.modals), b, c.tab)
		}
	}
}

func TestRecentsJumpUnfoldsMinimumAndNothingWhenVisible(t *testing.T) {
	m, _ := jumpModel(t, "mem", "s1", "s4")
	m = recentJump(t, m, "mem") // Execute: Initiative > Checkpoint > mem, both folded
	if got, want := jumpFlip(m), map[string]bool{"i:ini": true, "cp": true}; !maps.Equal(got, want) {
		t.Errorf("mem unfolded %v, want %v", got, want)
	}
	before := maps.Clone(jumpFlip(m))
	m = recentJump(t, m, "mem") // already visible: nothing more
	if !maps.Equal(before, jumpFlip(m)) {
		t.Errorf("visible row unfolded: %v -> %v", before, jumpFlip(m))
	}
	// s1 is on Ideate's Actionable list: its twin is visible, so nothing unfolds
	m = recentJump(t, m, "s1")
	if m.tab != tabIdeate || len(jumpFlip(m)) != 0 {
		t.Errorf("s1: tab %d flip %v", m.tab, jumpFlip(m))
	}
	m = recentJump(t, m, "s4") // blocked: only under its Initiative group
	if got := jumpFlip(m); m.tab != tabIdeate || !maps.Equal(got, map[string]bool{"i:ini": true}) {
		t.Errorf("s4: tab %d flip %v", m.tab, got)
	}
}

func TestRecentsJumpUnfoldsClosedInitiative(t *testing.T) {
	old := dbead("old", contract.KindInitiative, "closed", "", 1, titled("Shipped"))
	m := recentsModel(t, append(focusFixture(), old), "old")
	m = recentJump(t, m, "old")
	if b := m.selection(); m.tab != tabInits || b == nil || b.ID != "old" || !jumpFlip(m)[initsClosed] {
		t.Errorf("tab %d selection %v flip %v", m.tab, b, jumpFlip(m))
	}
}

func TestRecentsJumpUnfoldsAreSessionOnly(t *testing.T) {
	m, common := jumpModel(t, "mem")
	m = recentJump(t, m, "mem")
	if _, err := os.Stat(foldFile(common)); err == nil {
		t.Errorf("jump saved folds: %s", readSection(t, common, "execute"))
	}
	// space on a different container saves, without the jump's unfolds
	m = selectExec(t, m, actKey)
	m = press(t, m, runeKey(" "))
	var saved map[string]bool
	json.Unmarshal([]byte(readSection(t, common, "execute")), &saved)
	if !maps.Equal(saved, map[string]bool{actKey: true}) {
		t.Errorf("saved %v, want only the human's flip", saved)
	}
	// flipping a jump-unfolded container by hand makes it the human's
	m = selectExec(t, m, "cp")
	m = press(t, m, runeKey(" ")) // folds it back
	m = press(t, m, runeKey(" ")) // unfolds it: now by hand
	saved = nil
	json.Unmarshal([]byte(readSection(t, common, "execute")), &saved)
	if !maps.Equal(saved, map[string]bool{actKey: true, "cp": true}) {
		t.Errorf("saved %v, want the act section and cp", saved)
	}
}

func TestRecentsJumpLeavesFocusAndRecordsNothing(t *testing.T) {
	m, _ := jumpModel(t, "s1", "s2")
	m = ideaFocus(t, m)
	if !focused(m) {
		t.Fatal("not focused")
	}
	m = recentJump(t, m, "s2")
	if focused(m) || m.tab != tabIdeate {
		t.Errorf("focused %v tab %d", focused(m), m.tab)
	}
	if got := loadRecents(recentsPath()); len(got) != 2 || got[0] != "s1" {
		t.Errorf("jump reordered Recents: %v", got)
	}
}

func TestRecentsJumpNotListedScopesGraph(t *testing.T) {
	m, common := jumpModel(t, "ini", "s5") // s5 is closed: not on Ideate
	m = press(t, m, runeKey("6"))
	m = recentJump(t, m, "s5")
	g := m.tabAt(tabGraph).(graphTab)
	want := "not on Ideate → Graph (scoped to s5, esc clears)"
	if m.tab != tabGraph || m.status != want || g.q != (query{words: "s5", status: statusAny}) || g.sel != "s5" {
		t.Errorf("tab %d status %q q %+v sel %q", m.tab, m.status, g.q, g.sel)
	}
	path := filepath.Join(common, "mugicrew", "graph-filter.json")
	if _, err := os.Stat(path); err == nil {
		t.Error("scope saved to graph-filter.json")
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if g := m.tabAt(tabGraph).(graphTab); g.q != baseline || g.visit {
		t.Errorf("esc left q %+v visit %v", g.q, g.visit)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("esc saved graph-filter.json")
	}
}

// a filter the human picks after a fallback jump is theirs, not the visit's.
func TestRecentsJumpVisitEndsWhenHumanChangesQuery(t *testing.T) {
	savedQuery := func(common string) query { return loadQuery(filepath.Join(common, "mugicrew", "graph-filter.json")) }
	visit := func() (Model, string) {
		m, common := jumpModel(t, "ini", "s5")
		m = press(t, m, runeKey("6"))
		return recentJump(t, m, "s5"), common
	}

	m, common := visit()
	m = press(t, m, runeKey("s"))
	m = press(t, m, runeKey("j"))
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // picks the highlighted status
	want := m.tabAt(tabGraph).(graphTab).q
	if got := savedQuery(common); got != want {
		t.Errorf("facet pick saved %+v, want %+v", got, want)
	}

	m, common = visit()
	m = press(t, m, runeKey("/"))
	for _, r := range "zz" {
		m = press(t, m, runeKey(string(r)))
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if got := savedQuery(common); got.words != "s5zz" {
		t.Errorf("/ commit saved %+v", got)
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if g := m.tabAt(tabGraph).(graphTab); g.q.words != "" {
		t.Errorf("esc after the human's query reset to baseline: %+v", g.q)
	}

	m, _ = visit()
	next, _ := m.Update(revealMsg{"s5"})
	m = next.(Model)
	if g := m.tabAt(tabGraph).(graphTab); g.visit {
		t.Error("G reveal left the visit on")
	}
}

func chartLandingJump(t *testing.T, live bool) Model {
	t.Helper()
	common := t.TempDir()
	old := commonDir
	commonDir = func() (string, error) { return common, nil }
	t.Cleanup(func() { commonDir = old })
	state, status := session.Exited, "deferred"
	if live {
		state, status = session.Alive, "in_progress"
	}
	beads := append(focusFixture(), dbead("cl", contract.KindLanding, status, "c", 1))
	m := recentsModel(t, nil, "cl")
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{"cl": live},
		sessions: map[string]session.Info{"cl": {State: state}}})
	m = press(t, m, runeKey("6"))
	return recentJump(t, m, "cl")
}

func TestRecentsJumpChartLandingLiveSelectsDefogRow(t *testing.T) {
	m := chartLandingJump(t, true)
	if b := m.selection(); m.tab != tabDefog || b == nil || b.ID != "cl" || m.tabAt(tabDefog).(defogTab).sel != "live:cl" {
		t.Errorf("tab %d selection %v sel %q", m.tab, b, m.tabAt(tabDefog).(defogTab).sel)
	}
}

func TestRecentsJumpChartLandingNotLiveScopesGraph(t *testing.T) {
	m := chartLandingJump(t, false)
	if want := "not on Defog → Graph (scoped to cl, esc clears)"; m.tab != tabGraph || m.status != want {
		t.Errorf("tab %d status %q", m.tab, m.status)
	}
}
