package main

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
)

var probeWS = forge.Project{Host: "gl.example.com", ID: "ns%2Fp"}

// fakeProbeForge scripts the Forge reads and the strategy; mrs is keyed by
// "<project id>!<iid>", pipelines by "<project id>#<id>", and "<project id>#main".
func fakeProbeForge(t *testing.T, strategy string, mrs map[string]string, open map[string]string, pipelines map[string]string) {
	t.Helper()
	oStrat, oProj, oGet, oFind, oPl, oMain := git.LandStrategyName, forgeProject, getMR, probeFindOpenMR, probeGetPipeline, probeMainPipeline
	oShow, oRepoOf, oRepoProj := probeShow, probeRepoOf, probeRepoProject
	t.Cleanup(func() {
		git.LandStrategyName, forgeProject, getMR, probeFindOpenMR, probeGetPipeline, probeMainPipeline = oStrat, oProj, oGet, oFind, oPl, oMain
		probeShow, probeRepoOf, probeRepoProject = oShow, oRepoOf, oRepoProj
	})
	// a bead named "nest-*" lives in the nested context repo, which has its own Forge project
	probeShow = func(id string) (bd.Bead, error) {
		if strings.HasPrefix(id, "nest-") {
			return bd.Bead{ID: id, Labels: []string{"context:nest"}}, nil
		}
		return bd.Bead{ID: id}, nil
	}
	probeRepoOf = func(b bd.Bead) git.Repo {
		if bd.ContextOf(b) == "nest" {
			return "/ws/nest"
		}
		return ""
	}
	probeRepoProject = func(r git.Repo) (forge.Project, error) {
		return forge.Project{Host: probeWS.Host, ID: "ns%2Fnest"}, nil
	}
	git.LandStrategyName = func() (string, error) { return strategy, nil }
	forgeProject = func() (forge.Project, error) { return probeWS, nil }
	getMR = func(p forge.Project, iid int) (*forge.MR, error) {
		st, ok := mrs[p.ID+"!"+string(rune('0'+iid))]
		if !ok {
			return nil, forge.ErrNotFound
		}
		return &forge.MR{IID: iid, State: st}, nil
	}
	probeFindOpenMR = func(p forge.Project, branch string) (*forge.MR, error) {
		if st, ok := open[p.ID+":"+branch]; ok {
			return &forge.MR{IID: 1, State: st}, nil
		}
		return nil, nil
	}
	probeGetPipeline = func(p forge.Project, id int) (*forge.Pipeline, error) {
		st, ok := pipelines[p.ID+"#"+string(rune('0'+id))]
		if !ok {
			return nil, forge.ErrNotFound
		}
		return &forge.Pipeline{ID: id, Status: st}, nil
	}
	probeMainPipeline = func(p forge.Project) (*forge.Pipeline, error) {
		if st, ok := pipelines[p.ID+"#main"]; ok {
			return &forge.Pipeline{ID: 99, Status: st}, nil
		}
		return nil, nil
	}
}

func TestProbe(t *testing.T) {
	fakeProbeForge(t, git.LandStrategyGitlab,
		map[string]string{"ns%2Fp!1": "opened", "ns%2Fp!2": "merged", "ns%2Fp!3": "closed", "ns%2Fp!4": "locked", "a%2Fb%2Fc!5": "merged"},
		map[string]string{"ns%2Fp:chart/x": "opened", "ns%2Fnest:checkpoint/nest-cp": "opened"},
		map[string]string{"ns%2Fp#7": "running", "a%2Fb%2Fc#8": "success", "ns%2Fp#main": "failed"})
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"mr", "!1"}, "opened"},
		{[]string{"mr", "!2"}, "merged"},
		{[]string{"mr", "!3"}, "closed"},
		{[]string{"mr", "!4"}, "locked"},
		{[]string{"mr", "a/b/c!5"}, "merged"},
		{[]string{"mr", "https://gl.example.com/a/b/c/-/merge_requests/5"}, "merged"},
		{[]string{"mr", "https://gl.example.com/ns/p/-/merge_requests/1"}, "opened"},
		{[]string{"mr", "--branch", "chart/x"}, "opened"},
		{[]string{"mr", "--branch", "chart/none"}, "none"},
		{[]string{"mr", "--branch", "checkpoint/nest-cp"}, "opened"}, // a nested container's own project
		{[]string{"pipeline", "!1", "7"}, "running"},
		{[]string{"pipeline", "a/b/c!5", "8"}, "success"},
		{[]string{"pipeline", "!1", "--main"}, "failed"},
	} {
		var out bytes.Buffer
		if err := runProbe(tc.args, &out); err != nil {
			t.Errorf("probe %v: %v", tc.args, err)
			continue
		}
		if out.String() != tc.want+"\n" {
			t.Errorf("probe %v printed %q, want %q", tc.args, out.String(), tc.want+"\n")
		}
	}
}

