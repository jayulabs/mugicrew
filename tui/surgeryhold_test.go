package tui

import (
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// t on an open Critique refuses while its Checkpoint has an open Surgery, naming
// it (ADR 0160); once the Surgery closes the same Critique is eligible.
func TestRunNowCritiqueHeldBySurgery(t *testing.T) {
	orig := surgeryHold
	t.Cleanup(func() { surgeryHold = orig })
	crit := bd.Bead{ID: "cp1.2", Kind: contract.KindCritique, Status: "open", Parent: "cp1"}
	ready := []bd.Bead{crit}
	surg := bd.Bead{ID: "cp1.9", Kind: contract.KindSurgery, Status: "in_progress"}
	surgeryHold = func(bd.Bead) ([]bd.Bead, error) { return []bd.Bead{surg}, nil }

	ok, held, err := runNowEligible(crit, ready, "")
	if ok || err != nil {
		t.Fatalf("held Critique: ok %v, err %v; want refused", ok, err)
	}
	if got := runNowHint(crit, ready, "", held); !strings.Contains(got, "cp1.9") {
		t.Errorf("hint = %q, want it to name the Surgery", got)
	}

	surgeryHold = func(bd.Bead) ([]bd.Bead, error) { return nil, nil }
	if ok, _, err := runNowEligible(crit, ready, ""); !ok || err != nil {
		t.Errorf("released Critique: ok %v, err %v; want eligible", ok, err)
	}
}

// Run-now ignores [jira] poll_interval (ADR 0222 §5): a ready latch is eligible
// however recently the loop last checked it.
func TestRunNowLatchEligibleRegardlessOfLastCheck(t *testing.T) {
	latch := bd.Bead{ID: "l1", Kind: contract.KindLatch, Status: "open"}
	if ok, _, err := runNowEligible(latch, []bd.Bead{latch}, ""); !ok || err != nil {
		t.Errorf("latch: ok %v, err %v; want eligible", ok, err)
	}
}
