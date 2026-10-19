package tui

import (
	"slices"
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

// hdrAt is the (row, column) of header in v, or -1, -1.
func hdrAt(v, header string) (row, col int) {
	for i, l := range lines(v) {
		if c := strings.Index(l, header); c >= 0 {
			return i, c
		}
	}
	return -1, -1
}

func TestStatsSectionLayout(t *testing.T) {
	pinClock(t, time.Date(2026, 10, 9, 12, 0, 0, 0, time.Local))
	s := burnTab(append(distEvents(), usageReadEv(burnNow.Add(-time.Minute), 10, burnNow.Add(2*time.Hour)))...)
	for _, tc := range []struct{ w, perRow int }{{100, 1}, {109, 1}, {110, 2}, {141, 2}, {142, 3}, {238, 3}} {
		v := s.view(tc.w, 80)
		ls := lines(v)
		pr, pc := hdrAt(v, "Plan usage")
		sr, sc := hdrAt(v, "Sessions")
		dr, dc := hdrAt(v, "Distribution")
		if pr != 0 || pc != 0 {
			t.Errorf("w=%d Plan usage at %d,%d:\n%s", tc.w, pr, pc, v)
		}
		switch tc.perRow {
		case 1:
			if sr <= pr || dr <= sr || sc != 0 || dc != 0 || ls[sr-1] != "" || ls[dr-1] != "" {
				t.Errorf("w=%d not stacked:\n%s", tc.w, v)
			}
		case 2:
			if sr != 0 || sc < 36+2 || dr <= 0 || dc != 0 || ls[dr-1] != "" {
				t.Errorf("w=%d not Usage|Sessions over Distribution:\n%s", tc.w, v)
			}
		case 3:
			if sr != 0 || dr != 0 || dc < sc+72+2 {
				t.Errorf("w=%d not one row:\n%s", tc.w, v)
			}
		}
		for _, l := range ls {
			if lipgloss.Width(l) > tc.w {
				t.Errorf("w=%d line too wide: %q", tc.w, l)
			}
		}
		if tc.perRow > 1 && sc-2 > 92 {
			t.Errorf("w=%d Plan usage wider than 92", tc.w)
		}
		if tc.w == 100 { // stacked: Plan usage's band and chart stay within 92
			for _, l := range ls[:sr] {
				if lipgloss.Width(l) > 92 {
					t.Errorf("w=%d Plan usage line wider than 92: %q", tc.w, l)
				}
			}
		}
		if dc >= 0 && tc.perRow < 3 {
			for _, l := range ls[dr:] {
				if lipgloss.Width(l) > 46 {
					t.Errorf("w=%d wrapped Distribution wider than 46: %q", tc.w, l)
				}
			}
		}
	}
}

func TestStatsSessionsGrowsToNaturalWidth(t *testing.T) {
	pinClock(t, time.Date(2026, 10, 9, 12, 0, 0, 0, time.Local))
	s := burnTab(distEvents()...)
	s.by = slices.Index(statsGroups, "day") // ten-character keys
	s = s.recompute()
	nat := s.tableWidth()
	if nat <= 72 {
		t.Fatalf("natural width %d should exceed 72", nat)
	}
	v := s.view(238, 80)
	var hdr []string
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "SESSIONS") {
			hdr = strings.Fields(l[strings.Index(l, "KEY") : strings.Index(l, "TOKENS")+len("TOKENS")])
		}
	}
	if want := "KEY SESSIONS FAILED AGENT API COST AVG_COST AVG_API FAIL% TOKENS"; strings.Join(hdr, " ") != want || !strings.Contains(v, "2026-10-09") {
		t.Errorf("wide pane squeezed the table (header %q):\n%s", hdr, v)
	}
	// 110-141: Sessions grows before Plan usage passes 36.
	_, sc := hdrAt(s.view(115, 80), "Sessions")
	if sc != 36+2 {
		t.Errorf("w=115 Plan usage grew to %d before Sessions was full", sc-2)
	}
	s.by = 0
	if w := s.recompute().tableWidth(); w >= nat {
		t.Errorf("grouping none width %d should be under %d", w, nat)
	}
}

// modeEvents are an AFK build, an AFK failure and a HITL exit, all within 7d.
func modeEvents() []eventlog.Event {
	now := time.Now()
	e := func(ago time.Duration, msg, bead string, raw map[string]any) eventlog.Event {
		return eventlog.Event{Time: now.Add(-ago), Level: "INFO", Msg: msg, Bead: bead, Raw: raw}
	}
	return []eventlog.Event{
		e(time.Hour, "session.ended", "mc-a", map[string]any{"kind": "build", "crewmate": "franky", "dur_ms": 60000.0, "cost_usd": 1.5}),
		e(2*time.Hour, "session.failed", "mc-b", map[string]any{"kind": "critique", "crewmate": "chopper", "dur_ms": 60000.0, "cost_usd": 3.0}),
		e(3*time.Hour, "hitl.exited", "mc-c", map[string]any{"kind": "build", "crewmate": "nami", "dur_ms": 60000.0, "cost_usd": 2.0}),
	}
}

func TestStatsModeCycle(t *testing.T) {
	m := sized(t, 120, 30)
	m, _ = send(t, m, logsMsg{path: "/log", changed: true, events: modeEvents(), digest: modeEvents()})
	m, _ = send(t, m, runeKey("#"))
	plan := func(m Model) string {
		v := lines(m.View())
		for i, l := range v {
			if strings.Contains(l, "Plan usage") {
				var out []string
				for _, l := range v[i:min(i+3, len(v))] {
					out = append(out, strings.TrimRight(string([]rune(l + strings.Repeat(" ", 40))[:40]), " ")) // the left column only
				}
				return strings.Join(out, "\n")
			}
		}
		return ""
	}
	base := plan(m)
	if v := flat(m.View()); !strings.Contains(v, "m mode") || strings.Contains(v, "· AFK") || strings.Contains(v, "· HITL") {
		t.Errorf("default view:\n%s", v)
	}
	for _, c := range []struct{ label, count string }{{"· AFK", "2 "}, {"· HITL", "1 "}, {"", "3 "}} {
		m, _ = send(t, m, runeKey("m"))
		v := flat(m.View())
		if got := total(m); !strings.HasPrefix(got, c.count) {
			t.Errorf("%q total = %q", c.label, got)
		}
		if has := strings.Contains(v, "· AFK") || strings.Contains(v, "· HITL"); has != (c.label != "") || (c.label != "" && !strings.Contains(v, c.label)) {
			t.Errorf("%q head line:\n%s", c.label, v)
		}
		if want := c.count + "sessions"; !strings.Contains(v, " "+want) && !strings.Contains(v, "· "+want) {
			t.Errorf("%q Distribution lacks %q:\n%s", c.label, want, v)
		}
		if plan(m) != base {
			t.Errorf("%q changed Plan usage:\n%s", c.label, plan(m))
		}
	}
	// With f set, the mode label follows the kind label; m keeps across a reload.
	m, _ = send(t, m, runeKey("f")) // build
	m, _ = send(t, m, runeKey("m")) // AFK
	m, _ = send(t, m, logsMsg{path: "/log", changed: true, events: modeEvents(), digest: modeEvents()})
	if v := flat(m.View()); !strings.Contains(v, "kind build · AFK") || !strings.HasPrefix(total(m), "1 ") {
		t.Errorf("kind + mode after reload:\n%s", v)
	}
}
