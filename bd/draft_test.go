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

	"jayulabs.org/mugicrew/contexts/contextstest"
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

// The publish batch orders edges → open → the finishing close (Spec D4). A prefixed Draft's
// title is stripped in its open op (ADR 0097); an untouched one carries none.
func TestPublishOps(t *testing.T) {
	got := publishOps([]Bead{{ID: "mc-a", Title: "A"}, {ID: "mc-b", Title: "Fix Spec: wording"}},
		[]Edge{{From: "mc-b", To: "mc-a", Type: "blocks"}, {From: "ws-cp", To: "ws-pv"}},
		&Closing{ID: "ws-spec", Reason: "sliced"})
	want := []Op{EdgeOp("mc-b", "mc-a", "blocks"), EdgeOp("ws-cp", "ws-pv", ""), OpenOp("mc-a", ""),
		OpenOp("mc-b", ""), CloseOp("ws-spec", "sliced")}
	if !slices.Equal(got, want) {
		t.Errorf("ops = %+v\nwant %+v", got, want)
	}
	if got := publishOps([]Bead{{ID: "mc-c", Title: "Critique: say hi"}}, nil, nil); !slices.Equal(got, []Op{OpenOp("mc-c", "say hi")}) {
		t.Errorf("stripped title ops = %+v", got)
	}
	if got := publishOps(nil, nil, nil); len(got) != 0 {
		t.Errorf("nothing to publish must be no ops, got %+v", got)
	}
}

// The cli core pipes a batch as one line per op, quoted and flattened to one line.
func TestCliBatchScript(t *testing.T) {
	var got string
	t.Cleanup(swapBatch(func(script string) error { got = script; return nil }))
	ops := []Op{EdgeOp("mc-b", "mc-a", "blocks"), OpenOp("mc-a", ""), OpenOp("mc-c", "say \"hi\" \\ there\nnow"),
		CloseOp("ws-spec", `sliced into "2" Builds`), CloseOp("ws-x", "a\nb"), CloseOp("ws-y", "")}
	if err := (cli{}).Batch(ops); err != nil {
		t.Fatal(err)
	}
	want := "dep add mc-b mc-a blocks\n" +
		"update mc-a status=open\n" +
		`update mc-c status=open title="say \"hi\" \\ there now"` + "\n" +
		`close ws-spec "sliced into \"2\" Builds"` + "\n" +
		`close ws-x "a b"` + "\n" +
		"close ws-y\n"
	if got != want {
		t.Errorf("script =\n%s\nwant\n%s", got, want)
	}
}

