package bd

import (
	"strings"
	"testing"
)

// While a graph is pinned, an exec whose BEADS_DIR drifted or whose argv redirects with -C
// or --dir is refused before it reaches bd.
func TestPinGraphRefusesDriftAndRedirect(t *testing.T) {
	oonce, obatch := runOnce, runBatchOnce
	t.Cleanup(func() { runOnce, runBatchOnce = oonce, obatch })
	runOnce = func(string, ...string) (string, error) { return "ok", nil }
	t.Setenv("BEADS_DIR", "/scratch/.beads")
	t.Cleanup(PinGraph("/scratch/.beads"))

	if _, err := runIn("", "list"); err != nil {
		t.Fatalf("pinned and undrifted = %v, want it to run", err)
	}
	for _, argv := range [][]string{{"-C", "/live", "list"}, {"list", "--dir", "/live"}, {"list", "--dir=/live"}} {
		if _, err := runIn("", argv...); err == nil || !strings.Contains(err.Error(), "refused") {
			t.Errorf("runIn(%v) = %v, want a refusal", argv, err)
		}
	}
	t.Setenv("BEADS_DIR", "/live/.beads")
	if _, err := runIn("", "list"); err == nil || !strings.Contains(err.Error(), "pinned") {
		t.Errorf("drifted BEADS_DIR = %v, want a refusal", err)
	}
	if err := runBatchOnce("close x\n"); err == nil || !strings.Contains(err.Error(), "pinned") {
		t.Errorf("runBatchOnce drifted = %v, want a refusal", err)
	}
}

func TestPinGraphRestores(t *testing.T) {
	restore := PinGraph("/x/.beads")
	restore()
	if pinned != "" {
		t.Fatalf("pinned = %q after restore", pinned)
	}
}
