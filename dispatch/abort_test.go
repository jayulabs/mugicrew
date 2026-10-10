package dispatch

import (
	"errors"
	"io"
	"os"
	"slices"
	"syscall"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
)

// stubAbort stubs Abort's four seams, recording each call in order into *calls.
func stubAbort(t *testing.T, status string, pauseErr error, pids []int, killErr map[int]error) *[]string {
	t.Helper()
	oShow, oPause, oPids, oKill := show, abortPause, abortPids, killGroup
	t.Cleanup(func() { show, abortPause, abortPids, killGroup = oShow, oPause, oPids, oKill })
	var calls []string
	show = func(id string) (bd.Bead, error) { return bd.Bead{ID: id, Status: status}, nil }
	abortPause = func(id, cause string) error { calls = append(calls, "pause "+id+" "+cause); return pauseErr }
	abortPids = func(string) []int { calls = append(calls, "pids"); return pids }
	killGroup = func(pid int) error { calls = append(calls, "kill"); return killErr[pid] }
	return &calls
}

// A bead not in_progress is refused with "nothing in flight": no pause, no
// event, no signal (ADR 0083 §4).
func TestAbortRefusesNothingInFlight(t *testing.T) {
	evs := eventlogtest.Capture(t)
	calls := stubAbort(t, "open", nil, []int{7}, nil)
	if _, err := Abort("mc-a", "cli"); err == nil || err.Error() != "mc-a: nothing in flight" {
		t.Fatalf("err = %v, want nothing in flight", err)
	}
	if len(*calls) != 0 || len(*evs) != 0 {
		t.Errorf("a refusal writes nothing, got calls %v events %v", *calls, *evs)
	}
}

// A failed pause write returns the bd error with nothing logged or signalled:
// a kill without the pause would be re-dispatched (ADR 0083 §5).
func TestAbortPauseErrorSignalsNothing(t *testing.T) {
	evs := eventlogtest.Capture(t)
	boom := errors.New("bd down")
	calls := stubAbort(t, "in_progress", boom, []int{7}, nil)
	if _, err := Abort("mc-a", "cli"); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the pause error", err)
	}
	if !slices.Equal(*calls, []string{"pause mc-a manual"}) || len(*evs) != 0 {
		t.Errorf("calls %v events %v, want the pause only", *calls, *evs)
	}
}

// Success orders pause → effort.aborted {by, pids, paused} → one kill per pid,
// returning the signalled pids; a vanished pid (ESRCH) is skipped, not an error.
func TestAbortPausesLogsThenKills(t *testing.T) {
	evs := eventlogtest.Capture(t)
	calls := stubAbort(t, "in_progress", nil, []int{7, 8, 9}, map[int]error{8: syscall.ESRCH})
	got, err := Abort("mc-a", "tui")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []int{7, 9}) {
		t.Errorf("signalled = %v, want [7 9] (8 vanished)", got)
	}
	if want := []string{"pause mc-a manual", "pids", "kill", "kill", "kill"}; !slices.Equal(*calls, want) {
		t.Errorf("calls = %v, want %v", *calls, want)
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
	calls := stubAbort(t, "in_progress", nil, nil, nil)
	got, err := Abort("mc-a", "cli")
	if err != nil || len(got) != 0 {
		t.Fatalf("Abort = %v, %v, want no pids, no error", got, err)
	}
	if !slices.Equal(*calls, []string{"pause mc-a manual", "pids"}) {
		t.Errorf("calls = %v, want pause then pids only", *calls)
	}
}

// A failed effort's Outcome carries Aborted from release's one re-read: an aborted
// bead (paused:manual) reads true so the loop doesn't count it (ADR 0083 §8), on
// both the route and the run failure exits.
func TestFailedOutcomeReportsManualPause(t *testing.T) {
	origShow := show
	t.Cleanup(func() { show = origShow })
	for _, b := range []bd.Bead{{ID: "r1", Kind: contract.KindResearch}, {ID: "sp1", Kind: contract.KindSpec}} {
		for _, labels := range [][]string{{"paused:manual"}, nil} {
			show = func(string) (bd.Bead, error) { return bd.Bead{Status: "in_progress", Labels: labels}, nil }
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
	stubAbort(t, "in_progress", nil, []int{7, 8}, map[int]error{7: syscall.EPERM})
	got, err := Abort("mc-a", "cli")
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
	stubAbort(t, "in_progress", nil, []int{7, self, 9}, nil)
	killGroup = func(pid int) error { killed = append(killed, pid); return nil }
	got, err := Abort("mc-a", "cli")
	if err != nil || !slices.Equal(got, []int{7, 9}) || !slices.Equal(killed, []int{7, 9}) {
		t.Fatalf("Abort = %v, %v, killed %v, want [7 9] without self %d", got, err, killed, self)
	}
	if pids := (*evs)[0].Attrs["pids"].([]int); !slices.Equal(pids, []int{7, 9}) {
		t.Errorf("effort.aborted pids = %v, want [7 9] without self %d", pids, self)
	}
}
