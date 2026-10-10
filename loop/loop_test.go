package loop

import (
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/dispatch"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/git"
)

// The loop resolves its cwd to the repo root via git.RepoRoot (FR8/FR21), so the
// in-process leaf crew.Run inherits repo-root cwd regardless of the invocation
// dir — not just when a human launches mugicrew from the root. Asserted through
// the git.RepoRoot var seam: override it to a temp dir and confirm chdirRepoRoot
// lands the process there. EvalSymlinks makes /tmp-vs-realpath honest.
func TestChdirRepoRootUsesGitSeam(t *testing.T) {
	orig := git.RepoRoot
	cwd, _ := os.Getwd()
	defer func() { git.RepoRoot = orig; os.Chdir(cwd) }()

	root, _ := filepath.EvalSymlinks(t.TempDir())
	git.RepoRoot = func() (string, error) { return root, nil }
	if err := chdirRepoRoot(); err != nil {
		t.Fatalf("chdirRepoRoot: %v", err)
	}
	got, _ := os.Getwd()
	if got, _ = filepath.EvalSymlinks(got); got != root {
		t.Errorf("cwd = %q, want repo root %q from the git.RepoRoot seam", got, root)
	}
}

// The scope fence (spec §6.1/§6.3): only mode:afk registry leaves dispatch.
// HITL, unowned, container, and non-mugicrew kinds are not leaves.
func TestIsAFKLeafFencesToRegistry(t *testing.T) {
	cases := []struct {
		kind contract.Kind
		want bool
	}{
		{contract.KindResearch, true},      // afk leaf
		{contract.KindChartConflict, true}, // afk leaf since ADR 0080
		{contract.KindSpec, true},          // registered afk leaf — dispatched to Zoro (FR1)
		{contract.KindChart, false},        // afk but container, not a leaf
		{contract.KindSignal, false},       // hitl — the TUI owns it
		{contract.KindGrilling, false},     // hitl
		{contract.KindBuild, true},         // Franky's afk leaf — a candidate; the crew-lookup skip (no franky package yet) is dispatch's job, not the fence's
		{contract.KindApproval, false},     // hitl tail (Luffy) — surfaced, not looped
		{contract.KindPivot, false},        // hitl (Zoro) — a pivot is only ever settled by a human (FR26)
		{contract.KindCheckpoint, false},   // registered afk container, not a leaf
		{contract.KindRollback, true},      // Jinbe's afk leaf (ADR 0047) — the loop runs it, t triggers it
		{contract.KindTask, false},         // HITL (brook) — only a human launches a Task (FR19)
		{"", false},                        // non-mugicrew planning bead (no kind)
	}
	for _, c := range cases {
		if got := IsAFKLeaf(bd.Bead{Kind: c.kind}); got != c.want {
			t.Errorf("IsAFKLeaf(%q) = %v, want %v", c.kind, got, c.want)
		}
	}
}

// The effort liveness view's row fence (ADR 0032 §4): in_progress AFK kinds,
// containers included; HITL claims, open beads, and unregistered kinds get no verdict.
func TestIsActiveAFKEffort(t *testing.T) {
	cases := []struct {
		kind   contract.Kind
		status string
		want   bool
	}{
		{contract.KindBuild, "in_progress", true},      // afk leaf
		{contract.KindCheckpoint, "in_progress", true}, // afk container (land) runs as an effort too
		{contract.KindChart, "in_progress", true},      // afk container (drain/speccing)
		{contract.KindBuild, "open", false},            // not claimed
		{contract.KindSignal, "in_progress", false},    // hitl claim — no verdict
		{"", "in_progress", false},                     // non-mugicrew bead
	}
	for _, c := range cases {
		if got := IsActiveAFKEffort(bd.Bead{Kind: c.kind, Status: c.status}); got != c.want {
			t.Errorf("IsActiveAFKEffort(%q,%q) = %v, want %v", c.kind, c.status, got, c.want)
		}
	}
}

// A charted Chart is a drain candidate regardless of chart:defogged:
// candidacy is (kind:chart AFK container), so both the promote-fog and
// stamp-defogged flows route through this one path and reconcileChart
// discriminates downstream. A non-chart bead (research leaf, or the sibling
// Checkpoint container) is excluded.
func TestIsDrainedChartCandidate(t *testing.T) {
	charted := bd.Bead{Kind: contract.KindChart, Description: "Destination: ship it"}
	if !IsDrainedChartCandidate(charted) {
		t.Error("a charted, not-defogged Chart must be a drain candidate")
	}
	defogged := bd.Bead{Kind: contract.KindChart, Description: "Destination: ship it", Labels: []string{"chart:defogged"}}
	if !IsDrainedChartCandidate(defogged) {
		t.Error("a drained defogged Chart must still be a candidate — both flows route through one path")
	}
	// The placeholder gate is retired (ADR 0059 §7): Drafts are the one gate, so a
	// published Chart whose body is still the placeholder IS a candidate.
	placeholder := bd.Bead{Kind: contract.KindChart, Status: "open", Description: contract.ChartPlaceholderBody}
	if !IsDrainedChartCandidate(placeholder) {
		t.Error("a published Chart with the placeholder body must be a candidate — the body gate is retired")
	}
	if IsDrainedChartCandidate(bd.Bead{Kind: contract.KindResearch, Description: "Destination: ship it"}) {
		t.Error("a non-chart leaf kind is never a drain-chart candidate")
	}
	if IsDrainedChartCandidate(bd.Bead{Kind: contract.KindCheckpoint, Description: "Destination: ship it"}) {
		t.Error("a Checkpoint container is not a drain-chart candidate — the kind:chart guard separates them")
	}
}

// A Checkpoint is a drain candidate purely on being a registered AFK container of
// kind checkpoint — no chart:defogged-style label (FR43). The approvedAtHead /
// landed-trailer readiness is the I/O half CheckpointReady adds.
func TestIsCheckpointCandidate(t *testing.T) {
	if !IsCheckpointCandidate(bd.Bead{Kind: contract.KindCheckpoint}) {
		t.Error("a Checkpoint container must be a candidate")
	}
	if IsCheckpointCandidate(bd.Bead{Kind: contract.KindChart, Labels: []string{"chart:defogged"}}) {
		t.Error("a Chart is not a Checkpoint candidate")
	}
	if IsCheckpointCandidate(bd.Bead{Kind: contract.KindBuild}) {
		t.Error("a leaf kind is never a Checkpoint candidate")
	}
}

// Drafts are the one Chart gate (ADR 0059 §3, §7): a charted Chart whose only
// children are Drafts is not drained, so it is not a candidate — bd.OpenChildren
// counts each child through Bead.Open, which the stub mirrors.
func TestChartWithOnlyDraftChildrenIsNotCandidate(t *testing.T) {
	oKids := openChildren
	t.Cleanup(func() { openChildren = oKids })
	kids := []bd.Bead{{ID: "k1", Kind: contract.KindResearch, Status: "deferred", Labels: []string{"drafted-by:ws-sig"}}}
	openChildren = func(string) (int, error) {
		n := 0
		for _, k := range kids {
			if k.Open() {
				n++
			}
		}
		return n, nil
	}
	chart := bd.Bead{ID: "ws-chart", Kind: contract.KindChart, Status: "open", Description: "Destination: ship it"}
	got, err := (&Loop{}).candidates([]bd.Bead{chart}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("candidates = %v, want none: Draft children undrain the Chart", bd.IDs(got))
	}
}

// A closed child still holding effort/<child> undrains its Chart (Spec
// D7, FR 47): candidacy skips the Chart and waits silently, no event per tick.
func TestChartWithAHeldChildIsNotCandidateAndLogsNothing(t *testing.T) {
	oKids := openChildren
	t.Cleanup(func() { openChildren = oKids })
	evs := captureEvents(t)
	kids := []bd.Bead{{ID: "k1", Kind: contract.KindResearch, Status: "closed"}}
	openChildren = func(string) (int, error) {
		open, held := bd.DrainBlockersOver(kids, map[string]bool{"k1": true})
		return open + len(held), nil
	}
	chart := bd.Bead{ID: "ws-chart", Kind: contract.KindChart, Status: "open", Description: "Destination: ship it"}
	got, err := (&Loop{}).candidates([]bd.Bead{chart}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("candidates = %v, want none: k1's effort ref undrains the Chart", bd.IDs(got))
	}
	if len(*evs) != 0 {
		t.Errorf("events = %v, want none: an automated gate waits silently", eventlogtest.Names(*evs))
	}
}

// A paused bead is out of candidacy for every candidate kind, whatever its cause,
// and only the bead itself is read — an unpaused sibling still dispatches (ADR
// 0055 §3).
func TestCandidatesSkipPausedBeads(t *testing.T) {
	oKids, oClosed, oApproved, oOpen := openChildren, closedApprovalsOf, approvedAtHead, openChildrenOf
	t.Cleanup(func() {
		openChildren, closedApprovalsOf, approvedAtHead, openChildrenOf = oKids, oClosed, oApproved, oOpen
	})
	openChildren = func(string) (int, error) { return 0, nil }
	openChildrenOf = func(string, contract.Kind) (int, error) { return 0, nil }
	closedApprovalsOf = func(string, contract.Kind) ([]bd.Bead, error) { return []bd.Bead{{}}, nil }
	approvedAtHead = func(string, string) bool { return true }

	body := "Destination: ship it"
	var ready, want []bd.Bead
	for _, k := range []contract.Kind{contract.KindResearch, contract.KindChart, contract.KindCheckpoint} {
		for _, cause := range []string{bd.PausedForeignHold, bd.PausedManual, bd.PausedFailing} {
			ready = append(ready, bd.Bead{ID: string(k) + "-" + cause, Kind: k, Description: body, Labels: []string{"paused:" + cause}})
		}
		ready = append(ready, bd.Bead{ID: string(k) + "-both", Kind: k, Description: body, Labels: []string{"paused:foreign-hold", "paused:manual"}})
		live := bd.Bead{ID: string(k) + "-live", Kind: k, Description: body}
		ready, want = append(ready, live), append(want, live)
	}
	got, err := (&Loop{}).candidates(ready, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bd.IDs(got), bd.IDs(want)) {
		t.Errorf("candidates = %v, want only the unpaused %v", bd.IDs(got), bd.IDs(want))
	}
}

