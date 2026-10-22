package tui

import (
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// Spec mc-j3mw8 FR 16-21: a stamped Chart stays in Defog's tree while it has an open child.
func TestDefogStampedChartKeptForReviewGrilling(t *testing.T) {
	beads := []bd.Bead{
		dbead("dc", contract.KindChart, "open", "", 1, labeled("chart:defogged")),
		dbead("land", contract.KindLanding, "closed", "dc", 1),
		dbead("mrw", contract.KindMRWatch, "closed", "dc", 2),
		dbead("rev", contract.KindGrilling, "open", "dc", 3),
	}
	g := graphData{beads: beads, sessions: map[string]session.Info{"rev": {State: session.Alive, Waiting: session.WaitTurn}}}
	var act []string
	for _, r := range g.defogAct() {
		act = append(act, r.key)
	}
	if len(act) != 1 || act[0] != "act:rev" {
		t.Errorf("Actionable = %v, want [act:rev]", act)
	}
	if n := g.defogBadge(); n != 1 {
		t.Errorf("badge = %d, want 1 (the waiting review grilling)", n)
	}
	var keys []string
	for _, r := range g.defogList(defogOpen(g)) {
		keys = append(keys, r.key)
	}
	if !containsAll(keys, "dc", "rev") {
		t.Errorf("rows = %v, want the tree Chart with its grilling", keys)
	}
	beads[3].Status = "closed" // the last open child closes: the Chart leaves
	if n := len(graphData{beads: beads}.defogAct()); n != 0 {
		t.Errorf("Actionable = %d rows after the last child closed", n)
	}
	if cs := defogCharts(beads); len(cs) != 0 {
		t.Errorf("listed Charts = %d, want 0", len(cs))
	}
}

// Spec mc-j3mw8 FR 21: an own-paused, open, unblocked, listed stamped Chart joins the paused band.
func TestDefogStampedChartOwnPausedJoinsPausedBand(t *testing.T) {
	beads := []bd.Bead{
		dbead("dc", contract.KindChart, "open", "", 1, labeled("chart:defogged"), labeled("paused:manual")),
		dbead("land", contract.KindLanding, "open", "dc", 1, labeled("paused:failing")),
	}
	var got []string
	for _, r := range (graphData{beads: beads}).defogAct() {
		got = append(got, r.key)
	}
	if len(got) != 2 || got[0] != "act:land" || got[1] != "act:dc" {
		t.Errorf("Actionable = %v, want [act:land act:dc]", got)
	}
}

// Spec mc-j3mw8 FR 24: a Recents jump to an open Landing under a stamped Chart lands on its Defog tree row.
func TestRecentsJumpToStampedChartLanding(t *testing.T) {
	m := sized(t, 160, 40)
	beads := []bd.Bead{
		dbead("dc", contract.KindChart, "open", "", 1, labeled("chart:defogged")),
		dbead("land", contract.KindLanding, "open", "dc", 1),
	}
	m, _ = send(t, m, loadedMsg{beads: beads, live: map[string]bool{}})
	m, _ = m.recentsJump("land")
	if m.tab != tabDefog {
		t.Fatalf("tab = %d, want Defog", m.tab)
	}
	if r, _ := m.tabAt(tabDefog).(defogTab).selRow(); r.key != "land" {
		t.Errorf("cursor = %q, want the tree row land", r.key)
	}
}

func containsAll(have []string, want ...string) bool {
	for _, w := range want {
		found := false
		for _, h := range have {
			found = found || h == w
		}
		if !found {
			return false
		}
	}
	return true
}
