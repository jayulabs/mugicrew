package jinbe

import (
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/nami"
)

type stubCtx struct{}

func (stubCtx) Harness() contract.Harness { return nil }

// closeAfterMR stubs closeBead to record the close and flag any bead closed before the MR.
// Call it after stubJinbeSeams, which installs the default closeBead.
func (r *jinbeSeamRec) closeAfterMR(t *testing.T) {
	closeBead = func(id, reason string) error {
		if len(r.mrsClosed) != 1 {
			t.Errorf("%s closed before the MR", id)
		}
		r.closed = append(r.closed, id)
		return nil
	}
}

// jinbeSeamRec captures the bd/git legs so the tests can assert the approvedAtHead
// gate, the land-trailer idempotency roll-forward, and the conflict-raise path
// without live bd/git. Mirrors usoppSeamRec.
type jinbeSeamRec struct {
	trailerLanded  bool                // landedBase(checkpointTrailer) succeeds → already landed
	foreign        *git.ForeignLanding // non-nil → landedBase refuses a foreign land
	landedErr      error               // non-nil → landedBase fails with it (not a miss)
	taskBody       string
	tasks          []string        // blocking Task keys bd.SpawnBlockingTask was asked for, deduped
	approvalBody   string          // one closed Approval's body; "" → no closed Approval (single-approval shorthand)
	approvalBodies []string        // >1 closed Approval bodies in creation order; wins over approvalBody
	rewritten      bool            // the landed Tip: is not an ancestor of head
	head           string          // the checkpoint branch tip approvedAtHead compares the pin to
	landErr        error           // non-nil → git.Land failure (a *git.RebaseConflict raises a bead)
	landed         []string        // branches landBranch was called for
	strategyErr    error           // non-nil → landStrategy fails (an unusable land_strategy)
	landings       []bd.Bead       // the container's Landings landingsOf returns
	reasons        []string        // close reasons, parallel to closed
	spawned        []bd.Successor  // members spawned (Landing, Conflict or Review)
	closed         []string        // checkpoints closed
	kids           []bd.Bead       // the Checkpoint's children ReviewDue reads; a spawn adds an open one
	efforts        map[string]bool // ids holding effort/<id>
	pivots         []bd.Bead       // the Checkpoint's open pivots
	blockers       []bd.Bead       // the Checkpoint's open incoming blockers (the land hold)

	gated          bool               // land_strategy resolves to local (the gate applies to a Checkpoint)
	composeErr     error              // non-nil → composeLanding fails
	gateErr        error              // non-nil → gateBuild fails
	landErrs       []error            // gated: sameBaseLand outcomes per attempt, then success
	composes       int                // composeLanding calls
	composePackets []string           // the Decision Packet composeLanding saw per call
	composePins    []string           // composeLanding's pin per call (default r.head); a moved container
	gates          int                // gateBuild calls
	sbLands        int                // landBranch calls carrying the same-base strategy
	fetches        int                // fetchForge calls
	presentation   git.MRPresentation // the presentation pinnedGitlab last received (nestedGitlab)
	fetchRepos     []git.Repo         // the repo each fetchForge call named
	projects       []forge.Project    // the project each Forge seam call named, in order
	fetchErr       error              // non-nil → fetchForge fails
	fetchLands     bool               // a successful fetch brings the trailer onto main
	landsOnFetch   int                // the Nth fetch (1-based) brings the trailer onto main
	bodies         []string           // Landing bodies written by updateBody
	mr             *forge.MR          // forgeMR's snapshot
	mrErr          error              // non-nil → forgeMR fails
	mrReads        int                // forgeMR calls
	descAsserts    []string           // pins forgeAssertDesc was asked to assert
	jobReads       int                // forgeJobs calls
	assertErr      error              // non-nil → forgeAssertDesc fails
	retries        []int              // forgeRetry calls, one per POST
	jobs           []forge.Job
	retryErr       error // non-nil → forgeRetry fails
	jobsErr        error // non-nil → forgeJobs fails
	targetSHA      string
	sourceSHA      string   // forgeSource: the merged-results commit's second parent
	landedTip      string   // the Tip: of the landed squash; "" → none
	signals        []string // offshoot Signal bodies spawnSignal filed

	discussions     []forge.Discussion // forgeDiscussions' answer
	discussionsErr  error
	discussionsRead bool          // forgeDiscussions was called
	bots            map[int]bool  // author ids forgeIsBot reports as bots
	botReads        []int         // forgeIsBot calls
	botErrs         map[int]error // author ids forgeIsBot fails for
	threadKids      []bd.Bead     // the container's children of the filed kind, any status
	threadSpawned   []bd.Successor
	sends           []string // "reply <disc> <body>" / "resolve <disc>", in order
	replyErr        error
	resolveErr      error
	integrateSHA    string // newestByTrailer's answer

	probeErr   error      // the pre-review conflict probe's verdict: nil clean, *RebaseConflict/*NumberClash, else a git failure
	probeCalls int        // conflict probes run
	probeRepos []git.Repo // the repo each probe's base was resolved in

	clashes   []git.PostLandClash // postLandClashes' answer: the landed container is the later side
	clashErr  error
	clashRead int // postLandClashes calls

	openMR     *forge.MR // forgeFindOpenMR's answer: the MR open on the branch, nil when none
	findErr    error     // non-nil → forgeFindOpenMR fails
	closeMRErr error     // non-nil → forgeCloseMR fails
	mrsClosed  []int     // forgeCloseMR iids
}

func stubJinbeSeams(t *testing.T, r *jinbeSeamRec) {
	t.Helper()
	oClose, oSpawn, oClosedChildren, oLand, oLandedBase, oApproved, oDue, oHead, oDrained, oBlockers := closeBead, spawnChild, closedChildrenOfKind, landBranch, landedBase, approvedAtHead, reviewDue, branchHead, membersDrained, openBlockers
	oProbe, oProbeBase := probeConflict, probeBase
	t.Cleanup(func() { probeConflict, probeBase = oProbe, oProbeBase })
	oClash, oCausedBy, oSubtree := postLandClashes, causedBy, subtreeIDs
	subtreeIDs = func(string) ([]string, error) { return nil, nil }
	t.Cleanup(func() { subtreeIDs = oSubtree })
	causedBy = func(string, contract.Kind) (string, bool, error) { return "", false, nil }
	t.Cleanup(func() { causedBy = oCausedBy })
	oJira := jiraPrefix
	jiraPrefix = func(bd.Bead) (string, error) { return "", nil }
	t.Cleanup(func() { jiraPrefix = oJira })
	t.Cleanup(func() { postLandClashes = oClash })
	postLandClashes = func(string) ([]git.PostLandClash, error) { r.clashRead++; return r.clashes, r.clashErr }
	probeBase = func(repo git.Repo) (string, error) { r.probeRepos = append(r.probeRepos, repo); return "basesha", nil }
	probeConflict = func(git.Repo, string, string) error { r.probeCalls++; return r.probeErr }
	oStrategy, oLandings, oShow := landStrategy, landingsOf, showBead
	oMR := forgeMR
	oDisc, oBot, oKids, oThread := forgeDiscussions, forgeIsBot, childrenOfKind, spawnThreadBead
	botCache.Clear()
	t.Cleanup(notesSeen.Clear) // a Landing's first tick reads ungated
	oGPush, oGTask := graphPush, spawnUnblockingTask
	t.Cleanup(func() { graphPush, spawnUnblockingTask = oGPush, oGTask })
	// A Graph push must never reach the real bd or Forge; graphPushRec replaces these.
	graphPush = func(string) (bd.GraphPushResult, error) {
		t.Errorf("unstubbed Graph push: use graphPushRec")
		return bd.GraphPushResult{}, nil
	}
	spawnUnblockingTask = func(string, string, string) (string, bool, error) {
		t.Errorf("unstubbed unblocking Task: use graphPushRec")
		return "", false, nil
	}
	oReply, oResolve, oUser, oNewest := forgeReply, forgeResolve, forgeUser, newestByTrailer
	graphName.name = ""
	t.Cleanup(func() {
		forgeDiscussions, forgeIsBot, childrenOfKind, spawnThreadBead = oDisc, oBot, oKids, oThread
		forgeReply, forgeResolve, forgeUser, newestByTrailer = oReply, oResolve, oUser, oNewest
	})
	forgeReply = func(p forge.Project, _ int, d, body string) error {
		r.projects = append(r.projects, p)
		r.sends = append(r.sends, "reply "+d+" "+body)
		return r.replyErr
	}
	forgeResolve = func(p forge.Project, _ int, d string) error {
		r.projects = append(r.projects, p)
		r.sends = append(r.sends, "resolve "+d)
		return r.resolveErr
	}
	forgeUser = func() (string, error) { return "jinbe", nil }
	newestByTrailer = func(git.Repo, string, string) (string, error) { return r.integrateSHA, nil }
	git.SetGraphID("g00d")
	forgeDiscussions = func(p forge.Project, _ int) ([]forge.Discussion, error) {
		r.projects = append(r.projects, p)
		r.discussionsRead = true
		return r.discussions, r.discussionsErr
	}
	forgeIsBot = func(p forge.Project, id int) (bool, error) {
		r.projects = append(r.projects, p)
		r.botReads = append(r.botReads, id)
		return r.bots[id], r.botErrs[id]
	}
	childrenOfKind = func(_ string, k contract.Kind) ([]bd.Bead, error) {
		return slices.DeleteFunc(slices.Clone(r.threadKids), func(b bd.Bead) bool { return b.Kind != k }), nil
	}
	spawnThreadBead = func(_ string, s bd.Successor) (string, error) {
		r.threadSpawned = append(r.threadSpawned, s)
		return "ws-thread", nil
	}
	oFind, oCloseMR := forgeFindOpenMR, forgeCloseMR
	t.Cleanup(func() { forgeFindOpenMR, forgeCloseMR = oFind, oCloseMR })
	forgeFindOpenMR = func(p forge.Project, _ string) (*forge.MR, error) {
		r.projects = append(r.projects, p)
		return r.openMR, r.findErr
	}
	forgeCloseMR = func(p forge.Project, iid int) error {
		r.projects = append(r.projects, p)
		if r.closeMRErr == nil {
			r.mrsClosed = append(r.mrsClosed, iid)
		}
		return r.closeMRErr
	}
	oRetry, oJobs, oTrace, oTarget, oUpdate := forgeRetry, forgeJobs, forgeTrace, forgeTarget, updateBody
	t.Cleanup(func() {
		forgeMR, forgeRetry, forgeJobs, forgeTrace, forgeTarget, updateBody = oMR, oRetry, oJobs, oTrace, oTarget, oUpdate
	})
	forgeRetry = func(p forge.Project, id int) error {
		r.projects = append(r.projects, p)
		r.retries = append(r.retries, id)
		return r.retryErr
	}
	forgeJobs = func(p forge.Project, _ int) ([]forge.Job, error) {
		r.projects = append(r.projects, p)
		r.jobReads++
		return r.jobs, r.jobsErr
	}
	oAssert := forgeAssertDesc
	t.Cleanup(func() { forgeAssertDesc = oAssert })
	forgeAssertDesc = func(_ git.Repo, _ *forge.MR, pin, _ string) error {
		r.descAsserts = append(r.descAsserts, pin)
		return r.assertErr
	}
	forgeTrace = func(p forge.Project, job int) (string, error) {
		r.projects = append(r.projects, p)
		return fmt.Sprintf("tail of job %d", job), nil
	}
	forgeTarget = func(p forge.Project, _ string) (string, error) {
		r.projects = append(r.projects, p)
		return r.targetSHA, nil
	}
	oSource := forgeSource
	t.Cleanup(func() { forgeSource = oSource })
	forgeSource = func(p forge.Project, _ string) (string, error) {
		r.projects = append(r.projects, p)
		return r.sourceSHA, nil
	}
	forgeMR = func(p forge.Project, iid int) (*forge.MR, error) {
		r.projects = append(r.projects, p)
		r.mrReads++
		if r.mr == nil && r.mrErr == nil {
			return &forge.MR{IID: iid, State: "opened"}, nil
		}
		return r.mr, r.mrErr
	}
	t.Cleanup(func() {
		closeBead, spawnChild, closedChildrenOfKind, landBranch, landedBase, approvedAtHead, reviewDue, branchHead, membersDrained, openBlockers = oClose, oSpawn, oClosedChildren, oLand, oLandedBase, oApproved, oDue, oHead, oDrained, oBlockers
		landStrategy, landingsOf, showBead = oStrategy, oLandings, oShow
	})
	oCompose, oGate, oSB, oCrit, oAttr := composeLanding, gateBuild, sameBaseLand, openCritiques, gateAttr
	t.Cleanup(func() {
		composeLanding, gateBuild, sameBaseLand, openCritiques, gateAttr = oCompose, oGate, oSB, oCrit, oAttr
	})
	gateAttr = func() string { return "mugicrew" }
	openCritiques = func(string) ([]bd.Bead, error) {
		var out []bd.Bead
		for _, k := range r.kids {
			if k.Kind == contract.KindCritique && k.Open() {
				out = append(out, k)
			}
		}
		return out, nil
	}
	landStrategy = func() (string, git.LandStrategy, error) {
		if r.gated {
			return "local", git.LandLocal, r.strategyErr
		}
		return "gitlab", nil, r.strategyErr
	}
	composeLanding = func(_, _, packet string) (string, string, string, error) {
		r.composes++
		r.composePackets = append(r.composePackets, packet)
		pin := r.head
		if r.composes <= len(r.composePins) {
			pin = r.composePins[r.composes-1]
		}
		return fmt.Sprintf("composed%d", r.composes), fmt.Sprintf("base%d", r.composes), pin, r.composeErr
	}
	gateBuild = func(string, string, string) (string, error) {
		r.gates++
		return "/nix/store/out", r.gateErr
	}
	sameBaseLand = func(composed, _, _ string) git.LandStrategy {
		return func(string, string) (string, error) {
			i := r.sbLands
			r.sbLands++
			if i < len(r.landErrs) && r.landErrs[i] != nil {
				return "", r.landErrs[i]
			}
			return composed, nil
		}
	}
	landingsOf = func(string) ([]bd.Bead, error) { return r.landings, nil }
	showBead = func(string) (bd.Bead, error) { return openCheckpoint(), nil }
	openBlockers = func(string) ([]bd.Bead, error) { return r.blockers, nil }
	membersDrained = func(string) (bool, error) { return bd.MembersDrainedOver(r.kids, r.efforts), nil }
	reviewDue = func(id string, approved func(string, string) bool) (bool, error) {
		atHead := func(body string) bool { return approved(body, "checkpoint/"+id) }
		return bd.ReviewDueOver(r.kids, r.efforts, r.pivots, atHead), nil
	}
	closeBead = func(id, reason string) error {
		r.closed, r.reasons = append(r.closed, id), append(r.reasons, reason)
		return nil
	}
	spawnChild = func(_ string, s bd.Successor) (string, bool, error) {
		for _, k := range r.kids { // SpawnChildIfAbsent's open-child guard
			if k.Kind == s.Kind && k.Open() {
				return k.ID, false, nil
			}
		}
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
			out = append(out, bd.Bead{ID: fmt.Sprintf("ws-appr%d", i), Kind: contract.KindApproval, Status: "closed", CloseReason: bd.ApprovedReason, Description: body})
		}
		return out, nil
	}
	landBranch = func(branch, trailer string, st git.LandStrategy) (string, error) {
		r.landed = append(r.landed, branch)
		if r.gated {
			return st(branch, trailer)
		}
		return "landsha", r.landErr
	}
	oBody := updateBody
	t.Cleanup(func() { updateBody = oBody })
	updateBody = func(_ string, args []string) error {
		r.bodies = append(r.bodies, args[1])
		return nil
	}
	oProj := projectOf
	t.Cleanup(func() { projectOf = oProj })
	projectOf = func(repo git.Repo) (forge.Project, error) {
		if repo != "" {
			return childProject, nil
		}
		return workspaceProject, nil
	}
	oFetch := fetchForge
	t.Cleanup(func() { fetchForge = oFetch })
	fetchForge = func(repo git.Repo) (string, error) {
		r.fetches++
		r.fetchRepos = append(r.fetchRepos, repo)
		if r.fetchErr == nil && (r.fetchLands || r.fetches == r.landsOnFetch) {
			r.trailerLanded = true
		}
		return "forgesha", r.fetchErr
	}
	landedBase = func(string) (string, error) {
		if r.foreign != nil {
			return "", r.foreign
		}
		if r.trailerLanded {
			return "landsha", nil
		}
		if r.landedErr != nil {
			return "", r.landedErr
		}
		return "", git.ErrNotLanded
	}
	oTip, oSignal := landedTip, spawnSignal
	t.Cleanup(func() { landedTip, spawnSignal = oTip, oSignal })
	landedTip = func(git.Repo, string) (string, bool) { return r.landedTip, r.landedTip != "" }
	spawnSignal = func(_, _, body string) (string, bool, error) {
		r.signals = append(r.signals, body)
		return "ws-sig", true, nil
	}
	oTask := bd.SpawnBlockingTask
	t.Cleanup(func() { bd.SpawnBlockingTask = oTask })
	bd.SpawnBlockingTask = func(container, key, _, body string) (string, bool, error) { // bd's (container, key) guard
		k := container + "|" + key
		if slices.Contains(r.tasks, k) {
			return "ws-task", false, nil
		}
		r.tasks = append(r.tasks, k)
		r.taskBody = body
		return "ws-task", true, nil
	}
	// Stand in for git.ApprovedAtHead's pin==tip compare, reading the recorded tip so
	// the real git.Head shell-out is swapped out entirely.
	branchHead = func(string) (string, error) { return r.head, nil }
	oAnc := isAncestor
	t.Cleanup(func() { isAncestor = oAnc })
	isAncestor = func(string, string) bool { return !r.rewritten }
	approvedAtHead = func(body, _ string) bool {
		pin, ok := git.ParseCommit(body)
		return ok && pin == r.head
	}
}

