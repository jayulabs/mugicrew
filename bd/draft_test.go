package bd

import (
	"bytes"
	"errors"
	"log/slog"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/contract"
)

// A Draft is born complete in one bd create: the plain argv plus --status
// deferred and drafted-by:<creator> riding the one --labels value, so the bead
// still carries exactly one kind: label (ADR 0059 §2, Spec D3).
func TestDraftCreateArgs(t *testing.T) {
	s := Successor{Kind: contract.KindBuild, Title: "t", Description: "d", DraftedBy: "ws-spec"}
	for name, args := range map[string][]string{
		"successor":  successorCreateArgs("ws-pred", s),
		"child":      childCreateArgs("ws-cp", s),
		"discovered": discoveredCreateArgs("ws-pv", "ws-pred", s),
	} {
		if !hasPair(args, "--status", "deferred") {
			t.Errorf("%s: a Draft must be created --status deferred: %v", name, args)
		}
		if !hasPair(args, "--labels", "kind:build,drafted-by:ws-spec") {
			t.Errorf("%s: a Draft must carry its kind and drafted-by:<creator> in one --labels: %v", name, args)
		}
		kinds := 0
		for _, a := range args {
			kinds += strings.Count(a, "kind:")
		}
		if kinds != 1 {
			t.Errorf("%s: want exactly one kind: label, got %d: %v", name, kinds, args)
		}
		if indexOf(args, "--no-inherit-labels") < 0 {
			t.Errorf("%s: a Draft keeps --no-inherit-labels: %v", name, args)
		}
	}
	// No creator → not a Draft: the argv is unchanged.
	plain := successorCreateArgs("ws-pred", Successor{Kind: contract.KindBuild, Title: "t", Description: "d"})
	if indexOf(plain, "--status") >= 0 || !hasPair(plain, "--labels", "kind:build") {
		t.Errorf("a non-Draft create must stay as it was: %v", plain)
	}
}

func hasPair(args []string, flag, val string) bool {
	i := indexOf(args, flag)
	return i >= 0 && i+1 < len(args) && args[i+1] == val
}

