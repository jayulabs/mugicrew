package bd

import (
	"fmt"
	"os"
	"strings"
)

// pinned is the BEADS_DIR a PinGraph holds every bd exec to ("" when none).
var pinned string

// PinGraph holds this process's bd execs to the graph at beadsDir until restore runs: while
// pinned, runIn and runBatchOnce refuse to exec if $BEADS_DIR is not beadsDir, or if argv
// carries -C/--dir (bd -C ignores an ambient BEADS_DIR). Fail-closed guard for the real-bd
// contract leg, which must never reach the live Bead Graph.
func PinGraph(beadsDir string) (restore func()) {
	old := pinned
	pinned = beadsDir
	return func() { pinned = old }
}

// errUnpinned is the refusal of a bd exec that has left the pinned graph, nil when it has not.
func errUnpinned(argv ...string) error {
	if pinned == "" {
		return nil
	}
	if got := os.Getenv("BEADS_DIR"); got != pinned {
		return fmt.Errorf("bd %s: refused: BEADS_DIR is %q, pinned to %q", strings.Join(argv, " "), got, pinned)
	}
	for _, a := range argv {
		if a == "-C" || a == "--dir" || strings.HasPrefix(a, "--dir=") || strings.HasPrefix(a, "-C=") {
			return fmt.Errorf("bd %s: refused: %s redirects the graph while pinned to %q", strings.Join(argv, " "), a, pinned)
		}
	}
	return nil
}

// CLI is the real bd core, for a contract leg that runs against a scratch graph.
func CLI() Core { return cli{} }
