package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"jayulabs.org/mugicrew/claudecode"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
)

// statEvents are four session ends 1h, 2d, 10d and 40d old, the oldest with no kind.
func statEvents() []eventlog.Event {
	now := time.Now()
	e := func(ago time.Duration, msg, bead string, raw map[string]any) eventlog.Event {
		return eventlog.Event{Time: now.Add(-ago), Level: "INFO", Msg: msg, Bead: bead, Raw: raw}
	}
	return []eventlog.Event{
		e(time.Hour, "session.ended", "mc-a", map[string]any{"kind": "build", "crewmate": "franky", "dur_ms": 60000.0, "api_ms": 30000.0, "cost_usd": 1.5}),
		e(48*time.Hour, "session.failed", "mc-b", map[string]any{"kind": "critique", "crewmate": "chopper", "dur_ms": 60000.0, "cost_usd": 3.0}),
		e(240*time.Hour, "session.ended", "mc-c", map[string]any{"kind": "build", "crewmate": "franky", "dur_ms": 1000.0, "cost_usd": 2.0}),
		e(960*time.Hour, "session.ended", "mc-d", map[string]any{"crewmate": "nami", "dur_ms": 1000.0, "cost_usd": 7.0}),
	}
}

func statsModel(t *testing.T) Model {
	t.Helper()
	m := sized(t, 120, 30)
	m, _ = send(t, m, logsMsg{path: "/log", changed: true, events: statEvents(), digest: statEvents()})
	m, _ = send(t, m, runeKey("#"))
	return m
}

func total(m Model) string {
	for _, l := range lines(m.View()) {
		if f := strings.Fields(l); len(f) > 2 && f[0] == "TOTAL" {
			return f[1] + " $" + strings.Split(l, "$")[1][:4]
		}
	}
	return ""
}

func TestStatsWindowGroupKind(t *testing.T) {
	m := statsModel(t)
	if got := total(m); !strings.HasPrefix(got, "2 ") { // 7d: the 1h and 2d sessions
		t.Errorf("7d total = %q", got)
	}
	m, _ = send(t, m, runeKey("w")) // 30d
	if got := total(m); !strings.HasPrefix(got, "3 ") {
		t.Errorf("30d total = %q", got)
	}
	m, _ = send(t, m, runeKey("w")) // all time
	if got := total(m); !strings.HasPrefix(got, "4 ") {
		t.Errorf("all time total = %q", got)
	}
	m, _ = send(t, m, runeKey("b")) // crewmate
	v := flat(m.View())
	for _, want := range []string{"by crewmate", "franky", "nami", "chopper"} {
		if !strings.Contains(v, want) {
			t.Errorf("group by crewmate lacks %q:\n%s", want, v)
		}
	}
	m, _ = send(t, m, runeKey("f")) // build
	if v := flat(m.View()); !strings.Contains(v, "kind build") || strings.Contains(v, "nami") {
		t.Errorf("kind filter:\n%s", v)
	}
}

func flat(s string) string { return strings.Join(lines(s), "\n") }

func TestStatsFollowsTheLog(t *testing.T) {
	m := statsModel(t)
	m, _ = send(t, m, logsMsg{path: "/log", changed: true, events: statEvents()[:1], digest: statEvents()[:1]})
	if got := total(m); !strings.HasPrefix(got, "1 ") {
		t.Errorf("total after a smaller log = %q", got)
	}
}

type stubUsage struct {
	res func() (contract.PlanUsage, string, error)
}

func (s stubUsage) read(string) (contract.PlanUsage, string, error) { return s.res() }

// runRead folds every usageMsg a command (or batch) produces into m; it reports whether one ran.
func runRead(t *testing.T, m Model, cmd tea.Cmd) (Model, bool) {
	t.Helper()
	read := false
	var do func(tea.Cmd)
	do = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, c := range msg {
				do(c)
			}
		case usageMsg:
			read = true
			m, _ = send(t, m, msg)
		}
	}
	do(cmd)
	return m, read
}

func pinClock(t *testing.T, at time.Time) *time.Time {
	t.Helper()
	cur := at
	prev, ivl := clock, usageInterval
	clock, usageInterval = func() time.Time { return cur }, time.Millisecond
	t.Cleanup(func() { clock, usageInterval = prev, ivl })
	return &cur
}

func TestStatsUsageBandAndHeaderWarning(t *testing.T) {
	t0 := time.Date(2026, 10, 9, 12, 0, 0, 0, time.Local)
	pinClock(t, t0)
	u := stubUsage{func() (contract.PlanUsage, string, error) {
		return contract.PlanUsage{FiveHour: &contract.UsageWindow{Pct: 96, ResetsAt: t0.Add(2 * time.Hour)}, Weekly: &contract.UsageWindow{Pct: 40}}, "tok", nil
	}}
	m := statsModel(t).WithUsage(u.read)
	if v := flat(m.View()); !strings.Contains(v, "reading plan usage…") {
		t.Errorf("before a read:\n%s", v)
	}
	m, _ = runRead(t, m, m.Init())
	v := lines(m.View())
	if !strings.Contains(v[5], "5h 96% (resets 14:00) · wk 40%") { // band: under the Plan usage header
		t.Errorf("band = %q", v[5])
	}
	if !strings.Contains(v[0], "5h 96%") || strings.Contains(v[0], "wk") {
		t.Errorf("header = %q, want the 5h warning only", v[0])
	}
	m, _ = send(t, m, runeKey("4"))
	if h := lines(m.View())[0]; !strings.Contains(h, "5h 96%") {
		t.Errorf("warning missing on Graph: %q", h)
	}
}

