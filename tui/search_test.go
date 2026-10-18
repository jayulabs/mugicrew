package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

func searchModel(t *testing.T) (Model, *[]string) {
	t.Helper()
	var words []string
	seam(t, &searchClosed, func(w string) ([]bd.Bead, error) {
		words = append(words, w)
		return []bd.Bead{{ID: "old", Status: "closed", Title: "Alpha old", Kind: contract.KindBuild, Parent: "anc"}}, nil
	})
	m := demandModel(t, []bd.Bead{{ID: "a", Status: "open", Title: "Alpha open", Kind: contract.KindTask}})
	store := map[string]bd.Bead{
		"anc": {ID: "anc", Status: "closed", Title: "Old chart", Kind: contract.KindChart, Container: true},
		"old": {ID: "old", Status: "closed", Title: "Alpha old", Kind: contract.KindBuild, Parent: "anc"},
		"sib": {ID: "sib", Status: "closed", Title: "Sibling", Kind: contract.KindBuild, Parent: "anc"},
	}
	seam(t, &listByID, func(ids []string) ([]bd.Bead, error) {
		var out []bd.Bead
		for _, id := range ids {
			if b, ok := store[id]; ok {
				out = append(out, b)
			}
		}
		return out, nil
	})
	seam(t, &listChildren, func(id string) ([]bd.Bead, error) {
		return []bd.Bead{{ID: "old", Status: "closed", Title: "Alpha old", Kind: contract.KindBuild, Parent: "anc"}, {ID: "sib", Status: "closed", Title: "Sibling", Kind: contract.KindBuild, Parent: "anc"}}, nil
	})
	m.tab = tabGraph
	return m, &words
}

func searchView(m Model) string { return ansi.Strip(m.View()) }

// typeWords opens / and types text one key at a time, returning the cmd each key gave.
func typeWords(t *testing.T, m Model, text string) (Model, []tea.Cmd) {
	t.Helper()
	m, _ = send(t, m, runeKey("/"))
	var cmds []tea.Cmd
	for _, r := range text {
		var c tea.Cmd
		m, c = send(t, m, runeKey(string(r)))
		cmds = append(cmds, c)
	}
	return m, cmds
}

func TestWordsShowOpenAtOnceThenStreamClosedUnderSearching(t *testing.T) {
	m, _ := searchModel(t)
	m, cmds := typeWords(t, m, "alpha")
	m, _ = send(t, m, enterKey())
	v := searchView(m)
	if !strings.Contains(v, "Alpha open") || !strings.Contains(v, "searching…") || strings.Contains(v, "Alpha old") {
		t.Fatalf("before the fetch lands:\n%s", v)
	}
	m, _ = send(t, m, batchSearch(cmds[4]))
	v = searchView(m)
	if !strings.Contains(v, "Alpha old") || strings.Contains(v, "searching…") {
		t.Fatalf("after the fetch lands:\n%s", v)
	}
	if !strings.Contains(v, "Old chart") || !strings.Contains(v, "anc 2/2") {
		t.Errorf("closed hit not under its context with exact counts:\n%s", v)
	}
	m = runLoad(t, m, m.loadTier(false)) // 3 s loads keep the hits while Graph claims them
	if !slices.Contains(ids(m.beads), "sib") {
		t.Errorf("group lost across a 3 s load: %v", ids(m.beads))
	}
}

// batchSearch runs a (possibly batched) key cmd and returns its searchMsg.
func batchSearch(cmd tea.Cmd) tea.Msg {
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		for _, c := range b {
			if c == nil {
				continue
			}
			if sm, ok := c().(searchMsg); ok {
				return sm
			}
		}
	}
	return msg
}

func TestStaleSearchGenerationIsDropped(t *testing.T) {
	m, words := searchModel(t)
	m, cmds := typeWords(t, m, "al")
	first := batchSearch(cmds[0])
	m, _ = send(t, m, first) // the "a" search lands after "al" started
	if g := m.tabAt(tabGraph).(graphTab); !g.searching || len(m.hits) != 0 {
		t.Fatalf("stale result applied: searching %v hits %v", g.searching, m.hits)
	}
	m, _ = send(t, m, batchSearch(cmds[1]))
	if g := m.tabAt(tabGraph).(graphTab); g.searching || !m.hits["old"] {
		t.Errorf("current result dropped: searching %v hits %v", g.searching, m.hits)
	}
	if !slices.Equal(*words, []string{"a", "al"}) {
		t.Errorf("searches %v", *words)
	}
}

func TestNarrowingKeepsHitsUntilReplaced(t *testing.T) {
	m, _ := searchModel(t)
	m, cmds := typeWords(t, m, "alpha")
	m, _ = send(t, m, batchSearch(cmds[4]))
	m, _ = send(t, m, runeKey(" "))
	m, c := send(t, m, runeKey("o"))
	if v := searchView(m); !strings.Contains(v, "Alpha old") {
		t.Fatalf("hit flickered out while the narrowed fetch is in flight:\n%s", v)
	}
	m, _ = send(t, m, batchSearch(c))
	if !strings.Contains(searchView(m), "Alpha old") {
		t.Errorf("hit lost after the fetch replaced it")
	}
}

