package loop

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/session"
)

// fakeUpgrader is an Upgrader under the test's hand.
type fakeUpgrader struct {
	mu                sync.Mutex
	target, blockedBy string
	known             bool
	npm               int
	installs          []string
	doctors           int
	docCode, docFixes int
	onInstall         func() // runs inside Install, before it returns
}

func (f *fakeUpgrader) Check() (string, string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.target, f.blockedBy, f.known
}
func (f *fakeUpgrader) Install(t string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installs = append(f.installs, t)
	if f.onInstall != nil {
		f.onInstall()
	}
	return f.npm
}
func (f *fakeUpgrader) Doctor() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.doctors++
	return f.docCode, f.docFixes
}
func (f *fakeUpgrader) set(target, blockedBy string) {
	f.mu.Lock()
	f.target, f.blockedBy = target, blockedBy
	f.mu.Unlock()
}
func (f *fakeUpgrader) got() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.installs...)
}

// upgradeLoop is an idle loop opted in with a fake Upgrader and a session table.
func upgradeLoop(t *testing.T, table func() (map[string]session.Info, error)) (*Loop, *fakeUpgrader) {
	t.Helper()
	l, _, _, release, _ := blockingLoop(t)
	close(release)
	idle(l)
	l.Interval = 2 * time.Millisecond
	f := &fakeUpgrader{known: true}
	l.Upgrade, l.UpgradeGate = f, func() bool { return true }
	o := sessionTable
	sessionTable = table
	t.Cleanup(func() { sessionTable = o })
	return l, f
}

func empty() (map[string]session.Info, error) { return nil, nil }

func runFor(t *testing.T, l *Loop) {
	t.Helper()
	sig := make(chan os.Signal, 2)
	res, _ := runFull(l, sig)
	time.Sleep(60 * time.Millisecond)
	sig <- os.Interrupt
	<-res
}

// At natural idle the loop installs the target, and a clean doctor follows.
func TestUpgradeInstallsAtNaturalIdle(t *testing.T) {
	evs := captureEvents(t)
	l, f := upgradeLoop(t, empty)
	f.set("1.3.0", "")
	runFor(t, l)
	if got := f.got(); len(got) == 0 || got[0] != "1.3.0" {
		t.Fatalf("installs = %v; want 1.3.0", got)
	}
	if f.doctors == 0 {
		t.Error("no doctor after a clean install")
	}
	var waiting int
	for _, e := range *evs {
		if e.Name == "upgrade.waiting" {
			waiting++
		}
	}
	if waiting != 1 {
		t.Errorf("upgrade.waiting logged %d times; want once", waiting)
	}
}

// A live session, or a session-table read error, is not idle: nothing installs.
func TestUpgradeWaitsForSessionsAndReadErrors(t *testing.T) {
	captureEvents(t)
	for name, table := range map[string]func() (map[string]session.Info, error){
		"live": func() (map[string]session.Info, error) {
			return map[string]session.Info{"b": {State: session.Alive}}, nil
		},
		"error": func() (map[string]session.Info, error) { return nil, os.ErrPermission },
	} {
		t.Run(name, func(t *testing.T) {
			l, f := upgradeLoop(t, table)
			f.set("1.3.0", "")
			runFor(t, l)
			if got := f.got(); len(got) != 0 {
				t.Fatalf("installed %v with %s", got, name)
			}
		})
	}
}

// A loop whose gate says not opted in never installs.
func TestUpgradeNotOptedInNeverInstalls(t *testing.T) {
	captureEvents(t)
	l, f := upgradeLoop(t, empty)
	l.UpgradeGate = func() bool { return false }
	f.set("1.3.0", "")
	runFor(t, l)
	if got := f.got(); len(got) != 0 {
		t.Fatalf("installs = %v; want none", got)
	}
}

// A blockedBy logs upgrade.skipped once per value.
func TestUpgradeSkippedLoggedOncePerValue(t *testing.T) {
	evs := captureEvents(t)
	l, f := upgradeLoop(t, empty)
	f.set("", "1.4.0")
	runFor(t, l)
	var n int
	for _, e := range *evs {
		if e.Name == "upgrade.skipped" {
			n++
			if e.Attrs["version"] != "1.4.0" || e.Attrs["reason"] != "human-step" {
				t.Errorf("attrs = %v", e.Attrs)
			}
		}
	}
	if n != 1 || len(f.got()) != 0 {
		t.Errorf("skipped %d times, installs %v; want once, none", n, f.got())
	}
}

