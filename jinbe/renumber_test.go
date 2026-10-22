package jinbe

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/git"
)

// renumberRec records the renumber mint's graph and git writes, failing once at failAt.
type renumberRec struct {
	jinbeSeamRec
	failAt   string
	succ     []bd.Successor // successors spawned (deduped per predecessor)
	succPred string
	branches map[string]string
	builds   []bd.Successor
	parent   string
}

func (r *renumberRec) crash(leg string) error {
	if r.failAt == leg {
		r.failAt = ""
		return errors.New("crash at " + leg)
	}
	return nil
}

func stubRenumber(t *testing.T, r *renumberRec) {
	t.Helper()
	stubJinbeSeams(t, &r.jinbeSeamRec)
	oSucc, oCut, oBuilds, oInit, oTree, oChild := spawnSuccessor, cutBranch, buildChildren, initiativeOf, subtreeIDs, spawnChild
	t.Cleanup(func() {
		spawnSuccessor, cutBranch, buildChildren, initiativeOf, subtreeIDs, spawnChild = oSucc, oCut, oBuilds, oInit, oTree, oChild
	})
	r.branches = map[string]string{}
	spawnSuccessor = func(pred string, s bd.Successor) (string, bool, error) {
		if len(r.succ) == 0 {
			r.succ, r.succPred = append(r.succ, s), pred
		}
		return "ws-ren", true, r.crash("mint")
	}
	cutBranch = func(name, base string) (bool, error) {
		if _, ok := r.branches[name]; !ok {
			r.branches[name] = base
		}
		return true, r.crash("cut")
	}
	buildChildren = func(string) (int, error) { return len(r.builds), nil }
	initiativeOf = func(bd.Bead) (string, error) { return r.parent, nil }
	subtreeIDs = func(string) ([]string, error) { return []string{"ws-cp.1", "ws-cp.2"}, nil }
	spawnChild = func(parent string, s bd.Successor) (string, bool, error) {
		if parent != "ws-ren" {
			t.Errorf("Build spawned under %s, want the renumber Checkpoint", parent)
		}
		r.builds = append(r.builds, s)
		return "ws-ren.1", true, r.crash("build")
	}
}

func clash() []git.PostLandClash {
	return []git.PostLandClash{{Added: "docs/adr/0009-b.md", Others: []string{"docs/adr/0009-a.md"}, Mainline: "forgesha"}}
}

func landedCheckpoint(t *testing.T, r *renumberRec) error {
	r.trailerLanded = true
	cp := openCheckpoint()
	cp.Labels = []string{"context:ctx"}
	return reconcileCheckpoint(cp)
}

// The later side of a clash files one renumber Checkpoint before the container closes.
func TestLandedLaterSideMintsRenumberCheckpoint(t *testing.T) {
	r := &renumberRec{parent: "ws-init"}
	r.clashes = clash()
	stubRenumber(t, r)
	if err := landedCheckpoint(t, r); err != nil {
		t.Fatal(err)
	}
	s := r.succ[0]
	if s.Context != "ctx" || s.Repo != "" { // a repo-stamped container is nested and never mints
		t.Errorf("successor context/repo = %q/%q, want ctx/(none)", s.Context, s.Repo)
	}
	if r.succPred != "ws-cp" || s.Kind != contract.KindCheckpoint || !s.Container || s.Parent != "ws-init" ||
		s.Title != "Renumber docs/adr/0009-b.md from ws-cp" || strings.Contains(s.Description, "Spec:") ||
		!strings.Contains(s.Description, "docs/adr/0009-a.md") {
		t.Errorf("successor = %+v caused-by %s", s, r.succPred)
	}
	if r.branches["checkpoint/ws-ren"] != "forgesha" {
		t.Errorf("branches = %v, want checkpoint/ws-ren cut at the fetched main", r.branches)
	}
	if len(r.builds) != 1 || r.builds[0].Kind != contract.KindBuild ||
		!strings.Contains(r.builds[0].Description, "sequence-renumber.md") ||
		!strings.Contains(r.builds[0].Description, "landsha^1") || !strings.Contains(r.builds[0].Description, "ws-cp.1, ws-cp.2") {
		t.Errorf("builds = %+v", r.builds)
	}
	if !slices.Equal(r.closed, []string{"ws-cp"}) {
		t.Errorf("closed = %v, want the container closed after the filing", r.closed)
	}
}

// A Checkpoint landed behind an empty trailer commit reads its clashes from the MR's
// merge, the sha its trailerless hold recorded.
func TestEmptyLandedCommitPassesTheResolvedMergeSha(t *testing.T) {
	r := &renumberRec{parent: "ws-init"}
	r.clashes = clash()
	r.landedEmpty, r.mergeSha = true, "mergesha"
	stubRenumber(t, r)
	evs := eventlogtest.Capture(t)
	if err := landedCheckpoint(t, r); err != nil {
		t.Fatal(err)
	}
	if r.clashMerge != "mergesha" || len(r.succ) != 1 || !slices.Equal(r.closed, []string{"ws-cp"}) {
		t.Errorf("merge %q succ %d closed %v, want the resolved sha, a renumber and a close", r.clashMerge, len(r.succ), r.closed)
	}
	if n := countEvents(*evs, "land.merge_sha_missing"); n != 0 {
		t.Errorf("%d warnings, want none when the sha resolves", n)
	}
}

