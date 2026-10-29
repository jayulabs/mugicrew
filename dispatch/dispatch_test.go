package dispatch

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"jayulabs.org/mugicrew/liveness"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/bd/bdtest"
	"jayulabs.org/mugicrew/contexts/contextstest"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/route"
	"jayulabs.org/mugicrew/session"
)

// TestMain keeps tests that don't capture events from appending to the real repo's
// event log. The backstop's real `integrate` child would re-exec this test binary, so
// by default it only tears down; an effort cut goes through the test's
// git.NewWorktree, never a real branch. A test that runs an effort seeds the bead
// with bdtest.New (a closed bead, claimed, a Draft...); with no graph installed
// the bd CLI is live, so no test here reaches bd without one.
// chartDest is charting chart1's route: its own effort/chart1, onto chart/chart1.
var chartDest, _ = route.Route(bd.Bead{ID: "chart1", Kind: contract.KindChart})

func TestMain(m *testing.M) {
	eventlogtest.Discard()
	git.SetGraphID("testgraphroot") // Compose, Anchor and revert read the Graph id; never ask a live bd
	backstop = func(_ bd.Bead, _ string, _ bool, teardown func(), _ io.Writer, _ ...any) (bool, bool) {
		teardown()
		return false, false
	}
	git.NewEffortWorktree = func(id, _, _ string) (string, func(), error) { return git.NewWorktree(git.EffortBranch(id)) }
	sessions = func() (map[string]session.Info, error) { return nil, nil }
	os.Exit(m.Run())
}

// graph seeds a bdtest graph with beads in the given status (the old TestMain default
// re-read saw a closed bead, so a test that only runs an effort seeds "closed").
func graph(t *testing.T, status string, beads ...bd.Bead) *bdtest.Fake {
	t.Helper()
	for i := range beads {
		beads[i].Status = status
	}
	return bdtest.New(t, beads...)
}

// hold takes id's dispatcher hold for the test, as the loop, run-now and the run
// verb do before they hand the bead to Dispatch (ADR 0154).
func hold(t *testing.T, id string) *liveness.Hold {
	t.Helper()
	t.Setenv("BEADS_DIR", t.TempDir())
	h, err := liveness.Acquire(id)
	if err != nil {
		t.Fatalf("Acquire(%s): %v", id, err)
	}
	t.Cleanup(h.Release)
	return h
}

// A failed effort releases the caller's claim, guarded by a re-read (ADR 0034),
// on every failure exit: only a bead still in_progress is unclaimed, with one
// claim.released carrying the effort id after effort.failed; a closed or already
// open bead is left alone; a failed re-read records claim.failed and releases
// nothing. Dispatch returns the effort's own error throughout. The guard state
// lands at the semantic moment the effort fails.
func TestFailedEffortReleasesClaimGuarded(t *testing.T) {
	origNW, origRe := git.NewWorktree, reconcileInWorktree
	defer func() { git.NewWorktree, reconcileInWorktree = origNW, origRe }()

	boom := errors.New("boom")
	exits := []string{"route", "worktree cut", "reconcile", "crew.Run"}
	beads := map[string]bd.Bead{
		"route":        {ID: "r1", Kind: contract.KindResearch},
		"worktree cut": {ID: "chart1", Kind: contract.KindChart},
		"reconcile":    {ID: "chart1", Kind: contract.KindChart},
		"crew.Run":     {ID: "sp1", Kind: contract.KindSpec},
	}
	guards := []struct {
		status  string
		down    bool
		release bool
		claimEv string // trailing claim event, "" for none
	}{
		{"in_progress", false, true, "claim.released"},
		{"closed", false, false, ""},
		{"open", false, false, ""},
		{"in_progress", true, false, "claim.failed"},
	}
	for _, name := range exits {
		for _, g := range guards {
			t.Run(fmt.Sprintf("%s/%s/down=%v", name, g.status, g.down), func(t *testing.T) {
				evs := eventlogtest.Capture(t)
				b := beads[name]
				f := bdtest.New(t, bdtest.Claimed(b))
				hold(t, b.ID)
				// land puts the graph in the guard's state: the moment the effort fails.
				land := func() {
					f.Mutate(b.ID, func(x *bd.Bead) {
						x.Status = g.status
						if g.status != "in_progress" {
							x.Assignee = ""
						}
					})
					if g.down {
						f.FailOn("Show", b.ID, errors.New("bd down"))
						f.FailOn("List", "*", errors.New("bd down"))
					}
				}
				var crew contract.Crewmate = &fakeCrew{}
				git.NewWorktree = func(string) (string, func(), error) { return "/wt", func() {}, nil }
				reconcileInWorktree = func(_, _, _ string, _ io.Writer) error { land(); return nil }
				switch name {
				case "route":
					land()
				case "worktree cut":
					git.NewWorktree = func(string) (string, func(), error) { land(); return "", nil, boom }
				case "reconcile":
					reconcileInWorktree = func(_, _, _ string, _ io.Writer) error { land(); return boom }
				case "crew.Run":
					crew = hookCrew{land, boom}
				}

				_, err := Dispatch(nil, crew, b, io.Discard, NewEffortID(), nil)
				if err == nil || err.Error() == "bd down" {
					t.Fatalf("Dispatch err = %v, want the effort's own error", err)
				}
				wantStatus := g.status
				if g.release {
					wantStatus = "open"
				}
				if got := f.Bead(b.ID).Status; got != wantStatus {
					t.Errorf("status = %s, want %s", got, wantStatus)
				}
				want := []string{"effort.started", "effort.failed"}
				if g.down { // the Draft read fails too: the delete is recorded as failed
					want = append(want, "draft.failed")
				}
				if g.claimEv != "" {
					want = append(want, g.claimEv)
				}
				if got := eventlogtest.Names(*evs); !slices.Equal(got, want) {
					t.Fatalf("events = %v, want %v", got, want)
				}
				if last := len(want) - 1; g.claimEv != "" && (*evs)[last].Attrs["effort"] != (*evs)[0].Attrs["effort"] {
					t.Errorf("%s must carry the effort id, got %v", g.claimEv, (*evs)[last].Attrs)
				}
			})
		}
	}
}

// hookCrew runs fn, then fails with err.
type hookCrew struct {
	fn  func()
	err error
}

func (h hookCrew) Run(contract.Ctx, contract.Bead) error { h.fn(); return h.err }
func (hookCrew) Launch(contract.Ctx, contract.Bead) (contract.LaunchCommand, error) {
	return nil, nil
}

// A successful effort releases a claim its handler left in_progress (a non-terminal
// success, e.g. Jinbe filing a land conflict), with the same re-read guard as
// a failure: a closed or open bead is left alone.
func TestSucceededEffortReleasesStrandedClaim(t *testing.T) {
	for status, want := range map[string]string{"in_progress": "open", "closed": "closed", "open": "open"} {
		seed := bd.Bead{ID: "sp1", Kind: contract.KindSpec, Status: status}
		if status == "in_progress" {
			seed = bdtest.Claimed(seed)
		}
		f := bdtest.New(t, seed)
		if _, err := Dispatch(nil, &fakeCrew{}, bd.Bead{ID: "sp1", Kind: contract.KindSpec}, io.Discard, NewEffortID(), nil); err != nil {
			t.Fatalf("Dispatch: %v", err)
		}
		if got := f.Bead("sp1").Status; got != want {
			t.Errorf("%s: status after dispatch = %s, want %s", status, got, want)
		}
	}
}

type fakeCrew struct{ ran []string }

func (f *fakeCrew) Run(_ contract.Ctx, b contract.Bead) error {
	f.ran = append(f.ran, b.ID)
	return nil
}
func (f *fakeCrew) Launch(contract.Ctx, contract.Bead) (contract.LaunchCommand, error) {
	return nil, nil
}

// Dispatch routes each dispatched bead into the git tree its kind demands
// (route.Route): charting and an AFK research leaf each into a
// worktree of their own effort/<id> (integrated onto the Chart branch, Spec
// D1), and a repo-root leaf in-process on the
// caller's own tree. The worktreeRun var seam captures which branch was checked
// out AND the io.Writer the caller threaded down (the loop passes
// os.Stdout, the TUI a buffer — the extracted seam must honor it, not os.Stdout).
func TestRunEffortRoutesPerKind(t *testing.T) {
	graph(t, "closed", bd.Bead{ID: "chart1"}, bd.Bead{ID: "res1"}, bd.Bead{ID: "sp1"})
	type cut struct {
		id, branch string
		out        io.Writer
	}
	var isolated []cut
	orig := worktreeRun
	worktreeRun = func(b bd.Bead, d route.Destination, e *effort, out io.Writer) (bool, error) {
		isolated = append(isolated, cut{b.ID, d.Branch, out})
		e.started()
		return false, nil
	}
	defer func() { worktreeRun = orig }()

	crew := &fakeCrew{}
	buf := &bytes.Buffer{}

	// charting → worktree of its own effort branch (Spec D1).
	if _, err := Dispatch(nil, crew, bd.Bead{ID: "chart1", Kind: contract.KindChart}, buf, NewEffortID(), nil); err != nil {
		t.Fatalf("Dispatch chart: %v", err)
	}
	// research leaf → worktree of its own effort branch, too.
	if _, err := Dispatch(nil, crew, bd.Bead{ID: "res1", Kind: contract.KindResearch, Parent: "chart1"}, buf, NewEffortID(), nil); err != nil {
		t.Fatalf("Dispatch research: %v", err)
	}
	want := []cut{{"chart1", "effort/chart1", buf}, {"res1", "effort/res1", buf}}
	if len(isolated) != len(want) || isolated[0] != want[0] || isolated[1] != want[1] {
		t.Errorf("per-kind worktree routing = %v, want %v", isolated, want)
	}
	if len(crew.ran) != 0 {
		t.Errorf("worktree efforts must NOT run in-process on the shared tree; crew.ran=%v", crew.ran)
	}

	// a repo-root leaf (spec slicing) → in-process, no worktree.
	if _, err := Dispatch(nil, crew, bd.Bead{ID: "sp1", Kind: contract.KindSpec}, buf, NewEffortID(), nil); err != nil {
		t.Fatalf("Dispatch repo-root leaf: %v", err)
	}
	if len(crew.ran) != 1 || crew.ran[0] != "sp1" {
		t.Errorf("a repo-root leaf must run in-process; crew.ran=%v", crew.ran)
	}
	if len(isolated) != 2 {
		t.Errorf("a repo-root leaf must NOT allocate a worktree; isolated=%v", isolated)
	}
}

// worktreeRun's contention interlock: when git refuses the checkout
// (exit 128 — the branch is already used by another worktree), the effort skips
// (returns nil, logs "busy, retry next poll") instead of crashing, never
// reconciles a tree it never got, and releases the caller's claim so the next
// poll can actually retry (else the bead strands in_progress, dead). A non-128 add error still surfaces.
// Faked via git.NewWorktree with a real exit-128 error and a scripted listed holder
// (ADR 0056) — no live worktree race.
func TestWorktreeRunSkipsOnBusy(t *testing.T) {
	origNW, origRe, origH := git.NewWorktree, reconcileInWorktree, git.WorktreeOn
	defer func() { git.NewWorktree, reconcileInWorktree, git.WorktreeOn = origNW, origRe, origH }()
	git.WorktreeOn = heldBy("/tmp/mugicrew-wt-1/wt")

	f := bdtest.New(t, bdtest.Claimed(bd.Bead{ID: "chart1", Kind: contract.KindChart}))
	ran := false
	reconcileInWorktree = func(_, _, _ string, _ io.Writer) error { ran = true; return nil }
	b := bd.Bead{ID: "chart1", Kind: contract.KindChart}

	git.NewWorktree = func(string) (string, func(), error) {
		return "", nil, exec.Command("sh", "-c", "exit 128").Run()
	}
	if busy, err := worktreeRun(b, chartDest, newEffort(b, NewEffortID()), io.Discard); !busy || err != nil {
		t.Errorf("busy (exit 128) must skip as busy with nil, got busy=%v err=%v", busy, err)
	}
	if ran {
		t.Error("a busy effort must not reconcile a tree it never got")
	}
	if got := f.Bead("chart1"); got.Status != "open" || got.Assignee != "" {
		t.Errorf("a busy effort must release its claim, got %s held by %q", got.Status, got.Assignee)
	}

	git.NewWorktree = func(string) (string, func(), error) {
		return "", nil, exec.Command("sh", "-c", "exit 1").Run()
	}
	if busy, err := worktreeRun(b, chartDest, newEffort(b, NewEffortID()), io.Discard); busy || err == nil {
		t.Errorf("a non-128 add error must surface, not skip: busy=%v err=%v", busy, err)
	}
}