// An unknown cache arms nothing.
func TestUpgradeUnknownCacheDoesNotArm(t *testing.T) {
	captureEvents(t)
	l, f := upgradeLoop(t, empty)
	f.known = false
	f.set("1.3.0", "")
	runFor(t, l)
	if got := f.got(); len(got) != 0 {
		t.Fatalf("installs = %v", got)
	}
}

// An npm failure keeps polling, is recorded in the State, is not retried, and a
// newer target re-arms it.
func TestUpgradeNpmFailureIsRememberedAndNewerTargetRearms(t *testing.T) {
	evs := captureEvents(t)
	l, f := upgradeLoop(t, empty)
	f.npm = 1
	f.set("1.3.0", "")
	sig := make(chan os.Signal, 2)
	res, states := runFull(l, sig)
	var failedState bool
	deadline := time.After(2 * time.Second)
wait:
	for {
		select {
		case s := <-states:
			if s.UpgradeFailed == "1.3.0" && s.State == looplock.On {
				failedState = true
				break wait
			}
		case <-deadline:
			break wait
		}
	}
	time.Sleep(40 * time.Millisecond)
	if got := f.got(); len(got) != 1 {
		t.Fatalf("installs = %v; want exactly one try of 1.3.0", got)
	}
	f.set("1.3.1", "")
	time.Sleep(40 * time.Millisecond)
	sig <- os.Interrupt
	<-res
	if !failedState {
		t.Error("no On State carried UpgradeFailed=1.3.0")
	}
	if got := f.got(); len(got) < 2 || got[1] != "1.3.1" {
		t.Errorf("installs = %v; want 1.3.0 then 1.3.1", got)
	}
	var failed int
	for _, e := range *evs {
		if e.Name == "upgrade.failed" && e.Attrs["to"] == "1.3.0" && fmt.Sprint(e.Attrs["npm_code"]) == "1" {
			failed++
		}
	}
	if failed != 1 {
		t.Errorf("upgrade.failed for 1.3.0 logged %d times (events %+v)", failed, *evs)
	}
}

// A clean install is followed by the swap restart: the install changes the binary's
// identity and the loop execs onto its startup path.
func TestUpgradeCleanDoctorLeadsToSwapRestart(t *testing.T) {
	captureEvents(t)
	l, f := upgradeLoop(t, empty)
	bin, swap, _, _ := swapStubs(t, l)
	f.set("1.3.0", "")
	g := &swapOnInstall{fakeUpgrader: f, swap: swap}
	l.Upgrade = g
	sig := make(chan os.Signal, 2)
	res, _ := runFull(l, sig)
	got := <-res
	if got.err != nil || got.r == nil || got.r.Bin != bin || got.r.Rev != "1.2.3" {
		t.Fatalf("Run = %+v, %v; want Restart onto %s", got.r, got.err, bin)
	}
}

// A clean install ends polling in the same tick: a ready candidate is not
// dispatched under the old dispatcher once the new binary is on disk.
func TestUpgradeCleanInstallDispatchesNothingAfterward(t *testing.T) {
	captureEvents(t)
	l, _, started, release, _ := blockingLoop(t)
	defer close(release)
	l.Interval = 2 * time.Millisecond
	o := sessionTable
	sessionTable = empty
	t.Cleanup(func() { sessionTable = o })
	f := &fakeUpgrader{known: true}
	f.set("1.3.0", "")
	l.UpgradeGate = func() bool { return true }
	_, swap, _, _ := swapStubs(t, l)
	l.Upgrade = &swapOnInstall{fakeUpgrader: f, swap: swap}
	res, _ := runFull(l, make(chan os.Signal, 2))
	got := <-res
	if got.err != nil || got.r == nil {
		t.Fatalf("Run = %+v, %v; want the swap Restart", got.r, got.err)
	}
	select {
	case id := <-started:
		t.Fatalf("dispatched %s after the install", id)
	default:
	}
}

type swapOnInstall struct {
	*fakeUpgrader
	swap func(uint64)
}

func (s *swapOnInstall) Install(t string) int { s.swap(2); return s.fakeUpgrader.Install(t) }

