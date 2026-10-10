// Package liveness is the effort liveness probe (ADR 0025): is anything actually
// running for a bead? It scans the local process table for a mugicrew or claude
// process carrying the bead id as an exact argv token — `mugicrew reconcile <id>`
// or `claude … --name <id>`; either present = alive. No handle is captured at
// spawn and nothing is persisted; the verdict is a view (ADR 0031), re-derived on
// every call and never read by dispatch. Stdlib only, a leaf the TUI can share
// with `ps`.
package liveness

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// procDir is the process table root, a var so tests point it at a fake tree.
// ponytail: Linux-only /proc scan; reach for gopsutil if mugicrew ever targets
// non-Linux (OE ADR 0007's static binary doesn't need it today). Matching argv[0]
// assumes the native claude binary; an npm install runs as `node …/claude` and
// would read dead — match argv[1]'s basename too if that install ever appears.
var procDir = "/proc"

// Alive reports, for every queried id, whether some live mugicrew/claude process
// carries it as an exact argv token (so ws-a13.4 never matches ws-a13.40, nor an
// id buried inside a prompt). One process-table scan per call, not per id. A pid
// that vanishes mid-scan or can't be read is skipped, never an error.
func Alive(ids []string) map[string]bool {
	alive := make(map[string]bool, len(ids))
	for _, id := range ids {
		alive[id] = false
	}
	scan(func(_ int, tok string) {
		if _, ok := alive[tok]; ok {
			alive[tok] = true
		}
	})
	return alive
}

// Pids are the pids of every live mugicrew/claude process carrying id as an
// exact argv token, on Alive's match: what an abort signals (ADR 0083 §6).
func Pids(id string) []int {
	var pids []int
	scan(func(pid int, tok string) {
		if tok == id && !slices.Contains(pids, pid) {
			pids = append(pids, pid)
		}
	})
	return pids
}

// scan calls match with each argv token (argv[1:]) of every readable process
// whose argv[0] basename is mugicrew or claude, one /proc pass.
func scan(match func(pid int, tok string)) {
	paths, _ := filepath.Glob(filepath.Join(procDir, "[0-9]*", "cmdline"))
	for _, p := range paths {
		pid, err := strconv.Atoi(filepath.Base(filepath.Dir(p)))
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(p)
		if err != nil || len(raw) == 0 { // vanished, unreadable, or a kernel thread
			continue
		}
		argv := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
		if name := filepath.Base(argv[0]); name != "mugicrew" && name != "claude" {
			continue
		}
		for _, tok := range argv[1:] {
			match(pid, tok)
		}
	}
}
