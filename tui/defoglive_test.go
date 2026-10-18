package tui

import (
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// defogLiveFixture: turn (waiting) and live run sessions, afk is a live AFK effort, and
// an exited session on ready doesn't count.
func defogLiveFixture() ([]bd.Bead, map[string]bool, map[string]session.Info) {
	beads, sess := defogFixture()
	for i := range beads {
		if beads[i].ID == "afk" {
			beads[i].Status = "in_progress"
		}
	}
	sess["ready"] = session.Info{State: session.Exited}
	return beads, map[string]bool{"afk": true}, sess
}

func defogLiveKeys(rows []dRow) string {
	var ks []string
	for _, r := range rows {
		if r.kind == dLive {
			ks = append(ks, r.key)
		}
	}
	return strings.Join(ks, ",")
}

func defogLiveModel(t *testing.T, dir string) Model {
	t.Helper()
	beads, live, sess := defogLiveFixture()
	stubCommonDir(t, dir)
	m := sized(t, 120, 40)
	m, _ = send(t, m, loadedMsg{beads: beads, live: live, sessions: sess})
	return press(t, m, runeKey("2"))
}

func TestDefogLiveSection(t *testing.T) {
	beads, live, sess := defogLiveFixture()
	g := graphData{beads: beads, live: live, sessions: sess}
	rows := g.defogList(nil)
	i := 0
	for rows[i].key != liveKey {
		i++
	}
	if rows[i].title != "Live (2)" || (rows[i-1].kind != dAct && rows[i-1].kind != dACW) {
		t.Errorf("header = %q after %q", rows[i].title, rows[i-1].key)
	}
	if got := defogLiveKeys(rows); got != "live:live,live:afk" {
		t.Errorf("live rows = %s", got)
	}
	rows = g.defogList(map[string]bool{liveKey: true})
	if defogLiveKeys(rows) != "" || rows[i].title != "Live (2)" {
		t.Errorf("folded = %v", dshape(rows))
	}
	g.live, g.sessions = nil, nil
	rows = g.defogList(nil)
	for i = 0; rows[i].key != liveKey; i++ {
	}
	if rows[i].title != "Live (0)" || rows[i+1].kind != dPlace || rows[i+1].key != liveKey+"/none" {
		t.Errorf("empty = %v", rows[i:i+2])
	}
}

func TestDefogLiveFoldPersists(t *testing.T) {
	dir := t.TempDir()
	m := defogLiveModel(t, dir)
	press(t, defogAt(t, m, liveKey), runeKey(" "))
	if got := readSection(t, dir, "defog"); got != `{"s:live":true}` {
		t.Errorf("folds = %s", got)
	}
	if err := defogLiveModel(t, dir).tabAt(tabDefog).(defogTab).save(); err != nil {
		t.Fatal(err)
	}
	if got := readSection(t, dir, "defog"); got != `{"s:live":true}` {
		t.Errorf("pruned = %s", got)
	}
}

func TestDefogLiveShadeAndFallbackAndKeys(t *testing.T) {
	m := defogLiveModel(t, t.TempDir())
	m = press(t, defogAt(t, m, "i:ini"), runeKey(" ")) // open the tree so the twins show
	m = press(t, defogAt(t, m, "c"), runeKey(" "))
	shaded := func(m Model, k string) bool {
		tab := m.tabAt(tabDefog).(defogTab)
		rows := tab.rows()
		full := tab.g.defogList(tab.expanded())
		for i, r := range rows {
			if r.key == k {
				return shadeSet(tab.g.parentOf, rows, tab.selIndex(rows), full, dRow.shadeIn)[i]
			}
		}
		t.Fatalf("no row %s", k)
		return false
	}
	m = defogAt(t, m, "live:afk")
	if !shaded(m, "afk") || !shaded(m, "c") {
		t.Errorf("live:afk should shade its tree twin and Parent chain")
	}
	m = defogAt(t, m, "turn")
	if !shaded(m, "act:turn") {
		t.Errorf("tree turn should shade its Actionable twin")
	}
	a, twin := defogAt(t, m, "live:live"), defogAt(t, m, "live")
	if hintsOf(a) != hintsOf(twin) {
		t.Errorf("hints %q vs %q", hintsOf(a), hintsOf(twin))
	}
	// the selected Live row's bead goes non-live: fall back to the tree twin
	m = defogAt(t, m, "live:afk")
	beads, _, sess := defogLiveFixture()
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: sess})
	tab := m.tabAt(tabDefog).(defogTab)
	if rows := tab.rows(); rows[tab.selIndex(rows)].key != "afk" {
		t.Errorf("fell back to %q, want afk", rows[tab.selIndex(rows)].key)
	}
}

