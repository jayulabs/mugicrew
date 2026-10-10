package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/looplock"
)

// The loop seams (ADR 0051): readLoop is the lock probe `loop status` reads,
// readTip a watched loop's source mainline tip (the local ref the watch follows,
// ADR 0090 §5), and loopVerb runs `<self> loop <args>`. The TUI shells out and
// never owns the loop (§2), so a TUI-started loop outlives the TUI (setsid, ADR 0053 §2).
var (
	readTip  = git.MainlineAt
	readLoop = func() (looplock.Status, error) {
		common, err := git.CommonDir()
		if err != nil {
			return looplock.Status{}, err
		}
		path, err := looplock.Resolve(common)
		if err != nil {
			return looplock.Status{}, err
		}
		return looplock.Read(path)
	}
	loopVerb = func(args ...string) error {
		self, err := os.Executable()
		if err != nil {
			return err
		}
		out, err := execCommand(self, append([]string{"loop"}, args...)...).CombinedOutput()
		if err != nil {
			if s := strings.TrimSpace(string(out)); s != "" {
				return errors.New(s) // the CLI's own words, verbatim (§2)
			}
		}
		return err
	}
)

// loopDoneMsg is a loop verb's outcome. draining is set, and no verb ran, when a
// plain stop's re-read found the loop already draining.
type loopDoneMsg struct {
	err      error
	draining *looplock.Status
}

const (
	startingLoop = "starting loop…"
	stoppingLoop = "stopping loop…"
)

// stoppable reports whether o stops st with a plain stop: an on loop, or a
// restart drain, which the stop turns into a stop drain (ADR 0063 §5).
func stoppable(st looplock.Status) bool {
	return st.State == looplock.On || st.State == looplock.Draining && st.RestartTo != ""
}

// toggleLoop is o (ADR 0051 §1–3): off starts, on (or a restart drain) stops, a
// stop drain opens the force confirm. A press before a read confirms a stop is a
// no-op: m.loop is stale until then, and a second stop forces (FR38). Otherwise a
// press while a verb runs is a no-op until the verb returns or a read shows it
// took effect.
func (m Model) toggleLoop() (tea.Model, tea.Cmd) {
	if m.loopBusy {
		return m, nil
	}
	switch {
	case stoppable(m.loop):
		return m.runLoopVerb(stoppingLoop, "stop")
	case m.loop.State == looplock.Draining:
		m.editMode = editConfirmForceStop
		m.status = confirmForcePrompt(m.loop.Inflight, m.w)
		return m, nil
	}
	m.loopStarting = true
	if m.watch {
		return m.runLoopVerb(startingLoop, "start", "--watch")
	}
	return m.runLoopVerb(startingLoop, "start")
}

// WithWatch remembers a TUI opened with `mugicrew --watch`, so o starts a
// watched loop (Spec FR18, ADR 0090).
func (m Model) WithWatch(watch bool) Model {
	m.watch = watch
	return m
}

// runLoopVerb shows pending until a read shows the loop left the state the verb
// acted on (Update's loadedMsg), or the verb's error replaces it.
func (m Model) runLoopVerb(pending string, args ...string) (tea.Model, tea.Cmd) {
	m.status, m.loopBusy, m.loopFrom = pending, true, m.loop
	return m, func() tea.Msg {
		// m.loop can be up to a tick stale: a CLI stop may already have the loop
		// in a stop drain, and a plain stop then forces. Re-read and confirm instead (FR38).
		if stoppable(m.loopFrom) {
			if st, err := readLoop(); err == nil && !stoppable(st) && st.State == looplock.Draining {
				return loopDoneMsg{draining: &st}
			}
		}
		return loopDoneMsg{err: loopVerb(args...)}
	}
}

// loopDone folds a verb's outcome in. Only a plain stop stays busy until a read
// confirms: a repeat start or force is harmless, and a start whose loop died at
// once would never confirm.
func (m Model) loopDone(msg loopDoneMsg) (tea.Model, tea.Cmd) {
	if msg.draining != nil { // a CLI stop beat o to it: confirm the force (FR38)
		m.loopBusy, m.loop = false, *msg.draining
		if m.editMode != editNone {
			m.setStatus("loop is draining")
			return m, nil
		}
		return m.toggleLoop()
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

// loopLabel is o's footer label: what pressing it does from the current state.
func (m Model) loopLabel() string {
	switch {
	case stoppable(m.loop):
		return "loop off"
	case m.loop.State == looplock.Draining:
		return "force-stop"
	}
	return "loop on"
}

// loopSegment is the tab bar's loop state (ADR 0051 §4): dim off, green on, warn
// draining with its in-flight count; withRev appends ` @<short rev>` of the
// pinned build when the lock body names one (ADR 0060 §5), or ` @latest` when a
// watched loop's rev is its source's mainline tip (m.loopTip, a full-SHA match).
// An on loop whose build of main failed is warn and ends ` stale`; a restart
// drain is warn `loop restarting N @old→new` (ADR 0063 §4); both keep their
// hashes. All three apply only to a watched loop: an unwatched one follows
// nothing, so it is never stale nor latest (ADR 0090 §7). From o's start until a
// read confirms it, the segment is warn `loop starting…`.
func (m Model) loopSegment(withRev bool) string {
	if m.loopStarting {
		return warn.Render("loop starting…")
	}
	rev := ""
	if withRev && m.loop.Rev != "" {
		rev = " @" + m.loop.Rev[:min(7, len(m.loop.Rev))]
	}
	if withRev && m.loop.Watch && m.loopTip != "" && m.loopTip == m.loop.Rev && m.loop.FailedRev == "" {
		rev = " @latest"
	}
	if m.loop.Watch && m.loop.State == looplock.Draining && m.loop.RestartTo != "" {
		if withRev {
			rev = " @" + m.loop.Rev[:min(7, len(m.loop.Rev))] + "→" + m.loop.RestartTo[:min(7, len(m.loop.RestartTo))]
		}
		return warn.Render(fmt.Sprintf("loop restarting %d%s", len(m.loop.Inflight), rev))
	}
	switch m.loop.State {
	case looplock.On:
		if m.loop.Watch && m.loop.FailedRev != "" {
			return warn.Render("loop on" + rev + " stale")
		}
		return aliveSty.Render("loop on" + rev)
	case looplock.Draining:
		return warn.Render(fmt.Sprintf("loop draining %d%s", len(m.loop.Inflight), rev))
	}
	return dim.Render("loop off")
}

// confirmForcePrompt is the force-stop confirm (ADR 0051 §3): `N efforts in
// flight will be abandoned: <title>…, <title>… +N — force stop?`.
func confirmForcePrompt(inflight []string, w int) string {
	return foldTitles(fmt.Sprintf("%d efforts in flight will be abandoned: ", len(inflight)), inflight, " — force stop?", w)
}
