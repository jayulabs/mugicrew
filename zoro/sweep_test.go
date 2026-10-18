package zoro

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// fakeGraph is a stateful bd/git double for the confirm sweep: closes flip status,
// so a second run sees the first run's writes and the convergence case is real.
type fakeGraph struct {
	status     map[string]string
	kids       []string                 // the Checkpoint's members
	pivots     []string                 // the Checkpoint's blocks targets (kind:pivot)
	chart      string                   // old Spec's caused-by Chart ("" = fast-laned)
	spawnedIDs map[contract.Kind]string // bead of each kind discovered-from the pivot
	spawned    []bd.Successor           // SpawnDiscoveredIfAbsent creates
	pred       string                   // the predecessor the new Spec was caused-by
	superseded map[string]string        // old → new
	anchors    []string                 // ids whose Anchor: pin was written
	pins       map[string]string        // id → its Anchor: pin sha
	closes     map[string][]string      // id → every close reason written (a double close shows as 2)
	failClose  string                   // a close of this id errors once (partial failure)
	forced     []string                 // ids closed with --force
	cuts       map[string]string        // branch → base
	seq        []string                 // supersede, carry and close calls in order: "supersede <old> <new>" / "carry <pred> <succ>" / "close <id>"
	carryErr   error                    // carry: the error bd.Carry returns
	parent     map[string]string        // id → its parent (membership edge)
	labels     map[string][]string      // id → its labels
	surveys    [][3]string              // SpawnSurveyIfAbsent calls: {chart, title, drafter}
}

func newFakeGraph(chart string) *fakeGraph {
	return &fakeGraph{
		status: map[string]string{
			"ws-pv": "in_progress", "ws-pv2": "open", "ws-pvx": "closed", "ws-cp": "open",
			"ws-b1": "in_progress", "ws-c1": "open", "ws-a1": "open", "ws-done": "closed",
		},
		kids:       []string{"ws-b1", "ws-c1", "ws-a1", "ws-done"},
		pivots:     []string{"ws-pv", "ws-pv2", "ws-pvx"},
		chart:      chart,
		superseded: map[string]string{},
		pins:       map[string]string{},
		closes:     map[string][]string{},
		spawnedIDs: map[contract.Kind]string{},
		cuts:       map[string]string{},
	}
}

func (g *fakeGraph) bead(id string) bd.Bead {
	kind := contract.KindBuild
	if slices.Contains(g.pivots, id) {
		kind = contract.KindPivot
	}
	desc := ""
	if sha, ok := g.pins[id]; ok {
		desc = "Anchor: " + sha + "\n"
	}
	return bd.Bead{ID: id, Kind: kind, Status: g.status[id], Title: "Spec: old", Description: desc, Parent: g.parent[id], Labels: g.labels[id]}
}

func (g *fakeGraph) close(id, reason string) error {
	if id == g.failClose {
		g.failClose = ""
		return errors.New("bd close: boom")
	}
	g.seq = append(g.seq, "close "+id)
	g.closes[id] = append(g.closes[id], reason)
	g.status[id] = "closed"
	return nil
}

