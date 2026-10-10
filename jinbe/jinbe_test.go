package jinbe

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/git"
)

type stubCtx struct{}

func (stubCtx) Harness() contract.Harness { return nil }

// jinbeSeamRec captures the bd/git legs so the tests can assert the approvedAtHead
// gate, the land-trailer idempotency roll-forward, and the conflict-raise path
// without live bd/git. Mirrors usoppSeamRec.
type jinbeSeamRec struct {
	trailerLanded  bool            // landedBase(checkpointTrailer) succeeds → already landed
	approvalBody   string          // one closed Approval's body; "" → no closed Approval (single-approval shorthand)
	approvalBodies []string        // >1 closed Approval bodies in creation order; wins over approvalBody
	head           string          // the checkpoint branch tip approvedAtHead compares the pin to
	landErr        error           // non-nil → git.Land failure (a *git.RebaseConflict raises a bead)
	landed         []string        // branches landBranch was called for
	spawned        []bd.Successor  // members spawned (Conflict or Review)
	closed         []string        // checkpoints closed
	kids           []bd.Bead       // the Checkpoint's children ReviewDue reads; a spawn adds an open one
	efforts        map[string]bool // ids holding effort/<id>
	pivots         []bd.Bead       // the Checkpoint's open pivots
	blockers       []bd.Bead       // the Checkpoint's open incoming blockers (the land hold)
}

func stubJinbeSeams(t *testing.T, r *jinbeSeamRec) {
	t.Helper()
	oClose, oSpawn, oClosedChildren, oLand, oLandedBase, oApproved, oDue, oHead, oDrained, oBlockers := closeBead, spawnChild, closedChildrenOfKind, landBranch, landedBase, approvedAtHead, reviewDue, branchHead, membersDrained, openBlockers
	t.Cleanup(func() {
		closeBead, spawnChild, closedChildrenOfKind, landBranch, landedBase, approvedAtHead, reviewDue, branchHead, membersDrained, openBlockers = oClose, oSpawn, oClosedChildren, oLand, oLandedBase, oApproved, oDue, oHead, oDrained, oBlockers
	})
	openBlockers = func(string) ([]bd.Bead, error) { return r.blockers, nil }
	membersDrained = func(string) (bool, error) { return bd.MembersDrainedOver(r.kids, r.efforts), nil }
	reviewDue = func(id string, approved func(string, string) bool) (bool, error) {
		atHead := func(body string) bool { return approved(body, "checkpoint/"+id) }
		return bd.ReviewDueOver(r.kids, r.efforts, r.pivots, atHead), nil
	}
	closeBead = func(id, _ string) error { r.closed = append(r.closed, id); return nil }
	spawnChild = func(_ string, s bd.Successor) (string, bool, error) {
		r.spawned = append(r.spawned, s)
		r.kids = append(r.kids, bd.Bead{ID: fmt.Sprintf("ws-kid%d", len(r.kids)), Kind: s.Kind, Status: "open", Description: s.Description})
		return "ws-conf", true, nil
	}
	closedChildrenOfKind = func(_ string, _ contract.Kind) ([]bd.Bead, error) {
		bodies := r.approvalBodies
		if bodies == nil && r.approvalBody != "" {
			bodies = []string{r.approvalBody}
		}
		var out []bd.Bead
		for i, body := range bodies {
			out = append(out, bd.Bead{ID: fmt.Sprintf("ws-appr%d", i), Kind: contract.KindApproval, Status: "closed", Description: body})
		}
		return out, nil
	}
	landBranch = func(branch, _ string) (string, error) {
		r.landed = append(r.landed, branch)
		return "landsha", r.landErr
	}
	landedBase = func(string) (string, error) {
		if r.trailerLanded {
			return "landsha", nil
		}
		return "", errors.New("no landed commit")
	}
	// Stand in for git.ApprovedAtHead's pin==tip compare, reading the recorded tip so
	// the real git.Head shell-out is swapped out entirely.
	branchHead = func(string) (string, error) { return r.head, nil }
	approvedAtHead = func(body, _ string) bool {
		pin, ok := git.ParseCommit(body)
		return ok && pin == r.head
	}
}

func openCheckpoint() bd.Bead {
	return bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "in_progress"}
}

// Launch errors for every kind — the Checkpoint land is AFK, not launchable.
func TestLaunchErrorsForEveryKind(t *testing.T) {
	for _, k := range []contract.Kind{contract.KindCheckpoint, contract.KindApproval, contract.KindBuild} {
		if _, err := (Jinbe{}).Launch(stubCtx{}, contract.Bead{ID: "x", Kind: k}); err == nil {
			t.Errorf("Launch of AFK kind %q must error", k)
		}
	}
}

// The land trailer is "Checkpoint: <id>" (ADR 0010 §2), built from git/'s rule so
// LandedBase greps back the exact string Land stamped.
func TestCheckpointTrailer(t *testing.T) {
	if got := checkpointTrailer("ws-cp"); got != "Checkpoint: ws-cp" {
		t.Errorf("checkpointTrailer = %q, want %q", got, "Checkpoint: ws-cp")
	}
	if got := checkpointBranch("ws-cp"); got != "checkpoint/ws-cp" {
		t.Errorf("checkpointBranch = %q, want %q", got, "checkpoint/ws-cp")
	}
}

