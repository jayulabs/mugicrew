package tui

import (
	"slices"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// Spec mc-qgtw: an own-paused, otherwise ready leaf closes Actionable in a third band.
// Each case is a ready leaf mutated; want is its place: "" out, "wait", "ready", "paused".
type pausedCase struct {
	id, want string
	mut      []func(*bd.Bead)
	sess     session.Info
}

func pausedCases() []pausedCase {
	own := labeled("paused:manual")
	cs := []pausedCase{
		{id: "ready", want: "ready"},
		{id: "own", mut: []func(*bd.Bead){own}, want: "paused"},
		{id: "blk", mut: []func(*bd.Bead){own, blockedBy("ready")}},
		{id: "live", mut: []func(*bd.Bead){own}, sess: session.Info{State: session.Alive}},
		{id: "wait", mut: []func(*bd.Bead){own}, sess: session.Info{State: session.Alive, Waiting: session.WaitTurn}, want: "wait"},
		{id: "exit", mut: []func(*bd.Bead){own}, sess: session.Info{State: session.Exited}},
	}
	for _, c := range []string{"failing", "abandoned", "foreign-hold", "integrate-conflict", "profile", "context", "recovering"} {
		cs = append(cs, pausedCase{id: "c-" + c, mut: []func(*bd.Bead){labeled("paused:" + c)}, want: "paused"})
	}
	return cs
}

// checkBands asserts got (Actionable ids) is wait ++ ready ++ paused by membership, the
// paused ids last, and that the paused chart/checkpoint "inh" (inherited-only) is out
// while "both" (own + inherited) is in.
func checkBands(t *testing.T, got []string, cs []pausedCase) {
	t.Helper()
	var wait, ready, paused []string
	for _, c := range cs {
		switch c.want {
		case "wait":
			wait = append(wait, c.id)
		case "ready":
			ready = append(ready, c.id)
		case "paused":
			paused = append(paused, c.id)
		}
	}
	paused = append(paused, "both") // own + inherited is in
	n := len(wait) + len(ready)
	if len(got) != n+len(paused) ||
		!slices.Equal(slices.Sorted(slices.Values(got[:len(wait)])), wait) ||
		!slices.Equal(slices.Sorted(slices.Values(got[len(wait):n])), ready) ||
		!slices.Equal(slices.Sorted(slices.Values(got[n:])), slices.Sorted(slices.Values(paused))) {
		t.Errorf("actionable = %v, want %v ++ %v ++ %v", got, wait, ready, paused)
	}
}

func pausedSessions(cs []pausedCase) map[string]session.Info {
	m := map[string]session.Info{}
	for _, c := range cs {
		if c.sess.State != "" {
			m[c.id] = c.sess
		}
	}
	return m
}

func TestExecActionablePausedBand(t *testing.T) {
	cs := pausedCases()
	beads := []bd.Bead{
		dbead("cp", contract.KindCheckpoint, "open", "", 1, isContainer),
		dbead("cp2", contract.KindCheckpoint, "open", "", 1, isContainer, labeled("paused:manual")),
		dbead("inh", contract.KindApproval, "open", "cp2", 1),
		dbead("both", contract.KindApproval, "open", "cp2", 1, labeled("paused:failing")),
	}
	for _, c := range cs {
		beads = append(beads, dbead(c.id, contract.KindApproval, "open", "cp", 1, c.mut...))
	}
	g := graphData{beads: beads, sessions: pausedSessions(cs)}
	var got []string
	for _, r := range g.actionable() {
		got = append(got, r.b.ID)
	}
	if !slices.Contains(got, "cp2") { // the own-paused Checkpoint is itself a paused-band row
		t.Errorf("cp2 missing from %v", got)
	}
	got = slices.DeleteFunc(got, func(id string) bool { return id == "cp2" })
	checkBands(t, got, cs)
	if g.execBadge() != 1 { // amber stays waiting-only
		t.Errorf("amber = %d, want 1", g.execBadge())
	}
}

func TestDefogActionablePausedBand(t *testing.T) {
	cs := pausedCases()
	beads := []bd.Bead{
		dbead("ch", contract.KindChart, "open", "", 1),
		dbead("ch2", contract.KindChart, "open", "", 1, labeled("paused:manual")),
		dbead("inh", contract.KindGrilling, "open", "ch2", 1),
		dbead("both", contract.KindGrilling, "open", "ch2", 1, labeled("paused:failing")),
	}
	for _, c := range cs {
		beads = append(beads, dbead(c.id, contract.KindGrilling, "open", "ch", 1, c.mut...))
	}
	g := graphData{beads: beads, sessions: pausedSessions(cs)}
	var got []string
	for _, r := range g.defogAct() {
		got = append(got, r.b.ID)
	}
	if n := len(got) - 1; got[n] != "ch2" { // the own-paused Chart (Spec mc-qgtw FR 8) sorts last here
		t.Errorf("actionable = %v, want ch2 last", got)
	} else {
		got = got[:n]
	}
	checkBands(t, got, cs)
	if g.defogBadge() != 1 {
		t.Errorf("amber = %d, want 1", g.defogBadge())
	}
}

// Spec mc-qgtw FR 8, 10, 11, 13, 16: Defog lists an own-paused open unblocked listed Chart in
// the paused band; enter and d act as on its tree row; it twins its Charts-section row and
// tree header; inherited-only, blocked and foreign-kind containers are out.
func TestDefogActionablePausedChart(t *testing.T) {
	own := labeled("paused:manual")
	beads := []bd.Bead{
		dbead("ini", contract.KindInitiative, "open", "", 1, own),
		dbead("pc", contract.KindChart, "open", "", 1, own),
		dbead("inh", contract.KindChart, "open", "ini", 2),
		dbead("blk", contract.KindChart, "open", "", 3, own, blockedBy("pc")),
		dbead("cp", contract.KindCheckpoint, "open", "", 4, own, isContainer),
		dbead("l", contract.KindGrilling, "open", "pc", 1),
	}
	m := sized(t, 160, 40)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}})
	m = press(t, m, runeKey("2"))
	tab := m.tabAt(tabDefog).(defogTab)
	var got []string
	for _, r := range tab.g.defogAct() {
		got = append(got, r.key)
	}
	if want := []string{"act:pc"}; !slices.Equal(got, want) {
		t.Fatalf("Actionable = %v, want %v", got, want)
	}
	if tab.actN() != 1 {
		t.Errorf("actN = %d, want 1", tab.actN())
	}
	a, twin := defogAt(t, m, "act:pc"), defogAt(t, m, chartsPrefix+"pc")
	if hintsOf(a) != hintsOf(twin) {
		t.Errorf("hints %q vs %q", hintsOf(a), hintsOf(twin))
	}
	tab = a.tabAt(tabDefog).(defogTab)
	rows, full := tab.rows(), tab.g.defogList(tab.expanded())
	shaded := shadeSet(tab.g.parentOf, rows, tab.selIndex(rows), full, dRow.shadeIn)
	for i, r := range rows {
		if (r.key == chartsPrefix+"pc" || r.key == "pc") && !shaded[i] {
			t.Errorf("%s not shaded", r.key)
		}
	}
	if _, ca := send(t, a, runeKey("enter")); (ca == nil) != func() bool { _, c := send(t, twin, runeKey("enter")); return c == nil }() {
		t.Error("enter differs from the Chart's row")
	}
}

