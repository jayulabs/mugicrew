// Package transcript is the read side of `mugicrew transcript <bead>`: a bead's
// sessions found in the event log, and the digest renderer (ADR 0250) for them.
package transcript

import (
	"math"
	"sort"
	"strings"
	"time"

	"jayulabs.org/mugicrew/eventlog"
)

// Session is one claude session a bead ran, as the session index and event log recorded it.
type Session struct {
	ID, Mode, Kind, Crewmate string // Mode "AFK" (session.*) | "HITL" (hitl.*)
	Start                    time.Time
	Outcome                  string // "unended" | "ended" | "failed"
	Exit                     int
	Dur                      time.Duration
	Result, Stderr           string // session.failed only (1 KiB / 4 KiB tail)
}

// Sessions is bead's sessions from sessions.jsonl.1, sessions.jsonl, events.jsonl.1, events.jsonl, oldest
// first. Events without a session attr are skipped; a HITL launch and its resumes
// are one session.
func Sessions(logPath, bead string) ([]*Session, error) {
	var order []*Session
	by := map[string]*Session{}
	fold := func(ev eventlog.Event) bool {
		sid, _ := ev.Raw["session"].(string)
		mode, kind, _ := strings.Cut(ev.Msg, ".")
		if ev.Bead != bead || sid == "" || (mode != "session" && mode != "hitl") {
			return false
		}
		s := by[sid]
		if s == nil {
			s = &Session{ID: sid, Mode: map[string]string{"session": "AFK", "hitl": "HITL"}[mode], Start: ev.Time, Outcome: "unended"}
			s.Kind, _ = ev.Raw["kind"].(string)
			s.Crewmate, _ = ev.Raw["crewmate"].(string)
			by[sid] = s
			order = append(order, s)
		}
		exit, dur := 0, time.Duration(0)
		if n, ok := ev.Raw["exit"].(interface{ Int64() (int64, error) }); ok {
			v, _ := n.Int64()
			exit = int(v)
		}
		if n, ok := ev.Raw["dur_ms"].(interface{ Int64() (int64, error) }); ok {
			v, _ := n.Int64()
			dur = time.Duration(v) * time.Millisecond
		}
		switch kind {
		case "ended", "exited":
			s.Outcome, s.Exit, s.Dur = "ended", exit, dur
		case "failed":
			s.Outcome, s.Exit, s.Dur = "failed", exit, dur
			if r, ok := ev.Raw["result"].(map[string]any); ok {
				s.Result, _ = r["result"].(string)
			}
			s.Stderr, _ = ev.Raw["stderr"].(string)
		case "resumed":
			s.Outcome = "unended"
		}
		return false
	}
	// The index is older than the log: replay it first (ADR 0291 §4).
	needles := []string{`"bead":"` + bead + `"`}
	_, err := eventlog.ReadWhere(eventlog.IndexPath(logPath), math.MaxInt64, needles, fold)
	if err == nil {
		_, err = eventlog.ReadWhere(logPath, math.MaxInt64, needles, fold)
	}
	sort.SliceStable(order, func(i, j int) bool { return order[i].Start.Before(order[j].Start) })
	return order, err
}