// A tripped doctor, by a non-zero exit or by a To-fix count, stops the loop for good:
// no Restart even though the binary was swapped, and the reported state names
// mugicrew doctor fix in StoppedFor.
func TestUpgradeTrippedDoctorStopsWithoutRestart(t *testing.T) {
	for _, c := range []struct {
		name        string
		code, fixes int
	}{{"exit", 1, 0}, {"fixes", 0, 2}} {
		t.Run(c.name, func(t *testing.T) {
			evs := captureEvents(t)
			l, f := upgradeLoop(t, empty)
			_, swap, _, _ := swapStubs(t, l)
			f.set("1.3.0", "")
			f.docCode, f.docFixes = c.code, c.fixes
			l.Upgrade = &swapOnInstall{fakeUpgrader: f, swap: swap}
			res, states := runFull(l, make(chan os.Signal, 2))
			select {
			case got := <-res:
				if got.err != nil || got.r != nil {
					t.Fatalf("Run = %+v, %v; want a clean stop with no Restart", got.r, got.err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("loop did not stop")
			}
			var last State
			for len(states) > 0 {
				last = <-states
			}
			if !strings.Contains(last.StoppedFor, "mugicrew doctor fix") {
				t.Errorf("last state = %+v; want StoppedFor naming mugicrew doctor fix", last)
			}
			var tripped bool
			for _, e := range *evs {
				tripped = tripped || e.Name == "upgrade.doctor" && fmt.Sprint(e.Attrs["code"]) == fmt.Sprint(c.code) && fmt.Sprint(e.Attrs["fixes"]) == fmt.Sprint(c.fixes)
			}
			if !tripped {
				t.Errorf("no upgrade.doctor event in %+v", *evs)
			}
		})
	}
}

// clockAt stubs the loop's clock; adv moves it.
func clockAt(t *testing.T) (adv func(time.Duration)) {
	t.Helper()
	var mu sync.Mutex
	cur := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	o := now
	now = func() time.Time { mu.Lock(); defer mu.Unlock(); return cur }
	t.Cleanup(func() { now = o })
	return func(d time.Duration) { mu.Lock(); cur = cur.Add(d); mu.Unlock() }
}

// liveTable is a session table whose one session ends when end(true) is called.
func liveTable() (table func() (map[string]session.Info, error), end func(bool)) {
	var ended atomic.Bool
	return func() (map[string]session.Info, error) {
		if ended.Load() {
			return nil, nil
		}
		return map[string]session.Info{"b": {State: session.Alive}}, nil
	}, ended.Store
}

// After 24h without idle the loop drains (Draining with RestartTo = target), waits
// out a live session without limit, then installs and restarts onto the new binary.
func TestUpgradeDrainsAfter24hAndInstallsOnceSessionsEnd(t *testing.T) {
	evs := captureEvents(t)
	table, end := liveTable()
	l, f := upgradeLoop(t, table)
	_, swap, _, _ := swapStubs(t, l)
	adv := clockAt(t)
	f.set("1.3.0", "")
	l.Upgrade = &swapOnInstall{fakeUpgrader: f, swap: swap}
	sig := make(chan os.Signal, 2)
	res, states := runFull(l, sig)
	time.Sleep(30 * time.Millisecond)
	if got := f.got(); len(got) != 0 || hasEvent(*evs, "upgrade.draining") {
		t.Fatalf("before 24h: installs %v, draining logged", got)
	}
	adv(24 * time.Hour)
	var s State
	for s = range states {
		if s.State == "draining" {
			break
		}
	}
	if s.RestartTo != "1.3.0" {
		t.Errorf("RestartTo = %q; want 1.3.0", s.RestartTo)
	}
	time.Sleep(30 * time.Millisecond)
	if got := f.got(); len(got) != 0 {
		t.Fatalf("installed %v while a session was live", got)
	}
	end(true)
	got := <-res
	if got.err != nil || got.r == nil {
		t.Fatalf("Run = %+v, %v; want a Restart", got.r, got.err)
	}
	if g := f.got(); len(g) != 1 || g[0] != "1.3.0" {
		t.Errorf("installs = %v; want [1.3.0]", g)
	}
}

// A binary swapped by another loop or a human while the loop drains for its Upgrade
// is not overwritten: install never runs, and the restart is a plain swap onto the
// version the new binary answers, not the stale target.
func TestUpgradeDrainSupersededBySwapSkipsInstall(t *testing.T) {
	evs := captureEvents(t)
	table, end := liveTable()
	l, f := upgradeLoop(t, table)
	_, swap, _, version := swapStubs(t, l)
	adv := clockAt(t)
	f.set("1.3.0", "")
	version.Store("1.5.0")
	sig := make(chan os.Signal, 2)
	res, states := runFull(l, sig)
	time.Sleep(30 * time.Millisecond)
	adv(24 * time.Hour)
	for s := range states {
		if s.State == "draining" {
			break
		}
	}
	swap(2)
	end(true)
	got := <-res
	if got.err != nil || got.r == nil || got.r.Rev != "1.5.0" {
		t.Fatalf("Run = %+v, %v; want a Restart with Rev 1.5.0", got.r, got.err)
	}
	if g := f.got(); len(g) != 0 {
		t.Errorf("installs = %v; want none", g)
	}
	if !hasEvent(*evs, "upgrade.superseded") {
		t.Errorf("no upgrade.superseded event in %+v", *evs)
	}
}

// A superseded upgrade drain does not wait for live sessions: it restarts as a plain
// swap, and the lock stops naming the stale target.
func TestUpgradeDrainSupersededDoesNotWaitForSessions(t *testing.T) {
	captureEvents(t)
	table, _ := liveTable()
	l, f := upgradeLoop(t, table)
	_, swap, _, version := swapStubs(t, l)
	adv := clockAt(t)
	f.set("1.3.0", "")
	version.Store("1.5.0")
	sig := make(chan os.Signal, 2)
	res, states := runFull(l, sig)
	time.Sleep(30 * time.Millisecond)
	adv(24 * time.Hour)
	for s := range states {
		if s.State == "draining" {
			break
		}
	}
	swap(2)
	select {
	case got := <-res:
		if got.err != nil || got.r == nil || got.r.Rev != "1.5.0" {
			t.Fatalf("Run = %+v, %v; want a Restart with Rev 1.5.0", got.r, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("superseded drain still waits on the live session")
	}
}

// A newer target appearing mid-wait keeps the clock running: the cap counts from the
// first arm, so a busy loop still drains within 24h of it.
func TestUpgradeNewerTargetKeepsWaitClock(t *testing.T) {
	captureEvents(t)
	table, _ := liveTable()
	l, f := upgradeLoop(t, table)
	swapStubs(t, l)
	adv := clockAt(t)
	f.set("1.3.0", "")
	sig := make(chan os.Signal, 2)
	res, states := runFull(l, sig)
	time.Sleep(30 * time.Millisecond)
	adv(23 * time.Hour)
	f.set("1.4.0", "")
	time.Sleep(60 * time.Millisecond)
	adv(2 * time.Hour)
	var s State
	for s = range states {
		if s.State == "draining" {
			break
		}
	}
	if s.RestartTo != "1.4.0" {
		t.Errorf("RestartTo = %q; want 1.4.0", s.RestartTo)
	}
	sig <- os.Interrupt
	<-res
}

// A stop during the upgrade drain wins: nothing installs.
func TestUpgradeDrainStopWinsNothingInstalls(t *testing.T) {
	captureEvents(t)
	table, _ := liveTable()
	l, f := upgradeLoop(t, table)
	swapStubs(t, l)
	adv := clockAt(t)
	f.set("1.3.0", "")
	sig := make(chan os.Signal, 2)
	res, states := runFull(l, sig)
	time.Sleep(30 * time.Millisecond)
	adv(25 * time.Hour)
	for s := range states {
		if s.State == "draining" {
			break
		}
	}
	sig <- os.Interrupt
	got := <-res
	if got.r != nil || got.err != nil {
		t.Fatalf("Run = %+v, %v; want a plain stop", got.r, got.err)
	}
	if g := f.got(); len(g) != 0 {
		t.Errorf("installed %v after a stop", g)
	}
}

// A stop that arrives while the drain's npm install and doctor run still wins:
// the install finishes, but Run returns a plain stop, not the swap Restart.
func TestUpgradeDrainStopDuringInstallWins(t *testing.T) {
	captureEvents(t)
	table, end := liveTable()
	l, f := upgradeLoop(t, table)
	swapStubs(t, l)
	adv := clockAt(t)
	f.set("1.3.0", "")
	sig := make(chan os.Signal, 2)
	f.onInstall = func() {
		sig <- os.Interrupt
		time.Sleep(50 * time.Millisecond) // let Loop.signals close the gen's stop
	}
	res, states := runFull(l, sig)
	time.Sleep(30 * time.Millisecond)
	adv(25 * time.Hour)
	for s := range states {
		if s.State == "draining" {
			break
		}
	}
	end(true) // the sessions end: the drain installs
	got := <-res
	if got.r != nil || got.err != nil {
		t.Fatalf("Run = %+v, %v; want a plain stop", got.r, got.err)
	}
	if g := f.got(); len(g) != 1 {
		t.Errorf("installs = %v; want the install to finish", g)
	}
}

func hasEvent(evs []eventlogtest.Event, name string) bool {
	for _, e := range evs {
		if e.Name == name {
			return true
		}
	}
	return false
}
