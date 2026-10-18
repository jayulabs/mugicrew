package session

import (
	"cmp"
	"errors"
	"os"
	"os/signal"
	"slices"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/git"
)

// Placement turns route.Route into a concrete cwd + teardown: an in-tree
// kind gets a fresh worktree of its routed branch with a real teardown, an effort
// branch cut at its container (Spec D2); a leaf gets the repo root and a
// no-op teardown. Stub the git seams so the decision is checkable with fake beads
// and no real repo.
func TestPlacementPerKind(t *testing.T) {
	origWT, origNE, origRoot := git.NewWorktree, git.NewEffortWorktree, git.RepoRoot
	defer func() { git.NewWorktree, git.NewEffortWorktree, git.RepoRoot = origWT, origNE, origRoot }()

	var cutBranch string
	torn := false
	git.NewWorktree = func(branch string) (string, func(), error) {
		cutBranch = branch
		return "/wt/" + branch, func() { torn = true }, nil
	}
	git.NewEffortWorktree = func(id, container, pin string) (string, func(), error) {
		cutBranch = git.EffortBranch(id) + "@" + cmp.Or(pin, container)
		return "/wt/" + git.EffortBranch(id), func() { torn = true }, nil
	}
	git.RepoRoot = func() (string, error) { return "/repo/root", nil }

	// grilling → worktree of its own effort branch cut at its PARENT chart, with a
	// working teardown.
	cwd, teardown, err := Placement(bd.Bead{ID: "grl1", Kind: contract.KindGrilling, Parent: "chart1"})
	if err != nil {
		t.Fatalf("grilling placement: %v", err)
	}
	if cutBranch != "effort/grl1@chart/chart1" {
		t.Errorf("grilling worktree branch = %q, want effort/grl1 at chart/chart1", cutBranch)
	}
	if cwd != "/wt/effort/grl1" {
		t.Errorf("grilling cwd = %q, want the worktree", cwd)
	}
	teardown()
	if !torn {
		t.Error("worktree teardown must run (drops the tree via --force)")
	}

	// charting (a Chart) → its own effort branch cut at its OWN chart branch.
	if _, _, err := Placement(bd.Bead{ID: "chart1", Kind: contract.KindChart}); err != nil {
		t.Fatalf("chart placement: %v", err)
	}
	if cutBranch != "effort/chart1@chart/chart1" {
		t.Errorf("chart worktree branch = %q, want effort/chart1 at chart/chart1", cutBranch)
	}

	// prototype → its effort branch at its PARENT chart; chart-conflict (ADR 0071)
	// → its effort branch at the pinned Tip it carries (Spec D2).
	for k, want := range map[contract.Kind]string{contract.KindPrototype: "effort/x1@chart/chart1", contract.KindChartConflict: "effort/x1@t1p"} {
		cutBranch = ""
		if _, _, err := Placement(bd.Bead{ID: "x1", Kind: k, Parent: "chart1", Description: "Tip: t1p"}); err != nil {
			t.Fatalf("%s placement: %v", k, err)
		}
		if cutBranch != want {
			t.Errorf("%s worktree branch = %q, want %s", k, cutBranch, want)
		}
	}

	// signal triage → repo root, no-op teardown (must not panic when deferred).
	cutBranch = ""
	cwd, teardown, err = Placement(bd.Bead{ID: "sig1", Kind: contract.KindSignal})
	if err != nil {
		t.Fatalf("signal placement: %v", err)
	}
	if cwd != "/repo/root" {
		t.Errorf("leaf cwd = %q, want the repo root", cwd)
	}
	if cutBranch != "" {
		t.Errorf("a leaf must not cut a worktree; branch=%q", cutBranch)
	}
	teardown() // no-op; just must not panic
}

