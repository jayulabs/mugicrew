package tui

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

func demandModel(t *testing.T, open []bd.Bead) Model {
	t.Helper()
	def := []bd.Bead{}
	claimStubs(t, &open, &def, nil, nil)
	m := sized(t, 120, 30)
	return runLoad(t, m, m.loadSlow())
}

func stubAncestors(t *testing.T) {
	t.Helper()
	seam(t, &listWithAncestors, func(id string) ([]bd.Bead, error) {
		return []bd.Bead{{ID: "anc", Status: "closed", Title: "Old chart", Kind: contract.KindChart, Container: true}, {ID: id, Status: "closed", Title: "Old leaf", Kind: contract.KindBuild, Parent: "anc"}}, nil
	})
}

func landReveal(t *testing.T, m Model, id string) (Model, func(Model) Model) {
	t.Helper()
	m, cmd := send(t, m, revealMsg{id})
	return m, func(m Model) Model { m, _ = send(t, m, cmd()); return m }
}

func TestRevealUnloadedClosedBeadLandsCursorUnderAncestors(t *testing.T) {
	stubAncestors(t)
	m := demandModel(t, []bd.Bead{{ID: "a", Status: "open", Title: "Open", Kind: contract.KindTask}})
	m, land := landReveal(t, m, "old")
	if g := m.tabAt(tabGraph).(graphTab); m.tab != tabGraph || !g.searching || g.want != "old" {
		t.Fatalf("in flight: tab %d searching %v want %q", m.tab, g.searching, g.want)
	}
	m = land(m)
	g := m.tabAt(tabGraph).(graphTab)
	if b := g.selected(); b == nil || b.ID != "old" || g.searching || g.want != "" {
		t.Fatalf("landed: selected %v searching %v want %q", b, g.searching, g.want)
	}
	var shown []string
	for _, r := range g.rows() {
		shown = append(shown, r.sel)
	}
	if !slices.Contains(shown, "anc") {
		t.Errorf("ancestor missing from rows %v", shown)
	}
	m = runLoad(t, m, m.loadTier(false)) // 3 s loads keep it while Graph claims it
	if !slices.Contains(ids(m.beads), "old") {
		t.Errorf("fetched bead lost across a 3 s load: %v", ids(m.beads))
	}
}

func TestRevealParksCursorAndMoveCancels(t *testing.T) {
	stubAncestors(t)
	m := demandModel(t, []bd.Bead{{ID: "a", Status: "open", Title: "Open", Kind: contract.KindTask}, {ID: "b", Status: "open", Title: "Open b", Kind: contract.KindTask}})
	m, land := landReveal(t, m, "old")
	m = press(t, m, runeKey("j")) // any move cancels the memory
	if g := m.tabAt(tabGraph).(graphTab); g.want != "" {
		t.Fatalf("move kept the memory %q", g.want)
	}
	m = land(m)
	if b := m.tabAt(tabGraph).(graphTab).selected(); b != nil && b.ID == "old" {
		t.Errorf("cursor returned after a move cancelled it")
	}
}

func TestGraphLostCursorWithNothingInFlightFollowsNeighbour(t *testing.T) {
	m := demandModel(t, []bd.Bead{{ID: "a", Status: "open", Kind: contract.KindTask}, {ID: "b", Status: "open", Kind: contract.KindTask}})
	m.tab = tabGraph
	g := m.tabAt(tabGraph).(graphTab).rebuild()
	g.sel = "gone"
	g = g.fixSelection(0)
	if g.want != "" || g.sel == "gone" {
		t.Errorf("sel %q want %q", g.sel, g.want)
	}
}

