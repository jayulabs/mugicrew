package looplock

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/git"
)

func TestClassifyGate(t *testing.T) {
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	for _, c := range []struct {
		fixture string
		ctx     context.Context
		builder bool
		drv     string
		timeout bool
	}{
		{"gate-builder-fetch-progress.txt", context.Background(), true, "/nix/store/p45mg7c0aaaa-mugicrew-0.0.0.drv", false},
		{"gate-hash-mismatch.txt", context.Background(), true, "/nix/store/3vfrmhv0kq0mhfmfm2wkpi8ggrhcr517-fod.drv", false},
		{"gate-socket.txt", context.Background(), false, "", false},
		{"gate-object-not-found.txt", context.Background(), false, "", false},
		{"gate-attribute.txt", context.Background(), false, "", false},
		{"gate-eval.txt", context.Background(), false, "", false},
		{"gate-builder-fetch-progress.txt", expired, false, "", true},
	} {
		b, err := os.ReadFile(filepath.Join("testdata", c.fixture))
		if err != nil {
			t.Fatal(err)
		}
		got := classifyGate(c.ctx, errors.New(strings.TrimSpace(string(b))))
		var be *BuilderError
		var ie *InfraError
		switch {
		case c.builder:
			if !errors.As(got, &be) || be.Drv != c.drv || !strings.Contains(be.Log, "builder failed") && !strings.Contains(be.Log, "hash mismatch") {
				t.Errorf("%s: got %#v; want BuilderError drv %q", c.fixture, got, c.drv)
			}
		case !errors.As(got, &ie) || ie.Timeout != c.timeout || ie.Tail == "":
			t.Errorf("%s: got %#v; want InfraError timeout=%v", c.fixture, got, c.timeout)
		}
	}
}

func TestClassifyGateLogTailIs80Lines(t *testing.T) {
	text := strings.Repeat("noise\n", 200) + "Reason: builder failed with exit code 1."
	got := classifyGate(context.Background(), errors.New(text)).(*BuilderError)
	if n := len(strings.Split(got.Log, "\n")); n != 80 {
		t.Fatalf("log lines = %d; want 80", n)
	}
}

// The 80-line log tail survives whole: long lines are not byte-cut to maxTail.
func TestClassifyGateKeepsWholeLinesPastMaxTail(t *testing.T) {
	text := strings.Repeat(strings.Repeat("x", 100)+"\n", 79) + "Reason: builder failed with exit code 1."
	got := classifyGate(context.Background(), errors.New(text)).(*BuilderError)
	if n := len(strings.Split(got.Log, "\n")); n != 80 || len(got.Log) <= maxTail {
		t.Fatalf("log lines = %d, bytes = %d; want 80 whole lines", n, len(got.Log))
	}
}

// The gate's nix build asks for 80 builder-log lines (nix defaults to 25).
func TestGateBuildAsksNixForEightyLogLines(t *testing.T) {
	common, rev := gateRepo(t)
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "nix"), []byte("#!/bin/sh\necho \"$@\" >&2\nexit 1\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, err := GateBuild(context.Background(), common, "L3", rev, "pkg")
	if err == nil || !strings.Contains(err.Error(), "--log-lines 80") {
		t.Fatalf("err = %v; want nix argv carrying --log-lines 80", err)
	}
	if want := "git+file://" + common + "?ref=refs/mugicrew/gate/L3&rev=" + rev + "#pkg"; !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v; want flake URL %s", err, want)
	}
}

// No nix on PATH is an infra failure, and the scratch ref is gone afterwards.
func TestGateBuildNoNixIsInfraAndDeletesRef(t *testing.T) {
	common, rev := gateRepo(t)
	t.Setenv("PATH", t.TempDir())
	_, err := GateBuild(context.Background(), common, "L1", rev, "pkg")
	var ie *InfraError
	if !errors.As(err, &ie) {
		t.Fatalf("err = %#v; want InfraError", err)
	}
	if _, err := exec.Command("git", "--git-dir", common, "rev-parse", "--verify", "-q", "refs/mugicrew/gate/L1").Output(); err == nil {
		t.Fatal("scratch ref survived")
	}
}

