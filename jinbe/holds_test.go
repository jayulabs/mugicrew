package jinbe

import (
	"errors"
	"slices"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/session"
)

// holdRig stubs the step's seams: blocked beads by errand id, a queue of session
// tables (the last repeats), and records the closes.
type holdRig struct {
	targets map[string][]bd.Bead
	tables  []map[string]session.Info
	tabErr  error
	reads   int
	closed  map[string]string
	reasons []string
}

func newHoldRig(t *testing.T) *holdRig {
	t.Helper()
	r := &holdRig{targets: map[string][]bd.Bead{}, closed: map[string]string{}}
	oldT, oldS, oldC := holdTargets, holdSessions, holdClose
	t.Cleanup(func() { holdTargets, holdSessions, holdClose = oldT, oldS, oldC })
	holdTargets = func(id string) ([]bd.Bead, error) { return r.targets[id], nil }
	holdSessions = func() (map[string]session.Info, error) {
		if r.tabErr != nil {
			return nil, r.tabErr
		}
		i := min(r.reads, len(r.tables)-1)
		r.reads++
		if i < 0 {
			return nil, nil
		}
		return r.tables[i], nil
	}
	holdClose = func(id, reason string) error { r.closed[id] = reason; return nil }
	return r
}

func (r *holdRig) run(open []bd.Bead) (checked []string) {
	ClearHolds(open, func(string) bool { return true }, func(id string) { checked = append(checked, id) })
	return checked
}

func errand(id, status, key string) bd.Bead {
	return bd.Bead{ID: id, Kind: contract.KindErrand, Status: status, Description: "Lead.\n" + key + "\nMR link: x\n"}
}

func closedBlocked(id string, kind contract.Kind, reason string) bd.Bead {
	return bd.Bead{ID: id, Kind: kind, Status: "closed", CloseReason: reason}
}

func TestClearHoldsMootEveryKeyLineAndKind(t *testing.T) {
	keys := []string{"Closed MR: !12", "Merged MR: !12", "Pipeline: 4411", "Foreign: abc123"}
	kinds := []contract.Kind{contract.KindCheckpoint, contract.KindChart, contract.KindRollback}
	for _, k := range keys {
		for _, kind := range kinds {
			t.Run(k+"/"+string(kind), func(t *testing.T) {
				r := newHoldRig(t)
				r.targets["e1"] = []bd.Bead{closedBlocked("c1", kind, "obsolete: Spec superseded")}
				r.run([]bd.Bead{errand("e1", "open", k)})
				if got := r.closed["e1"]; got != "obsolete: c1 closed" {
					t.Fatalf("reason = %q", got)
				}
			})
		}
	}
}

func TestClearHoldsLeavesAlone(t *testing.T) {
	cases := map[string]struct {
		e       bd.Bead
		targets []bd.Bead
	}{
		"withdrawn":    {errand("e1", "open", "Closed MR: !1"), []bd.Bead{closedBlocked("c", contract.KindCheckpoint, "withdrawn")}},
		"withdrawn: x": {errand("e1", "open", "Closed MR: !1"), []bd.Bead{closedBlocked("c", contract.KindCheckpoint, "withdrawn: head")}},
		"cancelled":    {errand("e1", "open", "Closed MR: !1"), []bd.Bead{closedBlocked("c", contract.KindChart, "cancelled: why")}},
		"draft":        {errand("e1", "deferred", "Closed MR: !1"), []bd.Bead{closedBlocked("c", contract.KindCheckpoint, "x")}},
		"no key":       {errand("e1", "open", "Ask: something"), []bd.Bead{closedBlocked("c", contract.KindCheckpoint, "x")}},
		"no target":    {errand("e1", "open", "Closed MR: !1"), nil},
		"two targets": {errand("e1", "open", "Closed MR: !1"), []bd.Bead{
			closedBlocked("c", contract.KindCheckpoint, "x"), closedBlocked("d", contract.KindCheckpoint, "x")}},
		"other kind": {errand("e1", "open", "Closed MR: !1"), []bd.Bead{closedBlocked("c", contract.KindSignal, "x")}},
		"still open": {errand("e1", "open", "Closed MR: !1"), []bd.Bead{{ID: "c", Kind: contract.KindCheckpoint, Status: "open"}}},
		"not errand": {bd.Bead{ID: "e1", Kind: contract.KindLatch, Status: "open", Description: "Closed MR: !1\n"},
			[]bd.Bead{closedBlocked("c", contract.KindCheckpoint, "x")}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newHoldRig(t)
			r.targets["e1"] = c.targets
			r.run([]bd.Bead{c.e})
			if len(r.closed) != 0 {
				t.Fatalf("closed %v", r.closed)
			}
		})
	}
}

