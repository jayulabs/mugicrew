package bd

import (
	"reflect"
	"testing"

	"jayulabs.org/mugicrew/contract"
)

// Blocks/Related are idempotent by construction: the argv is built only when the
// edge is absent, so the run-twice case (edge already listed) adds nothing.
func TestDepAddArgsIfAbsent(t *testing.T) {
	for _, typ := range []string{"blocks", "related"} {
		got := depAddArgsIfAbsent([]Bead{{ID: "mc-x"}}, "ws-cp", "ws-pv", typ)
		if want := []string{"dep", "add", "ws-cp", "ws-pv", "-t", typ}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s absent: args = %v, want %v", typ, got, want)
		}
		if got := depAddArgsIfAbsent([]Bead{{ID: "mc-x"}, {ID: "ws-pv"}}, "ws-cp", "ws-pv", typ); got != nil {
			t.Errorf("%s present: args = %v, want nil (no-op)", typ, got)
		}
	}
}

// OpenPivots filters a `bd dep list --json` read to open
// kind:pivot only: closed pivots and other kinds (a Build blocker) drop out.
func TestOpenPivotsFilter(t *testing.T) {
	blob := `[
	 {"id":"ws-p1","status":"open","labels":["kind:pivot"],"dependency_type":"blocks"},
	 {"id":"ws-p2","status":"closed","labels":["kind:pivot"],"dependency_type":"blocks"},
	 {"id":"ws-b1","status":"open","labels":["kind:build"],"dependency_type":"blocks"},
	 {"id":"ws-p3","status":"in_progress","labels":["kind:pivot"],"dependency_type":"blocks"}]`
	beads, err := parseBeads([]byte(blob))
	if err != nil {
		t.Fatalf("parseBeads: %v", err)
	}
	var ids []string
	for _, b := range openChildrenOfKind(beads, contract.KindPivot) {
		ids = append(ids, b.ID)
	}
	if want := []string{"ws-p1", "ws-p3"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("open pivots = %v, want %v", ids, want)
	}
}

// PivotsDiscoveredFrom reads a raiser's up/discovered-from list: both the pivot it
// raised and an existing pivot a merged raise pointed at it count; a non-pivot
// discovered-from (a Critique offshoot) does not.
func TestPivotsDiscoveredFromParse(t *testing.T) {
	blob := `[
	 {"id":"ws-new","status":"open","labels":["kind:pivot"],"dependency_type":"discovered-from"},
	 {"id":"ws-crit","status":"open","labels":["kind:critique"],"dependency_type":"discovered-from"},
	 {"id":"mc-merged","status":"open","labels":["kind:pivot"],"dependency_type":"discovered-from"}]`
	beads, err := parseBeads([]byte(blob))
	if err != nil {
		t.Fatalf("parseBeads: %v", err)
	}
	if got := openChildrenOfKind(beads, contract.KindPivot); len(got) != 2 || got[0].ID != "ws-new" || got[1].ID != "mc-merged" {
		t.Errorf("discovered-from pivots = %v, want ws-new, mc-merged", got)
	}
}

// The reverse lookups pick the wanted kind out of the pivot's dep list: the
// Checkpoint it blocks (up, blocks) and its Spec (down, related), closed or not.
func TestPivotReverseLookupParse(t *testing.T) {
	up, err := parseBeads([]byte(`[{"id":"ws-b1","status":"open","labels":["kind:build"]},{"id":"ws-cp","status":"open","labels":["kind:checkpoint"]}]`))
	if err != nil {
		t.Fatalf("parseBeads: %v", err)
	}
	if id, ok := firstOfKind(up, contract.KindCheckpoint); !ok || id != "ws-cp" {
		t.Errorf("checkpoint = %q,%v, want ws-cp", id, ok)
	}
	down, err := parseBeads([]byte(`[{"id":"ws-sp","status":"closed","labels":["kind:spec"]}]`))
	if err != nil {
		t.Fatalf("parseBeads: %v", err)
	}
	if id, ok := firstOfKind(down, contract.KindSpec); !ok || id != "ws-sp" {
		t.Errorf("spec = %q,%v, want ws-sp", id, ok)
	}
	if _, ok := firstOfKind(nil, contract.KindSpec); ok {
		t.Error("no related Spec must report not found")
	}
}

