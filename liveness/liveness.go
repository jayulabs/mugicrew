// Package liveness is the effort liveness probe (ADR 0025): is anything
// actually running for a bead? It scans the local process table for a mugicrew
// or claude process carrying the bead id as an exact argv token —
// `mugicrew reconcile <id>` or `claude … --name <id>` — or for the dispatcher's
// per-effort flock, held from claim to release (ADR 0154); either present =
// alive. No handle is captured at spawn and nothing is persisted; the verdict
// is a view (ADR 0031), re-derived on every call (loop.LiveAFK feeds contention
// with it, ADR 0054). InTree adds recover's cwd scan (ADR 0127 §2): any
// process working inside a bead's tree. Stdlib only, a leaf the TUI can share
// with `ps`.
package liveness

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// procArgv is one process-table row: a pid and its argv.
type procArgv struct {
	pid  int
	argv []string
}

// scan calls match with each argv token (argv[1:]) of every readable process
// whose argv[0] basename is mugicrew or claude (Nix's .<name>-wrapped included),
// over the platform's process table (argvTable). Matching argv[0] assumes the
// native claude binary; an npm install runs as `node …/claude` and would read
// dead — match argv[1]'s basename too if that install ever appears.
func scan(match func(pid int, tok string)) {
	for _, p := range argvTable() {
		name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p.argv[0]), "."), "-wrapped")
		if name != "mugicrew" && name != "claude" {
			continue
		}
		for _, tok := range p.argv[1:] {
			match(p.pid, tok)
		}
	}
}

// Alive reports, for every queried id, whether some live mugicrew/claude process
// carries it as an exact argv token (so ws-a13.4 never matches ws-a13.40, nor an
// id buried inside a prompt) or its per-effort lock is held (Held). One
// process-table scan per call, not per id. A pid that vanishes mid-scan or can't
// be read is skipped, never an error.
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
	for _, id := range ids {
		if !alive[id] {
			alive[id] = Held(id)
		}
	}
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

// lockDir is where per-effort locks live; "" resolves to
// <BEADS_DIR>/mugicrew-efforts, beside the loop lock. A var so tests point it at
// a temp dir.
var lockDir string

// acquireCap bounds Acquire's retry: running out means another dispatcher holds it.
const acquireCap = 100 * time.Millisecond

// lockPath is id's lock file, or "" when no graph dir is known.
func lockPath(id string) string { return lockPathIn(os.Getenv("BEADS_DIR"), id) }

// lockPathIn is lockPath for a graph dir the caller already resolved (recover's
// bare shell has no BEADS_DIR exported).
func lockPathIn(beadsDir, id string) string {
	dir := lockDir
	if dir == "" {
		b := beadsDir
		if b == "" {
			return ""
		}
		dir = filepath.Join(b, "mugicrew-efforts")
	}
	return filepath.Join(dir, id+".lock")
}

// Hold is the dispatcher's per-effort hold (ADR 0154).
type Hold struct{ f *os.File }

// Acquire takes id's exclusive flock, retried every ~1ms on EWOULDBLOCK/EINTR
// (a probe's momentary shared lock must not fail a claim) up to acquireCap; an
// error then means "held by another dispatcher". The fd keeps Go's O_CLOEXEC, so
// children never inherit the hold.
func Acquire(id string) (*Hold, error) {
	path := lockPath(id)
	if path == "" {
		return nil, errors.New("liveness: BEADS_DIR unset, no lock dir")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(acquireCap)
	for {
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
		if err != nil {
			return nil, err
		}
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &Hold{f}, nil
		}
		f.Close()
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("effort %s held by another dispatcher: %w", id, err)
		}
		time.Sleep(time.Millisecond)
	}
}

// File is the hold's flock-bearing file, for a parent to pass a child through
// exec.Cmd.ExtraFiles (ADR 0330). The child adopts it with Adopt; the parent
// keeps ownership and Releases its own copy.
func (h *Hold) File() *os.File { return h.f }

// Adopt re-wraps an inherited fd as id's Hold and re-arms O_CLOEXEC on it, so
// the adopter's own children (reconcile, claude) never inherit the lock.
func Adopt(fd int, id string) *Hold {
	syscall.CloseOnExec(fd)
	return &Hold{os.NewFile(uintptr(fd), id+".lock")}
}

// Release drops the hold. It never unlinks the file: unlink-on-release let two
// holders (or a dead verdict on a held lock) through in the ADR 0154 prototype.
func (h *Hold) Release() {
	if h != nil {
		h.f.Close()
	}
}

// Held is the reader's try-lock: no file, or a shared lock acquired = dead;
// EWOULDBLOCK = alive. It never creates the file.
func Held(id string) bool { return HeldIn(os.Getenv("BEADS_DIR"), id) }

// HeldIn is Held under graph dir beadsDir, for a caller that resolved it itself.
func HeldIn(beadsDir, id string) bool {
	path := lockPathIn(beadsDir, id)
	if path == "" {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	return errors.Is(syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB), syscall.EWOULDBLOCK)
}
