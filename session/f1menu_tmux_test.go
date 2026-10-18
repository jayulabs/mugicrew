package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The F1 HITL keys menu against real tmux (ADR 0296, Spec D1–D5): a client attaches
// inside an outer term session; keys and SGR mouse sequences go through it.

type f1rig struct {
	t       *testing.T
	in, out string
}

func (r f1rig) tm(sock string, args ...string) string {
	r.t.Helper()
	b, err := tmuxCmd(sock, append([]string{"-f", "/dev/null"}, args...)...).CombinedOutput()
	if err != nil {
		r.t.Fatalf("tmux %v: %v: %s", args, err, b)
	}
	return strings.TrimSpace(string(b))
}

func (r f1rig) waitFor(what string, ok func() bool) {
	r.t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !ok(); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s; term screen:\n%s", what, r.screen())
		}
	}
}

func (r f1rig) screen() string { return r.tm(r.out, "capture-pane", "-p", "-t", "=term:") }
func (r f1rig) press(k string) { r.tm(r.out, "send-keys", "-t", "=term:", k) }
func (r f1rig) clients() string {
	return r.tm(r.in, "list-clients", "-F", "#{session_name}")
}
func (r f1rig) autofinish(s string) string {
	return r.tm(r.in, "display-message", "-p", "-t", "="+s+":", "#{@autofinish}")
}
func (r f1rig) arm(s string) { r.tm(r.in, "set-option", "-t", "="+s+":", "@autofinish", "1999999999") }
func (r f1rig) closed() bool { return !strings.Contains(r.screen(), "HITL keys") }

// openMenu presses F1 and returns the screen's rows.
func (r f1rig) openMenu() []string {
	r.t.Helper()
	r.press("F1")
	r.waitFor("the menu", func() bool { return strings.Contains(r.screen(), "HITL keys") })
	return strings.Split(r.screen(), "\n")
}

// menuText is the menu's own rows joined, without the screen around them.
func menuText(lines []string) string {
	var m []string
	for _, l := range lines {
		if i := strings.IndexAny(l, "┌│├└"); i >= 0 {
			m = append(m, strings.TrimSpace(l[i:]))
		}
	}
	return strings.Join(m, "\n")
}

// newF1Rig: a private server with the preamble, a TUI session tui-1 with a recorded
// @tty, and HITL sessions mc-a_1 and mc-b_1 (a quote in their cwd); "term" starts
// on mc-a_1.
func newF1Rig(t *testing.T) f1rig {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	dir := t.TempDir()
	r := f1rig{t, filepath.Join(dir, "in.sock"), filepath.Join(dir, "out.sock")}
	t.Cleanup(func() {
		tmuxCmd(r.in, "kill-server").Run()
		tmuxCmd(r.out, "kill-server").Run()
	})
	stub := filepath.Join(dir, "mugicrew")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho \"stub $* pwd=$PWD\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := beadExe
	beadExe = func() string { return stub }
	t.Cleanup(func() { beadExe = old })
	r.tm(r.in, append(preamble(), ";", "new-session", "-d", "-s", "tui-1", "-x", "100", "-y", "30")...)
	for _, s := range []string{"mc-a_1", "mc-b_1"} {
		wd := filepath.Join(dir, s+" it's")
		if err := os.Mkdir(wd, 0o755); err != nil {
			t.Fatal(err)
		}
		r.tm(r.in, "new-session", "-d", "-s", s, "-c", wd, "-x", "100", "-y", "30")
		r.tm(r.in, setArgs(s, [][2]string{{"@bead", strings.Replace(s, "_", ".", 1)}, {"key-table", hitlTable}})...)
	}
	r.tm(r.out, "new-session", "-d", "-s", "term", "-x", "100", "-y", "30",
		"env", "-u", "TMUX", "-u", "TMUX_PANE", "tmux", "-S", r.in, "attach-session", "-t", "=tui-1")
	r.waitFor("term on tui-1", func() bool { return r.clients() == "tui-1" })
	tui := r.tm(r.in, "display-message", "-p", "-t", "=tui-1:", "#{pane_id}")
	if err := recordClient(r.in, tui); err != nil {
		t.Fatal(err)
	}
	if err := switchClient(r.in, tui, "mc-a_1"); err != nil {
		t.Fatal(err)
	}
	r.waitFor("term on mc-a_1", func() bool { return r.clients() == "mc-a_1" })
	return r
}

// The menu lists Back to TUI then Bead pane; only an armed session also gets a
// separator and Cancel; it sits at the bottom-right just above the bar; Escape
// closes it.
func TestF1MenuShape(t *testing.T) {
	r := newF1Rig(t)
	lines := r.openMenu()
	m := menuText(lines)
	if !strings.Contains(m, "Back to TUI (F6)") || !strings.Contains(m, "(F10)") ||
		strings.Index(m, "Back to TUI") > strings.Index(m, "Bead pane") {
		t.Errorf("menu lacks its entries in order:\n%s", m)
	}
	if strings.Contains(m, "Cancel") || strings.Contains(m, "├") {
		t.Errorf("unarmed menu has a cancel entry or separator:\n%s", m)
	}
	bottom := lines[len(lines)-2] // capture is trimmed: the status line is last, the border just above
	if !strings.HasSuffix(bottom, "┘") || len([]rune(bottom)) != 100 {
		t.Errorf("menu bottom %q not at the right edge just above the bar", bottom)
	}
	r.press("Escape")
	r.waitFor("Escape closes the menu", r.closed)

	r.arm("mc-a_1")
	m = menuText(r.openMenu())
	if !strings.Contains(m, "Cancel auto-exit (c)") || !strings.Contains(m, "├") ||
		strings.Index(m, "├") > strings.Index(m, "Cancel") || strings.Index(m, "Bead pane") > strings.Index(m, "├") {
		t.Errorf("armed menu lacks the separator then Cancel after the entries:\n%s", m)
	}
}

