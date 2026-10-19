package withdraw

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/session"
)

func stubDrops(t *testing.T) (killed, drafted *[]string) {
	t.Helper()
	ok, od := killTmux, dropDrafts
	t.Cleanup(func() { killTmux, dropDrafts = ok, od })
	killed, drafted = &[]string{}, &[]string{}
	killTmux = func(n string) error { *killed = append(*killed, n); return nil }
	dropDrafts = func(b bd.Bead, _ ...any) error { *drafted = append(*drafted, b.ID); return nil }
	return
}

func cpWorld(t *testing.T) *world {
	w := newWorld(t)
	w.git(t, w.ws, "branch", "checkpoint/cp")
	w.git(t, w.ws, "branch", "effort/b1", "checkpoint/cp")
	return w
}

func events(evs []eventlogtest.Event, name string) []eventlogtest.Event {
	var out []eventlogtest.Event
	for _, e := range evs {
		if e.Name == name {
			out = append(out, e)
		}
	}
	return out
}

// Removable trees come down, an exited session is killed first, Drafts go, then the
// effort branch with its cut ref, and the container's branch last.
func TestCleanupRemovesTreesBranchesAndDraftsInOrder(t *testing.T) {
	w := cpWorld(t)
	killed, drafted := stubDrops(t)
	w.git(t, w.ws, "update-ref", "refs/mugicrew/cut/b1", "checkpoint/cp")
	p := w.tree(t, w.ws, "b1", "effort/b1")
	w.sessions["b1"] = session.Info{Session: "b1", Worktree: p, State: session.Exited}
	draftsOf = func(id string) ([]string, error) {
		if id == "b1" {
			return []string{"d1"}, nil
		}
		return nil, nil
	}
	evs := eventlogtest.Capture(t)
	set := []bd.Bead{build("b1"), {ID: "cp", Kind: "checkpoint", Status: "open"}}
	inv := take(t, Opts{}, set...)

	removed, kept := cleanup(inv, "cp", set)

	if len(kept) != 0 || len(removed) != 4 {
		t.Fatalf("removed %v kept %v", removed, kept)
	}
	if !slices.Equal(*killed, []string{"b1"}) || !slices.Equal(*drafted, []string{"b1"}) {
		t.Errorf("killed %v drafted %v", *killed, *drafted)
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("tree still on disk")
	}
	for _, ref := range []string{"effort/b1", "refs/mugicrew/cut/b1", "checkpoint/cp"} {
		if _, err := execGit(w.ws, "rev-parse", "--verify", "-q", ref); err == nil {
			t.Errorf("%s survived", ref)
		}
	}
	// observable order: trees, then effort branches, then the container branch
	names := slices.DeleteFunc(eventlogtest.Names(*evs), func(n string) bool { return !strings.HasPrefix(n, "tree.") && !strings.HasPrefix(n, "branch.") })
	if !slices.Equal(names, []string{"tree.removed", "branch.deleted", "branch.deleted"}) {
		t.Fatalf("events = %v", names)
	}
	e := events(*evs, "tree.removed")[0]
	if e.Attrs["bead"] != "b1" || e.Attrs["target"] != "cp" || e.Attrs["tree"] != p || e.Attrs["repo"] != "" {
		t.Errorf("tree.removed attrs = %v", e.Attrs)
	}
	bs := events(*evs, "branch.deleted")
	if bs[0].Attrs["branch"] != "effort/b1" || bs[0].Attrs["target"] != "cp" || bs[1].Attrs["branch"] != "checkpoint/cp" || bs[1].Attrs["bead"] != "cp" {
		t.Errorf("branch.deleted = %v", bs)
	}
}

// A foreign linked tree is skipped with its holder and its branch kept; an alive
// session's tree is never removed.
func TestCleanupKeepsForeignAndAliveTrees(t *testing.T) {
	w := cpWorld(t)
	stubDrops(t)
	w.git(t, w.ws, "branch", "effort/b2", "checkpoint/cp")
	foreign := filepath.Join(t.TempDir(), "mine")
	w.git(t, w.ws, "worktree", "add", "-q", foreign, "effort/b1")
	alive := w.tree(t, w.ws, "b2", "effort/b2")
	w.sessions["b2"] = session.Info{Session: "b2", Worktree: alive, State: session.Alive}
	evs := eventlogtest.Capture(t)
	set := []bd.Bead{build("b1"), build("b2")}
	inv := take(t, Opts{}, set...)

	removed, kept := cleanup(inv, "cp", set)

	if len(removed) != 0 || len(kept) != 4 {
		t.Fatalf("removed %v kept %v", removed, kept)
	}
	if _, err := os.Stat(alive); err != nil {
		t.Error("alive tree removed")
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Error("foreign tree removed")
	}
	tk := events(*evs, "tree.kept")
	if len(tk) != 2 || tk[0].Attrs["holder"] != foreign {
		t.Errorf("tree.kept = %v", tk)
	}
	bk := events(*evs, "branch.kept")
	if len(bk) != 2 || bk[0].Attrs["branch"] != "effort/b1" || bk[0].Attrs["holder"] != foreign {
		t.Errorf("branch.kept = %v", bk)
	}
}

