package liveness

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// lsofPath is the lsof binary InTree runs, a var so tests point it at a missing
// one. Process reads here are CGO-free sysctls (ADR 0195); only cwds need lsof.
var lsofPath = "lsof"

// argvTable is every readable process's pid and argv: kern.proc.all lists pids,
// kern.procargs2 gives argv. Any error (another user's pid, an exiting one:
// EINVAL) skips the pid. Matching is on argv[0]'s basename, never p_comm, which
// native claude sets to its version string (see scan).
func argvTable() []procArgv {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil
	}
	var out []procArgv
	for _, p := range procs {
		pid := int(p.Proc.P_pid)
		raw, err := unix.SysctlRaw("kern.procargs2", pid)
		if err != nil {
			continue
		}
		if argv := parseProcArgs(raw); len(argv) > 0 {
			out = append(out, procArgv{pid, argv})
		}
	}
	return out
}

// parseProcArgs reads a KERN_PROCARGS2 buffer: little-endian int32 argc, exec
// path, NUL padding, then argc NUL-terminated argv strings. A short or truncated
// buffer yields what it holds.
func parseProcArgs(raw []byte) []string {
	if len(raw) < 4 {
		return nil
	}
	argc := int(binary.LittleEndian.Uint32(raw))
	rest := raw[4:]
	i := bytes.IndexByte(rest, 0)
	if i < 0 {
		return nil
	}
	rest = bytes.TrimLeft(rest[i:], "\x00")
	var argv []string
	for len(argv) < argc && len(rest) > 0 {
		j := bytes.IndexByte(rest, 0)
		if j < 0 {
			j = len(rest)
		}
		argv = append(argv, string(rest[:j]))
		rest = rest[min(j+1, len(rest)):]
	}
	return argv
}

// parseLsofCwd reads `lsof -F pn` output: p<pid> then n<path> records.
func parseLsofCwd(out []byte) map[int]string {
	cwds := map[int]string{}
	pid := 0
	for _, l := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(l, "p"):
			pid, _ = strconv.Atoi(l[1:])
		case strings.HasPrefix(l, "n") && pid > 0:
			cwds[pid] = l[1:]
		}
	}
	return cwds
}

// InTree is the pid of every process, whatever its argv, whose cwd lies in tree
// (ADR 0127 §2), from one `lsof -a -d cwd -F pn` run. tree is resolved through
// symlinks first, as lsof's paths are. lsof exits non-zero when it can't read
// some pids; its output still counts if it ran. A missing or unstartable lsof is
// an error, so recover refuses the tree rather than reading it as unheld.
func InTree(tree string, exclude []int) ([]int, error) {
	if r, err := filepath.EvalSymlinks(tree); err == nil {
		tree = r
	}
	tree = filepath.Clean(tree)
	// lsof lists itself (and any helper it forks); run it from / so those never
	// fall in tree, as the caller's own cwd might.
	cmd := exec.Command(lsofPath, "-a", "-d", "cwd", "-F", "pn")
	cmd.Dir = "/"
	out, err := cmd.Output()
	if _, ran := err.(*exec.ExitError); err != nil && !ran {
		return nil, fmt.Errorf("liveness: cannot read process cwds: %w", err)
	}
	var pids []int
	for pid, cwd := range parseLsofCwd(out) {
		if !slices.Contains(exclude, pid) && (cwd == tree || strings.HasPrefix(cwd, tree+string(filepath.Separator))) {
			pids = append(pids, pid)
		}
	}
	slices.Sort(pids)
	return pids, nil
}

// Lineage is pid and its ancestors, nearest first, off kern.proc.pid's ppid: what
// a verb excludes from its own scans so its argv or cwd (and its shell's) never
// reads as the bead's holder. Any error ends the walk.
func Lineage(pid int) []int {
	out := []int{pid}
	for pid > 1 {
		k, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
		if err != nil {
			break
		}
		if pid = int(k.Eproc.Ppid); pid <= 0 || slices.Contains(out, pid) {
			break
		}
		out = append(out, pid)
	}
	return out
}
