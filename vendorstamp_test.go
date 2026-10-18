package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"regexp"
	"testing"
)

var goSumStamp = regexp.MustCompile(`(?m)^\s*# go\.sum sha256: ([0-9a-f]{64})\s*$`)

// flake.nix's vendorHash is only valid for the go.sum it was computed against.
// The stamp next to it records that go.sum's sha256, so a module change that
// skips the vendorHash refresh fails here instead of in the loop's nix build.
func TestFlakeVendorHashStampMatchesGoSum(t *testing.T) {
	flake, err := os.ReadFile("flake.nix")
	if os.IsNotExist(err) {
		t.Skip("flake.nix is excluded from the nix build source")
	}
	if err != nil {
		t.Fatal(err)
	}
	goSum, err := os.ReadFile("go.sum")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(goSum)
	actual := hex.EncodeToString(sum[:])

	m := goSumStamp.FindSubmatch(flake)
	if m == nil {
		t.Fatalf("flake.nix has no stamp line `# go.sum sha256: <64 lowercase hex>` next to vendorHash; go.sum sha256 is %s", actual)
	}
	if stamped := string(m[1]); stamped != actual {
		t.Fatalf("vendorHash is stale: flake.nix stamp %s, go.sum sha256 %s\n"+
			"Refresh: set vendorHash to lib.fakeHash, run `nix build ./apps/mugicrew#mugicrew`, "+
			"copy the reported hash into vendorHash, and write %s into the stamp.",
			stamped, actual, actual)
	}
}
