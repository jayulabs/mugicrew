package tui

import (
	"errors"
	"slices"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/loop"
	"jayulabs.org/mugicrew/session"
)

// tierStubs counts the open-set read and listAll; open and all are what they return.
func tierStubs(t *testing.T, open, all *[]bd.Bead) (opens, alls *int) {
	t.Helper()
	opens, alls = new(int), new(int)
	seam(t, &listOpen, func() ([]bd.Bead, error) { *opens++; return *open, nil })
	seam(t, &listAll, func() ([]bd.Bead, error) { *alls++; return *all, nil })
	seam(t, &defaultIDs, func() ([]string, error) { *alls++; return ids(*all), nil }) // the slow tier's id query counts as its read
	seam(t, &listByID, func(want []string) ([]bd.Bead, error) { return withIDs(*all, want), nil })
	return
}

// runLoad executes a load command and folds its message.
func runLoad(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	m, _ = send(t, m, cmd())
	return m
}

func ids(bs []bd.Bead) []string {
	var out []string
	for _, b := range bs {
		out = append(out, b.ID)
	}
	slices.Sort(out)
	return out
}

func TestTiers(t *testing.T) {
	open := []bd.Bead{{ID: "a", Status: "open"}}
	all := []bd.Bead{{ID: "a", Status: "open"}, {ID: "z", Status: "closed"}}
	opens, alls := tierStubs(t, &open, &all)
	m := sized(t, 120, 30)
	m = runLoad(t, m, m.loadSlow()) // startup
	if *opens != 1 || *alls != 1 {
		t.Fatalf("startup: opens %d alls %d, want 1 1", *opens, *alls)
	}
	if got := ids(m.beads); !slices.Equal(got, []string{"a", "z"}) {
		t.Fatalf("merged beads %v", got)
	}
	m = runLoad(t, m, m.loadTier(m.slowDue())) // a plain tick
	if *opens != 2 || *alls != 1 {
		t.Errorf("plain tick: opens %d alls %d, want 2 1", *opens, *alls)
	}
	if got := ids(m.beads); !slices.Equal(got, []string{"a", "z"}) {
		t.Errorf("cached closed lineage lost: %v", got)
	}
	m.slowAt = time.Now().Add(-slowInterval - time.Second)
	m = runLoad(t, m, m.loadTier(m.slowDue())) // 30 s elapsed
	if *opens != 3 || *alls != 2 {
		t.Errorf("after 30 s: opens %d alls %d, want 3 2", *opens, *alls)
	}
	m, cmd := send(t, m, runeKey("4")) // Graph entry
	if !m.loading {
		t.Fatal("Graph entry did not start a load")
	}
	m = runLoad(t, m, cmd)
	if *alls != 3 {
		t.Errorf("Graph entry: alls %d, want 3", *alls)
	}
	m, cmd = send(t, m, tea.KeyMsg{Type: tea.KeyF5})
	n := *alls
	for _, msg := range flatten(cmd()) {
		m, _ = send(t, m, msg)
	}
	if *alls != n+1 {
		t.Errorf("F5: alls %d, want %d", *alls, n+1)
	}
}

func TestGraphEntryDuringFastLoadReadsLineageNextTick(t *testing.T) {
	open := []bd.Bead{{ID: "a", Status: "open"}}
	all := []bd.Bead{{ID: "a", Status: "open"}, {ID: "z", Status: "closed"}}
	_, alls := tierStubs(t, &open, &all)
	m := sized(t, 120, 30)
	m = runLoad(t, m, m.loadSlow())
	m.loading = true // a fast tick's load is in flight
	m, cmd := send(t, m, runeKey("4"))
	if cmd != nil {
		t.Fatal("Graph entry stacked a load on one in flight")
	}
	*alls = 0
	m = runLoad(t, m, m.load()) // the in-flight fast load folds: no --all
	m, cmd = send(t, m, tickMsg{})
	if !m.loading {
		t.Fatal("tick after the fast load did not start a load")
	}
	for _, msg := range flatten(cmd()) {
		m, _ = send(t, m, msg)
	}
	if *alls != 1 {
		t.Errorf("alls %d, want the deferred slow read", *alls)
	}
	if m.slowWant {
		t.Error("slow read did not clear the pending flag")
	}
}

