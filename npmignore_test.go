package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"slices"
	"testing"
)

// requireGitNpmTree skips when the test can't observe what it checks: the Nix
// check sandbox has no .git and the flake src excludes npm/.
func requireGitNpmTree(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("npm"); err != nil {
		t.Skip("no npm/ in this tree")
	}
	if err := exec.Command("git", "rev-parse", "--is-inside-work-tree").Run(); err != nil {
		t.Skip("not inside a git work tree")
	}
}

// A CI-built binary placed in any per-platform npm package must be gitignored.
func TestNpmPlatformBinariesIgnored(t *testing.T) {
	requireGitNpmTree(t)
	for _, p := range []string{"linux-x64", "linux-arm64", "darwin-x64", "darwin-arm64"} {
		path := "npm/mugicrew-" + p + "/bin/mugicrew"
		if err := exec.Command("git", "check-ignore", "--no-index", "-q", path).Run(); err != nil {
			t.Errorf("%s is not gitignored: %v", path, err)
		}
	}
}

// The launcher shim must stay tracked.
func TestNpmLauncherNotIgnored(t *testing.T) {
	requireGitNpmTree(t)
	if err := exec.Command("git", "check-ignore", "--no-index", "-q", "npm/mugicrew/bin/mugicrew.js").Run(); err == nil {
		t.Error("npm/mugicrew/bin/mugicrew.js is gitignored")
	}
}

// The package README must ship in the tarball.
func TestNpmReadmeShips(t *testing.T) {
	requireGitNpmTree(t)
	b, err := os.ReadFile("npm/mugicrew/package.json")
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct{ Files []string }
	if err := json.Unmarshal(b, &pkg); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(pkg.Files, "README.md") {
		t.Errorf("files %v omit README.md", pkg.Files)
	}
	if _, err := os.Stat("npm/mugicrew/README.md"); err != nil {
		t.Error(err)
	}
}
