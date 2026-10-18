package session

import (
	"errors"
	"io"
	"slices"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/route"
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
		{"held", false, 2, false, "in_progress", "open", "", true, true, []string{"integrate", "abort /wt", "pause mc-b integrate-conflict"}},
		{"landed", false, 3, false, "in_progress", "open", "", false, true, []string{"integrate", "abort /wt", "signal mc-b", "close mc-b obsolete: container landed; carried by mc-sig", "teardown"}},
		{"landed on a closed bead", false, 3, false, "closed", "open", "", false, true, []string{"integrate", "abort /wt", "signal mc-b", "reopen mc-b", "close mc-b obsolete: container landed; carried by mc-sig", "teardown"}},
		{"landed, facts fail", false, 3, false, "in_progress", "open", "facts", true, true, []string{"integrate", "abort /wt", "pause mc-b integrate-conflict"}},
		{"landed, signal fails", false, 3, false, "in_progress", "open", "signal", true, true, []string{"integrate", "abort /wt", "signal mc-b", "pause mc-b integrate-conflict"}},
		{"landed, close fails", false, 3, false, "in_progress", "open", "close", true, true, []string{"integrate", "abort /wt", "signal mc-b", "close mc-b obsolete: container landed; carried by mc-sig", "pause mc-b integrate-conflict"}},
		{"swept, landed", false, 3, false, "closed", "closed", "member", false, true, []string{"teardown"}},
		{"no cut point", false, 4, false, "in_progress", "open", "", true, true, []string{"integrate", "abort /wt", "pause mc-b integrate-conflict"}},
		{"no cut point on a closed bead", false, 4, false, "closed", "open", "", true, true, []string{"integrate", "abort /wt", "pause mc-b integrate-conflict", "reopen mc-b"}},
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
			names := eventlogtest.Names(*evs)
			if landed := c.exit == 3 && !c.stranded && c.sweep == ""; slices.Contains(names, "effort.stranded") != landed {
				t.Errorf("events %v: effort.stranded logged %v, want %v", names, !landed, landed)
			}
			paused := slices.Contains(names, "effort.paused")
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
	origF, origSig, origC := landedFacts, fileStranded, closeBead
	t.Cleanup(func() { landedFacts, fileStranded, closeBead = origF, origSig, origC })
	landedFacts = func(b bd.Bead, _ *Child) (strandedInfo, error) {
		if sweep == "facts" {
			return strandedInfo{}, errors.New("no facts")
		}
		return strandedInfo{Title: "t", Body: "b"}, nil
	}
	fileStranded = func(origin, title, body string) (string, bool, error) {
		*did = append(*did, "signal "+origin)
		if sweep == "signal" {
			return "", false, errors.New("bd down")
		}
		return "mc-sig", true, nil
	}
	closeBead = func(id, reason string) error {
		*did = append(*did, "close "+id+" "+reason)
		if sweep == "close" {
			return errors.New("bd down")
		}
		return nil
	}
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
	origT, origN := trailerCount, notBuiltNote
	t.Cleanup(func() { trailerCount, notBuiltNote = origT, origN })
	trailerCount = func(git.Repo, string, string) (int, error) { return 1, nil }
	notBuiltNote = func(string) string { return "" }
	deleteBranch = func(name string) error { *did = append(*did, "delete "+name); return nil }
}

// A re-run Build carrying paused:not-built is unpaused only when its trailer count
// is above zero (ADR 0248); a count of 0 re-pauses it. Either way it is not reopened
// on the lift path, and its branch is deleted on the lift.
func TestBackstopLiftsNotBuilt(t *testing.T) {
	for _, c := range []struct {
		name  string
		count int
		want  []string
	}{
		{"built", 1, []string{"integrate", "teardown", "unpause mc-b not-built", "delete effort/mc-b"}},
		{"built nothing", 0, []string{"integrate", "teardown", "pause mc-b not-built", "reopen mc-b"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			evs := eventlogtest.Capture(t)
			var did []string
			stubBackstop(t, &did, 0, false, "closed", "open", "")
			origU := unpause
			t.Cleanup(func() { unpause = origU })
			unpause = func(id, cause string) error { did = append(did, "unpause "+id+" "+cause); return nil }
			trailerCount = func(git.Repo, string, string) (int, error) { return c.count, nil }
			b := bd.Bead{ID: "mc-b", Kind: contract.KindBuild, Parent: "ws-cp", Labels: []string{"paused:" + bd.PausedNotBuilt}}
			stranded, kept := Backstop(b, "/wt", false, func() { did = append(did, "teardown") }, io.Discard)
			if stranded != (c.count == 0) || kept != (c.count == 0) || !slices.Equal(did, c.want) {
				t.Errorf("stranded, kept = %v, %v; did %q, want %q", stranded, kept, did, c.want)
			}
			resumed := false
			for _, e := range *evs {
				resumed = resumed || e.Name == "effort.resumed" && e.Attrs["cause"] == bd.PausedNotBuilt
			}
			if resumed != (c.count > 0) {
				t.Errorf("effort.resumed logged = %v", resumed)
			}
		})
	}
}

