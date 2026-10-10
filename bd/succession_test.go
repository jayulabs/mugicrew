package bd

import (
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"jayulabs.org/mugicrew/contract"
)

// The §4.3 invariants, checkable without a DB: caused-by edge in the right
// direction (new bead caused-by pred), --no-inherit-labels, one kind: label,
// container → epic.
func TestSuccessorCreateArgsEnforceInvariants(t *testing.T) {
	args := successorCreateArgs("ws-sig", Successor{
		Kind: contract.KindChart, Container: true,
		Title: "a chart", Description: "a body",
	})
	has := func(want string) bool {
		for _, a := range args {
			if a == want {
				return true
			}
		}
		return false
	}
	if !has("--no-inherit-labels") {
		t.Error("successor create must pass --no-inherit-labels (§3.1 footgun)")
	}
	if !has("--deps") || !has("caused-by:ws-sig") {
		t.Error("successor must be wired caused-by the predecessor, new→pred direction (§4.2)")
	}
	if !has("kind:chart") {
		t.Error("successor must carry its own kind label")
	}
	if !has("--type") || !has("epic") {
		t.Error("a container successor is a bd epic (ADR 0002)")
	}
	if !has("a chart") {
		t.Error("an unprefixed title must reach the create unchanged")
	}
	if got := titleArg(successorCreateArgs("ws-sig", Successor{Kind: contract.KindChart, Title: "Signal: a chart"})); got != "a chart" {
		t.Errorf("copied prefixed title --title = %q, want stripped (Spec ws-hv14 D3)", got)
	}
}

func TestSuccessorCreateArgsLeafIsTask(t *testing.T) {
	args := successorCreateArgs("mc-x", Successor{Kind: contract.KindResearch, Container: false, Title: "t", Description: "d"})
	for i, a := range args {
		if a == "--type" && i+1 < len(args) && args[i+1] != "task" {
			t.Errorf("a leaf successor must be a bd task, got %q", args[i+1])
		}
	}
}

// findKindInDeps is idempotency's guard: a matching-kind dependent means no
// second successor is spawned.
func TestFindKindInDepsMatchesKind(t *testing.T) {
	depList := `[{"id":"ws-chart","labels":["kind:chart"]},{"id":"ws-other","labels":["kind:unowned"]}]`
	if id, ok := findKindInDeps([]byte(depList), contract.KindChart); !ok || id != "ws-chart" {
		t.Errorf("findKindInDeps = (%q,%v), want (ws-chart,true)", id, ok)
	}
	if _, ok := findKindInDeps([]byte(depList), contract.KindResearch); ok {
		t.Error("findKindInDeps matched a kind that isn't present")
	}
	if _, ok := findKindInDeps([]byte(`[]`), contract.KindChart); ok {
		t.Error("no dependents → no successor found")
	}
}

// A chart-conflict child is spawned on the PARENT edge (--parent, not caused-by),
// so it counts in OpenChildren and undrains the Chart (ADR 0011).
func TestChildCreateArgsUseParentEdge(t *testing.T) {
	args := childCreateArgs("ws-chart", Successor{
		Kind: contract.KindChartConflict, Container: false,
		Title: "Chart-conflict: x", Description: "base abc123",
	})
	has := func(want string) bool {
		for _, a := range args {
			if a == want {
				return true
			}
		}
		return false
	}
	if !has("--parent") || !has("ws-chart") {
		t.Error("a membership child must be wired --parent <container> (undrains via OpenChildren)")
	}
	for _, a := range args {
		if a == "caused-by:ws-chart" {
			t.Error("a member child must NOT be a caused-by successor — that would not count in OpenChildren")
		}
	}
	if !has("--no-inherit-labels") || !has("kind:chart-conflict") {
		t.Error("a child must keep its single kind label (§3.1 footgun)")
	}
	if got := titleArg(args); got != "x" {
		t.Errorf("prefixed child --title = %q, want stripped (Spec ws-hv14 D3)", got)
	}
}

// openChildOfKind is the idempotency guard: exactly one open conflict yields no
// second bead; a closed one no longer guards, so a fresh conflict spawns again.
func TestOpenChildOfKindGuardsOnOpenOnly(t *testing.T) {
	kids := []Bead{
		{ID: "ws-cf", Kind: contract.KindChartConflict, Status: "open"},
		{ID: "ws-done", Kind: contract.KindChartConflict, Status: "closed"},
		{ID: "mc-t", Kind: contract.Kind("unowned"), Status: "open"},
	}
	if id, ok := openChildOfKind(kids, contract.KindChartConflict); !ok || id != "ws-cf" {
		t.Errorf("openChildOfKind = (%q,%v), want (ws-cf,true) — an open conflict blocks a second", id, ok)
	}
	closedOnly := []Bead{{ID: "ws-done", Kind: contract.KindChartConflict, Status: "closed"}}
	if _, ok := openChildOfKind(closedOnly, contract.KindChartConflict); ok {
		t.Error("a closed conflict must not guard — a re-conflict after resolve spawns a fresh bead")
	}
}