// The approvedAtHead gate (FR40): a closed Approval pinning the current tip lands;
// no approval, or a pin behind the tip, does NOT — the Checkpoint stays open and
// visible, never landing unreviewed.
func TestApprovedAtHeadGateLandsThenCloses(t *testing.T) {
	r := &jinbeSeamRec{approvalBody: "Clean pass.\nCommit: cafef00d", head: "cafef00d"}
	stubJinbeSeams(t, r)

	if err := reconcileCheckpoint(openCheckpoint()); err != nil {
		t.Fatalf("reconcileCheckpoint: %v", err)
	}
	if len(r.landed) != 1 || r.landed[0] != "checkpoint/ws-cp" {
		t.Fatalf("an approved-at-head Checkpoint must land its branch once, got %v", r.landed)
	}
	if len(r.closed) != 1 || r.closed[0] != "ws-cp" {
		t.Errorf("a clean land must close the Checkpoint (the merged state), got %v", r.closed)
	}
	if len(r.spawned) != 0 {
		t.Errorf("a clean land raises no conflict, got %v", r.spawned)
	}
}

// The land hold (Spec FR 15): an Approval at head never lands past an
// open incoming blocker, so neither the loop nor t lands a Task-held Checkpoint;
// once the Task closes, the same Approval lands.
func TestApprovedLandHeldByOpenBlocker(t *testing.T) {
	r := &jinbeSeamRec{approvalBody: "Clean pass.\nCommit: cafef00d", head: "cafef00d",
		blockers: []bd.Bead{{ID: "ws-task", Kind: contract.KindTask, Status: "open"}}}
	stubJinbeSeams(t, r)
	if err := reconcileCheckpoint(openCheckpoint()); err != nil {
		t.Fatalf("reconcileCheckpoint: %v", err)
	}
	if len(r.landed)+len(r.closed)+len(r.spawned) != 0 {
		t.Fatalf("a held land must do nothing, landed %v closed %v spawned %v", r.landed, r.closed, r.spawned)
	}
	r.blockers = nil // the Task closed
	if err := reconcileCheckpoint(openCheckpoint()); err != nil {
		t.Fatalf("reconcileCheckpoint: %v", err)
	}
	if len(r.landed) != 1 || len(r.closed) != 1 {
		t.Errorf("released: landed %v closed %v, want one land and the close", r.landed, r.closed)
	}
}

// Spec FR 28: an Approval at head never lands without a member's work. A
// closed member still holding effort/<id> (a Conflict whose integrate didn't
// publish) holds the land, silently (FR 47); with the ref gone it lands.
func TestApprovedLandWaitsOnLiveEffortRef(t *testing.T) {
	kids := []bd.Bead{{ID: "ws-conf", Kind: contract.KindConflict, Status: "closed"}}
	for _, tc := range []struct {
		efforts map[string]bool
		lands   bool
	}{{map[string]bool{"ws-conf": true}, false}, {nil, true}} {
		r := &jinbeSeamRec{approvalBody: "Clean pass.\nCommit: cafef00d", head: "cafef00d", kids: kids, efforts: tc.efforts}
		stubJinbeSeams(t, r)
		evs := eventlogtest.Capture(t)

		if err := reconcileCheckpoint(openCheckpoint()); err != nil {
			t.Fatalf("reconcileCheckpoint: %v", err)
		}
		if got := len(r.landed) == 1 && len(r.closed) == 1; got != tc.lands {
			t.Errorf("efforts %v: landed=%v closed=%v, want land %v", tc.efforts, r.landed, r.closed, tc.lands)
		}
		if !tc.lands && (len(*evs) != 0 || len(r.spawned) != 0) {
			t.Errorf("a held land waits silently, got events %v spawned %v", eventlogtest.Names(*evs), r.spawned)
		}
	}
}

func TestNoApprovalDoesNotLand(t *testing.T) {
	r := &jinbeSeamRec{approvalBody: ""} // no closed Approval
	stubJinbeSeams(t, r)

	if err := reconcileCheckpoint(openCheckpoint()); err != nil {
		t.Fatalf("reconcileCheckpoint: %v", err)
	}
	if len(r.landed) != 0 || len(r.closed) != 0 {
		t.Errorf("no closed Approval must NOT land or close (fail safe), got landed=%v closed=%v", r.landed, r.closed)
	}
}

func TestStaleApprovalDoesNotLand(t *testing.T) {
	r := &jinbeSeamRec{approvalBody: "Clean pass.\nCommit: oldpin", head: "newtip"}
	stubJinbeSeams(t, r)

	if err := reconcileCheckpoint(openCheckpoint()); err != nil {
		t.Fatalf("reconcileCheckpoint: %v", err)
	}
	if len(r.landed) != 0 || len(r.closed) != 0 {
		t.Errorf("a stale approval (pin behind tip) must NOT land, got landed=%v closed=%v", r.landed, r.closed)
	}
}

// After a stale-Approval self-heal a Checkpoint carries >=2 closed
// Approvals — the stale obsoleted one (created first, pin behind HEAD) AND the real at-head
// one (closed later by Luffy). The gate must scan ALL closed Approvals and land on the
// at-head one; a first-match read would hit the stale pin and strand the land forever.
func TestAnyAtHeadClosedApprovalLandsAfterSelfHeal(t *testing.T) {
	r := &jinbeSeamRec{
		approvalBodies: []string{
			"Obsoleted.\nCommit: oldpin",  // A1: stale, created first
			"Clean pass.\nCommit: newtip", // A2: real at-head, created later
		},
		head: "newtip",
	}
	stubJinbeSeams(t, r)

	if err := reconcileCheckpoint(openCheckpoint()); err != nil {
		t.Fatalf("reconcileCheckpoint: %v", err)
	}
	if len(r.landed) != 1 || r.landed[0] != "checkpoint/ws-cp" {
		t.Fatalf("an at-head closed Approval behind a stale one must still land, got %v", r.landed)
	}
	if len(r.closed) != 1 || r.closed[0] != "ws-cp" {
		t.Errorf("the self-healed Checkpoint must close on land, got %v", r.closed)
	}
}

