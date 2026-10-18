package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

func execCPSKeys(rows []eRow) string {
	var ks []string
	for _, r := range rows {
		if r.kind == eCPS {
			ks = append(ks, r.key)
		}
	}
	return strings.Join(ks, ",")
}

func TestExecCheckpointsSection(t *testing.T) {
	g := graphData{beads: execFixture()}
	rows := g.execList(nil)
	// between Live and Initiatives; Drafts (cd) and non-Checkpoints are out; cs (cut) outranks cpA
	var heads []string
	for _, r := range rows {
		if r.kind == eSec && isSectionKey(r.key) {
			heads = append(heads, r.key+"="+r.title)
		}
	}
	if got, want := strings.Join(heads, ","), "s:act=Actionable (6),s:live=Live (0),s:specs=Specs (2),s:cps=Checkpoints (2),s:tree=Initiatives (2)"; got != want {
		t.Errorf("sections = %s, want %s", got, want)
	}
	if got := execCPSKeys(rows); got != "cps:cpA,cps:cs" {
		t.Errorf("rows = %s", got)
	}
	// rank first, beadCmp second: a Checkpoint with a bigger rank number sorts last whatever its priority
	g.beads = append(g.beads, dbead("cz", contract.KindCheckpoint, "open", "", 0, isContainer))
	if got := execCPSKeys(g.execList(nil)); got != "cps:cpA,cps:cs,cps:cz" {
		t.Errorf("rank order = %s", got) // cpA and cs are blocked (5), cs ranked after cpA; cz is cut (12)
	}
	// folding hides the rows, keeps N; empty gets a placeholder
	rows = g.execList(map[string]bool{cpsKey: true})
	if execCPSKeys(rows) != "" || !strings.Contains(strings.Join(eshape(rows), ","), "Checkpoints (3)") {
		t.Errorf("folded = %v", eshape(rows))
	}
	if s := strings.Join(eshape(graphData{}.execList(nil)), ","); !strings.Contains(s, "Checkpoints (0),.,# Initiatives") {
		t.Errorf("empty = %s", s)
	}
}

// A Checkpoint is Actionable only when it carries its own pause (see TestExecActionablePausedCheckpoint).
func TestExecCheckpointsNeverActionable(t *testing.T) {
	g := graphData{beads: execFixture()}
	for _, r := range g.actionable() {
		if r.b.Container {
			t.Errorf("%s in Actionable", r.key)
		}
	}
	if g.execBadge() != 0 {
		t.Errorf("badge = %d", g.execBadge())
	}
}

func TestExecCheckpointsRowLineAndDetail(t *testing.T) {
	g := graphData{beads: execFixture()}
	tab := execTab{g: g}
	var sec, tree *eRow
	for _, r := range g.execList(map[string]bool{"i:i1": true}) {
		switch r.key {
		case "cps:cpA":
			sec = &r
		case "cpA":
			tree = &r
		}
	}
	if sec == nil || tree == nil {
		t.Fatalf("sec=%v tree=%v", sec, tree)
	}
	l, _ := tab.body(*sec, 12, false, "")
	if !strings.Contains(l, "blocked on Task") {
		t.Errorf("line %q lacks the Rollup word", l)
	}
	if a, b := tab.rowText(*sec), tab.rowText(*tree); a != b {
		t.Errorf("detail %q, want the tree row's %q", a, b)
	}
}

