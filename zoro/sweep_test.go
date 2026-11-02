package zoro

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/bd/bdtest"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// sweepRig is a bdtest graph shaped like a pivoted Checkpoint: the Spec ws-old (caused-by
// the Chart ws-chart, else the Signal ws-sig) whose open Checkpoint ws-cp is blocked by the
// claimed lead pivot ws-pv and its open sibling ws-pv2 (ws-pvx is closed), over members
// ws-b1 (in progress), ws-c1, ws-a1 (open) and ws-done (closed), with the dependent ws-dep
// waiting on the Checkpoint. The git legs are recorded, not run.
type sweepRig struct {
	f    *bdtest.Fake
	cuts map[string]string // branch → base
}

func newSweepRig(t *testing.T, chart string) *sweepRig {
	t.Helper()
	member := func(id, status string) bd.Bead {
		return bd.Bead{ID: id, Kind: contract.KindBuild, Status: status, Parent: "ws-cp", Labels: []string{"kind:build"}}
	}
	pivot := func(id, status string) bd.Bead {
		return bd.Bead{ID: id, Kind: contract.KindPivot, Status: status, Related: []string{"ws-old"}, Labels: []string{"kind:pivot"}}
	}
	old := bd.Bead{ID: "ws-old", Kind: contract.KindSpec, Status: "closed", Title: "old", CausedBy: "ws-sig", Labels: []string{"kind:spec"}}
	seeds := []bd.Bead{
		{ID: "ws-sig", Kind: contract.KindSignal, Status: "closed", Title: "old", Labels: []string{"kind:signal"}},
		{ID: "ws-cp", Kind: contract.KindCheckpoint, Status: "open", Container: true, Title: "old", CausedBy: "ws-old",
			BlockedBy: []string{"ws-pv", "ws-pv2", "ws-pvx"}, Labels: []string{"kind:checkpoint"}},
		bdtest.Claimed(pivot("ws-pv", "open")), pivot("ws-pv2", "open"), pivot("ws-pvx", "closed"),
		member("ws-b1", "in_progress"), member("ws-c1", "open"), member("ws-a1", "open"), member("ws-done", "closed"),
		{ID: "ws-dep", Kind: contract.KindCheckpoint, Status: "open", BlockedBy: []string{"ws-cp"}},
	}
	if chart != "" {
		seeds = append(seeds, bd.Bead{ID: chart, Kind: contract.KindChart, Status: "closed", Title: "old", CausedBy: "ws-sig", Labels: []string{"kind:chart"}})
		old.CausedBy = chart
	}
	r := &sweepRig{f: bdtest.New(t, append(seeds, old)...), cuts: map[string]string{}}
	// Local strategy: the pin is the tip; no Spec: trailer is on main.
	swap(t, &landStrategy, func() (string, error) { return git.LandStrategyLocal, nil })
	swap(t, &repoLanded, func(git.Repo, string) (string, error) { return "", git.ErrNotLanded })
	swap(t, &repoTip, func(git.Repo) (string, error) { return "tipsha", nil })
	swap(t, &cutBranch, func(name, base string) (bool, error) {
		_, had := r.cuts[name]
		r.cuts[name] = base
		return !had, nil
	})
	return r
}

// all is every bead in the graph, closed ones too.
func (r *sweepRig) all(t *testing.T) []bd.Bead {
	t.Helper()
	bs, err := r.f.List(bd.Query{All: true})
	if err != nil {
		t.Fatal(err)
	}
	return bs
}

// spawned is the one bead of kind discovered-from the lead pivot.
func (r *sweepRig) spawned(t *testing.T, kind contract.Kind) bd.Bead {
	t.Helper()
	var got []bd.Bead
	for _, b := range r.all(t) {
		if b.Kind == kind && slices.Contains(b.DiscoveredFrom, "ws-pv") {
			got = append(got, b)
		}
	}
	if len(got) != 1 {
		t.Fatalf("beads of kind %s discovered-from the pivot = %+v, want exactly one", kind, got)
	}
	return got[0]
}

