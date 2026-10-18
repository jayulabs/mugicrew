package tui

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

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
	wide := lines(s.view(120, 30))
	var hdr int
	for i, l := range wide {
		if strings.Contains(l, "Distribution") {
			hdr = i
			if strings.Index(l, "Distribution") != statsTableW+2 {
				t.Errorf("wide Distribution not in the right column:\n%s", flat(s.view(120, 30)))
			}
		}
	}
	if len(wide[hdr-1]) > statsTableW && strings.TrimSpace(wide[hdr-1][statsTableW:]) != "" {
		t.Errorf("no blank line before the block:\n%s", flat(s.view(120, 30)))
	}
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