func openCheckpoint() bd.Bead {
	return bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "in_progress", Title: "Ship the thing"}
}

func openLanding(pin string) bd.Bead {
	return bd.Bead{ID: "ws-land", Kind: contract.KindLanding, Status: "in_progress", Parent: "ws-cp", Description: git.CommitPin(pin)}
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

// landingsSpawned asserts exactly one Landing was spawned, pinned and in succession to
// the Approval, and returns it.
func landingsSpawned(t *testing.T, r *jinbeSeamRec, pin, approval string) bd.Successor {
	t.Helper()
	if len(r.spawned) != 1 {
		t.Fatalf("want exactly one Landing spawned, got %v", r.spawned)
	}
	l := r.spawned[0]
	if l.Kind != contract.KindLanding || l.Container {
		t.Errorf("must spawn a kind:landing leaf, got %+v", l)
	}
	if got, ok := git.ParseCommit(l.Description); !ok || got != pin {
		t.Errorf("Landing must copy the Approval's Commit: pin %q, got %q", pin, l.Description)
	}
	if l.CausedBy != approval || l.Title != "Ship the thing" {
		t.Errorf("Landing = %+v, want caused-by %s and the Checkpoint's title", l, approval)
	}
	return l
}

// The approvedAtHead gate (FR40): a closed Approval pinning the current tip spawns
// one Landing and never lands or closes the Checkpoint itself; a re-run spawns no
// second one.
func TestApprovedAtHeadGateSpawnsOneLanding(t *testing.T) {
	r := &jinbeSeamRec{approvalBody: "Clean pass.\nCommit: cafef00d", head: "cafef00d"}
	stubJinbeSeams(t, r)

	for range 2 {
		if err := reconcileCheckpoint(openCheckpoint()); err != nil {
			t.Fatalf("reconcileCheckpoint: %v", err)
		}
	}
	landingsSpawned(t, r, "cafef00d", "ws-appr0")
	if len(r.landed) != 0 || len(r.closed) != 0 {
		t.Errorf("the Checkpoint never lands or closes itself, got landed=%v closed=%v", r.landed, r.closed)
	}
}

// The land hold (Spec FR 15): an Approval at head never spawns a Landing past an
// open incoming blocker; once the Task closes, the same Approval spawns it.
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
	landingsSpawned(t, r, "cafef00d", "ws-appr0")
}

// Spec FR 28: an Approval at head never lands without a member's work. A
// closed member still holding effort/<id> (a Conflict whose integrate didn't
// publish) holds the Landing, silently (FR 47); with the ref gone it spawns.
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
		if got := len(r.spawned) == 1; got != tc.lands {
			t.Errorf("efforts %v: spawned=%v, want a Landing %v", tc.efforts, r.spawned, tc.lands)
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
	for _, s := range r.spawned {
		if s.Kind == contract.KindLanding {
			t.Errorf("no Approval at head must spawn no Landing, got %+v", s)
		}
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
	for _, s := range r.spawned {
		if s.Kind == contract.KindLanding {
			t.Errorf("a stale Approval must spawn no Landing, got %+v", s)
		}
	}
}

// After a stale-Approval self-heal a Checkpoint carries >=2 closed
// Approvals — the stale obsoleted one (created first, pin behind HEAD) AND the real at-head
// one (closed later by Luffy). The gate must scan ALL closed Approvals and spawn the Landing
// off the at-head one; a first-match read would hit the stale pin and strand the land forever.
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
	landingsSpawned(t, r, "newtip", "ws-appr1")
}

// Idempotency roll-forward (FR42): the land trailer already on mainline means the
// Checkpoint landed. A re-run lands nothing again, closes the open Landing landed:
// <sha>, and only then the Checkpoint.
func TestLandTrailerIdempotencyRollsForward(t *testing.T) {
	r := &jinbeSeamRec{trailerLanded: true, landings: []bd.Bead{openLanding("cafef00d")}}
	stubJinbeSeams(t, r)

	if err := reconcileCheckpoint(openCheckpoint()); err != nil {
		t.Fatalf("reconcileCheckpoint re-run: %v", err)
	}
	if len(r.landed) != 0 {
		t.Errorf("an already-landed Checkpoint must NOT re-land, got %v", r.landed)
	}
	if want := []string{"ws-land", "ws-cp"}; !slices.Equal(r.closed, want) {
		t.Errorf("closed = %v, want the Landing then the Checkpoint %v", r.closed, want)
	}
	if r.reasons[0] != "landed: landsha" {
		t.Errorf("Landing close reason = %q, want landed: landsha", r.reasons[0])
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

// landingRun runs reconcileLanding over a Landing pinned at pin.
func landingRun(_ *jinbeSeamRec, pin string) error {
	return reconcileLanding(openLanding(pin))
}

// A clean land closes the Landing landed: <sha>; the Checkpoint is left for its own pass.
func TestLandingLandsThenClosesLanded(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d"}
	stubJinbeSeams(t, r)
	if err := landingRun(r, "cafef00d"); err != nil {
		t.Fatalf("reconcileLanding: %v", err)
	}
	if len(r.landed) != 1 || r.landed[0] != "checkpoint/ws-cp" {
		t.Errorf("landed = %v, want the Checkpoint branch once", r.landed)
	}
	if !slices.Equal(r.closed, []string{"ws-land"}) || r.reasons[0] != "landed: landsha" {
		t.Errorf("closed %v %v, want only the Landing, landed: landsha", r.closed, r.reasons)
	}
}

// The trailer already on mainline closes the Landing with no land call.
func TestLandingTrailerPresentClosesWithoutLanding(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", trailerLanded: true}
	stubJinbeSeams(t, r)
	if err := landingRun(r, "cafef00d"); err != nil {
		t.Fatalf("reconcileLanding: %v", err)
	}
	if len(r.landed) != 0 || !slices.Equal(r.closed, []string{"ws-land"}) || r.reasons[0] != "landed: landsha" {
		t.Errorf("landed %v closed %v %v, want a landed: close and no land", r.landed, r.closed, r.reasons)
	}
}

// A pin behind the branch head closes the Landing obsolete, with no land call.
func TestLandingPinBehindHeadIsObsolete(t *testing.T) {
	r := &jinbeSeamRec{head: "newtip"}
	stubJinbeSeams(t, r)
	if err := landingRun(r, "oldpin"); err != nil {
		t.Fatalf("reconcileLanding: %v", err)
	}
	if len(r.landed) != 0 || !slices.Equal(r.closed, []string{"ws-land"}) || r.reasons[0] != "obsolete: branch advanced without landing" {
		t.Errorf("landed %v closed %v %v, want obsolete: branch advanced without landing", r.landed, r.closed, r.reasons)
	}
}

// A rebase conflict files one Franky-owned kind:conflict member on the Checkpoint
// carrying the Base: and Tip: pins, then closes the Landing obsolete: conflict. The
// error does NOT propagate.
func TestRebaseConflictRaisesConflictBead(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", landErr: &git.RebaseConflict{Base: "basesha"}}
	stubJinbeSeams(t, r)

	if err := landingRun(r, "cafef00d"); err != nil {
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
	if !slices.Equal(r.closed, []string{"ws-land"}) || r.reasons[0] != "obsolete: conflict" {
		t.Errorf("closed %v %v, want the Landing obsolete: conflict, never the Checkpoint", r.closed, r.reasons)
	}
}

// ADR 0030/0069: Land's *NumberClash files exactly one Franky-owned kind:conflict member
// with the clash-variant body (the colliding ADRs, the Base: pin), then closes the
// Landing obsolete: conflict; the error doesn't propagate.
func TestSequenceClashRaisesClashVariantConflictBead(t *testing.T) {
	clash := "db/migration/V12__cp.sql (rule db/migration/V{n}__*.sql) (vs mainline db/migration/V12__main.sql)"
	r := &jinbeSeamRec{head: "cafef00d", landErr: &git.NumberClash{Clashes: []string{clash}, Base: "basesha"}}
	stubJinbeSeams(t, r)

	if err := landingRun(r, "cafef00d"); err != nil {
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
	if !strings.Contains(conf.Description, "numbered-file clash") || !strings.Contains(conf.Description, "ADR 0219") || !strings.Contains(conf.Description, "sequence-renumber.md") || !strings.Contains(conf.Description, clash) {
		t.Errorf("want the clash-variant body naming %q, got %q", clash, conf.Description)
	}
	if !slices.Equal(r.closed, []string{"ws-land"}) || r.reasons[0] != "obsolete: conflict" {
		t.Errorf("closed %v %v, want the Landing obsolete: conflict", r.closed, r.reasons)
	}
}

// ADR 0069: a dirty main is not a conflict. *MainDirty from the land files no
// conflict member, leaves the Landing open, and is the returned error, recorded
// on land.failed for the next tick to retry.
func TestMainDirtyLandFilesNoBead(t *testing.T) {
	evs := eventlogtest.Capture(t)
	r := &jinbeSeamRec{head: "cafef00d", landErr: &git.MainDirty{Path: "/repo"}}
	stubJinbeSeams(t, r)

	err := landingRun(r, "cafef00d")
	var dirty *git.MainDirty
	if !errors.As(err, &dirty) {
		t.Fatalf("reconcileLanding = %v, want *MainDirty", err)
	}
	if len(r.spawned) != 0 || len(r.closed) != 0 {
		t.Errorf("a dirty main must file no bead and close nothing, spawned %v closed %v", r.spawned, r.closed)
	}
	if len(*evs) != 1 || (*evs)[0].Name != "land.failed" || !strings.Contains(fmt.Sprint((*evs)[0].Attrs["err"]), "clean main, then re-run") {
		t.Errorf("events = %+v, want one land.failed carrying the *MainDirty", *evs)
	}
}

// ErrLandPending leaves the Landing open with a nil error; any other error is returned, files nothing and closes nothing.
func TestLandingPendingStaysOpenOtherErrorsReturn(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", landErr: git.ErrLandPending}
	stubJinbeSeams(t, r)
	if err := landingRun(r, "cafef00d"); err != nil || len(r.closed)+len(r.spawned) != 0 {
		t.Errorf("pending: err %v closed %v spawned %v, want nil, open, nothing filed", err, r.closed, r.spawned)
	}
	r.landErr = errors.New("git: the Forge is down")
	if err := landingRun(r, "cafef00d"); err == nil || !strings.Contains(err.Error(), "Forge") || len(r.closed)+len(r.spawned) != 0 {
		t.Errorf("gitlab stub: err %v closed %v spawned %v, want the error, open, nothing filed", err, r.closed, r.spawned)
	}
}

// A sequences config refusal is a plain land error: the Landing stays open on every
// retry and no Conflict is filed.
func TestSequenceConfigRefusalFilesNothing(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", landErr: errors.New(`sequences: .mugicrew/config.toml: sequence template "x": want exactly one {n}, found 0`)}
	stubJinbeSeams(t, r)
	for range 2 {
		if err := landingRun(r, "cafef00d"); err == nil || !strings.Contains(err.Error(), "config.toml") || len(r.closed)+len(r.spawned) != 0 {
			t.Fatalf("err %v closed %v spawned %v, want the error, open, nothing filed", err, r.closed, r.spawned)
		}
	}
}

// A foreign land of the Landing's trailer leaves it open, returns the refusal and
// files one blocking Task naming both graphs, the sha and the fix, across ticks (FR 41).
func TestLandingForeignLandHoldsOnOneTask(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", foreign: &git.ForeignLanding{Trailer: "Checkpoint: ws-cp", Ours: "ourgraph", Theirs: "theirgraph", Sha: "f00dbeef"}}
	stubJinbeSeams(t, r)
	for range 2 {
		var fe *git.ForeignLanding
		if err := landingRun(r, "cafef00d"); !errors.As(err, &fe) {
			t.Fatalf("err = %v, want *ForeignLanding", err)
		}
	}
	if len(r.closed)+len(r.landed)+len(r.spawned) != 0 || len(r.tasks) != 1 {
		t.Errorf("closed %v landed %v spawned %v tasks %v, want the Landing open and one Task", r.closed, r.landed, r.spawned, r.tasks)
	}
	for _, want := range []string{"ourgraph", "theirgraph", "f00dbeef", "bd rename-prefix", "re-cut"} {
		if !strings.Contains(r.taskBody, want) {
			t.Errorf("Task body %q lacks %q", r.taskBody, want)
		}
	}
	if strings.Contains(r.taskBody, "`bd rename`") {
		t.Errorf("Task body %q still offers a per-bead bd rename", r.taskBody)
	}
}

// A gitlab Landing pending on an MR records MR: !<iid> on its body once (idempotent),
// stays open and logs land.pending with the iid (Spec FR 12, 49).
func TestLandingPendingRecordsTheMROnce(t *testing.T) {
	evs := eventlogtest.Capture(t)
	r := &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7, Pushed: true, Opened: true}}
	stubJinbeSeams(t, r)
	graphPushRec(t, nil)
	l := openLanding("cafef00d")
	if err := reconcileLanding(l); err != nil || len(r.closed) != 0 {
		t.Fatalf("pending: err %v closed %v", err, r.closed)
	}
	if want := "Commit: cafef00d\nMR: !7"; len(r.bodies) != 1 || strings.TrimSpace(r.bodies[0]) != want {
		t.Errorf("bodies = %q, want %q", r.bodies, want)
	}
	l.Description = r.bodies[0]
	reconcileLanding(l)
	for _, b := range r.bodies {
		if strings.Count(b, "MR: !") != 1 { // the second tick's Checked: write is not this test's concern
			t.Errorf("body %q, want exactly one MR: line", b)
		}
	}
	if e := (*evs)[0]; e.Name != "land.pending" || e.Attrs["mr"] != int64(7) && e.Attrs["mr"] != 7 {
		t.Errorf("event = %+v, want land.pending with mr 7", e)
	}
}

// Each gitlab pending tick re-reads the MR its body names and classifies the snapshot
// (Spec FR 13, 22, 26, 33–36): only a closed MR it recorded obsoletes the Landing and
// files one Task; an opened MR (armed, unchecked, need_rebase, a stale conflict status
// with has_conflicts=false) asserts its description and skips the write leg (FR 36).
func TestLandingMirrorsItsMR(t *testing.T) {
	for _, c := range []struct {
		name       string
		mr         forge.MR
		obsolete   bool
		wantLanded int
	}{
		{"armed, CI running", forge.MR{IID: 7, State: "opened", DetailedMergeStatus: "ci_still_running", HeadPipeline: &forge.Pipeline{ID: 1, Status: "running"}}, false, 0},
		{"unchecked after force-push", forge.MR{IID: 7, State: "opened", DetailedMergeStatus: "unchecked"}, false, 0},
		{"need_rebase", forge.MR{IID: 7, State: "opened", DetailedMergeStatus: "need_rebase"}, false, 0},
		{"stale conflict status, has_conflicts=false", forge.MR{IID: 7, State: "opened", DetailedMergeStatus: "conflict"}, false, 0},
		{"closed, this Landing's iid", forge.MR{IID: 7, State: "closed"}, true, 0},
		{"locked, merge in progress", forge.MR{IID: 7, State: "locked"}, false, 0},
		{"unknown state", forge.MR{IID: 7, State: "weird"}, false, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7}, mr: &c.mr}
			stubJinbeSeams(t, r)
			l := openLanding("cafef00d")
			l.Description += "\nMR: !7\n"
			for range 2 {
				if err := reconcileLanding(l); err != nil {
					t.Fatalf("err = %v", err)
				}
			}
			wantClosed := 0
			if c.obsolete {
				wantClosed = 2
			}
			if len(r.closed) != wantClosed || len(r.landed) != 2*c.wantLanded || r.mrReads != 2 {
				t.Errorf("closed %v landed %v reads %d", r.closed, r.landed, r.mrReads)
			}
			if c.mr.State == "opened" && len(r.descAsserts) != 2 {
				t.Errorf("description asserts %v, want one per tick", r.descAsserts)
			}
			if c.obsolete {
				if r.reasons[0] != "obsolete: merge request closed" || len(r.tasks) != 1 {
					t.Errorf("reason %q tasks %v, want obsolete: merge request closed and one Task across ticks", r.reasons[0], r.tasks)
				}
				for _, want := range []string{"!7", "reopen", "merge", "abandon"} {
					if !strings.Contains(r.taskBody, want) {
						t.Errorf("Task body %q lacks %q", r.taskBody, want)
					}
				}
			} else if len(r.tasks) != 0 {
				t.Errorf("tasks %v, want none", r.tasks)
			}
		})
	}
}

