package jinbe

import (
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
)

// pendingRevert is a gitlab Rollback whose revert is pinned, pushed and open as !9.
func pendingRevert(mr forge.MR) *rollbackRec {
	r := gitlabRollback()
	r.body += "\nCommit: pinsha\nMR: !9\nChecked: forgesha\n"
	mr.IID = 9
	r.mr = &mr
	return r
}

func tick(t *testing.T, r *rollbackRec) {
	t.Helper()
	if err := reconcileRollback(r.rollback()); err != nil {
		t.Fatal(err)
	}
}

func eventNamed(evs []eventlogtest.Event, name string) eventlogtest.Event {
	for _, e := range evs {
		if e.Name == name {
			return e
		}
	}
	return eventlogtest.Event{}
}

func TestRevertMergedWithoutTrailerHoldsOnATask(t *testing.T) {
	r := pendingRevert(forge.MR{State: "merged", MergeCommitSHA: "m1"})
	stubRollbackSeams(t, r)
	if err := reconcileRollback(r.rollback()); err == nil {
		t.Fatal("want the tick to fail while the trailer is missing")
	}
	if !slices.Equal(r.tasks, []string{"ws-rb|Merged MR: !9\n"}) || !strings.Contains(r.taskBody, "Reverts-Checkpoint: ws-cp") {
		t.Errorf("tasks %q body %q, want one Task blocking the Rollback naming the trailer", r.tasks, r.taskBody)
	}
	if len(r.closed) != 0 || r.fetches != 2 {
		t.Errorf("closed %v fetches %d, want an open Rollback after a second fetch", r.closed, r.fetches)
	}
}

func TestRevertHumanCloseAbandons(t *testing.T) {
	r := pendingRevert(forge.MR{State: "closed"})
	stubRollbackSeams(t, r)
	evs := eventlogtest.Capture(t)
	tick(t, r)
	if r.reasons["ws-rb"] != "abandoned: revert MR closed" || len(r.successors) != 0 {
		t.Errorf("reasons %v successors %v, want abandoned with no successor", r.reasons, r.successors)
	}
	if e := eventNamed(*evs, "revert.closed"); e.Attrs["mr"] != 9 {
		t.Errorf("revert.closed = %v, want mr=9", e)
	}
}

func TestRevertCloseAfterSuccessorResumesFixForward(t *testing.T) {
	r := pendingRevert(forge.MR{State: "closed"})
	r.successors = []bd.Successor{{Description: git.ConflictPin(&git.RevertConflict{Base: "forgesha", Paths: []string{"a.go"}})}}
	stubRollbackSeams(t, r)
	tick(t, r)
	if r.reasons["ws-rb"] != bd.FixForwardReason+" revert conflicted" || r.reasons["ws-rb"] == "abandoned: revert MR closed" {
		t.Errorf("reasons %v, want the fix-forward close, not an abandon", r.reasons)
	}
}

func TestRevertCleanRebuildRepinsAndForcePushes(t *testing.T) {
	r := pendingRevert(forge.MR{State: "opened", HasConflicts: true})
	r.body = strings.Replace(r.body, "Checked: forgesha", "Checked: oldbase", 1) // pin built on an older main
	stubRollbackSeams(t, r)
	revertOnto = func(string, string, string) (string, error) { return "newsha", nil }
	evs := eventlogtest.Capture(t)
	tick(t, r)
	if want := []string{"ws-rb Reverts-Checkpoint: ws-cp newsha"}; !slices.Equal(r.pushes, want) {
		t.Errorf("pushes = %q, want %q", r.pushes, want)
	}
	if !strings.Contains(r.body, "Commit: newsha") || strings.Contains(r.body, "Commit: pinsha") {
		t.Errorf("body = %q, want the pin replaced", r.body)
	}
	if e := eventNamed(*evs, "revert.rebuilt"); e.Attrs["mr"] != 9 || e.Attrs["old"] != "pinsha" || e.Attrs["new"] != "newsha" {
		t.Errorf("revert.rebuilt = %v", e)
	}
	if len(r.closed)+len(r.successors) != 0 {
		t.Errorf("closed %v successors %v, want the Rollback pending", r.closed, r.successors)
	}
}

