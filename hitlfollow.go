package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"jayulabs.org/mugicrew/claudecode"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
)

// followWait is the follower's real wait (Spec mc-11b8 D5): the supervisor retires
// an idle bg destination after 1 h, so 75 min after hitl.exited re-armed by its
// activity, and 9 h at most.
var followWait = claudecode.FollowWait{Idle: 75 * time.Minute, Ceiling: 9 * time.Hour, Poll: 10 * time.Second}

// runHitlFollow is the hidden hitl-follow verb: the detached follower hitl-run
// spawns when its session handed off (ADR 0254). Its argv is hitl-run's
// --key=value attrs (bead attrs, session, continued_in, ...) plus --exited_at
// (RFC 3339), --events (the absolute event log path) and --baseline (the source's
// last cost-state as JSON). It waits for the destination, then appends one
// hitl.continued with the remainder cost and tail_ms. It reads nothing from its
// cwd and holds no log handle across the wait (ADR 0028 §2).
func runHitlFollow(args []string, w claudecode.FollowWait, out io.Writer) error {
	var attrs []any
	var events, dest string
	var exitedAt time.Time
	var base contract.Cost
	for _, a := range args {
		k, v, ok := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		if !ok || !strings.HasPrefix(a, "--") {
			return fmt.Errorf("hitl-follow: bad argument %q", a)
		}
		var err error
		switch k {
		case "events":
			events = v
		case "exited_at":
			exitedAt, err = time.Parse(time.RFC3339Nano, v)
		case "baseline":
			err = json.Unmarshal([]byte(v), &base)
		default:
			attrs = append(attrs, k, v)
			if k == "continued_in" {
				dest = v
			}
		}
		if err != nil {
			return fmt.Errorf("hitl-follow: --%s: %w", k, err)
		}
	}
	if events == "" || dest == "" || exitedAt.IsZero() {
		return fmt.Errorf("hitl-follow: --events, --continued_in and --exited_at are required")
	}
	d := claudecode.FollowHandoff(dest, exitedAt, w)
	tail := int64(0)
	if !d.Last.IsZero() {
		tail = max(0, d.Last.Sub(exitedAt).Milliseconds())
	}
	attrs = append(attrs, "tail_ms", tail)
	if d.Cost.Known {
		attrs = append(attrs, "cost_known", true)
		attrs = append(attrs, claudecode.Remainder(d.Cost, base).Attrs()...)
	} else {
		attrs = append(attrs, "cost_known", false, "reason", d.Reason)
	}
	lg := eventlog.New(events, func(msg string) { fmt.Fprintln(out, msg) }).Logger("hitl-follow")
	lg.Info("hitl.continued", attrs...)
	fmt.Fprintf(out, "hitl-follow: wrote hitl.continued (cost_known=%v)\n", d.Cost.Known)
	return nil
}
