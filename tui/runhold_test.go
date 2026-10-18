package tui

import (
	"io"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/dispatch"
	"jayulabs.org/mugicrew/liveness"
)

// run-now carries the claim's hold on its Inflight and drops it only after
// Dispatch returns (ADR 0154).
func TestRunNowCmdReleasesHoldAfterDispatch(t *testing.T) {
	t.Setenv("BEADS_DIR", t.TempDir())
	orig := runDispatch
	defer func() { runDispatch = orig }()
	b := bd.Bead{ID: "mc-r"}
	hold, err := liveness.Acquire(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Release()
	runDispatch = func(contract.Ctx, contract.Crewmate, bd.Bead, io.Writer, string, dispatch.InflightFunc) (dispatch.Outcome, error) {
		if !liveness.Held(b.ID) {
			t.Error("effort reads dead inside Dispatch")
		}
		return dispatch.Outcome{}, nil
	}
	runNowCmd(nil, nil, dispatch.Inflight{ID: "e1", Bead: b, Hold: hold})()
	if liveness.Held(b.ID) {
		t.Error("hold still held after the run-now Cmd ended")
	}
}
