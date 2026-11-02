package zoro

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// triageWrites is every graph write dismiss and amend make, in order: the sweep's writes
// plus comments.
func (r *sweepRig) triageWrites() []string {
	return slices.DeleteFunc(r.f.Calls(), func(c string) bool {
		verb, _, _ := strings.Cut(c, " ")
		return !slices.Contains([]string{"Create", "Close", "AddEdge", "Supersede", "SetField", "RemoveEdge", "AddComment"}, verb)
	})
}

// count is how many triage writes are exactly call.
func (r *sweepRig) count(call string) int {
	n := 0
	for _, c := range r.triageWrites() {
		if c == call {
			n++
		}
	}
	return n
}

// raisedBy seeds the open Errand ws-er and makes the lead pivot discovered-from it and from extra.
func (r *sweepRig) raisedBy(extra ...string) {
	r.f.Seed(bd.Bead{ID: "ws-er", Kind: contract.KindErrand, Status: "open", Title: "errand", Labels: []string{"kind:errand"}})
	r.f.Mutate("ws-pv", func(b *bd.Bead) { b.DiscoveredFrom = append([]string{"ws-er"}, extra...) })
}

// ask seeds an open Ask Critique discovered-from the lead pivot, with its raiser edge.
func (r *sweepRig) ask(id, title, body string, edge func(*bd.Bead)) {
	b := bd.Bead{ID: id, Kind: contract.KindCritique, Status: "open", Title: title, Description: body,
		DiscoveredFrom: []string{"ws-pv"}, Labels: []string{"kind:critique"}}
	edge(&b)
	r.f.Seed(b)
}

// notes is id's comment texts.
func (r *sweepRig) notes(t *testing.T, id string) []string {
	t.Helper()
	cs, err := r.f.Comments(id)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, c := range cs {
		texts = append(texts, c.Text)
	}
	return texts
}

// refiled is the one Critique discovered-from the lead pivot with no raiser edge.
func (r *sweepRig) refiled(t *testing.T) bd.Bead {
	t.Helper()
	return r.spawned(t, contract.KindCritique)
}

func TestDismissRefilesTheEffortRaisedFRThenClosesLast(t *testing.T) {
	r := newSweepRig(t, "ws-chart")
	r.f.Mutate("ws-cp", func(b *bd.Bead) { b.Labels = append(b.Labels, "context:apps-x", "repo:vendor/x") })
	r.raisedBy()
	if err := Dismiss("ws-pv", "FR3 stands", "# Finish FR3\n\nthe rest", nil, "ws-lead"); err != nil {
		t.Fatal(err)
	}
	c := r.refiled(t)
	if c.Parent != "ws-cp" || c.Status != "deferred" || !c.HasLabel("drafted-by:ws-lead") || c.Title != "Finish FR3" ||
		!c.HasLabel("context:apps-x") || !c.HasLabel("repo:vendor/x") || c.CausedBy != "" {
		t.Errorf("refile = %+v", c)
	}
	r.wantClosed(t, "ws-pv", "dismissed: FR3 stands")
	if want := []string{"Create", "AddComment ws-er", "Close ws-pv"}; !slices.Equal(r.triageWrites(), want) {
		t.Errorf("writes = %v, want %v", r.triageWrites(), want)
	}
}

func TestDismissRefusesBeforeAnyWrite(t *testing.T) {
	for name, tc := range map[string]struct {
		setup    func(*sweepRig)
		critique string
		want     string
	}{
		"no Ask and no --critique": {func(*sweepRig) {}, "", "--critique"},
		"Ask and --critique": {func(r *sweepRig) {
			r.ask("ws-ask", "a", "b", func(b *bd.Bead) { b.CausedBy = "ws-sig" })
		}, "body", "drop --critique"},
		"closed Checkpoint": {func(r *sweepRig) { r.f.Mutate("ws-cp", func(b *bd.Bead) { b.Status = "closed" }) }, "body", "Checkpoint ws-cp is closed"},
		"non-pivot":         {func(r *sweepRig) { r.f.Mutate("ws-pv", func(b *bd.Bead) { b.Kind = contract.KindBuild }) }, "body", `is kind "build"`},
	} {
		r := newSweepRig(t, "ws-chart")
		tc.setup(r)
		err := Dismiss("ws-pv", "g", tc.critique, nil, "ws-lead")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
		if w := r.triageWrites(); len(w) != 0 {
			t.Errorf("%s: wrote %v", name, w)
		}
	}
}

