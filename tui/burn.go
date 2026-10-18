package tui

// The Stats tab's burn chart (ADR 0259, ADR 0260): % used across the current Plan
// usage window as an eighth-block area chart, derived from the digest's usage.read
// events. newBurn runs in recompute; render only formats the cached burn (ADR 0101).

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"jayulabs.org/mugicrew/eventlog"
)

// burnWin is a limit window the chart can show: its usage.read attr prefix, label and span.
type burnWin struct {
	key, label string
	span       time.Duration
}

var burnWins = []burnWin{{"five_hour", "5h", 5 * time.Hour}, {"weekly", "wk", 7 * 24 * time.Hour}}

const (
	burnDup = 60 * time.Second // a read this soon after the last kept one is a second poller's
	burnGap = 10 * time.Minute // no read for longer (2 × the 300s poll) is a gap: no TUI running
)

// burnSty is the chart's base colour: muted orange, never the live warn 214.
var burnSty = lipgloss.NewStyle().Foreground(lipgloss.Color("180"))

type burnRead struct {
	at  time.Time
	pct float64
}

// burn is the chart's cached series, or the dim reason there is none.
type burn struct {
	win          burnWin
	reads        []burnRead
	start, reset time.Time
	now          time.Time
	sty          lipgloss.Style
	reason       string
}

// burnNum reads an attr the log decoded as json.Number; ok is false when absent or unparsable.
func burnNum(raw map[string]any, k string) (f float64, ok bool) {
	v, isNum := raw[k].(json.Number)
	if !isNum {
		return 0, false
	}
	f, err := v.Float64()
	return f, err == nil
}

// newBurn derives the window's series from events at now: the latest read's reset
// defines the window (else the newest earlier future one), reads outside
// [start, reset) or from a previous window drop, near-duplicates drop.
func newBurn(evs []eventlog.Event, w burnWin, now time.Time) burn {
	type read struct {
		burnRead
		reset time.Time // zero = unknown
	}
	var rs []read
	for _, ev := range evs {
		if ev.Msg != "usage.read" {
			continue
		}
		pct, ok := burnNum(ev.Raw, w.key+"_pct")
		if !ok {
			continue
		}
		r := read{burnRead: burnRead{ev.Time, pct}}
		if s, _ := ev.Raw[w.key+"_resets_at"].(string); s != "" {
			r.reset, _ = time.Parse(time.RFC3339Nano, s)
		}
		rs = append(rs, r)
	}
	slices.SortStableFunc(rs, func(a, b read) int { return a.at.Compare(b.at) })
	b := burn{win: w, now: now, sty: burnSty}
	var reset time.Time
	for i := len(rs) - 1; i >= 0; i-- {
		if i == len(rs)-1 || rs[i].reset.After(now) {
			if reset = rs[i].reset; !reset.IsZero() {
				break
			}
		}
	}
	switch {
	case reset.IsZero():
		b.reason = "(no " + w.label + " window running)"
		return b
	case !reset.After(now):
		b.reason = "(" + w.label + " window reset)"
		return b
	}
	b.reset, b.start = reset, reset.Add(-w.span)
	for _, r := range rs {
		if r.at.Before(b.start) || !r.at.Before(b.reset) || !r.reset.IsZero() && !r.reset.After(b.start) {
			continue
		}
		if n := len(b.reads); n > 0 && r.at.Sub(b.reads[n-1].at) < burnDup {
			continue
		}
		b.reads = append(b.reads, r.burnRead)
	}
	if n := len(b.reads); n > 0 {
		switch p := b.reads[n-1].pct; {
		case p >= usageErrPct:
			b.sty = usageErrSty
		case p >= usageWarnPct:
			b.sty = usageWarnSty
		}
	}
	return b
}

type colKind int

const (
	bData colKind = iota
	bGap
	bFuture
)

// col is one sampled chart column.
type col struct {
	v    float64
	kind colKind
}

