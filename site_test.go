package main

import (
	"os"
	"strings"
	"testing"
)

// The explainer site is not a build input of the binary: the flake's src
// exclusion must list it so site edits keep the Nix outPath.
func TestFlakeExcludesSite(t *testing.T) {
	if _, err := os.Stat("site"); err != nil {
		t.Skip("no site/ in this tree")
	}
	b, err := os.ReadFile("flake.nix")
	if err != nil {
		t.Skip("no flake.nix in this tree")
	}
	if !strings.Contains(string(b), "(pkgs.lib.fileset.maybeMissing ./site)") {
		t.Error("flake.nix src exclusion does not list ./site as maybeMissing")
	}
}
