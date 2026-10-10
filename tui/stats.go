package tui

// The Stats tab (Spec §5): what `mugicrew stats` reports, summed by the
// shared stats package over the already-loaded event log. Update recomputes and
// caches the table; View only formats it (ADR 0101, ADR 0098).

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"jayulabs.org/mugicrew/stats"
)

// statsWindows is w's cycle; d 0 is the whole log.
var statsWindows = []struct {
	label string
	d     time.Duration
}{{"last 24h", 24 * time.Hour}, {"last 7d", 7 * 24 * time.Hour}, {"last 30d", 30 * 24 * time.Hour}, {"all time", 0}}

// statsWinDefault is the window the tab opens on (FR20): 7d.
const statsWinDefault = 1

// statsGroups is b's cycle, as stats.Filter.By values ("" is none: TOTAL only).
var statsGroups = []string{"", "crewmate", "model", "day"}

// statsKeys are the keys Stats handles: the global ones plus w and b.
var statsKeys = []string{"q", "ctrl+c", "left", "shift+tab", "right", "G", "I", "S", "H", "L", "#", "*", "?", "o", "f5", "w", "b"}

// recomputeStats re-sums m.logs over the window and grouping into the cache;
// day rows newest first, the rest cost descending as Sum returns them.
func (m *Model) recomputeStats() {
	var since time.Time
	if d := statsWindows[m.statsWin].d; d > 0 {
		since = time.Now().Add(-d)
	}
	by := statsGroups[m.statsBy]
	m.statsTotal, m.statsRows = stats.Sum(m.logs, stats.Filter{Since: since, By: by})
	if by == "" {
		m.statsRows = nil
	}
	if by == "day" {
		slices.SortFunc(m.statsRows, func(a, b stats.Row) int { return strings.Compare(b.Key, a.Key) })
	}
}

// statsView is the Stats body: the Plan usage band, the header, the table, and
// the no-cost footer.
func (m Model) statsView(height int) string {
	by := statsGroups[m.statsBy]
	byLabel := by
	if by == "" {
		byLabel = "none"
	}
	head := dim.Render(statsWindows[m.statsWin].label + " · by " + byLabel)

	rows := [][]string{{"KEY", "SESSIONS", "FAILED", "AGENT", "API", "COST", "TOKENS"}}
	for i, r := range append([]stats.Row{m.statsTotal}, m.statsRows...) {
		c := r.Cells(by == "model" && i > 0)
		rows = append(rows, []string{r.Key, c[0], c[1], c[2], c[3], r.USD(), stats.Tokens(r.In + r.Out)})
	}
	// KEY takes what the numeric columns leave (FR29): a tight fit drops the gap
	// to one space, a tighter one shortens the wide headers.
	var widths []int
	gap := 2
	keyRoom := func() int {
		widths = make([]int, len(rows[0]))
		for _, r := range rows {
			for j, c := range r {
				widths[j] = max(widths[j], lipgloss.Width(c))
			}
		}
		room := m.w
		for _, w := range widths[1:] {
			room -= w + gap
		}
		return room
	}
	if keyRoom() < 8 {
		gap = 1
	}
	if keyRoom() < 8 {
		rows[0] = []string{"KEY", "SESS", "FAIL", "AGENT", "API", "COST", "TOK"}
	}
	widths[0] = max(1, min(widths[0], keyRoom()))

	lines := []string{trunc(m.usageBand(), m.w), trunc(head, m.w)}
	for i, r := range rows {
		s := padTo(trunc(r[0], widths[0]), widths[0])
		for j, c := range r[1:] {
			s += strings.Repeat(" ", gap+widths[j+1]-lipgloss.Width(c)) + c
		}
		if i == 0 {
			s = dim.Render(s)
		}
		lines = append(lines, s)
	}
	if n := m.statsTotal.CostPartial; n > 0 {
		// The footer stays on screen: rows past the height give way to it.
		lines = append(lines[:min(len(lines), max(1, height-1))], dim.Render(fmt.Sprintf("+%d sessions without a cost", n)))
	}
	return strings.Join(lines, "\n")
}

// padTo pads s with spaces to w terminal cells.
func padTo(s string, w int) string { return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s))) }
