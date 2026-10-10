package dispatch

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/route"
	"jayulabs.org/mugicrew/session"
)

// TestMain keeps tests that don't capture events from appending to the real repo's
// event log, and keeps a failed effort's claim guard off the live bd: by default
// the re-read sees a closed bead, so nothing is released. The backstop's real
// `integrate` child would re-exec this test binary, so by default it only tears
// down; an effort cut goes through the test's git.NewWorktree, never a real branch.
// chartDest is charting chart1's route: its own effort/chart1, onto chart/chart1.
var chartDest, _ = route.Route(bd.Bead{ID: "chart1", Kind: contract.KindChart})

func TestMain(m *testing.M) {
	eventlogtest.Discard()
	backstop = func(_ bd.Bead, _ string, _ bool, teardown func(), _ io.Writer, _ ...any) (bool, bool) {
		teardown()
		return false, false
	}
	git.NewEffortWorktree = func(id, _, _ string) (string, func(), error) { return git.NewWorktree(git.EffortBranch(id)) }
	show = func(string) (bd.Bead, error) { return bd.Bead{Status: "closed"}, nil }
	unclaim = func(string) error { return nil }
	sessions = func() (map[string]session.Info, error) { return nil, nil }
	publish = func(string, []bd.Edge, *bd.Closing) ([]string, error) { return nil, nil }
	deleteDrafts = func(string) ([]string, []bd.BranchOutcome, error) { return nil, nil, nil }
	os.Exit(m.Run())
}

// A failed effort releases the caller's claim, guarded by a re-read (ADR 0034),
// on every failure exit: only a bead still in_progress is unclaimed, with one
// claim.released carrying the effort id after effort.failed; a closed or already
// open bead is left alone; a failed re-read records claim.failed and releases
// nothing. Dispatch returns the effort's own error throughout.
func TestFailedEffortReleasesClaimGuarded(t *testing.T) {
	origNW, origRe, origShow, origUn := git.NewWorktree, reconcileInWorktree, show, unclaim
	defer func() { git.NewWorktree, reconcileInWorktree, show, unclaim = origNW, origRe, origShow, origUn }()

	boom := errors.New("boom")
	cutOK := func(string) (string, func(), error) { return "/wt", func() {}, nil }
	exits := []struct {
		name string
		b    bd.Bead
		crew contract.Crewmate
		nw   func(string) (string, func(), error)
		re   error
	}{
		{"route", bd.Bead{ID: "r1", Kind: contract.KindResearch}, &fakeCrew{}, cutOK, nil},
		{"worktree cut", bd.Bead{ID: "chart1", Kind: contract.KindChart}, &fakeCrew{},
			func(string) (string, func(), error) { return "", nil, boom }, nil},
		{"reconcile", bd.Bead{ID: "chart1", Kind: contract.KindChart}, &fakeCrew{}, cutOK, boom},
		{"crew.Run", bd.Bead{ID: "sp1", Kind: contract.KindSpec}, errCrew{boom}, cutOK, nil},
	}
	guards := []struct {
		status  string
		showErr error
		release bool
		claimEv string // trailing claim event, "" for none
	}{
		{"in_progress", nil, true, "claim.released"},
		{"closed", nil, false, ""},
		{"open", nil, false, ""},
		{"", errors.New("bd down"), false, "claim.failed"},
	}
	for _, x := range exits {
		for _, g := range guards {
			t.Run(x.name+"/"+g.status, func(t *testing.T) {
				evs := eventlogtest.Capture(t)
				git.NewWorktree = x.nw
				reconcileInWorktree = func(_, _, _ string, _ io.Writer) error { return x.re }
				show = func(string) (bd.Bead, error) { return bd.Bead{Status: g.status}, g.showErr }
				var unclaimed []string
				unclaim = func(id string) error { unclaimed = append(unclaimed, id); return nil }

				_, err := Dispatch(nil, x.crew, x.b, io.Discard, NewEffortID(), nil)
				if err == nil || err == g.showErr {
					t.Fatalf("Dispatch err = %v, want the effort's own error", err)
				}
				var wantUn []string
				if g.release {
					wantUn = []string{x.b.ID}
				}
				if !slices.Equal(unclaimed, wantUn) {
					t.Errorf("unclaimed = %v, want %v", unclaimed, wantUn)
				}
				want := []string{"effort.started", "effort.failed"}
				if g.claimEv != "" {
					want = append(want, g.claimEv)
				}
				if got := eventlogtest.Names(*evs); !slices.Equal(got, want) {
					t.Fatalf("events = %v, want %v", got, want)
				}
				if g.claimEv != "" && (*evs)[2].Attrs["effort"] != (*evs)[0].Attrs["effort"] {
					t.Errorf("%s must carry the effort id, got %v", g.claimEv, (*evs)[2].Attrs)
				}
			})
		}
	}
}