// A land step (Chart and Checkpoint land, route.Detached) runs in a tree cut
// detached at its container's tip, never a checkout of the container (Spec
// D1/D2, ADR 0102 §8); charting still checks its Chart branch out.
func TestLandStepCutsDetachedTree(t *testing.T) {
	graph(t, "closed", bd.Bead{ID: "cp1"}, bd.Bead{ID: "c1"}, bd.Bead{ID: "c2"})
	origNW, origND, origRe := git.NewWorktree, git.NewDetachedWorktree, reconcileInWorktree
	defer func() { git.NewWorktree, git.NewDetachedWorktree, reconcileInWorktree = origNW, origND, origRe }()
	var cuts []string
	git.NewWorktree = func(b string) (string, func(), error) {
		cuts = append(cuts, "checkout "+b)
		return "/wt", func() {}, nil
	}
	git.NewDetachedWorktree = func(r string) (string, func(), error) { cuts = append(cuts, "detach "+r); return "/wt", func() {}, nil }
	reconcileInWorktree = func(_, _, _ string, _ io.Writer) error { return nil }

	for _, b := range []bd.Bead{
		{ID: "cp1", Kind: contract.KindCheckpoint},
		{ID: "c1", Kind: contract.KindChart, Labels: []string{contract.LabelDefogged}},
		{ID: "c2", Kind: contract.KindChart},
	} {
		d, _ := route.Route(b)
		if _, err := worktreeRun(b, d, newEffort(b, NewEffortID()), io.Discard); err != nil {
			t.Fatalf("worktreeRun %s: %v", b.ID, err)
		}
	}
	if want := []string{"detach checkpoint/cp1", "detach chart/c1", "checkout effort/c2"}; !slices.Equal(cuts, want) {
		t.Errorf("cuts = %q, want %q", cuts, want)
	}
}

// The io.Writer the caller hands worktreeRun is threaded through to the effort's
// combined output, not swallowed to os.Stdout (FR20): the loop passes
// os.Stdout, the TUI a buffer it renders. Asserted at the worktreeRun→reconcile
// seam so the whole path from Dispatch's arg down to the subprocess is covered.
func TestWorktreeRunHonorsWriter(t *testing.T) {
	graph(t, "closed", bd.Bead{ID: "chart1"})
	origNW, origRe := git.NewWorktree, reconcileInWorktree
	defer func() { git.NewWorktree, reconcileInWorktree = origNW, origRe }()

	git.NewWorktree = func(string) (string, func(), error) { return "/wt", func() {}, nil }
	reconcileInWorktree = func(_, _, _ string, out io.Writer) error {
		io.WriteString(out, "effort output")
		return nil
	}

	buf := &bytes.Buffer{}
	if _, err := worktreeRun(bd.Bead{ID: "chart1", Kind: contract.KindChart}, chartDest, newEffort(bd.Bead{}, NewEffortID()), buf); err != nil {
		t.Fatalf("worktreeRun: %v", err)
	}
	if buf.String() != "effort output" {
		t.Errorf("worktreeRun must thread the caller's writer to the effort; got %q", buf.String())
	}
}

// A successful worktree effort records started then ended, sharing one minted
// effort id, with dur_ms and the child's exit 0; the same id reaches the child
// (ADR 0029 §2-5).
func TestWorktreeEffortStartedThenEnded(t *testing.T) {
	graph(t, "closed", bd.Bead{ID: "chart1"})
	evs := eventlogtest.Capture(t)
	origNW, origRe := git.NewWorktree, reconcileInWorktree
	defer func() { git.NewWorktree, reconcileInWorktree = origNW, origRe }()
	git.NewWorktree = func(string) (string, func(), error) { return "/wt", func() {}, nil }
	var childEffort string
	reconcileInWorktree = func(_, _, effortID string, _ io.Writer) error { childEffort = effortID; return nil }

	b := bd.Bead{ID: "chart1", Kind: contract.KindChart, Parent: "p1"}
	if _, err := Dispatch(nil, &fakeCrew{}, b, io.Discard, NewEffortID(), nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(eventlogtest.Names(*evs), []string{"effort.started", "effort.ended"}) {
		t.Fatalf("events = %v", eventlogtest.Names(*evs))
	}
	st, end := (*evs)[0], (*evs)[1]
	id := st.Attrs["effort"]
	if id == "" || id != end.Attrs["effort"] || id != childEffort {
		t.Errorf("effort id must be shared: started=%v ended=%v child=%q", id, end.Attrs["effort"], childEffort)
	}
	for _, k := range []string{"bead", "parent", "crewmate"} {
		if st.Attrs[k] == nil || st.Attrs[k] == "" {
			t.Errorf("started missing common attr %s: %v", k, st.Attrs)
		}
	}
	if _, ok := end.Attrs["dur_ms"]; !ok || end.Attrs["exit"] != 0 || end.Level != slog.LevelInfo {
		t.Errorf("ended = %+v, want Info with dur_ms and exit 0", end)
	}
}

// A failing child records effort.failed at Error with err, exit and dur_ms.
func TestWorktreeEffortFailed(t *testing.T) {
	graph(t, "closed", bd.Bead{ID: "chart1"})
	evs := eventlogtest.Capture(t)
	origNW, origRe := git.NewWorktree, reconcileInWorktree
	defer func() { git.NewWorktree, reconcileInWorktree = origNW, origRe }()
	git.NewWorktree = func(string) (string, func(), error) { return "/wt", func() {}, nil }
	reconcileInWorktree = func(_, _, _ string, _ io.Writer) error {
		return exec.Command("sh", "-c", "exit 3").Run()
	}

	if _, err := Dispatch(nil, &fakeCrew{}, bd.Bead{ID: "chart1", Kind: contract.KindChart}, io.Discard, NewEffortID(), nil); err == nil {
		t.Fatal("want the child's failure surfaced")
	}
	if !slices.Equal(eventlogtest.Names(*evs), []string{"effort.started", "effort.failed"}) {
		t.Fatalf("events = %v", eventlogtest.Names(*evs))
	}
	f := (*evs)[1]
	if f.Level != slog.LevelError || f.Attrs["exit"] != 3 || f.Attrs["err"] != "exit status 3" || f.Attrs["dur_ms"] == nil {
		t.Errorf("failed = %+v", f)
	}
	if f.Attrs["effort"] != (*evs)[0].Attrs["effort"] {
		t.Error("failed must share the started effort id")
	}
}

// A worktree cut failure spawned no reconcile child: git's own exit (wrapped by
// git.run) must not ride effort.failed as the child's exit (FR15).
func TestWorktreeCutFailedHasNoChildExit(t *testing.T) {
	graph(t, "closed", bd.Bead{ID: "chart1"})
	evs := eventlogtest.Capture(t)
	origNW, origRe := git.NewWorktree, reconcileInWorktree
	defer func() { git.NewWorktree, reconcileInWorktree = origNW, origRe }()
	git.NewWorktree = func(string) (string, func(), error) {
		return "", nil, fmt.Errorf("git worktree add: %w: fatal", exec.Command("sh", "-c", "exit 1").Run())
	}
	reconcileInWorktree = func(_, _, _ string, _ io.Writer) error { t.Error("no child on a cut failure"); return nil }

	o, err := Dispatch(nil, &fakeCrew{}, bd.Bead{ID: "chart1", Kind: contract.KindChart}, io.Discard, NewEffortID(), nil)
	if err == nil {
		t.Fatal("want the cut failure surfaced")
	}
	if o.Cut != CutFailed {
		t.Errorf("outcome = %+v, want CutFailed", o)
	}
	if !slices.Equal(eventlogtest.Names(*evs), []string{"effort.started", "effort.failed"}) {
		t.Fatalf("events = %v", eventlogtest.Names(*evs))
	}
	if f := (*evs)[1]; f.Attrs["err"] == nil || f.Attrs["exit"] != nil || f.Attrs["signal"] != nil {
		t.Errorf("failed = %+v, want err but no exit/signal", f)
	}
}

// The in-process route records the same lifecycle; its error chain rides as err,
// and with no child there is no exit to report.
func TestInProcessEffortFailed(t *testing.T) {
	graph(t, "closed", bd.Bead{ID: "sp1"})
	evs := eventlogtest.Capture(t)
	crew := errCrew{errors.New("reconcile: bd close: boom")}
	if _, err := Dispatch(nil, crew, bd.Bead{ID: "sp1", Kind: contract.KindSpec}, io.Discard, NewEffortID(), nil); err == nil {
		t.Fatal("want the Run error surfaced")
	}
	if !slices.Equal(eventlogtest.Names(*evs), []string{"effort.started", "effort.failed"}) {
		t.Fatalf("events = %v", eventlogtest.Names(*evs))
	}
	if f := (*evs)[1]; f.Attrs["err"] != "reconcile: bd close: boom" || f.Attrs["exit"] != nil {
		t.Errorf("failed = %+v", f)
	}
}

// noWrites fails if the graph saw a publish or a Draft delete: a refused cut ran
// nothing, so it has nothing to publish or delete.
func noWrites(t *testing.T, f *bdtest.Fake, what string) {
	t.Helper()
	for _, c := range f.Calls() {
		if strings.HasPrefix(c, "Batch") || strings.HasPrefix(c, "Delete") {
			t.Errorf("%s must not publish or delete Drafts, saw %q", what, c)
		}
	}
}

// A busy skip ran nothing: no started is left dangling, only the claim's release
// and a Debug effort.busy naming the branch (in the file only at MUGICREW_LOG_LEVEL=debug).
func TestBusyEffortLeavesNoStarted(t *testing.T) {
	evs := eventlogtest.Capture(t)
	origNE, origH := git.NewEffortWorktree, git.WorktreeOn
	defer func() { git.NewEffortWorktree, git.WorktreeOn = origNE, origH }()
	git.WorktreeOn = heldBy("/tmp/mugicrew-wt-1/wt")
	f := bdtest.New(t, bdtest.Claimed(bd.Bead{ID: "cc1", Kind: contract.KindChartConflict, Parent: "chart1"}))
	git.NewEffortWorktree = func(string, string, string) (string, func(), error) {
		return "", nil, exec.Command("sh", "-c", "exit 128").Run()
	}
	cut.Store("/tmp/mugicrew-wt-1/wt", true) // an effort in this process cut it
	defer cut.Delete("/tmp/mugicrew-wt-1/wt")
	Dispatch(nil, &fakeCrew{}, bd.Bead{ID: "cc1", Kind: contract.KindChartConflict, Parent: "chart1"}, io.Discard, NewEffortID(), nil)
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"claim.released", "effort.busy"}) {
		t.Fatalf("events = %v, want claim.released then effort.busy", got)
	}
	if rel := (*evs)[0]; rel.Attrs["effort"] == nil || rel.Attrs["effort"] != (*evs)[1].Attrs["effort"] {
		t.Errorf("claim.released must carry the busy effort's id, got %v", rel.Attrs)
	}
	if busy := (*evs)[1]; busy.Level != slog.LevelDebug || busy.Attrs["branch"] != "effort/cc1" {
		t.Errorf("effort.busy = %+v, want Debug with branch effort/cc1", busy)
	}
	noWrites(t, f, "a busy cut")
}