// An Errand blocking one open and one closed bead has two targets, not one: the
// open list alone would read it as exactly one.
func TestClearHoldsCountsClosedTargetsToo(t *testing.T) {
	r := newHoldRig(t)
	r.targets["e1"] = []bd.Bead{closedBlocked("c0", contract.KindCheckpoint, "x"), {ID: "c", Kind: contract.KindCheckpoint, Status: "open"}}
	r.run([]bd.Bead{errand("e1", "open", "Foreign: abc"), {ID: "c", Kind: contract.KindCheckpoint, Status: "open", BlockedBy: []string{"e1"}}})
	if len(r.closed) != 0 {
		t.Fatalf("closed %v", r.closed)
	}
}

func TestClearHoldsLiveness(t *testing.T) {
	moot := func(r *holdRig) []bd.Bead {
		r.targets["e1"] = []bd.Bead{closedBlocked("c", contract.KindCheckpoint, "x")}
		return []bd.Bead{errand("e1", "in_progress", "Closed MR: !1")}
	}
	alive := map[string]session.Info{"e1": {State: session.Alive}}
	closing := map[string]session.Info{"e1": {State: session.Alive, Closing: true}}
	none := map[string]session.Info{}
	exited := map[string]session.Info{"e1": {State: session.Exited}}
	cases := map[string]struct {
		tables []map[string]session.Info
		want   bool
	}{
		"alive":                  {[]map[string]session.Info{alive}, false},
		"closing":                {[]map[string]session.Info{closing}, false},
		"none (idle claim)":      {[]map[string]session.Info{none}, true},
		"exited":                 {[]map[string]session.Info{exited}, true},
		"alive on the re-read":   {[]map[string]session.Info{none, alive}, false},
		"alive only at 1st read": {[]map[string]session.Info{alive, none}, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newHoldRig(t)
			r.tables = c.tables
			r.run(moot(r))
			if _, got := r.closed["e1"]; got != c.want {
				t.Fatalf("closed = %v, want %v", got, c.want)
			}
		})
	}
	t.Run("table read error closes nothing", func(t *testing.T) {
		r := newHoldRig(t)
		r.tabErr = errors.New("tmux")
		r.run(moot(r))
		if len(r.closed) != 0 {
			t.Fatalf("closed %v", r.closed)
		}
	})
	t.Run("one table read per tick with nothing decided", func(t *testing.T) {
		r := newHoldRig(t)
		r.run([]bd.Bead{errand("e1", "open", "Closed MR: !1")})
		if r.reads != 0 {
			t.Fatalf("reads = %d", r.reads)
		}
	})
}

func TestClearHoldsPacingAndErrors(t *testing.T) {
	r := newHoldRig(t)
	open := []bd.Bead{errand("e1", "open", "Closed MR: !1"), errand("e2", "open", "Closed MR: !2")}
	ClearHolds(open, func(id string) bool { return id == "e2" }, func(id string) {
		if id != "e2" {
			t.Fatalf("stamped %s", id)
		}
	})
	holdTargets = func(string) ([]bd.Bead, error) { return nil, errors.New("bd down") }
	var stamped []string
	ClearHolds(open[:1], func(string) bool { return true }, func(id string) { stamped = append(stamped, id) })
	if len(stamped) != 1 || len(r.closed) != 0 {
		t.Fatalf("a failed check still stamps: %v closed %v", stamped, r.closed)
	}
}

// The pacing stamp lands after the check's reads, on a failed read too.
func TestClearHoldsStampsAfterTheReads(t *testing.T) {
	for _, fail := range []bool{false, true} {
		newHoldRig(t)
		var log []string
		holdTargets = func(string) ([]bd.Bead, error) {
			log = append(log, "read")
			if fail {
				return nil, errors.New("bd down")
			}
			return nil, nil
		}
		ClearHolds([]bd.Bead{errand("e1", "open", "Closed MR: !1")}, func(string) bool { return true }, func(string) { log = append(log, "stamp") })
		if !slices.Equal(log, []string{"read", "stamp"}) {
			t.Fatalf("fail=%v: order %v", fail, log)
		}
	}
}

// landRig stubs the landed read's seams on top of holdRig: fetches counted, the trailers
// asked for recorded, and the set of trailers on mainline.
type landRig struct {
	*holdRig
	fetches  int
	fetchErr error
	asked    []string
	onMain   map[string]bool
}

