package tui

import (
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
)

// explorerCmd runs $EXPLORER through sh like editorCmd, with the Workspace root as $1 and
// as the working directory; an unresolved root ("") runs at the process cwd. A var so tests
// assert the command without running it.
var explorerCmd = func(explorer, root string) *exec.Cmd {
	arg := root
	if arg == "" {
		arg = "."
	}
	c := exec.Command("sh", "-c", explorer+` "$1"`, "sh", arg)
	c.Dir = root
	return c
}

// execProcess is tea.ExecProcess, a var so tests capture the command and callback.
var execProcess = tea.ExecProcess

// explorerDoneMsg carries the explorer's exit error (nil on a clean exit).
type explorerDoneMsg struct{ err error }

// openExplorer is E: the TUI suspends while $EXPLORER runs at the Workspace root.
func (m Model) openExplorer() (tea.Model, tea.Cmd) {
	explorer := os.Getenv("EXPLORER")
	if explorer == "" {
		m.setStatus("$EXPLORER is unset")
		return m, nil
	}
	return m, execProcess(explorerCmd(explorer, repoRoot()), func(err error) tea.Msg { return explorerDoneMsg{err} })
}
