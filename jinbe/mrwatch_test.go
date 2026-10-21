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
	r := &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7}}
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
	if len(watches) != 1 || strings.TrimSpace(watches[0].Description) != "MR: !7" {
		t.Errorf("spawned %+v, want one mr-watch carrying MR: !7", watches)
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

func TestMRWatchClosesWhenItsMRIsMergedOrClosed(t *testing.T) {
	for _, state := range []string{"merged", "closed"} {
		r := &jinbeSeamRec{}
		if err := watchTick(t, r, state, 0); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(r.closed, []string{"ws-watch"}) {
			t.Errorf("%s: closed %v, want the Watch", state, r.closed)
		}
	}
}

func TestMRWatchClosesWithItsContainer(t *testing.T) {
	r := &jinbeSeamRec{}
	stubJinbeSeams(t, r)
	showBead = func(string) (bd.Bead, error) {
		return bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "closed"}, nil
	}
	if err := reconcileMRWatch(openWatch()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.closed, []string{"ws-watch"}) || r.mrReads != 0 {
		t.Errorf("closed %v reads %d, want the Watch closed with no MR read", r.closed, r.mrReads)
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
	want := []string{"reply " + disc1 + " 🤖 mugicrew: addressed by **Fix ws-a** (ws-a@jinbe): not needed: by design\n" + marker("ws-a", "g00d", "jinbe"), "resolve " + disc1}
	if got := slices.DeleteFunc(slices.Clone(r.sends), func(s string) bool { return !strings.HasPrefix(s, "reply ") && !strings.HasPrefix(s, "resolve ") }); !slices.Equal(got, want) {
		t.Errorf("sends %q, want %q", got, want)
	}
}