// With no recorded merge sha, detection runs as today, one warning is logged, and the
// Checkpoint still closes.
func TestEmptyLandedCommitWithoutMergeShaWarnsOnceAndCloses(t *testing.T) {
	r := &renumberRec{}
	r.landedEmpty, r.mergeWhy = true, "no hold Task"
	stubRenumber(t, r)
	evs := eventlogtest.Capture(t)
	if err := landedCheckpoint(t, r); err != nil {
		t.Fatal(err)
	}
	if r.clashRead != 1 || r.clashMerge != "" || !slices.Equal(r.closed, []string{"ws-cp"}) {
		t.Errorf("clash reads %d merge %q closed %v, want one plain read and a close", r.clashRead, r.clashMerge, r.closed)
	}
	if n := countEvents(*evs, "land.merge_sha_missing"); n != 1 {
		t.Errorf("%d warnings, want exactly one", n)
	}
}

// A non-empty landed commit is read as itself and never reaches the resolver.
func TestNonEmptyLandedCommitSkipsTheResolver(t *testing.T) {
	r := &renumberRec{}
	r.mergeSha = "mergesha"
	stubRenumber(t, r)
	if err := landedCheckpoint(t, r); err != nil {
		t.Fatal(err)
	}
	if r.mergeLookups != 0 || r.clashMerge != "" {
		t.Errorf("lookups %d merge %q, want the resolver never called", r.mergeLookups, r.clashMerge)
	}
}

func TestLandedEarlierSideFilesNothing(t *testing.T) {
	r := &renumberRec{}
	stubRenumber(t, r)
	if err := landedCheckpoint(t, r); err != nil {
		t.Fatal(err)
	}
	if len(r.succ)+len(r.builds) != 0 || !slices.Equal(r.closed, []string{"ws-cp"}) {
		t.Errorf("succ %v builds %v closed %v, want nothing filed and a close", r.succ, r.builds, r.closed)
	}
}

// A crash at any leg leaves the container open; the resume converges with no duplicate.
func TestRenumberResumesFromEveryLeg(t *testing.T) {
	for _, leg := range []string{"mint", "cut", "build"} {
		t.Run(leg, func(t *testing.T) {
			r := &renumberRec{failAt: leg}
			r.clashes = clash()
			stubRenumber(t, r)
			if err := landedCheckpoint(t, r); err == nil {
				t.Fatal("the crashed pass must report its error")
			}
			if len(r.closed) != 0 {
				t.Fatalf("closed %v, want the container open after a crash", r.closed)
			}
			if err := landedCheckpoint(t, r); err != nil {
				t.Fatal(err)
			}
			if len(r.succ) != 1 || len(r.builds) != 1 || len(r.branches) != 1 || !slices.Equal(r.closed, []string{"ws-cp"}) {
				t.Errorf("succ %d builds %d branches %v closed %v, want one each and a close", len(r.succ), len(r.builds), r.branches, r.closed)
			}
		})
	}
}

func TestLandedClashReadErrorKeepsContainerOpen(t *testing.T) {
	r := &renumberRec{}
	r.clashErr = errors.New("git down")
	stubRenumber(t, r)
	if err := landedCheckpoint(t, r); err == nil || len(r.closed) != 0 {
		t.Errorf("err %v closed %v, want the error and no close", err, r.closed)
	}
}

// Under land_strategy=local nothing new runs.
func TestLocalLandedRunsNoClashRead(t *testing.T) {
	r := &renumberRec{}
	r.gated = true
	r.clashes = clash()
	stubRenumber(t, r)
	if err := landedCheckpoint(t, r); err != nil {
		t.Fatal(err)
	}
	if r.clashRead != 0 || len(r.succ) != 0 {
		t.Errorf("clash reads %d succ %v, want none under local", r.clashRead, r.succ)
	}
}

// A landed renumber Checkpoint that re-clashes mints the next carrier whose Build sweeps
// the original container's subtree too; a clash conflict on an open one names them as its own beads.
func TestRenumberCheckpointReclashWidensBeadScope(t *testing.T) {
	r := &renumberRec{}
	r.clashes = clash()
	stubRenumber(t, r)
	causedBy = func(id string, want contract.Kind) (string, bool, error) {
		return "ws-orig", id == "ws-cp" && want == contract.KindChart, nil
	}
	subtreeIDs = func(id string) ([]string, error) { return []string{id + ".1"}, nil }
	if err := landedCheckpoint(t, r); err != nil {
		t.Fatal(err)
	}
	if len(r.builds) != 1 || !strings.Contains(r.builds[0].Description, "ws-cp, ws-cp.1, ws-orig, ws-orig.1") {
		t.Errorf("builds = %+v, want the original container's subtree in scope", r.builds)
	}

	r.builds = nil
	spawnChild = func(_ string, s bd.Successor) (string, bool, error) {
		r.builds = append(r.builds, s)
		return "c", true, nil
	}
	head := func(string) (string, error) { return "tip", nil }
	if err := fileConflict(openCheckpoint(), "base", []string{"c"}, head, false); err != nil {
		t.Fatal(err)
	}
	if d := r.builds[0].Description; !strings.Contains(d, "own beads: ws-cp, ws-cp.1, ws-orig, ws-orig.1") {
		t.Errorf("conflict body = %q", d)
	}

	causedBy = func(string, contract.Kind) (string, bool, error) { return "", false, nil }
	r.builds = nil
	if err := fileConflict(openCheckpoint(), "base", []string{"c"}, head, false); err != nil {
		t.Fatal(err)
	}
	if d := r.builds[0].Description; strings.Contains(d, "own beads") {
		t.Errorf("ordinary Checkpoint body changed: %q", d)
	}
}
