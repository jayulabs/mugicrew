package tui

// The Settings tab (ADR 0109): one full-width pane of setting rows,
// each drawn from what the refresh cached (ADR 0101): View never reads a file.

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/loop"
)

// readPool is the refresh's layered AFK pool read, a var so tests run without a live repo.
var readPool = func() loop.Setting {
	root, _ := git.RepoRoot() // unresolved ⇒ "" ⇒ the layers read relative to cwd, as run-now does
	return loop.PoolSetting(root)
}

// savePool is enter's local override write, a var so tests stub it as they stub bd.
var savePool = func(n int) error {
	root, _ := git.RepoRoot()
	return loop.SetLocalPool(root, n)
}

// readAutoExit is the refresh's layered auto_exit read, a var as readPool is.
var readAutoExit = func() loop.AutoExit {
	root, _ := git.RepoRoot()
	return loop.AutoExitSetting(root)
}

// saveAutoExit is enter's local auto_exit write, a var as savePool is.
var saveAutoExit = func(d time.Duration) error {
	root, _ := git.RepoRoot()
	return loop.SetLocalAutoExit(root, d)
}

// settingsKeys are the keys Settings handles: the global ones plus the row cursor's and the staged edit's.
var settingsKeys = []string{"q", "ctrl+c", "left", "shift+tab", "right", "G", "I", "S", "H", "L", "#", "*", "?", "o", "f5", "j", "k", "down", "up", "+", "-", "enter", "esc"}

// poolKey and autoExitKey are the saves' pending keys (ADR 0098 §3) and status prefixes.
const (
	poolKey     = "AFK pool"
	autoExitKey = "Auto-exit"
)

// autoExitStep is one +/- on the Auto-exit row (Spec FR27).
const autoExitStep = 30 * time.Second

// settingsKey is the staged edit (Spec FR10-16, FR21-24; D10):
// j/k pick the row; +/- step the selected row's staged value and write nothing;
// esc discards it; enter saves it as a press-time write under the row's pending
// key. ok is false for keys it leaves to updateKey.
func (m Model) settingsKey(k string) (_ tea.Model, _ tea.Cmd, ok bool) {
	switch k {
	case "j", "down":
		m.setRow = 1
		return m, nil, true
	case "k", "up":
		m.setRow = 0
		return m, nil, true
	}
	if m.setRow == 1 {
		return m.autoExitEdit(k)
	}
	cur := m.staged
	if cur == 0 {
		cur = m.pool.N
	}
	switch k {
	case "+", "-":
		if k == "-" && cur <= 1 {
			return m, nil, true
		}
		if m.staged == 0 {
			m.stagedFrom = m.pool.N
		}
		m.staged = cur + 1
		if k == "-" {
			m.staged = cur - 1
		}
		if m.staged == m.pool.N { // stepped back to the value in force: nothing staged
			m.staged = 0
		}
	case "esc":
		m.staged = 0
	case "enter":
		n := m.staged
		if n == 0 || n == m.pool.N {
			return m, nil, true
		}
		next, cmd := m.startWrite(poolKey, "saving", func() writeDoneMsg {
			err := savePool(n)
			return writeDoneMsg{poolKey, func(m Model) (tea.Model, tea.Cmd) {
				if err != nil {
					m.setStatus(poolKey + " save failed: " + err.Error())
					return m, nil
				}
				if m.staged == n { // a value staged during the save is the human's next edit
					m.staged = 0
				}
				m.setStatus(fmt.Sprintf("%s %d saved (this machine)", poolKey, n))
				return m, m.load()
			}}
		})
		return next, cmd, true
	default:
		return m, nil, false
	}
	return m, nil, true
}

