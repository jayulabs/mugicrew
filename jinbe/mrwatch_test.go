package jinbe

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
)

func openWatch() bd.Bead {
	return bd.Bead{ID: "ws-watch", Kind: contract.KindMRWatch, Status: "in_progress", Parent: "ws-cp", Description: "MR: !7\n"}
}

// watchTick is one MR Watch tick over an MR in state with count user notes.
func watchTick(t *testing.T, r *jinbeSeamRec, state string, count int, kids ...bd.Bead) error {
	t.Helper()
	r.mr = &forge.MR{IID: 7, State: state, Title: "Ship it", UserNotesCount: count}
	r.threadKids = kids
	stubJinbeSeams(t, r)
	return reconcileMRWatch(openWatch())
}

func TestPendingLandingSpawnsOneMRWatchCopyingItsMR(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7, WebURL: "https://gl.example.com/ns/p/-/merge_requests/7"}}
	stubJinbeSeams(t, r)
	l := openLanding("cafef00d")
	for range 2 {
		if err := reconcileLanding(l); err != nil {
			t.Fatal(err)
		}
	}
	var watches []bd.Successor
	for _, s := range r.spawned {
		if s.Kind == contract.KindMRWatch {
			watches = append(watches, s)
		}
	}
	if len(watches) != 1 || strings.TrimSpace(watches[0].Description) != "MR: !7\nMR link: https://gl.example.com/ns/p/-/merge_requests/7" {
		t.Errorf("spawned %+v, want one mr-watch carrying MR: !7 and its MR link:", watches)
	}
}

func TestMRWatchSpawnsAForgeReviewForANoteBetweenLandings(t *testing.T) {
	r := &jinbeSeamRec{discussions: []forge.Discussion{plain("p1", note(20, 7, "why X?"))}}
	if err := watchTick(t, r, "opened", 1); err != nil {
		t.Fatal(err)
	}
	if len(r.reviewSpawned) != 1 || r.reviewSpawned[0].DiscoveredFrom != "" {
		t.Errorf("spawned %+v, want one Forge Review discovered from no Landing", r.reviewSpawned)
	}
	if len(r.closed) != 0 {
		t.Errorf("closed %v, want the Watch left open", r.closed)
	}
}

func TestMRWatchSkipsWhileALandingIsPending(t *testing.T) {
	r := &jinbeSeamRec{landings: []bd.Bead{openLanding("cafef00d")}, discussions: []forge.Discussion{plain("p1", note(20, 7, "why X?"))}}
	if err := watchTick(t, r, "opened", 1); err != nil {
		t.Fatal(err)
	}
	if r.mrReads != 0 || len(r.reviewSpawned) != 0 {
		t.Errorf("reads %d spawned %v, want none: mirrorMR reads the MR then", r.mrReads, r.reviewSpawned)
	}
}

func TestMRWatchHoldsAForgeRateLimit(t *testing.T) {
	r := &jinbeSeamRec{mrErr: forge.ErrRateLimited}
	stubJinbeSeams(t, r)
	if err := reconcileMRWatch(openWatch()); err != nil {
		t.Errorf("err = %v, want the 429 held", err)
	}
	r2 := &jinbeSeamRec{mrErr: errors.New("boom")}
	stubJinbeSeams(t, r2)
	if err := reconcileMRWatch(openWatch()); err == nil {
		t.Error("other Forge errors must be returned")
	}
}

func TestLandedContainerClosesItsMRWatch(t *testing.T) {
	r := &jinbeSeamRec{threadKids: []bd.Bead{openWatch()}}
	stubJinbeSeams(t, r)
	if err := closeMRWatches("ws-cp", "container closed"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.closed, []string{"ws-watch"}) {
		t.Errorf("closed %v, want the Watch", r.closed)
	}
}

func TestMRWatchClosesOutAFiledBeadBetweenLandings(t *testing.T) {
	r := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc1, note(1, 5, "why X?"))}}
	c := &coRec{}
	oAppend := appendNotes
	t.Cleanup(func() { appendNotes = oAppend })
	appendNotes = func(id, line string) error { c.ledger = append(c.ledger, id+" "+line); return nil }
	if err := watchTick(t, r, "opened", 1, filed("ws-a", "closed", "not needed: by design", disc1+" 1")); err != nil {
		t.Fatal(err)
	}
	want := []string{"reply " + disc1 + " 🤖 mugicrew — ☑️ Closed · **Fix ws-a** (ws-a@jinbe)\n\n**Rationale:** not needed: by design\n" + marker("ws-a", "g00d", "jinbe"), "resolve " + disc1}
	if got := slices.DeleteFunc(slices.Clone(r.sends), func(s string) bool { return !strings.HasPrefix(s, "reply ") && !strings.HasPrefix(s, "resolve ") }); !slices.Equal(got, want) {
		t.Errorf("sends %q, want %q", got, want)
	}
}

