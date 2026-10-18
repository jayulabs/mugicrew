package tui

import (
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/session"
)

// The shared gates (ADR 0170 §9): a row's enter verb and refusal come from the
// snapshot alone and agree with what the key does.
func TestSharedGates(t *testing.T) {
	cp := bd.Bead{ID: "cp", Kind: contract.KindCheckpoint, Container: true, Status: "open", Title: "Checkpoint"}
	crit := bd.Bead{ID: "cp.2", Kind: contract.KindCritique, Status: "open", Parent: "cp"}
	surg := bd.Bead{ID: "cp.9", Kind: contract.KindSurgery, Status: "in_progress", Parent: "cp"}
	pivot := bd.Bead{ID: "pv", Kind: contract.KindPivot, Status: "open", Title: "Bad FR"}
	task := bd.Bead{ID: "tk", Kind: contract.KindTask, Status: "open", Title: "Mint token"}
	gated := func(b bd.Bead) bd.Bead { b.BlockedBy = []string{pivot.ID}; return b }
	taskHeld := func(b bd.Bead) bd.Bead { b.BlockedBy = []string{task.ID}; return b }
	build := func(id, status string) bd.Bead {
		return bd.Bead{ID: id, Kind: contract.KindBuild, Status: status, Parent: "cp"}
	}
	closedSurg := surg
	closedSurg.Status = "closed"

	for _, c := range []struct {
		name      string
		beads     []bd.Bead
		efforts   map[string]bool
		id        string
		launchWhy string // substring; "" means none
		runWhy    string
		verb      string
		runWhySub string
	}{
		{name: "idle surgery resumes", beads: []bd.Bead{cp, surg}, id: "cp.9", verb: "launch"},
		{name: "idle task resumes", beads: []bd.Bead{{ID: "t", Kind: contract.KindTask, Status: "in_progress"}}, id: "t", verb: "launch"},
		{name: "idle approval refuses", beads: []bd.Bead{{ID: "a", Kind: contract.KindApproval, Status: "in_progress"}}, id: "a", launchWhy: "x recovers it"},
		{name: "idle pivot refuses", beads: []bd.Bead{{ID: "p", Kind: contract.KindPivot, Status: "in_progress"}}, id: "p", launchWhy: "x recovers it"},
		{name: "surgery-held critique", beads: []bd.Bead{cp, crit, surg}, id: "cp.2", runWhySub: "held by open Surgery cp.9 — ending the Surgery releases it"},
		{name: "critique runs once the surgery closes", beads: []bd.Bead{cp, crit, closedSurg}, id: "cp.2", verb: "run"},
		{name: "pivot-gated member", beads: []bd.Bead{gated(cp), pivot, build("cp.1", "open")}, id: "cp.1", runWhySub: "cp is gated by Bad FR (pv) — not running"},
		{name: "signal-held member runs", beads: []bd.Bead{func() bd.Bead { b := cp; b.BlockedBy = []string{"sg"}; return b }(), {ID: "sg", Kind: contract.KindSignal, Status: "open"}, build("cp.1", "open")}, id: "cp.1", verb: "run"},
		{name: "task-held member runs", beads: []bd.Bead{taskHeld(cp), task, build("cp.1", "open")}, id: "cp.1", verb: "run"},
		{
			name: "review due", beads: []bd.Bead{cp, build("cp.1", "closed")}, id: "cp",
			runWhySub: "review due — the loop spawns it",
		},
		{
			name: "no members is not review due", beads: []bd.Bead{cp}, id: "cp",
			runWhySub: "needs a closed Approval",
		},
		{
			name: "a closed Review alone no longer vetoes review due", beads: []bd.Bead{cp, build("cp.1", "closed"), {ID: "cp.r", Kind: contract.KindReview, Status: "closed", Parent: "cp"}}, id: "cp",
			runWhySub: "review due",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := graphData{beads: c.beads, efforts: c.efforts, sessions: map[string]session.Info{}}
			b := g.beadByID(c.id)
			if got := g.launchWhy(b); c.launchWhy != "" || c.verb == "launch" {
				if (c.launchWhy == "") != (got == "") || !strings.Contains(got, c.launchWhy) {
					t.Errorf("launchWhy = %q, want substring %q", got, c.launchWhy)
				}
			}
			if c.runWhySub != "" {
				if got := g.runNowWhy(b); !strings.Contains(got, c.runWhySub) {
					t.Errorf("runNowWhy = %q, want substring %q", got, c.runWhySub)
				}
			}
			if got := g.enterVerb(b); got != c.verb {
				t.Errorf("enterVerb = %q, want %q", got, c.verb)
			}
		})
	}
}

func TestCheckpointLooksLandableCountsOnlyApprovedClose(t *testing.T) {
	for _, tc := range []struct {
		reason string
		want   bool
	}{
		{"approved", true},
		{"rework: mc-1", false},
		{"Approval obsoleted: tip moved", false},
		{"obsolete: x", false},
		{"", false},
	} {
		g := graphData{beads: []bd.Bead{{ID: "a", Kind: contract.KindApproval, Status: "closed", CloseReason: tc.reason, Parent: "cp"}}}
		if got := g.checkpointLooksLandable("cp"); got != tc.want {
			t.Errorf("close reason %q: looks landable = %v, want %v", tc.reason, got, tc.want)
		}
	}
}
