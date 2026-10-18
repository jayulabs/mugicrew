package tui

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/eventlog"
)

// distEvents are seven all-time sessions, one per Agent time bucket, the first two
// of kind build; the stats package reads json.Number, as the event log decodes.
func distEvents() []eventlog.Event {
	var evs []eventlog.Event
	for i, min := range []float64{0.5, 1.5, 3, 7, 20, 45, 120} {
		kind := "grill"
		if i < 2 {
			kind = "build"
		}
		evs = append(evs, eventlog.Event{Time: burnNow.Add(-time.Duration(i+1) * time.Hour), Msg: "session.ended", Bead: "mc-a",
			Raw: map[string]any{"kind": kind, "dur_ms": json.Number(strconv.FormatFloat(min*60000, 'f', -1, 64))}})
	}
	return evs
}

// distCounts reads the bucket counts (the last field of each bucket row) in v.
func distCounts(v string) (sum int) {
	for _, l := range lines(v) {
		for _, lab := range timeLabels {
			if f := strings.Fields(l); len(f) >= 2 && f[0] == lab {
				var n int
				for _, c := range f[len(f)-1] {
					n = n*10 + int(c-'0')
				}
				sum += n
			}
		}
	}
	return sum
}

func TestDistributionPlacementAndCounts(t *testing.T) {
	s := burnTab(distEvents()...)
	narrow := flat(s.view(80, 40))
	if strings.Index(narrow, "Distribution") < strings.Index(narrow, "TOTAL") {
		t.Errorf("narrow Distribution not below Sessions:\n%s", narrow)
	}
	if got := distCounts(narrow); got != s.total.Sessions || got != 7 {
		t.Errorf("bucket counts sum to %d, SESSIONS %d", got, s.total.Sessions)
	}
	if !strings.Contains(narrow, "agent time · 7 sessions · d session cost") {
		t.Errorf("head line:\n%s", narrow)
	}
	// w changes the counts, b does not.
	s7 := s
	s7.win = 0 // last 24h: the events are older
	s7 = s7.recompute()
	if got := distCounts(s7.view(80, 40)); got != 0 {
		t.Errorf("24h counts = %d, want 0", got)
	}
	sb := s
	sb.by = 1
	if got := distCounts(sb.recompute().view(80, 60)); got != 7 {
		t.Errorf("group by changed the counts: %d", got)
	}
	sk := s
	sk.kind = "build"
	if got := distCounts(sk.recompute().view(80, 40)); got != 2 {
		t.Errorf("kind build counts = %d, want 2", got)
	}
}

func TestDistributionSliver(t *testing.T) {
	if got := bar(0.001, 10); got == "" {
		t.Error("a non-empty bucket shows no sliver")
	}
	if bar(0, 10) != "" || bar(1, 10) != strings.Repeat("█", 10) {
		t.Error("empty is blank, the largest fills")
	}
}

// The table keeps its rows; the placeholder comes before the burn chart gives out;
// a partly drawn histogram never appears.
func TestDistributionGivesWayFirst(t *testing.T) {
	s := burnTab(append(distEvents(), usageReadEv(burnNow.Add(-time.Minute), 10, burnNow.Add(2*time.Hour)))...)
	sawPlaceholder, tooShortFirst := false, false
	for h := 60; h >= 9; h-- {
		v := flat(s.view(80, h))
		hasDist, hasPH, hasShort := strings.Contains(v, "Distribution"), strings.Contains(v, "distribution hidden"), strings.Contains(v, "chart hidden: too short")
		if !strings.Contains(v, "TOTAL") {
			t.Errorf("h=%d lost the table", h)
		}
		if hasDist && distCounts(v) != 7 {
			t.Errorf("h=%d partly drawn histogram:\n%s", h, v)
		}
		if hasPH {
			sawPlaceholder = true
		}
		if hasShort && !sawPlaceholder {
			tooShortFirst = true
		}
	}
	if !sawPlaceholder || tooShortFirst {
		t.Errorf("placeholder seen %v, burn too short before it %v", sawPlaceholder, tooShortFirst)
	}
}