// An exit-0 integrate of a closed, unswept Build with no integrate trailer on its
// Checkpoint is a Build that built nothing: tear down, keep effort/<id>, pause
// not-built, reopen, report (stranded, kept) with the agent's "Not built:" note on
// effort.paused (ADR 0248). Anything else keeps the integrated arm.
func TestBackstopPausesNotBuilt(t *testing.T) {
	cases := []struct {
		name   string
		kind   contract.Kind
		status string
		sweep  string
		count  int
		err    error
		note   string
		paused bool
	}{
		{"closed, no trailer", contract.KindBuild, "closed", "", 0, nil, "", true},
		{"closed, no trailer, noted", contract.KindBuild, "closed", "", 0, nil, "no repo", true},
		{"count errors", contract.KindBuild, "closed", "", 0, errors.New("git down"), "", true},
		{"pivoted build still open", contract.KindBuild, "in_progress", "", 0, nil, "", false},
		{"swept during integrate", contract.KindBuild, "closed", "during", 0, nil, "", false},
		{"closed surgery", contract.KindSurgery, "closed", "", 0, nil, "", false},
		{"closed, trailer present", contract.KindBuild, "closed", "", 1, nil, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			evs := eventlogtest.Capture(t)
			var did []string
			stubBackstop(t, &did, 0, false, c.status, "open", c.sweep)
			trailerCount = func(_ git.Repo, parent, trailer string) (int, error) {
				if parent != "ws-cp" || trailer != git.TrailerFor("build", "mc-b") {
					t.Errorf("counted %q %q", parent, trailer)
				}
				return c.count, c.err
			}
			notBuiltNote = func(string) string { return c.note }
			b := bd.Bead{ID: "mc-b", Kind: c.kind, Parent: "ws-cp"}
			stranded, kept := Backstop(b, "/wt", false, func() { did = append(did, "teardown") }, io.Discard)
			want := []string{"integrate", "teardown", "delete effort/mc-b"}
			if c.paused {
				want = []string{"integrate", "teardown", "pause mc-b not-built", "reopen mc-b"}
			}
			if stranded != c.paused || kept != c.paused {
				t.Errorf("stranded, kept = %v, %v", stranded, kept)
			}
			if !slices.Equal(did, want) {
				t.Errorf("did %q, want %q", did, want)
			}
			var ev map[string]any
			for _, e := range *evs {
				if e.Name == "effort.paused" {
					ev = e.Attrs
				}
			}
			if (ev != nil) != c.paused {
				t.Fatalf("events %v", eventlogtest.Names(*evs))
			}
			if !c.paused {
				return
			}
			if ev["cause"] != bd.PausedNotBuilt || ev["branch"] != "effort/mc-b" || ev["tree"] != nil {
				t.Errorf("attrs %v", ev)
			}
			if got, _ := ev["note"].(string); got != c.note {
				t.Errorf("note %q, want %q", got, c.note)
			}
			if _, has := ev["note"]; has != (c.note != "") {
				t.Errorf("note present = %v", has)
			}
			if _, has := ev["error"]; has != (c.err != nil) {
				t.Errorf("error present = %v", has)
			}
		})
	}
}