func TestLeavingGraphReleasesClaimAndEntryReReadsIt(t *testing.T) {
	m, _ := searchModel(t)
	m, cmds := typeWords(t, m, "alpha")
	m, _ = send(t, m, enterKey())
	m, _ = send(t, m, batchSearch(cmds[4]))
	m.tab = tabLogs
	m = runLoad(t, m, m.loadTier(false))
	if slices.Contains(ids(m.beads), "old") {
		t.Fatalf("claim held off Graph: %v", ids(m.beads))
	}
	if m.searched.words != "alpha" || !m.hits["old"] {
		t.Fatalf("query memory lost: %q %v", m.searched.words, m.hits)
	}
	m.tab = tabGraph
	m = runLoad(t, m, m.loadSlow())
	if !slices.Contains(ids(m.beads), "old") {
		t.Errorf("entry load did not re-read the hits: %v", ids(m.beads))
	}
}

func TestOpenMatchThatClosesStaysInHits(t *testing.T) {
	m, _ := searchModel(t)
	m, cmds := typeWords(t, m, "alpha")
	m, _ = send(t, m, batchSearch(cmds[4]))
	closed := bd.Bead{ID: "a", Status: "closed", Title: "Alpha open", Kind: contract.KindTask}
	seam(t, &listOpen, func() ([]bd.Bead, error) { return nil, nil })
	seam(t, &defaultIDs, func() ([]string, error) { return []string{"a"}, nil })
	seam(t, &listByID, func([]string) ([]bd.Bead, error) { return []bd.Bead{closed}, nil })
	m = runLoad(t, m, m.loadSlow())
	if !m.hits["a"] {
		t.Errorf("closing match left the hits: %v", m.hits)
	}
}

func TestOpenMatchThatClosesOutsideDefaultSetStaysInHits(t *testing.T) {
	m, _ := searchModel(t)
	m, cmds := typeWords(t, m, "alpha")
	m, _ = send(t, m, batchSearch(cmds[4]))
	closed := bd.Bead{ID: "a", Status: "closed", Title: "Alpha open", Kind: contract.KindTask}
	seam(t, &listOpen, func() ([]bd.Bead, error) { return nil, nil })
	seam(t, &defaultIDs, func() ([]string, error) { return nil, nil })
	seam(t, &listByID, func(ids []string) ([]bd.Bead, error) {
		if slices.Contains(ids, "a") {
			return []bd.Bead{closed}, nil
		}
		return nil, nil
	})
	m = runLoad(t, m, m.loadSlow())
	if !m.hits["a"] {
		t.Errorf("closing match outside the default set left the hits: %v", m.hits)
	}
}

func TestDeletedOnDemandBeadDropsOut(t *testing.T) {
	has := func(m Model, id string) bool { return beadIndex(m.beads, id) >= 0 }
	t.Run("search hit", func(t *testing.T) {
		m, cmds := typeWords(t, func() Model { m, _ := searchModel(t); return m }(), "alpha")
		m, _ = send(t, m, batchSearch(cmds[4]))
		if !has(m, "old") {
			t.Fatal("hit not loaded")
		}
		seam(t, &listByID, func([]string) ([]bd.Bead, error) { return nil, nil })
		m = runLoad(t, m, m.loadSlow())
		m = runLoad(t, m, m.loadSlow())
		if has(m, "old") {
			t.Errorf("deleted hit stayed: %+v", m.beads)
		}
	})
	t.Run("G reveal", func(t *testing.T) {
		m, _ := searchModel(t)
		stubAncestors(t)
		m, c := m.fetchReveal("old", true, func(m Model, _ string) (Model, tea.Cmd) { return m, nil })
		m, _ = send(t, m, c())
		if !has(m, "old") {
			t.Fatal("revealed bead not loaded")
		}
		seam(t, &listByID, func([]string) ([]bd.Bead, error) { return nil, nil })
		m = runLoad(t, m, m.loadSlow())
		m = runLoad(t, m, m.loadSlow())
		if has(m, "old") {
			t.Errorf("deleted revealed bead stayed: %+v", m.beads)
		}
	})
}

func TestRevealDoesNotStaleInFlightSearch(t *testing.T) {
	stubAncestors(t)
	m, _ := searchModel(t)
	m, cmds := typeWords(t, m, "a")
	m, _ = m.fetchReveal("old", false, func(m Model, _ string) (Model, tea.Cmd) { return m, nil })
	m, _ = send(t, m, batchSearch(cmds[0]))
	if g := m.tabAt(tabGraph).(graphTab); g.searching || !m.hits["old"] {
		t.Errorf("search dropped by a reveal: searching %v hits %v", g.searching, m.hits)
	}
}

func TestFailedRevealClearsSearching(t *testing.T) {
	m, _ := searchModel(t)
	m, _ = m.fetchReveal("old", true, func(m Model, _ string) (Model, tea.Cmd) { return m, nil })
	if !m.tabAt(tabGraph).(graphTab).searching {
		t.Fatal("G did not start searching")
	}
	m, _ = m.foldDemand(demandMsg{gen: m.demandGen, id: "old", err: errors.New("boom")})
	if g := m.tabAt(tabGraph).(graphTab); g.searching || g.want != "" {
		t.Errorf("failed fetch left searching %v want %q", g.searching, g.want)
	}
}
