package tui

// The Stats tab (FR53, ADR 0061): the summed table and Plan usage band —
// what `mugicrew stats` reports, summed by the stats package over the
// already-loaded event log. The shell recomputes the cache on each logs load;
// render only formats it (ADR 0101).

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/stats"
)

// Stats' keys: w cycles the window, b the grouping, f the kind, u the burn chart's limit, d the Distribution block's measure.
var (
	keyWindow = key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "window"))
	keyGroup  = key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "group by"))
	keyLimit  = key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "5h/wk"))
	keyDist   = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "dist time/cost"))
)

// statsWindows is w's cycle; d 0 is the whole log.
var statsWindows = []struct {
	label string
	d     time.Duration
}{{"last 24h", 24 * time.Hour}, {"last 7d", 7 * 24 * time.Hour}, {"last 30d", 30 * 24 * time.Hour}, {"all time", 0}}

// statsWinDefault is the window the tab opens on: 7d.
const statsWinDefault = 1

// statsGroups is b's cycle, as stats.Filter.By values ("" is none: TOTAL only).
var statsGroups = []string{"", "crewmate", "model", "profile", "kind", "day"}

// statsData is the slice of the shell's data Stats reads, handed over before every
// update and view.
type statsData struct {
	events []eventlog.Event
	band   string // the Plan usage band, formatted by the shell
	w      int
}

type statsTab struct {
	statsData
	win, by int
	cost    bool      // d: Distribution shows Session cost, not Agent time
	limit   int       // u's index into burnWins; 0 = 5h
	kind    string    // f's kind filter; "" = every kind
	kinds   []string  // f's kinds: those in the events, sorted, cached by recompute
	total   stats.Row // the table, cached by recompute
	rows    []stats.Row
	ses     []stats.Session // the Distribution block's sessions, cached by recompute
	burn    burn            // the Plan usage burn chart, cached by recompute
}

func newStats() statsTab { return statsTab{win: statsWinDefault} }

func (s statsTab) count() int { return 0 }
func (s statsTab) actN() int  { return 0 }

func (s statsTab) bindings() []key.Binding {
	bs := []key.Binding{keyWindow, keyGroup, keyLimit, keyDist}
	if len(s.kinds) > 0 {
		bs = append(bs, keyKind)
	}
	return bs
}

func (s statsTab) update(k tea.KeyMsg) (component, tea.Cmd) {
	switch {
	case key.Matches(k, keyWindow):
		s.win = (s.win + 1) % len(statsWindows)
	case key.Matches(k, keyGroup):
		s.by = (s.by + 1) % len(statsGroups)
	case key.Matches(k, keyLimit):
		s.limit = (s.limit + 1) % len(burnWins)
	case key.Matches(k, keyDist):
		s.cost = !s.cost
		return s, nil
	case key.Matches(k, keyKind) && len(s.kinds) > 0:
		cycle := append([]string{""}, s.kinds...) // a pinned kind gone from the log steps back to every kind
		s.kind = cycle[(slices.Index(cycle, s.kind)+1)%len(cycle)]
	default:
		return s, nil
	}
	return s.recompute(), nil
}

// recompute re-sums the events over the window, kind filter and grouping into the
// cache, and re-collects f's kinds; day rows newest first, the rest cost
// descending as Sum returns them.
func (s statsTab) recompute() statsTab {
	s.kinds = nil // fresh: tab copies share the old backing array
	for _, ev := range s.events {
		if k, _ := ev.Raw["kind"].(string); k != "" && !slices.Contains(s.kinds, k) {
			s.kinds = append(s.kinds, k)
		}
	}
	slices.Sort(s.kinds)
	var since time.Time
	if d := statsWindows[s.win].d; d > 0 {
		since = clock().Add(-d)
	}
	by := statsGroups[s.by]
	s.burn = newBurn(s.events, burnWins[s.limit], clock())
	s.total, s.rows = stats.Sum(s.events, stats.Filter{Since: since, Kind: s.kind, By: by})
	s.ses = stats.Sessions(s.events, stats.Filter{Since: since, Kind: s.kind})
	if by == "" {
		s.rows = nil
	}
	if by == "day" {
		slices.SortFunc(s.rows, func(a, b stats.Row) int { return strings.Compare(b.Key, a.Key) })
	}
	return s
}

// statsBlock is one block of the Stats flow: its width range and a draw at (w, h),
// where h caps how many lines it may take (statsNatural for no cap).
type statsBlock struct {
	min, max int
	draw     func(w, h int) []string
}

// A block's cap h is a line count, or one of these: statsNatural for no cap, and for
// the Distribution 0 (its placeholder only) and statsDrop (not drawn at all).
const (
	statsNatural = 1 << 20
	statsDrop    = -1
)

// The blocks' indexes in blocks(), in flow order.
const (
	blkUsage = iota
	blkSessions
	blkDist
	nBlocks
)

// growOrder is who gets a row's spare width first (ADR 0298).
var growOrder = [...]int{blkSessions, blkUsage, blkDist}

