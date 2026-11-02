package main

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/chopper"
	"jayulabs.org/mugicrew/contract"
)

// checkupWorld scripts runCheckup's seams over one target and records each write in order.
type checkupWorld struct {
	target  bd.Bead
	refusal error
	open    *bd.Bead // the target's open Checkup
	newErr  error    // the create's failure
	calls   []string
	problem string
}

func (w *checkupWorld) wire(t *testing.T) {
	orig := checkupSeams
	t.Cleanup(func() { checkupSeams = orig })
	checkupSeams = checkupIO{
		Show: func(id string) (bd.Bead, error) {
			if id != w.target.ID {
				return bd.Bead{}, errors.New("no such bead " + id)
			}
			return w.target, nil
		},
		Refusal: func(bd.Bead) error { return w.refusal },
		OpenOn: func(string) (bd.Bead, bool, error) {
			if w.open == nil {
				return bd.Bead{}, false, nil
			}
			return *w.open, true, nil
		},
		New: func(_ bd.Bead, problem string) (string, error) {
			w.calls, w.problem = append(w.calls, "new"), problem
			if w.newErr != nil {
				return "", w.newErr
			}
			return "mc-ck", nil
		},
		Fence:  func(id, cause string) error { w.calls = append(w.calls, "fence "+id+" "+cause); return nil },
		Launch: func(id string) error { w.calls = append(w.calls, "launch "+id); return nil },
	}
}

func runCheckupT(t *testing.T, w *checkupWorld, args ...string) (int, string) {
	t.Helper()
	w.wire(t)
	var errb bytes.Buffer
	return runCheckup(args, &errb), errb.String()
}

func TestCheckupFilesFencesThenLaunches(t *testing.T) {
	w := &checkupWorld{target: bd.Bead{ID: "mc-t", Title: "Odd"}}
	for _, args := range [][]string{{"mc-t", "--problem", "wrong edge"}, {"--problem", "wrong edge", "mc-t"}} {
		w.calls = nil
		if rc, msg := runCheckupT(t, w, args...); rc != 0 {
			t.Fatalf("%v: rc %d: %s", args, rc, msg)
		}
		if want := []string{"new", "fence mc-t checkup", "launch mc-ck"}; !slices.Equal(w.calls, want) {
			t.Errorf("%v: calls = %v, want %v", args, w.calls, want)
		}
		if w.problem != "wrong edge" {
			t.Errorf("%v: problem = %q", args, w.problem)
		}
	}
}

func TestCheckupAttachesToAnOpenCheckup(t *testing.T) {
	w := &checkupWorld{target: bd.Bead{ID: "mc-t"}, open: &bd.Bead{ID: "mc-old", Kind: contract.KindCheckup, Status: "in_progress"}}
	if rc, msg := runCheckupT(t, w, "mc-t"); rc != 0 {
		t.Fatalf("rc %d: %s", rc, msg)
	}
	if want := []string{"fence mc-t checkup", "launch mc-old"}; !slices.Equal(w.calls, want) {
		t.Errorf("calls = %v, want %v (no second create)", w.calls, want)
	}
}

func TestCheckupRefusalExits1WithItsMessageAndWritesNothing(t *testing.T) {
	w := &checkupWorld{target: bd.Bead{ID: "mc-t"}, refusal: errors.New("a session is open on Odd")}
	rc, msg := runCheckupT(t, w, "mc-t")
	if rc != 1 || strings.TrimSpace(msg) != "a session is open on Odd" || len(w.calls) != 0 {
		t.Errorf("rc %d, msg %q, calls %v", rc, msg, w.calls)
	}
}

func TestCheckupFailedCreateLeavesTargetUnfenced(t *testing.T) {
	w := &checkupWorld{target: bd.Bead{ID: "mc-t"}, newErr: errors.New("bd down")}
	rc, msg := runCheckupT(t, w, "mc-t")
	if rc != 1 || !strings.Contains(msg, "bd down") || !slices.Equal(w.calls, []string{"new"}) {
		t.Errorf("rc %d, msg %q, calls %v; want only the failed create", rc, msg, w.calls)
	}
}

func TestCheckupUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"a", "b"}, {"--problem"}, {"mc-t", "--force"}} {
		w := &checkupWorld{target: bd.Bead{ID: "mc-t"}}
		if rc, msg := runCheckupT(t, w, args...); rc != 1 || !strings.Contains(msg, "usage: mugicrew checkup") || len(w.calls) != 0 {
			t.Errorf("%v: rc %d, msg %q, calls %v", args, rc, msg, w.calls)
		}
	}
}

