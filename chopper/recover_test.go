package chopper

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/liveness"
	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/route"
	"jayulabs.org/mugicrew/session"
)

// stranded is the common force-stop leftover: a Build on its effort tree, still
// claimed, paused abandoned.
func stranded() bd.Bead {
	return bd.Bead{ID: "mc-a", Kind: contract.KindBuild, Parent: "mc-cp", Status: "in_progress", Labels: []string{"paused:abandoned"}}
}

// hitl is a Surgery, a HITL bead, stranded on its tree.
func hitl() bd.Bead {
	return bd.Bead{ID: "mc-a", Kind: contract.KindSurgery, Parent: "mc-cp", Status: "in_progress", Labels: []string{"paused:abandoned"}}
}

// world is the bead and host Recover's seams read and write: stub wires every
// seam to it, recording each write (and each liveness probe) in order.
type world struct {
	bead   bd.Bead
	tree   string
	files  int
	rebase bool // the tree is stopped mid-rebase, its HEAD detached
	after  int  // the files the tree holds once its rebase is aborted
	drafts []string
	probe  func(n int) []int // the n-th probe's pids, n from 1
	cwd    []int
	cwdErr error            // the cwd scan fails
	sess   session.Info     // the bead's session; zero is gone
	fail   map[string]error // a write's name → its error
	calls  []string
	probes int
	excl   []int                // the exclude set the cwd scan was handed
	loop   string               // the loop lock's state; "" reads off
	tuis   func(n int) []string // the n-th TUI read's tui-* sessions, n from 1
	reads  int
	tuiErr error // the TUI-alive read's error
}

// set points seam p at v until the test ends.
func set[T any](t *testing.T, p *T, v T) {
	orig := *p
	*p = v
	t.Cleanup(func() { *p = orig })
}

func stub(t *testing.T, w *world) {
	t.Helper()
	write := func(name string, apply func()) error {
		w.calls = append(w.calls, name)
		if err := w.fail[name]; err != nil {
			return err
		}
		apply()
		return nil
	}
	set(t, &show, func(string) (bd.Bead, error) { b := w.bead; b.Labels = slices.Clone(b.Labels); return b, nil })
	set(t, &holderAcross, func(branch string, _ map[string]string) (git.Holder, git.Repo, error) {
		if branch != "effort/"+w.bead.ID {
			t.Errorf("tree looked up on %s, want effort/%s", branch, w.bead.ID)
		}
		if w.tree == "" {
			return git.Holder{}, "", nil
		}
		return git.Holder{Path: w.tree, Rebasing: w.rebase}, "", nil
	})
	set(t, &abortRebase, func(p string) error {
		return write("abort rebase "+p, func() { w.rebase, w.files = false, w.after })
	})
	set(t, &dirtyFiles, func(string) (int, error) { return w.files, nil })
	set(t, &drafts, func(string) ([]string, error) { return w.drafts, nil })
	set(t, &sessions, func() (map[string]session.Info, error) {
		if !w.sess.Has() {
			return nil, nil
		}
		return map[string]session.Info{w.bead.ID: w.sess}, nil
	})
	set(t, &probe, func(string) []int {
		w.probes++
		w.calls = append(w.calls, "probe")
		if w.probe == nil {
			return nil
		}
		return w.probe(w.probes)
	})
	set(t, &inTree, func(_ string, exclude []int) ([]int, error) {
		w.excl = exclude
		return slices.DeleteFunc(slices.Clone(w.cwd), func(p int) bool { return slices.Contains(exclude, p) }), w.cwdErr
	})
	set(t, &loopState, func() (string, error) {
		w.calls = append(w.calls, "lock")
		if w.loop == "" {
			return looplock.Off, nil
		}
		return w.loop, nil
	})
	set(t, &tuiAlive, func(exclude string) (bool, error) {
		w.reads++
		if w.tuis == nil {
			return false, w.tuiErr
		}
		return slices.ContainsFunc(w.tuis(w.reads), func(s string) bool { return s != exclude }), nil
	})
	set(t, &lineage, func(pid int) []int { return []int{pid, 1} })
	set(t, &pause, func(_, c string) error {
		return write("pause "+c, func() { w.bead.Labels = append(w.bead.Labels, "paused:"+c) })
	})
	set(t, &unpause, func(_, c string) error {
		return write("unpause "+c, func() {
			w.bead.Labels = slices.DeleteFunc(w.bead.Labels, func(l string) bool { return l == "paused:"+c })
		})
	})
	set(t, &dropTree, func(p string) error { return write("drop "+p, func() { w.tree, w.files = "", 0 }) })
	set(t, &deleteDrafts, func(string) (ids []string, err error) {
		err = write("delete drafts", func() { ids, w.drafts = w.drafts, nil })
		return ids, err
	})
	set(t, &unclaim, func(string) error { return write("unclaim", func() { w.bead.Status = "open" }) })
}

