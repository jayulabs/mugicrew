package tui

import (
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// Rows inside a section sort by effective priority, so a P2 Checkpoint or Chart inside a P0
// Initiative lists above an own-P1 one with no container.
func TestSectionsSortByEffectivePriority(t *testing.T) {
	g := graphData{beads: []bd.Bead{
		dbead("ini", contract.KindInitiative, "open", "", 0),
		dbead("low", contract.KindCheckpoint, "open", "ini", 2, isContainer),
		dbead("own", contract.KindCheckpoint, "open", "", 1, isContainer),
	}}
	if got := execCPSKeys(g.execList(nil)); got != "cps:low,cps:own" {
		t.Errorf("Execute Checkpoints = %s", got)
	}
	kids := graphData{beads: []bd.Bead{
		dbead("cp", contract.KindCheckpoint, "open", "", 0, isContainer),
		dbead("a", contract.KindBuild, "open", "cp", 2),
		dbead("b", contract.KindBuild, "open", "cp", 1),
	}}
	open, _ := kids.cpKids(&kids.beads[0])
	if open[0].ID != "b" {
		t.Errorf("cpKids ties on effective, own breaks: first = %s", open[0].ID)
	}
	d := graphData{beads: []bd.Bead{
		dbead("ini", contract.KindInitiative, "open", "", 0),
		dbead("low", contract.KindChart, "open", "ini", 2),
		dbead("own", contract.KindChart, "open", "", 1),
	}}
	var ids []string
	for _, c := range d.openCharts() {
		ids = append(ids, c.ID)
	}
	if got := strings.Join(ids, ","); got != "low,own" {
		t.Errorf("Defog Charts = %s", got)
	}
}
