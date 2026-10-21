package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/dispatch"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/liveness"
	"jayulabs.org/mugicrew/loop"
	"jayulabs.org/mugicrew/registry"
)

// The run-now host's seams: bd's reads and writes, Dispatch, the adopted hold and
// the crew, vars so the exit-code table is testable without a live bd or a spawn.
var (
	runShow     = bd.Show
	runUnclaim  = bd.Unclaim
	runPause    = bd.Pause
	runUnpause  = bd.Unpause
	runDispatch = dispatch.Dispatch
	runAdopt    = adoptLockFd
	runCrew     = crew
)

// adoptLockFd adopts the effort's inherited hold (ADR 0330). An fd below 3 or one
// that is not open is refused, so a bad --lock-fd never wraps a stray descriptor.
func adoptLockFd(fd int, id string) (*liveness.Hold, error) {
	if fd < 3 {
		return nil, fmt.Errorf("--lock-fd=%d: not an inherited descriptor", fd)
	}
	h := liveness.Adopt(fd, id)
	if _, err := h.File().Stat(); err != nil {
		return nil, fmt.Errorf("--lock-fd=%d: %w", fd, err)
	}
	return h, nil
}

// runSignals is the host's signal wiring (Spec D4): SIGTERM and SIGINT arrive on
// the channel, SIGHUP is ignored. stop restores the defaults.
func runSignals() (<-chan os.Signal, func()) {
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	signal.Ignore(syscall.SIGHUP)
	return sig, func() { signal.Stop(sig); signal.Reset(syscall.SIGHUP) }
}

// runRun is the hidden `mugicrew run <id> --lock-fd=N` (ADR 0330, ADR 0332): it
// hosts one already-claimed effort and returns its exit code.
func runRun(argv []string) int {
	sig, stop := runSignals()
	defer stop()
	return runHost(argv, sig, os.Stdout)
}

// runHost carries one claimed effort to its end (Spec D2) and returns the D3 exit
// code. out is the starter's capture file: Dispatch's output and the host's last
// line. A second value on sig abandons the effort (Spec D4); the first is absorbed.
func runHost(argv []string, sig <-chan os.Signal, out io.Writer) int {
	effortID := dispatch.NewEffortID() // minted once, before the first event
	var mu sync.Mutex
	in := dispatch.Inflight{ID: effortID}
	set := func(f func(*dispatch.Inflight)) { mu.Lock(); f(&in); mu.Unlock() }

	done := make(chan struct{})
	defer close(done)
	abandon := make(chan struct{})
	go func() {
		for n := 0; ; {
			select {
			case <-sig:
				if n++; n == 2 {
					close(abandon)
					return
				}
			case <-done:
				return
			}
		}
	}()

	res := make(chan int, 1)
	go func() { res <- hostWork(argv, effortID, out, set) }()
	select {
	case code := <-res:
		return code
	case <-abandon:
		mu.Lock()
		e := in
		mu.Unlock()
		if e.Bead.ID != "" {
			dispatch.Abandon([]dispatch.Inflight{e}, runPause)
		}
		if e.Hold != nil {
			e.Hold.Release()
		}
		return dispatch.RunAbandoned
	}
}

// hostWork is the host's ordered steps; set publishes the bead and hold to the
// abandon path as they become known.
func hostWork(argv []string, effortID string, out io.Writer, set func(func(*dispatch.Inflight))) int {
	var b bd.Bead
	var hold *liveness.Hold
	fail := func(err error) int { // steps 1–6: nothing dispatched
		slog.Error("run.failed", append(eventlog.BeadAttrs(b), "effort", effortID, "err", err.Error())...)
		fmt.Fprintln(out, "mugicrew run:", err)
		if b.ID != "" {
			if uerr := eventlog.RecordClaim(b, "claim.released", runUnclaim(b.ID), "effort", effortID); uerr != nil {
				fmt.Fprintln(out, "mugicrew run: unclaim:", uerr)
			}
		}
		if hold != nil {
			hold.Release()
		}
		return dispatch.RunPreDispatch
	}

	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fd := fs.Int("lock-fd", -1, "the inherited effort hold")
	if len(argv) < 1 || strings.HasPrefix(argv[0], "-") {
		return fail(errors.New("usage: mugicrew run <bead-id> --lock-fd=N"))
	}
	b.ID = argv[0]
	if err := fs.Parse(argv[1:]); err != nil || fs.NArg() != 0 {
		return fail(errors.New("usage: mugicrew run <bead-id> --lock-fd=N"))
	}
	var err error
	if hold, err = runAdopt(*fd, b.ID); err != nil {
		hold = nil
		return fail(err)
	}
	set(func(in *dispatch.Inflight) { in.Bead, in.Hold = b, hold })
	ctx, crews := newCtx(), runCrew()
	if b, err = runShow(b.ID); err != nil { // a pause written since the press is honoured; the labels drive the lifts
		b = bd.Bead{ID: argv[0]}
		return fail(err)
	}
	set(func(in *dispatch.Inflight) { in.Bead = b })
	e, err := registry.Lookup(b.Kind)
	if err != nil {
		return fail(err)
	}
	c, ok := crews[e.Owner]
	if !ok {
		return fail(fmt.Errorf("no crewmate %q wired for %s", e.Owner, b.ID))
	}

	o, derr := runDispatch(ctx, c, b, out, effortID, loop.LiveAFK(bd.ListAll, liveness.Alive))
	clean := derr == nil // the run's own return, before any lift error joins in
	var lerr error
	var label string
	lift := func(cause string, attrs ...any) {
		if uerr := runUnpause(b.ID, cause); uerr != nil {
			lerr, label = errors.Join(lerr, fmt.Errorf("lift paused:%s: %w", cause, uerr)), cause
			return
		}
		slog.Info("effort.resumed", append(append(eventlog.BeadAttrs(b), "effort", effortID, "cause", cause), attrs...)...)
	}
	if o.Cut == dispatch.CutOK && bd.PausedBy(b, bd.PausedForeignHold) {
		lift(bd.PausedForeignHold, "branch", o.Branch)
	}
	if clean && bd.PausedBy(b, bd.PausedFailing) {
		lift(bd.PausedFailing)
	}
	hold.Release() // after the last bd write (ADR 0154)

	switch {
	case derr != nil:
		fmt.Fprintln(out, "mugicrew run:", errors.Join(derr, lerr))
		return dispatch.RunFailed
	case o.Cut == dispatch.CutBusy:
		fmt.Fprintf(out, "mugicrew run: %s not run: own contention on %s\n", b.ID, o.Branch)
		return dispatch.RunNotRun
	case o.Cut == dispatch.CutHeld:
		fmt.Fprintf(out, "mugicrew run: %s not run: %s is held by %s\n", b.ID, o.Branch, o.Holder.Path)
		return dispatch.RunNotRun
	case o.Aborted:
		fmt.Fprintln(out, "mugicrew run:", b.ID, "aborted")
		return dispatch.RunFailed
	case o.Stranded:
		fmt.Fprintln(out, "mugicrew run:", b.ID, "stranded at integrate")
		return dispatch.RunFailed
	case o.Limit != nil:
		fmt.Fprintln(out, "mugicrew run:", b.ID, "ended on a usage limit")
		return dispatch.RunFailed
	case lerr != nil:
		fmt.Fprintf(out, "mugicrew run: %s done; could not lift paused:%s: %v\n", b.ID, label, lerr)
		return dispatch.RunLiftFailed
	}
	return dispatch.RunClean
}