func TestGotoMenuStreamsInUnloadedNeighboursWhileOpen(t *testing.T) {
	open := []bd.Bead{{ID: "a", Status: "open", Title: "Opened one", Kind: contract.KindChart, Parent: "gone-parent", Container: true}}
	m := demandModel(t, open)
	seam(t, &listByID, func(ids []string) ([]bd.Bead, error) {
		return []bd.Bead{{ID: "gone-parent", Status: "closed", Title: "Closed parent", Kind: contract.KindInitiative}}, nil
	})
	seam(t, &listChildren, func(string) ([]bd.Bead, error) {
		return []bd.Bead{{ID: "a.old", Status: "closed", Title: "Closed child", Kind: contract.KindBuild, Parent: "a"}}, nil
	})
	m.tab = tabGraph
	g := m.tabAt(tabGraph).(graphTab).rebuild()
	g.sel = "a"
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabGraph] = g
	next, cmd := m.gotoOpen()
	m = next.(Model)
	if cmd == nil {
		t.Fatal("no neighbour fetch")
	}
	m, _ = send(t, m, cmd())
	view := m.View()
	for _, want := range []string{"parent", "Closed child"} {
		if !strings.Contains(view, want) {
			t.Errorf("menu lacks %q:\n%s", want, view)
		}
	}
	m = runLoad(t, m, m.loadTier(false)) // they stay loaded while the menu is open
	if !slices.Contains(ids(m.beads), "a.old") {
		t.Errorf("neighbour dropped while the menu is open: %v", ids(m.beads))
	}
	m = m.pop()
	m = runLoad(t, m, m.loadTier(false))
	if slices.Contains(ids(m.beads), "a.old") {
		t.Errorf("neighbour kept after the menu closed: %v", ids(m.beads))
	}
}

func TestGotoMenuStreamsInUnloadedDependents(t *testing.T) {
	m := demandModel(t, []bd.Bead{{ID: "a", Status: "open", Title: "Opened one", Kind: contract.KindChart, Container: true}})
	seam(t, &listDependents, func(id string) ([]string, error) { return []string{"a", "old-dep"}, nil })
	var asked []string
	seam(t, &listByID, func(ids []string) ([]bd.Bead, error) {
		asked = ids
		return []bd.Bead{{ID: "old-dep", Status: "closed", Title: "Closed dependent", Kind: contract.KindTask, BlockedBy: []string{"a"}}}, nil
	})
	m.tab = tabGraph
	g := m.tabAt(tabGraph).(graphTab).rebuild()
	g.sel = "a"
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabGraph] = g
	next, cmd := m.gotoOpen()
	m = next.(Model)
	if cmd == nil {
		t.Fatal("no neighbour fetch")
	}
	m, _ = send(t, m, cmd())
	if !slices.Equal(asked, []string{"old-dep"}) {
		t.Errorf("fetched %v, want only the unloaded dependent", asked)
	}
	if view := m.View(); !strings.Contains(view, "Closed dependent") {
		t.Errorf("menu lacks the dependent:\n%s", view)
	}
}

func TestGotoMenuStreamsInUnloadedOffshoot(t *testing.T) {
	m := demandModel(t, []bd.Bead{{ID: "a", Status: "open", Title: "Opened one", Kind: contract.KindChart, Container: true}})
	seam(t, &listDependents, func(id string) ([]string, error) { return []string{"old-pivot"}, nil })
	seam(t, &listByID, func(ids []string) ([]bd.Bead, error) {
		return []bd.Bead{{ID: "old-pivot", Status: "closed", Title: "Closed pivot", Kind: contract.KindPivot, DiscoveredFrom: []string{"a"}}}, nil
	})
	m.tab = tabGraph
	g := m.tabAt(tabGraph).(graphTab).rebuild()
	g.sel = "a"
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabGraph] = g
	next, cmd := m.gotoOpen()
	m, _ = send(t, next.(Model), cmd())
	if view := m.View(); !strings.Contains(view, "offshoot") || !strings.Contains(view, "Closed pivot") {
		t.Errorf("menu lacks the offshoot:\n%s", view)
	}
}

