package tui

// The Stats tab's Distribution block (ADR 0266): a teal eighth-block histogram of
// the Sessions table's sessions by Agent time, one row per fixed bucket.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	tealSt = lipgloss.NewStyle().Foreground(lipgloss.Color("109"))

	timeEdges  = []float64{1, 2, 5, 10, 30, 60} // minutes
	timeLabels = []string{"<1m", "1–2m", "2–5m", "5–10m", "10–30m", "30m–1h", "1h+"}

	costEdges  = []float64{0.005, 0.25, 0.50, 1, 2.50} // USD
	costLabels = []string{"$0", "<0.25", "0.25–0.50", "0.50–1", "1–2.50", "$2.50+"}

	eighths = []rune(" ▏▎▍▌▋▊▉")
)

// bucket is the index of the bucket v falls in: bucket i is [edge i−1, edge i).
func bucket(v float64, edges []float64) int {
	i := 0
	for i < len(edges) && v >= edges[i] {
		i++
	}
	return i
}

// bar is a bar of frac of w cells in eighth-blocks; a non-empty bucket always
// shows a sliver.
func bar(frac float64, w int) string {
	n := int(frac*float64(w)*8 + 0.5)
	if frac > 0 && n == 0 {
		n = 1
	}
	s := strings.Repeat("█", n/8)
	if n%8 > 0 {
		s += string(eighths[n%8])
	}
	return s
}

// distribution is the block's lines in a w-wide column, from the cached sessions:
// Agent time, or Session cost over the known-cost sessions when cost is set.
func (s statsTab) distribution(w int) []string {
	edges, labels, name, other := timeEdges, timeLabels, "agent time", "session cost"
	if s.cost {
		edges, labels, name, other = costEdges, costLabels, "session cost", "agent time"
	}
	counts := make([]int, len(labels))
	n, top, unknown := 0, 0, 0
	for _, se := range s.ses {
		v := float64(se.WallMs) / 60000
		if s.cost {
			if !se.Known {
				unknown++
				continue
			}
			v = se.CostUSD
		}
		i := bucket(v, edges)
		counts[i]++
		n++
		top = max(top, counts[i])
	}
	labelW, countW := 0, len(fmt.Sprint(top))
	for _, l := range labels {
		labelW = max(labelW, lipgloss.Width(l))
	}
	barW := max(1, w-labelW-countW-2)
	out := []string{brand.Render("Distribution"), trunc(dimSt.Render(fmt.Sprintf("%s · %d sessions · d %s", name, n, other)), w)}
	for i, c := range counts {
		frac := 0.0
		if top > 0 {
			frac = float64(c) / float64(top)
		}
		cnt := fmt.Sprintf("%*d", countW, c)
		if c == 0 {
			cnt = dimSt.Render(cnt)
		}
		out = append(out, padTo(labels[i], labelW)+" "+padTo(tealSt.Render(bar(frac, barW)), barW)+" "+cnt)
	}
	if unknown > 0 {
		out = append(out, trunc(dimSt.Render(fmt.Sprintf("+%d sessions without a cost", unknown)), w))
	}
	return out
}