// A failed head pipeline that did not run on the MR's current head belongs to an earlier
// attempt: the Landing stays pending, with no retry, Critique or close.
func TestStaleHeadPipelineIsPending(t *testing.T) {
	for _, c := range []struct {
		name   string
		pl     forge.Pipeline
		source string
	}{
		{"branch pipeline on an old sha", forge.Pipeline{ID: 42, Status: "failed", SHA: "old"}, ""},
		{"merged results on an old source", forge.Pipeline{ID: 42, Status: "failed", Ref: "refs/merge-requests/7/merge", SHA: "m1"}, "old"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7}, sourceSHA: c.source,
				mr: &forge.MR{IID: 7, State: "opened", SHA: "new", HeadPipeline: &c.pl}}
			stubJinbeSeams(t, r)
			l := openLanding("cafef00d")
			l.Description += "\nMR: !7\n"
			if err := reconcileLanding(l); err != nil {
				t.Fatal(err)
			}
			if len(r.retries) != 0 || len(r.spawned) != 0 || len(r.reasons) != 0 {
				t.Errorf("retries %v spawned %d reasons %v, want none", r.retries, len(r.spawned), r.reasons)
			}
		})
	}
}

// An infra-failed retried pipeline files its Task once; later ticks find it open and
// read no jobs or traces (FR 36).
func TestInfraFailureReadsJobsOnce(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7},
		jobs: []forge.Job{{ID: 9, Name: "build", FailureReason: "runner_system_failure"}},
		mr:   &forge.MR{IID: 7, State: "opened", HeadPipeline: &forge.Pipeline{ID: 42, Status: "failed"}}}
	stubJinbeSeams(t, r)
	l := openLanding("cafef00d")
	l.Description += "\nMR: !7\nRetried: pipeline 42\n"
	if err := reconcileLanding(l); err != nil || r.jobReads != 1 {
		t.Fatalf("first tick err %v jobReads %d", err, r.jobReads)
	}
	r.blockers = []bd.Bead{{ID: "ws-task", Kind: contract.KindTask, Status: "open", Description: r.taskBody}}
	if err := reconcileLanding(l); err != nil || r.jobReads != 1 {
		t.Errorf("second tick err %v jobReads %d, want no further read", err, r.jobReads)
	}
}

// A gitlab Landing keys conflict on facts (Spec FR 24–26): a trial rebase conflict, a
// trial ADR clash, has_conflicts or a merge_error file the Checkpoint's conflict member
// and close it obsolete: conflict with the MR left open (no Forge write); a stale
// conflict status with has_conflicts=false and a clean trial stays pending.
func TestLandingConflictFromTrialOrMR(t *testing.T) {
	for _, c := range []struct {
		name     string
		mr       forge.MR
		trialErr error
		conflict bool
	}{
		{"trial rebase conflict", forge.MR{IID: 7, State: "opened"}, &git.RebaseConflict{Base: "b"}, true},
		{"trial ADR clash", forge.MR{IID: 7, State: "opened"}, &git.NumberClash{Base: "b", Clashes: []string{"0030"}}, true},
		{"has_conflicts", forge.MR{IID: 7, State: "opened", HasConflicts: true}, nil, true},
		{"merge_error", forge.MR{IID: 7, State: "opened", MergeError: "Rebase failed"}, nil, true},
		{"stale conflict status, clean trial", forge.MR{IID: 7, State: "opened", DetailedMergeStatus: "conflict"}, nil, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7}, mr: &c.mr, composeErr: c.trialErr}
			stubJinbeSeams(t, r)
			l := openLanding("cafef00d")
			l.Description += "\nMR: !7\n"
			if err := reconcileLanding(l); err != nil {
				t.Fatalf("err = %v", err)
			}
			if c.conflict {
				if len(r.spawned) != 1 || r.spawned[0].Kind != contract.KindConflict || len(r.reasons) != 1 || r.reasons[0] != "obsolete: conflict" || len(r.landed) != 0 {
					t.Errorf("spawned %v reasons %v landed %v, want a conflict member and obsolete: conflict", r.spawned, r.reasons, r.landed)
				}
			} else if len(r.spawned)+len(r.closed) != 0 || len(r.landed) != 0 {
				t.Errorf("spawned %v closed %v landed %v, want pending", r.spawned, r.closed, r.landed)
			}
		})
	}
}

// The head pipeline arm (Spec FR 27–32, ADR 0188): a first failure is retried once and
// recorded, staying pending; a retried failure is a Task when infra, else a Critique
// (Checkpoint) or Task (Chart) and the Landing closes obsolete: pipeline failed.
func TestLandingMirrorsAFailedPipeline(t *testing.T) {
	failed := &forge.Pipeline{ID: 42, Status: "failed", WebURL: "https://gl/p/42"}
	infra := []forge.Job{{ID: 5, Name: "unit", WebURL: "https://gl/j/5", FailureReason: "runner_system_failure"}}
	real := []forge.Job{{ID: 6, Name: "lint", WebURL: "https://gl/j/6", FailureReason: "script_failure"}}
	for _, c := range []struct {
		name        string
		retried     bool
		jobs        []forge.Job
		kind        contract.Kind
		pipeline    *forge.Pipeline
		wantRetries int
		wantReason  string
		wantTasks   int
		wantCrit    int
		wantBase    string
		held        bool
	}{
		{"failed first time", false, real, contract.KindCheckpoint, failed, 1, "", 0, 0, "", false},
		{"retried then infra", true, infra, contract.KindCheckpoint, failed, 0, "", 1, 0, "", false},
		{"retried then real, Checkpoint", true, real, contract.KindCheckpoint, failed, 0, "obsolete: pipeline failed", 0, 1, "", false},
		{"retried then real, merged results", true, real, contract.KindCheckpoint, &forge.Pipeline{ID: 42, Status: "failed", WebURL: "https://gl/p/42", Ref: "refs/merge-requests/7/merge", SHA: "m1"}, 0, "obsolete: pipeline failed", 0, 1, "Base: base9", false},
		{"retried then real, Chart", true, real, contract.KindChart, failed, 0, "obsolete: pipeline failed", 1, 0, "", false},
		{"infra Task closed, pipeline unchanged", true, infra, contract.KindCheckpoint, failed, 0, "", 0, 0, "", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7}, jobs: c.jobs, targetSHA: "base9",
				mr: &forge.MR{IID: 7, State: "opened", HeadPipeline: c.pipeline}}
			stubJinbeSeams(t, r)
			showBead = func(string) (bd.Bead, error) { return bd.Bead{ID: "ws-cp", Kind: c.kind, Title: "Ship"}, nil }
			l := openLanding("cafef00d")
			l.Description += "\nMR: !7\n"
			if c.retried {
				l.Description += "Retried: pipeline 42\n"
			}
			if c.held {
				l.Description += "Held: pipeline 42\n"
			}
			ticks := 2
			if !c.retried {
				ticks = 1 // the next tick would see the retry recorded and file the failure
			}
			for range ticks {
				if err := reconcileLanding(l); err != nil {
					t.Fatalf("err = %v", err)
				}
			}
			if len(r.bodies) > 0 {
				l.Description = r.bodies[len(r.bodies)-1]
			}
			if len(r.retries) != c.wantRetries || len(r.landed) != 0 {
				t.Errorf("retries %v landed %v, want %d retry and no land attempt", r.retries, r.landed, c.wantRetries)
			}
			if c.wantRetries == 1 && !strings.Contains(l.Description, "\nRetried: pipeline 42\n") {
				t.Errorf("body %q lacks Retried:", l.Description)
			}
			if len(r.tasks) != c.wantTasks || len(r.spawned) != c.wantCrit {
				t.Errorf("tasks %v spawned %d, want %d and %d across two ticks", r.tasks, len(r.spawned), c.wantTasks, c.wantCrit)
			}
			if (c.wantReason == "") != (len(r.reasons) == 0) || (c.wantReason != "" && r.reasons[0] != c.wantReason) {
				t.Errorf("reasons %v, want %q", r.reasons, c.wantReason)
			}
			detail := r.taskBody
			if c.wantCrit == 1 {
				s := r.spawned[0]
				detail = s.Description
				if s.Kind != contract.KindCritique || s.DiscoveredFrom != "ws-land" || !strings.Contains(detail, "Tip: cafef00d\n") || !strings.Contains(detail, "Pipeline: 42\n") {
					t.Errorf("Critique = %+v", s)
				}
				if strings.Contains(detail, "Base: ") != (c.wantBase != "") || !strings.Contains(detail, c.wantBase) {
					t.Errorf("Critique body %q, want Base %q", detail, c.wantBase)
				}
			}
			if c.wantTasks+c.wantCrit == 1 {
				for _, want := range []string{"https://gl/p/42", c.jobs[0].Name, c.jobs[0].WebURL, c.jobs[0].FailureReason, fmt.Sprintf("tail of job %d", c.jobs[0].ID)} {
					if !strings.Contains(detail, want) {
						t.Errorf("body %q lacks %q", detail, want)
					}
				}
			}
		})
	}
}

// A Forge retry keeps the pipeline id but moves updated_at: Held: suppresses only the run
// it recorded (Spec FR 28–31, 33). The same run files nothing again; a re-run that fails
// for a real reason is classified; one that fails for infra again files one fresh Task.
func TestLandingHeldPipelineIsPerRun(t *testing.T) {
	infra := []forge.Job{{ID: 5, Name: "unit", FailureReason: "runner_system_failure"}}
	real := []forge.Job{{ID: 6, Name: "lint", FailureReason: "script_failure"}}
	for _, c := range []struct {
		name      string
		updated   string
		jobs      []forge.Job
		wantTasks int
		wantCrit  int
		wantClose bool
	}{
		{"same run", "t1", infra, 0, 0, false},
		{"re-run fails for real", "t2", real, 0, 1, true},
		{"re-run fails for infra again", "t2", infra, 1, 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			pl := &forge.Pipeline{ID: 42, Status: "failed", UpdatedAt: c.updated}
			r := &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7}, jobs: c.jobs, mr: &forge.MR{IID: 7, State: "opened", HeadPipeline: pl}}
			stubJinbeSeams(t, r)
			showBead = func(string) (bd.Bead, error) {
				return bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Title: "Ship"}, nil
			}
			l := openLanding("cafef00d")
			l.Description += "\nMR: !7\nRetried: pipeline 42\nHeld: pipeline 42 t1\n"
			for range 2 { // the second tick must not file again
				if err := reconcileLanding(l); err != nil {
					t.Fatalf("err = %v", err)
				}
				if len(r.bodies) > 0 {
					l.Description = r.bodies[len(r.bodies)-1]
				}
			}
			if len(r.tasks) != c.wantTasks || len(r.spawned) != c.wantCrit || (len(r.reasons) > 0) != c.wantClose {
				t.Errorf("tasks %v spawned %d reasons %v, want %d, %d, closed=%v", r.tasks, len(r.spawned), r.reasons, c.wantTasks, c.wantCrit, c.wantClose)
			}
		})
	}
}