// writes is every graph-changing Core call so far, in order.
func (r *sweepRig) writes() []string {
	return slices.DeleteFunc(r.f.Calls(), func(c string) bool {
		verb, _, _ := strings.Cut(c, " ")
		return !slices.Contains([]string{"Create", "Close", "AddEdge", "Supersede", "SetField", "RemoveEdge"}, verb)
	})
}

// at is the index of the first call starting with prefix, or the test fails.
func at(t *testing.T, calls []string, prefix string) int {
	t.Helper()
	i := slices.IndexFunc(calls, func(c string) bool { return strings.HasPrefix(c, prefix) })
	if i < 0 {
		t.Fatalf("no call %q in %v", prefix, calls)
	}
	return i
}

// wantClosed asserts id closed with reason, and that nothing else changed its closure.
func (r *sweepRig) wantClosed(t *testing.T, id, reason string) {
	t.Helper()
	if b := r.f.Bead(id); b.Status != "closed" || b.CloseReason != reason {
		t.Errorf("%s = %s %q, want closed %q", id, b.Status, b.CloseReason, reason)
	}
}

func (r *sweepRig) wantCloseOut(t *testing.T, reason, lead, leadReason string) {
	t.Helper()
	for _, id := range []string{"ws-b1", "ws-c1", "ws-a1", "ws-cp"} {
		r.wantClosed(t, id, reason)
	}
	r.wantClosed(t, "ws-pv2", "superseded: ws-pv")
	r.wantClosed(t, lead, leadReason)
	if b := r.f.Bead("ws-done"); b.CloseReason != "" {
		t.Errorf("already-closed ws-done re-closed: %q", b.CloseReason)
	}
	if b := r.f.Bead("ws-pvx"); b.CloseReason != "" {
		t.Errorf("already-closed ws-pvx re-closed: %q", b.CloseReason)
	}
}

// carried asserts the Checkpoint's dependent now waits on succ too (ADR 0085).
func (r *sweepRig) carried(t *testing.T, succ string) {
	t.Helper()
	if b := r.f.Bead("ws-dep"); !slices.Contains(b.BlockedBy, succ) {
		t.Errorf("ws-dep blocked by %v, want it to wait on %s too", b.BlockedBy, succ)
	}
}

// Confirm on a charted Spec (FR39–42): the corrected Spec is spawned a Draft caused-by
// the old Spec's Chart (so checkpointBase keeps the Chart's landed base) and discovered-
// from the pivot, with no anchor; old Spec supersedes→new; every open member, then the
// Checkpoint, close obsolete; the open sibling pivot closes 'superseded: <pivot>'; the
// pivot closes 'confirmed' last. Already-closed beads are not re-closed.
func TestConfirmChartedSweep(t *testing.T) {
	r := newSweepRig(t, "ws-chart")
	if err := Confirm("ws-pv", "", "corrected body", nil); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	spec := r.spawned(t, contract.KindSpec)
	if spec.CausedBy != "ws-chart" || spec.Title != "old" || spec.Description != "corrected body" || spec.Status != "deferred" || !spec.HasLabel("drafted-by:ws-pv") {
		t.Errorf("new Spec = %+v, want a Draft by the pivot titled old, caused-by the Chart, with the body untouched (no anchor)", spec)
	}
	if !slices.Contains(r.supersedes(t, "ws-old"), spec.ID) {
		t.Errorf("ws-old does not supersede %s", spec.ID)
	}
	calls := r.writes()
	if !(at(t, calls, "Supersede ws-old,"+spec.ID) < at(t, calls, "AddEdge ws-dep,"+spec.ID) && at(t, calls, "AddEdge ws-dep,"+spec.ID) < at(t, calls, "Close ")) {
		t.Errorf("writes = %v, want supersede, then carry, then every close", calls)
	}
	r.carried(t, spec.ID)
	r.wantCloseOut(t, bd.ObsoleteSuperseded("ws-old", spec.ID, "ws-pv"), "ws-pv", "confirmed")
}

