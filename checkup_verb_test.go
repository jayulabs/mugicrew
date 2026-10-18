package main

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
)

// checkupWorld scripts runCheckup's seams over one target and records each write in order.
type checkupWorld struct {
	target  bd.Bead
	refusal error
	open    *bd.Bead // the target's open Checkup
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