func TestProbeExitsNonZeroWhenTheReadFails(t *testing.T) {
	fakeProbeForge(t, git.LandStrategyGitlab, nil, nil, nil)
	for _, args := range [][]string{
		{"mr", "!9"},      // unknown MR
		{"mr", "garbage"}, // unparseable ref
		{"mr", "https://other.host/ns/p/-/merge_requests/1"},
		{"pipeline", "!1", "5"},      // unknown pipeline
		{"pipeline", "!1", "--main"}, // no pipeline on main
		{"pipeline", "!1"},           // neither id nor --main
		{"mr"},
		{"nope"},
		{},
	} {
		var out bytes.Buffer
		if err := runProbe(args, &out); err == nil || out.Len() != 0 {
			t.Errorf("probe %v = %q, %v; want an error and no output", args, out.String(), err)
		}
	}
}

func TestProbeNeedsAForge(t *testing.T) {
	fakeProbeForge(t, git.LandStrategyLocal, map[string]string{"ns%2Fp!1": "opened"}, nil, map[string]string{"ns%2Fp#7": "running"})
	for _, args := range [][]string{{"mr", "!1"}, {"mr", "--branch", "b"}, {"pipeline", "!1", "7"}, {"pipeline", "!1", "--main"}} {
		var out bytes.Buffer
		err := runProbe(args, &out)
		if err == nil || !strings.Contains(err.Error(), "no Forge") || out.Len() != 0 {
			t.Errorf("probe %v = %q, %v; want a no Forge error", args, out.String(), err)
		}
	}
	probeStrategyErr := errors.New("boom")
	git.LandStrategyName = func() (string, error) { return "", probeStrategyErr }
	if err := runProbe([]string{"mr", "!1"}, &bytes.Buffer{}); !errors.Is(err, probeStrategyErr) {
		t.Errorf("strategy error = %v, want it wrapped", err)
	}
}

// fakeProbeLand scripts the beads and the git reads landed and foreign make; it records
// the refs read and the fetches made.
func fakeProbeLand(t *testing.T, strategy string, beads map[string]bd.Bead, landed map[string]error) (fetches, reads *[]string) {
	t.Helper()
	oStrat, oShow, oTgt, oRepoOf, oRepos, oFetch, oRemote, oLanded, oScan, oMain :=
		git.LandStrategyName, probeShow, probeRollbackTarget, probeRepoOf, probeRepos, probeFetch, probeForgeRemote, probeLandedOn, probeForeignScan, probeMainline
	t.Cleanup(func() {
		git.LandStrategyName, probeShow, probeRollbackTarget, probeRepoOf, probeRepos, probeFetch, probeForgeRemote, probeLandedOn, probeForeignScan, probeMainline =
			oStrat, oShow, oTgt, oRepoOf, oRepos, oFetch, oRemote, oLanded, oScan, oMain
	})
	fetches, reads = new([]string), new([]string)
	git.LandStrategyName = func() (string, error) { return strategy, nil }
	probeShow = func(id string) (bd.Bead, error) {
		b, ok := beads[id]
		if !ok {
			return bd.Bead{}, errors.New("no such bead " + id)
		}
		return b, nil
	}
	probeRollbackTarget = func(id string) (string, bool, error) { return "mc-cp", id == "mc-rb", nil }
	probeRepoOf = func(b bd.Bead) git.Repo {
		if bd.ContextOf(b) == "nest" {
			return "/ws/nest"
		}
		return ""
	}
	probeRepos = func() map[string]string { return map[string]string{"nest": "nest"} }
	probeRemote := func(r git.Repo) string { return "origin" }
	probeForgeRemote = probeRemote
	probeFetch = func(r git.Repo, remote string) (string, error) {
		*fetches = append(*fetches, string(r)+"@"+remote)
		return "refs/remotes/origin/main", nil
	}
	probeMainline = func(r git.Repo) string { return "main" }
	probeLandedOn = func(r git.Repo, ref, trailer string) (string, error) {
		*reads = append(*reads, string(r)+" "+ref+" "+trailer)
		if err := landed[trailer]; err != nil {
			return "", err
		}
		return "abc", nil
	}
	probeForeignScan = func(r git.Repo, ref string) error {
		*reads = append(*reads, string(r)+" "+ref+" scan")
		return landed["scan"]
	}
	return fetches, reads
}