// An open surgery Checkpoint whose only member is an in_progress Surgery yields no
// candidate (ADR 0092): the Surgery is a HITL leaf, never an AFK one, and the
// Checkpoint has no at-head Approval nor a landed trailer, and the open Surgery
// keeps its Review from being due, so Jinbe's arm reads false.
func TestCandidatesSkipOpenSurgeryCheckpoint(t *testing.T) {
	stubCheckpointReady(t)
	surgery := bd.Bead{ID: "ws-surg", Kind: contract.KindSurgery, Status: "in_progress", Parent: "ws-cp"}
	reviewDue = func(id string, approved func(string, string) bool) (bool, error) {
		atHead := func(body string) bool { return approved(body, "checkpoint/"+id) }
		return bd.ReviewDueOver([]bd.Bead{surgery}, nil, nil, atHead), nil
	}
	openChildrenOf = func(string, contract.Kind) (int, error) { return 0, nil }
	closedApprovalsOf = func(string, contract.Kind) ([]bd.Bead, error) { return nil, nil }
	landedBase = func(string) (string, error) { return "", errors.New("no landed commit") }

	ready := []bd.Bead{
		{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open"},
		surgery,
	}
	if got, err := (&Loop{}).candidates(ready, nil); err != nil || len(got) != 0 {
		t.Errorf("candidates = %v, %v; want none, no error", bd.IDs(got), err)
	}
}

// No test reaches a live bd for the inherited-pause reads: nothing is paused and
// an unknown parent is an unpaused root. A test that cares stubs them.
func TestMain(m *testing.M) {
	pausedBeads = func() ([]bd.Bead, error) { return nil, nil }
	showBead = func(id string) (bd.Bead, error) { return bd.Bead{ID: id}, nil }
	membersDrained = func(string) (bool, error) { return true, nil } // never read a real bd or git
	openBlockers = func(string) ([]bd.Bead, error) { return nil, nil }
	openBeads = func() ([]bd.Bead, error) { return nil, nil }
	os.Exit(m.Run())
}

// Inherited pause (ADR 0081): a member under a manual-paused container is skipped
// in every arm, at any depth, reaching the paused ancestor through the known rows
// or one show; a container's failing pause stays local.
func TestCandidatesSkipInheritedPause(t *testing.T) {
	oKids, oClosed, oApproved, oOpen, oShow := openChildren, closedApprovalsOf, approvedAtHead, openChildrenOf, showBead
	t.Cleanup(func() {
		openChildren, closedApprovalsOf, approvedAtHead, openChildrenOf, showBead = oKids, oClosed, oApproved, oOpen, oShow
	})
	openChildren = func(string) (int, error) { return 0, nil }
	openChildrenOf = func(string, contract.Kind) (int, error) { return 0, nil }
	closedApprovalsOf = func(string, contract.Kind) ([]bd.Bead, error) { return []bd.Bead{{}}, nil }
	approvedAtHead = func(string, string) bool { return true }
	// ws-mid is in no set the tick holds, so the walk shows it.
	showBead = func(id string) (bd.Bead, error) {
		if id == "ws-mid" {
			return bd.Bead{ID: id, Parent: "mc-paused"}, nil
		}
		return bd.Bead{}, fmt.Errorf("unexpected show %s", id)
	}

	known := []bd.Bead{
		{ID: "mc-paused", Kind: contract.KindChart, Labels: []string{"paused:manual"}},
		{ID: "mc-failing", Kind: contract.KindChart, Labels: []string{"paused:failing"}},
	}
	body := "Destination: ship it"
	var ready, want []bd.Bead
	for _, k := range []contract.Kind{contract.KindResearch, contract.KindChart, contract.KindCheckpoint} {
		for _, parent := range []string{"mc-paused", "ws-mid"} {
			ready = append(ready, bd.Bead{ID: string(k) + "-under-" + parent, Kind: k, Description: body, Parent: parent})
		}
		live := bd.Bead{ID: string(k) + "-under-failing", Kind: k, Description: body, Parent: "mc-failing"}
		ready, want = append(ready, live), append(want, live)
	}
	got, err := (&Loop{}).candidates(ready, known)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bd.IDs(got), bd.IDs(want)) {
		t.Errorf("candidates = %v, want only those under the failing parent %v", bd.IDs(got), bd.IDs(want))
	}
}

// An own-paused bead is skipped before any ancestor walk: a show that would fail
// on its walk never fails the tick for a bead skipped anyway.
func TestCandidatesOwnPauseSkipsWalk(t *testing.T) {
	oShow := showBead
	t.Cleanup(func() { showBead = oShow })
	showBead = func(id string) (bd.Bead, error) { return bd.Bead{}, fmt.Errorf("unexpected show %s", id) }
	known := []bd.Bead{{ID: "mc-paused", Labels: []string{"paused:manual"}}}
	ready := []bd.Bead{{ID: "ws-own", Kind: contract.KindResearch, Parent: "mc-unknown", Labels: []string{"paused:manual"}}}
	if got, err := (&Loop{}).candidates(ready, known); err != nil || len(got) != 0 {
		t.Errorf("candidates = %v, %v; want none, no error", bd.IDs(got), err)
	}
}

// A failed paused-list read fails the tick — never a silent "nothing paused" —
// and nothing is claimed.
func TestTickFailsOnPausedListError(t *testing.T) {
	oReady, oIP, oPaused, oClaim := readyBeads, inProgressBeads, pausedBeads, claimBead
	t.Cleanup(func() { readyBeads, inProgressBeads, pausedBeads, claimBead = oReady, oIP, oPaused, oClaim })
	readyBeads = func() ([]bd.Bead, error) { return []bd.Bead{{ID: "mc-r", Kind: contract.KindResearch}}, nil }
	inProgressBeads = func() ([]bd.Bead, error) { return nil, nil }
	boom := errors.New("bd list: boom")
	pausedBeads = func() ([]bd.Bead, error) { return nil, boom }
	claimBead = func(bd.Bead) error { t.Error("a failed paused read must claim nothing"); return nil }
	if err := (&Loop{}).tick(); !errors.Is(err, boom) {
		t.Errorf("tick = %v, want the paused-list error", err)
	}
}

// The Checkpoint drain-candidacy arm (FR43/FR44): ready iff a closed Approval pins
// the current tip (approvedAtHead) OR the land trailer is already on mainline (roll-
// forward). A stale pin (HEAD moved) with no landed trailer is NOT ready on these
// arms; the Review arm is TestCheckpointReadyWhenReviewDue's.
func TestCheckpointReady(t *testing.T) {
	stubCheckpointReady(t)

	cp := bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "in_progress"}
	notLanded := func(string) (string, error) { return "", errors.New("no landed commit") }
	openChildrenOf = func(string, contract.Kind) (int, error) { return 0, nil }

	// Approved at head → ready.
	closedApprovalsOf = func(string, contract.Kind) ([]bd.Bead, error) {
		return []bd.Bead{{Description: "Commit: cafef00d"}}, nil
	}
	approvedAtHead = func(string, string) bool { return true }
	landedBase = notLanded
	if ready, err := CheckpointReady(cp); err != nil || !ready {
		t.Errorf("an approved-at-head Checkpoint must be ready, got (%v, %v)", ready, err)
	}

	// Stale approval (pin != tip), not landed → NOT ready.
	approvedAtHead = func(string, string) bool { return false }
	if ready, err := CheckpointReady(cp); err != nil || ready {
		t.Errorf("a stale approval with no landed trailer must NOT be ready, got (%v, %v)", ready, err)
	}

	// No approval but the land trailer is on mainline (post-land crash) → ready to roll forward.
	closedApprovalsOf = func(string, contract.Kind) ([]bd.Bead, error) { return nil, nil }
	landedBase = func(string) (string, error) { return "landsha", nil }
	if ready, err := CheckpointReady(cp); err != nil || !ready {
		t.Errorf("an already-landed-but-open Checkpoint must be ready to roll forward, got (%v, %v)", ready, err)
	}
}

// stubCheckpointReady restores CheckpointReady's seams after the test and stubs
// reviewDue to "not due", so a test opts into the Review arm explicitly.
func stubCheckpointReady(t *testing.T) {
	t.Helper()
	oClosed, oApproved, oLanded, oOpen, oDue, oDrained, oBlockers := closedApprovalsOf, approvedAtHead, landedBase, openChildrenOf, reviewDue, membersDrained, openBlockers
	t.Cleanup(func() {
		closedApprovalsOf, approvedAtHead, landedBase, openChildrenOf, reviewDue, membersDrained, openBlockers = oClosed, oApproved, oLanded, oOpen, oDue, oDrained, oBlockers
	})
	reviewDue = func(string, func(string, string) bool) (bool, error) { return false, nil }
	membersDrained = func(string) (bool, error) { return true, nil }
}

// An Approval at head is no candidate while a closed member still holds
// effort/<id> (Spec FR 28), silently (FR 47); with the ref gone it is.
func TestCheckpointReadyApprovedWaitsOnLiveEffortRef(t *testing.T) {
	stubCheckpointReady(t)
	openChildrenOf = func(string, contract.Kind) (int, error) { return 0, nil }
	landedBase = func(string) (string, error) { return "", errors.New("no landed commit") }
	closedApprovalsOf = func(string, contract.Kind) ([]bd.Bead, error) {
		return []bd.Bead{{Description: "Commit: tip"}}, nil
	}
	approvedAtHead = func(string, string) bool { return true }
	kids := []bd.Bead{{ID: "c1", Kind: contract.KindConflict, Status: "closed"}}
	cp := bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open"}
	for _, tc := range []struct {
		efforts map[string]bool
		want    bool
	}{{map[string]bool{"c1": true}, false}, {nil, true}} {
		membersDrained = func(string) (bool, error) { return bd.MembersDrainedOver(kids, tc.efforts), nil }
		evs := captureEvents(t)
		if ready, err := CheckpointReady(cp); err != nil || ready != tc.want {
			t.Errorf("efforts %v: CheckpointReady = %v, %v; want %v", tc.efforts, ready, err, tc.want)
		}
		if len(*evs) != 0 {
			t.Errorf("events = %v, want none: an automated gate waits silently", eventlogtest.Names(*evs))
		}
	}
}

