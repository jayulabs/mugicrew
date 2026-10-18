package tui

import (
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

func specsKeys(rows []eRow) string {
	var ks []string
	for _, r := range rows {
		if r.kind == eSPS {
			ks = append(ks, r.key)
		}
	}
	return strings.Join(ks, ",")
}

func TestExecSpecsSection(t *testing.T) {
	sp := contract.KindSpec
	g := graphData{beads: []bd.Bead{
		dbead("s2", sp, "open", "", 3),
		dbead("s1", sp, "in_progress", "", 1),
		dbead("sb", sp, "blocked", "", 2),
		dbead("sd", sp, "deferred", "", 0), // a Draft
		dbead("sc", sp, "closed", "", 0),
		dbead("cp", contract.KindCheckpoint, "open", "", 1, isContainer),
	}}
	rows := g.execList(nil)
	if got := specsKeys(rows); got != "specs:s1,specs:sb,specs:s2" {
		t.Errorf("rows = %s", got)
	}
	// Live, then Specs, then Checkpoints; unfolded by default
	if got := strings.Join(eshape(rows), ","); !strings.Contains(got, "# Live (0),.,# Specs (3),P s1,P sb,P s2,# Checkpoints (1)") {
		t.Errorf("shape = %s", got)
	}
	if rows := g.execList(map[string]bool{specsKey: true}); specsKeys(rows) != "" {
		t.Errorf("folded still lists %s", specsKeys(rows))
	}
	if s := strings.Join(eshape(graphData{}.execList(nil)), ","); !strings.Contains(s, "# Specs (0),.,# Checkpoints") {
		t.Errorf("empty = %s", s)
	}
	// not Actionable, not in the badge
	if g.execBadge() != (graphData{beads: g.beads[5:]}).execBadge() {
		t.Errorf("badge moved by Specs")
	}
	for _, r := range g.actionable() {
		if r.kind == eSPS {
			t.Errorf("%s in Actionable", r.key)
		}
	}
}

func TestExecSpecsRowActsAsTreeRow(t *testing.T) {
	dir := t.TempDir()
	stubCommonDir(t, dir)
	m, _ := execModelFresh(t, nil)
	m, _ = send(t, m, loadedMsg{beads: execFixture(), live: map[string]bool{}, sessions: execSessions()})
	m = press(t, m, runeKey("3"))
	m = press(t, execAt(t, m, "i:i1"), runeKey(" "))
	if a, b := hintsOf(execAt(t, m, "specs:sp")), hintsOf(execAt(t, m, "sp")); a != b {
		t.Errorf("hints %q vs tree %q", a, b)
	}
	tab := execAt(t, m, "specs:sp").tabAt(tabExecute).(execTab)
	if a, b := tab.rowText(eRow{kind: eSPS, b: tab.selected()}), tab.rowText(eRow{kind: eSpec, b: tab.selected()}); a != b {
		t.Errorf("detail %q vs tree %q", a, b)
	}
	press(t, execAt(t, m, specsKey), runeKey(" "))
	if got := readExecFolds(t, dir); got != `{"i:i1":true,"s:specs":true}` {
		t.Errorf("folds = %s", got)
	}
}
