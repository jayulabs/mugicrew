package main

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/bd/bdtest"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/luffy"
)

// triageRig seeds the given Signal (open, or held for the refusal cases) over a
// bdtest graph and stubs the exec seam; the Drafts helpers run for real against the
// graph. calls logs "run" for each exec; resolve is the cwd seam.
type triageRig struct {
	f       *bdtest.Fake
	calls   []string
	run     func() error // the stubbed agent session
	resolve func() (string, func(), error)
}

func newTriageRig(t *testing.T, sig bd.Bead) *triageRig {
	t.Helper()
	r := &triageRig{f: bdtest.New(t, sig), run: func() error { return nil }, resolve: cwdAt("/repo")}
	oe, od, op := interactiveExec, dropDrafts, publishDrafts
	t.Cleanup(func() { interactiveExec, dropDrafts, publishDrafts = oe, od, op })
	// The real helpers shell out to bd, so these stand in for them over the graph:
	// drop deletes the Signal's Drafts, publish opens them.
	dropDrafts = func(b bd.Bead) error {
		ids, err := r.draftIDs(b.ID)
		if err != nil || len(ids) == 0 {
			return err
		}
		return r.f.Delete(ids, bd.ClaimSer{ID: b.ID})
	}
	publishDrafts = func(id string, _ []any) error {
		ids, err := r.draftIDs(id)
		for _, d := range ids {
			r.f.Mutate(d, func(b *bd.Bead) { b.Status = "open" })
		}
		return err
	}
	interactiveExec = func(*exec.Cmd) error {
		r.calls = append(r.calls, "run")
		if b := r.f.Bead("sig1"); b.Status != "in_progress" || b.Assignee != bdtest.Actor {
			t.Errorf("during the session sig1 = %s/%q, want in_progress under %s", b.Status, b.Assignee, bdtest.Actor)
		}
		return r.run()
	}
	return r
}

func (r *triageRig) draftIDs(creator string) ([]string, error) {
	ds, err := r.f.List(bd.Query{All: true, Label: "drafted-by:" + creator})
	var ids []string
	for _, d := range ds {
		ids = append(ids, d.ID)
	}
	return ids, err
}

// seedDraft adds a Draft of sig1 to the graph.
func (r *triageRig) seedDraft() {
	r.f.Seed(bdtest.Draft("sig1", bd.Bead{ID: "d1", Kind: contract.KindErrand}))
}

// wantDraft checks d1's status; "" means it is gone.
func (r *triageRig) wantDraft(t *testing.T, status string) {
	t.Helper()
	ids, _ := r.f.List(bd.Query{All: true, IDs: []string{"d1"}})
	got := ""
	if len(ids) > 0 {
		got = ids[0].Status
	}
	if got != status {
		t.Errorf("d1 status = %q, want %q", got, status)
	}
}

func (r *triageRig) triage() error {
	ac := contract.AgentCommand{Argv: []string{"claude"}}
	return runInteractive(ac, r.resolve, bd.Bead{ID: "sig1", Kind: contract.KindSignal}, "")
}

func (r *triageRig) wantSignal(t *testing.T, status, assignee string) {
	t.Helper()
	if b := r.f.Bead("sig1"); b.Status != status || b.Assignee != assignee {
		t.Errorf("sig1 = %s/%q, want %s/%q", b.Status, b.Assignee, status, assignee)
	}
}

func openSig() bd.Bead { return bd.Bead{ID: "sig1", Kind: contract.KindSignal, Status: "open"} }

func eventNames(evs []eventlogtest.Event) []string {
	var out []string
	for _, e := range evs {
		out = append(out, e.Name)
	}
	return out
}