func indexOf(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

// A Draft counts as open everywhere (ADR 0059 §3), so the idempotency guard
// adopts a surviving Draft instead of spawning a duplicate.
func TestDraftCountsAsOpen(t *testing.T) {
	if !(Bead{Status: "deferred"}).Open() {
		t.Error("Open() must be true for a Draft (status deferred)")
	}
	kids := []Bead{{ID: "mc-d", Kind: contract.KindChartConflict, Status: "deferred"}}
	if id, ok := openChildOfKind(kids, contract.KindChartConflict); !ok || id != "mc-d" {
		t.Errorf("openChildOfKind = (%q,%v), want (mc-d,true)", id, ok)
	}
}

// The publish batch orders edges → status=open → the finishing close (Spec D4).
func TestPublishScript(t *testing.T) {
	got := publishScript([]Bead{{ID: "mc-a", Title: "A"}, {ID: "mc-b", Title: "Fix Spec: wording"}},
		[]Edge{{From: "mc-b", To: "mc-a", Type: "blocks"}, {From: "ws-cp", To: "ws-pv"}},
		&Closing{ID: "ws-spec", Reason: `sliced into "2" Builds`})
	want := "dep add mc-b mc-a blocks\n" +
		"dep add ws-cp ws-pv\n" +
		"update mc-a status=open\n" +
		"update mc-b status=open\n" +
		`close ws-spec "sliced into \"2\" Builds"` + "\n"
	if got != want {
		t.Errorf("script =\n%s\nwant\n%s", got, want)
	}
	// Spec D2: a prefixed Draft's title is stripped in its open line,
	// quoted like the close reason and flattened to one line.
	if got := publishScript([]Bead{{ID: "mc-c", Title: "Critique: say \"hi\" \\ there\nnow"}}, nil, nil); got != `update mc-c status=open title="say \"hi\" \\ there now"`+"\n" {
		t.Errorf("stripped title: %q", got)
	}
	if got := publishScript(nil, nil, nil); got != "" {
		t.Errorf("nothing to publish must be an empty script, got %q", got)
	}
	// A reason with a newline must not split into a second batch line.
	if got := publishScript(nil, nil, &Closing{ID: "mc-x", Reason: "a\nb"}); got != "close mc-x \"a b\"\n" {
		t.Errorf("multi-line reason: %q", got)
	}
	if got := publishScript(nil, nil, &Closing{ID: "mc-x"}); got != "close mc-x\n" {
		t.Errorf("empty reason: %q", got)
	}
}

// fakeDrafts swaps run and runBatch: run answers the Drafts list and records
// every other call; runBatch records the script.
func fakeDrafts(t *testing.T, listJSON string, batchErr, labelErr error) (calls *[][]string, scripts *[]string) {
	t.Helper()
	orun, obatch := run, runBatch
	t.Cleanup(func() { run, runBatch = orun, obatch })
	calls, scripts = &[][]string{}, &[]string{}
	run = func(args ...string) (string, error) {
		*calls = append(*calls, args)
		switch args[0] {
		case "list":
			return listJSON, nil
		case "label":
			return "", labelErr
		}
		return "", nil
	}
	runBatch = func(script string) error {
		*scripts = append(*scripts, script)
		return batchErr
	}
	return calls, scripts
}

func TestPublish(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	calls, scripts := fakeDrafts(t, `[{"id":"mc-a","title":"Spec: Foo misses bar","status":"deferred"},{"id":"mc-b","title":"Bar","status":"deferred"}]`, nil, errors.New("label gone"))
	ids, err := Publish("ws-spec", nil, &Closing{ID: "ws-spec", Reason: "sliced"})
	if err != nil {
		t.Fatalf("Publish = %v (label removal is best-effort)", err)
	}
	if !reflect.DeepEqual(ids, []string{"mc-a", "mc-b"}) {
		t.Errorf("published = %v", ids)
	}
	if want := []string{"list", "--label", "drafted-by:ws-spec", "--status", "deferred", "-n", "0", "--json"}; !reflect.DeepEqual((*calls)[0], want) {
		t.Errorf("Drafts argv = %v, want %v", (*calls)[0], want)
	}
	if len(*scripts) != 1 || !strings.Contains((*scripts)[0], "update mc-a status=open title=\"Foo misses bar\"\n") ||
		!strings.Contains((*scripts)[0], "update mc-b status=open\n") {
		t.Errorf("want one batch publishing the Drafts, mc-a's title stripped, got %q", *scripts)
	}
	if out := logs.String(); strings.Count(out, "title.stripped") != 1 || !strings.Contains(out, "id=mc-a") {
		t.Errorf("want one title.stripped event for mc-a, log = %q", out)
	}
	if want := []string{"label", "remove", "mc-a", "mc-b", "drafted-by:ws-spec"}; !reflect.DeepEqual((*calls)[len(*calls)-1], want) {
		t.Errorf("label removal = %v, want %v", (*calls)[len(*calls)-1], want)
	}
}

func TestPublishNothingIsNoOp(t *testing.T) {
	calls, scripts := fakeDrafts(t, `[]`, nil, nil)
	if ids, err := Publish("mc-x", nil, nil); err != nil || len(ids) != 0 {
		t.Fatalf("Publish = (%v,%v)", ids, err)
	}
	if len(*scripts) != 0 || len(*calls) != 1 {
		t.Errorf("no Drafts, no close → no batch, no label calls; got scripts %q calls %v", *scripts, *calls)
	}
}

// A failed batch rolls back in bd; Publish reports it and leaves labels alone.
func TestPublishBatchFailure(t *testing.T) {
	calls, _ := fakeDrafts(t, `[{"id":"mc-a","status":"deferred"}]`, errors.New("line 1: not found"), nil)
	if _, err := Publish("mc-x", nil, nil); err == nil {
		t.Fatal("a failed batch must fail Publish")
	}
	for _, c := range *calls {
		if c[0] == "label" {
			t.Errorf("no label removal after a failed batch: %v", c)
		}
	}
}

func TestDeleteDrafts(t *testing.T) {
	calls, _ := fakeDrafts(t, `[{"id":"mc-a","status":"deferred"},{"id":"mc-b","status":"deferred"}]`, nil, nil)
	ids, _, err := DeleteDrafts("mc-x")
	if err != nil || !reflect.DeepEqual(ids, []string{"mc-a", "mc-b"}) {
		t.Fatalf("DeleteDrafts = (%v,%v)", ids, err)
	}
	if want := []string{"delete", "mc-a", "mc-b", "--force"}; !reflect.DeepEqual((*calls)[1], want) {
		t.Errorf("delete argv = %v, want %v", (*calls)[1], want)
	}
	calls, _ = fakeDrafts(t, `[]`, nil, nil)
	if _, _, err := DeleteDrafts("mc-x"); err != nil || len(*calls) != 1 {
		t.Errorf("no Drafts → no delete; calls %v err %v", *calls, err)
	}
}

// Deleting Drafts also deletes each container Draft's unworked
// branch, after the bead delete; a branch with work, or one a worktree holds, is
// kept; no branch, a non-container kind, or a published container is untouched.
func TestDeleteDraftsDeletesUnworkedContainerBranches(t *testing.T) {
	newGitRepo(t)
	for _, b := range []string{"chart/ws-fresh", "checkpoint/ws-work", "chart/ws-held", "chart/ws-pub", "build/mc-b"} {
		gitT(t, "branch", b)
	}
	gitT(t, "checkout", "-q", "checkpoint/ws-work")
	gitT(t, "commit", "-q", "--allow-empty", "-m", "work")
	gitT(t, "checkout", "-q", "main")
	gitT(t, "worktree", "add", "-q", t.TempDir()+"/wt", "chart/ws-held")
	list := `[{"id":"ws-fresh","status":"deferred","labels":["kind:chart"]},
		{"id":"ws-work","status":"deferred","labels":["kind:checkpoint"]},
		{"id":"ws-held","status":"deferred","labels":["kind:chart"]},
		{"id":"ws-nocut","status":"deferred","labels":["kind:chart"]},
		{"id":"mc-b","status":"deferred","labels":["kind:build"]}]`

	// A failed bead delete touches no branch.
	fakeDrafts(t, list, nil, nil)
	inner := run
	run = func(args ...string) (string, error) {
		if args[0] == "delete" {
			return "", errors.New("db locked")
		}
		return inner(args...)
	}
	if _, outs, err := DeleteDrafts("mc-x"); err == nil || outs != nil || !hasBranch("chart/ws-fresh") {
		t.Fatalf("failed delete: outs %v err %v; branch must stay", outs, err)
	}

	fakeDrafts(t, list, nil, nil)
	ids, outs, err := DeleteDrafts("mc-x")
	if err != nil || len(ids) != 5 {
		t.Fatalf("DeleteDrafts = (%v,%v)", ids, err)
	}
	got := map[string]string{}
	for _, o := range outs {
		got[o.Branch] = "deleted"
		if o.Err != nil {
			got[o.Branch] = o.Err.Error()
		}
	}
	if len(got) != 3 || got["chart/ws-fresh"] != "deleted" || got["checkpoint/ws-work"] != "has work" ||
		!strings.Contains(got["chart/ws-held"], "worktree") {
		t.Errorf("outcomes = %v", got)
	}
	for b, want := range map[string]bool{"chart/ws-fresh": false, "checkpoint/ws-work": true, "chart/ws-held": true, "chart/ws-pub": true, "build/mc-b": true} {
		if hasBranch(b) != want {
			t.Errorf("branch %s exists = %v, want %v", b, !want, want)
		}
	}
}

func newGitRepo(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	gitT(t, "init", "-q", "-b", "main")
	gitT(t, "config", "user.email", "t@example.com")
	gitT(t, "config", "user.name", "t")
	gitT(t, "commit", "-q", "--allow-empty", "-m", "root")
}

func gitT(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func hasBranch(b string) bool {
	return exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/heads/"+b).Run() == nil
}

// ADR 0079 §5: a chart-conflict's escalation publishes its grilling Draft and the
// conflict blocks→grilling gate in ONE batch, with no close — the conflict stays
// open, blocked. In an effort the edge goes to the tail instead.
func TestEdgesOnPublishLandsDraftAndGateInOneBatch(t *testing.T) {
	_, scripts := fakeDrafts(t, `[{"id":"ws-grill","status":"deferred"}]`, nil, nil)
	gate := []Edge{{From: "ws-cf", To: "ws-grill", Type: "blocks"}}
	if err := EdgesOnPublish(nil, "ws-cf", gate); err != nil {
		t.Fatalf("EdgesOnPublish: %v", err)
	}
	if want := []string{"dep add ws-cf ws-grill blocks\nupdate ws-grill status=open\n"}; !reflect.DeepEqual(*scripts, want) {
		t.Errorf("batches = %q, want %q", *scripts, want)
	}

	ctx := &publishingCtx{}
	if err := EdgesOnPublish(ctx, "ws-cf", gate); err != nil {
		t.Fatalf("EdgesOnPublish in an effort: %v", err)
	}
	if len(*scripts) != 1 || !reflect.DeepEqual(ctx.edges, gate) || len(ctx.closes) != 0 {
		t.Errorf("an effort must hand the gate to its tail and close nothing, got batches %q edges %v closes %v", *scripts, ctx.edges, ctx.closes)
	}
}