// A lingering Forge flag on a pin already built on the fetched main is pending, not a
// rebuild: no new SHA, no push, however many ticks.
func TestRevertLingeringFlagOnCurrentPinDoesNotRebuild(t *testing.T) {
	for name, mr := range map[string]forge.MR{
		"has_conflicts": {State: "opened", HasConflicts: true},
		"merge_error":   {State: "opened", MergeError: "boom"},
	} {
		t.Run(name, func(t *testing.T) {
			r := pendingRevert(mr)
			stubRollbackSeams(t, r)
			revertOnto = func(string, string, string) (string, error) { return "newsha", nil }
			evs := eventlogtest.Capture(t)
			tick(t, r)
			tick(t, r)
			if len(r.pushes) != 0 || strings.Contains(r.body, "newsha") || len(r.closed)+len(r.successors) != 0 {
				t.Errorf("pushes %v body %q, want no rebuild", r.pushes, r.body)
			}
			if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"revert.pending", "revert.pending"}) {
				t.Errorf("events = %v", got)
			}
		})
	}
}

func TestRevertConflictedRebuildFixesForwardThenClosesMR(t *testing.T) {
	for name, mr := range map[string]forge.MR{
		"forge has_conflicts": {State: "opened", HasConflicts: true},
		"merge_error":         {State: "opened", MergeError: "boom"},
	} {
		t.Run(name, func(t *testing.T) {
			r := pendingRevert(mr)
			stubRollbackSeams(t, r)
			revertOnto = func(string, string, string) (string, error) {
				return "", &git.RevertConflict{Base: "forgesha", Paths: []string{"a.go"}}
			}
			evs := eventlogtest.Capture(t)
			tick(t, r)
			if r.branches["checkpoint/ws-new"] != "forgesha" || r.seeds != 0 {
				t.Errorf("branches %v seeds %d, want the successor cut at the fetched main unseeded", r.branches, r.seeds)
			}
			if !slices.Equal(r.mrClosed, []int{9}) || !slices.Equal(r.closedAtSet, []string{"1"}) {
				t.Errorf("MR closes %v with %v successors, want !9 closed after the successor", r.mrClosed, r.closedAtSet)
			}
			if r.reasons["ws-rb"] != bd.FixForwardReason+" revert conflicted" {
				t.Errorf("reasons %v", r.reasons)
			}
			if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"revert.conflict", "handoff.done", "revert.mr_closed"}) {
				t.Errorf("events = %v", got)
			}
			if e := eventNamed(*evs, "revert.conflict"); e.Attrs["paths"] != "a.go" || e.Attrs["mr"] != 9 {
				t.Errorf("revert.conflict = %v, want paths and mr", e)
			}
		})
	}
}

// A moved main with no Forge conflict flag is tried locally: a conflict fixes forward, a
// clean trial records the base and touches nothing.
func TestRevertMovedMainTrial(t *testing.T) {
	moved := func() *rollbackRec {
		r := pendingRevert(forge.MR{State: "opened"})
		r.body = strings.Replace(r.body, "Checked: forgesha", "Checked: oldbase", 1)
		return r
	}
	r := moved()
	stubRollbackSeams(t, r)
	revertOnto = func(string, string, string) (string, error) { return "", &git.RevertConflict{Base: "forgesha"} }
	tick(t, r)
	if r.reasons["ws-rb"] != bd.FixForwardReason+" revert conflicted" {
		t.Errorf("reasons %v, want a fix-forward", r.reasons)
	}
	r = moved()
	stubRollbackSeams(t, r)
	tick(t, r)
	if len(r.pushes) != 0 || !strings.Contains(r.body, "Checked: forgesha") || len(r.closed) != 0 {
		t.Errorf("pushes %v body %q closed %v, want a clean trial to record the base only", r.pushes, r.body, r.closed)
	}
}

