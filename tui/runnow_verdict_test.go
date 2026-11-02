package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/liveness"
	"jayulabs.org/mugicrew/loop"
	"jayulabs.org/mugicrew/registry"
)

// pressView is a loop.View over fixed beads: kids by Container id, landed by id, a
// checkpoint tip, and the beads it resolves by id.
type pressView struct {
	kids    map[string][]bd.Bead
	landed  map[string]loop.Trailer
	tip     string
	beads   map[string]bd.Bead
	blocked map[string][]bd.Bead
}

func (v pressView) Children(id string) ([]bd.Bead, error)          { return v.kids[id], nil }
func (v pressView) Efforts() (*git.EffortSet, error)               { return git.NewEffortSet(), nil }
func (v pressView) Landed(b bd.Bead) (loop.Trailer, error)         { return v.landed[b.ID], nil }
func (v pressView) SameRepo(_, _ bd.Bead) bool                     { return true }
func (v pressView) CheckpointTip(string) (string, error)           { return v.tip, nil }
func (v pressView) Contains(bd.Bead, string, string) (bool, error) { return true, nil }
func (v pressView) Bead(id string) (bd.Bead, error) {
	if b, ok := v.beads[id]; ok {
		return b, nil
	}
	return bd.Bead{}, errors.New("no bead " + id)
}
func (v pressView) Blockers(b bd.Bead) ([]loop.Blocker, error) {
	var out []loop.Blocker
	for _, x := range v.blocked[b.ID] {
		out = append(out, loop.Blocker{Bead: x, Edge: loop.EdgeBlocks})
	}
	return out, nil
}

// pressNow runs claimRunNow's eligibility over view with ready as the frontier and returns
// its status; a runnable verdict falls through to the "no crewmate" refusal.
func pressNow(t *testing.T, b bd.Bead, ready []bd.Bead, view loop.View, force bool) string {
	t.Helper()
	seam(t, &readyFrontier, func() ([]bd.Bead, error) { return ready, nil })
	seam(t, &openBeads, func() ([]bd.Bead, error) { return nil, nil })
	seam(t, &inProgressBeads, func() ([]bd.Bead, error) { return nil, nil })
	seam(t, &pausedBeads, func() ([]bd.Bead, error) { return nil, nil })
	seam(t, &newPressView, func(func() *git.EffortSet, ...[]bd.Bead) loop.View { return view })
	status, crew, _ := claimRunNow(nil, b, nil, force)
	if crew != nil {
		t.Fatalf("press claimed %s", b.ID)
	}
	return status
}

const eligible = "no crewmate"

func TestPressRefusesWithTheVerdictsCause(t *testing.T) {
	cp := bd.Bead{ID: "cp", Kind: contract.KindCheckpoint, Status: "open"}
	chart := bd.Bead{ID: "ch", Kind: contract.KindChart, Status: "open", Labels: []string{"defogged"}}
	if !loop.IsDrainedChartCandidate(chart) {
		t.Skip("fixture is not a drained Chart candidate")
	}
	foreign := &git.ForeignLanding{Sha: "f00d", Theirs: "a", Ours: "b", Trailer: "Chart: ch"}
	leaf := func(labels ...string) bd.Bead {
		return bd.Bead{ID: "r1", Kind: contract.KindResearch, Status: "open", Labels: labels}
	}
	crit := bd.Bead{ID: "cp.2", Kind: contract.KindCritique, Status: "open", Parent: "cp"}
	surg := bd.Bead{ID: "cp.9", Kind: contract.KindSurgery, Status: "in_progress", Parent: "cp"}
	cases := []struct {
		name  string
		b     bd.Bead
		ready []bd.Bead
		view  pressView
		force bool
		say   string
	}{
		{"paused checkup", leaf("paused:checkup"), nil, pressView{}, false, "paused:checkup"},
		{"unknown pause cause", leaf("paused:whatever"), nil, pressView{}, false, "paused:whatever"},
		{"manual pause", leaf("paused:manual"), nil, pressView{}, false, "paused by hand"},
		{"retry passes", leaf("paused:failing"), []bd.Bead{leaf("paused:failing")}, pressView{}, false, eligible},
		{"blocked", leaf(), nil, pressView{}, false, "blocked / not ready"},
		{"force waives blocked", leaf(), nil, pressView{}, true, eligible},
		{"surgery hold", crit, []bd.Bead{crit}, pressView{kids: map[string][]bd.Bead{"cp": {surg}}, beads: map[string]bd.Bead{"cp": cp}}, false, "held by open Surgery cp.9"},
		{"landing open on drained chart", chart, []bd.Bead{chart},
			pressView{kids: map[string][]bd.Bead{"ch": {{ID: "ch.1", Kind: contract.KindLanding, Status: "open"}}}}, false, "Landing is already open"},
		{"foreign chart", chart, []bd.Bead{chart},
			pressView{landed: map[string]loop.Trailer{"ch": {Foreign: foreign}}}, false, foreign.Error()},
		{"foreign checkpoint", cp, []bd.Bead{cp},
			pressView{landed: map[string]loop.Trailer{"cp": {Foreign: foreign}}}, false, foreign.Error()},
		{"defect open", cp, []bd.Bead{cp},
			pressView{kids: map[string][]bd.Bead{"cp": {{ID: "cp.3", Kind: contract.KindCritique, Status: "deferred"}}}}, false, "open conflict/critique: cp.3"},
		{"landed trailer rolls forward", cp, []bd.Bead{cp},
			pressView{landed: map[string]loop.Trailer{"cp": {Landed: true, Sha: "abc"}}}, false, eligible},
		{"review due refused in run-now", cp, []bd.Bead{cp},
			pressView{kids: map[string][]bd.Bead{"cp": {{ID: "cp.1", Kind: contract.KindBuild, Status: "closed"}}}}, false, "review due"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			seam(t, &holdForeign, func(bd.Bead, *git.ForeignLanding) error { return foreign })
			if got := pressNow(t, c.b, c.ready, c.view, c.force); !strings.Contains(got, c.say) {
				t.Errorf("status = %q, want it to contain %q", got, c.say)
			}
		})
	}
}