// supersedes is the ids old supersedes, read off the graph.
func (r *sweepRig) supersedes(t *testing.T, old string) []string {
	t.Helper()
	rows, err := r.f.Edges(old, bd.Down, "supersedes")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}

// A fast-laned Spec (no Chart): the corrected Spec is caused-by the old Spec's
// Signal and gets an Anchor: pin at the local tip, no commit on main (FR41, ADR 0035).
func TestConfirmFastLanedAnchorsNewSpec(t *testing.T) {
	r := newSweepRig(t, "")
	if err := Confirm("ws-pv", "fixed", "body", nil); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	spec := r.spawned(t, contract.KindSpec)
	if spec.CausedBy != "ws-sig" || spec.Title != "fixed" || !spec.HasLabel("drafted-by:ws-pv") {
		t.Errorf("new Spec = %+v, want a Draft by the pivot titled fixed, caused-by the Signal", spec)
	}
	if sha, ok := git.ParseAnchor(spec.Description); !ok || sha != "tipsha" || !strings.HasPrefix(spec.Description, "body") {
		t.Errorf("new Spec body = %q, want body pinned Anchor: tipsha", spec.Description)
	}
}

// FR48: a crash mid-sweep (the sibling close fails) is fixed by running it again —
// the re-run spawns no second Spec, writes no second anchor, and closes the rest. A
// third run over the settled graph is a no-op.
func TestConfirmRerunConverges(t *testing.T) {
	r := newSweepRig(t, "")
	r.f.FailOn("Close", "ws-pv2", errors.New("bd close: boom"))
	if err := Confirm("ws-pv", "", "body", nil); err == nil {
		t.Fatal("first run: want the injected close failure")
	}
	if r.f.Bead("ws-pv").Status == "closed" {
		t.Fatal("pivot closed before the sweep finished; it must close last")
	}
	r.f.Heal("Close", "ws-pv2")
	if err := Confirm("ws-pv", "", "body", nil); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	spec := r.spawned(t, contract.KindSpec) // exactly one
	if n := strings.Count(spec.Description, "Anchor:"); n != 1 {
		t.Errorf("body %q holds %d anchors, want 1", spec.Description, n)
	}
	r.wantCloseOut(t, bd.ObsoleteSuperseded("ws-old", spec.ID, "ws-pv"), "ws-pv", "confirmed")
	n := len(r.writes())
	if err := Confirm("ws-pv", "", "body", nil); err != nil {
		t.Fatalf("third run: %v", err)
	}
	if got := r.writes(); len(got) != n {
		t.Errorf("a run over a closed pivot wrote %v", got[n:])
	}
}

// A carry error fails the sweep before closeOut: nothing is closed, so a re-run
// (FR48) finds the same Spec via SpawnDiscoveredIfAbsent and carries again.
func TestConfirmCarryErrorStopsBeforeCloseOut(t *testing.T) {
	r := newSweepRig(t, "ws-chart")
	r.f.FailOn("AddEdge", "*", errors.New("bd down"))
	if err := Confirm("ws-pv", "", "body", nil); err == nil {
		t.Fatal("a carry error must fail the sweep")
	}
	if slices.ContainsFunc(r.writes(), func(c string) bool { return strings.HasPrefix(c, "Close ") }) {
		t.Errorf("closes after a failed carry: %v", r.writes())
	}
	r.f.Heal("AddEdge", "*")
	if err := Confirm("ws-pv", "", "body", nil); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	spec := r.spawned(t, contract.KindSpec)
	r.carried(t, spec.ID)
	if r.f.Bead("ws-pv").Status != "closed" {
		t.Error("re-run left the pivot open")
	}
}