// Idempotency roll-forward (FR42): the land trailer already on mainline means the
// Checkpoint landed (a crash between Land and close) — re-run lands nothing again and
// just rolls the close forward.
func TestLandTrailerIdempotencyRollsForward(t *testing.T) {
	r := &jinbeSeamRec{trailerLanded: true}
	stubJinbeSeams(t, r)

	if err := reconcileCheckpoint(openCheckpoint()); err != nil {
		t.Fatalf("reconcileCheckpoint re-run: %v", err)
	}
	if len(r.landed) != 0 {
		t.Errorf("an already-landed Checkpoint must NOT re-land, got %v", r.landed)
	}
	if len(r.closed) != 1 || r.closed[0] != "ws-cp" {
		t.Errorf("the roll-forward must close the still-open Checkpoint, got %v", r.closed)
	}
}

// A re-run over an already-closed, already-landed Checkpoint closes nothing again.
func TestLandTrailerClosedCheckpointNoDoubleClose(t *testing.T) {
	r := &jinbeSeamRec{trailerLanded: true}
	stubJinbeSeams(t, r)
	cp := openCheckpoint()
	cp.Status = "closed"

	if err := reconcileCheckpoint(cp); err != nil {
		t.Fatalf("reconcileCheckpoint: %v", err)
	}
	if len(r.closed) != 0 {
		t.Errorf("an already-closed Checkpoint must not be closed again, got %v", r.closed)
	}
}

// A rebase conflict raises one Franky-owned kind:conflict member carrying the Base:
// pin — the Checkpoint is NOT closed and the error does NOT propagate (mirrors
// fileChartConflict).
func TestRebaseConflictRaisesConflictBead(t *testing.T) {
	r := &jinbeSeamRec{approvalBody: "Commit: cafef00d", head: "cafef00d", landErr: &git.RebaseConflict{Base: "basesha"}}
	stubJinbeSeams(t, r)

	if err := reconcileCheckpoint(openCheckpoint()); err != nil {
		t.Fatalf("a rebase conflict must NOT propagate (it becomes a bead): %v", err)
	}
	if len(r.spawned) != 1 {
		t.Fatalf("a conflict must raise exactly one member, got %v", r.spawned)
	}
	conf := r.spawned[0]
	if conf.Kind != contract.KindConflict || conf.Container {
		t.Errorf("must raise a kind:conflict leaf (Franky-owned AFK), got %+v", conf)
	}
	if base, ok := git.ParseBase(conf.Description); !ok || base != "basesha" {
		t.Errorf("the conflict member must carry the Base: pin, got %q", conf.Description)
	}
	// The Tip the failed land pinned: Franky's effort is cut there and replace-CASes it.
	if tip, ok := git.ParseTip(conf.Description); !ok || tip != "cafef00d" {
		t.Errorf("the conflict member must carry the Checkpoint's Tip: pin, got %q", conf.Description)
	}
	if want := "Checkpoint ws-cp conflicts with main"; conf.Title != want {
		t.Errorf("conflict title = %q, want %q (no kind prefix)", conf.Title, want)
	}
	if len(r.closed) != 0 {
		t.Errorf("a conflicted land must NOT close the Checkpoint, got %v", r.closed)
	}
}

// ADR 0030/0069: Land's *ADRClash files exactly one Franky-owned kind:conflict member
// with the clash-variant body (the colliding ADRs, the Base: pin), and doesn't land:
// the Checkpoint stays open and the error doesn't propagate.
func TestADRClashRaisesClashVariantConflictBead(t *testing.T) {
	clash := "apps/mugicrew/docs/adr/0031-cp.md (vs mainline apps/mugicrew/docs/adr/0031-main.md)"
	r := &jinbeSeamRec{approvalBody: "Commit: cafef00d", head: "cafef00d", landErr: &git.ADRClash{Clashes: []string{clash}, Base: "basesha"}}
	stubJinbeSeams(t, r)

	if err := reconcileCheckpoint(openCheckpoint()); err != nil {
		t.Fatalf("an ADR clash must NOT propagate (it becomes a bead): %v", err)
	}
	if len(r.spawned) != 1 {
		t.Fatalf("a clash must raise exactly one member, got %v", r.spawned)
	}
	conf := r.spawned[0]
	if conf.Kind != contract.KindConflict || conf.Container {
		t.Errorf("must raise a kind:conflict leaf (Franky-owned AFK), got %+v", conf)
	}
	if base, ok := git.ParseBase(conf.Description); !ok || base != "basesha" {
		t.Errorf("the conflict member must carry the Base: pin, got %q", conf.Description)
	}
	if !strings.Contains(conf.Description, "ADR-number clash") || !strings.Contains(conf.Description, clash) {
		t.Errorf("want the clash-variant body naming %q, got %q", clash, conf.Description)
	}
	if len(r.closed) != 0 {
		t.Errorf("a clashed land must NOT close the Checkpoint, got %v", r.closed)
	}
}

