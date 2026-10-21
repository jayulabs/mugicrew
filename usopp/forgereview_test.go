package usopp

import (
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/promptguard"
)

const frSnapshot = `MR: !12
Read from note: none
Read through note: 903
Notes count: 3

<discussion id="d1" kind="diff thread on a.go:4">
<note id="901" by="alice" at="2026-10-20T10:00:00Z" status="JUDGE">
rename x
</note>
<note id="902" by="bot[bot] bot" at="2026-10-20T10:01:00Z" status="JUDGE">
rename x too
</note>
</discussion>
<discussion id="d2" kind="plain comment">
<note id="903" by="alice" at="2026-10-20T10:02:00Z" status="JUDGE">
LGTM
</note>
<note id="800" by="alice" at="2026-10-19T10:02:00Z" status="context, read earlier">
older
</note>
</discussion>`

// frRec captures the seams runForgeReview drives.
type frRec struct {
	container bd.Bead
	own       bd.Bead
	kids      map[contract.Kind][]bd.Bead
	drafts    []bd.Bead
	landings  []bd.Bead
	updates   []string
	closed    []string
	reasons   []string
	ownClosed string
}

func stubForgeReview(t *testing.T, r *frRec) {
	t.Helper()
	oShow, oClose, oKids, oDrafts, oUpdate, oLandings, oPublish := showBead, closeBead, childrenOfKind, draftsOf, updateBead, landingsOf, closeOnPublish
	t.Cleanup(func() {
		showBead, closeBead, childrenOfKind, draftsOf, updateBead, landingsOf, closeOnPublish = oShow, oClose, oKids, oDrafts, oUpdate, oLandings, oPublish
	})
	showBead = func(id string) (bd.Bead, error) {
		if id == r.container.ID {
			return r.container, nil
		}
		return r.own, nil
	}
	childrenOfKind = func(_ string, k contract.Kind) ([]bd.Bead, error) { return r.kids[k], nil }
	draftsOf = func(string) ([]bd.Bead, error) { return r.drafts, nil }
	updateBead = func(id string, args []string) error {
		r.updates = append(r.updates, id+" "+strings.Join(args, " "))
		return nil
	}
	landingsOf = func(string) ([]bd.Bead, error) { return r.landings, nil }
	closeBead = func(id, reason string) error {
		r.closed = append(r.closed, id)
		r.reasons = append(r.reasons, reason)
		return nil
	}
	closeOnPublish = func(_ contract.Ctx, id, reason string, _ []bd.Edge) error {
		r.ownClosed = id + ": " + reason
		return nil
	}
}

func newFRRec() *frRec {
	return &frRec{
		container: bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open", Priority: 1},
		own:       bd.Bead{ID: "ws-fr", Kind: contract.KindForgeReview, Status: "in_progress", Parent: "ws-cp", Description: frSnapshot},
		kids:      map[contract.Kind][]bd.Bead{},
	}
}

func runFR(t *testing.T, r *frRec, h *recordingHarness) error {
	t.Helper()
	stubForgeReview(t, r)
	return Usopp{}.Run(stubCtx{h}, contract.Bead{ID: "ws-fr", Kind: contract.KindForgeReview})
}