// Judge encodes the AFK rows of ADR 0127 §2 (as amended on mc-yr7o.6): a live
// probe or any cwd in the tree reads alive and names mugicrew abort; an AFK bead
// on a tree route the probe reads dead, paused abandoned (or fenced by an earlier
// recover), reads dead; a not-abandoned one is ambiguous, resolved only with the
// loop lock free and no TUI alive; an abandoned in-process one always needs
// --force; --force overrides ambiguous, never with the loop lock held.
func TestJudge(t *testing.T) {
	ab := []string{"paused:abandoned"}
	spec := bd.Bead{ID: "mc-a", Kind: contract.KindSpec, Labels: ab}
	held := bd.Bead{ID: "mc-a", Kind: contract.KindBuild, Status: "in_progress"} // claimed, not abandoned
	const heldBy = "held by its dispatcher"
	cases := []struct {
		name string
		h    Holder
		want string // a verdict, or a substring of the refusal
	}{
		{"probe alive", Holder{Bead: stranded(), Placement: route.Worktree, LockFree: true, Probe: []int{7}}, "mugicrew abort mc-a"},
		{"cwd in the tree", Holder{Bead: stranded(), Placement: route.Worktree, LockFree: true, Cwd: []int{8, 9}}, "8 9"},
		{"cwds unreadable", Holder{Bead: stranded(), Placement: route.Worktree, LockFree: true, CwdErr: errors.New("lsof: not found")}, "can't read process cwds"},
		{"cwds unreadable, forced", Holder{Bead: stranded(), Placement: route.Worktree, LockFree: true, Force: true, CwdErr: errors.New("lsof: not found")}, "can't read process cwds"},
		{"cwd names abort", Holder{Bead: stranded(), Placement: route.Worktree, LockFree: true, Cwd: []int{8}}, "mugicrew abort mc-a"},
		{"forced, still alive", Holder{Bead: stranded(), Placement: route.Worktree, LockFree: true, Force: true, Probe: []int{7}}, "mugicrew abort mc-a"},
		{"dispatcher lock held, dead", Holder{Bead: stranded(), Placement: route.Worktree, Held: true}, heldBy},
		{"dispatcher lock held, forced", Holder{Bead: stranded(), Placement: route.Worktree, LockFree: true, Held: true, Force: true}, heldBy},
		{"dispatcher lock held, forced with the loop on", Holder{Bead: stranded(), Placement: route.Worktree, Held: true, Force: true}, heldBy},
		{"dispatcher lock held, ambiguous resolved", Holder{Bead: held, Placement: route.Worktree, LockFree: true, Held: true}, "mugicrew abort mc-a"},
		{"worktree, abandoned", Holder{Bead: stranded(), Placement: route.Worktree}, Dead},
		{"detached, abandoned", Holder{Bead: bd.Bead{ID: "mc-a", Kind: contract.KindCheckpoint, Labels: ab}, Placement: route.Detached}, Dead},
		{"fenced by an earlier recover", Holder{Bead: bd.Bead{ID: "mc-a", Kind: contract.KindBuild, Labels: []string{"paused:recovering"}}, Placement: route.Worktree}, Dead},
		{"in-process, abandoned", Holder{Bead: spec, Placement: route.RepoRoot}, "pgrep -a claude"},
		{"in-process, abandoned, lock free, no TUI", Holder{Bead: spec, Placement: route.RepoRoot, LockFree: true}, "--force"},
		{"in-process, abandoned, forced", Holder{Bead: spec, Placement: route.RepoRoot, LockFree: true, Force: true}, Forced},
		{"not abandoned, lock free, no TUI", Holder{Bead: held, Placement: route.Worktree, LockFree: true}, AmbiguousResolved},
		{"not abandoned in-process, lock free, no TUI", Holder{Bead: bd.Bead{ID: "mc-a", Kind: contract.KindSpec, Status: "in_progress"}, Placement: route.RepoRoot, LockFree: true}, AmbiguousResolved},
		{"not abandoned, loop on", Holder{Bead: held, Placement: route.Worktree}, "the loop is on"},
		{"not abandoned, a TUI alive", Holder{Bead: held, Placement: route.Worktree, LockFree: true, TUIAlive: true}, "a TUI is alive"},
		{"not abandoned, TUI read fails", Holder{Bead: held, Placement: route.Worktree, LockFree: true, TUIErr: errors.New("tmux: EACCES")}, "tmux: EACCES"},
		{"not abandoned, loop on, forced", Holder{Bead: held, Placement: route.Worktree, Force: true}, "loop is off"},
		{"not abandoned, a TUI alive, forced", Holder{Bead: held, Placement: route.Worktree, LockFree: true, TUIAlive: true, Force: true}, Forced},
		{"dead, forced with the loop on", Holder{Bead: stranded(), Placement: route.Worktree, Force: true}, "loop is off"},
		{"dead, forced, lock free", Holder{Bead: stranded(), Placement: route.Worktree, LockFree: true, Force: true}, Dead},
		{"HITL session alive", Holder{Bead: hitl(), Session: session.Info{State: session.Alive}}, "alive"},
		{"HITL session alive and closing", Holder{Bead: hitl(), Session: session.Info{State: session.Alive, Closing: true}}, "alive"},
		{"HITL session exited", Holder{Bead: hitl(), Session: session.Info{State: session.Exited}}, "drop it from the TUI"},
		{"HITL session gone", Holder{Bead: hitl()}, Dead},
		{"HITL session gone, not abandoned", Holder{Bead: bd.Bead{ID: "mc-a", Kind: contract.KindSurgery, Status: "in_progress"}}, Dead},
		{"HITL session table unreadable", Holder{Bead: hitl(), SessionErr: errors.New("tmux: EACCES")}, "tmux: EACCES"},
		{"HITL session gone, probe alive", Holder{Bead: hitl(), Probe: []int{7}}, "mugicrew abort mc-a"},
		{"session table unreadable", Holder{Bead: stranded(), Placement: route.Worktree, SessionErr: errors.New("tmux: EACCES")}, "tmux: EACCES"},
		{"kindless bead", Holder{Bead: bd.Bead{ID: "mc-a", Status: "in_progress"}}, "not a mugicrew bead"},
		{"unroutable", Holder{Bead: stranded(), RouteErr: errors.New("no parent")}, "no parent"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, err := Judge(c.h)
			switch c.want {
			case Dead, AmbiguousResolved, Forced:
				if err != nil || v != c.want {
					t.Errorf("Judge = %q, %v; want %s", v, err, c.want)
				}
			default:
				var rf *Refused
				if !errors.As(err, &rf) || !strings.Contains(err.Error(), c.want) {
					t.Errorf("Judge = %q, %v; want a refusal naming %q", v, err, c.want)
				}
			}
		})
	}
}