func newLandRig(t *testing.T) *landRig {
	t.Helper()
	l := &landRig{holdRig: newHoldRig(t), onMain: map[string]bool{}}
	oF, oL, oR, oS, oD := fetchForge, landedBase, rollbackTarget, showBead, repoDirOf
	t.Cleanup(func() { fetchForge, landedBase, rollbackTarget, showBead, repoDirOf = oF, oL, oR, oS, oD })
	fetchForge = func(git.Repo) (string, error) { l.fetches++; return "", l.fetchErr }
	landedBase = func(tr string) (string, error) {
		l.asked = append(l.asked, tr)
		if l.onMain[tr] {
			return "sha", nil
		}
		return "", git.ErrNotLanded
	}
	rollbackTarget = func(string) (string, bool, error) { return "cp1", true, nil }
	showBead = func(id string) (bd.Bead, error) {
		return bd.Bead{ID: id, Kind: contract.KindCheckpoint, Status: "closed"}, nil
	}
	repoDirOf = func(bd.Bead) (git.Repo, string) { return "", "" }
	return l
}

func openBlocked(id string, kind contract.Kind) []bd.Bead {
	return []bd.Bead{{ID: id, Kind: kind, Status: "open"}}
}

func TestClearHoldsLandedTrailerClearsAsKeyLine(t *testing.T) {
	cp, rb := contract.KindCheckpoint, contract.KindRollback
	cases := []struct {
		key     string
		kind    contract.Kind
		trailer string
	}{
		{"Closed MR: !12", cp, checkpointTrailer("c1")},
		{"Merged MR: !12", cp, checkpointTrailer("c1")},
		{"Pipeline: 4411", cp, checkpointTrailer("c1")},
		{"Closed MR: !12", contract.KindChart, git.TrailerFor(string(contract.KindChart), "c1")},
		{"Merged MR: !12", contract.KindChart, git.TrailerFor(string(contract.KindChart), "c1")},
		{"Merged MR: !12", rb, git.RevertsTrailer(string(cp), "cp1")},
		{"Pipeline: 4411", rb, git.RevertsTrailer(string(cp), "cp1")},
	}
	for _, c := range cases {
		t.Run(c.key+"/"+string(c.kind), func(t *testing.T) {
			l := newLandRig(t)
			l.targets["e1"] = openBlocked("c1", c.kind)
			l.onMain[c.trailer] = true
			l.run([]bd.Bead{errand("e1", "open", c.key)})
			if got := l.closed["e1"]; got != "cleared: "+c.key {
				t.Fatalf("reason = %q", got)
			}
			if l.fetches != 1 || len(l.asked) != 1 || l.asked[0] != c.trailer {
				t.Fatalf("fetches %d asked %v, want one fetch for %q", l.fetches, l.asked, c.trailer)
			}
		})
	}
}

func TestClearHoldsLandedReadLeavesOpen(t *testing.T) {
	cp, rb := contract.KindCheckpoint, contract.KindRollback
	cases := map[string]struct {
		key     string
		kind    contract.Kind
		fetches int
	}{
		"no trailer cp":    {"Merged MR: !1", cp, 1},
		"no trailer rb":    {"Pipeline: 9", rb, 1},
		"foreign":          {"Foreign: abc", cp, 0},
		"chart pipeline":   {"Pipeline: 9", contract.KindChart, 0},
		"rollback closed":  {"Closed MR: !1", rb, 0},
		"foreign rollback": {"Foreign: abc", rb, 0},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			l := newLandRig(t)
			l.targets["e1"] = openBlocked("c1", c.kind)
			l.onMain[checkpointTrailer("c1")] = c.fetches == 0 // foreign/chart: landed, yet never read
			l.run([]bd.Bead{errand("e1", "open", c.key)})
			if len(l.closed) != 0 || l.fetches != c.fetches {
				t.Fatalf("closed %v fetches %d", l.closed, l.fetches)
			}
		})
	}
}

func TestClearHoldsMootDecidesBeforeLandedRead(t *testing.T) {
	l := newLandRig(t)
	l.targets["e1"] = []bd.Bead{closedBlocked("c1", contract.KindCheckpoint, "x")}
	l.onMain[checkpointTrailer("c1")] = true
	l.run([]bd.Bead{errand("e1", "open", "Merged MR: !1")})
	if l.closed["e1"] != "obsolete: c1 closed" || l.fetches != 0 {
		t.Fatalf("closed %v fetches %d", l.closed, l.fetches)
	}
}

