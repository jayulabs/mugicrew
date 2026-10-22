package jinbe

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
)

const disc2 = "d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2"

// coRec records the close-out pass's side effects beyond jinbeSeamRec's sends.
type coRec struct {
	ledger   []string // "<bead> <line>" appended to notes
	bodies   []string // container body writes
	fetched  []string
	have     bool            // the clone holds the target head
	reached  map[string]bool // integrate sha → reachable from the head
	signals  []bd.Bead       // the stranded-commits Signals found
	carriers []bd.Bead       // the Specs and Charts a carrier read finds beside the Signals
	shas     map[string]string
	subjects map[string]string  // integrate sha8 → commit subject
	pivots   map[string]bd.Bead // pivot id → the bead a by-id read finds
	readErr  error              // the carrier reads' failure
	reads    int                // carrier reads made
}

func filed(id, status, reason string, sources ...string) bd.Bead {
	var d strings.Builder
	for _, s := range sources {
		d.WriteString("Source: !7 " + s + "\n> quoted\n")
	}
	return bd.Bead{ID: id, Kind: contract.KindCritique, Status: status, Title: "Fix " + id, CloseReason: reason, Description: d.String()}
}

func closeOutRun(t *testing.T, r *jinbeSeamRec, c *coRec, target closeOutTarget, kids ...bd.Bead) error {
	t.Helper()
	r.threadKids = kids
	stubJinbeSeams(t, r)
	oPivot := pivotByID
	oCar, oHas, oFetch, oReach, oSig, oAppend, oUpdate, oShow, oNew, oSubj := carriersFrom, hasCommit, fetchCommit, isReachable, signalsFrom, appendNotes, updateBody, showBead, newestByTrailer, commitSubj
	t.Cleanup(func() {
		pivotByID = oPivot
		carriersFrom = oCar
		hasCommit, fetchCommit, isReachable, signalsFrom, appendNotes, updateBody, showBead, newestByTrailer, commitSubj = oHas, oFetch, oReach, oSig, oAppend, oUpdate, oShow, oNew, oSubj
	})
	commitSubj = func(_ git.Repo, sha string) (string, error) { return c.subjects[sha], nil }
	hasCommit = func(git.Repo, string) bool { return c.have }
	fetchCommit = func(_ git.Repo, sha string) error { c.fetched = append(c.fetched, sha); return nil }
	isReachable = func(_ git.Repo, sha, _ string) bool { return c.reached[sha] }
	signalsFrom = func(string) ([]bd.Bead, error) { return c.signals, nil }
	carriersFrom = func(string) ([]bd.Bead, error) { return c.carriers, nil }
	if c.readErr != nil || c.pivots != nil || c.signals != nil {
		signalsFrom = func(string) ([]bd.Bead, error) { c.reads++; return c.signals, c.readErr }
		carriersFrom = func(string) ([]bd.Bead, error) {
			c.reads++
			return append(slices.Clone(c.signals), c.carriers...), c.readErr
		}
	}
	pivotByID = func(id string) (bd.Bead, error) {
		c.reads++
		if c.readErr != nil {
			return bd.Bead{}, c.readErr
		}
		if b, ok := c.pivots[id]; ok {
			return b, nil
		}
		return bd.Bead{}, errors.New("bd show: bead " + id + " not found")
	}
	appendNotes = func(id, line string) error { c.ledger = append(c.ledger, id+" "+line); return nil }
	updateBody = func(_ string, args []string) error { c.bodies = append(c.bodies, args[1]); return nil }
	showBead = func(string) (bd.Bead, error) { return bd.Bead{ID: "ws-cp", Description: "Spec: x\n"}, nil }
	newestByTrailer = func(_ git.Repo, _, trailer string) (string, error) { return c.shas[trailer], nil }
	cp := bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Title: "Ship", Status: "open", Description: "Spec: x\n"}
	return closeOutArm(cp, legs{}, &forge.MR{IID: 7, SHA: "pushed"}, "checkpoint/ws-cp", target)
}

func replies(r *jinbeSeamRec) []string {
	return slices.DeleteFunc(slices.Clone(r.sends), func(s string) bool { return !strings.HasPrefix(s, "reply ") })
}