// A Build integrated into a nested repo's Checkpoint has its trailer counted in the
// child repo (kid.Main), not the host's tree, so it is not paused as not built (FR 29, ADR 0248).
func TestBackstopCountsNestedBuildTrailerInChild(t *testing.T) {
	var did []string
	stubBackstop(t, &did, 0, false, "closed", "open", "")
	origP := probeChildren
	t.Cleanup(func() { probeChildren = origP })
	kid := Child{Repo: "/wt/child", Main: "/root/child", Context: "c", Dir: "child", Effort: "mc-b"}
	probeChildren = func(string) []Child { return []Child{kid} }
	var counted git.Repo
	trailerCount = func(repo git.Repo, parent, trailer string) (int, error) {
		counted = repo
		return 1, nil
	}
	b := bd.Bead{ID: "mc-b", Kind: contract.KindBuild, Parent: "ws-cp"}
	stranded, kept := Backstop(b, "/wt", false, func() { did = append(did, "teardown") }, io.Discard)
	if counted != kid.Main {
		t.Errorf("counted in %q, want the child's %q", counted, kid.Main)
	}
	if stranded || kept || slices.Contains(did, "pause mc-b not-built") {
		t.Errorf("stranded, kept = %v, %v; did %q", stranded, kept, did)
	}
}

// A replace bead refused off its pinned Tip (FR 33–35), Franky's Conflict and
// Nami's Chart-conflict alike: closed moot when the container already descends from
// its Base:; else re-pinned once (Tip: replaced, Re-pinned: added, branch and cut
// ref dropped, bead left open); else paused on the second refusal.
func TestBackstopOffPin(t *testing.T) {
	const body = "Fix.\nBase: b4se\nTip: 0ld"
	for _, kind := range []contract.Kind{contract.KindConflict, contract.KindChartConflict} {
		for _, c := range []struct {
			name     string
			desc     string
			onBase   bool
			stranded bool
			want     []string
		}{
			{"moot", body, true, false, []string{"integrate", "abort /wt", "close mc-b moot: container already on base b4se", "teardown", "delete effort/mc-b"}},
			{"re-pin", body, false, false, []string{"integrate", "abort /wt", "describe mc-b Fix.\nBase: b4se\nTip: n3w\nRe-pinned: 0ld", "teardown", "delete effort/mc-b", "reopen mc-b"}},
			{"no pinned tip", "Fix.\nBase: b4se", false, true, []string{"integrate", "abort /wt", "pause mc-b integrate-conflict", "reopen mc-b"}},
			{"second refusal", body + "\nRe-pinned: 0lder", false, true, []string{"integrate", "abort /wt", "pause mc-b integrate-conflict", "reopen mc-b"}},
		} {
			t.Run(string(kind)+" "+c.name, func(t *testing.T) {
				evs := eventlogtest.Capture(t)
				var did []string
				stubBackstop(t, &did, ExitOffPin, false, "closed", "open", "")
				origS, origR, origT, origA, origD := showBead, routeBead, containerTip, isAncestor, setDescription
				t.Cleanup(func() {
					showBead, routeBead, containerTip, isAncestor, setDescription = origS, origR, origT, origA, origD
				})
				showBead = func(id string) (bd.Bead, error) {
					return bd.Bead{ID: id, Kind: kind, Status: "closed", Parent: "ws-cp", Description: c.desc}, nil
				}
				routeBead = func(bd.Bead) (route.Destination, error) { return route.Destination{Container: "checkpoint/ws-cp"}, nil }
				containerTip = func(git.Repo, string) (string, error) { return "n3w", nil }
				isAncestor = func(_ git.Repo, a, b string) bool { return c.onBase && a == "b4se" && b == "n3w" }
				setDescription = func(id, d string) error { did = append(did, "describe "+id+" "+d); return nil }
				closeBead = func(id, reason string) error { did = append(did, "close "+id+" "+reason); return nil }
				b := bd.Bead{ID: "mc-b", Kind: kind, Parent: "ws-cp"}
				stranded, kept := Backstop(b, "/wt", false, func() { did = append(did, "teardown") }, io.Discard)
				if stranded != c.stranded || !kept {
					t.Errorf("stranded, kept = %v, %v; want %v, true", stranded, kept, c.stranded)
				}
				if !slices.Equal(did, c.want) {
					t.Errorf("did %q, want %q", did, c.want)
				}
				if paused := slices.Contains(eventlogtest.Names(*evs), "effort.paused"); paused != c.stranded {
					t.Errorf("effort.paused logged %v, want %v", paused, c.stranded)
				}
				if c.name == "no pinned tip" {
					for _, e := range *evs {
						if e.Name == "effort.paused" && e.Attrs["reason"] != "no-pinned-tip" {
							t.Errorf("pause reason = %v, want no-pinned-tip", e.Attrs["reason"])
						}
					}
				}
			})
		}
	}
}