const mrLinkURL = "https://gl.example.com/ns/p/-/merge_requests/7"

// recordWrites replaces updateBody (after stubJinbeSeams) with a recorder of the bodies
// carrying an MR link: by bead id; a non-nil err fails those writes only.
func recordWrites(t *testing.T, err error) map[string][]string {
	t.Helper()
	writes := map[string][]string{}
	old := updateBody
	t.Cleanup(func() { updateBody = old })
	updateBody = func(id string, args []string) error {
		if git.RecordedMRLink(args[1]) == "" {
			return nil
		}
		if err != nil {
			return err
		}
		writes[id] = append(writes[id], args[1])
		return nil
	}
	return writes
}

// A pending Landing's tick backfills MR link: on itself and on the open Watch, and a second
// tick (bodies now current) makes no write.
func TestPendingLandingBackfillsMRLinkOnItselfAndItsWatch(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7}, mr: &forge.MR{IID: 7, State: "opened", WebURL: mrLinkURL}, threadKids: []bd.Bead{openWatch()}}
	stubJinbeSeams(t, r)
	writes := recordWrites(t, nil)
	l := openLanding("cafef00d")
	l.Description += "\nMR: !7\n"
	if err := reconcileLanding(l); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ws-land", "ws-watch"} {
		if len(writes[id]) == 0 || git.RecordedMRLink(writes[id][0]) != mrLinkURL {
			t.Errorf("writes to %s = %q, want the first carrying the MR link", id, writes[id])
		}
	}
	// the same bodies, now carrying the line: no further write
	l.Description = writes["ws-land"][len(writes["ws-land"])-1]
	w := openWatch()
	w.Description = writes["ws-watch"][0]
	r.threadKids = []bd.Bead{w}
	before := len(writes["ws-land"]) + len(writes["ws-watch"])
	if err := reconcileLanding(l); err != nil {
		t.Fatal(err)
	}
	if got := len(writes["ws-land"]) + len(writes["ws-watch"]); got != before {
		t.Errorf("second tick wrote %d more times, want none", got-before)
	}
}

// A Watch with no pending Landing backfills its own MR link: on an open MR.
func TestMRWatchBackfillsMRLinkWithoutAPendingLanding(t *testing.T) {
	r := &jinbeSeamRec{}
	r.mr = &forge.MR{IID: 7, State: "opened", WebURL: mrLinkURL}
	stubJinbeSeams(t, r)
	writes := recordWrites(t, nil)
	if err := reconcileMRWatch(openWatch()); err != nil {
		t.Fatal(err)
	}
	if len(writes["ws-watch"]) != 1 || git.RecordedMRLink(writes["ws-watch"][0]) != mrLinkURL {
		t.Errorf("writes = %q, want one carrying the MR link", writes)
	}
}

// A failed backfill write warns and neither fails the Watch's nor the Landing's reconcile.
func TestMRLinkBackfillWriteFailureDoesNotFailTheReconcile(t *testing.T) {
	r := &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7}, mr: &forge.MR{IID: 7, State: "opened", WebURL: mrLinkURL}, threadKids: []bd.Bead{openWatch()}}
	stubJinbeSeams(t, r)
	recordWrites(t, errors.New("bd down"))
	l := openLanding("cafef00d")
	l.Description += "\nMR: !7\n"
	if err := reconcileLanding(l); err != nil {
		t.Errorf("Landing reconcile = %v, want nil", err)
	}
	if err := reconcileMRWatch(openWatch()); err != nil {
		t.Errorf("Watch reconcile = %v, want nil", err)
	}
}

// lateWatch runs one Watch tick over a container in status (a closed one with landings),
// an MR in state, and the filed kids; closed-out notes land on the kids as they are written.
func lateWatch(t *testing.T, r *jinbeSeamRec, status, state string, kids ...bd.Bead) error {
	t.Helper()
	cp := openCheckpoint()
	cp.Status = status
	return lateWatchOf(t, r, cp, state, kids...)
}

