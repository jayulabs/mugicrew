package liveness

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// procargs2 builds a KERN_PROCARGS2 buffer: int32 argc, exec path, NUL padding,
// argv, then whatever follows (env) is ignored.
func procargs2(argc int, exe string, pad int, argv ...string) []byte {
	b := []byte{byte(argc), 0, 0, 0}
	b = append(b, exe...)
	b = append(b, make([]byte, 1+pad)...)
	for _, a := range argv {
		b = append(b, a...)
		b = append(b, 0)
	}
	return append(b, "ENV=1\x00"...)
}

func TestParseProcArgs(t *testing.T) {
	for name, tc := range map[string]struct {
		raw  []byte
		want []string
	}{
		"argv":      {procargs2(3, "/bin/claude", 5, "claude", "--name", "mc-1"), []string{"claude", "--name", "mc-1"}},
		"space":     {procargs2(2, "/bin/x", 0, "mugicrew", "a b"), []string{"mugicrew", "a b"}},
		"short":     {[]byte{1, 0}, nil},
		"empty":     {nil, nil},
		"truncated": {append([]byte{3, 0, 0, 0}, "/bin/x\x00\x00a\x00b"...), []string{"a", "b"}},
	} {
		if got := parseProcArgs(tc.raw); !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

func TestParseLsofCwd(t *testing.T) {
	got := parseLsofCwd([]byte("p12\nfcwd\nn/tmp/a b\np34\nfcwd\nn/x\np\nn/bad\npzz\nn/bad\n"))
	if len(got) != 2 || got[12] != "/tmp/a b" || got[34] != "/x" {
		t.Errorf("got %v", got)
	}
}

func TestInTreeMissingLsofFailsClosed(t *testing.T) {
	orig := lsofPath
	lsofPath = filepath.Join(t.TempDir(), "no-lsof")
	t.Cleanup(func() { lsofPath = orig })
	if _, err := InTree(t.TempDir(), nil); err == nil {
		t.Error("missing lsof must be an error")
	}
}

// A fake lsof reports its own pid and cwd, as the real one does: run from inside
// tree it must still not read as a holder.
func TestInTreeIgnoresLsofItself(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "lsof")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nprintf 'p%s\\nn%s\\n' $$ \"$PWD\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := lsofPath
	lsofPath = fake
	t.Cleanup(func() { lsofPath = orig })
	tree := t.TempDir()
	t.Chdir(tree)
	if got, err := InTree(tree, Lineage(os.Getpid())); err != nil || len(got) != 0 {
		t.Errorf("got %v, %v; want none", got, err)
	}
}

func TestLiveSleepInTreeAndAlive(t *testing.T) {
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("no lsof")
	}
	tree := t.TempDir()
	// Alive matches argv[0]'s basename, so run a copy of sleep named claude.
	bin := filepath.Join(tree, "claude")
	src, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, b, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "300")
	cmd.Dir = tree
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot run copied sleep: %v", err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	pids, err := InTree(tree, nil)
	if err != nil || !slices.Contains(pids, cmd.Process.Pid) {
		t.Errorf("InTree = %v, %v; want pid %d", pids, err, cmd.Process.Pid)
	}
	if got, _ := InTree(tree, []int{cmd.Process.Pid}); slices.Contains(got, cmd.Process.Pid) {
		t.Error("excluded pid listed")
	}
	if !slices.Contains(argvPids(), cmd.Process.Pid) {
		t.Error("argv table misses the child")
	}
	if !Alive([]string{"300"})["300"] || !slices.Contains(Pids("300"), cmd.Process.Pid) {
		t.Error("Alive/Pids miss the claude-named child by its argv token")
	}
	if l := Lineage(cmd.Process.Pid); len(l) < 2 || l[1] != os.Getpid() {
		t.Errorf("Lineage = %v, want parent %d", l, os.Getpid())
	}
}

func argvPids() (out []int) {
	for _, p := range argvTable() {
		out = append(out, p.pid)
	}
	return
}