func stubSweepSeams(t *testing.T, g *fakeGraph) {
	t.Helper()
	oShow, oCp, oSpec, oCause, oSpawn, oSup, oKids, oClose, oForce, oPiv, oCut, oCarry, oSurvey :=
		showBead, pivotCheckpoint, pivotSpec, causedBy, spawnDiscovered, supersede, openChildBeads, closeBead, forceClose, openPivots, cutBranch, carry, spawnSurvey
	t.Cleanup(func() {
		showBead, pivotCheckpoint, pivotSpec, causedBy, spawnDiscovered, supersede, openChildBeads, closeBead, forceClose, openPivots, cutBranch, carry, spawnSurvey =
			oShow, oCp, oSpec, oCause, oSpawn, oSup, oKids, oClose, oForce, oPiv, oCut, oCarry, oSurvey
	})
	spawnSurvey = func(chart, title, drafter string) (string, bool, error) {
		g.surveys = append(g.surveys, [3]string{chart, title, drafter})
		return "mc-survey", len(g.surveys) == 1, nil
	}
	carry = func(pred, succ string) ([]string, []bd.CarrySkip, error) {
		g.seq = append(g.seq, "carry "+pred+" "+succ)
		return nil, nil, g.carryErr
	}
	showBead = func(id string) (bd.Bead, error) { return g.bead(id), nil }
	pivotCheckpoint = func(string) (string, bool, error) { return "ws-cp", true, nil }
	pivotSpec = func(string) (string, bool, error) { return "ws-old", true, nil }
	causedBy = func(id string, k contract.Kind) (string, bool, error) {
		switch {
		case id == "ws-old" && k == contract.KindChart:
			return g.chart, g.chart != "", nil
		case id == "ws-old" && k == contract.KindSignal && g.chart == "",
			id == g.chart && k == contract.KindSignal:
			return "ws-sig", true, nil
		}
		return "", false, nil
	}
	spawnDiscovered = func(origin, pred string, s bd.Successor) (string, bool, error) {
		if id, ok := g.spawnedIDs[s.Kind]; ok {
			return id, false, nil
		}
		id := map[contract.Kind]string{contract.KindSpec: "ws-new", contract.KindChart: "mc-newchart"}[s.Kind]
		g.spawnedIDs[s.Kind], g.pred = id, pred
		g.spawned = append(g.spawned, s)
		return id, true, nil
	}
	// Local strategy: the pin is the tip; no Spec: trailer is on main.
	swap(t, &landStrategy, func() (string, error) { return git.LandStrategyLocal, nil })
	swap(t, &repoLanded, func(git.Repo, string) (string, error) { return "", git.ErrNotLanded })
	swap(t, &repoTip, func(git.Repo) (string, error) { return "tipsha", nil })
	swap(t, &updateBody, func(id string, a []string) error {
		g.anchors = append(g.anchors, id)
		g.pins[id] = "tipsha"
		return nil
	})
	supersede = func(old, new string) error {
		g.seq = append(g.seq, "supersede "+old+" "+new)
		g.superseded[old] = new
		return nil
	}
	openChildBeads = func(string) ([]bd.Bead, error) {
		var out []bd.Bead
		for _, id := range g.kids {
			if b := g.bead(id); b.Open() {
				out = append(out, b)
			}
		}
		return out, nil
	}
	closeBead = g.close
	forceClose = func(id, reason string) error { g.forced = append(g.forced, id); return g.close(id, reason) }
	cutBranch = func(name, base string) (bool, error) {
		_, had := g.cuts[name]
		g.cuts[name] = base
		return !had, nil
	}
	openPivots = func(string) ([]bd.Bead, error) {
		var out []bd.Bead
		for _, id := range g.pivots {
			if b := g.bead(id); b.Open() {
				out = append(out, b)
			}
		}
		return out, nil
	}
}

// carryBetween asserts the Checkpoint carries to succ exactly once, after the
// supersede (when there is one) and before any close (Spec D3).
func carryBetween(t *testing.T, g *fakeGraph, supersede, succ string) {
	t.Helper()
	want := "carry ws-cp " + succ
	i := slices.Index(g.seq, want)
	if i < 0 || slices.Index(g.seq[i+1:], want) >= 0 {
		t.Fatalf("seq = %v, want one %q", g.seq, want)
	}
	if supersede != "" && slices.Index(g.seq, supersede) > i {
		t.Errorf("seq = %v: carry ran before %q", g.seq, supersede)
	}
	if j := slices.IndexFunc(g.seq, func(s string) bool { return strings.HasPrefix(s, "close ") }); j >= 0 && j < i {
		t.Errorf("seq = %v: a close ran before the carry", g.seq)
	}
}