func TestExecActionablePausedCheckpoint(t *testing.T) {
	cp := contract.KindCheckpoint
	own := labeled("paused:manual")
	beads := []bd.Bead{
		dbead("i1", contract.KindInitiative, "open", "", 1, own),
		dbead("ch", contract.KindChart, "open", "", 1, own),
		dbead("pcp", cp, "open", "", 2, isContainer, own),
		dbead("blk", cp, "open", "", 1, isContainer, own, blockedBy("pcp")),
		dbead("inh", cp, "open", "i1", 1, isContainer),
		dbead("plain", cp, "open", "", 1, isContainer),
		dbead("ap", contract.KindApproval, "open", "plain", 1),
		dbead("pap", contract.KindApproval, "open", "plain", 3, own),
	}
	g := graphData{beads: beads}
	var got []string
	for _, r := range g.actionable() {
		got = append(got, r.b.ID)
	}
	if !slices.Equal(got, []string{"ap", "pcp", "pap"}) { // pcp and pap sort by priority in the paused band
		t.Errorf("actionable = %v", got)
	}
	if g.execList(nil)[0].title != "Actionable (3)" {
		t.Errorf("header = %q", g.execList(nil)[0].title)
	}
	if g.execBadge() != 0 {
		t.Errorf("amber = %d", g.execBadge())
	}
	// enter, d and the shade act as on the Checkpoint's tree row
	tab := execTab{g: g}
	var act, sec, tree eRow
	for _, r := range g.execList(map[string]bool{treeKey: false}) {
		switch r.key {
		case "act:pcp":
			act = r
		case "cps:pcp":
			sec = r
		}
	}
	if act.b == nil || sec.b == nil {
		t.Fatalf("act=%v sec=%v", act.b, sec.b)
	}
	tree = eRow{kind: eCP, b: act.b}
	if a, b := tab.rowText(act), tab.rowText(tree); a != b {
		t.Errorf("detail %q, want the tree row's %q", a, b)
	}
	if a, b := tab.enterWhy(act), tab.enterWhy(tree); a != b {
		t.Errorf("enter %q, want %q", a, b)
	}
	if act.shadeIn().id != "pcp" || sec.shadeIn().id != "pcp" || act.shadeIn().act == sec.shadeIn().act {
		t.Errorf("twins %v %v", act.shadeIn(), sec.shadeIn())
	}
}