// A drained Checkpoint with no Approval at head is a candidate, so Jinbe re-derives
// the Review spawn every tick (Spec FR 30); a closed member still holding
// effort/<id> keeps it out, silently (FR 47).
func TestCheckpointReadyWhenReviewDue(t *testing.T) {
	stubCheckpointReady(t)
	openChildrenOf = func(string, contract.Kind) (int, error) { return 0, nil }
	closedApprovalsOf = func(string, contract.Kind) ([]bd.Bead, error) { return nil, nil }
	landedBase = func(string) (string, error) { return "", errors.New("no landed commit") }
	kids := []bd.Bead{{ID: "b1", Kind: contract.KindBuild, Status: "closed"}}
	cp := bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open"}
	for _, tc := range []struct {
		efforts map[string]bool
		want    bool
	}{{nil, true}, {map[string]bool{"b1": true}, false}} {
		reviewDue = func(id string, approved func(string, string) bool) (bool, error) {
			atHead := func(body string) bool { return approved(body, "checkpoint/"+id) }
			return bd.ReviewDueOver(kids, tc.efforts, nil, atHead), nil
		}
		evs := captureEvents(t)
		if ready, err := CheckpointReady(cp); err != nil || ready != tc.want {
			t.Errorf("efforts %v: CheckpointReady = %v, %v; want %v", tc.efforts, ready, err, tc.want)
		}
		if len(*evs) != 0 {
			t.Errorf("events = %v, want none: an automated gate waits silently", eventlogtest.Names(*evs))
		}
	}
}

// After a stale-Approval self-heal CheckpointReady sees >=2 closed
// Approvals — the stale obsoleted one first, the real at-head one second. It must scan ALL
// of them and stay a candidate on the at-head one; a first-match read on the stale pin
// (approvedAtHead false) with no landed trailer would strand the land off the loop.
func TestCheckpointReadyScansAllClosedApprovalsAfterSelfHeal(t *testing.T) {
	stubCheckpointReady(t)

	cp := bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "in_progress"}
	openChildrenOf = func(string, contract.Kind) (int, error) { return 0, nil }
	landedBase = func(string) (string, error) { return "", errors.New("no landed commit") }
	closedApprovalsOf = func(string, contract.Kind) ([]bd.Bead, error) {
		return []bd.Bead{{Description: "Commit: oldpin"}, {Description: "Commit: newtip"}}, nil
	}
	// Only the second (at-head) Approval's pin matches the tip.
	approvedAtHead = func(body, _ string) bool { return body == "Commit: newtip" }
	if ready, err := CheckpointReady(cp); err != nil || !ready {
		t.Errorf("an at-head Approval behind a stale one must keep the Checkpoint ready, got (%v, %v)", ready, err)
	}
}

// The land-only hold (Spec D6, FR 16): bd ready hides a blocked
// Checkpoint and its members, so the frontier re-admits a Checkpoint whose every
// open blocker is a Task, plus its open, otherwise-unblocked members. Any other
// open blocker (a pivot's gate) keeps it, and its members, hidden.
func TestReadmitTaskHeldCheckpoint(t *testing.T) {
	task := bd.Bead{ID: "ws-task", Kind: contract.KindTask, Status: "open"}
	pivot := bd.Bead{ID: "ws-pivot", Kind: contract.KindPivot, Status: "open"}
	held := bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open", Container: true, BlockedBy: []string{"ws-task", "mc-donetask"}}
	build := bd.Bead{ID: "ws-cp.1", Kind: contract.KindBuild, Status: "open", Parent: "ws-cp"}
	open := []bd.Bead{
		task, pivot, held, build,
		{ID: "mc-donetask", Kind: contract.KindTask, Status: "closed"},
		{ID: "ws-cp.2", Kind: contract.KindBuild, Status: "open", Parent: "ws-cp", BlockedBy: []string{"ws-cp.1"}},
		{ID: "ws-cp.3", Kind: contract.KindBuild, Status: "in_progress", Parent: "ws-cp"},
		{ID: "ws-cp.4", Kind: contract.KindBuild, Status: "deferred", Parent: "ws-cp"},
		{ID: "ws-gated", Kind: contract.KindCheckpoint, Status: "open", Container: true, BlockedBy: []string{"ws-task", "ws-pivot"}},
		{ID: "ws-gated.1", Kind: contract.KindBuild, Status: "open", Parent: "ws-gated"},
		{ID: "ws-after", Kind: contract.KindTask, Status: "open", BlockedBy: []string{"ws-task"}},
	}
	ready := []bd.Bead{task, pivot}
	got := Readmit(ready, open)
	if want := []string{"ws-task", "ws-pivot", "ws-cp", "ws-cp.1"}; !reflect.DeepEqual(bd.IDs(got), want) {
		t.Errorf("Readmit = %v, want %v", bd.IDs(got), want)
	}
	// Already in the frontier: no duplicate.
	if got := Readmit([]bd.Bead{held, build}, open); !reflect.DeepEqual(bd.IDs(got), []string{"ws-cp", "ws-cp.1"}) {
		t.Errorf("Readmit over a frontier already holding them = %v, want no duplicates", bd.IDs(got))
	}
}

// A re-admitted Checkpoint's open members are loop candidates, and so is the
// drained Checkpoint itself for its Review spawn (FR 16): only the land waits.
func TestReadmittedCheckpointMembersAreCandidates(t *testing.T) {
	stubCheckpointReady(t)
	openChildrenOf = func(string, contract.Kind) (int, error) { return 0, nil }
	closedApprovalsOf = func(string, contract.Kind) ([]bd.Bead, error) { return nil, nil }
	landedBase = func(string) (string, error) { return "", errors.New("no landed commit") }
	reviewDue = func(string, func(string, string) bool) (bool, error) { return true, nil }
	openBlockers = func(string) ([]bd.Bead, error) {
		return []bd.Bead{{ID: "ws-task", Kind: contract.KindTask, Status: "open"}}, nil
	}
	open := []bd.Bead{
		{ID: "ws-task", Kind: contract.KindTask, Status: "open"},
		{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open", Container: true, BlockedBy: []string{"ws-task"}},
		{ID: "ws-cp.1", Kind: contract.KindBuild, Status: "open", Parent: "ws-cp"},
	}
	got, err := (&Loop{}).candidates(Readmit(nil, open), nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ws-cp", "ws-cp.1"}; !reflect.DeepEqual(bd.IDs(got), want) {
		t.Errorf("candidates = %v, want %v", bd.IDs(got), want)
	}
}

// The land guard (FR 14): an approved-at-head, drained Checkpoint with an open
// blocker is not land-ready; closing the blocker makes it land-ready with no
// extra step (FR 17).
func TestCheckpointReadyHeldByOpenBlocker(t *testing.T) {
	stubCheckpointReady(t)
	openChildrenOf = func(string, contract.Kind) (int, error) { return 0, nil }
	closedApprovalsOf = func(string, contract.Kind) ([]bd.Bead, error) {
		return []bd.Bead{{Description: "Commit: cafef00d"}}, nil
	}
	approvedAtHead = func(string, string) bool { return true }
	landedBase = func(string) (string, error) { return "", errors.New("no landed commit") }
	var blockers []bd.Bead
	openBlockers = func(string) ([]bd.Bead, error) { return blockers, nil }
	cp := bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open"}

	blockers = []bd.Bead{{ID: "ws-task", Kind: contract.KindTask, Status: "open", Title: "Rotate the key"}}
	if ready, err := CheckpointReady(cp); err != nil || ready {
		t.Errorf("held by an open Task: CheckpointReady = %v, %v; want not ready", ready, err)
	}
	if _, held, _ := CheckpointLandHold(cp); !reflect.DeepEqual(bd.Titles(held), []string{"Rotate the key"}) {
		t.Errorf("CheckpointLandHold held = %v, want the open blocker", bd.Titles(held))
	}
	blockers = nil // the Task closed
	if ready, err := CheckpointReady(cp); err != nil || !ready {
		t.Errorf("Task closed: CheckpointReady = %v, %v; want ready", ready, err)
	}
	openBlockers = func(string) ([]bd.Bead, error) { return nil, errors.New("boom") }
	if _, err := CheckpointReady(cp); err == nil {
		t.Error("a blocker read error must propagate, not read as unheld")
	}
}

// An open kind:conflict or kind:critique member undrains the Checkpoint: even
// with an approval still pinned to the tip (approvedAtHead true), a pending
// conflict/critique means re-dispatch would busy-spin, so candidacy must exclude
// it — restoring the undrain property the drained-Chart gate has (FR43/FR44).
func TestCheckpointReadyExcludesOpenConflictOrCritique(t *testing.T) {
	stubCheckpointReady(t)

	cp := bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "in_progress"}
	// Approved at head AND landed — the readiness arms both say "ready".
	closedApprovalsOf = func(string, contract.Kind) ([]bd.Bead, error) {
		return []bd.Bead{{Description: "Commit: cafef00d"}}, nil
	}
	approvedAtHead = func(string, string) bool { return true }
	landedBase = func(string) (string, error) { return "landsha", nil }

	for _, open := range []contract.Kind{contract.KindConflict, contract.KindCritique} {
		open := open
		openChildrenOf = func(_ string, k contract.Kind) (int, error) {
			if k == open {
				return 1, nil
			}
			return 0, nil
		}
		if ready, err := CheckpointReady(cp); err != nil || ready {
			t.Errorf("a Checkpoint with an open %s child must NOT be ready, got (%v, %v)", open, ready, err)
		}
	}

	// The open-children read failing propagates, not silently "ready".
	openChildrenOf = func(string, contract.Kind) (int, error) { return 0, errors.New("boom") }
	if _, err := CheckpointReady(cp); err == nil {
		t.Error("an open-children read error must propagate")
	}
}

// The AFK pool's in-flight total is re-derived from bd's in_progress beads,
// whatever their owner. Only mode:afk beads take a slot: non-mugicrew beads (no
// kind) and HITL beads — even nami-owned ones a human is working — must not
// shrink the pool (Spec FR5/FR10; spec §6.3).
func TestCountInflightCountsOnlyAFK(t *testing.T) {
	got := CountInflight([]bd.Bead{
		{Kind: contract.KindResearch}, // nami afk → counts
		{Kind: contract.KindChart},    // nami afk container → counts
		{Kind: contract.KindBuild},    // franky afk → counts
		{Kind: contract.KindGrilling}, // hitl, nami-owned → must NOT count
		{Kind: ""},                    // non-mugicrew → ignored
	})
	if got != 3 {
		t.Errorf("afk inflight = %d, want 3 (every owner's AFK counts; HITL grilling must not take a pool slot)", got)
	}
}

