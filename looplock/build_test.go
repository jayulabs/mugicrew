package looplock

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/git"
)

// The one real-nix check (opt-in: MUGICREW_NIX_TEST=1 and nix on PATH, so it
// never runs in checkPhase): a temp clone's main builds to a store binary rooted
// under its common dir, the outPath eval names that store dir, and a dirty
// worktree doesn't change the path.
func TestMainlineBuildRealNix(t *testing.T) {
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
	rev, bin, err := MainlineBuild(common, common)
	if err != nil {
		t.Fatal(err)
	}
	if len(rev) != 40 || !strings.HasPrefix(bin, "/nix/store/") || !strings.HasSuffix(bin, "/bin/mugicrew") {
		t.Fatalf("MainlineBuild = %q, %q; want a full SHA and a /nix/store/…/bin/mugicrew", rev, bin)
	}
	if _, err := os.Lstat(filepath.Join(common, "mugicrew", buildLink)); err != nil {
		t.Fatalf("out-link: %v", err)
	}
	// The loop compares this eval with its own store dir; drift restarts it forever (ADR 0064 §4).
	if out, err := OutPathAt(context.Background(), common, rev); err != nil || out != filepath.Dir(filepath.Dir(bin)) {
		t.Fatalf("OutPathAt = %q, %v; want the build's store dir %q", out, err, filepath.Dir(filepath.Dir(bin)))
	}
	os.WriteFile(filepath.Join(clone, "apps", "mugicrew", "main.go"), []byte("package main // dirty\n"), 0o644)
	os.WriteFile(filepath.Join(clone, "untracked.go"), []byte("dirty"), 0o644)
	if rev2, bin2, err := MainlineBuild(common, common); err != nil || rev2 != rev || bin2 != bin {
		t.Fatalf("dirty rebuild = %q, %q, %v; want %q, %q", rev2, bin2, err, rev, bin)
	}
}

// A from-source loop builds the source repo's mainline, not the Workspace's (ADR
// 0090 §2, §6): the flake ref names the source common dir, the source's detected
// mainline (origin/HEAD → trunk) and its SHA there, while the out-link lands under
// the Workspace's state dir. A stub nix records its argv.
func TestMainlineBuildFromSourceIntoWorkspaceState(t *testing.T) {
	src := t.TempDir()
	g := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", src, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	g("init", "-q", "-b", "trunk")
	g("commit", "-q", "--allow-empty", "-m", "one")
	g("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
	rev, srcCommon := g("rev-parse", "trunk"), filepath.Join(src, ".git")
	state := t.TempDir() // the Workspace's git common dir

	bin, log := t.TempDir(), filepath.Join(t.TempDir(), "nix.log")
	os.WriteFile(filepath.Join(bin, "nix"), []byte("#!/bin/sh\necho \"$*\" >> "+log+"\necho /nix/store/x-mugicrew\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	gotRev, gotBin, err := MainlineBuild(srcCommon, state)
	if err != nil {
		t.Fatal(err)
	}
	if gotRev != rev || gotBin != "/nix/store/x-mugicrew/bin/mugicrew" {
		t.Fatalf("MainlineBuild = %q, %q; want %q, the store bin", gotRev, gotBin, rev)
	}
	b, _ := os.ReadFile(log)
	want := "build git+file://" + srcCommon + "?ref=trunk&rev=" + rev + "#mugicrew --out-link " + filepath.Join(state, "mugicrew", buildLink) + " --print-out-paths\n"
	if string(b) != want {
		t.Fatalf("nix argv:\n%s\nwant:\n%s", b, want)
	}
}
