package jinbe

import (
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// remRec records the Remnant leg's bd writes over jinbeSeamRec's graph.
type remRec struct {
	*jinbeSeamRec
	remnant     *bd.Bead
	spawns      int
	landing     string // the Landing the Remnant was filed discovered-from
	forced      []string
	forcedWhy   []string
	blocks      []string // "<blocked> <- <blocker>"
	forceClosed []string // containers closed over their blockers
}

// keptBy is the Kept: line the stubbed keptLine returns for each child id.
var keptBy = map[string]string{}

// stubRemnantSeams is stubJinbeSeams' Remnant leg: a graph of the container's r.kids with no Remnant.
func stubRemnantSeams(t *testing.T, r *jinbeSeamRec) *remRec {
	t.Helper()
	rr := &remRec{jinbeSeamRec: r}
	r.rem = rr
	oKept := keptLine
	oOf, oSpawn, oAppend, oOpen, oForceAll, oBlock, oForce := remnantOf, spawnRemnant, appendLines, openChildren, forceCloseAll, blockOn, forceClose
	keptLine = func(b bd.Bead) (string, error) { return keptBy[b.ID], nil }
	t.Cleanup(func() {
		keptLine = oKept
		remnantOf, spawnRemnant, appendLines, openChildren, forceCloseAll, blockOn, forceClose = oOf, oSpawn, oAppend, oOpen, oForceAll, oBlock, oForce
	})
	remnantOf = func(string) (bd.Bead, bool, error) {
		if rr.remnant == nil {
			return bd.Bead{}, false, nil
		}
		return *rr.remnant, true, nil
	}
	spawnRemnant = func(_ bd.Bead, title, body, landing string) (bd.Bead, bool, error) {
		rr.spawns++
		rr.landing = landing
		rr.remnant = &bd.Bead{ID: "ws-rem", Kind: contract.KindRemnant, Status: "open", Title: title, Description: body}
		return *rr.remnant, true, nil
	}
	appendLines = func(_ string, lines ...string) error {
		for _, l := range lines {
			if !slices.Contains(strings.Split(rr.remnant.Description, "\n"), l) {
				rr.remnant.Description += "\n" + l
			}
		}
		return nil
	}
	openChildren = func(string) ([]bd.Bead, error) {
		var out []bd.Bead
		for _, k := range r.kids {
			if k.Open() {
				out = append(out, k)
			}
		}
		return out, nil
	}
	forceCloseAll = func(ids []string, reason string) error {
		rr.forced = append(rr.forced, ids...)
		rr.forcedWhy = append(rr.forcedWhy, reason)
		for i := range r.kids {
			if slices.Contains(ids, r.kids[i].ID) {
				r.kids[i].Status = "closed"
			}
		}
		return nil
	}
	blockOn = func(blocked, blocker string) error {
		e := blocked + " <- " + blocker
		if !slices.Contains(rr.blocks, e) {
			rr.blocks = append(rr.blocks, e)
		}
		return nil
	}
	forceClose = func(id, reason string) error {
		rr.forceClosed = append(rr.forceClosed, id+" "+reason)
		r.closed, r.reasons = append(r.closed, id), append(r.reasons, reason)
		return nil
	}
	return rr
}

func kid(id string, kind contract.Kind, status string) bd.Bead {
	return bd.Bead{ID: id, Kind: kind, Status: status, Title: id + " title"}
}

func TestFileRemnant(t *testing.T) {
	landing := openLanding("oldtip")
	for _, c := range []struct {
		name     string
		rec      jinbeSeamRec
		wantRem  bool
		contains []string
		absent   []string
		forced   []string
		blocks   []string
		landing  string
		keep     map[string]string
	}{
		{name: "on-time land files nothing", rec: jinbeSeamRec{head: "oldtip", landedTip: "oldtip", landings: []bd.Bead{landing},
			kids: []bd.Bead{kid("ws-w", contract.KindMRWatch, "open"), kid("ws-ap", contract.KindApproval, "closed")}}},
		{name: "Tip..head only", rec: jinbeSeamRec{head: "newhead", landedTip: "oldtip"}, wantRem: true,
			contains: []string{"Remnant of ws-cp, which a Forge merge landed at landsha.", "Landed-tip: oldtip", "Stranded: oldtip..newhead"}, absent: []string{"Child:"}},
		{name: "rewritten branch", rec: jinbeSeamRec{head: "newhead", landedTip: "oldtip", rewritten: true}, wantRem: true,
			contains: []string{"git range-diff oldtip...newhead"}, absent: []string{"Stranded: oldtip.."}},
		{name: "unstarted children only", rec: jinbeSeamRec{head: "oldtip", landedTip: "oldtip", landings: []bd.Bead{landing},
			kids: []bd.Bead{kid("ws-b1", contract.KindBuild, "open"), kid("ws-b2", contract.KindBuild, "deferred"), kid("ws-ld", contract.KindLanding, "open")}},
			wantRem: true, contains: []string{"Child: ws-b1 build ws-b1 title — obsolete: container landed; carried by ws-rem", "Child: ws-b2 build"},
			absent: []string{"Stranded:", "ws-ld"}, forced: []string{"ws-b1", "ws-b2"}, landing: "ws-land"},
		{name: "unstarted child with a kept effort branch lists its Kept: line before its Child: line", rec: jinbeSeamRec{head: "oldtip", landedTip: "oldtip",
			kids: []bd.Bead{kid("ws-b1", contract.KindBuild, "open")}}, keep: map[string]string{"ws-b1": "Kept: effort/ws-b1 c1..h1"},
			wantRem: true, contains: []string{"Kept: effort/ws-b1 c1..h1\nChild: ws-b1 build"}, forced: []string{"ws-b1"}},
		{name: "running child holds the Remnant", rec: jinbeSeamRec{head: "oldtip", landedTip: "oldtip",
			kids: []bd.Bead{kid("ws-b1", contract.KindBuild, "in_progress"), {ID: "ws-b2", Kind: contract.KindCritique, Status: "open", Assignee: "me"}}},
			wantRem: true, absent: []string{"Child:"}, blocks: []string{"ws-rem <- ws-b1", "ws-rem <- ws-b2"}},
		{name: "paused unassigned child holds the Remnant", rec: jinbeSeamRec{head: "oldtip", landedTip: "oldtip",
			kids: []bd.Bead{{ID: "ws-b1", Kind: contract.KindBuild, Status: "open", Labels: []string{"paused:integrate-conflict"}}}},
			wantRem: true, absent: []string{"Child:"}, blocks: []string{"ws-rem <- ws-b1"}},
		{name: "open pivot holds the Remnant", rec: jinbeSeamRec{head: "newhead", landedTip: "oldtip", pivots: []bd.Bead{kid("ws-pv", contract.KindPivot, "open")}},
			wantRem: true, blocks: []string{"ws-rem <- ws-pv"}},
		{name: "open pivot alone is stranded work", rec: jinbeSeamRec{head: "oldtip", landedTip: "oldtip", pivots: []bd.Bead{kid("ws-pv", contract.KindPivot, "open")}},
			wantRem: true, blocks: []string{"ws-rem <- ws-pv"}, absent: []string{"Stranded:", "Child:"}},
		{name: "before-land Errand is recorded, a Hold Errand is not", rec: jinbeSeamRec{head: "newhead", landedTip: "oldtip",
			blockers: []bd.Bead{kid("ws-er", contract.KindErrand, "open"), {ID: "ws-hold", Kind: contract.KindErrand, Status: "open", Description: "Closed MR: !7"}}},
			wantRem: true, contains: []string{"Errand: ws-er ws-er title"}, absent: []string{"ws-hold"}},
		{name: "before-land Errand alone is stranded work", rec: jinbeSeamRec{head: "oldtip", landedTip: "oldtip",
			blockers: []bd.Bead{kid("ws-er", contract.KindErrand, "open"), {ID: "ws-hold", Kind: contract.KindErrand, Status: "open", Description: "Closed MR: !7"}}},
			wantRem: true, contains: []string{"Errand: ws-er ws-er title"}, absent: []string{"ws-hold", "Stranded:", "Child:"}},
		{name: "a Hold Errand alone files nothing", rec: jinbeSeamRec{head: "oldtip", landedTip: "oldtip",
			blockers: []bd.Bead{{ID: "ws-hold", Kind: contract.KindErrand, Status: "open", Description: "Closed MR: !7"}}}},
		{name: "no Tip: records the range unknown", rec: jinbeSeamRec{head: "newhead", kids: []bd.Bead{kid("ws-b1", contract.KindBuild, "open")}},
			wantRem: true, forced: []string{"ws-b1"}, contains: []string{"Landed-tip: unknown", "Stranded: unknown"}},
		{name: "no Tip: and nothing else files nothing", rec: jinbeSeamRec{head: "newhead"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := c.rec
			r.trailerLanded = true
			stubJinbeSeams(t, &r)
			rr := r.rem
			keptBy = c.keep
			t.Cleanup(func() { keptBy = map[string]string{} })
			if err := reconcileCheckpoint(openCheckpoint()); err != nil {
				t.Fatalf("reconcileCheckpoint: %v", err)
			}
			if len(r.signals) != 0 {
				t.Errorf("signals = %v, want no stale-pin Signal", r.signals)
			}
			if c.wantRem != (rr.remnant != nil) {
				t.Fatalf("remnant filed = %v, want %v", rr.remnant != nil, c.wantRem)
			}
			if c.wantRem {
				if rr.remnant.Title != "Remnant of Ship the thing" || rr.landing != c.landing {
					t.Errorf("title %q landing %q, want the landed title and Landing %q", rr.remnant.Title, rr.landing, c.landing)
				}
			}
			if len(rr.forceClosed) != 1 || rr.forceClosed[0] != "ws-cp landed: landsha" {
				t.Errorf("forceClosed %v closed %v, want the container force-closed landed", rr.forceClosed, r.closed)
			}
			body := ""
			if rr.remnant != nil {
				body = rr.remnant.Description
			}
			for _, w := range c.contains {
				if !strings.Contains(body, w) {
					t.Errorf("body %q lacks %q", body, w)
				}
			}
			for _, w := range c.absent {
				if strings.Contains(body, w) {
					t.Errorf("body %q has %q", body, w)
				}
			}
			if !slices.Equal(rr.forced, c.forced) || !slices.Equal(rr.blocks, c.blocks) {
				t.Errorf("forced %v blocks %v, want %v and %v", rr.forced, rr.blocks, c.forced, c.blocks)
			}
		})
	}
}

// A re-run (crash, retry) converges on the one Remnant with no duplicate lines.
func TestFileRemnantRerunConverges(t *testing.T) {
	r := &jinbeSeamRec{head: "newhead", landedTip: "oldtip", trailerLanded: true,
		kids: []bd.Bead{kid("ws-b1", contract.KindBuild, "open"), kid("ws-b2", contract.KindBuild, "in_progress")}}
	stubJinbeSeams(t, r)
	rr := r.rem
	for range 2 {
		if err := reconcileCheckpoint(openCheckpoint()); err != nil {
			t.Fatal(err)
		}
	}
	if rr.spawns != 1 || strings.Count(rr.remnant.Description, "Child: ws-b1") != 1 || strings.Count(rr.remnant.Description, "Stranded:") != 1 {
		t.Errorf("spawns %d body %q, want one Remnant and no duplicate lines", rr.spawns, rr.remnant.Description)
	}
}

// A Chart's Landing keeps the stale-pin Signal; a Checkpoint's does not.
func TestLandedCloseStalePinSignalIsChartOnly(t *testing.T) {
	for kind, want := range map[contract.Kind]int{contract.KindChart: 1, contract.KindCheckpoint: 0} {
		r := &jinbeSeamRec{head: "newhead", landedTip: "oldtip"}
		stubJinbeSeams(t, r)
		cp := bd.Bead{ID: "ws-cp", Kind: kind, Title: "Ship"}
		if err := landedClose(openLanding("oldtip"), cp, "checkpoint/ws-cp", "landsha", git.LandStrategyGitlab, legs{head: func(string) (string, error) { return "newhead", nil }}); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if len(r.signals) != want {
			t.Errorf("%s: signals = %v, want %d", kind, r.signals, want)
		}
	}
}

// A nested Checkpoint (Out of Scope: nested respins) keeps today's strand handling: the
// stale-pin Signal and a plain close, with no Remnant filed.
func TestNestedCheckpointLandFilesStalePinNotRemnant(t *testing.T) {
	r := &jinbeSeamRec{head: "newhead", landedTip: "oldtip"}
	stubJinbeSeams(t, r)
	cp := bd.Bead{ID: "ws-cp", Kind: contract.KindCheckpoint, Title: "Ship"}
	lg := legs{repo: "/ws/kid", head: func(string) (string, error) { return "newhead", nil }}
	if err := landedClose(openLanding("oldtip"), cp, "checkpoint/ws-cp", "landsha", git.LandStrategyGitlab, lg); err != nil {
		t.Fatal(err)
	}
	if len(r.signals) != 1 {
		t.Errorf("signals = %v, want the stale-pin Signal", r.signals)
	}
	if err := fileRemnant(cp, "checkpoint/ws-cp", "landsha", openLanding("oldtip"), true, lg); err != nil || r.rem.remnant != nil {
		t.Errorf("fileRemnant = %v, remnant %v; want none for a nested container", err, r.rem.remnant)
	}
}

// A landed Checkpoint with nothing stranded still closes landed: <sha> over an open
// blocking Hold Errand (its guide session is live), its Landing first; the Errand stays open.
func TestCloseLandedForceClosesOverHoldErrand(t *testing.T) {
	for _, gated := range []bool{false, true} {
		name := "gitlab"
		if gated {
			name = "local"
		}
		t.Run(name, func(t *testing.T) {
			hold := bd.Bead{ID: "ws-hold", Kind: contract.KindErrand, Status: "open", Description: "Closed MR: !7"}
			r := &jinbeSeamRec{head: "oldtip", landedTip: "oldtip", trailerLanded: true, gated: gated,
				landings: []bd.Bead{openLanding("oldtip")}, blockers: []bd.Bead{hold}}
			stubJinbeSeams(t, r)
			rr := r.rem
			cp := openCheckpoint()
			cp.BlockedBy = []string{hold.ID}
			if err := reconcileCheckpoint(cp); err != nil {
				t.Fatal(err)
			}
			if rr.remnant != nil {
				t.Errorf("Remnant filed %q, want none", rr.remnant.Description)
			}
			if want := []string{"ws-land", "ws-cp"}; !slices.Equal(r.closed, want) {
				t.Errorf("closed %v, want the Landing then the Checkpoint %v", r.closed, want)
			}
			if len(r.reasons) != 2 || r.reasons[0] != "landed: landsha" || r.reasons[1] != "landed: landsha" {
				t.Errorf("reasons %v, want landed: landsha for both", r.reasons)
			}
			if slices.Contains(r.closed, "ws-hold") {
				t.Errorf("closed %v, want the Hold Errand left open", r.closed)
			}
		})
	}
}
