package tui

import (
	"errors"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// stubExec captures what E would run instead of suspending the TUI.
func stubExec(t *testing.T) (*[]*exec.Cmd, *tea.ExecCallback) {
	t.Helper()
	var cmds []*exec.Cmd
	var cb tea.ExecCallback
	seam(t, &execProcess, func(c *exec.Cmd, f tea.ExecCallback) tea.Cmd {
		cmds, cb = append(cmds, c), f
		return func() tea.Msg { return nil }
	})
	return &cmds, &cb
}

func TestEOpensExplorerAtRoot(t *testing.T) {
	cmds, _ := stubExec(t)
	t.Setenv("EXPLORER", "yazi --x")
	root := repoRoot()
	arg := root
	if arg == "" {
		arg = "."
	}
	m := sized(t, 100, 30)
	for _, tab := range []string{"1", "2", "3", "4", "5", "6", "#", "$", "*"} {
		m = press(t, m, runeKey(tab))
		*cmds = nil
		_, cmd := send(t, m, runeKey("E"))
		if cmd == nil || len(*cmds) != 1 {
			t.Fatalf("tab %s: E ran %d commands", tab, len(*cmds))
		}
		c := (*cmds)[0]
		if want := []string{"sh", "-c", `yazi --x "$1"`, "sh", arg}; !slices.Equal(c.Args, want) || c.Dir != root {
			t.Errorf("tab %s: args %q dir %q", tab, c.Args, c.Dir)
		}
	}
	f, _ := defaultFocus(t)
	f = ideaFocus(t, f)
	*cmds = nil
	send(t, f, runeKey("E"))
	if len(*cmds) != 1 {
		t.Errorf("focus view: E ran %d commands", len(*cmds))
	}
}

func TestEUnsetRunsNothing(t *testing.T) {
	cmds, _ := stubExec(t)
	t.Setenv("EXPLORER", "")
	m, _ := send(t, sized(t, 100, 30), runeKey("E"))
	if len(*cmds) != 0 || !strings.Contains(m.status, "$EXPLORER is unset") {
		t.Errorf("unset: ran %d, status %q", len(*cmds), m.status)
	}
}

func TestExplorerCmdUnresolvedRoot(t *testing.T) {
	if c := explorerCmd("ls", ""); c.Dir != "" || c.Args[len(c.Args)-1] != "." {
		t.Errorf("dir %q args %q", c.Dir, c.Args)
	}
}

func TestExplorerDone(t *testing.T) {
	_, cb := stubExec(t)
	t.Setenv("EXPLORER", "ls")
	m, _ := send(t, sized(t, 100, 30), runeKey("E"))
	if msg := (*cb)(nil); msg != (explorerDoneMsg{}) {
		t.Fatalf("clean exit msg %v", msg)
	}
	clean, cmd := send(t, m, (*cb)(nil))
	if clean.status != "" || cmd != nil {
		t.Errorf("clean exit: status %q, cmd %v", clean.status, cmd != nil)
	}
	bad, _ := send(t, m, (*cb)(errors.New("exit status 1")))
	if bad.status != "explorer: exit status 1" {
		t.Errorf("status %q", bad.status)
	}
}

func TestEIsInertUnderDialogInputAndRef(t *testing.T) {
	cmds, _ := stubExec(t)
	t.Setenv("EXPLORER", "ls")
	for name, m := range map[string]Model{
		"dialog": pressN(t, formModel(t), "s"),
		"input":  press(t, press(t, graphModel(t, 100, 30), runeKey("4")), runeKey("/")),
		"ref":    press(t, sized(t, 100, 30), runeKey("?")),
	} {
		send(t, m, runeKey("E"))
		if len(*cmds) != 0 {
			t.Errorf("%s: E ran the explorer", name)
		}
	}
}

func TestNoSurfaceBindsRawEdit(t *testing.T) {
	m, _ := defaultFocus(t)
	surfaces := map[string]Model{"focus": ideaFocus(t, m)}
	m = sized(t, 200, 60)
	for _, tab := range []string{"1", "2", "3", "4", "5", "6", "#", "$", "*"} {
		surfaces["tab "+tab] = press(t, m, runeKey(tab))
	}
	for name, sm := range surfaces {
		for _, b := range slices.Concat(sm.tabBindings(), sm.tabExtra()) {
			if b.Help().Desc == "raw" {
				t.Errorf("%s binds a raw edit: %v", name, b.Keys())
			}
		}
	}
}

func TestEInRefAfterLoopNeverInHints(t *testing.T) {
	m := sized(t, 200, 60)
	v := ansi.Strip(press(t, m, runeKey("?")).View())
	if !regexp.MustCompile(`loop[^\n]*\n[^\n]*E\s+explorer`).MatchString(v) {
		t.Errorf("? lacks E explorer right after o loop:\n%s", v)
	}
	for _, tab := range []string{"1", "2", "3", "4", "5", "6", "#", "$", "*"} {
		tm := press(t, m, runeKey(tab))
		if strings.Contains(hintsOf(tm), "explorer") {
			t.Errorf("tab %s: hints bar shows E", tab)
		}
	}
}
