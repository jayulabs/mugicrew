package usopp

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/bd/bdtest"
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

// frRec is the seeded Forge Review graph: the container (a Checkpoint at priority 1), the
// dispatched Forge Review (claimed by this process) and whatever the test adds.
type frRec struct {
	f *bdtest.Fake
	t *testing.T
}

func newFR(t *testing.T, notes string, extra ...bd.Bead) *frRec {
	t.Helper()
	own := bdtest.Claimed(bd.Bead{ID: "ws-fr", Kind: contract.KindForgeReview, Parent: "ws-cp", Description: frSnapshot, Notes: notes})
	container := bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open", Priority: 1}
	return &frRec{f: bdtest.New(t, append([]bd.Bead{container, own}, extra...)...), t: t}
}

func (r *frRec) run(h *recordingHarness) error {
	r.t.Helper()
	return Usopp{}.Run(stubCtx{h}, contract.Bead{ID: "ws-fr", Kind: contract.KindForgeReview})
}

// draft is a Draft of the Forge Review at priority 2, the container's being 1.
func draft(id string) bd.Bead {
	return bdtest.Draft("ws-fr", bd.Bead{ID: id, Kind: contract.KindCritique, Priority: 2, Parent: "ws-cp"})
}

// closeReason is the Forge Review's close reason, "" while it is open.
func (r *frRec) closeReason() string {
	b := r.f.Bead("ws-fr")
	if b.Open() {
		return ""
	}
	return b.CloseReason
}

