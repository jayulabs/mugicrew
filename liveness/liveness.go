// Package liveness is the effort liveness probe (ADR 0025): is anything
// actually running for a bead? It scans the local process table for a mugicrew
// or claude process carrying the bead id as an exact argv token —
// `mugicrew reconcile <id>` or `claude … --name <id>` (on the mugicrew side
// only the effort verbs reconcile and run count; a read-only or repair verb
// naming the bead, such as transcript, is not its holder) — or for the dispatcher's
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
	"sync"
	"syscall"
	"time"
)

// procArgv is one process-table row: a pid and its argv.
type procArgv struct {
	pid  int
	argv []string
}

// procRow is one row of the orphan reading's table: a pid, its argv (nil when
// unreadable) and its cwd when asked for (hasCwd false when unreadable).
type procRow struct {
	pid    int
	argv   []string
	cwd    string
	hasCwd bool
}

// effortVerbs are the mugicrew verbs that hold an effort: reconcile and the
// hidden run-now host run. hitl-run is left off on purpose: its claude child
// (--name <id>) is what keeps a HITL bead alive.
var effortVerbs = []string{"reconcile", "run"}

// mugicrewVerb is the verb of a mugicrew argv: the first token after argv[0],
// skipping a leading global -C <dir> pair (the only prefix main strips).
func mugicrewVerb(argv []string) string {
	args := argv[1:]
	if len(args) >= 2 && args[0] == "-C" {
		args = args[2:]
	}
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// holdsEffort reports whether argv is a claude process (argv[0] basename, Nix's
// .<name>-wrapped included) or a mugicrew process whose verb is in effortVerbs.
func holdsEffort(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(argv[0]), "."), "-wrapped")
	return name == "claude" || name == "mugicrew" && slices.Contains(effortVerbs, mugicrewVerb(argv))
}

// HoldsEffort reports whether a process running argv would read as id's effort
// holder under the process-table scan: for an effort spawn site's tests, which
// pass the argv their builder produces so a verb off the allowlist fails them.
func HoldsEffort(argv []string, id string) bool {
	return holdsEffort(argv) && slices.Contains(argv[1:], id)
}

// scan calls match with each argv token (argv[1:]) of every readable process
// over the platform's process table (argvTable) that holds an effort: a claude
// process (argv[0] basename, Nix's .<name>-wrapped included), or a mugicrew
// process whose verb is in effortVerbs. Other mugicrew verbs contribute no
// tokens. Matching argv[0] assumes the native claude binary; an npm install runs
// as `node …/claude` and would read dead — match argv[1]'s basename too if that
// install ever appears.
func scan(match func(pid int, tok string)) {
	for _, p := range argvTable() {
		if !holdsEffort(p.argv) {
			continue
		}
		for _, tok := range p.argv[1:] {
			match(p.pid, tok)
		}
	}
}

// Alive reports, for every queried id, whether some live effort-verb mugicrew
// (reconcile, run) or claude process carries it as an exact argv token (so ws-a13.4 never matches ws-a13.40, nor an
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

// Pids are the pids of every live effort-verb mugicrew or claude process carrying id as an
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
type Hold struct {
	f  *os.File
	id string
}

// held is this process's live holds by effort id: Holding reads it, so a write
// serialized by a hold can name that hold without the caller threading it.
var (
	heldMu sync.Mutex
	held   = map[string]*Hold{}
)

func register(h *Hold) *Hold {
	heldMu.Lock()
	held[h.id] = h
	heldMu.Unlock()
	return h
}

// Holding reports whether this process holds id's dispatcher hold now: taken by
// Acquire or Adopt and not yet Released.
func Holding(id string) bool {
	heldMu.Lock()
	defer heldMu.Unlock()
	return held[id] != nil
}

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
			return register(&Hold{f, id}), nil
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
	return register(&Hold{os.NewFile(uintptr(fd), id+".lock"), id})
}

// Release drops the hold. It never unlinks the file: unlink-on-release let two
// holders (or a dead verdict on a held lock) through in the ADR 0154 prototype.
func (h *Hold) Release() {
	if h != nil {
		heldMu.Lock()
		if held[h.id] == h {
			delete(held, h.id)
		}
		heldMu.Unlock()
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

// Effort is one active AFK effort the orphan reading judges: its bead id and its
// tree path, "" for a repo-root placement or a worktree-route bead with no tree
// (nothing to scan: the flock and argv alone decide it).
type Effort struct{ ID, Tree string }

// Orphans is the shared reading of an Orphaned claim (ADR 0391): per effort id,
// true when it is orphaned. A bead is alive, so false, if a live effort-verb
// mugicrew or claude process carries its id as an exact argv token, or its
// per-effort flock is held, or some process's cwd lies in its Tree (InTree's rule:
// symlinks resolved, " (deleted)" counted). Otherwise it is orphaned. The pids in
// exclude (the caller's Lineage) count for neither argv nor cwd. A platform that
// can't read cwds reads a Tree bead as not orphan, never as unheld. The process
// table is walked once for argv and cwd together (procTable; cwds only when some
// effort has a Tree); no history is read (ADR 0033). HITL beads are the caller's to leave out.
func Orphans(efforts []Effort, exclude []int) map[string]bool {
	return OrphansIn(os.Getenv("BEADS_DIR"), efforts, exclude)
}

// OrphansIn is Orphans with the flock read under graph dir beadsDir, for a caller
// (recover, from a bare shell) that resolved it itself.
func OrphansIn(beadsDir string, efforts []Effort, exclude []int) map[string]bool {
	orphan := make(map[string]bool, len(efforts))
	trees := map[string]string{} // id -> resolved tree
	for _, e := range efforts {
		orphan[e.ID] = true
		if e.Tree != "" {
			t := e.Tree
			if r, err := filepath.EvalSymlinks(t); err == nil {
				t = r
			}
			trees[e.ID] = filepath.Clean(t)
		}
	}
	wantCwd := len(trees) > 0
	rows, err := procTable(wantCwd)
	for _, p := range rows {
		if slices.Contains(exclude, p.pid) || !holdsEffort(p.argv) {
			continue
		}
		for _, tok := range p.argv[1:] {
			if _, ok := orphan[tok]; ok {
				orphan[tok] = false
			}
		}
	}
	for id := range orphan {
		if orphan[id] && HeldIn(beadsDir, id) {
			orphan[id] = false
		}
	}
	for id, tree := range trees {
		if !orphan[id] {
			continue
		}
		if err != nil {
			orphan[id] = false
			continue
		}
		for _, p := range rows {
			cwd := strings.TrimSuffix(p.cwd, " (deleted)")
			if p.hasCwd && !slices.Contains(exclude, p.pid) && (cwd == tree || strings.HasPrefix(cwd, tree+string(filepath.Separator))) {
				orphan[id] = false
				break
			}
		}
	}
	return orphan
}