// A refused cut's listed holder is own contention (effort.busy, Debug) only when it
// is temp-shaped, not prunable, and an in-flight effort's tree (this process's cut,
// or a live effort on the branch in another) or an alive session accounts for it; every other holder is a foreign hold (effort.held, Warn, naming holder and
// prunable) (ADR 0054). Both release the claim and record no effort.started, and
// nothing is removed or pruned (no git seam but the scripted lookup is reached).
func TestRefusedCutClassifiesHolder(t *testing.T) {
	origNE, origH, origS := git.NewEffortWorktree, git.WorktreeOn, sessions
	defer func() { git.NewEffortWorktree, git.WorktreeOn, sessions = origNE, origH, origS }()
	git.NewEffortWorktree = func(string, string, string) (string, func(), error) {
		return "", nil, exec.Command("sh", "-c", "exit 128").Run()
	}
	const tmp = "/tmp/mugicrew-wt-9/wt"
	self := bd.Bead{ID: "cc1", Kind: contract.KindChartConflict, Parent: "chart1"}
	sibling := bd.Bead{ID: "cc2", Kind: contract.KindChartConflict, Parent: "chart1"} // its own effort/cc2, same container
	elsewhere := bd.Bead{ID: "chart2", Kind: contract.KindChart}
	cases := []struct {
		name     string
		holder   git.Holder
		inflight []bd.Bead
		sess     map[string]session.Info
		want     string
	}{
		{"foreign linked worktree", git.Holder{Path: "/home/u/oe-chart1"}, []bd.Bead{sibling}, nil, "effort.held"},
		{"main worktree", git.Holder{Path: "/home/u/oe"}, nil, nil, "effort.held"},
		{"orphan temp tree", git.Holder{Path: tmp}, []bd.Bead{self, elsewhere}, nil, "effort.held"},
		{"exited session tree", git.Holder{Path: tmp}, nil, map[string]session.Info{"g1": {Worktree: tmp, State: session.Exited}}, "effort.held"},
		{"prunable", git.Holder{Path: tmp, Prunable: true}, []bd.Bead{sibling}, nil, "effort.held"},
		{"own in-flight effort, this process", git.Holder{Path: "/tmp/mugicrew-wt-cut/wt"}, nil, nil, "effort.busy"},
		{"sibling's live effort, other process", git.Holder{Path: tmp}, []bd.Bead{self, sibling}, nil, "effort.held"},
		{"own alive session", git.Holder{Path: tmp}, nil, map[string]session.Info{"g1": {Worktree: tmp, State: session.Alive}}, "effort.busy"},
	}
	cut.Store("/tmp/mugicrew-wt-cut/wt", true) // an effort in this process cut it
	defer cut.Delete("/tmp/mugicrew-wt-cut/wt")
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			evs := eventlogtest.Capture(t)
			git.WorktreeOn = func(string) (git.Holder, bool, error) { return c.holder, true, nil }
			sessions = func() (map[string]session.Info, error) { return c.sess, nil }
			f := bdtest.New(t, bdtest.Claimed(self))
			inflight := func() ([]bd.Bead, error) { return c.inflight, nil }

			o, err := Dispatch(nil, &fakeCrew{}, self, io.Discard, NewEffortID(), inflight)
			if err != nil {
				t.Fatalf("a refused cut is a skip, got err %v", err)
			}
			want := Outcome{Cut: CutHeld, Branch: "effort/cc1", Holder: c.holder}
			if c.want == "effort.busy" {
				want.Cut = CutBusy
			}
			if o != want {
				t.Errorf("outcome = %+v, want %+v", o, want)
			}
			if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"claim.released", c.want}) {
				t.Fatalf("events = %v, want claim.released then %s (no effort.started)", got, c.want)
			}
			if got := f.Bead("cc1"); got.Status != "open" || got.Assignee != "" {
				t.Errorf("cc1 = %s held by %q, want the claim released", got.Status, got.Assignee)
			}
			noWrites(t, f, "a refused cut")
			ev := (*evs)[1]
			if ev.Attrs["branch"] != "effort/cc1" || ev.Attrs["effort"] != (*evs)[0].Attrs["effort"] {
				t.Errorf("%s = %v, want branch effort/cc1 and the effort id", c.want, ev.Attrs)
			}
			if c.want == "effort.busy" {
				if ev.Level != slog.LevelDebug || ev.Attrs["holder"] != nil {
					t.Errorf("effort.busy = %+v, want Debug with {branch} only", ev)
				}
				return
			}
			if ev.Level != slog.LevelWarn || ev.Attrs["holder"] != c.holder.Path || ev.Attrs["prunable"] != c.holder.Prunable {
				t.Errorf("effort.held = %+v, want Warn holder=%q prunable=%v", ev, c.holder.Path, c.holder.Prunable)
			}
		})
	}
}

// A cut tree is this process's own while its effort runs, and not after: a later
// refusal by it (a torn-down-but-listed leftover) is no longer vouched for.
func TestCutTreeIsOwnOnlyWhileRunning(t *testing.T) {
	graph(t, "closed", bd.Bead{ID: "chart1"})
	origNW, origRe := git.NewWorktree, reconcileInWorktree
	defer func() { git.NewWorktree, reconcileInWorktree = origNW, origRe }()
	const wt = "/tmp/mugicrew-wt-run/wt"
	git.NewWorktree = func(string) (string, func(), error) { return wt, func() {}, nil }
	during := false
	reconcileInWorktree = func(string, string, string, io.Writer) error { _, during = cut.Load(wt); return nil }
	b := bd.Bead{ID: "chart1", Kind: contract.KindChart}
	worktreeRun(b, chartDest, newEffort(b, NewEffortID()), io.Discard)
	if _, after := cut.Load(wt); !during || after {
		t.Errorf("cut tree registered during=%v after=%v, want true then false", during, after)
	}
}

// A failed read of the in-flight set or the session table leaves a temp-shaped
// holder unexplained: not provably ours, so it surfaces as a foreign hold (ADR 0054).
func TestRefusedCutUnreadableOwnersAreHeld(t *testing.T) {
	origNW, origH, origS := git.NewWorktree, git.WorktreeOn, sessions
	defer func() { git.NewWorktree, git.WorktreeOn, sessions = origNW, origH, origS }()
	git.NewWorktree = func(string) (string, func(), error) {
		return "", nil, exec.Command("sh", "-c", "exit 128").Run()
	}
	git.WorktreeOn = heldBy("/tmp/mugicrew-wt-9/wt")
	f := bdtest.New(t)
	down := errors.New("down")
	for name, c := range map[string]struct{ inflightErr, sessErr error }{"inflight": {down, nil}, "sessions": {nil, down}} {
		evs := eventlogtest.Capture(t)
		f.Seed(bdtest.Claimed(bd.Bead{ID: "chart1", Kind: contract.KindChart}))
		sessions = func() (map[string]session.Info, error) { return nil, c.sessErr }
		Dispatch(nil, &fakeCrew{}, bd.Bead{ID: "chart1", Kind: contract.KindChart}, io.Discard, NewEffortID(),
			func() ([]bd.Bead, error) { return nil, c.inflightErr })
		if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"claim.released", "effort.held"}) {
			t.Errorf("%s read failed: events = %v, want claim.released then effort.held", name, got)
		}
	}
}

// A cut that succeeds reports CutOK, whatever the effort did after: the loop's
// hold count resets on it (ADR 0055 §4).
func TestCutOutcomeOK(t *testing.T) {
	graph(t, "closed", bd.Bead{ID: "chart1"})
	origNW, origRe := git.NewWorktree, reconcileInWorktree
	defer func() { git.NewWorktree, reconcileInWorktree = origNW, origRe }()
	git.NewWorktree = func(string) (string, func(), error) { return "/wt", func() {}, nil }
	reconcileInWorktree = func(string, string, string, io.Writer) error { return errors.New("boom") }
	if o, _ := Dispatch(nil, &fakeCrew{}, bd.Bead{ID: "chart1", Kind: contract.KindChart}, io.Discard, NewEffortID(), nil); o.Cut != CutOK || o.Branch != "effort/chart1" {
		t.Errorf("outcome = %+v, want CutOK on effort/chart1", o)
	}
}

// ForeignHeld, the auto-clear's check (ADR 0055 §5): a free branch or own
// contention is not foreign; any other listed holder is; a failed list errs.
func TestForeignHeld(t *testing.T) {
	origH, origS := git.WorktreeOn, sessions
	defer func() { git.WorktreeOn, sessions = origH, origS }()
	const tmp = "/tmp/mugicrew-wt-9/wt"
	sessions = func() (map[string]session.Info, error) {
		return map[string]session.Info{"g1": {Worktree: tmp, State: session.Alive}}, nil
	}
	b := bd.Bead{ID: "chart1", Kind: contract.KindChart}
	for _, c := range []struct {
		holder  string
		foreign bool
	}{{"", false}, {tmp, false}, {"/home/u/oe-chart1", true}} {
		git.WorktreeOn = heldBy(c.holder)
		if br, f, err := ForeignHeld(b, nil); err != nil || br != "effort/chart1" || f != c.foreign {
			t.Errorf("holder %q: ForeignHeld = %q, %v, %v; want effort/chart1, %v", c.holder, br, f, err, c.foreign)
		}
	}
	git.WorktreeOn = func(string) (git.Holder, bool, error) { return git.Holder{}, false, errors.New("down") }
	if _, _, err := ForeignHeld(b, nil); err == nil {
		t.Error("a failed worktree list must err, not read free")
	}
}

// Foreign hold is scoped to effort/<id> (ADR 0106, Spec FR 52): a human's
// hand checkout of the container a Build integrates onto pauses nothing; the same
// checkout of the Build's own effort branch does.
func TestForeignHeldIgnoresContainerCheckout(t *testing.T) {
	origH := git.WorktreeOn
	defer func() { git.WorktreeOn = origH }()
	b := bd.Bead{ID: "b1", Kind: contract.KindBuild, Parent: "mc-k"}
	for _, held := range []string{"checkpoint/mc-k", "effort/b1"} {
		git.WorktreeOn = func(br string) (git.Holder, bool, error) {
			return git.Holder{Path: "/home/u/oe-hand"}, br == held, nil
		}
		if br, f, err := ForeignHeld(b, nil); err != nil || br != "effort/b1" || f != (held == "effort/b1") {
			t.Errorf("hand checkout of %s: ForeignHeld = %q, %v, %v", held, br, f, err)
		}
	}
}

// heldBy scripts git.WorktreeOn: every branch is held by path ("" holds none).
func heldBy(path string) func(string) (git.Holder, bool, error) {
	return func(string) (git.Holder, bool, error) { return git.Holder{Path: path}, path != "", nil }
}

// An exit-128 cut with no listed holder is not busy (ADR 0056): the effort starts
// and fails at Error carrying git's stderr, its claim released (ADR 0034), with no
// effort.busy — so a real cut failure is loud instead of skipped forever.
func TestHolderlessExit128IsFailedEffort(t *testing.T) {
	evs := eventlogtest.Capture(t)
	origNW, origH := git.NewWorktree, git.WorktreeOn
	defer func() { git.NewWorktree, git.WorktreeOn = origNW, origH }()
	git.WorktreeOn = heldBy("")
	f := bdtest.New(t, bdtest.Claimed(bd.Bead{ID: "chart1", Kind: contract.KindChart}))
	const stderr = "fatal: invalid reference: chart/chart1"
	git.NewWorktree = func(string) (string, func(), error) {
		return "", nil, fmt.Errorf("git worktree add: %w: %s", exec.Command("sh", "-c", "exit 128").Run(), stderr)
	}

	o, err := Dispatch(nil, &fakeCrew{}, bd.Bead{ID: "chart1", Kind: contract.KindChart}, io.Discard, NewEffortID(), nil)
	if err == nil {
		t.Fatal("want the cut failure surfaced")
	}
	if o.Cut != CutFailed {
		t.Errorf("outcome = %+v, want CutFailed", o)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"effort.started", "effort.failed", "claim.released"}) {
		t.Fatalf("events = %v, want started, failed, claim.released (no effort.busy)", got)
	}
	if f := (*evs)[1]; f.Level != slog.LevelError || !strings.Contains(fmt.Sprint(f.Attrs["err"]), stderr) {
		t.Errorf("effort.failed = %+v, want Error carrying git's stderr", f)
	}
	if got := f.Bead("chart1"); got.Status != "open" {
		t.Errorf("chart1 = %s, want the claim released", got.Status)
	}
}

// The worktree child's env carries MUGICREW_EFFORT=<id> and its intent file, and it runs in its own
// process group so a terminal Ctrl-C reaches only the loop (ADR 0050).
func TestReconcileCmdCarriesEffortEnv(t *testing.T) {
	cmd := reconcileCmd("/bin/mugicrew", "/wt", "b1", "abcd1234", io.Discard)
	if !slices.Contains(cmd.Env, "MUGICREW_EFFORT=abcd1234") || !slices.Contains(cmd.Env, PublishEnv+"="+intentPath("abcd1234")) || cmd.Dir != "/wt" {
		t.Errorf("cmd env/dir = %v / %q", cmd.Env, cmd.Dir)
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Errorf("SysProcAttr = %+v; want Setpgid", cmd.SysProcAttr)
	}
}

type errCrew struct{ err error }

func (c errCrew) Run(contract.Ctx, contract.Bead) error { return c.err }
func (errCrew) Launch(contract.Ctx, contract.Bead) (contract.LaunchCommand, error) {
	return nil, nil
}

// harnessCtx/stubHarness: a crewmate's session runs against a canned Result.
type harnessCtx struct{ h contract.Harness }

func (c harnessCtx) Harness() contract.Harness { return c.h }

type stubHarness struct {
	res contract.Result
	err error
	inv contract.Invocation
}

