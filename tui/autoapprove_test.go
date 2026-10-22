package tui

import (
	"github.com/charmbracelet/x/ansi"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/registry"
)

func autoApproveOn(t *testing.T) {
	t.Helper()
	registry.SetAutoApprove(true)
	t.Cleanup(registry.ResetAutoApprove)
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

func TestRunNowAdmitsAFKApprovalAndRefusesMarkedOne(t *testing.T) {
	plain := dbead("ap", contract.KindApproval, "open", "cp", 1)
	marked := dbead("mk", contract.KindApproval, "open", "cp", 1, labeled(registry.AutoApproveFailedLabel))
	ready := []bd.Bead{plain, marked}

	if ok, _, _ := runNowEligible(plain, ready, ""); ok {
		t.Error("setting off: run-now admitted an Approval as AFK")
	}
	autoApproveOn(t)
	if ok, _, err := runNowEligible(plain, ready, ""); !ok || err != nil {
		t.Errorf("setting on: run-now refused an AFK Approval: ok=%v err=%v", ok, err)
	}
	if ok, _, _ := runNowEligible(marked, ready, ""); ok {
		t.Error("setting on: run-now admitted a marked Approval as AFK")
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