func TestRecentsJumpFetchesUnloadedBead(t *testing.T) {
	stubAncestors(t)
	m := demandModel(t, []bd.Bead{{ID: "a", Status: "open", Title: "Open", Kind: contract.KindTask}})
	os.WriteFile(recentsPath(), []byte(`["old"]`), 0o644) // a Recents row: the Recents claim holds it
	m.recents = []string{"old"}
	m, cmd := m.recentsJump("old")
	if cmd == nil || m.tab == tabGraph {
		t.Fatalf("no fetch, or Graph was taken over: tab %d", m.tab)
	}
	m, _ = send(t, m, cmd())
	if !slices.Contains(ids(m.beads), "old") || m.tab != tabGraph {
		t.Errorf("tab %d beads %v", m.tab, ids(m.beads))
	}
}

func TestGraphEntryAfterSAnyParksEvictedCursorAndReturns(t *testing.T) {
	open := []bd.Bead{{ID: "a", Status: "open", Kind: contract.KindTask}}
	old := bd.Bead{ID: "old", Status: "closed", Kind: contract.KindTask}
	def := []bd.Bead{}
	claimStubs(t, &open, &def, nil, nil)
	m := sized(t, 120, 30)
	m = runLoad(t, m, m.loadSlow()) // the cursor bead is not loaded: a load off-tab evicted it
	g := m.tabAt(tabGraph).(graphTab)
	g.q, g.sel = query{status: statusAny}, "old"
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabGraph] = g
	def = append(def, old) // s any reads every closed bead
	next, cmd := m.enter(tabGraph)
	m = next.(Model)
	if g := m.tabAt(tabGraph).(graphTab); !g.searching || g.want != "old" {
		t.Fatalf("entry: searching %v want %q", g.searching, g.want)
	}
	m = runLoad(t, m, cmd)
	if g := m.tabAt(tabGraph).(graphTab); g.sel != "old" || g.searching || g.want != "" {
		t.Errorf("landed: sel %q searching %v want %q", g.sel, g.searching, g.want)
	}
}

func TestStaleGotoFetchDoesNotReplaceNewerMenusClaim(t *testing.T) {
	m := demandModel(t, []bd.Bead{{ID: "a", Status: "open", Title: "Opened one", Kind: contract.KindTask, Parent: "p"}})
	calls := 0
	seam(t, &listByID, func([]string) ([]bd.Bead, error) {
		calls++
		return []bd.Bead{{ID: []string{"", "first", "second"}[calls], Status: "closed", Title: "Closed", Kind: contract.KindTask}}, nil
	})
	m.tab = tabGraph
	g := m.tabAt(tabGraph).(graphTab).rebuild()
	g.sel = "a"
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabGraph] = g
	next, first := m.gotoOpen()
	m = next.(Model).pop() // closed before the fetch landed
	next, second := m.gotoOpen()
	m = next.(Model)
	fm, sm := first(), second()
	m, _ = send(t, m, sm)
	m, _ = send(t, m, fm) // the older menu's fetch lands last
	if !m.gotoHeld["second"] || m.gotoHeld["first"] {
		t.Errorf("held %v", m.gotoHeld)
	}
}

func TestFoldDemandErrorKeepsFailureStatus(t *testing.T) {
	m := demandModel(t, nil)
	m, _ = m.foldDemand(demandMsg{err: errors.New("boom")})
	if !strings.Contains(m.status, "fetch failed: boom") {
		t.Fatalf("status = %q", m.status)
	}
}

// A load already in flight when Graph is entered predates the request: landing it does
// not end the entry's parked cursor; the load the next tick starts does.
func TestGraphEntryDuringInFlightLoadKeepsCursorParkedUntilEntryLoad(t *testing.T) {
	open := []bd.Bead{{ID: "a", Status: "open", Kind: contract.KindTask}}
	def := []bd.Bead{}
	claimStubs(t, &open, &def, nil, nil)
	m := sized(t, 120, 30)
	m = runLoad(t, m, m.loadSlow())
	g := m.tabAt(tabGraph).(graphTab)
	g.q, g.sel = query{status: statusAny}, "old"
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabGraph] = g
	stale := m.load() // a fast load began before the entry
	m.loading = true
	next, cmd := m.enter(tabGraph)
	m = next.(Model)
	if cmd != nil && !m.slowWant {
		t.Fatal("entry during a load must queue the slow tier")
	}
	m, _ = send(t, m, stale())
	if g := m.tabAt(tabGraph).(graphTab); !g.searching || g.want != "old" || !m.slowWant {
		t.Fatalf("stale load ended the entry: searching %v want %q slowWant %v", g.searching, g.want, m.slowWant)
	}
	def = append(def, bd.Bead{ID: "old", Status: "closed", Kind: contract.KindTask})
	m, cmd = send(t, m, tickMsg{})
	for _, msg := range flatten(cmd()) {
		m, _ = send(t, m, msg)
	}
	if g := m.tabAt(tabGraph).(graphTab); g.sel != "old" || g.searching || g.want != "" || m.slowWant {
		t.Errorf("entry load: sel %q searching %v want %q slowWant %v", g.sel, g.searching, g.want, m.slowWant)
	}
}