// Refusals before any write: not a pivot, or an empty corrected body.
func TestConfirmRefusesBadInput(t *testing.T) {
	r := newSweepRig(t, "ws-chart")
	if err := Confirm("ws-b1", "", "body", nil); err == nil {
		t.Error("Confirm over a non-pivot must error")
	}
	if err := Confirm("ws-pv", "", "  \n", nil); err == nil {
		t.Error("Confirm with an empty body must error")
	}
	if w := r.writes(); len(w) != 0 {
		t.Errorf("a refused Confirm wrote %v", w)
	}
}

// ADR 0070: a sweep writes only under the pivot's claim, so an open pivot that is not
// claimed (open, or in_progress with no assignee) is refused naming the claim command,
// with the graph unchanged; a closed pivot is a no-op.
func TestSweepsRefuseAnUnclaimedPivot(t *testing.T) {
	for name, run := range map[string]func(pivot string) error{
		"confirm":  func(pv string) error { return Confirm(pv, "", "body", nil) },
		"escalate": func(pv string) error { return Escalate(pv, "evidence", nil) },
	} {
		t.Run(name, func(t *testing.T) {
			r := newSweepRig(t, "ws-chart")
			for _, pv := range []string{"ws-pv2", "ws-pv3"} { // open; in_progress with no assignee
				r.f.Seed(bd.Bead{ID: "ws-pv3", Kind: contract.KindPivot, Status: "in_progress", Related: []string{"ws-old"}})
				err := run(pv)
				if err == nil || !strings.Contains(err.Error(), "bd update "+pv+" --claim") {
					t.Errorf("%s: err = %v, want one naming `bd update %s --claim`", pv, err, pv)
				}
			}
			if w := r.writes(); len(w) != 0 {
				t.Errorf("a refused sweep wrote %v", w)
			}
			if err := run("ws-pvx"); err != nil || len(r.writes()) != 0 {
				t.Errorf("closed pivot: err = %v writes = %v, want a no-op", err, r.writes())
			}
		})
	}
}

// Escalate on a charted Spec (FR43–44, 46): one fresh Chart caused-by the original
// Signal (reached through the old Chart), titled for the Signal and seeded with the
// evidence as a wayfinder map off the placeholder, with a Survey seeded under it
// (Spec mc-r832 D5), both Drafts by the pivot; chart/<new> cut from main; old Chart
// supersedes→new Chart; the same close-out as confirm; the pivot closes 'escalated'
// last. No Spec is spawned or superseded.
func TestEscalateChartedSweep(t *testing.T) {
	r := newSweepRig(t, "ws-chart")
	if err := Escalate("ws-pv", "the FR3 assumption is wrong", nil); err != nil {
		t.Fatalf("Escalate: %v", err)
	}
	c := r.spawned(t, contract.KindChart)
	if c.CausedBy != "ws-sig" || !c.Container || c.Status != "deferred" || !c.HasLabel("drafted-by:ws-pv") || c.Title != "old" {
		t.Errorf("new Chart = %+v, want a container Draft by the pivot, caused-by the Signal, titled for it", c)
	}
	if c.Description == contract.ChartPlaceholderBody || !strings.Contains(c.Description, "the FR3 assumption is wrong") {
		t.Errorf("Chart body %q must be seeded with the evidence", c.Description)
	}
	for _, f := range strings.Split(contract.WayfinderMapFields, " / ") {
		if !strings.Contains(c.Description, "## "+f+"\n") {
			t.Errorf("Chart body lacks the wayfinder section %q", f)
		}
	}
	surveys, err := bd.ChildrenOfKind(c.ID, contract.KindSurvey)
	if err != nil || len(surveys) != 1 || surveys[0].Title != "old" || surveys[0].Status != "deferred" || !surveys[0].HasLabel("drafted-by:ws-pv") {
		t.Errorf("surveys = %+v, %v, want one Draft by the pivot under the new Chart, titled for the Signal (Spec D5)", surveys, err)
	}
	if want := "chart/" + c.ID; len(r.cuts) != 1 || r.cuts[want] != git.Mainline() {
		t.Errorf("cuts = %v, want %s from %s", r.cuts, want, git.Mainline())
	}
	if got := r.supersedes(t, "ws-chart"); !slices.Equal(got, []string{c.ID}) {
		t.Errorf("ws-chart supersedes %v, want [%s]", got, c.ID)
	}
	if got := r.supersedes(t, "ws-old"); len(got) != 0 {
		t.Errorf("escalate superseded the Spec: %v", got)
	}
	calls := r.writes()
	if !(at(t, calls, "Supersede ws-chart,"+c.ID) < at(t, calls, "AddEdge ws-dep,"+c.ID) && at(t, calls, "AddEdge ws-dep,"+c.ID) < at(t, calls, "Close ")) {
		t.Errorf("writes = %v, want supersede, then carry, then every close", calls)
	}
	r.carried(t, c.ID)
	r.wantCloseOut(t, bd.ObsoleteEscalated("ws-old", c.ID, "ws-pv"), "ws-pv", "escalated")
	for _, b := range r.all(t) {
		if b.Kind == contract.KindSpec && slices.Contains(b.DiscoveredFrom, "ws-pv") {
			t.Errorf("escalate spawned a Spec: %+v", b)
		}
	}
}