// The full repair runs check → fence → re-check → drop tree → delete Drafts →
// unclaim → lift, prints one line per step, and logs one recover.done.
func TestRecoverRepairsInOrder(t *testing.T) {
	evs := eventlogtest.Capture(t)
	w := &world{bead: stranded(), tree: "/tmp/mugicrew-wt-1/wt", drafts: []string{"mc-d1", "mc-d2"}}
	w.bead.Labels = append(w.bead.Labels, "paused:foreign-hold")
	stub(t, w)
	r, err := Recover("mc-a", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"lock", "probe", "pause recovering", "lock", "probe", "drop /tmp/mugicrew-wt-1/wt", "delete drafts", "unclaim",
		"unpause abandoned", "unpause foreign-hold", "unpause recovering"}
	if !slices.Equal(w.calls, want) {
		t.Errorf("calls = %q\nwant    %q", w.calls, want)
	}
	lines := []string{"fenced with paused:recovering", "dropped tree /tmp/mugicrew-wt-1/wt", "deleted Drafts mc-d1, mc-d2", "unclaimed", "lifted abandoned, foreign-hold, recovering"}
	if !slices.Equal(r.Lines, lines) || len(r.Remaining) != 0 {
		t.Errorf("report = %q remaining %q, want %q", r.Lines, r.Remaining, lines)
	}
	if names := eventlogtest.Names(*evs); !slices.Equal(names, []string{"recover.done"}) {
		t.Fatalf("events = %v, want one recover.done", names)
	}
	a := (*evs)[0].Attrs
	if a["bead"] != "mc-a" || a["parent"] != "mc-cp" || a["verdict"] != "dead" || a["tree"] != "/tmp/mugicrew-wt-1/wt" ||
		a["discarded"] != false || !slices.Equal(a["drafts"].([]string), []string{"mc-d1", "mc-d2"}) ||
		a["unclaimed"] != true || !slices.Equal(a["lifted"].([]string), []string{"abandoned", "foreign-hold", "recovering"}) || len(a["remaining"].([]string)) != 0 {
		t.Errorf("recover.done = %v", a)
	}
}

// A bead with no leftover is nothing to recover: no write, no event.
func TestRecoverNothingToRecover(t *testing.T) {
	evs := eventlogtest.Capture(t)
	w := &world{bead: bd.Bead{ID: "mc-a", Kind: contract.KindBuild, Parent: "mc-cp", Status: "open", Labels: []string{"paused:foreign-hold"}}}
	stub(t, w)
	r, err := Recover("mc-a", Opts{})
	if err != nil || r.Verdict != "" || len(w.calls) != 0 || len(*evs) != 0 {
		t.Errorf("Recover = %+v, %v; calls %q events %v, want nothing", r, err, w.calls, *evs)
	}
}

// Each step with no leftover is a no-op: an unclaimed abandoned bead with no tree
// and no Drafts is only fenced and lifted.
func TestRecoverSkipsAbsentLeftovers(t *testing.T) {
	eventlogtest.Capture(t)
	w := &world{bead: stranded()}
	w.bead.Status = "open"
	stub(t, w)
	r, err := Recover("mc-a", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"lock", "probe", "pause recovering", "lock", "probe", "unpause abandoned", "unpause recovering"}; !slices.Equal(w.calls, want) {
		t.Errorf("calls = %q, want %q", w.calls, want)
	}
	if want := []string{"fenced with paused:recovering", "lifted abandoned, recovering"}; !slices.Equal(r.Lines, want) {
		t.Errorf("lines = %q, want %q", r.Lines, want)
	}
}