// lateWatchSeed is appended to the Watch's body before a late tick (lines it already holds).
var lateWatchSeed string

// bodies is the description each bead holds, rewritten as lateWatchOf's updateBody stand-in is called.
type bodies map[string]string

// lateWatchOf is lateWatch over the container cp itself.
func lateWatchOf(t *testing.T, r *jinbeSeamRec, cp bd.Bead, state string, kids ...bd.Bead) error {
	t.Helper()
	_, err := lateWatchBodies(t, r, cp, state, kids...)
	return err
}

// lateWatchBodies is lateWatchOf that also returns the container's and the Watch's description as the tick leaves them.
func lateWatchBodies(t *testing.T, r *jinbeSeamRec, cp bd.Bead, state string, kids ...bd.Bead) (bodies, error) {
	t.Helper()
	r.mr = &forge.MR{IID: 7, State: state, SHA: "pushed", UserNotesCount: 1, WebURL: mrLinkURL}
	r.threadKids = kids
	stubJinbeSeams(t, r)
	oHas, oFetch, oReach, oAppend, oShow, oUpdate := hasCommit, fetchCommit, isReachable, appendNotes, showBead, updateBody
	t.Cleanup(func() {
		hasCommit, fetchCommit, isReachable, appendNotes, showBead, updateBody = oHas, oFetch, oReach, oAppend, oShow, oUpdate
	})
	hasCommit = func(git.Repo, string) bool { return true }
	fetchCommit = func(git.Repo, string) error { return nil }
	isReachable = func(git.Repo, string, string) bool { return r.reachable }
	appendNotes = func(id, line string) error {
		for i := range r.threadKids {
			if r.threadKids[i].ID == id {
				r.threadKids[i].Notes += line + "\n"
			}
		}
		return nil
	}
	w := openWatch()
	w.Description += lateWatchSeed
	held := bodies{cp.ID: cp.Description, w.ID: w.Description}
	showBead = func(id string) (bd.Bead, error) {
		if id == w.ID {
			w.Description = held[id]
			return w, nil
		}
		cp.Description = held[id]
		return cp, nil
	}
	updateBody = func(id string, args []string) error {
		held[id] = args[1]
		return nil
	}
	return held, reconcileMRWatch(w)
}

func landedLanding(sha string) bd.Bead {
	l := openLanding("cafef00d")
	l.Status, l.CloseReason, l.Closed = "closed", "landed: "+sha, testNow
	return l
}

func TestMRWatchStaysOpenWhileAFiledBeadIsOwedThenClosesWithItsReason(t *testing.T) {
	for _, c := range []struct{ status, state, reason string }{
		{"closed", "opened", "container closed"},
		{"open", "merged", "obsolete: merge request merged"},
		{"open", "closed", "obsolete: merge request closed"},
	} {
		open := filed("ws-a", "open", "", disc1+" 1")
		r := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc1, note(1, 5, "x"))}}
		if err := lateWatch(t, r, c.status, c.state, open); err != nil {
			t.Fatal(err)
		}
		if len(r.closed) != 0 || len(r.reviewSpawned) != 0 {
			t.Errorf("%+v: closed %v spawned %v, want the Watch open and no Forge Review", c, r.closed, r.reviewSpawned)
		}
		done := filed("ws-a", "closed", "not needed: by design", disc1+" 1")
		r = &jinbeSeamRec{discussions: []forge.Discussion{thread(disc1, note(1, 5, "x"))}}
		if err := lateWatch(t, r, c.status, c.state, done); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(r.closed, []string{"ws-watch"}) || r.reasons[0] != c.reason {
			t.Errorf("%+v: closed %v %v, want the Watch closed %q once closed out", c, r.closed, r.reasons, c.reason)
		}
		if len(replies(r)) != 1 || len(r.reviewSpawned) != 0 {
			t.Errorf("%+v: replies %q spawned %v, want one reply and no Forge Review", c, replies(r), r.reviewSpawned)
		}
	}
}