func swapBatch(fn func(string) error) (restore func()) {
	old := runBatch
	runBatch = fn
	return func() { runBatch = old }
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

func TestDeleteDrafts(t *testing.T) {
	calls, _ := fakeDrafts(t, `[{"id":"mc-a","status":"deferred"},{"id":"mc-b","status":"deferred"}]`, nil, nil)
	ids, _, err := DeleteDrafts("mc-x", UnderClaim("mc-x"))
	if err != nil || !reflect.DeepEqual(ids, []string{"mc-a", "mc-b"}) {
		t.Fatalf("DeleteDrafts = (%v,%v)", ids, err)
	}
	if want := []string{"delete", "mc-a", "mc-b", "--force"}; !reflect.DeepEqual((*calls)[1], want) {
		t.Errorf("delete argv = %v, want %v", (*calls)[1], want)
	}
	calls, _ = fakeDrafts(t, `[]`, nil, nil)
	if _, _, err := DeleteDrafts("mc-x", UnderClaim("mc-x")); err != nil || len(*calls) != 1 {
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
	if _, outs, err := DeleteDrafts("mc-x", UnderClaim("mc-x")); err == nil || outs != nil || !hasBranch("chart/ws-fresh") {
		t.Fatalf("failed delete: outs %v err %v; branch must stay", outs, err)
	}

	fakeDrafts(t, list, nil, nil)
	ids, outs, err := DeleteDrafts("mc-x", UnderClaim("mc-x"))
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

// ADR 0079 §5: in an effort a chart-conflict's gate edge goes to the tail, and nothing closes.
func TestEdgesOnPublishInAnEffortHandsTheGateToTheTail(t *testing.T) {
	gate := []Edge{{From: "ws-cf", To: "ws-grill", Type: "blocks"}}
	ctx := &publishingCtx{}
	if err := EdgesOnPublish(ctx, "ws-cf", gate); err != nil {
		t.Fatalf("EdgesOnPublish in an effort: %v", err)
	}
	if !reflect.DeepEqual(ctx.edges, gate) || len(ctx.closes) != 0 {
		t.Errorf("an effort must hand the gate to its tail and close nothing, got edges %v closes %v", ctx.edges, ctx.closes)
	}
}

func TestPublishRetriesTransientBatchConflict(t *testing.T) {
	realBatch := runBatch
	fakeDrafts(t, `[{"id":"mc-a","title":"A","status":"deferred"}]`, nil, nil)
	runBatch = realBatch // the retry under test lives in runBatch; fake only its exec
	sleep = func(time.Duration) {}
	orig := runBatchOnce
	t.Cleanup(func() { runBatchOnce, sleep = orig, time.Sleep })
	n := 0
	runBatchOnce = func(string) error {
		if n++; n == 1 {
			return exitErr("Error 1213 (40001): serialization failure")
		}
		return nil
	}
	ids, err := Publish("ws-spec", nil, nil)
	if err != nil || !reflect.DeepEqual(ids, []string{"mc-a"}) || n != 2 {
		t.Fatalf("ids=%v err=%v attempts=%d", ids, err, n)
	}
}

// A nested Draft's branch was cut in the child, so it is judged and deleted there;
// an unlabelled Draft keeps the cwd's repo.
func TestDeleteDraftBranchesActsInTheChild(t *testing.T) {
	ws, kid := contextstest.Workspace(t)
	contextstest.Git(t, kid, "branch", "checkpoint/ws-nested")
	contextstest.Git(t, ws, "branch", "checkpoint/ws-plain")
	hasIn := func(dir, b string) bool {
		return exec.Command("git", "-C", dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+b).Run() == nil
	}
	outs := deleteDraftBranches([]Bead{
		{ID: "ws-nested", Kind: contract.KindCheckpoint, Labels: []string{"kind:checkpoint", contextstest.Label}},
		{ID: "ws-plain", Kind: contract.KindCheckpoint, Labels: []string{"kind:checkpoint"}},
	})
	if len(outs) != 2 || outs[0].Err != nil || outs[1].Err != nil {
		t.Fatalf("outcomes = %v", outs)
	}
	if hasIn(kid, "checkpoint/ws-nested") || hasIn(ws, "checkpoint/ws-plain") {
		t.Error("a Draft branch survived its delete")
	}
}

// A context-labelled Draft in a Workspace declaring no repo = makes no git call.
func TestDraftRepoSubtreeOnlyMakesNoGitCall(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if err := os.MkdirAll(filepath.Join(root, ".mugicrew"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".mugicrew", "config.toml"), []byte("[context.c]\npath = \"sub\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	real, _ := exec.LookPath("git")
	bin, log := t.TempDir(), filepath.Join(t.TempDir(), "calls")
	script := "#!/bin/sh\necho \"$@\" >> " + log + "\nexec " + real + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if r := draftRepo(Bead{Labels: []string{"context:c"}}); r != "" {
		t.Fatalf("draftRepo = %q; want none", r)
	}
	if b, _ := os.ReadFile(log); len(b) != 0 {
		t.Fatalf("draftRepo made git calls:\n%s", b)
	}
}

// A Robin-drafted Initiative (an unparented kind:initiative epic) rides the same
// wrapper paths as a Signal Draft: publish opens both in one batch.
func TestInitiativeDraftPublishesWithSignal(t *testing.T) {
	_, scripts := fakeDrafts(t, `[{"id":"mc-sig","title":"A signal","status":"deferred","labels":["kind:signal"]},{"id":"mc-ini","title":"Big effort","status":"deferred","issue_type":"epic","labels":["kind:initiative"]}]`, nil, nil)
	ids, err := Publish("mc-disc", nil, nil)
	if err != nil || !reflect.DeepEqual(ids, []string{"mc-sig", "mc-ini"}) {
		t.Fatalf("Publish = (%v,%v)", ids, err)
	}
	if len(*scripts) != 1 || !strings.Contains((*scripts)[0], "update mc-sig status=open\n") ||
		!strings.Contains((*scripts)[0], "update mc-ini status=open\n") {
		t.Errorf("want one batch opening both, got %q", *scripts)
	}
}

// Deleting a Robin-drafted Initiative reports no branch outcome, leaves its
// initiative/<id> branch alone and makes no git call.
func TestDeleteDraftsInitiativeHasNoBranchOutcome(t *testing.T) {
	newGitRepo(t)
	gitT(t, "branch", "initiative/mc-ini")
	fakeDrafts(t, `[{"id":"mc-ini","status":"deferred","issue_type":"epic","labels":["kind:initiative"]}]`, nil, nil)
	real, _ := exec.LookPath("git")
	bin, log := t.TempDir(), filepath.Join(t.TempDir(), "calls")
	script := "#!/bin/sh\necho \"$@\" >> " + log + "\nexec " + real + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ids, outs, err := DeleteDrafts("mc-disc", UnderClaim("mc-disc"))
	if err != nil || !reflect.DeepEqual(ids, []string{"mc-ini"}) || len(outs) != 0 {
		t.Fatalf("DeleteDrafts = (%v,%v,%v)", ids, outs, err)
	}
	if b, _ := os.ReadFile(log); len(b) != 0 {
		t.Fatalf("DeleteDrafts made git calls:\n%s", b)
	}
	if !hasBranch("initiative/mc-ini") {
		t.Error("an Initiative Draft must touch no branch")
	}
}