func (h *stubHarness) Run(_ contract.Ctx, inv contract.Invocation) (contract.Result, error) {
	h.inv = inv
	return h.res, h.err
}
func (h *stubHarness) LaunchCommand(contract.Invocation) (contract.LaunchCommand, error) {
	return nil, nil
}

func (h *stubHarness) ResumeCommand(string, bool) (contract.AgentCommand, error) {
	return contract.AgentCommand{}, nil
}

// sessionCrew runs one session through ctx.Harness() and returns its error.
type sessionCrew struct{}

func (sessionCrew) Run(ctx contract.Ctx, b contract.Bead) error {
	_, err := ctx.Harness().Run(ctx, contract.Invocation{Prompt: "p", BeadID: b.ID})
	return err
}
func (sessionCrew) Launch(contract.Ctx, contract.Bead) (contract.LaunchCommand, error) {
	return nil, nil
}

// A failing session inside an effort records session.started then session.failed
// with the full payload, both carrying the surrounding effort's id (ADR 0029 §4).
func TestSessionFailedSharesEffortID(t *testing.T) {
	graph(t, "closed", bd.Bead{ID: "sp1"})
	evs := eventlogtest.Capture(t)
	h := &stubHarness{
		res: contract.Result{Exit: 1, Stderr: "[claude-code:unrecognized_model]\n",
			Summary: contract.Summary{IsError: true, TerminalReason: "api_error", Errors: []string{"bad model"}}},
		err: errors.New("claude -p: exit status 1"),
	}
	b := bd.Bead{ID: "sp1", Kind: contract.KindSpec}
	if _, err := Dispatch(harnessCtx{h}, sessionCrew{}, b, io.Discard, NewEffortID(), nil); err == nil {
		t.Fatal("want the session's failure surfaced")
	}
	if !slices.Equal(eventlogtest.Names(*evs), []string{"effort.started", "session.started", "session.failed", "effort.failed"}) {
		t.Fatalf("events = %v", eventlogtest.Names(*evs))
	}
	eff, st, f := (*evs)[0], (*evs)[1], (*evs)[2]
	if st.Attrs["effort"] != eff.Attrs["effort"] || f.Attrs["effort"] != eff.Attrs["effort"] {
		t.Errorf("session events must share the effort id %v: %v / %v", eff.Attrs["effort"], st.Attrs["effort"], f.Attrs["effort"])
	}
	if st.Attrs["session"] == "" || st.Attrs["session"] != h.inv.SessionID || f.Attrs["session"] != h.inv.SessionID {
		t.Errorf("the pre-assigned session id must be on started (%v), passed to the harness (%q) and on failed (%v)",
			st.Attrs["session"], h.inv.SessionID, f.Attrs["session"])
	}
	if f.Level != slog.LevelError || f.Attrs["exit"] != 1 || f.Attrs["dur_ms"] == nil ||
		f.Attrs["stderr"] != "[claude-code:unrecognized_model]\n" || f.Attrs["err"] != "claude -p: exit status 1" || f.Attrs["bead"] != "sp1" {
		t.Errorf("failed = %+v", f)
	}
	if r, ok := f.Attrs["result"].(contract.Summary); !ok || r.TerminalReason != "api_error" || !r.IsError {
		t.Errorf("failed result group = %#v", f.Attrs["result"])
	}
}

// A soft failure — claude exits 0 but reports is_error — is still session.failed,
// with exit 0 and the result group saying why.
func TestSessionSoftFailure(t *testing.T) {
	graph(t, "closed", bd.Bead{ID: "sp1"})
	evs := eventlogtest.Capture(t)
	h := &stubHarness{
		res: contract.Result{Summary: contract.Summary{IsError: true, TerminalReason: "max_turns"}},
		err: errors.New("claude -p: is_error (max_turns): "),
	}
	Dispatch(harnessCtx{h}, sessionCrew{}, bd.Bead{ID: "sp1", Kind: contract.KindSpec}, io.Discard, NewEffortID(), nil)
	if !slices.Equal(eventlogtest.Names(*evs), []string{"effort.started", "session.started", "session.failed", "effort.failed"}) {
		t.Fatalf("events = %v", eventlogtest.Names(*evs))
	}
	f := (*evs)[2]
	if r, ok := f.Attrs["result"].(contract.Summary); f.Level != slog.LevelError || f.Attrs["exit"] != 0 || !ok || !r.IsError {
		t.Errorf("failed = %+v", f)
	}
}

// profileCtx is a Profiled Ctx: each kind's effort runs on its own harness and
// profile name, as main's ctx assembly resolves them (ADR 0125).
type profileCtx struct {
	harnessCtx
	profiles map[contract.Kind]string
	h        contract.Harness
}

func (c profileCtx) ForKind(k contract.Kind) (contract.Ctx, string, error) {
	return harnessCtx{c.h}, c.profiles[k], nil
}

// An effort runs on the harness its bead's profile assembles, and every session
// event names a named profile; the built-in ("") leaves the attr absent. Both the
// in-process route and the reconcile child's wrap assemble alike.
func TestSessionEventsNameTheProfile(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile string
		run     func(contract.Ctx, bd.Bead) error
	}{
		{"dispatch, named", "deepseek", func(ctx contract.Ctx, b bd.Bead) error {
			_, err := Dispatch(ctx, sessionCrew{}, b, io.Discard, NewEffortID(), nil)
			return err
		}},
		{"dispatch, built-in", "", func(ctx contract.Ctx, b bd.Bead) error {
			_, err := Dispatch(ctx, sessionCrew{}, b, io.Discard, NewEffortID(), nil)
			return err
		}},
		{"reconcile, named", "opus", func(ctx contract.Ctx, b bd.Bead) error { return Reconcile(ctx, sessionCrew{}, b, "") }},
		{"reconcile, built-in", "", func(ctx contract.Ctx, b bd.Bead) error { return Reconcile(ctx, sessionCrew{}, b, "") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evs := eventlogtest.Capture(t)
			graph(t, "closed", bd.Bead{ID: "sp1"})
			loopH, profH := &stubHarness{}, &stubHarness{}
			ctx := profileCtx{harnessCtx{loopH}, map[contract.Kind]string{contract.KindSpec: tc.profile}, profH}
			if err := tc.run(ctx, bd.Bead{ID: "sp1", Kind: contract.KindSpec}); err != nil {
				t.Fatal(err)
			}
			if profH.inv.BeadID != "sp1" || loopH.inv.BeadID != "" {
				t.Errorf("the session must run on the profile's harness, not the caller's")
			}
			for _, e := range *evs {
				if !strings.HasPrefix(e.Name, "session.") {
					continue
				}
				if p, ok := e.Attrs["profile"]; (tc.profile != "") != ok || (ok && p != tc.profile) {
					t.Errorf("%s profile attr = %v (present %v), want %q", e.Name, p, ok, tc.profile)
				}
			}
		})
	}
}

// A successful session records session.ended at Info.
func TestSessionEnded(t *testing.T) {
	graph(t, "closed", bd.Bead{ID: "sp1"})
	evs := eventlogtest.Capture(t)
	h := &stubHarness{res: contract.Result{Output: "ok"}}
	if _, err := Dispatch(harnessCtx{h}, sessionCrew{}, bd.Bead{ID: "sp1", Kind: contract.KindSpec}, io.Discard, NewEffortID(), nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(eventlogtest.Names(*evs), []string{"effort.started", "session.started", "session.ended", "effort.ended"}) {
		t.Fatalf("events = %v", eventlogtest.Names(*evs))
	}
	if e := (*evs)[2]; e.Level != slog.LevelInfo || e.Attrs["exit"] != 0 || e.Attrs["err"] != nil {
		t.Errorf("ended = %+v", e)
	}
}

// A known Session cost rides top-level (not in the result group) on session.ended
// and session.failed; an unknown one leaves every cost attr absent, not zero.
func TestSessionCostAttrs(t *testing.T) {
	models := map[string]contract.ModelCost{"claude-haiku-4-5-20251001": {USD: 0.5, In: 1, Out: 2, CacheRead: 3, CacheWrite: 4}}
	known := contract.Cost{Known: true, USD: 0.5, APIms: 1434, Models: models}
	for _, tc := range []struct {
		name string
		res  contract.Result
		err  error
		want string
	}{
		{"ended", contract.Result{Cost: known}, nil, "session.ended"},
		{"failed with a result", contract.Result{Exit: 1, Summary: contract.Summary{IsError: true, Subtype: "error_max_turns"},
			Cost: contract.Cost{Known: true, USD: 0.5, APIms: 1434, Models: models, Unpriced: true}}, errors.New("exit status 1"), "session.failed"},
		{"ended, unknown", contract.Result{}, nil, "session.ended"},
		{"failed, unknown", contract.Result{Exit: -1, Signal: "killed"}, errors.New("signal: killed"), "session.failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evs := eventlogtest.Capture(t)
			ctx, _ := Sessions(harnessCtx{&stubHarness{res: tc.res, err: tc.err}}, bd.Bead{ID: "sp1", Kind: contract.KindSpec})
			ctx.Harness().Run(ctx, contract.Invocation{Prompt: "p"})
			e := (*evs)[1]
			if e.Name != tc.want || e.Attrs["exit"] != tc.res.Exit || e.Attrs["dur_ms"] == nil {
				t.Fatalf("event = %+v", e)
			}
			if !tc.res.Cost.Known {
				for _, k := range []string{"cost_usd", "api_ms", "models", "cost_unpriced"} {
					if _, ok := e.Attrs[k]; ok {
						t.Errorf("unknown cost must leave %s absent, got %v", k, e.Attrs[k])
					}
				}
				return
			}
			if e.Attrs["cost_usd"] != 0.5 || e.Attrs["api_ms"] != 1434 || !reflect.DeepEqual(e.Attrs["models"], models) {
				t.Errorf("cost attrs = %v", e.Attrs)
			}
			if u, ok := e.Attrs["cost_unpriced"]; tc.res.Cost.Unpriced != ok || (ok && u != true) {
				t.Errorf("cost_unpriced = %v (present %v), want present only when unpriced", u, ok)
			}
		})
	}
}

// Sessions (the reconcile child's wrap) stamps the bead's common attrs; the effort
// id there comes from MUGICREW_EFFORT via the emitter, so none is set here.
func TestSessionsWrapStampsBeadAttrs(t *testing.T) {
	evs := eventlogtest.Capture(t)
	h := &stubHarness{}
	ctx, _ := Sessions(harnessCtx{h}, bd.Bead{ID: "chart1", Kind: contract.KindChart, Parent: "p1"})
	ctx.Harness().Run(ctx, contract.Invocation{Prompt: "p"})
	if !slices.Equal(eventlogtest.Names(*evs), []string{"session.started", "session.ended"}) {
		t.Fatalf("events = %v", eventlogtest.Names(*evs))
	}
	if a := (*evs)[0].Attrs; a["bead"] != "chart1" || a["parent"] != "p1" || a["crewmate"] != "nami" || a["effort"] != nil {
		t.Errorf("started attrs = %v", a)
	}
}

// Claim records claim.taken by the owning crewmate, and a bd failure as
// claim.failed at Error with err.
func TestClaimOpsAreEvents(t *testing.T) {
	t.Setenv("BEADS_DIR", t.TempDir())
	evs := eventlogtest.Capture(t)
	b := bd.Bead{ID: "mc-r", Kind: contract.KindResearch, Parent: "mc-c", Status: "open"}
	f := bdtest.New(t, b)
	h, _ := Claim(b)
	h.Release()
	if got := f.Bead(b.ID); got.Status != "in_progress" || got.Assignee != bdtest.Actor {
		t.Errorf("claimed bead = %s held by %q, want in_progress by the actor", got.Status, got.Assignee)
	}
	bdErr := errors.New("bd down")
	f.FailOn("Claim", b.ID, bdErr)
	if _, err := Claim(b); err != bdErr {
		t.Errorf("Claim err = %v, want the bd error", err)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"claim.taken", "claim.failed"}) {
		t.Fatalf("events = %v", got)
	}
	for _, e := range *evs {
		if e.Attrs["by"] != "nami" || e.Attrs["bead"] != "mc-r" {
			t.Errorf("%s attrs = %v, want by=nami bead=mc-r", e.Name, e.Attrs)
		}
	}
	if f := (*evs)[1]; f.Level != slog.LevelError || f.Attrs["err"] != "bd down" {
		t.Errorf("claim.failed = %+v", f)
	}
}