func TestPressFilesTheForeignHoldAndFailsOnItsError(t *testing.T) {
	cp := bd.Bead{ID: "cp", Kind: contract.KindCheckpoint, Status: "open"}
	foreign := &git.ForeignLanding{Sha: "f00d"}
	view := pressView{landed: map[string]loop.Trailer{"cp": {Foreign: foreign}}}
	var filed bd.Bead
	seam(t, &holdForeign, func(b bd.Bead, f *git.ForeignLanding) error { filed = b; return f })
	pressNow(t, cp, []bd.Bead{cp}, view, false)
	if filed.ID != "cp" {
		t.Errorf("hold filed for %q, want cp", filed.ID)
	}
	seam(t, &holdForeign, func(bd.Bead, *git.ForeignLanding) error { return errors.New("bd down") })
	if got := pressNow(t, cp, []bd.Bead{cp}, view, false); !strings.Contains(got, "bd down") {
		t.Errorf("status = %q, want the filing error", got)
	}
}

// The press stamps the setting from config before judging, so the AFK-Approval mode is
// exercised on the verdict the press reads.
func TestRunNowVerdictAdmitsAFKApprovalAndRefusesMarkedOne(t *testing.T) {
	plain := dbead("ap", contract.KindApproval, "open", "cp", 1)
	marked := dbead("mk", contract.KindApproval, "open", "cp", 1, labeled(registry.AutoApproveFailedLabel))
	ready := []bd.Bead{plain, marked}
	view := pressView{beads: map[string]bd.Bead{"cp": {ID: "cp", Kind: contract.KindCheckpoint, Status: "open"}}}
	judge := func(b bd.Bead) bool {
		v, err := loop.Judge(b, loop.ModeRunNow, ready, loop.Inputs{}, view)
		if err != nil {
			t.Fatal(err)
		}
		return v.Runnable
	}
	if judge(plain) {
		t.Error("setting off: run-now admitted an Approval as AFK")
	}
	autoApproveOn(t)
	if !judge(plain) {
		t.Error("setting on: run-now refused an AFK Approval")
	}
	if judge(marked) {
		t.Error("setting on: run-now admitted a marked Approval as AFK")
	}
}

// Every cause has wording: a new Cause with no case in causeHint fails here.
func TestCauseHintWordsEveryCause(t *testing.T) {
	h := loop.Holder{Bead: bd.Bead{ID: "h1", Title: "held"}}
	for _, c := range loop.Causes {
		v := loop.Verdict{Cause: c, PauseCause: "x", Holders: []loop.Holder{h}, Foreign: &git.ForeignLanding{Sha: "f"}}
		got := causeHint(bd.Bead{ID: "b1", Status: "open", Kind: contract.KindResearch}, v)
		if got == "" || strings.Contains(got, "("+string(c)+")") {
			t.Errorf("cause %q: hint %q is empty or the fallback", c, got)
		}
	}
}