func TestRevertRedPipelineRetriesOnceThenHolds(t *testing.T) {
	for _, infra := range []bool{false, true} {
		reason := "script_failure"
		if infra {
			reason = "runner_system_failure"
		}
		r := pendingRevert(forge.MR{State: "opened", SHA: "pinsha", HeadPipeline: &forge.Pipeline{ID: 42, Status: "failed", SHA: "pinsha", WebURL: "https://gl/p/42", UpdatedAt: "t1"}})
		r.jobs = []forge.Job{{ID: 5, Name: "test", FailureReason: reason}}
		stubRollbackSeams(t, r)
		evs := eventlogtest.Capture(t)
		tick(t, r) // retry
		if !slices.Equal(r.retries, []int{42}) || !strings.Contains(r.body, "Retried: pipeline 42") || len(r.tasks) != 0 {
			t.Fatalf("retries %v body %q tasks %v, want one retry recorded", r.retries, r.body, r.tasks)
		}
		tick(t, r) // failed again: hold
		tick(t, r) // held: nothing new
		if !slices.Equal(r.retries, []int{42}) || !slices.Equal(r.tasks, []string{"ws-rb|Pipeline: 42\n"}) {
			t.Errorf("infra=%v retries %v tasks %v, want one retry and one Task", infra, r.retries, r.tasks)
		}
		if !strings.Contains(r.taskBody, "https://gl/p/42") || !strings.Contains(r.taskBody, "tail of job 5") {
			t.Errorf("task body = %q, want the URL and the job tail", r.taskBody)
		}
		if e := eventNamed(*evs, "revert.pipeline_failed"); e.Attrs["infra"] != infra || e.Attrs["pipeline"] != 42 || e.Attrs["mr"] != 9 || e.Attrs["task"] != "ws-task" {
			t.Errorf("revert.pipeline_failed = %v", e)
		}
		if e := eventNamed(*evs, "revert.retried"); e.Attrs["pipeline"] != 42 || e.Attrs["mr"] != 9 {
			t.Errorf("revert.retried = %v", e)
		}
		if len(r.closed) != 0 {
			t.Errorf("closed %v, want the Rollback held open", r.closed)
		}
	}
}

// A held Rollback hands off once the merge shows, and abandons when the MR is closed.
func TestRevertHeldRollbackResolves(t *testing.T) {
	held := func() *rollbackRec {
		r := pendingRevert(forge.MR{State: "opened"})
		r.body += "Retried: pipeline 42\nHeld: pipeline 42 t1\n"
		return r
	}
	r := held()
	r.trailerLanded, r.mergedAt = true, "msha"
	stubRollbackSeams(t, r)
	tick(t, r)
	if len(r.successors) != 1 || len(r.reasons) != 1 || r.reasons["ws-rb"] != "" {
		t.Errorf("successors %v reasons %v, want a handoff", r.successors, r.reasons)
	}
	r = held()
	r.mr.State = "closed"
	stubRollbackSeams(t, r)
	tick(t, r)
	if r.reasons["ws-rb"] != "abandoned: revert MR closed" {
		t.Errorf("reasons %v, want abandoned", r.reasons)
	}
}

