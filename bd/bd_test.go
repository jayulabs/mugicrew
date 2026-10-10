package bd

import (
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

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
	// Every subset of the four causes, in every label order, ranks manual >
	// abandoned > failing > foreign-hold (ADR 0082).
	ranked := []string{PausedManual, PausedAbandoned, PausedFailing, PausedForeignHold}
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
	if n, err := OpenChildren("mc-1"); err != nil || n != 0 {
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
	if n, err := OpenChildren("mc-1"); err != nil || n != 1 {
		t.Errorf("OpenChildren = %d, %v; want 1 (mc-1.2 holds effort/mc-1.2)", n, err)
	}
	open, held, err := DrainBlockers("mc-1")
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
	run = func(args ...string) (string, error) { calls = append(calls, args); return "ws-rb", nil }

	id, err := NewRollback("Rollback: Roll back X", "broke prod", "ws-cp")
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
	sg, cp, err := NewSurgery("Surgery: Tweak colour", "rows too dim")
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

// A mid-sequence failure returns the ids already created, so the caller can name
// the orphan (mirrors NewRollback).
func TestNewSurgeryOrphanIDs(t *testing.T) {
	for failAt, want := range map[string][2]string{
		"cut":    {"", "ws-cp"},
		"update": {"ws-sg", "ws-cp"},
	} {
		surgeryHarness(t, failAt)
		sg, cp, err := NewSurgery("T", "")
		if err == nil || sg != want[0] || cp != want[1] {
			t.Errorf("fail at %s: NewSurgery = %q, %q, %v; want %q, %q, error", failAt, sg, cp, err, want[0], want[1])
		}
	}
}

// SurgeryOf finds a Checkpoint's kind:surgery child even once it is closed (an
// ended Surgery still carries the intent review reads; Spec D6), and
// reports not-found when the Checkpoint has none.
func TestSurgeryOf(t *testing.T) {
	kids := map[string]string{
		"mc-surgcp": `[{"id":"mc-b","status":"closed","labels":["kind:build"]},{"id":"ws-surg","status":"closed","labels":["kind:surgery"]}]`,
		"ws-cp":     `[{"id":"mc-b","status":"open","labels":["kind:build"]}]`,
	}
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(args ...string) (string, error) {
		if args[0] != "children" {
			t.Fatalf("SurgeryOf ran %v, want a bd children read", args)
		}
		return kids[args[1]], nil
	}
	if id, ok, err := SurgeryOf("mc-surgcp"); err != nil || !ok || id != "ws-surg" {
		t.Errorf("SurgeryOf(surgery Checkpoint) = %q, %v, %v; want ws-surg, true, nil", id, ok, err)
	}
	if id, ok, err := SurgeryOf("ws-cp"); err != nil || ok || id != "" {
		t.Errorf("SurgeryOf(no Surgery) = %q, %v, %v; want not-found", id, ok, err)
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
		{"closed child with live ref", []string{kid("b1", "closed", contract.KindBuild, "")}, map[string]bool{"b1": true}, "[]", false},
		{"open pivot", []string{kid("b1", "closed", contract.KindBuild, "")}, nil,
			`[{"id":"p1","status":"open","labels":["kind:pivot"]}]`, false},
		{"closed approval at head", []string{kid("b1", "closed", contract.KindBuild, ""), kid("a1", "closed", contract.KindApproval, "Commit: tip")}, nil, "[]", false},
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
		if got, err := ReviewDue("ws-cp", atHead); err != nil || got != tc.want {
			t.Errorf("%s: ReviewDue = %v, %v; want %v", tc.name, got, err, tc.want)
		}
	}
}