func TestParseShowReadsKindAndStatus(t *testing.T) {
	blob := `[{"id":"ws-sig","title":"a signal","status":"open","labels":["kind:signal","triage:ready-for-agent"]}]`
	b, err := parseShow([]byte(blob))
	if err != nil {
		t.Fatalf("parseShow: %v", err)
	}
	if b.ID != "ws-sig" || b.Kind != contract.KindSignal || b.Status != "open" || b.Title != "a signal" {
		t.Errorf("parseShow = %+v", b)
	}
}

// The prefactor guarantee: parseBeads carries the bead body through
// from the same ListAll read the render and guided-edit Builds ride, so no
// separate fetch is needed. Kept a focused test against a fixture bd blob.
func TestParseBeadsPopulatesDescription(t *testing.T) {
	blob := `[{"id":"ws-sig","title":"a signal","status":"open","description":"the full body\nsecond line","labels":["kind:signal"]}]`
	beads, err := parseBeads([]byte(blob))
	if err != nil {
		t.Fatalf("parseBeads: %v", err)
	}
	if len(beads) != 1 {
		t.Fatalf("parseBeads returned %d beads, want 1", len(beads))
	}
	if got, want := beads[0].Description, "the full body\nsecond line"; got != want {
		t.Errorf("Description = %q, want %q", got, want)
	}
}

func TestParseShowEmptyIsError(t *testing.T) {
	if _, err := parseShow([]byte(`[]`)); err == nil {
		t.Error("an empty result must be a loud error")
	}
}

// started_at is the claim time ps renders as age-since-claim (ADR 0025); an
// unclaimed bead carries none and parses to the zero time (rendered "—").
func TestParseBeadsStartedAt(t *testing.T) {
	blob := `[{"id":"mc-a","status":"in_progress","started_at":"2026-09-25T22:26:08Z"},{"id":"mc-b","status":"open"}]`
	beads, err := parseBeads([]byte(blob))
	if err != nil {
		t.Fatalf("parseBeads: %v", err)
	}
	if want := time.Date(2026, 9, 25, 22, 26, 8, 0, time.UTC); !beads[0].Started.Equal(want) {
		t.Errorf("Started = %v, want %v", beads[0].Started, want)
	}
	if !beads[1].Started.IsZero() {
		t.Errorf("absent started_at = %v, want zero", beads[1].Started)
	}
}

func TestParseBeadsUpdatedAt(t *testing.T) {
	blob := `[{"id":"mc-a","status":"closed","updated_at":"2026-10-05T09:01:02Z"},{"id":"mc-b","status":"open"}]`
	beads, err := parseBeads([]byte(blob))
	if err != nil {
		t.Fatalf("parseBeads: %v", err)
	}
	if want := time.Date(2026, 10, 5, 9, 1, 2, 0, time.UTC); !beads[0].Updated.Equal(want) {
		t.Errorf("Updated = %v, want %v", beads[0].Updated, want)
	}
	if !beads[1].Updated.IsZero() {
		t.Errorf("absent updated_at = %v, want zero", beads[1].Updated)
	}
}

// BlockedBy rides the same list read: only the Ordering edges
// (blocks, conditional-blocks) are kept, in edge order; membership and lineage
// edges are ignored, and a row with no dependencies key parses to nil.
func TestParseBeadsBlockedBy(t *testing.T) {
	blob := `[
	 {"id":"mc-a","status":"open","dependencies":[
	   {"issue_id":"mc-a","depends_on_id":"mc-b","type":"blocks"},
	   {"issue_id":"mc-a","depends_on_id":"mc-p","type":"parent-child"},
	   {"issue_id":"mc-a","depends_on_id":"mc-c","type":"conditional-blocks"},
	   {"issue_id":"mc-a","depends_on_id":"mc-s","type":"caused-by"},
	   {"issue_id":"mc-a","depends_on_id":"mc-d","type":"discovered-from"}]},
	 {"id":"mc-e","status":"open","dependencies":[{"issue_id":"mc-e","depends_on_id":"mc-p","type":"parent-child"}]},
	 {"id":"mc-f","status":"open"}]`
	beads, err := parseBeads([]byte(blob))
	if err != nil {
		t.Fatalf("parseBeads: %v", err)
	}
	for i, want := range [][]string{{"mc-b", "mc-c"}, nil, nil} {
		if got := beads[i].BlockedBy; !reflect.DeepEqual(got, want) {
			t.Errorf("%s BlockedBy = %#v, want %#v", beads[i].ID, got, want)
		}
	}
}

