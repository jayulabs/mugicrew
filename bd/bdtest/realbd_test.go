package bdtest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
)

const scratchPrefix = "bdct"

// TestRealBdContract runs the contract suite against real bd on a scratch embedded-Dolt graph
// it provisions itself. Opt-in like MUGICREW_NIX_TEST, so the Nix checkPhase needs neither bd
// nor a graph.
func TestRealBdContract(t *testing.T) {
	if os.Getenv("MUGICREW_BD_TEST") == "" {
		t.Skip("set MUGICREW_BD_TEST=1 to run the contract suite against a scratch bd graph")
	}
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("no bd on PATH")
	}
	dir := t.TempDir()
	Contract(t, func(t *testing.T) bd.Core { return provisionScratch(t, dir) },
		func(t *testing.T, id, status, assignee string) {
			t.Helper()
			if _, err := bd.RunIn(dir, "update", id, "--status", status, "--assignee", assignee); err != nil {
				t.Fatalf("bd update %s --status %s --assignee %s: %v", id, status, assignee, err)
			}
		})
}

// provisionScratch points this process's bd at a fresh graph under dir and returns the real
// (cli) core. It writes nothing until bd provably resolves to that graph.
func provisionScratch(t *testing.T, dir string) bd.Core {
	t.Helper()
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if k == "BEADS_DB" || strings.HasPrefix(k, "BEADS_DOLT_") {
			t.Setenv(k, "") // registers the restore
			os.Unsetenv(k)
		}
	}
	beadsDir := filepath.Join(dir, ".beads")
	t.Setenv("BEADS_DIR", beadsDir)
	t.Setenv("BEADS_ACTOR", Actor)
	t.Cleanup(bd.PinActor(Actor))
	t.Cleanup(bd.PinGraph(beadsDir))

	if _, err := bd.RunIn(dir, "init", "--prefix", scratchPrefix, "--non-interactive", "-q", "--skip-agents", "--skip-hooks"); err != nil {
		t.Fatalf("bd init: %v", err)
	}
	if err := verifyScratch(dir, beadsDir); err != nil {
		t.Fatal(err)
	}
	return bd.CLI()
}

// verifyScratch fails unless bd resolves to the scratch .beads with the scratch prefix.
func verifyScratch(dir, beadsDir string) error {
	got, err := bd.GraphDir(dir)
	if err != nil {
		return fmt.Errorf("bd where: %w", err)
	}
	gotReal, err1 := filepath.EvalSymlinks(got)
	wantReal, err2 := filepath.EvalSymlinks(beadsDir)
	if err1 != nil || err2 != nil || gotReal != wantReal {
		return fmt.Errorf("bd resolves to %q, not the scratch graph %q: refusing to write", got, beadsDir)
	}
	prefix, err := bd.RunIn(dir, "config", "get", "issue_prefix")
	if err != nil || strings.TrimSpace(prefix) != scratchPrefix {
		return fmt.Errorf("scratch issue_prefix = %q (%v), want %q: refusing to write", prefix, err, scratchPrefix)
	}
	return nil
}

// A provisioning check that does not resolve to the scratch graph aborts before any write.
func TestVerifyScratchRefusesAForeignGraph(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("no bd on PATH")
	}
	if err := verifyScratch(t.TempDir(), filepath.Join(t.TempDir(), ".beads")); err == nil {
		t.Fatal("verifyScratch of a graph that is not there = nil, want a refusal")
	}
}