// ADR 0069: a dirty main is not a conflict. *MainDirty from the land files no
// conflict member, leaves the Checkpoint open, and is the returned error, recorded
// on land.failed for the next tick to retry.
func TestMainDirtyLandFilesNoBead(t *testing.T) {
	evs := eventlogtest.Capture(t)
	r := &jinbeSeamRec{approvalBody: "Commit: cafef00d", head: "cafef00d", landErr: &git.MainDirty{Path: "/repo"}}
	stubJinbeSeams(t, r)

	err := reconcileCheckpoint(openCheckpoint())
	var dirty *git.MainDirty
	if !errors.As(err, &dirty) {
		t.Fatalf("reconcileCheckpoint = %v, want *MainDirty", err)
	}
	if len(r.spawned) != 0 || len(r.closed) != 0 {
		t.Errorf("a dirty main must file no bead and close nothing, spawned %v closed %v", r.spawned, r.closed)
	}
	if len(*evs) != 1 || (*evs)[0].Name != "land.failed" || !strings.Contains(fmt.Sprint((*evs)[0].Attrs["err"]), "clean main, then re-run") {
		t.Errorf("events = %+v, want one land.failed carrying the *MainDirty", *evs)
	}
}

// Run(checkpoint) re-reads the bead and drives the land; a non-checkpoint kind is a
// loud error, mirroring Franky.Run.
func TestRunLandsCheckpointAndRejectsOtherKinds(t *testing.T) {
	r := &jinbeSeamRec{approvalBody: "Commit: cafef00d", head: "cafef00d"}
	stubJinbeSeams(t, r)
	origShow := showBead
	t.Cleanup(func() { showBead = origShow })
	showBead = func(string) (bd.Bead, error) { return openCheckpoint(), nil }

	if err := (Jinbe{}).Run(stubCtx{}, contract.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(r.landed) != 1 || len(r.closed) != 1 {
		t.Errorf("Run must land then close, got landed=%v closed=%v", r.landed, r.closed)
	}

	showBead = func(id string) (bd.Bead, error) {
		return bd.Bead{ID: id, Kind: contract.KindBuild, Status: "open"}, nil
	}
	if err := (Jinbe{}).Run(stubCtx{}, contract.Bead{ID: "x", Kind: contract.KindBuild}); err == nil {
		t.Error("a kind Jinbe doesn't own must error")
	}
}

// TestMain keeps stderr quiet (slog's default handler); use eventlogtest.Capture
// to assert events.
func TestMain(m *testing.M) {
	eventlogtest.Discard()
	os.Exit(m.Run())
}

// The Checkpoint land records land.done with branch/into/sha, and a rebase
// conflict land.failed at Error.
func TestLandIsAnEvent(t *testing.T) {
	for _, tc := range []struct {
		landErr error
		want    string
		level   slog.Level
	}{
		{nil, "land.done", slog.LevelInfo},
		{&git.RebaseConflict{Base: "b"}, "land.failed", slog.LevelError},
	} {
		evs := eventlogtest.Capture(t)
		stubJinbeSeams(t, &jinbeSeamRec{approvalBody: "Commit: cafef00d", head: "cafef00d", landErr: tc.landErr})
		reconcileCheckpoint(openCheckpoint())
		if len(*evs) != 1 || (*evs)[0].Name != tc.want || (*evs)[0].Level != tc.level {
			t.Fatalf("events = %+v, want %s first", *evs, tc.want)
		}
		if a := (*evs)[0].Attrs; a["bead"] != "ws-cp" || a["branch"] != "checkpoint/ws-cp" || a["into"] != git.Mainline() {
			t.Errorf("%s attrs = %v", tc.want, a)
		}
	}
}

// rollbackRec stubs the Rollback legs as a small stateful world — the related
// target, the target bead, its land trailer, the revert on main, the successor, its
// branch and seed, its Build — so a re-run sees what the last run wrote. failAt
// names one leg to fail once (a crash there), for the resume tests.
type rollbackRec struct {
	jinbeSeamRec
	target    string      // RollbackTarget; "" → no related Checkpoint
	targetSt  string      // the target Checkpoint's status
	onMain    bool        // the target's Checkpoint: trailer is on main
	noSpec    bool        // the target resolves no Spec
	body      string      // the Rollback's body (breakage evidence)
	revertErr error       // non-nil → git.Revert failure
	failAt    string      // revert|successor|cut|reapply|build|close: fail that leg once
	reverted  [][2]string // (squash, trailer) per Revert call
	reasons   map[string]string

	reverts    int               // revert commits written to main
	successors []bd.Successor    // successor Checkpoints spawned
	branches   map[string]string // branch → tip
	seeds      int               // reapply commits written
	builds     []bd.Successor    // Build members spawned, open or closed
}

func (r *rollbackRec) crash(leg string) error {
	if r.failAt == leg {
		r.failAt = ""
		return errors.New("crash at " + leg)
	}
	return nil
}

func stubRollbackSeams(t *testing.T, r *rollbackRec) {
	t.Helper()
	stubJinbeSeams(t, &r.jinbeSeamRec)
	oTarget, oShow, oRevert, oSpec, oSucc, oSuccOf, oCut, oReapply, oBuilds := rollbackTarget, showBead, revertCommit, specOf, spawnSuccessor, successorOf, cutBranch, reapply, buildChildren
	t.Cleanup(func() {
		rollbackTarget, showBead, revertCommit, specOf, spawnSuccessor, successorOf, cutBranch, reapply, buildChildren = oTarget, oShow, oRevert, oSpec, oSucc, oSuccOf, oCut, oReapply, oBuilds
	})
	r.reasons = map[string]string{}
	r.branches = map[string]string{}
	closeBead = func(id, reason string) error {
		if err := r.crash("close"); err != nil {
			return err
		}
		r.closed = append(r.closed, id)
		r.reasons[id] = reason
		return nil
	}
	rollbackTarget = func(string) (string, bool, error) { return r.target, r.target != "", nil }
	showBead = func(id string) (bd.Bead, error) {
		if id == r.target {
			return bd.Bead{ID: id, Kind: contract.KindCheckpoint, Status: r.targetSt}, nil
		}
		if id == "ws-new" {
			return bd.Bead{ID: id, Kind: contract.KindCheckpoint, Status: "open", Description: r.successors[0].Description}, nil
		}
		return r.rollback(), nil
	}
	successorOf = func(pred string, _ contract.Kind) (string, bool, error) {
		return "ws-new", pred == "ws-rb" && len(r.successors) > 0, nil
	}
	landedBase = func(trailer string) (string, error) {
		if r.onMain && trailer == "Checkpoint: "+r.target {
			return "squashsha", nil
		}
		return "", git.ErrNotLanded
	}
	revertCommit = func(squash, trailer string) (string, error) {
		r.reverted = append(r.reverted, [2]string{squash, trailer})
		if r.revertErr != nil {
			return "", r.revertErr
		}
		if r.reverts == 0 { // trailer-idempotent, like git.Revert
			r.reverts++
		}
		return "revertsha", r.crash("revert")
	}
	specOf = func(string) (string, bool, error) { return "ws-spec", !r.noSpec, nil }
	spawnSuccessor = func(pred string, s bd.Successor) (string, bool, error) {
		if pred != "ws-rb" {
			t.Errorf("successor spawned caused-by %s, want the Rollback ws-rb", pred)
		}
		if len(r.successors) == 0 {
			r.successors = append(r.successors, s)
		}
		return "ws-new", true, r.crash("successor")
	}
	cutBranch = func(name, base string) (bool, error) {
		if _, ok := r.branches[name]; !ok {
			r.branches[name] = base
		}
		return true, r.crash("cut")
	}
	reapply = func(branch, revert string) (string, error) {
		if r.branches[branch] == revert {
			r.branches[branch] = "seedsha"
			r.seeds++
		}
		return "seedsha", r.crash("reapply")
	}
	spawnChild = func(parent string, s bd.Successor) (string, bool, error) {
		if parent != "ws-new" {
			t.Errorf("Build spawned under %s, want the successor ws-new", parent)
		}
		r.builds = append(r.builds, s) // no open-guard: a closed Build must be caught by Jinbe
		return "ws-new.1", true, r.crash("build")
	}
	buildChildren = func(string) (int, error) { return len(r.builds), nil }
}

// rollback is the Rollback bead as the graph holds it now: closed once closeBead ran.
func (r *rollbackRec) rollback() bd.Bead {
	rb := openRollback()
	rb.Description = r.body
	if len(r.closed) > 0 {
		rb.Status = "closed"
	}
	return rb
}

// assertHandedOff checks the full clean-revert end state (ADR 0048): one revert,
// one successor caused-by the Rollback, its branch cut at the revert and seeded
// once, one Build carrying the evidence, and the Rollback closed once.
func assertHandedOff(t *testing.T, r *rollbackRec) {
	t.Helper()
	if r.reverts != 1 {
		t.Errorf("revert commits = %d, want 1", r.reverts)
	}
	if len(r.successors) != 1 || r.successors[0].Kind != contract.KindCheckpoint || !r.successors[0].Container {
		t.Errorf("successors = %+v, want one container Checkpoint", r.successors)
	} else if !strings.Contains(r.successors[0].Description, "Spec: ws-spec") {
		t.Errorf("successor body %q must state the Spec", r.successors[0].Description)
	}
	if tip := r.branches["checkpoint/ws-new"]; tip != "seedsha" || r.seeds != 1 {
		t.Errorf("checkpoint/ws-new tip %q seeds %d, want cut at the revert and seeded once", tip, r.seeds)
	}
	if len(r.builds) != 1 || r.builds[0].Kind != contract.KindBuild || r.builds[0].Description != r.body {
		t.Errorf("builds = %+v, want one Build carrying %q", r.builds, r.body)
	} else if want := "Fix what " + r.target + " broke on main"; r.builds[0].Title != want {
		t.Errorf("Build title = %q, want %q (no kind prefix)", r.builds[0].Title, want)
	}
	if len(r.closed) != 1 || r.closed[0] != "ws-rb" || r.reasons["ws-rb"] != "" {
		t.Errorf("closed %v %v, want the Rollback closed once, no reason", r.closed, r.reasons)
	}
}

func openRollback() bd.Bead {
	return bd.Bead{ID: "ws-rb", Kind: contract.KindRollback, Status: "in_progress", Description: "main broke because …"}
}

// Invalid targets close the Rollback "invalid: <why>" and write nothing to main.
func TestRollbackInvalidTargetsCloseWithoutReverting(t *testing.T) {
	for _, tc := range []struct {
		name   string
		r      rollbackRec
		reason string
	}{
		{"no related", rollbackRec{}, "invalid: no related Checkpoint"},
		{"open target", rollbackRec{target: "ws-cp", targetSt: "open", onMain: true}, "invalid: ws-cp not landed"},
		{"no trailer", rollbackRec{target: "ws-cp", targetSt: "closed"}, "invalid: ws-cp has no land trailer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.r
			stubRollbackSeams(t, &r)
			if err := reconcileRollback(openRollback()); err != nil {
				t.Fatalf("reconcileRollback: %v", err)
			}
			if len(r.reverted) != 0 {
				t.Errorf("an invalid Rollback must not touch main, got reverts %v", r.reverted)
			}
			if len(r.closed) != 1 || r.closed[0] != "ws-rb" || r.reasons["ws-rb"] != tc.reason {
				t.Errorf("closed %v reasons %v, want ws-rb %q", r.closed, r.reasons, tc.reason)
			}
		})
	}
}

