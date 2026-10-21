package bd

import (
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// TestMain stubs the effort-ref read to none, so the drain checks stay hermetic
// outside a git checkout and blind to the host repo's effort/* branches; a test
// that wants refs sets its own.
func TestMain(m *testing.M) {
	effortRefs = func() (map[string]bool, error) { return nil, nil }
	os.Exit(m.Run())
}

// countOpenKind is the open-tail-of-kind filter: open AND kind==k.
// A closed build, and an open bead of another kind, must both be excluded.
func TestCountOpenKind(t *testing.T) {
	kids := []Bead{
		{Kind: contract.KindBuild, Status: "open"},
		{Kind: contract.KindBuild, Status: "in_progress"},
		{Kind: contract.KindBuild, Status: "closed"}, // closed → excluded
		{Kind: contract.KindReview, Status: "open"},  // other kind → excluded
	}
	if got := countOpenKind(kids, contract.KindBuild); got != 2 {
		t.Errorf("countOpenKind(build) = %d, want 2 (open + in_progress)", got)
	}
	if got := countOpenKind(kids, contract.KindReview); got != 1 {
		t.Errorf("countOpenKind(review) = %d, want 1", got)
	}
	if got := countOpenKind(nil, contract.KindBuild); got != 0 {
		t.Errorf("countOpenKind(nil) = %d, want 0", got)
	}
}

// closedChildrenOfKind is Jinbe's closed-Approval read: ALL CLOSED
// children of the wanted kind, with their bodies. An open Approval (not yet approved) and a
// closed bead of another kind are both excluded. Returning ALL (not the first) matters:
// a stale-Approval self-heal leaves >=2 closed Approvals and the gate must
// see the at-head one even when the stale obsoleted one comes first in creation order.
func TestClosedChildrenOfKind(t *testing.T) {
	kids := []Bead{
		{ID: "a", Kind: contract.KindApproval, Status: "open"},                                  // open → excluded (not approved)
		{ID: "b", Kind: contract.KindReview, Status: "closed"},                                  // other kind → excluded
		{ID: "stale", Kind: contract.KindApproval, Status: "closed", Description: "Commit: p1"}, // obsoleted, pin behind HEAD
		{ID: "head", Kind: contract.KindApproval, Status: "closed", Description: "Commit: p2"},  // the at-head one, created later
	}
	got := closedChildrenOfKind(kids, contract.KindApproval)
	if len(got) != 2 || got[0].ID != "stale" || got[1].ID != "head" {
		t.Errorf("closedChildrenOfKind(approval) = %+v, want [stale, head] in creation order", got)
	}
	if got := closedChildrenOfKind(kids, contract.KindConflict); got != nil {
		t.Errorf("no closed child of a kind must be nil, got %+v", got)
	}
	if got := closedChildrenOfKind(nil, contract.KindApproval); got != nil {
		t.Errorf("closedChildrenOfKind(nil) must be nil, got %+v", got)
	}
}

// The paused seam (ADR 0070 §3): pause adds its own label, unpause removes only
// its own, and the causes read back off the bead's labels, manual first.
func TestPausedState(t *testing.T) {
	var argv []string
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(args ...string) (string, error) { argv = args; return "", nil }

	if err := Pause("mc-1", PausedManual); err != nil {
		t.Fatal(err)
	}
	if want := []string{"update", "mc-1", "--add-label", "paused:manual"}; !reflect.DeepEqual(argv, want) {
		t.Errorf("pause argv = %v, want %v", argv, want)
	}
	if err := Unpause("mc-1", PausedForeignHold); err != nil {
		t.Fatal(err)
	}
	if want := []string{"label", "remove", "mc-1", "paused:foreign-hold"}; !reflect.DeepEqual(argv, want) {
		t.Errorf("unpause argv = %v, want %v", argv, want)
	}

	both := []string{"paused:foreign-hold", "kind:build", "paused:manual"}
	for _, c := range []struct {
		labels []string
		want   []string
	}{
		{nil, nil},
		{[]string{"kind:build"}, nil},
		{[]string{"kind:build", "paused:manual"}, []string{PausedManual}},
		{[]string{"paused:foreign-hold"}, []string{PausedForeignHold}},
		{both, []string{PausedManual, PausedForeignHold}},
	} {
		b := Bead{Labels: c.labels}
		if got := PausedCauses(b); !reflect.DeepEqual(got, c.want) || Paused(b) != (c.want != nil) {
			t.Errorf("PausedCauses(%v) = %q, Paused %v; want %q", c.labels, got, Paused(b), c.want)
		}
	}
	// Lifting foreign-hold off a doubly paused bead leaves it manually paused.
	b := Bead{Labels: slices.DeleteFunc(slices.Clone(both), func(l string) bool { return l == "paused:foreign-hold" })}
	if !Paused(b) || !PausedBy(b, PausedManual) {
		t.Errorf("after lifting foreign-hold: %v, want still manually paused", b.Labels)
	}
}

// PausedAncestor (ADR 0081): the nearest manual-paused ancestor over Parent, at
// any depth; other causes and caused-by lineage never cascade; a cycle ends.
func TestPausedAncestor(t *testing.T) {
	manual, failing := []string{"paused:manual"}, []string{"paused:failing", "paused:foreign-hold", "paused:abandoned"}
	for _, c := range []struct {
		name, parent string
		beads        []Bead
		want         string
	}{
		{"none", "p", []Bead{{ID: "p"}}, ""},
		{"no parent", "", []Bead{{ID: "p", Labels: manual}}, ""},
		{"direct parent", "p", []Bead{{ID: "p", Labels: manual}}, "p"},
		{"grandparent", "p", []Bead{{ID: "p", Parent: "g"}, {ID: "g", Labels: manual}}, "g"},
		{"nearest of two", "p", []Bead{{ID: "p", Parent: "g", Labels: manual}, {ID: "g", Labels: manual}}, "p"},
		{"other causes stay local", "p", []Bead{{ID: "p", Labels: failing}}, ""},
		// A caused-by predecessor is no Parent: Bead carries no lineage pointer for
		// the walk to follow, so a manual predecessor in the lookup never matches.
		{"caused-by predecessor", "", []Bead{{ID: "pred", Labels: manual}}, ""},
		// Spec FR29–30: membership carried at spawn puts a whole chain under its
		// Initiative, so a manual Initiative brakes the deepest member; a member's
		// own failing pause stays local.
		{"Initiative → Chart → defog", "ch", []Bead{{ID: "ch", Kind: contract.KindChart, Parent: "in"}, {ID: "in", Kind: contract.KindInitiative, Labels: manual}}, "in"},
		{"Initiative → Checkpoint → Build", "cp", []Bead{{ID: "cp", Kind: contract.KindCheckpoint, Parent: "in"}, {ID: "in", Kind: contract.KindInitiative, Labels: manual}}, "in"},
		{"member failing stays local", "cp", []Bead{{ID: "cp", Kind: contract.KindCheckpoint, Parent: "in", Labels: failing}, {ID: "in", Kind: contract.KindInitiative}}, ""},
		{"cycle", "p", []Bead{{ID: "p", Parent: "g"}, {ID: "g", Parent: "p"}}, ""},
		{"cycle back to self", "p", []Bead{{ID: "p", Parent: "b"}}, ""},
	} {
		byID := map[string]Bead{}
		for _, x := range c.beads {
			byID[x.ID] = x
		}
		b := Bead{ID: "b", Parent: c.parent}
		got, err := PausedAncestor(b, func(id string) (Bead, error) { return byID[id], nil })
		if err != nil || got != c.want {
			t.Errorf("%s: PausedAncestor = %q, %v; want %q", c.name, got, err, c.want)
		}
	}
	boom := errors.New("bd show: boom")
	if _, err := PausedAncestor(Bead{ID: "b", Parent: "p"}, func(string) (Bead, error) { return Bead{}, boom }); err != boom {
		t.Errorf("a failed lookup must surface, got %v", err)
	}
}

// PausedList reads every paused bead, closed included, in one uncapped list.
func TestPausedListArgv(t *testing.T) {
	var argv []string
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(args ...string) (string, error) {
		argv = args
		return `[{"id":"mc-1","parent":"mc-0","labels":["paused:manual"]}]`, nil
	}
	got, err := PausedList()
	if err != nil || len(got) != 1 || got[0].Parent != "mc-0" {
		t.Fatalf("PausedList = %+v, %v", got, err)
	}
	if want := []string{"list", "--all", "--json", "-n", "0", "--label-pattern", "paused:*"}; !reflect.DeepEqual(argv, want) {
		t.Errorf("argv = %v, want %v", argv, want)
	}
}

// PausedCauses ranks manual > failing > the rest (ADR 0078), whatever the label order.
func TestPausedCausesRanking(t *testing.T) {
	m, f, h := "paused:manual", "paused:failing", "paused:foreign-hold"
	want := []string{PausedManual, PausedFailing, PausedForeignHold}
	for _, labels := range [][]string{{m, f, h}, {m, h, f}, {f, m, h}, {f, h, m}, {h, m, f}, {h, f, m}} {
		if got := PausedCauses(Bead{Labels: labels}); !reflect.DeepEqual(got, want) {
			t.Errorf("PausedCauses(%v) = %q, want %q", labels, got, want)
		}
	}
	for _, c := range []struct{ labels, want []string }{
		{[]string{f, h}, []string{PausedFailing, PausedForeignHold}},
		{[]string{h, "kind:build", f}, []string{PausedFailing, PausedForeignHold}},
		// Unranked causes keep their label order behind the ranked ones.
		{[]string{"paused:z", h, f, "paused:a"}, []string{PausedFailing, "z", PausedForeignHold, "a"}},
	} {
		if got := PausedCauses(Bead{Labels: c.labels}); !reflect.DeepEqual(got, c.want) {
			t.Errorf("PausedCauses(%v) = %q, want %q", c.labels, got, c.want)
		}
	}
	// Every subset of these causes, in every label order, ranks manual >
	// abandoned > failing > profile > foreign-hold (ADR 0082, ADR 0123 §3).
	ranked := []string{PausedManual, PausedAbandoned, PausedFailing, PausedProfile, PausedForeignHold}
	var perms func(pre, rest []string)
	perms = func(pre, rest []string) {
		if len(rest) == 0 {
			var labels []string
			for _, c := range pre {
				labels = append(labels, "paused:"+c)
			}
			want := slices.DeleteFunc(slices.Clone(ranked), func(c string) bool { return !slices.Contains(pre, c) })
			if len(want) == 0 {
				want = nil
			}
			if got := PausedCauses(Bead{Labels: labels}); !reflect.DeepEqual(got, want) {
				t.Errorf("PausedCauses(%v) = %q, want %q", labels, got, want)
			}
			return
		}
		for i := range rest {
			perms(append(slices.Clone(pre), rest[i]), slices.Delete(slices.Clone(rest), i, i+1))
		}
	}
	for mask := range 1 << len(ranked) {
		var subset []string
		for i, c := range ranked {
			if mask&(1<<i) != 0 {
				subset = append(subset, c)
			}
		}
		perms(nil, subset)
	}
	if b := (Bead{Labels: []string{"kind:build", f}}); !Paused(b) || !PausedBy(b, PausedFailing) {
		t.Errorf("only paused:failing: Paused %v, PausedBy(failing) %v; want both true", Paused(b), PausedBy(b, PausedFailing))
	}
	if PausedBy(Bead{Labels: []string{h}}, PausedFailing) {
		t.Error("only paused:foreign-hold: PausedBy(failing) = true, want false")
	}
}

// bd writes closed event children (state-change history); drain checks count
// open children only, so a container whose only extra children are event beads
// still reads as drained (ADR 0055 Consequences). The blob is bd's real shape.
func TestEventChildrenDoNotUndrain(t *testing.T) {
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(...string) (string, error) {
		return `[{"id":"mc-1.1","title":"State change: paused → manual","status":"closed","issue_type":"event","parent":"mc-1"},
		         {"id":"mc-1.2","title":"done","status":"closed","labels":["kind:build"],"parent":"mc-1"}]`, nil
	}
	if n, err := OpenChildren("mc-1", nil); err != nil || n != 0 {
		t.Errorf("OpenChildren = %d, %v; want 0 (event child is closed)", n, err)
	}
	for _, k := range []contract.Kind{contract.KindConflict, contract.KindCritique, contract.KindBuild} {
		if n, err := OpenChildrenOfKind("mc-1", k); err != nil || n != 0 {
			t.Errorf("OpenChildrenOfKind(%s) = %d, %v; want 0", k, n, err)
		}
	}
}

// A closed child still holding effort/<child> keeps its container undrained
// (Spec D7, FR 27): it counts in OpenChildren, and DrainBlockers names it
// so a press-time refusal can say which bead holds unintegrated work. A closed
// child without a ref still drains.
func TestClosedChildWithEffortRefUndrains(t *testing.T) {
	orun, orefs := run, effortRefs
	t.Cleanup(func() { run, effortRefs = orun, orefs })
	run = func(...string) (string, error) {
		return `[{"id":"mc-1.1","title":"landed","status":"closed","labels":["kind:research"],"parent":"mc-1"},
		         {"id":"mc-1.2","title":"unintegrated","status":"closed","labels":["kind:research"],"parent":"mc-1"}]`, nil
	}
	effortRefs = func() (map[string]bool, error) { return map[string]bool{"mc-1.2": true, "mc-9": true}, nil }
	if n, err := OpenChildren("mc-1", nil); err != nil || n != 1 {
		t.Errorf("OpenChildren = %d, %v; want 1 (mc-1.2 holds effort/mc-1.2)", n, err)
	}
	open, held, err := DrainBlockers("mc-1", nil)
	if err != nil || open != 0 || !slices.Equal(IDs(held), []string{"mc-1.2"}) {
		t.Errorf("DrainBlockers = %d, %v, %v; want 0 open, held [mc-1.2]", open, IDs(held), err)
	}
	hint := HeldHint(held)
	for _, want := range []string{"mc-1.2", "mugicrew integrate", "git branch -D effort/mc-1.2"} {
		if !strings.Contains(hint, want) {
			t.Errorf("HeldHint = %q, want it to name %q", hint, want)
		}
	}
}

// The §3.1 footgun: every runtime create MUST carry --no-inherit-labels and the
// kind:signal routing label, and a Signal is a chain head with no --parent.
// These are the ticket's acceptance criteria, checked without a DB.
func TestNewSignalArgsEncapsulateFootgun(t *testing.T) {
	args := newSignalArgs("a title", "a body")

	has := func(flag string) bool {
		for _, a := range args {
			if a == flag {
				return true
			}
		}
		return false
	}

	if !has("--no-inherit-labels") {
		t.Error("signal create must pass --no-inherit-labels (§3.1 footgun)")
	}
	if !has("--labels") || !has("kind:signal") {
		t.Error("signal create must label the bead kind:signal")
	}
	if has("--parent") {
		t.Error("a Signal is a chain head — it must have no --parent")
	}
	if !has("a title") || !has("a body") {
		t.Error("title and description must reach the create")
	}
	if got := titleArg(newSignalArgs("Signal: a title", "b")); got != "a title" {
		t.Errorf("prefixed signal --title = %q, want stripped", got)
	}
}

// titleArg is the argv element after --title (Spec D3).
func titleArg(args []string) string {
	if i := slices.Index(args, "--title"); i >= 0 && i+1 < len(args) {
		return args[i+1]
	}
	return ""
}

// bd.Update strips both --title forms and passes every other arg through
// untouched (Spec D4).
func TestUpdateStripsTitle(t *testing.T) {
	var got []string
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(args ...string) (string, error) { got = args; return "", nil }

	if err := Update("mc-1", []string{"--title", "Spec: x", "--priority", "1", "--title=Spec: x", "--notes", "Spec: keep"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"update", "mc-1", "--title", "x", "--priority", "1", "--title=x", "--notes", "Spec: keep"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("argv = %v\nwant %v", got, want)
	}
	if err := Update("mc-1", []string{"--title"}); err != nil || !reflect.DeepEqual(got, []string{"update", "mc-1", "--title"}) {
		t.Errorf("dangling --title: argv = %v, %v", got, err)
	}
}

// NewRollback (ADR 0047 §3): an unparented kind:rollback create, then one related
// edge from the new bead to the target Checkpoint.
func TestNewRollbackArgs(t *testing.T) {
	var calls [][]string
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(args ...string) (string, error) {
		calls = append(calls, args)
		return "ws-rb", nil
	}

	id, err := NewRollback("Rollback: Roll back X", "broke prod", Bead{ID: "ws-cp"})
	if err != nil || id != "ws-rb" {
		t.Fatalf("NewRollback = %q, %v", id, err)
	}
	want := [][]string{
		{"create", "--type", "task", "--labels", "kind:rollback", "--no-inherit-labels",
			"--title", "Roll back X", "--description", "broke prod", "--silent"},
		{"dep", "add", "ws-rb", "ws-cp", "-t", "related"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("argv = %v\nwant %v", calls, want)
	}
}

// A nested target's context: and repo: labels ride onto the Rollback so the tick
// scan can hold it on drift.
func TestNewRollbackCopiesNestedLabels(t *testing.T) {
	var created []string
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(args ...string) (string, error) {
		if args[0] == "create" {
			created = args
		}
		return "ws-rb", nil
	}
	target := Bead{ID: "ws-cp", Labels: []string{"kind:checkpoint", "context:vendor", "repo:vendor/x"}}
	if _, err := NewRollback("Roll back X", "broke", target); err != nil {
		t.Fatal(err)
	}
	if got := created[slices.Index(created, "--labels")+1]; got != "kind:rollback,context:vendor,repo:vendor/x" {
		t.Errorf("labels = %q", got)
	}
}

// surgeryHarness fakes NewSurgery's bd and git legs: each create returns the next
// id, failAt names the first argv word (or "cut") that errors.
func surgeryHarness(t *testing.T, failAt string) (calls *[][]string, cuts *[][2]string) {
	t.Helper()
	orun, ocut, ohead, oactor := run, cutBranch, git.Head, actor
	t.Cleanup(func() { run, cutBranch, git.Head, actor = orun, ocut, ohead, oactor })
	actor = func() string { return "alice" }
	git.Head = func(string) (string, error) { return "abc123", nil }
	calls, cuts = &[][]string{}, &[][2]string{}
	ids := []string{"ws-cp", "ws-sg"}
	run = func(args ...string) (string, error) {
		*calls = append(*calls, args)
		if args[0] == failAt {
			return "", errors.New("boom")
		}
		if args[0] == "create" {
			id := ids[0]
			ids = ids[1:]
			return id, nil
		}
		return "[]", nil
	}
	cutBranch = func(name, base string) (bool, error) {
		*cuts = append(*cuts, [2]string{name, base})
		if failAt == "cut" {
			return false, errors.New("boom")
		}
		return true, nil
	}
	return calls, cuts
}

// NewSurgery (Spec D2): an unparented Checkpoint with no caused-by edge, its
// branch cut at main HEAD's SHA, a kind:surgery child carrying the problem, claimed.
func TestNewSurgery(t *testing.T) {
	calls, cuts := surgeryHarness(t, "")
	sg, cp, err := NewSurgery("Surgery: Tweak colour", "rows too dim", "", false)
	if err != nil || sg != "ws-sg" || cp != "ws-cp" {
		t.Fatalf("NewSurgery = %q, %q, %v", sg, cp, err)
	}
	want := [][]string{
		{"create", "--type", "epic", "--labels", "kind:checkpoint", "--no-inherit-labels",
			"--title", "Tweak colour", "--description", "", "--silent"},
		{"create", "--type", "task", "--labels", "kind:surgery", "--no-inherit-labels",
			"--parent", "ws-cp", "--title", "Tweak colour", "--description", "rows too dim", "--silent"},
		{"update", "ws-sg", "--if-status", "open", "--status", "in_progress", "--assignee", "alice"},
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Errorf("argv = %v\nwant %v", *calls, want)
	}
	if wantCut := [][2]string{{"checkpoint/ws-cp", "abc123"}}; !reflect.DeepEqual(*cuts, wantCut) {
		t.Errorf("cuts = %v, want %v", *cuts, wantCut)
	}
	// No caused-by edge, so the surgery Checkpoint serves no Spec.
	if spec, ok, err := SpecOf(cp); err != nil || ok {
		t.Errorf("SpecOf(%s) = %q, %v, %v; want not-found", cp, spec, ok, err)
	}
}

// A non-empty initiative files the Checkpoint with --parent; a closed or
// non-Initiative target, or an empty one under required, is refused with no create.
func TestNewSurgeryInitiative(t *testing.T) {
	calls, _ := surgeryHarness(t, "")
	orun := run
	run = func(args ...string) (string, error) {
		if args[0] == "list" {
			return `[{"id":"ini","status":"open","labels":["kind:initiative"]},{"id":"old","status":"closed","labels":["kind:initiative"]},{"id":"ch","status":"open","labels":["kind:chart"]}]`, nil
		}
		return orun(args...)
	}
	if _, _, err := NewSurgery("T", "", "ini", true); err != nil {
		t.Fatal(err)
	}
	want := []string{"create", "--type", "epic", "--labels", "kind:checkpoint", "--no-inherit-labels",
		"--parent", "ini", "--title", "T", "--description", "", "--silent"}
	if got := (*calls)[0]; !reflect.DeepEqual(got, want) {
		t.Errorf("argv = %v\nwant %v", got, want)
	}
	*calls = nil
	for _, c := range []struct {
		ini      string
		required bool
	}{{"old", false}, {"ch", false}, {"nope", true}, {"", true}} {
		if _, _, err := NewSurgery("T", "", c.ini, c.required); err == nil {
			t.Errorf("NewSurgery(%q, required=%v) = nil, want a refusal", c.ini, c.required)
		}
	}
	for _, c := range *calls {
		if c[0] == "create" {
			t.Errorf("a refused NewSurgery created: %v", c)
		}
	}
}

// A mid-sequence failure returns the ids already created, so the caller can name
// the orphan (mirrors NewRollback).
func TestNewSurgeryOrphanIDs(t *testing.T) {
	for failAt, want := range map[string][2]string{
		"cut":    {"", "ws-cp"},
		"update": {"ws-sg", "ws-cp"},
	} {
		surgeryHarness(t, failAt)
		sg, cp, err := NewSurgery("T", "", "", false)
		if err == nil || sg != want[0] || cp != want[1] {
			t.Errorf("fail at %s: NewSurgery = %q, %q, %v; want %q, %q, error", failAt, sg, cp, err, want[0], want[1])
		}
	}
}

// SurgeriesOf returns every Surgery child, closed and abandoned included, in
// creation order whatever order bd lists them in, ignoring other kinds.
func TestSurgeriesOf(t *testing.T) {
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(args ...string) (string, error) {
		if args[0] != "children" || args[1] != "ws-cp" {
			t.Fatalf("SurgeriesOf ran %v, want a bd children read of ws-cp", args)
		}
		return `[{"id":"mc-b","status":"open","labels":["kind:build"],"created_at":"2026-01-01T00:00:00Z"},
{"id":"sg-2","title":"second","status":"open","labels":["kind:surgery"],"created_at":"2026-01-03T00:00:00Z"},
{"id":"sg-1","title":"first","status":"closed","close_reason":"abandoned: none","labels":["kind:surgery"],"created_at":"2026-01-02T00:00:00Z"}]`, nil
	}
	got, err := SurgeriesOf("ws-cp")
	if err != nil || len(got) != 2 || got[0].ID != "sg-1" || got[1].ID != "sg-2" || got[0].Title != "first" {
		t.Errorf("SurgeriesOf = %+v, %v; want sg-1, sg-2 in creation order", got, err)
	}
}

// A Checkpoint's Review is due when it is drained (Spec D8, FRs 28–31):
// no Build, Critique, Conflict, Surgery or Review open or holding a live effort
// ref, no open pivot, and no Approval at head. A stale Approval doesn't hold it.
func TestReviewDue(t *testing.T) {
	orun, orefs := run, effortRefs
	t.Cleanup(func() { run, effortRefs = orun, orefs })
	kid := func(id, status string, kind contract.Kind, body string) string {
		return `{"id":"` + id + `","status":"` + status + `","labels":["kind:` + string(kind) + `"],"parent":"ws-cp","description":"` + body + `"}`
	}
	kidReason := func(id string, kind contract.Kind, body, reason string) string {
		return `{"id":"` + id + `","status":"closed","close_reason":"` + reason + `","labels":["kind:` + string(kind) + `"],"parent":"ws-cp","description":"` + body + `"}`
	}
	atHead := func(body, branch string) bool { return body == "Commit: tip" && branch == "checkpoint/ws-cp" }
	for _, tc := range []struct {
		name    string
		kids    []string
		efforts map[string]bool
		pivots  string
		want    bool
	}{
		{"drained", []string{kid("b1", "closed", contract.KindBuild, ""), kid("c1", "closed", contract.KindCritique, "")}, nil, "[]", true},
		{"open build", []string{kid("b1", "closed", contract.KindBuild, ""), kid("b2", "in_progress", contract.KindBuild, "")}, nil, "[]", false},
		{"open surgery", []string{kid("s1", "open", contract.KindSurgery, "")}, nil, "[]", false},
		{"open review", []string{kid("b1", "closed", contract.KindBuild, ""), kid("r1", "open", contract.KindReview, "")}, nil, "[]", false},
		{"open forge review", []string{kid("b1", "closed", contract.KindBuild, ""), kid("f1", "open", contract.KindForgeReview, "")}, nil, "[]", false},
		{"closed forge review", []string{kid("b1", "closed", contract.KindBuild, ""), kid("f1", "closed", contract.KindForgeReview, "")}, nil, "[]", true},
		{"closed child with live ref", []string{kid("b1", "closed", contract.KindBuild, "")}, map[string]bool{"b1": true}, "[]", false},
		{"open pivot", []string{kid("b1", "closed", contract.KindBuild, "")}, nil,
			`[{"id":"p1","status":"open","labels":["kind:pivot"]}]`, false},
		{"approved approval at head", []string{kid("b1", "closed", contract.KindBuild, ""), kidReason("a1", contract.KindApproval, "Commit: tip", "approved")}, nil, "[]", false},
		{"rework approval at head", []string{kid("b1", "closed", contract.KindBuild, ""), kidReason("a1", contract.KindApproval, "Commit: tip", "rework: mc-1")}, nil, "[]", true},
		{"obsoleted approval at head", []string{kid("b1", "closed", contract.KindBuild, ""), kidReason("a1", contract.KindApproval, "Commit: tip", "Approval obsoleted: moved")}, nil, "[]", true},
		{"obsolete: approval at head", []string{kid("b1", "closed", contract.KindBuild, ""), kidReason("a1", contract.KindApproval, "Commit: tip", "obsolete: x")}, nil, "[]", true},
		{"empty-reason approval at head", []string{kid("b1", "closed", contract.KindBuild, ""), kid("a1", "closed", contract.KindApproval, "Commit: tip")}, nil, "[]", true},
		{"open approval at head", []string{kid("b1", "closed", contract.KindBuild, ""), kid("a1", "open", contract.KindApproval, "Commit: tip")}, nil, "[]", false},
		{"stale approval", []string{kid("b1", "closed", contract.KindBuild, ""), kid("a1", "closed", contract.KindApproval, "Commit: old")}, nil, "[]", true},
		{"no members yet", nil, nil, "[]", false},
	} {
		run = func(args ...string) (string, error) {
			if args[0] == "children" {
				return "[" + strings.Join(tc.kids, ",") + "]", nil
			}
			return tc.pivots, nil
		}
		effortRefs = func() (map[string]bool, error) { return tc.efforts, nil }
		if got, err := ReviewDue("ws-cp", nil, atHead); err != nil || got != tc.want {
			t.Errorf("%s: ReviewDue = %v, %v; want %v", tc.name, got, err, tc.want)
		}
	}
}

// joinFake fakes bd for NewSurgeryOn: show returns cp, children kids, dep list pivots.
func joinFake(t *testing.T, show, kids, pivots string) *[][]string {
	t.Helper()
	orun, oactor := run, actor
	t.Cleanup(func() { run, actor = orun, oactor })
	actor = func() string { return "alice" }
	calls := &[][]string{}
	run = func(args ...string) (string, error) {
		*calls = append(*calls, args)
		switch args[0] {
		case "show":
			if show == "" {
				return "", errors.New("bd show: exit 1: Issue x not found")
			}
			return show, nil
		case "children":
			return kids, nil
		case "dep":
			return pivots, nil
		case "create":
			return "ws-sg", nil
		}
		return "[]", nil
	}
	return calls
}

// NewSurgeryOn (Spec D2): each refusal text, the refusal order when several apply,
// and a success that spawns+claims a child with no Checkpoint create or branch cut.
func TestNewSurgeryOn(t *testing.T) {
	cp := func(kind, status string) string {
		return `[{"id":"ws-cp","status":"` + status + `","labels":["kind:` + kind + `"]}]`
	}
	landing := `{"id":"ws-l","status":"open","labels":["kind:landing"]}`
	conflict := `{"id":"ws-c","status":"open","labels":["kind:conflict"]}`
	baseTip := `{"id":"ws-k","status":"open","labels":["kind:critique"],"description":"Base: aaa\nTip: bbb"}`
	baseOnly := `{"id":"ws-k","status":"open","labels":["kind:critique"],"description":"Base: aaa"}`
	surgery := `{"id":"ws-s","status":"in_progress","labels":["kind:surgery"]}`
	closedSurgery := `{"id":"ws-s","status":"closed","labels":["kind:surgery"]}`
	pivot := `[{"id":"ws-p","status":"open","labels":["kind:pivot"]}]`
	for _, tc := range []struct {
		name, show, kids, pivots, want string
	}{
		{"missing", "", "[]", "[]", "no such bead ws-cp"},
		{"not a checkpoint", cp("build", "open"), "[]", "[]", "ws-cp is not a Checkpoint"},
		{"landing", cp("checkpoint", "open"), "[" + landing + "]", "[]", "ws-cp is landing — no surgery while a Landing is live"},
		{"pivot", cp("checkpoint", "open"), "[]", pivot, "ws-cp is frozen at a pivot gate — resolve the pivot first"},
		{"conflict", cp("checkpoint", "open"), "[" + conflict + "]", "[]", "ws-cp has an open Conflict Critique rewriting its branch — wait for it to publish"},
		{"base+tip", cp("checkpoint", "open"), "[" + baseTip + "]", "[]", "ws-cp has an open Base+Tip Critique rewriting its branch — wait for it to publish"},
		{"open surgery", cp("checkpoint", "open"), "[" + surgery + "]", "[]", "ws-cp already has open Surgery ws-s — l to resume"},
		{"closed", cp("checkpoint", "closed"), "[]", "[]", "ws-cp is closed — file on a new Checkpoint instead"},
		// order: landing > pivot > rewriting > surgery > closed
		{"landing beats pivot", cp("checkpoint", "closed"), "[" + landing + "," + surgery + "]", pivot, "ws-cp is landing — no surgery while a Landing is live"},
		{"pivot beats rewriting", cp("checkpoint", "closed"), "[" + conflict + "," + surgery + "]", pivot, "ws-cp is frozen at a pivot gate — resolve the pivot first"},
		{"rewriting beats surgery", cp("checkpoint", "closed"), "[" + baseTip + "," + surgery + "]", "[]", "ws-cp has an open Base+Tip Critique rewriting its branch — wait for it to publish"},
		{"surgery beats closed", cp("checkpoint", "closed"), "[" + surgery + "]", "[]", "ws-cp already has open Surgery ws-s — l to resume"},
	} {
		joinFake(t, tc.show, tc.kids, tc.pivots)
		id, err := NewSurgeryOn("ws-cp", "T", "")
		var r JoinRefusal
		if !errors.As(err, &r) || string(r) != tc.want || id != "" {
			t.Errorf("%s: got %q, %v; want refusal %q", tc.name, id, err, tc.want)
		}
	}

	// Not refusing: a Base-only Critique, a closed Surgery, an open Build.
	calls := joinFake(t, cp("checkpoint", "open"), "["+baseOnly+","+closedSurgery+`,{"id":"ws-b","status":"open","labels":["kind:build"]}]`, "[]")
	id, err := NewSurgeryOn("ws-cp", "Tweak", "dim")
	if err != nil || id != "ws-sg" {
		t.Fatalf("NewSurgeryOn = %q, %v", id, err)
	}
	var creates, claims int
	for _, c := range *calls {
		switch {
		case c[0] == "create" && slices.Contains(c, "--parent") && slices.Contains(c, "kind:surgery"):
			creates++
		case c[0] == "create":
			t.Errorf("unexpected create %v", c)
		case c[0] == "update" && c[1] == "ws-sg":
			claims++
		}
	}
	if creates != 1 || claims != 1 {
		t.Errorf("creates=%d claims=%d, want 1 and 1 (calls %v)", creates, claims, *calls)
	}
}

// A nested leaf's closed-but-unintegrated effort lives in its child repo: the
// union EffortRefsAcross reads holds the container from draining (Spec FR 45).
func TestNestedClosedEffortHoldsTheContainer(t *testing.T) {
	ws := t.TempDir()
	g := func(dir string, args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@e.com", "-c", "user.name=t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	child := filepath.Join(ws, "child")
	os.MkdirAll(child, 0o755)
	os.MkdirAll(filepath.Join(ws, ".mugicrew"), 0o755)
	os.WriteFile(filepath.Join(ws, ".mugicrew", "config.toml"), []byte("[context.child]\nrepo = \"child\"\n"), 0o644)
	g(ws, "init", "-q", "-b", "main")
	g(child, "init", "-q", "-b", "main")
	g(child, "commit", "-q", "--allow-empty", "-m", "root")
	g(child, "branch", "effort/mc-1.2")
	t.Chdir(ws)

	refs, err := EffortRefsAcross()
	if err != nil || !refs["mc-1.2"] {
		t.Fatalf("EffortRefsAcross = %v, %v; want mc-1.2 from the child", refs, err)
	}
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(...string) (string, error) {
		return `[{"id":"mc-1.2","title":"unintegrated","status":"closed","labels":["kind:build"],"parent":"mc-1"}]`, nil
	}
	if n, err := OpenChildren("mc-1", refs); err != nil || n != 1 {
		t.Errorf("OpenChildren over the union = %d, %v; want 1", n, err)
	}
}

// A declared child that is not checked out contributes no refs and no error: the
// Workspace's own efforts still come back so its containers can drain.
func TestEffortRefsAcrossSkipsAbsentChild(t *testing.T) {
	ws := t.TempDir()
	g := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", ws, "-c", "user.email=t@e.com", "-c", "user.name=t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	os.MkdirAll(filepath.Join(ws, ".mugicrew"), 0o755)
	os.WriteFile(filepath.Join(ws, ".mugicrew", "config.toml"), []byte("[context.child]\nrepo = \"child\"\n"), 0o644)
	g("init", "-q", "-b", "main")
	g("commit", "-q", "--allow-empty", "-m", "root")
	g("branch", "effort/mc-1.1")
	t.Chdir(ws)

	refs, err := EffortRefsAcross()
	if err != nil || !refs["mc-1.1"] {
		t.Fatalf("EffortRefsAcross = %v, %v; want mc-1.1 from the Workspace", refs, err)
	}
}

// A caller-given effort set is the only effort read: DrainBlockers reaches neither
// git nor the declared-repo table (Spec FR 46).
func TestDrainBlockersWithAGivenSetReadsNoRefs(t *testing.T) {
	orun, orefs := run, effortRefs
	t.Cleanup(func() { run, effortRefs = orun, orefs })
	run = func(...string) (string, error) {
		return `[{"id":"mc-1.1","title":"x","status":"closed","labels":["kind:build"],"parent":"mc-1"}]`, nil
	}
	effortRefs = func() (map[string]bool, error) { t.Error("effortRefs read despite a given set"); return nil, nil }
	if open, held, err := DrainBlockers("mc-1", map[string]bool{}); err != nil || open != 0 || len(held) != 0 {
		t.Errorf("DrainBlockers = %d, %v, %v; want none", open, held, err)
	}
}

// NewInitiative is an unparented kind:initiative epic: no drafted-by, status, parent
// or deps; Signal and Rollback stay tasks.
func TestNewInitiativeArgs(t *testing.T) {
	var got []string
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(args ...string) (string, error) { got = args; return "mc-9", nil }

	id, err := NewInitiative("Initiative: seed", "the body", "")
	if err != nil || id != "mc-9" {
		t.Fatalf("NewInitiative = %q, %v", id, err)
	}
	if i := slices.Index(got, "--type"); i < 0 || got[i+1] != "epic" {
		t.Errorf("want --type epic: %v", got)
	}
	if i := slices.Index(got, "--labels"); i < 0 || got[i+1] != "kind:initiative" {
		t.Errorf("want only kind:initiative: %v", got)
	}
	for _, f := range []string{"--parent", "--deps", "--status"} {
		if slices.Contains(got, f) {
			t.Errorf("unexpected %s: %v", f, got)
		}
	}
	for _, a := range got {
		if strings.Contains(a, "drafted-by") {
			t.Errorf("drafted-by in %v", got)
		}
	}
	if !slices.Contains(got, "--no-inherit-labels") || titleArg(got) != "seed" || !slices.Contains(got, "the body") {
		t.Errorf("argv %v", got)
	}
	for _, a := range [][]string{newSignalArgs("t", "b"), unparentedArgs(Successor{Kind: contract.KindRollback}, "t", "b")} {
		if i := slices.Index(a, "--type"); a[i+1] != "task" {
			t.Errorf("want --type task: %v", a)
		}
	}
}

// RefsAcross reads checkpoint/<id> tips beside efforts, a nested Checkpoint's from the
// repo that declares it, in the same for-each-ref.
func TestRefsAcrossReadsCheckpointTipsPerRepo(t *testing.T) {
	ws := t.TempDir()
	child := filepath.Join(ws, "child")
	os.MkdirAll(child, 0o755)
	os.MkdirAll(filepath.Join(ws, ".mugicrew"), 0o755)
	os.WriteFile(filepath.Join(ws, ".mugicrew", "config.toml"), []byte("[context.child]\nrepo = \"child\"\n"), 0o644)
	g := func(dir string, args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@e.com", "-c", "user.name=t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	g(ws, "init", "-q", "-b", "main")
	g(ws, "commit", "-q", "--allow-empty", "-m", "root")
	g(ws, "branch", "checkpoint/mc-1")
	g(ws, "branch", "effort/mc-1.1")
	g(child, "init", "-q", "-b", "main")
	g(child, "commit", "-q", "--allow-empty", "-m", "root")
	g(child, "branch", "checkpoint/mc-2.1")
	t.Chdir(ws)

	efforts, tips, err := RefsAcross()
	if err != nil || !efforts["mc-1.1"] || len(efforts) != 1 {
		t.Fatalf("efforts = %v, %v", efforts, err)
	}
	want := map[string]string{"mc-1": g(ws, "rev-parse", "checkpoint/mc-1"), "mc-2.1": g(child, "rev-parse", "checkpoint/mc-2.1")}
	if !maps.Equal(tips, want) {
		t.Errorf("tips = %v, want %v", tips, want)
	}
}

// SetLineIfUnset appends the line when its key is absent and makes no write when
// the body already carries the key.
func TestSetLineIfUnset(t *testing.T) {
	var got []string
	update := func(id string, args []string) error { got = append(got, id+" "+strings.Join(args, "|")); return nil }
	if _, err := SetLineIfUnset(update, "x", "title\n", "Anchor:", "Anchor: abc"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"x --description|title\nAnchor: abc\n"}; !reflect.DeepEqual(got, want) {
		t.Errorf("absent: writes = %q, want %q", got, want)
	}
	got = nil
	if _, err := SetLineIfUnset(update, "x", "title\nAnchor: old\n", "Anchor:", "Anchor: new"); err != nil || got != nil {
		t.Errorf("present: err %v writes %q, want none", err, got)
	}
}

// The Initiative constructor carries the ref in its one create.
func TestNewInitiativeCarriesRef(t *testing.T) {
	var got []string
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(args ...string) (string, error) { got = args; return "mc-9", nil }
	if _, err := NewInitiative("Initiative: seed", "b", "PROJ-1"); err != nil {
		t.Fatal(err)
	}
	if i := slices.Index(got, "--external-ref"); i < 0 || got[i+1] != "PROJ-1" {
		t.Errorf("want --external-ref PROJ-1: %v", got)
	}
}

// The by-ref lookup spans every status and keeps only exact kind:initiative matches.
func TestInitiativesByRef(t *testing.T) {
	var got []string
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(args ...string) (string, error) {
		got = args
		return `[{"id":"a","status":"closed","labels":["kind:initiative"],"external_ref":"PROJ-1"},
{"id":"b","status":"open","labels":["kind:signal"],"external_ref":"PROJ-1"},
{"id":"c","status":"open","labels":["kind:initiative"],"external_ref":"proj-1"}]`, nil
	}
	hits, err := InitiativesByRef("PROJ-1")
	if err != nil || len(hits) != 1 || hits[0].ID != "a" {
		t.Fatalf("hits = %+v, %v; want only a", hits, err)
	}
	if want := []string{"list", "--external-ref", "PROJ-1", "--all", "-n", "0", "--json"}; !slices.Equal(got, want) {
		t.Errorf("argv = %v, want %v", got, want)
	}
}

// LatchOf lists without --all (a closed latch must not count), keeps a Draft
// kind:latch with the exact ref, and skips other kinds carrying the same ref.
func TestLatchOfIgnoresClosedAndOtherKinds(t *testing.T) {
	var argv []string
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(args ...string) (string, error) {
		argv = args
		return `[{"id":"mc-s","labels":["kind:signal"],"external_ref":"jira-latch-ABC-1"},
{"id":"mc-x","labels":["kind:latch"],"external_ref":"jira-latch-ABC-10"},
{"id":"mc-l","status":"deferred","labels":["kind:latch"],"external_ref":"jira-latch-ABC-1"}]`, nil
	}
	got, ok, err := LatchOf("jira-latch-ABC-1")
	if err != nil || !ok || got.ID != "mc-l" {
		t.Fatalf("LatchOf = %+v, %v, %v", got, ok, err)
	}
	if want := []string{"list", "--external-ref", "jira-latch-ABC-1", "--json"}; !reflect.DeepEqual(argv, want) {
		t.Errorf("argv = %v, want %v", argv, want)
	}
	run = func(...string) (string, error) {
		return `[{"id":"mc-s","labels":["kind:signal"],"external_ref":"jira-latch-ABC-1"}]`, nil
	}
	if _, ok, _ := LatchOf("jira-latch-ABC-1"); ok {
		t.Error("a kind:signal bead counted as the latch")
	}
}

// A nested container's foreign-land hold names whose mainline holds it; a Workspace one doesn't.
func TestHoldForeignNamesTheNestedContext(t *testing.T) {
	o := SpawnBlockingTask
	t.Cleanup(func() { SpawnBlockingTask = o })
	var body string
	SpawnBlockingTask = func(_, _, _, b string) (string, bool, error) { body = b; return "t", true, nil }
	f := &git.ForeignLanding{Trailer: "Checkpoint: x", Ours: "a", Theirs: "b", Sha: "f00d"}
	HoldForeign(Bead{ID: "cp", Labels: []string{"context:kid", "repo:kid"}}, f)
	if !strings.HasPrefix(body, `Mainline of context kid holds "Checkpoint: x"`) {
		t.Errorf("nested body = %q", body)
	}
	HoldForeign(Bead{ID: "cp"}, f)
	if !strings.HasPrefix(body, `Mainline holds "Checkpoint: x"`) {
		t.Errorf("workspace body = %q", body)
	}
}

// Fetch-by-id always passes --all (closed ids silently return nothing without it)
// and never --brief (Approvals read Description).
func TestListByIDFlags(t *testing.T) {
	orun := run
	t.Cleanup(func() { run = orun })
	var argv []string
	run = func(args ...string) (string, error) {
		argv = args
		return `[{"id":"a","status":"closed","description":"body"}]`, nil
	}
	got, err := ListByID([]string{"a", "b"})
	if err != nil || len(got) != 1 || got[0].Description != "body" {
		t.Fatalf("ListByID = %v, %v", got, err)
	}
	if j := strings.Join(argv, " "); j != "list --all -n 0 --id a,b --json" {
		t.Errorf("argv = %q", j)
	}
	argv = nil
	if got, _ := ListByID(nil); got != nil || argv != nil {
		t.Error("no ids must not shell out")
	}
}

// The ancestor walk sends the id with its dotted prefixes, then follows a parent
// that is not a prefix (mc-t72m.6's parent is mc-8ymw).
func TestListWithAncestorsFollowsNonDottedParent(t *testing.T) {
	orun := run
	t.Cleanup(func() { run = orun })
	var calls []string
	run = func(args ...string) (string, error) {
		ids := args[5]
		calls = append(calls, ids)
		switch ids {
		case "mc-t72m.6,mc-t72m":
			return `[{"id":"mc-t72m.6","status":"closed","parent":"mc-8ymw"}]`, nil
		case "mc-8ymw":
			return `[{"id":"mc-8ymw","status":"closed"}]`, nil
		}
		return "[]", nil
	}
	got, err := ListWithAncestors("mc-t72m.6")
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
	if want := []string{"mc-t72m.6,mc-t72m", "mc-8ymw"}; !reflect.DeepEqual(calls, want) {
		t.Errorf("calls = %v, want %v", calls, want)
	}
}

// Closed word search sends the longest word to bd search, matches the rest
// client-side, and fetches the survivors in full by id.
func TestSearchClosedFiltersRemainingWordsAndFetchesByID(t *testing.T) {
	orun := run
	t.Cleanup(func() { run = orun })
	var calls [][]string
	run = func(args ...string) (string, error) {
		calls = append(calls, args)
		if args[0] == "search" {
			return `[{"id":"mc-1","title":"Fix login bug","status":"closed"},{"id":"mc-2","title":"Fix logout","status":"closed"}]`, nil
		}
		return `[{"id":"mc-1","title":"Fix login bug","status":"closed","parent":"mc-0"}]`, nil
	}
	got, err := SearchClosed("fix BUG login")
	if err != nil || len(got) != 1 || got[0].Parent != "mc-0" {
		t.Fatalf("got %v, %v", got, err)
	}
	if want := []string{"search", "login", "-s", "closed", "-n", "0", "--json"}; !reflect.DeepEqual(calls[0], want) {
		t.Errorf("search call = %v", calls[0])
	}
	if c := calls[1]; c[0] != "list" || !slices.Contains(c, "--all") || !slices.Contains(c, "mc-1") || slices.Contains(c, "--brief") {
		t.Errorf("fetch call = %v", c)
	}
}

// The default id set is one bd sql call over bd's own tables (flat UNION, no CTE).
func TestDefaultIDsIsOneFlatUnionQuery(t *testing.T) {
	orun := run
	t.Cleanup(func() { run = orun })
	var calls [][]string
	run = func(args ...string) (string, error) { calls = append(calls, args); return "id\nmc-1\nmc-2.1", nil }
	got, err := DefaultIDs()
	if err != nil || !slices.Equal(got, []string{"mc-1", "mc-2.1"}) {
		t.Fatalf("got %v, %v", got, err)
	}
	if len(calls) != 1 || calls[0][0] != "sql" || strings.Contains(calls[0][len(calls[0])-1], "with ") {
		t.Errorf("calls = %v", calls)
	}
	for _, want := range []string{"depends_on_issue_id", "kind:landing", "'caused-by'"} {
		if !strings.Contains(defaultIDsSQL, want) {
			t.Errorf("sql lacks %q", want)
		}
	}
}

// fixtureIssue is one row of the default-set fixture store: the columns the
// default-set rule reads, with closedAgo in hours.
type fixtureIssue struct {
	id, label, status, typ, reason, parent, causedBy string
	closedAgo                                        int
}

// defaultSetFixture covers ADR 0232's default set and each of its edges.
var defaultSetFixture = []fixtureIssue{
	{id: "open-chart", label: "kind:chart", status: "open"},
	{id: "open-chart.1", label: "kind:build", status: "closed", parent: "open-chart", closedAgo: 900},
	{id: "open-cp", label: "kind:checkpoint", status: "open"},
	{id: "open-cp.1", label: "kind:build", status: "closed", parent: "open-cp", closedAgo: 900},
	{id: "open-cp.land", label: "kind:landing", status: "closed", reason: "landed: x", parent: "open-cp", closedAgo: 900},
	{id: "open-init", label: "kind:initiative", status: "open"},
	{id: "open-init.1", label: "kind:chart", status: "closed", parent: "open-init", closedAgo: 900},
	{id: "open-task", label: "kind:task", status: "in_progress"},
	{id: "old-closed", label: "kind:task", status: "closed", closedAgo: 900},
	{id: "closed-init", label: "kind:initiative", status: "closed", closedAgo: 900},
	{id: "closed-init.1", label: "kind:build", status: "closed", parent: "closed-init", closedAgo: 900},
	{id: "in-chart", label: "kind:chart", status: "closed", closedAgo: 23},
	{id: "in-chart.1", label: "kind:build", status: "closed", parent: "in-chart", closedAgo: 900},
	{id: "in-spec", label: "kind:spec", status: "closed", causedBy: "in-chart", closedAgo: 900},
	{id: "out-chart", label: "kind:chart", status: "closed", closedAgo: 25},
	{id: "out-chart.1", label: "kind:build", status: "closed", parent: "out-chart", closedAgo: 900},
	{id: "out-spec", label: "kind:spec", status: "closed", causedBy: "out-chart", closedAgo: 900},
	{id: "event-chart", label: "kind:chart", status: "closed", typ: "event", closedAgo: 1},
	{id: "event-chart.1", label: "kind:build", status: "closed", parent: "event-chart", closedAgo: 900},
	{id: "in-landing", label: "kind:landing", status: "closed", reason: "landed: y", parent: "old-cp", closedAgo: 23},
	{id: "old-cp", label: "kind:checkpoint", status: "closed", closedAgo: 900},
	{id: "out-landing", label: "kind:landing", status: "closed", reason: "landed: z", parent: "older-cp", closedAgo: 25},
	{id: "older-cp", label: "kind:checkpoint", status: "closed", closedAgo: 900},
	{id: "unlanded", label: "kind:landing", status: "closed", reason: "abandoned", parent: "unlanded-cp", closedAgo: 1},
	{id: "unlanded-cp", label: "kind:checkpoint", status: "closed", closedAgo: 900},
}

// defaultSetOf is the default set computed client-side, from the rule in
// docs/research/bd-read-paths-for-tui-default-and-on-demand-closed-sets.md §3.
func defaultSetOf(rows []fixtureIssue) []string {
	byID := map[string]fixtureIssue{}
	for _, r := range rows {
		byID[r.id] = r
	}
	// recent: a closed non-event Chart inside the 24 h window.
	recent := func(r fixtureIssue) bool {
		return r.status == "closed" && r.label == "kind:chart" && r.typ != "event" && r.closedAgo < 24
	}
	landed := func(r fixtureIssue) bool {
		return r.status == "closed" && r.label == "kind:landing" && strings.HasPrefix(r.reason, "landed: ") && r.closedAgo < 24
	}
	set := map[string]bool{}
	for _, r := range rows {
		p, hasParent := byID[r.parent]
		switch {
		case r.status != "closed", // the open set
			recent(r), landed(r),
			r.label == "kind:initiative" && r.status == "closed",
			hasParent && p.status != "closed" && slices.Contains([]string{"kind:chart", "kind:checkpoint", "kind:initiative"}, p.label),
			hasParent && p.status == "closed" && p.label == "kind:initiative",
			hasParent && recent(p),
			r.causedBy != "" && recent(byID[r.causedBy]):
			set[r.id] = true
		}
		if landed(r) && hasParent && p.label == "kind:checkpoint" && p.status == "closed" {
			set[p.id] = true
		}
	}
	return slices.Sorted(maps.Keys(set))
}

// The default set's client-side computation keeps exactly the hand-listed ids:
// each inside/outside-24h pair, the event Chart, the unlanded Landing and a
// Landing's closed Checkpoint. No real Dolt runs under go test (bd's embedded
// mode has no `bd sql`), so defaultIDsSQL itself is checked against this
// reference only by hand on a live store (research note §3 measured 0 missing).
func TestDefaultSetClientSideComputation(t *testing.T) {
	want := []string{
		"closed-init", "closed-init.1", "in-chart", "in-chart.1", "in-landing", "in-spec",
		"old-cp", "open-chart", "open-chart.1", "open-cp", "open-cp.1", "open-cp.land",
		"open-init", "open-init.1", "open-task",
	}
	if got := defaultSetOf(defaultSetFixture); !slices.Equal(got, want) {
		t.Errorf("default set\n got %v\nwant %v", got, want)
	}
}

func TestKindsReadsLabelsTable(t *testing.T) {
	old := run
	t.Cleanup(func() { run = old })
	run = func(args ...string) (string, error) {
		return "issue_id,label\nmc-1,kind:build\nmc-1.2,kind:chart", nil
	}
	ks, err := Kinds()
	if err != nil || ks["mc-1"] != contract.KindBuild || ks["mc-1.2"] != contract.KindChart || len(ks) != 2 {
		t.Fatalf("Kinds = %v, %v", ks, err)
	}
}

func TestNotBuiltFrom(t *testing.T) {
	cs, _ := parseComments([]byte(`[{"text":"Not built: old"},{"text":"hi"},{"text":"  Not built:  no repo \n"}]`))
	if got := notBuiltFrom(cs); got != "no repo" {
		t.Errorf("got %q", got)
	}
	if got := notBuiltFrom([]Comment{{Text: "hi"}}); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestParseComments(t *testing.T) {
	js := `[{"id":"1","issue_id":"mc-x","author":"Franky","text":"first **b**","created_at":"2026-10-17T09:00:00Z"},{"id":"2","issue_id":"mc-x","author":"Alice","text":"second","created_at":"2026-10-17T10:00:00Z"}]`
	cs, err := parseComments([]byte(js))
	if err != nil || len(cs) != 2 {
		t.Fatalf("%v %v", cs, err)
	}
	if c := cs[0]; c.Author != "Franky" || c.Text != "first **b**" || !c.Created.Equal(time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("got %+v", c)
	}
	if _, err := parseComments([]byte(`nope`)); err == nil {
		t.Error("want a decode error")
	}
}

// anchorRig stubs AnchorSeams over one Spec bead; calls logs every seam touched
// ("tip <repo>", "fetch <repo>") and body is the bead's description as written.
func anchorRig(strategy, body string, landedErr error) (AnchorSeams, *[]string, *string) {
	var calls []string
	return AnchorSeams{
		Strategy: func() (string, error) { return strategy, nil },
		Show:     func(string) (Bead, error) { return Bead{Description: body}, nil },
		Update: func(_ string, a []string) error {
			body = a[1]
			return nil
		},
		Fetch:     func() (string, error) { calls = append(calls, "fetch ws"); return "forge", nil },
		RepoFetch: func(r git.Repo) (string, error) { calls = append(calls, "fetch "+string(r)); return "kidforge", nil },
		Landed: func(git.Repo, string) (string, error) {
			if landedErr != nil {
				return "", landedErr
			}
			return "legacy", nil
		},
		Tip: func(r git.Repo) (string, error) {
			calls = append(calls, "tip "+string(r))
			return "tip" + string(r), nil
		},
	}, &calls, &body
}

// Anchor is one path under every strategy: an existing pin wins untouched, a legacy
// trailer wins unpinned, else the strategy's tip is pinned.
func TestAnchor(t *testing.T) {
	notLanded := git.ErrNotLanded
	for _, tc := range []struct {
		name, strategy, body string
		r                    git.Repo
		landedErr            error
		wantSha, wantBody    string
		wantCalls            []string
		wantErr              bool
	}{
		{"existing pin", git.LandStrategyLocal, "b\nAnchor: abc\n", "", notLanded, "abc", "b\nAnchor: abc\n", nil, false},
		{"legacy trailer", git.LandStrategyLocal, "b\n", "", nil, "legacy", "b\n", nil, false},
		{"local workspace tip", git.LandStrategyLocal, "b\n", "", notLanded, "tip", "b\nAnchor: tip\n", []string{"tip "}, false},
		{"local child tip", git.LandStrategyLocal, "b\n", "kid", notLanded, "tipkid", "b\nAnchor: tipkid\n", []string{"tip kid"}, false},
		{"gitlab fetched", git.LandStrategyGitlab, "b\n", "", notLanded, "forge", "b\nAnchor: forge\n", []string{"fetch ws"}, false},
		{"gitlab child fetched", git.LandStrategyGitlab, "b\n", "kid", notLanded, "kidforge", "b\nAnchor: kidforge\n", []string{"fetch kid"}, false},
		{"trailer error", git.LandStrategyLocal, "b\n", "", errors.New("git broke"), "", "b\n", nil, true},
	} {
		s, calls, body := anchorRig(tc.strategy, tc.body, tc.landedErr)
		sha, err := Anchor(s, tc.r, "x", "Spec: x")
		if (err != nil) != tc.wantErr || sha != tc.wantSha || *body != tc.wantBody || !slices.Equal(*calls, tc.wantCalls) {
			t.Errorf("%s: sha=%q err=%v body=%q calls=%v, want sha=%q body=%q calls=%v", tc.name, sha, err, *body, *calls, tc.wantSha, tc.wantBody, tc.wantCalls)
		}
	}
}

// NewCheckup (ADR 0272 §2): an unparented kind:checkup create carrying the target's
// context and repo stamp, then one related edge; the body names the target and the
// --problem line; a failed edge returns the created id.
func TestNewCheckup(t *testing.T) {
	var calls [][]string
	orun := run
	t.Cleanup(func() { run = orun })
	fail := ""
	run = func(args ...string) (string, error) {
		calls = append(calls, args)
		if args[0] == fail {
			return "", errors.New("boom")
		}
		return "ws-ck", nil
	}
	target := Bead{ID: "ws-t", Title: "Odd bead", Labels: []string{"kind:build", "context:vendor", "repo:vendor/x"}}
	id, err := NewCheckup(target, "wrong edge")
	if err != nil || id != "ws-ck" {
		t.Fatalf("NewCheckup = %q, %v", id, err)
	}
	want := [][]string{
		{"create", "--type", "task", "--labels", "kind:checkup,context:vendor,repo:vendor/x", "--no-inherit-labels",
			"--title", "Repair Odd bead", "--description", "Target: ws-t (Odd bead)\n\nProblem: wrong edge", "--silent"},
		{"dep", "add", "ws-ck", "ws-t", "-t", "related"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("argv = %v\nwant %v", calls, want)
	}
	calls, fail = nil, "dep"
	if id, err = NewCheckup(Bead{ID: "ws-t", Title: "T"}, ""); err == nil || id != "ws-ck" {
		t.Errorf("edge failure = %q, %v; want the orphan id and an error", id, err)
	}
	if got := calls[0][slices.Index(calls[0], "--description")+1]; got != "Target: ws-t (T)" {
		t.Errorf("no --problem body = %q", got)
	}
}

// An open Checkup is found by its related edge to the target, whatever else is open.
func TestOpenCheckupOn(t *testing.T) {
	beads := []Bead{
		{ID: "a", Kind: contract.KindCheckup, Status: "open", Related: []string{"other"}},
		{ID: "b", Kind: contract.KindTask, Status: "open", Related: []string{"t"}},
		{ID: "c", Kind: contract.KindCheckup, Status: "closed", Related: []string{"t"}},
		{ID: "d", Kind: contract.KindCheckup, Status: "in_progress", Related: []string{"t"}},
	}
	if got, ok := openCheckupIn(beads, "t"); !ok || got.ID != "d" {
		t.Errorf("openCheckupIn = %v, %v; want d", got.ID, ok)
	}
	if _, ok := openCheckupIn(beads, "none"); ok {
		t.Error("found a Checkup on a target with none")
	}
}

func TestPausedCheckupRanksAfterRecovering(t *testing.T) {
	b := Bead{Labels: []string{"paused:integrate-conflict", "paused:checkup", "paused:recovering"}}
	if got := PausedCauses(b); !slices.Equal(got, []string{"recovering", "checkup", "integrate-conflict"}) {
		t.Errorf("PausedCauses = %v", got)
	}
}

// TaskChildren counts a Spec's kind:task dependents over discovered-from in every
// status, and ignores a non-task row (a Signal) on the same edge.
func TestTaskChildrenCountsTasksInEveryStatus(t *testing.T) {
	old := run
	t.Cleanup(func() { run = old })
	var got []string
	run = func(args ...string) (string, error) {
		got = args
		return `[
			{"id":"t1","status":"deferred","labels":["kind:task"]},
			{"id":"t2","status":"open","labels":["kind:task"]},
			{"id":"t3","status":"closed","labels":["kind:task"]},
			{"id":"s1","status":"open","labels":["kind:signal"]}]`, nil
	}
	n, err := TaskChildren("spec-1")
	if err != nil || n != 3 {
		t.Fatalf("TaskChildren = %d, %v; want 3", n, err)
	}
	if want := []string{"dep", "list", "spec-1", "--direction=up", "--json", "-t", "discovered-from"}; !slices.Equal(got, want) {
		t.Errorf("argv = %v, want %v", got, want)
	}
}

func TestTaskChildrenPropagatesListError(t *testing.T) {
	old := run
	t.Cleanup(func() { run = old })
	run = func(...string) (string, error) { return "", errors.New("boom") }
	if _, err := TaskChildren("spec-1"); err == nil {
		t.Error("a failing dep list must error")
	}
}