func TestDropOutRunsAllAndShowsClosed(t *testing.T) {
	open := []bd.Bead{{ID: "a", Status: "open"}, {ID: "h", Status: "open"}}
	all := []bd.Bead{{ID: "a", Status: "open"}, {ID: "h", Status: "open"}}
	opens, alls := tierStubs(t, &open, &all)
	var bars map[string]bool
	seam(t, &setBars, func(b map[string]bool, _ map[string]string, _ map[string]session.Info) error { bars = b; return nil })
	seam(t, &readSessions, func() (map[string]session.Info, error) {
		return map[string]session.Info{"h": {State: session.Alive}}, nil
	})
	m := sized(t, 120, 30)
	m = runLoad(t, m, m.loadSlow())
	open = open[:1] // h closes
	all = []bd.Bead{{ID: "a", Status: "open"}, {ID: "h", Status: "closed"}}
	*alls = 0
	m = runLoad(t, m, m.load()) // a fast tick
	if *alls != 1 || *opens != 2 {
		t.Fatalf("drop-out: opens %d alls %d, want 2 1", *opens, *alls)
	}
	if !bars["h"] {
		t.Errorf("HITL done flip missing in the same load: %v", bars)
	}
	var h bd.Bead
	for _, b := range m.beads {
		if b.ID == "h" {
			h = b
		}
	}
	if h.Status != "closed" {
		t.Errorf("h = %+v, want closed", h)
	}
}

func TestDeleteVanishesAfterAll(t *testing.T) {
	open := []bd.Bead{}
	all := []bd.Bead{{ID: "z", Status: "closed"}}
	tierStubs(t, &open, &all)
	m := sized(t, 120, 30)
	m = runLoad(t, m, m.loadSlow())
	all = nil
	m = runLoad(t, m, m.loadSlow())
	if len(m.beads) != 0 {
		t.Errorf("deleted bead lingers: %v", ids(m.beads))
	}
}

func TestFreshOpenWinsAndEventsStayOut(t *testing.T) {
	open := []bd.Bead{{ID: "r", Status: "in_progress"}}
	all := []bd.Bead{{ID: "r", Status: "closed"}, {ID: "e", Status: "closed", Event: true}}
	tierStubs(t, &open, &all)
	m := sized(t, 120, 30)
	m = runLoad(t, m, m.loadSlow())
	if len(m.beads) != 1 || m.beads[0].Status != "in_progress" {
		t.Errorf("beads %+v, want the fresh open r only", m.beads)
	}
}

func TestReopenedBeadShowsOnceOnFastLoad(t *testing.T) {
	open := []bd.Bead{}
	all := []bd.Bead{{ID: "r", Status: "closed"}}
	_, alls := tierStubs(t, &open, &all)
	m := sized(t, 120, 30)
	m = runLoad(t, m, m.loadSlow())
	open = []bd.Bead{{ID: "r", Status: "open"}}
	m = runLoad(t, m, m.load())
	if *alls != 1 || len(m.beads) != 1 || m.beads[0].Status != "open" {
		t.Errorf("alls %d beads %+v, want one open r from the fast tier", *alls, m.beads)
	}
}

func TestFailedReadKeepsLastGoodBeads(t *testing.T) {
	for _, tier := range []string{"open", "all"} {
		open := []bd.Bead{{ID: "a", Status: "open"}}
		all := []bd.Bead{{ID: "a", Status: "open"}}
		tierStubs(t, &open, &all)
		m := sized(t, 120, 30)
		m = runLoad(t, m, m.loadSlow())
		if tier == "open" {
			seam(t, &listOpen, func() ([]bd.Bead, error) { return nil, errors.New("bd down") })
		} else {
			seam(t, &defaultIDs, func() ([]string, error) { return nil, errors.New("bd down") })
		}
		m = runLoad(t, m, m.loadSlow())
		if m.loadErr == nil || len(m.beads) != 1 {
			t.Errorf("%s fails: loadErr %v beads %v", tier, m.loadErr, ids(m.beads))
		}
	}
}