func TestMRWatchLandedLatePassReadsTrailersFromTheTip(t *testing.T) {
	r := &jinbeSeamRec{landedTip: "tip5678", reachable: true, integrateSHA: "abcdef123456", landings: []bd.Bead{landedLanding("merge1234")}, discussions: []forge.Discussion{thread(disc1, note(1, 5, "x"))}}
	if err := lateWatch(t, r, "closed", "merged", filed("ws-a", "closed", "renamed", disc1+" 1")); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.trailerRefs, []string{"tip5678"}) {
		t.Errorf("trailer read from %q, want the Tip, the container branch being gone", r.trailerRefs)
	}
	if got := replies(r); len(got) != 1 || !strings.Contains(got[0], "fixed in abcdef12") {
		t.Errorf("replies %q, want one naming the fix", got)
	}
	if !slices.Equal(r.closed, []string{"ws-watch"}) {
		t.Errorf("closed %v, want the Watch", r.closed)
	}
}

func TestMRWatchSweptLatePassMakesAnUnpushedCommitDueAtOnce(t *testing.T) {
	r := &jinbeSeamRec{integrateSHA: "abcdef123456", discussions: []forge.Discussion{thread(disc1, note(1, 5, "x"))}}
	if err := lateWatch(t, r, "closed", "opened", filed("ws-a", "closed", "renamed", disc1+" 1")); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.trailerRefs, []string{"checkpoint/ws-cp"}) {
		t.Errorf("trailer read from %q, want the kept container branch", r.trailerRefs)
	}
	if got := replies(r); len(got) != 1 {
		t.Errorf("replies %q, want one reply though the commit was never pushed", got)
	}
}

func TestMRWatchSweptLatePassWithTheBranchGoneClosesOutWithNoFix(t *testing.T) {
	r := &jinbeSeamRec{branchGone: true, integrateSHA: "abcdef123456", discussions: []forge.Discussion{thread(disc1, note(1, 5, "x"))}}
	if err := lateWatch(t, r, "closed", "opened", filed("ws-a", "closed", "renamed", disc1+" 1")); err != nil {
		t.Fatal(err)
	}
	if len(r.trailerRefs) != 0 {
		t.Errorf("trailer read from %q, want no read of a deleted branch", r.trailerRefs)
	}
	if got := replies(r); len(got) != 1 || strings.Contains(got[0], "abcdef12") {
		t.Errorf("replies %q, want one reply showing no sha", got)
	}
}

// brokenReply is a late pass's one filed child whose reply the Forge refuses.
func brokenReply() *jinbeSeamRec {
	return &jinbeSeamRec{replyErr: errors.New("403 locked"), discussions: []forge.Discussion{thread(disc1, note(1, 5, "x"))}}
}

func closedContainer(closedAgo time.Duration) bd.Bead {
	cp := openCheckpoint()
	cp.Status, cp.Closed = "closed", testNow.Add(-closedAgo)
	cp.Description = contract.CloseOutsDuePrefix + "1\n" + contract.CloseOutsFailingPrefix + "1: old\nSpec: ws-spec\n"
	return cp
}

func TestMRWatchLatePassWritesTheCloseOutLinesOnTheWatchAndClearsTheContainers(t *testing.T) {
	held, err := lateWatchBodies(t, brokenReply(), closedContainer(time.Hour), "opened", filed("ws-a", "closed", "done", disc1+" 1"))
	if err != nil {
		t.Fatalf("a late failure is recorded and retried, not returned: %v", err)
	}
	if !strings.Contains(held["ws-watch"], contract.CloseOutsFailingPrefix+"1: 403 locked") {
		t.Errorf("Watch body %q, want the failing line", held["ws-watch"])
	}
	if held["ws-cp"] != "Spec: ws-spec\n" {
		t.Errorf("container body %q, want its close-out lines removed", held["ws-cp"])
	}
}

func TestMRWatchLateFailureUnderALandedContainerIsRecordedNotReturned(t *testing.T) {
	r := brokenReply()
	r.landings = []bd.Bead{landedLanding("merge1234")}
	held, err := lateWatchBodies(t, r, closedContainer(time.Hour), "merged", filed("ws-a", "closed", "done", disc1+" 1"))
	if err != nil || !strings.Contains(held["ws-watch"], contract.CloseOutsFailingPrefix+"1:") || len(r.closed) != 0 {
		t.Errorf("err %v body %q closed %v, want the failure on the Watch, nothing returned, the Watch open", err, held["ws-watch"], r.closed)
	}
}

