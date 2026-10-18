package bd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type scratchFake struct {
	calls []string
	dirs  map[string]bool
}

func (f *scratchFake) install(t *testing.T, hasRef bool) {
	t.Helper()
	osr, ourl, ohas := scratchRun, forgeURL, forgeHasRef
	t.Cleanup(func() { scratchRun, forgeURL, forgeHasRef = osr, ourl, ohas })
	forgeURL = func() (string, error) { return "git@gitlab.com:ns/p.git", nil }
	forgeHasRef = func(string, []string) (bool, error) { return hasRef, nil }
	f.dirs = map[string]bool{}
	scratchRun = func(dir string, args ...string) (string, error) {
		f.dirs[dir] = true
		f.calls = append(f.calls, strings.Join(args, " "))
		switch args[0] {
		case "init":
			os.MkdirAll(filepath.Join(dir, ".beads"), 0o755)
			os.WriteFile(filepath.Join(dir, ".beads", "metadata.json"), []byte(`{"dolt_database":"beads","keep":1}`), 0o644)
			os.WriteFile(filepath.Join(dir, ".beads", "config.yaml"), []byte("issue-prefix: scratch\nkeep: 1"), 0o644)
		case "config":
			return "scratch", nil
		case "sql":
			if strings.Contains(args[len(args)-1], "_project_id") {
				return "value\nPID", nil
			}
		}
		return "", nil
	}
}

func (f *scratchFake) order(t *testing.T, want ...string) {
	t.Helper()
	i := 0
	for _, c := range f.calls {
		if i < len(want) && strings.Contains(c, want[i]) {
			i++
		}
	}
	if i != len(want) {
		t.Errorf("calls %q not in order %q", f.calls, want)
	}
}

func TestFetchGraphSequenceAndRepoint(t *testing.T) {
	f := &scratchFake{}
	f.install(t, true)
	dir := filepath.Join(t.TempDir(), "g")
	if err := FetchGraph("ann", dir); err != nil {
		t.Fatal(err)
	}
	f.order(t, "init --server", "config get issue_prefix", "dolt stop", "dolt start",
		"DOLT_CLONE('--ref','refs/mugicrew/graphs/ann','git+ssh://git@gitlab.com/ns/p.git','ann')",
		"_project_id", "migrate schema", "dolt stop")
	if f.calls[len(f.calls)-1] != "dolt stop" {
		t.Errorf("server left running: %q", f.calls)
	}
	if len(f.dirs) != 1 || !f.dirs[dir] {
		t.Errorf("a bd call left the scratch dir: %v", f.dirs)
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".beads", "metadata.json"))
	for _, w := range []string{`"dolt_database": "ann"`, `"project_id": "PID"`, `"keep": 1`} {
		if !strings.Contains(string(b), w) {
			t.Errorf("metadata.json missing %s: %s", w, b)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, fetchMarker)); err != nil {
		t.Error("no marker")
	}
}

func TestFetchGraphSetsReadonlyAfterMigrate(t *testing.T) {
	f := &scratchFake{}
	f.install(t, true)
	dir := filepath.Join(t.TempDir(), "g")
	cfg := filepath.Join(dir, ".beads", "config.yaml")
	inner := scratchRun
	var atMigrate string
	scratchRun = func(d string, args ...string) (string, error) {
		if args[0] == "migrate" {
			b, _ := os.ReadFile(cfg)
			atMigrate = string(b)
		}
		return inner(d, args...)
	}
	if err := FetchGraph("ann", dir); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(cfg)
	for _, w := range []string{"readonly: true", "issue-prefix: scratch", "keep: 1"} {
		if !strings.Contains(string(b), w) {
			t.Errorf("config.yaml missing %s: %s", w, b)
		}
	}
	if strings.Contains(atMigrate, "readonly") {
		t.Error("readonly set before migrate schema")
	}

	// a fetch that fails at migrate schema leaves it unset
	dir = filepath.Join(t.TempDir(), "h")
	scratchRun = func(d string, args ...string) (string, error) {
		if args[0] == "migrate" {
			return "", errors.New("boom")
		}
		return inner(d, args...)
	}
	if err := FetchGraph("ann", dir); err == nil {
		t.Fatal("want migrate failure")
	}
	b, _ = os.ReadFile(filepath.Join(dir, ".beads", "config.yaml"))
	if strings.Contains(string(b), "readonly") {
		t.Errorf("readonly set despite failed migrate: %s", b)
	}
	if _, err := os.Stat(filepath.Join(dir, fetchMarker)); err != nil {
		t.Error("failed fetch lost its marker")
	}
}