// The trial reruns only when the fetched mainline moved off the Landing's Checked: line,
// and records Checked: once.
func TestLandingTrialRerunsOnlyWhenMainMoves(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7}, mr: &forge.MR{IID: 7, State: "opened"}}
	stubJinbeSeams(t, r)
	l := openLanding("cafef00d")
	l.Description += "\nMR: !7\n"
	reconcileLanding(l)
	if r.composes != 1 || len(r.bodies) != 1 || !strings.Contains(r.bodies[0], "\nChecked: forgesha\n") {
		t.Fatalf("composes %d bodies %q, want one trial and Checked: forgesha", r.composes, r.bodies)
	}
	l.Description = r.bodies[0]
	reconcileLanding(l)
	if r.composes != 1 {
		t.Errorf("composes = %d after an unmoved main, want 1", r.composes)
	}
	l.Description = strings.Replace(l.Description, "forgesha", "older", 1)
	reconcileLanding(l)
	if r.composes != 2 || !strings.Contains(r.bodies[len(r.bodies)-1], "Checked: forgesha") || strings.Contains(r.bodies[len(r.bodies)-1], "older") {
		t.Errorf("composes %d bodies %q, want a rerun and the line replaced", r.composes, r.bodies)
	}
}

// A 429 leaves the Landing open with no error and logs once; any other Forge error is
// returned; neither closes, files nor writes (Spec FR 35).
func TestLandingMirrorRateLimitedLogsOnceOtherErrorsReturn(t *testing.T) {
	evs := eventlogtest.Capture(t)
	r := &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7}, mrErr: forge.ErrRateLimited}
	stubJinbeSeams(t, r)
	l := openLanding("cafef00d")
	l.Description += "\nMR: !7\n"
	for range 3 {
		if err := reconcileLanding(l); err != nil {
			t.Fatalf("429: err = %v, want nil", err)
		}
	}
	limited := 0
	for _, e := range *evs {
		if e.Name == "land.rate_limited" {
			limited++
		}
	}
	if limited != 1 || len(r.closed)+len(r.landed)+len(r.tasks) != 0 {
		t.Errorf("limited events %d, closed %v landed %v tasks %v, want one event and nothing else", limited, r.closed, r.landed, r.tasks)
	}
	r.mrErr = &forge.Error{Status: 500, Msg: "down"}
	if err := reconcileLanding(l); err == nil || len(r.closed)+len(r.landed)+len(r.tasks) != 0 {
		t.Errorf("500: err %v closed %v landed %v, want the error and the Landing open", err, r.closed, r.landed)
	}
}

// A 429 from any Forge call on a Landing — the write leg before or after MR: is
// recorded, the pipeline retry or the jobs read — is the same once-logged hold (FR 35).
func TestLandingForgeRateLimitAnywhereLogsOnce(t *testing.T) {
	failed := &forge.MR{IID: 7, State: "opened", HeadPipeline: &forge.Pipeline{ID: 42, Status: "failed"}}
	for _, c := range []struct {
		name    string
		r       jinbeSeamRec
		retried bool
		mr      bool
	}{
		{"write leg, no MR: yet", jinbeSeamRec{landErr: forge.ErrRateLimited}, false, false},
		{"description assert, MR: recorded", jinbeSeamRec{assertErr: fmt.Errorf("git: set description: %w", forge.ErrRateLimited), mr: &forge.MR{IID: 7, State: "opened"}}, false, true},
		{"pipeline retry", jinbeSeamRec{mr: failed, retryErr: forge.ErrRateLimited}, false, true},
		{"jobs read", jinbeSeamRec{mr: failed, jobsErr: forge.ErrRateLimited}, true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			evs := eventlogtest.Capture(t)
			rateLimited.Delete("ws-land")
			r := c.r
			r.head = "cafef00d"
			stubJinbeSeams(t, &r)
			l := openLanding("cafef00d")
			if c.mr {
				l.Description += "\nMR: !7\n"
			}
			if c.retried {
				l.Description += "Retried: pipeline 42\n"
			}
			for range 3 {
				if err := reconcileLanding(l); err != nil {
					t.Fatalf("429: err = %v, want nil", err)
				}
			}
			limited := 0
			for _, e := range *evs {
				if e.Name == "land.rate_limited" {
					limited++
				}
				if e.Name == "land.failed" {
					t.Errorf("land.failed logged on a 429, want only land.rate_limited")
				}
			}
			if limited != 1 || len(r.closed)+len(r.tasks)+len(r.spawned) != 0 {
				t.Errorf("limited %d closed %v tasks %v spawned %v, want one event and nothing else", limited, r.closed, r.tasks, r.spawned)
			}
		})
	}
}

// An unusable land_strategy is an error naming it: nothing lands, files or closes.
func TestLandingUnusableStrategyErrors(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", strategyErr: errors.New(`land_strategy in config.toml is unusable`)}
	stubJinbeSeams(t, r)
	if err := landingRun(r, "cafef00d"); err == nil || !strings.Contains(err.Error(), "land_strategy") {
		t.Errorf("err = %v, want one naming land_strategy", err)
	}
	if len(r.landed)+len(r.closed)+len(r.spawned) != 0 {
		t.Errorf("landed %v closed %v spawned %v, want nothing", r.landed, r.closed, r.spawned)
	}
}

