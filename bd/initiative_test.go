package bd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/git"
)

// node is one bead of a fake graph: id, kind, status, parent, and the ids it is
// blocked by (Ordering edges).
type node struct {
	id, kind, status, parent string
	blockedBy                []string
	causedBy                 string
	draft                    bool // an unpublished Draft: deferred with a drafted-by: label
}

// fakeSnapshot stands the snapshot behind run's `bd list --all` and records every
// other bd call, so a test asserts what was written (nothing, on a refusal).
func fakeSnapshot(t *testing.T, nodes ...node) *[][]string {
	t.Helper()
	var rows []map[string]any
	for _, n := range nodes {
		var deps []map[string]string
		for _, b := range n.blockedBy {
			deps = append(deps, map[string]string{"depends_on_id": b, "type": "blocks"})
		}
		if n.causedBy != "" {
			deps = append(deps, map[string]string{"depends_on_id": n.causedBy, "type": "caused-by"})
		}
		labels := []string{"kind:" + n.kind}
		if n.draft {
			labels = append(labels, "drafted-by:someone")
		}
		typ := "task"
		if n.kind == "initiative" || n.kind == "chart" || n.kind == "checkpoint" {
			typ = "epic"
		}
		rows = append(rows, map[string]any{"id": n.id, "title": "t " + n.id, "status": n.status, "labels": labels,
			"parent": n.parent, "issue_type": typ, "dependencies": deps})
	}
	snap, _ := json.Marshal(rows)
	var writes [][]string
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(args ...string) (string, error) {
		if args[0] == "list" {
			return string(snap), nil
		}
		writes = append(writes, args)
		return "", nil
	}
	return &writes
}

func heads(t *testing.T, nodes []node) (all []Bead, isHead func(string) bool) {
	t.Helper()
	fakeSnapshot(t, nodes...)
	all, err := ListAll()
	if err != nil {
		t.Fatal(err)
	}
	return all, func(id string) bool {
		i := slices.IndexFunc(all, func(b Bead) bool { return b.ID == id })
		return IsChainHead(all[i], all)
	}
}

func refusedNames(t *testing.T, err error) []string {
	t.Helper()
	var r *RefusedError
	if !errors.As(err, &r) {
		t.Fatalf("want a refusal, got %v", err)
	}
	return IDs(r.Blocking)
}

// An Initiative with two member chains: a landed phase 1 (closed Checkpoint, its
// closed Build) and a live phase 2 (open Checkpoint with two Builds, b2 ordered
// after b1), plus a phase-3 Signal Draft, and an unrelated bead outside.
func initiativeGraph() []node {
	return []node{
		{id: "in", kind: "initiative", status: "open"},
		{id: "cp1", kind: "checkpoint", status: "closed", parent: "in"},
		{id: "b0", kind: "build", status: "closed", parent: "cp1"},
		{id: "cp2", kind: "checkpoint", status: "open", parent: "in"},
		{id: "b1", kind: "build", status: "open", parent: "cp2"},
		{id: "b2", kind: "build", status: "open", parent: "cp2", blockedBy: []string{"b1"}},
		{id: "s3", kind: "signal", status: "deferred", parent: "in"},
		{id: "x", kind: "signal", status: "open"},
	}
}

// The refusal names each blocking bead by id and title.
func TestRefusedErrorNamesBeads(t *testing.T) {
	err := &RefusedError{What: "cancel in", Why: "in-flight effort", Blocking: []Bead{{ID: "b2", Title: "Do it"}}}
	if !strings.Contains(err.Error(), "b2") || !strings.Contains(err.Error(), "Do it") {
		t.Errorf("Error() = %q", err.Error())
	}
}

