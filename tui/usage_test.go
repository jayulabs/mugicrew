package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"jayulabs.org/mugicrew/claudecode"
	"jayulabs.org/mugicrew/contract"
)

// stubUsage is a Plan usage reader that returns res and records each skipToken.
type stubUsage struct {
	res   func() (contract.PlanUsage, string, error)
	skips []string
}

func (s *stubUsage) read(skip string) (contract.PlanUsage, string, error) {
	s.skips = append(s.skips, skip)
	return s.res()
}

// fakeClock pins the poller's clock and shrinks its tick so a poll's tick cmd returns at once.
func fakeClock(t *testing.T, at time.Time) *time.Time {
	t.Helper()
	cur := at
	prevClock, prevIvl := clock, usageInterval
	clock, usageInterval = func() time.Time { return cur }, time.Millisecond
	t.Cleanup(func() { clock, usageInterval = prevClock, prevIvl })
	return &cur
}

// poll fires one background usage tick and folds whatever it reads back into m.
func poll(t *testing.T, m Model) Model {
	t.Helper()
	next, cmd := m.Update(usageTickMsg{})
	m = next.(Model)
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, c := range msg {
				run(c)
			}
		case usageMsg:
			next, _ := m.Update(msg)
			m = next.(Model)
		}
	}
	run(cmd)
	return m
}

func usageOf(five, week float64) func() (contract.PlanUsage, string, error) {
	return func() (contract.PlanUsage, string, error) {
		return contract.PlanUsage{FiveHour: &contract.UsageWindow{Pct: five}, Weekly: &contract.UsageWindow{Pct: week}}, "tok", nil
	}
}

// FR12/13, FR35: the tab bar names only the windows at ≥80%, warn-coloured, or ≥95%,
// error-coloured, on every tab; it is quiet below 80%, when unavailable, and with no reader.
func TestTabBarPlanUsageWarning(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	defer lipgloss.SetColorProfile(prev)
	fakeClock(t, time.Date(2026, 10, 9, 12, 0, 0, 0, time.Local))

	bar := func(m Model) string {
		m.w = 200
		return m.tabBar()
	}
	for _, tc := range []struct {
		name      string
		res       func() (contract.PlanUsage, string, error)
		want, not []string
	}{
		{"warn at 80", usageOf(84, 50), []string{usageWarnSty.Render("5h 84%")}, []string{"wk"}},
		{"error at 95", usageOf(10, 96), []string{usageErrSty.Render("wk 96%")}, []string{"5h"}},
		{"both", usageOf(80, 95), []string{usageWarnSty.Render("5h 80%"), usageErrSty.Render("wk 95%")}, nil},
		{"below 80", usageOf(79.6, 12), nil, []string{"5h", "wk"}},
		{"past its reset", func() (contract.PlanUsage, string, error) {
			return contract.PlanUsage{FiveHour: &contract.UsageWindow{Pct: 99, ResetsAt: clock().Add(-time.Minute)}}, "tok", nil
		}, nil, []string{"5h"}},
		{"unavailable", func() (contract.PlanUsage, string, error) {
			return contract.PlanUsage{}, "", claudecode.ErrTokenExpired
		}, nil, []string{"5h", "wk"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &stubUsage{res: tc.res}
			m := poll(t, New(nil, nil).WithUsage(s.read))
			for _, tb := range []tab{tabGraph, tabLogs} {
				m.tab = tb
				got := bar(m)
				for _, w := range tc.want {
					if !strings.Contains(got, w) {
						t.Errorf("tab %d bar lacks %q:\n%q", tb, w, got)
					}
				}
				for _, n := range tc.not {
					if strings.Contains(got, n) {
						t.Errorf("tab %d bar shows %q:\n%q", tb, n, got)
					}
				}
			}
		})
	}

	m := New(nil, nil) // nil reader: unavailable, and no polling at all
	if _, cmd := m.Update(usageTickMsg{}); cmd != nil {
		t.Error("a nil reader must not poll")
	}
	if got := bar(m); strings.Contains(got, "5h") || strings.Contains(got, "wk") {
		t.Errorf("nil reader shows usage: %q", got)
	}
}