// The real resolver's error carries the rejected value and the layer, and returns no strategy.
func TestLandStrategyAtNamesRejectedValue(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.MkdirAll(filepath.Join(root, ".mugicrew"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".mugicrew", "config.toml"), []byte("land_strategy = \"gitlb\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, s, err := landStrategyAt(root)
	if s != nil || err == nil || !strings.Contains(err.Error(), `"gitlb"`) || !strings.Contains(err.Error(), "config.toml") {
		t.Errorf("strategy %v err %v, want nil and an error naming \"gitlb\" and config.toml", s, err)
	}
}

// Run(checkpoint) re-reads the bead and spawns its Landing; Run(landing) lands; a kind
// Jinbe doesn't own is a loud error, mirroring Franky.Run.
func TestRunDrivesCheckpointAndLandingAndRejectsOtherKinds(t *testing.T) {
	r := &jinbeSeamRec{approvalBody: "Commit: cafef00d", head: "cafef00d"}
	stubJinbeSeams(t, r)

	if err := (Jinbe{}).Run(stubCtx{}, contract.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(r.spawned) != 1 || len(r.landed) != 0 {
		t.Errorf("Run(checkpoint) must spawn the Landing, got spawned=%v landed=%v", r.spawned, r.landed)
	}

	showBead = func(id string) (bd.Bead, error) {
		if id == "ws-land" {
			return openLanding("cafef00d"), nil
		}
		return openCheckpoint(), nil
	}
	if err := (Jinbe{}).Run(stubCtx{}, contract.Bead{ID: "ws-land", Kind: contract.KindLanding}); err != nil {
		t.Fatalf("Run(landing): %v", err)
	}
	if len(r.landed) != 1 || !slices.Equal(r.closed, []string{"ws-land"}) {
		t.Errorf("Run(landing) must land then close it, got landed=%v closed=%v", r.landed, r.closed)
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
	git.SetGraphID("testgraphroot") // Compose, Anchor and revert read the Graph id; never ask a live bd
	// The nix sandbox has no git identity; the land's own git commit needs one.
	for _, k := range []string{"GIT_AUTHOR", "GIT_COMMITTER"} {
		os.Setenv(k+"_NAME", "t")
		os.Setenv(k+"_EMAIL", "t@t")
	}
	os.Exit(m.Run())
}

// The Landing's land records land.done with the Landing as bead, its Checkpoint as
// parent and the container branch, and a rebase conflict land.failed at Error.
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
		stubJinbeSeams(t, &jinbeSeamRec{head: "cafef00d", landErr: tc.landErr})
		reconcileLanding(openLanding("cafef00d"))
		if len(*evs) != 1 || (*evs)[0].Name != tc.want || (*evs)[0].Level != tc.level {
			t.Fatalf("events = %+v, want %s first", *evs, tc.want)
		}
		if a := (*evs)[0].Attrs; a["bead"] != "ws-land" || a["parent"] != "ws-cp" || a["branch"] != "checkpoint/ws-cp" || a["into"] != git.Mainline() {
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
	target       string      // RollbackTarget; "" → no related Checkpoint
	targetSt     string      // the target Checkpoint's status
	targetIn     string      // the target Checkpoint's parent (membership edge)
	targetLabels []string    // the target Checkpoint's labels
	onMain       bool        // the target's Checkpoint: trailer is on main
	noSpec       bool        // the target resolves no Spec
	body         string      // the Rollback's body (breakage evidence)
	revertErr    error       // non-nil → git.Revert failure
	failAt       string      // revert|successor|cut|reapply|build|close: fail that leg once
	reverted     [][2]string // (squash, trailer) per Revert call
	reasons      map[string]string

	reverts    int               // revert commits written to main
	successors []bd.Successor    // successor Checkpoints spawned
	branches   map[string]string // branch → tip
	seeds      int               // reapply commits written
	builds     []bd.Successor    // Build members spawned, open or closed

	gitlab      bool       // land_strategy gitlab: the revert goes through an MR
	pushes      []string   // pushRevertMR pins, one per push
	pushRepos   []git.Repo // the repo each pushRevertMR call named
	built       int        // revertOnto calls
	mrClosed    []int      // forgeCloseMR iids
	mergedAt    string     // firstParent's answer: the normalised merge M
	closedAtSet []string   // successors existing when an MR was closed
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
	r.gated = !r.gitlab
	stubJinbeSeams(t, &r.jinbeSeamRec)
	oOnto, oPush, oFP, oCloseMR, oBody := revertOnto, pushRevertMR, firstParent, forgeCloseMR, updateBody
	t.Cleanup(func() {
		revertOnto, pushRevertMR, firstParent, forgeCloseMR, updateBody = oOnto, oPush, oFP, oCloseMR, oBody
	})
	updateBody = func(_ string, args []string) error { r.body = args[1]; return nil }
	revertOnto = func(base, squash, trailer string) (string, error) {
		r.built++
		r.reverted = append(r.reverted, [2]string{base + " " + squash, trailer})
		return "pinsha", nil
	}
	pushRevertMR = func(repo git.Repo, id, trailer, pin string) (*git.LandPending, error) {
		r.pushRepos = append(r.pushRepos, repo)
		r.pushes = append(r.pushes, id+" "+trailer+" "+pin)
		return &git.LandPending{MR: 9, Pushed: true, Opened: len(r.pushes) == 1}, nil
	}
	firstParent = func(x string) (string, error) { return cmp.Or(r.mergedAt, x), nil }
	forgeCloseMR = func(p forge.Project, iid int) error {
		r.projects = append(r.projects, p)
		r.mrClosed = append(r.mrClosed, iid)
		r.closedAtSet = append(r.closedAtSet, fmt.Sprint(len(r.successors)))
		return nil
	}
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
			return bd.Bead{ID: id, Kind: contract.KindCheckpoint, Status: r.targetSt, Parent: r.targetIn, Labels: r.targetLabels}, nil
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
		if r.trailerLanded && trailer == "Reverts-Checkpoint: "+r.target {
			return "mergedsha", nil
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

// The pre-review conflict probe (Spec FR 1, 2, 6, 7, 9, 10, 12): a collision files the
// Checkpoint's Conflict instead of the Review, a re-run files no second one, and a
// probe failure spawns neither.
func TestPreReviewProbe(t *testing.T) {
	built := []bd.Bead{{ID: "b1", Kind: contract.KindBuild, Status: "closed"}}
	t.Run("clean spawns the Review", func(t *testing.T) {
		r := &jinbeSeamRec{kids: built, head: "tip1"}
		stubJinbeSeams(t, r)
		if err := reconcileCheckpoint(openCheckpoint()); err != nil {
			t.Fatal(err)
		}
		if len(r.spawned) != 1 || r.spawned[0].Kind != contract.KindReview || r.probeCalls != 1 {
			t.Errorf("spawned %v probes %d, want one Review after one probe", r.spawned, r.probeCalls)
		}
	})
	for name, tc := range map[string]struct {
		err     error
		clashes []string
	}{
		"textual conflict": {err: &git.RebaseConflict{Base: "basesha"}},
		"adr clash":        {err: &git.NumberClash{Base: "basesha", Clashes: []string{"0042-a.md"}}, clashes: []string{"0042-a.md"}},
	} {
		t.Run(name, func(t *testing.T) {
			r := &jinbeSeamRec{kids: built, head: "tip1", probeErr: tc.err}
			stubJinbeSeams(t, r)
			for range 2 { // the second pass finds the Conflict open
				if err := reconcileCheckpoint(openCheckpoint()); err != nil {
					t.Fatalf("a collision becomes a bead, not an error: %v", err)
				}
			}
			if len(r.spawned) != 1 || r.spawned[0].Kind != contract.KindConflict {
				t.Fatalf("want exactly one Conflict and no Review, got %v", r.spawned)
			}
			c := r.spawned[0]
			if base, ok := git.ParseBase(c.Description); !ok || base != "basesha" {
				t.Errorf("Base: pin missing in %q", c.Description)
			}
			if tip, ok := git.ParseTip(c.Description); !ok || tip != "tip1" {
				t.Errorf("Tip: pin missing in %q", c.Description)
			}
			if want := "Checkpoint ws-cp conflicts with main"; c.Title != want {
				t.Errorf("title = %q, want %q", c.Title, want)
			}
			if !strings.Contains(c.Description, "before review") || strings.Contains(c.Description, "re-lands") || strings.Contains(c.Description, "Checkpoint-land") {
				t.Errorf("body must say found before review, not at a land: %q", c.Description)
			}
			for _, cl := range tc.clashes {
				if !strings.Contains(c.Description, "numbered-file clash") || !strings.Contains(c.Description, cl) {
					t.Errorf("clash variant must name %q: %q", cl, c.Description)
				}
			}
		})
	}
	t.Run("a repo = context probes the child", func(t *testing.T) {
		child := git.Repo("/ws/child")
		oRepo := repoDirOf
		t.Cleanup(func() { repoDirOf = oRepo })
		repoDirOf = func(bd.Bead) (git.Repo, string) { return child, "child" }
		r := &jinbeSeamRec{kids: built, head: "tip1", probeErr: &git.RebaseConflict{Base: "basesha"}}
		stubJinbeSeams(t, r)
		oChild := repoHead
		t.Cleanup(func() { repoHead = oChild })
		repoHead = func(repo git.Repo) func(string) (string, error) {
			if repo != child {
				t.Errorf("tip read in %q, want the child", repo)
			}
			return func(string) (string, error) { return "childtip", nil }
		}
		if err := reconcileCheckpoint(openCheckpoint()); err != nil {
			t.Fatal(err)
		}
		if len(r.probeRepos) != 1 || r.probeRepos[0] != child {
			t.Errorf("base resolved in %v, want the child", r.probeRepos)
		}
		if len(r.spawned) != 1 {
			t.Fatalf("spawned %v", r.spawned)
		}
		if tip, _ := git.ParseTip(r.spawned[0].Description); tip != "childtip" {
			t.Errorf("Tip pin %q, want the child's", tip)
		}
	})
	t.Run("probe error spawns neither", func(t *testing.T) {
		boom := errors.New("git broke")
		r := &jinbeSeamRec{kids: built, probeErr: boom}
		stubJinbeSeams(t, r)
		if err := reconcileCheckpoint(openCheckpoint()); !errors.Is(err, boom) {
			t.Errorf("err = %v, want the probe error", err)
		}
		if len(r.spawned) != 0 {
			t.Errorf("spawned %v, want nothing", r.spawned)
		}
	})
	t.Run("review not due runs no probe", func(t *testing.T) {
		r := &jinbeSeamRec{kids: built, efforts: map[string]bool{"b1": true}, probeErr: errors.New("x")}
		stubJinbeSeams(t, r)
		if err := reconcileCheckpoint(openCheckpoint()); err != nil || r.probeCalls != 0 {
			t.Errorf("err %v probes %d, want none", err, r.probeCalls)
		}
	})
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
			{ID: "a1", Kind: contract.KindApproval, Status: "closed", CloseReason: bd.ApprovedReason, Description: "Commit: oldpin"},
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

// Spec D2, FR32: a Rollback's successor Checkpoint joins the rolled-back
// Checkpoint's Initiative only while it is unclosed; the Rollback-born Build
// stays a plain member of the successor.
func TestRollbackSuccessorJoinsAnUnclosedInitiative(t *testing.T) {
	for _, tc := range []struct{ parent, want string }{{"ws-init", "ws-init"}, {"ws-closed-init", ""}, {"", ""}} {
		t.Run(tc.parent, func(t *testing.T) {
			r := rollbackRec{target: "ws-cp", targetSt: "closed", targetIn: tc.parent, onMain: true, body: "main broke"}
			stubRollbackSeams(t, &r)
			o := initiativeOf
			t.Cleanup(func() { initiativeOf = o })
			initiativeOf = func(b bd.Bead) (string, error) {
				if b.Parent == "ws-init" {
					return "ws-init", nil
				}
				return "", nil
			}
			if err := reconcileRollback(r.rollback()); err != nil {
				t.Fatalf("reconcileRollback: %v", err)
			}
			if len(r.successors) != 1 || r.successors[0].Parent != tc.want {
				t.Errorf("successors %+v, want one with Parent %q", r.successors, tc.want)
			}
			if len(r.builds) != 1 || r.builds[0].Parent != "" {
				t.Errorf("builds %+v, want one member Build with no Parent", r.builds)
			}
		})
	}
}

// chartLandingRun runs reconcileLanding over a Landing whose parent is a Chart.
func chartLandingRun(r *jinbeSeamRec, pin string) error {
	showBead = func(string) (bd.Bead, error) {
		return bd.Bead{ID: "ws-cp", Kind: contract.KindChart, Status: "open", Title: "a chart"}, nil
	}
	return landingRun(r, pin)
}

// A Chart-parent Landing lands chart/<id> and closes landed: <sha>.
func TestChartLandingLandsThenClosesLanded(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d"}
	stubJinbeSeams(t, r)
	if err := chartLandingRun(r, "cafef00d"); err != nil {
		t.Fatalf("reconcileLanding: %v", err)
	}
	if !slices.Equal(r.landed, []string{"chart/ws-cp"}) || !slices.Equal(r.closed, []string{"ws-land"}) || r.reasons[0] != "landed: landsha" {
		t.Errorf("landed %v closed %v %v, want chart/ws-cp landed and the Landing closed landed: landsha", r.landed, r.closed, r.reasons)
	}
}

// A Chart-parent rebase conflict files the chart-conflict (rebase variant, Base:/Tip:
// pins), an ADR clash the clash variant; each closes the Landing obsolete: conflict.
func TestChartLandingConflictFilesChartConflict(t *testing.T) {
	for name, tc := range map[string]struct {
		err     error
		variant string
	}{
		"rebase": {&git.RebaseConflict{Base: "basesha"}, nami.ConflictRebase},
		"clash":  {&git.NumberClash{Clashes: []string{"docs/adr/0031-a.md"}, Base: "basesha"}, nami.ConflictNumberClash},
	} {
		t.Run(name, func(t *testing.T) {
			r := &jinbeSeamRec{head: "cafef00d", landErr: tc.err}
			stubJinbeSeams(t, r)
			if err := chartLandingRun(r, "cafef00d"); err != nil {
				t.Fatalf("a conflict must NOT propagate: %v", err)
			}
			if len(r.spawned) != 1 || r.spawned[0].Kind != contract.KindChartConflict || r.spawned[0].Container {
				t.Fatalf("spawned %+v, want one chart-conflict leaf", r.spawned)
			}
			d := r.spawned[0].Description
			if base, _ := git.ParseBase(d); base != "basesha" {
				t.Errorf("body %q lacks Base: basesha", d)
			}
			if tip, _ := git.ParseTip(d); tip != "cafef00d" {
				t.Errorf("body %q lacks Tip: cafef00d", d)
			}
			if !strings.Contains(d, "variant: "+tc.variant) {
				t.Errorf("body %q lacks variant %s", d, tc.variant)
			}
			if want := "a chart conflicts with main"; r.spawned[0].Title != want {
				t.Errorf("title = %q, want %q", r.spawned[0].Title, want)
			}
			if !slices.Equal(r.closed, []string{"ws-land"}) || r.reasons[0] != "obsolete: conflict" {
				t.Errorf("closed %v %v, want the Landing obsolete: conflict", r.closed, r.reasons)
			}
		})
	}
}

func gateRun(t *testing.T, r *jinbeSeamRec) (error, []eventlogtest.Event) {
	t.Helper()
	r.gated, r.head = true, "cafef00d"
	evs := eventlogtest.Capture(t)
	stubJinbeSeams(t, r)
	err := reconcileLanding(openLanding("cafef00d"))
	return err, *evs
}

func TestGatedLanding(t *testing.T) {
	moved := &git.BaseMoved{Base: "b", Now: "n"}
	infra := &looplock.InfraError{Err: errors.New("no nix"), Tail: "tail", Timeout: true}
	for _, tc := range []struct {
		name        string
		r           jinbeSeamRec
		wantErr     bool
		reason      string
		composes    int
		events      []string
		lastAttempt int
	}{
		{"pass lands the composed sha", jinbeSeamRec{}, false, "landed: composed1", 1, []string{"gate.started", "gate.passed", "land.done"}, 1},
		{"infra failure leaves it open", jinbeSeamRec{gateErr: infra}, true, "", 1, []string{"gate.started", "gate.infra"}, 1},
		{"base moved twice then landed is attempt 3", jinbeSeamRec{landErrs: []error{moved, moved}}, false, "landed: composed3", 3,
			[]string{"gate.started", "gate.passed", "gate.started", "gate.passed", "gate.started", "gate.passed", "land.done"}, 3},
		{"base moved 3 times is pending", jinbeSeamRec{landErrs: []error{moved, moved, moved}}, false, "", 3,
			[]string{"gate.started", "gate.passed", "gate.started", "gate.passed", "gate.started", "gate.passed", "gate.pending"}, 3},
		{"pin moved is obsolete", jinbeSeamRec{landErrs: []error{&git.PinMoved{Branch: "b", Pin: "p", Now: "n"}}}, false, "obsolete: branch advanced without landing", 1,
			[]string{"gate.started", "gate.passed", "land.failed"}, 1},
		{"compose conflict never builds", jinbeSeamRec{composeErr: &git.RebaseConflict{Base: "basesha"}}, false, "obsolete: conflict", 1, []string{"land.failed"}, 0},
		{"compose clash never builds", jinbeSeamRec{composeErr: &git.NumberClash{Base: "basesha", Clashes: []string{"x"}}}, false, "obsolete: conflict", 1, []string{"land.failed"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.r
			err, evs := gateRun(t, &r)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			var reason string
			if len(r.reasons) > 0 {
				reason = r.reasons[len(r.reasons)-1]
			}
			if reason != tc.reason || r.composes != tc.composes {
				t.Errorf("reason %q composes %d, want %q %d", reason, r.composes, tc.reason, tc.composes)
			}
			if got := eventlogtest.Names(evs); !slices.Equal(got, tc.events) && !(len(got) == 0 && len(tc.events) == 0) {
				t.Fatalf("events = %v, want %v", got, tc.events)
			}
			if tc.r.composeErr != nil {
				if r.gates != 0 {
					t.Errorf("gate build ran after a compose failure")
				}
				return
			}
			last := evs[len(evs)-1]
			if last.Name == "land.done" || last.Name == "land.failed" {
				last = evs[len(evs)-2]
			}
			a := last.Attrs
			if a["bead"] != "ws-land" || a["pin"] != "cafef00d" || fmt.Sprint(a["attempt"]) != fmt.Sprint(tc.lastAttempt) {
				t.Errorf("%s attrs = %v", last.Name, a)
			}
		})
	}
}

// FR6/FR9: a container that advanced off the Landing's pin since the pre-check, on
// the first compose or after a BaseMoved, closes it obsolete before any build or land.
func TestGatedLandingContainerMovedOffPin(t *testing.T) {
	moved := &git.BaseMoved{Base: "b", Now: "n"}
	for _, tc := range []struct {
		name            string
		r               jinbeSeamRec
		composes, gates int
	}{
		{"on the first compose", jinbeSeamRec{composePins: []string{"newtip"}}, 1, 0},
		{"on attempt 2 after BaseMoved", jinbeSeamRec{composePins: []string{"cafef00d", "newtip"}, landErrs: []error{moved}}, 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.r
			err, _ := gateRun(t, &r)
			if err != nil || !slices.Equal(r.reasons, []string{"obsolete: branch advanced without landing"}) {
				t.Fatalf("err %v reasons %v", err, r.reasons)
			}
			if r.composes != tc.composes || r.gates != tc.gates || r.sbLands != tc.gates {
				t.Errorf("composes %d gates %d lands %d, want %d %d", r.composes, r.gates, r.sbLands, tc.composes, tc.gates)
			}
		})
	}
}

// The gate.infra event carries its tail and timeout at Error; gate.pending is Warn.
func TestGateEventFields(t *testing.T) {
	_, evs := gateRun(t, &jinbeSeamRec{gateErr: &looplock.InfraError{Err: errors.New("x"), Tail: "tail", Timeout: true}})
	e := evs[1]
	if e.Name != "gate.infra" || e.Level != slog.LevelError || e.Attrs["err"] != "tail" || e.Attrs["timeout"] != true || e.Attrs["composed"] != "composed1" || e.Attrs["base"] != "base1" {
		t.Errorf("gate.infra = %+v", e)
	}
	if s := evs[0]; s.Attrs["ref"] != "refs/mugicrew/gate/ws-land" {
		t.Errorf("gate.started = %+v", s)
	}
	_, evs = gateRun(t, &jinbeSeamRec{gateErr: &looplock.InfraError{Err: errors.New("update-ref failed")}})
	if e := evs[1]; e.Name != "gate.infra" || e.Attrs["err"] != "update-ref failed" {
		t.Errorf("empty-tail gate.infra = %+v", e)
	}
	moved := &git.BaseMoved{}
	_, evs = gateRun(t, &jinbeSeamRec{landErrs: []error{moved, moved, moved}})
	if p := evs[len(evs)-1]; p.Name != "gate.pending" || p.Level != slog.LevelWarn || p.Attrs["composed"] != "composed3" {
		t.Errorf("gate.pending = %+v", p)
	}
}

// A non-local strategy never composes or gates a Checkpoint's Landing.
func TestNonLocalStrategyNeverGates(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d"}
	stubJinbeSeams(t, r)
	if err := reconcileLanding(openLanding("cafef00d")); err != nil || r.composes+r.gates != 0 || len(r.landed) != 1 {
		t.Errorf("err %v composes %d gates %d landed %v", err, r.composes, r.gates, r.landed)
	}
}

// A Chart's Landing never composes or gates, even under the local strategy.
func TestChartLandingNeverGates(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", gated: true}
	stubJinbeSeams(t, r)
	showBead = func(string) (bd.Bead, error) {
		return bd.Bead{ID: "ws-cp", Kind: contract.KindChart, Status: "open"}, nil
	}
	landBranch = func(branch, _ string, _ git.LandStrategy) (string, error) {
		r.landed = append(r.landed, branch)
		return "landsha", nil
	}
	if err := reconcileLanding(openLanding("cafef00d")); err != nil || r.composes+r.gates != 0 || len(r.landed) != 1 {
		t.Errorf("err %v composes %d gates %d landed %v", err, r.composes, r.gates, r.landed)
	}
}

// A builder failure files exactly one base-pinned Critique and closes the Landing
// obsolete: build failed; a matching open Critique dedupes without building.
func TestGatedLandingBuilderFailure(t *testing.T) {
	fail := &looplock.BuilderError{Drv: "/nix/store/x.drv", Log: "boom"}
	t.Run("files a Critique", func(t *testing.T) {
		r := &jinbeSeamRec{gateErr: fail}
		err, evs := gateRun(t, r)
		if err != nil || len(r.spawned) != 1 || r.gates != 1 {
			t.Fatalf("err %v spawned %v gates %d", err, r.spawned, r.gates)
		}
		c := r.spawned[0]
		base, _ := git.ParseBase(c.Description)
		tip, _ := git.ParseTip(c.Description)
		if c.Kind != contract.KindCritique || c.Container || c.Title != "Checkpoint ws-cp fails the pinned nix build" || c.DiscoveredFrom != "ws-land" || base != "base1" || tip != "cafef00d" {
			t.Errorf("critique = %+v", c)
		}
		for _, want := range []string{"Drv: /nix/store/x.drv", "```\nboom\n```", "nix build .#mugicrew -L"} {
			if !strings.Contains(c.Description, want) {
				t.Errorf("body missing %q:\n%s", want, c.Description)
			}
		}
		if !slices.Equal(r.reasons, []string{"obsolete: build failed"}) || !slices.Equal(r.closed, []string{"ws-land"}) {
			t.Errorf("closed %v %v", r.closed, r.reasons)
		}
		if got := eventlogtest.Names(evs); !slices.Equal(got, []string{"gate.started", "gate.failed"}) {
			t.Fatalf("events = %v", got)
		}
		e := evs[1]
		if e.Level != slog.LevelError || e.Attrs["drv"] != "/nix/store/x.drv" || e.Attrs["critique"] != "ws-conf" || e.Attrs["composed"] != "composed1" || e.Attrs["attempt"] != 1 {
			t.Errorf("gate.failed = %+v", e)
		}
		if _, ok := e.Attrs["dur_ms"]; !ok {
			t.Errorf("gate.failed lacks dur_ms: %v", e.Attrs)
		}
	})
	t.Run("matching open Critique dedupes without building", func(t *testing.T) {
		body := git.BasePin("base1") + "\n" + git.TipPin("cafef00d")
		r := &jinbeSeamRec{kids: []bd.Bead{{ID: "ws-crit", Kind: contract.KindCritique, Status: "open", Description: body}}}
		err, evs := gateRun(t, r)
		if err != nil || r.gates != 0 || len(r.spawned) != 0 {
			t.Fatalf("err %v gates %d spawned %v", err, r.gates, r.spawned)
		}
		if !slices.Equal(r.reasons, []string{"obsolete: build failed"}) {
			t.Errorf("reasons %v", r.reasons)
		}
		if len(evs) != 1 || evs[0].Name != "gate.failed" || evs[0].Attrs["critique"] != "deduped" || evs[0].Level != slog.LevelError {
			t.Errorf("events = %+v", evs)
		}
	})
	t.Run("a Critique on another base does not dedupe", func(t *testing.T) {
		body := git.BasePin("old") + "\n" + git.TipPin("cafef00d")
		r := &jinbeSeamRec{gateErr: fail, kids: []bd.Bead{{ID: "ws-crit", Kind: contract.KindCritique, Status: "open", Description: body}}}
		err, evs := gateRun(t, r)
		if err != nil || r.gates != 1 {
			t.Errorf("err %v gates %d", err, r.gates)
		}
		// the open-child guard absorbs the failure: nothing spawned, the event names the unrelated Critique
		if len(r.spawned) != 0 || len(evs) == 0 || evs[len(evs)-1].Name != "gate.failed" || evs[len(evs)-1].Attrs["critique"] != "ws-crit" {
			t.Errorf("spawned %v events %+v", r.spawned, evs)
		}
	})
}

// The Rollback's successor Checkpoint is born with the rolled-back Checkpoint's
// context: label; an unlabelled target yields none.
func TestRollbackSuccessorCopiesTheTargetsContext(t *testing.T) {
	for _, tc := range []struct {
		labels []string
		want   string
	}{{[]string{"kind:checkpoint", "context:apps-x", "repo:vendor/x"}, "apps-x"}, {nil, ""}} {
		r := rollbackRec{target: "ws-cp", targetSt: "closed", targetLabels: tc.labels, onMain: true, body: "main broke"}
		stubRollbackSeams(t, &r)
		oLive := liveRepos
		liveRepos = func() map[string]string { return map[string]string{"apps-x": "vendor/x"} } // the stamp matches: no drift
		t.Cleanup(func() { liveRepos = oLive })
		if err := reconcileRollback(r.rollback()); err != nil {
			t.Fatalf("reconcileRollback: %v", err)
		}
		if len(r.successors) != 1 || r.successors[0].Context != tc.want || r.successors[0].Repo != map[bool]string{true: "vendor/x"}[tc.want != ""] {
			t.Errorf("successors %+v, want one with Context %q and its Repo stamp", r.successors, tc.want)
		}
	}
}

// A Checkpoint Landing passes the at-head Approval's body with its Commit: pin line
// stripped (an empty packet for a pin-only Approval); a Chart Landing passes none.
func TestLandingPassesDecisionPacket(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"pin and packet", git.CommitPin("cafef00d") + "\n\n## Background\n\nwhy\n", "## Background\n\nwhy"},
		{"pin only", git.CommitPin("cafef00d"), ""},
		{"pre-packet spawn text", "Clean pass.\n" + git.CommitPin("cafef00d"), ""},
	} {
		for _, gated := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s gated=%v", tc.name, gated), func(t *testing.T) {
				r := &jinbeSeamRec{head: "cafef00d", gated: gated, approvalBody: tc.body}
				stubJinbeSeams(t, r)
				approvedAtHead = func(string, string) bool { return true }
				t.Cleanup(func() { approvedAtHead = git.ApprovedAtHead })
				if err := reconcileLanding(openLanding("cafef00d")); err != nil {
					t.Fatal(err)
				}
				if gated && (len(r.composePackets) != 1 || r.composePackets[0] != tc.want) {
					t.Errorf("compose packets = %q, want [%q]", r.composePackets, tc.want)
				}
			})
		}
	}
	t.Run("chart", func(t *testing.T) {
		r := &jinbeSeamRec{head: "cafef00d", approvalBody: git.CommitPin("cafef00d") + "\n\n## Background"}
		stubJinbeSeams(t, r)
		showBead = func(string) (bd.Bead, error) {
			return bd.Bead{ID: "ws-cp", Kind: contract.KindChart, Status: "open"}, nil
		}
		if err := reconcileLanding(openLanding("cafef00d")); err != nil || r.composes != 0 {
			t.Errorf("err %v composes %d, want a Chart Landing that never composes a packet", err, r.composes)
		}
	})
}

// With no gate key the Landing lands ungated: no build, a gate.skipped event.
func TestNoGateKeySkipsBuild(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", gated: true}
	evs := eventlogtest.Capture(t)
	stubJinbeSeams(t, r)
	gateAttr = func() string { return "" }
	err := reconcileLanding(openLanding("cafef00d"))
	if err != nil || r.gates != 0 || len(r.landed) != 1 {
		t.Errorf("err %v gates %d landed %v", err, r.gates, r.landed)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"gate.skipped", "land.done"}) {
		t.Errorf("events = %v", got)
	}
}