// An Initiative's state is derived over a snapshot, first match wins (Spec D6,
// FR18): closed, then drained (no unclosed child, Drafts included), then under way.
func TestStateOfInitiative(t *testing.T) {
	ini := Bead{ID: "ini", Kind: "initiative", Status: "open"}
	kid := func(status string) Bead { return Bead{ID: "k", Parent: "ini", Status: status} }
	for _, c := range []struct {
		name string
		ini  Bead
		all  []Bead
		want InitiativeState
	}{
		{"closed", Bead{ID: "ini", Kind: "initiative", Status: "closed"}, nil, InitiativeClosed},
		{"no member", ini, []Bead{ini, {ID: "x", Status: "open"}}, InitiativeDrained},
		{"every member closed", ini, []Bead{ini, kid("closed")}, InitiativeDrained},
		{"draft member", ini, []Bead{ini, kid("deferred")}, InitiativeUnderWay},
		{"in-progress member", ini, []Bead{ini, kid("in_progress")}, InitiativeUnderWay},
		{"blocked member", ini, []Bead{ini, kid("blocked")}, InitiativeUnderWay},
	} {
		if got := StateOf(c.ini, c.all); got != c.want {
			t.Errorf("%s: StateOf = %q, want %q", c.name, got, c.want)
		}
	}
}

// adoptGraph adds a second open Initiative, a standalone open Chart, a closed
// Signal, and an Initiative-free Build inside cp2 to initiativeGraph.
func adoptGraph() []node {
	return append(initiativeGraph(),
		node{id: "in2", kind: "initiative", status: "open"},
		node{id: "old", kind: "initiative", status: "closed"},
		node{id: "ch", kind: "chart", status: "open"},
		node{id: "gone", kind: "signal", status: "closed"},
	)
}

// FR13–17, D5: adopt refuses a closed head, an Initiative head (no nesting), a
// target that is not an open Initiative, and a bead inside another container;
// each refusal names its reason and writes nothing.
func TestAdoptRefusals(t *testing.T) {
	for _, c := range []struct{ head, target, why, blocking string }{
		{"gone", "in", "closed", "gone"},
		{"in2", "in", "Initiatives never nest", "in2"},
		{"ch", "old", "not an open Initiative", "old"},
		{"ch", "cp2", "not an open Initiative", "cp2"},
		{"ch", "nope", "not an open Initiative", "nope"},
		{"b1", "in2", "not a chain head", "cp2"},
	} {
		writes := fakeSnapshot(t, adoptGraph()...)
		evs := eventlogtest.Capture(t)
		err := AdoptChain(c.head, c.target)
		var r *RefusedError
		if !errors.As(err, &r) || !strings.Contains(r.Why, c.why) || !slices.Equal(IDs(r.Blocking), []string{c.blocking}) {
			t.Errorf("adopt %s into %s: %v, want a refusal %q naming %s", c.head, c.target, err, c.why, c.blocking)
		}
		if len(*writes) != 0 {
			t.Errorf("adopt %s into %s: a refusal wrote %v", c.head, c.target, *writes)
		}
		if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"initiative.adopt.refused"}) {
			t.Errorf("events = %v", got)
		}
	}
}

// FR14–15: adopt re-parents only the head, one bd update --parent; a move from
// another Initiative is the same single call.
func TestAdoptReparentsHead(t *testing.T) {
	for _, c := range []struct{ head, target string }{{"ch", "in"}, {"cp2", "in2"}} {
		writes := fakeSnapshot(t, adoptGraph()...)
		evs := eventlogtest.Capture(t)
		if err := AdoptChain(c.head, c.target); err != nil {
			t.Fatal(err)
		}
		want := [][]string{{"update", c.head, "--parent", c.target}}
		if fmt.Sprint(*writes) != fmt.Sprint(want) {
			t.Errorf("writes = %v, want %v", *writes, want)
		}
		if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"initiative.adopt"}) {
			t.Errorf("events = %v", got)
		}
	}
}