func TestClearHoldsFetchErrorLeavesOpenAndStamps(t *testing.T) {
	l := newLandRig(t)
	l.fetchErr = errors.New("net")
	l.targets["e1"] = openBlocked("c1", contract.KindCheckpoint)
	l.onMain[checkpointTrailer("c1")] = true
	if got := l.run([]bd.Bead{errand("e1", "open", "Merged MR: !1")}); len(l.closed) != 0 || len(got) != 1 {
		t.Fatalf("closed %v stamped %v", l.closed, got)
	}
}

// Roll-forward: a container's trailer lands under an open hold; the step closes the
// Errand cleared, then the container's reconcile closes it landed without --force.
func TestClearHoldsRollsContainerForward(t *testing.T) {
	r := &jinbeSeamRec{trailerLanded: true}
	stubJinbeSeams(t, r)
	h := newHoldRig(t)
	cp := openCheckpoint()
	cp.BlockedBy = []string{"e1"}
	h.targets["e1"] = []bd.Bead{cp}
	h.run([]bd.Bead{errand("e1", "open", "Merged MR: !7"), cp})
	if got := h.closed["e1"]; got != "cleared: Merged MR: !7" {
		t.Fatalf("hold closed %q", got)
	}
	if err := reconcileCheckpoint(cp); err != nil {
		t.Fatal(err)
	}
	if len(r.closed) != 1 || r.closed[0] != cp.ID {
		t.Fatalf("closed %v, want the Checkpoint landed", r.closed)
	}
}

// restRig stubs the REST read's seams on top of landRig: Forge reads counted.
type restRig struct {
	*landRig
	mrReads, plReads int
	mr               forge.MR
	pl               forge.Pipeline
	restErr          error
}

func newRestRig(t *testing.T) *restRig {
	t.Helper()
	x := &restRig{landRig: newLandRig(t)}
	oM, oP, oJ := forgeMR, holdGetPipeline, projectOf
	t.Cleanup(func() { forgeMR, holdGetPipeline, projectOf = oM, oP, oJ })
	projectOf = func(git.Repo) (forge.Project, error) { return forge.Project{}, nil }
	forgeMR = func(_ forge.Project, iid int) (*forge.MR, error) {
		x.mrReads++
		mr := x.mr
		mr.IID = iid
		return &mr, x.restErr
	}
	holdGetPipeline = func(_ forge.Project, id int) (*forge.Pipeline, error) {
		x.plReads++
		pl := x.pl
		pl.ID = id
		return &pl, x.restErr
	}
	return x
}

func TestClearHoldsRestReadVariants(t *testing.T) {
	cp, rb, ch := contract.KindCheckpoint, contract.KindRollback, contract.KindChart
	ok := &forge.Pipeline{ID: 77, Status: "success"} // not the key's id: a green head supersedes the failure
	bad := &forge.Pipeline{Status: "failed"}
	cases := map[string]struct {
		key           string
		kind          contract.Kind
		mr            forge.MR
		pl            forge.Pipeline
		want          string
		mrN, plN, fet int
	}{
		"closed mr reopened":    {"Closed MR: !12", cp, forge.MR{State: "opened"}, forge.Pipeline{}, "cleared: Closed MR: !12", 1, 0, 1},
		"chart closed reopened": {"Closed MR: !12", ch, forge.MR{State: "opened"}, forge.Pipeline{}, "cleared: Closed MR: !12", 1, 0, 1},
		"chart closed still":    {"Closed MR: !12", ch, forge.MR{State: "closed"}, forge.Pipeline{}, "", 1, 0, 1},
		"closed mr still":       {"Closed MR: !12", cp, forge.MR{State: "closed"}, forge.Pipeline{}, "", 1, 0, 1},
		"cp pipeline success":   {"Pipeline: 4411", cp, forge.MR{}, forge.Pipeline{Status: "success"}, "cleared: Pipeline: 4411", 0, 1, 1},
		"cp pipeline failed":    {"Pipeline: 4411", cp, forge.MR{}, forge.Pipeline{Status: "failed"}, "", 0, 1, 1},
		"cp pipeline running":   {"Pipeline: 4411", cp, forge.MR{}, forge.Pipeline{Status: "running"}, "", 0, 1, 1},
		"rb head success":       {"Pipeline: 9", rb, forge.MR{State: "opened", HeadPipeline: ok}, forge.Pipeline{}, "cleared: Pipeline: 9", 1, 0, 1},
		"rb merged":             {"Pipeline: 9", rb, forge.MR{State: "merged", HeadPipeline: bad}, forge.Pipeline{}, "cleared: Pipeline: 9", 1, 0, 1},
		"rb closed":             {"Pipeline: 9", rb, forge.MR{State: "closed", HeadPipeline: bad}, forge.Pipeline{}, "obsolete: revert merge request closed", 1, 0, 1},
		"rb undecided":          {"Pipeline: 9", rb, forge.MR{State: "opened", HeadPipeline: bad}, forge.Pipeline{}, "", 1, 0, 1},
		"merged mr no read":     {"Merged MR: !1", cp, forge.MR{State: "opened"}, forge.Pipeline{Status: "success"}, "", 0, 0, 1},
		"foreign no read":       {"Foreign: abc", cp, forge.MR{State: "opened"}, forge.Pipeline{Status: "success"}, "", 0, 0, 0},
		"chart pipeline":        {"Pipeline: 9", ch, forge.MR{State: "opened"}, forge.Pipeline{Status: "success"}, "", 0, 0, 0},
		"rb closed mr guide":    {"Closed MR: !1", rb, forge.MR{State: "opened"}, forge.Pipeline{Status: "success"}, "", 0, 0, 0},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			x := newRestRig(t)
			x.mr, x.pl = c.mr, c.pl
			blocked := bd.Bead{ID: "c1", Kind: c.kind, Status: "open", Description: "MR: !77\n"}
			x.targets["e1"] = []bd.Bead{blocked}
			x.run([]bd.Bead{errand("e1", "open", c.key)})
			if x.closed["e1"] != c.want {
				t.Fatalf("reason = %q, want %q", x.closed["e1"], c.want)
			}
			if x.mrReads != c.mrN || x.plReads != c.plN || x.fetches != c.fet {
				t.Fatalf("mr %d pipeline %d fetch %d, want %d %d %d", x.mrReads, x.plReads, x.fetches, c.mrN, c.plN, c.fet)
			}
		})
	}
}

