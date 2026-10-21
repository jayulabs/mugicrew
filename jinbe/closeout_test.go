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
	ledger  []string // "<bead> <line>" appended to notes
	bodies  []string // container body writes
	fetched []string
	have    bool            // the clone holds the target head
	reached map[string]bool // integrate sha → reachable from the head
	signals []bd.Bead       // the stranded-commits Signals found
	shas    map[string]string
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
	oHas, oFetch, oReach, oSig, oAppend, oUpdate, oShow, oNew := hasCommit, fetchCommit, isReachable, signalsFrom, appendNotes, updateBody, showBead, newestByTrailer
	t.Cleanup(func() {
		hasCommit, fetchCommit, isReachable, signalsFrom, appendNotes, updateBody, showBead, newestByTrailer = oHas, oFetch, oReach, oSig, oAppend, oUpdate, oShow, oNew
	})
	hasCommit = func(git.Repo, string) bool { return c.have }
	fetchCommit = func(_ git.Repo, sha string) error { c.fetched = append(c.fetched, sha); return nil }
	isReachable = func(_ git.Repo, sha, _ string) bool { return c.reached[sha] }
	signalsFrom = func(string) ([]bd.Bead, error) { return c.signals, nil }
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
	want := []string{"reply " + disc1 + " 🤖 mugicrew: addressed by **Fix ws-a** (ws-a@jinbe): not needed: by design\n" + marker("ws-a", "g00d", "jinbe"), "resolve " + disc1}
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
	if got := replies(r); len(got) != 1 || !strings.Contains(got[0], " in abcdef12: renamed") {
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
	r2 := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc1, note(1, 5, "a"), note(2, 5, "b"), note(3, 5, renderReply(a, "g00d", "jinbe", "")))}}
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
	out := thread(disc1, note(1, 5, "x"), note(2, 5, renderReply(a, "g00d", "jinbe", "")))
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
	d := thread(disc1, note(1, 5, "x"), note(2, 5, renderReply(a, "g00d", "jinbe", "")), note(3, 5, "that didn't fix it"))
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
	d := plain(disc1, note(1, 5, "x"), note(2, 5, renderReply(a, "g00d", "jinbe", "")), note(3, 5, "that didn't fix it"))
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
