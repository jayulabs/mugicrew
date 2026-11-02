package bd

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/contract"
)

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

// loopCore serves the reads the loop spawn helpers make: beads by id and the Signals
// already discovered-from the cause.
type loopCore struct {
	Core
	beads map[string]Bead
	found []Bead
}

func (c loopCore) Show(id string) (Bead, error) {
	b, ok := c.beads[id]
	if !ok {
		return Bead{}, fmt.Errorf("not found: %s", id)
	}
	return b, nil
}

func (c loopCore) Edges(string, Dir, string) ([]EdgeRow, error) {
	rows := make([]EdgeRow, len(c.found))
	for i, b := range c.found {
		rows[i] = EdgeRow{Bead: b, Type: "discovered-from"}
	}
	return rows, nil
}

func useLoopCore(t *testing.T, c loopCore) *[][]string {
	t.Helper()
	ocore, orun := core, run
	t.Cleanup(func() { core, run = ocore, orun })
	core = c
	var creates [][]string
	run = func(args ...string) (string, error) { creates = append(creates, args); return "new", nil }
	return &creates
}

// A loop offshoot is born in the cause's open Initiative, or unparented with the verb's
// outside line; always open, discovered-from only, Order: none above the Initiative: line.
func TestLoopOffshootPlacementArgv(t *testing.T) {
	beads := map[string]Bead{
		"cp":     {ID: "cp", Kind: contract.KindCheckpoint, Title: "CP", Parent: "init"},
		"cpold":  {ID: "cpold", Kind: contract.KindCheckpoint, Title: "Old", Parent: "done"},
		"cpbare": {ID: "cpbare", Kind: contract.KindCheckpoint, Title: "Bare"},
		"init":   {ID: "init", Kind: contract.KindInitiative, Title: "Init", Status: "open"},
		"done":   {ID: "done", Kind: contract.KindInitiative, Title: "Done", Status: "closed"},
		"b1":     {ID: "b1", Kind: contract.KindBuild, Title: "B", Parent: "cp"},
		"b2":     {ID: "b2", Kind: contract.KindBuild, Title: "B", Parent: "cpold"},
		"b3":     {ID: "b3", Kind: contract.KindBuild, Title: "B", Parent: "cpbare"},
		"b4":     {ID: "b4", Kind: contract.KindBuild, Title: "B4"},
	}
	for _, tc := range []struct{ cause, parent, line string }{
		{"b1", "init", "Initiative: Init"},
		{"b2", "", "Initiative: outside (Done is closed)"},
		{"b3", "", "Initiative: outside (Bare has no Initiative)"},
		{"b4", "", "Initiative: outside (B4 has no Initiative)"},
	} {
		for name, spawn := range map[string]func(string, string, string) (string, bool, error){
			"any":    SpawnOffshootSignalIfAbsent,
			"titled": SpawnOffshootSignalTitledIfAbsent,
		} {
			t.Run(tc.cause+"/"+name, func(t *testing.T) {
				creates := useLoopCore(t, loopCore{beads: beads})
				id, created, err := spawn(tc.cause, "Lost", "Body")
				if err != nil || !created || id != "new" || len(*creates) != 1 {
					t.Fatalf("spawn = %q, %v, %v with %d creates", id, created, err, len(*creates))
				}
				argv := (*creates)[0]
				if p := argAfter(argv, "--parent"); p != tc.parent {
					t.Errorf("--parent = %q, want %q", p, tc.parent)
				}
				if d := argAfter(argv, "--deps"); d != "discovered-from:"+tc.cause {
					t.Errorf("--deps = %q, want only discovered-from", d)
				}
				for _, bad := range []string{"--status", "drafted-by"} {
					if strings.Contains(strings.Join(argv, " "), bad) {
						t.Errorf("argv %v carries %s", argv, bad)
					}
				}
				if d := argAfter(argv, "--description"); d != "Body\n\nOrder: none\n"+tc.line {
					t.Errorf("body = %q", d)
				}
			})
		}
	}
}

