package tui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/dispatch"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/liveness"
)

// spawnFixture is a claimed run-now effort with its real hold, the host Start
// stubbed, and the common dir and unclaim redirected.
type spawnFixture struct {
	r        dispatch.Inflight
	common   string
	started  *exec.Cmd
	unclaims []string
}

func newSpawnFixture(t *testing.T, startErr error) *spawnFixture {
	t.Helper()
	t.Setenv("BEADS_DIR", t.TempDir())
	f := &spawnFixture{common: t.TempDir()}
	b := bd.Bead{ID: "mc-r"}
	hold, err := liveness.Acquire(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hold.Release)
	f.r = dispatch.Inflight{ID: "e1", Bead: b, Hold: hold}
	origCommon, origStart, origUnclaim := commonDir, startHost, hitlUnclaim
	t.Cleanup(func() { commonDir, startHost, hitlUnclaim = origCommon, origStart, origUnclaim })
	commonDir = func() (string, error) { return f.common, nil }
	startHost = func(c *exec.Cmd) error { f.started = c; return startErr }
	hitlUnclaim = func(id string) error { f.unclaims = append(f.unclaims, id); return nil }
	return f
}

func (f *spawnFixture) holdClosed() bool {
	_, err := f.r.Hold.File().Stat()
	return err != nil
}

// The spawn hands the host exactly the one hold as fd 3, in its own session, and
// closes the TUI's copy once Start returns.
func TestSpawnRunHostHandsOverTheHold(t *testing.T) {
	f := newSpawnFixture(t, nil)
	var heldAtStart bool
	startHost = func(c *exec.Cmd) error {
		f.started = c
		_, err := f.r.Hold.File().Stat()
		heldAtStart = err == nil
		return nil
	}
	cmd, out, err := spawnRunHost(f.r)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"run", "mc-r", "--lock-fd=3"}; !slices.Equal(cmd.Args[1:], want) {
		t.Errorf("args = %v, want self + %v", cmd.Args, want)
	}
	if len(cmd.ExtraFiles) != 1 || cmd.ExtraFiles[0] != f.r.Hold.File() {
		t.Errorf("ExtraFiles = %v, want the hold alone", cmd.ExtraFiles)
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setsid {
		t.Error("host is not setsid-detached")
	}
	if cmd.Stdin != nil {
		t.Errorf("stdin = %v, want nil", cmd.Stdin)
	}
	if want := filepath.Join(f.common, "mugicrew", "run-mc-r.out"); out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
	if !heldAtStart {
		t.Error("hold was closed before Start")
	}
	if !f.holdClosed() {
		t.Error("the TUI kept its copy of the hold after Start")
	}
	if len(f.unclaims) != 0 {
		t.Errorf("unclaimed %v after a good start", f.unclaims)
	}
}

// The out file is opened O_TRUNC: the latest run's output alone.
func TestSpawnRunHostTruncatesOut(t *testing.T) {
	f := newSpawnFixture(t, nil)
	out := filepath.Join(f.common, "mugicrew", "run-mc-r.out")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := spawnRunHost(f.r); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out); len(b) != 0 {
		t.Errorf("run-mc-r.out = %q, want truncated", b)
	}
}

// A Start error leaves the hold with the TUI: run.failed, unclaim, release.
func TestSpawnRunHostStartFailure(t *testing.T) {
	f := newSpawnFixture(t, errors.New("fork: nope"))
	evs := eventlogtest.Capture(t)
	if _, _, err := spawnRunHost(f.r); err == nil || !strings.Contains(err.Error(), "fork: nope") {
		t.Fatalf("err = %v, want the Start error", err)
	}
	if !slices.Equal(f.unclaims, []string{"mc-r"}) {
		t.Errorf("unclaims = %v, want mc-r", f.unclaims)
	}
	if !f.holdClosed() {
		t.Error("hold not released")
	}
	names := eventlogtest.Names(*evs)
	if !slices.Contains(names, "run.failed") || !slices.Contains(names, "claim.released") {
		t.Fatalf("events = %v, want run.failed and claim.released", names)
	}
	for _, e := range *evs {
		if e.Name == "run.failed" && e.Attrs["effort"] != "e1" {
			t.Errorf("run.failed attrs = %v, want effort e1", e.Attrs)
		}
	}
}

// exitErr runs sh to get a real Wait error.
func exitErr(t *testing.T, script string) error {
	t.Helper()
	return exec.Command("sh", "-c", script).Run()
}

// Every exit-code row of ADR 0332 maps to its status line and tail.
func TestRunStatus(t *testing.T) {
	out := strings.Join([]string{"l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8", "l9", "l10", "l11", "l12", "l13", "last"}, "\n") + "\n"
	tail12 := strings.Join([]string{"l3", "l4", "l5", "l6", "l7", "l8", "l9", "l10", "l11", "l12", "l13", "last"}, "\n")
	for _, c := range []struct {
		name string
		err  error
		want string
	}{
		{"clean", nil, "run X done"},
		{"failed", exitErr(t, "exit 1"), "run X failed: exit status 1\n" + tail12},
		{"pre-dispatch", exitErr(t, "exit 10"), "run X failed before dispatch\n" + tail12},
		{"not run", exitErr(t, "exit 11"), "run X not run\nlast"},
		{"abandoned", exitErr(t, "exit 13"), "run X abandoned (paused:abandoned)\n" + tail12},
		{"other", exitErr(t, "exit 7"), "run X failed: exit status 7\n" + tail12},
		{"usage 2", exitErr(t, "exit 2"), "run X failed: exit status 2\n" + tail12},
		{"signal", exitErr(t, "kill -9 $$"), "run X killed: signal: killed\n" + tail12},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := runStatus("X", c.err, out); got != c.want {
				t.Errorf("status = %q, want %q", got, c.want)
			}
		})
	}
}

// Exit 12 names the cause from the host's last line, which says why.
func TestRunStatusLiftFailed(t *testing.T) {
	last := "mugicrew run: X done; could not lift paused:failing: bd: down"
	got := runStatus("X", exitErr(t, "exit 12"), "noise\n"+last+"\n")
	want := "run X done; lifting paused:failing failed — clear it by hand\n" + last
	if got != want {
		t.Errorf("status = %q, want %q", got, want)
	}
}