// Confirm on a charted Spec (FR39–42): the corrected Spec is spawned caused-by the
// old Spec's Chart (so checkpointBase keeps the Chart's landed base) with no
// anchor; old Spec supersedes→new; every open member, then the Checkpoint, close
// obsolete; the open sibling pivot closes 'superseded: <pivot>'; the pivot closes
// 'confirmed' last. Already-closed beads are not re-closed.
func TestConfirmChartedSweep(t *testing.T) {
	g := newFakeGraph("ws-chart")
	stubSweepSeams(t, g)
	if err := Confirm("ws-pv", "", "corrected body"); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if g.pred != "ws-chart" {
		t.Errorf("new Spec caused-by %q, want the old Spec's Chart", g.pred)
	}
	if want := (bd.Successor{Kind: contract.KindSpec, Title: "Spec: old", Description: "corrected body", DraftedBy: "ws-pv"}); !reflect.DeepEqual(g.spawned, []bd.Successor{want}) {
		t.Errorf("spawned %+v, want one %+v", g.spawned, want)
	}
	if len(g.anchors) != 0 {
		t.Errorf("charted confirm wrote anchors %v, want none", g.anchors)
	}
	if g.superseded["ws-old"] != "ws-new" {
		t.Errorf("superseded = %v, want ws-old→ws-new", g.superseded)
	}
	carryBetween(t, g, "supersede ws-old ws-new", "ws-new")
	for _, id := range []string{"ws-b1", "ws-c1", "ws-a1", "ws-cp"} {
		if r := g.closes[id]; len(r) != 1 || r[0] != obsoleteReason("ws-old", "ws-new", "ws-pv") {
			t.Errorf("%s closes = %v, want one obsolete close", id, r)
		}
	}
	// Members (blocked by siblings and the pivots) and the frozen Checkpoint force-close.
	if want := []string{"ws-b1", "ws-c1", "ws-a1", "ws-cp"}; !reflect.DeepEqual(g.forced, want) {
		t.Errorf("forced = %v, want %v", g.forced, want)
	}
	if r := g.closes["ws-pv2"]; !reflect.DeepEqual(r, []string{"superseded: ws-pv"}) {
		t.Errorf("sibling closes = %v, want [superseded: ws-pv]", r)
	}
	if r := g.closes["ws-pv"]; !reflect.DeepEqual(r, []string{"confirmed"}) {
		t.Errorf("pivot closes = %v, want [confirmed]", r)
	}
	for _, id := range []string{"ws-done", "ws-pvx"} {
		if len(g.closes[id]) != 0 {
			t.Errorf("already-closed %s re-closed: %v", id, g.closes[id])
		}
	}
}

// A fast-laned Spec (no Chart): the corrected Spec is caused-by the old Spec's
// Signal and gets an Anchor: pin at the local tip, no commit on main (FR41, ADR 0035).
func TestConfirmFastLanedAnchorsNewSpec(t *testing.T) {
	g := newFakeGraph("")
	stubSweepSeams(t, g)
	if err := Confirm("ws-pv", "Spec: fixed", "body"); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if g.pred != "ws-sig" {
		t.Errorf("new Spec caused-by %q, want the old Spec's Signal", g.pred)
	}
	if g.spawned[0].Title != "Spec: fixed" {
		t.Errorf("title = %q, want the given one", g.spawned[0].Title)
	}
	if g.spawned[0].DraftedBy != "ws-pv" {
		t.Errorf("new Spec DraftedBy = %q, want the pivot (FR 28)", g.spawned[0].DraftedBy)
	}
	if want := []string{"ws-new"}; !reflect.DeepEqual(g.anchors, want) || g.pins["ws-new"] != "tipsha" {
		t.Errorf("anchors = %v pins = %v, want an Anchor: pin on %v at the tip", g.anchors, g.pins, want)
	}
}

// FR48: a crash mid-sweep (the sibling close fails) is fixed by running it again —
// the re-run spawns no second Spec, writes no second anchor, and closes each bead
// exactly once. A third run over the settled graph is a no-op.
func TestConfirmRerunConverges(t *testing.T) {
	g := newFakeGraph("")
	g.failClose = "ws-pv2"
	stubSweepSeams(t, g)
	if err := Confirm("ws-pv", "", "body"); err == nil {
		t.Fatal("first run: want the injected close failure")
	}
	if g.status["ws-pv"] == "closed" {
		t.Fatal("pivot closed before the sweep finished; it must close last")
	}
	if err := Confirm("ws-pv", "", "body"); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	// The re-run re-carries (add-only, a no-op in bd); the pivot is closed now, so
	// the third run is a no-op and carries nothing again.
	n := len(g.seq)
	if err := Confirm("ws-pv", "", "body"); err != nil {
		t.Fatalf("third run: %v", err)
	}
	if len(g.seq) != n {
		t.Errorf("a run over a closed pivot wrote %v", g.seq[n:])
	}
	if len(g.spawned) != 1 || len(g.anchors) != 1 {
		t.Errorf("spawned %d Specs, %d anchors; want 1 each", len(g.spawned), len(g.anchors))
	}
	for id, r := range g.closes {
		if len(r) != 1 {
			t.Errorf("%s closed %d times: %v", id, len(r), r)
		}
	}
}

