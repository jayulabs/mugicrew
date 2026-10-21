package tui

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/git"
)

var keyOpenMR = verbKey("w", "open MR")

// browserCmds are the launch candidates for url, tried in order: each $BROWSER entry run
// through sh like explorerCmd (url as $1, never parsed by the shell), else xdg-open on a
// linux with a display, else open on darwin. A var so tests assert argv without running it.
var browserCmds = func(url string) []*exec.Cmd {
	var out []*exec.Cmd
	if env := os.Getenv("BROWSER"); env != "" {
		for _, e := range strings.Split(env, ":") {
			if e = strings.TrimSpace(e); e == "" {
				continue
			}
			if strings.Contains(e, "%s") {
				e = strings.ReplaceAll(e, "%s", `"$1"`)
			} else {
				e += ` "$1"`
			}
			out = append(out, exec.Command("sh", "-c", e, "sh", url))
		}
		return out
	}
	switch runtime.GOOS {
	case "linux":
		if os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" { // as xdg-open's own has_display: no tab on a far screen over SSH
			out = append(out, exec.Command("xdg-open", url))
		}
	case "darwin":
		out = append(out, exec.Command("open", url))
	}
	return out
}

// runDetached runs c to completion without touching the terminal: stdio is /dev/null and
// it leads its own session. Run, not Start, so no zombie is left and the exit status is
// kept. A var so tests run nothing.
var runDetached = func(c *exec.Cmd) error {
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return c.Run()
}

// browserDoneMsg is a launch's outcome: err is nil when a candidate ran clean, else the
// last failure (noBrowserErr when there was none).
type browserDoneMsg struct {
	url, ref string
	err      error
}

type noBrowserErr struct{}

func (noBrowserErr) Error() string { return "no browser candidate" }

// openMR is w on a Merge Requests row: the TUI stays up while the browser launches.
func (m Model) openMR(w *bd.Bead, ref string) (tea.Model, tea.Cmd) {
	url := git.RecordedMRLink(w.Description)
	if url == "" {
		m.setStatus("no link yet for " + ref + " (waiting on Jinbe's backfill)")
		return m, nil
	}
	return m, func() tea.Msg {
		var err error = noBrowserErr{}
		for _, c := range browserCmds(url) {
			if err = runDetached(c); err == nil {
				break
			}
		}
		return browserDoneMsg{url: url, ref: ref, err: err}
	}
}

// browserDone says what the launch did; a failure copies the URL over OSC 52.
func (m Model) browserDone(msg browserDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err == nil {
		m.setStatus("opened " + msg.ref + " in browser")
		return m, nil
	}
	m.setStatus("couldn't open a browser; copied " + msg.ref + "'s link")
	return m, func() tea.Msg { copyWriter(msg.url); return nil }
}