func TestCauseHintNamesAChartsOpenApproval(t *testing.T) {
	chart := bd.Bead{ID: "ch", Status: "open", Kind: contract.KindChart}
	hold := func(kind contract.Kind, status, branch string) loop.Holder {
		return loop.Holder{Bead: bd.Bead{ID: string(kind) + status, Kind: kind, Status: status}, Branch: branch}
	}
	apOpen := hold(contract.KindApproval, "open", "")
	apOpen2 := loop.Holder{Bead: bd.Bead{ID: "ap2", Kind: contract.KindApproval, Status: "open"}}
	cases := []struct {
		name    string
		holders []loop.Holder
		want    string
	}{
		{"approval only", []loop.Holder{apOpen}, "ch: Chart not ready to land — Approval " + apOpen.Bead.ID + " is awaiting a human"},
		{"two approvals", []loop.Holder{apOpen, apOpen2}, "ch: Chart not ready to land — Approval " + apOpen.Bead.ID + ", ap2 is awaiting a human"},
		{"approval beside defog", []loop.Holder{apOpen, hold(contract.KindSpec, "open", "")}, "defog frontier hasn't cleared"},
		{"held child", []loop.Holder{hold(contract.KindSpec, "closed", "effort/specclosed")}, "effort/specclosed"},
		{"held child beside approval", []loop.Holder{apOpen, hold(contract.KindSpec, "closed", "effort/specclosed")}, "effort/specclosed"},
	}
	for _, c := range cases {
		got := causeHint(chart, loop.Verdict{Cause: loop.CauseUndrained, Holders: c.holders})
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: hint %q lacks %q", c.name, got, c.want)
		}
		if c.name != "approval only" && c.name != "two approvals" && strings.Contains(got, "awaiting a human") {
			t.Errorf("%s: hint %q should keep the old wording", c.name, got)
		}
	}
	cp := bd.Bead{ID: "cp", Status: "open", Kind: contract.KindCheckpoint}
	if got := causeHint(cp, loop.Verdict{Cause: loop.CauseUndrained, Holders: []loop.Holder{apOpen}}); !strings.Contains(got, "Checkpoint not drained") {
		t.Errorf("checkpoint undrained wording changed: %q", got)
	}
}

// Orphaned claims hold no pool slot (ADR 0391): with the whole pool in_progress,
// run-now refuses when a live host holds it and goes on to the claim when only
// orphans fill it, the loop's own CountInflight reading either way.
func TestRunNowPoolSkipsOrphans(t *testing.T) {
	b := bd.Bead{ID: "r1", Kind: contract.KindResearch, Status: "open"}
	var busy []bd.Bead
	for i := range loop.Pool(".") {
		busy = append(busy, bd.Bead{ID: fmt.Sprintf("busy%d", i), Kind: contract.KindBuild, Status: "in_progress"})
	}
	seam(t, &readyFrontier, func() ([]bd.Bead, error) { return []bd.Bead{b}, nil })
	seam(t, &openBeads, func() ([]bd.Bead, error) { return nil, nil })
	seam(t, &inProgressBeads, func() ([]bd.Bead, error) { return busy, nil })
	seam(t, &pausedBeads, func() ([]bd.Bead, error) { return nil, nil })
	seam(t, &newPressView, func(func() *git.EffortSet, ...[]bd.Bead) loop.View { return pressView{} })
	seam(t, &claimBead, func(bd.Bead) (*liveness.Hold, error) { return nil, errors.New("claim reached") })
	crews := map[string]contract.Crewmate{"nami": nil}

	seam(t, &readOrphans, func([]bd.Bead) map[string]bool { return nil })
	if status, _, _ := claimRunNow(nil, b, crews, false); !strings.Contains(status, "AFK pool full") {
		t.Errorf("live pool: status = %q, want the pool-full refusal", status)
	}
	seam(t, &readOrphans, func(bs []bd.Bead) map[string]bool {
		m := map[string]bool{}
		for _, x := range bs {
			m[x.ID] = true
		}
		return m
	})
	if status, _, _ := claimRunNow(nil, b, crews, false); strings.Contains(status, "AFK pool full") {
		t.Errorf("orphaned pool: status = %q, want run-now admitted past the pool", status)
	}
}
