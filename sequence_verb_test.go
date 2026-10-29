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
