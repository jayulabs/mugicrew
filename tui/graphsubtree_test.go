package tui

import (
	"slices"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// subtreeModel is a Graph-tab Model scoped to the open Chart "head", whose closed
// members "k1" and "k2" are not open or default, with the subtree claim seeded on them.
// bd list --all is a failure: a scope must never read it.
func subtreeModel(t *testing.T) Model {
	t.Helper()
	open := []bd.Bead{{ID: "head", Status: "open", Title: "Head", Kind: contract.KindChart, Container: true}}
	store := map[string]bd.Bead{
		"k1": {ID: "k1", Status: "closed", Title: "Done one", Kind: contract.KindBuild, Parent: "head"},
		"k2": {ID: "k2", Status: "closed", Title: "Done two", Kind: contract.KindBuild, Parent: "head"},
	}
	def := []bd.Bead{}
	claimStubs(t, &open, &def, nil, nil)
	seam(t, &listAll, func() ([]bd.Bead, error) {
		t.Error("bd list --all read under a scope")
		return nil, nil
	})
	seam(t, &listByID, func(ids []string) ([]bd.Bead, error) {
		var out []bd.Bead
		for _, id := range ids {
			if b, ok := store[id]; ok {
				out = append(out, b)
			}
		}
		return out, nil
	})
	m := sized(t, 120, 30)
	m = runLoad(t, m, m.loadSlow())
	m.tab = tabGraph
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabGraph] = gr(m).scopeTo("head", "head")
	return m.seedSubtree("head", "k1", "k2")
}

func loaded(m Model, id string) bool { return beadIndex(m.beads, id) >= 0 }

func TestSubtreeClaimKeepsClosedBeadsAcrossLoads(t *testing.T) {
	for name, narrow := range map[string]func(g graphTab) graphTab{
		"bare":   func(g graphTab) graphTab { return g },
		"words":  func(g graphTab) graphTab { g.q.words = "done"; return g },
		"status": func(g graphTab) graphTab { g.q.status = "open"; return g },
		"kind":   func(g graphTab) graphTab { g.q.kind = contract.KindTask; return g },
	} {
		t.Run(name, func(t *testing.T) {
			m := subtreeModel(t)
			m.tabs = slices.Clone(m.tabs)
			m.tabs[tabGraph] = narrow(gr(m)).rebuild()
			m = runLoad(t, m, m.loadSlow())
			m = runLoad(t, m, m.load())
			m = runLoad(t, m, m.loadSlow())
			if !loaded(m, "k1") || !loaded(m, "k2") {
				t.Errorf("subtree evicted: %v", ids(m.beads))
			}
		})
	}
}

func TestSubtreeClaimReleasedOffGraphAndReReadOnEntry(t *testing.T) {
	m := subtreeModel(t)
	m = runLoad(t, m, m.loadSlow())
	g := gr(m)
	g.sel = "k1"
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabGraph] = g
	m.tab = tabLogs
	m = runLoad(t, m, m.loadSlow())
	if loaded(m, "k1") {
		t.Fatalf("claim held off Graph: %v", ids(m.beads))
	}
	if !m.subtree["k1"] {
		t.Fatalf("set forgotten: %v", m.subtree)
	}
	nm, cmd := m.enter(tabGraph)
	m = nm.(Model)
	if g := gr(m); !g.searching || !g.entry {
		t.Errorf("entry did not park the cursor on k1: searching %v entry %v", g.searching, g.entry)
	}
	m = runLoad(t, m, cmd)
	if !loaded(m, "k1") || !loaded(m, "k2") {
		t.Errorf("entry load did not re-read the subtree: %v", ids(m.beads))
	}
}

func TestEscClearingScopeEmptiesSubtreeClaim(t *testing.T) {
	m := press(t, subtreeModel(t), esc())
	if len(m.subtree) != 0 {
		t.Errorf("claim after esc = %v", m.subtree)
	}
	m = runLoad(t, m, m.loadSlow())
	if loaded(m, "k1") {
		t.Errorf("released bead stayed: %v", ids(m.beads))
	}
}

func TestEscPeelingWordsKeepsSubtreeClaim(t *testing.T) {
	m := subtreeModel(t)
	g := gr(m)
	g.q.words = "x"
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabGraph] = g.rebuild()
	if m = press(t, m, esc()); len(m.subtree) != 3 {
		t.Errorf("claim = %v", m.subtree)
	}
}

func TestNewerScopeReplacesSubtreeClaim(t *testing.T) {
	m := subtreeModel(t)
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabGraph] = gr(m).scopeTo("k1", "k1")
	m = m.scopeChanged(query{scope: "head", status: statusAny})
	if len(m.subtree) != 0 {
		t.Errorf("old scope's claim = %v", m.subtree)
	}
}

func TestBareScopeNeverClaimsAllClosed(t *testing.T) {
	m := subtreeModel(t)
	if m.claimsAllClosed() {
		t.Error("a bare scope claims every closed bead")
	}
	m = runLoad(t, m, m.loadSlow()) // listAll fails the test if read
	nm, cmd := m.enter(tabGraph)
	runLoad(t, nm.(Model), cmd)
}

func TestSubtreeSeedSurvivesGotoMenuClosing(t *testing.T) {
	m := subtreeModel(t)
	m.gotoHeld = map[string]bool{"k1": true}
	m = m.seedSubtree("k1")
	m = runLoad(t, m, m.loadSlow())
	if !loaded(m, "k1") {
		t.Fatal("not loaded")
	}
	m.gotoHeld = nil // the menu closed
	m = runLoad(t, m, m.loadSlow())
	if !loaded(m, "k1") {
		t.Errorf("seeded bead evicted with the menu: %v", ids(m.beads))
	}
}
