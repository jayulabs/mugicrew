package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

const carriedTip = "aaaa1111bbbb"

func carriedKids(renames []bd.Rename) []bd.Bead {
	body := bd.CarriedApprovalBody("a0", carriedTip, "", "deadbeef0123", renames)
	return []bd.Bead{
		dbead("m", contract.KindBuild, "closed", "cp", 1),
		dbead("a", contract.KindApproval, "closed", "cp", 1, closedWith("approved"), func(b *bd.Bead) { b.Description = body }),
	}
}

func TestRollupApprovedRowReadsCarried(t *testing.T) {
	r := rollupOf(map[string]string{"cp": carriedTip}, carriedKids([]bd.Rename{{Path: "x/0395-a.md", Old: "0394", New: "0395"}, {Path: "y/0397-b.md", Old: "0396", New: "0397"}})...)
	if r.Word != "approved" {
		t.Fatalf("word = %q, want approved", r.Word)
	}
	want := "approved · carried (0394 → 0395, 0396 → 0397) from deadbeef"
	if got := r.label(); got != want {
		t.Errorf("label = %q, want %q", got, want)
	}
	if !strings.Contains(r.Why, "carried (0394 → 0395, 0396 → 0397) from deadbeef") {
		t.Errorf("why = %q", r.Why)
	}
}

func TestRollupApprovedRowUncarriedReadsAsToday(t *testing.T) {
	r := rollupOf(map[string]string{"cp": carriedTip},
		dbead("m", contract.KindBuild, "closed", "cp", 1),
		dbead("a", contract.KindApproval, "closed", "cp", 1, closedWith("approved"), pinned(carriedTip)))
	if got := r.label(); got != "approved" {
		t.Errorf("label = %q, want approved", got)
	}
}

func TestCarriedApprovalRowShowsDimCarriedWord(t *testing.T) {
	g := graphData{beads: append([]bd.Bead{dbead("cp", contract.KindCheckpoint, "open", "", 1, isContainer)}, carriedKids([]bd.Rename{{Old: "0394", New: "0395"}})...)}
	if got := ansi.Strip(g.rowSuffix(g.beadByID("a"))); !strings.Contains(got, "carried") {
		t.Errorf("carried Approval suffix = %q, want carried", got)
	}
	g.beads[2].Description = "Commit: " + carriedTip
	if got := ansi.Strip(g.rowSuffix(g.beadByID("a"))); strings.Contains(got, "carried") {
		t.Errorf("plain Approval suffix = %q, want no carried", got)
	}
}

// A carried Approval is closed at birth, so it never enters Actionable.
func TestCarriedApprovalNeverEntersActionable(t *testing.T) {
	g := graphData{beads: append([]bd.Bead{dbead("cp", contract.KindCheckpoint, "open", "", 1, isContainer)}, carriedKids([]bd.Rename{{Old: "0394", New: "0395"}})...), cpTips: map[string]string{"cp": carriedTip}}
	for _, r := range g.actionable() {
		if r.b.ID == "a" {
			t.Fatalf("carried Approval listed in Actionable")
		}
	}
}