// slowReads counts the slow tier's reads and returns what they last answered.
func slowReads(t *testing.T) (n *int, pool *int) {
	t.Helper()
	n, pool = new(int), new(int)
	seam(t, &readRefs, func() (map[string]bool, map[string]string, error) {
		*n++
		return map[string]bool{"e": true}, map[string]string{"cp": "tip"}, nil
	})
	seam(t, &readKindProfiles, func() kindProfiles { *n++; return kindProfiles{} })
	seam(t, &readMainline, func() ([]git.MainlineCommit, error) { *n++; return []git.MainlineCommit{{}}, nil })
	seam(t, &readPool, func() loop.Setting { *n++; return loop.Setting{N: *pool} })
	seam(t, &readAutoExit, func() loop.AutoExit { *n++; return loop.AutoExit{} })
	return
}

func TestSlowReadsRunOnlyOnSlowLoads(t *testing.T) {
	open := []bd.Bead{{ID: "a", Status: "open"}}
	all := []bd.Bead{{ID: "a", Status: "open"}}
	tierStubs(t, &open, &all)
	n, pool := slowReads(t)
	*pool = 3
	m := sized(t, 120, 30)
	m = runLoad(t, m, m.loadSlow())
	if *n != 5 || m.pool.N != 3 || !m.efforts["e"] || len(m.histCommits) != 1 {
		t.Fatalf("slow load: reads %d pool %d efforts %v commits %d", *n, m.pool.N, m.efforts, len(m.histCommits))
	}
	if m.gateData().cpTips["cp"] != "tip" {
		t.Errorf("checkpoint tips did not ride into graphData: %v", m.gateData().cpTips)
	}
	*n, *pool = 0, 9
	m = runLoad(t, m, m.load())
	if *n != 0 {
		t.Errorf("fast load ran %d slow reads", *n)
	}
	if m.pool.N != 3 || !m.efforts["e"] || len(m.histCommits) != 1 {
		t.Errorf("fast load dropped slow values: pool %d efforts %v commits %d", m.pool.N, m.efforts, len(m.histCommits))
	}
}

func TestSettingsSaveShowsNewValueAtOnce(t *testing.T) {
	open := []bd.Bead{}
	all := []bd.Bead{}
	tierStubs(t, &open, &all)
	_, pool := slowReads(t)
	*pool = 5
	m := sized(t, 120, 30)
	_, cmd, _ := m.saveEdit("pool", func() error { return nil }, func(*Model) {}, "saved")
	done := cmd().(writeDoneMsg)
	_, cmd = done.apply(m)
	m = runLoad(t, m, cmd)
	if m.pool.N != 5 {
		t.Errorf("pool %d after save, want 5 at once", m.pool.N)
	}
}

func TestDropOutCostsOneListAllNotTheSlowTier(t *testing.T) {
	open := []bd.Bead{{ID: "r", Status: "open"}}
	all := []bd.Bead{}
	tierStubs(t, &open, &all)
	n, _ := slowReads(t)
	m := sized(t, 120, 30)
	m = runLoad(t, m, m.loadSlow())
	*n = 0
	open = nil
	m = runLoad(t, m, m.load())
	if *n != 0 {
		t.Errorf("drop-out ran %d slow reads, want only listAll", *n)
	}
}

func TestRevealToGraphReadsLineage(t *testing.T) {
	m := graphModel(t, 120, 30)
	open, all := stubBeads(), stubBeads()
	_, alls := tierStubs(t, &open, &all)
	m.tab, m.loading = tabLogs, false
	m, cmd := send(t, m, revealMsg{"paused"})
	if m.tab != tabGraph || !m.loading || cmd == nil {
		t.Fatalf("reveal: tab %d loading %v cmd %v", m.tab, m.loading, cmd != nil)
	}
	for _, msg := range flatten(cmd()) {
		m, _ = send(t, m, msg)
	}
	if *alls != 1 {
		t.Errorf("reveal: alls %d, want 1", *alls)
	}
	m.tab, m.loading = tabLogs, true // a load in flight: defer to the next tick
	m, cmd = send(t, m, revealMsg{"paused"})
	if cmd != nil || !m.slowWant {
		t.Error("reveal during a load must set slowWant, not stack")
	}
}