func TestCloseOutNoFixBeadClosesOutAtClose(t *testing.T) {
	r := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc1, note(1, 5, "why X?"))}}
	c := &coRec{}
	if err := closeOutRun(t, r, c, closeOutTarget{head: "pushed"}, filed("ws-a", "closed", "not needed: by design", disc1+" 1")); err != nil {
		t.Fatal(err)
	}
	want := []string{"reply " + disc1 + " 🤖 mugicrew — ☑️ Closed · **Fix ws-a** (ws-a@jinbe)\n\n**Rationale:** not needed: by design\n" + marker("ws-a", "g00d", "jinbe"), "resolve " + disc1}
	if !slices.Equal(r.sends, want) {
		t.Errorf("sends %q, want %q", r.sends, want)
	}
	if !slices.Equal(c.ledger, []string{"ws-a Closed out: " + disc1}) {
		t.Errorf("ledger %q", c.ledger)
	}
}

func TestCloseOutWaitsForAnUnpushedIntegrateCommitThenFires(t *testing.T) {
	kid := filed("ws-a", "closed", "renamed", disc1+" 1")
	c := &coRec{have: true, shas: map[string]string{"Critique: ws-a": "abcdef123456"}, reached: map[string]bool{}}
	r := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc1, note(1, 5, "rename"))}}
	if err := closeOutRun(t, r, c, closeOutTarget{head: "pushed"}, kid); err != nil {
		t.Fatal(err)
	}
	if len(r.sends) != 0 || r.discussionsRead {
		t.Fatalf("an unpushed commit must wait without a Forge write or read; sends %q", r.sends)
	}
	if len(c.bodies) != 1 || !strings.Contains(c.bodies[0], "Close-outs due: 1") {
		t.Errorf("container body %q, want a Close-outs due: 1 line", c.bodies)
	}
	c.reached["abcdef123456"] = true
	if err := closeOutRun(t, r, c, closeOutTarget{head: "pushed"}, kid); err != nil {
		t.Fatal(err)
	}
	if got := replies(r); len(got) != 1 || !strings.Contains(got[0], "✅ Accepted · fixed in abcdef12 by **Fix ws-a** (ws-a@jinbe)\n\n**Rationale:** renamed\n") {
		t.Errorf("replies %q, want one naming abcdef12", got)
	}
}

func TestCloseOutFetchesAHeadTheCloneLacks(t *testing.T) {
	c := &coRec{shas: map[string]string{"Critique: ws-a": "abcdef123456"}, reached: map[string]bool{"abcdef123456": true}}
	r := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc1, note(1, 5, "x"))}}
	if err := closeOutRun(t, r, c, closeOutTarget{head: "pushed"}, filed("ws-a", "closed", "done", disc1+" 1")); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.fetched, []string{"pushed"}) {
		t.Errorf("fetched %q, want the pushed head", c.fetched)
	}
}

func TestCloseOutStrandedBeadIsCarriedBySignal(t *testing.T) {
	sig := bd.Bead{ID: "ws-sig", Kind: contract.KindSignal, Title: "Ship: commits after the landed Tip: are not on mainline"}
	c := &coRec{have: true, shas: map[string]string{"Critique: ws-a": "abcdef123456"}, reached: map[string]bool{}, signals: []bd.Bead{sig}}
	r := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc1, note(1, 5, "x"))}}
	merged := closeOutTarget{head: "merged", merged: true, landing: bd.Bead{ID: "ws-land"}}
	if err := closeOutRun(t, r, c, merged, filed("ws-a", "closed", "fixed", disc1+" 1")); err != nil {
		t.Fatal(err)
	}
	want := []string{"reply " + disc1 + " 🤖 mugicrew: not shipped with this MR: carried by **" + sig.Title + "**\n" + marker("ws-a", "g00d", "jinbe"), "resolve " + disc1}
	if !slices.Equal(r.sends, want) {
		t.Errorf("sends %q, want %q", r.sends, want)
	}
	if len(c.bodies) != 0 {
		t.Errorf("a merged pass writes no container line: %q", c.bodies)
	}
}