func TestSignalTriageSettlesClaim(t *testing.T) {
	t.Run("clean exit with no outcome publishes and releases", func(t *testing.T) {
		r := newTriageRig(t, openSig())
		r.seedDraft()
		r.run = func() error { r.f.Seed(bdtest.Draft("sig1", bd.Bead{ID: "d2", Kind: contract.KindErrand})); return nil }
		capt := eventlogtest.Capture(t)
		if err := r.triage(); err != nil {
			t.Fatal(err)
		}
		r.wantDraft(t, "")                           // the dead run's leftover is gone
		if b := r.f.Bead("d2"); b.Status != "open" { // the session's own is published
			t.Errorf("d2 status = %q, want open", b.Status)
		}
		r.wantSignal(t, "open", "")
		var taken, released bool
		for _, e := range *capt {
			taken = taken || e.Name == "claim.taken"
			released = released || e.Name == "claim.released"
		}
		if !taken || !released {
			t.Errorf("events = %v, want claim.taken and claim.released", eventNames(*capt))
		}
	})

	t.Run("a Signal the session closed stays closed", func(t *testing.T) {
		r := newTriageRig(t, openSig())
		r.run = func() error {
			r.f.Mutate("sig1", func(b *bd.Bead) { b.Status = "closed" })
			return nil
		}
		if err := r.triage(); err != nil {
			t.Fatal(err)
		}
		r.wantSignal(t, "closed", bdtest.Actor)
	})

	t.Run("non-zero exit releases and keeps Drafts", func(t *testing.T) {
		r := newTriageRig(t, openSig())
		r.run = func() error {
			r.seedDraft()
			return errors.New("rc=1")
		}
		if err := r.triage(); err == nil {
			t.Fatal("want the exit error")
		}
		r.wantDraft(t, "deferred")
		r.wantSignal(t, "open", "")
	})

	t.Run("drafts deletion failure releases", func(t *testing.T) {
		r := newTriageRig(t, openSig())
		dropDrafts = func(bd.Bead) error { return errors.New("bd down") }
		if err := r.triage(); err == nil || len(r.calls) != 0 { // no exec
			t.Errorf("err=%v calls=%v, want an error and no exec", err, r.calls)
		}
		r.wantSignal(t, "open", "")
	})

	t.Run("cwd failure releases", func(t *testing.T) {
		r := newTriageRig(t, openSig())
		r.resolve = func() (string, func(), error) { return "", nil, errors.New("no tree") }
		if err := r.triage(); err == nil || len(r.calls) != 0 {
			t.Errorf("err=%v calls=%v, want an error and no exec", err, r.calls)
		}
		r.wantSignal(t, "open", "")
	})

	t.Run("publish failure releases and returns the error", func(t *testing.T) {
		r := newTriageRig(t, openSig())
		publishDrafts = func(string, []any) error { return errors.New("publish boom") }
		if err := r.triage(); err == nil || !strings.Contains(err.Error(), "publish boom") {
			t.Errorf("err = %v", err)
		}
		r.wantSignal(t, "open", "")
	})
}

func TestSignalTriageRefusesHeldSignal(t *testing.T) {
	for name, sig := range map[string]bd.Bead{
		"another actor":    {ID: "sig1", Kind: contract.KindSignal, Status: "in_progress", Assignee: "zed@dev"},
		"this actor (TUI)": bdtest.Claimed(openSig()),
	} {
		t.Run(name, func(t *testing.T) {
			r := newTriageRig(t, sig)
			r.seedDraft()
			capt := eventlogtest.Capture(t)
			err := r.triage()
			if err == nil || !strings.Contains(err.Error(), "bd unclaim sig1") || !strings.Contains(err.Error(), "drop its session") {
				t.Fatalf("err = %v, want a refusal naming both recovery routes", err)
			}
			if name == "another actor" && (!strings.Contains(err.Error(), "zed@dev") || !strings.Contains(err.Error(), "bd unclaim sig1 --force")) {
				t.Errorf("err = %v, want the holder named and a runnable --force route", err)
			}
			if len(r.calls) != 0 {
				t.Errorf("calls = %v, want nothing exec'd", r.calls)
			}
			r.wantDraft(t, "deferred") // nothing deleted
			for _, e := range *capt {
				if e.Name == "hitl.launched" {
					t.Error("hitl.launched recorded for a refused launch")
				}
			}
			if b := r.f.Bead("sig1"); b.Status != "in_progress" || b.Assignee != sig.Assignee {
				t.Errorf("sig1 = %s/%q, claim must be untouched", b.Status, b.Assignee)
			}
		})
	}
}

// A real luffy Signal site run inside the exec passes the fake's Serializer check
// under the verb's own claim.
func TestSignalTriageRunsLuffySiteUnderItsClaim(t *testing.T) {
	// The Chart path cuts chart/<id> with git; a scratch repo keeps the real one clean.
	t.Chdir(t.TempDir())
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	r := newTriageRig(t, openSig())
	r.run = func() error {
		// the Chart path's Survey spawn runs under bd.UnderClaim(sig1); the fake refuses it unless held
		return luffy.Luffy{}.Reconcile(nil, r.f.Bead("sig1"))
	}
	if err := r.triage(); err != nil {
		t.Fatal(err)
	}
	r.wantSignal(t, "open", "")
	if out, err := exec.Command("git", "rev-parse", "--verify", "-q", "refs/heads/chart/bdct-1").CombinedOutput(); err != nil {
		t.Errorf("chart/bdct-1 not cut in the scratch repo, so the site was not reached: %v: %s", err, out)
	}
}
