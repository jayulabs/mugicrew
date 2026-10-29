package loop

import (
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// A Checkpoint verdict reads Children, Landed and the tip once per Container for the
// view's lifetime, however many beads are judged, and calls the supplied effort func at
// most once.
func TestLiveViewMemoisesPerContainer(t *testing.T) {
	stubLoopReads(t)
	oHead := headOf
	t.Cleanup(func() { headOf = oHead })
	var kidReads, landedReads, tipReads, effortReads int
	listChildren = func(id string) ([]bd.Bead, error) {
		kidReads++
		return []bd.Bead{{ID: "b1", Kind: contract.KindBuild, Status: "closed", Parent: id}}, nil
	}
	landedBase = func(string) (string, error) { landedReads++; return "", git.ErrNotLanded }
	headOf = func(string) (string, error) { tipReads++; return "tip", nil }
	view := newLive(func() *git.EffortSet { effortReads++; return git.NewEffortSet() })

	cp := bd.Bead{ID: "cp", Kind: contract.KindCheckpoint, Status: "open"}
	for range 3 {
		if v, err := Judge(cp, ModeLoop, []bd.Bead{cp}, Inputs{}, view); err != nil || v.Action != ActionReview {
			t.Fatalf("verdict = %+v, %v; want review", v, err)
		}
	}
	if kidReads != 1 || landedReads != 1 || tipReads != 1 || effortReads != 1 {
		t.Errorf("reads children/landed/tip/efforts = %d/%d/%d/%d, want 1 each", kidReads, landedReads, tipReads, effortReads)
	}
}