// A carry error fails the sweep before closeOut: nothing is closed, so a re-run
// (FR48) finds the same Spec via spawnDiscovered and carries again.
func TestConfirmCarryErrorStopsBeforeCloseOut(t *testing.T) {
	g := newFakeGraph("ws-chart")
	g.carryErr = errors.New("bd down")
	stubSweepSeams(t, g)
	if err := Confirm("ws-pv", "", "body"); err == nil {
		t.Fatal("a carry error must fail the sweep")
	}
	if len(g.closes) != 0 {
		t.Errorf("closes after a failed carry: %v", g.closes)
	}
	g.carryErr = nil
	if err := Confirm("ws-pv", "", "body"); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if len(g.spawned) != 1 || g.status["ws-pv"] != "closed" {
		t.Errorf("re-run spawned %d Specs, pivot %s; want 1, closed", len(g.spawned), g.status["ws-pv"])
	}
}

// Refusals before any write: not a pivot, or an empty corrected body.
func TestConfirmRefusesBadInput(t *testing.T) {
	g := newFakeGraph("ws-chart")
	stubSweepSeams(t, g)
	if err := Confirm("ws-b1", "", "body"); err == nil {
		t.Error("Confirm over a non-pivot must error")
	}
	if err := Confirm("ws-pv", "", "  \n"); err == nil {
		t.Error("Confirm with an empty body must error")
	}
	if len(g.spawned)+len(g.closes)+len(g.superseded) != 0 {
		t.Errorf("a refused Confirm wrote: spawned %v closes %v superseded %v", g.spawned, g.closes, g.superseded)
	}
}

// Escalate on a charted Spec (FR43–44, 46): one fresh Chart caused-by the original
// Signal (reached through the old Chart), titled for the Signal and seeded with the
// evidence as a wayfinder map off the placeholder, with a Survey seeded under it
// through the shared helper (Spec mc-r832 D5); chart/<new> cut from main; old
// Chart supersedes→new Chart; the same close-out as confirm; the pivot closes
// 'escalated' last. No Spec is spawned or superseded.
func TestEscalateChartedSweep(t *testing.T) {
	g := newFakeGraph("ws-chart")
	stubSweepSeams(t, g)
	if err := Escalate("ws-pv", "the FR3 assumption is wrong"); err != nil {
		t.Fatalf("Escalate: %v", err)
	}
	if g.pred != "ws-sig" {
		t.Errorf("new Chart caused-by %q, want the original Signal", g.pred)
	}
	if len(g.spawned) != 1 || g.spawned[0].Kind != contract.KindChart || !g.spawned[0].Container {
		t.Fatalf("spawned %+v, want one container Chart", g.spawned)
	}
	c := g.spawned[0]
	if c.DraftedBy != "ws-pv" {
		t.Errorf("new Chart DraftedBy = %q, want the pivot (FR 28)", c.DraftedBy)
	}
	if c.Title != "Spec: old" {
		t.Errorf("Chart title = %q, want the Signal's title verbatim", c.Title)
	}
	if c.Description == contract.ChartPlaceholderBody || !strings.Contains(c.Description, "the FR3 assumption is wrong") {
		t.Errorf("Chart body %q must be seeded with the evidence", c.Description)
	}
	for _, f := range strings.Split(contract.WayfinderMapFields, " / ") {
		if !strings.Contains(c.Description, "## "+f+"\n") {
			t.Errorf("Chart body lacks the wayfinder section %q", f)
		}
	}
	if want := [][3]string{{"mc-newchart", "Spec: old", "ws-pv"}}; !reflect.DeepEqual(g.surveys, want) {
		t.Errorf("surveys = %v, want one under the new Chart, titled for the Signal, drafted-by the pivot (Spec D5)", g.surveys)
	}
	if want := map[string]string{"chart/mc-newchart": git.Mainline()}; !reflect.DeepEqual(g.cuts, want) {
		t.Errorf("cuts = %v, want %v", g.cuts, want)
	}
	if want := map[string]string{"ws-chart": "mc-newchart"}; !reflect.DeepEqual(g.superseded, want) {
		t.Errorf("superseded = %v, want %v", g.superseded, want)
	}
	carryBetween(t, g, "supersede ws-chart mc-newchart", "mc-newchart")
	if len(g.anchors) != 0 {
		t.Errorf("escalate wrote anchors %v, want none", g.anchors)
	}
	for _, id := range []string{"ws-b1", "ws-c1", "ws-a1", "ws-cp"} {
		if r := g.closes[id]; len(r) != 1 || r[0] != escalateReason("ws-old", "mc-newchart", "ws-pv") {
			t.Errorf("%s closes = %v, want one obsolete close", id, r)
		}
	}
	if r := g.closes["ws-pv2"]; !reflect.DeepEqual(r, []string{"superseded: ws-pv"}) {
		t.Errorf("sibling closes = %v, want [superseded: ws-pv]", r)
	}
	if r := g.closes["ws-pv"]; !reflect.DeepEqual(r, []string{"escalated"}) {
		t.Errorf("pivot closes = %v, want [escalated]", r)
	}
}