// A failed step logs one recover.failed naming it, leaves paused:recovering and
// returns *Failed; the re-run rolls forward from there to one recover.done.
func TestRecoverFailedStepRollsForward(t *testing.T) {
	evs := eventlogtest.Capture(t)
	boom := errors.New("bd down")
	w := &world{bead: stranded(), tree: "/tmp/mugicrew-wt-1/wt", drafts: []string{"mc-d1"}, fail: map[string]error{"unclaim": boom}}
	stub(t, w)
	_, err := Recover("mc-a", Opts{})
	var f *Failed
	if !errors.As(err, &f) || f.Step != "unclaim" || !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the unclaim step failed", err)
	}
	if !strings.Contains(err.Error(), "re-run mugicrew recover mc-a") || !bd.PausedBy(w.bead, bd.PausedRecovering) {
		t.Errorf("a failed step must leave the fence and say re-run: %v, labels %q", err, w.bead.Labels)
	}
	if names := eventlogtest.Names(*evs); !slices.Equal(names, []string{"recover.failed"}) {
		t.Fatalf("events = %v, want one recover.failed", names)
	}
	if a := (*evs)[0].Attrs; a["bead"] != "mc-a" || a["step"] != "unclaim" || a["error"] != "bd down" {
		t.Errorf("recover.failed = %v", a)
	}

	*evs, w.calls, w.fail = nil, nil, nil
	r, err := Recover("mc-a", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"lock", "probe", "lock", "probe", "unclaim", "unpause abandoned", "unpause recovering"}; !slices.Equal(w.calls, want) {
		t.Errorf("re-run calls = %q, want %q", w.calls, want)
	}
	if want := []string{"unclaimed", "lifted abandoned, recovering"}; !slices.Equal(r.Lines, want) {
		t.Errorf("re-run lines = %q, want %q", r.Lines, want)
	}
	if names := eventlogtest.Names(*evs); !slices.Equal(names, []string{"recover.done"}) {
		t.Errorf("re-run events = %v, want one recover.done", names)
	}
}

// A holder that appears between check and fence is refused at the re-check, and
// the fence stays so the bead stays safe.
func TestRecoverRecheckRefusalLeavesFence(t *testing.T) {
	evs := eventlogtest.Capture(t)
	w := &world{bead: stranded(), tree: "/tmp/mugicrew-wt-1/wt", probe: func(n int) []int {
		if n == 2 {
			return []int{77}
		}
		return nil
	}}
	stub(t, w)
	_, err := Recover("mc-a", Opts{})
	var rf *Refused
	if !errors.As(err, &rf) || !strings.Contains(err.Error(), "mugicrew abort mc-a") || !strings.Contains(err.Error(), "paused:recovering stays") {
		t.Fatalf("err = %v, want refused naming abort and the fence left", err)
	}
	if want := []string{"lock", "probe", "pause recovering", "lock", "probe"}; !slices.Equal(w.calls, want) || !bd.PausedBy(w.bead, bd.PausedRecovering) {
		t.Errorf("calls = %q labels %q, want the fence left and nothing repaired", w.calls, w.bead.Labels)
	}
	if names := eventlogtest.Names(*evs); !slices.Equal(names, []string{"recover.refused"}) || (*evs)[0].Attrs["reason"] != err.Error() {
		t.Errorf("events = %v, want one recover.refused with the reason", *evs)
	}
}

