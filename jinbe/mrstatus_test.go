package jinbe

import (
	"errors"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
)

// mrStatusTick is one reconcile of a gitlab Landing whose body is body (Checked: already at
// the fetched base, so the trial writes nothing); it returns the recorder and the error.
func mrStatusTick(t *testing.T, mr forge.MR, body string, r *jinbeSeamRec) error {
	t.Helper()
	r.head, r.landErr, r.mr = "cafef00d", &git.LandPending{MR: 7}, &mr
	stubJinbeSeams(t, r)
	l := openLanding("cafef00d")
	l.Description += "\nMR: !7\nChecked: forgesha\n" + body
	return reconcileLanding(l)
}

func countStatus(bodies []string) (n int) {
	for _, b := range bodies {
		n += strings.Count(b, "MR status:")
	}
	return
}

func TestMRStatusRecordedOnceAndReplaced(t *testing.T) {
	r := &jinbeSeamRec{}
	if err := mrStatusTick(t, forge.MR{IID: 7, State: "opened", DetailedMergeStatus: "need_rebase"}, "", r); err != nil {
		t.Fatal(err)
	}
	if len(r.bodies) != 1 || !strings.Contains(r.bodies[0], "\nMR status: need_rebase\n") {
		t.Fatalf("bodies = %q, want one MR status: need_rebase", r.bodies)
	}
	same := &jinbeSeamRec{}
	mrStatusTick(t, forge.MR{IID: 7, State: "opened", DetailedMergeStatus: "need_rebase"}, "MR status: need_rebase\n", same)
	if len(same.bodies) != 0 {
		t.Errorf("unchanged status wrote %q", same.bodies)
	}
	chg := &jinbeSeamRec{}
	mrStatusTick(t, forge.MR{IID: 7, State: "opened", DetailedMergeStatus: "mergeable"}, "MR status: need_rebase\n", chg)
	if len(chg.bodies) != 1 || countStatus(chg.bodies) != 1 || !strings.Contains(chg.bodies[0], "MR status: mergeable") {
		t.Errorf("change wrote %q, want the line replaced", chg.bodies)
	}
}

func TestMRStatusTransientsAndLockedWriteNothing(t *testing.T) {
	for _, st := range []string{"unchecked", "checking", "preparing", "approvals_syncing", ""} {
		r := &jinbeSeamRec{}
		mrStatusTick(t, forge.MR{IID: 7, State: "opened", DetailedMergeStatus: st}, "MR status: need_rebase\n", r)
		if len(r.bodies) != 0 {
			t.Errorf("%q wrote %q", st, r.bodies)
		}
	}
	r := &jinbeSeamRec{}
	mrStatusTick(t, forge.MR{IID: 7, State: "locked", DetailedMergeStatus: "mergeable"}, "", r)
	if len(r.bodies) != 0 {
		t.Errorf("locked wrote %q", r.bodies)
	}
}

func TestMRStatusPendingPipelineExitsStillRecord(t *testing.T) {
	for name, c := range map[string]struct {
		pl   *forge.Pipeline
		body string
	}{
		"held":    {&forge.Pipeline{ID: 42, Status: "failed", UpdatedAt: "t1"}, "Retried: pipeline 42\nHeld: pipeline 42 t1\n"},
		"stalled": {&forge.Pipeline{ID: 42, Status: "failed", SHA: "old"}, ""},
	} {
		r := &jinbeSeamRec{}
		mr := forge.MR{IID: 7, State: "opened", SHA: "new", DetailedMergeStatus: "ci_still_running", HeadPipeline: c.pl}
		if err := mrStatusTick(t, mr, c.body, r); err != nil {
			t.Fatal(err)
		}
		if countStatus(r.bodies) != 1 {
			t.Errorf("%s: bodies %q, want one MR status:", name, r.bodies)
		}
	}
}

func TestMRStatusNotWrittenWhenTheTickClosesTheLanding(t *testing.T) {
	for name, mr := range map[string]forge.MR{
		"conflict": {IID: 7, State: "opened", HasConflicts: true, DetailedMergeStatus: "need_rebase"},
		"closed":   {IID: 7, State: "closed", DetailedMergeStatus: "need_rebase"},
	} {
		r := &jinbeSeamRec{}
		mrStatusTick(t, mr, "", r)
		if countStatus(r.bodies) != 0 {
			t.Errorf("%s: wrote %q", name, r.bodies)
		}
	}
	notesSeen.Delete("ws-land") // a first tick
	r := &jinbeSeamRec{discussions: []forge.Discussion{thread(disc1, note(11, 5, "rename this"))}}
	mrStatusTick(t, forge.MR{IID: 7, State: "opened", DetailedMergeStatus: "discussions_not_resolved"}, "", r)
	if len(r.threadSpawned) != 1 || countStatus(r.bodies) != 0 {
		t.Errorf("thread: spawned %d bodies %q", len(r.threadSpawned), r.bodies)
	}
	m := &jinbeSeamRec{trailerLanded: true}
	mrStatusTick(t, forge.MR{IID: 7, State: "merged", DetailedMergeStatus: "mergeable"}, "", m)
	if countStatus(m.bodies) != 0 {
		t.Errorf("merged: wrote %q", m.bodies)
	}
}

func TestMRStatusWriteFailureDoesNotFailTheTick(t *testing.T) {
	r := &jinbeSeamRec{}
	r.head, r.landErr, r.mr = "cafef00d", &git.LandPending{MR: 7}, &forge.MR{IID: 7, State: "opened", DetailedMergeStatus: "mergeable"}
	stubJinbeSeams(t, r)
	updateBody = func(string, []string) error { return errors.New("bd down") }
	l := openLanding("cafef00d")
	l.Description += "\nMR: !7\nChecked: forgesha\n"
	if err := reconcileLanding(l); err != nil || len(r.descAsserts) != 1 {
		t.Errorf("err %v asserts %v, want nil and the description assert run", err, r.descAsserts)
	}
}

func TestRecordedMRIgnoresMRStatus(t *testing.T) {
	if got := git.RecordedMR("Commit: x\nMR status: need_rebase\nMR: !7\n"); got != 7 {
		t.Errorf("RecordedMR = %d, want 7", got)
	}
}