// The Zoro wiring proof — a registered kind:spec resolves to owner
// "zoro" via the registry row (FR1); the tail kind:build now resolves to "franky",
// while an unregistered kind (task) stays ownerless and is left
// sitting. Leaf-vs-worktree dispatch moved to the dispatch package
// (dispatch.TestRunEffortRoutesPerKind); not re-spelled here.
func TestSpecOwnedByZoro(t *testing.T) {
	if got := ownerOf(bd.Bead{Kind: contract.KindSpec}); got != "zoro" {
		t.Errorf("kind:spec owner = %q, want zoro", got)
	}
	if got := ownerOf(bd.Bead{Kind: contract.KindBuild}); got != "franky" {
		t.Errorf("kind:build owner = %q, want franky", got)
	}
	if got := ownerOf(bd.Bead{Kind: contract.Kind("unowned")}); got != "" {
		t.Errorf("an unregistered kind must have no owner (left sitting); got %q", got)
	}
}

// The AFK pool (Spec FR5/FR6/FR8/FR11): picks stop once in-flight plus
// picked reach the pool, in any mix of owners, the same owner more than once.
func TestSelectDispatchPool(t *testing.T) {
	r1 := bd.Bead{ID: "r1", Kind: contract.KindResearch, Parent: "ws-c1"} // nami, chart/ws-c1
	r2 := bd.Bead{ID: "r2", Kind: contract.KindResearch, Parent: "ws-c2"} // nami, chart/ws-c2
	r3 := bd.Bead{ID: "r3", Kind: contract.KindResearch, Parent: "ws-c3"} // nami, chart/ws-c3
	f1 := bd.Bead{ID: "f1", Kind: contract.KindBuild, Parent: "mc-k"}     // franky, checkpoint/mc-k
	ids := func(bs []bd.Bead) string { return strings.Join(bd.IDs(bs), ",") }
	for _, c := range []struct {
		name           string
		cands          []bd.Bead
		pool, inflight int
		want           string
	}{
		{"same owner more than once", []bd.Bead{r1, r2}, 3, 0, "r1,r2"},
		{"mix of owners up to the pool", []bd.Bead{r1, f1, r2, r3}, 3, 0, "r1,f1,r2"},
		{"pool already full", []bd.Bead{r1, f1}, 3, 3, ""},
		{"lowered pool drains: over-full picks nothing", []bd.Bead{r1}, 1, 3, ""},
		{"partly full", []bd.Bead{r1, r2, f1}, 3, 2, "r1"},
		{"unowned kind skipped", []bd.Bead{{ID: "x", Kind: contract.Kind("unowned")}, r1}, 1, 0, "r1"},
	} {
		if got := ids(selectDispatch(c.cands, c.pool, c.inflight, nil)); got != c.want {
			t.Errorf("%s: picked %q, want %q", c.name, got, c.want)
		}
	}
}

// Container-level steps stay one per container (ADR 0102, Spec D11): a
// Chart or Checkpoint step whose container an in-flight step holds, or a pick
// earlier this tick took, is skipped and selection falls through without spending
// a pool slot. Leaves never skip: each works its own effort/<id> and integrates
// under the land lock, so siblings in one container dispatch concurrently (FR 54).
func TestSelectDispatchSkipsHeldContainer(t *testing.T) {
	r := bd.Bead{ID: "r", Kind: contract.KindResearch, Parent: "mc-c"}                            // nami, onto chart/mc-c
	f1 := bd.Bead{ID: "f1", Kind: contract.KindBuild, Parent: "mc-k"}                             // franky, onto checkpoint/mc-k
	f2 := bd.Bead{ID: "f2", Kind: contract.KindBuild, Parent: "mc-k"}                             // franky, onto checkpoint/mc-k
	k := bd.Bead{ID: "mc-k", Kind: contract.KindCheckpoint}                                       // jinbe, lands checkpoint/mc-k
	c := bd.Bead{ID: "mc-c", Kind: contract.KindChart}                                            // nami, charting chart/mc-c
	cl := bd.Bead{ID: "mc-c", Kind: contract.KindChart, Labels: []string{contract.LabelDefogged}} // nami, lands chart/mc-c
	j := bd.Bead{ID: "mc-j", Kind: contract.KindCheckpoint}                                       // jinbe, lands checkpoint/mc-j
	ids := func(bs []bd.Bead) string { return strings.Join(bd.IDs(bs), ",") }
	for _, tc := range []struct {
		name           string
		cands          []bd.Bead
		pool, inflight int
		held           map[string]bool
		want           string
	}{
		{"sibling Builds both pick at Cap 2", []bd.Bead{f1, f2}, 2, 0, nil, "f1,f2"},
		{"a leaf ignores a held container", []bd.Bead{r, f1}, 3, 0, map[string]bool{"chart/mc-c": true, "checkpoint/mc-k": true}, "r,f1"},
		{"two container steps on one Chart, one tick", []bd.Bead{c, cl}, 3, 0, nil, "mc-c"},
		{"in-flight holds the container", []bd.Bead{k, j}, 3, 0, map[string]bool{"checkpoint/mc-k": true}, "mc-j"},
		{"partly full plus held: held spends no slot", []bd.Bead{k, f1, j}, 3, 1, map[string]bool{"checkpoint/mc-k": true}, "f1,mc-j"},
	} {
		if got := ids(selectDispatch(tc.cands, tc.pool, tc.inflight, tc.held)); got != tc.want {
			t.Errorf("%s: picked %q, want %q", tc.name, got, tc.want)
		}
	}
}

// LiveAFK keeps only the active AFK efforts the probe finds alive, asking it about
// nothing else (ADR 0068 §5).
func TestLiveAFK(t *testing.T) {
	list := func() ([]bd.Bead, error) {
		return []bd.Bead{
			{ID: "live", Kind: contract.KindBuild, Status: "in_progress"},
			{ID: "dead", Kind: contract.KindBuild, Status: "in_progress"},
			{ID: "hitl", Kind: contract.KindGrilling, Status: "in_progress"},
			{ID: "open", Kind: contract.KindBuild, Status: "open"},
		}, nil
	}
	var asked []string
	alive := func(ids []string) map[string]bool { asked = ids; return map[string]bool{"live": true} }
	got, err := LiveAFK(list, alive)()
	if err != nil || strings.Join(bd.IDs(got), ",") != "live" || strings.Join(asked, ",") != "live,dead" {
		t.Errorf("LiveAFK = %v, %v (asked %v); want [live], asked [live dead]", bd.IDs(got), err, asked)
	}
}

// The loop passes its live-AFK reader to Dispatch and to resume's ForeignHeld (ADR
// 0068 §5). A sibling's live TUI run-now tree, fed through LiveAFK into the real
// ForeignHeld, is on its own effort branch (Spec D1), so it never accounts
// for a hold on p's effort/p: resume keeps paused:foreign-hold up. Dispatch gets the
// same reader for its effort.busy classification.
func TestLoopPassesLiveAFKReader(t *testing.T) {
	captureEvents(t)
	oReady, oIP, oClaim, oRun, oUn, oLive, oOn := readyBeads, inProgressBeads, claimBead, runEffort, unpauseBead, liveAFK, git.WorktreeOn
	t.Cleanup(func() {
		readyBeads, inProgressBeads, claimBead, runEffort, unpauseBead, liveAFK, git.WorktreeOn = oReady, oIP, oClaim, oRun, oUn, oLive, oOn
	})
	git.WorktreeOn = func(string) (git.Holder, bool, error) { return git.Holder{Path: "/tmp/mugicrew-wt-7/wt"}, true, nil }
	runNow := bd.Bead{ID: "tui", Kind: contract.KindChartConflict, Parent: "mc-c", Status: "in_progress"}
	liveAFK = LiveAFK(func() ([]bd.Bead, error) { return []bd.Bead{runNow}, nil }, func(ids []string) map[string]bool {
		return map[string]bool{"tui": true}
	})
	readyBeads = func() ([]bd.Bead, error) {
		return []bd.Bead{
			{ID: "p", Kind: contract.KindChartConflict, Parent: "mc-c", Labels: []string{"paused:" + bd.PausedForeignHold}},
			{ID: "r", Kind: contract.KindChartConflict, Parent: "mc-c"},
		}, nil
	}
	inProgressBeads = func() ([]bd.Bead, error) { return nil, nil }
	claimBead = func(bd.Bead) error { return nil }
	var lifted []string
	unpauseBead = func(id, _ string) error { lifted = append(lifted, id); return nil }
	var runOK bool
	runEffort = func(_ contract.Ctx, _ contract.Crewmate, _ bd.Bead, _ io.Writer, _ string, in dispatch.InflightFunc) (dispatch.Outcome, error) {
		got, err := in()
		runOK = err == nil && len(got) == 1 && got[0].ID == "tui"
		return dispatch.Outcome{Cut: dispatch.CutBusy}, nil
	}
	l := &Loop{Crew: map[string]contract.Crewmate{"nami": nil}}
	if err := l.tick(); err != nil {
		t.Fatal(err)
	}
	l.wg.Wait()
	if len(lifted) != 0 {
		t.Errorf("lifted %v; a sibling's run-now tree must not account for p's held effort branch", lifted)
	}
	if !runOK {
		t.Error("dispatch did not pass the live-AFK reader to Dispatch")
	}
}

// captureEvents captures events (eventlogtest.Capture), and fails the test if
// anything reaches stderr meanwhile.
func captureEvents(t *testing.T) *[]eventlogtest.Event {
	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	origStderr := os.Stderr
	os.Stderr = stderr
	t.Cleanup(func() {
		os.Stderr = origStderr
		if b, _ := os.ReadFile(stderr.Name()); len(b) > 0 {
			t.Errorf("stderr = %q, want nothing", b)
		}
	})
	return eventlogtest.Capture(t)
}

// noBD makes every bd shell-out fail, as a real bd outage would.
func noBD(t *testing.T) { t.Setenv("PATH", t.TempDir()) }