// A successful effort releases a claim its handler left in_progress (a non-terminal
// success, e.g. Jinbe filing a land conflict), with the same re-read guard as
// a failure: a closed or open bead is left alone.
func TestSucceededEffortReleasesStrandedClaim(t *testing.T) {
	origShow, origUn := show, unclaim
	defer func() { show, unclaim = origShow, origUn }()
	for status, want := range map[string]bool{"in_progress": true, "closed": false, "open": false} {
		show = func(string) (bd.Bead, error) { return bd.Bead{Status: status}, nil }
		var unclaimed []string
		unclaim = func(id string) error { unclaimed = append(unclaimed, id); return nil }
		if _, err := Dispatch(nil, &fakeCrew{}, bd.Bead{ID: "sp1", Kind: contract.KindSpec}, io.Discard, NewEffortID(), nil); err != nil {
			t.Fatalf("Dispatch: %v", err)
		}
		if got := len(unclaimed) == 1 && unclaimed[0] == "sp1"; got != want || len(unclaimed) > 1 {
			t.Errorf("%s: unclaimed = %v, want release=%v", status, unclaimed, want)
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
	origNW, origRe, origUn, origH := git.NewWorktree, reconcileInWorktree, unclaim, git.WorktreeOn
	defer func() { git.NewWorktree, reconcileInWorktree, unclaim, git.WorktreeOn = origNW, origRe, origUn, origH }()
	git.WorktreeOn = heldBy("/tmp/mugicrew-wt-1/wt")

	var unclaimed []string
	unclaim = func(id string) error { unclaimed = append(unclaimed, id); return nil }
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
	if len(unclaimed) != 1 || unclaimed[0] != "chart1" {
		t.Errorf("a busy effort must release its claim once, got %v", unclaimed)
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

// A busy skip ran nothing: no started is left dangling, only the claim's release
// and a Debug effort.busy naming the branch (in the file only at MUGICREW_LOG_LEVEL=debug).
func TestBusyEffortLeavesNoStarted(t *testing.T) {
	evs := eventlogtest.Capture(t)
	origNE, origUn, origH := git.NewEffortWorktree, unclaim, git.WorktreeOn
	defer func() { git.NewEffortWorktree, unclaim, git.WorktreeOn = origNE, origUn, origH }()
	git.WorktreeOn = heldBy("/tmp/mugicrew-wt-1/wt")
	unclaim = func(string) error { return nil }
	git.NewEffortWorktree = func(string, string, string) (string, func(), error) {
		return "", nil, exec.Command("sh", "-c", "exit 128").Run()
	}
	cut.Store("/tmp/mugicrew-wt-1/wt", true) // an effort in this process cut it
	defer cut.Delete("/tmp/mugicrew-wt-1/wt")
	origP, origD := publish, deleteDrafts
	defer func() { publish, deleteDrafts = origP, origD }()
	publish = func(string, []bd.Edge, *bd.Closing) ([]string, error) {
		t.Error("a busy cut must not publish")
		return nil, nil
	}
	deleteDrafts = func(string) ([]string, []bd.BranchOutcome, error) {
		t.Error("a busy cut must not delete Drafts")
		return nil, nil, nil
	}
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
}

// A refused cut's listed holder is own contention (effort.busy, Debug) only when it
// is temp-shaped, not prunable, and an in-flight effort's tree (this process's cut,
// or a live effort on the branch in another) or an alive session accounts for it; every other holder is a foreign hold (effort.held, Warn, naming holder and
// prunable) (ADR 0054). Both release the claim and record no effort.started, and
// nothing is removed or pruned (no git seam but the scripted lookup is reached).
func TestRefusedCutClassifiesHolder(t *testing.T) {
	origNE, origUn, origH, origS := git.NewEffortWorktree, unclaim, git.WorktreeOn, sessions
	defer func() { git.NewEffortWorktree, unclaim, git.WorktreeOn, sessions = origNE, origUn, origH, origS }()
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
			var unclaimed []string
			unclaim = func(id string) error { unclaimed = append(unclaimed, id); return nil }
			inflight := func() ([]bd.Bead, error) { return c.inflight, nil }
			origP, origD := publish, deleteDrafts
			defer func() { publish, deleteDrafts = origP, origD }()
			publish = func(string, []bd.Edge, *bd.Closing) ([]string, error) {
				t.Error("a refused cut must not publish")
				return nil, nil
			}
			deleteDrafts = func(string) ([]string, []bd.BranchOutcome, error) {
				t.Error("a refused cut must not delete Drafts")
				return nil, nil, nil
			}

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
			if !slices.Equal(unclaimed, []string{"cc1"}) {
				t.Errorf("unclaimed = %v, want the claim released once", unclaimed)
			}
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
	origNW, origUn, origH, origS := git.NewWorktree, unclaim, git.WorktreeOn, sessions
	defer func() { git.NewWorktree, unclaim, git.WorktreeOn, sessions = origNW, origUn, origH, origS }()
	git.NewWorktree = func(string) (string, func(), error) {
		return "", nil, exec.Command("sh", "-c", "exit 128").Run()
	}
	git.WorktreeOn = heldBy("/tmp/mugicrew-wt-9/wt")
	unclaim = func(string) error { return nil }
	down := errors.New("down")
	for name, c := range map[string]struct{ inflightErr, sessErr error }{"inflight": {down, nil}, "sessions": {nil, down}} {
		evs := eventlogtest.Capture(t)
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
	origNW, origShow, origUn, origH := git.NewWorktree, show, unclaim, git.WorktreeOn
	defer func() { git.NewWorktree, show, unclaim, git.WorktreeOn = origNW, origShow, origUn, origH }()
	git.WorktreeOn = heldBy("")
	show = func(string) (bd.Bead, error) { return bd.Bead{Status: "in_progress"}, nil }
	var unclaimed []string
	unclaim = func(id string) error { unclaimed = append(unclaimed, id); return nil }
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
	if !slices.Equal(unclaimed, []string{"chart1"}) {
		t.Errorf("unclaimed = %v, want the claim released once", unclaimed)
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

func (h *stubHarness) ResumeCommand(string) (contract.AgentCommand, error) {
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

// A successful session records session.ended at Info.
func TestSessionEnded(t *testing.T) {
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
			ctx := Sessions(harnessCtx{&stubHarness{res: tc.res, err: tc.err}}, bd.Bead{ID: "sp1", Kind: contract.KindSpec})
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
	ctx := Sessions(harnessCtx{h}, bd.Bead{ID: "chart1", Kind: contract.KindChart, Parent: "p1"})
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
	evs := eventlogtest.Capture(t)
	origC := claim
	defer func() { claim = origC }()
	var bdErr error
	claim = func(string) error { return bdErr }
	b := bd.Bead{ID: "mc-r", Kind: contract.KindResearch, Parent: "mc-c"}
	Claim(b)
	bdErr = errors.New("bd down")
	if err := Claim(b); err != bdErr {
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

// draftSeams records the claim and Draft ops in order; publish fails with pubErr.
func draftSeams(t *testing.T, pubErr error) *[]string {
	origP, origD, origShow, origUn := publish, deleteDrafts, show, unclaim
	t.Cleanup(func() { publish, deleteDrafts, show, unclaim = origP, origD, origShow, origUn })
	var ops []string
	publish = func(creator string, edges []bd.Edge, c *bd.Closing) ([]string, error) {
		ops = append(ops, fmt.Sprintf("publish %s %v %+v", creator, edges, c))
		if pubErr != nil {
			return nil, pubErr
		}
		return []string{"d1"}, nil
	}
	deleteDrafts = func(creator string) ([]string, []bd.BranchOutcome, error) {
		ops = append(ops, "delete "+creator)
		return []string{"d1"}, []bd.BranchOutcome{{Branch: "chart/d1", Err: errors.New("has work")}}, nil
	}
	show = func(string) (bd.Bead, error) { return bd.Bead{Status: "in_progress"}, nil }
	unclaim = func(id string) error { ops = append(ops, "unclaim "+id); return nil }
	return &ops
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
// the close and edges its Go flow handed over, before it releases — on both the
// worktree and in-process paths — recording draft.published with creator and ids.
func TestSuccessfulEffortPublishesBeforeRelease(t *testing.T) {
	for _, b := range []bd.Bead{{ID: "chart1", Kind: contract.KindChart}, {ID: "sp1", Kind: contract.KindSpec}} {
		t.Run(string(b.Kind), func(t *testing.T) {
			evs := eventlogtest.Capture(t)
			ops := draftSeams(t, nil)
			childReconcile(t, handCrew{}, b)
			if _, err := Dispatch(nil, handCrew{}, b, io.Discard, NewEffortID(), nil); err != nil {
				t.Fatal(err)
			}
			want := []string{
				fmt.Sprintf("publish %s [{d1 %s discovered-from}] &{ID:%s Reason:done}", b.ID, b.ID, b.ID),
				"unclaim " + b.ID,
			}
			if !slices.Equal(*ops, want) {
				t.Errorf("ops = %q, want %q", *ops, want)
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
				ops := draftSeams(t, c.pubErr)
				childReconcile(t, c.crew, b)
				if _, err := Dispatch(nil, c.crew, b, io.Discard, NewEffortID(), nil); !errors.Is(err, boom) {
					t.Fatalf("Dispatch err = %v, want boom", err)
				}
				want := []string{"delete " + b.ID, "unclaim " + b.ID}
				if c.publishCalled {
					want = append([]string{(*ops)[0]}, want...)
				}
				if !slices.Equal(*ops, want) {
					t.Errorf("ops = %q, want %q", *ops, want)
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
	if _, err := git.Land("chart/c1", git.TrailerFor(string(contract.KindChart), "c1")); err != nil {
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
	origShow := show
	defer func() { show = origShow }()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			landedRepo(t)
			if c.setup != nil {
				c.setup(t)
			}
			show = func(string) (bd.Bead, error) { return bd.Bead{Status: c.status}, nil }
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
	origNE, origRe, origBs, origPub, origShow, origUn := git.NewEffortWorktree, reconcileInWorktree, backstop, publish, show, unclaim
	defer func() {
		git.NewEffortWorktree, reconcileInWorktree, backstop, publish, show, unclaim = origNE, origRe, origBs, origPub, origShow, origUn
	}()
	for _, kept := range []bool{false, true} {
		t.Run(fmt.Sprintf("kept=%v", kept), func(t *testing.T) {
			git.NewEffortWorktree = func(string, string, string) (string, func(), error) { return "/wt", func() {}, nil }
			reconcileInWorktree = func(_, id, effortID string, _ io.Writer) error {
				return writeIntent(intentPath(effortID), intent{Edges: []bd.Edge{{From: "cp", To: "pv", Type: "blocks"}}, Close: &bd.Closing{ID: id, Reason: "pivoted: pv"}})
			}
			backstop = func(bd.Bead, string, bool, func(), io.Writer, ...any) (bool, bool) { return false, kept }
			var closing *bd.Closing
			var edges []bd.Edge
			publish = func(_ string, e []bd.Edge, c *bd.Closing) ([]string, error) { edges, closing = e, c; return nil, nil }
			show = func(string) (bd.Bead, error) { return bd.Bead{Status: "in_progress"}, nil }
			released := false
			unclaim = func(string) error { released = true; return nil }
			if _, err := Dispatch(nil, &fakeCrew{}, bd.Bead{ID: "b1", Kind: contract.KindBuild, Parent: "cp"}, io.Discard, NewEffortID(), nil); err != nil {
				t.Fatal(err)
			}
			if (closing == nil) != kept || len(edges) != 1 {
				t.Errorf("published close %v, edges %v; want the close dropped = %v and the edge kept", closing, edges, kept)
			}
			if !released {
				t.Error("the claim must be released so bd ready offers the bead again")
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
