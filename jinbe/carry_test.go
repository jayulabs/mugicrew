package jinbe

import (
	"errors"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// stubVerify swaps the renumber-only verifier and its rules, recording each call's pins.
func stubVerify(t *testing.T, v git.RenumberVerdict, err error) *[][3]string {
	t.Helper()
	oV, oR := verifyRenumber, sequenceRules
	t.Cleanup(func() { verifyRenumber, sequenceRules = oV, oR })
	var calls [][3]string
	sequenceRules = func(git.Repo) ([]git.Rule, error) { return []git.Rule{git.ADRRule}, nil }
	verifyRenumber = func(_ git.Repo, _ []git.Rule, oldPin, newHead, mainline string) (git.RenumberVerdict, error) {
		calls = append(calls, [3]string{oldPin, newHead, mainline})
		return v, err
	}
	return &calls
}

func approvedOld() bd.Bead {
	return bd.Bead{ID: "ws-old", Kind: contract.KindApproval, Title: "Ship the thing", Status: "closed", CloseReason: bd.ApprovedReason, Description: "Clean.\nCommit: oldpin"}
}

func carryRec(kids ...bd.Bead) *jinbeSeamRec {
	return &jinbeSeamRec{
		kids:       []bd.Bead{{ID: "b1", Kind: contract.KindBuild, Status: "closed"}},
		head:       "tip1",
		threadKids: kids,
	}
}

var accepted = git.RenumberVerdict{Accept: true, Renames: []git.Renamed{{Path: "docs/adr/0395-x.md", OldPath: "docs/adr/0394-x.md", Old: "0394", New: "0395"}}}

// A renumber-only head carries the newest approved Approval: Go writes a carried Approval
// closed approved at birth, caused-by the one it carries, instead of the Review, and the
// next tick's at-head gate spawns the Landing.
func TestRenumberOnlyHeadCarriesApproval(t *testing.T) {
	r := carryRec(approvedOld())
	stubJinbeSeams(t, r)
	calls := stubVerify(t, accepted, nil)
	if err := reconcileCheckpoint(openCheckpoint()); err != nil {
		t.Fatal(err)
	}
	if len(r.spawned) != 1 || r.spawned[0].Kind != contract.KindApproval {
		t.Fatalf("want one carried Approval and no Review, got %+v", r.spawned)
	}
	a := r.spawned[0]
	if a.CausedBy != "ws-old" || a.Title != "Ship the thing" {
		t.Errorf("carried Approval must be caused-by the carried one, got %+v", a)
	}
	want := bd.CarriedApprovalBody("Ship the thing", "tip1", "", "oldpin", []bd.Rename{{Path: "docs/adr/0395-x.md", Old: "0394", New: "0395"}})
	if a.Description != want {
		t.Errorf("body = %q, want %q", a.Description, want)
	}
	if len(r.closed) != 1 || r.reasons[0] != bd.ApprovedReason {
		t.Errorf("carried Approval must close approved at birth, got %v %v", r.closed, r.reasons)
	}
	if len(*calls) != 1 || (*calls)[0] != [3]string{"oldpin", "tip1", "basesha"} {
		t.Errorf("verifier calls %v", *calls)
	}
	// next tick: the carried Approval is at head, closed approved
	r.approvalBody = a.Description
	r.spawned, r.closed = nil, nil
	if err := reconcileCheckpoint(openCheckpoint()); err != nil {
		t.Fatal(err)
	}
	landingsSpawned(t, r, "tip1", "ws-appr0")
}

func TestRenumberRejectSpawnsReviewWithNote(t *testing.T) {
	r := carryRec(approvedOld())
	stubJinbeSeams(t, r)
	stubVerify(t, git.RenumberVerdict{Point: git.PointCompare, File: "bd/bd.go"}, nil)
	if err := reconcileCheckpoint(openCheckpoint()); err != nil {
		t.Fatal(err)
	}
	if len(r.spawned) != 1 || r.spawned[0].Kind != contract.KindReview {
		t.Fatalf("want one Review, got %+v", r.spawned)
	}
	if !strings.HasSuffix(r.spawned[0].Description, "\nnot carried: compare (bd/bd.go)") {
		t.Errorf("Review lacks the note: %q", r.spawned[0].Description)
	}
	if len(r.closed) != 0 {
		t.Errorf("a reject carries nothing, closed %v", r.closed)
	}
}

func TestNoApprovedApprovalSpawnsPlainReview(t *testing.T) {
	obsolete := approvedOld()
	obsolete.CloseReason = "Approval obsoleted: tip moved"
	for name, kids := range map[string][]bd.Bead{"none": nil, "only obsoleted": {obsolete}} {
		t.Run(name, func(t *testing.T) {
			r := carryRec(kids...)
			stubJinbeSeams(t, r)
			calls := stubVerify(t, accepted, nil)
			if err := reconcileCheckpoint(openCheckpoint()); err != nil {
				t.Fatal(err)
			}
			if len(r.spawned) != 1 || r.spawned[0].Kind != contract.KindReview || strings.Contains(r.spawned[0].Description, "not carried") {
				t.Errorf("want today's bare Review, got %+v", r.spawned)
			}
			if len(*calls) != 0 {
				t.Errorf("no approved Approval, nothing to verify against: %v", *calls)
			}
		})
	}
}

func TestRenumberGitErrorSpawnsNothing(t *testing.T) {
	r := carryRec(approvedOld())
	stubJinbeSeams(t, r)
	stubVerify(t, git.RenumberVerdict{}, errors.New("merge-tree boom"))
	if err := reconcileCheckpoint(openCheckpoint()); err == nil {
		t.Fatal("a git error must surface so the next tick retries")
	}
	if len(r.spawned) != 0 || len(r.closed) != 0 {
		t.Errorf("nothing spawned on a git error, got %v %v", r.spawned, r.closed)
	}
}

// A crash between the carried Approval's spawn and its close leaves it open at head; the
// re-run closes it and writes no second one.
func TestCarriedApprovalCrashHeals(t *testing.T) {
	open := bd.Bead{ID: "ws-car", Kind: contract.KindApproval, Status: "open", Description: bd.CarriedApprovalBody("Ship the thing", "tip1", "", "oldpin", nil)}
	r := carryRec(approvedOld(), open)
	r.kids = append(r.kids, open)
	stubJinbeSeams(t, r)
	calls := stubVerify(t, accepted, nil)
	if err := reconcileCheckpoint(openCheckpoint()); err != nil {
		t.Fatal(err)
	}
	if len(r.spawned) != 0 || len(*calls) != 0 {
		t.Errorf("no second carried Approval, got %v (verifier %v)", r.spawned, *calls)
	}
	if len(r.closed) != 1 || r.closed[0] != "ws-car" || r.reasons[0] != bd.ApprovedReason {
		t.Errorf("want the open carried Approval closed approved, got %v %v", r.closed, r.reasons)
	}
}