// FR16: release clears a member head's parent; a closed head or a non-member is
// refused with nothing written.
func TestReleaseChain(t *testing.T) {
	writes := fakeSnapshot(t, adoptGraph()...)
	evs := eventlogtest.Capture(t)
	if err := ReleaseChain("cp2"); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"update", "cp2", "--parent", ""}}; fmt.Sprint(*writes) != fmt.Sprint(want) {
		t.Errorf("writes = %v, want %v", *writes, want)
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"initiative.release"}) {
		t.Errorf("events = %v", got)
	}
	for _, id := range []string{"cp1", "ch", "b1", "in"} {
		writes := fakeSnapshot(t, adoptGraph()...)
		if err := ReleaseChain(id); !errors.As(err, new(*RefusedError)) || len(*writes) != 0 {
			t.Errorf("release %s: %v, writes %v; want a refusal, nothing written", id, err, *writes)
		}
	}
}

// Under Require Initiatives, release refuses before any write and points at adopt.
func TestReleaseChainRefusedWhenRequired(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".mugicrew"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".mugicrew", "config.toml"), []byte("[initiatives]\nrequired = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(ws, "apps", "x")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub) // a subdirectory: the gate must resolve the Workspace root, not the cwd
	old := git.RepoRoot
	git.RepoRoot = func() (string, error) { return ws, nil }
	t.Cleanup(func() { git.RepoRoot = old })
	writes := fakeSnapshot(t, adoptGraph()...)
	err := ReleaseChain("cp2")
	var re *RefusedError
	if !errors.As(err, &re) || !strings.Contains(re.Why, "mugicrew initiative adopt cp2 <other>") || len(*writes) != 0 {
		t.Errorf("err %v, writes %v; want an adopt-pointing refusal, nothing written", err, *writes)
	}
}