func TestExecCheckpointsFoldPersistsAndCursorFallsBack(t *testing.T) {
	dir := t.TempDir()
	stubCommonDir(t, dir)
	m, _ := execModelFresh(t, nil)
	m, _ = send(t, m, loadedMsg{beads: execFixture(), live: map[string]bool{}, sessions: execSessions()})
	m = press(t, m, runeKey("3"))
	press(t, execAt(t, m, cpsKey), runeKey(" "))
	if got := readExecFolds(t, dir); got != `{"s:cps":true}` {
		t.Errorf("folds = %s", got)
	}
	// enter and hints on the section row match the tree row
	m = press(t, execAt(t, m, "i:i1"), runeKey(" "))
	if a, b := hintsOf(execAt(t, m, "cps:cpA")), hintsOf(execAt(t, m, "cpA")); strings.Replace(b, "space fold · ", "", 1) != a {
		t.Errorf("hints %q vs tree %q", a, b)
	}
	// the Checkpoint closes: the cursor falls back to what is left of its twin chain
	m = execAt(t, m, "cps:cs")
	beads := slices.Clone(execFixture())
	for i := range beads {
		if beads[i].ID == "cs" {
			beads[i].Status = "closed"
		}
	}
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}, sessions: execSessions()})
	tab := m.tabAt(tabExecute).(execTab)
	if rows := tab.rows(); strings.HasPrefix(rows[tab.selIndex(rows)].key, cpsPrefix) {
		t.Errorf("cursor stays on %q", rows[tab.selIndex(rows)].key)
	}
}

// landedFixture: closed Checkpoints with their Landing, closed `ago` before now.
func landedFixture(now time.Time) []bd.Bead {
	cp, ln := contract.KindCheckpoint, contract.KindLanding
	closedAt := func(ago time.Duration, reason string) func(*bd.Bead) {
		return func(b *bd.Bead) { b.Closed, b.CloseReason = now.Add(-ago), reason }
	}
	return []bd.Bead{
		dbead("old", cp, "closed", "", 1, isContainer),
		dbead("oldL", ln, "closed", "old", 1, closedAt(23*time.Hour+59*time.Minute, "landed: 1111111122222222")),
		dbead("new", cp, "closed", "", 1, isContainer),
		dbead("newL", ln, "closed", "new", 1, closedAt(time.Hour, "landed: aaaaaaaabbbbbbbb")),
		dbead("out", cp, "closed", "", 1, isContainer),
		dbead("outL", ln, "closed", "out", 1, closedAt(24*time.Hour+time.Minute, "landed: cccccccc")),
		dbead("swept", cp, "closed", "", 1, isContainer),
		dbead("sweptL", ln, "closed", "swept", 1, closedAt(time.Hour, "obsolete: branch advanced without landing")),
		dbead("chart", contract.KindChart, "closed", "", 1),
		dbead("chartL", ln, "closed", "chart", 1, closedAt(time.Hour, "landed: dddddddd")),
	}
}

func TestExecLandedSubsection(t *testing.T) {
	now := time.Now()
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = time.Now })
	g := graphData{beads: append(execFixture(), landedFixture(now)...)}
	keys := func(rows []eRow) (out []string) {
		for _, r := range rows {
			if r.kind == eLanded {
				out = append(out, r.key)
			}
		}
		return
	}
	// folded by default: the header, nested last in the section (before Initiatives), no rows
	rows := g.execList(nil)
	at := slices.IndexFunc(rows, func(r eRow) bool { return r.key == landedKey })
	if at < 0 || rows[at].title != "Landed in last 24h (2)" || rows[at].depth != 1 || rows[at+1].key != treeKey || len(keys(rows)) != 0 {
		t.Fatalf("folded = %v", eshape(rows))
	}
	// unfolded: newest first; the window edge, obsolete: and Chart Landings stay out
	rows = g.execList(map[string]bool{landedKey: true})
	if got := strings.Join(keys(rows), ","); got != "landed:new,landed:old" {
		t.Errorf("rows = %s", got)
	}
	// omitted at zero
	if rows := (graphData{beads: execFixture()}).execList(map[string]bool{landedKey: true}); slices.ContainsFunc(rows, func(r eRow) bool { return r.key == landedKey }) {
		t.Errorf("header at zero")
	}
}