func TestCloseOutContainerLandedBeadGetsTheStrandedReply(t *testing.T) {
	sig := bd.Bead{ID: "ws-sig", Kind: contract.KindSignal, Title: "Ship: commits after the landed Tip: are not on mainline"}
	offshoot := bd.Bead{ID: "ws-off", Kind: contract.KindSignal, Title: "Mint a token"}
	reason := bd.ContainerLandedReason + "; carried by " + sig.ID
	late := closeOutTarget{head: "merged", merged: true, landing: bd.Bead{ID: "ws-land"}}
	for name, target := range map[string]closeOutTarget{"merged": late, "late": {head: "pushed"}} {
		c := &coRec{signals: []bd.Bead{offshoot, sig}}
		r := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc1, note(1, 5, "x"))}}
		if err := closeOutRun(t, r, c, target, filed("ws-a", "closed", reason, disc1+" 1")); err != nil {
			t.Fatal(name, err)
		}
		want := []string{"reply " + disc1 + " 🤖 mugicrew: not shipped with this MR: carried by **" + sig.Title + "**\n" + marker("ws-a", "g00d", "jinbe"), "resolve " + disc1}
		if !slices.Equal(r.sends, want) {
			t.Errorf("%s: sends %q, want %q", name, r.sends, want)
		}
	}
}

func TestCloseOutContainerLandedBeadWithoutASignalFallsBackToItsReason(t *testing.T) {
	c := &coRec{}
	r := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc1, note(1, 5, "x"))}}
	late := closeOutTarget{head: "merged", merged: true, landing: bd.Bead{ID: "ws-land"}}
	if err := closeOutRun(t, r, c, late, filed("ws-a", "closed", bd.ContainerLandedReason+"; carried by x", disc1+" 1")); err != nil {
		t.Fatal(err)
	}
	got := replies(r)
	if len(got) != 1 || strings.Contains(got[0], "not shipped with this MR") {
		t.Errorf("replies %q, want one reply from the reason's mapping", got)
	}
}

func TestCloseOutContainerLandedSignalReadFailurePostsNothing(t *testing.T) {
	boom := errors.New("bd down")
	c := &coRec{readErr: boom}
	r := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc1, note(1, 5, "x"))}}
	late := closeOutTarget{head: "merged", merged: true, landing: bd.Bead{ID: "ws-land"}}
	err := closeOutRun(t, r, c, late, filed("ws-a", "closed", bd.ContainerLandedReason+"; carried by x", disc1+" 1"))
	if !errors.Is(err, boom) || len(replies(r)) != 0 {
		t.Errorf("err %v replies %q, want the failure and no reply", err, replies(r))
	}
}

func TestCloseOutOneReplyPerBeadAndResolveOnlyWhenAllAreOut(t *testing.T) {
	a := filed("ws-a", "closed", "done a", disc1+" 1", disc1+" 2")
	b := filed("ws-b", "open", "", disc1+" 2")
	r := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc1, note(1, 5, "a"), note(2, 5, "b"))}}
	c := &coRec{}
	if err := closeOutRun(t, r, c, closeOutTarget{head: "pushed"}, a, b); err != nil {
		t.Fatal(err)
	}
	if got := replies(r); len(got) != 1 {
		t.Fatalf("replies %q, want one for the one closed bead, though it has two notes here", got)
	}
	if slices.Contains(r.sends, "resolve "+disc1) || len(c.ledger) != 0 {
		t.Errorf("an open sibling bead must keep the thread unresolved and unrecorded: sends %q ledger %q", r.sends, c.ledger)
	}
	// the sibling closes: the first reply is already on the thread, so only the second posts
	r2 := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc1, note(1, 5, "a"), note(2, 5, "b"), note(3, 5, renderReply(a, "g00d", "jinbe", verdictClosed, "", "", nil)))}}
	b.Status = "closed"
	if err := closeOutRun(t, r2, c, closeOutTarget{head: "pushed"}, a, b); err != nil {
		t.Fatal(err)
	}
	if got := replies(r2); len(got) != 1 || !strings.Contains(got[0], "ws-b@jinbe") {
		t.Errorf("replies %q, want one for ws-b only", got)
	}
	if !slices.Contains(r2.sends, "resolve "+disc1) {
		t.Errorf("sends %q, want the resolve once both are out", r2.sends)
	}
}