// A paused Checkpoint whose effort runs (in_progress, Live, launching or launched) stays out of the band.
func TestExecActionablePausedCheckpointRunning(t *testing.T) {
	own := labeled("paused:manual")
	cp := func(id, status string) bd.Bead {
		return dbead(id, contract.KindCheckpoint, status, "", 1, isContainer, own)
	}
	g := graphData{
		beads:     []bd.Bead{cp("run", "in_progress"), cp("lv", "in_progress"), cp("la", "open"), cp("ld", "open"), cp("ok", "open")},
		live:      map[string]bool{"lv": true},
		launching: map[string]bool{"la": true},
		launched:  map[string]time.Time{"ld": time.Now()},
	}
	var got []string
	for _, r := range g.actionable() {
		got = append(got, r.b.ID)
	}
	if !slices.Equal(got, []string{"ok"}) {
		t.Errorf("actionable = %v", got)
	}
}

// Spec mc-llx8: every other open non-container Checkpoint member joins only the paused band.
func TestExecActionablePausedBuilds(t *testing.T) {
	own := labeled("paused:manual")
	beads := []bd.Bead{
		dbead("cp", contract.KindCheckpoint, "open", "", 1, isContainer),
		dbead("cp2", contract.KindCheckpoint, "open", "", 1, isContainer, own),
		dbead("plain", contract.KindBuild, "open", "cp", 1), // ready, unpaused AFK: not in Actionable
		dbead("own", contract.KindBuild, "open", "cp", 2, own),
		dbead("nb", contract.KindBuild, "open", "cp", 3, labeled("paused:not-built")),
		dbead("land", contract.KindLanding, "open", "cp", 4, own),
		dbead("inh", contract.KindBuild, "open", "cp2", 1),
		dbead("both", contract.KindBuild, "open", "cp2", 2, own),
		dbead("blk", contract.KindBuild, "open", "cp", 5, own, blockedBy("plain")),
		dbead("live", contract.KindBuild, "open", "cp", 6, own),
		dbead("la", contract.KindBuild, "open", "cp", 7, own),
		dbead("ex", contract.KindBuild, "open", "cp", 8, own),
		dbead("ld", contract.KindBuild, "open", "cp", 9, own),
		dbead("fl", contract.KindBuild, "open", "cp", 10, labeled("paused:failing")),
		dbead("ab", contract.KindBuild, "open", "cp", 11, labeled("paused:abandoned")),
		dbead("ic", contract.KindLanding, "open", "cp", 12, labeled("paused:integrate-conflict")),
		dbead("tk", contract.KindTask, "open", "cp", 13, own), // a member that is also a Task: one row
	}
	beads[0].BlockedBy = []string{"tk"}
	g := graphData{
		beads:     beads,
		sessions:  map[string]session.Info{"live": {State: session.Alive}, "ex": {State: session.Exited}},
		launching: map[string]bool{"la": true},
		launched:  map[string]time.Time{"ld": time.Now()},
	}
	var got []string
	for _, r := range g.actionable() {
		got = append(got, r.b.ID)
	}
	if want := []string{"cp2", "own", "both", "nb", "land", "fl", "ab", "ic", "tk"}; !slices.Equal(got, want) {
		t.Errorf("actionable = %v, want %v", got, want)
	}
	if g.execList(nil)[0].title != "Actionable (9)" {
		t.Errorf("header = %q", g.execList(nil)[0].title)
	}
	if g.execBadge() != 0 {
		t.Errorf("amber = %d", g.execBadge())
	}
}

