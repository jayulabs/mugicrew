package tui

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// scopeBeads is stubBeads plus a second nested Chart with a closed member, a direct
// Initiative member, a closed member of ch and a lone Signal.
func scopeBeads() []bd.Bead {
	return append(stubBeads(),
		bd.Bead{ID: "ch2", Kind: contract.KindChart, Container: true, Status: "open", Title: "Second chart", Parent: "ini", Created: epoch.Add(time.Hour)},
		bd.Bead{ID: "ch2k", Kind: contract.KindBuild, Status: "closed", Title: "Second kid", Parent: "ch2", Created: epoch},
		bd.Bead{ID: "lone", Kind: contract.KindBuild, Status: "open", Title: "Lone member", Parent: "ini", Created: epoch.Add(2 * time.Hour)},
		bd.Bead{ID: "chdone", Kind: contract.KindBuild, Status: "closed", Title: "Done in ch", Parent: "ch", Created: epoch},
		bd.Bead{ID: "sig", Kind: contract.KindSignal, Status: "open", Title: "A signal", Created: epoch},
	)
}

// scoped is a Graph-tab Model whose query has been scoped to head with sel selected.
func scoped(t *testing.T, dir, head, sel string) Model {
	t.Helper()
	m := gfModel(t, dir, scopeBeads())
	g := gr(m).scopeTo(head, sel)
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabGraph] = g
	return m
}

func shownIDs(m Model) []string {
	var ids []string
	for _, r := range gr(m).rows() {
		if r.beadID != "" {
			ids = append(ids, r.beadID)
		}
	}
	return ids
}

func TestScopeShowsLoadedSubtreeAtStatusAny(t *testing.T) {
	m := scoped(t, t.TempDir(), "ini", "ini")
	g := gr(m)
	g.qfold = map[string]bool{}
	if got, want := slices.Sorted(slices.Values(matchIDs(g))), []string{"ch", "ch2", "ch2k", "chdone", "ini", "lone", "long", "paused", "run"}; !slices.Equal(got, want) {
		t.Errorf("subtree = %v, want %v", got, want)
	}
}

// matchIDs is every bead the scope's filtered groups hold, folds ignored.
func matchIDs(g graphTab) (ids []string) {
	ms, _ := shown(g.groups)
	return ms
}

func TestScopeSelfShowsTheBeadAlone(t *testing.T) {
	m := scoped(t, t.TempDir(), "sig", "sig")
	if got := shownIDs(m); !slices.Equal(got, []string{"sig"}) {
		t.Errorf("rows = %v", got)
	}
	if h := ansi.Strip(gr(m).header(100)); !strings.HasPrefix(h, "in sig · any") || strings.Contains(h, "/ sig") {
		t.Errorf("header = %q", h)
	}
}

func TestScopeHeaderSegmentsAndCounts(t *testing.T) {
	m := scoped(t, t.TempDir(), "ch", "paused")
	if h := ansi.Strip(gr(m).header(100)); !strings.HasPrefix(h, "in ch · any") || !strings.HasSuffix(h, "5 of 5") {
		t.Errorf("header = %q", h)
	}
	m = typeText(t, press(t, m, runeKey("/")), "paused")
	m = press(t, m, enter())
	h := ansi.Strip(gr(m).header(100))
	if !strings.HasPrefix(h, "in ch · / paused · any") || !strings.HasSuffix(h, "1 of 5") {
		t.Errorf("narrowed header = %q", h)
	}
	g := gr(m)
	g.q.kind = contract.KindBuild
	if h := ansi.Strip(g.headerWith("")); !strings.Contains(h, "in ch · / paused · any · build") {
		t.Errorf("order = %q", h)
	}
}

func TestScopeEscUntouchedGoesToBaseline(t *testing.T) {
	m := press(t, scoped(t, t.TempDir(), "ch", "paused"), esc())
	if gr(m).q != baseline {
		t.Errorf("q = %+v", gr(m).q)
	}
}

func TestScopeEscPeelsWordsStatusKindScope(t *testing.T) {
	m := scoped(t, t.TempDir(), "ch", "paused")
	g := gr(m)
	g.q.words, g.q.status, g.q.kind = "x", "open", contract.KindBuild
	m.tabs = slices.Clone(m.tabs)
	m.tabs[tabGraph] = g.rebuild()
	want := []query{
		{scope: "ch", status: "open", kind: contract.KindBuild},
		{scope: "ch", status: statusAny, kind: contract.KindBuild},
		{scope: "ch", status: statusAny},
	}
	for i, w := range want {
		m = press(t, m, esc())
		if gr(m).q != w {
			t.Fatalf("esc %d: q = %+v, want %+v", i+1, gr(m).q, w)
		}
	}
	if m = press(t, m, esc()); gr(m).q != baseline {
		t.Errorf("last esc: q = %+v", gr(m).q)
	}
}

func TestScopeWritesNeitherFilterNorFolds(t *testing.T) {
	dir := t.TempDir()
	const saved = `{"words":"zzz","status":"unclosed","kind":""}`
	gqWrite(t, dir, saved)
	m := scoped(t, dir, "ch", "paused")
	m = typeText(t, press(t, m, runeKey("/")), "run")
	m = press(t, m, enter())
	m = selectBead(t, m, "ch")
	m = space(t, m)
	m = press(t, m, esc())
	m = press(t, m, esc())
	if got := gqRead(t, dir); got != saved {
		t.Errorf("filter file = %s", got)
	}
	if _, err := os.Stat(gfFile(dir)); err == nil {
		t.Errorf("folds file written: %s", gfRead(t, dir))
	}
}

func TestScopeFoldsOpenOnlyTheAncestorChain(t *testing.T) {
	m := scoped(t, t.TempDir(), "ini", "paused")
	f := gr(m).folds()
	if f["ini"] || f["ch"] {
		t.Errorf("chain folded: %v", f)
	}
	if !f["ch2"] {
		t.Errorf("sibling nested head open: %v", f)
	}
	if got := shownIDs(m); slices.Contains(got, "ch2k") || !slices.Contains(got, "paused") {
		t.Errorf("rows = %v", got)
	}
}

func TestScopeOnHeadOpensItToShowMembers(t *testing.T) {
	m := scoped(t, t.TempDir(), "ini", "ch2")
	f := gr(m).folds()
	if f["ini"] || f["ch2"] || !f["ch"] {
		t.Errorf("folds = %v", f)
	}
	if got := shownIDs(m); !slices.Contains(got, "ch2k") || slices.Contains(got, "paused") {
		t.Errorf("rows = %v", got)
	}
	if gr(m).sel != "ch2" {
		t.Errorf("sel = %s", gr(m).sel)
	}
}

func TestScopeWordsDoNotStartClosedSearch(t *testing.T) {
	m := scoped(t, t.TempDir(), "ch", "paused")
	m = typeText(t, press(t, m, runeKey("/")), "run")
	m, cmd := send(t, m, enter())
	if _, sc := m.searchSync(); sc != nil || cmd != nil && m.searched.words != "" {
		t.Error("a scope runs the global closed search")
	}
}