func TestCloseOutIsIdempotentAcrossTicks(t *testing.T) {
	a := filed("ws-a", "closed", "done", disc1+" 1")
	out := thread(disc1, note(1, 5, "x"), note(2, 5, renderReply(a, "g00d", "jinbe", verdictClosed, "", "", nil)))
	out.Resolved = true
	r := &jinbeSeamRec{discussions: []forge.Discussion{out}}
	if err := closeOutRun(t, r, &coRec{}, closeOutTarget{head: "pushed"}, a); err != nil {
		t.Fatal(err)
	}
	if len(r.sends) != 0 {
		t.Errorf("sends %q, want none once the marker stands and the thread is resolved", r.sends)
	}
}

func TestCloseOutLedgeredDiscussionIsNotReadAgain(t *testing.T) {
	a := filed("ws-a", "closed", "done", disc1+" 1")
	a.Notes = "Closed out: " + disc1
	r := &jinbeSeamRec{}
	if err := closeOutRun(t, r, &coRec{}, closeOutTarget{head: "pushed"}, a); err != nil {
		t.Fatal(err)
	}
	if r.discussionsRead {
		t.Error("a settled bead must cost no discussions read")
	}
}

func TestCloseOutFoldedNoteInANewDiscussionGetsItsReply(t *testing.T) {
	a := filed("ws-a", "closed", "done", disc1+" 1")
	a.Notes = "Closed out: " + disc1 + "\nSource: !7 " + disc2 + " 9\n> bot repeat"
	r := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc2, note(9, 5, "again"))}}
	if err := closeOutRun(t, r, &coRec{}, closeOutTarget{head: "pushed"}, a); err != nil {
		t.Fatal(err)
	}
	if got := replies(r); len(got) != 1 || !strings.HasPrefix(got[0], "reply "+disc2) {
		t.Errorf("replies %q, want one on the folded note's discussion", got)
	}
}

func TestCloseOutAHumanReplyAfterTheMarkerIsLeftForTheNextReview(t *testing.T) {
	a := filed("ws-a", "closed", "done", disc1+" 1")
	d := thread(disc1, note(1, 5, "x"), note(2, 5, renderReply(a, "g00d", "jinbe", verdictClosed, "", "", nil)), note(3, 5, "that didn't fix it"))
	r := &jinbeSeamRec{discussions: []forge.Discussion{d}}
	if err := closeOutRun(t, r, &coRec{}, closeOutTarget{head: "pushed"}, a); err != nil {
		t.Fatal(err)
	}
	if len(r.sends) != 0 {
		t.Errorf("sends %q, want none: the new reply is input, not to be resolved over", r.sends)
	}
}

func TestCloseOutFailuresAreRecordedAndRetried(t *testing.T) {
	a := filed("ws-a", "closed", "done", disc1+" 1")
	r := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc1, note(1, 5, "x"))}, replyErr: errors.New("boom")}
	c := &coRec{}
	if err := closeOutRun(t, r, c, closeOutTarget{head: "pushed"}, a); err != nil {
		t.Fatalf("a Forge failure is recorded, not returned: %v", err)
	}
	if len(c.bodies) != 1 || !strings.Contains(c.bodies[0], "Close-outs failing: 1: boom") || len(c.ledger) != 0 || slices.Contains(r.sends, "resolve "+disc1) {
		t.Errorf("bodies %q ledger %q sends %q: want the failing line, no ledger, no resolve", c.bodies, c.ledger, r.sends)
	}
	r.replyErr = forge.ErrRateLimited
	if err := closeOutRun(t, r, &coRec{}, closeOutTarget{head: "pushed"}, a); !errors.Is(err, forge.ErrRateLimited) {
		t.Errorf("a 429 must be returned to be held, got %v", err)
	}
}