// s any picked while a slow load without the claim is in flight: that load must not
// clear slowWant or the marker, so the all-closed read still runs on the next tick.
func TestSAnyDuringInFlightSlowLoadStillReadsAll(t *testing.T) {
	open := []bd.Bead{{ID: "a", Status: "open", Kind: contract.KindTask}}
	def := []bd.Bead{}
	claimStubs(t, &open, &def, nil, nil)
	m := sized(t, 120, 30)
	m = runLoad(t, m, m.loadSlow())
	m.tab = tabGraph
	stale := m.loadSlow() // began under the default query
	m.loading = true
	g := m.tabAt(tabGraph).(graphTab)
	g.q = query{status: statusAny}
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabGraph] = g
	if cmd := m.claimLoad(false); cmd != nil || !m.slowWant {
		t.Fatalf("claim during a load must queue: cmd %v slowWant %v", cmd != nil, m.slowWant)
	}
	m, _ = send(t, m, stale())
	if g := m.tabAt(tabGraph).(graphTab); !g.searching || !m.slowWant {
		t.Fatalf("stale load cleared the claim: searching %v slowWant %v", g.searching, m.slowWant)
	}
	m, cmd := send(t, m, tickMsg{})
	for _, msg := range flatten(cmd()) {
		m, _ = send(t, m, msg)
	}
	if g := m.tabAt(tabGraph).(graphTab); g.searching || m.slowWant {
		t.Errorf("claim load: searching %v slowWant %v", g.searching, m.slowWant)
	}
}

func TestGotoMenuStreamsInUnloadedOrigin(t *testing.T) {
	m := demandModel(t, []bd.Bead{{ID: "a", Status: "open", Title: "Opened one", Kind: contract.KindChart, Parent: "gone-parent", Container: true, DiscoveredFrom: []string{"old-src"}}})
	var asked []string
	seam(t, &listByID, func(ids []string) ([]bd.Bead, error) {
		asked = ids
		return []bd.Bead{{ID: "gone-parent", Status: "closed", Title: "Closed parent", Kind: contract.KindInitiative}, {ID: "old-src", Status: "closed", Title: "Closed source", Kind: contract.KindBuild}}, nil
	})
	m.tab = tabGraph
	g := m.tabAt(tabGraph).(graphTab).rebuild()
	g.sel = "a"
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabGraph] = g
	next, cmd := m.gotoOpen()
	m = next.(Model)
	if cmd == nil {
		t.Fatal("no neighbour fetch")
	}
	m, _ = send(t, m, cmd())
	if !slices.Contains(asked, "old-src") || !strings.Contains(m.View(), "origin") || !strings.Contains(m.View(), "Closed source") {
		t.Errorf("origin not fetched/shown (asked %v):\n%s", asked, m.View())
	}
}

// The subtree read is a package-level seam, so TUI tests stub it instead of running bd.
func TestListSubtreeIsStubbable(t *testing.T) {
	seam(t, &listSubtree, func(id string, _ map[string]bd.Bead) (bd.Subtree, bool, error) {
		return bd.Subtree{Head: "h", IDs: []string{"h", id}}, true, nil
	})
	if st, ok, err := listSubtree("x", nil); err != nil || !ok || st.Head != "h" {
		t.Errorf("st=%+v ok=%v err=%v", st, ok, err)
	}
}