// A failed tick is a loop.tick_failed event at Error carrying err.
func TestTickFailureIsAnEvent(t *testing.T) {
	evs := captureEvents(t)
	noBD(t)
	(&Loop{}).poll()
	if len(*evs) != 1 || (*evs)[0].Name != "loop.tick_failed" || (*evs)[0].Level != slog.LevelError || (*evs)[0].Attrs["err"] == "" {
		t.Errorf("events = %+v, want one Error loop.tick_failed with err", *evs)
	}
}

// A bead whose owner has no crewmate is loop.no_owner at Warn, and nothing is claimed.
func TestNoOwnerIsAnEvent(t *testing.T) {
	evs := captureEvents(t)
	(&Loop{Crew: map[string]contract.Crewmate{}}).dispatch(bd.Bead{ID: "mc-r", Kind: contract.KindResearch, Parent: "mc-c"})
	if len(*evs) != 1 || (*evs)[0].Name != "loop.no_owner" || (*evs)[0].Level != slog.LevelWarn || (*evs)[0].Attrs["bead"] != "mc-r" {
		t.Errorf("events = %+v, want one Warn loop.no_owner for mc-r", *evs)
	}
}

type idleCrew struct{}

func (idleCrew) Run(contract.Ctx, contract.Bead) error { return nil }
func (idleCrew) Launch(contract.Ctx, contract.Bead) (contract.LaunchCommand, error) {
	return nil, nil
}

// A failed claim is claim.failed at Error (by, err) and dispatches nothing — no
// effort.* from the loop, which no longer reports a Run itself.
func TestClaimFailureIsAnEvent(t *testing.T) {
	evs := captureEvents(t)
	noBD(t)
	b := bd.Bead{ID: "mc-r", Kind: contract.KindResearch, Parent: "mc-c"}
	(&Loop{Crew: map[string]contract.Crewmate{ownerOf(b): idleCrew{}}}).dispatch(b)
	if len(*evs) != 1 || (*evs)[0].Name != "claim.failed" || (*evs)[0].Level != slog.LevelError ||
		(*evs)[0].Attrs["by"] != ownerOf(b) || (*evs)[0].Attrs["err"] == "" {
		t.Errorf("events = %+v, want one Error claim.failed by %s with err", *evs, ownerOf(b))
	}
}

// Loop and dispatch write nothing via stdlib log: their outcomes are events, and
// stderr would clobber the TUI alt-screen.
func TestNoStdlibLog(t *testing.T) {
	for _, p := range []string{"loop.go", "../dispatch/dispatch.go"} {
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, im := range f.Imports {
			if im.Path.Value == `"log"` {
				t.Errorf("%s imports stdlib log", p)
			}
		}
	}
}

// blockingLoop is a Loop whose first tick dispatches one research leaf to an effort
// that blocks until release closes; ticks counts polls. The bd/claim/dispatch seams
// are stubbed, so no live bd runs.
func blockingLoop(t *testing.T) (l *Loop, b bd.Bead, started chan string, release chan struct{}, ticks *atomic.Int32) {
	t.Helper()
	cwd, _ := os.Getwd()
	oRoot, oReady, oIP, oClaim, oRun, oPause := git.RepoRoot, readyBeads, inProgressBeads, claimBead, runEffort, pauseBead
	release = make(chan struct{})
	t.Cleanup(func() {
		os.Chdir(cwd)
		git.RepoRoot, readyBeads, inProgressBeads, claimBead, runEffort, pauseBead = oRoot, oReady, oIP, oClaim, oRun, oPause
	})
	pauseBead = func(string, string) error { return nil } // a force stop's abandon pause never reaches a live bd
	git.RepoRoot = func() (string, error) { return cwd, nil }
	b = bd.Bead{ID: "mc-r", Title: "Research: r", Kind: contract.KindResearch, Parent: "mc-c"}
	ticks = &atomic.Int32{}
	readyBeads = func() ([]bd.Bead, error) {
		if ticks.Add(1) == 1 {
			return []bd.Bead{b}, nil
		}
		return nil, nil
	}
	inProgressBeads = func() ([]bd.Bead, error) { return nil, nil }
	claimBead = func(bd.Bead) error { return nil }
	started = make(chan string, 1)
	runEffort = func(_ contract.Ctx, _ contract.Crewmate, _ bd.Bead, _ io.Writer, id string, _ dispatch.InflightFunc) (dispatch.Outcome, error) {
		started <- id
		<-release
		return dispatch.Outcome{}, nil
	}
	l = &Loop{Crew: map[string]contract.Crewmate{ownerOf(b): idleCrew{}}, Interval: time.Millisecond, Rev: "0123456"}
	return
}

// run starts l.Run in a goroutine, returning its error and the State sequence it
// reports (buffered, so onState never blocks the loop).
func run(l *Loop, sig <-chan os.Signal) (<-chan error, <-chan State) {
	errc, states := make(chan error, 1), make(chan State, 16)
	go func() {
		r, err := l.Run(sig, func(s State) { states <- s })
		if r != nil {
			err = fmt.Errorf("Run returned Restart %+v", r)
		}
		errc <- err
	}()
	return errc, states
}

// The first stop signal ends polling at once, reports the in-flight beads as
// draining, and Run returns only once the in-flight effort has finished (ADR 0050).
// The State sequence is on, draining [b], draining [], each carrying the Rev.
func TestStopDrainsInflight(t *testing.T) {
	evs := captureEvents(t)
	l, b, started, release, ticks := blockingLoop(t)
	sig := make(chan os.Signal, 2)
	errc, states := run(l, sig)
	if s := <-states; !reflect.DeepEqual(s, State{State: "on", Rev: l.Rev}) {
		t.Fatalf("first State = %+v; want on @%s", s, l.Rev)
	}
	<-started
	sig <- syscall.SIGTERM
	if s := <-states; !reflect.DeepEqual(s, State{State: "draining", Inflight: []bd.Bead{b}, Rev: l.Rev}) {
		t.Fatalf("State = %+v; want draining [%s] @%s", s, b.ID, l.Rev)
	}
	n := ticks.Load()
	select {
	case err := <-errc:
		t.Fatalf("Run returned %v with an effort in flight", err)
	case <-time.After(20 * time.Millisecond):
	}
	if ticks.Load() != n {
		t.Fatal("loop kept polling after the stop signal")
	}
	close(release)
	if err := <-errc; err != nil {
		t.Fatalf("drained Run = %v; want nil", err)
	}
	if s := <-states; s.State != "draining" || len(s.Inflight) != 0 || s.Rev != l.Rev {
		t.Fatalf("State after the effort finished = %+v; want draining, none in flight", s)
	}
	if len(states) != 0 {
		t.Fatalf("extra States: %+v", <-states)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"loop.draining"}) {
		t.Fatalf("events = %v; want loop.draining", got)
	}
	if a := (*evs)[0].Attrs; a["inflight"] != 1 || !reflect.DeepEqual(a["beads"], []string{b.ID}) {
		t.Fatalf("loop.draining attrs = %v; want inflight=1 beads=[%s]", a, b.ID)
	}
}

// A second stop signal while draining forces: paused:abandoned on each in-flight
// bead, then its effort.abandoned (claim not released, paused=true), then
// loop.forced, and Run returns without waiting (ADR 0050, 0082).
func TestSecondStopForces(t *testing.T) {
	evs := captureEvents(t)
	l, b, started, release, _ := blockingLoop(t)
	defer close(release)
	var writes []string
	pauseBead = func(id, cause string) error {
		writes = append(writes, fmt.Sprintf("%s %s after %v", id, cause, eventlogtest.Names(*evs)))
		return nil
	}
	sig := make(chan os.Signal, 2)
	errc, states := run(l, sig)
	id := <-started
	sig <- syscall.SIGINT
	sig <- syscall.SIGINT
	if err := <-errc; !errors.Is(err, ErrForced) {
		t.Fatalf("forced Run = %v; want ErrForced", err)
	}
	if got := []State{<-states, <-states}; !reflect.DeepEqual(got, []State{{State: "on", Rev: l.Rev}, {State: "draining", Inflight: []bd.Bead{b}, Rev: l.Rev}}) || len(states) != 0 {
		t.Fatalf("States = %+v (+%d more); want on, draining [%s]", got, len(states), b.ID)
	}
	if want := []string{b.ID + " abandoned after [loop.draining]"}; !slices.Equal(writes, want) {
		t.Fatalf("pause writes = %q; want %q (before effort.abandoned)", writes, want)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"loop.draining", "effort.abandoned", "loop.forced"}) {
		t.Fatalf("events = %v; want draining, abandoned, forced", got)
	}
	ab := (*evs)[1]
	if ab.Level != slog.LevelWarn || ab.Attrs["effort"] != id || ab.Attrs["bead"] != b.ID || ab.Attrs["crewmate"] != ownerOf(b) || ab.Attrs["paused"] != true || ab.Attrs["error"] != nil {
		t.Fatalf("effort.abandoned = %+v; want Warn effort=%s bead=%s crewmate=%s paused=true, no error", ab, id, b.ID, ownerOf(b))
	}
	if f := (*evs)[2]; f.Level != slog.LevelWarn || f.Attrs["inflight"] != 1 {
		t.Fatalf("loop.forced = %+v; want Warn inflight=1", f)
	}
}

// A failing or hung abandon pause never blocks the force: ErrForced still comes
// back, and effort.abandoned says paused=false with the error (ADR 0082 §3).
func TestForceWithFailedPause(t *testing.T) {
	oTimeout := dispatch.AbandonTimeout
	t.Cleanup(func() { dispatch.AbandonTimeout = oTimeout })
	dispatch.AbandonTimeout = 20 * time.Millisecond
	hang := make(chan struct{})
	defer close(hang)
	for name, c := range map[string]struct {
		pause func(string, string) error
		err   string
	}{
		"fails": {func(string, string) error { return errors.New("bd down") }, "bd down"},
		"hangs": {func(string, string) error { <-hang; return nil }, "pause timed out after 20ms"},
	} {
		t.Run(name, func(t *testing.T) {
			evs := captureEvents(t)
			l, _, started, release, _ := blockingLoop(t)
			defer close(release)
			pauseBead = c.pause
			sig := make(chan os.Signal, 2)
			errc, _ := run(l, sig)
			<-started
			sig <- syscall.SIGINT
			sig <- syscall.SIGINT
			if err := <-errc; !errors.Is(err, ErrForced) {
				t.Fatalf("forced Run = %v; want ErrForced", err)
			}
			if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"loop.draining", "effort.abandoned", "loop.forced"}) {
				t.Fatalf("events = %v; want draining, abandoned, forced", got)
			}
			if a := (*evs)[1].Attrs; a["paused"] != false || a["error"] != c.err {
				t.Fatalf("effort.abandoned attrs = %v; want paused=false error=%q", a, c.err)
			}
		})
	}
}

