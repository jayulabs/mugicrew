package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/locate"
)

// git runs git in dir as a fixed identity, failing the test on error.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestTriageLocateNeedsATerm(t *testing.T) {
	if err := runTriageLocate([]string{"--json"}, &bytes.Buffer{}); err == nil || !strings.HasPrefix(err.Error(), "usage: mugicrew triage locate") {
		t.Errorf("err = %v, want the usage", err)
	}
}

func TestTriageLocateEmptyWorkspaceRepoPrintsJSON(t *testing.T) {
	// An empty scratch repo is the only one searched, so the token cannot be found
	// in this repository's own sources.
	scratch := scratchRepo(t)
	t.Chdir(scratch)
	orig := locateDeps
	t.Cleanup(func() { locateDeps = orig })
	locateDeps = func() locate.Deps {
		return locate.Deps{
			Checkpoints: func() ([]bd.Bead, error) { return nil, nil },
			RepoOf:      func(bd.Bead) git.Repo { return "" },
			Repos:       func() ([]locate.Declared, error) { return nil, nil },
			Strategy:    func() (string, error) { return git.LandStrategyLocal, nil },
		}
	}
	origShow := showSignal
	t.Cleanup(func() { showSignal = origShow })
	showSignal = func(id string) (bd.Bead, error) { return bd.Bead{ID: id}, nil }
	var out bytes.Buffer
	if err := runTriageLocate([]string{"--signal", "s1", "--token", "zzz_nowhere", "--json"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"verdict": "none"`) {
		t.Errorf("output %s, want verdict none", out.String())
	}
}

// scratchRepo makes a git repo with one empty commit on main.
func scratchRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "init")
	return dir
}

// scriptContextRepoCheckpoint scripts a Workspace repo and a context repo whose
// live Checkpoint cp1 alone introduces the token ctx_only_token.
func scriptContextRepoCheckpoint(t *testing.T) {
	t.Helper()
	ws, ctx := scratchRepo(t), scratchRepo(t)
	gitIn(t, ctx, "checkout", "-q", "-b", "checkpoint/cp1")
	if err := os.WriteFile(filepath.Join(ctx, "f.txt"), []byte("ctx_only_token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, ctx, "add", "f.txt")
	gitIn(t, ctx, "commit", "-q", "-m", "add token")
	gitIn(t, ctx, "checkout", "-q", "main")
	t.Chdir(ws)
	orig := locateDeps
	t.Cleanup(func() { locateDeps = orig })
	locateDeps = func() locate.Deps {
		return locate.Deps{
			Checkpoints: func() ([]bd.Bead, error) { return []bd.Bead{{ID: "cp1"}}, nil },
			RepoOf: func(b bd.Bead) git.Repo {
				if bd.ContextOf(b) == "subtree" {
					return "" // a context with no declared repo of its own
				}
				return git.Repo(ctx)
			},
			Repos: func() ([]locate.Declared, error) {
				return []locate.Declared{{}, {Repo: git.Repo(ctx), Dir: "ctx"}}, nil
			},
			Strategy: func() (string, error) { return git.LandStrategyLocal, nil },
		}
	}
}

func locateSignalWith(t *testing.T, labels []string) string {
	t.Helper()
	origShow := showSignal
	t.Cleanup(func() { showSignal = origShow })
	showSignal = func(id string) (bd.Bead, error) { return bd.Bead{ID: id, Labels: labels}, nil }
	var out bytes.Buffer
	if err := runTriageLocate([]string{"--signal", "s1", "--token", "ctx_only_token", "--json"}, &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestTriageLocateUnlabeledSignalSearchesEveryRepo(t *testing.T) {
	scriptContextRepoCheckpoint(t)
	out := locateSignalWith(t, nil)
	if !strings.Contains(out, `"verdict": "only"`) || !strings.Contains(out, `"cp1"`) {
		t.Errorf("output %s, want verdict only cp1 from the context repo", out)
	}
}

func TestTriageLocateLabeledSignalWithoutDeclaredRepoSearchesWorkspaceOnly(t *testing.T) {
	scriptContextRepoCheckpoint(t)
	out := locateSignalWith(t, []string{"context:subtree"})
	if !strings.Contains(out, `"verdict": "none"`) || strings.Contains(out, `"cp1"`) {
		t.Errorf("output %s, want verdict none, the context repo unsearched", out)
	}
}

func TestTriageLocateLabeledSignalWithDeclaredRepoSearchesThatRepo(t *testing.T) {
	scriptContextRepoCheckpoint(t)
	out := locateSignalWith(t, []string{"context:ctxrepo"})
	if !strings.Contains(out, `"verdict": "only"`) || !strings.Contains(out, `"cp1"`) {
		t.Errorf("output %s, want verdict only cp1 from the context's repo", out)
	}
}