func TestRecentsFallbackToGraphReadsLineage(t *testing.T) {
	m := graphModel(t, 120, 30)
	open, all := stubBeads(), stubBeads()
	_, alls := tierStubs(t, &open, &all)
	m.tab, m.loading = tabIdeate, false
	m, cmd := m.recentsJump("oldb")
	if m.tab != tabGraph || cmd == nil {
		t.Fatalf("recents fallback: tab %d cmd %v", m.tab, cmd != nil)
	}
	for _, msg := range flatten(cmd()) {
		m, _ = send(t, m, msg)
	}
	if *alls != 1 {
		t.Errorf("recents fallback: alls %d, want 1", *alls)
	}
}

// A slow load in flight (startup, F5, Graph entry, a setting save) is never
// stacked by another: the tick, a repeated F5 and the save wait for it (FR21).
func TestSlowLoadsNeverStack(t *testing.T) {
	m := New(nil, nil)
	if !m.loading {
		t.Fatal("New must seed loading: Init's slow load is in flight")
	}
	m, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyF5})
	if !m.slowWant {
		t.Error("f5 during a load must queue a slow load for the next tick")
	}
	for _, msg := range flatten(cmd()) {
		if _, ok := msg.(loadedMsg); ok {
			t.Error("f5 stacked a second load on one in flight")
		}
	}
}

// The slow tier reads the default set in two bd processes (id query, fetch by id)
// and runs bd list --all only while the Graph query claims every closed bead.
func TestSlowTierReadsDefaultSetNotListAll(t *testing.T) {
	open := []bd.Bead{{ID: "a", Status: "open"}}
	all := []bd.Bead{{ID: "a", Status: "open"}, {ID: "c", Status: "closed"}}
	tierStubs(t, &open, &all)
	var listAlls, fetches int
	seam(t, &listAll, func() ([]bd.Bead, error) { listAlls++; return all, nil })
	seam(t, &listByID, func(want []string) ([]bd.Bead, error) { fetches++; return withIDs(all, want), nil })
	m := sized(t, 120, 30)
	m = runLoad(t, m, m.loadSlow())
	if listAlls != 0 || fetches != 1 || !slices.Equal(ids(m.beads), []string{"a", "c"}) {
		t.Errorf("default: listAll %d fetches %d beads %v", listAlls, fetches, ids(m.beads))
	}
	m.tab = tabGraph
	g := m.tabAt(tabGraph).(graphTab)
	g.q.status = "any"
	m.tabs[tabGraph] = g
	m = runLoad(t, m, m.loadSlow())
	if listAlls != 1 {
		t.Errorf("s any ran listAll %d times, want 1", listAlls)
	}
}

// The fast tier reads only the open set.
func TestFastTierReadsOnlyOpenSet(t *testing.T) {
	open := []bd.Bead{{ID: "a", Status: "open"}}
	all := []bd.Bead{}
	_, alls := tierStubs(t, &open, &all)
	m := sized(t, 120, 30)
	m = runLoad(t, m, m.loadSlow())
	*alls = 0
	runLoad(t, m, m.load())
	if *alls != 0 {
		t.Errorf("fast tier ran %d closed reads", *alls)
	}
}

// Picking s any with no words reads every closed bead at once under searching…, not
// on the next slow tick (FR7).
func TestPickAnyLoadsAllClosedAtOnce(t *testing.T) {
	open := []bd.Bead{{ID: "a", Status: "open"}}
	all := []bd.Bead{{ID: "a", Status: "open"}, {ID: "c", Status: "closed"}}
	_, alls := tierStubs(t, &open, &all)
	m := sized(t, 120, 30)
	m = runLoad(t, m, m.loadSlow())
	m.tab = tabGraph
	*alls = 0
	m = m.pickGraph("status")
	d := m.modals[len(m.modals)-1]
	d.cur = 0 // any
	m = m.pop()
	m, cmd := d.act(m, d)
	if !m.tabAt(tabGraph).(graphTab).searching || !m.loading || cmd == nil {
		t.Fatalf("pick: searching %v loading %v cmd %v", m.tabAt(tabGraph).(graphTab).searching, m.loading, cmd != nil)
	}
	m = runLoad(t, m, m.loadSlow())
	if *alls == 0 || m.tabAt(tabGraph).(graphTab).searching {
		t.Errorf("listAll ran %d times, searching %v", *alls, m.tabAt(tabGraph).(graphTab).searching)
	}
}