// Under gitlab the fetch runs before the trailer read: a trailer a human landed on
// the Forge closes the Landing landed: <sha>, with no land.
func TestGitlabLandingFetchesThenClosesOnAForgeTrailer(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", fetchLands: true}
	stubJinbeSeams(t, r)
	if err := landingRun(r, "cafef00d"); err != nil {
		t.Fatal(err)
	}
	if r.fetches != 1 || len(r.landed) != 0 || !slices.Equal(r.closed, []string{"ws-land"}) || r.reasons[0] != "landed: landsha" {
		t.Errorf("fetches %d landed %v closed %v %v, want one fetch and a landed: close", r.fetches, r.landed, r.closed, r.reasons)
	}
}

// Under gitlab the Landing's tick closes the MR still open on the branch (even the one its
// own MR: line names) before closing landed, and logs it.
func TestGitlabLandedCloseClosesTheOpenMRFirst(t *testing.T) {
	for _, own := range []bool{false, true} {
		r := &jinbeSeamRec{head: "cafef00d", fetchLands: true, openMR: &forge.MR{IID: 9, State: "opened"}}
		stubJinbeSeams(t, r)
		evs := eventlogtest.Capture(t)
		r.closeAfterMR(t)
		l := openLanding("cafef00d")
		if own {
			l.Description += "\nMR: !9\n"
		}
		if err := reconcileLanding(l); err != nil {
			t.Fatal(err)
		}
		var e eventlogtest.Event
		for _, ev := range *evs {
			if ev.Name == "land.mr_closed" {
				e = ev
			}
		}
		if !slices.Equal(r.mrsClosed, []int{9}) || len(r.closed) != 1 || e.Attrs["mr"] == nil || e.Attrs["reason"] != "container landed" {
			t.Errorf("own=%v: MRs closed %v, beads %v, event %v; want MR 9, the Landing and an event", own, r.mrsClosed, r.closed, e)
		}
	}
}

// No open MR: no CloseMR call. A Forge error leaves the Landing open; a later tick closes both.
func TestGitlabLandedCloseNoMRAndErrors(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", fetchLands: true}
	stubJinbeSeams(t, r)
	if err := landingRun(r, "cafef00d"); err != nil || len(r.mrsClosed) != 0 || len(r.closed) != 1 {
		t.Fatalf("err %v closed MRs %v beads %v, want no MR call and a close", err, r.mrsClosed, r.closed)
	}
	for name, c := range map[string]*jinbeSeamRec{
		"a 429 is held, not returned": {head: "cafef00d", fetchLands: true, findErr: forge.ErrRateLimited},
		"a close error is returned":   {head: "cafef00d", fetchLands: true, openMR: &forge.MR{IID: 9}, closeMRErr: errors.New("boom")},
	} {
		t.Run(name, func(t *testing.T) {
			stubJinbeSeams(t, c)
			err := landingRun(c, "cafef00d")
			if held := errors.Is(c.findErr, forge.ErrRateLimited); held && err != nil || !held && err == nil || len(c.closed) != 0 {
				t.Fatalf("err %v closed %v, want an open Landing (held 429: no error; else the error)", err, c.closed)
			}
			c.findErr, c.closeMRErr, c.openMR = nil, nil, &forge.MR{IID: 9}
			if err := landingRun(c, "cafef00d"); err != nil || !slices.Equal(c.mrsClosed, []int{9}) || len(c.closed) != 1 {
				t.Fatalf("retry: err %v MRs %v beads %v", err, c.mrsClosed, c.closed)
			}
		})
	}
}

// A non-gitlab strategy calls no Forge seam.
func TestLocalLandedCloseMakesNoForgeCall(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", trailerLanded: true, gated: true, openMR: &forge.MR{IID: 9}}
	stubJinbeSeams(t, r)
	if err := landingRun(r, "cafef00d"); err != nil || len(r.projects) != 0 || len(r.mrsClosed) != 0 {
		t.Errorf("err %v projects %v MRs %v, want no Forge call", err, r.projects, r.mrsClosed)
	}
}

// A nested Landing looks up and closes on the child's project, with the nested attrs.
func TestGitlabLandedCloseUsesTheLegsProject(t *testing.T) {
	r := &jinbeSeamRec{openMR: &forge.MR{IID: 9}}
	stubJinbeSeams(t, r)
	evs := eventlogtest.Capture(t)
	lg := legs{proj: childProject, attrs: []any{"context", "child"}}
	if err := landedClose(openLanding("x"), openCheckpoint(), "checkpoint/ws-cp", "landsha", "gitlab", lg); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.mrsClosed, []int{9}) || slices.ContainsFunc(r.projects, func(p forge.Project) bool { return p != childProject }) || eventNamed(*evs, "land.mr_closed").Attrs["context"] != "child" {
		t.Errorf("MRs %v projects %v, want 9 on the child's only with nested attrs", r.mrsClosed, r.projects)
	}
}

// A diverged local main leaves the Landing open and is returned.
func TestGitlabLandingDivergedMainStaysOpen(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", fetchErr: &git.MainDiverged{Local: "a", Forge: "b"}}
	stubJinbeSeams(t, r)
	var d *git.MainDiverged
	if err := landingRun(r, "cafef00d"); !errors.As(err, &d) {
		t.Fatalf("err = %v, want *git.MainDiverged", err)
	}
	if len(r.closed)+len(r.landed) != 0 {
		t.Errorf("closed %v landed %v, want nothing", r.closed, r.landed)
	}
}

// An ahead local main leaves the Landing open like diverge does.
func TestGitlabLandingAheadMainStaysOpen(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", fetchErr: &git.MainAhead{Remote: "origin", Main: "main", Commits: []string{"x"}}}
	stubJinbeSeams(t, r)
	var a *git.MainAhead
	if err := landingRun(r, "cafef00d"); !errors.As(err, &a) {
		t.Fatalf("err = %v, want *git.MainAhead", err)
	}
	if len(r.closed)+len(r.landed) != 0 {
		t.Errorf("closed %v landed %v, want nothing", r.closed, r.landed)
	}
}

// A local Landing performs no fetch.
func TestLocalLandingDoesNotFetch(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", gated: true}
	stubJinbeSeams(t, r)
	_ = landingRun(r, "cafef00d")
	if r.fetches != 0 {
		t.Errorf("fetches = %d, want 0 under local", r.fetches)
	}
}

// A merged MR is read after the fetch (Spec FR 18–21): a trailer on mainline closes the
// Landing landed; a trailerless merge leaves it open, errors and files one Task across
// ticks; a stale-pin merge closes landed and files one Signal naming Tip:, head and range,
// winning over "obsolete: branch advanced" for the same tick.
func TestLandingMergedMR(t *testing.T) {
	merged := &forge.MR{IID: 7, State: "merged", MergeCommitSHA: "m3rg3"}
	land := func(r *jinbeSeamRec, pin string) error {
		stubJinbeSeams(t, r)
		l := openLanding(pin)
		l.Description += "\nMR: !7\n"
		return reconcileLanding(l)
	}
	t.Run("merged", func(t *testing.T) {
		r := &jinbeSeamRec{head: "cafef00d", fetchLands: true, landedTip: "cafef00d", mr: merged}
		if err := land(r, "cafef00d"); err != nil || len(r.reasons) != 1 || r.reasons[0] != "landed: landsha" || len(r.signals)+len(r.tasks) != 0 {
			t.Errorf("err %v reasons %v signals %v tasks %v, want landed and nothing filed", err, r.reasons, r.signals, r.tasks)
		}
	})
	t.Run("merged-after-race", func(t *testing.T) { // the merge landed between the tick's fetch and the MR read
		r := &jinbeSeamRec{head: "cafef00d", landsOnFetch: 2, landedTip: "cafef00d", mr: merged}
		if err := land(r, "cafef00d"); err != nil || r.fetches != 2 || len(r.reasons) != 1 || r.reasons[0] != "landed: landsha" || len(r.tasks) != 0 {
			t.Errorf("err %v fetches %d reasons %v tasks %v, want a second fetch, landed and no Task", err, r.fetches, r.reasons, r.tasks)
		}
	})
	t.Run("merged-trailerless", func(t *testing.T) {
		r := &jinbeSeamRec{head: "cafef00d", mr: merged}
		for range 2 {
			if err := land(r, "cafef00d"); err == nil || !strings.Contains(err.Error(), "m3rg3") {
				t.Fatalf("err = %v, want one naming the merge sha", err)
			}
		}
		if len(r.closed) != 0 || len(r.tasks) != 1 || !strings.Contains(r.taskBody, "!7") || !strings.Contains(r.taskBody, "m3rg3") {
			t.Errorf("closed %v tasks %v body %q, want the Landing open and one Task naming the MR and sha", r.closed, r.tasks, r.taskBody)
		}
		if !strings.Contains(r.taskBody, "ADR or other sequence number") { // Spec FR 29
			t.Errorf("body %q, want the line asking the human to check for a clashing number", r.taskBody)
		}
	})
	t.Run("merged-landedbase-fails", func(t *testing.T) { // a transient read failure is not a trailerless merge
		boom := errors.New("bd sql down")
		r := &jinbeSeamRec{head: "cafef00d", mr: merged, landedErr: boom}
		if err := land(r, "cafef00d"); !errors.Is(err, boom) || len(r.closed) != 0 || len(r.tasks) != 0 {
			t.Errorf("err %v closed %v tasks %v, want the read error, the Landing open and no Task", err, r.closed, r.tasks)
		}
	})
	t.Run("merged-stale-pin", func(t *testing.T) {
		r := &jinbeSeamRec{head: "newhead", fetchLands: true, landedTip: "oldtip", mr: merged}
		if err := land(r, "oldtip"); err != nil || len(r.reasons) != 1 || r.reasons[0] != "landed: landsha" {
			t.Fatalf("err %v reasons %v, want landed", err, r.reasons)
		}
		if len(r.signals) != 1 {
			t.Fatalf("signals = %v, want one", r.signals)
		}
		for _, want := range []string{"oldtip", "newhead", "oldtip..newhead"} {
			if !strings.Contains(r.signals[0], want) {
				t.Errorf("Signal %q lacks %q", r.signals[0], want)
			}
		}
	})
}

