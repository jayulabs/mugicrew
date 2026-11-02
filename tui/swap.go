package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"jayulabs.org/mugicrew/release"
	"jayulabs.org/mugicrew/selfexe"
)

// swapMsg is the probe of a swapped binary: the strict release it now answers, ""
// when it did not answer one yet.
type swapMsg struct{ version string }

// swapped and swapProbe are seams for tests.
var (
	swapped   = selfexe.Swapped
	swapProbe = func(path string) string {
		v, err := selfexe.Version(path)
		if err != nil || !release.Strict(v) {
			return ""
		}
		return v
	}
)

// WithSwap records the startup path's identity (ADR 0406 §5): the refresh tick
// re-stats it, and the first change shows the one-time restart line. An empty
// path (a from-source build), or an unreadable identity, means no check.
func (m Model) WithSwap(path, running string) Model {
	if path == "" {
		return m
	}
	id, err := selfexe.Identify(path)
	if err != nil {
		return m
	}
	m.swapPath, m.swapID, m.swapWatch, m.swapFrom = path, id, true, running
	return m
}

// checkSwap runs on each refresh tick: once the startup path's identity has
// changed, it probes the version off the event loop, one probe at a time, on
// every tick until the binary answers a strict release (a half-extracted file
// does not). swapMsg then shows the line once.
func (m *Model) checkSwap() tea.Cmd {
	if !m.swapWatch || m.swapProbing || !swapped(m.swapPath, m.swapID) {
		return nil
	}
	m.swapProbing = true
	path := m.swapPath
	return func() tea.Msg { return swapMsg{swapProbe(path)} }
}

// swapLine is the one-time status line, worded for an install made by any
// loop or human on the machine.
func swapLine(from, to string) string {
	to = "v" + strings.TrimPrefix(to, "v")
	return "loop upgraded v" + strings.TrimPrefix(from, "v") + "→" + to + "; restart mugicrew to run it here"
}