// FR45: a fast-laned Spec escalates the same way minus the Chart supersede — the
// new Chart hangs off the old Spec's Signal and nothing is superseded (caused-by
// the Signal is the only link; the recorded decision, ADR 0046 §6).
func TestEscalateFastLanedSkipsSupersede(t *testing.T) {
	r := newSweepRig(t, "")
	if err := Escalate("ws-pv", "evidence", nil); err != nil {
		t.Fatalf("Escalate: %v", err)
	}
	c := r.spawned(t, contract.KindChart)
	if c.CausedBy != "ws-sig" {
		t.Errorf("new Chart caused-by %q, want the Signal", c.CausedBy)
	}
	for _, b := range r.all(t) {
		if got := r.supersedes(t, b.ID); len(got) != 0 {
			t.Errorf("fast-laned escalate: %s supersedes %v, want nothing", b.ID, got)
		}
	}
	r.carried(t, c.ID)
	r.wantCloseOut(t, bd.ObsoleteEscalated("ws-old", c.ID, "ws-pv"), "ws-pv", "escalated")
}

// FR48: a partial escalate re-runs to convergence — one Chart, one Survey, every bead closed.
func TestEscalateRerunConverges(t *testing.T) {
	r := newSweepRig(t, "ws-chart")
	r.f.FailOn("Close", "ws-pv2", errors.New("bd close: boom"))
	if err := Escalate("ws-pv", "evidence", nil); err == nil {
		t.Fatal("first run: want the injected close failure")
	}
	r.f.Heal("Close", "ws-pv2")
	if err := Escalate("ws-pv", "evidence", nil); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	c := r.spawned(t, contract.KindChart) // exactly one
	if surveys, _ := bd.ChildrenOfKind(c.ID, contract.KindSurvey); len(surveys) != 1 {
		t.Errorf("surveys = %+v, want one", surveys)
	}
	r.wantCloseOut(t, bd.ObsoleteEscalated("ws-old", c.ID, "ws-pv"), "ws-pv", "escalated")
	n := len(r.writes())
	if err := Escalate("ws-pv", "evidence", nil); err != nil {
		t.Fatalf("third run: %v", err)
	}
	if got := r.writes(); len(got) != n {
		t.Errorf("a run over a closed pivot wrote %v", got[n:])
	}
}

// A carry error fails the escalate before closeOut (the cut branch and supersede
// are idempotent legs a re-run repeats).
func TestEscalateCarryErrorStopsBeforeCloseOut(t *testing.T) {
	r := newSweepRig(t, "ws-chart")
	r.f.FailOn("AddEdge", "*", errors.New("bd down"))
	if err := Escalate("ws-pv", "evidence", nil); err == nil {
		t.Fatal("a carry error must fail the sweep")
	}
	if slices.ContainsFunc(r.writes(), func(c string) bool { return strings.HasPrefix(c, "Close ") }) {
		t.Errorf("closes after a failed carry: %v", r.writes())
	}
}