// A landed target is reverted once, keyed on the Reverts-Checkpoint: trailer, then
// handed off to a seeded successor with its Build, and only then the Rollback closes.
func TestRollbackRevertsThenHandsOff(t *testing.T) {
	r := rollbackRec{target: "ws-cp", targetSt: "closed", onMain: true, body: "main broke because …"}
	stubRollbackSeams(t, &r)
	if err := reconcileRollback(r.rollback()); err != nil {
		t.Fatalf("reconcileRollback: %v", err)
	}
	if len(r.reverted) != 1 || r.reverted[0] != [2]string{"squashsha", "Reverts-Checkpoint: ws-cp"} {
		t.Fatalf("want one revert of the squash with the trailer, got %v", r.reverted)
	}
	assertHandedOff(t, &r)
}

// Rollback ignores Tasks (Spec D9, FR 40): a Task the target's land
// released is never read, reopened, closed or named in the handoff.
func TestRollbackIgnoresReleasedTask(t *testing.T) {
	r := rollbackRec{target: "ws-cp", targetSt: "closed", onMain: true, body: "main broke because …"}
	stubRollbackSeams(t, &r)
	show := showBead
	showBead = func(id string) (bd.Bead, error) {
		if id == "ws-task" {
			t.Errorf("Rollback read the released Task %s", id)
		}
		return show(id)
	}
	if err := reconcileRollback(r.rollback()); err != nil {
		t.Fatalf("reconcileRollback: %v", err)
	}
	assertHandedOff(t, &r)
	if got := fmt.Sprint(r.closed, r.successors, r.builds); strings.Contains(got, "ws-task") {
		t.Errorf("the handoff names the released Task: %s", got)
	}
}