// manual and integrate-conflict are never lifted, nor failing with no rebase
// aborted; each is reported as remaining.
func TestRecoverKeepsManualAndIntegrateConflict(t *testing.T) {
	evs := eventlogtest.Capture(t)
	w := &world{bead: stranded()}
	w.bead.Labels = append(w.bead.Labels, "paused:integrate-conflict", "paused:manual", "paused:failing")
	stub(t, w)
	r, err := Recover("mc-a", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{bd.PausedManual, bd.PausedIntegrateConflict, bd.PausedFailing}; !slices.Equal(r.Remaining, want) || !slices.Equal(bd.PausedCauses(w.bead), want) {
		t.Errorf("remaining = %q, labels %q, want %q kept", r.Remaining, w.bead.Labels, want)
	}
	if got := (*evs)[0].Attrs["remaining"].([]string); !slices.Equal(got, []string{"manual", "integrate-conflict", "failing"}) {
		t.Errorf("recover.done remaining = %q", got)
	}
}

// A dirty tree is refused naming its path, writing nothing, unless Discard; with
// it the tree is dropped and the discard recorded. Nothing is ever committed.
func TestRecoverDirtyTree(t *testing.T) {
	evs := eventlogtest.Capture(t)
	w := &world{bead: stranded(), tree: "/tmp/mugicrew-wt-1/wt", files: 3}
	stub(t, w)
	_, err := Recover("mc-a", Opts{})
	var rf *Refused
	if !errors.As(err, &rf) || !strings.Contains(err.Error(), "/tmp/mugicrew-wt-1/wt") || !strings.Contains(err.Error(), "--discard") {
		t.Fatalf("err = %v, want refused naming the path and --discard", err)
	}
	if want := []string{"lock", "probe"}; !slices.Equal(w.calls, want) {
		t.Errorf("calls = %q, want no write", w.calls)
	}
	*evs, w.calls = nil, nil
	r, err := Recover("mc-a", Opts{Discard: true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.Lines, "dropped tree /tmp/mugicrew-wt-1/wt, discarding 3 changed files") {
		t.Errorf("lines = %q, want the discard named", r.Lines)
	}
	if a := (*evs)[0].Attrs; a["discarded"] != true {
		t.Errorf("recover.done = %v, want discarded", a)
	}
}

// recover's own pid and its ancestors (its shell, inside the tree, the id in its
// argv) are left out of both the probe and the cwd scan (prior art
// TestAbortExcludesItsOwnPid).
func TestRecoverExcludesItsOwnLineage(t *testing.T) {
	eventlogtest.Capture(t)
	self := os.Getpid()
	w := &world{bead: stranded(), tree: "/tmp/mugicrew-wt-1/wt", cwd: []int{self, 1}, probe: func(int) []int { return []int{self} }}
	stub(t, w)
	if _, err := Recover("mc-a", Opts{}); err != nil {
		t.Fatalf("Recover = %v, want its own lineage excluded", err)
	}
	if !slices.Equal(w.excl, []int{self, 1}) {
		t.Errorf("cwd scan excluded %v, want own lineage [%d 1]", w.excl, self)
	}
}

// An AFK bead the probe reads dead but not paused abandoned is ambiguous: it
// repairs as ambiguous-resolved once the loop lock is free and no TUI but the
// caller's own is alive, and that is judged again after the fence (the fence
// itself never stands in for abandoned in the run that set it).
func TestRecoverAmbiguous(t *testing.T) {
	evs := eventlogtest.Capture(t)
	claimed := bd.Bead{ID: "mc-a", Kind: contract.KindBuild, Parent: "mc-cp", Status: "in_progress"}
	w := &world{bead: claimed, tuis: func(int) []string { return []string{"tui-9"} }}
	stub(t, w)
	r, err := Recover("mc-a", Opts{ExcludeTUISession: "tui-9"})
	if err != nil || r.Verdict != AmbiguousResolved {
		t.Fatalf("Recover = %+v, %v; want ambiguous-resolved with its own TUI excluded", r, err)
	}
	if v := (*evs)[len(*evs)-1].Attrs["verdict"]; v != AmbiguousResolved {
		t.Errorf("recover.done verdict = %v", v)
	}

	// A TUI that appears between check and fence refuses at the re-check; the fence stays.
	*evs, w.calls, w.reads = nil, nil, 0
	w.bead = claimed
	w.tuis = func(n int) []string { return [][]string{nil, {"tui-3"}}[n-1] }
	_, err = Recover("mc-a", Opts{})
	var rf *Refused
	if !errors.As(err, &rf) || !strings.Contains(err.Error(), "a TUI is alive") || !bd.PausedBy(w.bead, bd.PausedRecovering) || w.bead.Status != "in_progress" {
		t.Fatalf("err = %v, bead %+v; want refused at the re-check with the fence left", err, w.bead)
	}

	// A loop that is on refuses before the fence.
	*evs, w.calls, w.reads = nil, nil, 0
	w.bead, w.tuis, w.loop = claimed, nil, looplock.Draining
	if _, err = Recover("mc-a", Opts{}); !errors.As(err, &rf) || !strings.Contains(err.Error(), "the loop is on") || len(w.bead.Labels) != 0 {
		t.Errorf("err = %v, labels %q; want refused naming the loop, unfenced", err, w.bead.Labels)
	}
}

// --force overrides an ambiguous verdict, recording forced, but is refused while
// the loop lock is held, before anything is read or written.
func TestRecoverForce(t *testing.T) {
	evs := eventlogtest.Capture(t)
	w := &world{bead: bd.Bead{ID: "mc-a", Kind: contract.KindSpec, Status: "in_progress", Labels: []string{"paused:abandoned"}}, loop: looplock.On}
	stub(t, w)
	_, err := Recover("mc-a", Opts{Force: true})
	var rf *Refused
	if !errors.As(err, &rf) || !strings.Contains(err.Error(), "loop is off") || !slices.Equal(w.calls, []string{"lock"}) {
		t.Fatalf("err = %v, calls %q; want refused on the lock alone", err, w.calls)
	}
	if names := eventlogtest.Names(*evs); !slices.Equal(names, []string{"recover.refused"}) {
		t.Errorf("events = %v, want one recover.refused", names)
	}
	*evs, w.loop = nil, ""
	r, err := Recover("mc-a", Opts{Force: true})
	if err != nil || r.Verdict != Forced || (*evs)[len(*evs)-1].Attrs["verdict"] != Forced {
		t.Fatalf("Recover = %+v, %v, events %v; want forced", r, err, *evs)
	}
}

// A HITL bead whose session is gone runs the full repair as a dead AFK bead does;
// one whose session Exited (resumable) is refused naming the TUI drop, with no
// write, not even the fence, so the Drafts a resume would publish are kept.
func TestRecoverHITLBySession(t *testing.T) {
	evs := eventlogtest.Capture(t)
	w := &world{bead: hitl(), tree: "/tmp/mugicrew-wt-1/wt", drafts: []string{"mc-d1"}, sess: session.Info{State: session.Exited}}
	stub(t, w)
	_, err := Recover("mc-a", Opts{})
	var rf *Refused
	if !errors.As(err, &rf) || !strings.Contains(err.Error(), "drop it from the TUI") {
		t.Fatalf("err = %v, want refused naming the TUI drop", err)
	}
	if want := []string{"lock", "probe"}; !slices.Equal(w.calls, want) {
		t.Errorf("exited calls = %q, want no write", w.calls)
	}
	if names := eventlogtest.Names(*evs); !slices.Equal(names, []string{"recover.refused"}) {
		t.Errorf("exited events = %v, want one recover.refused", names)
	}

	*evs, w.calls, w.sess = nil, nil, session.Info{}
	if _, err := Recover("mc-a", Opts{}); err != nil {
		t.Fatalf("gone session: Recover = %v, want repaired", err)
	}
	want := []string{"lock", "probe", "pause recovering", "lock", "probe", "drop /tmp/mugicrew-wt-1/wt", "delete drafts", "unclaim",
		"unpause abandoned", "unpause recovering"}
	if !slices.Equal(w.calls, want) {
		t.Errorf("gone calls = %q\nwant      %q", w.calls, want)
	}
}

// A tree stopped mid-rebase (detached, so only its rebase state names
// effort/<id>) has its rebase aborted between the re-check and the tree drop; its
// conflicted files are the rebase's, not uncommitted work, so no --discard is
// needed. failing, which the CutFailed loop it caused set, is lifted with it.
func TestRecoverAbortsAStoppedRebase(t *testing.T) {
	evs := eventlogtest.Capture(t)
	w := &world{bead: stranded(), tree: "/tmp/mugicrew-wt-1/wt", rebase: true, files: 2}
	w.bead.Labels = append(w.bead.Labels, "paused:failing")
	stub(t, w)
	r, err := Recover("mc-a", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"lock", "probe", "pause recovering", "lock", "probe", "abort rebase /tmp/mugicrew-wt-1/wt", "drop /tmp/mugicrew-wt-1/wt", "unclaim",
		"unpause abandoned", "unpause recovering", "unpause failing"}
	if !slices.Equal(w.calls, want) {
		t.Errorf("calls = %q\nwant    %q", w.calls, want)
	}
	if !slices.Contains(r.Lines, "aborted the rebase in /tmp/mugicrew-wt-1/wt") || len(r.Remaining) != 0 {
		t.Errorf("report = %q remaining %q, want the abort named and nothing left", r.Lines, r.Remaining)
	}
	if a := (*evs)[0].Attrs; a["rebase_aborted"] != true || a["discarded"] != false ||
		!slices.Equal(a["lifted"].([]string), []string{"abandoned", "recovering", "failing"}) {
		t.Errorf("recover.done = %v, want rebase_aborted and failing lifted", a)
	}
}

// A rebase abort that leaves files behind (an autostash put back) is a dirty tree:
// refused without --discard, the fence left, and the re-run drops it with the
// abort's step a no-op.
func TestRecoverDirtyAfterRebaseAbort(t *testing.T) {
	eventlogtest.Capture(t)
	w := &world{bead: stranded(), tree: "/tmp/mugicrew-wt-1/wt", rebase: true, after: 1}
	stub(t, w)
	_, err := Recover("mc-a", Opts{})
	var rf *Refused
	if !errors.As(err, &rf) || !strings.Contains(err.Error(), "--discard") || !bd.PausedBy(w.bead, bd.PausedRecovering) {
		t.Fatalf("err = %v labels %q, want refused naming --discard with the fence left", err, w.bead.Labels)
	}
	w.calls = nil
	if _, err := Recover("mc-a", Opts{Discard: true}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"lock", "probe", "lock", "probe", "drop /tmp/mugicrew-wt-1/wt", "unclaim", "unpause abandoned", "unpause recovering"}; !slices.Equal(w.calls, want) {
		t.Errorf("re-run calls = %q, want %q", w.calls, want)
	}
}

// Assess is the TUI's press-time read (Spec FR36-39): the inventory and the
// verdict, writing nothing and logging nothing. A dirty tree is the confirm's to
// state, not a refusal; an ambiguous holder's refusal says so, so the TUI can
// point at mugicrew recover --force instead of a confirm; one alive is not ambiguous.
func TestAssess(t *testing.T) {
	evs := eventlogtest.Capture(t)
	w := &world{bead: stranded(), tree: "/tmp/mugicrew-wt-1/wt", files: 2, drafts: []string{"mc-d1"}}
	stub(t, w)
	inv, v, err := Assess("mc-a", Opts{})
	if err != nil || v != Dead || inv.Tree != w.tree || inv.Files != 2 || !slices.Equal(inv.Drafts, []string{"mc-d1"}) {
		t.Fatalf("Assess = %+v, %q, %v; want dead with its tree, 2 files and its Draft", inv, v, err)
	}
	if want := []string{"lock", "probe"}; !slices.Equal(w.calls, want) || len(*evs) != 0 {
		t.Errorf("calls = %q events %v; want reads only, no event", w.calls, *evs)
	}

	w.bead, w.tree, w.files = bd.Bead{ID: "mc-a", Kind: contract.KindBuild, Parent: "mc-cp", Status: "in_progress"}, "", 0
	w.loop = looplock.On
	_, _, err = Assess("mc-a", Opts{})
	var rf *Refused
	if !errors.As(err, &rf) || rf.Verdict != Ambiguous {
		t.Errorf("loop on: err = %v; want an ambiguous refusal", err)
	}
	w.loop, w.tuiErr = "", errors.New("tmux: EACCES")
	if _, _, err = Assess("mc-a", Opts{}); !errors.As(err, &rf) || rf.Verdict == Ambiguous {
		t.Errorf("TUI read fails: err = %v; want a refusal that is not ambiguous", err)
	}
	w.tuiErr, w.probe = nil, func(int) []int { return []int{7} }
	if _, _, err = Assess("mc-a", Opts{}); !errors.As(err, &rf) || rf.Verdict == Ambiguous {
		t.Errorf("probe alive: err = %v; want a refusal that is not ambiguous", err)
	}

	w.probe, w.cwdErr, w.tree = nil, errors.New("lsof: not found"), "/tmp/mugicrew-wt-1/wt"
	if _, _, err = Assess("mc-a", Opts{}); !errors.As(err, &rf) || rf.Verdict == Ambiguous || !strings.Contains(err.Error(), "cwd") {
		t.Errorf("cwd read fails: err = %v; want a refusal that is not ambiguous naming the cwd read", err)
	}
	w.cwdErr, w.tree = nil, ""

	w.bead, w.probe, w.drafts = bd.Bead{ID: "mc-a", Kind: contract.KindBuild, Status: "open"}, nil, nil
	if _, v, err = Assess("mc-a", Opts{}); err != nil || v != "" {
		t.Errorf("nothing stranded: %q, %v; want no verdict", v, err)
	}
	if len(*evs) != 0 {
		t.Errorf("events = %v, want none", *evs)
	}
}

// A shell without BEADS_DIR still reads the loop lock: recover resolves the Bead
// Graph itself, so --force is refused for the held loop, not failed at check.
func TestRecoverForceResolvesGraphWithoutBeadsDir(t *testing.T) {
	graph := t.TempDir()
	t.Setenv("BEADS_DIR", "")
	set(t, &graphDir, func(string) (string, error) { return graph, nil })
	set(t, &git.RepoRoot, func() (string, error) { return t.TempDir(), nil })
	set(t, &git.CommonDir, func() (string, error) { return t.TempDir(), nil })
	lock, err := looplock.Acquire(filepath.Join(graph, "mugicrew-loop.lock"), "rev", "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lock.Close() })
	_, err = Recover("mc-x", Opts{Force: true})
	var r *Refused
	if !errors.As(err, &r) {
		t.Fatalf("Recover(--force, loop held, no BEADS_DIR) = %v, want a *Refused", err)
	}
}

// A lock read error before the fence is a failed check, as on the --force pre-check.
func TestRecoverLockReadErrorFailsCheck(t *testing.T) {
	w := &world{bead: stranded(), tree: "/tmp/mugicrew-wt-1/wt"}
	stub(t, w)
	set(t, &loopState, func() (string, error) { return "", errors.New("boom") })
	_, err := Recover("mc-a", Opts{})
	var f *Failed
	if !errors.As(err, &f) || f.Step != stepCheck {
		t.Fatalf("err = %v, want a Failed at step check", err)
	}
}

// A bare shell (no BEADS_DIR) reads the same per-effort lock the dispatcher took:
// recover refuses a held effort and, once the hold is released, proceeds.
func TestRecoverBareShellReadsDispatcherHold(t *testing.T) {
	graph := t.TempDir()
	t.Setenv("BEADS_DIR", graph)
	hold, err := liveness.Acquire("mc-a")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEADS_DIR", "")
	set(t, &graphDir, func(string) (string, error) { return graph, nil })
	set(t, &git.RepoRoot, func() (string, error) { return t.TempDir(), nil })
	set(t, &git.CommonDir, func() (string, error) { return t.TempDir(), nil })
	w := &world{bead: stranded(), tree: "/tmp/mugicrew-wt-1/wt"}
	stub(t, w)
	set(t, &loopState, func() (string, error) { return looplock.Off, nil })
	_, err = Recover("mc-a", Opts{Force: true})
	var r *Refused
	if !errors.As(err, &r) || r.Verdict != Alive || strings.Contains(err.Error(), "pid") {
		t.Fatalf("Recover under a held lock = %v, want an Alive refusal naming no pid", err)
	}
	hold.Release()
	if _, err := Recover("mc-a", Opts{Force: true}); err != nil {
		t.Fatalf("Recover after release = %v", err)
	}
}

// nested stubs w's holder as a repo = "sub" child inside a real host dir.
func nested(t *testing.T, w *world, rebase bool) (host, child string) {
	t.Helper()
	host = t.TempDir()
	child = filepath.Join(host, "sub")
	w.tree, w.rebase = host, rebase
	set(t, &holderAcross, func(string, map[string]string) (git.Holder, git.Repo, error) {
		return git.Holder{Path: child, Host: host, Dir: "sub", Rebasing: w.rebase}, git.Repo("/ws/sub"), nil
	})
	set(t, &repoDirs, func() map[string]string { return map[string]string{"s": "sub"} })
	return host, child
}

// A nested holder is recovered child first (Spec FR 52): its rebase aborted in the
// child, both trees' files counted (--discard covers both), the host dropped (which
// takes the child down first), and recover.done names the repo.
func TestRecoverNestedHolder(t *testing.T) {
	evs := eventlogtest.Capture(t)
	w := &world{bead: stranded(), after: 1}
	stub(t, w)
	host, child := nested(t, w, true)
	set(t, &dirtyFiles, func(p string) (int, error) {
		if p == child || p == host {
			return 1 + w.after, nil
		}
		t.Errorf("counted files in %s", p)
		return 0, nil
	})
	if _, err := Recover("mc-a", Opts{}); err == nil || !strings.Contains(err.Error(), "--discard") {
		t.Fatalf("a dirty child and host must refuse without --discard, got %v", err)
	}
	if !slices.Contains(w.calls, "abort rebase "+child) || slices.Contains(w.calls, "drop "+host) {
		t.Errorf("calls = %q, want the abort in the child and no drop yet", w.calls)
	}
	r, err := Recover("mc-a", Opts{Discard: true}) // the re-run rolls forward past the abort
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(w.calls, "drop "+host) || !slices.Contains(r.Lines, "dropped tree "+host+" (child "+child+" first), discarding 4 changed files") {
		t.Errorf("calls = %q lines = %q, want the host dropped with both trees' files", w.calls, r.Lines)
	}
	done := (*evs)[len(*evs)-1].Attrs
	if done["repo"] != "sub" || done["tree"] != host || done["discarded"] != true {
		t.Errorf("recover.done = %v, want repo=sub tree=host", done)
	}
}

// A drifted nested bead is refused with the hint, before any write (Spec FR 55).
func TestRecoverDriftRefuses(t *testing.T) {
	w := &world{bead: stranded()}
	w.bead.Labels = append(w.bead.Labels, "context:s", "repo:old")
	stub(t, w)
	nested(t, w, false)
	_, err := Recover("mc-a", Opts{})
	var rf *Refused
	if !errors.As(err, &rf) || !strings.Contains(err.Error(), "clear paused:context first") {
		t.Fatalf("err = %v, want the drift refusal", err)
	}
	if slices.Contains(w.calls, "pause recovering") {
		t.Errorf("calls = %q, drift must refuse before the fence", w.calls)
	}
}

// The stamp lives on the container: an unstamped leaf under a drifted container is
// refused too (Spec FR 55, D5).
func TestRecoverDriftRefusesUnstampedLeaf(t *testing.T) {
	w := &world{bead: stranded()}
	w.bead.Parent = "mc-c"
	stub(t, w)
	nested(t, w, false)
	set(t, &show, func(id string) (bd.Bead, error) {
		if id == "mc-c" {
			return bd.Bead{ID: id, Labels: []string{"context:s", "repo:old"}}, nil
		}
		return w.bead, nil
	})
	_, err := Recover("mc-a", Opts{})
	var rf *Refused
	if !errors.As(err, &rf) || !strings.Contains(err.Error(), "clear paused:context first") {
		t.Fatalf("err = %v, want the drift refusal", err)
	}
}

// A Workspace with no context reads no config in Take and Recover: the holder
// lookup alone runs, once for the cwd, and no repo is searched.
func TestRecoverNoContextReadsNoRepos(t *testing.T) {
	w := &world{bead: stranded(), tree: "/tmp/mugicrew-wt-1/wt"}
	stub(t, w)
	lookups := 0
	set(t, &holderAcross, func(string, map[string]string) (git.Holder, git.Repo, error) {
		lookups++
		return git.Holder{Path: w.tree}, "", nil
	})
	set(t, &repoDirs, func() map[string]string { return nil }) // config-only when nothing is declared
	if _, err := Recover("mc-a", Opts{}); err != nil {
		t.Fatal(err)
	}
	if lookups != 2 { // the check and the re-check each take the inventory
		t.Errorf("holder lookups = %d, want 2", lookups)
	}
}

// A parented leaf in a Workspace with no repo = declared costs no parent show.
func TestRecoverNoContextLeafSkipsParentShow(t *testing.T) {
	w := &world{bead: stranded()}
	w.bead.Parent = "mc-c"
	stub(t, w)
	set(t, &repoDirs, func() map[string]string { return nil })
	set(t, &show, func(id string) (bd.Bead, error) {
		if id == "mc-c" {
			t.Error("parent shown")
		}
		return w.bead, nil
	})
	if _, err := Recover("mc-a", Opts{}); err != nil {
		t.Fatal(err)
	}
}

// A stranded Initiative needs no special case: with no session and no effort
// branch it judges Dead, and recover drops its Drafts and unclaims with no tree.
func TestRecoverStrandedInitiative(t *testing.T) {
	w := &world{bead: bd.Bead{ID: "mc-a", Kind: contract.KindInitiative, Container: true, Status: "in_progress"}, drafts: []string{"mc-d1"}}
	stub(t, w)
	if _, err := Recover("mc-a", Opts{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"lock", "probe", "pause recovering", "lock", "probe", "delete drafts", "unclaim", "unpause recovering"}
	if !slices.Equal(w.calls, want) {
		t.Errorf("calls = %q\nwant    %q", w.calls, want)
	}
}