// close_reason rides the same JSON-to-Bead mapping (Spec FR6); a bead
// without the key parses to "".
func TestParseBeadsCloseReason(t *testing.T) {
	blob := `[{"id":"mc-a","status":"closed","close_reason":"superseded by mc-b"},{"id":"mc-b","status":"open"}]`
	beads, err := parseBeads([]byte(blob))
	if err != nil {
		t.Fatalf("parseBeads: %v", err)
	}
	if got := beads[0].CloseReason; got != "superseded by mc-b" {
		t.Errorf("CloseReason = %q, want %q", got, "superseded by mc-b")
	}
	if got := beads[1].CloseReason; got != "" {
		t.Errorf("absent close_reason = %q, want empty", got)
	}
}

// related edges ride the list JSON too (Spec FR6): a Rollback's target
// is readable from the snapshot; other edge types stay out of Related.
func TestParseBeadsRelated(t *testing.T) {
	blob := `[{"id":"ws-rb","dependencies":[{"depends_on_id":"ws-cp","type":"related"},{"depends_on_id":"mc-x","type":"blocks"},{"depends_on_id":"mc-y","type":"related"}]}]`
	beads, err := parseBeads([]byte(blob))
	if err != nil {
		t.Fatalf("parseBeads: %v", err)
	}
	if got := beads[0].Related; !slices.Equal(got, []string{"ws-cp", "mc-y"}) {
		t.Errorf("Related = %v, want [ws-cp mc-y]", got)
	}
}

// The claim is a status CAS (ADR 0052): open → in_progress with assignee = the
// resolved actor, guarded by --if-status open, no --claim (so no lease). A bead
// already in_progress — even one we hold — fails and is left unchanged: the fake
// CLI answers like bd's guard mismatch (exit 13, nothing written).
func TestClaimIsStatusCAS(t *testing.T) {
	oactor := actor
	t.Cleanup(func() { actor = oactor })
	actor = func() string { return "alice" }
	status := map[string]string{"ws-open": "open", "ws-held": "in_progress"}
	assignee := map[string]string{"ws-held": "alice"}
	var argv []string
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(args ...string) (string, error) {
		argv = args
		id := args[1]
		if status[id] != "open" {
			return "", errors.New("bd update: exit status 13")
		}
		status[id], assignee[id] = "in_progress", args[len(args)-1]
		return "", nil
	}
	if err := Claim("ws-open"); err != nil {
		t.Fatalf("Claim(open) = %v", err)
	}
	if want := []string{"update", "ws-open", "--if-status", "open", "--status", "in_progress", "--assignee", "alice"}; !reflect.DeepEqual(argv, want) {
		t.Errorf("argv = %v, want %v", argv, want)
	}
	if status["ws-open"] != "in_progress" || assignee["ws-open"] != "alice" {
		t.Errorf("open bead after claim = %s/%s", status["ws-open"], assignee["ws-open"])
	}
	if err := Claim("ws-held"); err == nil {
		t.Error("claiming a bead we already hold must fail")
	}
	if status["ws-held"] != "in_progress" || assignee["ws-held"] != "alice" {
		t.Errorf("held bead changed: %s/%s", status["ws-held"], assignee["ws-held"])
	}
}

// Actor resolution mirrors bd's: $BEADS_ACTOR, then git user.name, then $USER.
func TestActorFrom(t *testing.T) {
	for _, c := range []struct{ env, git, user, want string }{
		{"env", "git", "user", "env"},
		{"", "git", "user", "git"},
		{"", "", "user", "user"},
		{"", "", "", ""},
	} {
		if got := actorFrom(c.env, func() string { return c.git }, c.user); got != c.want {
			t.Errorf("actorFrom(%q,%q,%q) = %q, want %q", c.env, c.git, c.user, got, c.want)
		}
	}
}

// The carry classifier maps bd 1.3.0's verbatim dep-add refusals (research note
// "How bd treats blocking edges we'd carry", issueops/errors.go) to their skip;
// anything else is not a skip.
func TestCarrySkipReason(t *testing.T) {
	for _, c := range []struct{ stderr, want string }{
		{"mc-x cannot be blocked by its ancestor ws-cp: ws-cp cannot close until its descendants finish, so the gate would never clear", "hierarchy"},
		{"mc-x cannot be blocked by its descendant ws-cp: blocked status cascades to descendants, so ws-cp would inherit the block and never close", "hierarchy"},
		{"adding dependency would create a cycle", "cycle"},
		{`dependency mc-x -> ws-cp already exists with type "related" (requested "blocks"); remove it first with 'bd dep remove' then re-add`, "type-clash"},
		{"issue mc-x not found", ""},
		{"database is locked", ""},
	} {
		err := errors.New("bd dep add mc-x ws-cp -t blocks: exit status 1: Error: " + c.stderr)
		if got := carrySkipReason(err); got != c.want {
			t.Errorf("carrySkipReason(%q) = %q, want %q", c.stderr, got, c.want)
		}
	}
}