func TestCloseOutOnAChartUsesGrillings(t *testing.T) {
	g := filed("ws-g", "closed", "no edit needed", disc1+" 1")
	g.Kind = contract.KindGrilling
	r := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc1, note(1, 5, "x"))}}
	r.threadKids = []bd.Bead{g}
	stubJinbeSeams(t, r)
	oAppend, oNew := appendNotes, newestByTrailer
	t.Cleanup(func() { appendNotes, newestByTrailer = oAppend, oNew })
	appendNotes = func(string, string) error { return nil }
	newestByTrailer = func(git.Repo, string, string) (string, error) { return "", nil }
	chart := bd.Bead{ID: "ws-ch", Kind: contract.KindChart, Status: "open"}
	if err := closeOutArm(chart, legs{}, &forge.MR{IID: 7}, "chart/ws-ch", closeOutTarget{head: "pushed"}); err != nil {
		t.Fatal(err)
	}
	if got := replies(r); len(got) != 1 {
		t.Errorf("replies %q, want one for the grilling", got)
	}
}

func TestCloseOutPlainCommentIsResolvedWhateverTheSiblingOrAHumanSays(t *testing.T) {
	a := filed("ws-a", "closed", "done a", disc1+" 1")
	b := filed("ws-b", "open", "", disc1+" 1")
	r := &jinbeSeamRec{discussions: []forge.Discussion{plain(disc1, note(1, 5, "x"))}}
	if err := closeOutRun(t, r, &coRec{}, closeOutTarget{head: "pushed"}, a, b); err != nil {
		t.Fatal(err)
	}
	if len(replies(r)) != 1 || !slices.Contains(r.sends, "resolve "+disc1) {
		t.Errorf("sends %q, want one reply and a resolve though a sibling bead is open", r.sends)
	}
	d := plain(disc1, note(1, 5, "x"), note(2, 5, renderReply(a, "g00d", "jinbe", verdictClosed, "", "", nil)), note(3, 5, "that didn't fix it"))
	r2 := &jinbeSeamRec{discussions: []forge.Discussion{d}}
	if err := closeOutRun(t, r2, &coRec{}, closeOutTarget{head: "pushed"}, a); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r2.sends, "resolve "+disc1) {
		t.Errorf("sends %q, want a plain comment resolved despite a human note after the reply", r2.sends)
	}
}

func TestCloseOutMergedTargetReturnsANonRateLimitFailure(t *testing.T) {
	a := filed("ws-a", "closed", "done", disc1+" 1")
	boom := errors.New("500 server error")
	r := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc1, note(1, 5, "x"))}, replyErr: boom}
	if err := closeOutRun(t, r, &coRec{}, closeOutTarget{head: "pushed", merged: true}, a); !errors.Is(err, boom) {
		t.Fatalf("a merged target's failed reply must be returned so the container stays open, got %v", err)
	}
	r.replyErr = nil
	if err := closeOutRun(t, r, &coRec{}, closeOutTarget{head: "pushed", merged: true}, a); err != nil {
		t.Fatalf("the retry posts: %v", err)
	}
	if !slices.Contains(r.sends, "resolve "+disc1) {
		t.Errorf("sends %q, want the retry to resolve", r.sends)
	}
}

func TestCloseOutDiscussionGoneFromTheForgeIsLedgeredOnce(t *testing.T) {
	r := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc2, note(1, 5, "other"))}}
	c := &coRec{}
	if err := closeOutRun(t, r, c, closeOutTarget{head: "pushed"}, filed("ws-a", "closed", "done", disc1+" 1")); err != nil {
		t.Fatal(err)
	}
	if len(r.sends) != 0 || !slices.Equal(c.ledger, []string{"ws-a Closed out: " + disc1}) {
		t.Errorf("sends %q ledger %q, want no Forge write and the thread settled", r.sends, c.ledger)
	}
}