func (s statsTab) blocks() []statsBlock {
	return []statsBlock{
		blkUsage: {36, 92, func(w, h int) []string { // Plan usage: header, band, burn chart
			// h-5: the header, the band and the chart's 3-row axis
			return append([]string{brand.Render("Plan usage"), trunc(s.band, w)}, s.burn.lines(w, h-5)...)
		}},
		blkSessions: {72, max(72, s.tableWidth()), func(w, h int) []string { return s.sessions(w, h-1) }}, // h-1: the header
		blkDist: {30, 46, func(w, h int) []string { // the Distribution gives way whole
			if h < 0 {
				return nil
			}
			if d := s.distribution(w); len(d) <= h {
				return d
			}
			return []string{trunc(dimSt.Render("(distribution hidden: too short)"), w)}
		}},
	}
}

// view is the Stats body: Plan usage, Sessions and the Distribution flowing left to
// right, wrapping when the next block's minimum no longer fits (ADR 0297, ADR 0298).
// Too short, they give way tab-wide: the Distribution, then the burn chart, then
// the Sessions rows. The floor (about 7 lines stacked) is the headers plus the
// footer: below it the tab overflows the pane on purpose.
func (s statsTab) view(w, height int) string {
	f := newStatsFlow(s.blocks(), w)
	best := f.render(f.natCaps())
	if len(best) > height {
		// Walk the give-way order: Distribution, then the chart, then Sessions rows.
		var cands [][nBlocks]int
		cands = append(cands, [nBlocks]int{statsNatural, statsNatural, 0})
		for ph := len(f.nat[blkUsage]) - 1; ph >= 3; ph-- {
			// [ph, nat, nat] puts a whole Distribution back: right for a shared row (FR15).
			// Stacked it is safe because [ph+1, nat, 0] is tried first and is shorter
			// whenever the whole histogram is more than 2 lines taller than its placeholder.
			cands = append(cands, [nBlocks]int{ph, statsNatural, statsNatural}, [nBlocks]int{ph, statsNatural, 0})
		}
		cands = append(cands, [nBlocks]int{3, statsNatural, statsDrop}) // no room for the placeholder: it goes too
		// Each Sessions line cut is one line off the tab, so the cut height is the
		// first candidate's overshoot away from the natural one; the loop only
		// walks on when a taller block beside it hides the cut.
		over := len(f.render(cands[len(cands)-1])) - height
		shFrom := len(cands)
		for sh := max(2, len(f.nat[blkSessions])-over); sh >= 2; sh-- {
			cands = append(cands, [nBlocks]int{3, sh, statsDrop})
		}
		for i, c := range cands {
			prev := len(best)
			best = f.render(c)
			// A Sessions cut that shrinks nothing is hidden by a taller block: stop.
			if len(best) <= height || i > shFrom && len(best) >= prev {
				break
			}
		}
	}
	return strings.Join(best, "\n")
}

// statsFlow is the blocks packed into rows at a pane width.
type statsFlow struct {
	bs   []statsBlock
	rows [][2]int   // each row's blocks: bs[from:to]
	ws   []int      // each block's width
	nat  [][]string // each block drawn at its width with no cap, once per view
}

func newStatsFlow(bs []statsBlock, w int) statsFlow {
	f := statsFlow{bs: bs, ws: make([]int, len(bs)), nat: make([][]string, len(bs))}
	for i := 0; i < len(bs); {
		j, used := i, -2
		// Only the first row may hold several blocks: at 104-109 columns the
		// trimmed algorithm would pair Sessions|Distribution, but FR4 wants one
		// block per row under 110.
		for j < len(bs) && (j == i || i == 0 && used+2+bs[j].min <= w) {
			used += 2 + bs[j].min
			f.ws[j] = min(bs[j].min, w)
			j++
		}
		for _, k := range growOrder {
			if k >= i && k < j {
				g := min(max(0, w-used), bs[k].max-f.ws[k])
				f.ws[k] += g
				used += g
			}
		}
		f.rows = append(f.rows, [2]int{i, j})
		i = j
	}
	for k, b := range bs {
		f.nat[k] = b.draw(f.ws[k], statsNatural)
	}
	return f
}

func (f statsFlow) natCaps() (c [nBlocks]int) {
	for k := range c {
		c[k] = statsNatural
	}
	return c
}

// render lays the rows out with each block capped at caps[k], one blank line between rows.
func (f statsFlow) render(caps [nBlocks]int) []string {
	var out []string
	for _, r := range f.rows {
		rl := f.row(r, caps)
		if len(rl) == 0 {
			continue
		}
		if out != nil {
			out = append(out, "")
		}
		out = append(out, rl...)
	}
	return out
}