// Spec mc-llx8 FR 3-10, 12, 13: Defog lists an own-paused ready AFK Chart child in the paused
// band, as a twin of its tree row; unpaused, inherited-only and blocked AFK children are out.
func TestDefogActionablePausedAFK(t *testing.T) {
	own := labeled("paused:manual")
	beads := []bd.Bead{
		dbead("ch", contract.KindChart, "open", "", 1),
		dbead("ch2", contract.KindChart, "open", "", 2, own),
		dbead("plain", contract.KindBuild, "open", "ch", 1),
		dbead("afk", contract.KindBuild, "open", "ch", 2, own),
		dbead("nb", contract.KindLanding, "open", "ch", 3, labeled("paused:not-built")),
		dbead("blk", contract.KindBuild, "open", "ch", 4, own, blockedBy("plain")),
		dbead("inh", contract.KindBuild, "open", "ch2", 1),
		dbead("both", contract.KindBuild, "open", "ch2", 2, own),
		dbead("l", contract.KindGrilling, "open", "ch", 5),
	}
	m := sized(t, 160, 40)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}})
	m = press(t, m, runeKey("2"))
	tab := m.tabAt(tabDefog).(defogTab)
	var got []string
	for _, r := range tab.g.defogAct() {
		got = append(got, r.key)
	}
	m = press(t, defogAt(t, press(t, defogAt(t, m, "i:"), runeKey(" ")), "ch"), runeKey(" ")) // unfold the tree: the AFK child's own row
	if want := []string{"act:l", "act:afk", "act:both", "act:ch2", "act:nb"}; !slices.Equal(got, want) {
		t.Fatalf("Actionable = %v, want %v", got, want)
	}
	if tab.actN() != 5 { // the light-blue ●N
		t.Errorf("actN = %d, want 5", tab.actN())
	}
	if tab.g.defogBadge() != 0 {
		t.Errorf("amber = %d, want 0", tab.g.defogBadge())
	}
	a, twin := defogAt(t, m, "act:afk"), defogAt(t, m, "afk")
	if hintsOf(a) != hintsOf(twin) {
		t.Errorf("hints %q vs %q", hintsOf(a), hintsOf(twin))
	}
	for _, k := range []string{"enter", "d", "p"} {
		_, ca := send(t, a, runeKey(k))
		_, ct := send(t, twin, runeKey(k))
		if (ca == nil) != (ct == nil) {
			t.Errorf("%s differs from the AFK child's tree row", k)
		}
	}
}

// Spec mc-llx8 FR 14: unpausing an AFK child from its Actionable row leaves the cursor on its tree twin.
func TestDefogPausedAFKCursorFollowsTwin(t *testing.T) {
	beads := []bd.Bead{
		dbead("ch", contract.KindChart, "open", "", 1),
		dbead("afk", contract.KindBuild, "open", "ch", 2, labeled("paused:manual")),
	}
	m := sized(t, 160, 40)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}})
	m = press(t, m, runeKey("2"))
	m = press(t, defogAt(t, press(t, defogAt(t, m, "i:"), runeKey(" ")), "ch"), runeKey(" "))
	m = defogAt(t, m, "act:afk")
	beads[1].Labels = nil
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}})
	if r, _ := m.tabAt(tabDefog).(defogTab).selRow(); r.key != "afk" {
		t.Errorf("cursor = %q, want afk", r.key)
	}
}

// Spec mc-llx8 FR 14: unpausing a Build from its Actionable row keeps the cursor on its tree twin.
func TestExecPausedBuildCursorFollowsTwin(t *testing.T) {
	beads := []bd.Bead{
		dbead("cp", contract.KindCheckpoint, "open", "", 1, isContainer),
		dbead("b", contract.KindBuild, "open", "cp", 1, labeled("paused:manual")),
	}
	m, _ := execModel(t, beads)
	m = press(t, execAt(t, m, "cp"), runeKey(" "))
	m = execAt(t, m, "act:b")
	beads[1].Labels = nil
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: execSessions()})
	if r, _ := m.tabAt(tabExecute).(execTab).selRow(); r.key == "s:act" || r.b == nil || r.b.ID != "b" {
		t.Errorf("cursor = %q, want b's tree row", r.key)
	}
}

