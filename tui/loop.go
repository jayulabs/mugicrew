package tui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/looplock"
)

// o opens the loop menu (ADR 0288 §4, ADR 0290): the `loop status` text plus the
// options that fit the loop's state. Every option closes the menu and shells out;
// the TUI only runs the verb.

// loopDoneMsg is a loop verb's outcome. draining is set, and no verb ran, when a
// plain stop's re-read found the loop already draining. out is the verb's stdout;
// restart marks the `loop restart` verb.
type loopDoneMsg struct {
	err      error
	out      string
	restart  bool
	draining *looplock.Status
}

const (
	startingLoop   = "starting loop…"
	stoppingLoop   = "stopping loop…"
	resumingLoop   = "resuming loop…"
	restartingLoop = "restarting loop: building main…"
)

// stoppable reports whether a plain stop stops st: an on loop, or a
// restart drain, which the stop turns into a stop drain (ADR 0063 §5); a limited
// loop is live like an on one.
func stoppable(st looplock.Status) bool {
	return st.State == looplock.On || st.State == looplock.Limited || st.State == looplock.Draining && st.RestartTo != ""
}

// loopOpt is one menu option: its key and label.
type loopOpt struct{ key, label string }

// installed reports a live on or limited loop that was not built from source: no
// rev, so no restart (ADR 0288 §4).
func installed(st looplock.Status) bool {
	return st.Pid != 0 && st.Rev == "" && (st.State == looplock.On || st.State == looplock.Limited)
}

// loopOpts is the menu's options for the loop's state, esc not included.
func (m Model) loopOpts() []loopOpt {
	if installed(m.loop) && !m.loopStarting {
		return []loopOpt{{"s", "stop"}}
	}
	switch loopState(m.loop, m.loopStarting) {
	case lkOff:
		return []loopOpt{{"s", "start"}}
	case lkOn, lkLimited:
		return []loopOpt{{"r", "restart"}, {"s", "stop"}}
	case lkRestarting:
		return []loopOpt{{"s", "stop"}}
	case lkStopDrain:
		return []loopOpt{{"k", "keep on"}, {"f", "force stop"}}
	}
	return nil
}

// loopStateStyle colors the state value as the tab bar colors its word (ADR 0288
// §3): dim off, green a plain on, warn otherwise. It reads the Status, never the text.
func loopStateStyle(st looplock.Status) lipgloss.Style {
	switch {
	case st.State == looplock.Off:
		return dimSt
	case st.State == looplock.On && st.FailedRev == "":
		return aliveSt
	}
	return warnSt
}

// loopStatusRows is the shared status lines (ADR 0295) styled for the menu: dim
// labels, a colored state value, a blank row between sections, and values wrapped
// to width so continuation rows hang under the value column (ADR 0295 §4).
func loopStatusRows(st looplock.Status, width int) []string {
	var rows []string
	var prev looplock.Section
	for i, l := range st.Lines() {
		if i > 0 && l.Section != prev {
			rows = append(rows, "")
		}
		prev = l.Section
		prefix := l.Label + ": "
		pad := strings.Repeat(" ", lipgloss.Width(prefix))
		style := lipgloss.NewStyle()
		if l.Section == looplock.SecState && l.Label == "state" {
			style = loopStateStyle(st)
		}
		for j, part := range strings.Split(ansi.Wrap(l.Value, max(width-len(pad), 1), ""), "\n") {
			lead := pad
			if j == 0 {
				lead = softSt.Render(prefix)
			}
			rows = append(rows, lead+style.Render(part))
		}
	}
	return rows
}

// loopMenuBody is the menu's lines above the options line: the shared status
// lines styled by loopStatusRows, then the state's dim line. width is the modal's
// inner width.
func (m Model) loopMenuBody(width int) []string {
	body := loopStatusRows(m.loop, width)
	var note string
	switch {
	case m.loopStarting:
		note = "waiting for the loop to write its lock…"
	case installed(m.loop):
		note = "not from source: stop and start it to change builds"
	}
	if note != "" {
		body = append(body, "", dimSt.Render(note))
	}
	if m.loopRestarting {
		body = append(body, "", warnSt.Render("restart in progress"))
	}
	return body
}

// loopOptsLine is the options line, dimmed while a restart is out.
func (m Model) loopOptsLine() string {
	parts := []string{}
	for _, o := range m.loopOpts() {
		parts = append(parts, o.key+" "+o.label)
	}
	line := strings.Join(append(parts, "esc close"), " · ")
	if m.loopRestarting {
		return dimSt.Render(line)
	}
	return line
}

// openLoopMenu is o.
func (m Model) openLoopMenu() (tea.Model, tea.Cmd) {
	return m.push(dialog{kind: loopMenu, title: "Loop"}), nil
}