// FR16: a RetryAfter backs the poller off for its After, 300s when the endpoint
// gave none, capped at 3600s; polls inside the backoff send nothing.
func TestUsagePollRetryAfterBackoff(t *testing.T) {
	t0 := time.Date(2026, 10, 9, 12, 0, 0, 0, time.Local)
	for _, tc := range []struct {
		name  string
		after time.Duration
		wait  time.Duration
		retry string
	}{
		{"retry-after", 120 * time.Second, 120 * time.Second, "12:02"},
		{"default 300s", 0, 300 * time.Second, "12:05"},
		{"cap 3600s", 2 * time.Hour, 3600 * time.Second, "13:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := fakeClock(t, t0)
			s := &stubUsage{res: func() (contract.PlanUsage, string, error) {
				return contract.PlanUsage{}, "tok", &claudecode.RetryAfterError{After: tc.after}
			}}
			m := poll(t, New(nil, nil).WithUsage(s.read))
			if got := m.usageErr.Error(); got != "rate-limited, retry "+tc.retry {
				t.Errorf("reason = %q, want rate-limited, retry %s", got, tc.retry)
			}
			*now = t0.Add(tc.wait - time.Second)
			if m = poll(t, m); len(s.skips) != 1 {
				t.Fatalf("polled inside the backoff: %d reads", len(s.skips))
			}
			*now = t0.Add(tc.wait)
			if m = poll(t, m); len(s.skips) != 2 {
				t.Errorf("no read once the backoff passed: %d reads", len(s.skips))
			}
		})
	}
}

// FR17: the token that got a 401 is passed back as skipToken on the next poll.
func TestUsagePollPassesBack401Token(t *testing.T) {
	fakeClock(t, time.Date(2026, 10, 9, 12, 0, 0, 0, time.Local))
	s := &stubUsage{res: func() (contract.PlanUsage, string, error) {
		return contract.PlanUsage{}, "stale-tok", claudecode.ErrTokenExpired
	}}
	m := poll(t, New(nil, nil).WithUsage(s.read))
	poll(t, m)
	if len(s.skips) != 2 || s.skips[0] != "" || s.skips[1] != "stale-tok" {
		t.Errorf("skipTokens = %q, want [\"\" \"stale-tok\"]", s.skips)
	}
}

// runRead fires the cmd a key or tab switch returned, if any, folding a usage read
// into m; it reports whether a read ran.
func runRead(t *testing.T, m Model, cmd tea.Cmd) (Model, bool) {
	t.Helper()
	if cmd == nil {
		return m, false
	}
	read := false
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, c := range msg {
				run(c)
			}
		case usageMsg:
			read = true
			next, _ := m.Update(msg)
			m = next.(Model)
		}
	}
	run(cmd)
	return m, read
}