func TestCheckupIsInTopLevelHelp(t *testing.T) {
	if !strings.Contains(usage, "mugicrew checkup <bead>") {
		t.Error("usage does not list checkup")
	}
}

// closeWorld scripts runCheckup's close seams over one Checkup and records each write.
type closeWorld struct {
	ck    bd.Bead
	other bool // another open Checkup holds the target
	calls []string
}

func (w *closeWorld) run(t *testing.T, args ...string) (int, string) {
	t.Helper()
	orig := checkupCloseSeams
	t.Cleanup(func() { checkupCloseSeams = orig })
	checkupCloseSeams = checkupCloseIO{
		ListByID: func(ids []string) ([]bd.Bead, error) {
			if len(ids) != 1 || ids[0] != w.ck.ID {
				return nil, nil
			}
			return []bd.Bead{w.ck}, nil
		},
		Closing: chopper.CheckupClosing{
			Close:   func(id, reason string) error { w.calls = append(w.calls, "close "+id+" "+reason); return nil },
			OpenOn:  func(string) (bd.Bead, bool, error) { return bd.Bead{}, w.other, nil },
			Unpause: func(id, cause string) error { w.calls = append(w.calls, "unpause "+id+" "+cause); return nil },
		},
	}
	var errb bytes.Buffer
	return runCheckup(append([]string{"close"}, args...), &errb), errb.String()
}

func openCheckup() bd.Bead {
	return bd.Bead{ID: "mc-ck", Kind: contract.KindCheckup, Status: "in_progress", Related: []string{"mc-t"}}
}

func TestCheckupCloseClosesAndLifts(t *testing.T) {
	for _, reason := range []string{"repaired: fixed the label", "not a bead fault: nothing wrong"} {
		w := &closeWorld{ck: openCheckup()}
		if code, msg := w.run(t, "mc-ck", "--reason", reason); code != 0 {
			t.Fatalf("%q: exit %d: %s", reason, code, msg)
		}
		want := []string{"close mc-ck " + reason, "unpause mc-t " + bd.PausedCheckup}
		if !slices.Equal(w.calls, want) {
			t.Errorf("%q: calls = %v, want %v", reason, w.calls, want)
		}
	}
}

func TestCheckupCloseKeepsHeldFence(t *testing.T) {
	w := &closeWorld{ck: openCheckup(), other: true}
	if code, _ := w.run(t, "mc-ck", "--reason", "repaired: x"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if want := []string{"close mc-ck repaired: x"}; !slices.Equal(w.calls, want) {
		t.Errorf("calls = %v, want %v", w.calls, want)
	}
}

func TestCheckupCloseRefusals(t *testing.T) {
	closed := openCheckup()
	closed.Status = "closed"
	notCheckup := openCheckup()
	notCheckup.Kind = contract.KindSignal
	for name, c := range map[string]struct {
		ck   bd.Bead
		args []string
	}{
		"bare repaired":     {openCheckup(), []string{"mc-ck", "--reason", "repaired:"}},
		"blank detail":      {openCheckup(), []string{"mc-ck", "--reason", "not a bead fault:  "}},
		"dropped":           {openCheckup(), []string{"mc-ck", "--reason", "dropped"}},
		"arbitrary":         {openCheckup(), []string{"mc-ck", "--reason", "done"}},
		"no reason":         {openCheckup(), []string{"mc-ck"}},
		"no checkup":        {openCheckup(), []string{"--reason", "repaired: x"}},
		"not a Checkup":     {notCheckup, []string{"mc-ck", "--reason", "repaired: x"}},
		"closed Checkup":    {closed, []string{"mc-ck", "--reason", "repaired: x"}},
		"unknown bead":      {openCheckup(), []string{"mc-zz", "--reason", "repaired: x"}},
		"extra positionals": {openCheckup(), []string{"mc-ck", "mc-t", "--reason", "repaired: x"}},
	} {
		w := &closeWorld{ck: c.ck}
		code, msg := w.run(t, c.args...)
		if code != 1 || len(w.calls) != 0 {
			t.Errorf("%s: exit %d, calls %v; want exit 1 and no writes", name, code, w.calls)
		}
		if !strings.Contains(msg, "repaired: <what changed>") {
			t.Errorf("%s: stderr lacks the usage line: %q", name, msg)
		}
	}
}