// A HITL leaf on an effort branch (a Surgery) cuts or reuses effort/<id> at its
// container through the shared primitive, as AFK dispatch does (Spec D2).
func TestPlacementCutsEffortBranch(t *testing.T) {
	origNE := git.NewEffortWorktree
	defer func() { git.NewEffortWorktree = origNE }()
	var gotID, gotContainer string
	git.NewEffortWorktree = func(id, container, _ string) (string, func(), error) {
		gotID, gotContainer = id, container
		return "/wt/effort/" + id, func() {}, nil
	}
	cwd, _, err := Placement(bd.Bead{ID: "sg1", Kind: contract.KindSurgery, Parent: "cp1"})
	if err != nil {
		t.Fatalf("surgery placement: %v", err)
	}
	if gotID != "sg1" || gotContainer != "checkpoint/cp1" || cwd != "/wt/effort/sg1" {
		t.Errorf("cut %q at %q into %q, want sg1 at checkpoint/cp1", gotID, gotContainer, cwd)
	}
}

// optRead is one read of a session's @autofinish deadline and @waiting bit.
type optRead struct {
	deadline time.Time
	waiting  Waiting
}

// clockRun is what one run of the Auto-exit clock did: whether it fired, how
// often it unset @autofinish, and each status-right it wrote.
type clockRun struct {
	fired  bool
	unsets int
	bars   []string
}

// runClock runs hitl-run's Auto-exit clock (Spec D3, D4) for bead mc-x
// over the reads given, in order, at a fake now, until they run out.
func runClock(t *testing.T, now time.Time, reads ...optRead) (r clockRun) {
	t.Helper()
	origRead, origUnset, origNow, origPoll, origBar := readAutoExit, unsetAutoExit, clockNow, autoExitPoll, setBar
	t.Cleanup(func() {
		readAutoExit, unsetAutoExit, clockNow, autoExitPoll, setBar = origRead, origUnset, origNow, origPoll, origBar
	})
	setBar = func(sock, pane, right string) {
		if sock != "/s" || pane != "%3" {
			t.Errorf("bar set on %s %s, want the pane's /s %%3", sock, pane)
		}
		r.bars = append(r.bars, right)
	}
	clockNow, autoExitPoll = func() time.Time { return now }, time.Millisecond
	drained := make(chan struct{})
	readAutoExit = func(sock, pane string) (time.Time, Waiting) {
		if sock != "/s" || pane != "%3" {
			t.Errorf("read %s %s, want the pane's /s %%3", sock, pane)
		}
		if len(reads) == 0 {
			select {
			case <-drained:
			default:
				close(drained)
			}
			return time.Time{}, NotWaiting
		}
		r := reads[0]
		reads = reads[1:]
		return r.deadline, r.waiting
	}
	unsetAutoExit = func(string, string) { r.unsets++ }
	fire, done, ended := make(chan struct{}, 1), make(chan struct{}), make(chan struct{})
	go func() { autoExitClock("/s", "%3", "mc-x", fire, done); close(ended) }()
	<-drained
	close(done)
	<-ended
	select {
	case <-fire:
		r.fired = true
	default:
	}
	return r
}

// The clock fires only when, past the deadline, a fresh read still finds it set
// and the session still waiting on the human's turn; otherwise it unsets the
// option and sends nothing. No deadline, or one still ahead, is never signalled.
func TestAutoExitClock(t *testing.T) {
	now := time.Unix(1760000000, 0)
	past, ahead := now.Add(-time.Second), now.Add(time.Minute)
	for _, tc := range []struct {
		name   string
		reads  []optRead
		fired  bool
		unsets int
	}{
		{"deadline passed, waiting turn", []optRead{{past, WaitTurn}, {past, WaitTurn}}, true, 1},
		{"waiting cleared at the deadline", []optRead{{past, WaitTurn}, {past, NotWaiting}}, false, 1},
		{"asking at the deadline", []optRead{{past, WaitTurn}, {past, WaitAsk}}, false, 1},
		{"disarmed at the deadline", []optRead{{past, WaitTurn}, {time.Time{}, WaitTurn}}, false, 1},
		{"no deadline", []optRead{{time.Time{}, WaitTurn}, {time.Time{}, WaitTurn}}, false, 0},
		{"deadline ahead", []optRead{{ahead, WaitTurn}, {ahead, WaitTurn}}, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if r := runClock(t, now, tc.reads...); r.fired != tc.fired || r.unsets != tc.unsets {
				t.Errorf("fired %v, unsets %d; want %v, %d", r.fired, r.unsets, tc.fired, tc.unsets)
			}
		})
	}
}

