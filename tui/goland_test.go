package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// landModel is a Graph-tab Model over an Initiative "ini" holding the Chart "ch" (open
// member "k"), with Chart "ch"'s closed member "old" outside the loaded set and a Signal
// "sig". The subtree read is stubbed: asked records its ids, reply is its answer.
func landModel(t *testing.T) (m Model, asked *[]string, reply *func(string) (bd.Subtree, bool, error)) {
	t.Helper()
	open := []bd.Bead{
		{ID: "ini", Status: "open", Title: "Init", Kind: contract.KindInitiative, Container: true},
		{ID: "ch", Status: "open", Title: "Chart", Kind: contract.KindChart, Container: true, Parent: "ini"},
		{ID: "k", Status: "open", Title: "Kid", Kind: contract.KindBuild, Parent: "ch"},
		{ID: "sig", Status: "open", Title: "Sig", Kind: contract.KindSignal},
	}
	closed := bd.Bead{ID: "old", Status: "closed", Title: "Old", Kind: contract.KindBuild, Parent: "ch"}
	def := []bd.Bead{}
	claimStubs(t, &open, &def, map[string]bd.Bead{"old": closed}, nil)
	seam(t, &listAll, func() ([]bd.Bead, error) {
		t.Error("bd list --all read by a landing")
		return nil, nil
	})
	asked = new([]string)
	reply = new(func(string) (bd.Subtree, bool, error))
	*reply = func(id string) (bd.Subtree, bool, error) {
		return bd.Subtree{Head: "ini", IDs: []string{"ini", "ch", "k", "old"}, Beads: []bd.Bead{closed}}, true, nil
	}
	seam(t, &listSubtree, func(id string, _ map[string]bd.Bead) (bd.Subtree, bool, error) {
		*asked = append(*asked, id)
		return (*reply)(id)
	})
	m = sized(t, 120, 30)
	m = runLoad(t, m, m.loadSlow())
	return m, asked, reply
}

// land starts a goto landing on id and returns the in-flight Model and the fetch's landing.
func land(t *testing.T, m Model, id string) (Model, func(Model) Model) {
	t.Helper()
	m, cmd := m.gotoLand(id)
	return m, func(m Model) Model { m, _ = send(t, m, cmd()); return m }
}

func TestGotoLandInFlightShowsProvisionalScopeThenSwitchesToTrueHead(t *testing.T) {
	m, _, _ := landModel(t)
	m, fold := land(t, m, "old") // not loaded: its head is a guess until the read folds in
	g := gr(m)
	if m.tab != tabGraph || g.q.scope != "old" || g.q.status != statusAny || !g.searching || g.want != "old" {
		t.Fatalf("in flight: tab %d q %+v sel %q searching %v", m.tab, g.q, g.sel, g.searching)
	}
	if !strings.Contains(m.status, "fetching old's group…") || !strings.Contains(g.header(120), "searching…") {
		t.Errorf("in flight: status %q header %q", m.status, g.header(120))
	}
	m = fold(m)
	g = gr(m)
	if g.q.scope != "ini" || g.sel != "old" || g.searching || !loaded(m, "old") {
		t.Errorf("landed: scope %q sel %q searching %v old loaded %v", g.q.scope, g.sel, g.searching, loaded(m, "old"))
	}
	if m.status != "scoped Graph to ini (esc clears)" {
		t.Errorf("status %q", m.status)
	}
	if m = runLoad(t, m, m.loadTier(false)); !loaded(m, "old") {
		t.Errorf("subtree evicted by a 3 s load: %v", ids(m.beads))
	}
}

func TestGotoLandOnHeadAndSelfScope(t *testing.T) {
	m, _, reply := landModel(t)
	m, fold := land(t, m, "ini")
	m = fold(m)
	if g := gr(m); g.q.scope != "ini" || g.sel != "ini" || m.status != "scoped Graph to ini (esc clears)" {
		t.Errorf("head: scope %q sel %q status %q", g.q.scope, g.sel, m.status)
	}
	*reply = func(id string) (bd.Subtree, bool, error) {
		return bd.Subtree{Head: id, Self: true, IDs: []string{id}}, true, nil
	}
	m, fold = land(t, m, "sig")
	m = fold(m)
	if g := gr(m); g.q.scope != "sig" || g.sel != "sig" || m.status != "scoped Graph to sig, no group (esc clears)" {
		t.Errorf("self-scope: scope %q sel %q status %q", g.q.scope, g.sel, m.status)
	}
}

