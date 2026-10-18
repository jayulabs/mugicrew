package chopper

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/session"
)

// fenced is a target holding only a Checkup's fence.
func fenced() bd.Bead {
	return bd.Bead{ID: "mc-a", Title: "Broken", Kind: contract.KindBuild, Status: "open", Labels: []string{"paused:checkup"}}
}

// stubCheckup wires the Checkup lookup and its session: open "" is a closed Checkup.
func stubCheckup(t *testing.T, w *world, open string, st session.State) {
	set(t, &openCheckupOn, func(string) (bd.Bead, bool, error) { return bd.Bead{ID: open}, open != "", nil })
	set(t, &sessions, func() (map[string]session.Info, error) {
		return map[string]session.Info{"mc-ck": {Session: "mc-ck", State: st}}, nil
	})
}

// recover leaves a target alone while its Checkup's session is Alive or Exited,
// and lifts the fence once the session is gone or the Checkup is closed.
func TestRecoverCheckupFence(t *testing.T) {
	for _, st := range []session.State{session.Alive, session.Exited} {
		w := &world{bead: fenced()}
		stub(t, w)
		stubCheckup(t, w, "mc-ck", st)
		_, err := Recover("mc-a", Opts{})
		var rf *Refused
		if !errors.As(err, &rf) || !strings.Contains(err.Error(), "a Checkup is open on Broken") {
			t.Fatalf("state %v: err = %v, want refused", st, err)
		}
		if len(w.calls) > 0 && w.calls[len(w.calls)-1] != "probe" {
			t.Errorf("state %v: wrote %q", st, w.calls)
		}
	}
	for name, open := range map[string]string{"session gone": "mc-ck", "closed": ""} {
		w := &world{bead: fenced()}
		stub(t, w)
		stubCheckup(t, w, open, session.None)
		if _, err := Recover("mc-a", Opts{}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if bd.PausedBy(w.bead, bd.PausedCheckup) {
			t.Errorf("%s: paused:checkup stays", name)
		}
		if r, err := Recover("mc-a", Opts{}); err != nil || len(r.Lines) != 0 {
			t.Errorf("%s: re-run = %v, %v, want a no-op", name, r, err)
		}
	}
}

func TestListShowsCheckupFence(t *testing.T) {
	live, gone := fenced(), fenced()
	live.ID, gone.ID = "mc-live", "mc-gone"
	beads := map[string]bd.Bead{"mc-live": live, "mc-gone": gone}
	set(t, &inProgress, func() ([]bd.Bead, error) { return nil, nil })
	set(t, &pausedList, func() ([]bd.Bead, error) { return []bd.Bead{live, gone}, nil })
	set(t, &worktrees, func() ([]git.Worktree, error) { return nil, nil })
	set(t, &rebasing, func() (map[string]string, error) { return nil, nil })
	set(t, &nestedHosts, func() (map[string]string, error) { return nil, nil })
	set(t, &show, func(id string) (bd.Bead, error) { return beads[id], nil })
	set(t, &holderAcross, func(string, map[string]string) (git.Holder, git.Repo, error) { return git.Holder{}, "", nil })
	set(t, &drafts, func(string) ([]string, error) { return nil, nil })
	set(t, &openCheckupOn, func(id string) (bd.Bead, bool, error) {
		if id == "mc-live" {
			return bd.Bead{ID: "mc-ck"}, true, nil
		}
		return bd.Bead{}, false, nil
	})
	set(t, &sessions, func() (map[string]session.Info, error) {
		return map[string]session.Info{"mc-ck": {State: session.Alive}}, nil
	})
	set(t, &probe, func(string) []int { return nil })
	set(t, &lineage, func(pid int) []int { return []int{pid} })
	set(t, &loopState, func() (string, error) { return "off", nil })
	set(t, &tuiAlive, func(string) (bool, error) { return false, nil })
	var out bytes.Buffer
	if err := List(&out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"mc-live  refused", "a Checkup is open on Broken", "mc-gone  dead", "mugicrew recover mc-gone"} {
		if !strings.Contains(got, want) {
			t.Errorf("list missing %q:\n%s", want, got)
		}
	}
}
