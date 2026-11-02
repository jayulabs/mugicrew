package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func seqGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@e.x", "-c", "user.name=t"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func seqAdd(t *testing.T, repo, name string) {
	t.Helper()
	p := filepath.Join(repo, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seqGit(t, repo, "add", ".")
	seqGit(t, repo, "commit", "-m", name)
}

// A path in a child repo is judged in the child: its mainline and branches, not the Workspace's.
func TestSequenceNextJudgesAChildPathInItsOwnRepo(t *testing.T) {
	ws := t.TempDir()
	seqGit(t, ws, "init", "-b", "main")
	seqAdd(t, ws, "docs/adr/0050-ws.md")
	child := filepath.Join(ws, "kid")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	seqGit(t, child, "init", "-b", "main")
	seqAdd(t, child, "docs/adr/0002-kid.md")
	seqGit(t, child, "checkout", "-b", "chart/mc-x")
	seqAdd(t, child, "docs/adr/0004-chart.md")
	seqGit(t, child, "checkout", "-b", "effort/mc-y", "main")
	seqAdd(t, child, "docs/adr/0003-mine.md")
	t.Chdir(ws)
	var out bytes.Buffer
	if err := runSequence([]string{"next", "kid/docs/adr/0003-mine.md"}, &out); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "0005" {
		t.Errorf("sequence next = %q, want 0005", got)
	}
}

func TestSequenceNextUsage(t *testing.T) {
	for _, argv := range [][]string{nil, {"next"}, {"last", "x"}, {"next", "a", "b"}} {
		if err := runSequence(argv, io.Discard); err == nil {
			t.Errorf("runSequence(%v) = nil, want usage", argv)
		}
	}
}

// seqRootWorkspace is a Workspace repo declaring repo context "kid" (sequences V{n}__*.sql),
// its child carrying V1 on main and V3 on an open branch, and a linked worktree lacking kid/.
func seqRootWorkspace(t *testing.T) (ws, wt string) {
	t.Helper()
	ws = t.TempDir()
	if real, err := filepath.EvalSymlinks(ws); err == nil {
		ws = real
	}
	seqGit(t, ws, "init", "-b", "main")
	cfg := "[context.kid]\nrepo = \"kid\"\nsequences = [\"db/V{n}__*.sql\"]\n"
	if err := os.MkdirAll(filepath.Join(ws, ".mugicrew"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".mugicrew", "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".gitignore"), []byte("/kid/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seqAdd(t, ws, "README.md")
	child := filepath.Join(ws, "kid")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	seqGit(t, child, "init", "-b", "main")
	seqAdd(t, child, "db/V1__a.sql")
	seqGit(t, child, "checkout", "-b", "effort/mc-z")
	seqAdd(t, child, "db/V3__b.sql")
	seqGit(t, child, "checkout", "main")
	wt = filepath.Join(t.TempDir(), "wt")
	seqGit(t, ws, "worktree", "add", wt, "-b", "side")
	if real, err := filepath.EvalSymlinks(wt); err == nil {
		wt = real
	}
	return ws, wt
}

func seqNext(t *testing.T, arg string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := runSequence([]string{"next", arg}, &out)
	return strings.TrimSpace(out.String()), err
}

func TestSequenceNextNumbersARepoContextPathFromAWorktreeLackingItsDir(t *testing.T) {
	ws, wt := seqRootWorkspace(t)
	t.Chdir(wt)
	if got, err := seqNext(t, "kid/db/V0__x.sql"); err != nil || got != "4" {
		t.Errorf("(a) = %q, %v; want 4", got, err)
	}
	if got, err := seqNext(t, filepath.Join(wt, "kid/db/V0__x.sql")); err != nil || got != "4" {
		t.Errorf("(b) absolute = %q, %v; want 4", got, err)
	}
	sub := filepath.Join(wt, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	if got, err := seqNext(t, "../kid/db/V0__x.sql"); err != nil || got != "4" {
		t.Errorf("(b) subdir = %q, %v; want 4", got, err)
	}
	t.Chdir(wt)
	if _, err := seqNext(t, "db/V0__x.sql"); err == nil || !strings.Contains(err.Error(), "matches no sequence rule") {
		t.Errorf("(d) unprefixed err = %v, want matches no sequence rule", err)
	}
	if err := os.Rename(filepath.Join(ws, "kid"), filepath.Join(ws, "kid-gone")); err != nil {
		t.Fatal(err)
	}
	_, err := seqNext(t, "kid/db/V0__x.sql")
	if err == nil || !strings.Contains(err.Error(), "context kid's repo kid/ is not checked out here or at "+ws) || strings.Contains(err.Error(), "matches no sequence rule") {
		t.Errorf("(c) err = %v", err)
	}
}

func TestSequenceNextWithTheDirPresentIsUnchanged(t *testing.T) {
	ws, _ := seqRootWorkspace(t)
	t.Chdir(ws)
	if got, err := seqNext(t, "kid/db/V0__x.sql"); err != nil || got != "4" {
		t.Errorf("(e) = %q, %v; want 4", got, err)
	}
}