func TestMRWatchLateRateLimitIsHeldNotReturned(t *testing.T) {
	r := brokenReply()
	r.replyErr = forge.ErrRateLimited
	if err := lateWatchOf(t, r, closedContainer(time.Hour), "opened", filed("ws-a", "closed", "done", disc1+" 1")); err != nil {
		t.Errorf("err = %v, want the 429 held", err)
	}
}

func TestMRWatchOpenContainerStillReturnsAMergedFailure(t *testing.T) {
	r := brokenReply()
	r.landings = []bd.Bead{landedLanding("merge1234")}
	cp := openCheckpoint()
	if err := lateWatchOf(t, r, cp, "merged", filed("ws-a", "closed", "done", disc1+" 1")); !errors.Is(err, r.replyErr) {
		t.Errorf("err = %v, want the failure returned to hold the open container", err)
	}
}

func TestMRWatchAbandonsAfterSevenDaysFilingOneSignal(t *testing.T) {
	r := brokenReply()
	cp := closedContainer(7 * 24 * time.Hour)
	kid := filed("ws-a", "closed", "done", disc1+" 1")
	held, err := lateWatchBodies(t, r, cp, "opened", kid)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.closed, []string{"ws-watch"}) || r.reasons[0] != "obsolete: close-out abandoned; carried by ws-aband0" {
		t.Errorf("closed %v %v, want the Watch closed carried by the Signal", r.closed, r.reasons)
	}
	if len(r.abandons) != 1 {
		t.Fatalf("abandons %q, want one", r.abandons)
	}
	head, body, _ := strings.Cut(r.abandons[0], "\n")
	if head != "ws-watch Ship the thing: close-outs abandoned on !7" {
		t.Errorf("origin and title %q", head)
	}
	for _, want := range []string{"swept", "!7", mrLinkURL, "ws-a", disc1, "403 locked", "Fix:"} {
		if !strings.Contains(body, want) {
			t.Errorf("body %q lacks %q", body, want)
		}
	}
	if !strings.Contains(held["ws-watch"], contract.CloseOutsFailingPrefix+"1:") {
		t.Errorf("Watch body %q, want its lines kept beside the close reason", held["ws-watch"])
	}
	// a retry (the close failed after filing) dedups by title
	r.closed, r.reasons = nil, nil
	if _, err := lateWatchBodies(t, r, cp, "opened", kid); err != nil {
		t.Fatal(err)
	}
	if len(r.abandons) != 1 || r.reasons[0] != "obsolete: close-out abandoned; carried by ws-aband0" {
		t.Errorf("abandons %q reasons %v, want still one Signal and the same carrier", r.abandons, r.reasons)
	}
}

func TestMRWatchDoesNotAbandonBeforeSevenDays(t *testing.T) {
	r := brokenReply()
	if err := lateWatchOf(t, r, closedContainer(7*24*time.Hour-time.Minute), "opened", filed("ws-a", "closed", "done", disc1+" 1")); err != nil {
		t.Fatal(err)
	}
	if len(r.abandons) != 0 || len(r.closed) != 0 {
		t.Errorf("abandons %q closed %v, want the Watch left polling", r.abandons, r.closed)
	}
}

func TestMRWatchStaysOpenWhenFilingTheAbandonSignalFails(t *testing.T) {
	r := brokenReply()
	r.abandonErr = errors.New("bd down")
	err := lateWatchOf(t, r, closedContainer(8*24*time.Hour), "opened", filed("ws-a", "closed", "done", disc1+" 1"))
	if err == nil || len(r.closed) != 0 {
		t.Errorf("err %v closed %v, want the failure returned before the close", err, r.closed)
	}
}

func TestMRWatchNothingOwedCloseClearsItsLines(t *testing.T) {
	r := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc1, note(1, 5, "x"))}}
	cp := closedContainer(time.Hour)
	lateWatchSeed = contract.CloseOutsFailingPrefix + "1: 403 locked\n"
	t.Cleanup(func() { lateWatchSeed = "" })
	held, err := lateWatchBodies(t, r, cp, "opened", filed("ws-a", "closed", "done", disc1+" 1"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.closed, []string{"ws-watch"}) || strings.Contains(held["ws-watch"], "Close-outs") {
		t.Errorf("closed %v body %q, want the Watch closed with no lines", r.closed, held["ws-watch"])
	}
}
