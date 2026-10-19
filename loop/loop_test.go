package loop

import (
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"jayulabs.org/mugicrew/liveness"
	"jayulabs.org/mugicrew/looplock"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"jayulabs.org/mugicrew/agentprofile"
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
		{contract.KindSurvey, false},       // hitl — the Chart's charting leaf, only a human launches it
		{contract.KindBuild, true},         // Franky's afk leaf — a candidate; the crew-lookup skip (no franky package yet) is dispatch's job, not the fence's
		{contract.KindApproval, false},     // hitl tail (Luffy) — surfaced, not looped
		{contract.KindPivot, false},        // hitl (Zoro) — a pivot is only ever settled by a human (FR26)
		{contract.KindCheckpoint, false},   // registered afk container, not a leaf
		{contract.KindLatch, true},         // Brook's in-process Jira check
		{contract.KindRollback, true},      // Jinbe's afk leaf (ADR 0047) — the loop runs it, t triggers it
		{contract.KindTask, false},         // HITL (brook) — only a human launches a Task (FR19)
		{contract.KindInitiative, false},   // HITL container (luffy) — inert to the loop (FR45)
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

// A Chart whose only child is an open Survey is not a candidate, whatever its
// body; once the Survey and every defog bead are closed, it is, and an open
// Draft undrains it (Spec D10, FR 20-21).
func TestChartWithAnOpenSurveyIsNotCandidate(t *testing.T) {
	oKids, oOpen, oLanded := openChildren, openChildrenOf, landedBase
	t.Cleanup(func() { openChildren, openChildrenOf, landedBase = oKids, oOpen, oLanded })
	openChildrenOf = func(string, contract.Kind) (int, error) { return 0, nil }
	landedBase = func(string) (string, error) { return "", git.ErrNotLanded }
	survey := bd.Bead{ID: "s1", Kind: contract.KindSurvey, Status: "open"}
	closedSurvey := bd.Bead{ID: "s1", Kind: contract.KindSurvey, Status: "closed"}
	cases := []struct {
		name string
		kids []bd.Bead
		want bool
	}{
		{"only an open Survey", []bd.Bead{survey}, false},
		{"Survey and every defog bead closed", []bd.Bead{closedSurvey, {ID: "k1", Kind: contract.KindResearch, Status: "closed"}}, true},
		{"an open Draft", []bd.Bead{closedSurvey, {ID: "k1", Kind: contract.KindResearch, Status: "deferred", Labels: []string{"drafted-by:s1"}}}, false},
	}
	for _, c := range cases {
		openChildren = func(string) (int, error) {
			n := 0
			for _, k := range c.kids {
				if k.Open() {
					n++
				}
			}
			return n, nil
		}
		for _, body := range []string{"Destination: ship it", contract.ChartPlaceholderBody} {
			chart := bd.Bead{ID: "ws-chart", Kind: contract.KindChart, Status: "open", Description: body}
			got, err := (&Loop{}).candidates([]bd.Bead{chart}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if (len(got) == 1) != c.want {
				t.Errorf("%s, body %q: candidates = %v, want candidate=%v", c.name, body, bd.IDs(got), c.want)
			}
		}
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
	oKids, oClosed, oApproved, oOpen, oLanded := openChildren, closedApprovalsOf, approvedAtHead, openChildrenOf, landedBase
	t.Cleanup(func() {
		openChildren, closedApprovalsOf, approvedAtHead, openChildrenOf, landedBase = oKids, oClosed, oApproved, oOpen, oLanded
	})
	landedBase = func(string) (string, error) { return "", git.ErrNotLanded }
	openChildren = func(string) (int, error) { return 0, nil }
	openChildrenOf = func(string, contract.Kind) (int, error) { return 0, nil }
	closedApprovalsOf = func(string, contract.Kind) ([]bd.Bead, error) {
		return []bd.Bead{{Status: "closed", CloseReason: bd.ApprovedReason}}, nil
	}
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
	landedBase = func(string) (string, error) { return "", git.ErrNotLanded }

	ready := []bd.Bead{
		{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open"},
		surgery,
	}
	if got, err := (&Loop{}).candidates(ready, nil); err != nil || len(got) != 0 {
		t.Errorf("candidates = %v, %v; want none, no error", bd.IDs(got), err)
	}
}

// An open Critique is held while its Checkpoint has an open Surgery, running or
// idle (ADR 0160); it is admitted once the Surgery closes. A Critique already in
// progress is not in the ready frontier, so it is never re-read.
func TestCandidatesHoldCritiqueWhileSurgeryOpen(t *testing.T) {
	orig := openSurgeries
	t.Cleanup(func() { openSurgeries = orig })
	critique := bd.Bead{ID: "ws-crit", Kind: contract.KindCritique, Status: "open", Parent: "ws-cp"}
	for _, status := range []string{"open", "in_progress"} {
		openSurgeries = func(string) ([]bd.Bead, error) {
			return []bd.Bead{{ID: "ws-surg", Kind: contract.KindSurgery, Status: status}}, nil
		}
		if got, err := (&Loop{}).candidates([]bd.Bead{critique}, nil); err != nil || len(got) != 0 {
			t.Errorf("%s Surgery: candidates = %v, %v; want none", status, bd.IDs(got), err)
		}
	}
	openSurgeries = func(string) ([]bd.Bead, error) { return nil, nil }
	if got, err := (&Loop{}).candidates([]bd.Bead{critique}, nil); err != nil || len(got) != 1 {
		t.Errorf("Surgery closed: candidates = %v, %v; want the Critique", bd.IDs(got), err)
	}
}

// An open Initiative is a HITL container with no effort (FR45): never a
// candidate, and an in_progress one takes no pool slot.
func TestInitiativeIsInertToTheLoop(t *testing.T) {
	initiative := bd.Bead{ID: "ws-init", Kind: contract.KindInitiative, Status: "open"}
	if got, err := (&Loop{}).candidates([]bd.Bead{initiative}, nil); err != nil || len(got) != 0 {
		t.Errorf("candidates = %v, %v; want none, no error", bd.IDs(got), err)
	}
	initiative.Status = "in_progress"
	if n := CountInflight([]bd.Bead{initiative}); n != 0 {
		t.Errorf("CountInflight = %d, want 0: an Initiative takes no pool slot", n)
	}
}

// No test reaches a live bd for the inherited-pause reads: nothing is paused and
// an unknown parent is an unpaused root. A test that cares stubs them.
func TestMain(m *testing.M) {
	StopWindow = 0 // tests send stop and force back to back; window tests set their own
	pausedBeads = func() ([]bd.Bead, error) { return nil, nil }
	showBead = func(id string) (bd.Bead, error) { return bd.Bead{ID: id}, nil }
	membersDrained = func(string) (bool, error) { return true, nil } // never read a real bd or git
	openBlockers = func(string) ([]bd.Bead, error) { return nil, nil }
	landedBase = func(string) (string, error) {
		return "", errors.New("landedBase reached unstubbed: it would read the real repo")
	}
	openBeads = func() ([]bd.Bead, error) { return nil, nil }
	// Drop bd from PATH (any dir holding an entry named bd, executable or not) so an unstubbed bd call fails here as in the nix sandbox.
	var keep []string
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		if _, err := os.Stat(filepath.Join(d, "bd")); err != nil {
			keep = append(keep, d)
		}
	}
	os.Setenv("PATH", strings.Join(keep, string(os.PathListSeparator)))
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
	closedApprovalsOf = func(string, contract.Kind) ([]bd.Bead, error) {
		return []bd.Bead{{Status: "closed", CloseReason: bd.ApprovedReason}}, nil
	}
	approvedAtHead = func(string, string) bool { return true }
	// ws-mid is in no set the tick holds, so the walk shows it.
	showBead = func(id string) (bd.Bead, error) {
		switch id {
		case "ws-mid":
			return bd.Bead{ID: id, Parent: "mc-paused"}, nil
		case "ws-ichart", "ws-icp": // member chain heads under the paused Initiative (Spec FR29)
			return bd.Bead{ID: id, Parent: "mc-init"}, nil
		}
		return bd.Bead{}, fmt.Errorf("unexpected show %s", id)
	}

	oLanded := landedBase
	t.Cleanup(func() { landedBase = oLanded })
	landedBase = func(string) (string, error) { return "", git.ErrNotLanded }
	known := []bd.Bead{
		{ID: "mc-paused", Kind: contract.KindChart, Labels: []string{"paused:manual"}},
		{ID: "mc-failing", Kind: contract.KindChart, Labels: []string{"paused:failing"}},
		{ID: "mc-init", Kind: contract.KindInitiative, Labels: []string{"paused:manual"}},
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
	ready = append(ready,
		bd.Bead{ID: "defog-under-initiative", Kind: contract.KindResearch, Description: body, Parent: "ws-ichart"},
		bd.Bead{ID: "build-under-initiative", Kind: contract.KindBuild, Description: body, Parent: "ws-icp"})
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
	claimBead = func(bd.Bead) (*liveness.Hold, error) {
		t.Error("a failed paused read must claim nothing")
		return nil, nil
	}
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
	notLanded := func(string) (string, error) { return "", git.ErrNotLanded }
	openChildrenOf = func(string, contract.Kind) (int, error) { return 0, nil }

	// Approved at head → ready.
	closedApprovalsOf = func(string, contract.Kind) ([]bd.Bead, error) {
		return []bd.Bead{{CloseReason: bd.ApprovedReason, Description: "Commit: cafef00d"}}, nil
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

// A foreign land of a Checkpoint's or Chart's trailer is a refusal, not "not landed" (ADR 0187 §4).
func TestForeignLandRefusesReadiness(t *testing.T) {
	stubCheckpointReady(t)
	foreign := &git.ForeignLanding{Trailer: "Checkpoint: ws-cp", Ours: "a", Theirs: "b", Sha: "f00d"}
	landedBase = func(string) (string, error) { return "", foreign }
	openChildrenOf = func(string, contract.Kind) (int, error) { return 0, nil }
	var fe *git.ForeignLanding
	if _, err := CheckpointReady(bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint}); !errors.As(err, &fe) {
		t.Errorf("CheckpointReady err = %v, want *ForeignLanding", err)
	}
	if _, err := ChartLandingHeld(bd.Bead{ID: "ws-ch", Kind: contract.KindChart}); !errors.As(err, &fe) {
		t.Errorf("ChartLandingHeld err = %v, want *ForeignLanding", err)
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
	landedBase = func(string) (string, error) { return "", git.ErrNotLanded }
	closedApprovalsOf = func(string, contract.Kind) ([]bd.Bead, error) {
		return []bd.Bead{{CloseReason: bd.ApprovedReason, Description: "Commit: tip"}}, nil
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
	landedBase = func(string) (string, error) { return "", git.ErrNotLanded }
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
	landedBase = func(string) (string, error) { return "", git.ErrNotLanded }
	closedApprovalsOf = func(string, contract.Kind) ([]bd.Bead, error) {
		return []bd.Bead{{CloseReason: bd.ApprovedReason, Description: "Commit: oldpin"}, {CloseReason: bd.ApprovedReason, Description: "Commit: newtip"}}, nil
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

// Offshoot-shaped blockers (ADR 0269) hold only the land; a pivot or unknown kind
// freezes the whole Checkpoint.
func TestReadmitLandOnlyHolds(t *testing.T) {
	mk := func(kinds ...contract.Kind) []bd.Bead {
		open := []bd.Bead{}
		cp := bd.Bead{ID: "cp", Kind: contract.KindCheckpoint, Status: "open", Container: true}
		for i, k := range kinds {
			id := fmt.Sprintf("b%d", i)
			open = append(open, bd.Bead{ID: id, Kind: k, Status: "open"})
			cp.BlockedBy = append(cp.BlockedBy, id)
		}
		return append(open, cp, bd.Bead{ID: "cp.1", Kind: contract.KindBuild, Status: "open", Parent: "cp"})
	}
	for _, c := range []struct {
		name  string
		kinds []contract.Kind
		want  bool
	}{
		{"signal", []contract.Kind{contract.KindSignal}, true},
		{"chart", []contract.Kind{contract.KindChart}, true},
		{"spec", []contract.Kind{contract.KindSpec}, true},
		{"task+signal", []contract.Kind{contract.KindTask, contract.KindSignal}, true},
		{"pivot", []contract.Kind{contract.KindPivot}, false},
		{"pivot+signal", []contract.Kind{contract.KindPivot, contract.KindSignal}, false},
		{"unknown", []contract.Kind{contract.Kind("mystery")}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := bd.IDs(Readmit(nil, mk(c.kinds...)))
			if c.want != reflect.DeepEqual(got, []string{"cp", "cp.1"}) || (!c.want && len(got) != 0) {
				t.Errorf("Readmit = %v, want admitted=%v", got, c.want)
			}
		})
	}
	rb := []bd.Bead{
		{ID: "sig", Kind: contract.KindSignal, Status: "open"},
		{ID: "rb", Kind: contract.KindRollback, Status: "open", BlockedBy: []string{"sig"}},
	}
	if got := Readmit(nil, rb); len(got) != 0 {
		t.Errorf("Signal-held Rollback re-admitted: %v", bd.IDs(got))
	}
}

// A Rollback held only on an open Task (a revert MR's HITL hold) is re-admitted so
// Jinbe still ticks it and sees the human's merge or close; any other blocker keeps it hidden.
func TestReadmitTaskHeldRollback(t *testing.T) {
	task := bd.Bead{ID: "ws-task", Kind: contract.KindTask, Status: "open"}
	pivot := bd.Bead{ID: "ws-pivot", Kind: contract.KindPivot, Status: "open"}
	open := []bd.Bead{
		task, pivot,
		{ID: "ws-rb", Kind: contract.KindRollback, Status: "open", BlockedBy: []string{"ws-task"}},
		{ID: "ws-rb2", Kind: contract.KindRollback, Status: "open", BlockedBy: []string{"ws-task", "ws-pivot"}},
	}
	got := Readmit([]bd.Bead{task, pivot}, open)
	if want := []string{"ws-task", "ws-pivot", "ws-rb"}; !reflect.DeepEqual(bd.IDs(got), want) {
		t.Errorf("Readmit = %v, want %v", bd.IDs(got), want)
	}
}

// A re-admitted Checkpoint's open members are loop candidates, and so is the
// drained Checkpoint itself for its Review spawn (FR 16): only the land waits.
func TestReadmittedCheckpointMembersAreCandidates(t *testing.T) {
	stubCheckpointReady(t)
	openChildrenOf = func(string, contract.Kind) (int, error) { return 0, nil }
	closedApprovalsOf = func(string, contract.Kind) ([]bd.Bead, error) { return nil, nil }
	landedBase = func(string) (string, error) { return "", git.ErrNotLanded }
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
		return []bd.Bead{{CloseReason: bd.ApprovedReason, Description: "Commit: cafef00d"}}, nil
	}
	approvedAtHead = func(string, string) bool { return true }
	landedBase = func(string) (string, error) { return "", git.ErrNotLanded }
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
		return []bd.Bead{{CloseReason: bd.ApprovedReason, Description: "Commit: cafef00d"}}, nil
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

// Candidates dispatch by (own priority, nearest Checkpoint/Chart container's
// priority), both ascending; input order breaks full ties. A bead with no
// container, an Initiative parent, or a container absent from open uses its own
// priority as the container key.
func TestByContainerPriority(t *testing.T) {
	open := []bd.Bead{
		{ID: "k0", Kind: contract.KindCheckpoint, Priority: 0},
		{ID: "k3", Kind: contract.KindCheckpoint, Priority: 3},
		{ID: "k4", Kind: contract.KindCheckpoint, Priority: 4},
		{ID: "c1", Kind: contract.KindChart, Priority: 1},
		{ID: "i0", Kind: contract.KindInitiative, Priority: 0},
	}
	mk := func(id, parent string, p int) bd.Bead {
		return bd.Bead{ID: id, Kind: contract.KindBuild, Parent: parent, Priority: p}
	}
	for _, c := range []struct {
		name  string
		cands []bd.Bead
		want  string
	}{
		{"container priority breaks a tie", []bd.Bead{mk("a", "k4", 2), mk("b", "k0", 2)}, "b,a"},
		{"own priority beats container priority", []bd.Bead{mk("a", "k0", 2), mk("b", "k3", 0)}, "b,a"},
		{"a Chart is a container", []bd.Bead{mk("a", "k4", 2), mk("b", "c1", 2)}, "b,a"},
		{"containerless uses its own priority", []bd.Bead{mk("a", "k4", 2), mk("s", "", 2), mk("b", "k0", 2)}, "b,s,a"},
		{"full ties keep input order", []bd.Bead{mk("a", "k0", 2), mk("b", "k0", 2), mk("c", "k0", 2)}, "a,b,c"},
		{"an Initiative parent is ignored", []bd.Bead{mk("a", "k4", 2), mk("b", "i0", 2)}, "b,a"},
		{"a missing container falls back to own priority", []bd.Bead{mk("a", "k4", 2), mk("b", "gone", 2)}, "b,a"},
	} {
		if got := strings.Join(bd.IDs(byContainerPriority(c.cands, open)), ","); got != c.want {
			t.Errorf("%s: order %q, want %q", c.name, got, c.want)
		}
	}
}

// With one free pool slot the tick dispatches a P2 slice of a P0 Checkpoint
// ahead of a P2 slice of a P4 Checkpoint, whatever bd ready's order.
func TestTickDispatchesHigherPriorityContainerFirst(t *testing.T) {
	oReady, oIP, oOpen, oClaim, oRun := readyBeads, inProgressBeads, openBeads, claimBead, runEffort
	t.Cleanup(func() {
		readyBeads, inProgressBeads, openBeads, claimBead, runEffort = oReady, oIP, oOpen, oClaim, oRun
	})
	k0 := bd.Bead{ID: "k0", Kind: contract.KindCheckpoint, Status: "open", Priority: 0}
	k4 := bd.Bead{ID: "k4", Kind: contract.KindCheckpoint, Status: "open", Priority: 4}
	low := bd.Bead{ID: "low", Kind: contract.KindBuild, Status: "open", Parent: "k4", Priority: 2}
	high := bd.Bead{ID: "high", Kind: contract.KindBuild, Status: "open", Parent: "k0", Priority: 2}
	var busy []bd.Bead
	for i := range Pool(".") - 1 {
		busy = append(busy, bd.Bead{ID: fmt.Sprintf("busy%d", i), Kind: contract.KindBuild, Status: "in_progress", Parent: "kx"})
	}
	readyBeads = func() ([]bd.Bead, error) { return []bd.Bead{low, high}, nil }
	openBeads = func() ([]bd.Bead, error) { return []bd.Bead{k0, k4, low, high}, nil }
	inProgressBeads = func() ([]bd.Bead, error) { return busy, nil }
	var claimed []string
	claimBead = func(b bd.Bead) (*liveness.Hold, error) {
		claimed = append(claimed, b.ID)
		return nil, errors.New("stop")
	}
	l := &Loop{Crew: map[string]contract.Crewmate{"franky": nil}}
	if err := l.tick(); err != nil {
		t.Fatal(err)
	}
	l.wg.Wait()
	if !slices.Equal(claimed, []string{"high"}) {
		t.Errorf("claimed %v, want only the P0 Checkpoint's slice", claimed)
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
	claimBead = func(bd.Bead) (*liveness.Hold, error) { return nil, nil }
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
	claimBead = func(bd.Bead) (*liveness.Hold, error) { return nil, nil }
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
	claimBead = func(c bd.Bead) (*liveness.Hold, error) {
		claimed = append(claimed, c.ID)
		sig <- syscall.SIGTERM            // stop…
		sig <- syscall.SIGTERM            // …and force, so Run returns without waiting on the effort
		time.Sleep(20 * time.Millisecond) // let the stop land before the next candidate
		return nil, nil
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
	claimBead = func(bd.Bead) (*liveness.Hold, error) { return nil, nil }
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
	limited  bool // the effort hit the Plan usage limit
}

var (
	stepOK     = step{cut: dispatch.CutOK, err: nil}
	stepFailed = step{cut: dispatch.CutOK, err: errors.New("handler exploded")}
	stepCutErr = step{cut: dispatch.CutFailed, err: errors.New("cut failed")}
	stepBusy   = step{cut: dispatch.CutBusy, err: errors.New("unclaim failed")}
	stepHeld   = step{cut: dispatch.CutHeld, err: errors.New("unclaim failed")}
	stepAbort  = step{cut: dispatch.CutOK, err: errors.New("exit status -1"), manual: true}
	stepStrand = step{cut: dispatch.CutOK, err: nil, stranded: true}
	stepLimit  = step{cut: dispatch.CutOK, err: errors.New("usage limit"), limited: true}
)

// failLoop is holdLoop with each l.dispatch(b) returning the next scripted step,
// errors included.
func failLoop(t *testing.T, steps ...step) (l *Loop, b bd.Bead, pauses *[]string) {
	t.Helper()
	l, b, pauses = holdLoop(t)
	i := 0
	runEffort = func(contract.Ctx, contract.Crewmate, bd.Bead, io.Writer, string, dispatch.InflightFunc) (dispatch.Outcome, error) {
		i++
		o := dispatch.Outcome{Cut: steps[i-1].cut, Branch: "chart/mc-c", Aborted: steps[i-1].manual, Stranded: steps[i-1].stranded}
		if steps[i-1].limited {
			o.Limit = &contract.Limit{Source: "fallback"}
		}
		return o, steps[i-1].err
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

// A latch whose Jira read keeps failing is paused like any bead (Spec mc-8ymw.9 D5).
func TestThreeFailedLatchChecksPause(t *testing.T) {
	l, b, pauses := failLoop(t, stepCutErr, stepCutErr, stepFailed)
	b.Kind = contract.KindLatch
	l.Crew = map[string]contract.Crewmate{ownerOf(b): idleCrew{}}
	play(l, b, 3)
	if want := []string{b.ID + " failing"}; !slices.Equal(*pauses, want) {
		t.Fatalf("pauses = %q, want %q", *pauses, want)
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
		// A limited effort neither counts nor resets (Spec FR 11).
		{"limited doesn't count", []step{stepLimit, stepLimit, stepLimit}, false},
		{"limited doesn't reset", []step{stepFailed, stepFailed, stepLimit, stepFailed}, true},
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
	claimBead = func(b bd.Bead) (*liveness.Hold, error) { t.Errorf("claimed %s", b.ID); return nil, nil }
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

// profiledCtx is a dispatch.Profiled Ctx whose kinds in bad resolve unrunnable
// (kind → reason) on the "deepseek" profile, every other kind runnable.
type profiledCtx struct {
	bad map[contract.Kind]string
	err error // returned instead of the bad reason, when set
}

func (profiledCtx) Harness() contract.Harness { return nil }
func (c profiledCtx) ForKind(k contract.Kind) (contract.Ctx, string, error) {
	if r, ok := c.bad[k]; ok {
		if c.err != nil {
			return nil, "deepseek", c.err
		}
		return nil, "deepseek", errors.New(r)
	}
	return c, "deepseek", nil
}

// A refusal that carries its file logs it as profile.unusable's path attr.
func TestAdmitLogsPathAttr(t *testing.T) {
	evs := captureEvents(t)
	b := bd.Bead{ID: "mc-r", Kind: contract.KindResearch}
	ctx := profiledCtx{bad: map[contract.Kind]string{b.Kind: "x"}}
	ctx.err = &agentprofile.FileError{Path: "/w/c.toml", Err: errors.New("bad (from c.toml)")}
	if _, err := dispatch.Admit(ctx, b, func(string, string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if e := (*evs)[0]; e.Attrs["path"] != "/w/c.toml" || e.Attrs["reason"] != "bad (from c.toml)" {
		t.Errorf("profile.unusable = %+v", e)
	}
}

// An unrunnable Agent Profile pauses the bead paused:profile before any claim or
// effort (ADR 0123 §3, §6): profile.unusable at Warn {bead, kind, profile,
// reason, launch afk}, then effort.paused {cause profile}, and no effort.failed.
func TestUnrunnableProfilePausesBeforeClaim(t *testing.T) {
	evs := captureEvents(t)
	oClaim, oRun, oPause := claimBead, runEffort, pauseBead
	t.Cleanup(func() { claimBead, runEffort, pauseBead = oClaim, oRun, oPause })
	b := bd.Bead{ID: "mc-r", Kind: contract.KindResearch, Parent: "mc-c"}
	claimBead = func(bd.Bead) (*liveness.Hold, error) { t.Error("claimed an unrunnable bead"); return nil, nil }
	runEffort = func(contract.Ctx, contract.Crewmate, bd.Bead, io.Writer, string, dispatch.InflightFunc) (dispatch.Outcome, error) {
		t.Error("ran an effort on an unrunnable profile")
		return dispatch.Outcome{}, nil
	}
	var pauses []string
	pauseBead = func(id, cause string) error { pauses = append(pauses, id+" "+cause); return nil }
	l := &Loop{Ctx: profiledCtx{bad: map[contract.Kind]string{b.Kind: "not declared"}}, Crew: map[string]contract.Crewmate{ownerOf(b): idleCrew{}}}
	l.dispatch(b)
	l.wg.Wait()
	if !slices.Equal(pauses, []string{"mc-r profile"}) {
		t.Fatalf("pauses = %q, want mc-r profile", pauses)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"profile.unusable", "effort.paused"}) {
		t.Fatalf("events = %v, want profile.unusable then effort.paused", got)
	}
	if e := (*evs)[0]; e.Level != slog.LevelWarn || e.Attrs["bead"] != "mc-r" || e.Attrs["kind"] != string(b.Kind) || e.Attrs["profile"] != "deepseek" || e.Attrs["reason"] != "not declared" || e.Attrs["launch"] != "afk" {
		t.Errorf("profile.unusable = %+v", e)
	} else if _, ok := e.Attrs["path"]; ok {
		t.Errorf("undeclared profile.unusable carries path %v", e.Attrs["path"])
	}
	if e := (*evs)[1]; e.Level != slog.LevelWarn || e.Attrs["cause"] != bd.PausedProfile || e.Attrs["bead"] != "mc-r" {
		t.Errorf("effort.paused = %+v", e)
	}
}

// Each tick lifts paused:profile off a ready bead whose kind resolves runnable
// again, logging effort.resumed {cause profile} (ADR 0123 §3); a still-unrunnable
// kind stays paused, and manual or failing pauses are never touched. A bead
// carrying profile and manual loses only profile.
func TestAutoClearProfile(t *testing.T) {
	evs := captureEvents(t)
	oReady, oIP, oClaim, oUn := readyBeads, inProgressBeads, claimBead, unpauseBead
	t.Cleanup(func() { readyBeads, inProgressBeads, claimBead, unpauseBead = oReady, oIP, oClaim, oUn })
	paused := func(id string, k contract.Kind, causes ...string) bd.Bead {
		b := bd.Bead{ID: id, Kind: k, Parent: "mc-c"}
		for _, c := range causes {
			b.Labels = append(b.Labels, "paused:"+c)
		}
		return b
	}
	r, s := contract.KindResearch, contract.KindSpec
	readyBeads = func() ([]bd.Bead, error) {
		return []bd.Bead{
			paused("fixed", r, bd.PausedProfile),
			paused("broken", s, bd.PausedProfile),
			paused("man", r, bd.PausedManual),
			paused("fail", r, bd.PausedFailing),
			paused("both", r, bd.PausedProfile, bd.PausedManual),
		}, nil
	}
	inProgressBeads = func() ([]bd.Bead, error) { return nil, nil }
	claimBead = func(b bd.Bead) (*liveness.Hold, error) { t.Errorf("claimed %s", b.ID); return nil, nil }
	var lifted []string
	unpauseBead = func(id, cause string) error { lifted = append(lifted, id+" "+cause); return nil }

	l := &Loop{Ctx: profiledCtx{bad: map[contract.Kind]string{s: "claude not on PATH"}}}
	if err := l.tick(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(lifted, []string{"fixed profile", "both profile"}) {
		t.Errorf("lifted %v, want only the fixed kinds' profile pause", lifted)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"effort.resumed", "effort.resumed"}) {
		t.Fatalf("events = %v, want two effort.resumed", got)
	}
	if e := (*evs)[0]; e.Level != slog.LevelInfo || e.Attrs["cause"] != bd.PausedProfile || e.Attrs["bead"] != "fixed" {
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

// stubStragglers restores the tick's listing, show, close and claim seams after
// the test and lists open as the tick's open listing (and ready as its ready).
func stubStragglers(t *testing.T, ready, open []bd.Bead) {
	t.Helper()
	oReady, oIP, oOpen, oShow, oClose, oClaim := readyBeads, inProgressBeads, openBeads, showBead, forceCloseBeads, claimBead
	oGate, oSup := gateTargets, closeBead
	t.Cleanup(func() {
		readyBeads, inProgressBeads, openBeads, showBead, forceCloseBeads, claimBead = oReady, oIP, oOpen, oShow, oClose, oClaim
		gateTargets, closeBead = oGate, oSup
	})
	readyBeads = func() ([]bd.Bead, error) { return ready, nil }
	inProgressBeads = func() ([]bd.Bead, error) { return nil, nil }
	openBeads = func() ([]bd.Bead, error) { return open, nil }
}

// Every listed child of an obsolete:-closed Checkpoint (open Critique, Approval,
// deferred Draft) is force-closed with the Checkpoint's reason before any
// dispatch, as one sweep.straggler at Warn (Spec FR11/12/15, D4).
func TestTickRetiresStragglersOfObsoleteCheckpoint(t *testing.T) {
	critique := bd.Bead{ID: "ws-crit", Kind: contract.KindCritique, Status: "open", Parent: "ws-cp"}
	approval := bd.Bead{ID: "ws-appr", Kind: contract.KindApproval, Status: "open", Parent: "ws-cp"}
	draft := bd.Bead{ID: "ws-draft", Kind: contract.KindCritique, Status: "deferred", Parent: "ws-cp"}
	stubStragglers(t, []bd.Bead{critique, approval}, []bd.Bead{critique, approval, draft})
	evs := captureEvents(t)
	reason := bd.ObsoleteReason + " Spec ws-s superseded"
	showBead = func(id string) (bd.Bead, error) {
		return bd.Bead{ID: id, Kind: contract.KindCheckpoint, Status: "closed", CloseReason: reason}, nil
	}
	var closed []string
	var gotReason string
	forceCloseBeads = func(ids []string, r string) error { closed, gotReason = ids, r; return nil }
	claimBead = func(b bd.Bead) (*liveness.Hold, error) {
		t.Errorf("claimed straggler %s", b.ID)
		return nil, errors.New("no")
	}
	crew := map[string]contract.Crewmate{ownerOf(critique): idleCrew{}, ownerOf(approval): idleCrew{}}

	if err := (&Loop{Crew: crew}).tick(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"ws-crit", "ws-appr", "ws-draft"}; !reflect.DeepEqual(closed, want) || gotReason != reason {
		t.Errorf("force-closed %v with %q, want %v with %q", closed, gotReason, want, reason)
	}
	var hits []eventlogtest.Event
	for _, e := range *evs {
		if e.Name == "sweep.straggler" {
			hits = append(hits, e)
		}
	}
	if len(hits) != 1 || hits[0].Level != slog.LevelWarn || hits[0].Attrs["checkpoint"] != "ws-cp" {
		t.Errorf("events = %+v, want one Warn sweep.straggler for ws-cp", *evs)
	}
}

// A child of a land-closed Checkpoint is left alone (Spec FR17).
func TestTickLeavesChildOfLandedCheckpoint(t *testing.T) {
	kid := bd.Bead{ID: "ws-crit", Kind: contract.KindCritique, Status: "deferred", Parent: "ws-cp"}
	stubStragglers(t, nil, []bd.Bead{kid})
	showBead = func(id string) (bd.Bead, error) {
		return bd.Bead{ID: id, Kind: contract.KindCheckpoint, Status: "closed", CloseReason: "landed"}, nil
	}
	forceCloseBeads = func(ids []string, _ string) error { t.Errorf("force-closed %v", ids); return nil }
	if err := (&Loop{}).tick(); err != nil {
		t.Fatal(err)
	}
}

// A quiet tick (every parent listed) makes no extra bd read (Spec FR14).
func TestTickQuietMakesNoStragglerReads(t *testing.T) {
	cp := bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "in_progress"}
	kid := bd.Bead{ID: "ws-crit", Kind: contract.KindCritique, Status: "deferred", Parent: "ws-cp"}
	stubStragglers(t, nil, []bd.Bead{cp, kid})
	showBead = func(id string) (bd.Bead, error) { t.Errorf("read %s", id); return bd.Bead{}, nil }
	forceCloseBeads = func(ids []string, _ string) error { t.Errorf("force-closed %v", ids); return nil }
	if err := (&Loop{}).tick(); err != nil {
		t.Fatal(err)
	}
}

// A backstop bd error fails the tick and dispatches nothing (Spec FR16).
func TestTickFailsOnStragglerError(t *testing.T) {
	r := bd.Bead{ID: "mc-r", Kind: contract.KindResearch}
	kid := bd.Bead{ID: "ws-crit", Kind: contract.KindCritique, Status: "deferred", Parent: "ws-cp"}
	stubStragglers(t, []bd.Bead{r}, []bd.Bead{r, kid})
	boom := errors.New("bd close: boom")
	showBead = func(id string) (bd.Bead, error) {
		return bd.Bead{ID: id, Status: "closed", CloseReason: bd.ObsoleteReason + " x"}, nil
	}
	forceCloseBeads = func([]string, string) error { return boom }
	claimBead = func(b bd.Bead) (*liveness.Hold, error) {
		t.Errorf("claimed %s past a failed backstop", b.ID)
		return nil, errors.New("no")
	}
	if err := (&Loop{Crew: map[string]contract.Crewmate{ownerOf(r): idleCrew{}}}).tick(); !errors.Is(err, boom) {
		t.Errorf("tick = %v, want the backstop error", err)
	}
}

// A live pivot whose only gate target is closed is closed superseded:, naming
// the Checkpoint, with a Warn sweep.straggler (Spec FR13/15, D4).
func TestTickSupersedesPivotGatingClosedCheckpoint(t *testing.T) {
	pivot := bd.Bead{ID: "ws-pv", Kind: contract.KindPivot, Status: "open"}
	stubStragglers(t, []bd.Bead{pivot}, []bd.Bead{pivot})
	evs := captureEvents(t)
	gateTargets = func(id string) ([]bd.Bead, error) {
		return []bd.Bead{{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "closed"}}, nil
	}
	var closed, reason string
	closeBead = func(id, r string) error { closed, reason = id, r; return nil }
	claimBead = func(b bd.Bead) (*liveness.Hold, error) {
		t.Errorf("claimed straggler %s", b.ID)
		return nil, errors.New("no")
	}
	if err := (&Loop{Crew: map[string]contract.Crewmate{ownerOf(pivot): idleCrew{}}}).tick(); err != nil {
		t.Fatal(err)
	}
	if closed != "ws-pv" || !strings.HasPrefix(reason, "superseded:") || !strings.Contains(reason, "ws-cp") {
		t.Errorf("closed %q with %q, want ws-pv superseded: naming ws-cp", closed, reason)
	}
	var hits int
	for _, e := range *evs {
		if e.Name == "sweep.straggler" && e.Level == slog.LevelWarn && e.Attrs["checkpoint"] == "ws-cp" {
			hits++
		}
	}
	if hits != 1 {
		t.Errorf("events = %+v, want one Warn sweep.straggler for ws-cp", *evs)
	}
}

// An orphan pivot (no gate target) is left alone (Spec D6).
func TestTickLeavesOrphanPivot(t *testing.T) {
	pivot := bd.Bead{ID: "ws-pv", Kind: contract.KindPivot, Status: "open"}
	stubStragglers(t, nil, []bd.Bead{pivot})
	gateTargets = func(string) ([]bd.Bead, error) { return nil, nil }
	closeBead = func(id, _ string) error { t.Errorf("closed orphan %s", id); return nil }
	if err := (&Loop{}).tick(); err != nil {
		t.Fatal(err)
	}
}

// A pivot still gating a listed open bead triggers no gate-target read (FR14).
func TestTickLiveGatedPivotMakesNoTargetRead(t *testing.T) {
	pivot := bd.Bead{ID: "ws-pv", Kind: contract.KindPivot, Status: "open"}
	cp := bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open", BlockedBy: []string{"ws-pv"}}
	stubStragglers(t, nil, []bd.Bead{cp, pivot})
	gateTargets = func(id string) ([]bd.Bead, error) { t.Errorf("read gate targets of %s", id); return nil, nil }
	closeBead = func(id, _ string) error { t.Errorf("closed %s", id); return nil }
	if err := (&Loop{}).tick(); err != nil {
		t.Fatal(err)
	}
}

// A bd error reading gate targets fails the tick and dispatches nothing (FR16).
func TestTickFailsOnGateTargetError(t *testing.T) {
	r := bd.Bead{ID: "mc-r", Kind: contract.KindResearch}
	pivot := bd.Bead{ID: "ws-pv", Kind: contract.KindPivot, Status: "open"}
	stubStragglers(t, []bd.Bead{r}, []bd.Bead{r, pivot})
	boom := errors.New("bd dep list: boom")
	gateTargets = func(string) ([]bd.Bead, error) { return nil, boom }
	claimBead = func(b bd.Bead) (*liveness.Hold, error) {
		t.Errorf("claimed %s past a failed backstop", b.ID)
		return nil, errors.New("no")
	}
	if err := (&Loop{Crew: map[string]contract.Crewmate{ownerOf(r): idleCrew{}}}).tick(); !errors.Is(err, boom) {
		t.Errorf("tick = %v, want the backstop error", err)
	}
}

// An open Landing holds its approved Checkpoint so it isn't re-dispatched every
// tick, but the trailer on mainline still readies it for the roll-forward backstop.
func TestCheckpointLandHoldsOnOpenLanding(t *testing.T) {
	stubCheckpointReady(t)
	openChildrenOf = func(_ string, k contract.Kind) (int, error) {
		if k == contract.KindLanding {
			return 1, nil
		}
		return 0, nil
	}
	closedApprovalsOf = func(string, contract.Kind) ([]bd.Bead, error) {
		return []bd.Bead{{CloseReason: bd.ApprovedReason, Description: "Commit: cafef00d"}}, nil
	}
	approvedAtHead = func(string, string) bool { return true }
	landedBase = func(string) (string, error) { return "", git.ErrNotLanded }
	openBlockers = func(string) ([]bd.Bead, error) { return nil, nil }
	cp := bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open"}

	if ready, err := CheckpointReady(cp); err != nil || ready {
		t.Errorf("open Landing: CheckpointReady = %v, %v; want held", ready, err)
	}
	landedBase = func(string) (string, error) { return "landsha", nil }
	if ready, err := CheckpointReady(cp); err != nil || !ready {
		t.Errorf("trailer on mainline: CheckpointReady = %v, %v; want ready", ready, err)
	}
}

// A drained Chart with an open Landing and no trailer on mainline is not a
// candidate; with the trailer on mainline it stays one, so its reconcile backstops
// a stranded Landing.
func TestChartCandidacyHoldsOnOpenLanding(t *testing.T) {
	oKids, oOpen, oLanded := openChildren, openChildrenOf, landedBase
	t.Cleanup(func() { openChildren, openChildrenOf, landedBase = oKids, oOpen, oLanded })
	openChildren = func(string) (int, error) { return 0, nil }
	openChildrenOf = func(_ string, k contract.Kind) (int, error) {
		if k == contract.KindLanding {
			return 1, nil
		}
		return 0, nil
	}
	landedBase = func(string) (string, error) { return "", git.ErrNotLanded }
	chart := bd.Bead{ID: "ws-chart", Kind: contract.KindChart, Status: "open", Description: "Destination: ship it"}

	if got, err := (&Loop{}).candidates([]bd.Bead{chart}, nil); err != nil || len(got) != 0 {
		t.Errorf("open Landing: candidates = %v, %v; want held", got, err)
	}
	landedBase = func(string) (string, error) { return "landsha", nil }
	if got, err := (&Loop{}).candidates([]bd.Bead{chart}, nil); err != nil || len(got) != 1 {
		t.Errorf("trailer on mainline: candidates = %v, %v; want ready", got, err)
	}
}

// The loop holds the effort's lock from claim until its goroutine is done: the
// bead reads alive while the effort runs and dead once the loop has drained (ADR 0154).
func TestDispatchHoldsEffortLockUntilGoroutineEnds(t *testing.T) {
	t.Setenv("BEADS_DIR", t.TempDir())
	l, b, started, release, _ := blockingLoop(t)
	claimBead = func(b bd.Bead) (*liveness.Hold, error) { return liveness.Acquire(b.ID) }
	sig := make(chan os.Signal, 2)
	errc, _ := run(l, sig)
	<-started
	if !liveness.Held(b.ID) {
		t.Error("claimed bead reads dead while its effort runs")
	}
	sig <- syscall.SIGTERM
	close(release)
	if err := <-errc; err != nil {
		t.Fatalf("Run = %v", err)
	}
	if liveness.Held(b.ID) {
		t.Error("lock still held after the effort's goroutine ended")
	}
}

// An undeclared context: label holds its container with paused:context, whose
// children are then skipped; each of the three fixes (relabel, unlabel, declare)
// lifts the hold next tick, logging effort.resumed {cause context} (ADR 0158).
func TestPauseContexts(t *testing.T) {
	evs := captureEvents(t)
	oDecl, oPause, oUn := declaredContexts, pauseBead, unpauseBead
	t.Cleanup(func() { declaredContexts, pauseBead, unpauseBead = oDecl, oPause, oUn })
	var reads int
	var declared map[string]string
	declaredContexts = func(string) map[string]string { reads++; return declared }
	var acts []string
	pauseBead = func(id, cause string) error { acts = append(acts, "pause "+id+" "+cause); return nil }
	unpauseBead = func(id, cause string) error { acts = append(acts, "unpause "+id+" "+cause); return nil }
	ctxp := func(id, label string, paused bool) bd.Bead {
		b := bd.Bead{ID: id, Status: "open", Labels: []string{"kind:checkpoint"}}
		if label != "" {
			b.Labels = append(b.Labels, "context:"+label)
		}
		if paused {
			b.Labels = append(b.Labels, "paused:context")
		}
		return b
	}

	// No context: label anywhere: no config read, no write.
	pauseContexts([]bd.Bead{ctxp("plain", "", false)})
	if reads != 0 || len(acts) != 0 {
		t.Fatalf("no-label tick: reads=%d acts=%v", reads, acts)
	}

	// Undeclared label pauses once (a held duplicate in two lists is not re-paused).
	declared = map[string]string{"app": "apps/x"}
	held := ctxp("held", "gone", true)
	pauseContexts([]bd.Bead{ctxp("bad", "gone", false), ctxp("ok", "app", false), held}, []bd.Bead{held})
	if !slices.Equal(acts, []string{"pause bad context"}) || reads != 1 {
		t.Fatalf("undeclared: acts=%v reads=%d", acts, reads)
	}

	// The three fixes each lift the hold.
	acts = nil
	pauseContexts([]bd.Bead{ctxp("relabelled", "app", true), ctxp("unlabelled", "", true)})
	declared = map[string]string{"gone": "apps/y"}
	pauseContexts([]bd.Bead{ctxp("declared", "gone", true)})
	if want := []string{"unpause relabelled context", "unpause unlabelled context", "unpause declared context"}; !slices.Equal(acts, want) {
		t.Errorf("lifted %v, want %v", acts, want)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"effort.resumed", "effort.resumed", "effort.resumed"}) {
		t.Fatalf("events = %v", got)
	}
	if e := (*evs)[0]; e.Attrs["cause"] != bd.PausedContext || e.Attrs["bead"] != "relabelled" {
		t.Errorf("effort.resumed = %+v", e)
	}
}

// A context-paused container cascades to its children like a manual pause.
func TestCandidatesSkipContextPausedChildren(t *testing.T) {
	known := []bd.Bead{{ID: "cp", Kind: contract.KindCheckpoint, Labels: []string{"paused:context"}}}
	ready := []bd.Bead{{ID: "kid", Kind: contract.KindResearch, Description: "Destination: x", Parent: "cp"}}
	got, err := (&Loop{}).candidates(ready, known)
	if err != nil || len(got) != 0 {
		t.Errorf("candidates = %v, %v; want none under a context-paused container", bd.IDs(got), err)
	}
}

// The tick that first holds an undeclared-context container makes the hold
// effective at once: neither the container nor its AFK child is claimed (FR 30).
func TestTickContextHoldIsEffectiveSameTick(t *testing.T) {
	oReady, oIP, oOpen, oClaim, oDecl, oPause := readyBeads, inProgressBeads, openBeads, claimBead, declaredContexts, pauseBead
	t.Cleanup(func() {
		readyBeads, inProgressBeads, openBeads, claimBead, declaredContexts, pauseBead = oReady, oIP, oOpen, oClaim, oDecl, oPause
	})
	cp := bd.Bead{ID: "cp", Kind: contract.KindCheckpoint, Status: "open", Labels: []string{"context:gone"}}
	kid := bd.Bead{ID: "kid", Kind: contract.KindResearch, Status: "open", Description: "Destination: x", Parent: "cp"}
	ready := []bd.Bead{cp, kid}
	readyBeads = func() ([]bd.Bead, error) { return slices.Clone(ready), nil }
	openBeads = func() ([]bd.Bead, error) { return slices.Clone(ready), nil }
	inProgressBeads = func() ([]bd.Bead, error) { return nil, nil }
	declaredContexts = func(string) map[string]string { return map[string]string{} }
	var paused []string
	pauseBead = func(id, cause string) error { paused = append(paused, id+" "+cause); return nil }
	claimBead = func(b bd.Bead) (*liveness.Hold, error) { t.Errorf("claimed %s", b.ID); return nil, nil }

	if err := (&Loop{}).tick(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(paused, []string{"cp context"}) {
		t.Errorf("paused %v, want the container held", paused)
	}
}

// A limited effort holds claude dispatch until its reset + 1m, with every
// housekeeping step still running; a later reset extends the hold; past the hold
// the loop logs loop.resumed {cause limit}, writes on and dispatches in that same
// tick (Spec D5).
func TestLimitedEffortHoldsDispatchUntilReset(t *testing.T) {
	evs := captureEvents(t)
	oReady, oIP, oClaim, oRun, oUn, oHeld, oNow := readyBeads, inProgressBeads, claimBead, runEffort, unpauseBead, foreignHeld, now
	t.Cleanup(func() {
		readyBeads, inProgressBeads, claimBead, runEffort, unpauseBead, foreignHeld, now = oReady, oIP, oClaim, oRun, oUn, oHeld, oNow
	})
	t0 := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	clock := t0
	now = func() time.Time { return clock }
	freed := bd.Bead{ID: "free", Kind: contract.KindChartConflict, Parent: "mc-c", Labels: []string{"paused:" + bd.PausedForeignHold}}
	work := bd.Bead{ID: "r", Kind: contract.KindChartConflict, Parent: "mc-c"}
	readyBeads = func() ([]bd.Bead, error) { return []bd.Bead{freed, work}, nil }
	inProgressBeads = func() ([]bd.Bead, error) { return nil, nil }
	claimBead = func(bd.Bead) (*liveness.Hold, error) { return nil, nil }
	foreignHeld = func(bd.Bead, dispatch.InflightFunc) (string, bool, error) { return "chart/mc-c", false, nil }
	var lifted []string
	unpauseBead = func(id, _ string) error { lifted = append(lifted, id); return nil }
	runs := 0
	var limit *contract.Limit
	runEffort = func(contract.Ctx, contract.Crewmate, bd.Bead, io.Writer, string, dispatch.InflightFunc) (dispatch.Outcome, error) {
		runs++
		if limit == nil {
			return dispatch.Outcome{Cut: dispatch.CutOK}, nil
		}
		return dispatch.Outcome{Cut: dispatch.CutOK, Limit: limit}, errors.New("usage limit")
	}
	var states []State
	l := &Loop{Crew: map[string]contract.Crewmate{"nami": nil}, onState: func(s State) { states = append(states, s) }}
	tick := func(at time.Duration) {
		t.Helper()
		clock = t0.Add(at)
		if err := l.tick(); err != nil {
			t.Fatal(err)
		}
		l.wg.Wait()
	}

	limit = &contract.Limit{ResetsAt: t0.Add(10 * time.Minute), Source: "message"}
	tick(0)
	if last := states[len(states)-1]; runs != 1 || last.State != looplock.Limited || !last.ResetsAt.Equal(limit.ResetsAt) || last.LimitSource != "message" {
		t.Fatalf("runs = %d, last state = %+v; want one run and a limited State at the reset", runs, last)
	}
	lifted = nil
	tick(5 * time.Minute)
	if runs != 1 || !slices.Equal(lifted, []string{"free"}) {
		t.Fatalf("held tick: runs = %d, lifted = %v; want no dispatch with housekeeping still running", runs, lifted)
	}

	// A later limited effort (still in flight when the first hit) extends the hold.
	limit = &contract.Limit{ResetsAt: t0.Add(30 * time.Minute), Source: "usage"}
	clock = t0.Add(6 * time.Minute)
	play(l, work, 1)
	if last := states[len(states)-1]; last.State != looplock.Limited || !last.ResetsAt.Equal(limit.ResetsAt) || last.LimitSource != "usage" {
		t.Fatalf("last state = %+v; want the hold at the later reset", last)
	}
	tick(20 * time.Minute)
	tick(30*time.Minute + 30*time.Second)
	if runs != 2 {
		t.Fatalf("runs = %d; want no dispatch before the later reset + 1m", runs)
	}

	limit = nil
	*evs = (*evs)[:0]
	tick(31*time.Minute + time.Second)
	if runs != 3 {
		t.Fatalf("runs = %d; want dispatch in the resuming tick", runs)
	}
	if last := states[len(states)-1]; last.State != looplock.On {
		t.Errorf("last state = %+v; want on", last)
	}
	var resumed []eventlogtest.Event
	for _, e := range *evs {
		if e.Name == "loop.resumed" {
			resumed = append(resumed, e)
		}
	}
	if len(resumed) != 1 || resumed[0].Level != slog.LevelInfo || resumed[0].Attrs["cause"] != "limit" {
		t.Errorf("loop.resumed events = %+v; want one {cause limit}", resumed)
	}
}

func TestLimitedHoldDispatchesOnlyMechanicalCandidates(t *testing.T) {
	oReady, oIP, oClaim, oRun, oNow := readyBeads, inProgressBeads, claimBead, runEffort, now
	t.Cleanup(func() { readyBeads, inProgressBeads, claimBead, runEffort, now = oReady, oIP, oClaim, oRun, oNow })
	t0 := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return t0 }
	landing := bd.Bead{ID: "land", Kind: contract.KindLanding, Parent: "mc-c"}
	build := bd.Bead{ID: "build", Kind: contract.KindBuild, Parent: "mc-c"}
	readyBeads = func() ([]bd.Bead, error) { return []bd.Bead{landing, build}, nil }
	inProgressBeads = func() ([]bd.Bead, error) { return nil, nil }
	claimBead = func(bd.Bead) (*liveness.Hold, error) { return nil, nil }
	var ran []string
	runEffort = func(_ contract.Ctx, _ contract.Crewmate, b bd.Bead, _ io.Writer, _ string, _ dispatch.InflightFunc) (dispatch.Outcome, error) {
		ran = append(ran, b.ID)
		return dispatch.Outcome{Cut: dispatch.CutOK}, nil
	}
	l := &Loop{Crew: map[string]contract.Crewmate{"nami": nil, "jinbe": nil, "franky": nil}}
	l.mu.Lock()
	l.holdLocked(contract.Limit{ResetsAt: t0.Add(10 * time.Minute), Source: "message"})
	l.mu.Unlock()
	if err := l.tick(); err != nil {
		t.Fatal(err)
	}
	l.wg.Wait()
	if !slices.Equal(ran, []string{"land"}) {
		t.Fatalf("ran = %v; want only the Mechanical landing during the hold", ran)
	}
}

// An early resume dispatches at most one pool of limited efforts: with more
// candidates than the pool, the first limited return re-holds and later ticks
// dispatch no claude candidates until the new reset + 1m (Spec FR 19).
func TestEarlyResumeCostsAtMostOnePoolOfLimitedEfforts(t *testing.T) {
	oReady, oIP, oClaim, oRun, oNow := readyBeads, inProgressBeads, claimBead, runEffort, now
	t.Cleanup(func() { readyBeads, inProgressBeads, claimBead, runEffort, now = oReady, oIP, oClaim, oRun, oNow })
	t0 := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	clock := t0
	now = func() time.Time { return clock }
	var cands []bd.Bead
	for i := range Pool(".") + 2 {
		cands = append(cands, bd.Bead{ID: fmt.Sprintf("r%d", i), Kind: contract.KindChartConflict, Parent: fmt.Sprintf("mc-c%d", i)})
	}
	readyBeads = func() ([]bd.Bead, error) { return slices.Clone(cands), nil }
	inProgressBeads = func() ([]bd.Bead, error) { return nil, nil }
	claimBead = func(bd.Bead) (*liveness.Hold, error) { return nil, nil }
	var mu sync.Mutex
	runs := 0
	var limit contract.Limit
	runEffort = func(contract.Ctx, contract.Crewmate, bd.Bead, io.Writer, string, dispatch.InflightFunc) (dispatch.Outcome, error) {
		mu.Lock()
		defer mu.Unlock()
		runs++
		return dispatch.Outcome{Cut: dispatch.CutOK, Limit: &limit}, errors.New("usage limit")
	}
	l := &Loop{Crew: map[string]contract.Crewmate{"nami": nil}}
	tick := func(at time.Duration) {
		t.Helper()
		clock = t0.Add(at)
		if err := l.tick(); err != nil {
			t.Fatal(err)
		}
		l.wg.Wait()
	}

	l.mu.Lock()
	l.holdLocked(contract.Limit{ResetsAt: t0.Add(10 * time.Minute), Source: "message"}) // a wrong, early reset
	l.mu.Unlock()
	limit = contract.Limit{ResetsAt: t0.Add(2 * time.Hour), Source: "message"}
	tick(11*time.Minute + time.Second) // resumes and dispatches one pool
	if want := Pool("."); runs != want {
		t.Fatalf("runs = %d after the early resume; want one pool (%d)", runs, want)
	}
	tick(12 * time.Minute)
	tick(2*time.Hour + 30*time.Second)
	if want := Pool("."); runs != want {
		t.Fatalf("runs = %d while re-held; want still %d", runs, want)
	}
}

// A stamped container whose repo: disagrees with the live repo = is held with
// paused:context, and the hold lifts when the stamp matches again (ADR 0167); a
// Workspace with no stamped bead reads no repo table.
func TestPauseContextsHoldsRepoStampDrift(t *testing.T) {
	oDecl, oRepos, oPause, oUn := declaredContexts, declaredRepos, pauseBead, unpauseBead
	t.Cleanup(func() { declaredContexts, declaredRepos, pauseBead, unpauseBead = oDecl, oRepos, oPause, oUn })
	declaredContexts = func(string) map[string]string { return map[string]string{"x": "vendor/x"} }
	var reads int
	live := map[string]string{"x": "vendor/x"}
	declaredRepos = func() map[string]string { reads++; return live }
	var acts []string
	pauseBead = func(id, cause string) error { acts = append(acts, "pause "+id); return nil }
	unpauseBead = func(id, cause string) error { acts = append(acts, "unpause "+id); return nil }
	cp := func(id, stamp string, paused bool) bd.Bead {
		b := bd.Bead{ID: id, Kind: contract.KindCheckpoint, Status: "open", Labels: []string{"kind:checkpoint", "context:x"}}
		if stamp != "" {
			b.Labels = append(b.Labels, "repo:"+stamp)
		}
		if paused {
			b.Labels = append(b.Labels, "paused:context")
		}
		return b
	}

	oRoot := rootHasBranch
	t.Cleanup(func() { rootHasBranch = oRoot })
	rootHasBranch = func(string) bool { return false }
	pauseContexts([]bd.Bead{cp("unstamped", "", false)})
	if reads != 1 || len(acts) != 0 { // an unstamped labelled container reads the repo table; no root branch, no hold
		t.Fatalf("unstamped tick: reads=%d acts=%v", reads, acts)
	}
	reads = 0
	pauseContexts([]bd.Bead{cp("same", "vendor/x", false), cp("moved", "vendor/old", false), cp("back", "vendor/x", true)})
	if want := []string{"pause moved", "unpause back"}; !slices.Equal(acts, want) || reads != 1 {
		t.Errorf("acts = %v reads = %d, want %v and one read", acts, reads, want)
	}
}

// A container cut in the Workspace repo and then labelled with a repo = context is held
// (Checkpoint or Chart); the hold lifts on removing the label or on a stamp; no root
// branch (Zoro's stamp window) holds nothing; path = and no-context cost no git read.
func TestPauseContextsHoldsRelabelledUnstampedContainer(t *testing.T) {
	oDecl, oRepos, oPause, oUn, oRoot := declaredContexts, declaredRepos, pauseBead, unpauseBead, rootHasBranch
	t.Cleanup(func() {
		declaredContexts, declaredRepos, pauseBead, unpauseBead, rootHasBranch = oDecl, oRepos, oPause, oUn, oRoot
	})
	declaredContexts = func(string) map[string]string { return map[string]string{"x": "vendor/x", "p": "apps/p"} }
	declaredRepos = func() map[string]string { return map[string]string{"x": "vendor/x"} }
	var probes []string
	inRoot := true
	rootHasBranch = func(b string) bool { probes = append(probes, b); return inRoot }
	var acts []string
	pauseBead = func(id, cause string) error { acts = append(acts, "pause "+id); return nil }
	unpauseBead = func(id, cause string) error { acts = append(acts, "unpause "+id); return nil }
	mk := func(id, kind string, extra ...string) bd.Bead {
		return bd.Bead{ID: id, Kind: contract.Kind(kind), Status: "open", Labels: append([]string{"kind:" + kind}, extra...)}
	}
	run := func(bs ...bd.Bead) {
		acts, probes = nil, nil
		pauseContexts(bs)
	}

	run(mk("cp", "checkpoint", "context:x"), mk("ch", "chart", "context:x"))
	if want := []string{"pause cp", "pause ch"}; !slices.Equal(acts, want) {
		t.Errorf("acts = %v, want %v", acts, want)
	}
	if want := []string{"checkpoint/cp", "chart/ch"}; !slices.Equal(probes, want) {
		t.Errorf("probes = %v, want %v", probes, want)
	}
	run(mk("cp", "checkpoint", "context:x", "paused:context")) // still held: no churn
	if len(acts) != 0 {
		t.Errorf("held again acts = %v", acts)
	}
	run(mk("cp", "checkpoint", "paused:context")) // label removed
	run(mk("cp", "checkpoint", "context:x", "repo:vendor/x", "paused:context"))
	if want := []string{"unpause cp"}; !slices.Equal(acts, want) { // stamped: the drift arm judges, it matches
		t.Errorf("stamped acts = %v, want %v", acts, want)
	}
	inRoot = false
	run(mk("cp", "checkpoint", "context:x"))
	if len(acts) != 0 || len(probes) != 1 {
		t.Errorf("no root branch: acts %v probes %v, want no hold", acts, probes)
	}
	run(mk("cp", "checkpoint", "context:p"))
	if len(acts) != 0 || len(probes) != 0 {
		t.Errorf("path = context: acts %v probes %v, want no hold and no probe", acts, probes)
	}
	run(mk("cp", "checkpoint"))
	if len(acts) != 0 || len(probes) != 0 {
		t.Errorf("no context: acts %v probes %v", acts, probes)
	}
}

// The per-tick effort set is read at most once per tick however many gates ask, and a
// new tick reads afresh (Spec FR 46).
func TestTickEffortsReadOncePerTick(t *testing.T) {
	orig := effortRefsAcross
	t.Cleanup(func() { effortRefsAcross = orig; effortSet.Store(nil) })
	reads := 0
	effortRefsAcross = func() (map[string]bool, error) { reads++; return map[string]bool{"b1": true}, nil }
	effortSet.Store(nil)
	if tickEfforts() != nil {
		t.Error("tickEfforts outside a tick = non-nil, want nil (the gates read for themselves)")
	}
	effortSet.Store(memoEfforts(effortRefsAcross))
	for range 3 {
		if !tickEfforts()["b1"] {
			t.Fatal("tickEfforts missing b1")
		}
	}
	if reads != 1 {
		t.Errorf("reads in one tick = %d, want 1", reads)
	}
}

// A nested Chart or Checkpoint whose trailer is on the child's main (not the
// Workspace's) reads as landed: its drained-Chart hold and CheckpointReady look at the
// child, so a landed nested Chart never re-spawns Landings (Spec FR 35).
func TestLandedNestedContainerIsSeenInTheChild(t *testing.T) {
	stubCheckpointReady(t)
	ws := t.TempDir()
	g := func(dir string, args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@e.com", "-c", "user.name=t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	g(ws, "init", "-q", "-b", "main")
	os.MkdirAll(filepath.Join(ws, ".mugicrew"), 0o755)
	os.WriteFile(filepath.Join(ws, ".mugicrew", "config.toml"), []byte("[context.child]\nrepo = \"child\"\n"), 0o644)
	g(ws, "add", ".")
	g(ws, "commit", "-q", "-m", "root")
	child := filepath.Join(ws, "child")
	os.Mkdir(child, 0o755)
	g(child, "init", "-q", "-b", "main")
	g(child, "commit", "-q", "--allow-empty", "-m", "land\n\nChart: ch1")
	g(child, "commit", "-q", "--allow-empty", "-m", "land\n\nCheckpoint: cp1")
	t.Chdir(ws)

	landedBase = func(string) (string, error) {
		return "", fmt.Errorf("not on the Workspace's main: %w", git.ErrNotLanded)
	}
	openChildrenOf = func(_ string, k contract.Kind) (int, error) { // an open Landing, no conflict or critique
		if k == contract.KindLanding {
			return 1, nil
		}
		return 0, nil
	}
	labels := []string{"context:child"}

	chart := bd.Bead{ID: "ch1", Kind: contract.KindChart, Labels: labels}
	if held, err := ChartLandingHeld(chart); err != nil || held {
		t.Errorf("landed nested Chart: held = (%v, %v), want not held", held, err)
	}
	cp := bd.Bead{ID: "cp1", Kind: contract.KindCheckpoint, Labels: labels}
	if ready, _, err := CheckpointLandHold(cp); err != nil || !ready {
		t.Errorf("landed nested Checkpoint: ready = (%v, %v)", ready, err)
	}
	// the same ids, unlabelled, are not seen: the Workspace's main has no trailer
	if held, _ := ChartLandingHeld(bd.Bead{ID: "ch1", Kind: contract.KindChart}); !held {
		t.Error("an unlabelled Chart read the child")
	}
}

// One container whose trailer another Bead Graph landed is skipped, not a tick failure:
// the other ready bead is still a candidate (FR 42, ADR 0187).
func TestCandidatesSkipForeignLandedContainer(t *testing.T) {
	stubCheckpointReady(t)
	foreign := &git.ForeignLanding{Trailer: "Checkpoint: ws-cp", Ours: "a", Theirs: "b", Sha: "f00d"}
	landedBase = func(string) (string, error) { return "", foreign }
	openChildrenOf = func(string, contract.Kind) (int, error) { return 0, nil }
	oKids := openChildren
	t.Cleanup(func() { openChildren = oKids })
	openChildren = func(string) (int, error) { return 0, nil }
	body := "Destination: ship it"
	cp := bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Description: body}
	chart := bd.Bead{ID: "ws-ch", Kind: contract.KindChart, Description: body}
	other := bd.Bead{ID: "ws-other", Kind: contract.KindResearch, Description: body}
	oTask := bd.SpawnBlockingTask
	t.Cleanup(func() { bd.SpawnBlockingTask = oTask })
	var held []string
	bd.SpawnBlockingTask = func(container, key, _, _ string) (string, bool, error) {
		held = append(held, container+" "+strings.TrimSpace(key))
		return "t", true, nil
	}
	got, err := (&Loop{}).candidates([]bd.Bead{cp, chart, other}, nil)
	if err != nil {
		t.Fatalf("candidates err = %v, want the foreign land skipped", err)
	}
	if want := []string{"ws-cp Foreign: f00d", "ws-ch Foreign: f00d"}; !reflect.DeepEqual(held, want) {
		t.Errorf("hold Tasks = %v, want %v", held, want)
	}
	if want := []string{"ws-other"}; !reflect.DeepEqual(bd.IDs(got), want) {
		t.Errorf("candidates = %v, want %v", bd.IDs(got), want)
	}
}

// A failing hold Task is not a skip: the tick fails rather than losing the refusal.
func TestCandidatesForeignHoldTaskErrorFailsTick(t *testing.T) {
	stubCheckpointReady(t)
	foreign := &git.ForeignLanding{Trailer: "Checkpoint: ws-cp", Ours: "a", Theirs: "b", Sha: "f00d"}
	landedBase = func(string) (string, error) { return "", foreign }
	openChildrenOf = func(string, contract.Kind) (int, error) { return 0, nil }
	oKids := openChildren
	t.Cleanup(func() { openChildren = oKids })
	openChildren = func(string) (int, error) { return 0, nil }
	oTask := bd.SpawnBlockingTask
	t.Cleanup(func() { bd.SpawnBlockingTask = oTask })
	boom := errors.New("bd down")
	bd.SpawnBlockingTask = func(_, _, _, _ string) (string, bool, error) { return "", false, boom }
	cp := bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Description: "Destination: ship it"}
	if _, err := (&Loop{}).candidates([]bd.Bead{cp}, nil); !errors.Is(err, boom) {
		t.Errorf("candidates err = %v, want the Task-filing error", err)
	}
}

// Two stop signals inside StopWindow are one stop (npx forwards Ctrl-C on top of
// the terminal's): Run drains and returns nil. One after the window forces.
func TestStopWindowDebouncesDuplicate(t *testing.T) {
	defer func(w time.Duration) { StopWindow = w }(StopWindow)
	StopWindow = 300 * time.Millisecond
	captureEvents(t)
	l, _, started, release, _ := blockingLoop(t)
	sig := make(chan os.Signal, 3)
	errc, _ := run(l, sig)
	<-started
	sig <- syscall.SIGINT
	sig <- syscall.SIGINT // duplicate inside the window
	select {
	case err := <-errc:
		t.Fatalf("Run returned %v on a duplicate stop; want it still draining", err)
	case <-time.After(100 * time.Millisecond):
	}
	time.Sleep(300 * time.Millisecond)
	sig <- syscall.SIGINT // after the window: force
	if err := <-errc; !errors.Is(err, ErrForced) {
		t.Fatalf("Run = %v; want ErrForced", err)
	}
	close(release)
}

// Only an Approval closed exactly "approved" at the tip makes a Checkpoint land-ready
// (ADR 0203 §5); rework, obsoleted and empty closes land nothing.
func TestCheckpointReadyCountsOnlyApprovedClose(t *testing.T) {
	stubCheckpointReady(t)
	cp := bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "in_progress"}
	openChildrenOf = func(string, contract.Kind) (int, error) { return 0, nil }
	landedBase = func(string) (string, error) { return "", git.ErrNotLanded }
	approvedAtHead = func(string, string) bool { return true }
	membersDrained = func(string) (bool, error) { return true, nil }
	openBlockers = func(string) ([]bd.Bead, error) { return nil, nil }
	reviewDue = func(string, func(string, string) bool) (bool, error) { return false, nil }
	for _, tc := range []struct {
		reason string
		want   bool
	}{
		{"approved", true},
		{"rework: mc-1, mc-2", false},
		{"Approval obsoleted: tip moved", false},
		{"obsolete: x", false},
		{"Approved", false},
		{"", false},
	} {
		closedApprovalsOf = func(string, contract.Kind) ([]bd.Bead, error) {
			return []bd.Bead{{Status: "closed", CloseReason: tc.reason, Description: "Commit: tip"}}, nil
		}
		if ready, _, err := CheckpointLandHold(cp); err != nil || ready != tc.want {
			t.Errorf("close reason %q: ready = %v, %v; want %v", tc.reason, ready, err, tc.want)
		}
	}
}

// A latch is a candidate only when due: unstamped, or last checked over JiraPoll
// ago. Non-latch AFK leaves ignore the stamps (Spec mc-8ymw.9 D5).
func TestCandidatesLatchDueOnlyAfterInterval(t *testing.T) {
	oNow := now
	t.Cleanup(func() { now = oNow })
	t0 := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return t0 }
	l := &Loop{JiraPoll: 15 * time.Minute}
	l.checked = map[string]time.Time{
		"fresh": t0.Add(-5 * time.Minute),
		"stale": t0.Add(-16 * time.Minute),
		"build": t0, // stamped, but not a latch
		"gone":  t0, // no longer ready
	}
	ready := []bd.Bead{
		{ID: "new", Kind: contract.KindLatch},
		{ID: "fresh", Kind: contract.KindLatch},
		{ID: "stale", Kind: contract.KindLatch},
		{ID: "build", Kind: contract.KindLanding, Parent: "mc-c"},
	}
	got, err := l.candidates(ready, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"new", "stale", "build"}; !slices.Equal(bd.IDs(got), want) {
		t.Errorf("candidates = %v, want %v", bd.IDs(got), want)
	}
	if _, ok := l.checked["gone"]; ok || len(l.checked) != 3 {
		t.Errorf("checked = %v, want only ready ids kept", l.checked)
	}
}

// Three failed latch dispatches, each due again after the interval, pause it
// paused:failing; the not-due ticks between them never reset the count.
func TestLatchFailuresAcrossIntervalsPause(t *testing.T) {
	oNow := now
	t.Cleanup(func() { now = oNow })
	t0 := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	clock := t0
	now = func() time.Time { return clock }
	l, b, pauses := failLoop(t, stepCutErr, stepCutErr, stepFailed)
	b.Kind = contract.KindLatch
	l.Crew = map[string]contract.Crewmate{ownerOf(b): idleCrew{}}
	l.JiraPoll = 15 * time.Minute
	for _, step := range []time.Duration{0, 5, 11, 5, 11} { // minutes: due, not due, due, not due, due
		clock = clock.Add(step * time.Minute)
		cands, err := l.candidates([]bd.Bead{b}, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cands {
			l.dispatch(c)
			l.wg.Wait()
		}
	}
	if want := []string{b.ID + " failing"}; !slices.Equal(*pauses, want) {
		t.Fatalf("pauses = %q, want %q", *pauses, want)
	}
}

// A latch's not-due decision uses its watched system's interval: mr by GitlabPoll,
// jira by JiraPoll, an unparseable body by JiraPoll.
func TestLatchNotDueUsesWatchedSystemInterval(t *testing.T) {
	oNow := now
	t.Cleanup(func() { now = oNow })
	t0 := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return t0 }
	l := &Loop{JiraPoll: 15 * time.Minute, GitlabPoll: 2 * time.Minute}
	for _, tc := range []struct {
		name, body string
		age        time.Duration
		want       bool
	}{
		{"mr inside gitlab", "Watches: mr gitlab.com/g/p!1", time.Minute, true},
		{"mr past gitlab", "Watches: mr gitlab.com/g/p!1", 3 * time.Minute, false},
		{"jira inside jira", "Watches: jira ABC-1", 10 * time.Minute, true},
		{"jira past gitlab only", "Watches: jira ABC-1", 3 * time.Minute, true},
		{"unparseable uses jira", "junk", 10 * time.Minute, true},
	} {
		l.checked = map[string]time.Time{"x": t0.Add(-tc.age)}
		b := bd.Bead{ID: "x", Kind: contract.KindLatch, Description: tc.body}
		if got := l.latchNotDue(b); got != tc.want {
			t.Errorf("%s: latchNotDue = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// stateNames drains states, returning the name of each.
func stateNames(states <-chan State) (names []string) {
	for len(states) > 0 {
		names = append(names, (<-states).State)
	}
	return
}

// waitState reads states until one named name arrives and returns it.
func waitState(t *testing.T, states <-chan State, name string) State {
	t.Helper()
	for {
		select {
		case s := <-states:
			if s.State == name {
				return s
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no %s State", name)
		}
	}
}

// resumedCauses lists the cause of each loop.resumed event.
func resumedCauses(evs []eventlogtest.Event) (causes []any) {
	for _, e := range evs {
		if e.Name == "loop.resumed" {
			causes = append(causes, e.Attrs["cause"])
		}
	}
	return
}

// SIGUSR1 in a stop drain puts the loop back on with its effort still running
// and logs loop.resumed {cause cancel}; the next stop is a fresh drain, never a
// force even with no StopWindow, and finishes once the effort does (ADR 0236).
func TestCancelStopDrainResumesPolling(t *testing.T) {
	evs := captureEvents(t)
	l, b, started, release, ticks := blockingLoop(t)
	sig := make(chan os.Signal, 4)
	errc, states := run(l, sig)
	<-started
	sig <- syscall.SIGTERM
	waitState(t, states, "draining")
	sig <- syscall.SIGUSR1
	if s := waitState(t, states, "on"); s.Rev != l.Rev {
		t.Fatalf("State after a cancel = %+v; want on @%s", s, l.Rev)
	}
	n := ticks.Load()
	time.Sleep(30 * time.Millisecond)
	if ticks.Load() == n {
		t.Fatal("loop did not resume polling after the cancel")
	}
	select {
	case err := <-errc:
		t.Fatalf("Run returned %v after a cancel with an effort in flight", err)
	default:
	}
	sig <- syscall.SIGTERM // StopWindow is 0: a force if the cancel left the stop spent
	if s := waitState(t, states, "draining"); !reflect.DeepEqual(s.Inflight, []bd.Bead{b}) {
		t.Fatalf("State = %+v; want a fresh drain of [%s]", s, b.ID)
	}
	select {
	case err := <-errc:
		t.Fatalf("Run returned %v; the stop after a cancel must drain, not force", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := <-errc; err != nil {
		t.Fatalf("drained Run = %v; want nil", err)
	}
	if got := resumedCauses(*evs); !reflect.DeepEqual(got, []any{"cancel"}) {
		t.Fatalf("loop.resumed causes = %v; want [cancel]", got)
	}
}

// SIGUSR1 while polling is dropped.
func TestCancelWhilePollingIsDropped(t *testing.T) {
	evs := captureEvents(t)
	l, _, started, release, _ := blockingLoop(t)
	sig := make(chan os.Signal, 4)
	errc, states := run(l, sig)
	<-started
	sig <- syscall.SIGUSR1
	sig <- syscall.SIGTERM // still the first stop: a dropped cancel left nothing armed
	waitState(t, states, "draining")
	select {
	case err := <-errc:
		t.Fatalf("Run returned %v; want a drain", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := <-errc; err != nil {
		t.Fatalf("Run = %v; want nil", err)
	}
	if got := resumedCauses(*evs); len(got) != 0 {
		t.Fatalf("loop.resumed causes = %v; want none", got)
	}
}

// A stop that converted a restart drain, once cancelled, puts the loop back in
// that restart drain (restart_to set again, not polling) and it still finishes
// by returning the Restart (FR10). SIGUSR1 in the unconverted restart drain is dropped.
func TestCancelConvertedRestartDrainRestoresRestart(t *testing.T) {
	evs := captureEvents(t)
	l, b, sig, res, states, release, _, _ := restartDraining(t)
	sig <- syscall.SIGUSR1 // unconverted: dropped
	time.Sleep(20 * time.Millisecond)
	sig <- syscall.SIGTERM
	if s := <-states; s.State != "draining" || s.RestartTo != "" {
		t.Fatalf("State after a stop = %+v; want draining without restart_to", s)
	}
	sig <- syscall.SIGUSR1
	want := State{State: "draining", Inflight: []bd.Bead{b}, Rev: l.Rev, RestartTo: "r1"}
	if s := <-states; !reflect.DeepEqual(s, want) {
		t.Fatalf("State after a cancel = %+v; want %+v", s, want)
	}
	close(release)
	if got := <-res; got.err != nil || got.r == nil || got.r.Rev != "r1" {
		t.Fatalf("Run = %+v, %v; want Restart{r1}", got.r, got.err)
	}
	if got := resumedCauses(*evs); !reflect.DeepEqual(got, []any{"cancel"}) {
		t.Fatalf("loop.resumed causes = %v; want [cancel]", got)
	}
}

// A cancel that loses the race to the last effort finishing leaves a stop: Run
// returns nil, nil, and the signal goes unread.
func TestCancelLosingRaceExitsAsStop(t *testing.T) {
	captureEvents(t)
	l, _, started, release, _ := blockingLoop(t)
	sig := make(chan os.Signal, 2)
	errc, states := run(l, sig)
	<-started
	sig <- syscall.SIGTERM
	waitState(t, states, "draining")
	close(release)
	if err := <-errc; err != nil {
		t.Fatalf("Run = %v; want nil, nil", err)
	}
	sig <- syscall.SIGUSR1
}

// Ctrl-C after a cancel: TERM, USR1, then TERM, TERM inside StopWindow is one
// fresh stop, a drain and not a force.
func TestStopAfterCancelDebouncesInsideWindow(t *testing.T) {
	defer func(w time.Duration) { StopWindow = w }(StopWindow)
	StopWindow = 300 * time.Millisecond
	captureEvents(t)
	l, _, started, release, _ := blockingLoop(t)
	sig := make(chan os.Signal, 4)
	errc, states := run(l, sig)
	<-started
	sig <- syscall.SIGTERM
	waitState(t, states, "draining")
	sig <- syscall.SIGUSR1
	waitState(t, states, "on")
	sig <- syscall.SIGTERM
	sig <- syscall.SIGTERM
	select {
	case err := <-errc:
		t.Fatalf("Run returned %v; two stops inside the window are one drain", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-errc; err != nil {
		t.Fatalf("Run = %v; want nil", err)
	}
}

// A limited hold survives a cancel: the resumed loop reports limited again.
func TestCancelKeepsLimitedHold(t *testing.T) {
	captureEvents(t)
	l, _, started, release, _ := blockingLoop(t)
	reset := time.Now().Add(time.Hour)
	sig := make(chan os.Signal, 4)
	errc, states := run(l, sig)
	<-started
	l.mu.Lock()
	l.limitedUntil, l.limit = reset, contract.Limit{ResetsAt: reset, Source: "message"}
	l.mu.Unlock()
	sig <- syscall.SIGTERM
	waitState(t, states, "draining")
	sig <- syscall.SIGUSR1
	if s := waitState(t, states, "limited"); !s.ResetsAt.Equal(reset) || s.LimitSource != "message" {
		t.Fatalf("State after a cancel = %+v; want limited at %v", s, reset)
	}
	sig <- syscall.SIGTERM
	close(release)
	if err := <-errc; err != nil {
		t.Fatalf("Run = %v; want nil", err)
	}
}

// Stop, cancel, stop, then a later second stop forces; a dispatch between the
// cancel and the second drain must not trip the WaitGroup (run under -race).
func TestCancelThenStopThenForce(t *testing.T) {
	defer func(w time.Duration) { StopWindow = w }(StopWindow)
	StopWindow = 100 * time.Millisecond
	captureEvents(t)
	l, _, started, release, _ := blockingLoop(t)
	defer close(release)
	b2 := bd.Bead{ID: "mc-r2", Title: "Research: r2", Kind: contract.KindResearch, Parent: "mc-c"}
	var resumed atomic.Bool
	first := readyBeads
	readyBeads = func() ([]bd.Bead, error) {
		if resumed.Load() && resumed.CompareAndSwap(true, false) {
			return []bd.Bead{b2}, nil
		}
		return first()
	}
	sig := make(chan os.Signal, 4)
	errc, states := run(l, sig)
	<-started
	sig <- syscall.SIGTERM
	waitState(t, states, "draining")
	sig <- syscall.SIGUSR1
	waitState(t, states, "on")
	resumed.Store(true)
	<-started // the resumed tick dispatched a second effort
	sig <- syscall.SIGTERM
	waitState(t, states, "draining")
	time.Sleep(150 * time.Millisecond)
	sig <- syscall.SIGTERM
	if err := <-errc; !errors.Is(err, ErrForced) {
		t.Fatalf("Run = %v; want ErrForced", err)
	}
}

// reqEvents are evs' restart.* and build.* events as "name" or "name:outcome", in order.
func reqEvents(evs []eventlogtest.Event) (out []string) {
	for _, e := range evs {
		switch {
		case e.Name == "restart.requested":
			out = append(out, fmt.Sprintf("%s:%v", e.Name, e.Attrs["outcome"]))
		case strings.HasPrefix(e.Name, "restart.") || strings.HasPrefix(e.Name, "build."):
			out = append(out, e.Name)
		}
	}
	return
}

// asked is an unwatched pinned loop whose main has moved to r1: it builds only on SIGUSR2.
func asked(t *testing.T, eval, build func(context.Context, string) (string, error)) (l *Loop, setMain func(string), builds *atomic.Int32) {
	l, _, _, release, _ := blockingLoop(t)
	t.Cleanup(func() { close(release) })
	setMain, _, builds = nix(t, idle(l), eval, build)
	l.Watch = false
	setMain("r1")
	return
}

// waitReq blocks until n restart.* / build.* events have been logged.
func waitReq(t *testing.T, evs *[]eventlogtest.Event, n int) {
	t.Helper()
	eventlogtest.Wait(t, evs, func(e []eventlogtest.Event) bool { return len(reqEvents(e)) >= n })
}

func waitRes(t *testing.T, res <-chan result) result {
	t.Helper()
	select {
	case r := <-res:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return")
		return result{}
	}
}

// SIGUSR2 on an unwatched loop builds main and drains for a restart onto it,
// leaving the watch setting off (ADR 0289).
func TestSigusr2RestartsUnwatchedLoop(t *testing.T) {
	evs := captureEvents(t)
	l, _, builds := asked(t, newOutPath, func(context.Context, string) (string, error) { return newBin, nil })
	sig := make(chan os.Signal, 2)
	res, states := runFull(l, sig)
	if s := <-states; s.State != "on" {
		t.Fatalf("State = %+v; want on", s)
	}
	time.Sleep(20 * time.Millisecond)
	if builds.Load() != 0 {
		t.Fatal("an unwatched loop built without being asked")
	}
	sig <- syscall.SIGUSR2
	if got := waitRes(t, res); got.err != nil || !reflect.DeepEqual(got.r, &Restart{Rev: "r1", Bin: newBin}) {
		t.Fatalf("Run = %+v, %v; want Restart{r1}", got.r, got.err)
	}
	if want := []string{"restart.requested:building", "build.started", "build.done"}; !slices.Equal(reqEvents(*evs), want) {
		t.Fatalf("events = %v; want %v", reqEvents(*evs), want)
	}
	if a := (*evs)[0].Attrs; a["joined"] != false {
		t.Fatalf("restart.requested = %v; want joined false", a)
	}
	var last State
	for len(states) > 0 {
		last = <-states
	}
	if last.State != "draining" || last.RestartTo != "r1" || l.Watch {
		t.Fatalf("last State = %+v watch=%v; want a restart drain onto r1, still unwatched", last, l.Watch)
	}
}

// The same outPath rewrites Rev with no drain, and says so when asked.
func TestSigusr2SameOutPathLogsCurrent(t *testing.T) {
	evs := captureEvents(t)
	l, _, builds := asked(t, func(context.Context, string) (string, error) { return "/nix/store/old-mugicrew", nil }, nil)
	sig := make(chan os.Signal, 2)
	res, states := runFull(l, sig)
	<-states
	sig <- syscall.SIGUSR2
	deadline := time.Now().Add(2 * time.Second)
	for state := (State{}); state.Rev != "r1"; {
		select {
		case state = <-states:
		case <-time.After(time.Until(deadline)):
			t.Fatal("Rev was not rewritten to r1")
		}
		if state.State == "draining" {
			t.Fatalf("State = %+v; want no drain", state)
		}
	}
	sig <- syscall.SIGTERM
	if got := waitRes(t, res); got.r != nil || got.err != nil {
		t.Fatalf("Run = %+v; want nil", got)
	}
	if want := []string{"restart.requested:building", "restart.current"}; !slices.Equal(reqEvents(*evs), want) {
		t.Fatalf("events = %v; want %v", reqEvents(*evs), want)
	}
	if (*evs)[1].Attrs["rev"] != "r1" || builds.Load() != 0 {
		t.Fatalf("restart.current = %v builds=%d; want rev r1, no build", (*evs)[1].Attrs, builds.Load())
	}
}

// A watched tick's own same-outPath stays silent.
func TestWatchedSameOutPathLogsNothing(t *testing.T) {
	evs := captureEvents(t)
	l, _, _, release, _ := blockingLoop(t)
	defer close(release)
	setMain, _, _ := nix(t, idle(l), func(context.Context, string) (string, error) { return "/nix/store/old-mugicrew", nil }, nil)
	setMain("r1")
	sig := make(chan os.Signal, 2)
	res, states := runFull(l, sig)
	waitState(t, states, "on")
	if s := waitState(t, states, "on"); s.Rev != "r1" {
		t.Fatalf("State = %+v; want Rev r1", s)
	}
	sig <- syscall.SIGTERM
	waitRes(t, res)
	if got := reqEvents(*evs); len(got) != 0 {
		t.Fatalf("events = %v; want none", got)
	}
}

// Main equal to the running rev: current, nothing built.
func TestSigusr2AlreadyCurrent(t *testing.T) {
	evs := captureEvents(t)
	l, setMain, builds := asked(t, newOutPath, nil)
	setMain("r0")
	sig := make(chan os.Signal, 2)
	res, _ := runFull(l, sig)
	sig <- syscall.SIGUSR2
	waitReq(t, evs, 1)
	sig <- syscall.SIGTERM
	waitRes(t, res)
	if got := reqEvents(*evs); !slices.Equal(got, []string{"restart.requested:current"}) || builds.Load() != 0 {
		t.Fatalf("events = %v builds=%d; want only requested:current", got, builds.Load())
	}
}

// A failed asked build sets FailedRev on an unwatched loop, and the next SIGUSR2
// for the same rev builds again.
func TestSigusr2FailedBuildIsStaleAndRetried(t *testing.T) {
	captureEvents(t)
	l, _, builds := asked(t, newOutPath, func(context.Context, string) (string, error) { return "", errors.New("boom") })
	sig := make(chan os.Signal, 2)
	res, states := runFull(l, sig)
	<-states
	for i := int32(1); i <= 2; i++ {
		sig <- syscall.SIGUSR2
		for s := waitState(t, states, "on"); s.FailedRev != "r1"; s = waitState(t, states, "on") {
		}
		if builds.Load() != i {
			t.Fatalf("builds = %d; want %d", builds.Load(), i)
		}
	}
	sig <- syscall.SIGTERM
	waitRes(t, res)
}

// A request during a watched tick's build joins it; back-to-back requests are one build.
func TestSigusr2JoinsBuildInFlight(t *testing.T) {
	evs := captureEvents(t)
	l, _, _, release, _ := blockingLoop(t)
	defer close(release)
	inBuild, go_ := make(chan struct{}), make(chan struct{})
	setMain, _, builds := nix(t, idle(l), newOutPath, func(context.Context, string) (string, error) {
		close(inBuild)
		<-go_
		return newBin, nil
	})
	setMain("r1")
	sig := make(chan os.Signal, 4)
	res, _ := runFull(l, sig)
	<-inBuild
	sig <- syscall.SIGUSR2
	sig <- syscall.SIGUSR2
	waitReq(t, evs, 2) // build.started + a request; repeats merge in the one-slot asks
	close(go_)
	if got := waitRes(t, res); got.r == nil {
		t.Fatal("want a restart")
	}
	if builds.Load() != 1 {
		t.Fatalf("builds = %d; want 1", builds.Load())
	}
	if got := reqEvents(*evs); got[0] != "build.started" || got[1] != "restart.requested:building" || (*evs)[1].Attrs["joined"] != true {
		t.Fatalf("events = %v; want build.started then a joined request", got)
	}
}

// Back-to-back requests on an idle loop: one build.
func TestSigusr2RequestsMerge(t *testing.T) {
	evs := captureEvents(t)
	gate := make(chan struct{})
	l, _, builds := asked(t, newOutPath, func(context.Context, string) (string, error) { <-gate; return newBin, nil })
	sig := make(chan os.Signal, 4)
	res, _ := runFull(l, sig)
	sig <- syscall.SIGUSR2
	sig <- syscall.SIGUSR2
	waitReq(t, evs, 2) // a request and the one build.started; repeats merge
	close(gate)
	waitRes(t, res)
	if builds.Load() != 1 {
		t.Fatalf("builds = %d; want 1", builds.Load())
	}
}

// Without a source dir: unsourced.
func TestSigusr2Unsourced(t *testing.T) {
	evs := captureEvents(t)
	l, _, builds := asked(t, newOutPath, nil)
	l.SourceDir = ""
	sig := make(chan os.Signal, 2)
	res, _ := runFull(l, sig)
	sig <- syscall.SIGUSR2
	waitReq(t, evs, 1)
	sig <- syscall.SIGTERM
	waitRes(t, res)
	if got := reqEvents(*evs); !slices.Equal(got, []string{"restart.requested:unsourced"}) || builds.Load() != 0 {
		t.Fatalf("events = %v builds=%d; want only unsourced", got, builds.Load())
	}
}

// In a stop drain a request is answered stopping; in a restart drain, draining.
func TestSigusr2DuringDrains(t *testing.T) {
	evs := captureEvents(t)
	l, _, started, release, _ := blockingLoop(t)
	sig := make(chan os.Signal, 4)
	errc, states := run(l, sig)
	<-started
	sig <- syscall.SIGTERM
	waitState(t, states, "draining")
	sig <- syscall.SIGUSR2
	waitReq(t, evs, 1)
	close(release)
	<-errc
	if got := reqEvents(*evs); !slices.Equal(got, []string{"restart.requested:stopping"}) {
		t.Fatalf("stop drain events = %v; want stopping", got)
	}

	evs = captureEvents(t)
	_, _, sig, res, _, release, _, _ := restartDraining(t)
	sig <- syscall.SIGUSR2
	waitReq(t, evs, 3) // build.started, build.done, restart.requested
	close(release)
	waitRes(t, res)
	if got := reqEvents(*evs); !slices.Equal(got, []string{"build.started", "build.done", "restart.requested:draining"}) {
		t.Fatalf("restart drain events = %v; want draining", got)
	}
}

// A stop beats an asked build: it cancels the build and Run returns nil.
func TestStopCancelsAskedBuild(t *testing.T) {
	captureEvents(t)
	inBuild := make(chan struct{})
	l, _, _ := asked(t, newOutPath, func(ctx context.Context, _ string) (string, error) {
		close(inBuild)
		<-ctx.Done()
		return "", ctx.Err()
	})
	sig := make(chan os.Signal, 2)
	res, _ := runFull(l, sig)
	sig <- syscall.SIGUSR2
	<-inBuild
	sig <- syscall.SIGTERM
	if got := waitRes(t, res); got.r != nil || got.err != nil {
		t.Fatalf("Run = %+v; want nil, nil", got)
	}
}