func TestDistributionGolden(t *testing.T) {
	evs := distEvents()
	for _, i := range []int{0, 0, 0, 0, 2, 2, 5} { // skew the counts: 5, 1, 3, 1, 1, 2, 1
		evs = append(evs, evs[i])
	}
	s := burnTab(evs...)
	pick := func(v string) string {
		ls := lines(v)
		for i, l := range ls {
			if strings.Contains(l, "Distribution") {
				col := strings.Index(l, "Distribution")
				for j := i; j < len(ls); j++ {
					ls[j] = ls[j][min(col, len(ls[j])):]
				}
				return strings.Join(ls[i:], "\n")
			}
		}
		return v
	}
	got := pick(flat(s.view(120, 30))) + "\n=====\n" + pick(flat(s.view(80, 40))) + "\n"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		os.WriteFile("testdata/dist.golden", []byte(got), 0o644)
	}
	if want, err := os.ReadFile("testdata/dist.golden"); err != nil || string(want) != got {
		t.Errorf("distribution render differs from golden:\n%s", got)
	}
}

func TestDistributionCostToggle(t *testing.T) {
	var evs []eventlog.Event
	for i, c := range []any{0.0, 0.1, 0.3, 3.0, nil} {
		raw := map[string]any{"kind": "build", "dur_ms": json.Number("60000")}
		if c != nil {
			raw["cost_usd"] = json.Number(strconv.FormatFloat(c.(float64), 'f', -1, 64))
		}
		evs = append(evs, eventlog.Event{Time: burnNow.Add(-time.Duration(i+1) * time.Hour), Msg: "session.ended", Bead: "mc-a", Raw: raw})
	}
	s := burnTab(evs...)
	if v := flat(s.view(80, 40)); !strings.Contains(v, "agent time · 5 sessions") || strings.Count(v, "without a cost") != 1 { // the table's own footer only
		t.Errorf("agent time view:\n%s", v)
	}
	c, _ := s.update(runeKey("d"))
	s = c.(statsTab)
	v := flat(s.view(80, 40))
	if !strings.Contains(v, "session cost · 4 sessions · d agent time") || strings.Count(v, "without a cost") != 2 {
		t.Errorf("cost view:\n%s", v)
	}
	if got := distCounts(v); got != 0 { // distCounts reads time labels only
		t.Errorf("time rows in cost view: %d", got)
	}
	for _, lab := range []string{"$0 ", "<0.25", "0.25–0.50", "$2.50+"} {
		if !strings.Contains(v, lab) {
			t.Errorf("missing bucket %q:\n%s", lab, v)
		}
	}
	s.win = 2
	if !s.recompute().cost {
		t.Error("w/recompute reset the d choice")
	}
}

// A block gives way only when it alone is taller than its row's height: with
// Sessions the tallest block, the chart and Distribution stay whole until they
// alone exceed the height (wide and 110-141 widths).
func TestGiveWayPerRow(t *testing.T) {
	pinClock(t, burnNow)
	s := burnTab(append(distEvents(), usageReadEv(burnNow.Add(-30*time.Minute), 10, burnNow.Add(2*time.Hour)))...)
	s.by = slices.Index(statsGroups, "day")
	s = s.recompute()
	for len(s.rows) < 9 {
		s.rows = append(s.rows, s.rows[0])
	}
	for _, w := range []int{238, 120} {
		n := len(lines(s.view(w, 100)))
		for h := n; h >= n-2 && (w == 238 || h == n); h-- {
			v := flat(s.view(w, h))
			if strings.Contains(v, "hidden") || !strings.Contains(v, "1h+") || !strings.Contains(v, "▲") || !strings.Contains(v, "without a cost") {
				t.Errorf("w=%d h=%d gave way though the chart and Distribution fit:\n%s", w, h, v)
			}
		}
	}
	if v := flat(s.view(238, 7)); !strings.Contains(v, "distribution hidden") {
		t.Errorf("view(238,7) dropped the Distribution placeholder:\n%s", v)
	}
	// Walking the height down, the chart is hidden only once its row has less than
	// the 7 lines its 2-row minimum needs (at 120 the Distribution row takes 3 more),
	// and the Distribution is whole or its placeholder, never partial.
	for _, c := range []struct{ w, floor int }{{238, 7}, {120, 10}} {
		whole := distCounts(flat(s.view(c.w, 100)))
		for h := len(lines(s.view(c.w, 100))); h >= c.floor; h-- {
			v := flat(s.view(c.w, h))
			if strings.Contains(v, "chart hidden") {
				t.Errorf("w=%d h=%d hid the chart though its row has room:\n%s", c.w, h, v)
			}
			if got := distCounts(v); got != 0 && got != whole {
				t.Errorf("w=%d h=%d drew a partial Distribution:\n%s", c.w, h, v)
			}
			if !strings.Contains(v, "without a cost") {
				t.Errorf("w=%d h=%d lost the no-cost footer:\n%s", c.w, h, v)
			}
		}
	}
}