// Each reverse lookup walks its own direction and edge type and keeps only its
// kind: the fake CLI answers one dep list holding a decoy before the wanted bead,
// so a wrong argv or a kind-blind pick fails. RollbackTarget is the input the whole
// Rollback pass keys on (FR12).
func TestReverseLookupArgsAndKind(t *testing.T) {
	blob := `[{"id":"ws-decoy","status":"open","labels":["kind:build"]},
	 {"id":"ws-cp","status":"closed","labels":["kind:checkpoint"]},
	 {"id":"ws-sp","status":"closed","labels":["kind:spec"]}]`
	var argv []string
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(args ...string) (string, error) { argv = args; return blob, nil }
	for _, c := range []struct {
		name   string
		lookup func(string) (string, bool, error)
		dir    string
		typ    string
		want   string
	}{
		{"RollbackTarget", RollbackTarget, "down", "related", "ws-cp"},
		{"PivotSpec", PivotSpec, "down", "related", "ws-sp"},
		{"PivotCheckpoint", PivotCheckpoint, "up", "blocks", "ws-cp"},
	} {
		got, ok, err := c.lookup("mc-x")
		if err != nil || !ok || got != c.want {
			t.Errorf("%s = %q,%v,%v; want %q", c.name, got, ok, err, c.want)
		}
		if want := []string{"dep", "list", "mc-x", "--direction=" + c.dir, "--json", "-t", c.typ}; !reflect.DeepEqual(argv, want) {
			t.Errorf("%s argv = %v, want %v", c.name, argv, want)
		}
	}
	run = func(...string) (string, error) {
		return `[{"id":"ws-decoy","status":"open","labels":["kind:build"]}]`, nil
	}
	if _, ok, err := RollbackTarget("ws-rb"); ok || err != nil {
		t.Errorf("a Rollback related to no Checkpoint must report not found, got ok=%v err=%v", ok, err)
	}
}

// Supersede (FR47): already superseded → no-op; a pre-existing same-direction
// related edge is removed first (bd rejects supersede over it); no edge → just
// supersede.
func TestSupersedeArgs(t *testing.T) {
	sup := []string{"supersede", "ws-old", "--with", "ws-new"}
	rm := []string{"dep", "remove", "ws-old", "ws-new"}
	cases := []struct {
		name       string
		superseded []Bead // old's down supersedes targets
		all        []Bead // old's down targets, any type
		want       [][]string
	}{
		{"already superseded", []Bead{{ID: "ws-new"}}, []Bead{{ID: "ws-new"}}, nil},
		{"related edge first", nil, []Bead{{ID: "ws-chart"}, {ID: "ws-new"}}, [][]string{rm, sup}},
		{"no edge", []Bead{{ID: "ws-other"}}, []Bead{{ID: "ws-chart"}, {ID: "ws-other"}}, [][]string{sup}},
	}
	for _, c := range cases {
		if got := supersedeArgs(c.superseded, c.all, "ws-old", "ws-new"); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: args = %v, want %v", c.name, got, c.want)
		}
	}
}

// A sweep-created bead carries caused-by its predecessor AND discovered-from its
// origin pivot — the second edge is the re-run's idempotency key.
func TestDiscoveredCreateArgs(t *testing.T) {
	got := discoveredCreateArgs("ws-pv", "ws-chart", Successor{Kind: contract.KindSpec, Title: "t", Description: "d"})
	want := []string{"create", "--type", "task", "--labels", "kind:spec", "--no-inherit-labels",
		"--deps", "caused-by:ws-chart,discovered-from:ws-pv", "--title", "t", "--description", "d", "--silent"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v\nwant %v", got, want)
	}
	// A title or body spelling "--deps" must not catch the extra edge.
	got = discoveredCreateArgs("ws-pv", "ws-chart", Successor{Kind: contract.KindSpec, Title: "--deps", Description: "--deps"})
	want = []string{"create", "--type", "task", "--labels", "kind:spec", "--no-inherit-labels",
		"--deps", "caused-by:ws-chart,discovered-from:ws-pv", "--title", "--deps", "--description", "--deps", "--silent"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("--deps title: args = %v\nwant %v", got, want)
	}
	if got := titleArg(discoveredCreateArgs("ws-pv", "ws-chart", Successor{Kind: contract.KindSpec, Title: "Spec: t"})); got != "t" {
		t.Errorf("prefixed discovered --title = %q, want stripped", got)
	}
}

// The title-keyed offshoot filer matches only a Signal with the exact title: a
// crewmate's own offshoot Signal (Draft or closed) with another title never does.
func TestSignalTitled(t *testing.T) {
	found := []Bead{
		{ID: "mc-a", Kind: contract.KindSignal, Title: "other", Status: "deferred"},
		{ID: "mc-b", Kind: contract.KindSignal, Title: "other", Status: "closed"},
		{ID: "mc-c", Kind: contract.KindBuild, Title: "x: commits stranded"},
		{ID: "mc-d", Kind: contract.KindSignal, Title: "x: commits stranded"},
	}
	if id, ok := signalTitled(found[:3], "x: commits stranded"); ok {
		t.Errorf("different titles and a non-Signal matched %s", id)
	}
	if id, ok := signalTitled(found, "x: commits stranded"); !ok || id != "mc-d" {
		t.Errorf("exact title = %q, %v; want mc-d, true", id, ok)
	}
}

// A carrier read keeps Signals, Specs and Charts, whatever their status, and drops the rest.
func TestCarriersOnlyKeepsSignalSpecAndChart(t *testing.T) {
	found := []Bead{
		{ID: "a", Kind: contract.KindSignal, Status: "closed"},
		{ID: "b", Kind: contract.KindSpec, Status: "deferred"},
		{ID: "c", Kind: contract.KindChart},
		{ID: "d", Kind: contract.KindCritique},
		{ID: "e", Kind: contract.KindPivot},
		{ID: "f", Kind: contract.KindBuild},
	}
	var ids []string
	for _, b := range carriersOnly(found) {
		ids = append(ids, b.ID)
	}
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("carriers = %v, want %v", ids, want)
	}
}