// FR5–11, FR35/36: the band above the Stats table in each Plan usage state, the
// table always below it, at full and at narrow width.
func TestStatsPlanUsageBand(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	defer lipgloss.SetColorProfile(prev)
	t0 := time.Date(2026, 10, 9, 12, 0, 0, 0, time.Local)
	good := func() (contract.PlanUsage, string, error) {
		return contract.PlanUsage{
			FiveHour: &contract.UsageWindow{Pct: 34, ResetsAt: t0.Add(2 * time.Hour)},
			Weekly:   &contract.UsageWindow{Pct: 61.4, ResetsAt: time.Date(2026, 10, 12, 9, 0, 0, 0, time.Local)},
		}, "tok", nil
	}
	fail := func() (contract.PlanUsage, string, error) {
		return contract.PlanUsage{}, "tok", claudecode.ErrTokenExpired
	}
	fresh := "5h 34% (resets 14:00) · wk 61% (resets Oct 12 09:00)"
	for _, tc := range []struct {
		name  string
		reads []func() (contract.PlanUsage, string, error)
		later time.Duration // the clock's advance after the reads
		nilR  bool
		want  string
		dim   bool
	}{
		{name: "fresh", reads: []func() (contract.PlanUsage, string, error){good}, later: 600 * time.Second, want: fresh},
		{name: "stale by age", reads: []func() (contract.PlanUsage, string, error){good}, later: 601 * time.Second, want: fresh + " · as of 12:00", dim: true},
		{name: "stale by a failed read", reads: []func() (contract.PlanUsage, string, error){good, fail}, want: fresh + " · as of 12:00", dim: true},
		{name: "reset", reads: []func() (contract.PlanUsage, string, error){good}, later: 3 * time.Hour, want: "5h reset · wk 61% (resets Oct 12 09:00) · as of 12:00", dim: true},
		{name: "reset while fresh", reads: []func() (contract.PlanUsage, string, error){func() (contract.PlanUsage, string, error) {
			return contract.PlanUsage{FiveHour: &contract.UsageWindow{Pct: 99, ResetsAt: t0.Add(time.Minute)}}, "tok", nil
		}}, later: 2 * time.Minute, want: "5h reset"},
		{name: "unavailable", reads: []func() (contract.PlanUsage, string, error){fail}, want: "usage unavailable: token expired"},
		{name: "nil reader", nilR: true, want: "usage unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := fakeClock(t, t0)
			m := statsModel(t)
			s := &stubUsage{}
			if !tc.nilR {
				m = m.WithUsage(s.read)
			}
			for _, r := range tc.reads {
				s.res = r
				m = poll(t, m)
			}
			*now = t0.Add(tc.later)
			m, _ = send(t, m, runeKey("#"))
			for w, want := range map[int]string{100: tc.want, floorCols: tc.want[:min(len(tc.want), 17)]} { // narrow: the band's head, cut with …
				m.w = w
				ls := statsLines(m)
				if len(ls) < 3 || !strings.HasPrefix(ls[0], want) || !hasLine(ls, "TOTAL") {
					t.Errorf("w=%d: want band %q over the table:\n%s", w, tc.want, strings.Join(ls, "\n"))
				}
			}
			m.w = 100
			if tc.dim && !strings.Contains(m.View(), dim.Render(tc.want)) {
				t.Errorf("stale band not dimmed:\n%q", m.View())
			}
		})
	}
}

// FR15: entering Stats or f5 on it reads Plan usage only when the last read is
// over 60s old and no RetryAfter backoff is in force.
func TestStatsPlanUsageFloor(t *testing.T) {
	t0 := time.Date(2026, 10, 9, 12, 0, 0, 0, time.Local)
	for _, tc := range []struct {
		name  string
		after time.Duration // the clock's advance past the first read
		limit bool          // the first read was rate-limited (300s backoff)
		want  bool
	}{
		{"fresh", 60 * time.Second, false, false},
		{"over 60s", 61 * time.Second, false, true},
		{"in backoff", 120 * time.Second, true, false},
		{"backoff passed", 300 * time.Second, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := fakeClock(t, t0)
			s := &stubUsage{res: usageOf(10, 10)}
			if tc.limit {
				s.res = func() (contract.PlanUsage, string, error) {
					return contract.PlanUsage{}, "tok", &claudecode.RetryAfterError{}
				}
			}
			m := poll(t, statsModel(t).WithUsage(s.read))
			*now = t0.Add(tc.after)
			next, cmd := m.switchTab(tabStats)
			if _, read := runRead(t, next.(Model), cmd); read != tc.want {
				t.Errorf("entry read = %v, want %v", read, tc.want)
			}
			m.tab = tabStats
			next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyF5})
			if _, read := runRead(t, next.(Model), cmd); read != tc.want {
				t.Errorf("f5 read = %v, want %v", read, tc.want)
			}
		})
	}
}
