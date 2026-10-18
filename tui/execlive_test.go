package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// execLiveFixture: ap and sg run detached sessions (sg waiting on a turn), bl is a live AFK
// effort, pz's session exited; every one a member of cpA.
func execLiveFixture() (graphData, []bd.Bead) {
	beads := execFixture()
	for i := range beads {
		if beads[i].ID == "bl" {
			beads[i].Status = "in_progress"
		}
	}
	return graphData{
		beads: beads,
		live:  map[string]bool{"bl": true},
		sessions: map[string]session.Info{
			"ap": {State: session.Alive},
			"sg": {State: session.Alive, Waiting: session.WaitTurn},
			"pz": {State: session.Exited},
		},
	}, beads
}

func execLiveKeys(rows []eRow) string {
	var ks []string
	for _, r := range rows {
		if r.kind == eLive {
			ks = append(ks, r.key)
		}
	}
	return strings.Join(ks, ",")
}

func TestExecLiveSection(t *testing.T) {
	g, _ := execLiveFixture()
	rows := g.execList(nil)
	// directly below Actionable, a header with the count, rows in beadCmp order; exited pz is out
	i := 0
	for rows[i].key != liveKey {
		i++
	}
	if rows[i].title != "Live (2)" || rows[i-1].kind != eAct {
		t.Errorf("header = %q after %v", rows[i].title, rows[i-1].key)
	}
	if got := execLiveKeys(rows); got != "live:ap,live:bl" {
		t.Errorf("live rows = %s", got)
	}
	// a waiting session is under Actionable only
	if !strings.Contains(strings.Join(eshape(rows), ","), "A sg") {
		t.Errorf("sg missing from Actionable: %v", eshape(rows))
	}
	// folding hides the rows, keeps N
	rows = g.execList(map[string]bool{liveKey: true})
	if execLiveKeys(rows) != "" || !strings.Contains(strings.Join(eshape(rows), ","), "Live (2)") {
		t.Errorf("folded = %v", eshape(rows))
	}
	// empty: placeholder
	g.sessions, g.live = nil, nil
	rows = g.execList(nil)
	if s := strings.Join(eshape(rows), ","); !strings.Contains(s, "Live (0),.,#") {
		t.Errorf("empty = %s", s)
	}
}

func execLiveModel(t *testing.T, dir string) Model {
	t.Helper()
	g, beads := execLiveFixture()
	stubCommonDir(t, dir)
	m, _ := execModelFresh(t, nil)
	m, _ = send(t, m, loadedMsg{beads: beads, live: g.live, sessions: g.sessions})
	return press(t, m, runeKey("3"))
}

func TestExecLiveFoldPersists(t *testing.T) {
	dir := t.TempDir()
	m := execLiveModel(t, dir)
	press(t, execAt(t, m, liveKey), runeKey(" "))
	if got := readExecFolds(t, dir); got != `{"s:live":true}` {
		t.Errorf("folds = %s", got)
	}
	// survives pruning, though s:live names no bead
	seedExecFolds(t, dir, `{"s:live":true,"gone":true}`)
	if err := execLiveModel(t, dir).tabAt(tabExecute).(execTab).save(); err != nil {
		t.Fatal(err)
	}
	if got := readExecFolds(t, dir); got != `{"s:live":true}` {
		t.Errorf("pruned = %s", got)
	}
}

func TestExecLiveShadeAndFallbackAndKeys(t *testing.T) {
	m := execLiveModel(t, t.TempDir())
	m = press(t, execAt(t, m, "i:i1"), runeKey(" ")) // open the Initiative, then cpA: the tree twins show
	m = press(t, execAt(t, m, "cpA"), runeKey(" "))
	m = execAt(t, m, "live:bl")
	shaded := func(m Model, k string) bool {
		tab := m.tabAt(tabExecute).(execTab)
		rows := tab.rows()
		sel := tab.selIndex(rows)
		full := tab.g.execList(tab.expanded())
		return shadeSet(tab.g.parentOf, rows, sel, full, eRow.shadeIn)[execIndexOfKey(rows, k)]
	}
	if !shaded(m, "bl") || !shaded(m, "cpA") {
		t.Errorf("live:bl should shade its tree twin and Parent chain")
	}
	m = execAt(t, m, "sg")
	if !shaded(m, "act:sg") {
		t.Errorf("tree sg should shade its Actionable twin")
	}
	// enter on a Live row acts as on its tree row
	a, twin := execAt(t, m, "live:ap"), execAt(t, m, "ap")
	if hintsOf(a) != hintsOf(twin) {
		t.Errorf("hints %q vs %q", hintsOf(a), hintsOf(twin))
	}
	// the selected Live row's bead goes non-live: fall back to the tree twin
	m = execAt(t, m, "live:bl")
	g, _ := execLiveFixture()
	delete(g.live, "bl")
	m, _ = send(t, m, loadedMsg{beads: g.beads, live: g.live, sessions: g.sessions})
	tab := m.tabAt(tabExecute).(execTab)
	if rows := tab.rows(); rows[tab.selIndex(rows)].key != "bl" {
		t.Errorf("fell back to %q, want bl", rows[tab.selIndex(rows)].key)
	}
}