// Refusals before any write: not a pivot, or empty evidence.
func TestEscalateRefusesBadInput(t *testing.T) {
	r := newSweepRig(t, "ws-chart")
	if err := Escalate("ws-b1", "evidence", nil); err == nil {
		t.Error("Escalate over a non-pivot must error")
	}
	if err := Escalate("ws-pv", " ", nil); err == nil {
		t.Error("Escalate with empty evidence must error")
	}
	if w := r.writes(); len(w) != 0 {
		t.Errorf("a refused Escalate wrote %v", w)
	}
}

// Spec D2, FR31: a pivot's fresh Chart or corrected Spec joins the chain's Initiative,
// read off its Checkpoint (the chain's unclosed head), never the closed original Signal;
// a chain outside one yields an unparented bead.
func TestSweepSuccessorsJoinTheChainsInitiative(t *testing.T) {
	for name, tc := range map[string]struct {
		kind contract.Kind
		run  func() error
	}{
		"escalate": {contract.KindChart, func() error { return Escalate("ws-pv", "the FR3 assumption is wrong", nil) }},
		"confirm":  {contract.KindSpec, func() error { return Confirm("ws-pv", "", "the corrected body", nil) }},
	} {
		for _, p := range []struct{ parent, want string }{{"ws-init", "ws-init"}, {"ws-closed-init", ""}, {"", ""}} {
			t.Run(name+"/"+p.parent, func(t *testing.T) {
				r := newSweepRig(t, "ws-chart")
				r.f.Seed(
					bd.Bead{ID: "ws-init", Kind: contract.KindInitiative, Status: "open"},
					bd.Bead{ID: "ws-closed-init", Kind: contract.KindInitiative, Status: "closed"},
				)
				r.f.Mutate("ws-cp", func(b *bd.Bead) { b.Parent = p.parent })
				if err := tc.run(); err != nil {
					t.Fatal(err)
				}
				got := r.spawned(t, tc.kind)
				if want := p.want; (want == "" && got.Parent != "") || (want != "" && !strings.HasPrefix(got.ID, want+".")) {
					t.Errorf("spawned %s %s, want Parent %q", tc.kind, got.ID, want)
				}
			})
		}
	}
}

// The pivot-confirm Spec and pivot-escalate Chart are born with the Checkpoint
// head's context: label; an unlabelled head yields none.
func TestSweepSuccessorsCopyTheHeadsContext(t *testing.T) {
	for _, tc := range []struct {
		labels []string
		want   string
	}{{[]string{"kind:checkpoint", "context:apps-x", "repo:vendor/x"}, "apps-x"}, {[]string{"kind:checkpoint"}, ""}} {
		for name, run := range map[string]struct {
			kind contract.Kind
			run  func() error
		}{
			"confirm":  {contract.KindSpec, func() error { return Confirm("ws-pv", "", "the corrected body", nil) }},
			"escalate": {contract.KindChart, func() error { return Escalate("ws-pv", "the FR3 assumption is wrong", nil) }},
		} {
			r := newSweepRig(t, "ws-chart")
			r.f.Mutate("ws-cp", func(b *bd.Bead) { b.Labels = tc.labels })
			if err := run.run(); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			got := r.spawned(t, run.kind)
			if gotCtx := bd.ContextOf(got); gotCtx != tc.want || (bd.RepoStampOf(got) != "") != (tc.want != "") {
				t.Errorf("%s: spawned %+v, want Context %q and its Repo stamp", name, got, tc.want)
			}
		}
	}
}

