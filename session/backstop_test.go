package session

import (
	"errors"
	"io"
	"slices"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
)

// The backstop runs the integrate verb with no agent after an effort and maps its
// exit (Spec D6, FR 15–19, 23): 0 deletes the effort branch and the tree;
// a conflict aborts the rebase, pauses integrate-conflict, reopens a closed bead
// and keeps both; a transient keeps the branch, drops the tree and reopens a closed
// bead so the next tick retries it; a failed effort skips the integrate, keeps the
// branch and drops the tree. A transient exit over a rebase stopped in the tree
// (an agent's, on a detached HEAD the verb can't read) is a conflict, so the
// teardown never drops it mid-rebase. A bead a confirm sweep closed out from under
// the effort (it or its container closed obsolete:) is a failed effort whatever
// the integrate would say: never integrated, reopened or paused; a sweep that lands
// during the integrate is honoured too. A container closed any other way (an empty
// Surgery's abandoned close) is no sweep: the integrate runs. kept says the effort
// branch outlives the backstop.
func TestBackstopMapsIntegrateOutcome(t *testing.T) {
	cases := []struct {
		name     string
		failed   bool
		exit     int
		rebasing bool
		status   string
		cpStatus string
		sweep    string // "": none; "member" or "container": the bead or ws-cp closed obsolete: before the backstop; "during": both, while the integrate runs
		stranded bool
		kept     bool
		want     []string // what the backstop did, in order
	}{
		{"integrated", false, 0, false, "closed", "open", "", false, false, []string{"integrate", "teardown", "delete effort/mc-b"}},
		{"conflict on a closed bead", false, 2, true, "closed", "open", "", true, true, []string{"integrate", "abort /wt", "pause mc-b integrate-conflict", "reopen mc-b"}},
		{"conflict on an open bead", false, 2, true, "in_progress", "open", "", true, true, []string{"integrate", "abort /wt", "pause mc-b integrate-conflict"}},
		{"held or landed", false, 2, false, "in_progress", "open", "", true, true, []string{"integrate", "abort /wt", "pause mc-b integrate-conflict"}},
		{"transient on a closed bead", false, 1, false, "closed", "open", "", false, true, []string{"integrate", "abort /wt", "teardown", "reopen mc-b"}},
		{"transient on an open bead", false, 1, false, "in_progress", "open", "", false, true, []string{"integrate", "abort /wt", "teardown"}},
		{"transient mid-rebase", false, 1, true, "in_progress", "open", "", true, true, []string{"integrate", "abort /wt", "pause mc-b integrate-conflict"}},
		{"failed effort", true, 0, false, "in_progress", "open", "", false, true, []string{"teardown"}},
		{"swept, transient", false, 1, false, "closed", "closed", "member", false, true, []string{"teardown"}},
		{"swept, conflict", false, 2, true, "closed", "closed", "container", false, true, []string{"teardown"}},
		{"swept, clean", false, 0, false, "closed", "closed", "container", false, true, []string{"teardown"}},
		{"swept member, container still open", false, 1, false, "closed", "open", "member", false, true, []string{"teardown"}},
		{"empty surgery closed abandoned", false, 0, false, "closed", "closed", "", false, false, []string{"integrate", "teardown", "delete effort/mc-b"}},
		{"swept during a transient", false, 1, false, "closed", "open", "during", false, true, []string{"integrate", "abort /wt", "teardown"}},
		{"swept during a conflict", false, 2, true, "closed", "open", "during", false, true, []string{"integrate", "abort /wt", "teardown"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			evs := eventlogtest.Capture(t)
			var did []string
			stubBackstop(t, &did, c.exit, c.rebasing, c.status, c.cpStatus, c.sweep)
			b := bd.Bead{ID: "mc-b", Kind: contract.KindBuild, Parent: "ws-cp"}
			stranded, kept := Backstop(b, "/wt", c.failed, func() { did = append(did, "teardown") }, io.Discard)
			if stranded != c.stranded || kept != c.kept {
				t.Errorf("stranded, kept = %v, %v; want %v, %v", stranded, kept, c.stranded, c.kept)
			}
			if !slices.Equal(did, c.want) {
				t.Errorf("did %q, want %q", did, c.want)
			}
			paused := slices.Contains(eventlogtest.Names(*evs), "effort.paused")
			if paused != c.stranded {
				t.Errorf("events %v: effort.paused logged %v, want %v", eventlogtest.Names(*evs), paused, c.stranded)
			}
		})
	}
}

// stubBackstop scripts the backstop's integrate exit, whether a rebase is stopped
// in the tree, the bead's and its container ws-cp's status and which of them a
// sweep closed obsolete: (see the sweep column), and records every git and bd write in *did.
func stubBackstop(t *testing.T, did *[]string, exit int, rebasing bool, status, cpStatus, sweep string) {
	t.Helper()
	origI, origA, origP, origS, origR, origD := integrateIn, abortRebase, pauseBead, showBead, reopenBead, deleteBranch
	t.Cleanup(func() {
		integrateIn, abortRebase, pauseBead, showBead, reopenBead, deleteBranch = origI, origA, origP, origS, origR, origD
	})
	reason, cpReason := "Closed", "abandoned: surgery ended with no commits"
	switch sweep {
	case "member":
		reason = bd.ObsoleteReason + " x"
	case "container":
		cpReason = bd.ObsoleteReason + " x"
	}
	integrateIn = func(string, io.Writer) int {
		*did = append(*did, "integrate")
		if sweep == "during" {
			cpStatus, cpReason = "closed", bd.ObsoleteReason+" x"
		}
		return exit
	}
	abortRebase = func(wt string) error {
		*did = append(*did, "abort "+wt)
		if !rebasing {
			return errors.New("no rebase in progress")
		}
		return nil
	}
	pauseBead = func(id, cause string) error { *did = append(*did, "pause "+id+" "+cause); return nil }
	showBead = func(id string) (bd.Bead, error) {
		if id == "ws-cp" {
			return bd.Bead{ID: id, Status: cpStatus, CloseReason: cpReason}, nil
		}
		return bd.Bead{ID: id, Status: status, Parent: "ws-cp", CloseReason: reason}, nil
	}
	reopenBead = func(id string) error { *did = append(*did, "reopen "+id); return nil }
	deleteBranch = func(name string) error { *did = append(*did, "delete "+name); return nil }
}