func TestClearHoldsRollbackReadsRecordedRevertMR(t *testing.T) {
	x := newRestRig(t)
	x.mr = forge.MR{State: "merged"}
	var got int
	forgeMR = func(_ forge.Project, iid int) (*forge.MR, error) { got = iid; return &x.mr, nil }
	x.targets["e1"] = []bd.Bead{{ID: "r1", Kind: contract.KindRollback, Status: "open", Description: "MR: !77\n"}}
	e := errand("e1", "open", "Pipeline: 9")
	e.Description += "MR: !5\n"
	x.run([]bd.Bead{e})
	if got != 77 || x.closed["e1"] != "cleared: Pipeline: 9" {
		t.Fatalf("iid %d closed %v", got, x.closed)
	}
}

func TestClearHoldsLandedDecidesBeforeRest(t *testing.T) {
	x := newRestRig(t)
	x.mr = forge.MR{State: "opened"}
	x.targets["e1"] = openBlocked("c1", contract.KindCheckpoint)
	x.onMain[checkpointTrailer("c1")] = true
	x.run([]bd.Bead{errand("e1", "open", "Closed MR: !3")})
	if x.mrReads != 0 || x.closed["e1"] != "cleared: Closed MR: !3" {
		t.Fatalf("mr reads %d closed %v", x.mrReads, x.closed)
	}
}

func TestClearHoldsRestErrorLeavesOpenAndStamps(t *testing.T) {
	for _, err := range []error{forge.ErrRateLimited, errors.New("boom")} {
		x := newRestRig(t)
		x.restErr = err
		x.mr = forge.MR{State: "opened"}
		x.targets["e1"] = openBlocked("c1", contract.KindCheckpoint)
		stamped := x.run([]bd.Bead{errand("e1", "open", "Closed MR: !3")})
		if len(x.closed) != 0 || len(stamped) != 1 {
			t.Fatalf("%v: closed %v stamped %v", err, x.closed, stamped)
		}
	}
}

// A Rollback hold's check resolves the target and its repo bead once, however many
// reads follow.
func TestClearHoldsResolvesRollbackOnce(t *testing.T) {
	x := newRestRig(t)
	x.mr = forge.MR{State: "opened"}
	var targets, shows int
	rollbackTarget = func(string) (string, bool, error) { targets++; return "cp1", true, nil }
	oShow := showBead
	showBead = func(id string) (bd.Bead, error) { shows++; return oShow(id) }
	x.targets["e1"] = []bd.Bead{{ID: "r1", Kind: contract.KindRollback, Status: "open", Description: "MR: !77\n"}}
	x.run([]bd.Bead{errand("e1", "open", "Pipeline: 9")})
	if targets != 1 || shows != 1 || x.fetches != 1 || x.mrReads != 1 {
		t.Fatalf("target reads %d show reads %d fetches %d mr reads %d", targets, shows, x.fetches, x.mrReads)
	}
}
