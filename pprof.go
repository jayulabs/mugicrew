package main

import (
	"fmt"
	"io"
	"os"
	"runtime/pprof"
)

// startPprof is the env-gated CPU profile of the TUI (Spec mc-zdqv decision
// E): with MUGICREW_PPROF=<file> it profiles until the returned stop runs. Unset
// is a no-op; a create failure warns on w and the TUI carries on unprofiled.
func startPprof(w io.Writer) (stop func()) {
	p := os.Getenv("MUGICREW_PPROF")
	if p == "" {
		return func() {}
	}
	f, err := os.Create(p)
	if err != nil {
		fmt.Fprintf(w, "mugicrew: MUGICREW_PPROF: %v\n", err)
		return func() {}
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		fmt.Fprintf(w, "mugicrew: MUGICREW_PPROF: %v\n", err)
		f.Close()
		return func() {}
	}
	return func() { pprof.StopCPUProfile(); f.Close() }
}
