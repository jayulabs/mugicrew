package looplock

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// gateTimeout is the gate build's hard limit, applied as a ctx deadline (a var
// so a test can shorten it).
var gateTimeout = 15 * time.Minute

// maxTail caps InfraError.Tail in bytes: it is also the gate.infra event's err.
// BuilderError.Log is not capped: it keeps its 80 whole lines.
const maxTail = 4096

// BuilderError is a gate build whose derivation's own builder failed (a failing
// test, a vendorHash drift): the code's fault, so the land is refused (Spec D2).
type BuilderError struct {
	Drv  string // the failing .drv, "" if stderr names none or a command gate failed
	Exit int    // a command gate's non-zero exit code, 0 for a nix gate
	Log  string // the last 80 lines of stderr (of combined output for a command)
}

func (e *BuilderError) Error() string {
	if e.Drv == "" && e.Exit != 0 {
		return fmt.Sprintf("gate command failed: exit %d\n%s", e.Exit, e.Log)
	}
	return "gate build failed: builder failed (" + e.Drv + ")\n" + e.Log
}

// InfraError is any other gate build failure: no nix, socket, fetch, eval, or the
// deadline. Not the code's fault, so it never refuses a land.
type InfraError struct {
	Err     error
	Timeout bool   // the gate's deadline expired
	Tail    string // the last lines of stderr, for logging
}

func (e *InfraError) Error() string { return "gate build infra failure: " + e.Err.Error() }
func (e *InfraError) Unwrap() error { return e.Err }

// GateBuild builds the source repo's packages.<attr> at rev, a commit no branch
// reaches, through the scratch ref refs/mugicrew/gate/<landingID> (always
// deleted after; a stale one is overwritten), and returns its store out path. It
// never writes the loop's out-link (--no-link). A failure is a *BuilderError or an
// *InfraError (ADR 0152).
func GateBuild(ctx context.Context, commonDir, landingID, rev, attr string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gateTimeout)
	defer cancel()
	ref := "refs/mugicrew/gate/" + landingID
	git := func(ctx context.Context, args ...string) error {
		out, err := exec.CommandContext(ctx, "git", append([]string{"--git-dir", commonDir}, args...)...).CombinedOutput()
		if err != nil {
			return errors.New(strings.TrimSpace(string(out)) + ": " + err.Error())
		}
		return nil
	}
	if err := git(ctx, "update-ref", ref, rev); err != nil {
		return "", &InfraError{Err: err, Timeout: ctx.Err() == context.DeadlineExceeded}
	}
	defer git(context.Background(), "update-ref", "-d", ref)
	out, err := runNix(ctx, "build", "git+file://"+commonDir+"?ref="+ref+"&rev="+rev+"#"+attr,
		"--no-link", "--print-out-paths", "--log-lines", "80")
	if err != nil {
		return "", classifyGate(ctx, err)
	}
	store, _, _ := strings.Cut(out, "\n")
	return store, nil
}

var drvRe = regexp.MustCompile(`(?:Cannot build|hash mismatch in fixed-output derivation) '([^']+\.drv)'`)

// classifyGate sorts a failed gate build by its stderr (runNix's error text),
// searched whole: git fetch progress precedes nix's own lines.
func classifyGate(ctx context.Context, err error) error {
	text := err.Error()
	lines := strings.Split(text, "\n")
	tail := strings.Join(lines[max(0, len(lines)-80):], "\n")
	infraTail := capTail(tail)
	if ctx.Err() == context.DeadlineExceeded {
		return &InfraError{Err: err, Timeout: true, Tail: infraTail}
	}
	if strings.Contains(text, "Reason: builder failed with exit code") ||
		strings.Contains(text, "hash mismatch in fixed-output derivation") {
		var drv string
		if m := drvRe.FindStringSubmatch(text); m != nil {
			drv = m[1]
		}
		return &BuilderError{Drv: drv, Log: tail}
	}
	return &InfraError{Err: err, Tail: infraTail}
}

// capTail cuts s to its last maxTail bytes, starting at a line boundary, never
// mid-rune or mid-line.
func capTail(s string) string {
	if len(s) > maxTail {
		s = s[len(s)-maxTail:]
		if _, rest, ok := strings.Cut(s, "\n"); ok {
			s = rest
		}
	}
	return s
}

// lastLines is s's last n lines.
func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}

// cmdWaitDelay bounds how long a finished shell's output pipes may stay open.
const cmdWaitDelay = 5 * time.Second

// tailBuf keeps the last cap bytes written. Once it has wrapped it drops the
// partial first line, so the tail always starts at a line.
type tailBuf struct {
	mu      sync.Mutex
	buf     []byte
	cap     int
	wrapped bool
}

func (t *tailBuf) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.cap {
		t.wrapped = true
	}
	if len(t.buf) > 2*t.cap {
		// trim only at twice the cap, so the copy is amortised over cap bytes
		t.buf = append(t.buf[:0], t.buf[len(t.buf)-t.cap:]...)
	}
	return len(p), nil
}

func (t *tailBuf) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	b := t.buf
	if len(b) > t.cap {
		b = b[len(b)-t.cap:]
	}
	s := string(b)
	if t.wrapped {
		if _, rest, ok := strings.Cut(s, "\n"); ok {
			s = rest
		}
	}
	return s
}

// RunGateCommand runs sh -c cmd in dir under the gate timeout and sorts the
// outcome: nil on exit 0, a *BuilderError (Exit set) on a code failure, an
// *InfraError on anything that is not the code's fault.
func RunGateCommand(ctx context.Context, dir, cmd string) error {
	ctx, cancel := context.WithTimeout(ctx, gateTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	c.Dir = dir
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	c.WaitDelay = cmdWaitDelay
	out := &tailBuf{cap: 64 << 10}
	c.Stdout, c.Stderr = out, out
	err := c.Run()
	if c.Process != nil {
		// sweep stragglers in the shell's group (ESRCH when there are none)
		syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
	if err == nil || errors.Is(err, exec.ErrWaitDelay) && c.ProcessState != nil && c.ProcessState.ExitCode() == 0 {
		return nil
	}
	text := strings.TrimRight(out.String(), "\n")
	// with output, the tail ends in err's own status line ("exit status 127",
	// "signal: killed") so the gate.infra event says how the command ended
	last := lastLines(text, 80)
	tail := last
	if tail != "" && err != nil {
		tail += "\n" + err.Error()
	}
	tail = capTail(tail)
	infra := func(e error, timeout bool) error { return &InfraError{Err: e, Timeout: timeout, Tail: tail} }
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		return infra(err, true)
	case ctx.Err() != nil, c.Process == nil:
		return infra(err, false)
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return infra(err, false)
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return infra(fmt.Errorf("%w: %s", err, ws.Signal()), false)
	}
	switch code := ee.ExitCode(); code {
	case 126, 127:
		return infra(fmt.Errorf("%w: status %d", err, code), false)
	default:
		return &BuilderError{Exit: code, Log: last}
	}
}