func TestDismissRefusesEmptyInputs(t *testing.T) {
	for _, tc := range [][2]string{{"", "ws-lead"}, {"g", ""}} {
		r := newSweepRig(t, "ws-chart")
		if err := Dismiss("ws-pv", tc[0], "body", nil, tc[1]); err == nil || len(r.triageWrites()) != 0 {
			t.Errorf("gist %q drafted-by %q: err %v, wrote %v", tc[0], tc[1], err, r.triageWrites())
		}
	}
}

func TestDismissClosedPivotIsANoOp(t *testing.T) {
	r := newSweepRig(t, "ws-chart")
	r.f.Mutate("ws-pv", func(b *bd.Bead) { b.Status = "closed" })
	if err := Dismiss("ws-pv", "g", "body", nil, "ws-lead"); err != nil || len(r.triageWrites()) != 0 {
		t.Errorf("err %v, wrote %v", err, r.triageWrites())
	}
}

func TestDismissRerunAfterPartialFailureConverges(t *testing.T) {
	r := newSweepRig(t, "ws-chart")
	r.f.FailOn("Close", "ws-pv", errors.New("bd close: boom"))
	if err := Dismiss("ws-pv", "g", "body", nil, "ws-lead"); err == nil {
		t.Fatal("first run: want the close failure")
	}
	r.f.Heal("Close", "ws-pv")
	if err := Dismiss("ws-pv", "g", "body", nil, "ws-lead"); err != nil {
		t.Fatal(err)
	}
	r.refiled(t)
	if n := r.count("Create"); n != 1 {
		t.Errorf("created %d refiles, want one", n)
	}
	r.wantClosed(t, "ws-pv", "dismissed: g")
}

func TestDismissCreateFailureLeavesThePivotOpen(t *testing.T) {
	r := newSweepRig(t, "ws-chart")
	r.f.FailOn("Create", "*", errors.New("boom"))
	if err := Dismiss("ws-pv", "g", "body", nil, "ws-lead"); err == nil || !r.f.Bead("ws-pv").Open() {
		t.Errorf("err %v, pivot %s", err, r.f.Bead("ws-pv").Status)
	}
}

func TestPivotRaisersTellsAskFromRefileByEdgesNotLabels(t *testing.T) {
	r := newSweepRig(t, "ws-chart")
	r.raisedBy("ws-sig")
	r.ask("ws-askS", "s", "", func(b *bd.Bead) { b.CausedBy = "ws-sig" })
	r.ask("ws-askE", "e", "", func(b *bd.Bead) { b.DiscoveredFrom = append(b.DiscoveredFrom, "ws-er") })
	r.ask("ws-ref", "r", "", func(b *bd.Bead) { b.Labels = append(b.Labels, "drafted-by:ws-x") })
	r.f.Seed(bd.Bead{ID: "ws-x", Kind: contract.KindBuild, Status: "open", DiscoveredFrom: []string{"ws-pv"}})
	got, err := pivotRaisers("ws-pv")
	if err != nil {
		t.Fatal(err)
	}
	if asks := slices.Sorted(slices.Values(beadIDs(got.Asks))); !slices.Equal(asks, []string{"ws-askE", "ws-askS"}) || got.Refile != "ws-ref" || len(got.Errands) != 1 || got.Errands[0].ID != "ws-er" {
		t.Errorf("raisers = %v refile %q errands %v", asks, got.Refile, got.Errands)
	}
}