// While armed, each tick writes the countdown bar (Spec D4, FR14); once
// the option reads cleared it restores the normal bar for the bead's closed-ness,
// read as the TUI reads it, and then leaves the bar alone (FR15).
func TestAutoExitClockBar(t *testing.T) {
	now := time.Unix(1760000000, 0)
	for _, tc := range []struct {
		status, restored string
	}{
		{"open", "F10 bead · F12 back to TUI · /exit to finish"},
		{"closed", "#[fg=green,reverse] ✓ mc-x closed — /exit to finish · F10 bead · F12 back to TUI "},
	} {
		t.Run(tc.status, func(t *testing.T) {
			origShow := showBead
			t.Cleanup(func() { showBead = origShow })
			showBead = func(id string) (bd.Bead, error) { return bd.Bead{ID: id, Status: tc.status}, nil }
			r := runClock(t, now,
				optRead{now.Add(105 * time.Second), WaitTurn},
				optRead{now.Add(104 * time.Second), WaitTurn},
				optRead{time.Time{}, WaitTurn}, // F11 or a reply cleared it
				optRead{time.Time{}, WaitTurn})
			want := []string{"auto-exit in 1:45 · F11 cancels", "auto-exit in 1:44 · F11 cancels", tc.restored}
			if !slices.Equal(r.bars, want) {
				t.Errorf("bars %q, want %q", r.bars, want)
			}
		})
	}
}

// A session never armed has its bar left to the TUI: the clock writes nothing.
func TestAutoExitClockUnarmedBar(t *testing.T) {
	if r := runClock(t, time.Unix(1760000000, 0), optRead{}, optRead{}); len(r.bars) != 0 {
		t.Errorf("bars %q, want none", r.bars)
	}
}

// A fired Auto-exit is an ordinary rc-0 exit (Spec D3): the agent the
// clock interrupts exits 0 and hitl-run runs the same clean close as a /exit.
func TestHitlRunAutoExitCleanClose(t *testing.T) {
	eventlogtest.Capture(t)
	calls := stubDrafts(t, nil)
	origRun, origRead, origUnset, origPoll, origBar := runAgent, readAutoExit, unsetAutoExit, autoExitPoll, setBar
	t.Cleanup(func() {
		runAgent, readAutoExit, unsetAutoExit, autoExitPoll, setBar = origRun, origRead, origUnset, origPoll, origBar
	})
	autoExitPoll, setBar = time.Millisecond, func(string, string, string) {}
	readAutoExit = func(string, string) (time.Time, Waiting) { return time.Unix(1, 0), WaitTurn }
	unsetAutoExit = func(string, string) {}
	runAgent = func(_ []string, interrupt <-chan struct{}) error {
		select {
		case <-interrupt:
			return nil // claude exits 0 on SIGINT at an idle prompt
		case <-time.After(5 * time.Second):
			return errors.New("never interrupted")
		}
	}
	t.Setenv("TMUX", "/s,1,0")
	t.Setenv("TMUX_PANE", "%3")
	if rc := HitlRun([]string{"--bead=g", argvArg(t, "claude")}, meter(t)); rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if want := []string{"closing /s,1,0 %3", "return /s,1,0 %3", "publish g"}; !slices.Equal(*calls, want) {
		t.Errorf("close = %q, want %q", *calls, want)
	}
}

// The real runAgent delivers the clock's interrupt as SIGINT to the agent's pid
// alone: a shell trapping INT exits 0, and this process (hitl-run) is not signalled.
func TestRunAgentInterruptsChildOnly(t *testing.T) {
	self := make(chan os.Signal, 1)
	signal.Notify(self, os.Interrupt)
	defer signal.Stop(self)
	interrupt := make(chan struct{})
	// Fire once the shell has set its trap; sleep in the background so the trap
	// runs as soon as INT lands.
	go func() { time.Sleep(200 * time.Millisecond); interrupt <- struct{}{} }()
	err := runAgent([]string{"sh", "-c", `trap 'exit 0' INT; sleep 5 & wait`}, interrupt)
	if err != nil {
		t.Fatalf("agent exit = %v, want 0", err)
	}
	select {
	case <-self:
		t.Error("hitl-run itself got SIGINT")
	case <-time.After(50 * time.Millisecond):
	}
}
