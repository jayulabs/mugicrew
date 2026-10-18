package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
)

// startFollower launches `/proc/self/exe args` detached (ADR 0254 §1): its own
// session, stdin /dev/null, stdout and stderr on outPath (never the pane's pty,
// whose hangup kills a child left on it), released and never waited on. A var so
// tests spawn nothing.
var startFollower = func(outPath string, args []string) error {
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close() // the child holds its own copy
	cmd := exec.Command("/proc/self/exe", args...)
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// Handoff is the exit-time half of a handoff, shared by both HITL runners: the
// metered cost's attrs (plus continued_in), and a follow func to call just before
// hitl.exited that spawns the follower and warns on failure. The event log path is
// resolved now, before a teardown can remove the cwd's tree. spawn is SpawnFollower
// (a seam for tests); follow is a no-op when m names no destination.
func Handoff(attrs []any, m contract.Metered, spawn func([]any, contract.Metered, string) error) (cost []any, follow func()) {
	cost = m.Attrs()
	if m.ContinuedIn == "" {
		return cost, func() {}
	}
	events, _ := eventlog.Resolve()
	return append(cost, "continued_in", m.ContinuedIn), func() {
		if err := spawn(attrs, m, events); err != nil {
			slog.Warn("hitl.follow", "err", err)
		}
	}
}

// SpawnFollower starts the hitl-follow subcommand for a session that handed off
// (Spec mc-11b8 D4, FR21), just before its hitl.exited: attrs are the bead and
// session attrs the event carries; events the absolute event-log path. Nothing
// when m names no destination. Best-effort: a failed spawn only loses the
// remainder, never the session's end.
func SpawnFollower(attrs []any, m contract.Metered, events string) error {
	if m.ContinuedIn == "" {
		return nil
	}
	if events == "" { // eventlog.Resolve found no git repo: say so, never drop the remainder silently
		return errors.New("no event log path")
	}
	base, err := json.Marshal(m.Last)
	if err != nil {
		return err
	}
	args := []string{"hitl-follow"}
	for i := 0; i+1 < len(attrs); i += 2 {
		args = append(args, fmt.Sprintf("--%v=%v", attrs[i], attrs[i+1]))
	}
	args = append(args, "--continued_in="+m.ContinuedIn, "--baseline="+string(base),
		"--exited_at="+time.Now().UTC().Format(time.RFC3339Nano), "--events="+events)
	// ponytail: the .out files pile up, one per handoff; sweep them if they ever matter.
	return startFollower(filepath.Join(filepath.Dir(events), fmt.Sprintf("hitl-follow-%d.out", os.Getpid())), args)
}