// The reconcile verb records reconcile.done, or reconcile.failed at Error, with the
// bead's routed branch.
func TestReconcileEvents(t *testing.T) {
	evs := eventlogtest.Capture(t)
	b := bd.Bead{ID: "chart1", Kind: contract.KindChart}
	Reconcile(nil, errCrew{}, b, "")
	if err := Reconcile(nil, errCrew{errors.New("boom")}, b, ""); err == nil {
		t.Error("Reconcile must return the crewmate's error")
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"reconcile.done", "reconcile.failed"}) {
		t.Fatalf("events = %v", got)
	}
	for _, e := range *evs {
		if e.Attrs["branch"] != "effort/chart1" {
			t.Errorf("%s branch = %v", e.Name, e.Attrs["branch"])
		}
	}
	if f := (*evs)[1]; f.Level != slog.LevelError || f.Attrs["err"] != "boom" {
		t.Errorf("reconcile.failed = %+v", f)
	}
}

// An unroutable bead (a leaf missing its parent edge) is a failed effort, so the
// loop, which no longer logs a Run error, loses nothing.
func TestUnroutableIsFailedEffort(t *testing.T) {
	graph(t, "closed", bd.Bead{ID: "r1"})
	evs := eventlogtest.Capture(t)
	if _, err := Dispatch(nil, &fakeCrew{}, bd.Bead{ID: "r1", Kind: contract.KindResearch}, io.Discard, NewEffortID(), nil); err == nil {
		t.Fatal("an unroutable bead must error")
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"effort.started", "effort.failed"}) {
		t.Errorf("events = %v", got)
	}
}

// handCrew hands its finishing close and a final edge to the publish (Spec D6).
type handCrew struct{ err error }

func (c handCrew) Run(ctx contract.Ctx, b contract.Bead) error {
	p, ok := ctx.(contract.Publisher)
	if !ok {
		return errors.New("no Publisher on the effort's Ctx")
	}
	p.EdgeOnPublish(contract.Edge{From: "d1", To: b.ID, Type: "discovered-from"})
	p.CloseOnPublish("done")
	return c.err
}
func (handCrew) Launch(contract.Ctx, contract.Bead) (contract.LaunchCommand, error) {
	return nil, nil
}

// draftGraph seeds b as the claimed bead with one Draft of its own, d1, and takes
// b's dispatcher hold, as the caller of Dispatch does.
func draftGraph(t *testing.T, b bd.Bead, kind contract.Kind) *bdtest.Fake {
	t.Helper()
	f := bdtest.New(t, bdtest.Claimed(b), bdtest.Draft(b.ID, bd.Bead{ID: "d1", Kind: kind, Title: "d1"}))
	hold(t, b.ID)
	return f
}

// index is the position of the first call with prefix in calls, -1 for none.
func index(calls []string, prefix string) int {
	return slices.IndexFunc(calls, func(c string) bool { return strings.HasPrefix(c, prefix) })
}

