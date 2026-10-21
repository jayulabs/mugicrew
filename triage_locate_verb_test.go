package main

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/locate"
)

func TestTriageLocateNeedsATerm(t *testing.T) {
	if err := runTriageLocate([]string{"--json"}, &bytes.Buffer{}); err == nil || !strings.HasPrefix(err.Error(), "usage: mugicrew triage locate") {
		t.Errorf("err = %v, want the usage", err)
	}
}

func TestTriageLocateSignalNarrowsToItsRepoAndPrintsJSON(t *testing.T) {
	// An empty scratch repo is the one the Signal narrows to, so the token cannot
	// be found in this repository's own sources.
	scratch := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = scratch
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	t.Chdir(scratch)
	orig := locateDeps
	t.Cleanup(func() { locateDeps = orig })
	locateDeps = func() locate.Deps {
		return locate.Deps{
			Checkpoints: func() ([]bd.Bead, error) { return nil, nil },
			RepoOf:      func(bd.Bead) git.Repo { return "" },
			Repos:       func() ([]git.Repo, error) { return nil, nil },
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