// ADR 0003/0048 §3: a crash at any leg resumes on re-dispatch with no duplicate
// revert, successor, seed or Build, and the Rollback stays open until the handoff
// is whole.
func TestRollbackResumesFromEveryLeg(t *testing.T) {
	for _, leg := range []string{"revert", "successor", "cut", "reapply", "build", "close"} {
		t.Run(leg, func(t *testing.T) {
			r := rollbackRec{target: "ws-cp", targetSt: "closed", onMain: true, body: "evidence", failAt: leg}
			stubRollbackSeams(t, &r)
			if err := reconcileRollback(r.rollback()); err == nil {
				t.Fatal("the crashed run must report its error")
			}
			if len(r.closed) != 0 {
				t.Fatalf("a crash before the close must leave the Rollback open, closed %v", r.closed)
			}
			for range 2 { // the resume, then a stray re-dispatch of the closed Rollback
				if err := reconcileRollback(r.rollback()); err != nil {
					t.Fatalf("resume: %v", err)
				}
			}
			assertHandedOff(t, &r)
		})
	}
}

// The Build body is the Rollback's evidence, or — empty — reproduce-then-fix at the
// target's land.
func TestBuildBody(t *testing.T) {
	tail := "\n\nTarget squash: sq\nConflicted paths: a.go, b.go" +
		"\nThe revert was skipped because later lands built on it (git log sq..HEAD -- <paths>)."
	for _, c := range []struct {
		name, got, want string
	}{
		{"clean/evidence", buildBody("main broke because x", "ws-cp", "sq"), "main broke because x"},
		{"clean/empty", buildBody(" \n", "ws-cp", "sq"), "Reproduce the breakage on main at ws-cp's land (sq), then fix it."},
		{"fix-forward/evidence", fixForwardBody("main broke because x", "sq", []string{"a.go", "b.go"}), "main broke because x" + tail},
		{"fix-forward/empty", fixForwardBody(" \n", "sq", []string{"a.go", "b.go"}), "Reproduce the breakage on main HEAD, then fix it." + tail},
	} {
		if c.got != c.want {
			t.Errorf("%s: body = %q\nwant %q", c.name, c.got, c.want)
		}
	}
}

// ADR 0069: a dirty main refusing the revert is not a conflict, so the Rollback
// does not fix forward: nothing is handed off, the Rollback stays open, and the
// *MainDirty is the returned error, recorded on revert.failed.
func TestRollbackRevertMainDirtyFilesNoBead(t *testing.T) {
	evs := eventlogtest.Capture(t)
	r := rollbackRec{target: "ws-cp", targetSt: "closed", onMain: true, body: "evidence", revertErr: &git.MainDirty{Path: "/repo"}}
	stubRollbackSeams(t, &r)
	err := reconcileRollback(r.rollback())
	var dirty *git.MainDirty
	if !errors.As(err, &dirty) {
		t.Fatalf("reconcileRollback = %v, want *MainDirty", err)
	}
	if len(r.successors) != 0 || len(r.builds) != 0 || len(r.closed) != 0 {
		t.Errorf("a dirty main must hand nothing off (no fix-forward): successors %v builds %v closed %v", r.successors, r.builds, r.closed)
	}
	if len(*evs) != 1 || (*evs)[0].Name != "revert.failed" || !strings.Contains(fmt.Sprint((*evs)[0].Attrs["err"]), "clean main, then re-run") {
		t.Errorf("events = %+v, want one revert.failed carrying the *MainDirty", *evs)
	}
}