func TestGotoLandReadsOnEveryLandingEvenWhenLoaded(t *testing.T) {
	m, asked, _ := landModel(t)
	for range 2 {
		var fold func(Model) Model
		m, fold = land(t, m, "k")
		m = fold(m)
	}
	if len(*asked) != 2 {
		t.Errorf("subtree read %d times, want once per landing", len(*asked))
	}
}

func TestGotoLandCursorMoveDoesNotCancelScopeSwitch(t *testing.T) {
	m, _, _ := landModel(t)
	m, fold := land(t, m, "k")
	m = press(t, m, runeKey("k")) // up: off the landed bead
	moved := sel(m)
	m = fold(m)
	if g := gr(m); g.q.scope != "ini" || g.sel != moved || g.searching {
		t.Errorf("scope %q sel %q (want %q) searching %v", g.q.scope, g.sel, moved, g.searching)
	}
}

func TestGotoLandFailureKeepsProvisionalScope(t *testing.T) {
	m, _, reply := landModel(t)
	*reply = func(string) (bd.Subtree, bool, error) { return bd.Subtree{}, true, errors.New("boom") }
	m, fold := land(t, m, "k")
	m = fold(m)
	g := gr(m)
	if g.q.scope != "ini" || g.searching || !strings.Contains(m.status, "fetch failed: boom; showing the loaded part of ini (esc still clears)") {
		t.Errorf("scope %q searching %v status %q", g.q.scope, g.searching, m.status)
	}
}

func TestGotoLandNoRowsUndoesToPreviousTabAndGraph(t *testing.T) {
	m, _, reply := landModel(t)
	*reply = func(string) (bd.Subtree, bool, error) { return bd.Subtree{}, false, nil }
	m.tab = tabLogs
	before := gr(m).q
	m, fold := land(t, m, "k")
	m = fold(m)
	if m.tab != tabLogs || gr(m).q != before || m.status != "k: not in the graph" || len(m.subtree) != 0 {
		t.Errorf("tab %d q %+v status %q claim %v", m.tab, gr(m).q, m.status, m.subtree)
	}
}

func TestGotoLandStaleFetchIsDropped(t *testing.T) {
	m, _, reply := landModel(t)
	m, fold1 := land(t, m, "k")
	*reply = func(string) (bd.Subtree, bool, error) {
		return bd.Subtree{Head: "sig", Self: true, IDs: []string{"sig"}}, true, nil
	}
	m, fold2 := land(t, m, "sig")
	m = fold1(m)
	if g := gr(m); g.q.scope == "ini" {
		t.Fatalf("stale landing replaced the newer scope: %q", g.q.scope)
	}
	m = fold2(m)
	if g := gr(m); g.q.scope != "sig" || m.subtree["k"] {
		t.Errorf("scope %q claim %v", g.q.scope, m.subtree)
	}
}

func TestGotoLandClosesFocusAndWritesNoRecents(t *testing.T) {
	m, _, _ := landModel(t)
	m.focus = focus{id: "ini", sel: "head", flip: map[string]bool{}}
	m, cmd := m.gotoLand("k")
	if m.focus.id != "" {
		t.Error("focus view left open")
	}
	if _, ok := cmd().(recentsMsg); ok {
		t.Error("a landing recorded a Recents interaction")
	}
}

func TestGotoLandEscClearsScopeAndClaim(t *testing.T) {
	m, _, _ := landModel(t)
	m, fold := land(t, m, "k")
	m = press(t, fold(m), escKey())
	if g := gr(m); g.q.scope != "" || len(m.subtree) != 0 {
		t.Errorf("scope %q claim %v", g.q.scope, m.subtree)
	}
}

func TestGotoLandNoRowsUndoRestoresPriorScopeClaim(t *testing.T) {
	m, _, reply := landModel(t)
	m, fold := land(t, m, "k")
	m = fold(m)
	want := len(m.subtree)
	*reply = func(string) (bd.Subtree, bool, error) { return bd.Subtree{}, false, nil }
	m, fold = land(t, m, "ghost")
	m = fold(m)
	if gr(m).q.scope != "ini" || len(m.subtree) != want || want == 0 || !m.demandClaims()["k"] {
		t.Errorf("scope %q claim %v want %d", gr(m).q.scope, m.subtree, want)
	}
}

