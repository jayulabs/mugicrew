package tui

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"jayulabs.org/mugicrew/eventlog"
)

var burnNow = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

// usageReadEv is a usage.read event at t with a 5h pct and, unless zero, a reset.
func usageReadEv(t time.Time, pct float64, reset time.Time) eventlog.Event {
	raw := map[string]any{"five_hour_pct": json.Number(strconv.FormatFloat(pct, 'f', -1, 64))}
	if !reset.IsZero() {
		raw["five_hour_resets_at"] = reset.Format(time.RFC3339Nano)
	}
	return eventlog.Event{Time: t, Msg: "usage.read", Raw: raw}
}

func TestBurnSeriesWindow(t *testing.T) {
	reset := burnNow.Add(2 * time.Hour) // window 9:00 → 14:00
	m := func(d time.Duration) time.Time { return burnNow.Add(d) }
	for _, tc := range []struct {
		name   string
		evs    []eventlog.Event
		reason string
		reads  int
		reset  time.Time
	}{
		{"latest reset defines the window", []eventlog.Event{usageReadEv(m(-time.Hour), 10, reset.Add(3*time.Second)), usageReadEv(m(-time.Minute), 20, reset)}, "", 2, reset},
		{"zero latest reset falls back to the newest earlier future reset", []eventlog.Event{usageReadEv(m(-30*time.Minute), 10, reset), usageReadEv(m(-time.Minute), 20, time.Time{})}, "", 2, reset},
		{"no known reset", []eventlog.Event{usageReadEv(m(-time.Minute), 20, time.Time{})}, "(no 5h window running)", 0, time.Time{}},
		{"no reads", nil, "(no 5h window running)", 0, time.Time{}},
		{"reset passed", []eventlog.Event{usageReadEv(m(-time.Hour), 20, m(-time.Minute))}, "(5h window reset)", 0, time.Time{}},
		{"reads outside [start, reset) dropped", []eventlog.Event{usageReadEv(m(-4*time.Hour), 10, reset), usageReadEv(m(-time.Hour), 20, reset), usageReadEv(m(3*time.Hour), 30, reset)}, "", 1, reset},
		{"read with own reset <= start dropped", []eventlog.Event{usageReadEv(m(-time.Hour), 90, reset.Add(-5*time.Hour)), usageReadEv(m(-30*time.Minute), 5, reset)}, "", 1, reset},
		{"reads <60s after the last kept dropped", []eventlog.Event{usageReadEv(m(-10*time.Minute), 10, reset), usageReadEv(m(-10*time.Minute+30*time.Second), 11, reset), usageReadEv(m(-9*time.Minute), 12, reset)}, "", 2, reset},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newBurn(tc.evs, burnWins[0], burnNow)
			if b.reason != tc.reason || len(b.reads) != tc.reads || !b.reset.Equal(tc.reset) {
				t.Errorf("reason %q reads %d reset %v; want %q %d %v", b.reason, len(b.reads), b.reset, tc.reason, tc.reads, tc.reset)
			}
		})
	}
}

// burnTab is a Stats tab at burnNow with the given reads.
func burnTab(evs ...eventlog.Event) statsTab {
	prev := clock
	clock = func() time.Time { return burnNow }
	defer func() { clock = prev }()
	return statsTab{statsData: statsData{events: evs, band: "5h 62% (resets 14:00) · wk 40%"}, win: 3}.recompute()
}

func TestBurnChartRender(t *testing.T) {
	reset := burnNow.Add(2 * time.Hour)
	evs := []eventlog.Event{}
	for _, d := range []time.Duration{-170, -165, -160, -100, -95, -5} { // minutes; gap between -160 and -100
		evs = append(evs, usageReadEv(burnNow.Add(d*time.Minute), 0, reset))
	}
	evs[len(evs)-1] = usageReadEv(burnNow.Add(-5*time.Minute), 50, reset)
	v := flat(burnTab(evs...).view(120, 30))
	for _, want := range []string{"100┤", "  0┤", "▲", "resets ", "░", "┄", "▁", "0h", "5h", "3h0m in", "2h0m left"} {
		if !strings.Contains(v, want) {
			t.Errorf("chart lacks %q:\n%s", want, v)
		}
	}
}

