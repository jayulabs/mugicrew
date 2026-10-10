package tui

// The Plan usage poller (Spec §4): the reader runs off the event loop on
// its own 300s tick, the Model keeps the state and backoff, and render only
// formats what is cached (ADR 0101, ADR 0098).

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/claudecode"
	"jayulabs.org/mugicrew/contract"
)

// Plan usage cadence and tab-bar thresholds. ponytail: hard-coded, configurable only if asked.
const (
	usageWarnPct    = 80
	usageErrPct     = 95
	usageBackoff    = 300 * time.Second // a RetryAfter with no retry-after
	usageMaxBackoff = 3600 * time.Second
	usageFloor      = 60 * time.Second  // Stats entry and f5 read only past this
	usageStale      = 600 * time.Second // the band dims a good read older than this
)

var (
	clock         = time.Now          // the poller's clock, a var so cadence tests pin it
	usageInterval = 300 * time.Second // the background poll, a var so tests don't wait on its tick

	usageWarnSty = waitSty // the amber warn colour
	usageErrSty  = warn    // the red error colour
)

// usageReader is claudecode.ReadUsage's shape: the token that last got a 401 in,
// the Plan usage and the token it used out.
type usageReader func(skipToken string) (contract.PlanUsage, string, error)

// usageTickMsg fires a background Plan usage poll; usageMsg is one read's result.
type usageTickMsg struct{}

type usageMsg struct {
	usage contract.PlanUsage
	token string
	err   error
}

// WithUsage hands the model its Plan usage reader (main wires claudecode.ReadUsage).
// nil means Plan usage is unavailable and nothing polls, so tui tests never touch
// the network or the credentials file.
func (m Model) WithUsage(read usageReader) Model {
	m.usageRead = read
	return m
}

func usageTick() tea.Cmd {
	return tea.Tick(usageInterval, func(time.Time) tea.Msg { return usageTickMsg{} })
}

// pollUsage is a usageTickMsg: read unless a RetryAfter backoff is in force, and
// re-arm the tick either way.
func (m Model) pollUsage() tea.Cmd {
	if m.usageRead == nil {
		return nil
	}
	if clock().Before(m.usageNext) {
		return usageTick()
	}
	return tea.Batch(m.readUsage(), usageTick())
}

// readUsage is one off-loop read of Plan usage.
func (m Model) readUsage() tea.Cmd {
	read, skip := m.usageRead, m.usageSkip
	return func() tea.Msg {
		u, tok, err := read(skip)
		return usageMsg{u, tok, err}
	}
}

// refreshUsage is Stats entry and f5's read (FR15): only when the last read is
// over usageFloor old and no backoff is in force.
func (m *Model) refreshUsage() tea.Cmd {
	now := clock()
	if m.usageRead == nil || now.Sub(m.usageReadAt) <= usageFloor || now.Before(m.usageNext) {
		return nil
	}
	m.usageReadAt = now // in flight counts, so f5 spam during a slow read sends one
	return m.readUsage()
}

// foldUsage keeps the last good Plan usage, the latest error, the 401 token and
// any RetryAfter backoff.
func (m *Model) foldUsage(msg usageMsg) {
	now := clock()
	m.usageErr, m.usageReadAt = msg.err, now
	var ra *claudecode.RetryAfterError
	switch {
	case msg.err == nil:
		m.usage, m.usageAt = msg.usage, now
	case errors.Is(msg.err, claudecode.ErrTokenExpired):
		m.usageSkip = msg.token // a new token in the file differs, so it clears itself
	case errors.As(msg.err, &ra):
		d := ra.After
		if d == 0 {
			d = usageBackoff
		}
		m.usageNext = now.Add(min(d, usageMaxBackoff))
		m.usageErr = fmt.Errorf("rate-limited, retry %s", m.usageNext.Format("15:04"))
	}
}

// usageWindows is the last good read's windows, labelled as the tab bar and band show them.
func (m Model) usageWindows() []struct {
	label string
	win   *contract.UsageWindow
} {
	return []struct {
		label string
		win   *contract.UsageWindow
	}{{"5h", m.usage.FiveHour}, {"wk", m.usage.Weekly}}
}

// usageWarning is the tab bar's ` · 5h NN% · wk NN%`: each window at usageWarnPct
// or more and not yet reset, error-styled from usageErrPct; "" with no good read
// or nothing over.
func (m Model) usageWarning() string {
	var s string
	for _, w := range m.usageWindows() {
		if w.win == nil || w.win.Pct < usageWarnPct || !w.win.ResetsAt.IsZero() && clock().After(w.win.ResetsAt) {
			continue // a window past its reset no longer holds that % (FR7)
		}
		st := usageWarnSty
		if w.win.Pct >= usageErrPct {
			st = usageErrSty
		}
		s += dim.Render(" · ") + st.Render(fmt.Sprintf("%s %d%%", w.label, int(w.win.Pct)))
	}
	return s
}

// usageBand is the Stats tab's Plan usage line: each window's % and reset time,
// "reset" once past it, dimmed with "as of HH:MM" when stale (FR7, FR10/11);
// with no good read ever, "usage unavailable: <reason>" (FR8, FR36).
func (m Model) usageBand() string {
	switch {
	case m.usageAt.IsZero() && m.usageErr != nil:
		return warn.Render("usage unavailable: " + m.usageErr.Error())
	case m.usageAt.IsZero() && m.usageRead == nil:
		return warn.Render("usage unavailable")
	case m.usageAt.IsZero():
		return dim.Render("reading plan usage…")
	}
	now := clock()
	stale := m.usageErr != nil || now.Sub(m.usageAt) > usageStale
	var parts []string
	for _, w := range m.usageWindows() {
		switch {
		case w.win == nil:
		case w.win.ResetsAt.IsZero():
			parts = append(parts, fmt.Sprintf("%s %d%%", w.label, int(w.win.Pct)))
		case now.After(w.win.ResetsAt):
			parts = append(parts, w.label+" reset")
		default:
			at := w.win.ResetsAt.Local()
			layout := "Jan 2 15:04"
			if y, mo, d := now.Date(); at.Year() == y && at.Month() == mo && at.Day() == d {
				layout = "15:04"
			}
			parts = append(parts, fmt.Sprintf("%s %d%% (resets %s)", w.label, int(w.win.Pct), at.Format(layout)))
		}
	}
	s := strings.Join(parts, " · ")
	if stale {
		return dim.Render(s + " · as of " + m.usageAt.Format("15:04"))
	}
	return s
}