// Whatever an existing Signal's status or parent, it stands in for the create; the titled
// helper accepts only a same-titled one.
func TestLoopOffshootIdempotency(t *testing.T) {
	beads := map[string]Bead{"c": {ID: "c", Kind: contract.KindBuild, Title: "C"}}
	existing := []Bead{{ID: "old", Kind: contract.KindSignal, Title: "Lost", Status: "closed", Parent: "init"}}

	creates := useLoopCore(t, loopCore{beads: beads, found: existing})
	if id, created, err := SpawnOffshootSignalIfAbsent("c", "Other", "b"); err != nil || created || id != "old" || len(*creates) != 0 {
		t.Errorf("any helper = %q, %v, %v, %d creates; want old, no create", id, created, err, len(*creates))
	}
	if id, created, err := SpawnOffshootSignalTitledIfAbsent("c", "Lost", "b"); err != nil || created || id != "old" || len(*creates) != 0 {
		t.Errorf("titled same title = %q, %v, %v, %d creates; want old, no create", id, created, err, len(*creates))
	}
	if _, created, err := SpawnOffshootSignalTitledIfAbsent("c", "Other", "b"); err != nil || !created || len(*creates) != 1 {
		t.Errorf("titled other title: created=%v err=%v creates=%d; want one create", created, err, len(*creates))
	}
}

// retireCore is a Core stub for Retire: three overridden methods, the rest nil.
type retireCore struct {
	Core
	beads  map[string]Bead
	edges  []Bead // old's supersedes targets
	closes [][2]string
	forced bool
	ran    [][]string // the supersedes written
}

func (c *retireCore) Supersede(id, with string) error {
	c.ran = append(c.ran, []string{"supersede", id, with})
	return nil
}

func (c *retireCore) Show(id string) (Bead, error) {
	b, ok := c.beads[id]
	if !ok {
		return Bead{}, errors.New("not found: " + id)
	}
	return b, nil
}

func (c *retireCore) Close(ids []string, reason string, force bool) error {
	c.closes, c.forced = append(c.closes, [2]string{ids[0], reason}), force
	return nil
}

func (c *retireCore) Edges(string, Dir, string) ([]EdgeRow, error) {
	rows := make([]EdgeRow, len(c.edges))
	for i, b := range c.edges {
		rows[i] = EdgeRow{Bead: b, Type: "supersedes"}
	}
	return rows, nil
}

func newRetire(t *testing.T, edges []Bead, beads ...Bead) (*retireCore, *[][]string) {
	t.Helper()
	c := &retireCore{beads: map[string]Bead{}, edges: edges}
	for _, b := range beads {
		c.beads[b.ID] = b
	}
	t.Cleanup(Use(c))
	return c, &c.ran
}

var supersedeOldNew = []string{"supersede", "mc-old", "mc-new"}

func TestRetireClosesAnOpenBeadWithTheReasonThenAddsTheEdge(t *testing.T) {
	c, ran := newRetire(t, nil, Bead{ID: "mc-old", Status: "open"}, Bead{ID: "mc-new", Status: "open"})
	if err := Retire("mc-old", "mc-new", "duplicate: mc-new"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.closes, [][2]string{{"mc-old", "duplicate: mc-new"}}) || !c.forced {
		t.Errorf("closes = %v forced=%v, want one forced close with the reason", c.closes, c.forced)
	}
	if !reflect.DeepEqual(*ran, [][]string{supersedeOldNew}) {
		t.Errorf("ran = %v, want the supersede", *ran)
	}
}

func TestRetireOfAClosedBeadAddsOnlyTheEdge(t *testing.T) {
	c, ran := newRetire(t, nil, Bead{ID: "mc-old", Status: "closed", CloseReason: "kept"}, Bead{ID: "mc-new", Status: "open"})
	if err := Retire("mc-old", "mc-new", "duplicate: mc-new"); err != nil {
		t.Fatal(err)
	}
	if len(c.closes) != 0 {
		t.Errorf("closes = %v, want none", c.closes)
	}
	if !reflect.DeepEqual(*ran, [][]string{supersedeOldNew}) {
		t.Errorf("ran = %v, want the supersede", *ran)
	}
}

func TestRetireWithTheEdgeAlreadyThereIsANoOp(t *testing.T) {
	c, ran := newRetire(t, []Bead{{ID: "mc-new"}}, Bead{ID: "mc-old", Status: "closed"}, Bead{ID: "mc-new", Status: "open"})
	if err := Retire("mc-old", "mc-new", "duplicate: mc-new"); err != nil {
		t.Fatal(err)
	}
	if len(c.closes) != 0 || len(*ran) != 0 {
		t.Errorf("closes = %v ran = %v, want nothing written", c.closes, *ran)
	}
}

func TestRetireRefusesAnUnknownNewIDBeforeAnyWrite(t *testing.T) {
	c, ran := newRetire(t, nil, Bead{ID: "mc-old", Status: "open"})
	if err := Retire("mc-old", "mc-typo", "duplicate: mc-typo"); err == nil {
		t.Fatal("Retire of an unknown new id succeeded")
	}
	if len(c.closes) != 0 || len(*ran) != 0 {
		t.Errorf("closes = %v ran = %v, want nothing written", c.closes, *ran)
	}
}