func TestStatsUsageNilReader(t *testing.T) {
	if v := flat(statsModel(t).View()); !strings.Contains(v, "usage unavailable") {
		t.Errorf("nil reader:\n%s", v)
	}
}

// Entering Stats or f5 on it reads usage only past the 60s floor and outside a backoff.
func TestStatsUsageFloorAndBackoff(t *testing.T) {
	t0 := time.Date(2026, 10, 9, 12, 0, 0, 0, time.Local)
	for _, tc := range []struct {
		name  string
		after time.Duration
		limit bool
		want  bool
	}{
		{"fresh", 60 * time.Second, false, false},
		{"over 60s", 61 * time.Second, false, true},
		{"in backoff", 120 * time.Second, true, false},
		{"backoff passed", 300 * time.Second, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := pinClock(t, t0)
			u := stubUsage{func() (contract.PlanUsage, string, error) {
				if tc.limit {
					return contract.PlanUsage{}, "tok", &claudecode.RetryAfterError{}
				}
				return contract.PlanUsage{FiveHour: &contract.UsageWindow{Pct: 10}}, "tok", nil
			}}
			m := sized(t, 120, 30).WithUsage(u.read)
			m, _ = runRead(t, m, m.pollUsage())
			*now = t0.Add(tc.after)
			entry, cmd := send(t, m, runeKey("#"))
			if _, read := runRead(t, entry, cmd); read != tc.want {
				t.Errorf("entry read = %v, want %v", read, tc.want)
			}
			on := m
			on.tab = tabStats
			_, cmd = send(t, on, tea.KeyMsg{Type: tea.KeyF5})
			if _, read := runRead(t, on, cmd); read != tc.want {
				t.Errorf("f5 read = %v, want %v", read, tc.want)
			}
		})
	}
}

// ruleIdx is the index of the first line made only of ─, or -1.
func ruleIdx(ls []string) int {
	for i, l := range ls {
		// The rule may share its row with the right column.
		if t, _, _ := strings.Cut(strings.TrimSpace(l), " "); t != "" && strings.Trim(t, "─") == "" {
			return i
		}
	}
	return -1
}

func TestStatsTotalRule(t *testing.T) {
	m := statsModel(t)
	if i := ruleIdx(lines(m.View())); i >= 0 {
		t.Errorf("grouping none has a rule at line %d", i)
	}
	m, _ = send(t, m, runeKey("b")) // crewmate
	ls := lines(m.View())
	i := ruleIdx(ls)
	if i < 1 || strings.Fields(ls[i-1])[0] != "TOTAL" || strings.Fields(ls[i+1])[0] == "TOTAL" {
		t.Errorf("rule not directly after TOTAL (rule at %d):\n%s", i, flat(m.View()))
	}
	for _, w := range []int{120, 60, 30, 12} {
		s := statsTab{statsData: statsData{events: statEvents()}, win: 3, by: 1}.recompute()
		ls := lines(s.view(w, 30))
		i := ruleIdx(ls)
		if i < 0 || lipgloss.Width(ls[i]) > w {
			t.Errorf("w=%d: rule at %d too wide or missing:\n%s", w, i, flat(s.view(w, 30)))
		}
	}
}

func TestStatsFooterSurvivesRule(t *testing.T) {
	evs := statEvents()
	delete(evs[0].Raw, "cost_usd")
	s := statsTab{statsData: statsData{events: evs, w: 120}, win: 3, by: 1}.recompute()
	for h := 3; h <= 8; h++ {
		ls := lines(s.view(120, h))
		if last := ls[len(ls)-1]; !strings.Contains(last, "without a cost") {
			t.Errorf("height %d: last line %q", h, last)
		}
	}
}

func TestStatsSectionLayout(t *testing.T) {
	t0 := time.Date(2026, 10, 9, 12, 0, 0, 0, time.Local)
	pinClock(t, t0)
	band := "5h 62% (resets 14:00) · wk 40%"
	s := statsTab{statsData: statsData{events: statEvents(), band: band}, win: 3}.recompute()

	wide := lines(s.view(120, 30))
	if i, j := strings.Index(wide[0], "Sessions"), strings.Index(wide[0], "Plan usage"); i != 0 || j != statsTableW+2 {
		t.Errorf("wide headers at %d, %d:\n%s", i, j, flat(s.view(120, 30)))
	}
	if lipgloss.Width(wide[1][:strings.Index(wide[1], "5h 62%")]) != statsTableW+2 {
		t.Errorf("wide band not right of the head line:\n%s", flat(s.view(120, 30)))
	}

	narrow := lines(s.view(80, 30))
	if !strings.Contains(narrow[0], "Plan usage") || !strings.Contains(narrow[1], "5h 62%") || !strings.Contains(narrow[2], "no 5h window") || narrow[3] != "" || !strings.Contains(narrow[4], "Sessions") {
		t.Errorf("narrow does not stack:\n%s", flat(s.view(80, 30)))
	}
	if h := brand.Render("Sessions"); !strings.Contains(s.view(80, 30), h) {
		t.Errorf("header not bold accent")
	}
}