// FR45: a fast-laned Spec escalates the same way minus the Chart supersede — the
// new Chart hangs off the old Spec's Signal and nothing is superseded (caused-by
// the Signal is the only link; the recorded decision, ADR 0046 §6).
func TestEscalateFastLanedSkipsSupersede(t *testing.T) {
	g := newFakeGraph("")
	stubSweepSeams(t, g)
	if err := Escalate("ws-pv", "evidence"); err != nil {
		t.Fatalf("Escalate: %v", err)
	}
	if g.pred != "ws-sig" || len(g.spawned) != 1 {
		t.Errorf("spawned %v caused-by %q, want one Chart caused-by the Signal", g.spawned, g.pred)
	}
	if len(g.superseded) != 0 {
		t.Errorf("fast-laned escalate superseded %v, want nothing", g.superseded)
	}
	carryBetween(t, g, "", "mc-newchart")
	if g.status["ws-cp"] != "closed" || g.status["ws-pv"] != "closed" {
		t.Error("fast-laned escalate must close the Checkpoint and the pivot")
	}
}

// FR48: a partial escalate re-runs to convergence — one Chart, each bead closed once.
func TestEscalateRerunConverges(t *testing.T) {
	g := newFakeGraph("ws-chart")
	g.failClose = "ws-pv2"
	stubSweepSeams(t, g)
	if err := Escalate("ws-pv", "evidence"); err == nil {
		t.Fatal("first run: want the injected close failure")
	}
	if err := Escalate("ws-pv", "evidence"); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	n := len(g.seq)
	if err := Escalate("ws-pv", "evidence"); err != nil {
		t.Fatalf("third run: %v", err)
	}
	if len(g.seq) != n {
		t.Errorf("a run over a closed pivot wrote %v", g.seq[n:])
	}
	if len(g.spawned) != 1 {
		t.Errorf("spawned %d Charts, want 1", len(g.spawned))
	}
	for id, r := range g.closes {
		if len(r) != 1 {
			t.Errorf("%s closed %d times: %v", id, len(r), r)
		}
	}
}

// A carry error fails the escalate before closeOut (the cut branch and supersede
// are idempotent legs a re-run repeats).
func TestEscalateCarryErrorStopsBeforeCloseOut(t *testing.T) {
	g := newFakeGraph("ws-chart")
	g.carryErr = errors.New("bd down")
	stubSweepSeams(t, g)
	if err := Escalate("ws-pv", "evidence"); err == nil {
		t.Fatal("a carry error must fail the sweep")
	}
	if len(g.closes) != 0 {
		t.Errorf("closes after a failed carry: %v", g.closes)
	}
}

// Refusals before any write: not a pivot, or empty evidence.
func TestEscalateRefusesBadInput(t *testing.T) {
	g := newFakeGraph("ws-chart")
	stubSweepSeams(t, g)
	if err := Escalate("ws-b1", "evidence"); err == nil {
		t.Error("Escalate over a non-pivot must error")
	}
	if err := Escalate("ws-pv", " "); err == nil {
		t.Error("Escalate with empty evidence must error")
	}
	if len(g.spawned)+len(g.closes)+len(g.superseded) != 0 {
		t.Errorf("a refused Escalate wrote: spawned %v closes %v superseded %v", g.spawned, g.closes, g.superseded)
	}
}

// Spec D2, FR31: an escalated pivot's fresh Chart joins the escalated chain's
// Initiative, read off its Checkpoint (the chain's unclosed head), never the
// closed original Signal; a chain outside one yields an unparented Chart.
func TestEscalateChartJoinsTheChainsInitiative(t *testing.T) {
	for _, tc := range []struct{ parent, want string }{{"ws-init", "ws-init"}, {"ws-closed-init", ""}, {"", ""}} {
		t.Run(tc.parent, func(t *testing.T) {
			g := newFakeGraph("ws-chart")
			g.parent = map[string]string{"ws-cp": tc.parent}
			stubSweepSeams(t, g)
			stubInitiative(t)
			if err := Escalate("ws-pv", "the FR3 assumption is wrong"); err != nil {
				t.Fatalf("Escalate: %v", err)
			}
			if len(g.spawned) != 1 || g.spawned[0].Parent != tc.want {
				t.Errorf("spawned %+v, want one Chart with Parent %q", g.spawned, tc.want)
			}
		})
	}
}

