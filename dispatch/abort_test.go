package dispatch

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"syscall"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/bd/bdtest"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
)

// stubAbort seeds mc-a in status and stubs Abort's process seams (the lock unheld),
// recording each pid scan and kill in order into *calls. The scan records whether
// mc-a was already paused by hand, so the pause-first order shows in the calls.
func stubAbort(t *testing.T, status string, pids []int, killErr map[int]error) (*bdtest.Fake, *[]string) {
	t.Helper()
	oPids, oKill, oHeld := abortPids, killGroup, abortHeld
	t.Cleanup(func() { abortPids, killGroup, abortHeld = oPids, oKill, oHeld })
	f := bdtest.New(t, bd.Bead{ID: "mc-a", Status: status})
	abortHeld = func(string) bool { return false }
	var calls []string
	abortPids = func(string) []int {
		calls = append(calls, fmt.Sprintf("pids paused=%v", bd.PausedBy(f.Bead("mc-a"), bd.PausedManual)))
		return pids
	}
	killGroup = func(pid int) error { calls = append(calls, "kill"); return killErr[pid] }
	return f, &calls
}

// A bead not in_progress is refused with "nothing in flight": no pause, no
// event, no signal (ADR 0083 §4).
func TestAbortRefusesNothingInFlight(t *testing.T) {
	evs := eventlogtest.Capture(t)
	f, calls := stubAbort(t, "open", []int{7}, nil)
	if _, _, err := Abort("mc-a", "cli"); err == nil || err.Error() != "mc-a: nothing in flight" {
		t.Fatalf("err = %v, want nothing in flight", err)
	}
	if bd.Paused(f.Bead("mc-a")) || len(*calls) != 0 || len(*evs) != 0 {
		t.Errorf("a refusal writes nothing, got calls %v events %v", *calls, *evs)
	}
}

// A failed pause write returns the bd error with nothing logged or signalled:
// a kill without the pause would be re-dispatched (ADR 0083 §5).
func TestAbortPauseErrorSignalsNothing(t *testing.T) {
	evs := eventlogtest.Capture(t)
	boom := errors.New("bd down")
	f, calls := stubAbort(t, "in_progress", []int{7}, nil)
	f.FailOn("AddLabels", "mc-a", boom)
	if _, _, err := Abort("mc-a", "cli"); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the pause error", err)
	}
	if len(*calls) != 0 || len(*evs) != 0 {
		t.Errorf("calls %v events %v, want nothing signalled or logged", *calls, *evs)
	}
}

// Success orders pause → effort.aborted {by, pids, paused} → one kill per pid,
// returning the signalled pids; a vanished pid (ESRCH) is skipped, not an error.
func TestAbortPausesLogsThenKills(t *testing.T) {
	evs := eventlogtest.Capture(t)
	f, calls := stubAbort(t, "in_progress", []int{7, 8, 9}, map[int]error{8: syscall.ESRCH})
	got, _, err := Abort("mc-a", "tui")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []int{7, 9}) {
		t.Errorf("signalled = %v, want [7 9] (8 vanished)", got)
	}
	if want := []string{"pids paused=true", "kill", "kill", "kill"}; !slices.Equal(*calls, want) {
		t.Errorf("calls = %v, want %v", *calls, want)
	}
	if !bd.PausedBy(f.Bead("mc-a"), bd.PausedManual) {
		t.Error("mc-a must end paused:manual")
	}
	if names := eventlogtest.Names(*evs); !slices.Equal(names, []string{"effort.aborted"}) {
		t.Fatalf("events = %v, want one effort.aborted", names)
	}
	a := (*evs)[0].Attrs
	if a["bead"] != "mc-a" || a["by"] != "tui" || a["paused"] != true || !slices.Equal(a["pids"].([]int), []int{7, 8, 9}) {
		t.Errorf("effort.aborted = %v", a)
	}
}