func execIndexOfKey(rows []eRow, k string) int {
	for i, r := range rows {
		if r.key == k {
			return i
		}
	}
	return -1
}

func TestExecLiveCheckpointRendersLikeTreeRow(t *testing.T) {
	g, _ := execLiveFixture()
	g.live["cpA"] = true
	for i := range g.beads {
		if g.beads[i].ID == "cpA" {
			g.beads[i].Status = "in_progress"
		}
	}
	tab := execTab{g: g}
	var live, tree *eRow
	for _, r := range g.execList(map[string]bool{"i:i1": true}) {
		switch {
		case r.key == "live:cpA":
			live = &r
		case r.key == "cpA":
			tree = &r
		}
	}
	if live == nil || tree == nil {
		t.Fatalf("live=%v tree=%v", live, tree)
	}
	l, _ := tab.body(*live, 12, false, "")
	want, _ := tab.body(*tree, 12, false, "")
	if want = strings.Replace(want, "▸ ", "", 1); strings.TrimSpace(l) != strings.TrimSpace(want) {
		t.Errorf("live Checkpoint line %q, want %q", l, want)
	}
	var leaf *eRow
	for _, r := range g.execList(map[string]bool{"i:i1": true}) {
		if r.kind == eLive && r.b != nil && r.b.Kind != contract.KindCheckpoint && r.depth == live.depth {
			leaf = &r
			break
		}
	}
	if leaf == nil {
		t.Fatal("no Live leaf at the twin's depth")
	}
	col := func(r eRow) int {
		l, _ := tab.body(r, 12, false, "")
		l = ansi.Strip(l)
		return len(l) - len(strings.TrimLeft(l, " "))
	}
	if col(*live) != col(*leaf) {
		t.Errorf("Live Checkpoint starts at column %d, Live leaf at %d", col(*live), col(*leaf))
	}
	if lt, tt := tab.rowText(*live), tab.rowText(*tree); lt != tt {
		t.Errorf("live Checkpoint detail %q, want %q", lt, tt)
	}
}

func TestExecInitiativesHeader(t *testing.T) {
	dir := t.TempDir()
	m := execLiveModel(t, dir)
	// the blank line sits above the header, right after the Live section, and not above a group
	lines := strings.Split(screen(m), "\n")
	hi := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "Initiatives") && strings.Contains(l, "▾") })
	if hi < 1 || strings.TrimSpace(lines[hi-1]) != "" || strings.TrimSpace(lines[hi-2]) == "" {
		t.Fatalf("no blank line above the Initiatives header:\n%s", screen(m))
	}
	// folding keeps the group folds and drops every tree row; the fold persists under s:tree
	m = press(t, execAt(t, m, "i:i1"), runeKey(" "))
	m = press(t, execAt(t, m, treeKey), runeKey(" "))
	for _, r := range m.tabAt(tabExecute).(execTab).rows() {
		if strings.HasPrefix(r.key, "i:") && r.key != noInitKey {
			t.Fatalf("tree row %s survives the fold", r.key)
		}
	}
	if got := readExecFolds(t, dir); got != `{"i:i1":true,"s:tree":true}` {
		t.Errorf("folds = %s", got)
	}
	m = press(t, m, runeKey(" "))
	if execAt(t, m, "i:i1"); !m.tabAt(tabExecute).(execTab).flip["i:i1"] {
		t.Errorf("unfolding lost the group's fold")
	}
	// survives pruning though it names no bead
	seedExecFolds(t, dir, `{"s:tree":true,"gone":true}`)
	if err := execLiveModel(t, dir).tabAt(tabExecute).(execTab).save(); err != nil {
		t.Fatal(err)
	}
	if got := readExecFolds(t, dir); got != `{"s:tree":true}` {
		t.Errorf("pruned = %s", got)
	}
}