// Each gitlab Forge outcome is a land.* event carrying the MR iid (Spec FR 49).
func TestLandingForgeOutcomesRecordEventsWithMR(t *testing.T) {
	failed := &forge.Pipeline{ID: 42, Status: "failed"}
	real := []forge.Job{{ID: 6, Name: "lint", FailureReason: "script_failure"}}
	for _, c := range []struct {
		name  string
		mr    forge.MR
		body  string
		trial error
		trail bool
		want  string
	}{
		{"retried", forge.MR{IID: 7, State: "opened", HeadPipeline: failed}, "", nil, false, "land.retried"},
		{"pipeline failed", forge.MR{IID: 7, State: "opened", HeadPipeline: failed}, "Retried: pipeline 42\n", nil, false, "land.pipeline_failed"},
		{"conflict", forge.MR{IID: 7, State: "opened", HasConflicts: true}, "", nil, false, "land.conflict"},
		{"closed", forge.MR{IID: 7, State: "closed"}, "", nil, false, "land.closed"},
		{"landed", forge.MR{IID: 7, State: "merged"}, "", nil, true, "land.landed"},
	} {
		t.Run(c.name, func(t *testing.T) {
			evs := eventlogtest.Capture(t)
			r := &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7}, mr: &c.mr, jobs: real, trailerLanded: c.trail}
			stubJinbeSeams(t, r)
			l := openLanding("cafef00d")
			l.Description += "\nMR: !7\n" + c.body
			if err := reconcileLanding(l); err != nil {
				t.Fatalf("err = %v", err)
			}
			for _, e := range *evs {
				if e.Name == c.want {
					if e.Attrs["mr"] != int64(7) && e.Attrs["mr"] != 7 {
						t.Errorf("%s mr = %v, want 7", c.want, e.Attrs["mr"])
					}
					return
				}
			}
			t.Errorf("events %v lack %s", eventlogtest.Names(*evs), c.want)
		})
	}
}

// A first failed pipeline on a moved mainline records Checked: and Retried: in one
// body, not the second over the first's stale copy.
func TestFailedPipelineKeepsCheckedLine(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7},
		mr: &forge.MR{IID: 7, State: "opened", HeadPipeline: &forge.Pipeline{ID: 42, Status: "failed"}}}
	stubJinbeSeams(t, r)
	l := openLanding("cafef00d")
	l.Description += "\nMR: !7\n"
	if err := reconcileLanding(l); err != nil || len(r.bodies) != 2 {
		t.Fatalf("err %v bodies %q, want Checked: then Retried: writes", err, r.bodies)
	}
	if last := r.bodies[1]; !strings.Contains(last, "\nChecked: forgesha\n") || !strings.Contains(last, "\nRetried: pipeline 42\n") {
		t.Errorf("last body %q lacks Checked: or Retried:", last)
	}
}

// Under local the whole Landing never runs glab, even when it is on PATH (Spec FR 2).
func TestLocalLandingNeverInvokesGlab(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "glab"), []byte("#!/bin/sh\ntouch "+dir+"/invoked\nexit 1\n"), 0o755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	r := &jinbeSeamRec{head: "cafef00d", gated: true}
	stubJinbeSeams(t, r)
	if err := landingRun(r, "cafef00d"); err != nil {
		t.Errorf("landingRun: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "invoked")); err == nil {
		t.Error("glab was invoked by a local Landing")
	}
	if r.fetches+r.mrReads+r.jobReads+len(r.retries)+len(r.descAsserts) != 0 {
		t.Errorf("Forge seams called by a local Landing: fetches %d, mrReads %d, jobReads %d, retries %v, asserts %v",
			r.fetches, r.mrReads, r.jobReads, r.retries, r.descAsserts)
	}
}

// A stale-pin squash merged before the Landing's own tick: the Checkpoint's reconcile
// closes the Landing landed, and files the offshoot Signal first (Spec FR 20).
func TestReconcileCheckpointStaleMergedPinFilesSignal(t *testing.T) {
	r := &jinbeSeamRec{head: "newhead", landedTip: "oldtip", trailerLanded: true, landings: []bd.Bead{openLanding("oldtip")}}
	stubJinbeSeams(t, r)
	if err := reconcileCheckpoint(openCheckpoint()); err != nil {
		t.Fatalf("reconcileCheckpoint: %v", err)
	}
	if len(r.signals) != 1 || !strings.Contains(r.signals[0], "oldtip..newhead") {
		t.Errorf("signals = %v, want one naming oldtip..newhead", r.signals)
	}
}

// A branch rewritten after the landed pin gets the range-diff body, with no Range: line (Spec FR 39).
func TestFlagStalePinRewrittenPointsAtRangeDiff(t *testing.T) {
	r := &jinbeSeamRec{head: "newhead", landedTip: "oldtip", trailerLanded: true, rewritten: true, landings: []bd.Bead{openLanding("oldtip")}}
	stubJinbeSeams(t, r)
	if err := reconcileCheckpoint(openCheckpoint()); err != nil {
		t.Fatalf("reconcileCheckpoint: %v", err)
	}
	if len(r.signals) != 1 || strings.Contains(r.signals[0], "Range:") || !strings.Contains(r.signals[0], "git range-diff oldtip...newhead") {
		t.Errorf("signals = %v, want one with range-diff wording and no Range:", r.signals)
	}
}

// A transient head-read failure keeps the Landing open so the Signal is not lost.
func TestFlagStalePinHeadErrorIsReturned(t *testing.T) {
	r := &jinbeSeamRec{landedTip: "oldtip"}
	stubJinbeSeams(t, r)
	lg := legs{head: func(string) (string, error) { return "", errors.New("transient") }}
	if err := flagStalePin(openLanding("oldtip"), openCheckpoint(), "checkpoint/ws-cp", "landsha", lg); err == nil {
		t.Error("a head read failure must be returned")
	}
	lg.head = func(string) (string, error) {
		return "", git.ErrMissingRef
	}
	if err := flagStalePin(openLanding("oldtip"), openCheckpoint(), "checkpoint/ws-cp", "landsha", lg); err != nil || len(r.signals) != 0 {
		t.Errorf("missing branch: err %v signals %v, want nothing", err, r.signals)
	}
}

// A Landing that closed while limited is pruned on the next new 429, not kept forever.
func TestRateLimitedPrunesClosedLandings(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", landErr: forge.ErrRateLimited}
	stubJinbeSeams(t, r)
	rateLimited.Store("ws-gone", true)
	rateLimited.Delete("ws-land")
	t.Cleanup(func() { rateLimited.Delete("ws-gone") })
	showBead = func(id string) (bd.Bead, error) {
		if id == "ws-gone" {
			return bd.Bead{ID: id, Status: "closed"}, nil
		}
		return openCheckpoint(), nil
	}
	if err := reconcileLanding(openLanding("cafef00d")); err != nil {
		t.Fatal(err)
	}
	if _, ok := rateLimited.Load("ws-gone"); ok {
		t.Error("closed Landing still in rateLimited")
	}
}

// A second pipeline on the same Landing (a human re-ran it) is not retried again once
// its own "Retried:" line is recorded, even though an earlier pipeline's line comes first.
func TestLandingDoesNotRetryASecondPipelineTwice(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7},
		jobs: []forge.Job{{ID: 6, Name: "lint", WebURL: "https://gl/j/6", FailureReason: "script_failure"}}, targetSHA: "base9",
		mr: &forge.MR{IID: 7, State: "opened", HeadPipeline: &forge.Pipeline{ID: 43, Status: "failed", WebURL: "https://gl/p/43"}}}
	stubJinbeSeams(t, r)
	showBead = func(string) (bd.Bead, error) {
		return bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Title: "Ship"}, nil
	}
	l := openLanding("cafef00d")
	l.Description += "\nMR: !7\nRetried: pipeline 42\nRetried: pipeline 43\n"
	if err := reconcileLanding(l); err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(r.retries) != 0 {
		t.Errorf("retries %v, want none: pipeline 43 was already retried", r.retries)
	}
}

// The only Landing closed obsolete and its MR was merged afterwards: the container still
// closes landed and the closed Landing is flagged (Spec FR 20).
func TestReconcileCheckpointStaleMergedPinFlagsClosedLanding(t *testing.T) {
	l := openLanding("oldtip")
	l.Status, l.CloseReason = "closed", "obsolete: branch advanced without landing"
	r := &jinbeSeamRec{head: "newhead", landedTip: "oldtip", trailerLanded: true, landings: []bd.Bead{l}}
	stubJinbeSeams(t, r)
	if err := reconcileCheckpoint(openCheckpoint()); err != nil {
		t.Fatalf("reconcileCheckpoint: %v", err)
	}
	if len(r.signals) != 1 || !strings.Contains(r.signals[0], "oldtip..newhead") {
		t.Errorf("signals = %v, want one naming oldtip..newhead", r.signals)
	}
}

// Under local, a full Landing never invokes glab (Spec FR 2), even with a failing one on PATH.
func TestLandingLocalNeverInvokesGlab(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "glab"), []byte("#!/bin/sh\ntouch "+dir+"/invoked\nexit 1\n"), 0o755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	r := &jinbeSeamRec{head: "cafef00d"}
	stubJinbeSeams(t, r)
	if err := landingRun(r, "cafef00d"); err != nil || !slices.Equal(r.closed, []string{"ws-land"}) {
		t.Fatalf("err %v closed %v, want the Landing closed", err, r.closed)
	}
	if _, err := os.Stat(filepath.Join(dir, "invoked")); err == nil {
		t.Error("glab was invoked under local")
	}
}

// reconcileCheckpoint on a foreign land files the hold Task once even with no Landing
// (FR 41), and keeps returning the refusal.
func TestReconcileCheckpointForeignLandFilesHoldOnce(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", foreign: &git.ForeignLanding{Trailer: "Checkpoint: ws-cp", Ours: "ourgraph", Theirs: "theirgraph", Sha: "f00dbeef"}}
	stubJinbeSeams(t, r)
	for range 2 {
		var fe *git.ForeignLanding
		if err := reconcileCheckpoint(openCheckpoint()); !errors.As(err, &fe) {
			t.Fatalf("err = %v, want *ForeignLanding", err)
		}
	}
	if len(r.tasks) != 1 || len(r.spawned) != 0 {
		t.Errorf("tasks %v spawned %v, want one hold Task and no Landing", r.tasks, r.spawned)
	}
}

// checkpointApproval returns only an Approval closed exactly "approved" at the tip
// (ADR 0203 §5).
func TestCheckpointApprovalCountsOnlyApprovedClose(t *testing.T) {
	oc, oa := closedChildrenOfKind, approvedAtHead
	t.Cleanup(func() { closedChildrenOfKind, approvedAtHead = oc, oa })
	approvedAtHead = func(string, string) bool { return true }
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
		closedChildrenOfKind = func(string, contract.Kind) ([]bd.Bead, error) {
			return []bd.Bead{{Kind: contract.KindApproval, Status: "closed", CloseReason: tc.reason, Description: "Commit: tip"}}, nil
		}
		got, err := checkpointApproval(bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint})
		if err != nil || (got != nil) != tc.want {
			t.Errorf("close reason %q: approval = %v, %v; want found=%v", tc.reason, got, err, tc.want)
		}
	}
}

// graphPushRec stubs the Graph push legs and returns what they saw.
func graphPushRec(t *testing.T, err error) (pushes *[]string, tasks *[]string) {
	t.Helper()
	pushes, tasks = &[]string{}, &[]string{}
	oUser, oPush, oTask := forgeUser, graphPush, spawnUnblockingTask
	t.Cleanup(func() { forgeUser, graphPush, spawnUnblockingTask = oUser, oPush, oTask })
	forgeUser = func() (string, error) { return "brook", nil }
	graphPush = func(name string) (bd.GraphPushResult, error) {
		*pushes = append(*pushes, name)
		return bd.GraphPushResult{}, err
	}
	spawnUnblockingTask = func(key, _, _ string) (string, bool, error) {
		*tasks = append(*tasks, key)
		return "ws-gt", true, nil
	}
	return pushes, tasks
}

// A LandPending write leg pushes the Graph ref once the MR is recorded; a failed push
// is an event, never an error (ADR 0216).
func TestLandPendingPushesTheGraphAndNeverHolds(t *testing.T) {
	evs := eventlogtest.Capture(t)
	r := &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7}}
	stubJinbeSeams(t, r)
	pushes, tasks := graphPushRec(t, errors.New("network down"))
	if err := reconcileLanding(openLanding("cafef00d")); err != nil {
		t.Fatalf("a failed Graph push must not hold the Landing: %v", err)
	}
	if len(*pushes) != 1 || (*pushes)[0] != "brook" || len(*tasks) != 0 {
		t.Errorf("pushes %v tasks %v, want one push for brook and no Task", *pushes, *tasks)
	}
	if !slices.Contains(eventlogtest.Names(*evs), "graph.push_failed") {
		t.Errorf("events %v, want graph.push_failed", eventlogtest.Names(*evs))
	}
}

func TestSuccessfulGraphPushIsRecorded(t *testing.T) {
	evs := eventlogtest.Capture(t)
	r := &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7}}
	stubJinbeSeams(t, r)
	graphPushRec(t, nil)
	reconcileLanding(openLanding("cafef00d"))
	if !slices.Contains(eventlogtest.Names(*evs), "graph.push") {
		t.Errorf("events %v, want graph.push", eventlogtest.Names(*evs))
	}
}

// A failed recordMR publishes nothing.
func TestFailedRecordMRDoesNoGraphPush(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7}}
	stubJinbeSeams(t, r)
	pushes, _ := graphPushRec(t, nil)
	updateBody = func(string, []string) error { return errors.New("bd down") }
	if err := reconcileLanding(openLanding("cafef00d")); err == nil {
		t.Fatal("recordMR failure must be returned")
	}
	if len(*pushes) != 0 {
		t.Errorf("pushes %v, want none", *pushes)
	}
}

// A pending tick (the MR already recorded, mirrorMR) never pushes.
func TestPendingTickDoesNoGraphPush(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7}, mr: &forge.MR{IID: 7, State: "opened"}}
	stubJinbeSeams(t, r)
	pushes, _ := graphPushRec(t, nil)
	l := openLanding("cafef00d")
	l.Description += "\nMR: !7"
	reconcileLanding(l)
	if len(*pushes) != 0 {
		t.Errorf("pushes %v, want none", *pushes)
	}
}

// The typed foreign-root refusal files the non-blocking Task, keyed on the ref and
// the foreign Graph id, and still never holds the Landing.
func TestForeignGraphRefFilesTheTask(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7}}
	stubJinbeSeams(t, r)
	_, tasks := graphPushRec(t, &bd.ForeignGraphRef{Ref: "refs/mugicrew/graphs/brook", Ours: "aaa", Theirs: "bbb"})
	if err := reconcileLanding(openLanding("cafef00d")); err != nil {
		t.Fatalf("foreign ref must not hold the Landing: %v", err)
	}
	if want := "Graph ref: refs/mugicrew/graphs/brook\nTheirs: bbb\n"; len(*tasks) != 1 || (*tasks)[0] != want {
		t.Errorf("tasks %q, want [%q]", *tasks, want)
	}
}