// askGraph is a pivot raised by a reroute Signal (Ask ws-askS, caused-by ws-sig) and an
// Errand (Ask ws-askE, discovered-from ws-er), each Ask open and discovered-from the pivot;
// ws-er raised the pivot.
func askGraph(t *testing.T) *sweepRig {
	r := newSweepRig(t, "ws-chart")
	r.raisedBy("ws-sig")
	r.ask("ws-askS", "old S", "old S", func(b *bd.Bead) { b.CausedBy = "ws-sig" })
	r.ask("ws-askE", "old E", "old E", func(b *bd.Bead) { b.DiscoveredFrom = append(b.DiscoveredFrom, "ws-er") })
	return r
}

func TestDismissRewritesOneAskAndClosesTheRest(t *testing.T) {
	r := askGraph(t)
	if err := Dismiss("ws-pv", "FR3 stands", "", map[string]string{"ws-askE": "New title\n\nnew body\n"}, "ws-lead"); err != nil {
		t.Fatal(err)
	}
	if b := r.f.Bead("ws-askE"); b.Title != "New title" || b.Description != "new body" || !b.Open() {
		t.Errorf("rewritten Ask = %q %q %s", b.Title, b.Description, b.Status)
	}
	r.wantClosed(t, "ws-askS", "dismissed: ws-pv")
	if b := r.f.Bead("ws-sig"); b.CloseReason != "" {
		t.Errorf("Signal re-closed: %q", b.CloseReason)
	}
	if n := r.count("Create"); n != 0 {
		t.Errorf("filed %d refiles, want none", n)
	}
	want := []string{"pivot ws-pv dismissed: FR3 stands. Wait for ws-askE."}
	if got := r.notes(t, "ws-er"); !slices.Equal(got, want) {
		t.Errorf("Errand comments = %q, want %q", got, want)
	}
	if w := r.triageWrites(); w[len(w)-1] != "Close ws-pv" {
		t.Errorf("writes = %v: the pivot close is not last", w)
	}
	if !r.f.Bead("ws-er").Open() {
		t.Error("Errand closed")
	}
}

func TestDismissRewriteSkippedWhenAskAlreadyMatches(t *testing.T) {
	r := askGraph(t)
	r.f.Mutate("ws-askE", func(b *bd.Bead) { b.Description = "same body" })
	if err := Dismiss("ws-pv", "g", "", map[string]string{"ws-askE": "old E\nsame body"}, "ws-lead"); err != nil {
		t.Fatal(err)
	}
	if n := r.count("SetField ws-askE"); n != 0 {
		t.Errorf("wrote %d fields on ws-askE, want none", n)
	}
}

func TestDismissClosingEveryAskTellsTheErrandToAmend(t *testing.T) {
	r := askGraph(t)
	if err := Dismiss("ws-pv", "wrong Spec", "", nil, "ws-lead"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ws-askS", "ws-askE"} {
		r.wantClosed(t, id, "dismissed: ws-pv")
	}
	want := []string{"pivot ws-pv dismissed: wrong Spec. The step may be wrong; amend it on resume."}
	if got := r.notes(t, "ws-er"); !slices.Equal(got, want) {
		t.Errorf("Errand comments = %q, want %q", got, want)
	}
}

func TestDismissErrandWhoseOwnAskWasClosedIsToldToAmend(t *testing.T) {
	r := askGraph(t)
	if err := Dismiss("ws-pv", "g", "", map[string]string{"ws-askS": "New\nbody"}, "ws-lead"); err != nil {
		t.Fatal(err)
	}
	r.wantClosed(t, "ws-askE", "dismissed: ws-pv")
	want := []string{"pivot ws-pv dismissed: g. The step may be wrong; amend it on resume."}
	if got := r.notes(t, "ws-er"); !slices.Equal(got, want) {
		t.Errorf("Errand comments = %q, want %q", got, want)
	}
}