func TestDefogInitiativesHeader(t *testing.T) {
	dir := t.TempDir()
	m := defogLiveModel(t, dir)
	// the blank line sits above the header, right after the Live section, and not above a group
	lines := strings.Split(screen(m), "\n")
	hi := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "Initiatives") && strings.Contains(l, "▾") })
	if hi < 1 || strings.TrimSpace(lines[hi-1]) != "" || strings.TrimSpace(lines[hi-2]) == "" {
		t.Fatalf("no blank line above the Initiatives header:\n%s", screen(m))
	}
	// folding keeps the group folds, drops every tree row, and leaves Live alone
	liveN := defogLiveKeys(m.tabAt(tabDefog).(defogTab).rows())
	m = press(t, defogAt(t, m, "i:ini"), runeKey(" "))
	m = press(t, defogAt(t, m, treeKey), runeKey(" "))
	rows := m.tabAt(tabDefog).(defogTab).rows()
	for _, r := range rows {
		if r.kind == dHead && !r.noInit() || r.kind == dChart || r.kind == dLeaf {
			t.Fatalf("tree row %s survives the fold", r.key)
		}
	}
	if got := defogLiveKeys(rows); got != liveN {
		t.Errorf("live rows %q after folding, were %q", got, liveN)
	}
	if got := readDefogFolds(t, dir); got != `{"i:ini":true,"s:tree":true}` {
		t.Errorf("folds = %s", got)
	}
	m = press(t, m, runeKey(" "))
	if defogAt(t, m, "i:ini"); !m.tabAt(tabDefog).(defogTab).flip["i:ini"] {
		t.Errorf("unfolding lost the group's fold")
	}
	// survives pruning though it names no bead
	seedDefogFolds(t, dir, `{"s:tree":true,"gone":true}`)
	if err := defogLiveModel(t, dir).tabAt(tabDefog).(defogTab).save(); err != nil {
		t.Fatal(err)
	}
	if got := readDefogFolds(t, dir); got != `{"s:tree":true}` {
		t.Errorf("pruned = %s", got)
	}
}

func TestDefogInitiativesCursorAndShade(t *testing.T) {
	m := defogLiveModel(t, t.TempDir())
	m = defogAt(t, m, "i:ini")
	tab := m.tabAt(tabDefog).(defogTab)
	rows := tab.rows()
	full := tab.g.defogList(tab.expanded())
	ti := slices.IndexFunc(rows, func(r dRow) bool { return r.key == treeKey })
	if shadeSet(tab.g.parentOf, rows, tab.selIndex(rows), full, dRow.shadeIn)[ti] {
		t.Errorf("a selected group shades the Initiatives header")
	}
	tab.flip = map[string]bool{treeKey: true} // folded away under the cursor
	if rows := tab.rows(); rows[tab.selIndex(rows)].key != treeKey {
		t.Errorf("cursor on %q, want the Initiatives header", rows[tab.selIndex(rows)].key)
	}
	if tab.selected() != nil {
		t.Errorf("the header selects a bead")
	}
}

func TestDefogCursorFollowsActionableLive(t *testing.T) {
	refresh := func(m Model, id string, s session.Info) Model {
		beads, _, sess := defogLiveFixture()
		sess[id] = s
		m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{"afk": true}, sessions: sess})
		return m
	}
	waiting := session.Info{State: session.Alive, Waiting: session.WaitTurn}
	alive := session.Info{State: session.Alive}
	resolved := func(m Model) string {
		r, _ := m.tabAt(tabDefog).(defogTab).selRow()
		if r.b == nil { // a header: no bead under the cursor
			return ""
		}
		return map[int]string{dAct: "act:", dLive: "live:"}[r.kind] + r.b.ID // section + bead, via the selected row
	}
	row0 := func(m Model) string { // the cursor row's key, headers included
		tab := m.tabAt(tabDefog).(defogTab)
		rows := tab.rows()
		return rows[tab.selIndex(rows)].key
	}
	m := defogAt(t, defogLiveModel(t, t.TempDir()), "live:live")
	m = refresh(m, "live", waiting)
	if got := resolved(m); got != "act:live" {
		t.Fatalf("Live to Actionable: cursor on %q", got)
	}
	if m = refresh(m, "live", waiting); resolved(m) != "act:live" {
		t.Errorf("second refresh: cursor on %q", resolved(m))
	}
	m = defogAt(t, defogLiveModel(t, t.TempDir()), "act:turn")
	if m = refresh(m, "turn", alive); resolved(m) != "live:turn" {
		t.Errorf("Actionable to Live: cursor on %q", resolved(m))
	}
	if m = refresh(m, "turn", alive); resolved(m) != "live:turn" {
		t.Errorf("second refresh: cursor on %q", resolved(m))
	}
	// folded destination: the tree twin
	m = press(t, defogAt(t, defogLiveModel(t, t.TempDir()), liveKey), runeKey(" "))
	m = defogAt(t, m, "act:turn")
	if m = refresh(m, "turn", alive); row0(m) != "s:act" {
		t.Errorf("folded Live: cursor on %q, want today's fallback s:act", row0(m))
	}
	// left both sections: today's fallback
	m = defogAt(t, defogLiveModel(t, t.TempDir()), "act:turn")
	beads, _, sess := defogLiveFixture()
	beads = slices.DeleteFunc(beads, func(b bd.Bead) bool { return b.ID == "turn" })
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{"afk": true}, sessions: sess})
	if row0(m) != "s:act" {
		t.Errorf("left both: cursor on %q, want today's fallback s:act", row0(m))
	}
}

// TestDefogLiveStampedChart: a stamped Chart's own effort and its Landing are Live rows with
// the Chart as context; a non-running or Draft Landing is not.
func TestDefogLiveStampedChart(t *testing.T) {
	beads, live, sess := defogLiveFixture()
	beads = append(beads,
		dbead("land", contract.KindLanding, "in_progress", "dc", 1),
		dbead("land2", contract.KindLanding, "open", "dc", 2),
		dbead("draft", contract.KindLanding, "deferred", "dc", 3),
	)
	sess["dc"] = session.Info{State: session.Alive}
	live["land"], live["draft"] = true, true
	g := graphData{beads: beads, live: live, sessions: sess}
	rows := g.defogList(nil)
	if got := defogLiveKeys(rows); got != "live:land,live:live,live:afk,live:dc" {
		t.Errorf("live rows = %s", got)
	}
	for _, r := range rows {
		if r.key == "live:land" && r.chart.ID != "dc" {
			t.Errorf("landing context = %v", r.chart)
		}
		if r.key == liveKey && r.title != "Live (4)" {
			t.Errorf("header = %q", r.title)
		}
	}
}