func TestForgeReviewPromptCarriesSnapshotFiledBeadsDraftBlockAndRubric(t *testing.T) {
	r := newFR(t, "901 file k1 — ask\n902 fold ws-c1 — bot\n903 skip — praise",
		bd.Bead{ID: "ws-c1", Kind: contract.KindCritique, Parent: "ws-cp", Status: "closed", CloseReason: "not needed: by design", Description: "Source: !12 d0 700\n> x"},
		bd.Bead{ID: "ws-c2", Kind: contract.KindCritique, Parent: "ws-cp", Status: "open", Description: "Source: !99 d9 1\nSource: !12 d0 701"},
		bd.Bead{ID: "ws-c3", Kind: contract.KindCritique, Parent: "ws-cp", Status: "open", Description: "unrelated Source: !12 inline"},
		bd.Bead{ID: "ws-c4", Kind: contract.KindCritique, Parent: "ws-cp", Status: "open", Description: "no sources", Notes: "Source: !12 d0 702"},
		draft("d-1"))
	h := &recordingHarness{}
	if err := r.run(h); err != nil {
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
	if !strings.Contains(p, "ws-c2 [open] source notes: 701\n") { // not "d9" anywhere: the crewdocs hash path can contain it
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

func TestForgeReviewFiledClosesWithCountsAndObsoletesLandingWithoutStampingPriority(t *testing.T) {
	r := newFR(t, "901 file k1 — ask\n902 file k1 — same\n903 skip — praise", draft("dr-1"),
		bd.Bead{ID: "ws-land", Kind: contract.KindLanding, Parent: "ws-cp", Status: "open"},
		bd.Bead{ID: "ws-oldland", Kind: contract.KindLanding, Parent: "ws-cp", Status: "closed", CloseReason: "merged"})
	if err := r.run(&recordingHarness{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := "completed: filed 1, folded 0, skipped 1; read through note 903"; r.closeReason() != want {
		t.Errorf("close = %q, want %q", r.closeReason(), want)
	}
	if d := r.f.Bead("dr-1"); d.Priority != 2 || d.Status != "open" {
		t.Errorf("the Draft must publish at its default priority, not the container's, got %+v", d)
	}
	if l := r.f.Bead("ws-land"); l.Open() || l.CloseReason != "obsolete: review comment" {
		t.Errorf("the open Landing must close obsolete: review comment, got %+v", l)
	}
	if l := r.f.Bead("ws-oldland"); l.CloseReason != "merged" {
		t.Errorf("a closed Landing must stay as it was, got %+v", l)
	}
}

func TestForgeReviewFoldedCountsAndNothingActionable(t *testing.T) {
	r := newFR(t, "901 fold ws-c1 — dup\n902 fold ws-c1 — dup\n903 skip — praise",
		bd.Bead{ID: "ws-land", Kind: contract.KindLanding, Parent: "ws-cp", Status: "open"})
	if err := r.run(&recordingHarness{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := "completed: filed 0, folded 2, skipped 1; read through note 903"; r.closeReason() != want {
		t.Errorf("close = %q, want %q", r.closeReason(), want)
	}
	if !r.f.Bead("ws-land").Open() {
		t.Error("no Draft: the pending Landing must stay open")
	}

	r = newFR(t, "901 skip — a\n902 skip — b\n903 skip — c",
		bd.Bead{ID: "ws-land", Kind: contract.KindLanding, Parent: "ws-cp", Status: "open"})
	if err := r.run(&recordingHarness{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := "completed: nothing actionable; read 3 notes through 903"; r.closeReason() != want {
		t.Errorf("close = %q, want %q", r.closeReason(), want)
	}
	if !r.f.Bead("ws-land").Open() {
		t.Error("empty pass closed the Landing")
	}
}

func TestForgeReviewLedgerMissingAJudgeNoteFailsTheRun(t *testing.T) {
	r := newFR(t, "901 file k1 — ask\n902 skip — x", draft("dr-1"), // 903 missing
		bd.Bead{ID: "ws-land", Kind: contract.KindLanding, Parent: "ws-cp", Status: "open"})
	err := r.run(&recordingHarness{})
	if err == nil || !strings.Contains(err.Error(), "903") {
		t.Fatalf("err = %v, want one naming the missing note 903", err)
	}
	if d := r.f.Bead("dr-1"); r.closeReason() != "" || d.Priority != 2 || d.Status != "deferred" || !r.f.Bead("ws-land").Open() {
		t.Errorf("a failed pass must write nothing: close %q, draft %+v", r.closeReason(), d)
	}
}

func TestForgeReviewFileVerdictWithoutDraftFailsTheRun(t *testing.T) {
	r := newFR(t, "901 file k1 — ask\n902 skip — x\n903 skip — y")
	if err := r.run(&recordingHarness{}); err == nil {
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
	r := newFR(t, "901 skip — a\n902 skip — b\n903 skip — c")
	err := r.run(&recordingHarness{err: errors.New("boom")})
	if err == nil || r.closeReason() != "" {
		t.Errorf("err %v, close %q: want an error and no close", err, r.closeReason())
	}
}

func TestForgeReviewRoutesThroughUsoppAndRejectsLaunch(t *testing.T) {
	if _, err := (Usopp{}).Launch(stubCtx{&recordingHarness{}}, contract.Bead{ID: "x", Kind: contract.KindForgeReview}); err == nil {
		t.Error("forge-review is AFK: Launch must error")
	}
}

// FR49 wiring: a confirm sweep closes the forge-review while the session fails; the
// runner reports it done, so the tail writes nothing.
func TestForgeReviewSweptDuringFailedRunIsDone(t *testing.T) {
	r := newFR(t, "901 file k1 — a\n902 skip — b\n903 skip — c", draft("dr-1"))
	h := &recordingHarness{err: errors.New("session killed"), onRun: func(contract.Invocation) {
		r.f.Mutate("ws-fr", func(b *bd.Bead) { b.Status = "closed"; b.CloseReason = "swept" })
	}}
	if err := r.run(h); err != nil {
		t.Errorf("Run = %v, want nil (swept is done)", err)
	}
	if d := r.f.Bead("dr-1"); r.closeReason() != "swept" || d.Priority != 2 || d.Status != "deferred" {
		t.Errorf("swept forge-review touched the graph: close %q, draft %+v", r.closeReason(), d)
	}
}