// Carry reads pred's dependents once, carries only blocks/conditional-blocks
// (each with its own type), skips closed and succ itself, maps known dep-add
// refusals to skips, and never removes an edge (Spec D1).
func TestCarry(t *testing.T) {
	list := `[
	 {"id":"mc-a","status":"open","dependency_type":"blocks"},
	 {"id":"mc-b","status":"deferred","dependency_type":"conditional-blocks"},
	 {"id":"ws-rel","status":"open","dependency_type":"related"},
	 {"id":"ws-wf","status":"open","dependency_type":"waits-for"},
	 {"id":"ws-pc","status":"open","dependency_type":"parent-child"},
	 {"id":"ws-done","status":"closed","dependency_type":"blocks"},
	 {"id":"ws-cp","status":"open","dependency_type":"blocks"},
	 {"id":"ws-kid","status":"open","dependency_type":"blocks"},
	 {"id":"ws-loop","status":"in_progress","dependency_type":"blocks"},
	 {"id":"ws-lin","status":"open","dependency_type":"blocks"}
	]`
	addErr := map[string]string{
		"ws-kid":  "ws-kid cannot be blocked by its ancestor ws-cp: ws-cp cannot close until its descendants finish, so the gate would never clear",
		"ws-loop": "adding dependency would create a cycle",
		"ws-lin":  `dependency ws-lin -> ws-cp already exists with type "caused-by" (requested "blocks"); remove it first with 'bd dep remove' then re-add`,
	}
	var calls [][]string
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(args ...string) (string, error) {
		calls = append(calls, args)
		if args[1] == "list" {
			return list, nil
		}
		if msg := addErr[args[2]]; msg != "" {
			return "", errors.New("bd dep add: exit status 1: Error: " + msg)
		}
		return "", nil
	}
	moved, skipped, err := Carry("ws-spec", "ws-cp")
	if err != nil {
		t.Fatalf("Carry: %v", err)
	}
	if want := []string{"mc-a", "mc-b"}; !slices.Equal(moved, want) {
		t.Errorf("moved = %v, want %v", moved, want)
	}
	wantSkip := []CarrySkip{{"ws-done", "closed"}, {"ws-cp", "hierarchy"}, {"ws-kid", "hierarchy"}, {"ws-loop", "cycle"}, {"ws-lin", "type-clash"}}
	if !reflect.DeepEqual(skipped, wantSkip) {
		t.Errorf("skipped = %v, want %v", skipped, wantSkip)
	}
	want := [][]string{
		{"dep", "list", "ws-spec", "--direction=up", "--json"},
		{"dep", "add", "mc-a", "ws-cp", "-t", "blocks"},
		{"dep", "add", "mc-b", "ws-cp", "-t", "conditional-blocks"},
		{"dep", "add", "ws-kid", "ws-cp", "-t", "blocks"},
		{"dep", "add", "ws-loop", "ws-cp", "-t", "blocks"},
		{"dep", "add", "ws-lin", "ws-cp", "-t", "blocks"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("calls = %v\nwant %v", calls, want)
	}
	for _, c := range calls {
		if c[1] == "remove" {
			t.Errorf("Carry must never remove an edge: %v", c)
		}
	}
}

// An unknown dep-add error returns at once with what landed; a dep-list failure
// returns err; no dependents is nil/nil/nil.
func TestCarryErrors(t *testing.T) {
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(args ...string) (string, error) {
		if args[1] == "list" {
			return `[{"id":"mc-a","status":"open","dependency_type":"blocks"},
			 {"id":"mc-b","status":"open","dependency_type":"blocks"},
			 {"id":"mc-c","status":"open","dependency_type":"blocks"}]`, nil
		}
		if args[2] == "mc-b" {
			return "", errors.New("bd dep add: exit status 1: Error: database is locked")
		}
		return "", nil
	}
	moved, skipped, err := Carry("mc-p", "mc-s")
	if err == nil || !slices.Equal(moved, []string{"mc-a"}) || skipped != nil {
		t.Errorf("unknown dep-add error = %v,%v,%v; want [mc-a],nil,err", moved, skipped, err)
	}

	run = func(...string) (string, error) { return "", errors.New("bd dep list: exit status 1") }
	if moved, skipped, err := Carry("mc-p", "mc-s"); err == nil || moved != nil || skipped != nil {
		t.Errorf("dep-list failure = %v,%v,%v; want nil,nil,err", moved, skipped, err)
	}

	run = func(...string) (string, error) { return "[]", nil }
	if moved, skipped, err := Carry("mc-p", "mc-s"); moved != nil || skipped != nil || err != nil {
		t.Errorf("empty = %v,%v,%v; want nil,nil,nil", moved, skipped, err)
	}
}