// A failing removal is reported with its err once, later steps still run, and the
// beads stay closed.
func TestCleanupReportsAFailureAndGoesOn(t *testing.T) {
	w := cpWorld(t)
	killed, drafted := stubDrops(t)
	p := w.tree(t, w.ws, "b1", "effort/b1")
	w.sessions["b1"] = session.Info{Session: "b1", Worktree: p, State: session.Exited}
	killTmux = func(string) error { *killed = append(*killed, "x"); return os.ErrPermission }
	draftsOf = func(string) ([]string, error) { return []string{"d1"}, nil }
	evs := eventlogtest.Capture(t)
	set := []bd.Bead{build("b1")}
	inv := take(t, Opts{}, set...)

	removed, kept := cleanup(inv, "cp", set)

	if len(removed) != 1 || len(kept) != 2 || len(*killed) != 1 || len(*drafted) != 1 {
		t.Fatalf("removed %v kept %v killed %v drafted %v", removed, kept, *killed, *drafted)
	}
	if _, err := os.Stat(p); err != nil {
		t.Error("tree removed though its session would not die")
	}
	tk := events(*evs, "tree.kept")
	if len(tk) != 1 || !strings.Contains(tk[0].Attrs["err"].(string), "permission") {
		t.Errorf("tree.kept = %v", tk)
	}
	if bk := events(*evs, "branch.kept"); len(bk) != 1 || bk[0].Attrs["err"] == nil {
		t.Errorf("branch.kept = %v", bk)
	}
}

// Withdraw runs cleanup after the close and returns its outcome on the Result.
func TestWithdrawCleansUpAfterClose(t *testing.T) {
	graph(t, fixture()...)
	ok := dropDrafts
	t.Cleanup(func() { dropDrafts = ok })
	takeInventory = func([]bd.Bead, Opts) (Inventory, error) {
		return Inventory{Beads: []BeadLeftovers{{ID: "b1", Branch: "effort/b1"}}}, nil
	}
	res, err := Withdraw("cp", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Closed) == 0 || len(res.Removed)+len(res.Kept) != 1 {
		t.Errorf("res %+v", res)
	}
}

// A kept throwaway tree holds no effort branch, so it never keeps effort/<id>.
func TestCleanupKeptThrowawayDoesNotHoldEffortBranch(t *testing.T) {
	w := cpWorld(t)
	stubDrops(t)
	inv := Inventory{Beads: []BeadLeftovers{{ID: "b1", Branch: "effort/b1",
		Trees: []TreeLeftover{{Host: "/tmp/throwaway", Class: Foreign, Holder: "someone"}}}}}
	removed, kept := cleanup(inv, "cp", []bd.Bead{build("b1")})
	if len(removed) != 1 || removed[0].Kind != ItemBranch || len(kept) != 1 || kept[0].Kind != ItemTree {
		t.Errorf("removed %v kept %v", removed, kept)
	}
	if _, err := execGit(w.ws, "rev-parse", "--verify", "-q", "effort/b1"); err == nil {
		t.Error("effort/b1 survived a throwaway tree")
	}
}

// An alive tree is kept under its session id (FR 46), and a deleted Draft set is reported.
func TestCleanupNamesAliveSessionAndReportsDrafts(t *testing.T) {
	cpWorld(t)
	stubDrops(t)
	inv := Inventory{Beads: []BeadLeftovers{{ID: "b1", Drafts: []string{"d1"},
		Trees: []TreeLeftover{{Host: "/tmp/t", Class: Alive, Session: "s1"}}}}}
	removed, kept := cleanup(inv, "cp", []bd.Bead{build("b1")})
	if len(kept) != 1 || kept[0].Holder != "s1" {
		t.Errorf("kept = %+v", kept)
	}
	if len(removed) != 1 || removed[0].Kind != ItemDrafts || removed[0].Path != "d1" {
		t.Errorf("removed = %+v", removed)
	}
}
