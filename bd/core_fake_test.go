package bd_test

import (
	"bytes"
	"errors"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/bd/bdtest"
	"jayulabs.org/mugicrew/contract"
)

// The dispatch helpers run for real against a seeded graph: claim → publish → release →
// close, asserted on the graph's end state.
func TestClaimPublishReleaseCloseAgainstASeededGraph(t *testing.T) {
	f := bdtest.New(t,
		bd.Bead{ID: "t-build", Status: "open", Kind: contract.KindBuild},
		bdtest.Draft("t-build", bd.Bead{ID: "t-d1", Title: "One"}),
	)
	if err := bd.Claim("t-build"); err != nil {
		t.Fatal(err)
	}
	if b := f.Bead("t-build"); b.Status != "in_progress" || b.Assignee != bdtest.Actor {
		t.Fatalf("claimed = %s held by %q", b.Status, b.Assignee)
	}
	if _, err := bd.Publish("t-build", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := bd.Unclaim("t-build"); err != nil {
		t.Fatal(err)
	}
	if b := f.Bead("t-build"); b.Status != "open" || b.Assignee != "" {
		t.Fatalf("released = %s held by %q", b.Status, b.Assignee)
	}
	if err := bd.Close("t-build", "done"); err != nil {
		t.Fatal(err)
	}
	if b := f.Bead("t-build"); b.Status != "closed" || b.CloseReason != "done" {
		t.Fatalf("closed = %s %q", b.Status, b.CloseReason)
	}
}

func TestPublishOpensDraftsAndClosesTheCreatorInOneBatch(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	f := bdtest.New(t,
		bdtest.Claimed(bd.Bead{ID: "ws-spec"}),
		bdtest.Draft("ws-spec", bd.Bead{ID: "mc-a", Title: "Spec: Foo misses bar"}),
		bdtest.Draft("ws-spec", bd.Bead{ID: "mc-b", Title: "Bar"}),
		bdtest.Draft("someone-else", bd.Bead{ID: "mc-other"}),
	)
	ids, err := bd.Publish("ws-spec", []bd.Edge{{From: "mc-b", To: "mc-a", Type: "blocks"}}, &bd.Closing{ID: "ws-spec", Reason: "sliced"})
	if err != nil || !slices.Equal(ids, []string{"mc-a", "mc-b"}) {
		t.Fatalf("Publish = %v, %v", ids, err)
	}
	if a := f.Bead("mc-a"); a.Status != "open" || a.Title != "Foo misses bar" || a.HasLabel("drafted-by:ws-spec") {
		t.Errorf("mc-a = %s %q %v; want open, title stripped, label removed", a.Status, a.Title, a.Labels)
	}
	if b := f.Bead("mc-b"); b.Status != "open" || !slices.Equal(b.BlockedBy, []string{"mc-a"}) {
		t.Errorf("mc-b = %s blocked by %v", b.Status, b.BlockedBy)
	}
	if s := f.Bead("ws-spec"); s.Status != "closed" || s.CloseReason != "sliced" {
		t.Errorf("creator = %s %q; want closed: sliced", s.Status, s.CloseReason)
	}
	if o := f.Bead("mc-other"); o.Status != "deferred" {
		t.Errorf("another creator's Draft = %s; must stay deferred", o.Status)
	}
	if out := logs.String(); strings.Count(out, "title.stripped") != 1 || !strings.Contains(out, "id=mc-a") {
		t.Errorf("want one title.stripped event for mc-a, log = %q", out)
	}
}

func TestPublishNothingIsNoOp(t *testing.T) {
	f := bdtest.New(t, bd.Bead{ID: "mc-x", Status: "open"})
	if ids, err := bd.Publish("mc-x", nil, nil); err != nil || len(ids) != 0 {
		t.Fatalf("Publish = (%v,%v)", ids, err)
	}
	if calls := f.Calls(); len(calls) != 1 || calls[0] != "List" {
		t.Errorf("no Drafts, no close → only the Drafts read; calls %v", calls)
	}
}

// A failing op rolls the whole batch back; Publish reports it and leaves labels alone.
func TestPublishIsAtomic(t *testing.T) {
	f := bdtest.New(t, bdtest.Draft("mc-x", bd.Bead{ID: "mc-a"}))
	_, err := bd.Publish("mc-x", []bd.Edge{{From: "mc-a", To: "mc-gone", Type: "blocks"}}, nil)
	if err == nil {
		t.Fatal("a failed batch must fail Publish")
	}
	if a := f.Bead("mc-a"); a.Status != "deferred" || !a.HasLabel("drafted-by:mc-x") {
		t.Errorf("mc-a = %s %v; a failed batch changes nothing", a.Status, a.Labels)
	}
}

// ADR 0079 §5: a chart-conflict's escalation publishes its grilling Draft and the
// conflict blocks→grilling gate in ONE batch, with no close.
func TestEdgesOnPublishLandsDraftAndGateInOneBatch(t *testing.T) {
	f := bdtest.New(t, bd.Bead{ID: "ws-cf", Status: "open"}, bdtest.Draft("ws-cf", bd.Bead{ID: "ws-grill"}))
	if err := bd.EdgesOnPublish(nil, "ws-cf", []bd.Edge{{From: "ws-cf", To: "ws-grill", Type: "blocks"}}); err != nil {
		t.Fatal(err)
	}
	if g := f.Bead("ws-grill"); g.Status != "open" {
		t.Errorf("grilling = %s", g.Status)
	}
	if c := f.Bead("ws-cf"); c.Status != "open" || !slices.Equal(c.BlockedBy, []string{"ws-grill"}) {
		t.Errorf("conflict = %s blocked by %v; want open, blocked by the grilling", c.Status, c.BlockedBy)
	}
	if n := strings.Count(strings.Join(f.Calls(), "\n"), "Batch"); n != 1 {
		t.Errorf("batches = %d, want 1", n)
	}
}

func TestDraftsAreTheCreatorsDeferredBeads(t *testing.T) {
	bdtest.New(t,
		bdtest.Draft("c", bd.Bead{ID: "t-1"}),
		bdtest.Draft("other", bd.Bead{ID: "t-2"}),
		bd.Bead{ID: "t-3", Status: "open", Labels: []string{"drafted-by:c"}},
	)
	ds, err := bd.Drafts("c")
	if err != nil || len(ds) != 1 || ds[0].ID != "t-1" {
		t.Fatalf("Drafts = %+v, %v; want only t-1", ds, err)
	}
}

func TestPauseAddsItsLabelAndKeepsOthers(t *testing.T) {
	f := bdtest.New(t, bd.Bead{ID: "t-a", Status: "open", Labels: []string{"kind:build"}})
	for _, cause := range []string{bd.PausedManual, bd.PausedForeignHold, bd.PausedManual} {
		if err := bd.Pause("t-a", cause); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.Bead("t-a").Labels; !slices.Equal(got, []string{"kind:build", "paused:manual", "paused:foreign-hold"}) {
		t.Errorf("labels = %v", got)
	}
}

func TestCloseAndForceCloseAll(t *testing.T) {
	f := bdtest.New(t,
		bdtest.Claimed(bd.Bead{ID: "t-a", Status: "open"}),
		bd.Bead{ID: "t-b", Status: "open"},
		bd.Bead{ID: "t-c", Status: "open"},
	)
	if err := bd.Close("t-c", ""); err != nil || f.Bead("t-c").Status != "closed" {
		t.Fatalf("Close = %v, %s", err, f.Bead("t-c").Status)
	}
	if err := bd.ForceCloseAll([]string{"t-a", "t-b"}, "withdrawn: tk"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"t-a", "t-b"} {
		if b := f.Bead(id); b.Status != "closed" || b.CloseReason != "withdrawn: tk" {
			t.Errorf("%s = %s %q", id, b.Status, b.CloseReason)
		}
	}
	if a := f.Bead("t-a"); a.Assignee != bdtest.Actor {
		t.Errorf("close must keep the assignee, got %q", a.Assignee)
	}
	if err := bd.ForceCloseAll([]string{"t-a", "t-nope"}, "x"); !bd.IsNotFound(err) {
		t.Errorf("ForceCloseAll with an unknown id = %v, want not-found", err)
	}
}

// Claim's refusals carry the texts classifyClaim maps, so code branching on them behaves as on bd.
func TestClaimRefusalsUnderTheFake(t *testing.T) {
	f := bdtest.New(t,
		bd.Bead{ID: "t-open", Status: "open"},
		bdtest.Claimed(bd.Bead{ID: "t-mine", Status: "open"}),
		bd.Bead{ID: "t-theirs", Status: "in_progress", Assignee: "A"},
		bd.Bead{ID: "t-closed", Status: "closed"},
	)
	if err := bd.Claim("t-open"); err != nil {
		t.Fatal(err)
	}
	err := bd.Claim("t-mine")
	if !errors.Is(err, bd.ErrClaimedSince) {
		t.Errorf("claim of my own = %v, want ErrClaimedSince", err)
	}
	var h *bd.HeldError
	if err := bd.Claim("t-theirs"); !errors.As(err, &h) || h.Holder != "A" {
		t.Errorf("claim of another's = %v, want HeldError{A}", err)
	}
	if err := bd.Claim("t-closed"); err == nil || errors.Is(err, bd.ErrClaimedSince) || errors.As(err, &h) {
		t.Errorf("claim of a closed bead = %v, want a plain refusal", err)
	}
	if err := bd.Claim("t-nope"); !bd.IsNotFound(err) {
		t.Errorf("claim of an unknown id = %v", err)
	}
	if b := f.Bead("t-theirs"); b.Assignee != "A" {
		t.Errorf("a refused claim wrote: %+v", b)
	}
}

// Unclaim follows bd 1.3.0's check order, one row of the research trial table each.
func TestUnclaimRows(t *testing.T) {
	const me = bdtest.Actor
	for _, c := range []struct {
		name   string
		bead   bd.Bead
		guard  bd.IfAssignee
		errHas string
		want   [2]string // status, assignee after
	}{
		{"closed held by me", bd.Bead{Status: "closed", Assignee: me}, me, "cannot unclaim closed issue", [2]string{"closed", me}},
		{"closed held by another", bd.Bead{Status: "closed", Assignee: "A"}, me, "cannot unclaim closed issue", [2]string{"closed", "A"}},
		{"open held by me", bd.Bead{Status: "open", Assignee: me}, me, "", [2]string{"open", ""}},
		{"blocked held by me", bd.Bead{Status: "blocked", Assignee: me}, me, "failed to unclaim issue t-x: no matching row", [2]string{"blocked", me}},
		{"in_progress mismatch", bd.Bead{Status: "in_progress", Assignee: "A"}, me, `assignee mismatch: t-x is held by "A", expected "bdtest"`, [2]string{"in_progress", "A"}},
		{"in_progress held by me", bd.Bead{Status: "in_progress", Assignee: me}, me, "", [2]string{"open", ""}},
		{"empty guard", bd.Bead{Status: "in_progress", Assignee: me}, "", "empty", [2]string{"in_progress", me}},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.bead.ID = "t-x"
			f := bdtest.New(t, c.bead)
			err := f.Unclaim("t-x", c.guard)
			if (c.errHas == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), c.errHas)) {
				t.Fatalf("Unclaim = %v, want error containing %q", err, c.errHas)
			}
			if b := f.Bead("t-x"); b.Status != c.want[0] || b.Assignee != c.want[1] {
				t.Errorf("after: %s held by %q, want %v", b.Status, b.Assignee, c.want)
			}
		})
	}
	t.Run("unknown id", func(t *testing.T) {
		f := bdtest.New(t)
		if err := f.Unclaim("t-nope", bd.Mine()); !bd.IsNotFound(err) {
			t.Errorf("Unclaim(unknown) = %v", err)
		}
	})
	t.Run("a free function never clears another's claim", func(t *testing.T) {
		f := bdtest.New(t, bd.Bead{ID: "t-x", Status: "in_progress", Assignee: "A"})
		if err := bd.Unclaim("t-x"); err == nil {
			t.Error("bd.Unclaim of another actor's claim succeeded")
		}
		if b := f.Bead("t-x"); b.Assignee != "A" {
			t.Errorf("claim cleared: %+v", b)
		}
	})
}

// InProgress keeps bd list's default 50-row cap (the loop's busy count reads it); whether
// the cap is intended is for the loop's migration phase.
func TestInProgressTruncatesAtBdsDefaultCap(t *testing.T) {
	var beads []bd.Bead
	for i := range 60 {
		beads = append(beads, bdtest.Claimed(bd.Bead{ID: "t-" + strconv.Itoa(i)}))
	}
	bdtest.New(t, beads...)
	got, err := bd.InProgress()
	if err != nil || len(got) != 50 {
		t.Fatalf("InProgress = %d beads, %v; want 50", len(got), err)
	}
}
