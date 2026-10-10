package zoro

import (
	"errors"
	"os/exec"
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
	anchors    []string                 // trailers written onto main
	landed     map[string]bool          // trailers already on main
	closes     map[string][]string      // id → every close reason written (a double close shows as 2)
	failClose  string                   // a close of this id errors once (partial failure)
	forced     []string                 // ids closed with --force
	cuts       map[string]string        // branch → base
	seq        []string                 // supersede, carry and close calls in order: "supersede <old> <new>" / "carry <pred> <succ>" / "close <id>"
	carryErr   error                    // carry: the error bd.Carry returns
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
		landed:     map[string]bool{},
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
	return bd.Bead{ID: id, Kind: kind, Status: g.status[id], Title: "Spec: old"}
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
	// AnchorIfAbsent takes the land lock under the git-common-dir: give it a scratch
	// repo, so the test needs no checkout (Nix sandbox) and never touches the real one.
	t.Chdir(t.TempDir())
	if out, err := exec.Command("git", "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	oShow, oCp, oSpec, oCause, oSpawn, oLand, oAnchor, oSup, oKids, oClose, oForce, oPiv, oCut, oCarry :=
		showBead, pivotCheckpoint, pivotSpec, causedBy, spawnDiscovered, landedBase, writeAnchor, supersede, openChildBeads, closeBead, forceClose, openPivots, cutBranch, carry
	t.Cleanup(func() {
		showBead, pivotCheckpoint, pivotSpec, causedBy, spawnDiscovered, landedBase, writeAnchor, supersede, openChildBeads, closeBead, forceClose, openPivots, cutBranch, carry =
			oShow, oCp, oSpec, oCause, oSpawn, oLand, oAnchor, oSup, oKids, oClose, oForce, oPiv, oCut, oCarry
	})
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
	landedBase = func(trailer string) (string, error) {
		if g.landed[trailer] {
			return "anchorsha", nil
		}
		return "", git.ErrNotLanded
	}
	writeAnchor = func(trailer string) (string, error) {
		g.anchors = append(g.anchors, trailer)
		g.landed[trailer] = true
		return "anchorsha", nil
	}
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
// Signal and gets a fresh "Spec: <new>" anchor on main (FR41, ADR 0035).
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
	if want := []string{baseTrailer(contract.KindSpec, "ws-new")}; !reflect.DeepEqual(g.anchors, want) {
		t.Errorf("anchors = %v, want %v", g.anchors, want)
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
// evidence as a wayfinder map off the placeholder; chart/<new> cut from main; old
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