// ADR 0049: a conflicting revert fixes forward — nothing reverted, the successor cut
// at main HEAD with no seed, one Build carrying the evidence, the squash, the
// conflicted paths and the skipped-revert line, and the Rollback closed
// "fix-forward: revert conflicted". A crash at any leg resumes (past the successor
// spawn the decision is pinned on it) with no duplicate successor or Build.
func TestRollbackRevertConflictFixesForward(t *testing.T) {
	for _, leg := range []string{"", "successor", "cut", "build", "close"} {
		for _, body := range []string{"evidence", ""} {
			t.Run(leg+"/"+body, func(t *testing.T) {
				r := rollbackRec{target: "ws-cp", targetSt: "closed", onMain: true, body: body, failAt: leg,
					revertErr: &git.RevertConflict{Base: "mainhead", Paths: []string{"a.go", "b.go"}}}
				stubRollbackSeams(t, &r)
				if err := reconcileRollback(r.rollback()); (err != nil) != (leg != "") {
					t.Fatalf("first run err = %v (crash at %q)", err, leg)
				}
				for range 2 { // the resume (or a stray re-dispatch), then one more
					if err := reconcileRollback(r.rollback()); err != nil {
						t.Fatalf("resume: %v", err)
					}
				}
				if r.reverts != 0 || r.seeds != 0 {
					t.Errorf("reverts %d seeds %d, want nothing written to main and no seed", r.reverts, r.seeds)
				}
				if len(r.successors) != 1 || r.branches["checkpoint/ws-new"] != "mainhead" {
					t.Errorf("successors %d branch %v, want one successor cut at main HEAD", len(r.successors), r.branches)
				}
				if len(r.successors) == 1 && !strings.Contains(r.successors[0].Description, "fixing forward") {
					t.Errorf("successor body %q must say it fixes forward, not reverted", r.successors[0].Description)
				}
				if len(r.builds) != 1 {
					t.Fatalf("builds = %+v, want one", r.builds)
				}
				d := r.builds[0].Description
				want := []string{"squashsha", "a.go, b.go", "revert was skipped because later lands built on it"}
				if body == "" {
					want = append(want, "Reproduce the breakage on main HEAD, then fix it.")
				} else {
					want = append(want, body)
				}
				for _, w := range want {
					if !strings.Contains(d, w) {
						t.Errorf("Build body %q missing %q", d, w)
					}
				}
				if len(r.closed) != 1 || r.reasons["ws-rb"] != "fix-forward: revert conflicted" {
					t.Errorf("closed %v %v, want ws-rb closed once fix-forward", r.closed, r.reasons)
				}
			})
		}
	}
}

// Once a fix-forward pass has spawned its successor, the decision is pinned
// on it. If main then changes so the revert would apply cleanly (a human reverted the
// later land), the resume still fixes forward — no revert on main, the Build still
// carries the conflicted paths, and the Rollback closes "fix-forward: revert conflicted".
func TestRollbackFixForwardResumeStaysFixForward(t *testing.T) {
	for _, leg := range []string{"successor", "cut", "build", "close"} {
		t.Run(leg, func(t *testing.T) {
			r := rollbackRec{target: "ws-cp", targetSt: "closed", onMain: true, body: "evidence", failAt: leg,
				revertErr: &git.RevertConflict{Base: "mainhead", Paths: []string{"a.go", "b.go"}}}
			stubRollbackSeams(t, &r)
			if err := reconcileRollback(r.rollback()); err == nil {
				t.Fatal("the crashed run must report its error")
			}
			r.revertErr = nil // main changed: the revert would now apply cleanly
			if err := reconcileRollback(r.rollback()); err != nil {
				t.Fatalf("resume: %v", err)
			}
			if r.reverts != 0 || r.seeds != 0 {
				t.Errorf("reverts %d seeds %d, want the resume to keep fixing forward", r.reverts, r.seeds)
			}
			if r.branches["checkpoint/ws-new"] != "mainhead" {
				t.Errorf("branches %v, want the successor cut at the pinned main HEAD", r.branches)
			}
			if len(r.builds) != 1 || !strings.Contains(r.builds[0].Description, "a.go, b.go") {
				t.Errorf("builds = %+v, want one fix-forward Build carrying the conflicted paths", r.builds)
			}
			if len(r.closed) != 1 || r.reasons["ws-rb"] != "fix-forward: revert conflicted" {
				t.Errorf("closed %v %v, want ws-rb closed once fix-forward", r.closed, r.reasons)
			}
		})
	}
}

// A failed revert that is not a conflict is loud and leaves the Rollback open; a
// manual unclaim and re-dispatch converges (ADR 0052: no lease).
func TestRollbackRevertErrorLeavesOpen(t *testing.T) {
	r := rollbackRec{target: "ws-cp", targetSt: "closed", onMain: true, revertErr: errors.New("dirty tree")}
	stubRollbackSeams(t, &r)
	if err := reconcileRollback(openRollback()); err == nil {
		t.Fatal("a failed revert must propagate")
	}
	if len(r.closed) != 0 {
		t.Errorf("a failed revert must not close the Rollback, got %v", r.closed)
	}
}

