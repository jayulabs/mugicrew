package tui

import (
	"github.com/charmbracelet/x/ansi"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/loop"
	"jayulabs.org/mugicrew/registry"
)

func autoApproveOn(t *testing.T) {
	t.Helper()
	registry.SetAutoApprove(true)
	t.Cleanup(registry.ResetModeSnapshots)
}

func actionableIDs(g graphData) []string {
	var ids []string
	for _, r := range g.actionable() {
		ids = append(ids, r.b.ID)
	}
	return ids
}

func TestActionableDropsAFKApprovalButKeepsMarkedOne(t *testing.T) {
	beads := append(execFixture(),
		dbead("mk", contract.KindApproval, "open", "cpA", 2, labeled(registry.AutoApproveFailedLabel)))
	g := graphData{beads: beads, sessions: execSessions()}

	if ids := actionableIDs(g); !slices.Contains(ids, "ap") || !slices.Contains(ids, "mk") {
		t.Fatalf("setting off: actionable = %v, want ap and mk listed", ids)
	}

	autoApproveOn(t)
	ids := actionableIDs(g)
	if slices.Contains(ids, "ap") {
		t.Errorf("setting on: waiting AFK Approval ap still in actionable %v", ids)
	}
	if !slices.Contains(ids, "mk") {
		t.Errorf("setting on: marked Approval mk missing from actionable %v", ids)
	}
	if !slices.Contains(ids, "sg") { // Surgery is unchanged
		t.Errorf("setting on: Surgery sg missing from actionable %v", ids)
	}
}

func TestIsHITLReadsApprovalMode(t *testing.T) {
	plain := dbead("ap", contract.KindApproval, "open", "cp", 1)
	marked := dbead("mk", contract.KindApproval, "open", "cp", 1, labeled(registry.AutoApproveFailedLabel))
	if !isHITL(&plain) {
		t.Error("setting off: Approval is not HITL")
	}
	autoApproveOn(t)
	if isHITL(&plain) {
		t.Error("setting on: Approval is HITL")
	}
	if !isHITL(&marked) {
		t.Error("setting on: marked Approval is not HITL")
	}
}

func TestMarkedApprovalShowsQualifierInActionable(t *testing.T) {
	autoApproveOn(t)
	beads := append(execFixture(),
		dbead("mk", contract.KindApproval, "open", "cpA", 2, labeled(registry.AutoApproveFailedLabel)))
	tab := execTab{g: graphData{beads: beads, sessions: execSessions()}}
	tab.flip = tab.expanded()
	var marked, plain string
	for _, r := range tab.g.actionable() {
		l, _ := tab.line(r, 10, nil, "")
		switch r.b.ID {
		case "mk":
			marked = ansi.Strip(l)
		case "sg":
			plain = ansi.Strip(l)
		}
	}
	if !strings.Contains(marked, "auto-approve failed") {
		t.Errorf("marked Approval row lacks the qualifier: %q", marked)
	}
	if strings.Contains(plain, "auto-approve failed") {
		t.Errorf("unmarked row carries the qualifier: %q", plain)
	}
}

func TestKindlessBeadIsNotHITL(t *testing.T) {
	kindless := bd.Bead{ID: "k"}
	if isHITL(&kindless) {
		t.Error("kindless bead is HITL")
	}
	if (graphData{}).launches(&kindless) {
		t.Error("kindless bead is a launchable leaf")
	}
}

// A refresh stamps the mode snapshots through a seam TestMain stubs, so a
// machine-local auto_approve never leaks into the tests that follow.
func TestRefreshDoesNotStampTheRealAutoApprove(t *testing.T) {
	o := loop.ReadAutoApprove
	t.Cleanup(func() { loop.ReadAutoApprove = o; registry.ResetModeSnapshots() })
	loop.ReadAutoApprove = func() loop.AutoApprove { return loop.AutoApprove{On: true} }
	registry.ResetModeSnapshots()

	New(nil, nil).load()()

	if !isHITL(&bd.Bead{Kind: contract.KindApproval}) {
		t.Fatal("a refresh stamped the real auto_approve: an Approval reads AFK")
	}
}

// The test binary's stamp reads fixed modes, never the machine's auto_approve or
// land_strategy: a plain Approval stays HITL after a stamp.
func TestStampReadsFixedModesNotTheMachinesConfig(t *testing.T) {
	t.Cleanup(registry.ResetModeSnapshots)
	loop.StampModeSnapshots()
	if got := registry.ModeOf(contract.KindApproval, nil); got != registry.HITL {
		t.Errorf("plain Approval after a stamp = %v, want HITL", got)
	}
}
