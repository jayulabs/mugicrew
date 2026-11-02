package tui

import tea "github.com/charmbracelet/bubbletea"

// releaseCheck is the Release notice's check: the newer release's version, or ""
// when there is nothing to show (from-source, opted out, failed, not newer).
type releaseCheck func() string

// releaseMsg is one check's result.
type releaseMsg struct{ latest string }

// WithRelease hands the model its Release notice check (main wires the release
// package's). nil means no notice and no check, so tui tests never touch the
// network or the user cache.
func (m Model) WithRelease(check releaseCheck) Model {
	m.releaseRead = check
	return m
}

// checkRelease runs the check off the event loop.
func (m Model) checkRelease() tea.Cmd {
	check := m.releaseRead
	if check == nil {
		return nil
	}
	return func() tea.Msg { return releaseMsg{check()} }
}

// releaseSegment is the tab bar's Release notice: ↑ v<latest> in the warn style
// while a newer release is known; "" otherwise.
func (m Model) releaseSegment() string {
	if m.releaseLatest == "" {
		return ""
	}
	return warnSt.Render("↑ v" + m.releaseLatest)
}
