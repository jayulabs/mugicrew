package bd

import (
	"errors"
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

func TestSuccessorCreateArgsLeafIsErrand(t *testing.T) {
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

// closed_at is the real close time (the Initiatives tab's Closed order); an open
// bead carries none and parses to the zero time.
func TestParseBeadsClosedAt(t *testing.T) {
	blob := `[{"id":"mc-a","status":"closed","closed_at":"2026-10-01T10:00:00Z"},{"id":"mc-b","status":"open"}]`
	beads, err := parseBeads([]byte(blob))
	if err != nil {
		t.Fatalf("parseBeads: %v", err)
	}
	if want := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC); !beads[0].Closed.Equal(want) {
		t.Errorf("Closed = %v, want %v", beads[0].Closed, want)
	}
	if !beads[1].Closed.IsZero() {
		t.Errorf("open bead Closed = %v, want zero", beads[1].Closed)
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

// discovered-from edges parse oldest edge first, ties on target id, whatever bd's array
// order; the other edge fields are unchanged with them mixed in (Spec D1).
func TestParseBeadsDiscoveredFromOldestFirst(t *testing.T) {
	blob := `[{"id":"ws-pv","dependencies":[
	 {"depends_on_id":"z","type":"discovered-from","created_at":"2026-01-02T00:00:00Z"},
	 {"depends_on_id":"mc-x","type":"blocks","created_at":"2026-01-09T00:00:00Z"},
	 {"depends_on_id":"b","type":"discovered-from","created_at":"2026-01-03T00:00:00Z"},
	 {"depends_on_id":"y","type":"related"},
	 {"depends_on_id":"a","type":"discovered-from","created_at":"2026-01-03T00:00:00Z"},
	 {"depends_on_id":"old","type":"discovered-from","created_at":"2026-01-01T00:00:00Z"},
	 {"depends_on_id":"c","type":"caused-by"}]}]`
	beads, err := parseBeads([]byte(blob))
	if err != nil {
		t.Fatalf("parseBeads: %v", err)
	}
	b := beads[0]
	if want := []string{"old", "z", "a", "b"}; !slices.Equal(b.DiscoveredFrom, want) {
		t.Errorf("DiscoveredFrom = %v, want %v", b.DiscoveredFrom, want)
	}
	if !slices.Equal(b.BlockedBy, []string{"mc-x"}) || !slices.Equal(b.Related, []string{"y"}) || b.CausedBy != "c" {
		t.Errorf("other edges changed: %+v", b)
	}
}

// The caused-by edge rides the list JSON too (Spec D10): the Graph links an
// Initiative's member chains off the snapshot.
func TestParseBeadsCausedBy(t *testing.T) {
	blob := `[{"id":"ws-ch","dependencies":[{"depends_on_id":"ws-in","type":"parent-child"},{"depends_on_id":"ws-sig","type":"caused-by"}]},{"id":"ws-sig"}]`
	beads, err := parseBeads([]byte(blob))
	if err != nil {
		t.Fatalf("parseBeads: %v", err)
	}
	if beads[0].CausedBy != "ws-sig" || beads[1].CausedBy != "" {
		t.Errorf("CausedBy = %q, %q, want ws-sig, \"\"", beads[0].CausedBy, beads[1].CausedBy)
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

// The actor is read from bd (`bd create --dry-run --json` created_by), and an
// unreadable bd falls back to the bare name.
func TestReadActor(t *testing.T) {
	orun := run
	t.Cleanup(func() { run = orun })
	t.Setenv("BEADS_ACTOR", "")
	t.Setenv("USER", "bare")
	for _, c := range []struct {
		name, out string
		err       error
		want      string
		degraded  bool
	}{
		{"configured device", `{"created_by":"alice@laptop"}`, nil, "alice@laptop", false},
		{"bare name", `{"created_by":"alice"}`, nil, "alice", false},
		{"unreadable bd", "", errors.New("boom"), "bare", true},
		{"no created_by", `{}`, nil, "bare", true},
	} {
		run = func(...string) (string, error) { return c.out, c.err }
		if got, degraded := readActor(); got != c.want || degraded != c.degraded {
			t.Errorf("%s: readActor = %q,%v; want %q,%v", c.name, got, degraded, c.want, c.degraded)
		}
	}
}

// A degraded read is cached too: with bd unreadable, two calls run bd once and
// return the same bare name.
func TestCachedActorCachesDegradedRead(t *testing.T) {
	orun := run
	t.Cleanup(func() { run = orun })
	t.Setenv("BEADS_ACTOR", "")
	t.Setenv("USER", "bare")
	calls := 0
	run = func(...string) (string, error) { calls++; return "", errors.New("boom") }
	get := cachedActor()
	n1, d1 := get()
	n2, d2 := get()
	if calls != 1 || n1 != "bare" || n2 != n1 || !d1 || !d2 {
		t.Errorf("calls=%d got %q,%v then %q,%v; want 1 call, bare,true twice", calls, n1, d1, n2, d2)
	}
}

// Against a real bd in a scratch HOME (never the user's ~/.config/bd) and a
// scratch Bead Graph (never the devShell's BEADS_DIR, the real graph), the config
// actor: key is read, and $BEADS_ACTOR overrides it.
// scratchActorGraph points HOME at a user config setting actor: me@box, then
// inits a scratch Bead Graph (BEADS_DIR inside a fresh git repo, which is the cwd)
// and confirms it is the one in force, so a probe never touches the real graph.
func scratchActorGraph(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("no bd on PATH")
	}
	home := t.TempDir()
	cfg := filepath.Join(home, ".config", "bd")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "config.yaml"), []byte("actor: me@box\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	scratch := t.TempDir()
	if out, err := exec.Command("git", "-C", scratch, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Setenv("BEADS_DIR", filepath.Join(scratch, ".beads"))
	t.Chdir(scratch)
	if out, err := exec.Command("bd", "init", "--prefix", "t", "--non-interactive").CombinedOutput(); err != nil {
		t.Skipf("bd init in a scratch graph: %v\n%s", err, out)
	}
	if out, err := exec.Command("bd", "config", "get", "issue_prefix").Output(); err != nil || strings.TrimSpace(string(out)) != "t" {
		t.Fatalf("scratch graph not in force (issue_prefix = %q, %v); refusing to probe the real Bead Graph", out, err)
	}
}

func TestReadActorRealBd(t *testing.T) {
	scratchActorGraph(t)
	for env, want := range map[string]string{"": "me@box", "zed@dev": "zed@dev"} {
		t.Setenv("BEADS_ACTOR", env)
		if got, degraded := readActor(); got != want || degraded {
			t.Errorf("BEADS_ACTOR=%q: readActor = %q,%v; want %q", env, got, degraded, want)
		}
	}
}

// A Workspace whose bd validates task descriptions on create must not make the
// probe fail into the degraded bare name: the configured actor still reads back.
func TestReadActorRealBdValidatingWorkspace(t *testing.T) {
	scratchActorGraph(t)
	t.Setenv("BEADS_ACTOR", "")
	if out, err := exec.Command("bd", "config", "set", "validation.on-create", "error").CombinedOutput(); err != nil {
		t.Fatalf("bd config set: %v: %s", err, out)
	}
	if got, degraded := readActor(); got != "me@box" || degraded {
		t.Errorf("readActor = %q,%v; want me@box,false", got, degraded)
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
		if got := CarrySkipReason(err); got != c.want {
			t.Errorf("CarrySkipReason(%q) = %q, want %q", c.stderr, got, c.want)
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
		if args[0] == "show" { // the container the hold is placed from: no Initiative
			return `[{"id":"ws-cp","title":"cp","status":"open","labels":["kind:checkpoint"]}]`, nil
		}
		if args[1] == "list" {
			if args[3] == "--direction=down" {
				return "[]", nil
			}
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
		{"dep", "list", "ws-spec", "--direction=down", "-t", "blocks", "--json"},
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

// Carry also makes succ blocked by each OPEN kind:latch blocker of pred, and by
// nothing else incoming: closed latches and non-latch blockers stay behind (Spec D6).
func TestCarryLatches(t *testing.T) {
	orun := run
	t.Cleanup(func() { run = orun })
	var adds [][]string
	run = func(args ...string) (string, error) {
		switch {
		case args[0] == "show":
			return `[{"id":"ws-cp","status":"open","labels":["kind:checkpoint"]}]`, nil
		case args[1] == "add":
			adds = append(adds, args)
		case args[3] == "--direction=down":
			return `[{"id":"lt-open","status":"open","labels":["kind:latch"]},
			 {"id":"lt-paused","status":"open","labels":["kind:latch","paused:failing"]},
			 {"id":"lt-done","status":"closed","labels":["kind:latch"]},
			 {"id":"mc-blk","status":"open","labels":["kind:build"]}]`, nil
		}
		return "[]", nil
	}
	moved, skipped, err := Carry("ws-spec", "ws-cp")
	if err != nil || skipped != nil {
		t.Fatalf("Carry = %v,%v,%v", moved, skipped, err)
	}
	if want := []string{"lt-open", "lt-paused"}; !slices.Equal(moved, want) {
		t.Errorf("moved = %v, want %v", moved, want)
	}
	if want := [][]string{{"dep", "add", "ws-cp", "lt-open", "-t", "blocks"}, {"dep", "add", "ws-cp", "lt-paused", "-t", "blocks"}}; !reflect.DeepEqual(adds, want) {
		t.Errorf("adds = %v, want %v", adds, want)
	}
}

// carryInitiativeRun serves a Latch step whose successor wears kind label succKind
// (or whose show fails when showErr is set), and records the dep adds.
func carryInitiativeRun(t *testing.T, succKind string, showErr bool) *[][]string {
	orun := run
	t.Cleanup(func() { run = orun })
	adds := &[][]string{}
	run = func(args ...string) (string, error) {
		switch {
		case args[0] == "show":
			if showErr {
				return "", errors.New("bd show: exit status 1: database is locked")
			}
			return `[{"id":"ws-succ","status":"open","labels":["kind:` + succKind + `"]}]`, nil
		case args[1] == "add":
			*adds = append(*adds, args)
		case args[3] == "--direction=down":
			return `[{"id":"lt-open","status":"open","labels":["kind:latch"]},
			 {"id":"lt-done","status":"closed","labels":["kind:latch"]},
			 {"id":"mc-blk","status":"open","labels":["kind:build"]}]`, nil
		case args[3] == "--direction=up":
			return `[{"id":"mc-a","status":"open","dependency_type":"blocks"}]`, nil
		}
		return "[]", nil
	}
	return adds
}

// An Initiative successor takes no Latch edge: the open Latch is skipped as
// initiative, closed Latches and other blockers are not reported, and the outgoing
// ordering still carries (ADR 0348 §8, ADR 0085).
func TestCarrySkipsLatchOntoInitiative(t *testing.T) {
	adds := carryInitiativeRun(t, "initiative", false)
	moved, skipped, err := Carry("ws-sig", "ws-succ")
	if err != nil {
		t.Fatalf("Carry: %v", err)
	}
	if want := []string{"mc-a"}; !slices.Equal(moved, want) {
		t.Errorf("moved = %v, want %v", moved, want)
	}
	if want := []CarrySkip{{"lt-open", "initiative"}}; !reflect.DeepEqual(skipped, want) {
		t.Errorf("skipped = %v, want %v", skipped, want)
	}
	if want := [][]string{{"dep", "add", "mc-a", "ws-succ", "-t", "blocks"}}; !reflect.DeepEqual(*adds, want) {
		t.Errorf("adds = %v, want %v", *adds, want)
	}
}

func TestCarryTargetsSkipsLatchOntoInitiative(t *testing.T) {
	adds := carryInitiativeRun(t, "initiative", false)
	moved, skipped, err := CarryTargets("ws-sig", "ws-ord", "ws-succ")
	if err != nil {
		t.Fatalf("CarryTargets: %v", err)
	}
	if want := []string{"mc-a"}; !slices.Equal(moved, want) {
		t.Errorf("moved = %v, want %v", moved, want)
	}
	if want := []CarrySkip{{"lt-open", "initiative"}}; !reflect.DeepEqual(skipped, want) {
		t.Errorf("skipped = %v, want %v", skipped, want)
	}
	if want := [][]string{{"dep", "add", "mc-a", "ws-ord", "-t", "blocks"}}; !reflect.DeepEqual(*adds, want) {
		t.Errorf("adds = %v, want %v", *adds, want)
	}
}

// Every other successor kind still takes the Latch edge.
func TestCarryLatchOntoOtherKinds(t *testing.T) {
	for _, kind := range []string{"chart", "spec", "checkpoint"} {
		adds := carryInitiativeRun(t, kind, false)
		_, skipped, err := Carry("ws-sig", "ws-succ")
		if err != nil || skipped != nil {
			t.Fatalf("%s: Carry = %v,%v", kind, skipped, err)
		}
		if want := []string{"ws-succ", "lt-open"}; len(*adds) != 2 || !slices.Equal((*adds)[1][2:4], want) {
			t.Errorf("%s: adds = %v, want the latch edge", kind, *adds)
		}
	}
}

// A failed read of the successor fails the carry.
func TestCarryLatchSuccessorReadFails(t *testing.T) {
	carryInitiativeRun(t, "chart", true)
	if _, _, err := Carry("ws-sig", "ws-succ"); err == nil {
		t.Fatal("Carry succeeded though the successor could not be read")
	}
}

// CarryTargets sends ordering dependents to one bead and open Latches to another,
// in one pass; neither walk touches the other's target.
func TestCarryTargets(t *testing.T) {
	orun := run
	t.Cleanup(func() { run = orun })
	var adds [][]string
	run = func(args ...string) (string, error) {
		switch {
		case args[0] == "show":
			return `[{"id":"ws-pivot","status":"open","labels":["kind:chart"]}]`, nil
		case args[1] == "add":
			adds = append(adds, args)
		case args[3] == "--direction=down":
			return `[{"id":"lt-open","status":"open","labels":["kind:latch"]}]`, nil
		default:
			return `[{"id":"mc-a","status":"open","dependency_type":"blocks"},{"id":"ws-cp","status":"open","dependency_type":"blocks"}]`, nil
		}
		return "", nil
	}
	moved, skipped, err := CarryTargets("ws-sig", "ws-cp", "ws-pivot")
	if err != nil {
		t.Fatalf("CarryTargets: %v", err)
	}
	if want := []string{"mc-a", "lt-open"}; !slices.Equal(moved, want) {
		t.Errorf("moved = %v, want %v", moved, want)
	}
	if want := []CarrySkip{{"ws-cp", "hierarchy"}}; !reflect.DeepEqual(skipped, want) {
		t.Errorf("skipped = %v, want the host as a hierarchy skip", skipped)
	}
	if want := [][]string{{"dep", "add", "mc-a", "ws-cp", "-t", "blocks"}, {"dep", "add", "ws-pivot", "lt-open", "-t", "blocks"}}; !reflect.DeepEqual(adds, want) {
		t.Errorf("adds = %v, want %v", adds, want)
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

// --parent rides a successor create only when Parent is set (Spec D2), and
// --no-inherit-labels stays either way: the parent's kind: label never leaks.
func TestSuccessorCreateArgsParentOnlyWhenSet(t *testing.T) {
	for _, tc := range []struct {
		parent string
		want   bool
	}{{"", false}, {"ws-init", true}} {
		args := successorCreateArgs("ws-sig", Successor{Kind: contract.KindChart, Container: true, Title: "t", Parent: tc.parent})
		i := slices.Index(args, "--parent")
		if got := i >= 0; got != tc.want {
			t.Errorf("Parent %q: --parent present = %v, want %v (%v)", tc.parent, got, tc.want, args)
		}
		if tc.want && args[i+1] != tc.parent {
			t.Errorf("--parent %q, want %q", args[i+1], tc.parent)
		}
		if !slices.Contains(args, "--no-inherit-labels") {
			t.Errorf("Parent %q: --no-inherit-labels missing (%v)", tc.parent, args)
		}
	}
}

// InitiativeOf copies a bead's parent only when that parent is an unclosed
// Initiative (Spec D2, FR32): no parent, a non-Initiative parent (a Checkpoint's
// Build) or a closed Initiative yields none, and an Initiative is never parented.
func TestInitiativeOf(t *testing.T) {
	parents := map[string]string{
		"ws-open":    `[{"id":"ws-open","status":"open","issue_type":"epic","labels":["kind:initiative"]}]`,
		"ws-draft":   `[{"id":"ws-draft","status":"deferred","issue_type":"epic","labels":["kind:initiative"]}]`,
		"ws-blocked": `[{"id":"ws-blocked","status":"blocked","issue_type":"epic","labels":["kind:initiative"]}]`,
		"ws-closed":  `[{"id":"ws-closed","status":"closed","issue_type":"epic","labels":["kind:initiative"]}]`,
		"ws-cp":      `[{"id":"ws-cp","status":"open","issue_type":"epic","labels":["kind:checkpoint"]}]`,
	}
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(args ...string) (string, error) {
		if len(args) < 2 || args[0] != "show" || parents[args[1]] == "" {
			return "", errors.New("unexpected bd call")
		}
		return parents[args[1]], nil
	}
	for _, tc := range []struct{ parent, want string }{
		{"", ""}, {"ws-open", "ws-open"}, {"ws-draft", "ws-draft"}, {"ws-blocked", "ws-blocked"}, {"ws-closed", ""}, {"ws-cp", ""},
	} {
		got, err := InitiativeOf(Bead{ID: "ws-x", Parent: tc.parent})
		if err != nil || got != tc.want {
			t.Errorf("parent %q: InitiativeOf = %q, %v; want %q", tc.parent, got, err, tc.want)
		}
	}
}

// surveyChild is the one Survey shape both Chart creators seed (Spec D3): a
// kind:survey task (not a container) carrying contract.SurveyBody, a Draft by
// the Chart's own drafter so it publishes and deletes with the Chart.
func TestSurveyChildShape(t *testing.T) {
	want := Successor{Kind: contract.KindSurvey, Title: "Fix the frobnicator", Description: contract.SurveyBody, DraftedBy: "ws-sig"}
	if got := surveyChild("Fix the frobnicator", "ws-sig"); got != want {
		t.Errorf("surveyChild = %+v, want %+v", got, want)
	}
}

// SpawnSurveyIfAbsent spawns one Survey under the Chart, and none when a
// kind:survey child already exists in ANY status — a closed Survey too, which
// SpawnChildIfAbsent's open-only guard would re-spawn past (Spec D3, FR5).
func TestSpawnSurveyIfAbsentGuardsOnAnyStatus(t *testing.T) {
	for _, tc := range []struct {
		name, kids string
		create     bool
	}{
		{"no children", `[]`, true},
		{"only a defog grilling", `[{"id":"ws-ch.2","status":"closed","labels":["kind:grilling"]}]`, true},
		{"open survey", `[{"id":"ws-ch.1","status":"open","labels":["kind:survey"]}]`, false},
		{"draft survey", `[{"id":"ws-ch.1","status":"deferred","labels":["kind:survey"]}]`, false},
		{"closed survey", `[{"id":"ws-ch.1","status":"closed","labels":["kind:survey"]}]`, false},
	} {
		var created [][]string
		orun := run
		run = func(args ...string) (string, error) {
			switch args[0] {
			case "children":
				return tc.kids, nil
			case "create":
				created = append(created, args)
				return "ws-ch.9", nil
			}
			return "", errors.New("unexpected bd call")
		}
		id, made, err := SpawnSurveyIfAbsent("ws-ch", "Fix it", "ws-sig")
		run = orun
		if err != nil || made != tc.create || len(created) != map[bool]int{true: 1, false: 0}[tc.create] {
			t.Errorf("%s: SpawnSurveyIfAbsent = (%q,%v,%v), creates %d; want created=%v once at most", tc.name, id, made, err, len(created), tc.create)
			continue
		}
		if !tc.create {
			if id != "ws-ch.1" {
				t.Errorf("%s: id = %q, want the existing ws-ch.1", tc.name, id)
			}
			continue
		}
		want := childCreateArgs("ws-ch", surveyChild("Fix it", "ws-sig"))
		if id != "ws-ch.9" || !reflect.DeepEqual(created[0], want) {
			t.Errorf("%s: create %v → %q, want %v → ws-ch.9", tc.name, created[0], id, want)
		}
	}
}

// A Landing child also succeeds its Approval: the caused-by edge rides the create
// beside the parent edge, and a plain child carries none.
func TestChildCreateArgsCausedByRidesBesideParent(t *testing.T) {
	args := strings.Join(childCreateArgs("ws-cp", Successor{Kind: contract.KindLanding, Title: "t", CausedBy: "ws-appr"}), " ")
	if !strings.Contains(args, "--deps caused-by:ws-appr") || !strings.Contains(args, "--parent ws-cp") {
		t.Errorf("args = %s, want caused-by:ws-appr and --parent ws-cp", args)
	}
	if plain := strings.Join(childCreateArgs("ws-cp", Successor{Kind: contract.KindConflict, Title: "t"}), " "); strings.Contains(plain, "--deps") {
		t.Errorf("a plain child must carry no deps, got %s", plain)
	}
}

// An open Landing never undrains its Chart; every other open child, a Survey
// included, still counts.
func TestDrainBlockersOverSkipsOpenLanding(t *testing.T) {
	landing := Bead{ID: "l", Kind: contract.KindLanding, Status: "open"}
	if open, held := DrainBlockersOver([]Bead{landing}, nil); open != 0 || len(held) != 0 {
		t.Errorf("open Landing: open %d held %v, want drained", open, IDs(held))
	}
	survey := Bead{ID: "s", Kind: contract.KindSurvey, Status: "open"}
	if open, _ := DrainBlockersOver([]Bead{landing, survey}, nil); open != 1 {
		t.Errorf("Landing + open Survey: open %d, want 1", open)
	}
}

// A closed child holding its effort branch is held, unless it was closed
// container landed (with or without a carrier suffix); a plain obsolete: sweep
// still holds.
func TestDrainBlockersOverSkipsContainerLandedChild(t *testing.T) {
	efforts := &git.EffortSet{Branches: map[string]bool{"a": true, "b": true, "c": true}}
	kids := []Bead{
		{ID: "a", Status: "closed", CloseReason: ContainerLandedReason + "; carried by sig"},
		{ID: "b", Status: "closed", CloseReason: ObsoleteReason + " Spec x superseded"},
		{ID: "c", Status: "closed", CloseReason: "Closed"},
	}
	open, held := DrainBlockersOver(kids, efforts)
	if open != 0 || strings.Join(IDs(held), ",") != "b,c" {
		t.Errorf("open %d held %v, want 0 and [b c]", open, IDs(held))
	}
}

// A closed child is held only when its effort branch exists and is not integrated
// (tip == cut ref, ADR 0394 (a-closed-effort-whose-tip-is-its-cut-ref…)): an open child
// opens the gate whatever its refs, a container-landed close is skipped as before.
func TestDrainGatesReadIntegratedChildrenAsNotHeld(t *testing.T) {
	efforts := &git.EffortSet{
		Branches:   map[string]bool{"integ": true, "ahead": true, "nocut": true, "open": true, "landed": true},
		Integrated: map[string]bool{"integ": true, "open": true, "landed": true},
	}
	member := func(id, status, reason string) Bead {
		return Bead{ID: id, Kind: contract.KindBuild, Status: status, CloseReason: reason}
	}
	for _, tc := range []struct {
		name       string
		kid        Bead
		open, held int
		drained    bool
	}{
		{"closed and integrated", member("integ", "closed", "Closed"), 0, 0, true},
		{"closed and ahead of its cut", member("ahead", "closed", "Closed"), 0, 1, false},
		{"closed with no cut ref", member("nocut", "closed", "Closed"), 0, 1, false},
		{"open", member("open", "open", ""), 1, 0, false},
		{"container landed", member("landed", "closed", ContainerLandedReason), 0, 0, true},
	} {
		kids := []Bead{tc.kid}
		if open, held := DrainBlockersOver(kids, efforts); open != tc.open || len(held) != tc.held {
			t.Errorf("%s: DrainBlockersOver = %d, %v; want %d, %d", tc.name, open, IDs(held), tc.open, tc.held)
		}
		if got := MembersDrainedOver(kids, efforts); got != tc.drained {
			t.Errorf("%s: MembersDrainedOver = %v, want %v", tc.name, got, tc.drained)
		}
	}
}

// CloseLandings closes only the open Landings, each landed: <sha>, via the given closer.
func TestCloseLandingsClosesOnlyOpenOnes(t *testing.T) {
	var got []string
	closer := func(id, reason string) error { got = append(got, id+"|"+reason); return nil }
	ls := []Bead{{ID: "a", Status: "open"}, {ID: "b", Status: "closed"}}
	if err := CloseLandings(ls, "abc", closer); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "a|landed: abc" {
		t.Errorf("closed = %v, want [a|landed: abc]", got)
	}
}

func TestChildCreateArgsDiscoveredFrom(t *testing.T) {
	args := strings.Join(childCreateArgs("ws-cp", Successor{Kind: contract.KindCritique, Title: "t", DiscoveredFrom: "ws-land"}), " ")
	if !strings.Contains(args, "--deps discovered-from:ws-land") {
		t.Errorf("args = %s", args)
	}
}

func TestRepoStampRidesTheCreateLabels(t *testing.T) {
	args := strings.Join(labelArgs(Successor{Kind: contract.KindChart, Context: "x", Repo: "vendor/x"}), " ")
	if !strings.Contains(args, "context:x,repo:vendor/x") {
		t.Errorf("labels %q lack the repo stamp", args)
	}
	if got := RepoStampOf(Bead{Labels: []string{"context:x", "repo:vendor/x"}}); got != "vendor/x" {
		t.Errorf("RepoStampOf = %q", got)
	}
	if got := RepoStampOf(Bead{}); got != "" {
		t.Errorf("RepoStampOf unlabelled = %q", got)
	}
}

func TestNewestLanding(t *testing.T) {
	t0 := time.Unix(100, 0)
	old := Bead{ID: "old", Status: "closed", Closed: t0}
	recent := Bead{ID: "recent", Status: "closed", Closed: t0.Add(time.Hour)}
	open := Bead{ID: "open", Status: "open"}
	for name, c := range map[string]struct {
		in   []Bead
		want string
	}{
		"open wins":     {[]Bead{recent, open, old}, "open"},
		"latest closed": {[]Bead{old, recent}, "recent"},
		"only one":      {[]Bead{old}, "old"},
	} {
		if got, ok := NewestLanding(c.in); !ok || got.ID != c.want {
			t.Errorf("%s: got %q ok=%v, want %q", name, got.ID, ok, c.want)
		}
	}
	if _, ok := NewestLanding(nil); ok {
		t.Error("no Landings must report not found")
	}
}

// The hold's blocks edge rides the create: a failure between two calls would leave
// an Errand blocking nothing, invisible to the (container, key) dedupe.
func TestSpawnBlockingErrandIfAbsentCreatesTheEdgeAtomically(t *testing.T) {
	orig := run
	t.Cleanup(func() { run = orig })
	var calls [][]string
	run = func(args ...string) (string, error) {
		calls = append(calls, args)
		if args[0] == "show" { // the container the hold is placed from: no Initiative
			return `[{"id":"ws-cp","title":"cp","status":"open","labels":["kind:checkpoint"]}]`, nil
		}
		if args[0] == "create" {
			return "ws-errand", nil
		}
		return "[]", nil
	}
	id, created, err := SpawnBlockingErrandIfAbsent("ws-cp", "Closed MR: !3\n", "t", "Closed MR: !3\n")
	if err != nil || !created || id != "ws-errand" {
		t.Fatalf("got %q %v %v", id, created, err)
	}
	for _, c := range calls {
		if c[0] == "dep" && c[1] == "add" {
			t.Errorf("separate edge call %v", c)
		}
		if c[0] == "create" && !strings.Contains(strings.Join(c, " "), "--deps blocks:ws-cp") {
			t.Errorf("create lacks the blocks edge: %v", c)
		}
	}
}

// A hold that outlives its closed Errand re-files related to it (ADR 0338): the
// edge goes to the most recently closed same-key Errand blocking the container.
func TestSpawnBlockingErrandIfAbsentLinksTheMostRecentlyClosedSameKeyErrand(t *testing.T) {
	const key = "Closed MR: !3\n"
	for name, c := range map[string]struct {
		blockers string
		want     string // related target; "" = no edge
		created  bool
	}{
		"one closed":    {`[{"id":"old","status":"closed","labels":["kind:errand"],"description":"Closed MR: !3\n","closed_at":"2026-10-01T10:00:00Z"}]`, "old", true},
		"two closed":    {`[{"id":"old","status":"closed","labels":["kind:errand"],"description":"Closed MR: !3\n","closed_at":"2026-10-01T10:00:00Z"},{"id":"newer","status":"closed","labels":["kind:errand"],"description":"Closed MR: !3\n","closed_at":"2026-10-02T10:00:00Z"}]`, "newer", true},
		"other key":     {`[{"id":"old","status":"closed","labels":["kind:errand"],"description":"Other\n","closed_at":"2026-10-01T10:00:00Z"}]`, "", true},
		"open same key": {`[{"id":"live","status":"open","labels":["kind:errand"],"description":"Closed MR: !3\n"}]`, "", false},
	} {
		orig := run
		var related []string
		run = func(args ...string) (string, error) {
			switch {
			case args[0] == "show":
				return `[{"id":"ws-cp","title":"cp","status":"open","labels":["kind:checkpoint"]}]`, nil
			case args[0] == "dep" && args[1] == "list" && args[2] == "ws-cp":
				return c.blockers, nil
			case args[0] == "dep" && args[1] == "list":
				return "[]", nil
			case args[0] == "dep" && args[1] == "add" && args[4] == "-t" && args[5] == "related":
				related = append(related, args[2]+">"+args[3])
			case args[0] == "create":
				return "ws-new", nil
			}
			return "", nil
		}
		_, created, err := SpawnBlockingErrandIfAbsent("ws-cp", key, "t", key)
		run = orig
		if err != nil || created != c.created {
			t.Fatalf("%s: created=%v err=%v", name, created, err)
		}
		want := []string(nil)
		if c.want != "" {
			want = []string{"ws-new>" + c.want}
		}
		if !slices.Equal(related, want) {
			t.Errorf("%s: related edges = %v, want %v", name, related, want)
		}
	}
}

func TestParseBeadsExternalRef(t *testing.T) {
	beads, err := parseBeads([]byte(`[{"id":"ws-1","labels":["kind:signal"],"external_ref":"jira-ABC-1"},{"id":"ws-2","labels":["kind:signal"]}]`))
	if err != nil {
		t.Fatal(err)
	}
	if beads[0].ExternalRef != "jira-ABC-1" || beads[1].ExternalRef != "" {
		t.Errorf("ExternalRef = %q, %q; want jira-ABC-1, empty", beads[0].ExternalRef, beads[1].ExternalRef)
	}
}

func TestExternalRefEmittedOnlyWhenSet(t *testing.T) {
	for name, args := range map[string][]string{
		"unparented": unparentedArgs(Successor{Kind: "signal", ExternalRef: "jira-ABC-1"}, "t", "b"),
		"successor":  successorCreateArgs("ws-1", Successor{Kind: "signal", ExternalRef: "jira-ABC-1", Title: "t"}),
	} {
		if i := slices.Index(args, "--external-ref"); i < 0 || args[i+1] != "jira-ABC-1" {
			t.Errorf("%s: want --external-ref jira-ABC-1: %v", name, args)
		}
	}
	if slices.Contains(unparentedArgs(Successor{Kind: "signal"}, "t", "b"), "--external-ref") ||
		slices.Contains(successorCreateArgs("ws-1", Successor{Kind: "signal", Title: "t"}), "--external-ref") {
		t.Error("an unset ref must emit no --external-ref")
	}
}

func TestNewSignalWithRefFilesAKindSignalCarryingTheRef(t *testing.T) {
	var got []string
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(args ...string) (string, error) { got = args; return "mc-9", nil }
	if id, err := NewSignalWithRef("ABC-1: t", "b", "jira-ABC-1"); err != nil || id != "mc-9" {
		t.Fatalf("NewSignalWithRef = %q, %v", id, err)
	}
	if i := slices.Index(got, "--external-ref"); i < 0 || got[i+1] != "jira-ABC-1" || !slices.Contains(got, "kind:signal") || !slices.Contains(got, "--no-inherit-labels") {
		t.Errorf("argv = %v", got)
	}
}

func TestCarrierOfPicksInitiativeThenSignalThenNothing(t *testing.T) {
	orun := run
	t.Cleanup(func() { run = orun })
	var got []string
	list := func(out string) { run = func(args ...string) (string, error) { got = args; return out, nil } }
	both := `[{"id":"s","labels":["kind:signal"],"external_ref":"jira-A-1"},{"id":"i","labels":["kind:initiative"],"external_ref":"jira-A-1"}]`
	list(both)
	if b, ok, err := CarrierOf("jira-A-1"); err != nil || !ok || b.ID != "i" {
		t.Errorf("both: %v %v %v, want the Initiative", b.ID, ok, err)
	}
	if i := slices.Index(got, "--external-ref"); i < 0 || got[i+1] != "jira-A-1" {
		t.Errorf("argv = %v", got)
	}
	list(`[{"id":"s","labels":["kind:signal"],"external_ref":"jira-A-1"}]`)
	if b, ok, _ := CarrierOf("jira-A-1"); !ok || b.ID != "s" {
		t.Errorf("signal alone: %v %v", b.ID, ok)
	}
	list(`[]`)
	if _, ok, _ := CarrierOf("jira-A-1"); ok {
		t.Error("no live bead must find no carrier")
	}
}

// OpenBlocked counts a Draft (deferred) or conditional-blocks dependent as open,
// ignores closed ones and non-blocking edges, and surfaces a dep-list error.
func TestOpenBlocked(t *testing.T) {
	orun := run
	t.Cleanup(func() { run = orun })
	for _, tc := range []struct {
		name, reply string
		want        bool
	}{
		{"draft blocks", `[{"status":"deferred","dependency_type":"blocks"}]`, true},
		{"closed blocks", `[{"status":"closed","dependency_type":"blocks"}]`, false},
		{"open parent-child", `[{"status":"open","dependency_type":"parent-child"},{"status":"open","dependency_type":"discovered-from"}]`, false},
		{"open conditional-blocks", `[{"status":"open","dependency_type":"conditional-blocks"}]`, true},
	} {
		run = func(args ...string) (string, error) {
			if want := []string{"dep", "list", "lt", "--direction=up", "--json"}; !slices.Equal(args, want) {
				t.Errorf("args = %v, want %v", args, want)
			}
			return tc.reply, nil
		}
		if got, err := OpenBlocked("lt"); err != nil || got != tc.want {
			t.Errorf("%s: OpenBlocked = %v,%v; want %v", tc.name, got, err, tc.want)
		}
	}
	run = func(...string) (string, error) { return "", errors.New("boom") }
	if _, err := OpenBlocked("lt"); err == nil {
		t.Error("dep-list error not returned")
	}
}

func TestClaimedElsewhere(t *testing.T) {
	t.Cleanup(PinActor("alice@laptop"))
	for a, want := range map[string]bool{"": false, "alice@laptop": false, "alice@desktop": true, "alice": true, "bob@laptop": true} {
		if got := ClaimedElsewhere(a); got != want {
			t.Errorf("ClaimedElsewhere(%q) = %v, want %v", a, got, want)
		}
	}
}

// Claim classifies bd's two claim refusals (texts pinned in
// docs/research/initiative-open-status-checks-under-a-cut-session-claim.md) and
// leaves any other error alone.
func TestClaimClassifiesRefusals(t *testing.T) {
	orun := run
	t.Cleanup(func() { run = orun })
	for _, c := range []struct {
		name, stderr, holder string
		since, held          bool
	}{
		{"held elsewhere", `bd update zz-cbz: exit status 1: cannot reassign zz-cbz: held by "A" (in_progress); coordinate with the holder (bd mail A)
Error: 1 of 1 issues failed to update`, "A", false, true},
		{"held by us", `bd update zz-cbz: exit status 13: Error updating zz-cbz: status mismatch: zz-cbz has status "in_progress", expected "open"`, "", true, false},
		{"closed since", `bd update zz-cbz: exit status 13: Error updating zz-cbz: status mismatch: zz-cbz has status "closed", expected "open"`, "", false, false},
		{"unrelated", "bd update zz-cbz: exit status 1: database is locked", "", false, false},
	} {
		run = func(...string) (string, error) { return "", errors.New(c.stderr) }
		err := Claim("zz-cbz")
		var h *HeldError
		if got := errors.As(err, &h); got != c.held || (got && h.Holder != c.holder) {
			t.Errorf("%s: HeldError = %v %+v", c.name, got, h)
		}
		if got := errors.Is(err, ErrClaimedSince); got != c.since {
			t.Errorf("%s: ErrClaimedSince = %v", c.name, got)
		}
	}
}

func TestParseBeadsBodyFields(t *testing.T) {
	bs, err := parseBeads([]byte(`[{"id":"x","notes":"N","design":"D","acceptance_criteria":"A","comment_count":3}]`))
	if err != nil || len(bs) != 1 {
		t.Fatal(err)
	}
	if b := bs[0]; b.Notes != "N" || b.Design != "D" || b.AcceptanceCriteria != "A" || b.CommentCount != 3 {
		t.Errorf("body fields not decoded: %+v", b)
	}
}

// An open MR Watch never undrains a Chart, like a Landing.
func TestDrainBlockersOverSkipsOpenMRWatch(t *testing.T) {
	watch := Bead{ID: "w", Kind: contract.KindMRWatch, Status: "open"}
	if open, held := DrainBlockersOver([]Bead{watch}, nil); open != 0 || len(held) != 0 {
		t.Errorf("open %d held %v, want an open MR Watch skipped", open, held)
	}
}

// An MR Watch is no Checkpoint member, so it never holds a land.
func TestIsMemberExcludesMRWatch(t *testing.T) {
	if IsMember(contract.KindMRWatch) {
		t.Error("an MR Watch must not be a member")
	}
}

// A pivot Draft's metadata rides the create as --metadata; a Successor without it adds none.
func TestSuccessorCreateArgsMetadata(t *testing.T) {
	s := Successor{Kind: contract.KindPivot, Title: "t", DraftedBy: "ws-sig", Metadata: `{"raise_checkpoint":"ws-cp"}`}
	if args := successorCreateArgs("ws-sig", s); !slices.Contains(args, "--metadata") || !slices.Contains(args, `{"raise_checkpoint":"ws-cp"}`) {
		t.Errorf("args = %q, want --metadata with the JSON", args)
	}
	s.Metadata = ""
	if args := successorCreateArgs("ws-sig", s); slices.Contains(args, "--metadata") {
		t.Errorf("args = %q, want no --metadata", args)
	}
}

// A raise Draft is discovered-from its creator, never caused-by it: no successor edge.
func TestCreateRaiseDraftArgsHaveNoCausedBy(t *testing.T) {
	args := successorCreateArgs("", Successor{Kind: contract.KindCritique, Title: "t", DraftedBy: "ws-errand", Parent: "ws-cp"}, "discovered-from:ws-errand")
	i := slices.Index(args, "--deps")
	if i < 0 || args[i+1] != "discovered-from:ws-errand" {
		t.Errorf("args = %v, want --deps discovered-from:ws-errand only", args)
	}
	if !slices.Contains(args, "drafted-by:ws-errand") && !slices.Contains(args, "kind:critique,drafted-by:ws-errand") {
		t.Errorf("args = %v, want the drafted-by label", args)
	}
}

func TestParseSweptReasonRoundTripsTheSweepWriters(t *testing.T) {
	got, ok := ParseSweptReason(ObsoleteSuperseded("ws-old", "ws-new", "ws-pv"))
	if want := (SweptReason{OldSpec: "ws-old", Successor: "ws-new", Pivot: "ws-pv"}); !ok || got != want {
		t.Errorf("confirm = %+v %v, want %+v", got, ok, want)
	}
	got, ok = ParseSweptReason(ObsoleteEscalated("ws-old", "mc-chart", "ws-pv"))
	if want := (SweptReason{OldSpec: "ws-old", Successor: "mc-chart", Pivot: "ws-pv", Escalated: true}); !ok || got != want {
		t.Errorf("escalate = %+v %v, want %+v", got, ok, want)
	}
}

func TestParseSweptReasonRejectsEveryOtherReason(t *testing.T) {
	for _, r := range []string{
		ContainerLandedReason + "; carried by ws-sig",
		"obsolete: retired Draft",
		"obsolete: free text",
		"obsolete: Spec ws-old superseded by ws-new (pivot ws-pv confirmed) extra",
		"superseded: ws-pv",
		"",
	} {
		if got, ok := ParseSweptReason(r); ok {
			t.Errorf("%q parsed as %+v", r, got)
		}
	}
}

// An open Errand still carrying the old free-form body satisfies the key match, so the
// reformatted filer files no duplicate (Spec D10).
func TestSpawnBlockingErrandIfAbsentMatchesAnOldFormatOpenBody(t *testing.T) {
	orig := run
	t.Cleanup(func() { run = orig })
	run = func(args ...string) (string, error) {
		if args[0] == "create" {
			t.Errorf("filed a duplicate: %v", args)
		}
		return `[{"id":"old","status":"open","labels":["kind:errand"],"description":"MR !3 was closed.\nClosed MR: !3\nFix: reopen it.\n"}]`, nil
	}
	id, created, err := SpawnBlockingErrandIfAbsent("ws-cp", "Closed MR: !3\n", "t", "new body")
	if err != nil || created || id != "old" {
		t.Errorf("got %q %v %v, want the old open Errand", id, created, err)
	}
}

func TestAwaitingUndraft(t *testing.T) {
	mr := "Commit: abc\nMR: !7\n"
	cases := map[string]struct {
		b    Bead
		want bool
	}{
		"open approval with MR":   {Bead{Kind: contract.KindApproval, Status: "open", Description: mr}, true},
		"qualified path":          {Bead{Kind: contract.KindApproval, Status: "open", Description: "MR: g/p!3"}, true},
		"no MR line":              {Bead{Kind: contract.KindApproval, Status: "open", Description: "Commit: abc"}, false},
		"closed approval":         {Bead{Kind: contract.KindApproval, Status: "closed", Description: mr}, false},
		"other kind with MR line": {Bead{Kind: contract.KindCheckpoint, Status: "open", Description: mr}, false},
	}
	for name, c := range cases {
		if got := AwaitingUndraft(c.b); got != c.want {
			t.Errorf("%s: AwaitingUndraft = %v, want %v", name, got, c.want)
		}
	}
}