// ADR 0212: under gitlab a corrected direct Spec is pinned Anchor: <fetched sha>
// and writes no anchor commit; a Chart-backed one is untouched.
func TestConfirmGitlabPinsCorrectedDirectSpec(t *testing.T) {
	for _, chart := range []string{"", "ws-chart"} {
		r := newSweepRig(t, chart)
		swap(t, &landStrategy, func() (string, error) { return git.LandStrategyGitlab, nil })
		swap(t, &fetchForge, func() (string, error) { return "forgesha", nil })
		if err := Confirm("ws-pv", "", "corrected body", nil); err != nil {
			t.Fatalf("Confirm: %v", err)
		}
		sha, pinned := git.ParseAnchor(r.spawned(t, contract.KindSpec).Description)
		if want := chart == ""; pinned != want || (want && sha != "forgesha") {
			t.Errorf("chart=%q pin = %q, %v; want a forgesha pin only for the direct Spec", chart, sha, pinned)
		}
	}
}

// FR 13: a nested pivot-corrected direct Spec pins at the child's fetched mainline,
// never the Workspace's, and writes no anchor commit.
func TestConfirmGitlabPinsNestedCorrectedSpecInChild(t *testing.T) {
	r := newSweepRig(t, "")
	swap(t, &repoOf, func(bd.Bead) git.Repo { return "/ws/child" })
	swap(t, &landStrategy, func() (string, error) { return git.LandStrategyGitlab, nil })
	swap(t, &fetchForge, func() (string, error) { return "wssha", nil })
	swap(t, &repoFetch, func(r git.Repo) (string, error) { return "childsha", nil })
	if err := Confirm("ws-pv", "", "corrected body", nil); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if sha, ok := git.ParseAnchor(r.spawned(t, contract.KindSpec).Description); !ok || sha != "childsha" {
		t.Errorf("pin = %q, %v, want childsha", sha, ok)
	}
}

func swap[T any](t *testing.T, seam *T, v T) {
	old := *seam
	t.Cleanup(func() { *seam = old })
	*seam = v
}

// withCritiques adds three Checkpoint members: an open filed Critique (ws-k1, with a
// Source: line), an open Critique without one (ws-k2) and a closed filed one (ws-k3).
func withCritiques(r *sweepRig) {
	src := "Source: !7 d1 9\n> quoted\n"
	crit := func(id, status, desc string) bd.Bead {
		return bd.Bead{ID: id, Kind: contract.KindCritique, Status: status, Parent: "ws-cp", Description: desc, Labels: []string{"kind:critique"}}
	}
	r.f.Seed(crit("ws-k1", "open", src), crit("ws-k2", "open", "no source"), crit("ws-k3", "closed", src))
}

// --carries (FR 19–21): each id is recorded as successor discovered-from Critique
// after the successor spawn and the supersede, before the Checkpoint carry and every close.
func TestSweepCarriesWriteTheEdgeBeforeCarryCheckpointAndCloseOut(t *testing.T) {
	for name, tc := range map[string]struct {
		kind contract.Kind
		run  func() error
	}{
		"confirm":  {contract.KindSpec, func() error { return Confirm("ws-pv", "", "body", []string{"ws-k1"}) }},
		"escalate": {contract.KindChart, func() error { return Escalate("ws-pv", "evidence", []string{"ws-k1"}) }},
	} {
		t.Run(name, func(t *testing.T) {
			r := newSweepRig(t, "ws-chart")
			withCritiques(r)
			if err := tc.run(); err != nil {
				t.Fatal(err)
			}
			succ := r.spawned(t, tc.kind)
			if !slices.Contains(succ.DiscoveredFrom, "ws-k1") {
				t.Errorf("successor discovered-from %v, want ws-k1 too", succ.DiscoveredFrom)
			}
			calls := r.writes()
			edge, carry, closed := at(t, calls, "AddEdge "+succ.ID+",ws-k1"), at(t, calls, "AddEdge ws-dep,"+succ.ID), at(t, calls, "Close ")
			if !(edge < carry && carry < closed) {
				t.Errorf("writes = %v, want the edge before the carry before every close", calls)
			}
			if sup := at(t, calls, "Supersede "); sup > edge {
				t.Errorf("writes = %v: the edge ran before the supersede", calls)
			}
		})
	}
}

