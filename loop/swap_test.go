package loop

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/selfexe"
)

// swapStubs makes l an installed loop watching a real file, with the stat
// identity and the --version answer under the test's hand: swap(n) changes the
// identity, missing(true) makes the stat fail, version holds the answer.
func swapStubs(t *testing.T, l *Loop) (bin string, swap func(uint64), missing func(bool), version *atomic.Value) {
	t.Helper()
	bin = filepath.Join(t.TempDir(), "mugicrew")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	var ino atomic.Uint64
	var gone atomic.Bool
	oID, oVer, oRe, oSt := selfexe.Identify, swapVersion, swapRecheck, swapStat
	t.Cleanup(func() { selfexe.Identify, swapVersion, swapRecheck, swapStat = oID, oVer, oRe, oSt })
	swapStat = func(p string) (os.FileInfo, error) {
		if gone.Load() {
			return nil, os.ErrNotExist
		}
		return os.Stat(p)
	}
	swapRecheck = time.Millisecond
	selfexe.Identify = func(string) (selfexe.Identity, error) {
		if gone.Load() {
			return selfexe.Identity{}, errors.New("missing")
		}
		return selfexe.Identity{Dev: 1, Ino: ino.Load()}, nil
	}
	version = &atomic.Value{}
	version.Store("1.2.3")
	swapVersion = func(p string) (string, error) {
		if p != bin {
			t.Errorf("--version ran %q; want the startup path %q", p, bin)
		}
		return version.Load().(string), nil
	}
	l.Rev, l.SwapPath = "", bin
	return bin, func(n uint64) { ino.Store(n) }, gone.Store, version
}

// A changed identity ends polling for a restart drain onto the startup path, with
// a Draining State carrying RestartTo; the Restart's Rev is the new --version.
func TestSwapDrainsAndRestartsOntoStartupPath(t *testing.T) {
	captureEvents(t)
	l, b, started, release, _ := blockingLoop(t)
	bin, swap, _, _ := swapStubs(t, l)
	sig := make(chan os.Signal, 2)
	res, states := runFull(l, sig)
	<-started
	swap(2)
	var s State
	for s = range states {
		if s.State == "draining" {
			break
		}
	}
	if want := (State{State: "draining", Inflight: []bd.Bead{b}, RestartTo: swapRestartTo}); !reflect.DeepEqual(s, want) {
		t.Fatalf("State = %+v; want %+v", s, want)
	}
	close(release)
	if got := <-res; got.err != nil || !reflect.DeepEqual(got.r, &Restart{Rev: "1.2.3", Bin: bin}) {
		t.Fatalf("Run = %+v, %v; want Restart{1.2.3 %s}", got.r, got.err, bin)
	}
}

// A missing file waits and does not exec; a pre-release answer waits too; the strict
// release ends it.
func TestSwapWaitsForFileAndStrictVersion(t *testing.T) {
	captureEvents(t)
	l, _, _, _, _ := blockingLoop(t)
	idle(l)
	_, swap, missing, version := swapStubs(t, l)
	sig := make(chan os.Signal, 2)
	res, _ := runFull(l, sig)
	time.Sleep(30 * time.Millisecond) // Run has recorded the identity
	missing(true)
	select {
	case got := <-res:
		t.Fatalf("Run returned %+v, %v with the file missing", got.r, got.err)
	case <-time.After(50 * time.Millisecond):
	}
	missing(false)
	swap(9)
	version.Store("1.3.0-rc.1")
	select {
	case got := <-res:
		t.Fatalf("Run returned %+v, %v on a pre-release answer", got.r, got.err)
	case <-time.After(50 * time.Millisecond):
	}
	version.Store("1.3.0")
	select {
	case got := <-res:
		if got.err != nil || got.r == nil || got.r.Rev != "1.3.0" {
			t.Fatalf("Run = %+v, %v; want Restart at 1.3.0", got.r, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no restart once the strict release answered")
	}
}

// A stop during the swap drain wins; SIGUSR1 reverts it to the swap drain.
func TestStopDuringSwapDrainWinsAndCancelReverts(t *testing.T) {
	captureEvents(t)
	l, b, started, release, _ := blockingLoop(t)
	bin, swap, _, _ := swapStubs(t, l)
	sig := make(chan os.Signal, 2)
	res, states := runFull(l, sig)
	<-started
	swap(2)
	for s := range states {
		if s.State == "draining" && s.RestartTo != "" {
			break
		}
	}
	sig <- syscall.SIGTERM
	if s := <-states; !reflect.DeepEqual(s, State{State: "draining", Inflight: []bd.Bead{b}}) {
		t.Fatalf("State after a stop = %+v; want draining, no restart_to", s)
	}
	sig <- syscall.SIGUSR1
	if s := <-states; s.RestartTo != swapRestartTo {
		t.Fatalf("State after cancel = %+v; want restart_to again", s)
	}
	sig <- syscall.SIGTERM
	<-states
	close(release)
	if got := <-res; got.r != nil || got.err != nil {
		t.Fatalf("Run = %+v, %v; want a plain stop", got.r, got.err)
	}
	_ = bin
}

// A loop without a SwapPath (from source, pinned) never compares anything.
func TestNoSwapPathNeverSwaps(t *testing.T) {
	l := idle(&Loop{})
	l.swapKnown = true
	l.checkSwap() // Swapped("", zero) would be true on a missing file
	if l.restart != nil {
		t.Fatal("a loop with no SwapPath began a restart")
	}
}

// A swap whose binary never answers a strict release logs swap.waiting once per
// answer, so the event log explains a drain that never ends.
func TestSwapReadyLogsAStallOnce(t *testing.T) {
	events := captureEvents(t)
	l := &Loop{}
	_, _, _, version := swapStubs(t, l)
	version.Store("1.3.0-rc.1")
	l.swapping = true
	r := &Restart{Bin: l.SwapPath}
	for range 3 {
		if l.swapReady(r) {
			t.Fatal("swapReady = true for a pre-release; want false")
		}
	}
	n := 0
	for _, e := range *events {
		if e.Name == "swap.waiting" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("swap.waiting logged %d times, want 1", n)
	}
}