func TestCloseOutRepliesPerVerdict(t *testing.T) {
	sig := bd.Bead{ID: "ws-sig", Kind: contract.KindSignal, Title: "Carrier"}
	for _, c := range []struct {
		name, reason, sha, want string
	}{
		{"rejected", "rejected: not needed", "", "❌ Rejected · by **Fix ws-a** (ws-a@jinbe)\n\n**Rationale:** not needed\n"},
		{"rejected drops sha", "rejected: no", "abcdef123456", "❌ Rejected · by **Fix ws-a** (ws-a@jinbe)\n\n**Rationale:** no\n"},
		{"answered", "Answered: because", "", "💬 Answered · by **Fix ws-a** (ws-a@jinbe)\n\n**Rationale:** because\n"},
		{"deferred", "deferred: later", "", "⏭️ Deferred · **Fix ws-a** (ws-a@jinbe) · tracked by **Carrier**\n\n**Rationale:** later\n"},
		{"pivoted", "pivoted: mc-a", "", "⏭️ Deferred · **Fix ws-a** (ws-a@jinbe)\n\n**Rationale:** " + pivotRationale + "\n"},
		{"accepted no commit", "accepted: already fixed", "", "✅ Accepted · by **Fix ws-a** (ws-a@jinbe)\n\n**Rationale:** already fixed\n"},
		{"commit subject", "Closed", "abcdef123456", "✅ Accepted · fixed in abcdef12 by **Fix ws-a** (ws-a@jinbe)\n\n**Rationale:** feat: the fix\n"},
		{"bare", "Closed", "", "☑️ Closed · **Fix ws-a** (ws-a@jinbe)\n"},
	} {
		c2 := &coRec{have: true, shas: map[string]string{"Critique: ws-a": c.sha}, reached: map[string]bool{c.sha: true},
			subjects: map[string]string{"abcdef12": "feat: the fix"}, signals: []bd.Bead{sig}}
		r := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc1, note(1, 5, "x"))}}
		if err := closeOutRun(t, r, c2, closeOutTarget{head: "pushed"}, filed("ws-a", "closed", c.reason, disc1+" 1")); err != nil {
			t.Fatal(err)
		}
		want := "reply " + disc1 + " 🤖 mugicrew — " + c.want + marker("ws-a", "g00d", "jinbe")
		if got := replies(r); len(got) != 1 || got[0] != want {
			t.Errorf("%s: replies %q, want %q", c.name, got, want)
		}
	}
}

func deferredReply(t *testing.T, c *coRec, reason string) (*jinbeSeamRec, error) {
	t.Helper()
	r := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc1, note(1, 5, "x"))}}
	err := closeOutRun(t, r, c, closeOutTarget{head: "pushed"}, filed("ws-a", "closed", reason, disc1+" 1"))
	return r, err
}

func TestCloseOutDeferredNamesEverySignalCarrierByTitle(t *testing.T) {
	c := &coRec{signals: []bd.Bead{{ID: "ws-s1", Title: "First", Status: "open"}, {ID: "ws-s2", Title: "Second", Status: "closed"}}}
	r, err := deferredReply(t, c, "deferred: later")
	if err != nil {
		t.Fatal(err)
	}
	if got := replies(r); len(got) != 1 || !strings.Contains(got[0], "⏭️ Deferred · **Fix ws-a** (ws-a@jinbe) · tracked by **First**, **Second**\n\n**Rationale:** later\n") {
		t.Errorf("replies %q", got)
	}
}

func TestCloseOutPivotedNamesTheFirstIDsPivot(t *testing.T) {
	c := &coRec{pivots: map[string]bd.Bead{"mc-a": {ID: "mc-a", Title: "Pivot A", Status: "closed"}, "mc-b": {ID: "mc-b", Title: "Pivot B"}}}
	r, err := deferredReply(t, c, "pivoted: mc-a, mc-b")
	if err != nil {
		t.Fatal(err)
	}
	if got := replies(r); len(got) != 1 || !strings.Contains(got[0], "· tracked by **Pivot A**\n\n**Rationale:** "+pivotRationale+"\n") || strings.Contains(got[0], "Pivot B") {
		t.Errorf("replies %q", got)
	}
}

func TestCloseOutDeferredWithoutACarrierDropsTrackedBy(t *testing.T) {
	for _, reason := range []string{"deferred: later", "pivoted: mc-gone", "pivoted:"} {
		r, err := deferredReply(t, &coRec{}, reason)
		if err != nil {
			t.Fatal(err)
		}
		if got := replies(r); len(got) != 1 || !strings.Contains(got[0], "⏭️ Deferred · **Fix ws-a** (ws-a@jinbe)\n") || strings.Contains(got[0], "tracked by") {
			t.Errorf("%q: replies %q", reason, got)
		}
	}
}