// No live process: the bead is paused and nothing is signalled.
func TestAbortNoPidsPausesOnly(t *testing.T) {
	eventlogtest.Capture(t)
	f, calls := stubAbort(t, "in_progress", nil, nil)
	got, _, err := Abort("mc-a", "cli")
	if err != nil || len(got) != 0 {
		t.Fatalf("Abort = %v, %v, want no pids, no error", got, err)
	}
	if !slices.Equal(*calls, []string{"pids paused=true"}) || !bd.PausedBy(f.Bead("mc-a"), bd.PausedManual) {
		t.Errorf("calls = %v, want paused, then the pid scan only", *calls)
	}
}

// A dispatcher-only hold (a held lock, no pid): paused, nothing signalled, not
// refused, and the result says held (ADR 0155).
func TestAbortNoPidsLockHeldPausesAndReportsHeld(t *testing.T) {
	eventlogtest.Capture(t)
	f, calls := stubAbort(t, "in_progress", nil, nil)
	abortHeld = func(string) bool { return true }
	got, held, err := Abort("mc-a", "cli")
	if err != nil || len(got) != 0 || !held {
		t.Fatalf("Abort = %v, %v, %v, want no pids, held, no error", got, held, err)
	}
	if !slices.Equal(*calls, []string{"pids paused=true"}) || !bd.PausedBy(f.Bead("mc-a"), bd.PausedManual) {
		t.Errorf("calls = %v, want paused, then the pid scan only", *calls)
	}
}

// A failed effort's Outcome carries Aborted from release's one re-read: an aborted
// bead (paused:manual) reads true so the loop doesn't count it (ADR 0083 §8), on
// both the route and the run failure exits.
func TestFailedOutcomeReportsManualPause(t *testing.T) {
	for _, b := range []bd.Bead{{ID: "r1", Kind: contract.KindResearch}, {ID: "sp1", Kind: contract.KindSpec}} {
		for _, labels := range [][]string{{"paused:manual"}, nil} {
			bdtest.New(t, bdtest.Claimed(bd.Bead{ID: b.ID, Kind: b.Kind, Labels: labels}))
			o, err := Dispatch(nil, errCrew{errors.New("signal: terminated")}, b, io.Discard, NewEffortID(), nil)
			if err == nil || o.Aborted != (labels != nil) {
				t.Errorf("%s labels %v: Aborted = %v, err %v", b.ID, labels, o.Aborted, err)
			}
		}
	}
}

// A kill that fails for a reason other than ESRCH (EPERM) is returned, after the
// other pids are still signalled, so the CLI exits non-zero beside a survivor.
func TestAbortReturnsOtherKillErrors(t *testing.T) {
	eventlogtest.Capture(t)
	stubAbort(t, "in_progress", []int{7, 8}, map[int]error{7: syscall.EPERM})
	got, _, err := Abort("mc-a", "cli")
	if !errors.Is(err, syscall.EPERM) || !slices.Equal(got, []int{8}) {
		t.Errorf("Abort = %v, %v, want [8] and EPERM", got, err)
	}
}

// `mugicrew abort <id>` itself carries <id> as an argv token, so the liveness
// scan finds the verb's own pid: Abort must neither log nor signal it (its
// process group is the caller's shell or agent), only the effort's.
func TestAbortExcludesItsOwnPid(t *testing.T) {
	evs := eventlogtest.Capture(t)
	self := os.Getpid()
	var killed []int
	stubAbort(t, "in_progress", []int{7, self, 9}, nil)
	killGroup = func(pid int) error { killed = append(killed, pid); return nil }
	got, _, err := Abort("mc-a", "cli")
	if err != nil || !slices.Equal(got, []int{7, 9}) || !slices.Equal(killed, []int{7, 9}) {
		t.Fatalf("Abort = %v, %v, killed %v, want [7 9] without self %d", got, err, killed, self)
	}
	if pids := (*evs)[0].Attrs["pids"].([]int); !slices.Equal(pids, []int{7, 9}) {
		t.Errorf("effort.aborted pids = %v, want [7 9] without self %d", pids, self)
	}
}
