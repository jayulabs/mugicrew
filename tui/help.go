package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// helpKeyW is the ? reference's key column: the widest key, "space/enter", plus a gap.
const helpKeyW = 12

// helpKeys is the ? reference for a tab (ADR 0058 §4): every key updateKey handles
// there, whatever the selection. Hand-kept: a new key in updateKey goes here too
// (TestHelpCoversUpdateKey catches a key no tab lists).
func helpKeys(t tab, prefix string) []footerKey {
	ks := []footerKey{
		{"q/ctrl+c", "quit"},
		{"←/→", "previous / next tab"},
		{"shift+tab", "previous tab"},
		{"G", "Graph tab"},
		{"I", "Inbox tab"},
		{"S", "Search tab"},
		{"H", "History tab"},
		{"#", "Stats tab"},
		{"*", "Settings tab"},
	}
	switch t {
	case tabGraph:
		ks = append(ks,
			footerKey{"L", "Logs, scoped to the selection"},
			footerKey{"tab", "focus list / detail"},
			footerKey{"j/k/↓/↑", "move or scroll the detail"},
			footerKey{"space/enter", "fold / unfold a group"},
			footerKey{"h", "hide / show closed roots"},
			footerKey{"n", "new Signal"},
			footerKey{"u", "new Surgery: file it and start dictating"},
			footerKey{"f", "step the kind filter"},
			footerKey{"/", "search id + title (regex)"},
			footerKey{"esc", "clear the search, then the kind"},
			footerKey{"l", "launch, attach or resume"},
			footerKey{"b", "launch or resume without switching"},
			footerKey{"t", "run now"},
			footerKey{"p", "pause / unpause"},
			footerKey{"+/=/-", "raise / lower priority"},
			// Spec G's "x abort the effort", widened: x also drops a session.
			footerKey{"x", "drop the session / abort the AFK effort"},
			footerKey{"z", "reject an open, unclaimed Signal"},
			footerKey{"e", "edit title or description"},
			footerKey{"E", "raw bd update"})
	case tabInbox:
		ks[slices.IndexFunc(ks, func(k footerKey) bool { return k.k == "G" })].label = "Graph, revealing the selection" // Spec D2
		ks = append(ks,
			footerKey{"L", "Logs tab"},
			footerKey{"tab", "focus list / detail"},
			footerKey{"j/k/↓/↑", "move or scroll the detail"},
			footerKey{"n", "new Signal"},
			footerKey{"u", "new Surgery: file it and start dictating"},
			footerKey{"f", "step the kind filter"},
			footerKey{"/", "search id + title (regex)"},
			footerKey{"esc", "clear the search, then the kind"},
			footerKey{"l", "launch, attach or resume"},
			footerKey{"b", "launch or resume without switching"},
			footerKey{"+/=/-", "raise / lower priority"},
			footerKey{"x", "drop the session / abort the AFK effort"},
			footerKey{"z", "reject an open, unclaimed Signal"},
			footerKey{"e", "edit title or description"},
			footerKey{"E", "raw bd update"})
	case tabSearch:
		ks = append(ks,
			footerKey{"L", "Logs tab"},
			footerKey{"tab", "focus list / detail"},
			footerKey{"j/k/↓/↑", "move or scroll the detail"},
			footerKey{"enter", "open the detail"},
			footerKey{"G", "reveal the selection in the Graph"},
			footerKey{"/", "live word search (not a regex, unlike other tabs)"},
			footerKey{"esc", "clear the search text"},
			footerKey{"f", "pick the status filter"},
			footerKey{"c", "pick the kind filter"},
			footerKey{"s", "pick the sort"},
			footerKey{"z", "reject an open, unclaimed Signal"})
	case tabHistory:
		ks = append(ks,
			footerKey{"L", "Logs tab"},
			footerKey{"j/k/↓/↑", "move, skipping hidden lines"},
			footerKey{"enter/G", "reveal the Checkpoint in the Graph"},
			footerKey{"R", "roll back: file a Rollback of the Checkpoint"},
			footerKey{"", fmt.Sprintf("History reads main's newest %d commits", historyCap)})
	case tabStats:
		ks = append(ks,
			footerKey{"L", "Logs tab"},
			footerKey{"w", "window: 24h / 7d / 30d / all"},
			footerKey{"b", "group: none / crewmate / model / day"})
	case tabSettings:
		ks = append(ks,
			footerKey{"L", "Logs tab"},
			footerKey{"j/k/↓/↑", "select a row"},
			footerKey{"+/-", "stage the row: pool ±1, Auto-exit ±30s"},
			footerKey{"enter", "save the staged value (this machine only)"},
			footerKey{"esc", "discard the staged value"})
	default:
		ks = append(ks,
			footerKey{"L", "Logs tab"},
			footerKey{"tab", "focus list / detail"},
			footerKey{"j/k/↓/↑", "move or scroll the detail"},
			footerKey{"d", "show / hide debug records"},
			footerKey{"e", "errors only / all"},
			footerKey{"f", "step the kind filter (the event's bead)"},
			footerKey{"/", "search msg + bead + attrs (regex)"},
			footerKey{"esc", "clear the search, then the kind, then the scope"})
	}
	return append(ks,
		footerKey{"o", "start / stop the loop"},
		footerKey{"F5", "refresh"},
		footerKey{"?", "this reference"},
		footerKey{prefix + " ?", "tmux keys — all tmux commands work"},
		footerKey{prefix + " d", "leave tmux — quits mugicrew"})
}

// prefixCmd reads the tmux prefix once (ADR 0066 §2) as the reference's notation,
// C-b → ctrl+b; a failed read keeps tmux's default.
func (m Model) prefixCmd() tea.Cmd {
	return func() tea.Msg {
		p, err := readPrefix()
		if err != nil || p == "" {
			p = "C-b"
		}
		return prefixMsg(strings.NewReplacer("C-", "ctrl+", "M-", "alt+", "S-", "shift+").Replace(p))
	}
}

// helpView is the ? reference in place of the body: one column, no border, each
// line cut to the width so nothing wraps; j/k scroll it (ADR 0058 §4).
func (m Model) helpView(height int) string {
	ks := helpKeys(m.tab, m.prefix)
	lines := make([]string, len(ks))
	for i, k := range ks {
		lines[i] = trunc(selMarker.Render(k.k)+strings.Repeat(" ", max(1, helpKeyW-len([]rune(k.k))))+k.label, m.w)
	}
	return strings.Join(windowRows(strings.Join(lines, "\n"), m.w, height, m.helpOffset), "\n")
}

// updateHelp handles keys while the reference is open: ?/esc/q close it (q never
// quits), j/k scroll, everything else is swallowed.
func (m Model) updateHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "?", "esc", "q":
		m.help, m.helpOffset = false, 0
	case "j", "down":
		m.helpOffset = min(m.helpOffset+1, m.helpMaxOffset())
	case "k", "up":
		m.helpOffset = max(min(m.helpOffset, m.helpMaxOffset())-1, 0) // a resize may have shrunk the range
	}
	return m, nil
}

// helpMaxOffset is the last scroll offset that still fills the body.
func (m Model) helpMaxOffset() int { return max(0, len(helpKeys(m.tab, m.prefix))-m.paneHeight()) }