// The revert records revert.done / revert.failed (Spec FR42), the land.done twin —
// or revert.conflict for a planned fix-forward (ADR 0049) — and the handoff
// handoff.done with the successor.
func TestRevertIsAnEvent(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{{nil, "revert.done"}, {errors.New("x"), "revert.failed"}, {&git.RevertConflict{Base: "b", Paths: []string{"a.go"}}, "revert.conflict"}} {
		evs := eventlogtest.Capture(t)
		stubRollbackSeams(t, &rollbackRec{target: "ws-cp", targetSt: "closed", onMain: true, revertErr: tc.err})
		reconcileRollback(openRollback())
		if len(*evs) < 1 || (*evs)[0].Name != tc.want {
			t.Fatalf("events = %+v, want %s first", *evs, tc.want)
		}
		if a := (*evs)[0].Attrs; a["bead"] != "ws-rb" || a["target"] != "ws-cp" || a["into"] != git.Mainline() {
			t.Errorf("%s attrs = %v", tc.want, a)
		}
		if tc.want != "revert.failed" && (len(*evs) != 2 || (*evs)[1].Name != "handoff.done" || (*evs)[1].Attrs["successor"] != "ws-new") {
			t.Errorf("a clean or fix-forward revert must then record handoff.done to ws-new, events = %+v", *evs)
		}
	}
}

// Run dispatches on kind: a Rollback reaches reconcileRollback.
func TestRunDispatchesRollback(t *testing.T) {
	r := rollbackRec{target: "ws-cp", targetSt: "closed", onMain: true}
	stubRollbackSeams(t, &r)
	if err := (Jinbe{}).Run(stubCtx{}, contract.Bead{ID: "ws-rb", Kind: contract.KindRollback}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(r.reverted) != 1 || len(r.closed) != 1 {
		t.Errorf("Run(rollback) must revert then close, got %v %v", r.reverted, r.closed)
	}
}

// A closed Rollback is done (handed off, or invalid): a stray re-dispatch writes
// nothing — no revert, no second successor or Build, no second close.
func TestClosedRollbackIsNoop(t *testing.T) {
	r := rollbackRec{target: "ws-cp", targetSt: "closed", onMain: true}
	stubRollbackSeams(t, &r)
	rb := openRollback()
	rb.Status = "closed"
	if err := reconcileRollback(rb); err != nil {
		t.Fatalf("reconcileRollback: %v", err)
	}
	if len(r.reverted) != 0 || len(r.successors) != 0 || len(r.builds) != 0 || len(r.closed) != 0 {
		t.Errorf("a closed Rollback must write nothing, got reverts %v successors %v builds %v closed %v", r.reverted, r.successors, r.builds, r.closed)
	}
}

// A drained Checkpoint with no Approval at head gets exactly one bare Review from
// Jinbe (Spec D8, FR 29), and a re-run spawns no second one.
func TestDrainedCheckpointGetsOneReview(t *testing.T) {
	r := &jinbeSeamRec{kids: []bd.Bead{{ID: "b1", Kind: contract.KindBuild, Status: "closed"}}}
	stubJinbeSeams(t, r)
	for range 2 {
		if err := reconcileCheckpoint(openCheckpoint()); err != nil {
			t.Fatalf("reconcileCheckpoint: %v", err)
		}
	}
	if len(r.spawned) != 1 {
		t.Fatalf("spawned %d, want exactly one Review", len(r.spawned))
	}
	rev := r.spawned[0]
	if rev.Kind != contract.KindReview || rev.Container {
		t.Errorf("Review must be a kind:review leaf, got %+v", rev)
	}
	if want := "Built slice of checkpoint ws-cp"; rev.Title != want {
		t.Errorf("Review title = %q, want %q", rev.Title, want)
	}
	if _, hasPin := git.ParseCommit(rev.Description); hasPin {
		t.Errorf("Review must be spawned bare (Usopp pins HEAD), got %q", rev.Description)
	}
	if len(r.landed)+len(r.closed) != 0 {
		t.Errorf("an unapproved Checkpoint must not land or close, got landed=%v closed=%v", r.landed, r.closed)
	}
}

// A closed member still holding effort/<id>, or an open pivot, holds the Review
// (FRs 28–29): Jinbe spawns nothing and waits.
func TestReviewHeldByLiveRefOrPivot(t *testing.T) {
	built := []bd.Bead{{ID: "b1", Kind: contract.KindBuild, Status: "closed"}}
	for name, r := range map[string]*jinbeSeamRec{
		"live effort ref": {kids: built, efforts: map[string]bool{"b1": true}},
		"open pivot":      {kids: built, pivots: []bd.Bead{{ID: "p1", Kind: contract.KindPivot, Status: "open"}}},
	} {
		stubJinbeSeams(t, r)
		if err := reconcileCheckpoint(openCheckpoint()); err != nil {
			t.Fatalf("%s: reconcileCheckpoint: %v", name, err)
		}
		if len(r.spawned) != 0 {
			t.Errorf("%s: spawned %v, want no Review", name, r.spawned)
		}
	}
}

// A tip moved past a stale Approval (a late integrate) gets a fresh Review (FR 31);
// one at head lands instead.
func TestStaleApprovalGetsFreshReview(t *testing.T) {
	r := &jinbeSeamRec{
		head:         "newtip",
		approvalBody: "Commit: oldpin",
		kids: []bd.Bead{
			{ID: "b1", Kind: contract.KindBuild, Status: "closed"},
			{ID: "a1", Kind: contract.KindApproval, Status: "closed", Description: "Commit: oldpin"},
		},
	}
	stubJinbeSeams(t, r)
	if err := reconcileCheckpoint(openCheckpoint()); err != nil {
		t.Fatalf("reconcileCheckpoint: %v", err)
	}
	if len(r.landed) != 0 || len(r.spawned) != 1 || r.spawned[0].Kind != contract.KindReview {
		t.Errorf("landed=%v spawned=%v; want no land and one fresh Review", r.landed, r.spawned)
	}
}