func TestExecInitiativesCursorAndShade(t *testing.T) {
	m := execLiveModel(t, t.TempDir())
	m = press(t, execAt(t, m, "i:i1"), runeKey(" ")) // open the group; select it, then fold the section
	m = execAt(t, m, "i:i1")
	tab := m.tabAt(tabExecute).(execTab)
	shaded := func(tab execTab) bool {
		rows := tab.rows()
		return shadeSet(tab.g.parentOf, rows, tab.selIndex(rows), tab.g.execList(tab.expanded()), eRow.shadeIn)[execIndexOfKey(rows, treeKey)]
	}
	if shaded(tab) {
		t.Errorf("a selected tree row shades the Initiatives header")
	}
	tab.flip = map[string]bool{treeKey: true} // folded away under the cursor
	if rows := tab.rows(); rows[tab.selIndex(rows)].key != treeKey {
		t.Errorf("cursor on %q, want the Initiatives header", rows[tab.selIndex(rows)].key)
	}
	// bead keys refuse on the header as on Actionable's
	if tab.selected() != nil {
		t.Errorf("the header selects a bead")
	}
}

func TestExecCursorFollowsActionableLive(t *testing.T) {
	refresh := func(m Model, id string, s session.Info) Model {
		g, _ := execLiveFixture()
		g.sessions[id] = s
		m, _ = send(t, m, loadedMsg{beads: g.beads, live: g.live, sessions: g.sessions})
		return m
	}
	waiting := session.Info{State: session.Alive, Waiting: session.WaitTurn}
	alive := session.Info{State: session.Alive}
	resolved := func(m Model) string {
		r, _ := m.tabAt(tabExecute).(execTab).selRow()
		if r.b == nil { // a header: no bead under the cursor
			return ""
		}
		return map[int]string{eAct: "act:", eLive: "live:"}[r.kind] + r.b.ID // section + bead, via the selected row
	}
	row0 := func(m Model) string { // the cursor row's key, headers included
		tab := m.tabAt(tabExecute).(execTab)
		rows := tab.rows()
		return rows[tab.selIndex(rows)].key
	}
	m := execAt(t, execLiveModel(t, t.TempDir()), "live:ap")
	if m = refresh(m, "ap", waiting); resolved(m) != "act:ap" {
		t.Fatalf("Live to Actionable: cursor on %q", resolved(m))
	}
	if m = refresh(m, "ap", waiting); resolved(m) != "act:ap" {
		t.Errorf("second refresh: cursor on %q", resolved(m))
	}
	m = execAt(t, execLiveModel(t, t.TempDir()), "act:sg")
	if m = refresh(m, "sg", alive); resolved(m) != "live:sg" {
		t.Errorf("Actionable to Live: cursor on %q", resolved(m))
	}
	if m = refresh(m, "sg", alive); resolved(m) != "live:sg" {
		t.Errorf("second refresh: cursor on %q", resolved(m))
	}
	// folded destination: the tree twin, else the Initiative header
	m = press(t, execAt(t, execLiveModel(t, t.TempDir()), liveKey), runeKey(" "))
	m = execAt(t, m, "act:sg")
	if m = refresh(m, "sg", alive); row0(m) != "s:act" {
		t.Errorf("folded Live: cursor on %q, want today's fallback s:act", row0(m))
	}
	// left both sections: today's fallback
	m = execAt(t, execLiveModel(t, t.TempDir()), "act:sg")
	g, _ := execLiveFixture()
	g.beads = slices.DeleteFunc(g.beads, func(b bd.Bead) bool { return b.ID == "sg" })
	m, _ = send(t, m, loadedMsg{beads: g.beads, live: g.live, sessions: g.sessions})
	if row0(m) != "s:act" {
		t.Errorf("left both: cursor on %q, want today's fallback s:act", row0(m))
	}
}

// A Closing session is never Waiting: its bead leaves Actionable for Live, showing closing…
// and no waiting word.
func TestExecClosingReadsLiveNotActionable(t *testing.T) {
	for _, w := range []session.Waiting{session.WaitTurn, session.WaitAsk} {
		g, _ := execLiveFixture()
		g.sessions["sg"] = session.Info{State: session.Alive, Waiting: w, Closing: true}
		rows := g.execList(nil)
		if s := strings.Join(eshape(rows), ","); strings.Contains(s, "A sg") {
			t.Errorf("%s: sg still Actionable: %s", w, s)
		}
		if got := execLiveKeys(rows); !strings.Contains(got, "live:sg") {
			t.Errorf("%s: live rows = %s", w, got)
		}
		if g.execBadge() != 0 {
			t.Errorf("%s: badge = %d, want 0", w, g.execBadge())
		}
	}
}

// TestExecLiveOmitsChartLanding: a Chart's Landing is not an Execute row.
func TestExecLiveOmitsChartLanding(t *testing.T) {
	g, beads := execLiveFixture()
	g.beads = append(beads, dbead("cl", contract.KindChart, "open", "", 1), dbead("land", contract.KindLanding, "open", "cl", 1))
	g.live["land"] = true
	if got := execLiveKeys(g.execList(nil)); strings.Contains(got, "land") {
		t.Errorf("live rows = %s", got)
	}
}