// A stop that lands mid-tick claims nothing more: the tick's remaining candidates
// stay unclaimed.
func TestStopMidTickClaimsNoMore(t *testing.T) {
	captureEvents(t)
	l, b, _, release, _ := blockingLoop(t)
	defer close(release)
	build := bd.Bead{ID: "mc-b", Kind: contract.KindBuild, Parent: "mc-k"}
	l.Crew[ownerOf(build)] = idleCrew{}
	first := true
	readyBeads = func() ([]bd.Bead, error) {
		if first {
			first = false
			return []bd.Bead{b, build}, nil
		}
		return nil, nil
	}
	sig := make(chan os.Signal, 2)
	var claimed []string
	claimBead = func(c bd.Bead) error {
		claimed = append(claimed, c.ID)
		sig <- syscall.SIGTERM            // stop…
		sig <- syscall.SIGTERM            // …and force, so Run returns without waiting on the effort
		time.Sleep(20 * time.Millisecond) // let the stop land before the next candidate
		return nil
	}
	errc, states := run(l, sig)
	if err := <-errc; !errors.Is(err, ErrForced) {
		t.Fatalf("Run = %v; want ErrForced", err)
	}
	if got := []State{<-states, <-states}; !reflect.DeepEqual(got, []State{{State: "on", Rev: l.Rev}, {State: "draining", Inflight: []bd.Bead{b}, Rev: l.Rev}}) || len(states) != 0 {
		t.Fatalf("States = %+v (+%d more); want on, draining [%s]", got, len(states), b.ID)
	}
	if !slices.Equal(claimed, []string{b.ID}) {
		t.Fatalf("claimed = %v; want only %s", claimed, b.ID)
	}
}

// holdLoop stubs the claim and effort seams so each l.dispatch(b) reports the next
// scripted cut outcome, and records the loop's pause writes.
func holdLoop(t *testing.T, cuts ...dispatch.Cut) (l *Loop, b bd.Bead, pauses *[]string) {
	t.Helper()
	oClaim, oRun, oPause := claimBead, runEffort, pauseBead
	t.Cleanup(func() { claimBead, runEffort, pauseBead = oClaim, oRun, oPause })
	b = bd.Bead{ID: "mc-r", Kind: contract.KindResearch, Parent: "mc-c"}
	claimBead = func(bd.Bead) error { return nil }
	i := 0
	runEffort = func(contract.Ctx, contract.Crewmate, bd.Bead, io.Writer, string, dispatch.InflightFunc) (dispatch.Outcome, error) {
		i++
		return dispatch.Outcome{Cut: cuts[i-1], Branch: "chart/mc-c", Holder: git.Holder{Path: "/home/u/stray"}}, nil
	}
	pauses = &[]string{}
	pauseBead = func(id, cause string) error {
		*pauses = append(*pauses, id+" "+cause)
		return nil
	}
	l = &Loop{Crew: map[string]contract.Crewmate{ownerOf(b): idleCrew{}}}
	return
}

// 3 consecutive foreign holds pause the bead once: paused:foreign-hold, and
// effort.paused at Warn {cause foreign-hold, branch, holder, skips=3}
// (ADR 0055 §2/§4). The count then restarts.
func TestThreeHoldsPause(t *testing.T) {
	evs := captureEvents(t)
	h := dispatch.CutHeld
	l, b, pauses := holdLoop(t, h, h, h, h, h)
	play(l, b, 5)
	if want := []string{"mc-r foreign-hold"}; !slices.Equal(*pauses, want) {
		t.Fatalf("pauses = %q, want %q", *pauses, want)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"effort.paused"}) {
		t.Fatalf("events = %v, want one effort.paused", got)
	}
	if e := (*evs)[0]; e.Level != slog.LevelWarn || e.Attrs["branch"] != "chart/mc-c" || e.Attrs["holder"] != "/home/u/stray" || e.Attrs["skips"] != 3 || e.Attrs["cause"] != bd.PausedForeignHold || e.Attrs["bead"] != b.ID || e.Attrs["effort"] == nil {
		t.Errorf("effort.paused = %+v", e)
	}
}

// Any non-held cut outcome (success, own contention, failed cut) resets the count,
// so only an unbroken run of 3 holds pauses (ADR 0055 §4).
func TestHoldCountResets(t *testing.T) {
	h := dispatch.CutHeld
	for _, reset := range []dispatch.Cut{dispatch.CutOK, dispatch.CutBusy, dispatch.CutFailed} {
		l, b, pauses := holdLoop(t, h, h, reset, h, h)
		play(l, b, 5)
		if len(*pauses) != 0 {
			t.Errorf("cut %v between holds: pauses = %q, want none", reset, *pauses)
		}
	}
}

// step is one scripted effort return: its cut outcome and Dispatch's error.
type step struct {
	cut      dispatch.Cut
	err      error
	manual   bool // the release re-read showed paused:manual (an abort)
	stranded bool // the integrate backstop hit a conflict
}

var (
	stepOK     = step{cut: dispatch.CutOK, err: nil}
	stepFailed = step{cut: dispatch.CutOK, err: errors.New("handler exploded")}
	stepCutErr = step{cut: dispatch.CutFailed, err: errors.New("cut failed")}
	stepBusy   = step{cut: dispatch.CutBusy, err: errors.New("unclaim failed")}
	stepHeld   = step{cut: dispatch.CutHeld, err: errors.New("unclaim failed")}
	stepAbort  = step{cut: dispatch.CutOK, err: errors.New("exit status -1"), manual: true}
	stepStrand = step{cut: dispatch.CutOK, err: nil, stranded: true}
)

// failLoop is holdLoop with each l.dispatch(b) returning the next scripted step,
// errors included.
func failLoop(t *testing.T, steps ...step) (l *Loop, b bd.Bead, pauses *[]string) {
	t.Helper()
	l, b, pauses = holdLoop(t)
	i := 0
	runEffort = func(contract.Ctx, contract.Crewmate, bd.Bead, io.Writer, string, dispatch.InflightFunc) (dispatch.Outcome, error) {
		i++
		return dispatch.Outcome{Cut: steps[i-1].cut, Branch: "chart/mc-c", Aborted: steps[i-1].manual, Stranded: steps[i-1].stranded}, steps[i-1].err
	}
	return
}

// play dispatches b n times, waiting out each effort.
func play(l *Loop, b bd.Bead, n int) {
	for range n {
		l.dispatch(b)
		l.wg.Wait()
	}
}

// 3 consecutive failed efforts — a failed cut or a CutOK effort that returned an
// error — pause the bead once: paused:failing, and effort.paused at Warn {cause
// failing, failures 3, the last error} (ADR 0078 §2). The count then restarts.
func TestThreeFailuresPause(t *testing.T) {
	evs := captureEvents(t)
	l, b, pauses := failLoop(t, stepCutErr, stepCutErr, stepFailed, stepCutErr, stepFailed)
	play(l, b, 5)
	if want := []string{"mc-r failing"}; !slices.Equal(*pauses, want) {
		t.Fatalf("pauses = %q, want %q", *pauses, want)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"effort.paused"}) {
		t.Fatalf("events = %v, want one effort.paused", got)
	}
	if e := (*evs)[0]; e.Level != slog.LevelWarn || e.Attrs["cause"] != bd.PausedFailing || e.Attrs["failures"] != 3 || e.Attrs["error"] != "handler exploded" || e.Attrs["bead"] != b.ID || e.Attrs["effort"] == nil {
		t.Errorf("effort.paused = %+v", e)
	}
}

// A clean return resets the failure count; busy and held skips (even ones whose
// unclaim errored) neither add nor reset, so a held/failed alternation pauses on
// the 3rd failure (ADR 0078 §2).
func TestFailureCount(t *testing.T) {
	for _, c := range []struct {
		name  string
		steps []step
		pause bool
	}{
		{"success resets", []step{stepFailed, stepFailed, stepOK, stepFailed, stepFailed}, false},
		{"busy skips", []step{stepFailed, stepBusy, stepFailed, stepBusy, stepBusy, stepBusy}, false},
		{"busy then fail", []step{stepFailed, stepBusy, stepFailed, stepBusy, stepFailed}, true},
		{"held alternation", []step{stepHeld, stepFailed, stepHeld, stepFailed, stepHeld, stepFailed}, true},
		{"aborts skip", []step{stepFailed, stepAbort, stepFailed, stepAbort, stepAbort}, false}, // ADR 0083 §8
		{"abort then fail", []step{stepFailed, stepAbort, stepFailed, stepFailed}, true},
		// A backstop conflict neither counts nor resets (Spec FR 43).
		{"stranded doesn't reset", []step{stepFailed, stepFailed, stepStrand, stepFailed}, true},
		{"stranded doesn't count", []step{stepStrand, stepStrand, stepStrand}, false},
	} {
		l, b, pauses := failLoop(t, c.steps...)
		play(l, b, len(c.steps))
		if got := slices.Contains(*pauses, "mc-r failing"); got != c.pause {
			t.Errorf("%s: pauses = %q, want failing pause %v", c.name, *pauses, c.pause)
		}
	}
}

// A failed pause write logs loop.tick_failed and keeps the count, so the next
// failure retries the write.
func TestFailingPauseWriteRetries(t *testing.T) {
	evs := captureEvents(t)
	l, b, pauses := failLoop(t, stepFailed, stepFailed, stepFailed, stepFailed)
	writes := 0
	pauseBead = func(id, cause string) error {
		if writes++; writes == 1 {
			return errors.New("bd down")
		}
		*pauses = append(*pauses, id+" "+cause)
		return nil
	}
	play(l, b, 4)
	if want := []string{"mc-r failing"}; writes != 2 || !slices.Equal(*pauses, want) {
		t.Fatalf("writes = %d, pauses = %q; want 2 writes, %q", writes, *pauses, want)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"loop.tick_failed", "effort.paused"}) {
		t.Fatalf("events = %v, want loop.tick_failed then effort.paused", got)
	}
	if e := (*evs)[1]; e.Attrs["failures"] != 4 {
		t.Errorf("effort.paused failures = %v, want 4", e.Attrs["failures"])
	}
}

