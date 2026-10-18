package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// claim marks the Initiative id in_progress, as its cut session's launch claim does.
func claim(bs []bd.Bead, id string) []bd.Bead {
	for i := range bs {
		if bs[i].ID == id {
			bs[i].Status = "in_progress"
		}
	}
	return bs
}

func TestIdeateClaimedInitiativeKeepsOpenBehaviour(t *testing.T) {
	// member-less with a cut session: listed, cut and launchable (attach)
	m, _ := drainedModel(t, claim(drainedBeads(), "ua"), map[string]session.Info{"ua": {State: session.Alive, Waiting: session.WaitTurn}})
	if got, want := hintsOf(ideaAt(t, m, "i:ua")), "enter attach · d detail · +/- prio · J/K section"; got != want {
		t.Errorf("claimed cut hints %q, want %q", got, want)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "your turn") {
		t.Errorf("claimed waiting Initiative lacks its wait band:\n%s", v)
	}
	if n := m.tabAt(tabIdeate).count(); n != 1 { // claimed waiting "ua"
		t.Errorf("badge count = %d, want 1", n)
	}
	// drained: header and cue
	m, _ = drainedModel(t, claim(drainedBeads(), "d"), nil)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "all members closed") {
		t.Errorf("claimed drained lost its cue:\n%s", v)
	}
	// bodyless
	m, _ = drainedModel(t, claim(stripBody(drainedBeads()), "d"), nil)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "no body yet · enter: Luffy settles its body") {
		t.Errorf("claimed bodyless lost its cue:\n%s", v)
	}
}

func TestIdeateClaimedInitiativeKeepsNoRefAndLiveDrained(t *testing.T) {
	i := ini("i", "in_progress", "Init", 1)
	if g := (graphData{extRef: true}); !g.noRef(&i) {
		t.Errorf("claimed Initiative without a ref lost its missing-ref flag")
	}
	beads := []bd.Bead{i, {ID: "m", Kind: contract.KindBuild, Status: "closed", Parent: "i", Created: epoch}}
	m := ideaModel(t, 100, 30, beads, map[string]session.Info{"i": {State: session.Alive}})
	for _, r := range m.tabAt(tabIdeate).(ideaTab).rows() {
		if r.live && r.init != nil && r.init.ID == "i" && !r.drained {
			t.Errorf("claimed Live row lost its drained flag")
		}
	}
}