func TestConfirmSpecJoinsTheChainsInitiative(t *testing.T) {
	for _, tc := range []struct{ parent, want string }{{"ws-init", "ws-init"}, {"ws-closed-init", ""}, {"", ""}} {
		t.Run(tc.parent, func(t *testing.T) {
			g := newFakeGraph("ws-chart")
			g.parent = map[string]string{"ws-cp": tc.parent}
			stubSweepSeams(t, g)
			stubInitiative(t)
			if err := Confirm("ws-pv", "", "the corrected body"); err != nil {
				t.Fatalf("Confirm: %v", err)
			}
			if len(g.spawned) != 1 || g.spawned[0].Parent != tc.want {
				t.Errorf("spawned %+v, want one Spec with Parent %q", g.spawned, tc.want)
			}
		})
	}
}

// The pivot-confirm Spec and pivot-escalate Chart are born with the Checkpoint
// head's context: label; an unlabelled head yields none.
func TestSweepSuccessorsCopyTheHeadsContext(t *testing.T) {
	for _, tc := range []struct {
		labels []string
		want   string
	}{{[]string{"kind:checkpoint", "context:apps-x", "repo:vendor/x"}, "apps-x"}, {[]string{"kind:checkpoint"}, ""}} {
		for name, run := range map[string]func() error{
			"confirm":  func() error { return Confirm("ws-pv", "", "the corrected body") },
			"escalate": func() error { return Escalate("ws-pv", "the FR3 assumption is wrong") },
		} {
			g := newFakeGraph("ws-chart")
			g.labels = map[string][]string{"ws-cp": tc.labels}
			stubSweepSeams(t, g)
			stubInitiative(t)
			if err := run(); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if len(g.spawned) != 1 || g.spawned[0].Context != tc.want || g.spawned[0].Repo != map[bool]string{true: "vendor/x"}[tc.want != ""] {
				t.Errorf("%s: spawned %+v, want Context %q and its Repo stamp", name, g.spawned, tc.want)
			}
		}
	}
}

// ADR 0212: under gitlab a corrected direct Spec is pinned Anchor: <fetched sha>
// and writes no anchor commit; a Chart-backed one is untouched.
func TestConfirmGitlabPinsCorrectedDirectSpec(t *testing.T) {
	for _, chart := range []string{"", "ws-chart"} {
		g := newFakeGraph(chart)
		stubSweepSeams(t, g)
		var pinned []string
		swap(t, &landStrategy, func() (string, error) { return git.LandStrategyGitlab, nil })
		swap(t, &fetchForge, func() (string, error) { return "forgesha", nil })
		swap(t, &updateBody, func(id string, a []string) error { pinned = append(pinned, id+a[1]); return nil })
		if err := Confirm("ws-pv", "", "corrected body"); err != nil {
			t.Fatalf("Confirm: %v", err)
		}
		want := []string{"ws-new\nAnchor: forgesha\n"}
		if chart != "" {
			want = nil
		}
		if !slices.Equal(pinned, want) || len(g.anchors) != 0 {
			t.Errorf("chart=%q pinned=%q anchors=%v, want %q and no commit", chart, pinned, g.anchors, want)
		}
	}
}

// FR 13: a nested pivot-corrected direct Spec pins at the child's fetched mainline,
// never the Workspace's, and writes no anchor commit.
func TestConfirmGitlabPinsNestedCorrectedSpecInChild(t *testing.T) {
	g := newFakeGraph("")
	stubSweepSeams(t, g)
	var pinned []string
	swap(t, &repoOf, func(bd.Bead) git.Repo { return "/ws/child" })
	swap(t, &landStrategy, func() (string, error) { return git.LandStrategyGitlab, nil })
	swap(t, &fetchForge, func() (string, error) { return "wssha", nil })
	swap(t, &repoFetch, func(r git.Repo) (string, error) { return "childsha", nil })
	swap(t, &updateBody, func(id string, a []string) error { pinned = append(pinned, id+a[1]); return nil })
	if err := Confirm("ws-pv", "", "corrected body"); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if want := []string{"ws-new\nAnchor: childsha\n"}; !slices.Equal(pinned, want) || len(g.anchors) != 0 {
		t.Errorf("pinned=%q anchors=%v, want %q and no commit", pinned, g.anchors, want)
	}
}

func swap[T any](t *testing.T, seam *T, v T) {
	old := *seam
	t.Cleanup(func() { *seam = old })
	*seam = v
}