// loopMenuKey runs the pressed option and closes the menu; a key that is no
// option, or any key while a restart is out, does nothing. Existing busy rule: a
// verb that has not confirmed keeps s, k and f inert.
func (m Model) loopMenuKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(k, keyCancel) {
		return m.pop(), nil
	}
	if m.loopRestarting || !slices.ContainsFunc(m.loopOpts(), func(o loopOpt) bool { return o.key == k.String() }) {
		return m, nil
	}
	if m.loopBusy && k.String() != "r" {
		return m, nil
	}
	m = m.pop()
	switch k.String() {
	case "r":
		m.setStatus(restartingLoop)
		m.loopRestarting = true
		return m, func() tea.Msg {
			out, err := loopVerb("restart")
			return loopDoneMsg{out: out, err: err, restart: true}
		}
	case "k":
		return m.runLoopVerb(resumingLoop, m.startArgs()...)
	case "f":
		return m.runLoopVerb(stoppingLoop, "stop", "--force")
	}
	if m.loop.State == looplock.Off {
		m.loopStarting = true
		return m.runLoopVerb(startingLoop, m.startArgs()...)
	}
	return m.runLoopVerb(stoppingLoop, "stop")
}

// startArgs is loop start, watched when the TUI is.
func (m Model) startArgs() []string {
	if m.watched {
		return []string{"start", "--watch"}
	}
	return []string{"start"}
}

// WithWatch remembers a TUI opened with `mugicrew --watch`, so the menu starts a
// watched loop (Spec FR18, ADR 0090).
func (m Model) WithWatch(watch bool) Model {
	m.watched = watch
	return m
}

// runLoopVerb shows pending until a read shows the loop left the state the verb
// acted on (the loadedMsg fold), or the verb's error replaces it.
func (m Model) runLoopVerb(pending string, args ...string) (tea.Model, tea.Cmd) {
	m.setStatus(pending)
	m.loopBusy, m.loopFrom = true, m.loop
	return m, func() tea.Msg {
		// m.loop can be up to a tick stale: a CLI stop may already have the loop in a
		// stop drain, and a plain stop then forces. Re-read and confirm instead.
		if stoppable(m.loopFrom) {
			if st, err := readLoop(); err == nil && !stoppable(st) && st.State == looplock.Draining {
				return loopDoneMsg{draining: &st}
			}
		}
		_, err := loopVerb(args...)
		return loopDoneMsg{err: err}
	}
}

// loopDone folds a verb's outcome in. Only a plain stop stays busy until a read
// confirms: a repeat start or force is harmless, and a start whose loop died at
// once would never confirm.
func (m Model) loopDone(msg loopDoneMsg) (tea.Model, tea.Cmd) {
	if msg.restart { // the CLI's own words, exit 0 too (ADR 0290 §3)
		m.loopRestarting = false
		if out := strings.TrimSpace(msg.out); msg.err == nil {
			m.setStatus(out)
		} else {
			m.setStatus(msg.err.Error())
		}
		return m, m.load()
	}
	if msg.draining != nil { // a CLI stop beat o to it: confirm the force
		m.loopBusy, m.loop = false, *msg.draining
		if len(m.modals) > 0 {
			m.setStatus("loop is draining")
			return m, nil
		}
		return m.openLoopMenu()
	}
	if msg.err != nil || !stoppable(m.loopFrom) {
		m.loopBusy = false
	}
	if msg.err != nil {
		m.loopStarting = false
		m.setStatus(msg.err.Error())
	}
	return m, m.load() // the immediate read after o (ADR 0051 §6)
}

// loopKind is a loopState class.
type loopKind string

const (
	lkOff        loopKind = "off"
	lkStarting   loopKind = "starting"
	lkOn         loopKind = "on"
	lkLimited    loopKind = "limited"
	lkRestarting loopKind = "restarting"
	lkStopDrain  loopKind = "stopdrain"
)

// loopState classifies the loop for the tab bar word and the loop menu (ADR 0288
// §3): off, starting, on, limited, restarting (a restart drain), or
// stopdrain. stale is separate: FailedRev non-empty, watched or not.
func loopState(st looplock.Status, starting bool) loopKind {
	switch {
	case starting:
		return lkStarting
	case st.State == looplock.Draining && st.RestartTo != "":
		return lkRestarting
	case st.State == looplock.Draining:
		return lkStopDrain
	case st.State == looplock.On:
		return lkOn
	case st.State == looplock.Limited:
		return lkLimited
	}
	return lkOff
}

// loopSegment is the tab bar's loop state: one word, dim off, green on, warn
// otherwise; `stale` follows an on or limited loop whose build of main failed.
func (m Model) loopSegment() string {
	stale := m.loop.FailedRev != ""
	switch loopState(m.loop, m.loopStarting) {
	case lkStarting:
		return warnSt.Render("loop starting…")
	case lkOn:
		if stale {
			return warnSt.Render("loop on stale")
		}
		return aliveSt.Render("loop on")
	case lkLimited:
		if stale {
			return warnSt.Render("loop limited stale")
		}
		return warnSt.Render("loop limited")
	case lkRestarting:
		return warnSt.Render("loop restarting")
	case lkStopDrain:
		return warnSt.Render("loop draining")
	}
	return dimSt.Render("loop off")
}