// Each tick lifts paused:foreign-hold off a bead whose branch is no longer
// foreign-held and logs effort.resumed {cause foreign-hold, branch}; a still-held
// bead stays paused and a manual or failing pause is never looked at (ADR 0055 §5,
// ADR 0078). A bead with both labels loses only foreign-hold, staying paused by the
// other. No claim is touched.
func TestAutoClearForeignHold(t *testing.T) {
	evs := captureEvents(t)
	oReady, oIP, oClaim, oHeld, oUn := readyBeads, inProgressBeads, claimBead, foreignHeld, unpauseBead
	t.Cleanup(func() {
		readyBeads, inProgressBeads, claimBead, foreignHeld, unpauseBead = oReady, oIP, oClaim, oHeld, oUn
	})
	paused := func(id, cause string) bd.Bead {
		return bd.Bead{ID: id, Kind: contract.KindResearch, Parent: "mc-c", Labels: []string{"paused:" + cause}}
	}
	readyBeads = func() ([]bd.Bead, error) {
		both := paused("both", bd.PausedForeignHold)
		both.Labels = append(both.Labels, "paused:"+bd.PausedManual)
		fboth := paused("fboth", bd.PausedForeignHold)
		fboth.Labels = append(fboth.Labels, "paused:"+bd.PausedFailing)
		return []bd.Bead{paused("free", bd.PausedForeignHold), paused("held", bd.PausedForeignHold), paused("man", bd.PausedManual), both, paused("fail", bd.PausedFailing), fboth}, nil
	}
	inProgressBeads = func() ([]bd.Bead, error) { return nil, nil }
	claimBead = func(b bd.Bead) error { t.Errorf("claimed %s", b.ID); return nil }
	var looked, lifted []string
	foreignHeld = func(b bd.Bead, _ dispatch.InflightFunc) (string, bool, error) {
		looked = append(looked, b.ID)
		return "chart/mc-c", b.ID == "held", nil
	}
	unpauseBead = func(id, cause string) error { lifted = append(lifted, id+" "+cause); return nil }

	l := &Loop{}
	if err := l.tick(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(looked, []string{"free", "held", "both", "fboth"}) {
		t.Errorf("looked up %v, want only the foreign-hold pauses", looked)
	}
	if !slices.Equal(lifted, []string{"free foreign-hold", "both foreign-hold", "fboth foreign-hold"}) {
		t.Errorf("lifted %v, want only the freed beads' foreign-hold", lifted)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"effort.resumed", "effort.resumed", "effort.resumed"}) {
		t.Fatalf("events = %v, want three effort.resumed", got)
	}
	if e := (*evs)[0]; e.Level != slog.LevelInfo || e.Attrs["branch"] != "chart/mc-c" || e.Attrs["cause"] != bd.PausedForeignHold || e.Attrs["bead"] != "free" {
		t.Errorf("effort.resumed = %+v", e)
	}
}

// nix stubs the mainline seams for a watched pinned loop at l.Rev "r0" whose store
// dir is /nix/store/old-mugicrew: the source's main reads the returned setter's
// last value, and eval and build count their calls before running the given funcs.
func nix(t *testing.T, l *Loop, eval, build func(ctx context.Context, rev string) (string, error)) (setMain func(string), evals, builds *atomic.Int32) {
	t.Helper()
	oMain, oEval, oBuild := mainRev, evalOutPath, buildRev
	t.Cleanup(func() { mainRev, evalOutPath, buildRev = oMain, oEval, oBuild })
	l.Rev, l.Bin, l.SourceDir, l.CommonDir, l.Watch = "r0", "/nix/store/old-mugicrew/bin/mugicrew", "/source", "/common", true
	var main atomic.Value
	main.Store("r0")
	evals, builds = &atomic.Int32{}, &atomic.Int32{}
	mainRev = func(dir string) (string, error) {
		if dir != l.SourceDir {
			t.Errorf("mainRev(%q); want the loop's source dir", dir)
		}
		return main.Load().(string), nil
	}
	evalOutPath = func(ctx context.Context, _, rev string) (string, error) { evals.Add(1); return eval(ctx, rev) }
	buildRev = func(ctx context.Context, _, _, rev string) (string, error) { builds.Add(1); return build(ctx, rev) }
	return func(r string) { main.Store(r) }, evals, builds
}

// idle stubs bd so the loop ticks with nothing ready.
func idle(l *Loop) *Loop {
	readyBeads = func() ([]bd.Bead, error) { return nil, nil }
	return l
}

// buildEvents are evs' build.* events.
func buildEvents(evs []eventlogtest.Event) []eventlogtest.Event {
	var out []eventlogtest.Event
	for _, e := range evs {
		if strings.HasPrefix(e.Name, "build.") {
			out = append(out, e)
		}
	}
	return out
}

// stopAfterTicks runs l for a few ticks, then stops it.
func stopAfterTicks(t *testing.T, l *Loop) {
	t.Helper()
	sig := make(chan os.Signal, 2)
	errc, _ := run(l, sig)
	time.Sleep(20 * time.Millisecond)
	sig <- syscall.SIGTERM
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
}

// An unwatched pinned loop never checks for a new build: no mainline read, eval or
// build, however the source's main moves (Spec FR22, ADR 0090 §4).
func TestUnwatchedPinnedLoopNeverChecks(t *testing.T) {
	evs := captureEvents(t)
	l, _, _, release, _ := blockingLoop(t)
	close(release)
	setMain, evals, builds := nix(t, idle(l), newOutPath, noBuild)
	l.Watch = false
	setMain("r1")
	var reads atomic.Int32
	mainRev = func(string) (string, error) { reads.Add(1); return "r1", nil }
	stopAfterTicks(t, l)
	if reads.Load() != 0 || evals.Load() != 0 || builds.Load() != 0 || len(buildEvents(*evs)) != 0 {
		t.Fatalf("reads=%d evals=%d builds=%d build events=%v; want none", reads.Load(), evals.Load(), builds.Load(), buildEvents(*evs))
	}
}

// A watched loop follows its source repo, never the Workspace: the Workspace's
// mainline moving (a land, a fetch) triggers no eval or build (Spec FR21).
func TestWorkspaceMainlineMovingTriggersNothing(t *testing.T) {
	evs := captureEvents(t)
	l, _, _, release, _ := blockingLoop(t)
	close(release)
	_, evals, builds := nix(t, idle(l), newOutPath, noBuild)
	mainRev = func(dir string) (string, error) {
		if dir == l.CommonDir {
			return "workspace-moved", nil
		}
		return "r0", nil
	}
	stopAfterTicks(t, l)
	if evals.Load() != 0 || builds.Load() != 0 || len(buildEvents(*evs)) != 0 {
		t.Fatalf("evals=%d builds=%d build events=%v; want none", evals.Load(), builds.Load(), buildEvents(*evs))
	}
}

var noBuild = func(context.Context, string) (string, error) { return "", errors.New("unexpected build") }

// Main moved but its outPath is this image's store dir: Rev is rewritten to main,
// with no build and no event (ADR 0062 §2), and the loop doesn't re-eval.
func TestUnchangedOutPathRewritesRev(t *testing.T) {
	evs := captureEvents(t)
	l, _, _, release, _ := blockingLoop(t)
	defer close(release)
	setMain, evals, builds := nix(t, idle(l), func(context.Context, string) (string, error) { return "/nix/store/old-mugicrew", nil }, noBuild)
	setMain("r1")
	sig := make(chan os.Signal, 2)
	errc, states := run(l, sig)
	if got := []State{<-states, <-states}; !reflect.DeepEqual(got, []State{{State: "on", Rev: "r0"}, {State: "on", Rev: "r1"}}) {
		t.Fatalf("States = %+v; want on @r0, on @r1", got)
	}
	time.Sleep(20 * time.Millisecond)
	sig <- syscall.SIGTERM
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	if s := <-states; s.State != "draining" || s.Rev != "r1" {
		t.Fatalf("draining State = %+v; want @r1", s)
	}
	if evals.Load() != 1 || builds.Load() != 0 || len(buildEvents(*evs)) != 0 {
		t.Fatalf("evals=%d builds=%d build events=%v; want 1, 0, none", evals.Load(), builds.Load(), buildEvents(*evs))
	}
}

// A failed build is build.started then build.failed (rev, dur_ms, the last 4 KiB
// of nix's stderr from a line boundary), reported as FailedRev on an on loop, and
// not retried until main moves (ADR 0062 §3).
func TestFailedBuildIsStaleUntilMainMoves(t *testing.T) {
	evs := captureEvents(t)
	l, _, _, release, _ := blockingLoop(t)
	defer close(release)
	stderr := strings.Repeat("error: long nix log line\n", 400) + "error: builder failed"
	setMain, evals, builds := nix(t, idle(l),
		func(context.Context, string) (string, error) { return "/nix/store/new-mugicrew", nil },
		func(context.Context, string) (string, error) { return "", errors.New(stderr) })
	setMain("r1")
	sig := make(chan os.Signal, 2)
	errc, states := run(l, sig)
	if got := []State{<-states, <-states}; !reflect.DeepEqual(got, []State{{State: "on", Rev: "r0"}, {State: "on", Rev: "r0", FailedRev: "r1"}}) {
		t.Fatalf("States = %+v; want on @r0, on @r0 failed r1", got)
	}
	time.Sleep(20 * time.Millisecond)
	if evals.Load() != 1 || builds.Load() != 1 {
		t.Fatalf("evals=%d builds=%d; want a failed rev never retried", evals.Load(), builds.Load())
	}
	setMain("r2")
	if s := <-states; !reflect.DeepEqual(s, State{State: "on", Rev: "r0", FailedRev: "r2"}) {
		t.Fatalf("State = %+v; want on @r0 failed r2", s)
	}
	sig <- syscall.SIGTERM
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	got := buildEvents(*evs)
	if names := eventlogtest.Names(got); !slices.Equal(names, []string{"build.started", "build.failed", "build.started", "build.failed"}) {
		t.Fatalf("events = %v; want started, failed per rev", names)
	}
	if got[0].Attrs["rev"] != "r1" || got[1].Attrs["rev"] != "r1" || got[3].Attrs["rev"] != "r2" || got[1].Level != slog.LevelError {
		t.Fatalf("events = %+v; want r1, r1 (Error), …, r2", got)
	}
	msg, _ := got[1].Attrs["err"].(string)
	if len(msg) > 4<<10 || !strings.HasPrefix(msg, "error: long") || !strings.HasSuffix(msg, "builder failed") {
		t.Fatalf("build.failed err = %d bytes %q…; want ≤4 KiB tail from a line boundary", len(msg), msg[:min(40, len(msg))])
	}
	if _, ok := got[1].Attrs["dur_ms"]; !ok {
		t.Fatal("build.failed has no dur_ms")
	}
}