func TestProbeLandedAndForeign(t *testing.T) {
	beads := map[string]bd.Bead{
		"mc-cp":  {ID: "mc-cp", Kind: contract.KindCheckpoint},
		"mc-ch":  {ID: "mc-ch", Kind: contract.KindChart},
		"mc-no":  {ID: "mc-no", Kind: contract.KindCheckpoint},
		"mc-fo":  {ID: "mc-fo", Kind: contract.KindCheckpoint},
		"mc-ne":  {ID: "mc-ne", Kind: contract.KindCheckpoint, Labels: []string{"context:nest"}},
		"mc-rb":  {ID: "mc-rb", Kind: contract.KindRollback},
		"mc-tsk": {ID: "mc-tsk", Kind: contract.KindBuild},
	}
	landed := map[string]error{
		"Checkpoint: mc-no":         fmt.Errorf("%w for trailer", git.ErrNotLanded),
		"Checkpoint: mc-fo":         &git.ForeignLanding{Theirs: "g9"},
		"Reverts-Checkpoint: mc-cp": git.ErrNotLanded,
	}
	fetches, reads := fakeProbeLand(t, git.LandStrategyGitlab, beads, landed)
	root, err := git.RepoRoot()
	if err != nil {
		t.Skip("no repo root:", err)
	}
	nestDir := filepath.Join(root, "nest")
	for _, tc := range []struct {
		args       []string
		want, read string
	}{
		{[]string{"landed", "mc-cp"}, "landed", " refs/remotes/origin/main Checkpoint: mc-cp"},
		{[]string{"landed", "mc-ch"}, "landed", " refs/remotes/origin/main Chart: mc-ch"},
		{[]string{"landed", "mc-no"}, "absent", " refs/remotes/origin/main Checkpoint: mc-no"},
		{[]string{"landed", "mc-fo"}, "foreign", " refs/remotes/origin/main Checkpoint: mc-fo"},
		{[]string{"landed", "mc-ne"}, "landed", "/ws/nest refs/remotes/origin/main Checkpoint: mc-ne"},
		{[]string{"landed", "mc-rb"}, "absent", " refs/remotes/origin/main Reverts-Checkpoint: mc-cp"},
		{[]string{"foreign"}, "none", " refs/remotes/origin/main scan"},
		{[]string{"foreign", "--context", "nest"}, "none", nestDir + " refs/remotes/origin/main scan"},
	} {
		*fetches, *reads = nil, nil
		var out bytes.Buffer
		if err := runProbe(tc.args, &out); err != nil {
			t.Errorf("probe %v: %v", tc.args, err)
			continue
		}
		if out.String() != tc.want+"\n" || len(*reads) != 1 || (*reads)[0] != tc.read || len(*fetches) != 1 {
			t.Errorf("probe %v = %q reads %q fetches %q; want %q read %q after one fetch", tc.args, out.String(), *reads, *fetches, tc.want, tc.read)
		}
	}
	landed["scan"] = &git.ForeignLanding{Theirs: "g9"}
	var out bytes.Buffer
	if err := runProbe([]string{"foreign"}, &out); err != nil || out.String() != "foreign\n" {
		t.Errorf("probe foreign with a foreign trailer = %q, %v", out.String(), err)
	}
}

func TestProbeLandedAndForeignReadLocalMainWithoutFetching(t *testing.T) {
	fetches, reads := fakeProbeLand(t, git.LandStrategyLocal, map[string]bd.Bead{"mc-cp": {ID: "mc-cp", Kind: contract.KindCheckpoint}}, nil)
	for _, args := range [][]string{{"landed", "mc-cp"}, {"foreign"}} {
		var out bytes.Buffer
		if err := runProbe(args, &out); err != nil {
			t.Errorf("probe %v: %v", args, err)
		}
	}
	if len(*fetches) != 0 || len(*reads) != 2 || !strings.Contains((*reads)[0], " main ") || !strings.Contains((*reads)[1], " main ") {
		t.Errorf("fetches %q reads %q; want no fetch and local main", *fetches, *reads)
	}
}

func TestProbeLandedAndForeignErrors(t *testing.T) {
	boom := errors.New("boom")
	fakeProbeLand(t, git.LandStrategyGitlab, map[string]bd.Bead{
		"mc-cp":  {ID: "mc-cp", Kind: contract.KindCheckpoint},
		"mc-tsk": {ID: "mc-tsk", Kind: contract.KindBuild},
	}, map[string]error{"Checkpoint: mc-cp": boom, "scan": boom})
	for _, args := range [][]string{{"landed", "mc-nope"}, {"landed", "mc-tsk"}, {"landed", "mc-cp"}, {"landed"}, {"foreign"}, {"foreign", "--context", "nope"}, {"foreign", "x"}} {
		var out bytes.Buffer
		if err := runProbe(args, &out); err == nil || out.Len() != 0 {
			t.Errorf("probe %v = %q, %v; want an error and no output", args, out.String(), err)
		}
	}
}
