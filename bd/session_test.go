package bd

import (
	"errors"
	"reflect"
	"testing"

	"jayulabs.org/mugicrew/contract"
)

type sessHarness struct {
	contract.Harness
	out string
	err error
}

func (h sessHarness) Run(contract.Ctx, contract.Invocation) (contract.Result, error) {
	return contract.Result{Output: h.out}, h.err
}

type sessCtx struct{ h contract.Harness }

func (c sessCtx) Harness() contract.Harness { return c.h }

func TestRunSession(t *testing.T) {
	runErr, showErr := errors.New("run failed"), errors.New("show failed")
	open := Bead{ID: "x", Status: "in_progress", Title: "fresh"}
	closed := Bead{ID: "x", Status: "closed"}
	tests := []struct {
		name     string
		out      string
		runErr   error
		bead     Bead
		showErr  error
		wantErr  error
		wantOpen bool
		wantLine string
	}{
		{"run errors, open", "", runErr, open, nil, runErr, true, ""},
		{"run errors, closed", "", runErr, closed, nil, nil, false, ""},
		{"run errors, show errors", "", runErr, Bead{}, showErr, runErr, true, ""},
		{"ok, closed", "Approved", nil, closed, nil, nil, false, ""},
		{"ok, open", "prose\nResolved: yes\n\n  \n", nil, open, nil, nil, true, "Resolved: yes"},
		{"ok, open, whitespace", "  Escalate: why \t\n", nil, open, nil, nil, true, "Escalate: why"},
		{"ok, open, empty output", "", nil, open, nil, nil, true, ""},
		{"ok, show errors", "Approved", nil, Bead{}, showErr, showErr, true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := sessCtx{sessHarness{out: tc.out, err: tc.runErr}}
			fresh, line, res, isOpen, err := RunSession(ctx, contract.Invocation{BeadID: "x"}, func(id string) (Bead, error) {
				if id != "x" {
					t.Errorf("show(%q), want the Invocation's bead", id)
				}
				return tc.bead, tc.showErr
			})
			if err != tc.wantErr || isOpen != tc.wantOpen || line != tc.wantLine {
				t.Errorf("got (err=%v open=%v line=%q), want (%v %v %q)", err, isOpen, line, tc.wantErr, tc.wantOpen, tc.wantLine)
			}
			if res.Output != tc.out {
				t.Errorf("res.Output = %q, want the full output %q", res.Output, tc.out)
			}
			if tc.name == "ok, open" && fresh.Title != "fresh" {
				t.Errorf("fresh = %+v, want the re-read bead", fresh)
			}
		})
	}
}

func TestRaiseIfPivoted(t *testing.T) {
	oP, oR := pivotsDiscoveredFrom, raisePivot
	t.Cleanup(func() { pivotsDiscoveredFrom, raisePivot = oP, oR })
	b := Bead{ID: "b", Parent: "cp"}
	boom := errors.New("boom")

	var calls int
	var gotCp, gotSpec string
	var gotPivots []Bead
	raisePivot = func(_ contract.Ctx, bead Bead, cp, spec string, ps []Bead) error {
		calls++
		gotCp, gotSpec, gotPivots = cp, spec, ps
		return nil
	}

	pivotsDiscoveredFrom = func(string) ([]Bead, error) { return nil, nil }
	if raised, err := RaiseIfPivoted(nil, b, "sp"); raised || err != nil || calls != 0 {
		t.Errorf("no pivots: raised=%v err=%v calls=%d", raised, err, calls)
	}

	pv := []Bead{{ID: "p1"}, {ID: "p2"}}
	pivotsDiscoveredFrom = func(string) ([]Bead, error) { return pv, nil }
	if raised, err := RaiseIfPivoted(nil, b, "sp"); !raised || err != nil || calls != 1 || gotCp != "cp" || gotSpec != "sp" || !reflect.DeepEqual(gotPivots, pv) {
		t.Errorf("pivots: raised=%v err=%v calls=%d cp=%q spec=%q pivots=%v", raised, err, calls, gotCp, gotSpec, gotPivots)
	}

	raisePivot = func(contract.Ctx, Bead, string, string, []Bead) error { return boom }
	if raised, err := RaiseIfPivoted(nil, b, "sp"); raised || err != boom {
		t.Errorf("raise error: raised=%v err=%v", raised, err)
	}

	pivotsDiscoveredFrom = func(string) ([]Bead, error) { return nil, boom }
	if raised, err := RaiseIfPivoted(nil, b, "sp"); raised || err != boom {
		t.Errorf("find error: raised=%v err=%v", raised, err)
	}
}