func TestForgeReviewPromptCarriesSnapshotFiledBeadsDraftBlockAndRubric(t *testing.T) {
	r := newFRRec()
	r.kids[contract.KindCritique] = []bd.Bead{
		{ID: "ws-c1", Status: "closed", CloseReason: "not needed: by design", Description: "Source: !12 d0 700\n> x"},
		{ID: "ws-c2", Status: "open", Description: "Source: !99 d9 1\nSource: !12 d0 701"},
		{ID: "ws-c3", Status: "open", Description: "unrelated Source: !12 inline"},
		{ID: "ws-c4", Status: "open", Description: "no sources", Notes: "Source: !12 d0 702"},
	}
	h := &recordingHarness{}
	r.own.Notes = "901 file k1 — ask\n902 fold ws-c1 — bot\n903 skip — praise"
	r.drafts = []bd.Bead{{ID: "d-1"}}
	if err := runFR(t, r, h); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(h.invs) != 1 {
		t.Fatalf("want one session, got %d", len(h.invs))
	}
	p := h.invs[0].Prompt
	for _, want := range []string{
		frSnapshot, "ws-c1", "closed", "not needed: by design", "700", "ws-c2", "open", "701", "ws-c4", "702",
		"drafted-by:ws-fr", "kind:critique", "--parent ws-cp", "discovered-from:ws-fr",
		"Judged: unsure", "Source: !<iid> <discussion-id> <note-id>", "<note-id> file", "<note-id> fold", "<note-id> skip",
		"git diff", "ws-fr",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	if strings.Contains(p, "ws-c3") {
		t.Errorf("a bead with no Source: line for this MR must not be listed")
	}
	if strings.Contains(p, "ws-c2 ") && strings.Contains(p, "d9") {
		t.Errorf("another MR's source note leaked into the filed list")
	}
}

func TestForgeReviewPromptOnChartFilesGrillings(t *testing.T) {
	p := forgeReviewPrompt(forgeArgs{ReviewID: "ws-fr", ContainerID: "ws-ch", FiledKind: contract.KindGrilling, Branch: "chart/ws-ch"})
	if !strings.Contains(p, "kind:grilling") || strings.Contains(p, "kind:critique") {
		t.Errorf("chart prompt must file kind:grilling only:\n%s", p)
	}
}

func TestForgeReviewPromptSkillFree(t *testing.T) {
	p := forgeReviewPrompt(forgeArgs{ReviewID: "ws-fr", ContainerID: "ws-cp", FiledKind: contract.KindCritique, Branch: "checkpoint/ws-cp", Snapshot: frSnapshot})
	promptguard.Check(t, "forge-review", p)
}

func TestForgeReviewFiledClosesWithCountsAtContainerPriorityAndObsoletesLanding(t *testing.T) {
	r := newFRRec()
	r.own.Notes = "901 file k1 — ask\n902 file k1 — same\n903 skip — praise"
	r.drafts = []bd.Bead{{ID: "dr-1"}}
	r.landings = []bd.Bead{{ID: "ws-land", Status: "open"}, {ID: "ws-oldland", Status: "closed"}}
	if err := runFR(t, r, &recordingHarness{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := "ws-fr: filed 1, folded 0, skipped 1: read through note 903"; r.ownClosed != want {
		t.Errorf("close = %q, want %q", r.ownClosed, want)
	}
	if want := []string{"dr-1 --priority " + strconv.Itoa(1)}; !reflect.DeepEqual(r.updates, want) {
		t.Errorf("updates = %v, want %v", r.updates, want)
	}
	if !reflect.DeepEqual(r.closed, []string{"ws-land"}) || r.reasons[0] != "obsolete: review comment" {
		t.Errorf("closed %v %v, want the open Landing obsolete: review comment", r.closed, r.reasons)
	}
}

func TestForgeReviewFoldedCountsAndNothingActionable(t *testing.T) {
	r := newFRRec()
	r.own.Notes = "901 fold ws-c1 — dup\n902 fold ws-c1 — dup\n903 skip — praise"
	r.landings = []bd.Bead{{ID: "ws-land", Status: "open"}}
	if err := runFR(t, r, &recordingHarness{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := "ws-fr: filed 0, folded 2, skipped 1: read through note 903"; r.ownClosed != want {
		t.Errorf("close = %q, want %q", r.ownClosed, want)
	}
	if len(r.closed) != 0 {
		t.Errorf("no Draft: the pending Landing must stay open, closed %v", r.closed)
	}

	r = newFRRec()
	r.own.Notes = "901 skip — a\n902 skip — b\n903 skip — c"
	r.landings = []bd.Bead{{ID: "ws-land", Status: "open"}}
	if err := runFR(t, r, &recordingHarness{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := "ws-fr: nothing actionable: read 3 notes through 903"; r.ownClosed != want {
		t.Errorf("close = %q, want %q", r.ownClosed, want)
	}
	if len(r.closed) != 0 || len(r.updates) != 0 {
		t.Errorf("empty pass wrote %v %v", r.closed, r.updates)
	}
}

func TestForgeReviewLedgerMissingAJudgeNoteFailsTheRun(t *testing.T) {
	r := newFRRec()
	r.own.Notes = "901 file k1 — ask\n902 skip — x" // 903 missing
	r.drafts = []bd.Bead{{ID: "dr-1"}}
	r.landings = []bd.Bead{{ID: "ws-land", Status: "open"}}
	err := runFR(t, r, &recordingHarness{})
	if err == nil || !strings.Contains(err.Error(), "903") {
		t.Fatalf("err = %v, want one naming the missing note 903", err)
	}
	if r.ownClosed != "" || len(r.closed) != 0 || len(r.updates) != 0 {
		t.Errorf("a failed pass must write nothing: close %q, closed %v, updates %v", r.ownClosed, r.closed, r.updates)
	}
}

func TestForgeReviewFileVerdictWithoutDraftFailsTheRun(t *testing.T) {
	r := newFRRec()
	r.own.Notes = "901 file k1 — ask\n902 skip — x\n903 skip — y"
	if err := runFR(t, r, &recordingHarness{}); err == nil {
		t.Fatal("a file verdict with no Draft must fail the run")
	}
}

func TestForgeReviewContextNotesAreNotJudged(t *testing.T) {
	ids := judgeIDs(frSnapshot)
	if want := []string{"901", "902", "903"}; !slices.Equal(ids, want) {
		t.Errorf("judgeIDs = %v, want %v", ids, want)
	}
}

func TestForgeReviewHarnessErrorLeavesBeadOpen(t *testing.T) {
	r := newFRRec()
	r.own.Notes = "901 skip — a\n902 skip — b\n903 skip — c"
	err := runFR(t, r, &recordingHarness{err: errors.New("boom")})
	if err == nil || r.ownClosed != "" {
		t.Errorf("err %v, closed %q: want an error and no close", err, r.ownClosed)
	}
}

func TestForgeReviewRoutesThroughUsoppAndRejectsLaunch(t *testing.T) {
	if _, err := (Usopp{}).Launch(stubCtx{&recordingHarness{}}, contract.Bead{ID: "x", Kind: contract.KindForgeReview}); err == nil {
		t.Error("forge-review is AFK: Launch must error")
	}
}