func TestHasDoneWhen(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		want bool
	}{
		"heading present": {"## Done when\n\nShip it.", true},
		"heading absent":  {"Just a paragraph.", false},
		"empty body":      {"", false},
		"loose h3 match":  {"### Done when\n", true},
	} {
		t.Run(name, func(t *testing.T) {
			if got := HasDoneWhen(tc.body); got != tc.want {
				t.Errorf("HasDoneWhen(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

// Spec D1: every head kind is a chain head standalone and under an open and a
// closed Initiative; defog beads, Builds, Landings, Drafts, closed beads and beads
// with an unclosed successor are not.
func TestIsChainHead(t *testing.T) {
	g := []node{
		{id: "in", kind: "initiative", status: "open"},
		{id: "done", kind: "initiative", status: "closed"},
		{id: "ch", kind: "chart", status: "open"},
		{id: "sig", kind: "signal", status: "open"},
		{id: "spec", kind: "spec", status: "open", parent: "in"},
		{id: "cp", kind: "checkpoint", status: "blocked", parent: "done"},
		{id: "rb", kind: "rollback", status: "open"},
		{id: "survey", kind: "survey", status: "open", parent: "ch"},
		{id: "build", kind: "build", status: "open", parent: "cp"},
		{id: "land", kind: "landing", status: "open", parent: "cp"},
		{id: "drspec", kind: "spec", status: "deferred", parent: "ch", draft: true},
		{id: "drsig", kind: "signal", status: "deferred", draft: true},
		{id: "gone", kind: "signal", status: "closed"},
		{id: "task", kind: "task", status: "open"},
		{id: "pred", kind: "spec", status: "open"},
		{id: "succ", kind: "checkpoint", status: "open", causedBy: "pred"},
		{id: "pred2", kind: "spec", status: "open"},
		{id: "succ2", kind: "checkpoint", status: "closed", causedBy: "pred2"},
	}
	_, isHead := heads(t, g)
	for _, id := range []string{"ch", "sig", "spec", "cp", "rb", "succ", "pred2"} {
		if !isHead(id) {
			t.Errorf("%s: want a chain head", id)
		}
	}
	for _, id := range []string{"in", "survey", "build", "land", "drspec", "drsig", "gone", "task", "pred"} {
		if isHead(id) {
			t.Errorf("%s: want not a chain head", id)
		}
	}
}

// Withdrawing a Checkpoint takes its still-open Spec and both subtrees, leaves
// first, head last; a closed predecessor's closed subtree stays out.
func TestWithdrawSet(t *testing.T) {
	g := []node{
		{id: "spec", kind: "spec", status: "open"},
		{id: "sd", kind: "signal", status: "deferred", parent: "spec", draft: true},
		{id: "cp", kind: "checkpoint", status: "open", causedBy: "spec"},
		{id: "b1", kind: "build", status: "open", parent: "cp"},
		{id: "b0", kind: "build", status: "closed", parent: "cp"},
	}
	all, _ := heads(t, g)
	if got := IDs(WithdrawSet("cp", all)); !slices.Equal(got, []string{"sd", "spec", "b1", "cp"}) {
		t.Errorf("WithdrawSet = %v", got)
	}
}

// Cancel's set is every unclosed member (a Draft member too) then the Initiative.
func TestCancelSet(t *testing.T) {
	all, _ := heads(t, initiativeGraph())
	if got := IDs(CancelSet("in", all)); !slices.Equal(got, []string{"b1", "b2", "cp2", "s3", "in"}) {
		t.Errorf("CancelSet = %v", got)
	}
}

// OrderedAfter scans every unclosed bead outside the set, wherever it sits.
func TestOrderedAfter(t *testing.T) {
	g := append(initiativeGraph(),
		node{id: "s4", kind: "signal", status: "open", parent: "in", blockedBy: []string{"b1"}},
		node{id: "s5", kind: "signal", status: "closed", parent: "in", blockedBy: []string{"cp2"}},
		node{id: "y", kind: "signal", status: "open", blockedBy: []string{"cp2"}},
		node{id: "z", kind: "signal", status: "open", blockedBy: []string{"x"}},
	)
	all, _ := heads(t, g)
	if got := IDs(OrderedAfter(WithdrawSet("cp2", all), all)); !slices.Equal(got, []string{"s4", "y"}) {
		t.Errorf("OrderedAfter = %v, want [s4 y] (b2 is inside, s5 is closed, z waits on x)", got)
	}
}

// FR40: a Task filed as a direct Initiative child is a member like any other: an open
// one and a Draft one each hold the Initiative under way, and it drains only when both
// are closed.
func TestStateOfInitiativeCountsMemberTasks(t *testing.T) {
	ini := Bead{ID: "ini", Kind: "initiative", Status: "open"}
	task := func(id, status string) Bead { return Bead{ID: id, Kind: "task", Parent: "ini", Status: status} }
	for _, c := range []struct {
		name string
		all  []Bead
		want InitiativeState
	}{
		{"open member Task", []Bead{ini, task("t", "open")}, InitiativeUnderWay},
		{"member Task Draft", []Bead{ini, task("t", "deferred")}, InitiativeUnderWay},
		{"every member Task closed", []Bead{ini, task("t", "closed"), task("d", "closed")}, InitiativeDrained},
	} {
		if got := StateOf(ini, c.all); got != c.want {
			t.Errorf("%s: StateOf = %q, want %q", c.name, got, c.want)
		}
	}
}

// ADR 0349: under Require Initiatives a member Task releases while a Checkpoint refuses.
func TestReleaseChainTaskExemptFromRequired(t *testing.T) {
	old := git.RepoRoot
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".mugicrew"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".mugicrew", "config.toml"), []byte("[initiatives]\nrequired = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git.RepoRoot = func() (string, error) { return ws, nil }
	t.Cleanup(func() { git.RepoRoot = old })
	g := append(adoptGraph(), node{id: "tk", kind: "task", status: "open", parent: "in"})
	writes := fakeSnapshot(t, g...)
	if err := ReleaseChain("tk"); err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"update", "tk", "--parent", ""}}; fmt.Sprint(*writes) != fmt.Sprint(want) {
		t.Errorf("writes = %v", *writes)
	}
	if err := ReleaseChain("cp2"); !errors.As(err, new(*RefusedError)) {
		t.Errorf("checkpoint release: %v", err)
	}
}