// sample buckets [start, reset) into n columns: the max pct read in a column's
// slice, else the last read before it if no more than burnGap older, else a gap;
// a column starting after now is future.
func (b burn) sample(n int) []col {
	var cols []col
	step := b.reset.Sub(b.start) / time.Duration(n)
	for i := range n {
		t0 := b.start.Add(time.Duration(i) * step)
		t1 := t0.Add(step)
		v, k := 0.0, bGap
		var last *burnRead
		for j := range b.reads {
			if b.reads[j].at.Before(t0) {
				last = &b.reads[j]
			} else if b.reads[j].at.Before(t1) {
				v, k = math.Max(v, b.reads[j].pct), bData
			}
		}
		if t0.After(b.now) {
			k = bFuture // the future is baseline, never data
		} else if k == bGap && last != nil && t0.Sub(last.at) <= burnGap {
			v, k = last.pct, bData
		}
		cols = append(cols, col{v, k})
	}
	return cols
}

// blocks draws the area chart, w cells × h rows.
func (b burn) blocks(w, h int) []string {
	rn := []rune(" ▁▂▃▄▅▆▇█")
	cols := b.sample(w)
	lines := make([]string, h)
	for r := range h {
		lvl := h - 1 - r // 0 = bottom row
		var sb strings.Builder
		for _, c := range cols {
			switch {
			case c.kind == bGap:
				sb.WriteString(dimSt.Render("░"))
			case c.kind == bFuture && lvl == 0:
				sb.WriteString(dimSt.Render("┄"))
			case c.kind == bFuture:
				sb.WriteByte(' ')
			default:
				n := int(math.Round(c.v / 100 * float64(8*h)))
				if lvl == 0 && n == 0 {
					n = 1 // data shows even at 0%
				}
				sb.WriteString(b.sty.Render(string(rn[min(8, max(0, n-8*lvl))])))
			}
		}
		lines[r] = sb.String()
	}
	return lines
}

// chart is the area chart in h rows with the y gutter and the 3 axis rows, w cells wide in all.
func (b burn) chart(w, h int) []string {
	w -= 4 // the gutter
	out := make([]string, 0, h+3)
	for r, l := range b.blocks(w, h) {
		g := "   │"
		switch r {
		case 0:
			g = "100┤"
		case h / 2:
			if h >= 5 {
				g = " 50┤"
			}
		}
		if r == h-1 {
			g = "  0┤"
		}
		out = append(out, dimSt.Render(g)+l)
	}
	span := b.reset.Sub(b.start)
	tick, unit := time.Hour, "h"
	if span > 24*time.Hour {
		tick, unit = 24*time.Hour, "d"
	}
	col := func(t time.Duration) int { return max(0, min(w-1, int(float64(w)*float64(t)/float64(span)))) }
	ax, lb := []rune(strings.Repeat("─", w)), []rune(strings.Repeat(" ", w))
	for i := 0; time.Duration(i)*tick <= span; i++ {
		x := col(time.Duration(i) * tick)
		ax[x] = '┬'
		if i == 0 {
			ax[x] = '┴'
		}
		l := []rune(fmt.Sprintf("%d%s", i, unit))
		copy(lb[max(0, min(x, w-len(l))):], l)
	}
	ax[w-1] = '┤'
	ax[col(b.now.Sub(b.start))] = '▲'
	note := fmt.Sprintf("▲ %s in · %s left · resets %s", burnDur(max(0, b.now.Sub(b.start))), burnDur(b.reset.Sub(b.now)), b.reset.Local().Format("Mon 15:04"))
	return append(out, dimSt.Render("   └"+string(ax)), dimSt.Render("    "+string(lb)), trunc(dimSt.Render("    "+note), w+4))
}

// burnDur is 3h45m / 2d4h: two units, no seconds.
func burnDur(d time.Duration) string {
	d = d.Round(time.Minute)
	if d >= 24*time.Hour {
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	}
	if d == 0 {
		return "0m"
	}
	return strings.TrimSuffix(d.String(), "0s")
}

// lines is the chart under the band in a w-wide column with rows chart rows, or the
// dim reason it is absent.
func (b burn) lines(w, rows int) []string {
	switch {
	case b.reason != "":
		return []string{trunc(dimSt.Render(b.reason), w)}
	case rows < 2:
		return []string{trunc(dimSt.Render("(chart hidden: too short)"), w)}
	case w < 12:
		return []string{trunc(dimSt.Render("(chart hidden: too narrow)"), w)}
	}
	return b.chart(w, min(6, rows))
}