// Spec mc-f22wp FR 1-3, 5-10, 12-13: Defog's paused band lists every own-paused, otherwise
// ready bead whose home tab is Defog, a defogged Chart and its children included. The
// stamped Chart's land-phase beads arrive through the AFK-paused path (it is listed while it
// has an open child), the no-Initiative Chart's through the listed-Chart path.
func TestDefogActionablePausedHomeTab(t *testing.T) {
	own := labeled("paused:failing")
	defogged := labeled("chart:defogged")
	beads := []bd.Bead{
		dbead("nd", contract.KindChart, "open", "", 3),
		dbead("ndk", contract.KindBuild, "open", "nd", 1, own),
		dbead("dc", contract.KindChart, "open", "", 1, defogged),
		dbead("land", contract.KindLanding, "open", "dc", 1, own),
		dbead("mrw", contract.KindMRWatch, "open", "dc", 2, own),
		dbead("fr", contract.KindForgeReview, "open", "dc", 3, own),
		dbead("unp", contract.KindLanding, "open", "dc", 4),
		dbead("blk", contract.KindLanding, "open", "dc", 5, own, blockedBy("unp")),
		dbead("lv", contract.KindMRWatch, "open", "dc", 6, own),
		dbead("ex", contract.KindForgeReview, "open", "dc", 7, own),
		dbead("pc", contract.KindChart, "open", "", 2, defogged, own),
		dbead("inh", contract.KindLanding, "open", "pc", 1),
	}
	m := sized(t, 160, 40)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{"lv": true}, sessions: map[string]session.Info{"lv": {State: session.Alive}, "ex": {State: session.Exited}}})
	m = press(t, m, runeKey("2"))
	tab := m.tabAt(tabDefog).(defogTab)
	var got []string
	for _, r := range tab.g.defogAct() {
		got = append(got, r.key)
		if r.b.ID == "land" && r.chart.ID != "dc" {
			t.Errorf("land carries chart %q, want dc", r.chart.ID)
		}
	}
	if want := []string{"act:ndk", "act:land", "act:mrw", "act:pc", "act:fr"}; !slices.Equal(got, want) {
		t.Fatalf("Actionable = %v, want %v", got, want)
	}
	if tab.actN() != 5 || tab.g.defogBadge() != 0 {
		t.Errorf("actN = %d, amber = %d, want 5, 0", tab.actN(), tab.g.defogBadge())
	}
	for _, k := range []string{"enter", "d", "p"} {
		_, c := send(t, defogAt(t, m, "act:land"), runeKey(k))
		_, ct := send(t, defogAt(t, m, "act:ndk"), runeKey(k))
		if (c == nil) != (ct == nil) {
			t.Errorf("%s on a Landing differs from a Chart child's twin row", k)
		}
	}
}

// Spec mc-f22wp FR 14, mc-j3mw8 FR 23: unpausing a stamped Chart's Landing from Actionable leaves
// the cursor on its tree row (its Chart's tree row while folded; the Chart's Charts row once the
// Chart is unlisted), and pausing it again lists it in Actionable.
func TestDefogPausedLandingCursorFollowsTwin(t *testing.T) {
	beads := func() []bd.Bead {
		return []bd.Bead{
			dbead("dc", contract.KindChart, "open", "", 1, labeled("chart:defogged")),
			dbead("land", contract.KindLanding, "open", "dc", 1, labeled("paused:failing")),
		}
	}
	cursor := func(m Model, bs []bd.Bead) string {
		m, _ = send(t, m, loadedMsg{beads: bs, live: map[string]bool{}})
		r, _ := m.tabAt(tabDefog).(defogTab).selRow()
		return r.key
	}
	start := func(unfold bool) Model {
		m := sized(t, 160, 40)
		m, _ = send(t, m, loadedMsg{beads: beads(), live: map[string]bool{}})
		m = press(t, treesOpen(m), runeKey("2"))
		if !unfold {
			m = press(t, defogAt(t, m, "dc"), runeKey(" "))
		}
		return defogAt(t, m, "act:land")
	}
	unpaused := beads()
	unpaused[1].Labels = nil
	if got := cursor(start(false), unpaused); got != "dc" {
		t.Errorf("folded: cursor = %q, want the tree Chart row dc", got)
	}
	if got := cursor(start(true), unpaused); got != "land" {
		t.Errorf("unfolded: cursor = %q, want the Landing's tree row", got)
	}
	closed := beads() // the Landing closes: the Chart has no open child and is unlisted
	closed[1].Labels, closed[1].Status = nil, "closed"
	if got := cursor(start(true), closed); got != chartsPrefix+"dc" {
		t.Errorf("unlisted: cursor = %q, want %s", got, chartsPrefix+"dc")
	}
	m, _ := send(t, start(false), loadedMsg{beads: unpaused, live: map[string]bool{}})
	m, _ = send(t, m, loadedMsg{beads: beads(), live: map[string]bool{}})
	m = defogAt(t, m, "act:land")
	if r, _ := m.tabAt(tabDefog).(defogTab).selRow(); r.key != "act:land" {
		t.Errorf("cursor = %q, want act:land", r.key)
	}
}