// row draws one row's blocks side by side, giving way per block (FR15).
func (f statsFlow) row(r [2]int, caps [nBlocks]int) []string {
	var cols [][]string
	for k := r[0]; k < r[1]; k++ {
		cols = append(cols, f.bs[k].draw(f.ws[k], caps[k]))
	}
	// A block gives way only when it alone is taller than the height its row
	// has left: one whose natural height fits beside the others stays whole.
	drawn := make([]int, len(cols)) // heights before any block is redrawn below
	for n, c := range cols {
		drawn[n] = len(c)
	}
	for n := range cols {
		k, other := r[0]+n, 0
		for m, d := range drawn {
			if m != n {
				other = max(other, d)
			}
		}
		if caps[k] == statsNatural || len(cols) < 2 {
			continue
		}
		if len(f.nat[k]) <= other {
			cols[n] = f.nat[k]
		} else if caps[k] == statsDrop && other > 0 { // the row has a line left for the placeholder
			cols[n] = f.bs[k].draw(f.ws[k], 0)
		} else if caps[k] >= 0 && other > caps[k] { // redraw at the height the row has
			cols[n] = f.bs[k].draw(f.ws[k], other)
		}
	}
	h := 0
	for _, c := range cols {
		h = max(h, len(c))
	}
	out := make([]string, h)
	for y := range out {
		var l string
		for n, c := range cols {
			cell := ""
			if y < len(c) {
				cell = c[y]
			}
			if n > 0 {
				l += "  "
			}
			l += padTo(trunc(cell, f.ws[r[0]+n]), f.ws[r[0]+n]) // a draw func can't run past its block
		}
		out[y] = strings.TrimRight(l, " ")
	}
	return out
}

// sessions is the Sessions section under its header: the head line, the table and
// the no-cost footer, within height lines.
func (s statsTab) sessions(w, height int) []string {
	by := statsGroups[s.by]
	byLabel := by
	if by == "" {
		byLabel = "none"
	}
	head := statsWindows[s.win].label + " · by " + byLabel
	if s.kind != "" {
		head += " · kind " + s.kind
	}
	head = dimSt.Render(head)

	rows := s.tableRows()
	// KEY takes what the numeric columns leave: a tight fit drops the gap to one
	// space, a tighter one shortens the wide headers, a tighter still hides the
	// three average columns (the six originals always stay).
	var widths []int
	gap := 2
	keyRoom := func() int {
		widths = columnWidths(rows)
		room := w
		for _, cw := range widths[1:] {
			room -= cw + gap
		}
		return room
	}
	if keyRoom() < 8 {
		gap = 1
	}
	if keyRoom() < 8 {
		rows[0] = []string{"KEY", "SESS", "FAIL", "AGENT", "API", "COST", "AVG$", "AAPI", "F%", "TOK"}
	}
	if keyRoom() < 8 {
		for i, r := range rows {
			rows[i] = slices.Delete(r, 6, 9)
		}
		keyRoom()
	}
	widths[0] = max(1, min(widths[0], keyRoom()))

	lines := []string{trunc(head, w)}
	for i, r := range rows {
		l := padTo(trunc(r[0], widths[0]), widths[0])
		for j, c := range r[1:] {
			l += strings.Repeat(" ", gap+widths[j+1]-lipgloss.Width(c)) + c
		}
		switch i {
		case 0:
			l = dimSt.Render(l)
		case 1:
			l = boldSt.Render(l)
		}
		lines = append(lines, l)
		if i == 1 && len(s.rows) > 0 { // the rule spans the laid-out row, capped at the pane
			lines = append(lines, dimSt.Render(strings.Repeat("─", min(w, widths[0]+sumPlus(widths[1:], gap)))))
		}
	}
	// Rows past the height give way; the no-cost footer, when there is one, stays.
	n := s.total.CostPartial
	if n > 0 {
		height--
	}
	lines = lines[:min(len(lines), max(1, height))]
	if n > 0 {
		lines = append(lines, dimSt.Render(fmt.Sprintf("+%d sessions without a cost", n)))
	}
	return append([]string{brand.Render("Sessions")}, lines...)
}

// tableRows is the table's cells, header first, then TOTAL and the group rows.
func (s statsTab) tableRows() [][]string {
	by := statsGroups[s.by]
	rows := [][]string{{"KEY", "SESSIONS", "FAILED", "AGENT", "API", "COST", "AVG_COST", "AVG_API", "FAIL%", "TOKENS"}}
	for i, r := range append([]stats.Row{s.total}, s.rows...) {
		c := r.Cells(by == "model" && i > 0)
		rows = append(rows, []string{r.Key, c[0], c[1], c[2], c[3], r.USD(), c[4], c[5], c[6], stats.Tokens(r.In + r.Out)})
	}
	return rows
}

// tableWidth is the table's natural width: KEY untruncated and every column at
// two-space gaps. Sessions' block grows to it before Plan usage does.
func (s statsTab) tableWidth() int {
	widths := columnWidths(s.tableRows())
	// KEY is at least 8, so the squeezes stay off
	return max(widths[0], 8) + sumPlus(widths[1:], 2)
}

// columnWidths is each column's widest cell.
func columnWidths(rows [][]string) []int {
	widths := make([]int, len(rows[0]))
	for _, r := range rows {
		for j, c := range r {
			widths[j] = max(widths[j], lipgloss.Width(c))
		}
	}
	return widths
}

var boldSt = lipgloss.NewStyle().Bold(true)

func sumPlus(ws []int, gap int) (n int) {
	for _, w := range ws {
		n += w + gap
	}
	return n
}

// padTo pads s with spaces to w terminal cells.
func padTo(s string, w int) string { return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s))) }
