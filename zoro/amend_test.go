package zoro

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

const amendText = "FR3 — use X — rejected: Y"

func TestAmendEffortRaiserCommentsSpecRefilesAndClosesLast(t *testing.T) {
	r := newSweepRig(t, "ws-chart")
	r.raisedBy()
	if err := Amend("ws-pv", "FR3 mechanism", amendText, "# Do FR3\n\nbody", "ws-lead"); err != nil {
		t.Fatal(err)
	}
	want := []string{"AddComment ws-old", "Create", "AddComment ws-er", "Close ws-pv"}
	if got := r.triageWrites(); !slices.Equal(got, want) {
		t.Errorf("writes = %v, want %v", got, want)
	}
	if got := r.notes(t, "ws-old"); !slices.Equal(got, []string{"AMENDMENT (ws-pv): " + amendText}) {
		t.Errorf("spec notes = %v", got)
	}
	if got, want := r.notes(t, "ws-er"), []string{"pivot ws-pv amended: FR3 mechanism. Wait for " + r.refiled(t).ID + "."}; !slices.Equal(got, want) {
		t.Errorf("errand notes = %v, want %v", got, want)
	}
	r.wantClosed(t, "ws-pv", "amended: FR3 mechanism")
}

func TestAmendAskRaisersGetTheCommentAndNoRefile(t *testing.T) {
	r := askGraph(t)
	if err := Amend("ws-pv", "g", amendText, "", "ws-lead"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ws-old", "ws-askS", "ws-askE"} {
		if got := r.notes(t, id); !slices.Equal(got, []string{"AMENDMENT (ws-pv): " + amendText}) {
			t.Errorf("%s notes = %v", id, got)
		}
	}
	if r.count("Create") != 0 || !r.f.Bead("ws-askS").Open() || r.f.Bead("ws-sig").CloseReason != "" {
		t.Errorf("writes %v: want no refile and no Ask or Signal close", r.triageWrites())
	}
	// ws-er waits for its own Ask only, not the Signal's
	if got := r.notes(t, "ws-er"); !slices.Equal(got, []string{"pivot ws-pv amended: g. Wait for ws-askE."}) {
		t.Errorf("errand notes = %v", got)
	}
}

func TestAmendRefusesBeforeAnyWrite(t *testing.T) {
	for name, tc := range map[string]struct {
		setup     func(*sweepRig)
		amendment string
		critique  string
		want      string
	}{
		"empty amendment":          {func(*sweepRig) {}, " ", "body", "amendment"},
		"no Ask and no --critique": {func(*sweepRig) {}, amendText, "", "--critique"},
		"Ask and --critique": {func(r *sweepRig) {
			r.ask("ws-ask", "a", "b", func(b *bd.Bead) { b.CausedBy = "ws-sig" })
		}, amendText, "body", "drop --critique"},
		"closed Checkpoint": {func(r *sweepRig) { r.f.Mutate("ws-cp", func(b *bd.Bead) { b.Status = "closed" }) }, amendText, "body", "is closed"},
		"non-pivot":         {func(r *sweepRig) { r.f.Mutate("ws-pv", func(b *bd.Bead) { b.Kind = contract.KindBuild }) }, amendText, "body", "not a pivot"},
	} {
		r := newSweepRig(t, "ws-chart")
		tc.setup(r)
		err := Amend("ws-pv", "g", tc.amendment, tc.critique, "ws-lead")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
		if w := r.triageWrites(); len(w) != 0 {
			t.Errorf("%s: wrote %v", name, w)
		}
	}
}

func TestAmendClosedPivotIsANoOp(t *testing.T) {
	r := newSweepRig(t, "ws-chart")
	r.f.Mutate("ws-pv", func(b *bd.Bead) { b.Status = "closed" })
	if err := Amend("ws-pv", "g", amendText, "body", "ws-lead"); err != nil || len(r.triageWrites()) != 0 {
		t.Errorf("err %v, wrote %v", err, r.triageWrites())
	}
}

func TestAmendRerunConvergesWithOneAmendmentAndOneClose(t *testing.T) {
	r := newSweepRig(t, "ws-chart")
	r.f.FailOn("Close", "ws-pv", errors.New("bd close: boom"))
	if err := Amend("ws-pv", "g", amendText, "body", "ws-lead"); err == nil {
		t.Fatal("first run: want the close failure")
	}
	r.f.Heal("Close", "ws-pv")
	if err := Amend("ws-pv", "g", amendText, "body", "ws-lead"); err != nil {
		t.Fatal(err)
	}
	if len(r.notes(t, "ws-old")) != 1 || r.count("Create") != 1 {
		t.Errorf("spec notes %v, writes %v: want one amendment and one refile", r.notes(t, "ws-old"), r.triageWrites())
	}
	r.wantClosed(t, "ws-pv", "amended: g")
}
