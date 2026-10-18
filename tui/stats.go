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

// statsSplit is the pane width from which Sessions (statsTableW) and Plan usage sit
// side by side: the table, a 36-column chart and the 2-column gap.
const (
	statsTableW = 72
	statsSplit  = 110
)

// view is the Stats body: Plan usage (the band) and Sessions (the head line, the
// table and the no-cost footer), side by side from statsSplit columns, else stacked.
func (s statsTab) view(w, height int) string {
	if w >= statsSplit {
		left := s.sessions(statsTableW, height-1)
		rw := w - statsTableW - 2
		burn, tail := s.burnAndDist(rw, rw, height-2)
		right := append(append([]string{brand.Render("Plan usage"), trunc(s.band, rw)}, burn...), tail...)
		out := make([]string, max(len(left), len(right)))
		for i := range out {
			var l, r string
			if i < len(left) {
				l = left[i]
			}
			if i < len(right) {
				r = right[i]
			}
			out[i] = strings.TrimRight(padTo(l, statsTableW)+"  "+r, " ")
		}
		return strings.Join(out, "\n")
	}
	sess := s.sessions(w, height-5)
	lines := []string{brand.Render("Plan usage"), trunc(s.band, w)}
	burn, tail := s.burnAndDist(w, min(w, statsTableW), height-len(sess)-3)
	lines = append(append(append(lines, burn...), ""), sess...)
	return strings.Join(append(lines, tail...), "\n")
}

// burnAndDist splits free lines between the burn chart (bw wide) and the
// Distribution block (dw wide, after a blank line). The distribution gives way
// first, whole: a placeholder takes its place, and only then does the burn chart
// shrink by its own rules (ADR 0266, ADR 0259). The block never draws partly.
func (s statsTab) burnAndDist(bw, dw, free int) (burn, tail []string) {
	dist := s.distribution(dw)
	burnWant := len(s.burn.lines(bw, 6))
	burnLines := min(burnWant, max(0, free-1))
	switch {
	case free-burnLines >= len(dist)+1:
		tail = append([]string{""}, dist...)
	case free-2 >= min(burnWant, 5): // room for the placeholder and a chart that fits
		tail = []string{"", dimSt.Render("(distribution hidden: too short)")}
		burnLines = min(burnWant, free-2)
	default:
		burnLines = free
	}
	return s.burn.lines(bw, burnLines-3), tail
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

	rows := [][]string{{"KEY", "SESSIONS", "FAILED", "AGENT", "API", "COST", "AVG_COST", "AVG_API", "FAIL%", "TOKENS"}}
	for i, r := range append([]stats.Row{s.total}, s.rows...) {
		c := r.Cells(by == "model" && i > 0)
		rows = append(rows, []string{r.Key, c[0], c[1], c[2], c[3], r.USD(), c[4], c[5], c[6], stats.Tokens(r.In + r.Out)})
	}
	// KEY takes what the numeric columns leave: a tight fit drops the gap to one
	// space, a tighter one shortens the wide headers, a tighter still hides the
	// three average columns (the six originals always stay).
	var widths []int
	gap := 2
	keyRoom := func() int {
		widths = make([]int, len(rows[0]))
		for _, r := range rows {
			for j, c := range r {
				widths[j] = max(widths[j], lipgloss.Width(c))
			}
		}
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
	if n := s.total.CostPartial; n > 0 {
		// The footer stays on screen: rows past the height give way to it.
		lines = append(lines[:min(len(lines), max(1, height-1))], dimSt.Render(fmt.Sprintf("+%d sessions without a cost", n)))
	}
	return append([]string{brand.Render("Sessions")}, lines...)
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