// c in the menu disarms only the pressing session and leaves the client put.
func TestF1MenuCancel(t *testing.T) {
	r := newF1Rig(t)
	r.arm("mc-a_1")
	r.arm("mc-b_1")
	r.openMenu()
	r.press("c")
	r.waitFor("c disarms mc-a_1", func() bool { return r.autofinish("mc-a_1") == "" })
	if got := r.autofinish("mc-b_1"); got != "1999999999" {
		t.Errorf("mc-b_1 @autofinish = %q after cancel in mc-a_1, want it still armed", got)
	}
	if got := r.clients(); got != "mc-a_1" {
		t.Errorf("clients %q after cancel, want still on mc-a_1", got)
	}
}

// F10 then F6 in the menu act on the pressing client and its session.
func TestF1MenuActs(t *testing.T) {
	r := newF1Rig(t)
	r.tm(r.in, "switch-client", "-c", r.tm(r.in, "list-clients", "-F", "#{client_tty}"), "-t", "=mc-b_1")
	r.waitFor("term hopped to mc-b_1", func() bool { return r.clients() == "mc-b_1" })
	r.openMenu()
	r.press("F10")
	panes := func(s string) int {
		return len(strings.Fields(r.tm(r.in, "list-panes", "-t", "="+s+":", "-F", "#{pane_id}")))
	}
	r.waitFor("F10 opens the bead pane in mc-b_1", func() bool { return panes("mc-b_1") == 2 })
	bead := r.tm(r.in, "display-message", "-p", "-t", "=mc-b_1:", "#{pane_id}")
	r.waitFor("bead-detail for mc-b.1 from mc-b_1's path", func() bool {
		s := r.tm(r.in, "capture-pane", "-p", "-J", "-t", bead)
		return strings.Contains(s, "stub bead-detail mc-b.1 ") && strings.Contains(s, "mc-b_1 it's")
	})
	if start := r.tm(r.in, "display-message", "-p", "-t", bead, "#{pane_start_command}"); !strings.Contains(start, "#{@bead}") {
		t.Errorf("bead id spliced into the pane command: %s", start)
	}
	if n := panes("mc-a_1"); n != 1 {
		t.Errorf("mc-a_1 got %d panes, want 1", n)
	}
	r.tm(r.in, "select-pane", "-t", "=mc-b_1:.0")
	r.openMenu()
	r.press("F10")
	r.waitFor("a second F10 focuses the bead pane", func() bool {
		return r.tm(r.in, "display-message", "-p", "-t", "=mc-b_1:", "#{pane_id}") == bead
	})
	if n := panes("mc-b_1"); n != 2 {
		t.Errorf("second F10 left %d panes, want 2", n)
	}

	r.openMenu()
	r.press("F6")
	r.waitFor("F6 returns term to tui-1", func() bool { return r.clients() == "tui-1" })

	// no TUI for this terminal: F6 detaches
	r.tm(r.in, "set-option", "-u", "-t", "=tui-1:", ttyOption)
	r.tm(r.in, "switch-client", "-c", r.tm(r.in, "list-clients", "-F", "#{client_tty}"), "-t", "=mc-a_1")
	r.waitFor("term on mc-a_1", func() bool { return r.clients() == "mc-a_1" })
	r.openMenu()
	r.press("F6")
	r.waitFor("F6 detaches", func() bool { return r.clients() == "" })
}

// A click picks an entry with tmux mouse on or off. tmux picks on release, so the
// SGR press is followed by the release.
func TestF1MenuMouse(t *testing.T) {
	for _, mouse := range []string{"on", "off"} {
		t.Run(mouse, func(t *testing.T) {
			r := newF1Rig(t)
			r.tm(r.in, "set-option", "-g", "mouse", mouse)
			r.arm("mc-a_1")
			lines := r.openMenu()
			row, col := -1, -1
			for i, l := range lines {
				if j := strings.Index(l, "Cancel auto-exit"); j >= 0 {
					row, col = 30-(len(lines)-i), len([]rune(l[:j])) // capture is trimmed: count from the bottom
				}
			}
			at := func(b string) string { return "\x1b[<0;" + strconv.Itoa(col+3) + ";" + strconv.Itoa(row+1) + b }
			r.tm(r.out, "send-keys", "-t", "=term:", "-l", at("M"))
			r.tm(r.out, "send-keys", "-t", "=term:", "-l", at("m"))
			r.waitFor("the click cancels", func() bool { return r.autofinish("mc-a_1") == "" })
		})
	}
}
