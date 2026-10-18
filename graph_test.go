package main

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
)

func stubGraph(t *testing.T, strategy string, authErr error) *[]string {
	t.Helper()
	var pushed []string
	ol, op, oc, ou, og := git.LandStrategyName, forgeProject, forgeCheck, forgeUserName, graphPush
	t.Cleanup(func() {
		git.LandStrategyName, forgeProject, forgeCheck, forgeUserName, graphPush = ol, op, oc, ou, og
	})
	git.LandStrategyName = func() (string, error) { return strategy, nil }
	forgeProject = func() (forge.Project, error) { return forge.Project{Host: "gitlab.com", ID: "ns%2Fp"}, nil }
	forgeCheck = func(string) error { return authErr }
	forgeUserName = func(forge.Project) (string, error) { return "ann", nil }
	graphPush = func(name string) (bd.GraphPushResult, error) {
		pushed = append(pushed, name)
		return bd.GraphPushResult{Ref: bd.GraphRefOf(name), Warning: "w"}, nil
	}
	return &pushed
}

func TestGraphPushRefusesUnderLocal(t *testing.T) {
	pushed := stubGraph(t, git.LandStrategyLocal, nil)
	err := runGraph([]string{"push"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "land_strategy=gitlab") || len(*pushed) != 0 {
		t.Fatalf("err = %v, pushed %v", err, *pushed)
	}
}

func TestGraphPushNamesGlabAuthLogin(t *testing.T) {
	pushed := stubGraph(t, git.LandStrategyGitlab, errors.New("logged out"))
	err := runGraph([]string{"push"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "glab auth login") || len(*pushed) != 0 {
		t.Fatalf("err = %v, pushed %v", err, *pushed)
	}
}

func TestGraphPushPrintsRefAndWarning(t *testing.T) {
	stubGraph(t, git.LandStrategyGitlab, nil)
	var out bytes.Buffer
	if err := runGraph([]string{"push"}, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "refs/mugicrew/graphs/ann") || !strings.Contains(got, "warning: w") {
		t.Errorf("out = %q", got)
	}
}

func TestGraphPushForeignRootNamesBothIds(t *testing.T) {
	stubGraph(t, git.LandStrategyGitlab, nil)
	graphPush = func(string) (bd.GraphPushResult, error) {
		return bd.GraphPushResult{}, &bd.ForeignGraphRef{Ref: "r", Ours: "aaa", Theirs: "bbb"}
	}
	err := runGraph([]string{"push"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "aaa") || !strings.Contains(err.Error(), "bbb") {
		t.Fatalf("err = %v", err)
	}
}

func TestGraphFetchDefaultsDirAndPrintsNextStep(t *testing.T) {
	var gotName, gotDir string
	var gotKeep []string
	of, or := graphFetch, repoRoot
	t.Cleanup(func() { graphFetch, repoRoot = of, or })
	repoRoot = func() (string, error) { return "/ws", nil }
	graphFetch = func(n, d string, keep ...string) error { gotName, gotDir, gotKeep = n, d, keep; return nil }
	t.Setenv("XDG_CACHE_HOME", "/cache")
	t.Setenv("BEADS_DIR", "/ws/.beads")
	var out bytes.Buffer
	if err := runGraph([]string{"fetch", "ann"}, &out); err != nil {
		t.Fatal(err)
	}
	if gotName != "ann" || gotDir != "/cache/mugicrew/graphs/ann" || len(gotKeep) != 2 || gotKeep[0] != "/ws" || gotKeep[1] != "/ws/.beads" {
		t.Errorf("%q %q %q", gotName, gotDir, gotKeep)
	}
	for _, want := range []string{"bd dolt start", "BEADS_DIR=/cache/mugicrew/graphs/ann/.beads bd list", "bd dolt stop"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("out = %q, missing %q", out.String(), want)
		}
	}
}

func TestGraphPushRecordsBeadlessEvents(t *testing.T) {
	stubGraph(t, git.LandStrategyGitlab, nil)
	evs := eventlogtest.Capture(t)
	if err := runGraph([]string{"push"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	graphPush = func(string) (bd.GraphPushResult, error) { return bd.GraphPushResult{}, errors.New("denied") }
	if err := runGraph([]string{"push"}, &bytes.Buffer{}); err == nil {
		t.Fatal("want the push error")
	}
	if got := eventlogtest.Names(*evs); !slices.Equal(got, []string{"graph.push", "graph.push_failed"}) {
		t.Fatalf("events = %v", got)
	}
	for _, e := range *evs {
		if e.Attrs["bead"] != nil {
			t.Errorf("%s names a bead: %v", e.Name, e.Attrs)
		}
	}
}