func TestBurnChartColour(t *testing.T) {
	reset := burnNow.Add(2 * time.Hour)
	for _, tc := range []struct {
		pct  float64
		want lipgloss.Style
	}{{50, burnSty}, {80, usageWarnSty}, {95, usageErrSty}} {
		b := newBurn([]eventlog.Event{usageReadEv(burnNow.Add(-time.Minute), tc.pct, reset)}, burnWins[0], burnNow)
		if b.sty.GetForeground() != tc.want.GetForeground() {
			t.Errorf("pct %v: colour %v, want %v", tc.pct, b.sty.GetForeground(), tc.want.GetForeground())
		}
	}
}

func TestBurnChartTooShort(t *testing.T) {
	reset := burnNow.Add(2 * time.Hour)
	s := burnTab(usageReadEv(burnNow.Add(-time.Minute), 10, reset))
	for _, h := range []int{10, 11} { // stacked at 80: height − 5 table lines − 6 leaves < 2 rows
		v := flat(s.view(80, h))
		if !strings.Contains(v, "(chart hidden: too short)") || !strings.Contains(v, "TOTAL") {
			t.Errorf("h=%d:\n%s", h, v)
		}
	}
	if v := flat(s.view(80, 30)); strings.Contains(v, "too short") {
		t.Errorf("tall stacked hides the chart:\n%s", v)
	}
}

// Goldens for one wide and one narrow render at a fixed epoch (UPDATE_GOLDEN=1 re-records).
func TestBurnChartGolden(t *testing.T) {
	prev := time.Local
	time.Local = time.UTC // the reset note renders in Local
	defer func() { time.Local = prev }()
	reset := burnNow.Add(2 * time.Hour)
	var evs []eventlog.Event
	for i := 0; i < 30; i++ {
		evs = append(evs, usageReadEv(burnNow.Add(-time.Duration(i)*5*time.Minute-time.Minute), float64(60-i*2), reset))
	}
	evs = append(evs, statEvents()...)
	evs[len(evs)-1].Time, evs[len(evs)-2].Time = burnNow.Add(-time.Hour), burnNow.Add(-2*time.Hour)
	s := burnTab(evs...)
	got := flat(s.view(120, 20)) + "\n=====\n" + flat(s.view(80, 30)) + "\n"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		os.WriteFile("testdata/burn.golden", []byte(got), 0o644)
	}
	want, err := os.ReadFile("testdata/burn.golden")
	if err != nil || string(want) != got {
		t.Errorf("burn render differs from golden:\n%s", got)
	}
}

func TestBurnWeeklyWindow(t *testing.T) {
	reset := burnNow.Add(2 * 24 * time.Hour) // window start 5d ago
	wk := func(at time.Time, pct float64, reset time.Time) eventlog.Event {
		raw := map[string]any{"weekly_pct": json.Number(strconv.FormatFloat(pct, 'f', -1, 64))}
		if !reset.IsZero() {
			raw["weekly_resets_at"] = reset.Format(time.RFC3339Nano)
		}
		return eventlog.Event{Time: at, Msg: "usage.read", Raw: raw}
	}
	for _, tc := range []struct {
		name   string
		evs    []eventlog.Event
		reason string
		reads  int
	}{
		{"wk window", []eventlog.Event{wk(burnNow.Add(-4*24*time.Hour), 10, reset), wk(burnNow.Add(-time.Hour), 20, reset), usageReadEv(burnNow, 5, reset)}, "", 2},
		{"no wk window", []eventlog.Event{usageReadEv(burnNow, 5, reset)}, "(no wk window running)", 0},
		{"wk reset", []eventlog.Event{wk(burnNow.Add(-time.Hour), 20, burnNow.Add(-time.Minute))}, "(wk window reset)", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newBurn(tc.evs, burnWins[1], burnNow)
			if b.reason != tc.reason || len(b.reads) != tc.reads {
				t.Errorf("reason %q reads %d; want %q %d", b.reason, len(b.reads), tc.reason, tc.reads)
			}
		})
	}
}

