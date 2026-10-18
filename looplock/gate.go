package looplock

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"strings"
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
	Drv string // the failing .drv, "" if stderr names none
	Log string // the last 80 lines of stderr
}

func (e *BuilderError) Error() string {
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
	infraTail := tail
	if len(infraTail) > maxTail {
		infraTail = infraTail[len(infraTail)-maxTail:]
		// start at a line boundary, never mid-rune or mid-line
		if _, rest, ok := strings.Cut(infraTail, "\n"); ok {
			infraTail = rest
		}
	}
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