// A failed eval is build.failed too, with no build.started (ADR 0062 §8).
func TestFailedEvalIsBuildFailed(t *testing.T) {
	evs := captureEvents(t)
	l, _, _, release, _ := blockingLoop(t)
	defer close(release)
	setMain, _, builds := nix(t, idle(l), func(context.Context, string) (string, error) { return "", errors.New("error: flake has no mugicrew") }, noBuild)
	setMain("r1")
	sig := make(chan os.Signal, 2)
	errc, states := run(l, sig)
	if got := []State{<-states, <-states}; !reflect.DeepEqual(got, []State{{State: "on", Rev: "r0"}, {State: "on", Rev: "r0", FailedRev: "r1"}}) {
		t.Fatalf("States = %+v; want on @r0, on @r0 failed r1", got)
	}
	sig <- syscall.SIGTERM
	<-errc
	got := buildEvents(*evs)
	if len(got) != 1 || got[0].Name != "build.failed" || got[0].Attrs["rev"] != "r1" || got[0].Attrs["err"] != "error: flake has no mugicrew" || builds.Load() != 0 {
		t.Fatalf("events = %+v builds=%d; want one build.failed r1 with nix's error, no build", got, builds.Load())
	}
}

// A build still running doesn't block dispatch, and a stop cancels it (the nix
// child's ctx) without waiting for it to return (ADR 0064 §5): no build.done or
// build.failed follows.
func TestBuildInFlightKeepsDispatchingAndStopCancelsIt(t *testing.T) {
	evs := captureEvents(t)
	l, b, started, release, _ := blockingLoop(t)
	inBuild, cancelled, never := make(chan struct{}), make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(never) })
	setMain, _, _ := nix(t, l,
		func(context.Context, string) (string, error) { return "/nix/store/new-mugicrew", nil },
		func(ctx context.Context, _ string) (string, error) {
			close(inBuild)
			<-ctx.Done()
			close(cancelled)
			<-never // a nix child that never exits on its own
			return "", ctx.Err()
		})
	var once atomic.Bool
	readyBeads = func() ([]bd.Bead, error) { // ready only once the build is under way
		select {
		case <-inBuild:
			if once.CompareAndSwap(false, true) {
				return []bd.Bead{b}, nil
			}
		default:
		}
		return nil, nil
	}
	setMain("r1")
	sig := make(chan os.Signal, 2)
	errc, _ := run(l, sig)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("no effort dispatched while the build ran")
	}
	sig <- syscall.SIGTERM
	<-cancelled
	close(release)
	select {
	case err := <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run waited on the cancelled build")
	}
	if got := eventlogtest.Names(buildEvents(*evs)); !slices.Equal(got, []string{"build.started"}) {
		t.Fatalf("build events = %v; want only build.started", got)
	}
}

// tail keeps whole lines of the last n bytes, including one starting exactly at the cut.
func TestTailCutsAtLineBoundary(t *testing.T) {
	for _, c := range []struct{ s, want string }{
		{"short", "short"},
		{"aaaa\nbb\ncc", "bb\ncc"}, // last 5 = "\nbb\ncc"[1:]: "bb\ncc" starts right after a newline
		{"aaaa\nbbbcc", "bbbcc"},
		{"aaaaabbbcc", "bbbcc"}, // no newline: a raw cut
	} {
		if got := tail(c.s, 5); got != c.want {
			t.Errorf("tail(%q, 5) = %q; want %q", c.s, got, c.want)
		}
	}
}

// result is one Run's return.
type result struct {
	r   *Restart
	err error
}

// runFull is run, keeping Run's *Restart.
func runFull(l *Loop, sig <-chan os.Signal) (<-chan result, <-chan State) {
	res, states := make(chan result, 1), make(chan State, 16)
	go func() {
		r, err := l.Run(sig, func(s State) { states <- s })
		res <- result{r, err}
	}()
	return res, states
}

const newBin = "/nix/store/new-mugicrew/bin/mugicrew"

// newOutPath evaluates every rev to a store dir other than the loop's.
func newOutPath(context.Context, string) (string, error) { return "/nix/store/new-mugicrew", nil }

// Main moving mid-build still restarts onto the build already made, once: Run
// drains for it with nothing in flight and returns Restart{r1} at once, logging
// build.started, build.done, loop.draining restart_to=r1 (ADR 0062 §4–5).
func TestMainMovingMidBuildRestartsOntoFirst(t *testing.T) {
	evs := captureEvents(t)
	l, _, _, release, _ := blockingLoop(t)
	defer close(release)
	var setMain func(string)
	setMain, evals, builds := nix(t, idle(l), newOutPath, func(_ context.Context, rev string) (string, error) {
		setMain("r2") // main moves again while r1 builds
		time.Sleep(20 * time.Millisecond)
		return newBin, nil
	})
	setMain("r1")
	res, states := runFull(l, make(chan os.Signal, 2))
	var got result
	select {
	case got = <-res:
	case <-time.After(2 * time.Second):
		t.Fatal("an idle restart drain did not return")
	}
	if got.err != nil || !reflect.DeepEqual(got.r, &Restart{Rev: "r1", Bin: newBin}) {
		t.Fatalf("Run = %+v, %v; want Restart{r1 %s}", got.r, got.err, newBin)
	}
	if evals.Load() != 1 || builds.Load() != 1 {
		t.Fatalf("evals=%d builds=%d; want one of each", evals.Load(), builds.Load())
	}
	var last State
	for len(states) > 0 {
		last = <-states
	}
	if !reflect.DeepEqual(last, State{State: "draining", Inflight: []bd.Bead{}, Rev: "r0", RestartTo: "r1"}) {
		t.Fatalf("last State = %+v; want draining @r0 restart_to r1", last)
	}
	if names := eventlogtest.Names(*evs); !slices.Equal(names, []string{"build.started", "build.done", "loop.draining"}) {
		t.Fatalf("events = %v; want build.started, build.done, loop.draining", names)
	}
	if d := (*evs)[1].Attrs; d["rev"] != "r1" || d["store_path"] != "/nix/store/new-mugicrew" {
		t.Fatalf("build.done = %v; want r1 at the new store dir", d)
	}
	if a := (*evs)[2].Attrs; a["restart_to"] != "r1" || a["inflight"] != 0 {
		t.Fatalf("loop.draining = %v; want restart_to=r1 inflight=0", a)
	}
}

// restartDraining starts a pinned loop that dispatches b, then builds main r1 and
// drains for a restart onto it with b in flight; it returns once the restart
// drain is reported.
func restartDraining(t *testing.T) (l *Loop, b bd.Bead, sig chan os.Signal, res <-chan result, states <-chan State, release chan struct{}, setMain func(string), evals *atomic.Int32) {
	t.Helper()
	l, b, started, release, _ := blockingLoop(t)
	dispatched := make(chan struct{})
	setMain, evals, _ = nix(t, l, newOutPath, func(context.Context, string) (string, error) {
		<-dispatched
		return newBin, nil
	})
	setMain("r1")
	sig = make(chan os.Signal, 2)
	res, states = runFull(l, sig)
	<-started
	close(dispatched)
	want := State{State: "draining", Inflight: []bd.Bead{b}, Rev: "r0", RestartTo: "r1"}
	for s := range states {
		if reflect.DeepEqual(s, want) {
			return
		}
		if s.State != "on" {
			t.Fatalf("State = %+v; want on until %+v", s, want)
		}
	}
	return
}

// Main moving mid-drain runs no new eval, and the drain still ends in Restart{r1}
// once the in-flight effort finishes (ADR 0062 §4).
func TestMainMovingMidDrainRunsNoEval(t *testing.T) {
	captureEvents(t)
	l, _, _, res, states, release, setMain, evals := restartDraining(t)
	setMain("r2")
	time.Sleep(20 * time.Millisecond)
	if evals.Load() != 1 {
		t.Fatalf("evals = %d; want none while restart-draining", evals.Load())
	}
	close(release)
	if got := <-res; got.err != nil || got.r == nil || got.r.Rev != "r1" {
		t.Fatalf("Run = %+v, %v; want Restart{r1}", got.r, got.err)
	}
	if s := <-states; !reflect.DeepEqual(s, State{State: "draining", Inflight: []bd.Bead{}, Rev: l.Rev, RestartTo: "r1"}) {
		t.Fatalf("State = %+v; want draining, none in flight, restart_to r1", s)
	}
}

// A stop during a restart drain clears RestartTo and the drain ends as a stop's
// (nil, nil); a second stop forces (ADR 0062 §6).
func TestStopDuringRestartDrain(t *testing.T) {
	captureEvents(t)
	l, b, sig, res, states, release, _, _ := restartDraining(t)
	sig <- syscall.SIGTERM
	if s := <-states; !reflect.DeepEqual(s, State{State: "draining", Inflight: []bd.Bead{b}, Rev: l.Rev}) {
		t.Fatalf("State after a stop = %+v; want draining [%s], no restart_to", s, b.ID)
	}
	close(release)
	if got := <-res; got.r != nil || got.err != nil {
		t.Fatalf("Run = %+v, %v; want nil, nil", got.r, got.err)
	}

	captureEvents(t)
	_, _, sig, res, states, release, _, _ = restartDraining(t)
	defer close(release)
	sig <- syscall.SIGTERM
	<-states
	sig <- syscall.SIGTERM
	if got := <-res; got.r != nil || !errors.Is(got.err, ErrForced) {
		t.Fatalf("Run = %+v, %v; want ErrForced", got.r, got.err)
	}
}