func TestBurnLimitKey(t *testing.T) {
	s := burnTab(usageReadEv(burnNow.Add(-time.Minute), 20, burnNow.Add(2*time.Hour)))
	if s.limit != 0 || !strings.Contains(flat(s.view(120, 30)), "5h") {
		t.Fatal("new tab must open on 5h")
	}
	var hint bool
	for _, b := range s.bindings() {
		hint = hint || b.Help().Key == "u" && b.Help().Desc == "5h/wk"
	}
	if !hint {
		t.Error("bindings lack u 5h/wk")
	}
	c, _ := s.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	if v := flat(c.(statsTab).view(120, 30)); !strings.Contains(v, "(no wk window running)") {
		t.Errorf("u did not switch to wk:\n%s", v)
	}
	c, _ = c.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	if c.(statsTab).limit != 0 {
		t.Error("second u must return to 5h")
	}
}

func TestBurnChartResetAheadOfClock(t *testing.T) {
	b := newBurn([]eventlog.Event{usageReadEv(burnNow.Add(-time.Minute), 10, burnNow.Add(5*time.Hour+20*time.Minute))}, burnWins[0], burnNow)
	out := strings.Join(b.lines(40, 6), "\n")
	if !strings.Contains(out, "▲ 0m in") {
		t.Fatalf("want ▲ 0m in, got:\n%s", out)
	}
}

func TestBurnDur(t *testing.T) {
	for d, want := range map[time.Duration]string{0: "0m", 20 * time.Second: "0m", 3*time.Hour + 45*time.Minute: "3h45m", 52 * time.Hour: "2d4h"} {
		if got := burnDur(d); got != want {
			t.Errorf("burnDur(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestBurnNarrow(t *testing.T) {
	b := newBurn([]eventlog.Event{usageReadEv(burnNow, 5, burnNow.Add(time.Hour))}, burnWins[0], burnNow)
	if got := b.lines(8, 6)[0]; !strings.Contains(flat(got), "(chart …") || lipgloss.Width(got) > 8 {
		t.Errorf("narrow lines = %q", got)
	}
}

// u to wk draws a wk chart: 0d…7d ticks and a 2d4h-style duration.
func TestBurnWeeklyChart(t *testing.T) {
	pinClock(t, burnNow)
	reset := burnNow.Add(52 * time.Hour)
	raw := map[string]any{"weekly_pct": json.Number("30"), "weekly_resets_at": reset.Format(time.RFC3339Nano)}
	s := burnTab(eventlog.Event{Time: burnNow.Add(-time.Hour), Msg: "usage.read", Raw: raw})
	c, _ := s.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	v := flat(c.(statsTab).view(120, 30))
	for _, want := range []string{"0d", "7d", "4d20h in", "2d4h left"} {
		if !strings.Contains(v, want) {
			t.Errorf("wk chart lacks %q:\n%s", want, v)
		}
	}
}

// A column starting after now is future, whatever the hold rule says (FR12).
func TestBurnSampleNoDataAfterNow(t *testing.T) {
	reset := burnNow.Add(2 * time.Hour)
	b := newBurn([]eventlog.Event{usageReadEv(burnNow.Add(-time.Minute), 10, reset)}, burnWins[0], burnNow)
	cols := b.sample(300) // 1-minute columns
	step := b.reset.Sub(b.start) / 300
	for i, c := range cols {
		if t0 := b.start.Add(time.Duration(i) * step); t0.After(burnNow) && c.kind != bFuture {
			t.Fatalf("column %d starts %v after now but kind %d", i, t0.Sub(burnNow), c.kind)
		}
	}
}

// The stacked layout stays within its height and keeps the no-cost footer on screen.
func TestStackedStatsKeepsNoCostFooter(t *testing.T) {
	reset := burnNow.Add(2 * time.Hour)
	s := burnTab(usageReadEv(burnNow.Add(-time.Minute), 10, reset))
	s.total.CostPartial = 3
	if n := strings.Count(s.view(80, 8), "\n") + 1; n > 8 {
		t.Errorf("view is %d lines, want at most 8", n)
	}
	if v := flat(s.view(80, 8)); !strings.Contains(v, "+3 sessions without a cost") {
		t.Errorf("footer lost:\n%s", v)
	}
}
