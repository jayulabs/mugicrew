package bd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// GraphDir resolves the Bead Graph the way bd does (Spec FR4, FR7): an
// in-tree .beads/, a .beads/redirect and an ambient BEADS_DIR all yield the
// graph's absolute dir; a repo with none is the typed ErrNoGraph, whose message
// names the bd init commands (FR6). Real bd in scratch repos, skipped without it
// (the nix sandbox checkPhase).
func TestGraphDirResolvesLikeBd(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not on PATH")
	}
	t.Setenv("BEADS_DIR", "")
	os.Unsetenv("BEADS_DIR")
	gitInit := func(t *testing.T) string {
		dir := t.TempDir()
		if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
			t.Fatalf("git init: %v %s", err, out)
		}
		return dir
	}

	none := gitInit(t)
	_, err := GraphDir(none)
	var ng *NoGraphError
	if !errors.As(err, &ng) {
		t.Fatalf("GraphDir(no graph) = %v, want a *NoGraphError", err)
	}
	for _, want := range []string{none, "mugicrew init"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not name %q", err, want)
		}
	}

	stealth := gitInit(t)
	cmd := exec.Command("bd", "init", "--stealth", "-q")
	cmd.Dir = stealth
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bd init --stealth: %v %s", err, out)
	}
	graph := filepath.Join(stealth, ".beads")
	if got, err := GraphDir(stealth); err != nil || got != graph {
		t.Errorf("GraphDir(in-tree) = %q, %v; want %q", got, err, graph)
	}

	redirected := gitInit(t)
	os.Mkdir(filepath.Join(redirected, ".beads"), 0o700)
	if err := os.WriteFile(filepath.Join(redirected, ".beads", "redirect"), []byte(graph+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := GraphDir(redirected); err != nil || got != graph {
		t.Errorf("GraphDir(redirect) = %q, %v; want %q", got, err, graph)
	}

	t.Setenv("BEADS_DIR", graph)
	if got, err := GraphDir(none); err != nil || got != graph {
		t.Errorf("GraphDir(ambient BEADS_DIR) = %q, %v; want %q", got, err, graph)
	}
}