// Cutting Sessions rows must leave no line of the pane unused where no chart is
// beside Sessions (stacked width): the view is exactly h lines once rows are cut.
func TestSessionsCutFillsPane(t *testing.T) {
	pinClock(t, burnNow)
	s := burnTab(append(distEvents(), usageReadEv(burnNow.Add(-30*time.Minute), 10, burnNow.Add(2*time.Hour)))...)
	s.by = slices.Index(statsGroups, "day")
	s = s.recompute()
	for len(s.rows) < 9 {
		s.rows = append(s.rows, s.rows[0])
	}
	dayRe := regexp.MustCompile(`(?m)^\d{4}-\d\d-\d\d`)
	nat := len(dayRe.FindAllString(flat(s.view(80, 100)), -1))
	if nat < 9 {
		t.Fatalf("fixture shows %d day rows, want >= 9", nat)
	}
	cut := 0
	for h := len(lines(s.view(80, 100))); h >= 12; h-- {
		v := s.view(80, h)
		if len(dayRe.FindAllString(flat(v), -1)) < nat {
			cut++
			if got := len(lines(v)); got != h {
				t.Errorf("h=%d cut Sessions rows but drew %d lines:\n%s", h, got, flat(v))
			}
		}
	}
	if cut == 0 {
		t.Error("never cut Sessions rows")
	}
}

// Without a no-cost footer Sessions rows still give way, so the tab never draws
// taller than the pane and the chart/Distribution follow their row's real height.
func TestGiveWayWithoutFooter(t *testing.T) {
	pinClock(t, burnNow)
	s := burnTab(append(distEvents(), usageReadEv(burnNow.Add(-30*time.Minute), 10, burnNow.Add(2*time.Hour)))...)
	s.by = slices.Index(statsGroups, "day")
	s = s.recompute()
	for len(s.rows) < 20 {
		s.rows = append(s.rows, s.rows[0])
	}
	s.total.CostPartial = 0
	for _, w := range []int{238, 120, 80} {
		whole := distCounts(flat(s.view(w, 100)))
		for h := 25; h >= 12; h-- {
			v := s.view(w, h)
			if got := len(lines(v)); got > h {
				t.Errorf("w=%d h=%d drew %d lines", w, h, got)
			}
			f := flat(v)
			if got := distCounts(f); got != 0 && got != whole {
				t.Errorf("w=%d h=%d partial Distribution:\n%s", w, h, f)
			}
		}
	}
}

// The placeholder never makes a row wider than the pane, even in the Distribution's
// 30-column minimum.
func TestDistributionPlaceholderFitsItsBlock(t *testing.T) {
	pinClock(t, burnNow)
	s := burnTab(append(distEvents(), usageReadEv(burnNow.Add(-30*time.Minute), 10, burnNow.Add(2*time.Hour)))...)
	s.by = slices.Index(statsGroups, "day")
	s = s.recompute()
	for _, w := range []int{142, 180} {
		for h := 10; h >= 7; h-- {
			for i, l := range lines(s.view(w, h)) {
				if n := ansi.StringWidth(l); n > w {
					t.Errorf("w=%d h=%d line %d is %d wide", w, h, i, n)
				}
			}
		}
	}
}
