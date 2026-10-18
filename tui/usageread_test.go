package tui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/claudecode"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
)

// One good read is one usage.read with flat attrs; an absent window or a zero reset
// omits its attrs; no top-level kind (the f picker would list it).
func TestGoodReadEmitsUsageRead(t *testing.T) {
	t0 := time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)
	reset := t0.Add(2 * time.Hour)
	for name, tc := range map[string]struct {
		u    contract.PlanUsage
		want map[string]any
	}{
		"both": {contract.PlanUsage{FiveHour: &contract.UsageWindow{Pct: 62, ResetsAt: reset}, Weekly: &contract.UsageWindow{Pct: 40, ResetsAt: reset}},
			map[string]any{"five_hour_pct": 62.0, "five_hour_resets_at": reset, "weekly_pct": 40.0, "weekly_resets_at": reset}},
		"absent week": {contract.PlanUsage{FiveHour: &contract.UsageWindow{Pct: 62, ResetsAt: reset}},
			map[string]any{"five_hour_pct": 62.0, "five_hour_resets_at": reset}},
		"zero reset": {contract.PlanUsage{FiveHour: &contract.UsageWindow{Pct: 0}},
			map[string]any{"five_hour_pct": 0.0}},
	} {
		t.Run(name, func(t *testing.T) {
			pinClock(t, t0)
			evs := eventlogtest.Capture(t)
			m := statsModel(t).WithUsage(stubUsage{func() (contract.PlanUsage, string, error) { return tc.u, "tok", nil }}.read)
			runRead(t, m, m.pollUsage())
			if len(*evs) != 1 || (*evs)[0].Name != "usage.read" {
				t.Fatalf("events = %v", eventlogtest.Names(*evs))
			}
			got := (*evs)[0].Attrs
			if len(got) != len(tc.want) {
				t.Fatalf("attrs = %v, want %v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("%s = %v, want %v", k, got[k], v)
				}
			}
		})
	}
}

func TestFailedReadEmitsNothing(t *testing.T) {
	for name, err := range map[string]error{
		"expired": claudecode.ErrTokenExpired, "retry-after": &claudecode.RetryAfterError{}, "other": errors.New("boom"),
	} {
		t.Run(name, func(t *testing.T) {
			pinClock(t, time.Now())
			evs := eventlogtest.Capture(t)
			m := statsModel(t).WithUsage(stubUsage{func() (contract.PlanUsage, string, error) { return contract.PlanUsage{}, "tok", err }}.read)
			runRead(t, m, m.pollUsage())
			if len(*evs) != 0 {
				t.Errorf("events = %v", eventlogtest.Names(*evs))
			}
		})
	}
}

// usage.read rides the full scan and the tail extend into the digest, and leaves
// the session totals and the f kinds alone.
func TestUsageReadInDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.log")
	line := `{"time":"2026-01-01T00:00:01Z","msg":"usage.read","proc":"tui","five_hour_pct":12.5}` + "\n"
	write := func(flag int) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|flag, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		f.WriteString(line)
	}
	m := statsModel(t).WithLog(eventlog.New(path, func(string) {}))
	read := func() {
		var cmd = m.loadLogs()
		next, c := send(t, m, cmd().(logsMsg))
		if c != nil {
			next, _ = send(t, next, c().(digestMsg))
		}
		m = next
	}
	write(os.O_TRUNC)
	read()
	if len(m.digest) != 1 {
		t.Fatalf("full scan digest = %d, want 1", len(m.digest))
	}
	write(os.O_APPEND)
	read()
	if len(m.digest) != 2 {
		t.Fatalf("extend digest = %d, want 2", len(m.digest))
	}
	if got := total(m); got[:2] != "0 " {
		t.Errorf("total = %q, want no sessions", got)
	}
}

// usage.read adds no f kind, and the hints bar on Stats shows u 5h/wk.
func TestUsageReadLeavesKindsAndShowsHint(t *testing.T) {
	m := statsModel(t)
	before := m.tabAt(tabStats).(statsTab).kinds
	d := append(statEvents(), eventlog.Event{Time: time.Now(), Msg: "usage.read", Raw: map[string]any{"five_hour_pct": json.Number("12")}})
	m, _ = send(t, m, logsMsg{path: "/log", changed: true, events: d, digest: d})
	if !slices.Equal(m.tabAt(tabStats).(statsTab).kinds, before) {
		t.Errorf("kinds %v, want %v", m.tabAt(tabStats).(statsTab).kinds, before)
	}
	if bar := flat(m.hintsView()); !strings.Contains(bar, "u 5h/wk") {
		t.Errorf("hints bar lacks u 5h/wk: %q", bar)
	}
}
