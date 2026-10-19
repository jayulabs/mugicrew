package withdraw

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
)

type forgeFake struct {
	reads []string
	state string
	err   error
}

// forgeWorld stubs the land strategy, the repo a bead resolves to and the Forge read.
func forgeWorld(t *testing.T, strategy string) *forgeFake {
	t.Helper()
	f := &forgeFake{state: forge.StateOpened}
	os, orr, op, og := strategyName, repoDirOf, projectOf, getMR
	t.Cleanup(func() { strategyName, repoDirOf, projectOf, getMR = os, orr, op, og })
	strategyName = func() (string, error) { return strategy, nil }
	repoDirOf = func(bd.Bead) (git.Repo, string) { return "", "" }
	projectOf = func(r git.Repo) (forge.Project, error) {
		if r == "" {
			return forge.Project{Host: "h", ID: "ws"}, nil
		}
		return forge.Project{Host: "h", ID: "ws%2Fchild"}, nil
	}
	getMR = func(p forge.Project, iid int) (*forge.MR, error) {
		f.reads = append(f.reads, p.ID+"!"+string(rune('0'+iid)))
		if f.err != nil {
			return nil, f.err
		}
		return &forge.MR{IID: iid, State: f.state}, nil
	}
	return f
}

func landing(id, parent, status, reason, body string) bd.Bead {
	l := b(id, contract.KindLanding, parent)
	l.Status, l.CloseReason, l.Description = status, reason, body
	return l
}

func withLanding(l bd.Bead) []bd.Bead { return append(fixture(), l) }

func refusal(t *testing.T, err error) *RefusedError {
	t.Helper()
	var re *RefusedError
	if !errors.As(err, &re) {
		t.Fatalf("err = %v, want a refusal", err)
	}
	return re
}

// An open Landing under a closing container refuses, naming it and the fix; nothing is closed.
func TestOpenLandingRefuses(t *testing.T) {
	forgeWorld(t, git.LandStrategyLocal)
	calls := graph(t, withLanding(landing("l1", "cp", "open", "", ""))...)
	_, err := Withdraw("cp", Opts{})
	re := refusal(t, err)
	r := re.Refusals[0]
	if r.Kind != OpenLanding || !slices.Equal(r.Beads, []string{"l1"}) || !strings.Contains(r.Fix, "let the Landing finish or close it") {
		t.Errorf("refusal = %+v", r)
	}
	if len(*calls) != 0 {
		t.Errorf("closed %v", *calls)
	}
}

// An open Landing that already opened its MR names both, with the MR fix; the error text
// carries the MR too.
func TestOpenLandingWithMRNamesMR(t *testing.T) {
	forgeWorld(t, git.LandStrategyGitlab)
	graph(t, withLanding(landing("l1", "cp", "open", "", "MR: !42"))...)
	_, err := Withdraw("cp", Opts{})
	re := refusal(t, err)
	r := re.Refusals[0]
	if r.Kind != OpenLanding || !slices.Equal(r.Beads, []string{"l1"}) || r.MR != "!42" ||
		!strings.Contains(r.Fix, "close !42 on the Forge, then withdraw again") ||
		!strings.Contains(r.Fix, "let the Landing finish or close it") {
		t.Errorf("refusal = %+v", r)
	}
	if !strings.Contains(re.Error(), "!42") {
		t.Errorf("error = %q", re.Error())
	}
}

// A closed obsolete: conflict Landing whose MR the Forge still reports opened refuses with
// the Landing, the !iid and the fix; closed or merged passes.
func TestPossiblyOpenMR(t *testing.T) {
	body := "MR: !7"
	for _, c := range []struct {
		name, state string
		err         error
		refuses     bool
		fix         string
	}{
		{"opened", forge.StateOpened, nil, true, "close !7 on the Forge, then withdraw again"},
		{"closed", "closed", nil, false, ""},
		{"merged", "merged", nil, false, ""},
		{"read fails", "", forge.ErrRateLimited, true, "can't confirm !7 is closed"},
	} {
		f := forgeWorld(t, git.LandStrategyGitlab)
		f.state, f.err = c.state, c.err
		calls := graph(t, withLanding(landing("l1", "cp", "closed", "obsolete: conflict", body))...)
		_, err := Withdraw("cp", Opts{})
		if !c.refuses {
			if err != nil || len(*calls) != 1 {
				t.Errorf("%s: err %v calls %v", c.name, err, *calls)
			}
			continue
		}
		r := refusal(t, err).Refusals[0]
		if r.Kind != OpenLanding || !slices.Equal(r.Beads, []string{"l1"}) || !strings.Contains(r.Fix, c.fix) {
			t.Errorf("%s: refusal = %+v", c.name, r)
		}
		if len(*calls) != 0 {
			t.Errorf("%s: closed %v", c.name, *calls)
		}
	}
}

// A Landing that closed landed: or obsolete: merge request closed is known-done: no Forge read.
func TestSettledLandingMakesNoRead(t *testing.T) {
	for _, reason := range []string{"landed: abc", "obsolete: merge request closed"} {
		f := forgeWorld(t, git.LandStrategyGitlab)
		graph(t, withLanding(landing("l1", "cp", "closed", reason, "MR: !7"))...)
		if _, err := Withdraw("cp", Opts{}); err != nil || len(f.reads) != 0 {
			t.Errorf("%s: err %v reads %v", reason, err, f.reads)
		}
	}
}