func TestExecLandedRowLineDetailAndEnter(t *testing.T) {
	now := time.Now()
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = time.Now })
	g := graphData{beads: landedFixture(now)}
	tab := execTab{g: g}
	var row eRow
	for _, r := range g.execList(map[string]bool{landedKey: true}) {
		if r.key == "landed:new" {
			row = r
		}
	}
	if l, _ := tab.body(row, 12, false, ""); !strings.Contains(l, "landed aaaaaaaa · 1h") {
		t.Errorf("line %q", l)
	}
	if d := tab.rowText(row); !strings.Contains(d, "Landing newL closed landed: aaaaaaaabbbbbbbb") {
		t.Errorf("detail %q", d)
	}
	if why := tab.enterWhy(row); why != "landed — nothing to launch" {
		t.Errorf("enter = %q", why)
	}
}

// A Checkpoints-section row draws as a container row: prio colour kept, title not AFK-muted.
func TestExecCheckpointsRowContainerLook(t *testing.T) {
	forceANSI256(t)
	g := graphData{beads: append(execFixture(),
		dbead("c0", contract.KindCheckpoint, "open", "", 0, isContainer),
		dbead("lb", contract.KindBuild, "open", "cpA", 3))}
	et := execTab{g: g}
	ran := map[string]bool{}
	var leaf *eRow
	rows := g.execList(nil)
	for i, r := range rows {
		if leaf == nil && r.kind == eSPS {
			leaf = &rows[i]
		}
		switch {
		case r.kind == eCPS && r.key == cpsPrefix+"c0":
			ran["c0"] = true
			s, _ := et.body(r, 6, false, "")
			if !strings.Contains(s, prio(0)) {
				t.Errorf("P0 row lacks prio(0): %q", s)
			}
			if strings.Contains(s, leafTitle(r.b, false)) && leafTitle(r.b, false) != r.b.Title {
				t.Errorf("title is AFK-muted: %q", s)
			}
			if !strings.Contains(s, dimSt.Render(g.rollup(r.b).label())) {
				t.Errorf("rollup word missing: %q", s)
			}
			if hot, _ := et.body(r, 6, true, ""); !strings.Contains(hot, pinkFg.Render(r.b.Title)) {
				t.Errorf("hot title not pink: %q", hot)
			}
		case r.kind == eCPS && r.key == cpsPrefix+"cpA":
			ran["cpA"] = true
			if s, _ := et.body(r, 6, false, ""); !strings.Contains(s, prio(1)) {
				t.Errorf("P1 row lacks prio(1): %q", s)
			}
			// FR7: the title starts in the shared kind-badge column, as a leaf row's does
			const tagW = 14
			s, _ := et.body(r, tagW, false, "")
			if leaf == nil {
				t.Fatal("no Specs-section leaf row")
			}
			ls, _ := et.body(*leaf, tagW, false, "")
			col := func(s, title string) int { return lipgloss.Width(s[:strings.Index(s, title)]) }
			if a, b := col(s, r.b.Title), col(ls, leaf.b.Title); a != b {
				t.Errorf("title column %d, leaf row's %d", a, b)
			}
		}
	}
	for _, id := range []string{"c0", "cpA"} {
		if !ran[id] {
			t.Fatalf("no Checkpoints-section row for %s", id)
		}
	}
	// regression (FR8): an AFK leaf row rendered by the Execute tab keeps its muted badge
	g.beads = append(g.beads, dbead("lb0", contract.KindBuild, "in_progress", "cpA", 0))
	g.live = map[string]bool{"lb0": true}
	et = execTab{g: g}
	var found bool
	for _, r := range g.execList(nil) {
		if r.b == nil || r.b.ID != "lb0" || r.kind == eCPS {
			continue
		}
		found = true
		cp, _ := g.containerPrio(r.b)
		s, _ := et.body(r, 6, false, "")
		if strings.Contains(s, prio(0)) || !strings.Contains(s, tinted(softSt, cp).Render("P0")) {
			t.Errorf("leaf badge not muted: %q", s)
		}
	}
	if !found {
		t.Fatal("no AFK leaf row for lb0")
	}
}