// gitlabRollback is a landed target's Rollback under gitlab, as the graph holds it.
func gitlabRollback() *rollbackRec {
	return &rollbackRec{gitlab: true, target: "ws-cp", targetSt: "closed", onMain: true, body: "main broke because …"}
}

// First tick: the revert is built on the fetched main, pinned, pushed and opened as an
// MR recorded on the body; later ticks reuse the pin and push nothing (ADR 0213 §2).
func TestGitlabRollbackPinsPushesThenReusesThePin(t *testing.T) {
	r := gitlabRollback()
	stubRollbackSeams(t, r)
	evs := eventlogtest.Capture(t)
	for range 3 {
		if err := reconcileRollback(r.rollback()); err != nil {
			t.Fatal(err)
		}
	}
	if r.built != 1 || r.reverted[0] != [2]string{"forgesha squashsha", "Reverts-Checkpoint: ws-cp"} {
		t.Errorf("built %d %v, want one revert on the fetched main", r.built, r.reverted)
	}
	if want := []string{"ws-rb Reverts-Checkpoint: ws-cp pinsha"}; !slices.Equal(r.pushes, want) {
		t.Errorf("pushes = %q, want one: %q", r.pushes, want)
	}
	if !strings.Contains(r.body, "Commit: pinsha") || !strings.Contains(r.body, "MR: !9") || !strings.HasPrefix(r.body, "main broke") {
		t.Errorf("body = %q, want the evidence kept and Commit:/MR: pinned", r.body)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"revert.pending", "revert.pending", "revert.pending"}) {
		t.Errorf("events = %v", got)
	}
	if len(r.closed)+len(r.successors) != 0 || r.fetches != 3 {
		t.Errorf("closed %v successors %v fetches %d, want an open Rollback with no successor", r.closed, r.successors, r.fetches)
	}
	if n := len(r.descAsserts); n != 2 || r.descAsserts[0] != "" {
		t.Errorf("descAsserts = %q, want the tip-less block asserted on the two pending ticks", r.descAsserts)
	}
}

// Once the trailer is on the fetched main the successor is cut at the normalised merge,
// revert.done (sha=M) precedes handoff.done, and the MR closes after the successor exists.
func TestGitlabRollbackHandsOffOnTheMerge(t *testing.T) {
	r := gitlabRollback()
	r.trailerLanded, r.mergedAt = true, "msha"
	r.body += "\nCommit: pinsha\nMR: !9\n"
	stubRollbackSeams(t, r)
	evs := eventlogtest.Capture(t)
	if err := reconcileRollback(r.rollback()); err != nil {
		t.Fatal(err)
	}
	if got := r.branches["checkpoint/ws-new"]; got != "seedsha" || r.seeds != 1 {
		t.Errorf("successor tip %q seeds %d, want cut at the merge and seeded once", got, r.seeds)
	}
	if r.built != 0 || len(r.pushes) != 0 {
		t.Errorf("built %d pushes %v, want no revert work after the merge", r.built, r.pushes)
	}
	if !slices.Equal(r.mrClosed, []int{9}) || !slices.Equal(r.closedAtSet, []string{"1"}) {
		t.Errorf("MR closes %v with %v successors, want !9 closed after the successor", r.mrClosed, r.closedAtSet)
	}
	if !slices.Equal(r.closed, []string{"ws-rb"}) {
		t.Errorf("closed %v, want the Rollback", r.closed)
	}
	var names []string
	for _, e := range *evs {
		names = append(names, e.Name)
		if e.Name == "revert.done" && (e.Attrs["sha"] != "msha" || e.Attrs["mr"] != 9) {
			t.Errorf("revert.done attrs = %v, want sha=msha mr=9", e.Attrs)
		}
	}
	if want := []string{"revert.done", "handoff.done", "revert.mr_closed"}; !slices.Equal(names, want) {
		t.Errorf("events = %v, want %v", names, want)
	}
}

// An invalid Rollback closes its still-open MR; a closed MR is left alone.
func TestGitlabRollbackInvalidClosesItsOpenMR(t *testing.T) {
	r := gitlabRollback()
	r.targetSt, r.body = "open", "x\nMR: !9\n"
	stubRollbackSeams(t, r)
	if err := reconcileRollback(r.rollback()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.mrClosed, []int{9}) || r.reasons["ws-rb"] != "invalid: ws-cp not landed" {
		t.Errorf("MR closes %v reasons %v, want !9 closed and the Rollback invalid", r.mrClosed, r.reasons)
	}
}

// A 429 holds the Rollback: no error, no close, one rate_limited event across ticks.
func TestGitlabRollbackRateLimitedHolds(t *testing.T) {
	r := gitlabRollback()
	r.mrErr = fmt.Errorf("read: %w", forge.ErrRateLimited)
	r.body += "\nMR: !9\n"
	stubRollbackSeams(t, r)
	evs := eventlogtest.Capture(t)
	for range 2 {
		if err := reconcileRollback(r.rollback()); err != nil {
			t.Fatalf("err = %v, want the 429 held", err)
		}
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"revert.rate_limited"}) || len(r.closed) != 0 {
		t.Errorf("events %v closed %v, want one rate_limited and an open Rollback", got, r.closed)
	}
}

// The two projects the seam recorders tell apart: the Workspace's, and a nested child's.
var (
	workspaceProject = forge.Project{Host: "gitlab.test", ID: "ns%2Fp"}
	childProject     = forge.Project{Host: "gitlab.test", ID: "ns%2Fchild"}
)

// A nested Landing reads the landed Tip: through the child repo, where the sha lives.
func TestFlagStalePinReadsTheTipInTheChild(t *testing.T) {
	r := &jinbeSeamRec{}
	stubJinbeSeams(t, r)
	var readIn git.Repo
	landedTip = func(rp git.Repo, _ string) (string, bool) { readIn = rp; return "oldtip", true }
	lg := legs{repo: "/ws/kid", head: func(string) (string, error) { return "newhead", nil }}
	if err := flagStalePin(openLanding("oldtip"), openCheckpoint(), "checkpoint/ws-cp", "landsha", lg); err != nil {
		t.Fatal(err)
	}
	if readIn != "/ws/kid" || len(r.signals) != 1 {
		t.Errorf("tip read in %q, signals %v; want the child and one Signal", readIn, r.signals)
	}
}

// A 429 on a no-context Workspace Landing or Rollback logs with no bead read past the
// attempt's own (Spec FR 33): holdRateLimited does no I/O.
func TestRateLimitedHoldReadsNoBeadsOnWorkspace(t *testing.T) {
	rateLimited.Clear() // other tests' entries would make pruneRateLimited read their beads
	t.Run("landing", func(t *testing.T) {
		stubJinbeSeams(t, &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7}, mrErr: forge.ErrRateLimited})
		l := openLanding("cafef00d")
		l.Description += "\nMR: !7\n"
		nested, err := reconcileLandingOnce(l)
		reads := 0
		showBead = func(id string) (bd.Bead, error) { // pruneRateLimited re-reads the limited bead itself; any other read is the defect
			if id != l.ID {
				reads++
			}
			return bd.Bead{}, nil
		}
		if herr := holdRateLimited(l, "land.rate_limited", err, nested); herr != nil || reads != 0 {
			t.Errorf("err %v, %d bead reads after the attempt, want nil and 0", herr, reads)
		}
	})
	t.Run("rollback", func(t *testing.T) {
		r := gitlabRollback()
		r.mrErr = fmt.Errorf("read: %w", forge.ErrRateLimited)
		r.body += "\nMR: !9\n"
		stubRollbackSeams(t, r)
		rb := r.rollback()
		nested, err := reconcileRollbackOnce(rb)
		reads := 0
		showBead = func(id string) (bd.Bead, error) { // pruneRateLimited re-reads the limited bead itself; any other read is the defect
			if id != rb.ID {
				reads++
			}
			return bd.Bead{}, nil
		}
		rollbackTarget = func(string) (string, bool, error) { reads++; return "", false, nil }
		if herr := holdRateLimited(rb, "revert.rate_limited", err, nested); herr != nil || reads != 0 {
			t.Errorf("err %v, %d bead reads after the attempt, want nil and 0", herr, reads)
		}
	})
}

// A gitlab Landing's write leg presents the MR with the container's title and body: a
// Checkpoint's Decision Packet ("" for a pin-only Approval), a Chart's summary.
func TestGitlabLandingPresentsTheMR(t *testing.T) {
	chart := "## Destination\n\nGo here.\n\n## Decisions-so-far\n\n## Notes\nn\n"
	for _, tc := range []struct {
		name, kind, approval, want, jira string
	}{
		{"checkpoint packet", "checkpoint", git.CommitPin("cafef00d") + "\n\n## Background\n\nwhy\n", "## Background\n\nwhy", ""},
		{"packet-less approval", "checkpoint", git.CommitPin("cafef00d"), "", ""},
		{"chart summary", "chart", "", "Go here.", ""},
		{"jira prefix", "chart", "", "Go here.", "ABC-1: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &jinbeSeamRec{head: "cafef00d", approvalBody: tc.approval, landErr: &git.LandPending{MR: 5}}
			stubJinbeSeams(t, r)
			graphPushRec(t, nil)
			approvedAtHead = func(string, string) bool { return true }
			t.Cleanup(func() { approvedAtHead = git.ApprovedAtHead })
			showBead = func(string) (bd.Bead, error) {
				b := bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open", Title: "Ship it"}
				if tc.kind == "chart" {
					b.Kind, b.Description = contract.KindChart, chart
				}
				return b, nil
			}
			if tc.jira != "" {
				jiraPrefix = func(bd.Bead) (string, error) { return tc.jira, nil }
			}
			var got git.MRPresentation
			o := pinnedGitlab
			t.Cleanup(func() { pinnedGitlab = o })
			pinnedGitlab = func(_ git.Repo, _ string, p git.MRPresentation) git.LandStrategy {
				got = p
				return func(string, string) (string, error) { return "", r.landErr }
			}
			if err := landingRun(r, "cafef00d"); err != nil {
				t.Fatal(err)
			}
			if want := (git.MRPresentation{Title: tc.jira + "Ship it", Body: tc.want}); got != want {
				t.Errorf("presentation = %+v, want %+v", got, want)
			}
		})
	}
}

// A merged trailer that beats the Landing's tick: the roll-forward closes the MR still open
// on the branch and logs it, only then the Landing and the container.
func TestGitlabRollForwardClosesTheOpenMRFirst(t *testing.T) {
	r := &jinbeSeamRec{trailerLanded: true, openMR: &forge.MR{IID: 9}, landings: []bd.Bead{openLanding("cafef00d")}}
	stubJinbeSeams(t, r)
	evs := eventlogtest.Capture(t)
	r.closeAfterMR(t)
	if err := reconcileCheckpoint(openCheckpoint()); err != nil {
		t.Fatal(err)
	}
	if e := eventNamed(*evs, "land.mr_closed"); !slices.Equal(r.mrsClosed, []int{9}) || len(r.closed) != 2 || e.Attrs["reason"] != "container landed" {
		t.Errorf("MRs %v beads %v event %v, want MR 9, the Landing and the container, and an event", r.mrsClosed, r.closed, e)
	}
}

// No open MR: no close call. A Forge error leaves everything open; the next roll-forward retries.
func TestGitlabRollForwardNoMRAndErrors(t *testing.T) {
	r := &jinbeSeamRec{trailerLanded: true, landings: []bd.Bead{openLanding("cafef00d")}}
	stubJinbeSeams(t, r)
	if err := reconcileCheckpoint(openCheckpoint()); err != nil || len(r.mrsClosed) != 0 || len(r.closed) != 2 {
		t.Fatalf("err %v MRs %v beads %v, want no MR call and both closed", err, r.mrsClosed, r.closed)
	}
	// A Forge 429 is held once, not returned each tick (ADR 0185 §8); the next tick retries.
	rl := &jinbeSeamRec{trailerLanded: true, findErr: forge.ErrRateLimited, landings: []bd.Bead{openLanding("cafef00d")}}
	stubJinbeSeams(t, rl)
	evs := eventlogtest.Capture(t)
	rateLimited.Clear()
	for range 2 {
		if err := reconcileCheckpoint(openCheckpoint()); err != nil || len(rl.closed) != 0 {
			t.Fatalf("err %v closed %v, want nil and everything open", err, rl.closed)
		}
	}
	n := 0
	for _, name := range eventlogtest.Names(*evs) {
		if name == "land.rate_limited" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d land.rate_limited events over two ticks, want 1", n)
	}
	rl.findErr, rl.openMR = nil, &forge.MR{IID: 9}
	if err := reconcileCheckpoint(openCheckpoint()); err != nil || !slices.Equal(rl.mrsClosed, []int{9}) || len(rl.closed) != 2 {
		t.Fatalf("retry: err %v MRs %v beads %v", err, rl.mrsClosed, rl.closed)
	}
	c := &jinbeSeamRec{trailerLanded: true, openMR: &forge.MR{IID: 9}, closeMRErr: errors.New("boom"), landings: []bd.Bead{openLanding("cafef00d")}}
	stubJinbeSeams(t, c)
	if err := reconcileCheckpoint(openCheckpoint()); err == nil || len(c.closed) != 0 {
		t.Fatalf("err %v closed %v, want the error and everything open", err, c.closed)
	}
	c.closeMRErr = nil
	if err := reconcileCheckpoint(openCheckpoint()); err != nil || !slices.Equal(c.mrsClosed, []int{9}) || len(c.closed) != 2 {
		t.Fatalf("retry: err %v MRs %v beads %v", err, c.mrsClosed, c.closed)
	}
}

// A nested container's roll-forward closes on the child's project.
func TestGitlabRollForwardUsesTheChildsProject(t *testing.T) {
	r := &jinbeSeamRec{trailerLanded: true, openMR: &forge.MR{IID: 9}}
	stubJinbeSeams(t, r)
	t.Chdir(t.TempDir()) // legsFor reads git.TopLevelOf("."): root it in a scratch repo, not the checkout
	if out, err := exec.Command("git", "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	oRepo := repoDirOf
	t.Cleanup(func() { repoDirOf = oRepo })
	repoDirOf = func(bd.Bead) (git.Repo, string) { return "/child", "child" }
	r.landings = []bd.Bead{openLanding("cafef00d")}
	if err := closeLanded(openCheckpoint(), "checkpoint/ws-cp", "landsha"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.mrsClosed, []int{9}) || slices.ContainsFunc(r.projects, func(p forge.Project) bool { return p != childProject }) {
		t.Errorf("MRs %v projects %v, want 9 on the child's only", r.mrsClosed, r.projects)
	}
}

// Under a non-gitlab strategy the roll-forward makes no Forge call.
func TestLocalRollForwardMakesNoForgeCall(t *testing.T) {
	r := &jinbeSeamRec{trailerLanded: true, gated: true, openMR: &forge.MR{IID: 9}, landings: []bd.Bead{openLanding("cafef00d")}}
	stubJinbeSeams(t, r)
	if err := reconcileCheckpoint(openCheckpoint()); err != nil || len(r.projects) != 0 || len(r.mrsClosed) != 0 {
		t.Errorf("err %v projects %v MRs %v, want no Forge call", err, r.projects, r.mrsClosed)
	}
}