// Only the newest Landing counts: an older opened-MR Landing behind a landed one passes.
func TestOnlyNewestLandingCounts(t *testing.T) {
	f := forgeWorld(t, git.LandStrategyGitlab)
	old := landing("l0", "cp", "closed", "obsolete: conflict", "MR: !3")
	old.Closed = old.Closed.Add(-1)
	newer := landing("l1", "cp", "closed", "landed: abc", "MR: !7")
	graph(t, append(withLanding(old), newer)...)
	if _, err := Withdraw("cp", Opts{}); err != nil || len(f.reads) != 0 {
		t.Errorf("err %v reads %v", err, f.reads)
	}
}

// The local strategy has no MR: only the open Landing applies.
func TestLocalStrategyIgnoresMR(t *testing.T) {
	f := forgeWorld(t, git.LandStrategyLocal)
	graph(t, withLanding(landing("l1", "cp", "closed", "obsolete: conflict", "MR: !7"))...)
	if _, err := Withdraw("cp", Opts{}); err != nil || len(f.reads) != 0 {
		t.Errorf("err %v reads %v", err, f.reads)
	}
}

// A nested MR: <path>!<iid> is read on the nested repo's project, and named with its path.
func TestNestedMRResolvesChildProject(t *testing.T) {
	f := forgeWorld(t, git.LandStrategyGitlab)
	repoDirOf = func(bd.Bead) (git.Repo, string) { return git.Repo("/ws/child"), "child" }
	graph(t, withLanding(landing("l1", "cp", "closed", "obsolete: conflict", "MR: ws/child!5"))...)
	_, err := Withdraw("cp", Opts{})
	r := refusal(t, err).Refusals[0]
	if !slices.Equal(f.reads, []string{"ws%2Fchild!5"}) || !strings.Contains(r.Fix, "ws/child!5") {
		t.Errorf("reads %v refusal %+v", f.reads, r)
	}
}

// A Rollback in the set with an open revert MR refuses, naming the Rollback.
func TestRollbackRevertMRRefuses(t *testing.T) {
	f := forgeWorld(t, git.LandStrategyGitlab)
	rb := b("rb", contract.KindRollback, "")
	rb.Description = "revert\nMR: !9"
	calls := graph(t, rb)
	_, err := Withdraw("rb", Opts{})
	r := refusal(t, err).Refusals[0]
	if r.Kind != OpenLanding || !slices.Equal(r.Beads, []string{"rb"}) || !strings.Contains(r.Fix, "close !9 on the Forge") || len(f.reads) != 1 || len(*calls) != 0 {
		t.Errorf("refusal %+v reads %v calls %v", r, f.reads, *calls)
	}
}

// Cancel takes the same refusals, and PlanOf reports them without writing.
func TestCancelRefusesOpenLanding(t *testing.T) {
	forgeWorld(t, git.LandStrategyLocal)
	calls := graph(t, withLanding(landing("l1", "cp", "open", "", ""))...)
	p, err := PlanOf("in", Opts{})
	if err != nil || p.Kind != KindCancel || len(p.Refusals) != 1 || p.Refusals[0].Kind != OpenLanding || !strings.Contains(p.Refusals[0].Fix, "cancel") && !strings.Contains(p.Refusals[0].Fix, "Landing") {
		t.Fatalf("plan = %+v, %v", p, err)
	}
	if _, err := Cancel("in", Opts{}); !errors.As(err, new(*RefusedError)) || len(*calls) != 0 {
		t.Errorf("err %v calls %v", err, *calls)
	}
}

// A nested Rollback's revert MR is read on the project of the bead Jinbe decides
// from: the Rollback when it carries a repo: stamp, else the rolled-back Checkpoint.
func TestRollbackRevertMRResolvesOnDecidingBead(t *testing.T) {
	for _, c := range []struct {
		name  string
		stamp bool
		want  string
	}{{"unstamped rollback uses its checkpoint", false, "ws%2Fchild!9"}, {"stamped rollback uses itself", true, "ws!9"}} {
		f := forgeWorld(t, git.LandStrategyGitlab)
		repoDirOf = func(x bd.Bead) (git.Repo, string) {
			if x.ID == "cp0" {
				return git.Repo("/ws/child"), "child"
			}
			return "", ""
		}
		cp0 := b("cp0", contract.KindCheckpoint, "")
		cp0.Status = "closed"
		rb := b("rb", contract.KindRollback, "")
		rb.Description, rb.Related = "revert\nMR: !9", []string{"cp0"}
		if c.stamp {
			rb.Labels = []string{"repo:other"}
		}
		graph(t, cp0, rb)
		if _, err := Withdraw("rb", Opts{}); !errors.As(err, new(*RefusedError)) {
			t.Fatalf("%s: err = %v", c.name, err)
		}
		if !slices.Equal(f.reads, []string{c.want}) {
			t.Errorf("%s: reads %v, want %s", c.name, f.reads, c.want)
		}
	}
}