func TestDismissRefusesAnUnknownRewriteBeforeAnyWrite(t *testing.T) {
	r := askGraph(t)
	err := Dismiss("ws-pv", "g", "", map[string]string{"ws-askE": "t\nb", "ws-other": "t\nb"}, "ws-lead")
	if err == nil || !strings.Contains(err.Error(), "ws-other") {
		t.Fatalf("err = %v", err)
	}
	if w := r.triageWrites(); len(w) != 0 {
		t.Errorf("wrote %v", w)
	}
}

func TestDismissAskRerunConverges(t *testing.T) {
	r := askGraph(t)
	r.f.FailOn("Close", "ws-pv", errors.New("bd close: boom"))
	rw := map[string]string{"ws-askE": "New\nbody"}
	if err := Dismiss("ws-pv", "g", "", rw, "ws-lead"); err == nil {
		t.Fatal("first run: want the close failure")
	}
	r.f.Heal("Close", "ws-pv")
	if err := Dismiss("ws-pv", "g", "", rw, "ws-lead"); err != nil {
		t.Fatal(err)
	}
	if r.count("Close ws-askS") != 1 || len(r.notes(t, "ws-er")) != 1 {
		t.Errorf("writes %v, Errand comments %q: want one Ask close and one comment", r.triageWrites(), r.notes(t, "ws-er"))
	}
	r.wantClosed(t, "ws-pv", "dismissed: g")
}

func TestDismissRefileErrandCommentNamesTheRefile(t *testing.T) {
	r := newSweepRig(t, "ws-chart")
	r.raisedBy()
	if err := Dismiss("ws-pv", "g", "body", nil, "ws-lead"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"pivot ws-pv dismissed: g. Wait for " + r.refiled(t).ID + "."}; !slices.Equal(r.notes(t, "ws-er"), want) {
		t.Errorf("Errand comments = %q, want %q", r.notes(t, "ws-er"), want)
	}
}

func TestDismissRewriteStripsAKindPrefixFromTheAskTitle(t *testing.T) {
	r := askGraph(t)
	if err := Dismiss("ws-pv", "FR3 stands", "", map[string]string{"ws-askE": "Critique: New title\n\nnew body\n"}, "ws-lead"); err != nil {
		t.Fatal(err)
	}
	if b := r.f.Bead("ws-askE"); b.Title != "New title" || b.Description != "new body" || !b.Open() {
		t.Errorf("rewritten Ask = %q %q %s", b.Title, b.Description, b.Status)
	}
}

func TestDismissRewriteWithAStrippedTitleAlreadyMatchingWritesNoTitle(t *testing.T) {
	r := askGraph(t)
	r.f.Mutate("ws-askE", func(b *bd.Bead) { b.Description = "same body" })
	if err := Dismiss("ws-pv", "g", "", map[string]string{"ws-askE": "Critique: old E\nsame body"}, "ws-lead"); err != nil {
		t.Fatal(err)
	}
	if n := r.count("SetField ws-askE"); n != 0 {
		t.Errorf("wrote %d fields on ws-askE, want none", n)
	}
}

// ADR 0419 §3: the dismiss Critique has no predecessor, so it keeps the default P2 even
// when its Checkpoint sits at another level.
func TestDismissCritiqueIsBornAtTheDefaultPriority(t *testing.T) {
	r := newSweepRig(t, "ws-chart")
	r.f.Mutate("ws-cp", func(b *bd.Bead) { b.Priority = 0 })
	r.raisedBy()
	if err := Dismiss("ws-pv", "FR3 stands", "# Finish FR3\n\nthe rest", nil, "ws-lead"); err != nil {
		t.Fatal(err)
	}
	if got := r.refiled(t).Priority; got != 2 {
		t.Errorf("dismiss Critique born at P%d, want P2", got)
	}
}