// Spec mc-f22wp FR 4-5, 6-14: Execute's paused band lists every own-paused, otherwise ready
// bead whose home tab is Execute, a Spec and a bead under no listed Checkpoint included.
func TestExecActionablePausedHomeTab(t *testing.T) {
	own := labeled("paused:failing")
	beads := []bd.Bead{
		dbead("cp", contract.KindCheckpoint, "open", "", 1, isContainer),
		dbead("m", contract.KindBuild, "open", "cp", 1, own),
		dbead("sp", contract.KindSpec, "open", "", 2, own),
		dbead("orph", contract.KindBuild, "open", "", 3, own),
		dbead("inh", contract.KindSpec, "open", "", 4),
		dbead("pc", contract.KindCheckpoint, "open", "", 5, isContainer, own),
		dbead("kid", contract.KindBuild, "open", "pc", 1),
		dbead("blk", contract.KindSpec, "open", "", 6, own, blockedBy("inh")),
		dbead("lv", contract.KindSpec, "open", "", 7, own),
		dbead("ex", contract.KindSpec, "open", "", 8, own),
	}
	m, _ := execModel(t, beads)
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{"lv": true}, sessions: map[string]session.Info{"lv": {State: session.Alive}, "ex": {State: session.Exited}}})
	g := m.tabAt(tabExecute).(execTab).g
	var got []string
	for _, r := range g.actionable() {
		got = append(got, r.key)
		if r.b.ID == "orph" && r.cp != nil {
			t.Errorf("orph carries cp %q, want none", r.cp.ID)
		}
	}
	if want := []string{"act:m", "act:sp", "act:orph", "act:pc"}; !slices.Equal(got, want) {
		t.Fatalf("Actionable = %v, want %v", got, want)
	}
	if g.execList(nil)[0].title != "Actionable (4)" || g.execBadge() != 0 {
		t.Errorf("header = %q, amber = %d", g.execList(nil)[0].title, g.execBadge())
	}
	for _, k := range []string{"enter", "d", "p"} {
		_, ca := send(t, execAt(t, m, "act:sp"), runeKey(k))
		_, ct := send(t, execAt(t, m, specsPrefix+"sp"), runeKey(k))
		if (ca == nil) != (ct == nil) {
			t.Errorf("%s on act:sp differs from its Specs-section twin", k)
		}
	}
}

// Spec mc-f22wp FR 14: pausing then unpausing a Spec keeps the cursor on its twin each time.
func TestExecPausedSpecCursorFollowsTwin(t *testing.T) {
	beads := []bd.Bead{dbead("sp", contract.KindSpec, "open", "", 1)}
	m, _ := execModel(t, beads)
	execRow := func(m Model) string { r, _ := m.tabAt(tabExecute).(execTab).selRow(); return r.key }
	m = execAt(t, m, specsPrefix+"sp")
	beads[0].Labels = []string{"paused:failing"}
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: execSessions()})
	if got := execRow(m); got != specsPrefix+"sp" {
		t.Errorf("after pause cursor = %q, want the Specs row", got)
	}
	m = execAt(t, m, "act:sp")
	beads[0].Labels = nil
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: execSessions()})
	if got := execRow(m); got != specsPrefix+"sp" {
		t.Errorf("after unpause cursor = %q, want the Specs row", got)
	}
}
