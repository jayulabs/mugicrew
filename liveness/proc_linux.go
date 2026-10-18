package liveness

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// procDir is the process table root, a var so tests point it at a fake tree.
// Nix's makeWrapper naming (.<name>-wrapped) is undone before matching argv[0]
// (scan), so the pinned build's self-spawned children count. The process reads
// are split by build tag (proc_linux.go, proc_darwin.go; ADR 0195), not a
// gopsutil-style dependency.
var procDir = "/proc"

// argvTable is every readable process's pid and argv, one /proc pass. A pid that
// vanished, can't be read or has no argv (a kernel thread) is skipped.
func argvTable() []procArgv {
	var out []procArgv
	paths, _ := filepath.Glob(filepath.Join(procDir, "[0-9]*", "cmdline"))
	for _, p := range paths {
		pid, err := strconv.Atoi(filepath.Base(filepath.Dir(p)))
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(p)
		if err != nil || len(raw) == 0 {
			continue
		}
		out = append(out, procArgv{pid, strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")})
	}
	return out
}

// InTree is the pid of every process, whatever its argv, whose /proc/<pid>/cwd
// lies in tree (ADR 0127 §2): an orphaned tool grandchild or a reconcile child the
// argv scan can't see still holds the bead. tree is resolved through symlinks
// first, as the kernel's cwd is; a cwd the kernel marks " (deleted)" still counts.
// A vanished pid or an unreadable cwd (EACCES: another uid's, never an effort's)
// is skipped, and so is every excluded pid. The error is non-nil when a platform
// can't read cwds at all (recover then refuses the tree, never reads it as unheld);
// the /proc scan always returns nil.
func InTree(tree string, exclude []int) ([]int, error) {
	if r, err := filepath.EvalSymlinks(tree); err == nil {
		tree = r
	}
	tree = filepath.Clean(tree)
	paths, _ := filepath.Glob(filepath.Join(procDir, "[0-9]*"))
	var pids []int
	for _, p := range paths {
		pid, err := strconv.Atoi(filepath.Base(p))
		if err != nil || slices.Contains(exclude, pid) {
			continue
		}
		cwd, err := os.Readlink(filepath.Join(p, "cwd"))
		if err != nil {
			continue
		}
		cwd = strings.TrimSuffix(cwd, " (deleted)")
		if cwd == tree || strings.HasPrefix(cwd, tree+string(filepath.Separator)) {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

// Lineage is pid and its ancestors, nearest first, off each /proc/<pid>/stat's
// ppid: what a verb excludes from its own scans so its argv or cwd (and its
// shell's) never reads as the bead's holder. An unreadable stat ends the walk.
func Lineage(pid int) []int {
	out := []int{pid}
	for pid > 1 {
		raw, err := os.ReadFile(filepath.Join(procDir, strconv.Itoa(pid), "stat"))
		if err != nil {
			break
		}
		// comm (field 2) may hold spaces and parens; the state and ppid follow the last ')'.
		f := strings.Fields(string(raw[strings.LastIndexByte(string(raw), ')')+1:]))
		if len(f) < 2 {
			break
		}
		if pid, err = strconv.Atoi(f[1]); err != nil || pid <= 0 || slices.Contains(out, pid) {
			break
		}
		out = append(out, pid)
	}
	return out
}