func TestReplacingScopeOutsideEscReleasesSubtreeClaim(t *testing.T) {
	m, _, _ := landModel(t)
	m, fold := land(t, m, "k")
	m = fold(m)
	if !m.demandClaims()["k"] {
		t.Fatal("scoped landing holds no claim")
	}
	nm, _ := m.Update(revealMsg{"sig"})
	if m = nm.(Model); gr(m).q.scope != "" || m.demandClaims()["k"] {
		t.Errorf("reveal: scope %q claim %v", gr(m).q.scope, m.demandClaims())
	}
}

func TestSubtreeClaimNeedsScope(t *testing.T) {
	m, _, _ := landModel(t)
	m, fold := land(t, m, "k")
	m = fold(m)
	g := gr(m)
	g.q.scope = ""
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabGraph] = g
	if m.demandClaims()["k"] {
		t.Error("claim held with no scope")
	}
}

// pickDuring picks item v of Graph's facet picker, as the human does mid-fetch.
func pickDuring(t *testing.T, m Model, facet, v string) Model {
	t.Helper()
	m = m.pickGraph(facet)
	d := m.modals[len(m.modals)-1]
	d.cur = slices.Index(d.items, v)
	m = m.pop()
	m, _ = d.act(m, d)
	return m
}

func TestGotoLandFoldKeepsTypingStartedDuringFetch(t *testing.T) {
	m, _, _ := landModel(t)
	m, fold := land(t, m, "k")
	m = press(t, m, runeKey("/"))
	m = press(t, m, runeKey("o"))
	m = fold(m)
	g := gr(m)
	if !g.typing || g.edit.text != "o" || g.q.scope != "ini" || g.q.status != statusAny || g.searching || !loaded(m, "old") {
		t.Errorf("typing %v edit %q q %+v searching %v", g.typing, g.edit.text, g.q, g.searching)
	}
}

func TestGotoLandFoldKeepsStatusPickedDuringFetch(t *testing.T) {
	m, _, _ := landModel(t)
	m, fold := land(t, m, "k")
	m = pickDuring(t, m, "status", "open")
	m = fold(m)
	if g := gr(m); g.q.scope != "ini" || g.q.status != "open" || g.searching || !loaded(m, "old") {
		t.Errorf("q %+v searching %v", g.q, g.searching)
	}
}

func TestGotoLandFoldKeepsKindPickedDuringFetch(t *testing.T) {
	m, _, _ := landModel(t)
	m, fold := land(t, m, "k")
	m = pickDuring(t, m, "kind", string(contract.KindBuild))
	m = fold(m)
	if g := gr(m); g.q.scope != "ini" || g.q.kind != contract.KindBuild || g.q.status != statusAny || g.searching || !loaded(m, "old") {
		t.Errorf("q %+v searching %v", g.q, g.searching)
	}
}

func TestBestHeadNeedsLoadedContainerGrandparent(t *testing.T) {
	m := Model{beads: []bd.Bead{
		{ID: "t", Status: "open", Kind: contract.KindBuild},
		{ID: "t.1", Status: "open", Kind: contract.KindBuild, Parent: "t"},
		{ID: "t.1.1", Status: "open", Kind: contract.KindBuild, Parent: "t.1"},
		{ID: "ch", Status: "open", Kind: contract.KindChart, Container: true, Parent: "gone"},
		{ID: "k", Status: "open", Kind: contract.KindBuild, Parent: "ch"},
	}}
	for id, want := range map[string]string{"t.1.1": "t.1.1", "k": "ch"} {
		if head, _ := m.bestHead(id); head != want {
			t.Errorf("bestHead(%s) = %q, want %q", id, head, want)
		}
	}
}

func TestGotoLandNotesDepthGuardStrays(t *testing.T) {
	m, _, reply := landModel(t)
	*reply = func(string) (bd.Subtree, bool, error) {
		return bd.Subtree{Head: "ini", IDs: []string{"ini", "ch", "k"}, Strays: []string{"z"}}, true, nil
	}
	m, fold := land(t, m, "k")
	m = fold(m)
	if !strings.Contains(m.status, "1 beads nest deeper") {
		t.Errorf("status %q", m.status)
	}
}