// autoExitEdit is settingsKey on the Auto-exit row: steps of 30s, floor off, no ceiling.
func (m Model) autoExitEdit(k string) (_ tea.Model, _ tea.Cmd, ok bool) {
	cur := m.exit.D
	if m.exitStaged {
		cur = m.stagedExit
	}
	switch k {
	case "+", "-":
		if !m.exitStaged {
			m.exitFrom = m.exit.D
		}
		m.stagedExit = cur + autoExitStep
		if k == "-" {
			m.stagedExit = max(cur-autoExitStep, 0)
		}
		m.exitStaged = m.stagedExit != m.exit.D // back at the value in force: nothing staged
	case "esc":
		m.exitStaged = false
	case "enter":
		d := m.stagedExit
		if !m.exitStaged || d == m.exit.D {
			return m, nil, true
		}
		next, cmd := m.startWrite(autoExitKey, "saving", func() writeDoneMsg {
			err := saveAutoExit(d)
			return writeDoneMsg{autoExitKey, func(m Model) (tea.Model, tea.Cmd) {
				if err != nil {
					m.setStatus(autoExitKey + " save failed: " + err.Error())
					return m, nil
				}
				if m.stagedExit == d { // a value staged during the save is the human's next edit
					m.exitStaged = false
				}
				m.setStatus(fmt.Sprintf("%s %s saved (this machine)", autoExitKey, fmtAutoExit(d)))
				return m, m.load()
			}}
		})
		return next, cmd, true
	default:
		return m, nil, false
	}
	return m, nil, true
}

// settingsStaged is whether either row holds an unsaved edit.
func (m Model) settingsStaged() bool { return m.staged != 0 || m.exitStaged }

// fmtAutoExit shows an Auto-exit duration as 1m, 1m30s, 45s, 1h or off.
func fmtAutoExit(d time.Duration) string {
	if d == 0 {
		return "off"
	}
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// settingRow is one setting: its name, its value in force, and a one-line gloss.
type settingRow struct{ name, value, gloss string }

// settingRows are the tab's rows; a later setting appends one (FR37).
func (m Model) settingRows() []settingRow {
	shared := ""
	if m.pool.Shared > 0 {
		shared = fmt.Sprint(m.pool.Shared)
	}
	n := fmt.Sprint(m.pool.N)
	if m.staged != 0 {
		n = stagedValue(fmt.Sprint(m.stagedFrom), fmt.Sprint(m.staged), n)
	}
	eshared := ""
	if m.exit.SharedSet {
		eshared = fmtAutoExit(m.exit.Shared)
	}
	e := fmtAutoExit(m.exit.D)
	if m.exitStaged {
		e = stagedValue(fmtAutoExit(m.exitFrom), fmtAutoExit(m.stagedExit), e)
	}
	return []settingRow{{
		name:  poolKey,
		value: fmt.Sprintf("%s  (%s)  %d in flight", n, sourceText(m.pool.Source, m.pool.Unusable, shared), m.inflight),
		gloss: "how many AFK efforts the loop and run-now start at once on this machine",
	}, {
		name:  autoExitKey,
		value: fmt.Sprintf("%s  (%s)", e, sourceText(m.exit.Source, m.exit.Unusable, eshared)),
		gloss: "how long a HITL session waits after its closing line before it exits by itself",
	}}
}

// sourceText is a row's source: local, config.toml or default, unusable → that
// fallback, or local; shared <the checked-in value> ("" = none set).
func sourceText(source, unusable, shared string) string {
	switch {
	case unusable != "":
		return "unusable → " + source
	case shared != "":
		return source + "; shared " + shared
	}
	return source
}

// stagedValue is a staged row's value, noting a refresh that moved the base under
// the edit (Spec FR26).
func stagedValue(from, to, now string) string {
	if now != from {
		return fmt.Sprintf("%s → %s (unsaved; file now %s)", from, to, now)
	}
	return fmt.Sprintf("%s → %s (unsaved)", from, to)
}

// settingsView is the Settings body: each row, its gloss dimmed under it.
func (m Model) settingsView() string {
	var lines []string
	for i, r := range m.settingRows() {
		name := r.name
		if i == m.setRow { // the row +/- act on
			name = selMarker.Render(name)
		}
		lines = append(lines, trunc(name+"  "+r.value, m.w), trunc(dim.Render("  "+r.gloss), m.w))
	}
	return strings.Join(lines, "\n")
}