func TestCloseOutCarrierReadFailureIsRecordedAndPostsNothing(t *testing.T) {
	for _, reason := range []string{"deferred: later", "pivoted: mc-a"} {
		c := &coRec{readErr: errors.New("graph down")}
		r, err := deferredReply(t, c, reason)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.sends) != 0 || len(c.ledger) != 0 || len(c.bodies) != 1 || !strings.Contains(c.bodies[0], "Close-outs failing: 1: graph down") {
			t.Errorf("%q: sends %q ledger %q bodies %q, want a recorded failure and no reply", reason, r.sends, c.ledger, c.bodies)
		}
	}
}

func TestCloseOutOtherVerdictsReadNoCarrier(t *testing.T) {
	for _, reason := range []string{"accepted: ok", "rejected: no", "answered: yes", "plain"} {
		c := &coRec{signals: []bd.Bead{{ID: "ws-s1", Title: "First"}}, pivots: map[string]bd.Bead{}}
		if _, err := deferredReply(t, c, reason); err != nil {
			t.Fatal(err)
		}
		if c.reads != 0 {
			t.Errorf("%q: %d carrier reads, want none", reason, c.reads)
		}
	}
}

func sweptRun(t *testing.T, reason string, c *coRec, target closeOutTarget) (*jinbeSeamRec, error) {
	t.Helper()
	r := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc1, note(1, 5, "x"))}}
	c.shas = map[string]string{git.TrailerFor("critique", "ws-a"): "abcdef0123456789"}
	c.reached = map[string]bool{"abcdef0123456789": true}
	err := closeOutRun(t, r, c, target, filed("ws-a", "closed", reason, disc1+" 1"))
	return r, err
}

func TestCloseOutSweptCritiqueRepliesHonestlyWithoutShaOrReason(t *testing.T) {
	confirm := bd.ObsoleteSuperseded("ws-old", "ws-new", "ws-pv")
	escalate := bd.ObsoleteEscalated("ws-old", "mc-chart", "ws-pv")
	head := "🤖 mugicrew — "
	q := "**Fix ws-a** (ws-a@jinbe)"
	for _, tc := range []struct {
		name, reason string
		carrier      []bd.Bead
		want         string
	}{
		{"confirm carried", confirm, []bd.Bead{{ID: "ws-new", Kind: contract.KindSpec, Title: "Revised Spec", Status: "deferred"}}, head + "⏭️ Deferred · " + q + " · tracked by **Revised Spec**\n\n**Rationale:** The Spec was revised and this Merge Request was replaced; this ask moves to the revised Spec.\n"},
		{"escalate carried", escalate, []bd.Bead{{ID: "mc-chart", Kind: contract.KindChart, Title: "New Chart"}}, head + "⏭️ Deferred · " + q + " · tracked by **New Chart**\n\n**Rationale:** The Spec was sent back to charting and this Merge Request was replaced; this ask moves to the new Chart.\n"},
		{"confirm not carried", confirm, nil, head + "☑️ Closed · " + q + "\n\n**Rationale:** The Spec was revised and this Merge Request was replaced; this ask was not carried into the revised Spec.\n"},
		{"escalate not carried", escalate, nil, head + "☑️ Closed · " + q + "\n\n**Rationale:** The Spec was sent back to charting and this Merge Request was replaced; this ask was not carried forward.\n"},
	} {
		// the head reaches the integrate commit, and lacks it: both are due at once
		for name, target := range map[string]closeOutTarget{"reached": {head: "pushed"}, "lacks": {head: "pushed", merged: true, landing: bd.Bead{ID: "ws-land"}}} {
			c := &coRec{carriers: tc.carrier}
			if name == "lacks" {
				c.reached = nil
			}
			r, err := sweptRun(t, tc.reason, c, target)
			if err != nil {
				t.Fatal(err)
			}
			got := replies(r)
			if len(got) != 1 || got[0] != "reply "+disc1+" "+tc.want+marker("ws-a", "g00d", "jinbe") {
				t.Errorf("%s/%s: replies %q", tc.name, name, got)
			}
			if strings.Contains(got[0], "obsolete") || strings.Contains(got[0], "in abcdef01") {
				t.Errorf("%s/%s: reply leaks the reason or a sha: %q", tc.name, name, got[0])
			}
		}
	}
}