// A re-run after a partial failure adds no duplicate edge (FR 21).
func TestSweepCarriesRerunAddsNoDuplicateEdge(t *testing.T) {
	r := newSweepRig(t, "ws-chart")
	withCritiques(r)
	r.f.FailOn("Close", "ws-pv2", errors.New("bd close: boom"))
	if err := Confirm("ws-pv", "", "body", []string{"ws-k1"}); err == nil {
		t.Fatal("first run: want the injected close failure")
	}
	r.f.Heal("Close", "ws-pv2")
	if err := Confirm("ws-pv", "", "body", []string{"ws-k1"}); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	spec := r.spawned(t, contract.KindSpec)
	if n := strings.Count(strings.Join(r.writes(), "\n"), "AddEdge "+spec.ID+",ws-k1"); n != 1 {
		t.Errorf("writes = %v, want one edge write", r.writes())
	}
	if n := slices.Index(spec.DiscoveredFrom, "ws-k1"); n < 0 || slices.Index(spec.DiscoveredFrom[n+1:], "ws-k1") >= 0 {
		t.Errorf("discovered-from = %v, want ws-k1 once", spec.DiscoveredFrom)
	}
}

// A bad --carries id (not a child, not filed, no Source:, already closed) is refused
// before any write (FR 20).
func TestSweepCarriesRefusesBadIDBeforeAnyWrite(t *testing.T) {
	for _, bad := range []string{"ws-nope", "ws-b1", "ws-k2", "ws-k3"} {
		for name, run := range map[string]func() error{
			"confirm":  func() error { return Confirm("ws-pv", "", "body", []string{"ws-k1", bad}) },
			"escalate": func() error { return Escalate("ws-pv", "evidence", []string{"ws-k1", bad}) },
		} {
			r := newSweepRig(t, "ws-chart")
			withCritiques(r)
			if err := run(); err == nil {
				t.Errorf("%s with --carries %s: want a refusal", name, bad)
			}
			if w := r.writes(); len(w) != 0 {
				t.Errorf("%s with --carries %s wrote %v", name, bad, w)
			}
		}
	}
}

// A sweep leaves the Checkpoint's open MR Watch open so it can answer the swept
// MR's reviewers; every other member and the Checkpoint still force-close.
func TestSweepLeavesTheMRWatchOpen(t *testing.T) {
	r := newSweepRig(t, "ws-chart")
	r.f.Seed(bd.Bead{ID: "ws-w", Kind: contract.KindMRWatch, Status: "open", Parent: "ws-cp"})
	if err := Confirm("ws-pv", "", "corrected body", nil); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if w := r.f.Bead("ws-w"); w.Status != "open" {
		t.Errorf("MR Watch status %s, want left open", w.Status)
	}
	for _, id := range []string{"ws-b1", "ws-c1", "ws-a1", "ws-cp"} {
		if r.f.Bead(id).Status != "closed" {
			t.Errorf("%s left open", id)
		}
	}
}

// ADR 0419 §3: a pivot's successors are born at the obsoleted Checkpoint's priority,
// not the Signal's or the Chart's the seam would otherwise read.
func TestPivotSuccessorsAreBornAtTheObsoletedCheckpointsPriority(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func() error
		kind contract.Kind
	}{
		{"escalate", func() error { return Escalate("ws-pv", "wrong", nil) }, contract.KindChart},
		{"confirm", func() error { return Confirm("ws-pv", "", "corrected body", nil) }, contract.KindSpec},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newSweepRig(t, "ws-chart")
			r.f.Mutate("ws-sig", func(b *bd.Bead) { b.Priority = 1 })
			r.f.Mutate("ws-chart", func(b *bd.Bead) { b.Priority = 3 })
			r.f.Mutate("ws-cp", func(b *bd.Bead) { b.Priority = 0 })
			if err := tc.run(); err != nil {
				t.Fatal(err)
			}
			if got := r.spawned(t, tc.kind).Priority; got != 0 {
				t.Errorf("%s successor born at P%d, want the Checkpoint's P0", tc.name, got)
			}
		})
	}
}