// A first build that conflicts has no MR to rebuild: it fixes forward like a rebuild would.
func TestRevertFirstBuildConflictFixesForward(t *testing.T) {
	r := gitlabRollback()
	stubRollbackSeams(t, r)
	revertOnto = func(string, string, string) (string, error) {
		return "", &git.RevertConflict{Base: "forgesha", Paths: []string{"a.go"}}
	}
	evs := eventlogtest.Capture(t)
	tick(t, r)
	if r.branches["checkpoint/ws-new"] != "forgesha" || r.seeds != 0 {
		t.Errorf("branches %v seeds %d, want the successor cut at the fetched main unseeded", r.branches, r.seeds)
	}
	if r.reasons["ws-rb"] != bd.FixForwardReason+" revert conflicted" {
		t.Errorf("reasons %v", r.reasons)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"revert.conflict", "handoff.done"}) {
		t.Errorf("events = %v", got)
	}
	if len(r.pushes) != 0 || len(r.mrClosed) != 0 {
		t.Errorf("pushes %v mrClosed %v, want none", r.pushes, r.mrClosed)
	}
}

// A human closing the revert MR mid-rebuild makes the push open a second MR: that one is
// closed and the Rollback keeps the old MR:, so the next tick abandons with none open.
func TestRevertRebuildOpeningFreshMRClosesItAndAbandonsNextTick(t *testing.T) {
	r := pendingRevert(forge.MR{State: "opened", HasConflicts: true})
	r.body = strings.Replace(r.body, "Checked: forgesha", "Checked: oldbase", 1)
	stubRollbackSeams(t, r)
	revertOnto = func(string, string, string) (string, error) { return "newsha", nil }
	pushRevertMR = func(git.Repo, string, string, string) (*git.LandPending, error) {
		return &git.LandPending{MR: 12, Pushed: true, Opened: true}, nil
	}
	evs := eventlogtest.Capture(t)
	tick(t, r)
	if !slices.Equal(r.mrClosed, []int{12}) || !strings.Contains(r.body, "MR: !9") {
		t.Errorf("closed %v body %q, want the fresh !12 closed and MR: !9 kept", r.mrClosed, r.body)
	}
	if e := eventNamed(*evs, "revert.mr_closed"); e.Attrs["mr"] != 12 {
		t.Errorf("revert.mr_closed = %v, want mr=12", e)
	}
	r.mr.State = "closed"
	tick(t, r)
	if r.reasons["ws-rb"] != "abandoned: revert MR closed" {
		t.Errorf("reasons %v, want abandoned", r.reasons)
	}
}

// A rebuild whose push failed after re-pinning leaves Commit: ahead of the MR head, with
// Checked: already on the fetched main: the next tick pushes the pin, it does not rebuild.
func TestRevertUnpushedPinIsPushedToTheSameMR(t *testing.T) {
	r := pendingRevert(forge.MR{State: "opened", HasConflicts: true, SHA: "pinsha"})
	r.body = strings.Replace(r.body, "Commit: pinsha", "Commit: newsha\nRebuilt-from: pinsha", 1)
	stubRollbackSeams(t, r)
	tick(t, r)
	if want := []string{"ws-rb Reverts-Checkpoint: ws-cp newsha"}; !slices.Equal(r.pushes, want) {
		t.Errorf("pushes = %q, want %q", r.pushes, want)
	}
	if strings.Contains(r.body, "Commit: pinsha") || len(r.mrClosed) != 0 {
		t.Errorf("body %q closed %v, want no rebuild", r.body, r.mrClosed)
	}
}

// A human's Forge-side Rebase moves the MR head off the pin; that is not a stranded push
// (ADR 0189): the pending tick neither pushes nor rebuilds.
func TestRevertForgeRebaseStaysPending(t *testing.T) {
	r := pendingRevert(forge.MR{State: "opened", SHA: "rebasedsha"})
	stubRollbackSeams(t, r)
	evs := eventlogtest.Capture(t)
	tick(t, r)
	if len(r.pushes) != 0 || len(r.mrClosed) != 0 || !strings.Contains(r.body, "Commit: pinsha") {
		t.Errorf("pushes %v closed %v body %q, want none", r.pushes, r.mrClosed, r.body)
	}
	if eventNamed(*evs, "revert.pending").Name == "" {
		t.Errorf("events = %v, want revert.pending", eventlogtest.Names(*evs))
	}
}