func TestCloseOutSweptCarrierReadFailurePostsNothing(t *testing.T) {
	c := &coRec{readErr: errors.New("graph down")}
	r, err := sweptRun(t, bd.ObsoleteSuperseded("a", "b", "c"), c, closeOutTarget{head: "pushed"})
	if err != nil || len(r.sends) != 0 || len(c.bodies) != 1 || !strings.Contains(c.bodies[0], "Close-outs failing: 1: graph down") {
		t.Errorf("err %v sends %q bodies %q, want a recorded failure and no reply", err, r.sends, c.bodies)
	}
}

func TestCloseOutDeferredCloseNamesASpecOrChartCarrier(t *testing.T) {
	c := &coRec{carriers: []bd.Bead{{Kind: contract.KindSpec, Title: "A Spec"}, {Kind: contract.KindChart, Title: "A Chart"}}}
	r, err := deferredReply(t, c, "deferred: later")
	if err != nil {
		t.Fatal(err)
	}
	if got := replies(r); len(got) != 1 || !strings.Contains(got[0], "tracked by **A Spec**, **A Chart**") {
		t.Errorf("replies %q", got)
	}
}

// A pass inside closeOut's unserialized window (Critique closed, Checkpoint open) and a
// later one read the same graph: the second finds the first's reply and posts nothing.
func TestCloseOutSweptRaceBodiesMatchAndPostOnce(t *testing.T) {
	carrier := []bd.Bead{{ID: "ws-new", Kind: contract.KindSpec, Title: "Revised Spec"}}
	reason := bd.ObsoleteSuperseded("ws-old", "ws-new", "ws-pv")
	first, err := sweptRun(t, reason, &coRec{carriers: carrier}, closeOutTarget{head: "pushed"})
	if err != nil || len(replies(first)) != 1 {
		t.Fatalf("first pass: err %v sends %q", err, first.sends)
	}
	body := strings.SplitN(replies(first)[0], " ", 3)[2]
	b := filed("ws-a", "closed", reason, disc1+" 1")
	out := thread(disc1, note(1, 5, "x"), note(2, 5, body))
	r := &jinbeSeamRec{discussions: []forge.Discussion{out}}
	c := &coRec{carriers: carrier}
	if err := closeOutRun(t, r, c, closeOutTarget{head: "pushed", merged: true, landing: bd.Bead{ID: "ws-land"}}, b); err != nil {
		t.Fatal(err)
	}
	if got := replies(r); len(got) != 0 {
		t.Errorf("second pass posted %q, want none: the marker keeps it to one post", got)
	}
}

// Only the successor the swept reason names carries a swept Critique's ask: an offshoot
// Signal discovered-from it is not the successor.
func TestCloseOutSweptNamesOnlyTheSuccessorCarrier(t *testing.T) {
	reason := bd.ObsoleteSuperseded("ws-old", "ws-new", "ws-pv")
	offshoot := bd.Bead{ID: "ws-sig", Kind: contract.KindSignal, Title: "Offshoot Signal"}
	c := &coRec{signals: []bd.Bead{offshoot}}
	r, err := sweptRun(t, reason, c, closeOutTarget{head: "pushed"})
	if err != nil {
		t.Fatal(err)
	}
	if got := replies(r); len(got) != 1 || !strings.Contains(got[0], "☑️ Closed") || strings.Contains(got[0], "Offshoot Signal") || strings.Contains(got[0], "tracked by") {
		t.Errorf("unrelated Signal only: replies %q, want neutral Closed naming no carrier", got)
	}
	c = &coRec{signals: []bd.Bead{offshoot}, carriers: []bd.Bead{{ID: "ws-new", Kind: contract.KindSpec, Title: "Revised Spec"}}}
	r, err = sweptRun(t, reason, c, closeOutTarget{head: "pushed"})
	if err != nil {
		t.Fatal(err)
	}
	if got := replies(r); len(got) != 1 || !strings.Contains(got[0], "tracked by **Revised Spec**") || strings.Contains(got[0], "Offshoot Signal") {
		t.Errorf("successor and Signal: replies %q, want only the successor named", got)
	}
}