// workedDraftBranch cuts a scratch repo whose chart/d1 branch carries work, so
// deleting the Draft d1 keeps its branch.
func workedDraftBranch(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, args := range [][]string{
		{"init", "-b", "main"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "test"},
		{"commit", "--allow-empty", "-m", "root"}, {"checkout", "-b", "chart/d1"},
		{"commit", "--allow-empty", "-m", "work"}, {"checkout", "main"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

// childReconcile stubs the worktree child with the real reconcile verb under the
// env reconcileCmd gives it, so the handed close crosses the process boundary
// through the intent file.
func childReconcile(t *testing.T, crew contract.Crewmate, b bd.Bead) {
	origNW, origRe := git.NewWorktree, reconcileInWorktree
	t.Cleanup(func() { git.NewWorktree, reconcileInWorktree = origNW, origRe })
	git.NewWorktree = func(string) (string, func(), error) { return "/wt", func() {}, nil }
	reconcileInWorktree = func(_, _, effortID string, _ io.Writer) error {
		return Reconcile(nil, crew, b, intentPath(effortID))
	}
}

// A successful effort publishes its Drafts, scoped to the dispatched bead, with
// the close and edges its Go flow handed over — on both the worktree and in-process
// paths — recording draft.published with creator and ids. The publish closes the
// bead itself, so the release then finds nothing to unclaim.
func TestSuccessfulEffortPublishesBeforeRelease(t *testing.T) {
	for _, b := range []bd.Bead{{ID: "chart1", Kind: contract.KindChart}, {ID: "sp1", Kind: contract.KindSpec}} {
		t.Run(string(b.Kind), func(t *testing.T) {
			evs := eventlogtest.Capture(t)
			f := draftGraph(t, b, contract.KindBuild)
			childReconcile(t, handCrew{}, b)
			if _, err := Dispatch(nil, handCrew{}, b, io.Discard, NewEffortID(), nil); err != nil {
				t.Fatal(err)
			}
			if d := f.Bead("d1"); d.Status != "open" || slices.Contains(d.Labels, "drafted-by:"+b.ID) {
				t.Errorf("d1 = %s %v, want published: open, unlabelled", d.Status, d.Labels)
			}
			if got := f.Bead(b.ID); got.Status != "closed" || got.CloseReason != "done" {
				t.Errorf("%s = %s %q, want closed done by the handed close", b.ID, got.Status, got.CloseReason)
			}
			if got := f.Bead("d1").DiscoveredFrom; !slices.Equal(got, []string{b.ID}) {
				t.Errorf("d1 discovered-from %v, want the handed edge to %s", got, b.ID)
			}
			if i := index(f.Calls(), "Unclaim"); i >= 0 {
				t.Errorf("calls = %v, a closed bead has no claim to release", f.Calls())
			}
			i := slices.Index(eventlogtest.Names(*evs), "draft.published")
			if i < 0 {
				t.Fatalf("events = %v, want draft.published", eventlogtest.Names(*evs))
			}
			if p := (*evs)[i]; p.Attrs["creator"] != b.ID || fmt.Sprint(p.Attrs["ids"]) != "[d1]" || p.Attrs["effort"] == nil {
				t.Errorf("draft.published = %v", p.Attrs)
			}
		})
	}
}

// A failed effort, or a failed publish, deletes the bead's Drafts and then
// releases, recording draft.deleted with creator and ids, then each Draft branch
// outcome.
func TestFailedEffortDeletesDraftsThenReleases(t *testing.T) {
	boom := errors.New("boom")
	for _, c := range []struct {
		name          string
		crew          contract.Crewmate
		pubErr        error
		publishCalled bool
	}{
		{"effort", handCrew{err: boom}, nil, false},
		{"publish", handCrew{}, boom, true},
	} {
		for _, b := range []bd.Bead{{ID: "chart1", Kind: contract.KindChart}, {ID: "sp1", Kind: contract.KindSpec}} {
			t.Run(c.name+"/"+string(b.Kind), func(t *testing.T) {
				evs := eventlogtest.Capture(t)
				workedDraftBranch(t)
				f := draftGraph(t, b, contract.KindChart)
				if c.pubErr != nil {
					f.FailOn("Batch", "*", c.pubErr)
				}
				childReconcile(t, c.crew, b)
				if _, err := Dispatch(nil, c.crew, b, io.Discard, NewEffortID(), nil); !errors.Is(err, boom) {
					t.Fatalf("Dispatch err = %v, want boom", err)
				}
				calls := f.Calls()
				del, un := index(calls, "Delete d1"), index(calls, "Unclaim "+b.ID)
				if del < 0 || un < del {
					t.Errorf("calls = %q, want the Draft delete then the release", calls)
				}
				if got := index(calls, "Batch") >= 0; got != c.publishCalled {
					t.Errorf("publish attempted = %v, want %v", got, c.publishCalled)
				}
				if got := f.Bead(b.ID); got.Status != "open" || got.Assignee != "" {
					t.Errorf("%s = %s held by %q, want released", b.ID, got.Status, got.Assignee)
				}
				names := eventlogtest.Names(*evs)
				i := slices.Index(names, "draft.deleted")
				if i < 0 || i < slices.Index(names, "effort.failed") {
					t.Fatalf("events = %v, want draft.deleted after effort.failed", names)
				}
				if d := (*evs)[i]; d.Attrs["creator"] != b.ID || fmt.Sprint(d.Attrs["ids"]) != "[d1]" {
					t.Errorf("draft.deleted = %v", d.Attrs)
				}
				if k := (*evs)[i+1]; k.Name != "branch.kept" || k.Attrs["branch"] != "chart/d1" || k.Attrs["err"] != "has work" || k.Attrs["effort"] == nil {
					t.Errorf("after draft.deleted = %+v, want branch.kept", k)
				}
				if c.publishCalled && !slices.Contains(names, "draft.failed") {
					t.Errorf("events = %v, want the failed publish recorded", names)
				}
				if f := (*evs)[slices.Index(names, "effort.failed")]; c.publishCalled && f.Attrs["exit"] != nil {
					t.Errorf("effort.failed = %v, a failed publish has no child exit", f.Attrs)
				}
			})
		}
	}
}

// The fail path deletes the Drafts after the handler closed its own bead, so the
// delete runs under the effort's dispatcher hold (ADR 0154): with the hold taken
// the Drafts go, and with it released first the bdtest graph fails the test and
// deletes nothing.
func TestFailPathDeleteNamesTheLiveDispatcherHold(t *testing.T) {
	b := bd.Bead{ID: "sp1", Kind: contract.KindSpec}
	crew := &closeThenFail{}
	t.Run("held", func(t *testing.T) {
		f := draftGraph(t, b, contract.KindSpec)
		crew.f = f
		if _, err := Dispatch(nil, crew, b, io.Discard, NewEffortID(), nil); err == nil {
			t.Fatal("want the effort's error")
		}
		if index(f.Calls(), "Delete d1") < 0 {
			t.Errorf("calls = %q, want the Draft delete under the hold", f.Calls())
		}
	})
	t.Run("released first", func(t *testing.T) {
		rec := &holdRecorder{TB: t}
		f := bdtest.New(rec, bdtest.Claimed(b), bdtest.Draft(b.ID, bd.Bead{ID: "d1", Kind: contract.KindSpec, Title: "d1"}))
		hold(t, b.ID).Release()
		crew.f = f
		Dispatch(nil, crew, b, io.Discard, NewEffortID(), nil)
		if len(rec.errs) != 1 || !strings.Contains(rec.errs[0], "ADR 0070 violation") || !strings.Contains(rec.errs[0], "hold") {
			t.Errorf("violations = %q, want one ADR 0070 violation naming the released hold", rec.errs)
		}
		if f.Bead("d1").ID == "" {
			t.Error("the refused delete must leave the Draft")
		}
	})
}

// closeThenFail is a crewmate whose handler closes its own bead, then fails.
type closeThenFail struct{ f *bdtest.Fake }

func (c *closeThenFail) Run(_ contract.Ctx, b contract.Bead) error {
	c.f.Mutate(b.ID, func(x *bd.Bead) { x.Status, x.Assignee = "closed", "" })
	return errors.New("boom")
}
func (*closeThenFail) Launch(contract.Ctx, contract.Bead) (contract.LaunchCommand, error) {
	return nil, nil
}

// holdRecorder is a testing.TB that records Errorf instead of failing the test.
type holdRecorder struct {
	testing.TB
	errs []string
}

func (r *holdRecorder) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

// Outside a dispatch child (no intent path) the reconcile verb's Ctx has no
// Publisher, so the flow closes its own bead.
func TestReconcileOutsideEffortHasNoPublisher(t *testing.T) {
	if err := Reconcile(nil, handCrew{}, bd.Bead{ID: "chart1", Kind: contract.KindChart}, ""); err == nil {
		t.Error("want no Publisher outside a dispatch child")
	}
	if err := Reconcile(nil, handCrew{}, bd.Bead{ID: "chart1", Kind: contract.KindChart}, filepath.Join(t.TempDir(), "intent.json")); err != nil {
		t.Fatal(err)
	}
}

// landedRepo is a scratch Workspace where chart/c1 has landed from a detached tree
// (Land leaves the branch at the tip it pinned) and a prototype ref points at it;
// the effort's worktree and child are stubbed, so Dispatch runs only its
// post-publish git.
func landedRepo(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	g := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	g("init", "-b", "main")
	g("config", "user.email", "test@example.com")
	g("config", "user.name", "test")
	g("commit", "--allow-empty", "-m", "root")
	g("branch", "chart/c1")
	g("update-ref", "refs/mugicrew/prototypes/c1", "chart/c1")
	g("commit", "--allow-empty", "-m", "main moves on") // the squash is not the container tip
	root, _ := os.Getwd()
	wt, teardown, err := git.NewDetachedWorktree("chart/c1")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(wt)
	if _, err := git.Land("chart/c1", git.TrailerFor(string(contract.KindChart), "c1"), git.LandLocal); err != nil {
		t.Fatal(err)
	}
	teardown() // an effort's tree is gone by now
	t.Chdir(root)
	origNW, origRe := git.NewWorktree, reconcileInWorktree
	t.Cleanup(func() { git.NewWorktree, reconcileInWorktree = origNW, origRe })
	git.NewWorktree = func(string) (string, func(), error) { return "/wt", func() {}, nil }
	reconcileInWorktree = func(_, _, _ string, _ io.Writer) error { return nil }
}

func refExists(ref string) bool {
	return exec.Command("git", "rev-parse", "--verify", "--quiet", ref).Run() == nil
}

// A Landing effort deletes nothing: its container is still open, so chart/c1 stays
// until the container's own closing effort deletes it (FR26 as amended).
func TestLandingEffortKeepsOpenContainerBranch(t *testing.T) {
	landedRepo(t)
	origKind := route.ParentKind
	defer func() { route.ParentKind = origKind }()
	route.ParentKind = func(string) (contract.Kind, error) { return contract.KindChart, nil }
	bdtest.New(t, bd.Bead{ID: "c1", Kind: contract.KindChart, Status: "open"}, bd.Bead{ID: "l1", Kind: contract.KindLanding, Status: "closed"})
	l := bd.Bead{ID: "l1", Kind: contract.KindLanding, Parent: "c1"}
	if _, err := Dispatch(nil, &fakeCrew{}, l, io.Discard, NewEffortID(), nil); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !refExists("refs/heads/chart/c1") {
		t.Error("chart/c1 must survive its Landing effort while the container is open")
	}
}

// After a successful effort, a landed and closed container's own branch is
// deleted (Spec FR30–32); every unmet gate keeps it, a refused delete
// never fails the effort, and the prototype ref always survives.
func TestLandedContainerBranchDeleted(t *testing.T) {
	chart := bd.Bead{ID: "c1", Kind: contract.KindChart, Labels: []string{contract.LabelDefogged}} // only a defogged Chart lands
	cases := []struct {
		name    string
		b       bd.Bead
		status  string
		setup   func(t *testing.T)
		deleted bool
	}{
		{"landed and closed", chart, "closed", nil, true},
		{"leaf on parent's branch", bd.Bead{ID: "r1", Kind: contract.KindResearch, Parent: "c1"}, "closed", nil, false},
		{"open bead", chart, "open", nil, false},
		{"tip past land", chart, "closed", func(t *testing.T) {
			out, _ := exec.Command("git", "commit-tree", "-m", "more", "-p", "chart/c1", "chart/c1^{tree}").Output()
			if err := exec.Command("git", "update-ref", "refs/heads/chart/c1", strings.TrimSpace(string(out))).Run(); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"held by another worktree", chart, "closed", func(t *testing.T) {
			if err := exec.Command("git", "worktree", "add", filepath.Join(t.TempDir(), "wt"), "chart/c1").Run(); err != nil {
				t.Fatal(err)
			}
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			landedRepo(t)
			if c.setup != nil {
				c.setup(t)
			}
			graph(t, c.status, bd.Bead{ID: c.b.ID}, bd.Bead{ID: "c1"})
			evs := eventlogtest.Capture(t)
			if _, err := Dispatch(nil, &fakeCrew{}, c.b, io.Discard, NewEffortID(), nil); err != nil {
				t.Fatalf("Dispatch: %v", err)
			}
			// A held container's refused delete Warns, and the land still succeeds (ADR 0106 §3).
			if i := slices.Index(eventlogtest.Names(*evs), "branch.kept"); (i >= 0) != (c.name == "held by another worktree") || (i >= 0 && (*evs)[i].Level != slog.LevelWarn) {
				t.Errorf("events = %v, want branch.kept at Warn only when held", eventlogtest.Names(*evs))
			}
			if got := !refExists("refs/heads/chart/c1"); got != c.deleted {
				t.Errorf("chart/c1 deleted = %v, want %v", got, c.deleted)
			}
			if !refExists("refs/mugicrew/prototypes/c1") {
				t.Error("the prototype ref must survive (ADR 0009 §2)")
			}
		})
	}
}

// A Build or Critique runs in a tree on its own effort/<id>, cut (or reused) at its
// Checkpoint, and the integrate backstop owns that tree after the reconcile (Spec
// D2, D6): it is told whether the effort failed, its stranded verdict
// reaches the Outcome. A chart-conflict's effort is cut at its pinned Tip.
func TestEffortRouteCutsEffortTreeAndRunsBackstop(t *testing.T) {
	origNW, origNE, origRe, origBs := git.NewWorktree, git.NewEffortWorktree, reconcileInWorktree, backstop
	defer func() {
		git.NewWorktree, git.NewEffortWorktree, reconcileInWorktree, backstop = origNW, origNE, origRe, origBs
	}()
	boom := errors.New("boom")
	cases := []struct {
		name     string
		b        bd.Bead
		re       error
		stranded bool
		cut      string
		backstop string // "" for none
	}{
		{"build integrated", bd.Bead{ID: "b1", Kind: contract.KindBuild, Parent: "cp"}, nil, false, "effort b1@checkpoint/cp", "b1 failed=false"},
		{"critique stranded", bd.Bead{ID: "k1", Kind: contract.KindCritique, Parent: "cp"}, nil, true, "effort k1@checkpoint/cp", "k1 failed=false"},
		{"build failed", bd.Bead{ID: "b1", Kind: contract.KindBuild, Parent: "cp"}, boom, false, "effort b1@checkpoint/cp", "b1 failed=true"},
		{"charting", bd.Bead{ID: "c1", Kind: contract.KindChart}, nil, false, "effort c1@chart/c1", "c1 failed=false"},
		{"research", bd.Bead{ID: "r1", Kind: contract.KindResearch, Parent: "c1"}, nil, false, "effort r1@chart/c1", "r1 failed=false"},
		{"chart-conflict", bd.Bead{ID: "cc1", Kind: contract.KindChartConflict, Parent: "c1", Description: "Tip: t1p"}, nil, false, "effort cc1@t1p", "cc1 failed=false"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			parents := []bd.Bead{}
			if c.b.Parent != "" {
				parents = append(parents, bd.Bead{ID: c.b.Parent})
			}
			graph(t, "closed", append(parents, bd.Bead{ID: c.b.ID, Parent: c.b.Parent})...)
			var cut, ran string
			torn := false
			git.NewWorktree = func(b string) (string, func(), error) {
				cut = "checkout " + b
				return "/wt", func() { torn = true }, nil
			}
			git.NewEffortWorktree = func(id, container, pin string) (string, func(), error) {
				cut = "effort " + id + "@" + cmp.Or(pin, container)
				return "/wt", func() { torn = true }, nil
			}
			reconcileInWorktree = func(_, _, _ string, _ io.Writer) error { return c.re }
			backstop = func(b bd.Bead, wt string, failed bool, teardown func(), _ io.Writer, _ ...any) (bool, bool) {
				ran = fmt.Sprintf("%s failed=%v", b.ID, failed)
				return c.stranded, c.stranded
			}
			o, _ := Dispatch(nil, &fakeCrew{}, c.b, io.Discard, NewEffortID(), nil)
			if cut != c.cut || ran != c.backstop {
				t.Errorf("cut %q, backstop %q; want %q, %q", cut, ran, c.cut, c.backstop)
			}
			if o.Stranded != c.stranded {
				t.Errorf("Outcome.Stranded = %v, want %v", o.Stranded, c.stranded)
			}
			if wantTorn := c.backstop == ""; torn != wantTorn {
				t.Errorf("worktreeRun tore the tree down = %v, want %v (the backstop owns an effort tree)", torn, wantTorn)
			}
		})
	}
}

// A backstop that keeps effort/<id> (a transient integrate, or a conflict) keeps
// its bead open, so the loop re-dispatches it and the roll-forward integrate
// retries (Spec FR 18): a close the flow handed over (a pivoting Build's
// pivoted:) is dropped from the publish, while its Drafts and edges still publish,
// and the claim is released back to bd ready.
func TestKeptEffortBranchKeepsBeadOpen(t *testing.T) {
	origNE, origRe, origBs := git.NewEffortWorktree, reconcileInWorktree, backstop
	defer func() { git.NewEffortWorktree, reconcileInWorktree, backstop = origNE, origRe, origBs }()
	for _, kept := range []bool{false, true} {
		t.Run(fmt.Sprintf("kept=%v", kept), func(t *testing.T) {
			git.NewEffortWorktree = func(string, string, string) (string, func(), error) { return "/wt", func() {}, nil }
			reconcileInWorktree = func(_, id, effortID string, _ io.Writer) error {
				return writeIntent(intentPath(effortID), intent{Edges: []bd.Edge{{From: "cp", To: "pv", Type: "blocks"}}, Close: &bd.Closing{ID: id, Reason: "pivoted: pv"}})
			}
			backstop = func(bd.Bead, string, bool, func(), io.Writer, ...any) (bool, bool) { return false, kept }
			f := bdtest.New(t, bdtest.Claimed(bd.Bead{ID: "b1", Kind: contract.KindBuild, Parent: "cp"}), bd.Bead{ID: "cp", Status: "open"}, bd.Bead{ID: "pv", Status: "open"})
			if _, err := Dispatch(nil, &fakeCrew{}, bd.Bead{ID: "b1", Kind: contract.KindBuild, Parent: "cp"}, io.Discard, NewEffortID(), nil); err != nil {
				t.Fatal(err)
			}
			if got := f.Bead("cp").BlockedBy; !slices.Equal(got, []string{"pv"}) {
				t.Errorf("cp blocked by %v, want the handed edge published either way", got)
			}
			// kept: the close is dropped, so the claim is released and bd ready offers the bead again.
			if want := map[bool]string{false: "closed", true: "open"}[kept]; f.Bead("b1").Status != want {
				t.Errorf("b1 = %s, want %s", f.Bead("b1").Status, want)
			}
		})
	}
}

// effort.abandoned names the abandoned effort's routed branch, effort/<id> for a
// Build, which abandon never deletes: the next launch reuses it (Spec FR 48–49).
func TestAbandonNamesEffortBranch(t *testing.T) {
	evs := eventlogtest.Capture(t)
	b := bd.Bead{ID: "b1", Kind: contract.KindBuild, Parent: "cp"}
	Abandon([]Inflight{{ID: "e1", Bead: b}}, func(string, string) error { return nil })
	if len(*evs) != 1 || (*evs)[0].Name != "effort.abandoned" || (*evs)[0].Attrs["branch"] != "effort/b1" {
		t.Fatalf("events = %+v, want one effort.abandoned with branch effort/b1", *evs)
	}
	// A land step's detached tree is on its container, no branch left to reuse.
	*evs = nil
	Abandon([]Inflight{{ID: "e2", Bead: bd.Bead{ID: "cp", Kind: contract.KindCheckpoint}}}, func(string, string) error { return nil })
	if len(*evs) != 1 || (*evs)[0].Attrs["branch"] != "" {
		t.Fatalf("events = %+v, want one effort.abandoned with no branch", *evs)
	}
}

// sweptGraph seeds rv's swept member b (the sweep closed it obsolete, so session.Swept
// reads true) or, if not swept, b still claimed. Its Drafts are a Critique, a pivot,
// a Signal and one of a kind no crewmate knows yet; d1 is the bead handCrew's edge
// points from, and cp is the container. It takes b's dispatcher hold.
func sweptGraph(t *testing.T, b bd.Bead, swept bool) *bdtest.Fake {
	t.Helper()
	if swept {
		b.Status, b.CloseReason = "closed", bd.ObsoleteReason+" swept"
	} else {
		b = bdtest.Claimed(b)
	}
	f := bdtest.New(t, b, bd.Bead{ID: "cp", Kind: contract.KindCheckpoint, Status: "open"}, bd.Bead{ID: "d1", Status: "open"},
		bdtest.Draft(b.ID, bd.Bead{ID: "k1", Kind: contract.KindCritique}), bdtest.Draft(b.ID, bd.Bead{ID: "p1", Kind: contract.KindPivot}),
		bdtest.Draft(b.ID, bd.Bead{ID: "s1", Kind: contract.KindSignal}), bdtest.Draft(b.ID, bd.Bead{ID: "x1", Kind: "future"}))
	hold(t, b.ID)
	return f
}

// A swept member's tail (ADR 0124, Spec D2), from the repo root (a Usopp-shaped
// intent) or a worktree (intent read from the child): the handed edges and close
// are dropped, every Draft but its Signals is closed obsolete: in one call naming
// the child and its container (an unknown kind included), the Signals publish,
// draft.obsoleted and draft.published are recorded, and no Draft is deleted. A
// member not swept publishes its handed intent as before.
func TestSweptMemberRetiresNonSignalDrafts(t *testing.T) {
	for _, c := range []struct {
		name  string
		b     bd.Bead
		swept bool
	}{
		{"repo root", bd.Bead{ID: "rv1", Kind: contract.KindReview, Parent: "cp"}, true},
		{"worktree", bd.Bead{ID: "b1", Kind: contract.KindBuild, Parent: "cp"}, true},
		{"chart child", bd.Bead{ID: "sv1", Kind: contract.KindSurvey, Parent: "cp"}, true},
		{"not swept", bd.Bead{ID: "rv1", Kind: contract.KindReview, Parent: "cp"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			evs := eventlogtest.Capture(t)
			f := sweptGraph(t, c.b, c.swept)
			childReconcile(t, handCrew{}, c.b)
			if _, err := Dispatch(nil, handCrew{}, c.b, io.Discard, NewEffortID(), nil); err != nil {
				t.Fatal(err)
			}
			calls := f.Calls()
			if index(calls, "Delete") >= 0 {
				t.Errorf("calls = %q, no Draft is deleted", calls)
			}
			names := eventlogtest.Names(*evs)
			if !slices.Contains(names, "draft.published") {
				t.Errorf("events = %v, want draft.published", names)
			}
			i := slices.Index(names, "draft.obsoleted")
			if !c.swept {
				if i >= 0 {
					t.Errorf("events = %v, a member not swept obsoletes nothing", names)
				}
				for _, id := range []string{"k1", "p1", "s1", "x1"} {
					if got := f.Bead(id).Status; got != "open" {
						t.Errorf("%s = %s, want published open", id, got)
					}
				}
				if got := f.Bead("d1").DiscoveredFrom; !slices.Equal(got, []string{c.b.ID}) {
					t.Errorf("d1 discovered-from %v, want the handed edge", got)
				}
				if got := f.Bead(c.b.ID); got.Status != "closed" || got.CloseReason != "done" {
					t.Errorf("%s = %s %q, want closed by the handed close", c.b.ID, got.Status, got.CloseReason)
				}
				return
			}
			reason := fmt.Sprintf("%s %s of cp was retired", bd.ObsoleteReason, c.b.ID)
			for _, id := range []string{"k1", "p1", "x1"} {
				if got := f.Bead(id); got.Status != "closed" || got.CloseReason != reason {
					t.Errorf("%s = %s %q, want closed %q", id, got.Status, got.CloseReason, reason)
				}
			}
			if got := f.Bead("s1").Status; got != "open" {
				t.Errorf("Signal s1 = %s, want published open", got)
			}
			if got := f.Bead("d1").DiscoveredFrom; len(got) != 0 {
				t.Errorf("d1 discovered-from %v, the swept member's handed edges are dropped", got)
			}
			if got := f.Bead(c.b.ID).CloseReason; !strings.HasPrefix(got, bd.ObsoleteReason) {
				t.Errorf("%s close reason = %q, the handed close is dropped", c.b.ID, got)
			}
			if cl, pub := index(calls, "Close"), index(calls, "Batch"); cl < 0 || pub < cl {
				t.Errorf("calls = %q, want the obsolete close before the publish", calls)
			}
			if i < 0 {
				t.Fatalf("events = %v, want draft.obsoleted", names)
			}
			if o := (*evs)[i]; o.Attrs["creator"] != c.b.ID || o.Attrs["container"] != "cp" || fmt.Sprint(o.Attrs["ids"]) != "[k1 p1 x1]" || o.Attrs["effort"] == nil {
				t.Errorf("draft.obsoleted = %v", o.Attrs)
			}
		})
	}
}

// A failed obsolete close fails the effort through the existing failure path:
// its Drafts are deleted and nothing is published.
func TestSweptMemberFailedCloseFailsEffort(t *testing.T) {
	boom := errors.New("boom")
	b := bd.Bead{ID: "rv1", Kind: contract.KindReview, Parent: "cp"}
	f := sweptGraph(t, b, true)
	f.FailOn("Close", "*", boom)
	if _, err := Dispatch(nil, handCrew{}, b, io.Discard, NewEffortID(), nil); !errors.Is(err, boom) {
		t.Fatalf("Dispatch err = %v, want boom", err)
	}
	calls := f.Calls()
	if cl, del := index(calls, "Close"), index(calls, "Delete"); cl < 0 || del < cl || index(calls, "Batch") >= 0 {
		t.Errorf("calls = %q, want the failed close, then the delete, and no publish", calls)
	}
	for _, id := range []string{"k1", "p1", "s1", "x1"} {
		if _, err := bd.Show(id); !bd.IsNotFound(err) {
			t.Errorf("Show(%s) = %v, want the Draft deleted", id, err)
		}
	}
}

// A publish batch that fails once on 1213 is replayed inside bd, so the effort
// sees a clean publish and never reaches fail / Draft deletion (Spec D2, ADR 0153).
func TestPublishRetriesTransientBatchConflict(t *testing.T) {
	dir := t.TempDir()
	flag := filepath.Join(dir, "failed-once")
	script := `case "$1" in
list) echo '[{"id":"d1","title":"D","status":"deferred"}]' ;;
batch) cat >/dev/null; if [ ! -e ` + flag + ` ]; then touch ` + flag + `; echo 'Error 1213 (40001): serialization failure' >&2; exit 1; fi ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "bd"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	evs := eventlogtest.Capture(t)
	if err := (&effort{}).publish(bd.Bead{ID: "chart1", Kind: contract.KindChart}); err != nil {
		t.Fatalf("publish = %v, want the retry to absorb the 1213", err)
	}
	if _, err := os.Stat(flag); err != nil {
		t.Fatal("fake bd never failed the first batch")
	}
	if names := eventlogtest.Names(*evs); !slices.Contains(names, "draft.published") || slices.Contains(names, "draft.failed") || slices.Contains(names, "draft.deleted") {
		t.Errorf("events = %v, want draft.published only", names)
	}
}

// A bead paused by hand in the pre-cut window (abort found no child to signal) is
// re-read just before the spawn on both routes (ADR 0155): the effort fails
// without spawning a reconcile child or crew.Run, Drafts deleted, tree torn
// down, claim released, Outcome Aborted. A failed re-read fails it the same way;
// a pause that lands after the child still publishes (no re-read after it).
func TestPreSpawnPauseReRead(t *testing.T) {
	origNE, origRe, origBs := git.NewEffortWorktree, reconcileInWorktree, backstop
	defer func() { git.NewEffortWorktree, reconcileInWorktree, backstop = origNE, origRe, origBs }()
	boom := errors.New("boom")
	routes := []struct {
		name string
		b    bd.Bead
	}{
		{"worktree", bd.Bead{ID: "b1", Kind: contract.KindBuild, Parent: "cp"}},
		{"repo-root", bd.Bead{ID: "sp1", Kind: contract.KindSpec}},
	}
	// A read error is bd down for the rest of the effort, so its release re-read
	// fails too and leaves the claim (claim.failed), as the guarded release does.
	cases := []struct {
		name      string
		paused    bool // paused:manual before the spawn
		down      error
		late      bool // the pause lands after the child
		spawned   bool
		published bool
		released  bool
	}{
		{"paused", true, nil, false, false, false, true},
		{"read error", false, boom, false, false, false, false},
		{"paused after child", false, nil, true, true, true, true},
	}
	for _, r := range routes {
		for _, c := range cases {
			t.Run(r.name+"/"+c.name, func(t *testing.T) {
				b := bdtest.Claimed(r.b)
				if c.paused {
					b.Labels = []string{"paused:manual"}
				}
				f := draftGraph(t, b, contract.KindBuild)
				if c.down != nil {
					f.FailOn("Show", b.ID, c.down)
				}
				pause := func() { f.Mutate(b.ID, func(x *bd.Bead) { x.Labels = append(x.Labels, "paused:manual") }) }
				var spawned, torn bool
				git.NewEffortWorktree = func(string, string, string) (string, func(), error) {
					return "/wt", func() { torn = true }, nil
				}
				reconcileInWorktree = func(_, _, _ string, _ io.Writer) error {
					spawned = true
					if c.late {
						pause()
					}
					return nil
				}
				backstop = func(_ bd.Bead, _ string, _ bool, teardown func(), _ io.Writer, _ ...any) (bool, bool) {
					teardown()
					return false, false
				}
				var crew contract.Crewmate = &fakeCrew{}
				if c.late {
					crew = crewFunc(func() { spawned = true; pause() })
				}
				o, err := Dispatch(nil, crew, r.b, io.Discard, NewEffortID(), nil)
				if spawned != c.spawned {
					t.Errorf("spawned = %v, want %v", spawned, c.spawned)
				}
				if got := index(f.Calls(), "Batch") >= 0; got != c.published {
					t.Errorf("published = %v, want %v", got, c.published)
				}
				if c.spawned {
					if err != nil {
						t.Errorf("err = %v, want nil", err)
					}
					return
				}
				if err == nil {
					t.Error("want a failed effort")
				}
				if _, derr := bd.Show("d1"); !bd.IsNotFound(derr) {
					t.Errorf("Show(d1) = %v, want the Draft deleted", derr)
				}
				if got := f.Bead(b.ID).Status == "open"; got != c.released {
					t.Errorf("released = %v, want %v", got, c.released)
				}
				if r.name == "worktree" && !torn {
					t.Error("tree not torn down")
				}
				if wantAborted := c.paused; o.Aborted != wantAborted {
					t.Errorf("Aborted = %v, want %v", o.Aborted, wantAborted)
				}
			})
		}
	}
}

// aliveCore is a graph that probes liveness.Alive(id) on every read and release
// the effort makes, so a window where the hold has lapsed is named.
type aliveCore struct {
	*bdtest.Fake
	probe func(window string)
}

func (c aliveCore) Show(id string) (bd.Bead, error) { c.probe("Show"); return c.Fake.Show(id) }
func (c aliveCore) List(q bd.Query) ([]bd.Bead, error) {
	c.probe("List")
	return c.Fake.List(q)
}
func (c aliveCore) Batch(ops []bd.Op) error { c.probe("Batch"); return c.Fake.Batch(ops) }
func (c aliveCore) Unclaim(id string, g bd.IfAssignee) error {
	c.probe("Unclaim")
	return c.Fake.Unclaim(id, g)
}

// watch installs f behind aliveCore, so each of its calls fails the test unless id's
// dispatcher hold is live then, and returns the windows seen.
func watch(t *testing.T, f *bdtest.Fake, id string) *[]string {
	t.Helper()
	var seen []string
	t.Cleanup(bd.Use(aliveCore{f, func(window string) {
		if !liveness.Alive([]string{id})[id] {
			t.Errorf("%s dead inside %s", id, window)
		}
		seen = append(seen, window)
	}}))
	return &seen
}

// A claimed effort reads alive (liveness.Alive) in every dispatcher-only window
// (ADR 0154): inside the worktree cut, the integrate backstop and the release's
// unclaim on a worktree route, and inside the crewmate's Run on the repo-root
// route; it reads dead once the caller releases the Inflight's hold.
func TestClaimHoldSpansDispatcherWindows(t *testing.T) {
	t.Setenv("BEADS_DIR", t.TempDir())
	origNW, origRe, origBs := git.NewWorktree, reconcileInWorktree, backstop
	defer func() { git.NewWorktree, reconcileInWorktree, backstop = origNW, origRe, origBs }()
	for _, b := range []bd.Bead{{ID: "chart1", Kind: contract.KindChart}, {ID: "sg1", Kind: contract.KindSignal}} {
		t.Run(string(b.Kind), func(t *testing.T) {
			f := bdtest.New(t, bd.Bead{ID: b.ID, Kind: b.Kind, Status: "open"})
			var seen []string
			probe := func(window string) {
				if !liveness.Alive([]string{b.ID})[b.ID] {
					t.Errorf("%s dead inside %s", b.ID, window)
				}
				seen = append(seen, window)
			}
			git.NewWorktree = func(string) (string, func(), error) { probe("cut"); return "/wt", func() {}, nil }
			reconcileInWorktree = func(_, _, _ string, _ io.Writer) error { return nil }
			backstop = func(_ bd.Bead, _ string, _ bool, teardown func(), _ io.Writer, _ ...any) (bool, bool) {
				probe("backstop")
				teardown()
				return false, false
			}

			hold, err := Claim(b)
			if err != nil {
				t.Fatal(err)
			}
			graphSeen := watch(t, f, b.ID)
			crew := crewFunc(func() { probe("Run") })
			if _, err := Dispatch(nil, crew, b, io.Discard, NewEffortID(), nil); err != nil {
				t.Fatalf("Dispatch: %v", err)
			}
			if len(seen) == 0 || !slices.Contains(*graphSeen, "Unclaim") {
				t.Fatalf("windows probed = %v, graph calls = %v; want the effort's windows and the release's Unclaim", seen, *graphSeen)
			}
			if !liveness.Alive([]string{b.ID})[b.ID] {
				t.Error("dead after Dispatch returned, before the caller released")
			}
			hold.Release()
			if liveness.Alive([]string{b.ID})[b.ID] {
				t.Error("alive after release")
			}
		})
	}
}

// The hold also spans the success tail (ADR 0154): a swept member's retire, the
// publish, and a landed container's deleteLanded on the Detached route read the
// effort alive; and a busy cut returns with the hold still the caller's, dead
// once the caller releases it.
func TestClaimHoldSpansTailAndBusyCut(t *testing.T) {
	t.Run("tail", func(t *testing.T) {
		landedRepo(t)
		t.Setenv("BEADS_DIR", t.TempDir())
		chart := bd.Bead{ID: "c1", Kind: contract.KindChart, Status: "open", Labels: []string{contract.LabelDefogged}}
		f := bdtest.New(t, chart)
		hold, err := Claim(chart)
		if err != nil {
			t.Fatal(err)
		}
		defer hold.Release()
		f.Mutate("c1", func(b *bd.Bead) { b.Status, b.CloseReason = "closed", bd.ObsoleteReason+" swept" }) // swept and closed mid-effort
		seen := watch(t, f, chart.ID)
		if _, err := Dispatch(nil, &fakeCrew{}, chart, io.Discard, NewEffortID(), nil); err != nil {
			t.Fatalf("Dispatch: %v", err)
		}
		// retire and publish each read the Drafts (List), deleteLanded reads the bead (Show) last.
		if n := strings.Count(strings.Join(*seen, " "), "List"); n < 2 || (*seen)[len(*seen)-1] != "Show" {
			t.Errorf("windows probed = %v, want retire's and publish's Draft reads, then deleteLanded's Show", *seen)
		}
	})
	t.Run("busy cut", func(t *testing.T) {
		t.Setenv("BEADS_DIR", t.TempDir())
		origNE, origH := git.NewEffortWorktree, git.WorktreeOn
		defer func() { git.NewEffortWorktree, git.WorktreeOn = origNE, origH }()
		git.WorktreeOn = heldBy("/tmp/mugicrew-wt-1/wt")
		git.NewEffortWorktree = func(string, string, string) (string, func(), error) {
			return "", nil, exec.Command("sh", "-c", "exit 128").Run()
		}
		b := bd.Bead{ID: "cc1", Kind: contract.KindChartConflict, Parent: "chart1", Status: "open"}
		f := bdtest.New(t, b)
		hold, err := Claim(b)
		if err != nil {
			t.Fatal(err)
		}
		seen := watch(t, f, b.ID)
		Dispatch(nil, &fakeCrew{}, b, io.Discard, NewEffortID(), nil)
		if !slices.Contains(*seen, "Unclaim") {
			t.Errorf("windows probed = %v, want the busy cut's Unclaim", *seen)
		}
		if !liveness.Alive([]string{b.ID})[b.ID] {
			t.Error("busy cut dropped the caller's hold")
		}
		hold.Release()
		if liveness.Alive([]string{b.ID})[b.ID] {
			t.Error("busy cut leaked the hold past the caller's release")
		}
	})
}

// crewFunc is a Crewmate whose Run calls f.
type crewFunc func()

func (f crewFunc) Run(contract.Ctx, contract.Bead) error { f(); return nil }
func (f crewFunc) Launch(contract.Ctx, contract.Bead) (contract.LaunchCommand, error) {
	return nil, nil
}

// A failed claim CAS releases the hold, and a bead another dispatcher holds is
// never claimed (claim.failed).
func TestClaimHoldFailureModes(t *testing.T) {
	t.Setenv("BEADS_DIR", t.TempDir())
	b := bd.Bead{ID: "mc-r", Kind: contract.KindResearch, Status: "open"}
	f := bdtest.New(t, b)
	claims := func() (n int) {
		for _, c := range f.Calls() {
			if c == "Claim mc-r" {
				n++
			}
		}
		return n
	}

	f.FailOn("Claim", b.ID, errors.New("bd down"))
	if h, err := Claim(b); err == nil || h != nil {
		t.Fatalf("Claim = %v, %v; want the CAS error and no hold", h, err)
	}
	if liveness.Alive([]string{b.ID})[b.ID] {
		t.Error("failed CAS left the lock held")
	}

	f.Heal("Claim", b.ID)
	held, err := Claim(b)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	before := claims()
	if _, err := Claim(b); err == nil || claims() != before {
		t.Errorf("second dispatcher: err = %v, claims %d -> %d; want refused, no claim", err, before, claims())
	}
}

// A limited effort (the harness returned a LimitError) ends as effort.limited at
// Warn instead of effort.failed, carries Outcome.Limit, and still deletes its
// Drafts and releases its claim; the error is returned. On both placements; the
// worktree child hands Limit across the failed run in its intent file.
func TestLimitedEffort(t *testing.T) {
	reset := time.Date(2026, 10, 16, 15, 44, 0, 0, time.UTC)
	lerr := &contract.LimitError{Limit: contract.Limit{ResetsAt: reset, Source: "message"}, Err: errors.New("claude: exit 1")}
	for _, b := range []bd.Bead{{ID: "chart1", Kind: contract.KindChart}, {ID: "sp1", Kind: contract.KindSpec}} {
		t.Run(string(b.Kind), func(t *testing.T) {
			evs := eventlogtest.Capture(t)
			f := draftGraph(t, b, contract.KindBuild)
			crew := errCrew{lerr}
			childReconcile(t, crew, b)
			o, err := Dispatch(nil, crew, b, io.Discard, NewEffortID(), nil)
			if err == nil {
				t.Fatal("want the error returned")
			}
			if o.Limit == nil || !o.Limit.ResetsAt.Equal(reset) || o.Limit.Source != "message" {
				t.Errorf("Outcome.Limit = %+v", o.Limit)
			}
			got := eventlogtest.Names(*evs)
			i := slices.Index(got, "effort.limited")
			if i < 0 || slices.Contains(got, "effort.failed") {
				t.Fatalf("events = %v, want effort.limited and no effort.failed", got)
			}
			l := (*evs)[i]
			if l.Level != slog.LevelWarn || l.Attrs["resets_at"] != "2026-10-16T15:44:00Z" || l.Attrs["source"] != "message" {
				t.Errorf("limited = %+v", l)
			}
			calls := f.Calls()
			if del, un := index(calls, "Delete d1"), index(calls, "Unclaim "+b.ID); del < 0 || un < del {
				t.Errorf("calls = %q, want the Draft delete, then the release", calls)
			}
		})
	}
}

// A nested bead's effort/<id> is looked for in the child (Spec FR 25, 29): its holder
// is the child tree lifted to the host, which a live session accounts for; the
// Workspace's own worktree list is never asked.
func TestForeignHeldAndDeleteLandedConsultTheChild(t *testing.T) {
	_, kid := contextstest.Workspace(t)
	tmp, err := filepath.EvalSymlinks(t.TempDir()) // minted trees are resolved
	if err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(tmp, "mugicrew-wt-n", "wt")
	contextstest.Git(t, kid, "worktree", "add", "-q", "-b", "effort/chart1", filepath.Join(host, "kid"))
	origH, origS := git.WorktreeOn, sessions
	t.Cleanup(func() { git.WorktreeOn, sessions = origH, origS })
	git.WorktreeOn = func(string) (git.Holder, bool, error) {
		t.Error("the Workspace's worktree list was read for a nested bead")
		return git.Holder{}, false, nil
	}
	sessions = func() (map[string]session.Info, error) {
		return map[string]session.Info{"g1": {Worktree: host, State: session.Alive}}, nil
	}
	b := bd.Bead{ID: "chart1", Kind: contract.KindChart, Labels: []string{contextstest.Label}}
	if _, foreign, err := ForeignHeld(b, nil); err != nil || foreign {
		t.Errorf("ForeignHeld = %v, %v; want own contention, not foreign", foreign, err)
	}
	sessions = func() (map[string]session.Info, error) { return nil, nil }
	if _, foreign, err := ForeignHeld(b, nil); err != nil || !foreign {
		t.Errorf("ForeignHeld with no session = %v, %v; want foreign", foreign, err)
	}

	// deleteLanded: landed in the child, so its branch goes from the child
	contextstest.Git(t, kid, "branch", "chart/chart1")
	trailer := git.TrailerFor(string(contract.KindChart), "chart1")
	contextstest.Git(t, kid, "commit", "-q", "--allow-empty", "-m", "squash\n\n"+trailer+"\n\n"+git.TipPin(contextstest.Git(t, kid, "rev-parse", "chart/chart1")))
	graph(t, "closed", bd.Bead{ID: "chart1"})
	(&effort{}).deleteLanded(b, "chart/chart1")
	if contextstest.Git(t, kid, "branch", "--list", "chart/chart1") != "" {
		t.Error("the landed branch was not deleted from the child")
	}
}

// A landed container whose branch head is not the landed Tip: (a stale pin merged
// after the branch advanced) keeps its branch, so the unlanded commits survive; a
// Tip: equal to head still deletes it (Spec FR 21).
func TestDeleteLandedKeepsBranchPastLandedTip(t *testing.T) {
	_, kid := contextstest.Workspace(t)
	graph(t, "closed", bd.Bead{ID: "chart1"})
	b := bd.Bead{ID: "chart1", Kind: contract.KindChart, Labels: []string{contextstest.Label}}
	trailer := git.TrailerFor(string(contract.KindChart), "chart1")
	contextstest.Git(t, kid, "branch", "chart/chart1")
	contextstest.Git(t, kid, "commit", "-q", "--allow-empty", "-m", "squash\n\n"+trailer+"\n\n"+git.TipPin(contextstest.Git(t, kid, "rev-parse", "chart/chart1")))
	// advance the branch past the pin
	contextstest.Git(t, kid, "update-ref", "refs/heads/chart/chart1", contextstest.Git(t, kid, "commit-tree", "HEAD^{tree}", "-p", "chart/chart1", "-m", "later"))
	(&effort{}).deleteLanded(b, "chart/chart1")
	if contextstest.Git(t, kid, "branch", "--list", "chart/chart1") == "" {
		t.Error("a branch past the landed Tip: was deleted")
	}
}

// The reconcile child's argv reads as b1's effort holder under liveness's scan, so
// renaming the verb without updating liveness's effort-verb list fails here.
func TestReconcileCmdReadsAliveUnderLiveness(t *testing.T) {
	cmd := reconcileCmd("/bin/mugicrew", "/wt", "b1", "abcd1234", io.Discard)
	if !liveness.HoldsEffort(cmd.Args, "b1") {
		t.Errorf("argv %v reads dead under liveness: add its verb to liveness's effort verbs", cmd.Args)
	}
}

// lockProbe wraps the installed bd core and records whether id's lock was held when
// the claim was written.
type lockProbe struct {
	bd.Core
	dir  string
	held bool
}

func (p *lockProbe) Claim(id string, g bd.IfStatus) error {
	p.held = liveness.HeldIn(p.dir, id)
	return p.Core.Claim(id, g)
}

// dispatch.Claim, the loop's and run-now's claim path, reads the bead's lock held
// when the bd claim is written, and stays held until the caller releases it (Spec
// D11): a claim written first would read as an Orphaned claim.
func TestClaimHoldsTheLockWhenTheClaimIsWritten(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BEADS_DIR", dir)
	f := bdtest.New(t, bd.Bead{ID: "mc-a", Status: "open"})
	probe := &lockProbe{Core: f, dir: dir}
	t.Cleanup(bd.Use(probe))

	hold, err := Claim(f.Bead("mc-a"))
	if err != nil {
		t.Fatal(err)
	}
	if !probe.held {
		t.Error("lock free when the bd claim was written")
	}
	if !liveness.HeldIn(dir, "mc-a") {
		t.Error("lock free after Claim returned, before the release")
	}
	hold.Release()
	if liveness.HeldIn(dir, "mc-a") {
		t.Error("lock still held after the release")
	}
}
