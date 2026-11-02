// Package selfexe holds mugicrew's own executable path, captured once when the
// process starts (ADR 0406 §2). A later os.Executable() reads /proc/self/exe,
// which names a ghost path once npm replaces the binary under a running loop or
// TUI; every re-exec of mugicrew reads Path instead and reaches the new binary.
package selfexe

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

var startPath, startErr = os.Executable() // package init: before main and anything importing this

// Path is the path mugicrew was started from. A var so tests stub it.
var Path = func() (string, error) { return startPath, startErr }

// Stub points Path at p for the test and returns the restore.
func Stub(p string) (restore func()) {
	old := Path
	Path = func() (string, error) { return p, nil }
	return func() { Path = old }
}

// Identity is a file's (device, inode): what npm replaces when it swaps the
// binary under a running loop or TUI (ADR 0406 §3, §5).
type Identity struct{ Dev, Ino uint64 }

// Identify stats path and returns its identity; a missing file is the error.
// Stat only, no subprocess, the same on Linux and darwin. A var so tests stub it.
var Identify = func(path string) (Identity, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return Identity{}, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return Identity{}, errors.New("selfexe: no stat identity on this platform")
	}
	return Identity{Dev: uint64(st.Dev), Ino: uint64(st.Ino)}, nil
}

// Swapped reports whether the file at path is no longer the one recorded as
// was: a different identity, or a missing file ("changed but not yet complete").
func Swapped(path string, was Identity) bool {
	now, err := Identify(path)
	return err != nil || now != was
}

// Version is `path --version`'s answer, trimmed, bounded to 5s. A var so tests
// stub it.
var Version = func(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	return strings.TrimSpace(string(out)), err
}