func TestFetchGraphDirGuards(t *testing.T) {
	root := t.TempDir()
	cases := map[string]string{"inside workspace": filepath.Join(root, "sub"), "is workspace": root}
	for name, dir := range cases {
		t.Run(name, func(t *testing.T) {
			f := &scratchFake{}
			f.install(t, true)
			if err := FetchGraph("ann", dir, root); err == nil || len(f.calls) != 0 {
				t.Fatalf("err %v calls %v", err, f.calls)
			}
		})
	}
	t.Run("unmarked non-empty", func(t *testing.T) {
		f := &scratchFake{}
		f.install(t, true)
		dir := t.TempDir()
		keep := filepath.Join(dir, "mine.txt")
		os.WriteFile(keep, nil, 0o644)
		if err := FetchGraph("ann", dir); err == nil || len(f.calls) != 0 {
			t.Fatalf("err %v calls %v", err, f.calls)
		}
		if _, err := os.Stat(keep); err != nil {
			t.Error("deleted the user's file")
		}
	})
	t.Run("marked dir is re-cloned", func(t *testing.T) {
		f := &scratchFake{}
		f.install(t, true)
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, fetchMarker), nil, 0o644)
		os.WriteFile(filepath.Join(dir, "stale"), nil, 0o644)
		if err := FetchGraph("ann", dir); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "stale")); err == nil {
			t.Error("stale file survived")
		}
	})
	t.Run("bad username", func(t *testing.T) {
		f := &scratchFake{}
		f.install(t, true)
		if err := FetchGraph("../x'", t.TempDir()); err == nil || len(f.calls) != 0 {
			t.Fatalf("err %v", err)
		}
	})
}

func TestFetchGraphNoRef(t *testing.T) {
	f := &scratchFake{}
	f.install(t, false)
	dir := t.TempDir()
	err := FetchGraph("ann", dir)
	var nr *NoGraphRef
	if !errors.As(err, &nr) || !strings.Contains(err.Error(), "git ls-remote") || !strings.Contains(err.Error(), "refs/mugicrew/graphs/ann") {
		t.Fatalf("err = %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("ran bd: %v", f.calls)
	}
}

func TestScratchRunSetsBeadsDir(t *testing.T) {
	t.Setenv("BEADS_DIR", "/real/graph/.beads")
	dir := t.TempDir()
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "bd"), []byte("#!/bin/sh\necho \"$BEADS_DIR|$DOLT_REMOTE_INFO_BRANCH|$(pwd)\"\n"), 0o755)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	got, err := scratchRun(dir, "x")
	if err != nil || !strings.HasPrefix(got, filepath.Join(dir, ".beads")+"||") {
		t.Fatalf("%q %v", got, err)
	}
}

func TestWithinResolvesSymlinks(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "ws")
	os.Mkdir(ws, 0o755)
	link := filepath.Join(root, "link")
	if err := os.Symlink(ws, link); err != nil {
		t.Skip(err)
	}
	if !within(filepath.Join(link, "scratch"), ws) {
		t.Error("symlinked dir into the Workspace passed the guard")
	}
}

func TestGraphPushRejectsBadName(t *testing.T) {
	if _, err := GraphPush("x'); DROP"); err == nil {
		t.Error("want username refusal")
	}
}

func TestFetchGraphDottedUsernameUsesSafeDatabase(t *testing.T) {
	f := &scratchFake{}
	f.install(t, true)
	dir := filepath.Join(t.TempDir(), "g")
	if err := FetchGraph("john.doe", dir); err != nil {
		t.Fatal(err)
	}
	f.order(t, "DOLT_CLONE('--ref','refs/mugicrew/graphs/john.doe','git+ssh://git@gitlab.com/ns/p.git','john_doe')",
		"`john_doe`.metadata")
	b, _ := os.ReadFile(filepath.Join(dir, ".beads", "metadata.json"))
	if !strings.Contains(string(b), `"dolt_database": "john_doe"`) {
		t.Errorf("metadata.json: %s", b)
	}
}
