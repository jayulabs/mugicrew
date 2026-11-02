package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/release"
)

// u in the Loop menu, or a click on the Release notice, upgrades mugicrew: a y/n
// dialog, then the same classify-plan-upgrade-doctor sequence as `mugicrew upgrade`
// through the release package's seams. A print plan only shows its text; a run
// plan stops a live loop first and waits for the lock to clear.

var (
	// upgradePoll is the wait between reads of the loop lock; upgradeWaitMax bounds
	// the wait for a draining loop to finish.
	upgradePoll    = time.Second
	upgradeWaitMax = 10 * time.Minute
)

// upgradePlanMsg is the classified plan, off the event loop.
type upgradePlanMsg struct {
	latest string
	in     release.Install
	plan   release.Action
	st     looplock.Status
	err    error
}

// upgradeStoppedMsg says the loop is stopped (err: it could not be).
type upgradeStoppedMsg struct {
	latest string
	in     release.Install
	plan   release.Action
	err    error
}

// upgradeDoneMsg is the upgrade's outcome: npm's failure, or the doctor summary.
type upgradeDoneMsg struct {
	latest string
	code   int // npm's exit status when it failed
	doctor int // doctor's exit status
	fixes  int // doctor's To-fix items
}

// openUpgrade is u: the confirm dialog naming the target version.
func (m Model) openUpgrade() (tea.Model, tea.Cmd) {
	if m.releaseLatest == "" {
		return m, nil
	}
	if m.upgrading {
		m.setStatus("upgrade in progress")
		return m, nil
	}
	latest := m.releaseLatest
	live := m.loop.State != looplock.Off
	return m.push(promptDialog("Upgrade mugicrew?", func(w int) []string {
		lines := []string{fmt.Sprintf("Upgrade mugicrew to v%s?", latest)}
		if live {
			lines = append(lines, "The loop is on: it is stopped first.")
		}
		return lines
	}, "", func(m Model, _ dialog) (Model, tea.Cmd) {
		m.upgrading = true
		m.setStatus("checking how mugicrew was installed…")
		return m, func() tea.Msg {
			in := release.Classify()
			msg := upgradePlanMsg{latest: latest, in: in, plan: release.Plan(in, latest)}
			if msg.plan.Run != nil {
				msg.st, msg.err = readLoop()
			}
			return msg
		}
	})), nil
}

// upgradePlanned acts on the plan: a print plan shows its text and touches nothing.
func (m Model) upgradePlanned(msg upgradePlanMsg) (tea.Model, tea.Cmd) {
	if msg.plan.Run == nil {
		m.upgrading = false
		m.status = ""
		return m.push(confirmDialog("Upgrade mugicrew", strings.Split(strings.TrimRight(msg.plan.Text, "\n"), "\n"), "", nil)), nil
	}
	if msg.err != nil {
		m.upgrading = false
		m.setStatus("upgrade stopped: " + msg.err.Error())
		return m, nil
	}
	if msg.st.State == looplock.Off {
		return m.upgradeRun(msg.latest, msg.in, msg.plan)
	}
	m.setStatus("stopping loop…")
	return m, func() tea.Msg {
		if stoppable(msg.st) { // a draining loop is waited for, never forced
			if _, err := loopVerb("stop"); err != nil {
				return upgradeStoppedMsg{err: err}
			}
		}
		deadline := time.Now().Add(upgradeWaitMax)
		for {
			st, err := readLoop()
			if err != nil {
				return upgradeStoppedMsg{err: err}
			}
			if st.State == looplock.Off {
				return upgradeStoppedMsg{latest: msg.latest, in: msg.in, plan: msg.plan}
			}
			if time.Now().After(deadline) {
				return upgradeStoppedMsg{err: fmt.Errorf("the loop is still running (pid %d)", st.Pid)}
			}
			time.Sleep(upgradePoll)
		}
	}
}

// upgradeStopped continues once the lock is unheld.
func (m Model) upgradeStopped(msg upgradeStoppedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.upgrading = false
		m.setStatus("upgrade stopped: " + msg.err.Error())
		return m, m.load()
	}
	return m.upgradeRun(msg.latest, msg.in, msg.plan)
}

// upgradeRun is npm then the new binary's doctor, one Cmd off the event loop.
func (m Model) upgradeRun(latest string, in release.Install, plan release.Action) (tea.Model, tea.Cmd) {
	m.setStatus("upgrading to v" + latest + "…")
	return m, func() tea.Msg {
		if _, code := release.RunCaptured(plan.Run); code != 0 {
			return upgradeDoneMsg{latest: latest, code: code}
		}
		out, code := release.RunCaptured([]string{in.Exe, "doctor"})
		return upgradeDoneMsg{latest: latest, doctor: code, fixes: release.FixCount(out)}
	}
}

// upgradeDone reports the outcome on the status line; the TUI does not re-exec.
func (m Model) upgradeDone(msg upgradeDoneMsg) (tea.Model, tea.Cmd) {
	m.upgrading = false
	switch {
	case msg.code != 0:
		m.setStatus(fmt.Sprintf("upgrade failed: npm exited %d", msg.code))
	case msg.fixes > 0:
		m.setStatus(fmt.Sprintf("upgraded to v%s: quit and restart mugicrew · doctor: %d to fix, run mugicrew doctor", msg.latest, msg.fixes))
	case msg.doctor != 0:
		m.setStatus(fmt.Sprintf("upgraded to v%s: quit and restart mugicrew · doctor exited %d, run mugicrew doctor", msg.latest, msg.doctor))
	default:
		m.setStatus(fmt.Sprintf("upgraded to v%s: quit and restart mugicrew · doctor: clean", msg.latest))
	}
	return m, m.load()
}