func TestGateBuildDeadlineIsInfraTimeout(t *testing.T) {
	common, rev := gateRepo(t)
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "nix"), []byte("#!/bin/sh\nexec sleep 30\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	old := gateTimeout
	gateTimeout = 200 * time.Millisecond
	defer func() { gateTimeout = old }()
	_, err := GateBuild(context.Background(), common, "L2", rev, "pkg")
	var ie *InfraError
	if !errors.As(err, &ie) || !ie.Timeout {
		t.Fatalf("err = %#v; want InfraError timeout", err)
	}
}

// gateRepo is a throwaway repo (common dir, one commit).
func gateRepo(t *testing.T) (common, rev string) {
	t.Helper()
	src := t.TempDir()
	g := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", src, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	g("init", "-q", "-b", "trunk")
	g("commit", "-q", "--allow-empty", "-m", "one")
	return filepath.Join(src, ".git"), g("rev-parse", "HEAD")
}

// The opt-in real-nix check (as TestMainlineBuildRealNix): an unreachable commit
// builds through the scratch ref, which is gone afterwards, leaving loop-build
// untouched; a commit with a failing test classifies as a builder failure.
func TestGateBuildRealNix(t *testing.T) {
	if os.Getenv("MUGICREW_NIX_TEST") == "" {
		t.Skip("set MUGICREW_NIX_TEST=1 to run the real nix build")
	}
	if _, err := exec.LookPath("nix"); err != nil {
		t.Skip("no nix on PATH")
	}
	src, err := git.CommonDir()
	if err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(t.TempDir(), "repo")
	if out, err := exec.Command("git", "clone", "-q", "--branch", git.Mainline(), src, clone).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v %s", err, out)
	}
	common := filepath.Join(clone, ".git")
	g := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", clone, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	// A commit no branch reaches: the tree of main, one more parent-linked commit.
	good := g("commit-tree", "HEAD^{tree}", "-p", "HEAD", "-m", "gate")
	store, err := GateBuild(context.Background(), common, "real1", good, "mugicrew")
	if err != nil || !strings.HasPrefix(store, "/nix/store/") {
		t.Fatalf("GateBuild good = %q, %v", store, err)
	}
	if out := g("for-each-ref", "refs/mugicrew/gate"); out != "" {
		t.Fatalf("scratch ref survived: %s", out)
	}
	if _, err := os.Lstat(filepath.Join(common, "mugicrew", buildLink)); err == nil {
		t.Fatal("gate wrote loop-build")
	}

	// A failing test in the checkPhase.
	os.WriteFile(filepath.Join(clone, "apps", "mugicrew", "looplock", "zz_fail_test.go"),
		[]byte("package looplock\nimport \"testing\"\nfunc TestZZFail(t *testing.T) { t.Fatal(\"boom\") }\n"), 0o644)
	g("add", "-A")
	g("-c", "commit.gpgsign=false", "commit", "-q", "-m", "broken")
	bad := g("rev-parse", "HEAD")
	g("reset", "-q", "--hard", "origin/"+git.Mainline())
	_, err = GateBuild(context.Background(), common, "real2", bad, "mugicrew")
	var be *BuilderError
	if !errors.As(err, &be) || !strings.HasSuffix(be.Drv, ".drv") {
		t.Fatalf("GateBuild broken = %#v; want BuilderError with a .drv", err)
	}
}

func TestClassifyGateTailStartsAtLine(t *testing.T) {
	text := strings.Repeat("é", maxTail) + "\nlast line"
	got := classifyGate(context.Background(), errors.New(text)).(*InfraError)
	if got.Tail != "last line" {
		t.Errorf("Tail = %q, want the whole line after the cut", got.Tail)
	}
}
