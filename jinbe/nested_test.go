package jinbe

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/looplock"
)

// nestedWS builds a Workspace declaring a repo context "child" (gitignored child repo
// with checkpoint/ws-cp one commit ahead of main) and chdirs into a land tree: a
// Workspace host detached at main plus the child detached at the branch tip, as
// session.CutTree cuts it. It returns the child's main checkout.
func nestedWS(t *testing.T) (child git.Repo, g func(dir string, args ...string) string) {
	t.Helper()
	g = func(dir string, args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@e.com", "-c", "user.name=t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	ws := t.TempDir()
	g(ws, "init", "-q", "-b", "main")
	os.MkdirAll(filepath.Join(ws, ".mugicrew"), 0o755)
	os.WriteFile(filepath.Join(ws, ".mugicrew", "config.toml"), []byte("[context.child]\nrepo = \"child\"\ngate = \"childgate\"\n"), 0o644)
	os.WriteFile(filepath.Join(ws, ".gitignore"), []byte("child/\n"), 0o644)
	g(ws, "add", ".")
	g(ws, "commit", "-q", "-m", "root")
	cdir := filepath.Join(ws, "child")
	os.Mkdir(cdir, 0o755)
	g(cdir, "init", "-q", "-b", "main")
	g(cdir, "commit", "-q", "--allow-empty", "-m", "root")
	g(cdir, "checkout", "-q", "-b", "checkpoint/ws-cp")
	os.WriteFile(filepath.Join(cdir, "f.txt"), []byte("f\n"), 0o644)
	g(cdir, "add", ".")
	g(cdir, "commit", "-q", "-m", "work")
	g(cdir, "checkout", "-q", "main")

	host := filepath.Join(t.TempDir(), "host")
	g(ws, "worktree", "add", "-q", "--detach", host, "main")
	child = git.Repo(cdir)
	if _, err := child.MaterializeDetached(filepath.Join(host, "child"), "checkpoint/ws-cp"); err != nil {
		t.Fatal(err)
	}
	t.Chdir(host)
	return child, g
}

func nestedSeams(t *testing.T, r *jinbeSeamRec, label string) {
	t.Helper()
	r.gated = true
	stubJinbeSeams(t, r)
	cp := openCheckpoint()
	cp.Labels = []string{"context:child", label}
	showBead = func(string) (bd.Bead, error) { return cp, nil }
	oGate := gateBuildAt
	t.Cleanup(func() { gateBuildAt = oGate })
}

// A nested Checkpoint lands on the child's main through the child seams and the
// context's own gate; its events carry context=/repo= with into naming the child's
// trunk (Spec FR 31, 37, 49).
func TestNestedLandingLandsInTheChildThroughTheContextGate(t *testing.T) {
	child, g := nestedWS(t)
	wsMain := g(filepath.Dir(string(child)), "rev-parse", "main")
	r := &jinbeSeamRec{}
	evs := eventlogtest.Capture(t)
	nestedSeams(t, r, "repo:child")
	tip := g(string(child), "rev-parse", "checkpoint/ws-cp")
	var gotCommon, gotAttr string
	gateBuildAt = func(_ context.Context, common, _, _, attr string) (string, error) {
		gotCommon, gotAttr = common, attr
		return "/nix/store/out", nil
	}

	if err := reconcileLanding(openLanding(tip)); err != nil {
		t.Fatal(err)
	}
	sha, err := child.LandedBase(checkpointTrailer("ws-cp"))
	if err != nil {
		t.Fatalf("trailer not on the child's main: %v", err)
	}
	if got := g(string(child), "rev-parse", "main"); got != sha {
		t.Errorf("child main = %s, want the land %s", got, sha)
	}
	if g(filepath.Dir(string(child)), "rev-parse", "main") != wsMain {
		t.Error("Workspace main moved")
	}
	if want := filepath.Join(string(child), ".git"); gotCommon != want || gotAttr != "childgate" {
		t.Errorf("gate built (%q, %q), want the child's common dir %q and its own attr", gotCommon, gotAttr, want)
	}
	if !slices.Equal(r.closed, []string{"ws-land"}) || r.reasons[0] != "landed: "+sha {
		t.Errorf("closed %v %v, want the Landing landed: %s", r.closed, r.reasons, sha)
	}
	var land eventlogtest.Event
	for _, e := range *evs {
		if e.Name == "land.done" {
			land = e
		}
	}
	if a := land.Attrs; a["context"] != "child" || a["repo"] != "child" || a["into"] != "main" {
		t.Errorf("land.done attrs = %v, want context=child repo=child into=main", a)
	}
	if len(r.landed) != 0 {
		t.Errorf("the Workspace's landBranch seam ran: %v", r.landed)
	}
}

// A repo: stamp that disagrees with the live repo = makes Jinbe return without
// acting: nothing lands, nothing closes (Spec FR 17).
func TestNestedLandingRefusesOnStampDrift(t *testing.T) {
	child, g := nestedWS(t)
	r := &jinbeSeamRec{}
	nestedSeams(t, r, "repo:elsewhere")
	tip := g(string(child), "rev-parse", "checkpoint/ws-cp")
	gateBuildAt = func(context.Context, string, string, string, string) (string, error) {
		t.Error("gate ran under drift")
		return "", nil
	}
	if err := reconcileLanding(openLanding(tip)); err != nil {
		t.Fatal(err)
	}
	if _, err := child.LandedBase(checkpointTrailer("ws-cp")); err == nil {
		t.Error("landed under drift")
	}
	if len(r.closed) != 0 || r.composes != 0 {
		t.Errorf("acted under drift: closed %v composes %d", r.closed, r.composes)
	}
}

// landNestedCheckpoint lands checkpoint/ws-cp on the child's main from the land tree
// nestedWS cut, and returns the squash.
func landNestedCheckpoint(t *testing.T, child git.Repo) string {
	t.Helper()
	tree := git.Repo(filepath.Join(".", "child"))
	composed, base, pin, err := tree.Compose("checkpoint/ws-cp", checkpointTrailer("ws-cp"), "")
	if err != nil {
		t.Fatal(err)
	}
	sha, err := child.Land("checkpoint/ws-cp", checkpointTrailer("ws-cp"), tree.LandSameBase(composed, base, pin))
	if err != nil {
		t.Fatal(err)
	}
	return sha
}

// A nested Rollback reverts the landed Checkpoint on the child's main and cuts and
// seeds the successor in the child; its revert event carries context=/repo= with
// into naming the child's trunk (Spec FR 13, 17, 49).
func TestNestedRollbackRevertsAndReappliesInTheChild(t *testing.T) {
	child, g := nestedWS(t)
	squash := landNestedCheckpoint(t, child)
	wsMain := g(filepath.Dir(string(child)), "rev-parse", "main")
	evs := eventlogtest.Capture(t)
	r := rollbackRec{target: "ws-cp", targetSt: "closed", targetLabels: []string{"context:child", "repo:child"}, body: "broke"}
	stubRollbackSeams(t, &r)

	if err := reconcileRollback(r.rollback()); err != nil {
		t.Fatal(err)
	}
	rev, err := child.LandedBase(git.RevertsTrailer("checkpoint", "ws-cp"))
	if err != nil {
		t.Fatalf("no revert on the child's main: %v", err)
	}
	if got := g(string(child), "rev-parse", "main"); got != rev {
		t.Errorf("child main = %s, want the revert %s (squash %s)", got, rev, squash)
	}
	if tip, err := child.Head("checkpoint/ws-new"); err != nil || tip == rev {
		t.Errorf("successor branch in the child = (%q, %v), want cut and seeded past the revert", tip, err)
	}
	if g(filepath.Dir(string(child)), "rev-parse", "main") != wsMain {
		t.Error("Workspace main moved")
	}
	if len(r.reverted) != 0 || len(r.branches) != 0 {
		t.Errorf("the Workspace seams ran: reverts %v branches %v", r.reverted, r.branches)
	}
	if len(r.successors) != 1 || r.successors[0].Context != "child" || r.successors[0].Repo != "child" {
		t.Errorf("successors %+v, want one copying context and repo stamp", r.successors)
	}
	var rv eventlogtest.Event
	for _, e := range *evs {
		if e.Name == "revert.done" {
			rv = e
		}
	}
	if a := rv.Attrs; a["context"] != "child" || a["repo"] != "child" || a["into"] != "main" {
		t.Errorf("revert.done attrs = %v, want context=child repo=child into=main", a)
	}
}

// A drifted repo: stamp makes a nested Rollback return without reverting or closing.
func TestNestedRollbackRefusesOnStampDrift(t *testing.T) {
	child, g := nestedWS(t)
	squash := landNestedCheckpoint(t, child)
	r := rollbackRec{target: "ws-cp", targetSt: "closed", targetLabels: []string{"context:child", "repo:elsewhere"}, body: "broke"}
	stubRollbackSeams(t, &r)
	rb := r.rollback()
	rb.Labels = []string{"context:child", "repo:elsewhere"}
	if err := reconcileRollback(rb); err != nil {
		t.Fatal(err)
	}
	if got := g(string(child), "rev-parse", "main"); got != squash {
		t.Errorf("child main = %s, want unmoved at %s", got, squash)
	}
	if len(r.closed) != 0 || len(r.successors) != 0 {
		t.Errorf("acted under drift: closed %v successors %v", r.closed, r.successors)
	}
}

// A failed gate Critique names the attr that was built and the repo it was built in,
// never the Workspace's #mugicrew.
func TestGateCritiqueBodyNamesAttrAndChild(t *testing.T) {
	b := &looplock.BuilderError{Drv: "x.drv", Log: "boom\n"}
	got := gateCritiqueBody(b, "base1", "tip1", legs{gateAttr: func() string { return "lib" }, dir: "child"})
	for _, want := range []string{"#lib", "nix build .#lib -L", "child"} {
		if !strings.Contains(got, want) {
			t.Errorf("body missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "mugicrew") {
		t.Errorf("nested body mentions mugicrew:\n%s", got)
	}
}

// Jinbe judges a nested Rollback's drift on the Rollback's own context:/repo: copy,
// the stamp the tick scan holds it on, so relabelling the bead the pause line names
// lets Jinbe act, and relabelling only the closed target does not (mc-3hri.8.43).
func TestNestedRollbackJudgesDriftOnItsOwnStamp(t *testing.T) {
	for _, tc := range []struct {
		name, rbRepo, targetRepo string
		acts                     bool
	}{
		{"rollback relabelled to the live dir", "repo:child", "repo:old", true},
		{"only the target relabelled", "repo:old", "repo:child", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			child, _ := nestedWS(t)
			landNestedCheckpoint(t, child)
			r := rollbackRec{target: "ws-cp", targetSt: "closed", targetLabels: []string{"context:child", tc.targetRepo}, body: "broke"}
			stubRollbackSeams(t, &r)
			rb := r.rollback()
			rb.Labels = []string{"context:child", tc.rbRepo}
			if err := reconcileRollback(rb); err != nil {
				t.Fatal(err)
			}
			if acted := len(r.successors) == 1; acted != tc.acts {
				t.Errorf("acted = %v, want %v", acted, tc.acts)
			}
			if tc.acts && r.successors[0].Repo != "child" {
				t.Errorf("successor Repo = %q, want child", r.successors[0].Repo)
			}
		})
	}
}

// One deciding bead (the stamped Rollback, else the target) drives drift, address and
// the successor's context/repo pair.
func TestNestedRollbackUsesOneDecidingBead(t *testing.T) {
	run := func(t *testing.T, cfg string, rbLabels, targetLabels []string) *rollbackRec {
		child, _ := nestedWS(t)
		landNestedCheckpoint(t, child)
		if cfg != "" {
			if err := os.WriteFile(filepath.Join(filepath.Dir(string(child)), ".mugicrew", "config.toml"), []byte(cfg), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		r := rollbackRec{target: "ws-cp", targetSt: "closed", targetLabels: targetLabels, body: "broke"}
		stubRollbackSeams(t, &r)
		rb := r.rollback()
		rb.Labels = rbLabels
		if err := reconcileRollback(rb); err != nil {
			t.Fatal(err)
		}
		return &r
	}
	t.Run("unstamped rollback, drifted target holds", func(t *testing.T) {
		r := run(t, "", []string{"context:child"}, []string{"context:child", "repo:old"})
		if len(r.successors) != 0 || len(r.reverted) != 0 {
			t.Errorf("acted: successors %d reverts %d, want none", len(r.successors), len(r.reverted))
		}
	})
	t.Run("rollback relabelled to a renamed context reverts in its child", func(t *testing.T) {
		r := run(t, "[context.lib]\nrepo = \"child\"\n", []string{"context:lib", "repo:child"}, []string{"context:child", "repo:child"})
		if len(r.successors) != 1 { // the nested path reverts through the child's own seam, not revertCommit
			t.Fatalf("successors %d, want 1 (reasons %v)", len(r.successors), r.reasons)
		}
		if s := r.successors[0]; s.Context != "lib" || s.Repo != "child" {
			t.Errorf("successor = %q/%q, want lib/child", s.Context, s.Repo)
		}
	})
	t.Run("successor pair comes from the target when the rollback is unstamped", func(t *testing.T) {
		r := run(t, "", []string{"context:other"}, []string{"context:child", "repo:child"})
		if s := r.successors[0]; s.Context != "child" || s.Repo != "child" {
			t.Errorf("successor = %q/%q, want child/child", s.Context, s.Repo)
		}
	})
}

// nestedGitlabRollback wires a nested Rollback under gitlab: the child's landed
// Checkpoint, fetches that return the child's real main, and the child's project.
func nestedGitlabRollback(t *testing.T, body string) (*rollbackRec, git.Repo, string, func(dir string, args ...string) string) {
	t.Helper()
	child, g := nestedWS(t)
	squash := landNestedCheckpoint(t, child)
	r := &rollbackRec{target: "ws-cp", targetSt: "closed", targetLabels: []string{"context:child", "repo:child"}, body: body, gitlab: true}
	stubRollbackSeams(t, r)
	landStrategy = func() (string, git.LandStrategy, error) { return "gitlab", nil, nil }
	fetchForge = func(repo git.Repo) (string, error) {
		r.fetches++
		r.fetchRepos = append(r.fetchRepos, repo)
		return g(string(repo), "rev-parse", "main"), nil
	}
	return r, child, squash, g
}

// A nested gitlab Rollback fetches, reverts and pushes its revert MR in the child and
// against the child's project, records MR: <group/proj>!<iid>, and once the revert is
// merged cuts the successor branch in the child; no step touches the Workspace's main.
func TestNestedGitlabRollbackRevertsThroughTheChildsMR(t *testing.T) {
	r, child, squash, g := nestedGitlabRollback(t, "broke")
	wsMain := g(filepath.Dir(string(child)), "rev-parse", "main")
	evs := eventlogtest.Capture(t)

	if err := reconcileRollback(r.rollback()); err != nil {
		t.Fatal(err)
	}
	if len(r.fetchRepos) == 0 || slices.ContainsFunc(r.fetchRepos, func(f git.Repo) bool { return f != child }) {
		t.Errorf("fetched %v, want only the child", r.fetchRepos)
	}
	if len(r.pushRepos) != 1 || r.pushRepos[0] != child {
		t.Errorf("pushRevertMR repos %v, want the child", r.pushRepos)
	}
	if !strings.Contains(r.body, "MR: ns/child!9") {
		t.Errorf("body %q, want MR: ns/child!9", r.body)
	}
	if len(r.reverted) != 0 {
		t.Errorf("the Workspace's revertOnto ran: %v", r.reverted)
	}

	// the human merges the revert MR: its trailer lands on the child's main
	if _, err := child.Revert(squash, git.RevertsTrailer("checkpoint", "ws-cp")); err != nil {
		t.Fatal(err)
	}
	rb := r.rollback()
	rb.Description = r.body
	if err := reconcileRollback(rb); err != nil {
		t.Fatal(err)
	}
	if _, err := child.Head("checkpoint/ws-new"); err != nil {
		t.Errorf("successor branch not cut in the child: %v", err)
	}
	if len(r.branches) != 0 {
		t.Errorf("the Workspace seams cut %v", r.branches)
	}
	if g(filepath.Dir(string(child)), "rev-parse", "main") != wsMain {
		t.Error("Workspace main moved")
	}
	for _, e := range *evs {
		if strings.HasPrefix(e.Name, "revert.") {
			if a := e.Attrs; a["context"] != "child" || a["repo"] != "child" || a["into"] != "main" {
				t.Errorf("%s attrs = %v, want context=child repo=child into=main", e.Name, a)
			}
		}
	}
}

// D10 parity with the Landing: a child remote on another host refuses a Rollback before
// any fetch, push or MR call.
func TestNestedGitlabRollbackRefusesAChildOnAnotherHost(t *testing.T) {
	r, _, _, _ := nestedGitlabRollback(t, "broke")
	oProj := projectOf
	t.Cleanup(func() { projectOf = oProj })
	projectOf = func(repo git.Repo) (forge.Project, error) {
		if repo != "" {
			return forge.Project{Host: "other.test", ID: "ns%2Fchild"}, nil
		}
		return workspaceProject, nil
	}
	var d *git.DifferentHost
	if err := reconcileRollback(r.rollback()); !errors.As(err, &d) {
		t.Fatalf("err = %v, want *git.DifferentHost", err)
	}
	if r.fetches != 0 || len(r.pushRepos) != 0 {
		t.Errorf("fetches %d pushes %v, want nothing before the refusal", r.fetches, r.pushRepos)
	}
}

// With an open !5 in both projects, closing a nested Rollback's MR closes only the child's.
func TestNestedGitlabRollbackClosesOnlyTheChildsMR(t *testing.T) {
	r, child, squash, _ := nestedGitlabRollback(t, "broke\nMR: ns/child!5\n")
	r.mr = &forge.MR{IID: 5, State: forge.StateOpened}
	if _, err := child.Revert(squash, git.RevertsTrailer("checkpoint", "ws-cp")); err != nil {
		t.Fatal(err)
	}
	if err := reconcileRollback(r.rollback()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.mrClosed, []int{5}) || len(r.projects) == 0 || slices.ContainsFunc(r.projects, func(p forge.Project) bool { return p != childProject }) {
		t.Errorf("closed %v in projects %v, want only the child's !5", r.mrClosed, r.projects)
	}
}

// An invalid nested Rollback (target lacks its land trailer, or was reopened) that closes
// its open recorded revert MR logs revert.mr_closed with the nested attrs (Spec FR 30).
func TestNestedGitlabRollbackInvalidCloseLogsNestedAttrs(t *testing.T) {
	for name, setup := range map[string]func(r *rollbackRec){
		"no land trailer": func(r *rollbackRec) { r.target = "ws-untrailered" },
		"target reopened": func(r *rollbackRec) { r.targetSt = "open" },
	} {
		t.Run(name, func(t *testing.T) {
			r, _, _, _ := nestedGitlabRollback(t, "broke\nMR: ns/child!5\n")
			r.mr = &forge.MR{IID: 5, State: forge.StateOpened}
			setup(r)
			evs := eventlogtest.Capture(t)
			if err := reconcileRollback(r.rollback()); err != nil {
				t.Fatal(err)
			}
			e := eventNamed(*evs, "revert.mr_closed")
			if e.Attrs["context"] != "child" || e.Attrs["repo"] == nil || e.Attrs["into"] != "main" {
				t.Errorf("revert.mr_closed = %v, want context/repo/into", e)
			}
		})
	}
}

// nestedGitlab wires a nested Landing under gitlab on the real strategy: the child
// fetches, the pending Landing names the child's project and its MR line is qualified.
func nestedGitlab(t *testing.T, r *jinbeSeamRec) (tip string, pushes *[]string, child git.Repo, g func(dir string, args ...string) string) {
	t.Helper()
	child, g = nestedWS(t)
	nestedSeams(t, r, "repo:child")
	r.gated = false
	tip = g(string(child), "rev-parse", "checkpoint/ws-cp")
	landStrategy = func() (string, git.LandStrategy, error) { return "gitlab", git.LandLocal, nil }
	oStrategy := strategyFor
	t.Cleanup(func() { strategyFor = oStrategy })
	strategyFor = func(string, git.Repo) (git.LandStrategy, error) { return git.LandLocal, nil }
	oPinned := pinnedGitlab
	t.Cleanup(func() { pinnedGitlab = oPinned })
	pinnedGitlab = func(repo git.Repo, _ string, p git.MRPresentation) git.LandStrategy {
		r.presentation = p
		r.fetchRepos = append(r.fetchRepos, repo) // marks the repo the push strategy is bound to
		return func(string, string) (string, error) { return "", r.landErr }
	}
	pushes, _ = graphPushRec(t, nil)
	return tip, pushes, child, g
}

// A nested Landing under gitlab fetches the child, records MR: <group/proj>!<iid>, logs
// the nested attrs and pushes the Graph ref once, from the Workspace (D6, D12, FR 1, 24, 30).
func TestGitlabNestedLandingIsPendingOnTheChildsProject(t *testing.T) {
	r := &jinbeSeamRec{landErr: &git.LandPending{MR: 5, Pushed: true, Opened: true}}
	evs := eventlogtest.Capture(t)
	tip, pushes, _, _ := nestedGitlab(t, r)
	r.head = tip
	if err := reconcileLanding(openLanding(tip)); err != nil {
		t.Fatal(err)
	}
	if r.fetches != 1 || len(r.fetchRepos) != 2 || r.fetchRepos[0] == "" || r.fetchRepos[1] != r.fetchRepos[0] {
		t.Errorf("fetches %d repos %v, want a child fetch and the push bound to the same child", r.fetches, r.fetchRepos)
	}
	if len(r.bodies) != 1 || !strings.Contains(r.bodies[0], "MR: ns/child!5") {
		t.Errorf("bodies %q, want MR: ns/child!5", r.bodies)
	}
	if len(*pushes) != 1 {
		t.Errorf("Graph pushes %v, want one (the Workspace's)", *pushes)
	}
	if r.presentation.Title == "" {
		t.Errorf("nested Landing got presentation %+v, want the container's title", r.presentation)
	}
	var e eventlogtest.Event
	for _, ev := range *evs {
		if ev.Name == "land.pending" {
			e = ev
		}
	}
	if a := e.Attrs; a["context"] != "child" || a["into"] != "main" || a["project"] != nil {
		t.Errorf("land.pending attrs = %v, want nested attrs and no project", a)
	}
}

// With both projects holding an open !5, a nested arm touches only the child's.
func TestGitlabNestedArmsTouchOnlyTheChildsMR(t *testing.T) {
	r := &jinbeSeamRec{mr: &forge.MR{IID: 5, State: "opened", HeadPipeline: &forge.Pipeline{ID: 9, Status: forge.StatusFailed, SHA: "x"}}}
	tip, _, _, _ := nestedGitlab(t, r)
	r.head = tip
	l := openLanding(tip)
	l.Description += "\nMR: ns/child!5\nChecked: forgesha\n"
	if err := reconcileLanding(l); err != nil {
		t.Fatal(err)
	}
	if len(r.projects) == 0 || slices.ContainsFunc(r.projects, func(p forge.Project) bool { return p != childProject }) {
		t.Errorf("projects = %v, want only the child's", r.projects)
	}
}

// A child remote on another host refuses with the typed error before any fetch or MR call.
func TestGitlabNestedLandingRefusesAChildOnAnotherHost(t *testing.T) {
	r := &jinbeSeamRec{}
	tip, _, _, _ := nestedGitlab(t, r)
	oProj := projectOf
	t.Cleanup(func() { projectOf = oProj })
	projectOf = func(repo git.Repo) (forge.Project, error) {
		if repo != "" {
			return forge.Project{Host: "other.test", ID: "ns%2Fchild"}, nil
		}
		return workspaceProject, nil
	}
	var d *git.DifferentHost
	if err := reconcileLanding(openLanding(tip)); !errors.As(err, &d) {
		t.Fatalf("err = %v, want *git.DifferentHost", err)
	}
	if r.fetches != 0 || r.mrReads != 0 || len(r.landed) != 0 {
		t.Errorf("fetches %d mrReads %d landed %v, want nothing before the refusal", r.fetches, r.mrReads, r.landed)
	}
}

// A foreign Graph: trailer on the child's mainline holds the container on a Task instead
// of landing or closing it (ADR 0187, FR 8, 9).
func TestGitlabNestedForeignLandHoldsOnATask(t *testing.T) {
	r := &jinbeSeamRec{}
	tip, _, child, g := nestedGitlab(t, r)
	r.head = tip
	oName := git.LandStrategyName
	t.Cleanup(func() { git.LandStrategyName = oName })
	git.LandStrategyName = func() (string, error) { return git.LandStrategyGitlab, nil }
	g(string(child), "commit", "-q", "--allow-empty", "-m", "land\n\nCheckpoint: ws-cp\nGraph: g99")
	var foreign *git.ForeignLanding
	if err := reconcileLanding(openLanding(tip)); !errors.As(err, &foreign) || foreign.Theirs != "g99" {
		t.Fatalf("err = %v, want *git.ForeignLanding g99", err)
	}
	if len(r.tasks) != 1 || len(r.closed) != 0 || len(r.landed) != 0 {
		t.Errorf("tasks %v closed %v landed %v, want one hold Task and nothing closed", r.tasks, r.closed, r.landed)
	}
}

// A 429 on a nested Landing or Rollback logs land/revert.rate_limited with the nested
// attrs; a Workspace one carries no context attr (Spec FR 30, 33).
func TestNestedRateLimitedCarriesNestedAttrs(t *testing.T) {
	attrs := func(evs *[]eventlogtest.Event, name string) map[string]any {
		for _, e := range *evs {
			if e.Name == name {
				return e.Attrs
			}
		}
		t.Fatalf("no %s event in %v", name, eventlogtest.Names(*evs))
		return nil
	}
	t.Run("landing", func(t *testing.T) {
		r := &jinbeSeamRec{mrErr: forge.ErrRateLimited}
		evs := eventlogtest.Capture(t)
		tip, _, _, _ := nestedGitlab(t, r)
		r.head = tip
		l := openLanding(tip)
		l.Description += "\nMR: ns/child!5\n"
		if err := reconcileLanding(l); err != nil {
			t.Fatal(err)
		}
		if a := attrs(evs, "land.rate_limited"); a["context"] != "child" || a["repo"] != "child" || a["into"] != "main" {
			t.Errorf("attrs = %v, want context=child repo=child into=main", a)
		}
	})
	t.Run("rollback", func(t *testing.T) {
		r, _, _, _ := nestedGitlabRollback(t, "broke\nMR: ns/child!9\n")
		r.mrErr = forge.ErrRateLimited
		evs := eventlogtest.Capture(t)
		if err := reconcileRollback(r.rollback()); err != nil {
			t.Fatal(err)
		}
		if a := attrs(evs, "revert.rate_limited"); a["context"] != "child" || a["repo"] != "child" || a["into"] != "main" {
			t.Errorf("attrs = %v, want context=child repo=child into=main", a)
		}
	})
	t.Run("invalid rollback", func(t *testing.T) {
		r, _, _, _ := nestedGitlabRollback(t, "broke\nMR: ns/child!9\n")
		r.targetSt = "open"
		r.mrErr = forge.ErrRateLimited
		rateLimited.Delete(r.rollback().ID) // the sibling subtest logged this bead once already
		evs := eventlogtest.Capture(t)
		if err := reconcileRollback(r.rollback()); err != nil {
			t.Fatal(err)
		}
		if a := attrs(evs, "revert.rate_limited"); a["context"] != "child" || a["repo"] != "child" || a["into"] != "main" {
			t.Errorf("attrs = %v, want context=child repo=child into=main", a)
		}
	})
}

// D10 on the invalid-close path: a nested Rollback whose target reopened and whose child
// remote moved to another host refuses before any MR read or close.
func TestNestedGitlabInvalidRollbackRefusesAChildOnAnotherHost(t *testing.T) {
	r, _, _, _ := nestedGitlabRollback(t, "broke\nMR: ns/child!5\n")
	r.targetSt = "open"
	r.mr = &forge.MR{IID: 5, State: forge.StateOpened}
	oProj := projectOf
	t.Cleanup(func() { projectOf = oProj })
	projectOf = func(repo git.Repo) (forge.Project, error) {
		if repo != "" {
			return forge.Project{Host: "other.test", ID: "ns%2Fchild"}, nil
		}
		return workspaceProject, nil
	}
	var d *git.DifferentHost
	if err := reconcileRollback(r.rollback()); !errors.As(err, &d) {
		t.Fatalf("err = %v, want *git.DifferentHost", err)
	}
	if r.mrReads != 0 || len(r.mrClosed) != 0 {
		t.Errorf("mrReads %d closed %v, want no MR call", r.mrReads, r.mrClosed)
	}
}
