package withdraw

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/session"
)

// The D6 attr sets: each outcome writes the rows Spec mc-5rttm D6 names.

func TestRefusedNamesTheMRAndDropsTheSuffix(t *testing.T) {
	forgeWorld(t, git.LandStrategyGitlab)
	graph(t, withLanding(landing("l1", "cp", "open", "", "MR: !42"))...)
	evs := eventlogtest.Capture(t)
	if _, err := Withdraw("cp", Opts{}); err == nil {
		t.Fatal("want a refusal")
	}
	e := (*evs)[0]
	if e.Name != "withdraw.refused" || e.Attrs["reason"] != "open Landing" || e.Attrs["mr"] != "!42" ||
		!slices.Equal(e.Attrs["blocking"].([]string), []string{"l1"}) {
		t.Errorf("event = %+v", e)
	}
}

func TestCancelRefusedReasons(t *testing.T) {
	g := append(fixture(), b("far", "signal", "", "b1"))
	graph(t, g...)
	evs := eventlogtest.Capture(t)
	if _, err := Cancel("in", Opts{}); err == nil {
		t.Fatal("want a refusal")
	}
	if e := (*evs)[0]; e.Name != "cancel.refused" || e.Attrs["reason"] != "ordered after the chain" {
		t.Errorf("event = %+v", e)
	}
	if _, has := (*evs)[0].Attrs["mr"]; has {
		t.Errorf("mr on a refusal naming none: %v", (*evs)[0].Attrs)
	}

	graph(t, fixture()...)
	stubRelease(t)
	required = func() bool { return true }
	*evs = nil
	if _, err := Cancel("in", Opts{Release: []string{"cp"}}); err == nil {
		t.Fatal("want a refusal")
	}
	if e := (*evs)[0]; e.Attrs["reason"] != "release refused: Require Initiatives" || !slices.Equal(e.Attrs["blocking"].([]string), []string{"cp"}) {
		t.Errorf("event = %+v", e)
	}
}

func TestCancelFailedAtReleaseNamesHeadAndReleased(t *testing.T) {
	graph(t, fixture()...)
	var n int
	or := releaseChain
	t.Cleanup(func() { releaseChain = or })
	required = func() bool { return false }
	releaseChain = func(h string) error {
		if n++; n == 2 {
			return errors.New("boom")
		}
		return nil
	}
	evs := eventlogtest.Capture(t)
	if _, err := Cancel("in", Opts{Release: []string{"cp", "s2"}}); err == nil {
		t.Fatal("want an error")
	}
	e := (*evs)[0]
	if e.Name != "cancel.failed" || e.Attrs["step"] != "release s2" || !slices.Equal(e.Attrs["released"].([]string), []string{"cp"}) {
		t.Errorf("event = %+v", e)
	}
}

func TestCancelFailedAtCloseNamesReleased(t *testing.T) {
	graph(t, fixture()...)
	stubRelease(t)
	forceClose = func([]string, string) error { return errors.New("boom") }
	evs := eventlogtest.Capture(t)
	if _, err := Cancel("in", Opts{Release: []string{"cp"}}); err == nil {
		t.Fatal("want an error")
	}
	if e := (*evs)[0]; !slices.Equal(e.Attrs["released"].([]string), []string{"cp"}) || e.Attrs["step"] != "close" {
		t.Errorf("event = %+v", e)
	}
}

func TestTreeRemovedCarriesSessionDirtyRebasingPrunable(t *testing.T) {
	w := cpWorld(t)
	stubDrops(t)
	p := w.tree(t, w.ws, "b1", "effort/b1")
	if err := os.WriteFile(filepath.Join(p, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.sessions["b1"] = session.Info{Session: "s1", Worktree: p, State: session.Exited}
	evs := eventlogtest.Capture(t)
	set := []bd.Bead{build("b1")}
	cleanup(take(t, Opts{}, set...), "cp", set)
	e := events(*evs, "tree.removed")[0]
	if e.Attrs["session"] != "s1" || e.Attrs["dirty"] != 1 || e.Attrs["rebasing"] != false || e.Attrs["prunable"] != false {
		t.Errorf("tree.removed = %v", e.Attrs)
	}
}

func TestTreeKeptForeignFlagged(t *testing.T) {
	cpWorld(t)
	stubDrops(t)
	inv := Inventory{Beads: []BeadLeftovers{{ID: "b1", Trees: []TreeLeftover{{Host: "/tmp/t", Class: Foreign, Holder: "someone"}}}}}
	evs := eventlogtest.Capture(t)
	cleanup(inv, "cp", []bd.Bead{build("b1")})
	if e := events(*evs, "tree.kept")[0]; e.Attrs["foreign"] != true || e.Attrs["holder"] != "someone" {
		t.Errorf("tree.kept = %v", e.Attrs)
	}
}

func TestBranchDeletedCarriesCounts(t *testing.T) {
	cpWorld(t)
	stubDrops(t)
	inv := Inventory{
		Beads:      []BeadLeftovers{{ID: "b1", Branch: "effort/b1", Unintegrated: 3, Replace: true}},
		Containers: []ContainerBranch{{ID: "cp", Branch: "checkpoint/cp", Ahead: 5}},
	}
	evs := eventlogtest.Capture(t)
	cleanup(inv, "cp", []bd.Bead{build("b1"), {ID: "cp", Kind: "checkpoint", Status: "open"}})
	bs := events(*evs, "branch.deleted")
	if len(bs) != 2 {
		t.Fatalf("events = %v", *evs)
	}
	if a := bs[0].Attrs; a["unintegrated"] != 3 || a["replace"] != true || a["ahead"] != nil {
		t.Errorf("effort branch.deleted = %v", a)
	}
	if a := bs[1].Attrs; a["ahead"] != 5 || a["unintegrated"] != nil {
		t.Errorf("container branch.deleted = %v", a)
	}
}

func TestBranchKeptAfterFailedTreeRemovalCarriesErr(t *testing.T) {
	w := cpWorld(t)
	killed, _ := stubDrops(t)
	p := w.tree(t, w.ws, "b1", "effort/b1")
	w.sessions["b1"] = session.Info{Session: "b1", Worktree: p, State: session.Exited}
	killTmux = func(string) error { *killed = append(*killed, "x"); return os.ErrPermission }
	evs := eventlogtest.Capture(t)
	set := []bd.Bead{build("b1")}
	cleanup(take(t, Opts{}, set...), "cp", set)
	e := events(*evs, "branch.kept")[0]
	if e.Attrs["err"] == nil || e.Attrs["holder"] != nil || e.Attrs["target"] != "cp" {
		t.Errorf("branch.kept = %v", e.Attrs)
	}
}

func TestDraftsDeletedViaTargetedDrop(t *testing.T) {
	cpWorld(t)
	stubDrops(t)
	var got []any
	dropDrafts = func(_ bd.Bead, extra ...any) error { got = extra; return nil }
	inv := Inventory{Beads: []BeadLeftovers{{ID: "b1", Drafts: []string{"d1"}}}}
	cleanup(inv, "cp", []bd.Bead{build("b1")})
	if !slices.Equal(got, []any{"target", "cp"}) {
		t.Errorf("extra = %v", got)
	}
}
